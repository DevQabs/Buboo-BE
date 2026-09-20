// tax_service.go — 해외주식 양도소득세 계산.
//
// 세법 요지: 양도소득세는 인별 과세다. 한 해 동안 실현한 손익을 사람별로
// 통산한 뒤, 각자 기본공제 250만원을 빼고, 남은 금액에 22%(지방소득세 포함)를
// 매긴다. 공제는 부부 합산이 아니라 각자 250만원씩이다.
package service

const (
	// BasicDeduction은 양도소득 기본공제로, 인별 연 250만원이다.
	BasicDeduction = 2_500_000.0
	// CapitalGainsTaxRate는 양도소득세율 20% + 지방소득세 2%다.
	CapitalGainsTaxRate = 0.22
)

// UserGain은 한 사람의 연간 실현손익(원화, 손익 통산 후)이다.
type UserGain struct {
	UserID      string
	RealizedPnL float64
}

// UserTax는 한 사람의 양도소득세 계산 결과다. 화면에서 계산 근거를 보여줄 수
// 있도록 공제액과 과세표준을 함께 담는다.
type UserTax struct {
	UserID       string  `json:"user_id"`
	RealizedPnL  float64 `json:"realized_pnl"`  // KRW, 손익 통산 후
	Deduction    float64 `json:"deduction"`     // 실제 적용된 기본공제
	TaxableGain  float64 `json:"taxable_gain"`  // max(0, 손익 - 공제)
	EstimatedTax float64 `json:"estimated_tax"` // 과세표준 × 22%
}

// CalcCapitalGainsTax는 사람별 실현손익으로 양도소득세를 계산한다.
func CalcCapitalGainsTax(gains []UserGain) []UserTax {
	out := make([]UserTax, 0, len(gains))
	for _, g := range gains {
		// 손실이면 공제를 소진하지 않는다. 양도소득 기본공제는 이월되지 않고
		// 그 해 이익에만 적용되므로, 이익이 없으면 공제액도 0으로 본다.
		deduction := 0.0
		if g.RealizedPnL > 0 {
			deduction = min(g.RealizedPnL, BasicDeduction)
		}
		taxable := g.RealizedPnL - deduction
		if taxable < 0 {
			taxable = 0
		}
		out = append(out, UserTax{
			UserID:       g.UserID,
			RealizedPnL:  g.RealizedPnL,
			Deduction:    deduction,
			TaxableGain:  taxable,
			EstimatedTax: taxable * CapitalGainsTaxRate,
		})
	}
	return out
}
