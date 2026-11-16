package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/models"
)

func TestCreate_같은고정비같은날은_한번만(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPgTransactionRepository(pool)
	couple := newCouple(t, pool)
	fe := "fe-" + uuid.NewString()
	day := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)

	create := func() error {
		_, err := repo.Create(ctx, &models.Transaction{
			CoupleID: couple, UserID: "u1", Type: "expense", Amount: 1000,
			Currency: "KRW", Date: day, FixedExpenseID: &fe,
		})
		return err
	}
	// 반영 버튼을 두 번 눌러 요청 둘이 동시에 들어온 경우
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- create() }()
	}
	dup := 0
	for range 2 {
		err := <-errs
		switch {
		case errors.Is(err, ErrDuplicate):
			dup++
		case err != nil:
			t.Fatal(err)
		}
	}
	if dup != 1 {
		t.Fatalf("두 요청 중 정확히 하나만 중복으로 막혀야 한다: 막힌 수 %d", dup)
	}
}
