package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/models"
)

func TestRedeemInvite_이름은30자로자른다(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	couple := newCouple(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO couples (id, name) VALUES ($1, 't')`, couple); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE couple_id = $1`, couple)
		_, _ = pool.Exec(context.Background(), `DELETE FROM couples WHERE id = $1`, couple)
	})
	repo := NewPgInviteRepository(pool)
	inv, err := repo.CreateInvite(ctx, couple, "owner", "spouse", "")
	if err != nil {
		t.Fatal(err)
	}
	// 이름 없이 들어오면 메일 앞부분(fallback)이 이름이 된다. 그 값도 30자를 넘지 않아야 한다.
	u, err := repo.RedeemInvite(ctx, inv.Code, models.User{ID: uuid.NewString(), Email: uuid.NewString() + "@t"}, " "+strings.Repeat("가", 40))
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != strings.Repeat("가", 30) {
		t.Errorf("이름은 앞뒤 공백을 빼고 30자: %q", u.Name)
	}
}
