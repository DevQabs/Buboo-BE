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
	Symbol   string
	AnnualPS float64 // 최근 4회 지급 합계 (TTM), 종목 통화 기준
	CAGR3Y   float64 // 최근 3개년 연 성장률. 이력이 모자라면 0
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

// summarizeDividends는 배당 이벤트에서 TTM 배당과 3개년 성장률을 낸다.
//
// TTM 은 최근 12개월 지급 합계다. 연초에 인상이 반영되므로 연도별 합계보다
// 지금 받는 금액에 가깝다. 성장률은 완결된 연도끼리만 비교한다 — 진행 중인
// 올해를 넣으면 아직 안 받은 분기 때문에 성장률이 음수로 나온다.
func summarizeDividends(symbol string, events []dividendEvent, now time.Time) *DividendHistory {
	out := &DividendHistory{Symbol: symbol}

	ttmFrom := now.AddDate(-1, 0, 0).Unix()
	byYear := make(map[int]float64, 8)
	for _, e := range events {
		if e.Date >= ttmFrom {
			out.AnnualPS += e.Amount
		}
		byYear[time.Unix(e.Date, 0).UTC().Year()] += e.Amount
	}

	last := now.Year() - 1
	base := last - 3
	if byYear[base] > 0 && byYear[last] > 0 {
		out.CAGR3Y = math.Pow(byYear[last]/byYear[base], 1.0/3.0) - 1
	}
	return out
}

// DividendPlanFor는 보유 종목에서 배당 계획을 만든다.
//
// 배당률이 dividendYieldFloor 미만인 종목은 넣지 않는다. 성장률은 그 종목의
// 최근 3개년 실적을 그대로 쓴다 — 감속·하한 같은 전망은 사람이 넣을 값이지
// 과거 데이터에서 나오지 않는다.
func DividendPlanFor(symbol string, shares, price float64, h *DividendHistory) (models.DividendHolding, bool) {
	if h == nil || h.AnnualPS <= 0 || price <= 0 {
		return models.DividendHolding{}, false
	}
	if h.AnnualPS/price < dividendYieldFloor {
		return models.DividendHolding{}, false
	}
	return models.DividendHolding{
		Symbol:      symbol,
		Shares:      shares,
		DPS:         h.AnnualPS,
		GrowthStart: h.CAGR3Y,
		Floor:       h.CAGR3Y, // 감속 없이 3개년 성장률을 유지한다고 본다
		StartsYear:  time.Now().UTC().Year() + 1,
	}, true
}
