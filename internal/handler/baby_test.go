package handler

import (
	"net/http"
	"testing"

	"github.com/yourname/couple-app/internal/models"
)

func fp(v float64) *float64 { return &v }

func TestValidBaby(t *testing.T) {
	ok := models.Baby{Name: "튼튼이", BirthDate: "2026-10-05", Sex: "female"}
	if err := validBaby(ok); err != nil {
		t.Errorf("정상 프로필: %v", err)
	}
	for name, b := range map[string]models.Baby{
		"이름 없음":  {Name: " ", BirthDate: "2026-10-05", Sex: "male"},
		"날짜 형식":  {Name: "a", BirthDate: "2026.10.05", Sex: "male"},
		"성별 값 밖": {Name: "a", BirthDate: "2026-10-05", Sex: "boy"},
	} {
		if validBaby(b) == nil {
			t.Errorf("%s 는 거부해야 한다", name)
		}
	}
}

func TestValidGrowth(t *testing.T) {
	if err := validGrowth(models.BabyGrowth{MeasuredOn: "2026-11-16", WeightKg: fp(4.8)}); err != nil {
		t.Errorf("몸무게만 있어도 통과: %v", err)
	}
	for name, g := range map[string]models.BabyGrowth{
		"측정값 없음": {MeasuredOn: "2026-11-16"},
		"날짜 형식":  {MeasuredOn: "11/16", HeightCm: fp(56.2)},
		"0 이하":   {MeasuredOn: "2026-11-16", HeightCm: fp(0)},
		"자릿수 초과": {MeasuredOn: "2026-11-16", WeightKg: fp(1000)},
	} {
		if validGrowth(g) == nil {
			t.Errorf("%s 는 거부해야 한다", name)
		}
	}
}

// TestBabyRouteWiring checks 설계서 4.2 for /baby on the real router (denials only).
func TestBabyRouteWiring(t *testing.T) {
	photoView := &models.Permissions{Menus: map[string]string{"baby_photo": "view"}}
	h := newPermHandler(&fakePerms{perms: map[string]*models.Permissions{
		"grand": grandPerms, "ledgerView": ledgerView, "photoView": photoView,
	}}, nil)
	denied := []struct{ user, method, path, body string }{
		{"ledgerView", http.MethodGet, "/api/baby", ""},
		{"grand", http.MethodPut, "/api/baby", `{"name":"a","birth_date":"2026-10-05","sex":"female"}`},
		{"ledgerView", http.MethodGet, "/api/baby/moments", ""},
		{"photoView", http.MethodPost, "/api/baby/moments", `{}`},
		{"photoView", http.MethodPut, "/api/baby/moments/m1", `{}`},
		{"photoView", http.MethodDelete, "/api/baby/moments/m1", ""},
		{"photoView", http.MethodDelete, "/api/baby/media/x1", ""},
		{"photoView", http.MethodGet, "/api/baby/growth", ""},
		{"photoView", http.MethodPost, "/api/baby/growth", `{}`},
		{"photoView", http.MethodPost, "/api/media/upload-url", `{"purpose":"baby","content_type":"image/jpeg","size":1}`},
	}
	for _, c := range denied {
		if got := do(h, c.user, c.method, c.path, c.body).Code; got != http.StatusForbidden {
			t.Errorf("%s %s %s: 403 이어야 한다, got %d", c.user, c.method, c.path, got)
		}
	}
}
