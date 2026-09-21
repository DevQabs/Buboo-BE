package handler

import (
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

func TestPlanFromBaseline_PrependsTheAnchorAndKeepsTheFrozenTargets(t *testing.T) {
	b := models.RoadmapBaseline{
		AnchorMonth:       time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		AnchorNetWorthKRW: 415_000_974,
		Years:             []models.RoadmapYearRow{{Year: 2026, ProjectedNetWorthKRW: 427_788_427}},
		Months: []models.RoadmapMonthPoint{
			{Month: "2026-10", ContributionKRW: 2_500_000, ProjectedNetWorthKRW: 419_000_000},
		},
	}

	years, months := planFromBaseline(b)

	if len(months) != 2 || months[0].Month != "2026-09" || months[0].ProjectedNetWorthKRW != 415_000_974 {
		t.Fatalf("첫 줄은 출발 월 4.15억이어야 한다: got %+v", months)
	}
	if months[1].ProjectedNetWorthKRW != 419_000_000 || years[0].ProjectedNetWorthKRW != 427_788_427 {
		t.Errorf("저장된 목표가 그대로 나와야 한다: months=%+v years=%+v", months, years)
	}
	// 실적을 채우느라 결과를 고쳐도 저장된 기준선은 바뀌면 안 된다.
	actual := int64(1)
	years[0].ActualNetWorthKRW = &actual
	months[1].ActualNetWorthKRW = &actual
	if b.Years[0].ActualNetWorthKRW != nil || b.Months[0].ActualNetWorthKRW != nil {
		t.Error("기준선이 결과와 메모리를 공유한다")
	}
}
