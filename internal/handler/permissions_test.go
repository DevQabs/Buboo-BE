package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

type fakePerms struct {
	perms    map[string]*models.Permissions // userID -> 권한 (없으면 그룹에서 빠진 사람)
	err      error
	replaced map[string]string
	removed  string
}

func (f *fakePerms) Get(_ context.Context, userID, _ string) (*models.Permissions, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.perms[userID], nil
}

func (f *fakePerms) ListByCouple(context.Context, string) (map[string]map[string]string, error) {
	return map[string]map[string]string{}, nil
}

func (f *fakePerms) Replace(_ context.Context, _ string, levels map[string]string) error {
	f.replaced = levels
	return nil
}

func (f *fakePerms) RemoveMember(_ context.Context, userID string) error {
	f.removed = userID
	return nil
}

// fakeUsers implements only what these tests call; other methods panic on the nil embed.
type fakeUsers struct {
	repository.UserRepository
	users []models.User
}

func (f *fakeUsers) ListUsers(_ context.Context, coupleID string) ([]models.User, error) {
	var out []models.User
	for _, u := range f.users {
		if u.CoupleID == coupleID {
			out = append(out, u)
		}
	}
	return out, nil
}

var (
	ownerPerms = &models.Permissions{IsOwner: true, Menus: map[string]string{}}
	grandPerms = &models.Permissions{Menus: map[string]string{"baby_photo": "edit", "baby_growth": "edit"}}
	ledgerView = &models.Permissions{Menus: map[string]string{"ledger": "view"}}
)

func newPermHandler(perms *fakePerms, users []models.User) *Handler {
	return &Handler{jwtSecret: []byte("jwt"), permRepo: perms, userRepo: &fakeUsers{users: users}}
}

// do sends a request through the real router as userID of couple c1.
func do(h *Handler, userID, method, path, body string) *httptest.ResponseRecorder {
	token, _ := auth.IssueToken(h.jwtSecret, userID, "c1", "husband")
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if h.internalSecret != nil {
		req.Header.Set("X-Internal-Secret", string(h.internalSecret))
	}
	rec := httptest.NewRecorder()
	h.NewRouter().ServeHTTP(rec, req)
	return rec
}

func TestRequireMenu(t *testing.T) {
	cases := []struct {
		name   string
		perms  *models.Permissions
		method string
		want   int
	}{
		{"owner 는 행 없이 쓰기 통과", ownerPerms, http.MethodPost, http.StatusNoContent},
		{"가계부 보기로 자산 목록 읽기 통과", ledgerView, http.MethodGet, http.StatusNoContent},
		{"HEAD 는 읽기로 본다", ledgerView, http.MethodHead, http.StatusNoContent},
		{"가계부 보기로 자산 쓰기 거부", ledgerView, http.MethodPost, http.StatusForbidden},
		{"가계부 편집이어도 자산 쓰기 거부", &models.Permissions{Menus: map[string]string{"ledger": "edit"}}, http.MethodPut, http.StatusForbidden},
		{"자산 보기로 쓰기 거부", &models.Permissions{Menus: map[string]string{"assets": "view"}}, http.MethodDelete, http.StatusForbidden},
		{"자산 편집으로 쓰기 통과", &models.Permissions{Menus: map[string]string{"assets": "edit"}}, http.MethodDelete, http.StatusNoContent},
		{"권한 없는 메뉴 읽기 거부", grandPerms, http.MethodGet, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newPermHandler(&fakePerms{perms: map[string]*models.Permissions{"u": c.perms}}, nil)
			r := chi.NewRouter()
			r.Use(auth.RequireAuth(h.jwtSecret))
			r.Use(h.loadPermissions)
			r.With(requireMenu("assets", "ledger")).MethodFunc(c.method, "/x", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			token, _ := auth.IssueToken(h.jwtSecret, "u", "c1", "wife")
			req := httptest.NewRequest(c.method, "/x", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestLoadPermissions(t *testing.T) {
	h := newPermHandler(&fakePerms{perms: map[string]*models.Permissions{}}, nil)
	if got := do(h, "gone", http.MethodGet, "/api/users", "").Code; got != http.StatusForbidden {
		t.Errorf("그룹에서 빠진 사람의 토큰은 403: got %d", got)
	}
	h = newPermHandler(&fakePerms{err: errors.New("db down")}, nil)
	if got := do(h, "u", http.MethodGet, "/api/users", "").Code; got != http.StatusInternalServerError {
		t.Errorf("권한 조회 실패는 500: got %d", got)
	}
}

// TestRouteWiring checks the route→menu table (설계서 4.2) on the real router.
// Only denials and fake-backed routes are hit, so no nil repository is reached.
func TestRouteWiring(t *testing.T) {
	perms := &fakePerms{perms: map[string]*models.Permissions{
		"owner": ownerPerms, "grand": grandPerms, "ledgerView": ledgerView,
	}}
	h := newPermHandler(perms, []models.User{{ID: "owner", CoupleID: "c1", IsOwner: true}, {ID: "grand", CoupleID: "c1"}})

	denied := []struct{ user, method, path string }{
		{"grand", http.MethodGet, "/api/transactions"},
		{"grand", http.MethodGet, "/api/summary"},
		{"grand", http.MethodGet, "/api/categories"},
		{"grand", http.MethodGet, "/api/dividends"},
		{"grand", http.MethodGet, "/api/fixed-expenses"},
		{"grand", http.MethodPut, "/api/couple"},
		{"grand", http.MethodGet, "/api/stocks"},
		{"grand", http.MethodGet, "/api/assets"},
		{"grand", http.MethodGet, "/api/roadmap/projection"},
		{"grand", http.MethodGet, "/api/schedules"},
		{"grand", http.MethodGet, "/api/diaries"},
		{"ledgerView", http.MethodPost, "/api/transactions"},
		{"ledgerView", http.MethodPost, "/api/stocks"},
		{"ledgerView", http.MethodGet, "/api/roadmap/projection"},
		{"grand", http.MethodPost, "/api/auth/invite"},
	}
	for _, c := range denied {
		if got := do(h, c.user, c.method, c.path, "{}").Code; got != http.StatusForbidden {
			t.Errorf("%s %s %s: 403 이어야 한다, got %d", c.user, c.method, c.path, got)
		}
	}

	if got := do(h, "grand", http.MethodGet, "/api/users", "").Code; got != http.StatusOK {
		t.Errorf("구성원 목록은 누구나: got %d", got)
	}
	rec := do(h, "grand", http.MethodGet, "/api/me/permissions", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"baby_photo":"edit"`) || !strings.Contains(rec.Body.String(), `"ledger":"hidden"`) {
		t.Errorf("내 권한은 7개 메뉴 전부: got %d %s", rec.Code, rec.Body.String())
	}
}
