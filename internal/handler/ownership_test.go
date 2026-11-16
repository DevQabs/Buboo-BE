package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/auth"
)

type fakeOwner struct {
	owned map[string]string // id -> couple_id
	err   error
	table string
}

func (f *fakeOwner) Owns(_ context.Context, table, id, coupleID string) (bool, error) {
	f.table = table
	if f.err != nil {
		return false, f.err
	}
	return f.owned[id] == coupleID, nil
}

func TestOwned(t *testing.T) {
	secret := []byte("jwt")
	token, _ := auth.IssueToken(secret, "u1", "c1", "husband")

	run := func(owner *fakeOwner, id string) int {
		h := &Handler{owner: owner}
		r := chi.NewRouter()
		r.Use(auth.RequireAuth(secret))
		r.Route("/x/{id}", func(r chi.Router) {
			r.Use(h.owned("transactions"))
			r.Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		})
		req := httptest.NewRequest(http.MethodGet, "/x/"+id+"/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	owner := &fakeOwner{owned: map[string]string{"mine": "c1", "theirs": "c2"}}
	if got := run(owner, "mine"); got != http.StatusNoContent {
		t.Errorf("내 그룹 id 는 통과해야 한다: got %d", got)
	}
	if owner.table != "transactions" {
		t.Errorf("테이블 이름 전달: got %q", owner.table)
	}
	if got := run(owner, "theirs"); got != http.StatusNotFound {
		t.Errorf("다른 그룹 id 는 404: got %d", got)
	}
	if got := run(owner, "missing"); got != http.StatusNotFound {
		t.Errorf("없는 id 는 404: got %d", got)
	}
	if got := run(&fakeOwner{err: errors.New("db down")}, "mine"); got != http.StatusInternalServerError {
		t.Errorf("조회 실패는 500: got %d", got)
	}
}
