package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgUserRepository struct {
	db *pgxpool.Pool
}

func NewPgUserRepository(db *pgxpool.Pool) *PgUserRepository {
	return &PgUserRepository{db: db}
}

func (r *PgUserRepository) GetUser(ctx context.Context, userID string) (*models.User, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, couple_id, name, email, role, avatar_color, created_at
		 FROM users WHERE id = $1`, userID)
	var u models.User
	var coupleID string
	if err := row.Scan(&u.ID, &coupleID, &u.Name, &u.Email, &u.Role, &u.AvatarColor, &u.CreatedAt); err != nil {
		return nil, fmt.Errorf("user %s not found: %w", userID, err)
	}
	return &u, nil
}

func (r *PgUserRepository) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, name, email, role, avatar_color, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.AvatarColor, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *PgUserRepository) GetCouple(ctx context.Context, coupleID string) (*models.Couple, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, name, monthly_budget, ledger_start_day, currency, created_at
		 FROM couples WHERE id = $1`, coupleID)
	var c models.Couple
	if err := row.Scan(&c.ID, &c.Name, &c.MonthlyBudget, &c.LedgerStartDay, &c.Currency, &c.CreatedAt); err != nil {
		return nil, fmt.Errorf("couple %s not found: %w", coupleID, err)
	}
	return &c, nil
}

func (r *PgUserRepository) UpdateCouple(ctx context.Context, coupleID string, monthlyBudget int64, ledgerStartDay int) (*models.Couple, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE couples
		 SET monthly_budget = $2, ledger_start_day = $3
		 WHERE id = $1
		 RETURNING id, name, monthly_budget, ledger_start_day, currency, created_at`,
		coupleID, monthlyBudget, ledgerStartDay)
	var c models.Couple
	if err := row.Scan(&c.ID, &c.Name, &c.MonthlyBudget, &c.LedgerStartDay, &c.Currency, &c.CreatedAt); err != nil {
		return nil, fmt.Errorf("couple %s not found: %w", coupleID, err)
	}
	return &c, nil
}
