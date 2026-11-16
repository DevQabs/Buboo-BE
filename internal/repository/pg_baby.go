package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

// ErrMediaKeyUsed: 이 파일 키가 이미 다른 기록에 붙어 있다(idx_baby_media_key).
var ErrMediaKeyUsed = errors.New("이미 다른 기록에 붙은 파일입니다. 다시 올려 주세요")

type PgBabyRepository struct{ db *pgxpool.Pool }

func NewPgBabyRepository(db *pgxpool.Pool) *PgBabyRepository { return &PgBabyRepository{db: db} }

// DATE·NUMERIC 은 ::text·::float8 로 읽는다(simple protocol 에서 string·float64 로 바로 받기 위해).
const (
	momentCols = `id, user_id, date::text, content, created_at`
	mediaCols  = `id, moment_id, kind, object_key, content_type, size_bytes, sort_order`
	growthCols = `id, measured_on::text, height_cm::float8, weight_kg::float8, head_cm::float8, memo`
)

func (r *PgBabyRepository) Get(ctx context.Context, coupleID string) (*models.Baby, error) {
	var b models.Baby
	err := r.db.QueryRow(ctx,
		`SELECT name, birth_date::text, sex FROM babies WHERE couple_id = $1`, coupleID,
	).Scan(&b.Name, &b.BirthDate, &b.Sex)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *PgBabyRepository) Put(ctx context.Context, coupleID string, b models.Baby) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO babies (couple_id, name, birth_date, sex) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (couple_id) DO UPDATE
		 SET name = EXCLUDED.name, birth_date = EXCLUDED.birth_date, sex = EXCLUDED.sex`,
		coupleID, b.Name, b.BirthDate, b.Sex)
	return err
}

func scanMoment(row pgx.Row) (*models.BabyMoment, error) {
	m := models.BabyMoment{Media: []models.BabyMedia{}}
	if err := row.Scan(&m.ID, &m.UserID, &m.Date, &m.Content, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *PgBabyRepository) listMedia(ctx context.Context, where, arg string) ([]models.BabyMedia, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+mediaCols+` FROM baby_media WHERE `+where+` ORDER BY sort_order, created_at`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BabyMedia
	for rows.Next() {
		var md models.BabyMedia
		if err := rows.Scan(&md.ID, &md.MomentID, &md.Kind, &md.Key, &md.ContentType, &md.SizeBytes, &md.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, md)
	}
	return out, rows.Err()
}

func (r *PgBabyRepository) ListMoments(ctx context.Context, coupleID string) ([]models.BabyMoment, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+momentCols+` FROM baby_moments WHERE couple_id = $1 ORDER BY date DESC, created_at DESC`, coupleID)
	if err != nil {
		return nil, err
	}
	out := []models.BabyMoment{}
	idx := map[string]int{}
	for rows.Next() {
		m, err := scanMoment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		idx[m.ID] = len(out)
		out = append(out, *m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// ponytail: 그룹 전체 기록을 한 번에 읽는다 — 수천 건이 되면 날짜 페이지로.
	media, err := r.listMedia(ctx, "couple_id = $1", coupleID)
	if err != nil {
		return nil, err
	}
	for _, md := range media {
		if i, ok := idx[md.MomentID]; ok {
			out[i].Media = append(out[i].Media, md)
		}
	}
	return out, nil
}

func (r *PgBabyRepository) GetMoment(ctx context.Context, id string) (*models.BabyMoment, error) {
	m, err := scanMoment(r.db.QueryRow(ctx, `SELECT `+momentCols+` FROM baby_moments WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("moment %s not found: %w", id, err)
	}
	media, err := r.listMedia(ctx, "moment_id = $1", id)
	if err != nil {
		return nil, err
	}
	if media != nil {
		m.Media = media
	}
	return m, nil
}

// insertMedia fills each item's ID, MomentID and SortOrder and inserts it inside tx.
func insertMedia(ctx context.Context, tx pgx.Tx, coupleID, momentID string, media []models.BabyMedia, start int) error {
	for i := range media {
		md := &media[i]
		md.ID = "media-" + uuid.NewString()
		md.MomentID = momentID
		md.SortOrder = start + i
		if _, err := tx.Exec(ctx,
			`INSERT INTO baby_media (id, moment_id, couple_id, kind, object_key, content_type, size_bytes, sort_order)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			md.ID, momentID, coupleID, md.Kind, md.Key, md.ContentType, md.SizeBytes, md.SortOrder); err != nil {
			if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" &&
				pgErr.ConstraintName == "idx_baby_media_key" {
				return ErrMediaKeyUsed
			}
			return err
		}
	}
	return nil
}

func (r *PgBabyRepository) CreateMoment(ctx context.Context, coupleID string, m *models.BabyMoment) error {
	m.ID = "moment-" + uuid.NewString()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx,
		`INSERT INTO baby_moments (id, couple_id, user_id, date, content) VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		m.ID, coupleID, m.UserID, m.Date, m.Content).Scan(&m.CreatedAt); err != nil {
		return err
	}
	if err := insertMedia(ctx, tx, coupleID, m.ID, m.Media, 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgBabyRepository) UpdateMoment(ctx context.Context, coupleID string, m *models.BabyMoment, add []models.BabyMedia) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE baby_moments SET date = $2, content = $3, updated_at = NOW() WHERE id = $1`,
		m.ID, m.Date, m.Content); err != nil {
		return err
	}
	var next int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(sort_order) + 1, 0) FROM baby_media WHERE moment_id = $1`, m.ID).Scan(&next); err != nil {
		return err
	}
	if err := insertMedia(ctx, tx, coupleID, m.ID, add, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgBabyRepository) DeleteMoment(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM baby_moments WHERE id = $1`, id)
	return err
}

func (r *PgBabyRepository) GetMedia(ctx context.Context, id string) (*models.BabyMedia, error) {
	media, err := r.listMedia(ctx, "id = $1", id)
	if err != nil {
		return nil, err
	}
	if len(media) == 0 {
		return nil, fmt.Errorf("media %s not found", id)
	}
	return &media[0], nil
}

func (r *PgBabyRepository) DeleteMedia(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM baby_media WHERE id = $1`, id)
	return err
}

func (r *PgBabyRepository) ListGrowth(ctx context.Context, coupleID string) ([]models.BabyGrowth, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+growthCols+` FROM baby_growth WHERE couple_id = $1 ORDER BY measured_on, created_at`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.BabyGrowth{}
	for rows.Next() {
		var g models.BabyGrowth
		if err := rows.Scan(&g.ID, &g.MeasuredOn, &g.HeightCm, &g.WeightKg, &g.HeadCm, &g.Memo); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *PgBabyRepository) CreateGrowth(ctx context.Context, coupleID string, g *models.BabyGrowth) error {
	g.ID = "growth-" + uuid.NewString()
	_, err := r.db.Exec(ctx,
		`INSERT INTO baby_growth (id, couple_id, measured_on, height_cm, weight_kg, head_cm, memo)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		g.ID, coupleID, g.MeasuredOn, g.HeightCm, g.WeightKg, g.HeadCm, g.Memo)
	return err
}

func (r *PgBabyRepository) UpdateGrowth(ctx context.Context, g *models.BabyGrowth) error {
	_, err := r.db.Exec(ctx,
		`UPDATE baby_growth SET measured_on = $2, height_cm = $3, weight_kg = $4, head_cm = $5, memo = $6
		 WHERE id = $1`,
		g.ID, g.MeasuredOn, g.HeightCm, g.WeightKg, g.HeadCm, g.Memo)
	return err
}

func (r *PgBabyRepository) DeleteGrowth(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM baby_growth WHERE id = $1`, id)
	return err
}
