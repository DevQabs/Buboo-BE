package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

// fakePushRepo 는 PushRepository 를 메모리로 흉내 낸다.
type fakePushRepo struct {
	savedBy string
	saved   models.PushSubscription
	deleted [2]string // userID, endpoint
	gone    []string  // DeleteEndpoint 로 지운 endpoint
	subs    map[string][]models.PushSubscription
	due     []models.DueReminder
}

func (f *fakePushRepo) SaveSubscription(_ context.Context, userID, _ string, s models.PushSubscription) error {
	f.savedBy, f.saved = userID, s
	return nil
}

func (f *fakePushRepo) DeleteSubscription(_ context.Context, userID, endpoint string) error {
	f.deleted = [2]string{userID, endpoint}
	return nil
}

func (f *fakePushRepo) DeleteEndpoint(_ context.Context, endpoint string) error {
	f.gone = append(f.gone, endpoint)
	return nil
}

func (f *fakePushRepo) ListSubscriptions(_ context.Context, userIDs []string) ([]models.PushSubscription, error) {
	var out []models.PushSubscription
	for _, id := range userIDs {
		out = append(out, f.subs[id]...)
	}
	return out, nil
}

func (f *fakePushRepo) ClaimDue(context.Context, time.Time, int) ([]models.DueReminder, error) {
	d := f.due
	f.due = nil
	return d, nil
}

// fakeSender 는 보낸 것을 "endpoint payload" 로 적고, status 에 없는 endpoint 는 201 을 준다.
type fakeSender struct {
	status map[string]int
	sent   []string
}

func (f *fakeSender) Send(_ context.Context, s models.PushSubscription, payload []byte, _ string) (int, error) {
	f.sent = append(f.sent, s.Endpoint+" "+string(payload))
	if c, ok := f.status[s.Endpoint]; ok {
		return c, nil
	}
	return http.StatusCreated, nil
}

// pushHandler: 아기 탭만 보는 u1(grandPerms)로 /api 를 부르는 핸들러.
func pushHandler(push PushSender) (*Handler, *fakePushRepo) {
	repo := &fakePushRepo{}
	h := newPermHandler(&fakePerms{perms: map[string]*models.Permissions{"u1": grandPerms}}, nil)
	h.pushRepo, h.push, h.vapidPublic = repo, push, "PUB"
	return h, repo
}

func TestPushSubscriptionAPI(t *testing.T) {
	h, repo := pushHandler(&fakeSender{})

	rec := do(h, "u1", http.MethodGet, "/api/push/public-key", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"public_key":"PUB"`) {
		t.Fatalf("공개키: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "u1", http.MethodPost, "/api/push/subscriptions",
		`{"endpoint":"http://evil.test/x","keys":{"p256dh":"p","auth":"a"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("https 아닌 endpoint: %d, want 400", rec.Code)
	}
	if rec := do(h, "u1", http.MethodPost, "/api/push/subscriptions",
		`{"endpoint":"https://fcm.googleapis.com/x","keys":{"p256dh":"p"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("auth 키 없음: %d, want 400", rec.Code)
	}
	rec = do(h, "u1", http.MethodPost, "/api/push/subscriptions",
		`{"endpoint":"https://fcm.googleapis.com/x","expirationTime":null,"keys":{"p256dh":"p","auth":"a"}}`)
	if rec.Code != http.StatusNoContent || repo.savedBy != "u1" || repo.saved.Keys.Auth != "a" {
		t.Fatalf("구독 저장: %d %+v by %q", rec.Code, repo.saved, repo.savedBy)
	}
	rec = do(h, "u1", http.MethodDelete, "/api/push/subscriptions", `{"endpoint":"https://fcm.googleapis.com/x"}`)
	if rec.Code != http.StatusNoContent || repo.deleted != [2]string{"u1", "https://fcm.googleapis.com/x"} {
		t.Fatalf("구독 삭제: %d %v", rec.Code, repo.deleted)
	}
}

func TestPushSubscriptionAPI_VAPID_없으면_503(t *testing.T) {
	h, _ := pushHandler(nil)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/push/public-key", ""},
		{http.MethodPost, "/api/push/subscriptions", `{"endpoint":"https://x","keys":{"p256dh":"p","auth":"a"}}`},
		{http.MethodDelete, "/api/push/subscriptions", `{"endpoint":"https://x"}`},
	} {
		if rec := do(h, "u1", c.method, c.path, c.body); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: %d, want 503", c.method, c.path, rec.Code)
		}
	}
}

func TestNewWebPush_하나라도_비면_nil(t *testing.T) {
	if NewWebPush("pub", "", "https://x") != nil {
		t.Error("개인키가 없으면 nil")
	}
	if NewWebPush("pub", "priv", "https://x") == nil {
		t.Error("셋 다 있으면 발송기")
	}
}
