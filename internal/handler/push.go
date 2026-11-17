package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
)

// PushSender 는 구독 하나에 알림 하나를 보내고 푸시 서비스의 응답 코드를 돌려준다. 테스트는 가짜를 넣는다.
type PushSender interface {
	Send(ctx context.Context, sub models.PushSubscription, payload []byte, urgency string) (int, error)
}

type webPush struct{ public, private, subject string }

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
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if !strings.HasPrefix(s.Endpoint, "https://") || s.Keys.P256dh == "" || s.Keys.Auth == "" {
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
