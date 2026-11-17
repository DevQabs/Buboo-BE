package handler

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

type fakeSchedules struct {
	repository.ScheduleRepository
	saved *models.Schedule
}

func (f *fakeSchedules) Create(_ context.Context, s *models.Schedule) (*models.Schedule, error) {
	f.saved = s
	return s, nil
}

func (f *fakeSchedules) GetByID(_ context.Context, id string) (*models.Schedule, error) {
	return &models.Schedule{ID: id, AllDay: true, Reminders: []string{"d1_9"}}, nil
}

func (f *fakeSchedules) Update(_ context.Context, s *models.Schedule) (*models.Schedule, error) {
	f.saved = s
	return s, nil
}

func TestScheduleReminders_검사(t *testing.T) {
	sch := &fakeSchedules{}
	h := newPermHandler(&fakePerms{perms: map[string]*models.Permissions{"u1": ownerPerms}}, nil)
	h.scheduleRepo = sch
	h.owner = &fakeOwner{owned: map[string]string{"s1": "c1"}}

	if rec := do(h, "u1", http.MethodPost, "/api/schedules",
		`{"title":"a","start_date":"2026-11-20","all_day":true,"reminders":["m10"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("하루 종일 일정에 10분 전: %d, want 400", rec.Code)
	}
	rec := do(h, "u1", http.MethodPost, "/api/schedules",
		`{"title":"a","start_date":"2026-11-20","all_day":true,"reminders":["d0_9","d1_9"]}`)
	if rec.Code != http.StatusCreated || !slices.Equal(sch.saved.Reminders, []string{"d0_9", "d1_9"}) {
		t.Fatalf("정상 알림 저장: %d %v", rec.Code, sch.saved)
	}
	if rec := do(h, "u1", http.MethodPut, "/api/schedules/s1", `{"reminders":["h1"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("기존 하루 종일 일정에 1시간 전: %d, want 400", rec.Code)
	}
	if rec := do(h, "u1", http.MethodPut, "/api/schedules/s1", `{"reminders":[]}`); rec.Code != http.StatusOK || len(sch.saved.Reminders) != 0 {
		t.Errorf("알림 모두 끄기: %d %v", rec.Code, sch.saved.Reminders)
	}
	if rec := do(h, "u1", http.MethodPut, "/api/schedules/s1", `{"title":"b"}`); rec.Code != http.StatusOK || !slices.Equal(sch.saved.Reminders, []string{"d1_9"}) {
		t.Errorf("reminders 없이 수정하면 기존 값 유지: %d %v", rec.Code, sch.saved.Reminders)
	}
}
