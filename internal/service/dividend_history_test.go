package service

import (
	"math"
	"testing"
	"time"
)

func ev(y int, m time.Month, amount float64) dividendEvent {
	return dividendEvent{Date: time.Date(y, m, 15, 0, 0, 0, 0, time.UTC).Unix(), Amount: amount}
}

func TestSummarizeDividends_TakesTheRaiseMonthFromTheHistory(t *testing.T) {
	// UNH 실제 이력: 3·6·9·12월 지급이고 인상은 6월 지급분부터다.
	var events []dividendEvent
	for _, m := range []time.Month{time.March, time.June, time.September, time.December} {
		amount := 2.10
		if m >= time.June {
			amount = 2.21
		}
		events = append(events, ev(2025, m, amount))
	}
	events = append(events, ev(2026, time.March, 2.21), ev(2026, time.June, 2.32), ev(2026, time.September, 2.32))

	got := summarizeDividends("UNH", events, time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC))

	if got.RaiseMonth != int(time.June) {
		t.Errorf("인상 월: want 6, got %d", got.RaiseMonth)
	}
	// 앞으로 받을 금액은 최근 지급액 기준이다. TTM 합계(9.06)는 인상 전
	// 3월치가 섞여 있어 과소평가된다.
	if got.PerPayment != 2.32 {
		t.Errorf("최근 지급액: want 2.32, got %v", got.PerPayment)
	}
	if want := 2.32 * 4; !almostEqual(got.AnnualPS, want) {
		t.Errorf("연 지급률: want %v, got %v", want, got.AnnualPS)
	}
	if len(got.PaymentMonths) != 4 {
		t.Errorf("지급 월 4개여야 한다: %v", got.PaymentMonths)
	}
}

func TestDividendPerShareAt_RaisesOnTheSymbolsOwnMonth(t *testing.T) {
	// UNH: 2026-09 기준 2.32, 인상은 6월. 2027년 3월까지는 그대로고 6월에 오른다.
	h, ok := DividendPlanFor("UNH", 350, 376.90, &DividendHistory{
		PerPayment: 2.32, PaymentMonths: []int{3, 6, 9, 12}, RaiseMonth: 6,
		AnnualPS: 9.28, CAGR3Y: 0.109, LastYear: 2026, LastMonth: 9,
	})
	if !ok {
		t.Fatal("계획을 만들지 못했다")
	}

	if got := DividendPerShareAt(h, 2026, time.October); got != 0 {
		t.Errorf("지급 달이 아니면 0이어야 한다: %v", got)
	}
	if got := DividendPerShareAt(h, 2026, time.December); !almostEqual(got, 2.32) {
		t.Errorf("연말은 아직 인상 전이다: want 2.32, got %v", got)
	}
	if got := DividendPerShareAt(h, 2027, time.March); !almostEqual(got, 2.32) {
		t.Errorf("3월은 아직 인상 전이다 — 연초 인상이 아니다: want 2.32, got %v", got)
	}
	if want, got := 2.32*1.109, DividendPerShareAt(h, 2027, time.June); !almostEqual(got, want) {
		t.Errorf("6월에 올라야 한다: want %v, got %v", want, got)
	}
	if want, got := 2.32*math.Pow(1.109, 2), DividendPerShareAt(h, 2028, time.June); !almostEqual(got, want) {
		t.Errorf("이듬해 6월에 한 번 더: want %v, got %v", want, got)
	}
}

func TestDividendPerShareAt_HandlesADecemberRaise(t *testing.T) {
	// MCD: 인상이 12월 지급분부터다. 기준 시점(9월)이 인상 월보다 앞서므로
	// 그해 12월에 이미 한 번 오른다.
	h, _ := DividendPlanFor("MCD", 230, 248.24, &DividendHistory{
		PerPayment: 1.86, PaymentMonths: []int{3, 6, 9, 12}, RaiseMonth: 12,
		AnnualPS: 7.44, CAGR3Y: 0.082, LastYear: 2026, LastMonth: 9,
	})

	if want, got := 1.86*1.082, DividendPerShareAt(h, 2026, time.December); !almostEqual(got, want) {
		t.Errorf("12월에 올라야 한다: want %v, got %v", want, got)
	}
	if want, got := 1.86*1.082, DividendPerShareAt(h, 2027, time.September); !almostEqual(got, want) {
		t.Errorf("이듬해 9월까지는 그대로다: want %v, got %v", want, got)
	}
}

func TestMeetsDividendYieldFloor_SkipsSymbolsUnderOnePercent(t *testing.T) {
	// NVDA: 연 $0.04, 주가 $222 → 0.02%. 성장률은 35%지만 금액이 없다.
	if MeetsDividendYieldFloor(0.04, 222.27) {
		t.Error("배당률 1% 미만인데 기준을 넘었다")
	}
	// MCD: 연 $7.44, 주가 $248 → 3.0%.
	if !MeetsDividendYieldFloor(7.44, 248.24) {
		t.Error("배당률 3%인데 기준에 못 미쳤다")
	}
}
