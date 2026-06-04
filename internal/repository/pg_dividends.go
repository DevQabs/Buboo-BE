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

type PgDividendRepository struct {
	db *pgxpool.Pool
}

func NewPgDividendRepository(db *pgxpool.Pool) *PgDividendRepository {
	return &PgDividendRepository{db: db}
}

const divCols = `id, couple_id, user_id, stock_asset_id, symbol, exchange, name,
	quantity, amount_per_share, currency, total_amount, tax_rate, after_tax_amount,
	usd_krw_rate, amount_krw, ex_dividend_date, payment_date,
	is_applied, memo, created_at, updated_at`

func scanDiv(row interface{ Scan(dest ...any) error }) (*models.DividendEvent, error) {
	var d models.DividendEvent
	var exDate pgtype.Timestamptz

	if err := row.Scan(
		&d.ID, &d.CoupleID, &d.UserID, &d.StockAssetID,
		&d.Symbol, &d.Exchange, &d.Name,
		&d.Quantity, &d.AmountPerShare, &d.Currency,
		&d.TotalAmount, &d.TaxRate, &d.AfterTaxAmount,
		&d.USDKRWRate, &d.AmountKRW,
		&exDate, &d.PaymentDate,
		&d.IsApplied, &d.Memo, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if exDate.Valid {
		t := exDate.Time
		d.ExDividendDate = &t
	}
	return &d, nil
}

func (r *PgDividendRepository) GetByID(ctx context.Context, id string) (*models.DividendEvent, error) {
	row := r.db.QueryRow(ctx, `SELECT `+divCols+` FROM dividends WHERE id = $1`, id)
	d, err := scanDiv(row)
	if err != nil {
		return nil, fmt.Errorf("dividend %s not found: %w", id, err)
	}
	return d, nil
}

func (r *PgDividendRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.DividendEvent, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+divCols+` FROM dividends WHERE couple_id = $1 ORDER BY payment_date DESC`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDivRows(rows)
}

func (r *PgDividendRepository) ListByYear(ctx context.Context, coupleID string, year int) ([]models.DividendEvent, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	rows, err := r.db.Query(ctx,
		`SELECT `+divCols+` FROM dividends
		 WHERE couple_id = $1 AND payment_date >= $2 AND payment_date < $3
		 ORDER BY payment_date DESC`,
		coupleID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDivRows(rows)
}

func (r *PgDividendRepository) Create(ctx context.Context, d *models.DividendEvent) (*models.DividendEvent, error) {
	d.ID = "div-" + uuid.NewString()
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now

	_, err := r.db.Exec(ctx,
		`INSERT INTO dividends
		 (id, couple_id, user_id, stock_asset_id, symbol, exchange, name,
		  quantity, amount_per_share, currency, total_amount, tax_rate, after_tax_amount,
		  usd_krw_rate, amount_krw, ex_dividend_date, payment_date,
		  is_applied, memo, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		d.ID, d.CoupleID, d.UserID, d.StockAssetID,
		d.Symbol, d.Exchange, d.Name,
		d.Quantity, d.AmountPerShare, d.Currency,
		d.TotalAmount, d.TaxRate, d.AfterTaxAmount,
		d.USDKRWRate, d.AmountKRW,
		d.ExDividendDate, d.PaymentDate,
		d.IsApplied, d.Memo, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create dividend: %w", err)
	}
	return d, nil
}

func (r *PgDividendRepository) MarkApplied(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE dividends SET is_applied = TRUE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("dividend %s not found", id)
	}
	return nil
}

func (r *PgDividendRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM dividends WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("dividend %s not found", id)
	}
	return nil
}

func collectDivRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]models.DividendEvent, error) {
	var result []models.DividendEvent
	for rows.Next() {
		d, err := scanDiv(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *d)
	}
	if result == nil {
		result = []models.DividendEvent{}
	}
	return result, rows.Err()
}
