package service

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

// 저축 추가매수와 직접 매수가 동시에 같은 종목을 건드려도 수량이 하나도 빠지지 않아야 한다.
func TestApplyStock_저축추가매수와직접매수가_동시에와도_수량이맞다(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 이 없어 DB 테스트를 건너뜀")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	couple := "c-test-" + uuid.NewString()
	t.Cleanup(func() {
		for _, table := range []string{"stock_transactions", "stock_assets"} {
			_, _ = pool.Exec(context.Background(), `DELETE FROM `+table+` WHERE couple_id = $1`, couple)
		}
	})

	stocks := repository.NewPgStockRepository(pool)
	stxRepo := repository.NewPgStockTransactionRepository(pool)
	a, err := stocks.Create(ctx, &models.StockAsset{CoupleID: couple, UserID: "u1", Symbol: "005930", Exchange: "KRX",
		Name: "삼성전자", Quantity: 10, AveragePrice: 100, AvgKRWPrice: 100, Currency: "KRW"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSavingService(nil, stocks, stxRepo, nil, nil)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for range n {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tx := &models.Transaction{ID: uuid.NewString(), CoupleID: couple, UserID: "u1", Date: time.Now().UTC()}
			errs <- s.applyStock(ctx, tx, &models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: a.ID, AddStockQty: 1, AddStockPrice: 100})
		}()
		go func() {
			defer wg.Done()
			_, err := stocks.Trade(ctx, a.ID, couple, func(x *models.StockAsset) (*models.StockTransaction, error) {
				x.Quantity++
				return nil, nil
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := stocks.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Quantity != 10+2*n {
		t.Fatalf("수량은 %d 이어야 한다: %.0f", 10+2*n, got.Quantity)
	}
	logs, _ := stxRepo.ListByCouple(ctx, couple)
	if len(logs) != n {
		t.Fatalf("저축 매수 기록이 %d건이어야 한다: %d", n, len(logs))
	}
}
