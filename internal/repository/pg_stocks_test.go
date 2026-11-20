package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/yourname/couple-app/internal/models"
)

func newStock(t *testing.T, repo *PgStockRepository, couple string, qty float64) *models.StockAsset {
	t.Helper()
	a, err := repo.Create(context.Background(), &models.StockAsset{
		CoupleID: couple, UserID: "u1", Symbol: "005930", Exchange: "KRX",
		Name: "삼성전자", Quantity: qty, AveragePrice: 100, AvgKRWPrice: 100, Currency: "KRW",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func buyLog(couple string, s *models.StockAsset) *models.StockTransaction {
	return &models.StockTransaction{CoupleID: couple, UserID: "u1", StockAssetID: s.ID, Symbol: s.Symbol,
		Type: models.StockTxBuy, Quantity: 1, Price: 100, Currency: "KRW"}
}

func TestTrade_동시매수가_모두반영된다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStockRepository(pool)
	couple := newCouple(t, pool)
	a := newStock(t, repo, couple, 10)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Trade(ctx, a.ID, couple, func(s *models.StockAsset) (*models.StockTransaction, error) {
				s.Quantity++
				return buyLog(couple, s), nil
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
	got, err := repo.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Quantity != 10+n {
		t.Fatalf("동시 매수 %d건이 모두 반영돼야 한다: 수량 %.0f", n, got.Quantity)
	}
	logs, _ := NewPgStockTransactionRepository(pool).ListByCouple(ctx, couple)
	if len(logs) != n {
		t.Fatalf("매수 기록이 %d건이어야 한다: %d", n, len(logs))
	}
}

func TestTrade_전량매도두건은_하나만된다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStockRepository(pool)
	couple := newCouple(t, pool)
	a := newStock(t, repo, couple, 5)

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := repo.Trade(ctx, a.ID, couple, func(s *models.StockAsset) (*models.StockTransaction, error) {
				if s.Quantity < 5 {
					return nil, errors.New("수량 부족")
				}
				s.Quantity -= 5
				return buyLog(couple, s), nil
			})
			results <- err
		}()
	}
	ok, gone := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			ok++
		case errors.Is(err, ErrNotFound):
			gone++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || gone != 1 {
		t.Fatalf("한 건만 매도되고 다른 건은 종목 없음이어야 한다: 성공 %d, 없음 %d", ok, gone)
	}
}

func TestTrade_실패하면_아무것도안바뀐다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStockRepository(pool)
	couple := newCouple(t, pool)
	a := newStock(t, repo, couple, 10)

	// apply 가 거부한 경우
	_, err := repo.Trade(ctx, a.ID, couple, func(s *models.StockAsset) (*models.StockTransaction, error) {
		s.Quantity = 999
		return nil, errors.New("거부")
	})
	if err == nil {
		t.Fatal("apply 오류가 그대로 나와야 한다")
	}
	// 기록 저장이 실패한 경우 (PG 는 잘못된 UTF-8 을 거부한다)
	_, err = repo.Trade(ctx, a.ID, couple, func(s *models.StockAsset) (*models.StockTransaction, error) {
		s.Quantity = 999
		l := buyLog(couple, s)
		l.Memo = "\xff"
		return l, nil
	})
	if err == nil {
		t.Fatal("기록 저장 실패가 오류로 나와야 한다")
	}
	got, _ := repo.GetByID(ctx, a.ID)
	logs, _ := NewPgStockTransactionRepository(pool).ListByCouple(ctx, couple)
	if got.Quantity != 10 || len(logs) != 0 {
		t.Fatalf("잔고·기록이 그대로여야 한다: 수량 %.0f, 기록 %d", got.Quantity, len(logs))
	}
}

func TestUpdate_소유자를저장한다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStockRepository(pool)
	couple := newCouple(t, pool)
	a := newStock(t, repo, couple, 1)

	a.UserID = "u2"
	if _, err := repo.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetByID(ctx, a.ID)
	if got.UserID != "u2" {
		t.Fatalf("수정한 소유자가 저장돼야 한다: %s", got.UserID)
	}
}

func TestTrade_float오차로남은수량은_0으로보고종목을지운다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStockRepository(pool)
	couple := newCouple(t, pool)
	a := newStock(t, repo, couple, 0.1)

	if _, err := repo.Trade(ctx, a.ID, couple, func(s *models.StockAsset) (*models.StockTransaction, error) {
		s.Quantity += 0.2
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Trade(ctx, a.ID, couple, func(s *models.StockAsset) (*models.StockTransaction, error) {
		s.Quantity -= 0.3 // 0.1+0.2-0.3 = 5.55e-17
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Quantity != 0 {
		t.Fatalf("남은 수량은 0 이어야 한다: %v", got.Quantity)
	}
	if _, err := repo.GetByID(ctx, a.ID); err == nil {
		t.Fatal("종목이 지워져야 한다")
	}
}
