package repository

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

// newScheduleCouple 은 겹치지 않는 couple_id 를 주고, 끝나면 그 일정을 지운다(push_reminders 는 CASCADE).
func newScheduleCouple(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := "c-test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM schedules WHERE couple_id = $1`, id)
	})
	return id
}

type remRow struct {
	Kind   string
	Due    time.Time
	Unsent bool
}

func reminderRows(t *testing.T, pool *pgxpool.Pool, scheduleID string) []remRow {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT kind, due_at, sent_at IS NULL FROM push_reminders WHERE schedule_id = $1 ORDER BY kind`, scheduleID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []remRow
	for rows.Next() {
		var r remRow
		if err := rows.Scan(&r.Kind, &r.Due, &r.Unsent); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestSchedule_저장하면_알림을_다시_계산한다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgScheduleRepository(pool)
	couple := newScheduleCouple(t, pool)

	d := time.Now().In(models.KST).AddDate(0, 0, 3)
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	nine := func(day int) time.Time { return time.Date(d.Year(), d.Month(), d.Day()+day, 9, 0, 0, 0, models.KST) }

	s, err := repo.Create(ctx, &models.Schedule{CoupleID: couple, UserID: "u1", Title: "소아과",
		StartDate: start, AllDay: true, Color: "indigo", Reminders: []string{"d0_9", "d1_9"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := reminderRows(t, pool, s.ID)
	if len(rows) != 2 || rows[0].Kind != "d0_9" || !rows[0].Due.Equal(nine(0)) || rows[1].Kind != "d1_9" || !rows[1].Due.Equal(nine(-1)) || !rows[0].Unsent || !rows[1].Unsent {
		t.Fatalf("새 일정 알림 2건(당일·하루 전 9시, 미발송): %+v", rows)
	}
	got, err := repo.GetByID(ctx, s.ID)
	if err != nil || !slices.Equal(got.Reminders, []string{"d0_9", "d1_9"}) {
		t.Fatalf("조회한 reminders: %v, %v", got, err)
	}

	// 보낸 하루 전 알림도 날짜를 옮기면 새 시각으로 다시 보낼 수 있게 된다
	if _, err := pool.Exec(ctx, `UPDATE push_reminders SET sent_at = now() WHERE schedule_id = $1 AND kind = 'd1_9'`, s.ID); err != nil {
		t.Fatal(err)
	}
	s.StartDate = start.AddDate(0, 0, 7)
	s.Reminders = []string{"d1_9"}
	if _, err := repo.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	rows = reminderRows(t, pool, s.ID)
	if len(rows) != 1 || rows[0].Kind != "d1_9" || !rows[0].Due.Equal(nine(6)) || !rows[0].Unsent {
		t.Fatalf("수정 후 하루 전 알림 1건(7일 뒤 기준, 미발송): %+v", rows)
	}

	// 지난 날짜 일정은 알림 행을 넣지 않는다
	past, err := repo.Create(ctx, &models.Schedule{CoupleID: couple, UserID: "u1", Title: "지난 일",
		StartDate: start.AddDate(0, 0, -10), AllDay: true, Color: "indigo", Reminders: []string{"d0_9"}})
	if err != nil {
		t.Fatal(err)
	}
	if rows := reminderRows(t, pool, past.ID); len(rows) != 0 {
		t.Errorf("지난 일정 알림은 없어야 한다: %+v", rows)
	}

	// 알림 없는 일정(nil)도 저장된다 — reminders 는 NOT NULL
	plain, err := repo.Create(ctx, &models.Schedule{CoupleID: couple, UserID: "u1", Title: "알림 없음",
		StartDate: start, AllDay: true, Color: "indigo"})
	if err != nil {
		t.Fatalf("reminders nil 저장 실패: %v", err)
	}
	if got, err := repo.GetByID(ctx, plain.ID); err != nil || len(got.Reminders) != 0 {
		t.Errorf("알림 없는 일정 조회: %v, %v", got, err)
	}

	// 일정을 지우면 알림도 지워진다(CASCADE)
	if err := repo.Delete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if rows := reminderRows(t, pool, s.ID); len(rows) != 0 {
		t.Errorf("삭제한 일정의 알림이 남았다: %+v", rows)
	}
}

// 시간 지정 일정: 알림은 실제 시각 기준, 00:30 KST(전날 UTC)도 KST 달로 묶인다
func TestSchedule_시간_지정_알림과_달_경계(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgScheduleRepository(pool)
	couple := newScheduleCouple(t, pool)

	y := time.Now().In(models.KST).Year() + 1
	start, _, err := models.ScheduleTimes(false, fmt.Sprintf("%d-03-01", y), "00:30", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := repo.Create(ctx, &models.Schedule{CoupleID: couple, UserID: "u1", Title: "새벽",
		StartDate: start, AllDay: false, Color: "indigo", Reminders: []string{"at", "h1"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := reminderRows(t, pool, s.ID)
	if len(rows) != 2 || rows[0].Kind != "at" || !rows[0].Due.Equal(time.Date(y, 3, 1, 0, 30, 0, 0, models.KST)) ||
		rows[1].Kind != "h1" || !rows[1].Due.Equal(time.Date(y, 2, 28, 23, 30, 0, 0, models.KST)) {
		t.Fatalf("시간 지정 알림: %+v", rows)
	}
	mar, err := repo.ListByMonth(ctx, couple, y, 3)
	if err != nil || len(mar) != 1 {
		t.Fatalf("3월 목록에 00:30 일정: %v %v", mar, err)
	}
	if feb, _ := repo.ListByMonth(ctx, couple, y, 2); len(feb) != 0 {
		t.Errorf("2월 목록에 들면 안 된다: %v", feb)
	}
}
