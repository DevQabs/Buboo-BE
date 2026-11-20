package handler

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
)

var corrDay = time.Date(2026, 10, 1, 10, 0, 0, 0, models.KST)

func corrLeg(id string, day int, typ models.StockTxType, qty, price, fx float64) models.StockTransaction {
	return models.StockTransaction{ID: id, CoupleID: "", StockAssetID: "stock-1", Symbol: "NU", Type: typ, Quantity: qty, Price: price,
		Currency: "USD", ExchangeRateAtTx: fx, Source: models.StockTxSourceTrade, ExecutedAt: corrDay.AddDate(0, 0, day)}
}

// corrFixture: 10주@$10(1300) 매수, 5주@$12(1400) 매도 → 지금 5주@$10, 원화 13,000. 매도 손익 19,000.
func corrFixture(rates *fakeRates) (*Handler, *memStocks) {
	st := &memStocks{
		rows: map[string]models.StockAsset{"stock-1": {ID: "stock-1", Symbol: "NU", Currency: "USD", Quantity: 5, AveragePrice: 10, AvgKRWPrice: 13_000}},
		logs: []models.StockTransaction{corrLeg("b1", 0, models.StockTxBuy, 10, 10, 1300), corrLeg("s1", 3, models.StockTxSell, 5, 12, 1400)},
	}
	st.logs[1].AvgPriceAtTx, st.logs[1].RealizedPnL = 10, 19_000
	return &Handler{stockRepo: st, stxRepo: &memSTx{stocks: st}, rates: rates}, st
}

func corrCall(fn http.HandlerFunc, method, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func logByID(st *memStocks, id string) models.StockTransaction {
	for _, t := range st.logs {
		if t.ID == id {
			return t
		}
	}
	return models.StockTransaction{}
}

func TestUpdateStockTx_매수환율을고치면_원화평단과매도손익이다시계산된다(t *testing.T) {
	h, st := corrFixture(nil)
	w := corrCall(h.updateStockTx, http.MethodPut, "b1", `{"exchange_rate":1200}`)
	if w.Code != http.StatusOK {
		t.Fatalf("정정 실패: %d %s", w.Code, w.Body)
	}
	if a := st.rows["stock-1"]; a.Quantity != 5 || math.Abs(a.AvgKRWPrice-12_000) > 1e-6 {
		t.Fatalf("5주, 원화 평단 12,000 이어야 한다: %+v", a)
	}
	if got := logByID(st, "s1").RealizedPnL; math.Abs(got-24_000) > 1e-6 {
		t.Fatalf("매도 손익 (16,800-12,000)×5 = 24,000 이어야 한다: %v", got)
	}
}

func TestUpdateStockTx_날짜만바꾸면_그날환율을다시정한다(t *testing.T) {
	rates := &fakeRates{live: 1500, on: 1250}
	h, st := corrFixture(rates)
	w := corrCall(h.updateStockTx, http.MethodPut, "b1", `{"date":"2026-09-20"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("정정 실패: %d %s", w.Code, w.Body)
	}
	b := logByID(st, "b1")
	if b.ExchangeRateAtTx != 1250 || b.ExecutedAt.In(models.KST).Format("2006-01-02 15:04") != "2026-09-20 10:00" {
		t.Fatalf("9/20 10:00, 환율 1250 이어야 한다: %+v", b)
	}
	if w := corrCall(h.updateStockTx, http.MethodPut, "b1", `{"date":"2999-01-01"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("앞날은 400: %d", w.Code)
	}
}

func TestUpdateStockTx_보유가음수가되는정정은_거부하고아무것도안바꾼다(t *testing.T) {
	h, st := corrFixture(&fakeRates{on: 1400})
	before := append([]models.StockTransaction(nil), st.logs...)
	if w := corrCall(h.updateStockTx, http.MethodPut, "s1", `{"quantity":11}`); w.Code != http.StatusConflict {
		t.Fatalf("10주 보유에 11주 매도는 409: %d %s", w.Code, w.Body)
	}
	if w := corrCall(h.updateStockTx, http.MethodPut, "s1", `{"date":"2026-09-01"}`); w.Code != http.StatusConflict {
		t.Fatalf("매도를 매수 앞으로 옮기면 409: %d %s", w.Code, w.Body)
	}
	if a := st.rows["stock-1"]; a.Quantity != 5 || logByID(st, "s1").Quantity != before[1].Quantity {
		t.Fatalf("거부했는데 바뀌었다: %+v", a)
	}
}

func TestDeleteStockTx_매도를지우면_수량이돌아오고_매수를지우면_종목이빈다(t *testing.T) {
	h, st := corrFixture(nil)
	if w := corrCall(h.deleteStockTx, http.MethodDelete, "s1", ``); w.Code != http.StatusOK {
		t.Fatalf("삭제 실패: %d %s", w.Code, w.Body)
	}
	if a := st.rows["stock-1"]; a.Quantity != 10 || len(st.logs) != 1 {
		t.Fatalf("10주, 기록 1건이어야 한다: %+v %d", a, len(st.logs))
	}
	w := corrCall(h.deleteStockTx, http.MethodDelete, "b1", ``)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"removed":true`) {
		t.Fatalf("마지막 매수를 지우면 종목이 없어진다: %d %s", w.Code, w.Body)
	}
	if _, ok := st.rows["stock-1"]; ok {
		t.Fatal("종목이 남았다")
	}
}

func TestDeleteStockTx_기록없이등록한보유분은_그대로둔다(t *testing.T) {
	// 기록 없이 등록한 20주@100(KRW) 에 5주 매수(@200) 기록 → 지금 25주@120. 매수를 지우면 20주@100.
	st := &memStocks{
		rows: map[string]models.StockAsset{"stock-1": {ID: "stock-1", Currency: "KRW", Quantity: 25, AveragePrice: 120, AvgKRWPrice: 120}},
		logs: []models.StockTransaction{{ID: "b1", StockAssetID: "stock-1", Type: models.StockTxBuy, Quantity: 5, Price: 200, Currency: "KRW", ExecutedAt: corrDay}},
	}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{stocks: st}}
	if w := corrCall(h.deleteStockTx, http.MethodDelete, "b1", ``); w.Code != http.StatusOK {
		t.Fatalf("삭제 실패: %d %s", w.Code, w.Body)
	}
	if a := st.rows["stock-1"]; math.Abs(a.Quantity-20) > 1e-9 || math.Abs(a.AveragePrice-100) > 1e-9 {
		t.Fatalf("20주@100 이 남아야 한다: %+v", a)
	}
}

func TestCorrectStockTx_저축매수기록은_날짜환율만(t *testing.T) {
	h, st := corrFixture(nil)
	st.logs[0].Source = models.StockTxSourceSaving
	if w := corrCall(h.deleteStockTx, http.MethodDelete, "b1", ``); w.Code != http.StatusConflict {
		t.Fatalf("저축 매수 삭제는 409: %d", w.Code)
	}
	if w := corrCall(h.updateStockTx, http.MethodPut, "b1", `{"quantity":3}`); w.Code != http.StatusConflict {
		t.Fatalf("저축 매수 수량 정정은 409: %d", w.Code)
	}
	if w := corrCall(h.updateStockTx, http.MethodPut, "b1", `{"exchange_rate":1350}`); w.Code != http.StatusOK {
		t.Fatalf("저축 매수 환율 정정은 된다: %d %s", w.Code, w.Body)
	}
}

// 라우터 전체: 다른 그룹 기록은 404, 자산 편집 권한이 없으면 403.
func TestStockTxRoutes_다른그룹은404_보기권한은403(t *testing.T) {
	secret := []byte("jwt")
	h, _ := corrFixture(nil)
	h.jwtSecret = secret
	h.owner = &fakeOwner{owned: map[string]string{"b1": "c1", "theirs": "c2"}}
	h.permRepo = &fakePerms{perms: map[string]*models.Permissions{
		"u1": {IsOwner: true},
		"u2": {Menus: map[string]string{"assets": models.LevelView}},
	}}
	call := func(user, method, id string) int {
		token, _ := auth.IssueToken(secret, user, "c1", "husband")
		req := httptest.NewRequest(method, "/api/stocks/transactions/"+id, strings.NewReader(`{"price":11}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.NewRouter().ServeHTTP(rec, req)
		return rec.Code
	}
	for _, m := range []string{http.MethodPut, http.MethodDelete} {
		if got := call("u1", m, "theirs"); got != http.StatusNotFound {
			t.Errorf("%s 다른 그룹 기록은 404: %d", m, got)
		}
		if got := call("u2", m, "b1"); got != http.StatusForbidden {
			t.Errorf("%s 보기 권한은 403: %d", m, got)
		}
	}
}
