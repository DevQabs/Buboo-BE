package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/models"
)

func TestCalcDividend(t *testing.T) {
	zero, neg := 0.0, -1.0
	rate := func(r float64, err error) func() (float64, error) {
		return func() (float64, error) { return r, err }
	}
	cases := []struct {
		name     string
		req      models.CreateDividendRequest
		fetch    func() (float64, error)
		wantKRW  int64
		wantRate float64
		wantErr  bool
	}{
		{"세율을 안 보내면 15.4%", models.CreateDividendRequest{Currency: "USD", Quantity: 10, AmountPerShare: 1, USDKRWRate: 1000}, nil, 8460, 1000, false},
		{"세율 0 은 그대로 0", models.CreateDividendRequest{Currency: "USD", Quantity: 10, AmountPerShare: 1, USDKRWRate: 1000, TaxRate: &zero}, nil, 10000, 1000, false},
		{"음수 세율은 기본값", models.CreateDividendRequest{Currency: "USD", Quantity: 10, AmountPerShare: 1, USDKRWRate: 1000, TaxRate: &neg}, nil, 8460, 1000, false},
		{"환율이 없으면 현재 환율", models.CreateDividendRequest{Currency: "USD", Quantity: 10, AmountPerShare: 1, TaxRate: &zero}, rate(1400, nil), 14000, 1400, false},
		{"환율 조회 실패면 오류", models.CreateDividendRequest{Currency: "USD", Quantity: 10, AmountPerShare: 1}, rate(0, errors.New("down")), 0, 0, true},
		{"원화 배당은 환율과 무관", models.CreateDividendRequest{Currency: "KRW", Quantity: 10, AmountPerShare: 1000, TaxRate: &zero}, nil, 10000, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := calcDividend(c.req, c.fetch)
			if (err != nil) != c.wantErr {
				t.Fatalf("오류 여부가 다르다: %v", err)
			}
			if !c.wantErr && (got.AmountKRW != c.wantKRW || got.Rate != c.wantRate) {
				t.Fatalf("원화 %d·환율 %.0f 이어야 한다: %+v", c.wantKRW, c.wantRate, got)
			}
		})
	}
}

func TestDeleteDividend_가계부수입도지운다(t *testing.T) {
	txs := &fakeTxRepo{txs: map[string]*models.Transaction{"txn-1": {ID: "txn-1"}}}
	divs := &fakeDivRepo{divs: map[string]*models.DividendEvent{"div-1": {ID: "div-1", TransactionID: "txn-1"}}}
	h := &Handler{txRepo: txs, divRepo: divs}
	r := chi.NewRouter()
	r.Delete("/d/{id}", h.deleteDividend)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/d/div-1", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("204 이어야 한다: %d %s", rec.Code, rec.Body)
	}
	if _, ok := divs.divs["div-1"]; ok {
		t.Error("배당이 남았다")
	}
	if _, ok := txs.txs["txn-1"]; ok {
		t.Error("배당으로 만든 가계부 수입이 남았다")
	}
}

func TestDeleteDividend_수입삭제가실패하면배당을남긴다(t *testing.T) {
	txs := &fakeTxRepo{txs: map[string]*models.Transaction{"txn-1": {ID: "txn-1"}}, delErr: errors.New("db down")}
	divs := &fakeDivRepo{divs: map[string]*models.DividendEvent{"div-1": {ID: "div-1", TransactionID: "txn-1"}}}
	h := &Handler{txRepo: txs, divRepo: divs}
	r := chi.NewRouter()
	r.Delete("/d/{id}", h.deleteDividend)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/d/div-1", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("500 이어야 한다: %d", rec.Code)
	}
	if _, ok := divs.divs["div-1"]; !ok {
		t.Error("수입 삭제가 실패했는데 배당이 지워졌다")
	}
}

func TestDeleteDividend_수입조회가실패하면배당을남긴다(t *testing.T) {
	txs := &fakeTxRepo{txs: map[string]*models.Transaction{"txn-1": {ID: "txn-1"}}, getErr: errors.New("db down")}
	divs := &fakeDivRepo{divs: map[string]*models.DividendEvent{"div-1": {ID: "div-1", TransactionID: "txn-1"}}}
	h := &Handler{txRepo: txs, divRepo: divs}
	r := chi.NewRouter()
	r.Delete("/d/{id}", h.deleteDividend)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/d/div-1", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("500 이어야 한다: %d", rec.Code)
	}
	if _, ok := divs.divs["div-1"]; !ok {
		t.Error("수입 조회가 실패했는데 배당이 지워졌다")
	}
}

func TestDeleteDividend_수입을이미지웠으면_배당만지운다(t *testing.T) {
	txs := &fakeTxRepo{txs: map[string]*models.Transaction{}}
	divs := &fakeDivRepo{divs: map[string]*models.DividendEvent{"div-1": {ID: "div-1", TransactionID: "txn-gone"}}}
	h := &Handler{txRepo: txs, divRepo: divs}
	r := chi.NewRouter()
	r.Delete("/d/{id}", h.deleteDividend)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/d/div-1", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("204 이어야 한다: %d %s", rec.Code, rec.Body)
	}
	if _, ok := divs.divs["div-1"]; ok {
		t.Error("배당이 남았다")
	}
}
