package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgStockTransactionRepository struct {
	db *pgxpool.Pool
}

func NewPgStockTransactionRepository(db *pgxpool.Pool) *PgStockTransactionRepository {
	return &PgStockTransactionRepository{db: db}
}

const stxCols = `id, couple_id, user_id, stock_asset_id, symbol, exchange, name,
	type, quantity, price, currency, avg_price_at_tx, realized_pnl,
	memo, executed_at, created_at, exchange_rate_at_tx`

func scanSTx(row interface{ Scan(dest ...any) error }) (*models.StockTransaction, error) {
	var t models.StockTransaction
	if err := row.Scan(
		&t.ID, &t.CoupleID, &t.UserID, &t.StockAssetID,
		&t.Symbol, &t.Exchange, &t.Name,
		&t.Type, &t.Quantity, &t.Price, &t.Currency,
		&t.AvgPriceAtTx, &t.RealizedPnL,
		&t.Memo, &t.ExecutedAt, &t.CreatedAt, &t.ExchangeRateAtTx,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *PgStockTransactionRepository) Create(ctx context.Context, tx *models.StockTransaction) (*models.StockTransaction, error) {
	tx.ID = "stx-" + uuid.NewString()
	now := time.Now().UTC()
	tx.CreatedAt = now
	if tx.ExecutedAt.IsZero() {
		tx.ExecutedAt = now
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO stock_transactions
		 (id, couple_id, user_id, stock_asset_id, symbol, exchange, name,
		  type, quantity, price, currency, avg_price_at_tx, realized_pnl,
		  memo, executed_at, created_at, exchange_rate_at_tx)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		tx.ID, tx.CoupleID, tx.UserID, tx.StockAssetID,
		tx.Symbol, tx.Exchange, tx.Name,
		string(tx.Type), tx.Quantity, tx.Price, tx.Currency,
		tx.AvgPriceAtTx, tx.RealizedPnL,
		tx.Memo, tx.ExecutedAt, tx.CreatedAt, tx.ExchangeRateAtTx,
	)
	if err != nil {
		return nil, fmt.Errorf("create stock_transaction: %w", err)
	}
	return tx, nil
}

func (r *PgStockTransactionRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.StockTransaction, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+stxCols+` FROM stock_transactions WHERE couple_id = $1 ORDER BY executed_at DESC`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectSTxRows(rows)
}

func (r *PgStockTransactionRepository) ListBySymbol(ctx context.Context, coupleID, symbol string) ([]models.StockTransaction, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+stxCols+` FROM stock_transactions
		 WHERE couple_id = $1 AND symbol = $2 ORDER BY executed_at DESC`,
		coupleID, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectSTxRows(rows)
}

func (r *PgStockTransactionRepository) HasSellInYear(ctx context.Context, coupleID, symbol string, year int) (bool, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	row := r.db.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM stock_transactions
		   WHERE couple_id=$1 AND symbol=$2 AND type='sell'
		     AND executed_at >= $3 AND executed_at < $4
		 )`, coupleID, symbol, start, end)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *PgStockTransactionRepository) AnnualSummary(ctx context.Context, coupleID string, year int) ([]models.SymbolTaxSummary, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	rows, err := r.db.Query(ctx,
		`SELECT symbol, exchange,
		        COUNT(*) AS sell_count,
		        SUM(realized_pnl) AS realized_pnl
		 FROM stock_transactions
		 WHERE couple_id=$1 AND type='sell'
		   AND executed_at >= $2 AND executed_at < $3
		 GROUP BY symbol, exchange`,
		coupleID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	const taxRate = 0.22
	var result []models.SymbolTaxSummary
	for rows.Next() {
		var s models.SymbolTaxSummary
		if err := rows.Scan(&s.Symbol, &s.Exchange, &s.SellCount, &s.RealizedPnL); err != nil {
			return nil, err
		}
		taxable := s.RealizedPnL
		if taxable < 0 {
			taxable = 0
		}
		s.EstimatedTax = taxable * taxRate
		result = append(result, s)
	}
	if result == nil {
		result = []models.SymbolTaxSummary{}
	}
	return result, rows.Err()
}

func collectSTxRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]models.StockTransaction, error) {
	var result []models.StockTransaction
	for rows.Next() {
		t, err := scanSTx(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *t)
	}
	if result == nil {
		result = []models.StockTransaction{}
	}
	return result, rows.Err()
}
