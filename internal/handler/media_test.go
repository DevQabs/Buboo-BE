package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yourname/couple-app/internal/models"
)

func TestCheckUpload(t *testing.T) {
	const limit = int64(9_500_000_000)
	cases := []struct {
		name       string
		req        uploadRequest
		used       int64
		wantStatus int
		wantExt    string
	}{
		{"일기 사진 통과", uploadRequest{"diary", "image/jpeg", 5 << 20, 0}, 0, 0, "jpg"},
		{"일기 HEIC 통과", uploadRequest{"diary", "image/heic", 5 << 20, 0}, 0, 0, "heic"},
		{"일기에 영상은 거부", uploadRequest{"diary", "video/mp4", 5 << 20, 10}, 0, http.StatusBadRequest, ""},
		{"모르는 목적 거부", uploadRequest{"x", "image/jpeg", 1, 0}, 0, http.StatusBadRequest, ""},
		{"모르는 형식 거부", uploadRequest{"baby", "image/gif", 1, 0}, 0, http.StatusBadRequest, ""},
		{"0바이트 거부", uploadRequest{"baby", "image/png", 0, 0}, 0, http.StatusBadRequest, ""},
		{"100MB 정확히 통과", uploadRequest{"baby", "video/mp4", 100 << 20, 10}, 0, 0, "mp4"},
		{"100MB 초과 거부", uploadRequest{"baby", "video/mp4", 100<<20 + 1, 10}, 0, http.StatusBadRequest, ""},
		{"영상 30.5초 통과", uploadRequest{"baby", "video/quicktime", 1 << 20, 30.5}, 0, 0, "mov"},
		{"영상 30.6초 거부", uploadRequest{"baby", "video/quicktime", 1 << 20, 30.6}, 0, http.StatusBadRequest, ""},
		{"영상 길이 없음 거부", uploadRequest{"baby", "video/mp4", 1 << 20, 0}, 0, http.StatusBadRequest, ""},
		{"상한 딱 맞으면 통과", uploadRequest{"baby", "image/jpeg", 10, 0}, limit - 10, 0, "jpg"},
		{"상한 넘으면 413", uploadRequest{"baby", "image/jpeg", 11, 0}, limit - 10, http.StatusRequestEntityTooLarge, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ext, status, err := checkUpload(c.req, c.used, limit)
			if status != c.wantStatus || ext != c.wantExt || (err == nil) != (c.wantStatus == 0) {
				t.Fatalf("got ext=%q status=%d err=%v, want ext=%q status=%d", ext, status, err, c.wantExt, c.wantStatus)
			}
		})
	}
	_, _, err := checkUpload(uploadRequest{"baby", "image/jpeg", 11, 0}, limit-10, limit)
	if err.Error() != "저장 공간 한도에 도달했습니다. 관리자에게 문의하세요" {
		t.Errorf("상한 문구: %q", err)
	}
}

func TestObjectKeyAndPrefix(t *testing.T) {
	k := objectKey("c1", "baby", "mp4")
	if !strings.HasPrefix(k, "c1/baby/") || !strings.HasSuffix(k, ".mp4") {
		t.Fatalf("키 형식 {couple}/{purpose}/{uuid}.{ext}: %s", k)
	}
	if !keyBelongs("c1", "baby", k) {
		t.Errorf("자기 그룹 키는 통과: %s", k)
	}
	cases := []struct {
		couple, purpose, key string
		want                 bool
	}{
		{"c2", "baby", k, false},
		{"c1", "diary", k, false},
		{"c1", "baby", "c1/baby/../c2/baby/a.jpg", false},
		{"c1", "baby", "c1/baby/x/a.jpg", false},
		{"c1", "baby", "c1/babyx/a.jpg", false},
		{"c1", "baby", "c10/baby/a.jpg", false},
		{"c1", "baby", "c1/baby/", false},
		{"", "baby", "/baby/a.jpg", false},
	}
	for _, c := range cases {
		if got := keyBelongs(c.couple, c.purpose, c.key); got != c.want {
			t.Errorf("keyBelongs(%q, %q, %q) = %v, want %v", c.couple, c.purpose, c.key, got, c.want)
		}
	}
}

func TestContentTypeOfKey(t *testing.T) {
	if got := contentTypeOfKey("baby", "c1/baby/a.mov"); got != "video/quicktime" {
		t.Errorf("mov: %q", got)
	}
	if got := contentTypeOfKey("diary", "c1/diary/a.jpg"); got != "image/jpeg" {
		t.Errorf("jpg: %q", got)
	}
	if got := contentTypeOfKey("diary", "c1/diary/a.mp4"); got != "" {
		t.Errorf("일기에 mp4 는 없다: %q", got)
	}
}

func TestUploadRoutes(t *testing.T) {
	h := newPermHandler(&fakePerms{perms: map[string]*models.Permissions{"owner": ownerPerms, "grand": grandPerms}}, nil)
	diary := `{"purpose":"diary","content_type":"image/jpeg","size":10}`
	if got := do(h, "grand", http.MethodPost, "/api/media/upload-url", diary).Code; got != http.StatusForbidden {
		t.Errorf("일기 편집 권한이 없으면 일기 사진 주소를 못 받는다: got %d", got)
	}
	if got := do(h, "owner", http.MethodPost, "/api/media/upload-url", `{"purpose":"x"}`).Code; got != http.StatusBadRequest {
		t.Errorf("모르는 목적은 400: got %d", got)
	}
	baby := `{"purpose":"baby","content_type":"image/jpeg","size":10}`
	if got := do(h, "owner", http.MethodPost, "/api/media/upload-url", baby).Code; got != http.StatusServiceUnavailable {
		t.Errorf("저장소 미설정이면 503: got %d", got)
	}
	if got := do(h, "grand", http.MethodGet, "/api/storage/usage", "").Code; got != http.StatusForbidden {
		t.Errorf("사용량은 owner 전용: got %d", got)
	}
}
