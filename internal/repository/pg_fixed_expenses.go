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

type PgFixedExpenseRepository struct {
	db *pgxpool.Pool
}

func NewPgFixedExpenseRepository(db *pgxpool.Pool) *PgFixedExpenseRepository {
	return &PgFixedExpenseRepository{db: db}
}

const feCols = `id, couple_id, user_id, owner, kind, title, category,
	amount, currency, cycle, day_of_month, day_of_week,
	is_active, memo, saving_link, created_at, updated_at, deactivated_at`

func scanFE(row interface{ Scan(dest ...any) error }) (*models.FixedExpense, error) {
	var fe models.FixedExpense
	var slJSON []byte
	var dayOfWeek pgtype.Int4
	var deactivatedAt *time.Time

	if err := row.Scan(
		&fe.ID, &fe.CoupleID, &fe.UserID, &fe.Owner, &fe.Kind,
		&fe.Title, &fe.Category, &fe.Amount, &fe.Currency,
		&fe.Cycle, &fe.DayOfMonth, &dayOfWeek,
		&fe.IsActive, &fe.Memo, &slJSON,
		&fe.CreatedAt, &fe.UpdatedAt, &deactivatedAt,
	); err != nil {
		return nil, err
	}
	fe.DeactivatedAt = deactivatedAt
	if dayOfWeek.Valid {
		v := int(dayOfWeek.Int32)
		fe.DayOfWeek = &v
	}
	if slJSON != nil {
		var sl models.SavingLink
		json.Unmarshal(slJSON, &sl)
		fe.SavingLink = &sl
	}
	return &fe, nil
}

func (r *PgFixedExpenseRepository) GetByID(ctx context.Context, id string) (*models.FixedExpense, error) {
	row := r.db.QueryRow(ctx, `SELECT `+feCols+` FROM fixed_expenses WHERE id = $1`, id)
	fe, err := scanFE(row)
	if err != nil {
		return nil, fmt.Errorf("fixed expense %s not found: %w", id, err)
	}
	return fe, nil
}

func (r *PgFixedExpenseRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.FixedExpense, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+feCols+` FROM fixed_expenses WHERE couple_id = $1 ORDER BY created_at`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.FixedExpense
	for rows.Next() {
		fe, err := scanFE(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *fe)
	}
	if result == nil {
		result = []models.FixedExpense{}
	}
	return result, rows.Err()
}

func (r *PgFixedExpenseRepository) Create(ctx context.Context, fe *models.FixedExpense) (*models.FixedExpense, error) {
	fe.ID = "fe-" + uuid.NewString()
	now := time.Now().UTC()
	fe.CreatedAt = now
	fe.UpdatedAt = now
	fe.IsActive = true
	if fe.Currency == "" {
		fe.Currency = "KRW"
	}

	var slParam interface{}
	if fe.SavingLink != nil {
		b, _ := json.Marshal(fe.SavingLink)
		slParam = string(b)
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO fixed_expenses
		 (id, couple_id, user_id, owner, kind, title, category, amount, currency,
		  cycle, day_of_month, day_of_week, is_active, memo, saving_link, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		fe.ID, fe.CoupleID, fe.UserID, string(fe.Owner), string(fe.Kind),
		fe.Title, fe.Category, fe.Amount, fe.Currency,
		string(fe.Cycle), fe.DayOfMonth, fe.DayOfWeek,
		fe.IsActive, fe.Memo, slParam, fe.CreatedAt, fe.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create fixed expense: %w", err)
	}
	return fe, nil
}

func (r *PgFixedExpenseRepository) Update(ctx context.Context, fe *models.FixedExpense) (*models.FixedExpense, error) {
	fe.UpdatedAt = time.Now().UTC()

	var slParam interface{}
	if fe.SavingLink != nil {
		b, _ := json.Marshal(fe.SavingLink)
		slParam = string(b)
	}

	tag, err := r.db.Exec(ctx,
		`UPDATE fixed_expenses SET
		 owner=$2, kind=$3, title=$4, category=$5, amount=$6,
		 cycle=$7, day_of_month=$8, day_of_week=$9, is_active=$10,
		 memo=$11, saving_link=$12, updated_at=$13, deactivated_at=$14
		 WHERE id=$1`,
		fe.ID, string(fe.Owner), string(fe.Kind), fe.Title, fe.Category,
		fe.Amount, string(fe.Cycle), fe.DayOfMonth, fe.DayOfWeek,
		fe.IsActive, fe.Memo, slParam, fe.UpdatedAt, fe.DeactivatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("fixed expense %s not found", fe.ID)
	}
	return fe, nil
}

func (r *PgFixedExpenseRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM fixed_expenses WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("fixed expense %s not found", id)
	}
	return nil
}
