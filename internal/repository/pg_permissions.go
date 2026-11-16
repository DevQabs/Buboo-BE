package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgPermissionRepository struct{ db *pgxpool.Pool }

func NewPgPermissionRepository(db *pgxpool.Pool) *PgPermissionRepository {
	return &PgPermissionRepository{db: db}
}

func (r *PgPermissionRepository) Get(ctx context.Context, userID, coupleID string) (*models.Permissions, error) {
	rows, err := r.db.Query(ctx,
		`SELECT u.is_owner, mp.menu, mp.level
		 FROM users u LEFT JOIN member_permissions mp ON mp.user_id = u.id
		 WHERE u.id = $1 AND u.couple_id = $2`, userID, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var p *models.Permissions
	for rows.Next() {
		var owner bool
		var menu, level *string
		if err := rows.Scan(&owner, &menu, &level); err != nil {
			return nil, err
		}
		if p == nil {
			p = &models.Permissions{IsOwner: owner, Menus: map[string]string{}}
		}
		if menu != nil {
			p.Menus[*menu] = *level
		}
	}
	return p, rows.Err()
}

func (r *PgPermissionRepository) ListByCouple(ctx context.Context, coupleID string) (map[string]map[string]string, error) {
	rows, err := r.db.Query(ctx,
		`SELECT mp.user_id, mp.menu, mp.level
		 FROM member_permissions mp JOIN users u ON u.id = mp.user_id
		 WHERE u.couple_id = $1`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]map[string]string{}
	for rows.Next() {
		var userID, menu, level string
		if err := rows.Scan(&userID, &menu, &level); err != nil {
			return nil, err
		}
		if out[userID] == nil {
			out[userID] = map[string]string{}
		}
		out[userID][menu] = level
	}
	return out, rows.Err()
}

func (r *PgPermissionRepository) Replace(ctx context.Context, userID string, levels map[string]string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM member_permissions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if err := insertLevels(ctx, tx, userID, levels); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgPermissionRepository) RemoveMember(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM users WHERE id = $1 AND NOT is_owner`, userID)
	return err
}

// insertLevels writes one row per non-hidden menu inside tx.
func insertLevels(ctx context.Context, tx pgx.Tx, userID string, levels map[string]string) error {
	for menu, level := range levels {
		if level == models.LevelHidden {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO member_permissions (user_id, menu, level) VALUES ($1, $2, $3)`,
			userID, menu, level); err != nil {
			return err
		}
	}
	return nil
}
