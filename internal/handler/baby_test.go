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
	if err := validGrowth(&models.BabyGrowth{MeasuredOn: "2026-11-16", WeightKg: fp(4.8)}); err != nil {
		t.Errorf("몸무게만 있어도 통과: %v", err)
	}
	for name, g := range map[string]models.BabyGrowth{
		"측정값 없음":       {MeasuredOn: "2026-11-16"},
		"날짜 형식":        {MeasuredOn: "11/16", HeightCm: fp(56.2)},
		"0 이하":         {MeasuredOn: "2026-11-16", HeightCm: fp(0)},
		"자릿수 초과":       {MeasuredOn: "2026-11-16", WeightKg: fp(1000)},
		"반올림하면 1000":   {MeasuredOn: "2026-11-16", WeightKg: fp(999.996)},
		"반올림하면 0":      {MeasuredOn: "2026-11-16", HeightCm: fp(0.04)},
		"키 반올림하면 1000": {MeasuredOn: "2026-11-16", HeightCm: fp(999.96)},
	} {
		if validGrowth(&g) == nil {
			t.Errorf("%s 는 거부해야 한다", name)
		}
	}
}

// 저장 전에 열 자릿수(NUMERIC(5,1)·(5,2))로 반올림한다.
func TestValidGrowth_반올림(t *testing.T) {
	g := models.BabyGrowth{MeasuredOn: "2026-11-16", HeightCm: fp(56.25), WeightKg: fp(4.567), HeadCm: fp(38.04)}
	if err := validGrowth(&g); err != nil {
		t.Fatal(err)
	}
	if *g.HeightCm != 56.3 || *g.WeightKg != 4.57 || *g.HeadCm != 38.0 {
		t.Errorf("반올림 결과 = %v, %v, %v; want 56.3, 4.57, 38", *g.HeightCm, *g.WeightKg, *g.HeadCm)
	}
	w := models.BabyGrowth{MeasuredOn: "2026-11-16", WeightKg: fp(999.994)}
	if err := validGrowth(&w); err != nil || *w.WeightKg != 999.99 {
		t.Errorf("999.994 → 999.99 통과해야 한다: %v, %v", *w.WeightKg, err)
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
