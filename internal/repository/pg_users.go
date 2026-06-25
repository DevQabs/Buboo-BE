package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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
		`SELECT id, couple_id, name, email, COALESCE(google_sub,''), role, avatar_color, created_at
		 FROM users WHERE id = $1`, userID)
	var u models.User
	if err := row.Scan(&u.ID, &u.CoupleID, &u.Name, &u.Email, &u.GoogleSub, &u.Role, &u.AvatarColor, &u.CreatedAt); err != nil {
		return nil, fmt.Errorf("user %s not found: %w", userID, err)
	}
	return &u, nil
}

func (r *PgUserRepository) ListUsers(ctx context.Context, coupleID string) ([]models.User, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, couple_id, name, email, COALESCE(google_sub,''), role, avatar_color, created_at
		 FROM users WHERE couple_id = $1 ORDER BY created_at`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.CoupleID, &u.Name, &u.Email, &u.GoogleSub, &u.Role, &u.AvatarColor, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *PgUserRepository) GetUserByGoogleSub(ctx context.Context, sub string) (*models.User, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, couple_id, name, email, COALESCE(google_sub,''), role, avatar_color, created_at
		 FROM users WHERE google_sub = $1`, sub)
	var u models.User
	if err := row.Scan(&u.ID, &u.CoupleID, &u.Name, &u.Email, &u.GoogleSub, &u.Role, &u.AvatarColor, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *PgUserRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, couple_id, name, email, COALESCE(google_sub,''), role, avatar_color, created_at
		 FROM users WHERE email = $1`, email)
	var u models.User
	if err := row.Scan(&u.ID, &u.CoupleID, &u.Name, &u.Email, &u.GoogleSub, &u.Role, &u.AvatarColor, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *PgUserRepository) CreateUser(ctx context.Context, u *models.User) (*models.User, error) {
	row := r.db.QueryRow(ctx,
		`INSERT INTO users (id, couple_id, name, email, google_sub, role, avatar_color)
		 VALUES ($1, $2, $3, $4, NULLIF($5,''), $6, $7)
		 RETURNING id, couple_id, name, email, COALESCE(google_sub,''), role, avatar_color, created_at`,
		u.ID, u.CoupleID, u.Name, u.Email, u.GoogleSub, u.Role, u.AvatarColor)
	var out models.User
	if err := row.Scan(&out.ID, &out.CoupleID, &out.Name, &out.Email, &out.GoogleSub, &out.Role, &out.AvatarColor, &out.CreatedAt); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &out, nil
}

func (r *PgUserRepository) CreateCouple(ctx context.Context, c *models.Couple) (*models.Couple, error) {
	row := r.db.QueryRow(ctx,
		`INSERT INTO couples (id, name, monthly_budget, ledger_start_day, currency)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, monthly_budget, ledger_start_day, currency, created_at`,
		c.ID, c.Name, c.MonthlyBudget, c.LedgerStartDay, c.Currency)
	var out models.Couple
	if err := row.Scan(&out.ID, &out.Name, &out.MonthlyBudget, &out.LedgerStartDay, &out.Currency, &out.CreatedAt); err != nil {
		return nil, fmt.Errorf("create couple: %w", err)
	}
	return &out, nil
}

func (r *PgUserRepository) UpdateUserGoogleSub(ctx context.Context, userID, sub string) error {
	_, err := r.db.Exec(ctx, `UPDATE users SET google_sub = $2 WHERE id = $1`, userID, sub)
	return err
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
