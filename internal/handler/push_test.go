package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
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

// GetUser 는 fakeUsers(permissions_test.go)에 사진 알림이 쓰는 조회를 더한다.
func (f *fakeUsers) GetUser(_ context.Context, id string) (*models.User, error) {
	for i := range f.users {
		if f.users[i].ID == id {
			return &f.users[i], nil
		}
	}
	return nil, fmt.Errorf("user %s not found", id)
}

// levelPerms 는 ListByCouple 만 답한다(run 은 loadPermissions 를 거치지 않는다).
type levelPerms struct {
	repository.PermissionRepository
	levels map[string]map[string]string
}

func (f levelPerms) ListByCouple(context.Context, string) (map[string]map[string]string, error) {
	return f.levels, nil
}

var family = []models.User{
	{ID: "owner", CoupleID: "c1", Name: "아빠", IsOwner: true},
	{ID: "mom", CoupleID: "c1", Name: "엄마"},
	{ID: "grand", CoupleID: "c1", Name: "할머니"},
	{ID: "uncle", CoupleID: "c1", Name: "삼촌"},
}

var familyLevels = map[string]map[string]string{
	"mom":   {"schedule": "edit", "baby_photo": "edit"},
	"grand": {"baby_photo": "edit", "baby_growth": "edit"},
	"uncle": {"ledger": "view"},
}

func familySubs() map[string][]models.PushSubscription {
	out := map[string][]models.PushSubscription{}
	for _, u := range family {
		out[u.ID] = []models.PushSubscription{{Endpoint: "https://push/" + u.ID}}
	}
	return out
}

func TestCanReceive(t *testing.T) {
	if got := canReceive(family, familyLevels, "schedule", ""); !slices.Equal(got, []string{"owner", "mom"}) {
		t.Errorf("일정: owner(행 없이 전체)와 일정 권한자만: %v", got)
	}
	if got := canReceive(family, familyLevels, "baby_photo", "mom"); !slices.Equal(got, []string{"owner", "grand"}) {
		t.Errorf("아기 사진: 올린 사람 빼고 사진일기 권한자만: %v", got)
	}
}

func TestMediaWord(t *testing.T) {
	if got := mediaWord([]models.BabyMedia{{Kind: "video"}, {Kind: "image"}}); got != "사진" {
		t.Errorf("사진이 섞이면 사진: %q", got)
	}
	if got := mediaWord([]models.BabyMedia{{Kind: "video"}}); got != "영상" {
		t.Errorf("영상만이면 영상: %q", got)
	}
}

func runReq(h *Handler, secret string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/push/run", nil)
	req.Header.Set("X-Push-Cron-Secret", secret)
	rec := httptest.NewRecorder()
	h.NewRouter().ServeHTTP(rec, req)
	return rec
}

func TestRunPush(t *testing.T) {
	now := time.Now()
	repo := &fakePushRepo{subs: familySubs(), due: []models.DueReminder{
		{ID: "r1", ScheduleID: "s1", CoupleID: "c1", Title: "소아과", Kind: "m10", DueAt: now.Add(-time.Minute)},
		{ID: "r2", ScheduleID: "s2", CoupleID: "c1", Title: "옛 일정", Kind: "h1", DueAt: now.Add(-25 * time.Hour)},
	}}
	sender := &fakeSender{status: map[string]int{"https://push/mom": http.StatusGone}}
	h := &Handler{userRepo: &fakeUsers{users: family}, permRepo: levelPerms{levels: familyLevels},
		pushRepo: repo, push: sender, pushCronSecret: []byte("cron")}

	if rec := runReq(h, "wrong"); rec.Code != http.StatusUnauthorized || len(repo.due) != 2 {
		t.Fatalf("틀린 비밀값은 401 이고 아무것도 잡지 않는다: %d, 남은 %d", rec.Code, len(repo.due))
	}
	rec := runReq(h, "cron")
	var got map[string]int
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got["sent"] != 1 || got["skipped"] != 1 {
		t.Fatalf("응답: %d %s", rec.Code, rec.Body)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("owner·mom 기기 2곳에만 보낸다: %v", sender.sent)
	}
	for _, s := range sender.sent {
		if !strings.Contains(s, "소아과 · 10분 전") || !strings.Contains(s, `"title":"우리집 일정"`) ||
			!strings.Contains(s, "/dashboard?tab=life\\u0026menu=schedule") {
			t.Errorf("일정 알림 내용: %s", s)
		}
		if strings.Contains(s, "옛 일정") || strings.HasPrefix(s, "https://push/grand") || strings.HasPrefix(s, "https://push/uncle") {
			t.Errorf("보내면 안 되는 알림: %s", s)
		}
	}
	if !slices.Equal(repo.gone, []string{"https://push/mom"}) {
		t.Errorf("410 구독은 지운다: %v", repo.gone)
	}
}

func TestRunPush_VAPID_없으면_아무것도_안_한다(t *testing.T) {
	repo := &fakePushRepo{due: []models.DueReminder{{ID: "r1", CoupleID: "c1", Kind: "m10", DueAt: time.Now()}}}
	h := &Handler{pushRepo: repo, pushCronSecret: []byte("cron")}
	rec := runReq(h, "cron")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"sent":0`) || len(repo.due) != 1 {
		t.Fatalf("no-op: %d %s, 남은 %d", rec.Code, rec.Body, len(repo.due))
	}
}

func TestNotifyBabyMedia(t *testing.T) {
	repo := &fakePushRepo{subs: familySubs()}
	sender := &fakeSender{}
	h := &Handler{userRepo: &fakeUsers{users: family}, permRepo: levelPerms{levels: familyLevels}, pushRepo: repo, push: sender}

	h.notifyBabyMedia(context.Background(), "c1", "mom", "m1", "사진")
	if len(sender.sent) != 2 || !strings.HasPrefix(sender.sent[0], "https://push/owner ") || !strings.HasPrefix(sender.sent[1], "https://push/grand ") {
		t.Fatalf("올린 엄마 빼고 owner·할머니에게: %v", sender.sent)
	}
	if !strings.Contains(sender.sent[0], "엄마님이 아기 사진을 올렸어요") || !strings.Contains(sender.sent[0], `"title":"우리집"`) {
		t.Errorf("사진 알림 문구: %s", sender.sent[0])
	}
}
