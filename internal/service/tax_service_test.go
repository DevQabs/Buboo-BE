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

// 과세는 종목 단위가 아니라 사람 단위다. 한 사람이 A종목에서 1,000만원을
// 벌고 B종목에서 400만원을 잃었다면 과세 대상은 순액 600만원이며, 거기서
// 250만원을 공제한다. 종목별로 따로 22%를 매기면 손실이 상계되지 않아
// 세금이 부풀려진다.
func TestCapitalGainsTax_TaxesTheNetAcrossSymbolsNotEachSymbol(t *testing.T) {
	const netAcrossSymbols = 10_000_000 - 4_000_000

	got := CalcCapitalGainsTax([]UserGain{
		{UserID: "husband", RealizedPnL: netAcrossSymbols},
	})[0]

	// 순액 과세: (6,000,000 - 2,500,000) * 0.22 = 770,000
	if got.EstimatedTax != 770_000 {
		t.Errorf("want 770,000 on the net gain, got %v", got.EstimatedTax)
	}
	// 종목별 과세라면 이익 종목에만 (10,000,000 - 2,500,000) * 0.22 = 1,650,000 이 잡힌다.
	if got.EstimatedTax == 1_650_000 {
		t.Error("loss on another symbol was not offset against the gain")
	}
}

func TestValidateKRWCostBasis_AcceptsAPlausibleBasis(t *testing.T) {
	// MCD: $273.24 를 환율 1,180 에 샀다.
	if err := ValidateKRWCostBasis(273.24, 322_381.74); err != nil {
		t.Errorf("a basis implying FX 1,180 is plausible: %v", err)
	}
}

func TestValidateKRWCostBasis_RejectsMissingBasis(t *testing.T) {
	// 실제 데이터: NVDA 6주의 avg_krw_price 가 0이다. 이 상태로 매도하면
	// USD 손익이 KRW 컬럼에 기록되어 양도소득세가 1,386배 어긋난다.
	if err := ValidateKRWCostBasis(182.75, 0); err == nil {
		t.Error("a zero KRW basis must be rejected, not silently treated as USD")
	}
}

func TestValidateKRWCostBasis_RejectsImpossibleExchangeRate(t *testing.T) {
	// 실제 데이터: UNH 329주의 avg_krw_price 가 3,135.41 로, 역산 환율이 9.4다.
	// 이대로 매도하면 양도차익이 4,219만원 대신 17,099만원으로 잡힌다.
	if err := ValidateKRWCostBasis(334.41, 3_135.41); err == nil {
		t.Error("a basis implying FX 9.4 must be rejected")
	}
}
