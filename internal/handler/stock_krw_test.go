package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
	"github.com/yourname/couple-app/internal/service"
)

// memStocks 는 테스트에 필요한 메서드만 구현한다. 나머지는 nil 인터페이스라 호출되면 panic.
type memStocks struct {
	repository.StockRepository
	rows map[string]models.StockAsset
	logs []models.StockTransaction // Trade 가 남긴 매매 기록
}

func (m *memStocks) GetByID(_ context.Context, id string) (*models.StockAsset, error) {
	a, ok := m.rows[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &a, nil
}

func (m *memStocks) Create(_ context.Context, a *models.StockAsset) (*models.StockAsset, error) {
	a.ID = "stock-1"
	m.rows[a.ID] = *a
	return a, nil
}

func (m *memStocks) Update(_ context.Context, a *models.StockAsset) (*models.StockAsset, error) {
	m.rows[a.ID] = *a
	return a, nil
}

// Trade 는 PG 구현처럼 apply 결과를 저장(0 이하면 삭제)하고 기록을 남긴다.
func (m *memStocks) Trade(_ context.Context, id, _ string, apply func(*models.StockAsset) (*models.StockTransaction, error)) (*models.StockAsset, error) {
	a, ok := m.rows[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	log, err := apply(&a)
	if err != nil {
		return nil, err
	}
	if a.Quantity <= 0 {
		delete(m.rows, id)
	} else {
		m.rows[id] = a
	}
	if log != nil {
		m.logs = append(m.logs, *log)
	}
	return &a, nil
}

type memSTx struct {
	repository.StockTransactionRepository
	rows []models.StockTransaction
}

func (m *memSTx) Create(_ context.Context, t *models.StockTransaction) (*models.StockTransaction, error) {
	m.rows = append(m.rows, *t)
	return t, nil
}

func stockCall(fn http.HandlerFunc, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func TestStockKRWBasis_CreateBuySell(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{}}
	sx := &memSTx{}
	h := &Handler{stockRepo: st, stxRepo: sx}

	// 생성: 10주 × $14 × 1,400 → 원화 평단 19,600
	w := stockCall(h.createStock, "", `{"user_id":"u1","symbol":"nu","exchange":"NYSE","name":"NU",
		"quantity":10,"average_price":14,"currency":"USD","exchange_rate":1400}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("생성 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 19_600 {
		t.Fatalf("생성 시 원화 평단: want 19600, got %v", got)
	}

	// 매수: 10주 × $16 × 1,500 → (196,000 + 240,000) / 20 = 21,800
	w = stockCall(h.buyStock, "stock-1", `{"quantity":10,"price":16,"exchange_rate":1500}`)
	if w.Code != http.StatusOK {
		t.Fatalf("매수 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 21_800 {
		t.Errorf("매수 후 원화 평단: want 21800, got %v", got)
	}
	if got := st.logs[len(st.logs)-1].ExchangeRateAtTx; got != 1500 {
		t.Errorf("매수 로그 환율: want 1500, got %v", got)
	}

	// 매도: 원화 평단이 있으니 422 가 아니어야 한다.
	w = stockCall(h.sellStock, "stock-1", `{"quantity":5,"price":20,"exchange_rate":1450}`)
	if w.Code != http.StatusOK {
		t.Errorf("매도 실패: %d %s", w.Code, w.Body)
	}
}

func TestCreateStock_RejectsAnImplausibleRate(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}

	w := stockCall(h.createStock, "", `{"user_id":"u1","symbol":"NU","exchange":"NYSE","name":"NU",
		"quantity":10,"average_price":14,"currency":"USD","exchange_rate":1}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("환율 1 은 400 이어야 한다: got %d", w.Code)
	}
	if len(st.rows) != 0 {
		t.Errorf("거부했는데 저장됐다")
	}
}

func TestCreateStock_KRWStockAverageIsItsOwnKRWBasis(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}

	w := stockCall(h.createStock, "", `{"user_id":"u1","symbol":"005930","exchange":"KRX","name":"삼성전자",
		"quantity":10,"average_price":70000,"currency":"KRW"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("생성 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 70_000 {
		t.Errorf("KRW 원화 평단: want 70000, got %v", got)
	}
}

func TestBuyStock_RecoversAStuckZeroBasis(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", Currency: "USD", Quantity: 203, AveragePrice: 13.9465, AvgKRWPrice: 0},
	}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}

	w := stockCall(h.buyStock, "stock-1", `{"quantity":108,"price":13.94,"exchange_rate":1418.12}`)
	if w.Code != http.StatusOK {
		t.Fatalf("매수 실패: %d %s", w.Code, w.Body)
	}
	a := st.rows["stock-1"]
	if math.Abs(a.AvgKRWPrice-19_774.61) > 0.01 {
		t.Errorf("0 고착이 풀려야 한다: got %v, want 19774.61", a.AvgKRWPrice)
	}
	if err := service.ValidateKRWCostBasis(a.AveragePrice, a.AvgKRWPrice); err != nil {
		t.Errorf("매수 후에도 매도가 막힌다: %v", err)
	}
}

func TestUpdateStock_AcceptsAPlausibleKRWBasisOnly(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", Currency: "USD", Quantity: 10, AveragePrice: 14, AvgKRWPrice: 0},
	}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}

	if w := stockCall(h.updateStock, "stock-1", `{"avg_krw_price":3135}`); w.Code != http.StatusBadRequest {
		t.Errorf("역산 환율 224 는 400 이어야 한다: got %d", w.Code)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 0 {
		t.Errorf("거부했는데 바뀌었다: %v", got)
	}

	if w := stockCall(h.updateStock, "stock-1", `{"avg_krw_price":19600}`); w.Code != http.StatusOK {
		t.Fatalf("수정 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 19_600 {
		t.Errorf("원화 평단 수정: want 19600, got %v", got)
	}
}
