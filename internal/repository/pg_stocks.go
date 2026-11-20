package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	purchased_at, created_at, updated_at, avg_krw_price, sort_order`

func scanStock(row interface{ Scan(dest ...any) error }) (*models.StockAsset, error) {
	var a models.StockAsset
	var logoURL pgtype.Text
	if err := row.Scan(
		&a.ID, &a.CoupleID, &a.UserID, &a.Symbol, &a.Exchange,
		&a.Name, &a.NameEn, &a.Quantity, &a.AveragePrice,
		&a.Currency, &a.Sector, &a.Memo, &logoURL,
		&a.PurchasedAt, &a.CreatedAt, &a.UpdatedAt, &a.AvgKRWPrice, &a.SortOrder,
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
	rows, err := r.db.Query(ctx, `SELECT `+stockCols+` FROM stock_assets WHERE couple_id = $1 ORDER BY sort_order, created_at`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStockRows(rows)
}

func (r *PgStockRepository) ListByUser(ctx context.Context, coupleID, userID string) ([]models.StockAsset, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+stockCols+` FROM stock_assets WHERE couple_id = $1 AND user_id = $2 ORDER BY sort_order, created_at`,
		coupleID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStockRows(rows)
}

// Create 는 종목을 만들고, buy 가 있으면 그 매수 기록을 같은 DB 트랜잭션에 남긴다.
func (r *PgStockRepository) Create(ctx context.Context, asset *models.StockAsset, buy *models.StockTransaction) (*models.StockAsset, error) {
	asset.ID = "stock-" + uuid.NewString()
	now := time.Now().UTC()
	asset.CreatedAt = now
	asset.UpdatedAt = now
	if asset.PurchasedAt.IsZero() {
		asset.PurchasedAt = now
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx,
		`INSERT INTO stock_assets
		 (id, couple_id, user_id, symbol, exchange, name, name_en, quantity,
		  average_price, currency, sector, memo, logo_url, purchased_at, created_at, updated_at, avg_krw_price,
		  sort_order)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
		         COALESCE(
		             (SELECT MIN(sort_order) FROM stock_assets WHERE couple_id = $2 AND symbol = $4 AND exchange = $5),
		             (SELECT MAX(sort_order) + 1 FROM stock_assets WHERE couple_id = $2),
		             0))`,
		asset.ID, asset.CoupleID, asset.UserID, asset.Symbol, asset.Exchange,
		asset.Name, asset.NameEn, asset.Quantity, asset.AveragePrice,
		asset.Currency, asset.Sector, asset.Memo, asset.LogoURL,
		asset.PurchasedAt, asset.CreatedAt, asset.UpdatedAt, asset.AvgKRWPrice,
	)
	if err != nil {
		return nil, fmt.Errorf("create stock: %w", err)
	}
	if buy != nil {
		buy.StockAssetID = asset.ID
		if err := insertSTx(ctx, tx, buy); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return asset, nil
}

// updateStockRow 는 풀이나 트랜잭션으로 종목 한 행을 저장한다. 소유자(user_id)도 저장한다.
func updateStockRow(ctx context.Context, db execer, a *models.StockAsset) error {
	a.UpdatedAt = time.Now().UTC()
	tag, err := db.Exec(ctx,
		`UPDATE stock_assets SET
		 user_id=$2, name=$3, name_en=$4, quantity=$5, average_price=$6,
		 sector=$7, memo=$8, logo_url=$9, updated_at=$10, avg_krw_price=$11
		 WHERE id=$1`,
		a.ID, a.UserID, a.Name, a.NameEn, a.Quantity, a.AveragePrice,
		a.Sector, a.Memo, a.LogoURL, a.UpdatedAt, a.AvgKRWPrice,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("stock %s not found", a.ID)
	}
	return nil
}

func (r *PgStockRepository) Update(ctx context.Context, asset *models.StockAsset) (*models.StockAsset, error) {
	if err := updateStockRow(ctx, r.db, asset); err != nil {
		return nil, err
	}
	return asset, nil
}

// Trade 는 매수·매도를 한 DB 트랜잭션으로 처리한다. 종목 행을 FOR UPDATE 로 잠가
// 동시에 들어온 두 요청이 같은 수량을 읽고 서로 덮어쓰지 못하게 한다. 기록 저장이
// 실패하면 잔고 변경도 되돌린다.
func (r *PgStockRepository) Trade(ctx context.Context, id, coupleID string, apply func(*models.StockAsset) (*models.StockTransaction, error)) (*models.StockAsset, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	a, err := scanStock(tx.QueryRow(ctx,
		`SELECT `+stockCols+` FROM stock_assets WHERE id = $1 AND couple_id = $2 FOR UPDATE`, id, coupleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	log, err := apply(a)
	if err != nil {
		return nil, err
	}
	if a.Quantity < models.QtyEpsilon {
		a.Quantity = 0
		_, err = tx.Exec(ctx, `DELETE FROM stock_assets WHERE id = $1`, id)
	} else {
		err = updateStockRow(ctx, tx, a)
	}
	if err != nil {
		return nil, err
	}
	if log != nil {
		if err := insertSTx(ctx, tx, log); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// Rewrite 는 종목 행을 FOR UPDATE 로 잠그고 그 종목의 매매 기록을 시간순으로 읽어 fn 에
// 넘긴다. fn 이 바꾼 종목(수량이 0 이면 행 삭제)과 기록 변경을 한 트랜잭션으로 저장한다.
// 다 팔아 종목 행이 없으면 a 는 nil 이다. 그때는 잠글 행이 없으니 종목 id 로 advisory
// lock 을 잡아 두 정정이 겹치지 않게 한다. 행도 기록도 없으면 ErrNotFound.
func (r *PgStockRepository) Rewrite(ctx context.Context, id, coupleID string, fn func(*models.StockAsset, []models.StockTransaction) (StockTxChanges, error)) (*models.StockAsset, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, id); err != nil {
		return nil, err
	}
	a, err := scanStock(tx.QueryRow(ctx,
		`SELECT `+stockCols+` FROM stock_assets WHERE id = $1 AND couple_id = $2 FOR UPDATE`, id, coupleID))
	if errors.Is(err, pgx.ErrNoRows) {
		a = nil
	} else if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx,
		`SELECT `+stxCols+` FROM stock_transactions
		 WHERE stock_asset_id = $1 AND couple_id = $2 ORDER BY executed_at, created_at, id`, id, coupleID)
	if err != nil {
		return nil, err
	}
	txs, err := collectSTxRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if a == nil && len(txs) == 0 {
		return nil, ErrNotFound
	}

	ch, err := fn(a, txs)
	if err != nil {
		return nil, err
	}
	for _, d := range ch.Delete {
		if _, err := tx.Exec(ctx, `DELETE FROM stock_transactions WHERE id = $1 AND couple_id = $2`, d, coupleID); err != nil {
			return nil, err
		}
	}
	for _, t := range ch.Update {
		if _, err := tx.Exec(ctx,
			`UPDATE stock_transactions SET quantity=$3, price=$4, exchange_rate_at_tx=$5, executed_at=$6,
			        avg_price_at_tx=$7, realized_pnl=$8
			 WHERE id = $1 AND couple_id = $2`,
			t.ID, coupleID, t.Quantity, t.Price, t.ExchangeRateAtTx, t.ExecutedAt, t.AvgPriceAtTx, t.RealizedPnL); err != nil {
			return nil, err
		}
	}
	if a != nil {
		if a.Quantity < models.QtyEpsilon {
			a.Quantity = 0
			_, err = tx.Exec(ctx, `DELETE FROM stock_assets WHERE id = $1`, id)
		} else {
			err = updateStockRow(ctx, tx, a)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// Reorder rewrites sort_order for the given stock asset IDs, using their
// position in the slice. The frontend groups holdings by symbol+exchange, so it
// sends every row of a group consecutively — that keeps a group's rows adjacent
// in the stored order. IDs outside the couple are ignored.
func (r *PgStockRepository) Reorder(ctx context.Context, coupleID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for i, id := range ids {
		if _, err := tx.Exec(ctx,
			`UPDATE stock_assets SET sort_order = $1 WHERE id = $2 AND couple_id = $3`,
			i, id, coupleID); err != nil {
			return fmt.Errorf("reorder stock %s: %w", id, err)
		}
	}
	return tx.Commit(ctx)
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

// ListPriceSnapshots는 여러 종목의 시세를 한 번에 가져온다. 종목마다
// GetPriceSnapshot 을 부르면 보유 종목 수만큼 왕복이 생긴다 — 9종목이면
// 원격 DB 기준 2초가 넘는다.
func (r *PgStockRepository) ListPriceSnapshots(ctx context.Context, symbols []string) (map[string]models.PriceSnapshot, error) {
	out := make(map[string]models.PriceSnapshot, len(symbols))
	if len(symbols) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT symbol, exchange, price, currency, change, change_percent, snapshotted_at
		 FROM price_snapshots WHERE symbol = ANY($1)`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s models.PriceSnapshot
		if err := rows.Scan(&s.Symbol, &s.Exchange, &s.Price, &s.Currency,
			&s.Change, &s.ChangePercent, &s.SnapshottedAt); err != nil {
			return nil, err
		}
		out[s.Symbol] = s
	}
	return out, rows.Err()
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
