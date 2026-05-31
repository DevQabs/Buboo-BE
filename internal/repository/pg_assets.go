package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgOtherAssetRepository struct {
	db *pgxpool.Pool
}

func NewPgOtherAssetRepository(db *pgxpool.Pool) *PgOtherAssetRepository {
	return &PgOtherAssetRepository{db: db}
}

const assetCols = `id, couple_id, user_id, asset_type, name, description,
	value_krw, value_usd, cost_krw, currency, is_liability, is_locked,
	location, maturity_date, interest_rate,
	loan_type, payment_day,
	memo, acquired_at, created_at, updated_at`

func scanAsset(row interface{ Scan(dest ...any) error }) (*models.OtherAsset, error) {
	var a models.OtherAsset
	var locJSON []byte
	var maturity pgtype.Timestamptz
	var rate, valueUSD pgtype.Float8

	if err := row.Scan(
		&a.ID, &a.CoupleID, &a.UserID, &a.AssetType, &a.Name, &a.Description,
		&a.ValueKRW, &valueUSD, &a.CostKRW, &a.Currency, &a.IsLiability, &a.IsLocked,
		&locJSON, &maturity, &rate,
		&a.LoanType, &a.PaymentDay,
		&a.Memo, &a.AcquiredAt, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if locJSON != nil {
		var loc models.Location
		json.Unmarshal(locJSON, &loc)
		a.Location = &loc
	}
	if maturity.Valid {
		t := maturity.Time
		a.MaturityDate = &t
	}
	if rate.Valid {
		a.InterestRate = &rate.Float64
	}
	if valueUSD.Valid {
		a.ValueUSD = &valueUSD.Float64
	}
	return &a, nil
}

func (r *PgOtherAssetRepository) GetByID(ctx context.Context, id string) (*models.OtherAsset, error) {
	row := r.db.QueryRow(ctx, `SELECT `+assetCols+` FROM other_assets WHERE id = $1`, id)
	a, err := scanAsset(row)
	if err != nil {
		return nil, fmt.Errorf("asset %s not found: %w", id, err)
	}
	return a, nil
}

func (r *PgOtherAssetRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.OtherAsset, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+assetCols+` FROM other_assets WHERE couple_id = $1 ORDER BY created_at`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectAssetRows(rows)
}

func (r *PgOtherAssetRepository) ListByUser(ctx context.Context, coupleID, userID string) ([]models.OtherAsset, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+assetCols+` FROM other_assets WHERE couple_id = $1 AND user_id = $2 ORDER BY created_at`,
		coupleID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectAssetRows(rows)
}

func (r *PgOtherAssetRepository) ListByType(ctx context.Context, coupleID string, assetType models.OtherAssetType) ([]models.OtherAsset, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+assetCols+` FROM other_assets WHERE couple_id = $1 AND asset_type = $2 ORDER BY created_at`,
		coupleID, string(assetType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectAssetRows(rows)
}

func (r *PgOtherAssetRepository) Create(ctx context.Context, asset *models.OtherAsset) (*models.OtherAsset, error) {
	asset.ID = "asset-" + uuid.NewString()
	now := time.Now().UTC()
	asset.CreatedAt = now
	asset.UpdatedAt = now
	if asset.AcquiredAt.IsZero() {
		asset.AcquiredAt = now
	}

	locJSON, _ := json.Marshal(asset.Location)
	if asset.Location == nil {
		locJSON = nil
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO other_assets
		 (id, couple_id, user_id, asset_type, name, description, value_krw, value_usd, cost_krw,
		  currency, is_liability, is_locked, location, maturity_date, interest_rate,
		  loan_type, payment_day, memo, acquired_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		asset.ID, asset.CoupleID, asset.UserID, string(asset.AssetType),
		asset.Name, asset.Description, asset.ValueKRW, asset.ValueUSD, asset.CostKRW,
		asset.Currency, asset.IsLiability, asset.IsLocked,
		locJSON, asset.MaturityDate, asset.InterestRate,
		asset.LoanType, asset.PaymentDay,
		asset.Memo, asset.AcquiredAt, asset.CreatedAt, asset.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create asset: %w", err)
	}
	return asset, nil
}

func (r *PgOtherAssetRepository) Update(ctx context.Context, asset *models.OtherAsset) (*models.OtherAsset, error) {
	asset.UpdatedAt = time.Now().UTC()

	locJSON, _ := json.Marshal(asset.Location)
	if asset.Location == nil {
		locJSON = nil
	}

	tag, err := r.db.Exec(ctx,
		`UPDATE other_assets SET
		 asset_type=$2, name=$3, description=$4, value_krw=$5, value_usd=$6, cost_krw=$7,
		 currency=$8, is_liability=$9, is_locked=$10, location=$11,
		 maturity_date=$12, interest_rate=$13,
		 loan_type=$14, payment_day=$15, memo=$16, updated_at=$17
		 WHERE id=$1`,
		asset.ID, string(asset.AssetType), asset.Name, asset.Description,
		asset.ValueKRW, asset.ValueUSD, asset.CostKRW,
		asset.Currency, asset.IsLiability, asset.IsLocked, locJSON,
		asset.MaturityDate, asset.InterestRate,
		asset.LoanType, asset.PaymentDay,
		asset.Memo, asset.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("asset %s not found", asset.ID)
	}
	return asset, nil
}

func (r *PgOtherAssetRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM other_assets WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("asset %s not found", id)
	}
	return nil
}

func collectAssetRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]models.OtherAsset, error) {
	var result []models.OtherAsset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *a)
	}
	if result == nil {
		result = []models.OtherAsset{}
	}
	return result, rows.Err()
}
