package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/models"
)

func TestPush_구독은_endpoint_로_덮어쓰고_본인것만_지운다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgPushRepository(pool)
	ep := "https://push.test/" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM push_subscriptions WHERE endpoint = $1`, ep)
	})
	u1, u2 := "u-"+uuid.NewString(), "u-"+uuid.NewString()

	sub := models.PushSubscription{Endpoint: ep, Keys: models.PushKeys{P256dh: "p", Auth: "a"}}
	if err := repo.SaveSubscription(ctx, u1, "ua", sub); err != nil {
		t.Fatal(err)
	}
	sub.Keys.Auth = "a2"
	if err := repo.SaveSubscription(ctx, u2, "ua", sub); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListSubscriptions(ctx, []string{u1}); len(got) != 0 {
		t.Errorf("같은 기기로 다른 사람이 로그인하면 앞 사람 구독은 없다: %+v", got)
	}
	got, err := repo.ListSubscriptions(ctx, []string{u2})
	if err != nil || len(got) != 1 || got[0].Keys.Auth != "a2" {
		t.Fatalf("덮어쓴 구독: %+v, %v", got, err)
	}

	if err := repo.DeleteSubscription(ctx, u1, ep); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListSubscriptions(ctx, []string{u2}); len(got) != 1 {
		t.Errorf("남의 구독은 지워지지 않는다: %+v", got)
	}
	if err := repo.DeleteSubscription(ctx, u2, ep); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListSubscriptions(ctx, []string{u2}); len(got) != 0 {
		t.Errorf("본인 구독 삭제: %+v", got)
	}
}

func TestPush_ClaimDue_는_한번만_잡는다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgPushRepository(pool)
	couple := "c-test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM schedules WHERE couple_id = $1`, couple)
	})
	sid := "sch-" + uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO schedules (id, couple_id, user_id, title, start_date) VALUES ($1, $2, 'u1', '소아과', now())`,
		sid, couple); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dueID, laterID := "rem-"+uuid.NewString(), "rem-"+uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO push_reminders (id, schedule_id, due_at, kind) VALUES ($1, $2, $3, 'm10'), ($4, $2, $5, 'h1')`,
		dueID, sid, now.Add(-time.Minute), laterID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	find := func(list []models.DueReminder, id string) *models.DueReminder {
		for i := range list {
			if list[i].ID == id {
				return &list[i]
			}
		}
		return nil
	}
	first, err := repo.ClaimDue(ctx, now, 100)
	if err != nil {
		t.Fatal(err)
	}
	d := find(first, dueID)
	if d == nil || d.Title != "소아과" || d.CoupleID != couple || d.Kind != "m10" || d.ScheduleID != sid {
		t.Fatalf("지난 알림을 일정 정보와 함께 잡아야 한다: %+v", d)
	}
	if find(first, laterID) != nil {
		t.Errorf("아직 안 된 알림을 잡았다")
	}
	again, err := repo.ClaimDue(ctx, now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if find(again, dueID) != nil {
		t.Errorf("한 번 잡은 알림을 다시 잡았다(재발송)")
	}
}
