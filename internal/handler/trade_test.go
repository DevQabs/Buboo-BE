package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

func TestRespondTradeError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"요청 오류는 그 상태로", statusError{http.StatusUnprocessableEntity, errors.New("원화 취득가가 없어요")}, http.StatusUnprocessableEntity},
		{"없는 종목은 404", repository.ErrNotFound, http.StatusNotFound},
		{"그 밖은 500", errors.New("db down"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		respondTradeError(rec, c.err)
		if rec.Code != c.want {
			t.Errorf("%s: %d 이어야 한다, got %d", c.name, c.want, rec.Code)
		}
	}
}

func TestSellStock_거래로처리한다(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", UserID: "u1", Symbol: "005930", Quantity: 5, AveragePrice: 100, AvgKRWPrice: 100, Currency: "KRW"},
	}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}

	if w := stockCall(h.sellStock, "stock-1", `{"quantity":6,"price":120}`); w.Code != http.StatusBadRequest {
		t.Fatalf("보유보다 많이 팔면 400: %d %s", w.Code, w.Body)
	}
	if len(st.logs) != 0 || st.rows["stock-1"].Quantity != 5 {
		t.Fatalf("거부된 매도는 아무것도 바꾸지 않아야 한다: %+v", st.logs)
	}
	w := stockCall(h.sellStock, "stock-1", `{"quantity":5,"price":120}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"removed":true`) {
		t.Fatalf("전량 매도는 종목을 지운다: %d %s", w.Code, w.Body)
	}
	if _, ok := st.rows["stock-1"]; ok {
		t.Fatal("종목이 남아 있다")
	}
	if len(st.logs) != 1 || st.logs[0].RealizedPnL != 100 || st.logs[0].Source != models.StockTxSourceTrade {
		t.Fatalf("매도 기록(손익 100, source trade)이 하나여야 한다: %+v", st.logs)
	}
	if w := stockCall(h.sellStock, "stock-1", `{"quantity":1,"price":120}`); w.Code != http.StatusNotFound {
		t.Fatalf("지워진 종목 매도는 404: %d", w.Code)
	}
}

func TestSellStock_0점1더하기0점2를사고0점3을팔면_종목이지워진다(t *testing.T) {
	st := &memStocks{rows: map[string]models.StockAsset{
		"stock-1": {ID: "stock-1", UserID: "u1", Symbol: "005930", Quantity: 0.1 + 0.2, AveragePrice: 100, AvgKRWPrice: 100, Currency: "KRW"},
	}}
	h := &Handler{stockRepo: st, stxRepo: &memSTx{}}
	w := stockCall(h.sellStock, "stock-1", `{"quantity":0.3,"price":100}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"removed":true`) {
		t.Fatalf("남은 5.55e-17 주는 0 으로 보고 지워야 한다: %d %s", w.Code, w.Body)
	}
}
