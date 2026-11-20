package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	memo, executed_at, created_at, exchange_rate_at_tx, source`

func scanSTx(row interface{ Scan(dest ...any) error }) (*models.StockTransaction, error) {
	var t models.StockTransaction
	if err := row.Scan(
		&t.ID, &t.CoupleID, &t.UserID, &t.StockAssetID,
		&t.Symbol, &t.Exchange, &t.Name,
		&t.Type, &t.Quantity, &t.Price, &t.Currency,
		&t.AvgPriceAtTx, &t.RealizedPnL,
		&t.Memo, &t.ExecutedAt, &t.CreatedAt, &t.ExchangeRateAtTx, &t.Source,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

// execer 는 풀과 트랜잭션을 둘 다 받는다. 같은 INSERT 를 Trade 의 트랜잭션 안에서도 쓴다.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func insertSTx(ctx context.Context, db execer, tx *models.StockTransaction) error {
	tx.ID = "stx-" + uuid.NewString()
	now := time.Now().UTC()
	tx.CreatedAt = now
	if tx.ExecutedAt.IsZero() {
		tx.ExecutedAt = now
	}
	if tx.Source == "" {
		tx.Source = models.StockTxSourceTrade
	}
	_, err := db.Exec(ctx,
		`INSERT INTO stock_transactions
		 (id, couple_id, user_id, stock_asset_id, symbol, exchange, name,
		  type, quantity, price, currency, avg_price_at_tx, realized_pnl,
		  memo, executed_at, created_at, exchange_rate_at_tx, source)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		tx.ID, tx.CoupleID, tx.UserID, tx.StockAssetID,
		tx.Symbol, tx.Exchange, tx.Name,
		string(tx.Type), tx.Quantity, tx.Price, tx.Currency,
		tx.AvgPriceAtTx, tx.RealizedPnL,
		tx.Memo, tx.ExecutedAt, tx.CreatedAt, tx.ExchangeRateAtTx, tx.Source,
	)
	if err != nil {
		return fmt.Errorf("create stock_transaction: %w", err)
	}
	return nil
}

func (r *PgStockTransactionRepository) Create(ctx context.Context, tx *models.StockTransaction) (*models.StockTransaction, error) {
	if err := insertSTx(ctx, r.db, tx); err != nil {
		return nil, err
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

func (r *PgStockTransactionRepository) GetByID(ctx context.Context, coupleID, id string) (*models.StockTransaction, error) {
	t, err := scanSTx(r.db.QueryRow(ctx,
		`SELECT `+stxCols+` FROM stock_transactions WHERE id = $1 AND couple_id = $2`, id, coupleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
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
	start := time.Date(year, 1, 1, 0, 0, 0, 0, models.KST)
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

// 국내 상장 주식 매도는 대주주가 아니면 양도세 대상이 아니라 USD 만 센다.
func (r *PgStockTransactionRepository) AnnualSummary(ctx context.Context, coupleID string, year int) ([]models.SymbolTaxSummary, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, models.KST)
	end := start.AddDate(1, 0, 0)
	rows, err := r.db.Query(ctx,
		`SELECT symbol, exchange,
		        COUNT(*) AS sell_count,
		        SUM(realized_pnl) AS realized_pnl
		 FROM stock_transactions
		 WHERE couple_id=$1 AND type='sell' AND currency='USD'
		   AND executed_at >= $2 AND executed_at < $3
		 GROUP BY symbol, exchange`,
		coupleID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 종목별 세액은 계산하지 않는다. 양도소득세는 사람 단위로 손익을 통산한
	// 뒤 기본공제를 적용해 매기므로, 종목 하나만 떼어 22%를 곱한 값은 실제
	// 세액과 무관하다. 세액은 AnnualGainsByUser + service.CalcCapitalGainsTax 몫이다.
	result := []models.SymbolTaxSummary{}
	for rows.Next() {
		var s models.SymbolTaxSummary
		if err := rows.Scan(&s.Symbol, &s.Exchange, &s.SellCount, &s.RealizedPnL); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// AnnualGainsByUser nets realized P&L per person for the given year.
// GROUP BY user_id 가 한 사람 안에서 종목 간 손익을 통산한다 — 손실 종목이
// 이익 종목을 상계하는 것이 실제 과세 방식이다.
// 국내 상장 주식 매도는 대주주가 아니면 양도세 대상이 아니라 USD 만 센다.
func (r *PgStockTransactionRepository) AnnualGainsByUser(ctx context.Context, coupleID string, year int) ([]models.UserGainRow, error) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, models.KST)
	end := start.AddDate(1, 0, 0)
	rows, err := r.db.Query(ctx,
		`SELECT user_id,
		        COUNT(*) AS sell_count,
		        SUM(realized_pnl) AS realized_pnl
		 FROM stock_transactions
		 WHERE couple_id=$1 AND type='sell' AND currency='USD'
		   AND executed_at >= $2 AND executed_at < $3
		 GROUP BY user_id
		 ORDER BY user_id`,
		coupleID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []models.UserGainRow{}
	for rows.Next() {
		var g models.UserGainRow
		if err := rows.Scan(&g.UserID, &g.SellCount, &g.RealizedPnL); err != nil {
			return nil, err
		}
		result = append(result, g)
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
