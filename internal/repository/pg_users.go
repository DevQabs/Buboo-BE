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

// userCols is the column list scanUser reads, in order.
const userCols = `id, couple_id, name, email, COALESCE(google_sub,''), role, avatar_color, is_owner, created_at`

func scanUser(row scanner) (*models.User, error) {
	var u models.User
	if err := row.Scan(&u.ID, &u.CoupleID, &u.Name, &u.Email, &u.GoogleSub, &u.Role, &u.AvatarColor, &u.IsOwner, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// findUser returns nil, nil when no row matches.
func (r *PgUserRepository) findUser(ctx context.Context, where string, arg any) (*models.User, error) {
	u, err := scanUser(r.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE `+where, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (r *PgUserRepository) GetUser(ctx context.Context, userID string) (*models.User, error) {
	u, err := r.findUser(ctx, "id = $1", userID)
	if err == nil && u == nil {
		return nil, fmt.Errorf("user %s not found", userID)
	}
	return u, err
}

func (r *PgUserRepository) ListUsers(ctx context.Context, coupleID string) ([]models.User, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+userCols+` FROM users WHERE couple_id = $1 ORDER BY created_at`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (r *PgUserRepository) GetUserByGoogleSub(ctx context.Context, sub string) (*models.User, error) {
	return r.findUser(ctx, "google_sub = $1", sub)
}

func (r *PgUserRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return r.findUser(ctx, "email = $1", email)
}

func (r *PgUserRepository) CreateUser(ctx context.Context, u *models.User) (*models.User, error) {
	out, err := scanUser(r.db.QueryRow(ctx,
		`INSERT INTO users (id, couple_id, name, email, google_sub, role, avatar_color, is_owner)
		 VALUES ($1, $2, $3, $4, NULLIF($5,''), $6, $7, $8)
		 RETURNING `+userCols,
		u.ID, u.CoupleID, u.Name, u.Email, u.GoogleSub, u.Role, u.AvatarColor, u.IsOwner))
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return out, nil
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
