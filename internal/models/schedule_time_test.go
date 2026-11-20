package models

import (
	"errors"
	"testing"
	"time"
)

func TestScheduleTimes(t *testing.T) {
	utc := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name                            string
		allDay                          bool
		date, startTime, endDate, endTm string
		start                           time.Time
		end                             *time.Time // nil = 끝 없음
		err                             error      // nil 이고 wantErr 면 파싱 오류
		wantErr                         bool
	}{
		{"하루 종일은 UTC 자정, 시각 무시", true, "2026-11-20", "14:00", "", "", utc(2026, 11, 20), nil, nil, false},
		{"하루 종일 끝 날짜", true, "2026-11-20", "", "2026-11-22", "", utc(2026, 11, 20), ptr(utc(2026, 11, 22)), nil, false},
		{"00:30 KST 는 전날 15:30 UTC", false, "2026-11-20", "00:30", "", "", time.Date(2026, 11, 19, 15, 30, 0, 0, time.UTC), nil, nil, false},
		{"23:50 KST 는 같은 날 14:50 UTC", false, "2026-11-20", "23:50", "", "", time.Date(2026, 11, 20, 14, 50, 0, 0, time.UTC), nil, nil, false},
		{"끝 시각만 주면 같은 날", false, "2026-11-20", "14:00", "", "15:30", kst(2026, 11, 20, 14, 0), ptr(kst(2026, 11, 20, 15, 30)), nil, false},
		{"끝 날짜+시각", false, "2026-11-20", "23:00", "2026-11-21", "01:00", kst(2026, 11, 20, 23, 0), ptr(kst(2026, 11, 21, 1, 0)), nil, false},
		{"시간 지정인데 시작 시각 없음", false, "2026-11-20", "", "", "", time.Time{}, nil, ErrStartTimeRequired, true},
		{"시간 지정 끝 날짜만", false, "2026-11-20", "10:00", "2026-11-21", "", time.Time{}, nil, ErrEndWithoutTime, true},
		{"끝이 시작보다 빠름", false, "2026-11-20", "10:00", "", "09:00", time.Time{}, nil, ErrEndBeforeStart, true},
		{"잘못된 시각", false, "2026-11-20", "25:00", "", "", time.Time{}, nil, nil, true},
		{"잘못된 날짜", true, "2026-13-01", "", "", "", time.Time{}, nil, nil, true},
	}
	for _, c := range cases {
		start, end, err := ScheduleTimes(c.allDay, c.date, c.startTime, c.endDate, c.endTm)
		if c.wantErr {
			if err == nil || (c.err != nil && !errors.Is(err, c.err)) {
				t.Errorf("%s: err = %v, want %v", c.name, err, c.err)
			}
			continue
		}
		if err != nil || !start.Equal(c.start) || (end == nil) != (c.end == nil) || (end != nil && !end.Equal(*c.end)) {
			t.Errorf("%s: %v %v %v, want %v %v", c.name, start, end, err, c.start, c.end)
		}
	}
}

func ptr(t time.Time) *time.Time { return &t }

func TestInKST(t *testing.T) {
	timed := Schedule{StartDate: time.Date(2026, 11, 19, 15, 30, 0, 0, time.UTC), EndDate: ptr(time.Date(2026, 11, 19, 16, 0, 0, 0, time.UTC))}
	timed.InKST()
	if got := timed.StartDate.Format(time.RFC3339); got != "2026-11-20T00:30:00+09:00" || timed.StartTime != "00:30" || timed.EndTime != "01:00" {
		t.Errorf("시간 지정 00:30: %s %q %q", got, timed.StartTime, timed.EndTime)
	}
	allDay := Schedule{AllDay: true, StartDate: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}
	allDay.InKST()
	if got := allDay.StartDate.Format(time.DateOnly); got != "2026-11-20" || allDay.StartTime != "" {
		t.Errorf("하루 종일은 같은 날짜, 시각 없음: %s %q", got, allDay.StartTime)
	}
}

// 시간 지정 일정의 알림은 저장되는 실제 시각 기준이다(00:30·23:50 경계)
func TestScheduleTimes_알림_시각(t *testing.T) {
	early := kst(2026, 11, 1, 0, 0)
	for _, c := range []struct {
		hm   string
		want []Reminder
	}{
		{"00:30", []Reminder{{"at", kst(2026, 11, 20, 0, 30)}, {"m10", kst(2026, 11, 20, 0, 20)}, {"h1", kst(2026, 11, 19, 23, 30)}, {"d1_9", kst(2026, 11, 19, 9, 0)}}},
		{"23:50", []Reminder{{"at", kst(2026, 11, 20, 23, 50)}, {"m10", kst(2026, 11, 20, 23, 40)}, {"h1", kst(2026, 11, 20, 22, 50)}, {"d1_9", kst(2026, 11, 19, 9, 0)}}},
	} {
		start, _, err := ScheduleTimes(false, "2026-11-20", c.hm, "", "")
		if err != nil {
			t.Fatal(err)
		}
		got := ReminderTimes(start, false, []string{"at", "m10", "h1", "d1_9"}, early)
		if len(got) != len(c.want) {
			t.Fatalf("%s: %v", c.hm, got)
		}
		for i := range got {
			if got[i].Kind != c.want[i].Kind || !got[i].Due.Equal(c.want[i].Due) {
				t.Errorf("%s [%d]: %s %s, want %s", c.hm, i, got[i].Kind, got[i].Due.In(KST), c.want[i].Due.In(KST))
			}
		}
	}
}
