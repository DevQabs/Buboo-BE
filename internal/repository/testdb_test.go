package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool 은 TEST_DATABASE_URL 의 PG 에 붙는다. 없으면 건너뛴다.
// 띄우는 법: docs/superpowers/plans/2026-11-16-uri-jip-phase5-bugs.md Task 1 Step 3.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 이 없어 DB 테스트를 건너뜀")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("테스트 DB 연결 실패: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newCouple 은 테스트마다 겹치지 않는 couple_id 를 주고, 끝나면 그 그룹 행을 지운다.
func newCouple(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := "c-test-" + uuid.NewString()
	t.Cleanup(func() {
		for _, table := range []string{"transactions", "stock_transactions", "stock_assets", "dividends", "fixed_expenses"} {
			_, _ = pool.Exec(context.Background(), `DELETE FROM `+table+` WHERE couple_id = $1`, id)
		}
	})
	return id
}
