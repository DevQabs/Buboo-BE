package service

import "testing"

func TestCapitalGainsTax_GainWithinBasicDeductionIsNotTaxed(t *testing.T) {
	got := CalcCapitalGainsTax([]UserGain{
		{UserID: "husband", RealizedPnL: 2_000_000},
	})

	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].EstimatedTax != 0 {
		t.Errorf("gain 2,000,000 is under the 2,500,000 deduction: want tax 0, got %v", got[0].EstimatedTax)
	}
}

func TestCapitalGainsTax_OnlyTheAmountAboveTheDeductionIsTaxed(t *testing.T) {
	got := CalcCapitalGainsTax([]UserGain{
		{UserID: "husband", RealizedPnL: 10_000_000},
	})

	// (10,000,000 - 2,500,000) * 0.22
	const want = 1_650_000
	if got[0].EstimatedTax != want {
		t.Errorf("want tax %v, got %v", want, got[0].EstimatedTax)
	}
}

func TestCapitalGainsTax_ReportsHowTheTaxWasDerived(t *testing.T) {
	got := CalcCapitalGainsTax([]UserGain{
		{UserID: "husband", RealizedPnL: 10_000_000},
	})[0]

	if got.RealizedPnL != 10_000_000 {
		t.Errorf("want realized 10,000,000, got %v", got.RealizedPnL)
	}
	if got.Deduction != 2_500_000 {
		t.Errorf("want deduction 2,500,000, got %v", got.Deduction)
	}
	if got.TaxableGain != 7_500_000 {
		t.Errorf("want taxable 7,500,000, got %v", got.TaxableGain)
	}
}

func TestCapitalGainsTax_LossIsNotTaxedAndConsumesNoDeduction(t *testing.T) {
	got := CalcCapitalGainsTax([]UserGain{
		{UserID: "wife", RealizedPnL: -4_000_000},
	})[0]

	if got.EstimatedTax != 0 {
		t.Errorf("a loss owes no tax: got %v", got.EstimatedTax)
	}
	if got.TaxableGain != 0 {
		t.Errorf("a loss has no taxable gain: got %v", got.TaxableGain)
	}
	if got.Deduction != 0 {
		t.Errorf("a loss consumes no deduction: got %v", got.Deduction)
	}
}

// 이 앱이 고치려는 버그: 부부 합산으로 과세하면 공제를 한 번만 적용해
// 실제로는 내지 않아도 될 세금이 잡힌다. 두 사람 각자 250만원을 공제받는다.
func TestCapitalGainsTax_EachPersonGetsTheirOwnDeduction(t *testing.T) {
	got := CalcCapitalGainsTax([]UserGain{
		{UserID: "husband", RealizedPnL: 2_000_000},
		{UserID: "wife", RealizedPnL: 2_000_000},
	})

	var total float64
	for _, u := range got {
		total += u.EstimatedTax
	}
	// 합산 과세라면 (4,000,000 - 2,500,000) * 0.22 = 330,000 이 잡힌다.
	if total != 0 {
		t.Errorf("each person is under their own 2,500,000 deduction: want 0, got %v", total)
	}
}
