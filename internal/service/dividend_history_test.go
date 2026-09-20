package service

import (
	"math"
	"testing"
	"time"
)

func ev(y int, m time.Month, amount float64) dividendEvent {
	return dividendEvent{Date: time.Date(y, m, 15, 0, 0, 0, 0, time.UTC).Unix(), Amount: amount}
}

func TestSummarizeDividends_UsesTheTrailingYearAndCompletedYearsOnly(t *testing.T) {
	// MCD 실제 이력에 가깝게: 2022 $5.66 → 2025 $7.17, 2026 은 3분기까지만.
	var events []dividendEvent
	for _, y := range []struct {
		year int
		q    float64
	}{{2022, 1.415}, {2023, 1.5575}, {2024, 1.695}, {2025, 1.7925}} {
		for _, m := range []time.Month{time.March, time.June, time.September, time.December} {
			events = append(events, ev(y.year, m, y.q))
		}
	}
	for _, m := range []time.Month{time.March, time.June, time.September} {
		events = append(events, ev(2026, m, 1.86))
	}

	got := summarizeDividends("MCD", events, time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC))

	// TTM: 2025-12 + 2026-03·06·09 = 1.7925 + 1.86×3
	if want := 1.7925 + 1.86*3; !almostEqual(got.AnnualPS, want) {
		t.Errorf("TTM: want %v, got %v", want, got.AnnualPS)
	}
	// 성장률은 완결된 2022→2025 로만 낸다. 진행 중인 2026을 넣으면 아직 안 받은
	// 분기 때문에 음수가 된다.
	if want := math.Pow(1.7925*4/(1.415*4), 1.0/3.0) - 1; !almostEqual(got.CAGR3Y, want) {
		t.Errorf("CAGR: want %.4f, got %.4f", want, got.CAGR3Y)
	}
	if got.CAGR3Y < 0.07 || got.CAGR3Y > 0.09 {
		t.Errorf("MCD 3개년 성장률은 8%% 근처여야 한다: %.4f", got.CAGR3Y)
	}
}

func TestDividendPlanFor_SkipsSymbolsUnderTheYieldFloor(t *testing.T) {
	// NVDA: 연 $0.04, 주가 $222 → 배당률 0.02%. 성장률은 35%지만 금액이 없다.
	if _, ok := DividendPlanFor("NVDA", 6, 222.27, &DividendHistory{AnnualPS: 0.04, CAGR3Y: 0.357}); ok {
		t.Error("배당률 1%% 미만 종목이 계획에 들어갔다")
	}
	// MCD: 연 $7.44, 주가 $248 → 2.99%.
	d, ok := DividendPlanFor("MCD", 230, 248.24, &DividendHistory{AnnualPS: 7.44, CAGR3Y: 0.082})
	if !ok {
		t.Fatal("배당률 3%% 종목이 빠졌다")
	}
	if d.Shares != 230 || d.DPS != 7.44 || d.GrowthStart != 0.082 || d.Floor != 0.082 {
		t.Errorf("보유 주식수와 실적 성장률이 그대로 들어가야 한다: %+v", d)
	}
}
