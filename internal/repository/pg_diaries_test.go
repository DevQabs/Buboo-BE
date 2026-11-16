package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/yourname/couple-app/internal/models"
)

func TestDiary_AddPhoto_키_중복(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgDiaryRepository(pool)
	couple := newCouple(t, pool)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM diaries WHERE couple_id = $1`, couple) })

	a, err := repo.Create(ctx, &models.DiaryEntry{CoupleID: couple, UserID: "u1", Date: "2026-01-01", Mood: "normal"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Create(ctx, &models.DiaryEntry{CoupleID: couple, UserID: "u1", Date: "2026-01-02", Mood: "normal"})
	if err != nil {
		t.Fatal(err)
	}
	key := couple + "/diary/x.jpg"

	for i := 0; i < 2; i++ { // 같은 일기에 두 번 붙여도 한 번만 들어간다
		if err := repo.AddPhoto(ctx, a.ID, key); err != nil {
			t.Fatalf("붙이기 %d: %v", i, err)
		}
	}
	if got, _ := repo.GetByID(ctx, a.ID); len(got.Photos) != 1 {
		t.Errorf("A photos = %v, want 키 하나", got.Photos)
	}
	if err := repo.AddPhoto(ctx, b.ID, key); !errors.Is(err, ErrPhotoInUse) {
		t.Errorf("다른 일기에 같은 키 = %v, want ErrPhotoInUse", err)
	}
	if got, _ := repo.GetByID(ctx, b.ID); len(got.Photos) != 0 {
		t.Errorf("B photos = %v, want 비어 있음", got.Photos)
	}
	if err := repo.AddPhoto(ctx, "diary-none", couple+"/diary/y.jpg"); err == nil || errors.Is(err, ErrPhotoInUse) {
		t.Errorf("없는 일기 = %v, want not found", err)
	}
}
