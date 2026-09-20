// roadmap_service.go — 목표 순자산까지의 궤적 시뮬레이션.
//
// DB에 의존하지 않는 순수 함수다. 산술이 핵심이므로 DB 없이 테스트할 수 있어야
// 한다. 계산은 2층으로 나뉜다.
//
//  1. 기존 보유분 — 종목별 주식수가 고정이고 배당이 나온다. 주당 배당은
//     시작률에서 매년 일정 폭씩 감속해 하한에 수렴한다.
//  2. 신규 풀 — 적립금과 재투자 배당이 쌓인다. 가격상승률만 적용하고 배당은
//     계산하지 않는다. 어떤 종목을 살지 정해지지 않았기 때문이다.
//
// 종목을 팔고 사도 신규 풀로 흡수되므로 포트폴리오가 바뀌어도 로드맵은 유효하다.
package service

import (
	"math"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

// DividendPerShare는 주어진 연도의 주당 배당을 낸다.
//
// StartsYear 전에는 기준 배당 그대로다. 그 뒤로는 시작률에서 해마다 Decay만큼
// 깎인 인상률을 곱해 나가고, 인상률은 Floor 아래로 내려가지 않는다.
func DividendPerShare(h models.DividendHolding, year int) float64 {
	dps := h.DPS
	for y := h.StartsYear; y <= year; y++ {
		rate := h.GrowthStart - h.Decay*float64(y-h.StartsYear)
		if rate < h.Floor {
			rate = h.Floor
		}
		dps *= 1 + rate
	}
	return dps
}

// monthlyContribution은 해당 연도의 월 적립액이다. 스케줄에 없는 해는 0이다.
func monthlyContribution(cs []models.Contribution, year int) int64 {
	for _, c := range cs {
		if c.Year == year {
			return c.MonthlyKRW
		}
	}
	return 0
}

// Project는 가정대로 굴렸을 때의 궤적을 연 단위와 월 단위로 낸다. 두 축을
// 같은 루프에서 뽑아, 표와 차트가 어긋날 일이 없게 한다.
//
// 월 단위로 굴린다. 연 단위는 적립 시점에 따라 오차가 커진다. 한 달은 적립금과
// 세후 배당을 신규 풀에 넣은 뒤 가격상승률을 적용하는 순서로 진행한다.
func Project(a models.RoadmapAssumptions, stockKRW int64, fx float64, start, end time.Time, birthYear int) ([]models.RoadmapYearRow, []models.RoadmapMonthPoint) {
	monthlyGrowth := math.Pow(1+a.PriceGrowth, 1.0/12.0)

	existing := float64(stockKRW) // 기존 보유분. 배당은 신규 풀로 빠진다
	pool := 0.0                   // 신규 풀

	rows := make([]models.RoadmapYearRow, 0, end.Year()-start.Year()+1)
	months := make([]models.RoadmapMonthPoint, 0, 12*(end.Year()-start.Year()+1))
	var row *models.RoadmapYearRow

	for m := nextMonthStart(start); !m.After(end); m = m.AddDate(0, 1, 0) {
		year := m.Year()
		if row == nil || row.Year != year {
			rows = append(rows, models.RoadmapYearRow{
				Year:       year,
				Age:        year - birthYear,
				MonthlyKRW: monthlyContribution(a.Contributions, year),
			})
			row = &rows[len(rows)-1]
		}

		contribution := row.MonthlyKRW
		dividend := dividendAfterTax(a, fx, year, m.Month())

		pool += float64(contribution) + dividend
		pool *= monthlyGrowth
		existing *= monthlyGrowth

		row.AnnualContributionKRW += contribution
		row.DividendAfterTaxKRW += int64(dividend)
		row.ProjectedNetWorthKRW = int64(existing + pool + float64(a.OtherAssetsKRW))
		months = append(months, models.RoadmapMonthPoint{
			Month:                m.Format("2006-01"),
			ProjectedNetWorthKRW: row.ProjectedNetWorthKRW,
		})
	}
	return rows, months
}

// dividendAfterTax는 기존 보유분에서 그 달에 나오는 세후 배당이다.
//
// UNH·MCD 모두 3·6·9·12월 분기 지급이라, 연 배당의 1/4이 그 네 달에만
// 들어온다. 12로 나눠 매달 흘리면 연 합계는 같아도 월별 궤적이 실제와
// 어긋난다 — 배당 달의 계단이 사라진다.
func dividendAfterTax(a models.RoadmapAssumptions, fx float64, year int, month time.Month) float64 {
	switch month {
	case time.March, time.June, time.September, time.December:
	default:
		return 0
	}
	return annualDividendAfterTax(a, fx, year) / 4
}

// annualDividendAfterTax는 그 해 기존 보유분의 세후 배당 총액이다.
func annualDividendAfterTax(a models.RoadmapAssumptions, fx float64, year int) float64 {
	var annualUSD float64
	for _, h := range a.DividendPlan {
		annualUSD += h.Shares * DividendPerShare(h, year)
	}
	return annualUSD * fx * (1 - a.DividendTaxRate)
}

// SolveRequiredGrowth는 목표일에 목표액이 되는 연 가격상승률을 이분탐색으로 찾는다.
// 적립만으로 목표를 넘으면 음수가, 범위 밖이면 경계값이 나온다.
func SolveRequiredGrowth(a models.RoadmapAssumptions, stockKRW int64, fx float64, targetKRW int64, start, by time.Time, birthYear int) float64 {
	lo, hi := -0.5, 0.5
	final := func(growth float64) int64 {
		a.PriceGrowth = growth
		rows, _ := Project(a, stockKRW, fx, start, by, birthYear)
		if len(rows) == 0 {
			return 0
		}
		return rows[len(rows)-1].ProjectedNetWorthKRW
	}
	if final(hi) < targetKRW {
		return hi
	}
	if final(lo) > targetKRW {
		return lo
	}
	for range 60 { // 60회면 배정밀도 한계까지 좁혀진다
		mid := (lo + hi) / 2
		if final(mid) < targetKRW {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// nextMonthStart는 굴리기 시작할 달의 1일이다.
//
// 시작일이 달 중간이면 그 달은 건너뛴다. 이번 달의 적립과 배당은 이미 지금
// 순자산에 반영돼 있어서, 다시 더하면 두 번 계산된다. 9월 20일에 열면
// 2026년은 10~12월 세 달만 굴린다.
func nextMonthStart(t time.Time) time.Time {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	if t.Equal(first) {
		return first
	}
	return first.AddDate(0, 1, 0)
}

// DividendYield는 기존 보유분의 세후 배당수익률이다. 필요 수익률을 가격상승률과
// 배당으로 나눠 보여주기 위한 값이다.
func DividendYield(a models.RoadmapAssumptions, stockKRW int64, fx float64, year int) float64 {
	if stockKRW <= 0 {
		return 0
	}
	return annualDividendAfterTax(a, fx, year) / float64(stockKRW)
}
