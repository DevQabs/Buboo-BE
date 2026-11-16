package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgStorageRepository struct{ db *pgxpool.Pool }

func NewPgStorageRepository(db *pgxpool.Pool) *PgStorageRepository {
	return &PgStorageRepository{db: db}
}

func (r *PgStorageRepository) Record(ctx context.Context, o models.StorageObject) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO storage_objects (object_key, couple_id, purpose, size_bytes)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (object_key) DO NOTHING`,
		o.Key, o.CoupleID, o.Purpose, o.SizeBytes)
	return err
}

func (r *PgStorageRepository) Get(ctx context.Context, key string) (*models.StorageObject, error) {
	var o models.StorageObject
	err := r.db.QueryRow(ctx,
		`SELECT object_key, couple_id, purpose, size_bytes FROM storage_objects WHERE object_key = $1`, key,
	).Scan(&o.Key, &o.CoupleID, &o.Purpose, &o.SizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *PgStorageRepository) Delete(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `DELETE FROM storage_objects WHERE object_key = ANY($1)`, keys)
	return err
}

func (r *PgStorageRepository) TotalBytes(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM storage_objects`).Scan(&n)
	return n, err
}
