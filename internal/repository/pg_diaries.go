package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgDiaryRepository struct {
	db *pgxpool.Pool
}

func NewPgDiaryRepository(db *pgxpool.Pool) *PgDiaryRepository {
	return &PgDiaryRepository{db: db}
}

const diaryCols = `id, couple_id, user_id, date, content, photos, mood, created_at, updated_at`

func scanDiary(row interface{ Scan(dest ...any) error }) (*models.DiaryEntry, error) {
	var d models.DiaryEntry
	if err := row.Scan(
		&d.ID, &d.CoupleID, &d.UserID, &d.Date,
		&d.Content, &d.Photos, &d.Mood,
		&d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if d.Photos == nil {
		d.Photos = []string{}
	}
	return &d, nil
}

func (r *PgDiaryRepository) GetByID(ctx context.Context, id string) (*models.DiaryEntry, error) {
	row := r.db.QueryRow(ctx, `SELECT `+diaryCols+` FROM diaries WHERE id = $1`, id)
	d, err := scanDiary(row)
	if err != nil {
		return nil, fmt.Errorf("diary %s not found: %w", id, err)
	}
	return d, nil
}

func (r *PgDiaryRepository) GetByDate(ctx context.Context, coupleID, date string) (*models.DiaryEntry, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+diaryCols+` FROM diaries WHERE couple_id = $1 AND date = $2`,
		coupleID, date)
	d, err := scanDiary(row)
	if err != nil {
		// not found is OK — return nil, nil
		return nil, nil
	}
	return d, nil
}

func (r *PgDiaryRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.DiaryEntry, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+diaryCols+` FROM diaries WHERE couple_id = $1 ORDER BY date DESC`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDiaryRows(rows)
}

func (r *PgDiaryRepository) ListByMonth(ctx context.Context, coupleID string, year, month int) ([]models.DiaryEntry, error) {
	prefix := fmt.Sprintf("%04d-%02d", year, month)
	rows, err := r.db.Query(ctx,
		`SELECT `+diaryCols+` FROM diaries
		 WHERE couple_id = $1 AND date LIKE $2
		 ORDER BY date DESC`,
		coupleID, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDiaryRows(rows)
}

func (r *PgDiaryRepository) Create(ctx context.Context, d *models.DiaryEntry) (*models.DiaryEntry, error) {
	d.ID = "diary-" + uuid.NewString()
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	if d.Photos == nil {
		d.Photos = []string{}
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO diaries (id, couple_id, user_id, date, content, photos, mood, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		d.ID, d.CoupleID, d.UserID, d.Date,
		d.Content, d.Photos, d.Mood,
		d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create diary: %w", err)
	}
	return d, nil
}

func (r *PgDiaryRepository) Update(ctx context.Context, d *models.DiaryEntry) (*models.DiaryEntry, error) {
	d.UpdatedAt = time.Now().UTC()
	tag, err := r.db.Exec(ctx,
		`UPDATE diaries SET content=$2, mood=$3, updated_at=$4 WHERE id=$1`,
		d.ID, d.Content, d.Mood, d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("diary %s not found", d.ID)
	}
	return d, nil
}

// ErrPhotoInUse: the key is already attached to another diary. Deleting that
// diary would remove the shared R2 object, so a key lives in one diary only.
var ErrPhotoInUse = errors.New("이미 사용 중인 사진입니다")

// AddPhoto appends key unless some diary already has it. Re-adding to the same
// diary is a no-op (nil); another diary holding it gives ErrPhotoInUse.
// ponytail: two concurrent attaches to different diaries can both pass NOT EXISTS (READ COMMITTED) — rare for two people; add a diary_photos(key UNIQUE) table if it shows up.
func (r *PgDiaryRepository) AddPhoto(ctx context.Context, id, key string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE diaries SET photos = array_append(photos, $2), updated_at = NOW()
		 WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM diaries WHERE $2 = ANY(photos))`,
		id, key,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var holder string
	err = r.db.QueryRow(ctx, `SELECT id FROM diaries WHERE $1 = ANY(photos) LIMIT 1`, key).Scan(&holder)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("diary %s not found", id)
	case err != nil:
		return err
	case holder == id:
		return nil
	default:
		return ErrPhotoInUse
	}
}

func (r *PgDiaryRepository) DeletePhoto(ctx context.Context, id, photoURL string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE diaries SET photos = array_remove(photos, $2), updated_at = NOW() WHERE id = $1`,
		id, photoURL,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("diary %s not found", id)
	}
	return nil
}

func (r *PgDiaryRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM diaries WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("diary %s not found", id)
	}
	return nil
}

func collectDiaryRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]models.DiaryEntry, error) {
	var result []models.DiaryEntry
	for rows.Next() {
		d, err := scanDiary(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *d)
	}
	if result == nil {
		result = []models.DiaryEntry{}
	}
	return result, rows.Err()
}
