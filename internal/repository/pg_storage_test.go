package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/models"
)

func TestStorage_기록_합계_삭제(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStorageRepository(pool)
	couple := "c-test-" + uuid.NewString()
	k1, k2 := couple+"/baby/a.jpg", couple+"/diary/b.jpg"
	t.Cleanup(func() { _ = repo.Delete(context.Background(), []string{k1, k2}) })

	before, err := repo.TotalBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []models.StorageObject{
		{Key: k1, CoupleID: couple, Purpose: "baby", SizeBytes: 100},
		{Key: k1, CoupleID: couple, Purpose: "baby", SizeBytes: 999}, // 같은 키는 무시
		{Key: k2, CoupleID: couple, Purpose: "diary", SizeBytes: 20},
	} {
		if err := repo.Record(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := repo.TotalBytes(ctx); got-before != 120 {
		t.Errorf("합계 증가 = %d, want 120", got-before)
	}
	if o, err := repo.Get(ctx, k1); err != nil || o == nil || o.SizeBytes != 100 || o.Purpose != "baby" {
		t.Errorf("Get(k1) = %+v, %v", o, err)
	}
	if err := repo.Delete(ctx, []string{k1, k2}); err != nil {
		t.Fatal(err)
	}
	if o, err := repo.Get(ctx, k1); o != nil || err != nil {
		t.Errorf("지운 키는 nil, nil: %+v, %v", o, err)
	}
	if got, _ := repo.TotalBytes(ctx); got != before {
		t.Errorf("삭제 후 합계 = %d, want %d", got, before)
	}
}
