package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgFridgeRepository struct {
	db *pgxpool.Pool
}

func NewPgFridgeRepository(db *pgxpool.Pool) *PgFridgeRepository {
	return &PgFridgeRepository{db: db}
}

// ── FridgeItem ─────────────────────────────────────────────────────────────

const fridgeItemCols = `id, couple_id, name, quantity, expiry_date, location, category, memo, created_at, updated_at`

func scanFridgeItem(row interface{ Scan(dest ...any) error }) (*models.FridgeItem, error) {
	var item models.FridgeItem
	var expiryDate pgtype.Timestamptz

	if err := row.Scan(
		&item.ID, &item.CoupleID, &item.Name, &item.Quantity,
		&expiryDate, &item.Location, &item.Category, &item.Memo,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if expiryDate.Valid {
		t := expiryDate.Time
		item.ExpiryDate = &t
	}
	return &item, nil
}

func (r *PgFridgeRepository) ListItems(ctx context.Context, coupleID string) ([]models.FridgeItem, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+fridgeItemCols+` FROM fridge_items WHERE couple_id = $1 ORDER BY created_at DESC`,
		coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.FridgeItem
	for rows.Next() {
		item, err := scanFridgeItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	if result == nil {
		result = []models.FridgeItem{}
	}
	return result, rows.Err()
}

func (r *PgFridgeRepository) CreateItem(ctx context.Context, item *models.FridgeItem) (*models.FridgeItem, error) {
	item.ID = "fi-" + uuid.NewString()
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now

	_, err := r.db.Exec(ctx,
		`INSERT INTO fridge_items
		 (id, couple_id, name, quantity, expiry_date, location, category, memo, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		item.ID, item.CoupleID, item.Name, item.Quantity,
		item.ExpiryDate, item.Location, item.Category, item.Memo,
		item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create fridge item: %w", err)
	}
	return item, nil
}

func (r *PgFridgeRepository) UpdateItem(ctx context.Context, item *models.FridgeItem) (*models.FridgeItem, error) {
	item.UpdatedAt = time.Now().UTC()
	tag, err := r.db.Exec(ctx,
		`UPDATE fridge_items SET
		 name=$2, quantity=$3, expiry_date=$4, location=$5, category=$6, memo=$7, updated_at=$8
		 WHERE id=$1`,
		item.ID, item.Name, item.Quantity, item.ExpiryDate,
		item.Location, item.Category, item.Memo, item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("fridge item %s not found", item.ID)
	}
	return item, nil
}

func (r *PgFridgeRepository) DeleteItem(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM fridge_items WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("fridge item %s not found", id)
	}
	return nil
}

// ── SideDish ───────────────────────────────────────────────────────────────

const sideDishCols = `id, couple_id, name, made_at, expires_at, location, memo, created_at, updated_at`

func scanSideDish(row interface{ Scan(dest ...any) error }) (*models.SideDish, error) {
	var dish models.SideDish
	var expiresAt pgtype.Timestamptz

	if err := row.Scan(
		&dish.ID, &dish.CoupleID, &dish.Name, &dish.MadeAt,
		&expiresAt, &dish.Location, &dish.Memo,
		&dish.CreatedAt, &dish.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		dish.ExpiresAt = &t
	}
	return &dish, nil
}

func (r *PgFridgeRepository) ListDishes(ctx context.Context, coupleID string) ([]models.SideDish, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+sideDishCols+` FROM side_dishes WHERE couple_id = $1 ORDER BY made_at DESC`,
		coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.SideDish
	for rows.Next() {
		dish, err := scanSideDish(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *dish)
	}
	if result == nil {
		result = []models.SideDish{}
	}
	return result, rows.Err()
}

func (r *PgFridgeRepository) CreateDish(ctx context.Context, dish *models.SideDish) (*models.SideDish, error) {
	dish.ID = "sd-" + uuid.NewString()
	now := time.Now().UTC()
	dish.CreatedAt = now
	dish.UpdatedAt = now

	_, err := r.db.Exec(ctx,
		`INSERT INTO side_dishes
		 (id, couple_id, name, made_at, expires_at, location, memo, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		dish.ID, dish.CoupleID, dish.Name, dish.MadeAt,
		dish.ExpiresAt, dish.Location, dish.Memo,
		dish.CreatedAt, dish.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create side dish: %w", err)
	}
	return dish, nil
}

func (r *PgFridgeRepository) UpdateDish(ctx context.Context, dish *models.SideDish) (*models.SideDish, error) {
	dish.UpdatedAt = time.Now().UTC()
	tag, err := r.db.Exec(ctx,
		`UPDATE side_dishes SET
		 name=$2, made_at=$3, expires_at=$4, location=$5, memo=$6, updated_at=$7
		 WHERE id=$1`,
		dish.ID, dish.Name, dish.MadeAt, dish.ExpiresAt,
		dish.Location, dish.Memo, dish.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("side dish %s not found", dish.ID)
	}
	return dish, nil
}

func (r *PgFridgeRepository) DeleteDish(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM side_dishes WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("side dish %s not found", id)
	}
	return nil
}
