package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
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
	traded  *models.StockTransaction // Trade 가 같은 트랜잭션에 넣은 매수 기록
}

func (f *savingStocks) Trade(_ context.Context, id, coupleID string, apply func(*models.StockAsset) (*models.StockTransaction, error)) (*models.StockAsset, error) {
	a, ok := f.byID[id]
	if !ok || a.CoupleID != coupleID {
		return nil, repository.ErrNotFound
	}
	log, err := apply(&a)
	if err != nil {
		return nil, err
	}
	f.updated, f.traded = &a, log
	return &a, nil
}

func (f *savingStocks) GetByID(_ context.Context, id string) (*models.StockAsset, error) {
	a, ok := f.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &a, nil
}

func (f *savingStocks) Create(_ context.Context, a *models.StockAsset, buy *models.StockTransaction) (*models.StockAsset, error) {
	a.ID = "stock-new"
	f.created, f.traded = a, buy
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
	s := &SavingService{stockRepo: st, stxRepo: sx, rates: &fakeRates{live: 9999, on: 1400}}
	link := &models.SavingLink{Kind: models.SavingKindStock, NewStockSymbol: "nu", NewStockExchange: "nyse",
		NewStockName: "NU", NewStockQty: 10, NewStockPrice: 14, NewStockCurrency: "USD"}

	if err := s.applyStock(context.Background(), savingTx(), link); err != nil {
		t.Fatalf("저축 신규 매수 실패: %v", err)
	}
	if st.created.AvgKRWPrice != 19_600 {
		t.Errorf("신규 원화 평단: want 19600, got %v", st.created.AvgKRWPrice)
	}
	if st.traded.ExchangeRateAtTx != 1400 || st.traded.Source != models.StockTxSourceSaving {
		t.Errorf("매매 로그: want (1400, saving), got (%v, %v)", st.traded.ExchangeRateAtTx, st.traded.Source)
	}
}

func TestApplyStock_AddToExistingRecoversAZeroBasis(t *testing.T) {
	st := &savingStocks{byID: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", CoupleID: "c1", Currency: "USD", Quantity: 203, AveragePrice: 13.9465, AvgKRWPrice: 0},
	}}
	s := &SavingService{stockRepo: st, stxRepo: &savingSTx{}, rates: &fakeRates{live: 9999, on: 1418.12}}
	link := &models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "stock-1", AddStockQty: 108, AddStockPrice: 13.94}

	if err := s.applyStock(context.Background(), savingTx(), link); err != nil {
		t.Fatalf("저축 추가매수 실패: %v", err)
	}
	if math.Abs(st.updated.AvgKRWPrice-19_774.61) > 0.01 {
		t.Errorf("추가매수 원화 평단: want 19774.61, got %v", st.updated.AvgKRWPrice)
	}
	if st.traded.ExchangeRateAtTx != 1418.12 || st.traded.Source != models.StockTxSourceSaving {
		t.Errorf("매매 로그: want (1418.12, saving), got (%v, %v)", st.traded.ExchangeRateAtTx, st.traded.Source)
	}
}

func TestApplyStock_FailsWithoutARateAndWritesNothing(t *testing.T) {
	st := &savingStocks{byID: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", CoupleID: "c1", Currency: "USD", Quantity: 10, AveragePrice: 14, AvgKRWPrice: 19_600},
	}}
	s := &SavingService{stockRepo: st, stxRepo: &savingSTx{}, rates: &fakeRates{err: errors.New("timeout")}}
	link := &models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "stock-1", AddStockQty: 1, AddStockPrice: 14}

	err := s.applyStock(context.Background(), savingTx(), link)
	if !errors.Is(err, ErrTradeFX) {
		t.Errorf("환율 없음은 ErrTradeFX 여야 한다: got %v", err)
	}
	if st.updated != nil {
		t.Errorf("환율 없이 종목을 갱신했다")
	}
}

func TestApplyStock_KRWStockNeedsNoRate(t *testing.T) {
	st, sx := &savingStocks{}, &savingSTx{}
	s := &SavingService{stockRepo: st, stxRepo: sx, rates: &fakeRates{err: errors.New("호출되면 안 된다")}}
	link := &models.SavingLink{Kind: models.SavingKindStock, NewStockSymbol: "005930", NewStockExchange: "KRX",
		NewStockName: "삼성전자", NewStockQty: 10, NewStockPrice: 70_000, NewStockCurrency: "KRW"}

	if err := s.applyStock(context.Background(), savingTx(), link); err != nil {
		t.Fatalf("KRW 저축 매수 실패: %v", err)
	}
	if st.created.AvgKRWPrice != 70_000 || st.traded.ExchangeRateAtTx != 0 {
		t.Errorf("KRW: want (70000, 0), got (%v, %v)", st.created.AvgKRWPrice, st.traded.ExchangeRateAtTx)
	}
}

type memStocks struct {
	repository.StockRepository
	m         map[string]*models.StockAsset
	txs       []models.StockTransaction // Rewrite 가 넘기고 지우는 매매 기록
	deletedTx string                    // Rewrite 가 같은 트랜잭션에서 지운 가계부 거래
}

// Rewrite 는 PG 구현처럼 오류면 아무것도 바꾸지 않고, 성공하면 종목과 기록 변경을 함께 반영한다.
func (f *memStocks) Rewrite(_ context.Context, id, coupleID string, fn func(*models.StockAsset, []models.StockTransaction) (repository.StockTxChanges, error)) (*models.StockAsset, error) {
	var c *models.StockAsset
	if a, ok := f.m[id]; ok && a.CoupleID == coupleID {
		cp := *a
		c = &cp
	}
	var txs []models.StockTransaction
	for _, t := range f.txs {
		if t.StockAssetID == id && t.CoupleID == coupleID {
			txs = append(txs, t)
		}
	}
	if c == nil && len(txs) == 0 {
		return nil, repository.ErrNotFound
	}
	ch, err := fn(c, txs)
	if err != nil {
		return nil, err
	}
	f.deletedTx = ch.DeleteTransaction
	for _, d := range ch.Delete {
		f.txs = slices.DeleteFunc(f.txs, func(t models.StockTransaction) bool { return t.ID == d })
	}
	if c != nil {
		if c.Quantity < models.QtyEpsilon {
			delete(f.m, id)
		} else {
			f.m[id] = c
		}
	}
	return c, nil
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

func noDel(context.Context) error { return nil }

func linkedSavingTx(amount int64, link models.SavingLink) *models.Transaction {
	return &models.Transaction{ID: "txn-1", CoupleID: "c1", Type: "saving", Amount: amount, Date: savingDay, SavingLink: &link}
}

func TestRevertSaving_예금은_금액만큼뺀다(t *testing.T) {
	assets := &memAssets{m: map[string]*models.OtherAsset{"a1": {ID: "a1", CoupleID: "c1", ValueKRW: 1_500_000}}}
	s := NewSavingService(nil, &memStocks{}, nil, assets, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(500_000, models.SavingLink{Kind: models.SavingKindDeposit, LinkAssetID: "a1"}), noDel)
	if err != nil || assets.m["a1"].ValueKRW != 1_000_000 {
		t.Fatalf("예금이 100만원으로 돌아가야 한다: %d, err=%v", assets.m["a1"].ValueKRW, err)
	}
}

func TestRevertSaving_다른그룹자산은_건드리지않는다(t *testing.T) {
	assets := &memAssets{m: map[string]*models.OtherAsset{"a1": {ID: "a1", CoupleID: "other", ValueKRW: 1_500_000}}}
	s := NewSavingService(nil, &memStocks{}, nil, assets, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(500_000, models.SavingLink{Kind: models.SavingKindDeposit, LinkAssetID: "a1"}), noDel)
	if err != nil || assets.m["a1"].ValueKRW != 1_500_000 {
		t.Fatalf("다른 그룹 자산이 바뀌었다: %d, err=%v", assets.m["a1"].ValueKRW, err)
	}
}

func TestRevertSaving_추가매수는_수량과평단을되돌린다(t *testing.T) {
	// 10주@100 에 저축으로 10주@200 을 더해 20주@150 이 된 상태
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Quantity: 20, AveragePrice: 150, AvgKRWPrice: 150, Currency: "KRW"}}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(2000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 200}), noDel)
	got := stocks.m["s1"]
	if err != nil || got.Quantity != 10 || got.AveragePrice != 100 || got.AvgKRWPrice != 100 {
		t.Fatalf("10주@100 으로 돌아가야 한다: %+v, err=%v", got, err)
	}
}

func TestRevertSaving_USD추가매수는_기록된환율로원화평단도되돌리고_저축매수기록을지운다(t *testing.T) {
	// 10주@$10(원화 13,000) 에 저축으로 10주@$20 을 환율 1,400 에 더해 20주@$15, 원화 20,500
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Symbol: "NU", Quantity: 20, AveragePrice: 15, AvgKRWPrice: 20_500, Currency: "USD"}},
		txs: []models.StockTransaction{
			{ID: "t1", CoupleID: "c1", Symbol: "NU", StockAssetID: "s1", Type: models.StockTxBuy, Source: models.StockTxSourceTrade, Quantity: 10, Price: 20, ExchangeRateAtTx: 9999, ExecutedAt: savingDay},
			{ID: "t2", CoupleID: "c1", Symbol: "NU", StockAssetID: "s1", Type: models.StockTxBuy, Source: models.StockTxSourceSaving, Quantity: 10, Price: 20, ExchangeRateAtTx: 1400, ExecutedAt: savingDay},
		}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(280_000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 20}), noDel)
	got := stocks.m["s1"]
	if err != nil || got.Quantity != 10 || got.AveragePrice != 10 || math.Abs(got.AvgKRWPrice-13_000) > 1e-6 {
		t.Fatalf("10주@$10, 원화 13,000 으로 돌아가야 한다: %+v, err=%v", got, err)
	}
	if len(stocks.txs) != 1 || stocks.txs[0].ID != "t1" {
		t.Fatalf("저축 매수 기록(t2)만 지워져야 한다: %+v", stocks.txs)
	}
	if stocks.deletedTx != "txn-1" {
		t.Fatalf("가계부 거래도 같은 트랜잭션에서 지워야 한다: %q", stocks.deletedTx)
	}
}

func TestRevertSaving_USD매수기록이없으면_원화평단은그대로(t *testing.T) {
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Symbol: "NU", Quantity: 20, AveragePrice: 15, AvgKRWPrice: 20_500, Currency: "USD"}}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(280_000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 20}), noDel)
	got := stocks.m["s1"]
	if err != nil || got.Quantity != 10 || got.AveragePrice != 10 || got.AvgKRWPrice != 20_500 {
		t.Fatalf("수량·달러 평단만 되돌려야 한다: %+v, err=%v", got, err)
	}
}

func TestRevertSaving_저축으로만든종목은_지운다(t *testing.T) {
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Quantity: 3, AveragePrice: 50, Currency: "USD"}}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)

	err := s.RevertSaving(context.Background(), linkedSavingTx(200_000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", NewStockQty: 3, NewStockPrice: 50}), noDel)
	if _, ok := stocks.m["s1"]; ok || err != nil {
		t.Fatalf("종목이 지워져야 한다: err=%v", err)
	}
}

func TestRevertSaving_자산을이미지웠으면_거래만지운다(t *testing.T) {
	s := NewSavingService(nil, &memStocks{m: map[string]*models.StockAsset{}}, nil, &memAssets{m: map[string]*models.OtherAsset{}}, nil)
	ctx := context.Background()
	for _, l := range []models.SavingLink{
		{Kind: models.SavingKindDeposit, LinkAssetID: "gone"},
		{Kind: models.SavingKindStock, LinkAssetID: "gone", AddStockQty: 1, AddStockPrice: 1},
	} {
		deleted := false
		err := s.RevertSaving(ctx, linkedSavingTx(1, l), func(context.Context) error { deleted = true; return nil })
		if err != nil || !deleted {
			t.Fatalf("%s: 거래는 지워져야 한다: deleted=%v err=%v", l.Kind, deleted, err)
		}
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

func TestRevertSaving_저축뒤에판기록이있으면_409로막고아무것도안바꾼다(t *testing.T) {
	// 10주@100, 저축 10주@200 → 20주@150, 5주 매도 → 15주@150. 빼서 되돌리면 5주@50 이 된다.
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Symbol: "X", Quantity: 15, AveragePrice: 150, AvgKRWPrice: 150, Currency: "KRW"}},
		txs: []models.StockTransaction{
			{ID: "b1", CoupleID: "c1", StockAssetID: "s1", Type: models.StockTxBuy, Source: models.StockTxSourceSaving, Quantity: 10, Price: 200, ExecutedAt: savingDay, CreatedAt: savingDay},
			{ID: "s9", CoupleID: "c1", StockAssetID: "s1", Type: models.StockTxSell, Quantity: 5, Price: 180, ExecutedAt: savingDay.Add(time.Hour), CreatedAt: savingDay.Add(time.Hour)},
		}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)
	deleted := false

	err := s.RevertSaving(context.Background(), linkedSavingTx(2000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 200}),
		func(context.Context) error { deleted = true; return nil })
	if !errors.Is(err, ErrRevertAfterSell) {
		t.Fatalf("ErrRevertAfterSell 이어야 한다: %v", err)
	}
	if got := stocks.m["s1"]; deleted || got.Quantity != 15 || got.AveragePrice != 150 || len(stocks.txs) != 2 {
		t.Fatalf("거래·종목·기록이 그대로여야 한다: deleted=%v %+v txs=%d", deleted, got, len(stocks.txs))
	}
}

func TestRevertSaving_날짜를고친저축도_매수환율을찾는다(t *testing.T) {
	stocks := &memStocks{m: map[string]*models.StockAsset{"s1": {ID: "s1", CoupleID: "c1", Symbol: "NU", Quantity: 20, AveragePrice: 15, AvgKRWPrice: 20_500, Currency: "USD"}},
		txs: []models.StockTransaction{{ID: "b1", CoupleID: "c1", Symbol: "NU", StockAssetID: "s1", Type: models.StockTxBuy,
			Source: models.StockTxSourceSaving, Quantity: 10, Price: 20, ExchangeRateAtTx: 1400, ExecutedAt: savingDay}}}
	s := NewSavingService(nil, stocks, nil, &memAssets{}, nil)
	tx := linkedSavingTx(280_000, models.SavingLink{Kind: models.SavingKindStock, LinkAssetID: "s1", AddStockQty: 10, AddStockPrice: 20})
	tx.Date = savingDay.AddDate(0, 0, 5) // 거래 날짜만 나중에 고침

	err := s.RevertSaving(context.Background(), tx, noDel)
	if got := stocks.m["s1"]; err != nil || math.Abs(got.AvgKRWPrice-13_000) > 1e-6 {
		t.Fatalf("원화 평단이 13,000 으로 돌아가야 한다: %+v, err=%v", got, err)
	}
}
