package service

import (
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestProject_WithNoGrowthAndNoDividendsSumsTheContributions(t *testing.T) {
	a := models.RoadmapAssumptions{
		OtherAssetsKRW: 95_000_000,
		Contributions:  []models.Contribution{{Year: 2026, MonthlyKRW: 2_500_000}},
	}

	rows := Project(a, 320_000_000, 1386.01, date(2026, time.October, 1), date(2026, time.December, 31), 1992)

	if len(rows) != 1 {
		t.Fatalf("한 해만 굴렸는데 %d행이 나왔다", len(rows))
	}
	if want := int64(7_500_000); rows[0].AnnualContributionKRW != want {
		t.Errorf("3개월 적립: want %v, got %v", want, rows[0].AnnualContributionKRW)
	}
	// 성장 0, 배당 0 이면 순자산은 시작 자산 + 적립 원금이다.
	if want := int64(320_000_000 + 95_000_000 + 7_500_000); rows[0].ProjectedNetWorthKRW != want {
		t.Errorf("want %v, got %v", want, rows[0].ProjectedNetWorthKRW)
	}
	if rows[0].Age != 34 {
		t.Errorf("1992년생의 2026년 나이: want 34, got %d", rows[0].Age)
	}
}

func TestProject_CompoundsMonthlyNotAnnually(t *testing.T) {
	a := models.RoadmapAssumptions{PriceGrowth: 0.12}

	rows := Project(a, 100_000_000, 1386.01, date(2026, time.January, 1), date(2026, time.December, 31), 1992)

	// 월 복리 12회는 연 1회와 같은 결과여야 한다: 1억 × 1.12 = 1.12억
	got := rows[0].ProjectedNetWorthKRW
	if diff := got - 112_000_000; diff > 100 || diff < -100 {
		t.Errorf("want 약 112,000,000, got %v", got)
	}
}

func TestDividendPerShare_DecaysToTheFloorAndStaysThere(t *testing.T) {
	// UNH: 2026년은 발표된 $9.28 고정, 2027년부터 10.5%로 시작해 매년 0.3%p씩
	// 감속하고 6.5% 아래로는 내려가지 않는다.
	h := models.DividendHolding{DPS: 9.28, GrowthStart: 0.105, Decay: 0.003, Floor: 0.065, StartsYear: 2027}

	if got := DividendPerShare(h, 2026); got != 9.28 {
		t.Errorf("인상 시작 전에는 기준 배당 그대로여야 한다: got %v", got)
	}
	if want, got := 9.28*1.105, DividendPerShare(h, 2027); !almostEqual(got, want) {
		t.Errorf("2027: want %v, got %v", want, got)
	}
	if want, got := 9.28*1.105*1.102, DividendPerShare(h, 2028); !almostEqual(got, want) {
		t.Errorf("2028 인상률은 10.2%%여야 한다: want %v, got %v", want, got)
	}
	// 10.5%에서 0.3%p씩 깎으면 14년 뒤 6.3%가 되지만 하한 6.5%에서 멈춘다.
	far := DividendPerShare(h, 2050)
	prev := DividendPerShare(h, 2049)
	if !almostEqual(far/prev, 1.065) {
		t.Errorf("하한 6.5%%에 수렴해야 한다: got %v", far/prev)
	}
}

func TestProject_ReinvestsDividendsIntoTheNewPool(t *testing.T) {
	// 성장 0, 적립 0. 배당만 들어오므로 순자산 증가분이 곧 세후 배당이다.
	a := models.RoadmapAssumptions{
		DividendTaxRate: 0.154,
		DividendPlan:    []models.DividendHolding{{Shares: 100, DPS: 10, StartsYear: 2027}},
	}

	rows := Project(a, 100_000_000, 1000, date(2026, time.January, 1), date(2026, time.December, 31), 1992)

	want := int64(100 * 10 * 1000 * (1 - 0.154)) // 846,000
	if got := rows[0].DividendAfterTaxKRW; got != want {
		t.Errorf("세후 배당: want %v, got %v", want, got)
	}
	if got := rows[0].ProjectedNetWorthKRW; got != 100_000_000+want {
		t.Errorf("배당이 재투자되지 않았다: got %v", got)
	}
}

func TestSolveRequiredGrowth_FindsTheRateThatHitsTheTarget(t *testing.T) {
	a := models.RoadmapAssumptions{
		DividendTaxRate: 0.154,
		OtherAssetsKRW:  95_000_000,
		Contributions: []models.Contribution{
			{Year: 2026, MonthlyKRW: 2_500_000}, {Year: 2027, MonthlyKRW: 4_000_000},
			{Year: 2028, MonthlyKRW: 5_000_000}, {Year: 2029, MonthlyKRW: 5_150_000},
			{Year: 2030, MonthlyKRW: 5_300_000}, {Year: 2031, MonthlyKRW: 5_460_000},
			{Year: 2032, MonthlyKRW: 5_630_000},
		},
		DividendPlan: []models.DividendHolding{
			{Symbol: "UNH", Shares: 352, DPS: 9.28, GrowthStart: 0.105, Decay: 0.003, Floor: 0.065, StartsYear: 2027},
			{Symbol: "MCD", Shares: 250, DPS: 7.08, GrowthStart: 0.075, Decay: 0.0015, Floor: 0.065, StartsYear: 2027},
		},
	}
	start, by := date(2026, time.October, 1), date(2032, time.December, 31)
	const target = 1_000_000_000

	growth := SolveRequiredGrowth(a, 320_000_000, 1386.01, target, start, by, 1992)

	// 역검증: 찾은 상승률로 굴리면 목표액에 닿아야 한다.
	a.PriceGrowth = growth
	rows := Project(a, 320_000_000, 1386.01, start, by, 1992)
	final := rows[len(rows)-1].ProjectedNetWorthKRW
	if diff := final - target; diff > 1_000_000 || diff < -1_000_000 {
		t.Errorf("상승률 %.4f로 굴린 결과가 목표에서 벗어났다: %v", growth, final)
	}
	if rows[len(rows)-1].Age != 40 {
		t.Errorf("2032년 나이: want 40, got %d", rows[len(rows)-1].Age)
	}
}

func almostEqual(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}
