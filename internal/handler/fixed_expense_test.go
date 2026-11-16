package handler

import "testing"

func TestClampDay(t *testing.T) {
	cases := []struct {
		year, month, day, want int
	}{
		{2026, 2, 31, 28}, // 평년 2월
		{2028, 2, 31, 29}, // 윤년 2월
		{2026, 4, 31, 30},
		{2026, 12, 31, 31},
		{2026, 1, 15, 15},
	}
	for _, c := range cases {
		if got := clampDay(c.year, c.month, c.day); got != c.want {
			t.Errorf("%d년 %d월 %d일은 %d일로 당겨져야 한다: got %d", c.year, c.month, c.day, c.want, got)
		}
	}
}
