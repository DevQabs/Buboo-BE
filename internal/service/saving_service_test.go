package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

type savingStocks struct {
	repository.StockRepository
	byID    map[string]models.StockAsset
	created *models.StockAsset
	updated *models.StockAsset
}

func (f *savingStocks) GetByID(_ context.Context, id string) (*models.StockAsset, error) {
	a, ok := f.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &a, nil
}

func (f *savingStocks) Create(_ context.Context, a *models.StockAsset) (*models.StockAsset, error) {
	a.ID = "stock-new"
	f.created = a
	return a, nil
}

func (f *savingStocks) Update(_ context.Context, a *models.StockAsset) (*models.StockAsset, error) {
	f.updated = a
	return a, nil
}

type savingSTx struct {
	repository.StockTransactionRepository
	logged []models.StockTransaction
}

func (f *savingSTx) Create(_ context.Context, t *models.StockTransaction) (*models.StockTransaction, error) {
	f.logged = append(f.logged, *t)
	return t, nil
}

func savingTx() *models.Transaction {
	return &models.Transaction{ID: "tx-1", CoupleID: "c1", UserID: "u1", Date: time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)}
}

func TestApplyStock_NewUSDStockGetsAKRWBasis(t *testing.T) {
	st, sx := &savingStocks{}, &savingSTx{}
	s := &SavingService{stockRepo: st, stxRepo: sx, usdKRW: fixedFX(1400, nil)}
	link := &models.SavingLink{Kind: models.SavingKindStock, NewStockSymbol: "nu", NewStockExchange: "nyse",
		NewStockName: "NU", NewStockQty: 10, NewStockPrice: 14, NewStockCurrency: "USD"}

	if err := s.applyStock(context.Background(), savingTx(), link); err != nil {
		t.Fatalf("저축 신규 매수 실패: %v", err)
	}
	if st.created.AvgKRWPrice != 19_600 {
		t.Errorf("신규 원화 평단: want 19600, got %v", st.created.AvgKRWPrice)
	}
	if sx.logged[0].ExchangeRateAtTx != 1400 {
		t.Errorf("매매 로그 환율: want 1400, got %v", sx.logged[0].ExchangeRateAtTx)
	}
}

func TestApplyStock_AddToExistingRecoversAZeroBasis(t *testing.T) {
	st := &savingStocks{byID: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", CoupleID: "c1", Currency: "USD", Quantity: 203, AveragePrice: 13.9465, AvgKRWPrice: 0},
	}}
	sx := &savingSTx{}
	s := &SavingService{stockRepo: st, stxRepo: sx, usdKRW: fixedFX(1418.12, nil)}
	link := &models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "stock-1", AddStockQty: 108, AddStockPrice: 13.94}

	if err := s.applyStock(context.Background(), savingTx(), link); err != nil {
		t.Fatalf("저축 추가매수 실패: %v", err)
	}
	if math.Abs(st.updated.AvgKRWPrice-19_774.61) > 0.01 {
		t.Errorf("추가매수 원화 평단: want 19774.61, got %v", st.updated.AvgKRWPrice)
	}
	if sx.logged[0].ExchangeRateAtTx != 1418.12 {
		t.Errorf("매매 로그 환율: want 1418.12, got %v", sx.logged[0].ExchangeRateAtTx)
	}
}

func TestApplyStock_FailsWithoutARateAndWritesNothing(t *testing.T) {
	st := &savingStocks{byID: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", CoupleID: "c1", Currency: "USD", Quantity: 10, AveragePrice: 14, AvgKRWPrice: 19_600},
	}}
	s := &SavingService{stockRepo: st, stxRepo: &savingSTx{}, usdKRW: fixedFX(0, errors.New("timeout"))}
	link := &models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "stock-1", AddStockQty: 1, AddStockPrice: 14}

	err := s.applyStock(context.Background(), savingTx(), link)
	if !errors.Is(err, ErrBuyFX) {
		t.Errorf("환율 없음은 ErrBuyFX 여야 한다: got %v", err)
	}
	if st.updated != nil {
		t.Errorf("환율 없이 종목을 갱신했다")
	}
}

func TestApplyStock_KRWStockNeedsNoRate(t *testing.T) {
	st, sx := &savingStocks{}, &savingSTx{}
	s := &SavingService{stockRepo: st, stxRepo: sx, usdKRW: fixedFX(0, errors.New("호출되면 안 된다"))}
	link := &models.SavingLink{Kind: models.SavingKindStock, NewStockSymbol: "005930", NewStockExchange: "KRX",
		NewStockName: "삼성전자", NewStockQty: 10, NewStockPrice: 70_000, NewStockCurrency: "KRW"}

	if err := s.applyStock(context.Background(), savingTx(), link); err != nil {
		t.Fatalf("KRW 저축 매수 실패: %v", err)
	}
	if st.created.AvgKRWPrice != 70_000 || sx.logged[0].ExchangeRateAtTx != 0 {
		t.Errorf("KRW: want (70000, 0), got (%v, %v)", st.created.AvgKRWPrice, sx.logged[0].ExchangeRateAtTx)
	}
}
