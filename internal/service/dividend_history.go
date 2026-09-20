// dividend_history.go — 종목별 배당 이력 조회와 성장률 추정.
//
// 로드맵의 배당 가정을 수기로 관리하면 금방 낡는다. 보유 종목에서 바로
// 뽑아 쓰도록, 야후의 같은 chart API 에 events=div 를 붙여 5년치 배당
// 이벤트를 받아 TTM 배당과 3개년 성장률을 낸다.
package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

// dividendYieldFloor는 배당 종목으로 볼 최소 배당률이다. 이보다 낮으면
// 금액이 미미해 궤적에 영향이 없고, NVDA 처럼 $0.02 → $0.04 같은 변화가
// 성장률만 폭발적으로 보이게 만든다.
const dividendYieldFloor = 0.01

type dividendEvent struct {
	Date   int64   `json:"date"`
	Amount float64 `json:"amount"`
}

type cachedDividends struct {
	events    []dividendEvent
	fetchedAt time.Time
}

// DividendHistory는 한 종목의 배당 이력에서 뽑은 요약이다.
type DividendHistory struct {
	Symbol string
	// PerPayment는 최근 1회 지급액이다. TTM 합계는 인상 전 분기가 섞여 있어
	// 앞으로 받을 금액보다 작다.
	PerPayment    float64
	PaymentMonths []int   // 최근 1년간 지급이 있었던 달
	RaiseMonth    int     // 가장 최근에 지급액이 오른 달
	AnnualPS      float64 // PerPayment × 연 지급 횟수 (현재 지급률 기준)
	CAGR3Y        float64 // 최근 3개년 연 성장률. 이력이 모자라면 0
	LastYear      int     // PerPayment 기준 시점
	LastMonth     int
}

// FetchDividendHistory는 종목의 TTM 배당과 3개년 성장률을 낸다.
// 배당이 없는 종목은 (nil, nil) 이다.
func (p *PriceService) FetchDividendHistory(ctx context.Context, symbol, exchange string) (*DividendHistory, error) {
	ys := toYahooSymbol(symbol, exchange)

	p.mu.RLock()
	cached, ok := p.dividends[ys]
	p.mu.RUnlock()

	events := cached.events
	if !ok || time.Since(cached.fetchedAt) >= cacheTTL {
		url := fmt.Sprintf("%s/%s?interval=1mo&range=6y&events=div", yahooBaseURL, ys)
		var yr yahooDividendResponse
		if err := p.doRequest(ctx, url, &yr); err != nil {
			return nil, err
		}
		if len(yr.Chart.Result) == 0 {
			return nil, fmt.Errorf("no dividend data for %s", symbol)
		}
		events = make([]dividendEvent, 0, len(yr.Chart.Result[0].Events.Dividends))
		for _, e := range yr.Chart.Result[0].Events.Dividends {
			events = append(events, e)
		}
		p.mu.Lock()
		p.dividends[ys] = cachedDividends{events: events, fetchedAt: time.Now()}
		p.mu.Unlock()
	}

	if len(events) == 0 {
		return nil, nil
	}
	return summarizeDividends(symbol, events, time.Now().UTC()), nil
}

type yahooDividendResponse struct {
	Chart struct {
		Result []struct {
			Events struct {
				Dividends map[string]dividendEvent `json:"dividends"`
			} `json:"events"`
		} `json:"result"`
	} `json:"chart"`
}

// summarizeDividends는 배당 이벤트에서 현재 지급률과 3개년 성장률을 낸다.
//
// 앞으로 받을 금액은 최근 1회 지급액이 기준이다. TTM 합계는 인상 전 분기가
// 섞여 있어 과소평가된다 — UNH 는 6월에 올랐는데 TTM 에는 3월치 옛 금액이
// 남아 있다.
//
// 인상 월은 지급액이 마지막으로 오른 달이다. 종목마다 다르고(UNH 6월,
// MCD 12월) 연초 일괄 인상으로 보면 한 해치가 어긋난다.
//
// 성장률은 완결된 연도끼리만 비교한다 — 진행 중인 올해를 넣으면 아직 안 받은
// 분기 때문에 성장률이 음수로 나온다.
func summarizeDividends(symbol string, events []dividendEvent, now time.Time) *DividendHistory {
	out := &DividendHistory{Symbol: symbol}
	if len(events) == 0 {
		return out
	}

	sort.Slice(events, func(i, j int) bool { return events[i].Date < events[j].Date })

	byYear := make(map[int]float64, 8)
	monthSeen := make(map[int]bool, 12)
	ttmFrom := now.AddDate(-1, 0, 0).Unix()
	var prev float64
	for _, e := range events {
		t := time.Unix(e.Date, 0).UTC()
		byYear[t.Year()] += e.Amount
		if e.Date >= ttmFrom {
			monthSeen[int(t.Month())] = true
		}
		if prev > 0 && e.Amount > prev {
			out.RaiseMonth = int(t.Month())
		}
		prev = e.Amount
	}

	last := events[len(events)-1]
	lastAt := time.Unix(last.Date, 0).UTC()
	out.PerPayment = last.Amount
	out.LastYear, out.LastMonth = lastAt.Year(), int(lastAt.Month())

	out.PaymentMonths = make([]int, 0, len(monthSeen))
	for m := 1; m <= 12; m++ {
		if monthSeen[m] {
			out.PaymentMonths = append(out.PaymentMonths, m)
		}
	}
	out.AnnualPS = out.PerPayment * float64(len(out.PaymentMonths))
	if out.RaiseMonth == 0 {
		out.RaiseMonth = out.LastMonth // 인상 이력이 없으면 최근 지급 달을 기준으로 둔다
	}

	lastDone := now.Year() - 1
	if base := lastDone - 3; byYear[base] > 0 && byYear[lastDone] > 0 {
		out.CAGR3Y = math.Pow(byYear[lastDone]/byYear[base], 1.0/3.0) - 1
	}
	return out
}

// MeetsDividendYieldFloor는 배당 종목으로 볼 만한 배당률인지 본다.
func MeetsDividendYieldFloor(annualPS, price float64) bool {
	return price > 0 && annualPS/price >= dividendYieldFloor
}

// DividendPlanFor는 배당 이력을 계산에 쓸 형태로 바꾼다.
//
// 배당률 판정은 하지 않는다 — 기준에 못 미쳐도 사용자가 직접 고르면 넣어야
// 하기 때문이다. 성장률은 그 종목의 최근 3개년 실적을 그대로 쓴다. 감속 같은
// 전망은 사람이 넣을 값이지 과거 데이터에서 나오지 않는다.
func DividendPlanFor(symbol string, shares, price float64, h *DividendHistory) (models.DividendHolding, bool) {
	if h == nil || h.PerPayment <= 0 || price <= 0 || len(h.PaymentMonths) == 0 {
		return models.DividendHolding{}, false
	}
	return models.DividendHolding{
		Symbol:        symbol,
		Shares:        shares,
		PerPayment:    h.PerPayment,
		PaymentMonths: h.PaymentMonths,
		RaiseMonth:    h.RaiseMonth,
		GrowthRate:    h.CAGR3Y,
		BaseYear:      h.LastYear,
		BaseMonth:     h.LastMonth,
	}, true
}
