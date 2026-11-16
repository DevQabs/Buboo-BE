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

// storageCapLockKey 는 저장소 상한 검사를 줄 세우는 advisory lock 번호다(임의의 고정값).
const storageCapLockKey = 0x75726a69 // "urji"

// ReserveWithinCap 은 버킷 합계 + o.SizeBytes 가 limit 이하일 때만 o 를 기록한다.
// 잠금·합계·기록을 한 트랜잭션에서 하므로 동시 요청이 함께 상한을 넘지 못한다.
// ponytail: 버킷 전체 하나의 잠금 — 업로드 발급이 몰려 느려지면 그때 나눈다.
func (r *PgStorageRepository) ReserveWithinCap(ctx context.Context, o models.StorageObject, limit int64) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, storageCapLockKey); err != nil {
		return false, err
	}
	var used int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM storage_objects`).Scan(&used); err != nil {
		return false, err
	}
	if used+o.SizeBytes > limit {
		return false, nil
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO storage_objects (object_key, couple_id, purpose, size_bytes) VALUES ($1, $2, $3, $4)`,
		o.Key, o.CoupleID, o.Purpose, o.SizeBytes); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
