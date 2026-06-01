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

type PgTransactionRepository struct {
	db *pgxpool.Pool
}

func NewPgTransactionRepository(db *pgxpool.Pool) *PgTransactionRepository {
	return &PgTransactionRepository{db: db}
}

const txSelectCols = `id, couple_id, user_id, type, amount, currency, category,
	subcategory, title, memo, date, payment_method, is_fixed, tags,
	location, fixed_expense_id, saving_link, created_at, updated_at`

func scanTx(row interface {
	Scan(dest ...any) error
}) (*models.Transaction, error) {
	var tx models.Transaction
	var locJSON, slJSON []byte
	var fixedExpID pgtype.Text

	if err := row.Scan(
		&tx.ID, &tx.CoupleID, &tx.UserID, &tx.Type, &tx.Amount, &tx.Currency,
		&tx.Category, &tx.Subcategory, &tx.Title, &tx.Memo, &tx.Date,
		&tx.PaymentMethod, &tx.IsFixed, &tx.Tags,
		&locJSON, &fixedExpID, &slJSON,
		&tx.CreatedAt, &tx.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if fixedExpID.Valid {
		tx.FixedExpenseID = &fixedExpID.String
	}
	if locJSON != nil {
		var loc models.Location
		json.Unmarshal(locJSON, &loc)
		tx.Location = &loc
	}
	if slJSON != nil {
		var sl models.SavingLink
		json.Unmarshal(slJSON, &sl)
		tx.SavingLink = &sl
	}
	if tx.Tags == nil {
		tx.Tags = []string{}
	}
	return &tx, nil
}

func (r *PgTransactionRepository) GetByID(ctx context.Context, id string) (*models.Transaction, error) {
	row := r.db.QueryRow(ctx, `SELECT `+txSelectCols+` FROM transactions WHERE id = $1`, id)
	tx, err := scanTx(row)
	if err != nil {
		return nil, fmt.Errorf("transaction %s not found: %w", id, err)
	}
	return tx, nil
}

func (r *PgTransactionRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.Transaction, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+txSelectCols+` FROM transactions WHERE couple_id = $1 ORDER BY date DESC`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxRows(rows)
}

func (r *PgTransactionRepository) ListByMonth(ctx context.Context, coupleID string, year, month int) ([]models.Transaction, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	rows, err := r.db.Query(ctx,
		`SELECT `+txSelectCols+` FROM transactions
		 WHERE couple_id = $1 AND date >= $2 AND date < $3
		 ORDER BY date DESC`, coupleID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxRows(rows)
}

func (r *PgTransactionRepository) ListByUser(ctx context.Context, coupleID, userID string) ([]models.Transaction, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+txSelectCols+` FROM transactions
		 WHERE couple_id = $1 AND user_id = $2 ORDER BY date DESC`, coupleID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTxRows(rows)
}

func (r *PgTransactionRepository) Create(ctx context.Context, tx *models.Transaction) (*models.Transaction, error) {
	tx.ID = "txn-" + uuid.NewString()
	now := time.Now().UTC()
	tx.CreatedAt = now
	tx.UpdatedAt = now

	locJSON, _ := json.Marshal(tx.Location)
	slJSON, _ := json.Marshal(tx.SavingLink)
	if tx.Location == nil {
		locJSON = nil
	}
	if tx.SavingLink == nil {
		slJSON = nil
	}
	if tx.Tags == nil {
		tx.Tags = []string{}
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO transactions
		 (id, couple_id, user_id, type, amount, currency, category, subcategory,
		  title, memo, date, payment_method, is_fixed, tags, location,
		  fixed_expense_id, saving_link, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		tx.ID, tx.CoupleID, tx.UserID, tx.Type, tx.Amount, tx.Currency,
		tx.Category, tx.Subcategory, tx.Title, tx.Memo, tx.Date,
		tx.PaymentMethod, tx.IsFixed, tx.Tags, locJSON,
		tx.FixedExpenseID, slJSON, tx.CreatedAt, tx.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}
	return tx, nil
}

func (r *PgTransactionRepository) Update(ctx context.Context, tx *models.Transaction) (*models.Transaction, error) {
	tx.UpdatedAt = time.Now().UTC()

	locJSON, _ := json.Marshal(tx.Location)
	slJSON, _ := json.Marshal(tx.SavingLink)
	if tx.Location == nil {
		locJSON = nil
	}
	if tx.SavingLink == nil {
		slJSON = nil
	}
	if tx.Tags == nil {
		tx.Tags = []string{}
	}

	tag, err := r.db.Exec(ctx,
		`UPDATE transactions SET
		 user_id=$2, type=$3, amount=$4, currency=$5, category=$6, subcategory=$7,
		 title=$8, memo=$9, date=$10, payment_method=$11, is_fixed=$12,
		 tags=$13, location=$14, fixed_expense_id=$15, saving_link=$16, updated_at=$17
		 WHERE id=$1`,
		tx.ID, tx.UserID, tx.Type, tx.Amount, tx.Currency, tx.Category, tx.Subcategory,
		tx.Title, tx.Memo, tx.Date, tx.PaymentMethod, tx.IsFixed,
		tx.Tags, locJSON, tx.FixedExpenseID, slJSON, tx.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("transaction %s not found", tx.ID)
	}
	return tx, nil
}

func (r *PgTransactionRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM transactions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("transaction %s not found", id)
	}
	return nil
}

func (r *PgTransactionRepository) MonthlySummary(ctx context.Context, coupleID string, year, month int) (*models.MonthlySummary, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return r.SummaryByDateRange(ctx, coupleID, start, end)
}

func (r *PgTransactionRepository) SummaryByDateRange(ctx context.Context, coupleID string, start, end time.Time) (*models.MonthlySummary, error) {
	row := r.db.QueryRow(ctx,
		`SELECT
		   COALESCE(SUM(amount) FILTER (WHERE type = 'income'),  0),
		   COALESCE(SUM(amount) FILTER (WHERE type = 'expense'), 0)
		 FROM transactions
		 WHERE couple_id = $1 AND date >= $2 AND date < $3`,
		coupleID, start, end)
	var income, expense int64
	if err := row.Scan(&income, &expense); err != nil {
		return nil, err
	}
	return &models.MonthlySummary{
		Year:         start.Year(),
		Month:        int(start.Month()),
		TotalIncome:  income,
		TotalExpense: expense,
		Balance:      income - expense,
	}, nil
}

func collectTxRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]models.Transaction, error) {
	var result []models.Transaction
	for rows.Next() {
		tx, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *tx)
	}
	if result == nil {
		result = []models.Transaction{}
	}
	return result, rows.Err()
}
