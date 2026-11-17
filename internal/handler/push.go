package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
)

// PushSender 는 구독 하나에 알림 하나를 보내고 푸시 서비스의 응답 코드를 돌려준다. 테스트는 가짜를 넣는다.
type PushSender interface {
	Send(ctx context.Context, sub models.PushSubscription, payload []byte, urgency string) (int, error)
}

type webPush struct{ public, private, subject string }

var (
	errBlockedAddr = errors.New("push: 내부 주소로는 보내지 않습니다")
	cgnat          = netip.MustParsePrefix("100.64.0.0/10")
)

// publicDialControl 은 연결 직전 실제 IP 를 보고 내부망·루프백·링크로컬·멀티캐스트·CGNAT 를 막는다.
// endpoint 는 사용자가 보낸 값이라(SSRF) 호스트 이름이 아니라 연결 IP 로 판단한다 — DNS 재바인딩도 여기서 막힌다.
// 푸시 서비스 호스트는 브라우저마다 달라 허용 목록을 두지 않는다.
func publicDialControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	ip := ap.Addr().Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() ||
		ip.IsMulticast() || cgnat.Contains(ip) {
		return errBlockedAddr
	}
	return nil
}

// pushClient: 5초 제한, 리다이렉트 따라가지 않음, 환경 프록시 없음(Proxy nil — 프록시를 거치면 위 검사가 무의미하다).
var pushClient = &http.Client{
	Timeout:       5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, Control: publicDialControl}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
	},
}

// NewWebPush 는 VAPID 값이 하나라도 비면 nil 을 준다 — 푸시만 꺼지고 서버는 뜬다(push 설계서 6절).
func NewWebPush(public, private, subject string) PushSender {
	if public == "" || private == "" || subject == "" {
		return nil
	}
	return webPush{public: public, private: private, subject: subject}
}

func (p webPush) Send(ctx context.Context, sub models.PushSubscription, payload []byte, urgency string) (int, error) {
	res, err := webpush.SendNotificationWithContext(ctx, payload,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth}},
		&webpush.Options{
			Subscriber:      p.subject,
			VAPIDPublicKey:  p.public,
			VAPIDPrivateKey: p.private,
			TTL:             24 * 60 * 60,
			Urgency:         webpush.Urgency(urgency),
			HTTPClient:      pushClient,
		})
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

var errPushOff = errors.New("알림 기능이 설정되지 않았습니다")

// GET /api/push/public-key — FE 는 환경변수 없이 이 값으로 구독한다.
func (h *Handler) pushPublicKey(w http.ResponseWriter, _ *http.Request) {
	if h.push == nil {
		respondError(w, http.StatusServiceUnavailable, errPushOff)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"public_key": h.vapidPublic})
}

// POST /api/push/subscriptions — endpoint 기준 upsert. 서버가 그 주소로 보내므로 https 만 받는다.
func (h *Handler) savePushSubscription(w http.ResponseWriter, r *http.Request) {
	if h.push == nil {
		respondError(w, http.StatusServiceUnavailable, errPushOff)
		return
	}
	var s models.PushSubscription
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if !strings.HasPrefix(s.Endpoint, "https://") || len(s.Endpoint) > 1024 ||
		s.Keys.P256dh == "" || s.Keys.Auth == "" || len(s.Keys.P256dh) > 256 || len(s.Keys.Auth) > 256 {
		respondError(w, http.StatusBadRequest, errors.New("구독 정보가 올바르지 않습니다"))
		return
	}
	ctx := r.Context()
	if err := h.pushRepo.SaveSubscription(ctx, auth.UserIDFromCtx(ctx), r.UserAgent(), s); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/push/subscriptions — 본인 구독만 지운다.
func (h *Handler) deletePushSubscription(w http.ResponseWriter, r *http.Request) {
	if h.push == nil {
		respondError(w, http.StatusServiceUnavailable, errPushOff)
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	if err := h.pushRepo.DeleteSubscription(ctx, auth.UserIDFromCtx(ctx), body.Endpoint); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pushMessage 는 서비스 워커(public/sw.js)가 읽는 알림 내용.
type pushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// canReceive 는 menu 를 볼 수 있는 구성원 ID 를 users 순서대로 준다(except 제외).
// 판정은 requireMenu 와 같은 Permissions.Allows(읽기).
func canReceive(users []models.User, levels map[string]map[string]string, menu, except string) []string {
	var out []string
	for _, u := range users {
		if u.ID == except {
			continue
		}
		if (models.Permissions{IsOwner: u.IsOwner, Menus: levels[u.ID]}).Allows(false, menu) {
			out = append(out, u.ID)
		}
	}
	return out
}

// notify 는 couple 에서 menu 를 볼 수 있는 구성원(except 제외)의 모든 기기에 보낸다.
// 404·410 은 끊긴 구독이라 지운다. 실패는 로그만 남긴다(한 번 놓치면 끝, push 설계서 4절).
func (h *Handler) notify(ctx context.Context, coupleID, menu, except string, msg pushMessage, urgency string) {
	users, err := h.userRepo.ListUsers(ctx, coupleID)
	if err != nil {
		log.Printf("push: 구성원 조회 실패: %v", err)
		return
	}
	levels, err := h.permRepo.ListByCouple(ctx, coupleID)
	if err != nil {
		log.Printf("push: 권한 조회 실패: %v", err)
		return
	}
	ids := canReceive(users, levels, menu, except)
	if len(ids) == 0 {
		return
	}
	subs, err := h.pushRepo.ListSubscriptions(ctx, ids)
	if err != nil {
		log.Printf("push: 구독 조회 실패: %v", err)
		return
	}
	payload, _ := json.Marshal(msg)
	for _, s := range subs {
		status, err := h.push.Send(ctx, s, payload, urgency)
		switch {
		case err != nil:
			log.Printf("push: 발송 실패: %v", err)
		case status == http.StatusNotFound || status == http.StatusGone:
			if err := h.pushRepo.DeleteEndpoint(ctx, s.Endpoint); err != nil {
				log.Printf("push: 끊긴 구독 삭제 실패: %v", err)
			}
		case status >= 300:
			log.Printf("push: 푸시 서비스 응답 %d", status)
		}
	}
}

// 서버가 오래 잠들어 있다 깨어나도 묵은 알림을 한꺼번에 보내지 않는다(push 설계서 4절).
const pushStaleAfter = 24 * time.Hour

// POST /internal/push/run — Supabase Cron 이 보낼 알림이 있을 때만 1분마다 부른다.
func (h *Handler) runPush(w http.ResponseWriter, r *http.Request) {
	if h.push == nil {
		respondJSON(w, http.StatusOK, map[string]int{"sent": 0, "skipped": 0})
		return
	}
	// pg_net 이 5초에 연결을 끊어도 이미 잡은(sent_at 을 찍은) 알림은 끝까지 보낸다
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	now := time.Now()
	due, err := h.pushRepo.ClaimDue(ctx, now, 100)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	sent, skipped := 0, 0
	for _, d := range due {
		if now.Sub(d.DueAt) > pushStaleAfter {
			skipped++
			continue
		}
		h.notify(ctx, d.CoupleID, "schedule", "", pushMessage{
			Title: "우리집 일정",
			Body:  d.Title + " · " + models.ReminderLabel(d.Kind),
			URL:   "/dashboard?tab=life&menu=schedule",
			Tag:   d.ScheduleID + "-" + d.Kind,
		}, "high")
		sent++
	}
	respondJSON(w, http.StatusOK, map[string]int{"sent": sent, "skipped": skipped})
}

// mediaWord: 사진이 하나라도 있으면 "사진", 영상뿐이면 "영상".
func mediaWord(media []models.BabyMedia) string {
	for _, m := range media {
		if m.Kind == "image" {
			return "사진"
		}
	}
	return "영상"
}

// notifyBabyMedia 는 새 사진·영상을 아기 사진일기를 볼 수 있는 다른 구성원에게 알린다. 요청 한 번에 1건.
// 요청 흐름을 막지 않게 고루틴으로 부른다 — 서버가 꺼지며 끊겨도 괜찮다(push 설계서 5절).
func (h *Handler) notifyBabyMedia(ctx context.Context, coupleID, uploaderID, momentID, what string) {
	if h.push == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	name := "가족"
	if u, err := h.userRepo.GetUser(ctx, uploaderID); err == nil {
		name = u.Name
	}
	h.notify(ctx, coupleID, "baby_photo", uploaderID, pushMessage{
		Title: "우리집",
		Body:  name + "님이 아기 " + what + "을 올렸어요",
		URL:   "/dashboard?tab=baby&menu=baby_photo",
		Tag:   "baby-" + momentID,
	}, "normal")
}
