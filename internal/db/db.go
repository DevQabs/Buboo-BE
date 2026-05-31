package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a pgxpool connection using DATABASE_URL env var.
func New(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping failed: %w", err)
	}
	return pool, nil
}
