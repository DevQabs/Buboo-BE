package models

import (
	"testing"
	"time"
)

func kst(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, KST)
}

func TestValidReminders(t *testing.T) {
	cases := []struct {
		name   string
		allDay bool
		codes  []string
		want   bool
	}{
		{"빈 배열", true, nil, true},
		{"하루 종일: 당일·하루 전", true, []string{"d0_9", "d1_9"}, true},
		{"하루 종일에 10분 전 거부", true, []string{"m10"}, false},
		{"시간 지정: 정시·10분·1시간·하루 전", false, []string{"at", "m10", "h1", "d1_9"}, true},
		{"시간 지정에 당일 9시 거부", false, []string{"d0_9"}, false},
		{"모르는 코드 거부", true, []string{"d2_9"}, false},
		{"같은 코드 두 번 거부", true, []string{"d1_9", "d1_9"}, false},
	}
	for _, c := range cases {
		if got := ValidReminders(c.allDay, c.codes); got != c.want {
			t.Errorf("%s: ValidReminders = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReminderTimes(t *testing.T) {
	early := kst(2026, 11, 1, 0, 0)
	// 하루 종일 일정은 그 날짜의 UTC 자정으로 저장된다(createSchedule 의 time.Parse("2006-01-02"))
	allDay := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name   string
		start  time.Time
		allDay bool
		codes  []string
		now    time.Time
		want   []Reminder
	}{
		{"하루 종일: 당일·하루 전 오전 9시 KST", allDay(2026, 11, 20), true, []string{"d0_9", "d1_9"}, early,
			[]Reminder{{"d0_9", kst(2026, 11, 20, 9, 0)}, {"d1_9", kst(2026, 11, 19, 9, 0)}}},
		{"하루 종일: 달이 바뀌는 하루 전", allDay(2026, 12, 1), true, []string{"d1_9"}, early,
			[]Reminder{{"d1_9", kst(2026, 11, 30, 9, 0)}}},
		{"시간 지정: KST 자정 넘긴 시각은 KST 날짜 기준 하루 전", kst(2026, 11, 20, 0, 30), false,
			[]string{"at", "m10", "h1", "d1_9"}, early,
			[]Reminder{
				{"at", kst(2026, 11, 20, 0, 30)},
				{"m10", kst(2026, 11, 20, 0, 20)},
				{"h1", kst(2026, 11, 19, 23, 30)},
				{"d1_9", kst(2026, 11, 19, 9, 0)},
			}},
		{"지난 시각은 뺀다(같은 시각 포함)", allDay(2026, 11, 20), true, []string{"d0_9", "d1_9"}, kst(2026, 11, 19, 9, 0),
			[]Reminder{{"d0_9", kst(2026, 11, 20, 9, 0)}}},
		{"일정 종류와 맞지 않는 코드는 무시", allDay(2026, 11, 20), true, []string{"m10", "at"}, early, nil},
	}
	for _, c := range cases {
		got := ReminderTimes(c.start, c.allDay, c.codes, c.now)
		if len(got) != len(c.want) {
			t.Errorf("%s: %d개, want %d개 (%v)", c.name, len(got), len(c.want), got)
			continue
		}
		for i := range got {
			if got[i].Kind != c.want[i].Kind || !got[i].Due.Equal(c.want[i].Due) {
				t.Errorf("%s: [%d] = %s %s, want %s %s", c.name, i,
					got[i].Kind, got[i].Due.In(KST), c.want[i].Kind, c.want[i].Due.In(KST))
			}
		}
	}
}

func TestReminderLabel(t *testing.T) {
	want := map[string]string{"at": "지금", "m10": "10분 전", "h1": "1시간 전", "d0_9": "오늘", "d1_9": "내일"}
	for k, w := range want {
		if got := ReminderLabel(k); got != w {
			t.Errorf("ReminderLabel(%q) = %q, want %q", k, got, w)
		}
	}
}
