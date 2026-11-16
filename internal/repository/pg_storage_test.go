package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// 상한 근처에서 동시에 예약해도 합계가 상한을 넘지 않아야 한다(advisory lock).
func TestStorage_ReserveWithinCap_동시예약(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgStorageRepository(pool)
	couple := "c-test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM storage_objects WHERE couple_id = $1`, couple)
	})
	before, err := repo.TotalBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	limit := before + 1000 // 100바이트짜리 10개까지만 들어간다
	const n = 30
	var wg sync.WaitGroup
	var okCount atomic.Int32
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := repo.ReserveWithinCap(ctx, models.StorageObject{
				Key: fmt.Sprintf("%s/baby/%d.jpg", couple, i), CoupleID: couple, Purpose: "baby", SizeBytes: 100,
			}, limit)
			if err != nil {
				errs <- err
				return
			}
			if ok {
				okCount.Add(1)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	got, _ := repo.TotalBytes(ctx)
	if got > limit {
		t.Fatalf("합계 %d 가 상한 %d 를 넘었다", got, limit)
	}
	if okCount.Load() != 10 || got != limit {
		t.Errorf("성공 %d 건, 합계 %d — want 10 건, %d", okCount.Load(), got, limit)
	}
}
