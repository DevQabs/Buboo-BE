package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

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

func (m *memStocks) Create(_ context.Context, a *models.StockAsset, buy *models.StockTransaction) (*models.StockAsset, error) {
	a.ID = "stock-1"
	m.rows[a.ID] = *a
	if buy != nil {
		buy.StockAssetID = a.ID
		m.logs = append(m.logs, *buy)
	}
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
	if a.Quantity < models.QtyEpsilon {
		a.Quantity = 0
		delete(m.rows, id)
	} else {
		m.rows[id] = a
	}
	if log != nil {
		m.logs = append(m.logs, *log)
	}
	return &a, nil
}

// Rewrite 는 PG 구현처럼 오류면 아무것도 바꾸지 않고, 성공하면 종목과 기록(logs) 변경을 함께 반영한다.
func (m *memStocks) Rewrite(_ context.Context, id, _ string, fn func(*models.StockAsset, []models.StockTransaction) (repository.StockTxChanges, error)) (*models.StockAsset, error) {
	var a *models.StockAsset
	if r, ok := m.rows[id]; ok {
		a = &r
	}
	var txs []models.StockTransaction
	for _, t := range m.logs {
		if t.StockAssetID == id {
			txs = append(txs, t)
		}
	}
	if a == nil && len(txs) == 0 {
		return nil, repository.ErrNotFound
	}
	ch, err := fn(a, slices.Clone(txs))
	if err != nil {
		return nil, err
	}
	m.logs = slices.DeleteFunc(m.logs, func(t models.StockTransaction) bool { return slices.Contains(ch.Delete, t.ID) })
	for _, u := range ch.Update {
		for i := range m.logs {
			if m.logs[i].ID == u.ID {
				m.logs[i] = u
			}
		}
	}
	if a != nil {
		if a.Quantity < models.QtyEpsilon {
			a.Quantity = 0
			delete(m.rows, id)
		} else {
			m.rows[id] = *a
		}
	}
	return a, nil
}

// memSTx.GetByID 는 stocks.logs 를 본다(정정 테스트는 기록을 memStocks 한 곳에 둔다).
type memSTx struct {
	repository.StockTransactionRepository
	rows   []models.StockTransaction
	stocks *memStocks
}

func (m *memSTx) GetByID(_ context.Context, coupleID, id string) (*models.StockTransaction, error) {
	for _, t := range m.stocks.logs {
		if t.ID == id && t.CoupleID == coupleID {
			return &t, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *memSTx) Create(_ context.Context, t *models.StockTransaction) (*models.StockTransaction, error) {
	m.rows = append(m.rows, *t)
	return t, nil
}

// fakeRates 는 지금 환율(live)과 날짜별 종가(on)를 돌려준다.
type fakeRates struct {
	live, on float64
	err      error
	onDays   []time.Time
}

func (f *fakeRates) FetchUSDKRW(context.Context) (float64, error) { return f.live, f.err }
func (f *fakeRates) FetchUSDKRWOn(_ context.Context, d time.Time) (float64, error) {
	f.onDays = append(f.onDays, d)
	return f.on, f.err
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
	rates := &fakeRates{live: 1400}
	h := &Handler{stockRepo: st, stxRepo: sx, rates: rates}

	// 생성: 10주 × $14 × 1,400 → 원화 평단 19,600. 화면이 보낸 환율(1380)은 쓰지 않는다.
	w := stockCall(h.createStock, "", `{"user_id":"u1","symbol":"nu","exchange":"NYSE","name":"NU",
		"quantity":10,"average_price":14,"currency":"USD","exchange_rate":1380}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("생성 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 19_600 {
		t.Fatalf("생성 시 원화 평단: want 19600, got %v", got)
	}

	// 매수: 10주 × $16 × 1,500 → (196,000 + 240,000) / 20 = 21,800
	rates.live = 1500
	w = stockCall(h.buyStock, "stock-1", `{"quantity":10,"price":16,"exchange_rate":1380}`)
	if w.Code != http.StatusOK {
		t.Fatalf("매수 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 21_800 {
		t.Errorf("매수 후 원화 평단: want 21800, got %v", got)
	}
	if got := st.logs[len(st.logs)-1].ExchangeRateAtTx; got != 1500 {
		t.Errorf("매수 로그 환율: want 1500, got %v", got)
	}

	if !strings.Contains(w.Body.String(), `"exchange_rate":1500`) {
		t.Errorf("응답에 쓴 환율이 있어야 한다: %s", w.Body)
	}

	// 매도: 원화 평단이 있으니 422 가 아니어야 한다. 손익 = (20×1,450 − 21,800) × 5 = 36,000
	rates.live = 1450
	w = stockCall(h.sellStock, "stock-1", `{"quantity":5,"price":20,"exchange_rate":1380}`)
	if w.Code != http.StatusOK {
		t.Fatalf("매도 실패: %d %s", w.Code, w.Body)
	}
	if l := st.logs[len(st.logs)-1]; l.ExchangeRateAtTx != 1450 || l.RealizedPnL != 36_000 {
		t.Errorf("매도 기록: want (1450, 36000), got (%v, %v)", l.ExchangeRateAtTx, l.RealizedPnL)
	}
}

func TestCreateStock_RejectsAnImplausibleRate(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}, rates: &fakeRates{live: 1}}

	w := stockCall(h.createStock, "", `{"user_id":"u1","symbol":"NU","exchange":"NYSE","name":"NU",
		"quantity":10,"average_price":14,"currency":"USD"}`)
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
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}, rates: &fakeRates{live: 1418.12}}

	w := stockCall(h.buyStock, "stock-1", `{"quantity":108,"price":13.94}`)
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

func TestUpdateStock_다른그룹사람에게는_넘길수없다(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{"stock-1": {ID: "stock-1", UserID: "u1", Currency: "KRW", Quantity: 1, AveragePrice: 100, AvgKRWPrice: 100}}}
	// stockCall 은 인증 없이 부르므로 호출자 그룹은 "" 다.
	users := &fakeUsers{users: []models.User{{ID: "u1", CoupleID: ""}, {ID: "u2", CoupleID: ""}, {ID: "x9", CoupleID: "other"}}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}, userRepo: users}

	if w := stockCall(h.updateStock, "stock-1", `{"user_id":"x9"}`); w.Code != http.StatusBadRequest {
		t.Errorf("다른 그룹 사용자는 400 이어야 한다: got %d", w.Code)
	}
	if got := st.rows["stock-1"].UserID; got != "u1" {
		t.Errorf("거부했는데 소유자가 바뀌었다: %v", got)
	}
	if w := stockCall(h.updateStock, "stock-1", `{"user_id":"u2"}`); w.Code != http.StatusOK {
		t.Fatalf("같은 그룹 사용자로 바꾸기 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].UserID; got != "u2" {
		t.Errorf("소유자: want u2, got %v", got)
	}
}

func TestUpdateStock_원화종목평단을고치면_원화평단도같이바뀐다(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{"stock-1": {ID: "stock-1", Currency: "KRW", Quantity: 1, AveragePrice: 100, AvgKRWPrice: 100}}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}

	if w := stockCall(h.updateStock, "stock-1", `{"average_price":70000}`); w.Code != http.StatusOK {
		t.Fatalf("수정 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 70_000 {
		t.Errorf("원화 종목 원화 평단: want 70000, got %v", got)
	}
}

func TestUpdateStock_보내지않은수량은_그사이매수를덮어쓰지않는다(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{"stock-1": {ID: "stock-1", Currency: "KRW", Quantity: 10, AveragePrice: 100, AvgKRWPrice: 100}}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}

	// 수정 화면을 연 뒤 다른 사람이 5주를 샀다.
	a := st.rows["stock-1"]
	a.Quantity, a.AveragePrice, a.AvgKRWPrice = 15, 120, 120
	st.rows["stock-1"] = a

	if w := stockCall(h.updateStock, "stock-1", `{"name":"삼성전자","memo":"장기"}`); w.Code != http.StatusOK {
		t.Fatalf("수정 실패: %d %s", w.Code, w.Body)
	}
	got := st.rows["stock-1"]
	if got.Quantity != 15 || got.AveragePrice != 120 || got.Name != "삼성전자" || got.Memo != "장기" {
		t.Fatalf("이름·메모만 바뀌고 수량·평단은 15주@120 이어야 한다: %+v", got)
	}
	if w := stockCall(h.updateStock, "missing", `{"name":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("없는 종목은 404: %d", w.Code)
	}
}

func TestCreateStock_지난매수일이면_그날환율을쓴다(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{}}
	rates := &fakeRates{live: 1500, on: 1300}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}, rates: rates}

	w := stockCall(h.createStock, "", `{"user_id":"u1","symbol":"NU","exchange":"NYSE","name":"NU",
		"quantity":10,"average_price":10,"currency":"USD","purchased_at":"2026-03-09T00:00:00+09:00"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("생성 실패: %d %s", w.Code, w.Body)
	}
	if got := st.rows["stock-1"].AvgKRWPrice; got != 13_000 || len(rates.onDays) != 1 {
		t.Fatalf("3월 9일 종가 1,300 으로 원화 평단 13,000 이어야 한다: %v %v", got, rates.onDays)
	}
	if len(st.logs) != 1 || st.logs[0].Source != models.StockTxSourceInitial || st.logs[0].ExchangeRateAtTx != 1300 ||
		st.logs[0].Quantity != 10 || st.logs[0].ExecutedAt.Format(time.DateOnly) != "2026-03-09" {
		t.Fatalf("첫 보유분 매수 기록(initial, 환율 1300, 매수일)이 남아야 한다: %+v", st.logs)
	}
}

func TestTrade_환율을못가져오면_대체환율없이400(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", Currency: "USD", Quantity: 10, AveragePrice: 14, AvgKRWPrice: 19_600},
	}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}, rates: &fakeRates{err: errors.New("yahoo down")}}

	for name, fn := range map[string]http.HandlerFunc{"매수": h.buyStock, "매도": h.sellStock} {
		w := stockCall(fn, "stock-1", `{"quantity":1,"price":15,"exchange_rate":1380}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "다시 시도") {
			t.Errorf("%s: 400 다시 시도여야 한다: %d %s", name, w.Code, w.Body)
		}
	}
	if len(st.logs) != 0 || st.rows["stock-1"].Quantity != 10 {
		t.Fatalf("아무것도 바뀌면 안 된다: %+v", st.logs)
	}
}

func TestSellStock_범위밖환율은_400(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", Currency: "USD", Quantity: 10, AveragePrice: 14, AvgKRWPrice: 19_600},
	}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}, rates: &fakeRates{live: 3000}}
	if w := stockCall(h.sellStock, "stock-1", `{"quantity":1,"price":15}`); w.Code != http.StatusBadRequest {
		t.Fatalf("환율 3000 매도는 400: %d %s", w.Code, w.Body)
	}
}
