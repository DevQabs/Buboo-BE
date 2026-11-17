package models

import (
	"slices"
	"time"
)

// 일정 알림 시점 코드(push 설계서 3절). 시간 지정 일정과 하루 종일 일정이 고를 수 있는 코드가 다르다.
var (
	timedReminders  = []string{"at", "m10", "h1", "d1_9"}
	allDayReminders = []string{"d0_9", "d1_9"}
	reminderLabels  = map[string]string{"at": "지금", "m10": "10분 전", "h1": "1시간 전", "d0_9": "오늘", "d1_9": "내일"}
)

// Reminder 는 보낼 알림 하나: 코드와 발송 시각.
type Reminder struct {
	Kind string
	Due  time.Time
}

// ValidReminders 는 코드가 모두 그 일정 종류에서 고를 수 있고 겹치지 않는지 본다.
func ValidReminders(allDay bool, codes []string) bool {
	allowed := timedReminders
	if allDay {
		allowed = allDayReminders
	}
	for i, c := range codes {
		if !slices.Contains(allowed, c) || slices.Contains(codes[:i], c) {
			return false
		}
	}
	return true
}

// ReminderTimes 는 코드마다 발송 시각을 낸다. now 이전(같은 시각 포함)과 일정 종류에 맞지 않는 코드는 뺀다.
// 날짜는 KST 로 본다. 하루 종일 일정은 그 날짜의 UTC 자정으로 저장되므로 KST 로 바꿔도 날짜가 같다.
func ReminderTimes(start time.Time, allDay bool, codes []string, now time.Time) []Reminder {
	k := start.In(KST)
	nineAM := func(daysBefore int) time.Time {
		return time.Date(k.Year(), k.Month(), k.Day()-daysBefore, 9, 0, 0, 0, KST)
	}
	var out []Reminder
	for _, c := range codes {
		if !ValidReminders(allDay, []string{c}) {
			continue
		}
		var due time.Time
		switch c {
		case "at":
			due = start
		case "m10":
			due = start.Add(-10 * time.Minute)
		case "h1":
			due = start.Add(-time.Hour)
		case "d0_9":
			due = nineAM(0)
		case "d1_9":
			due = nineAM(1)
		}
		if due.After(now) {
			out = append(out, Reminder{Kind: c, Due: due})
		}
	}
	return out
}

// ReminderLabel 은 알림 본문 끝에 붙는 말(예: "10분 전").
func ReminderLabel(kind string) string { return reminderLabels[kind] }
