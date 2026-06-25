package repository

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgInviteRepository struct {
	db *pgxpool.Pool
}

func NewPgInviteRepository(db *pgxpool.Pool) *PgInviteRepository {
	return &PgInviteRepository{db: db}
}

func (r *PgInviteRepository) GetInviteByCode(ctx context.Context, code string) (*models.Invite, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, couple_id, code, role, created_by, expires_at, used_at, used_by, created_at
		 FROM couple_invites
		 WHERE code = $1 AND used_at IS NULL AND expires_at > NOW()`, code)
	return scanInvite(row)
}

func (r *PgInviteRepository) CreateInvite(ctx context.Context, coupleID, createdBy string, role *string) (*models.Invite, error) {
	code, err := generateCode()
	if err != nil {
		return nil, fmt.Errorf("generate invite code: %w", err)
	}
	row := r.db.QueryRow(ctx,
		`INSERT INTO couple_invites (couple_id, code, role, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, couple_id, code, role, created_by, expires_at, used_at, used_by, created_at`,
		coupleID, code, role, createdBy)
	return scanInvite(row)
}

func (r *PgInviteRepository) MarkInviteUsed(ctx context.Context, inviteID, userID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE couple_invites SET used_at = NOW(), used_by = $2 WHERE id = $1`,
		inviteID, userID)
	return err
}

func (r *PgInviteRepository) ListInvitesByCouple(ctx context.Context, coupleID string) ([]models.Invite, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, couple_id, code, role, created_by, expires_at, used_at, used_by, created_at
		 FROM couple_invites
		 WHERE couple_id = $1 AND used_at IS NULL AND expires_at > NOW()
		 ORDER BY created_at DESC`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []models.Invite
	for rows.Next() {
		inv, err := scanInviteRow(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, *inv)
	}
	return invites, rows.Err()
}

// ─────────────────────────────────────────────
//  helpers

type scanner interface {
	Scan(dest ...any) error
}

func scanInvite(row scanner) (*models.Invite, error) {
	return scanInviteRow(row)
}

func scanInviteRow(row scanner) (*models.Invite, error) {
	var inv models.Invite
	if err := row.Scan(
		&inv.ID, &inv.CoupleID, &inv.Code, &inv.Role, &inv.CreatedBy,
		&inv.ExpiresAt, &inv.UsedAt, &inv.UsedBy, &inv.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan invite: %w", err)
	}
	return &inv, nil
}

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I confusion

func generateCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 8)
	for i, v := range b {
		out[i] = codeChars[int(v)%len(codeChars)]
	}
	return string(out), nil
}
