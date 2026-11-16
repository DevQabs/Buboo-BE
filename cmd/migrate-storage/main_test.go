package main

import "testing"

func TestPlan(t *testing.T) {
	const prefix = "https://ref.supabase.co/storage/v1/object/public/diary-photos/"
	cases := []struct {
		name, photo string
		want        step
	}{
		{"옛 공개 URL 은 복사", prefix + "abc.jpg", step{kind: "copy", src: "abc.jpg", key: "c1/diary/abc.jpg"}},
		{"이미 키면 건너뛰고 기록만 확인", "c1/diary/abc.jpg", step{kind: "key", key: "c1/diary/abc.jpg"}},
		{"로컬 업로드 경로는 건너뜀", "/uploads/abc.jpg", step{kind: "skip"}},
		{"다른 그룹 키는 건너뜀", "c2/diary/abc.jpg", step{kind: "skip"}},
		{"다른 버킷 URL 은 건너뜀", "https://ref.supabase.co/storage/v1/object/public/other/abc.jpg", step{kind: "skip"}},
	}
	for _, c := range cases {
		if got := plan(c.photo, prefix, "c1"); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	// 같은 원본은 언제나 같은 키 → 중간에 멈췄다 다시 돌려도 같은 객체를 덮어쓴다
	if plan(prefix+"abc.jpg", prefix, "c1").key != plan(prefix+"abc.jpg", prefix, "c1").key {
		t.Error("키는 결정적이어야 한다")
	}
}
