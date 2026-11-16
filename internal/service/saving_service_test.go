package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

type memStocks struct {
	repository.StockRepository
	m map[string]*models.StockAsset
}

func (f *memStocks) Trade(_ context.Context, id, coupleID string, apply func(*models.StockAsset) (*models.StockTransaction, error)) (*models.StockAsset, error) {
	a, ok := f.m[id]
	if !ok || a.CoupleID != coupleID {
		return nil, repository.ErrNotFound
	}
	c := *a
	if _, err := apply(&c); err != nil {
		return nil, err
	}
	if c.Quantity <= 0 {
		delete(f.m, id)
	} else {
		f.m[id] = &c
	}
	return &c, nil
}

type memAssets struct {
	repository.OtherAssetRepository
	m map[string]*models.OtherAsset
}

func (f *memAssets) GetByID(_ context.Context, id string) (*models.OtherAsset, error) {
	if a, ok := f.m[id]; ok {
		c := *a
		return &c, nil
	}
	return nil, fmt.Errorf("asset %s not found: %w", id, pgx.ErrNoRows)
}

func (f *memAssets) Update(_ context.Context, a *models.OtherAsset) (*models.OtherAsset, error) {
	f.m[a.ID] = a
	return a, nil
}

type memSTx struct {
	repository.StockTransactionRepository
	rows []models.StockTransaction
}

func (f *memSTx) ListBySymbol(_ context.Context, coupleID, symbol string) ([]models.StockTransaction, error) {
	var out []models.StockTransaction
	for _, r := range f.rows {
		if r.CoupleID == coupleID && r.Symbol == symbol {
			out = append(out, r)
		}
	}
	return out, nil
}

var savingDay = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func linkedSavingTx(amount int64, link models.SavingLink) *models.Transaction {
	return &models.Transaction{ID: "txn-1", CoupleID: "c1", Type: "saving", Amount: amount, Date: savingDay, SavingLink: &link}
}

func TestRevertSaving_예금은_금액만큼뺀다(t *testing.T) {
	assets := &memAssets{m: map[string]*models.OtherAsset{"a1": {ID: "a1", CoupleID: "c1", ValueKRW: 1_500_000}}}
	s := NewSavingService(nil, &memStocks{}, nil, assets, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(500_000, models.SavingLink{Kind: models.SavingKindDeposit, LinkAssetID: "a1"}))
	if err != nil || assets.m["a1"].ValueKRW != 1_000_000 {
		t.Fatalf("예금이 100만원으로 돌아가야 한다: %d, err=%v", assets.m["a1"].ValueKRW, err)
	}
}

func TestRevertSaving_다른그룹자산은_건드리지않는다(t *testing.T) {
	assets := &memAssets{m: map[string]*models.OtherAsset{"a1": {ID: "a1", CoupleID: "other", ValueKRW: 1_500_000}}}
	s := NewSavingService(nil, &memStocks{}, nil, assets, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(500_000, models.SavingLink{Kind: models.SavingKindDeposit, LinkAssetID: "a1"}))
	if err != nil || assets.m["a1"].ValueKRW != 1_500_000 {
		t.Fatalf("다른 그룹 자산이 바뀌었다: %d, err=%v", assets.m["a1"].ValueKRW, err)
	}
}

func TestRevertSaving_추가매수는_수량과평단을되돌린다(t *testing.T) {
	// 10주@100 에 저축으로 10주@200 을 더해 20주@150 이 된 상태
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Quantity: 20, AveragePrice: 150, AvgKRWPrice: 150, Currency: "KRW"}}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(2000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 200}))
	got := stocks.m["s1"]
	if err != nil || got.Quantity != 10 || got.AveragePrice != 100 || got.AvgKRWPrice != 100 {
		t.Fatalf("10주@100 으로 돌아가야 한다: %+v, err=%v", got, err)
	}
}

func TestRevertSaving_USD추가매수는_기록된환율로원화평단도되돌린다(t *testing.T) {
	// 10주@$10(원화 13,000) 에 저축으로 10주@$20 을 환율 1,400 에 더해 20주@$15, 원화 20,500
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Symbol: "NU", Quantity: 20, AveragePrice: 15, AvgKRWPrice: 20_500, Currency: "USD"}}}
	stx := &memSTx{rows: []models.StockTransaction{
		{CoupleID: "c1", Symbol: "NU", StockAssetID: "s1", Type: models.StockTxBuy, Source: models.StockTxSourceTrade, Quantity: 10, Price: 20, ExchangeRateAtTx: 9999, ExecutedAt: savingDay},
		{CoupleID: "c1", Symbol: "NU", StockAssetID: "s1", Type: models.StockTxBuy, Source: models.StockTxSourceSaving, Quantity: 10, Price: 20, ExchangeRateAtTx: 1400, ExecutedAt: savingDay},
	}}
	s := NewSavingService(nil, stocks, stx, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(280_000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 20}))
	got := stocks.m["s1"]
	if err != nil || got.Quantity != 10 || got.AveragePrice != 10 || math.Abs(got.AvgKRWPrice-13_000) > 1e-6 {
		t.Fatalf("10주@$10, 원화 13,000 으로 돌아가야 한다: %+v, err=%v", got, err)
	}
}

func TestRevertSaving_USD매수기록이없으면_원화평단은그대로(t *testing.T) {
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Symbol: "NU", Quantity: 20, AveragePrice: 15, AvgKRWPrice: 20_500, Currency: "USD"}}}
	s := NewSavingService(nil, stocks, &memSTx{}, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(280_000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 20}))
	got := stocks.m["s1"]
	if err != nil || got.Quantity != 10 || got.AveragePrice != 10 || got.AvgKRWPrice != 20_500 {
		t.Fatalf("수량·달러 평단만 되돌려야 한다: %+v, err=%v", got, err)
	}
}

func TestRevertSaving_저축으로만든종목은_지운다(t *testing.T) {
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Quantity: 3, AveragePrice: 50, Currency: "USD"}}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(200_000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", NewStockQty: 3, NewStockPrice: 50}))
	if _, ok := stocks.m["s1"]; ok || err != nil {
		t.Fatalf("종목이 지워져야 한다: err=%v", err)
	}
}

func TestRevertSaving_자산을이미지웠으면_그냥넘어간다(t *testing.T) {
	s := NewSavingService(nil, &memStocks{m: map[string]*models.StockAsset{}}, nil, &memAssets{m: map[string]*models.OtherAsset{}}, nil)
	ctx := context.Background()
	if err := s.RevertSaving(ctx, linkedSavingTx(1, models.SavingLink{Kind: models.SavingKindDeposit, LinkAssetID: "gone"})); err != nil {
		t.Fatalf("지운 예금: %v", err)
	}
	if err := s.RevertSaving(ctx, linkedSavingTx(1, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "gone", AddStockQty: 1, AddStockPrice: 1})); err != nil {
		t.Fatalf("지운 종목: %v", err)
	}
}

func TestResizeSaving_예금은_차액만큼옮긴다(t *testing.T) {
	assets := &memAssets{m: map[string]*models.OtherAsset{"a1": {ID: "a1", CoupleID: "c1", ValueKRW: 1_000_000}}}
	s := NewSavingService(nil, &memStocks{}, nil, assets, nil)

	err := s.ResizeSaving(context.Background(), linkedSavingTx(300_000, models.SavingLink{Kind: models.SavingKindGeneral, LinkAssetID: "a1"}), 500_000)
	if err != nil || assets.m["a1"].ValueKRW != 1_200_000 {
		t.Fatalf("30만→50만 수정이면 자산이 20만 늘어야 한다: %d, err=%v", assets.m["a1"].ValueKRW, err)
	}
}
