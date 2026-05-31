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

type PgStockRepository struct {
	db *pgxpool.Pool
}

func NewPgStockRepository(db *pgxpool.Pool) *PgStockRepository {
	return &PgStockRepository{db: db}
}

const stockCols = `id, couple_id, user_id, symbol, exchange, name, name_en,
	quantity, average_price, currency, sector, memo, logo_url,
	purchased_at, created_at, updated_at`

func scanStock(row interface{ Scan(dest ...any) error }) (*models.StockAsset, error) {
	var a models.StockAsset
	var logoURL pgtype.Text
	if err := row.Scan(
		&a.ID, &a.CoupleID, &a.UserID, &a.Symbol, &a.Exchange,
		&a.Name, &a.NameEn, &a.Quantity, &a.AveragePrice,
		&a.Currency, &a.Sector, &a.Memo, &logoURL,
		&a.PurchasedAt, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if logoURL.Valid {
		a.LogoURL = &logoURL.String
	}
	return &a, nil
}

func (r *PgStockRepository) GetByID(ctx context.Context, id string) (*models.StockAsset, error) {
	row := r.db.QueryRow(ctx, `SELECT `+stockCols+` FROM stock_assets WHERE id = $1`, id)
	a, err := scanStock(row)
	if err != nil {
		return nil, fmt.Errorf("stock %s not found: %w", id, err)
	}
	return a, nil
}

func (r *PgStockRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.StockAsset, error) {
	rows, err := r.db.Query(ctx, `SELECT `+stockCols+` FROM stock_assets WHERE couple_id = $1 ORDER BY created_at`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStockRows(rows)
}

func (r *PgStockRepository) ListByUser(ctx context.Context, coupleID, userID string) ([]models.StockAsset, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+stockCols+` FROM stock_assets WHERE couple_id = $1 AND user_id = $2 ORDER BY created_at`,
		coupleID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStockRows(rows)
}

func (r *PgStockRepository) Create(ctx context.Context, asset *models.StockAsset) (*models.StockAsset, error) {
	asset.ID = "stock-" + uuid.NewString()
	now := time.Now().UTC()
	asset.CreatedAt = now
	asset.UpdatedAt = now
	if asset.PurchasedAt.IsZero() {
		asset.PurchasedAt = now
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO stock_assets
		 (id, couple_id, user_id, symbol, exchange, name, name_en, quantity,
		  average_price, currency, sector, memo, logo_url, purchased_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		asset.ID, asset.CoupleID, asset.UserID, asset.Symbol, asset.Exchange,
		asset.Name, asset.NameEn, asset.Quantity, asset.AveragePrice,
		asset.Currency, asset.Sector, asset.Memo, asset.LogoURL,
		asset.PurchasedAt, asset.CreatedAt, asset.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create stock: %w", err)
	}
	return asset, nil
}

func (r *PgStockRepository) Update(ctx context.Context, asset *models.StockAsset) (*models.StockAsset, error) {
	asset.UpdatedAt = time.Now().UTC()
	tag, err := r.db.Exec(ctx,
		`UPDATE stock_assets SET
		 name=$2, name_en=$3, quantity=$4, average_price=$5,
		 sector=$6, memo=$7, logo_url=$8, updated_at=$9
		 WHERE id=$1`,
		asset.ID, asset.Name, asset.NameEn, asset.Quantity, asset.AveragePrice,
		asset.Sector, asset.Memo, asset.LogoURL, asset.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("stock %s not found", asset.ID)
	}
	return asset, nil
}

func (r *PgStockRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM stock_assets WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("stock %s not found", id)
	}
	return nil
}

func (r *PgStockRepository) GetPriceSnapshot(ctx context.Context, symbol string) (*models.PriceSnapshot, error) {
	row := r.db.QueryRow(ctx,
		`SELECT symbol, exchange, price, currency, change, change_percent, snapshotted_at
		 FROM price_snapshots WHERE symbol = $1`, symbol)
	var s models.PriceSnapshot
	if err := row.Scan(&s.Symbol, &s.Exchange, &s.Price, &s.Currency,
		&s.Change, &s.ChangePercent, &s.SnapshottedAt); err != nil {
		return nil, fmt.Errorf("snapshot for %s not found: %w", symbol, err)
	}
	return &s, nil
}

func (r *PgStockRepository) UpsertPriceSnapshot(ctx context.Context, snap *models.PriceSnapshot) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO price_snapshots (symbol, exchange, price, currency, change, change_percent, snapshotted_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (symbol) DO UPDATE SET
		   exchange=EXCLUDED.exchange, price=EXCLUDED.price,
		   currency=EXCLUDED.currency, change=EXCLUDED.change,
		   change_percent=EXCLUDED.change_percent, snapshotted_at=EXCLUDED.snapshotted_at`,
		snap.Symbol, snap.Exchange, snap.Price, snap.Currency,
		snap.Change, snap.ChangePercent, snap.SnapshottedAt,
	)
	return err
}

func collectStockRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]models.StockAsset, error) {
	var result []models.StockAsset
	for rows.Next() {
		a, err := scanStock(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *a)
	}
	if result == nil {
		result = []models.StockAsset{}
	}
	return result, rows.Err()
}
