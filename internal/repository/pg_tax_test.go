package repository

import (
	"context"
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

func TestAnnualTax_미국주식만_한국날짜기준(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStockTransactionRepository(pool)
	couple := newCouple(t, pool)

	sell := func(sym, cur string, pnl float64, at time.Time) {
		t.Helper()
		if _, err := repo.Create(ctx, &models.StockTransaction{
			CoupleID: couple, UserID: "u1", Symbol: sym, Type: models.StockTxSell,
			Quantity: 1, Price: 1, Currency: cur, RealizedPnL: pnl, ExecutedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	sell("AAPL", "USD", 100, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	sell("005930", "KRW", 50, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))  // 국내 주식: 양도세 대상 아님
	sell("AAPL", "USD", 7, time.Date(2026, 12, 31, 16, 0, 0, 0, time.UTC))  // 한국 2027-01-01 01:00
	sell("AAPL", "USD", 3, time.Date(2025, 12, 31, 15, 30, 0, 0, time.UTC)) // 한국 2026-01-01 00:30

	gains, err := repo.AnnualGainsByUser(ctx, couple, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(gains) != 1 || gains[0].RealizedPnL != 103 || gains[0].SellCount != 2 {
		t.Fatalf("2026년 과세 대상은 미국 주식, 한국 날짜 기준 103원 2건이어야 한다: %+v", gains)
	}

	bySymbol, err := repo.AnnualSummary(ctx, couple, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(bySymbol) != 1 || bySymbol[0].Symbol != "AAPL" || bySymbol[0].RealizedPnL != 103 {
		t.Fatalf("종목별 집계에 국내 종목이나 다른 해 매도가 섞였다: %+v", bySymbol)
	}

	has, err := repo.HasSellInYear(ctx, couple, "AAPL", 2027)
	if err != nil || !has {
		t.Fatalf("한국 2027-01-01 01:00 매도는 2027년 매도다: has=%v err=%v", has, err)
	}
}
