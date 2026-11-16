package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

// newBabyCouple 은 겹치지 않는 couple_id 를 주고, 끝나면 아기 탭 행을 지운다.
func newBabyCouple(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := "c-test-" + uuid.NewString()
	t.Cleanup(func() {
		for _, table := range []string{"baby_moments", "baby_growth", "babies"} { // baby_media 는 CASCADE
			_, _ = pool.Exec(context.Background(), `DELETE FROM `+table+` WHERE couple_id = $1`, id)
		}
	})
	return id
}

func img(key string) models.BabyMedia {
	return models.BabyMedia{Key: key, Kind: "image", ContentType: "image/jpeg", SizeBytes: 10}
}

func TestBaby_이미_붙은_키는_ErrMediaKeyUsed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgBabyRepository(pool)
	couple := newBabyCouple(t, pool)
	key := couple + "/baby/a.jpg"

	first := &models.BabyMoment{UserID: "u1", Date: "2026-11-16", Media: []models.BabyMedia{img(key)}}
	if err := repo.CreateMoment(ctx, couple, first); err != nil {
		t.Fatal(err)
	}
	second := &models.BabyMoment{UserID: "u1", Date: "2026-11-16", Media: []models.BabyMedia{img(key)}}
	if err := repo.CreateMoment(ctx, couple, second); !errors.Is(err, ErrMediaKeyUsed) {
		t.Errorf("새 기록에 같은 키: err = %v, want ErrMediaKeyUsed", err)
	}
	if err := repo.UpdateMoment(ctx, couple, first, []models.BabyMedia{img(key)}); !errors.Is(err, ErrMediaKeyUsed) {
		t.Errorf("수정에 같은 키: err = %v, want ErrMediaKeyUsed", err)
	}
}

func TestBaby_프로필_등록_조회_수정(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgBabyRepository(pool)
	couple := newBabyCouple(t, pool)

	if b, err := repo.Get(ctx, couple); b != nil || err != nil {
		t.Fatalf("미등록은 nil, nil: %+v, %v", b, err)
	}
	if err := repo.Put(ctx, couple, models.Baby{Name: "튼튼이", BirthDate: "2026-10-05", Sex: "female"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Put(ctx, couple, models.Baby{Name: "튼튼", BirthDate: "2026-10-06", Sex: "male"}); err != nil {
		t.Fatal(err)
	}
	b, err := repo.Get(ctx, couple)
	if err != nil || b == nil || *b != (models.Baby{Name: "튼튼", BirthDate: "2026-10-06", Sex: "male"}) {
		t.Errorf("Get = %+v, %v; 두 번째 Put 값이어야 한다", b, err)
	}
}

func mediaKeys(ms []models.BabyMedia) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Key
	}
	return out
}

func TestBaby_기록_사진순서_수정_삭제(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgBabyRepository(pool)
	couple := newBabyCouple(t, pool)
	k := func(n string) string { return couple + "/baby/" + n }

	m := &models.BabyMoment{UserID: "u1", Date: "2026-11-15", Content: "첫 기록",
		Media: []models.BabyMedia{img(k("c.jpg")), img(k("a.jpg")), img(k("b.jpg"))}}
	if err := repo.CreateMoment(ctx, couple, m); err != nil {
		t.Fatal(err)
	}
	if m.ID == "" || m.Media[2].ID == "" || m.Media[2].SortOrder != 2 || m.Media[2].MomentID != m.ID {
		t.Fatalf("만든 뒤 ID·순서가 채워져야 한다: %+v", m)
	}
	newer := &models.BabyMoment{UserID: "u2", Date: "2026-11-16", Content: "둘째 날"}
	if err := repo.CreateMoment(ctx, couple, newer); err != nil {
		t.Fatal(err)
	}

	// 수정: 날짜·내용 바꾸고 뒤에 한 장 덧붙인다
	m.Content = "고친 기록"
	if err := repo.UpdateMoment(ctx, couple, m, []models.BabyMedia{img(k("d.jpg"))}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetMoment(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{k("c.jpg"), k("a.jpg"), k("b.jpg"), k("d.jpg")}
	if got.Content != "고친 기록" || fmt.Sprint(mediaKeys(got.Media)) != fmt.Sprint(want) || got.Media[3].SortOrder != 3 {
		t.Errorf("GetMoment = %q %v, want 고친 기록 %v (올린 순서)", got.Content, mediaKeys(got.Media), want)
	}

	list, err := repo.ListMoments(ctx, couple)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != newer.ID || list[1].ID != m.ID {
		t.Fatalf("ListMoments 는 날짜 내림차순: %+v", list)
	}
	if len(list[0].Media) != 0 || list[0].Media == nil || fmt.Sprint(mediaKeys(list[1].Media)) != fmt.Sprint(want) {
		t.Errorf("목록 사진: 빈 기록은 [], 나머지는 %v — got %v / %v", want, list[0].Media, mediaKeys(list[1].Media))
	}

	// 사진 한 장 삭제
	md, err := repo.GetMedia(ctx, got.Media[1].ID)
	if err != nil || md.Key != k("a.jpg") {
		t.Fatalf("GetMedia = %+v, %v", md, err)
	}
	if err := repo.DeleteMedia(ctx, md.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetMedia(ctx, md.ID); err == nil {
		t.Error("지운 사진은 GetMedia 가 오류여야 한다")
	}

	// 기록 삭제 → 사진 행도 CASCADE
	if err := repo.DeleteMoment(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetMoment(ctx, m.ID); err == nil {
		t.Error("지운 기록은 GetMoment 가 오류여야 한다")
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM baby_media WHERE moment_id = $1`, m.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("기록을 지우면 사진 행도 지워져야 한다: 남은 %d", left)
	}
}

func TestBaby_성장기록_CRUD(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgBabyRepository(pool)
	couple := newBabyCouple(t, pool)
	f := func(v float64) *float64 { return &v }

	later := &models.BabyGrowth{MeasuredOn: "2026-11-16", HeightCm: f(56.3), WeightKg: f(4.57), Memo: "1개월"}
	earlier := &models.BabyGrowth{MeasuredOn: "2026-10-05", WeightKg: f(3.2), HeadCm: f(34.5)}
	for _, g := range []*models.BabyGrowth{later, earlier} {
		if err := repo.CreateGrowth(ctx, couple, g); err != nil {
			t.Fatal(err)
		}
	}
	gs, err := repo.ListGrowth(ctx, couple)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 || gs[0].ID != earlier.ID || gs[1].ID != later.ID {
		t.Fatalf("측정일 오름차순: %+v", gs)
	}
	if gs[0].HeightCm != nil || *gs[0].WeightKg != 3.2 || *gs[0].HeadCm != 34.5 ||
		*gs[1].HeightCm != 56.3 || *gs[1].WeightKg != 4.57 || gs[1].HeadCm != nil || gs[1].Memo != "1개월" {
		t.Errorf("값·NULL 이 그대로 돌아와야 한다: %+v / %+v", gs[0], gs[1])
	}

	later.WeightKg, later.HeightCm = f(4.6), nil
	if err := repo.UpdateGrowth(ctx, later); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteGrowth(ctx, earlier.ID); err != nil {
		t.Fatal(err)
	}
	gs, err = repo.ListGrowth(ctx, couple)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 1 || gs[0].ID != later.ID || *gs[0].WeightKg != 4.6 || gs[0].HeightCm != nil {
		t.Errorf("수정·삭제 후: %+v", gs)
	}
}
