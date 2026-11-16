package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireInternalSecret(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := RequireInternalSecret([]byte("s3cret"))(ok)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"헤더 없음", "", http.StatusUnauthorized},
		{"값 다름", "wrong", http.StatusUnauthorized},
		{"값 일치", "s3cret", http.StatusNoContent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/callback", nil)
			if c.header != "" {
				req.Header.Set("X-Internal-Secret", c.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestRequireInternalSecret_EmptySecretRejectsAll(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := RequireInternalSecret(nil)(ok)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Internal-Secret", "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("빈 시크릿 설정이면 모두 거부해야 한다: got %d", rec.Code)
	}
}
