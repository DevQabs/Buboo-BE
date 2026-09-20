// tax_service.go — 해외주식 양도소득세 계산.
//
// 세법 요지: 양도소득세는 인별 과세다. 한 해 동안 실현한 손익을 사람별로
// 통산한 뒤, 각자 기본공제 250만원을 빼고, 남은 금액에 22%(지방소득세 포함)를
// 매긴다. 공제는 부부 합산이 아니라 각자 250만원씩이다.
package service

import "fmt"

const (
	// BasicDeduction은 양도소득 기본공제로, 인별 연 250만원이다.
	BasicDeduction = 2_500_000.0
	// CapitalGainsTaxRate는 양도소득세율 20% + 지방소득세 2%다.
	CapitalGainsTaxRate = 0.22
)

// 원화 취득가의 타당성 검사 범위. USD/KRW 가 이 밖으로 나간 적은 없으므로,
// 역산 환율이 범위를 벗어나면 저장된 값이 손상된 것이다. 실제로 UNH 329주는
// 역산 환율 9.4(avg_krw_price=3,135)로 저장돼 있었고, 그대로 매도하면
// 양도차익이 4배로 잡혀 세금이 2,833만원 과대계상된다.
const (
	minPlausibleUSDKRW = 700
	maxPlausibleUSDKRW = 2500
)

// ValidateKRWCostBasis는 원화 취득가가 달러 취득가와 앞뒤가 맞는지 본다.
// 값이 손상됐으면 매도를 막아, 잘못된 실현손익이 기록되지 않게 한다.
func ValidateKRWCostBasis(avgUSDPrice, avgKRWPrice float64) error {
	if avgKRWPrice <= 0 {
		return fmt.Errorf("원화 취득가가 없습니다 (avg_krw_price=%.4f). 매도 전 취득가를 정정해 주세요", avgKRWPrice)
	}
	if avgUSDPrice <= 0 {
		return fmt.Errorf("달러 취득가가 없습니다 (average_price=%.4f)", avgUSDPrice)
	}
	impliedFX := avgKRWPrice / avgUSDPrice
	if impliedFX < minPlausibleUSDKRW || impliedFX > maxPlausibleUSDKRW {
		return fmt.Errorf(
			"원화 취득가가 비정상입니다: %.2f원 ÷ $%.2f = 환율 %.1f. 매도 전 취득가를 정정해 주세요",
			avgKRWPrice, avgUSDPrice, impliedFX)
	}
	return nil
}

// KRWCostBasis는 보유분의 원화 취득원가를 낸다.
//
// 저장된 원화 평단이 성하면 그걸 쓴다 — 매입 시점 환율이 반영돼 있어
// 환차손익이 손익에 잡힌다. 손상됐으면 달러 평단을 오늘 환율로 환산해
// 근사하고, exact=false 로 알린다. 이 경우 환차손익은 0으로 묻힌다.
func KRWCostBasis(quantity, avgUSDPrice, avgKRWPrice, todayFX float64) (cost float64, exact bool) {
	if err := ValidateKRWCostBasis(avgUSDPrice, avgKRWPrice); err != nil {
		return quantity * avgUSDPrice * todayFX, false
	}
	return quantity * avgKRWPrice, true
}

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

// LiquidationTax는 한 사람이 보유분을 지금 전량 매도할 때 더 낼 양도소득세다.
//
// 기본공제 250만원은 한 해에 한 번뿐이라, 올해 이미 실현한 이익이 공제를
// 얼마나 썼는지에 따라 미실현분의 세금이 달라진다. 그래서 (실현+미실현)
// 세액에서 실현분만의 세액을 뺀다. 실현이 손실이면 통산돼 세금이 준다.
func LiquidationTax(realizedPnL, unrealizedPnL float64) float64 {
	both := CalcCapitalGainsTax([]UserGain{{RealizedPnL: realizedPnL + unrealizedPnL}})[0].EstimatedTax
	realizedOnly := CalcCapitalGainsTax([]UserGain{{RealizedPnL: realizedPnL}})[0].EstimatedTax
	return both - realizedOnly
}
