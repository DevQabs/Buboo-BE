package models

import (
	"errors"
	"time"
)

var (
	ErrStartTimeRequired = errors.New("시간 지정 일정은 start_time(HH:MM)이 필요합니다")
	ErrEndWithoutTime    = errors.New("시간 지정 일정의 end_date 는 end_time 과 함께 보내야 합니다")
	ErrEndBeforeStart    = errors.New("끝 시각이 시작 시각보다 빠릅니다")
)

// ScheduleTimes 는 요청의 날짜("YYYY-MM-DD")·시각("HH:MM", KST)을 저장할 시각으로 바꾼다.
// 하루 종일은 예전처럼 그 날짜의 UTC 자정(시각은 무시). 시간 지정은 KST 벽시계 시각.
// 끝: 하루 종일은 endDate 만, 시간 지정은 endTime 이 있을 때만(endDate 가 없으면 시작 날짜).
func ScheduleTimes(allDay bool, date, startTime, endDate, endTime string) (time.Time, *time.Time, error) {
	if allDay {
		start, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return time.Time{}, nil, err
		}
		if endDate == "" {
			return start, nil, nil
		}
		end, err := time.Parse(time.DateOnly, endDate)
		if err != nil {
			return time.Time{}, nil, err
		}
		return start, &end, nil
	}
	if startTime == "" {
		return time.Time{}, nil, ErrStartTimeRequired
	}
	start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+startTime, KST)
	if err != nil {
		return time.Time{}, nil, err
	}
	if endTime == "" {
		if endDate != "" {
			return time.Time{}, nil, ErrEndWithoutTime
		}
		return start, nil, nil
	}
	if endDate == "" {
		endDate = date
	}
	end, err := time.ParseInLocation("2006-01-02 15:04", endDate+" "+endTime, KST)
	if err != nil {
		return time.Time{}, nil, err
	}
	if end.Before(start) {
		return time.Time{}, nil, ErrEndBeforeStart
	}
	return start, &end, nil
}

// InKST 는 응답용으로 시각을 KST 로 돌리고 start_time/end_time 을 채운다.
// start_date 앞 10글자가 곧 KST 날짜라 프론트는 시간대 계산 없이 잘라 쓴다.
func (s *Schedule) InKST() {
	s.StartDate = s.StartDate.In(KST)
	s.StartTime, s.EndTime = "", ""
	if !s.AllDay {
		s.StartTime = s.StartDate.Format("15:04")
	}
	if s.EndDate != nil {
		e := s.EndDate.In(KST)
		s.EndDate = &e
		if !s.AllDay {
			s.EndTime = e.Format("15:04")
		}
	}
}
