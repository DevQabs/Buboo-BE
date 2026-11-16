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
		{"인코딩된 이름은 풀어서", prefix + "%ED%95%9C%EA%B8%80%20a.jpg", step{kind: "copy", src: "한글 a.jpg", key: "c1/diary/한글 a.jpg"}},
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

func TestCollisions(t *testing.T) {
	a := item{step: step{kind: "copy", src: "x/a.jpg", key: "c1/diary/a.jpg"}, coupleID: "c1"}
	b := item{step: step{kind: "copy", src: "y/a.jpg", key: "c1/diary/a.jpg"}, coupleID: "c1"}
	if len(collisions([]item{a, a})) != 0 {
		t.Error("같은 원본 중복은 충돌이 아니다")
	}
	if len(collisions([]item{a, b})) != 1 {
		t.Error("다른 원본이 같은 키면 충돌이어야 한다")
	}
}
