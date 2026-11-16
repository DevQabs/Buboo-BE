package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestClient(region string) *Client {
	return NewClient(Config{
		Endpoint:        "https://acct.r2.cloudflarestorage.com",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		Bucket:          "uri-jip-media",
		Region:          region,
	})
}

func TestPresignPut(t *testing.T) {
	raw, err := newTestClient("").PresignPut(context.Background(), "c1/baby/a.mp4", "video/mp4", 1234, 15*time.Minute)
	if err != nil {
		t.Fatalf("서명 실패: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("주소 해석 실패: %v", err)
	}
	if u.Host != "acct.r2.cloudflarestorage.com" || u.Path != "/uri-jip-media/c1/baby/a.mp4" {
		t.Errorf("경로 방식(버킷/키) 주소여야 한다: %s", raw)
	}
	q := u.Query()
	if q.Get("X-Amz-Expires") != "900" {
		t.Errorf("15분 유효여야 한다: X-Amz-Expires=%s", q.Get("X-Amz-Expires"))
	}
	if !strings.Contains(q.Get("X-Amz-Credential"), "/auto/s3/") {
		t.Errorf("리전 기본값은 auto 여야 한다: %s", q.Get("X-Amz-Credential"))
	}
	signed := q.Get("X-Amz-SignedHeaders")
	for _, h := range []string{"content-type", "content-length", "host"} {
		if !strings.Contains(signed, h) {
			t.Errorf("서명 헤더에 %s 가 있어야 한다: %s", h, signed)
		}
	}
	if strings.Contains(strings.ToLower(raw), "checksum") {
		t.Errorf("브라우저 PUT 이 맞출 수 없는 체크섬 파라미터가 붙으면 안 된다: %s", raw)
	}
}

func TestPresignGet(t *testing.T) {
	raw, err := newTestClient("ap-southeast-2").PresignGet(context.Background(), "c1/diary/a.jpg", time.Hour)
	if err != nil {
		t.Fatalf("서명 실패: %v", err)
	}
	u, _ := url.Parse(raw)
	if u.Query().Get("X-Amz-Expires") != "3600" {
		t.Errorf("1시간 유효여야 한다: %s", raw)
	}
	if !strings.Contains(u.Query().Get("X-Amz-Credential"), "/ap-southeast-2/s3/") {
		t.Errorf("설정한 리전을 써야 한다: %s", raw)
	}
}
