package models

import "time"

// PushKeys / PushSubscription 은 브라우저 PushSubscription.toJSON() 모양 그대로다(push 설계서 4절).
type PushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type PushSubscription struct {
	Endpoint string   `json:"endpoint"`
	Keys     PushKeys `json:"keys"`
}

// DueReminder 는 run 이 잡은 일정 알림 하나와 보낼 때 필요한 일정 정보.
type DueReminder struct {
	ID         string
	ScheduleID string
	CoupleID   string
	Title      string
	Kind       string
	DueAt      time.Time
}
