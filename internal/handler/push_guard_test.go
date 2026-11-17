package handler

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/yourname/couple-app/internal/models"
)

func TestPublicDialControl(t *testing.T) {
	blocked := []string{"127.0.0.1:443", "[::1]:443", "10.1.2.3:443", "172.16.0.1:443", "192.168.0.1:443",
		"[fd00::1]:443", "169.254.169.254:80", "[fe80::1]:443", "0.0.0.0:443", "[::]:443",
		"224.0.0.1:443", "[ff02::1]:443", "100.64.0.1:443", "[::ffff:127.0.0.1]:443"}
	for _, a := range blocked {
		if err := publicDialControl("tcp", a, nil); !errors.Is(err, errBlockedAddr) {
			t.Errorf("%s 는 막아야 한다: %v", a, err)
		}
	}
	for _, a := range []string{"142.250.1.1:443", "[2607:f8b0::1]:443", "100.128.0.1:443"} {
		if err := publicDialControl("tcp", a, nil); err != nil {
			t.Errorf("%s 는 통과해야 한다: %v", a, err)
		}
	}
}

// 실제 발송기로 로컬 TLS 서버에 보내면 연결 단계에서 막힌다(DNS 재바인딩도 같은 지점에서 막힌다).
func TestWebPush_내부_주소로는_보내지_않는다(t *testing.T) {
	hit := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit = true }))
	defer srv.Close()

	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	sub := models.PushSubscription{Endpoint: srv.URL + "/x", Keys: models.PushKeys{
		P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(auth),
	}}
	_, err = NewWebPush(pub, priv, "https://example.com").Send(context.Background(), sub, []byte(`{}`), "normal")
	if !errors.Is(err, errBlockedAddr) || hit {
		t.Fatalf("내부 주소 발송은 막혀야 한다: err=%v hit=%v", err, hit)
	}
}

func TestPushSubscriptionAPI_길이_제한(t *testing.T) {
	h, _ := pushHandler(&fakeSender{})
	long := strings.Repeat("a", 257)
	for name, body := range map[string]string{
		"endpoint 1024자 초과": `{"endpoint":"https://x/` + strings.Repeat("a", 1020) + `","keys":{"p256dh":"p","auth":"a"}}`,
		"p256dh 256자 초과":    `{"endpoint":"https://x","keys":{"p256dh":"` + long + `","auth":"a"}}`,
		"auth 256자 초과":      `{"endpoint":"https://x","keys":{"p256dh":"p","auth":"` + long + `"}}`,
		"본문 8KiB 초과":        `{"endpoint":"https://x","keys":{"p256dh":"p","auth":"a"},"pad":"` + strings.Repeat("a", 9<<10) + `"}`,
	} {
		if rec := do(h, "u1", http.MethodPost, "/api/push/subscriptions", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, rec.Code)
		}
	}
	if rec := do(h, "u1", http.MethodDelete, "/api/push/subscriptions",
		`{"endpoint":"https://x","pad":"`+strings.Repeat("a", 9<<10)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("삭제 본문 8KiB 초과: %d, want 400", rec.Code)
	}
}

// ctxSender 는 받은 ctx 의 남은 시간을 적고, panicky 면 패닉한다.
type ctxSender struct {
	left    []time.Duration
	panicky bool
}

func (c *ctxSender) Send(ctx context.Context, _ models.PushSubscription, _ []byte, _ string) (int, error) {
	if c.panicky {
		panic("boom")
	}
	d, _ := ctx.Deadline()
	c.left = append(c.left, time.Until(d))
	return http.StatusCreated, nil
}

func TestNotify_기기마다_5초_제한(t *testing.T) {
	s := &ctxSender{}
	h := &Handler{userRepo: &fakeUsers{users: family}, permRepo: levelPerms{levels: familyLevels},
		pushRepo: &fakePushRepo{subs: familySubs()}, push: s}
	h.notifyBabyMedia(context.Background(), "c1", "mom", "m1", "사진")
	if len(s.left) != 2 {
		t.Fatalf("2곳에 보내야 한다: %v", s.left)
	}
	for _, l := range s.left {
		if l <= 0 || l > pushSendTimeout {
			t.Errorf("발송 ctx 남은 시간 %v, want 0 < x <= %v", l, pushSendTimeout)
		}
	}
}

func TestNotifyBabyMedia_패닉은_삼킨다(t *testing.T) {
	h := &Handler{userRepo: &fakeUsers{users: family}, permRepo: levelPerms{levels: familyLevels},
		pushRepo: &fakePushRepo{subs: familySubs()}, push: &ctxSender{panicky: true}}
	h.notifyBabyMedia(context.Background(), "c1", "mom", "m1", "사진") // 패닉이 새면 테스트 프로세스가 죽는다
}
