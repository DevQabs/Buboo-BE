package repository

import (
	"context"
	"errors"
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
