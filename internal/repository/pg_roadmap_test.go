package repository

import (
	"context"
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

func TestMonthlyContributions_저축매수는_한번만_한국월기준(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	couple := newCouple(t, pool)
	stx := NewPgStockTransactionRepository(pool)
	txr := NewPgTransactionRepository(pool)
	oct := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	buy := func(qty float64, source string, at time.Time) {
		_, err := stx.Create(ctx, &models.StockTransaction{
			CoupleID: couple, UserID: "u1", Symbol: "005930", Type: models.StockTxBuy,
			Quantity: qty, Price: 100_000, Currency: "KRW", Source: source, ExecutedAt: at,
		})
		must(err)
	}
	// 저축 100만원으로 산 주식: 저축 거래와 source=saving 매수 기록이 함께 남는다
	_, err := txr.Create(ctx, &models.Transaction{
		CoupleID: couple, UserID: "u1", Type: "saving", Amount: 1_000_000,
		Currency: "KRW", Category: "저축/투자", Date: oct,
	})
	must(err)
	buy(10, models.StockTxSourceSaving, oct)
	// 매수 화면에서 직접 산 50만원 (source 비우면 trade)
	buy(5, "", oct)
	// 한국 시각 11월 1일 00:30 매수 10만원 → 11월 적립
	buy(1, "", time.Date(2026, 10, 31, 15, 30, 0, 0, time.UTC))

	got, err := NewPgRoadmapRepository(pool).MonthlyContributions(ctx, couple, time.Date(2026, 9, 1, 0, 0, 0, 0, models.KST))
	must(err)
	if got["2026-10"] != 1_500_000 {
		t.Errorf("10월 적립은 저축 100만 + 직접 매수 50만 = 150만이어야 한다: %d", got["2026-10"])
	}
	if got["2026-11"] != 100_000 {
		t.Errorf("한국 11월 1일 매수는 11월 적립이어야 한다: %d", got["2026-11"])
	}
}
