package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

var (
	ErrInviteInvalid = errors.New("invalid or expired invite code")
	// ErrUserExists: the joiner's email or Google account is already another
	// user's (users.email and users.google_sub are unique).
	ErrUserExists = errors.New("this email or Google account is already registered")
)

type PgInviteRepository struct {
	db *pgxpool.Pool
}

func NewPgInviteRepository(db *pgxpool.Pool) *PgInviteRepository {
	return &PgInviteRepository{db: db}
}

const inviteCols = `id, couple_id, code, role, preset, nickname, created_by, expires_at, used_at, used_by, created_at`

func (r *PgInviteRepository) GetInviteByCode(ctx context.Context, code string) (*models.Invite, error) {
	return scanInviteRow(r.db.QueryRow(ctx,
		`SELECT `+inviteCols+` FROM couple_invites
		 WHERE code = $1 AND used_at IS NULL AND expires_at > NOW()`, code))
}

func (r *PgInviteRepository) CreateInvite(ctx context.Context, coupleID, createdBy, preset, nickname string) (*models.Invite, error) {
	code, err := generateCode()
	if err != nil {
		return nil, fmt.Errorf("generate invite code: %w", err)
	}
	return scanInviteRow(r.db.QueryRow(ctx,
		`INSERT INTO couple_invites (couple_id, code, preset, nickname, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+inviteCols,
		coupleID, code, preset, nickname, createdBy))
}

// RedeemInvite marks the invite used first, guarded by used_at IS NULL: a
// second concurrent join with the same code waits on the row lock, then finds
// it used and gets ErrInviteInvalid — one code makes exactly one member. The
// couple row lock serialises joins through different codes so two spouses
// never both become "husband".
func (r *PgInviteRepository) RedeemInvite(ctx context.Context, code string, u models.User, fallbackName string) (*models.User, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var inviteID, preset, nickname string
	err = tx.QueryRow(ctx,
		`UPDATE couple_invites SET used_at = NOW()
		 WHERE code = $1 AND used_at IS NULL AND expires_at > NOW()
		 RETURNING id, couple_id, preset, nickname`, code).
		Scan(&inviteID, &u.CoupleID, &preset, &nickname)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInviteInvalid
	}
	if err != nil {
		return nil, err
	}
	levels, ok := models.PresetLevels(preset)
	if !ok {
		return nil, fmt.Errorf("invite %s: unknown preset %q", inviteID, preset)
	}

	if _, err := tx.Exec(ctx, `SELECT 1 FROM couples WHERE id = $1 FOR UPDATE`, u.CoupleID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT role FROM users WHERE couple_id = $1`, u.CoupleID)
	if err != nil {
		return nil, err
	}
	roles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, role := range roles {
		taken[role] = true
	}

	u.Role = models.RoleForPreset(preset, taken)
	u.AvatarColor = models.DefaultAvatarColor(u.Role)
	for _, n := range []string{u.Name, nickname, fallbackName} {
		if n != "" {
			u.Name = n
			break
		}
	}

	created, err := scanUser(tx.QueryRow(ctx,
		`INSERT INTO users (id, couple_id, name, email, google_sub, role, avatar_color, is_owner)
		 VALUES ($1, $2, $3, $4, NULLIF($5,''), $6, $7, FALSE)
		 RETURNING `+userCols,
		u.ID, u.CoupleID, u.Name, u.Email, u.GoogleSub, u.Role, u.AvatarColor))
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrUserExists
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE couple_invites SET used_by = $2 WHERE id = $1`, inviteID, created.ID); err != nil {
		return nil, err
	}
	if err := insertLevels(ctx, tx, created.ID, levels); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *PgInviteRepository) ListInvitesByCouple(ctx context.Context, coupleID string) ([]models.Invite, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+inviteCols+` FROM couple_invites
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

func scanInviteRow(row scanner) (*models.Invite, error) {
	var inv models.Invite
	if err := row.Scan(
		&inv.ID, &inv.CoupleID, &inv.Code, &inv.Role, &inv.Preset, &inv.Nickname, &inv.CreatedBy,
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
