package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgScheduleRepository struct {
	db *pgxpool.Pool
}

func NewPgScheduleRepository(db *pgxpool.Pool) *PgScheduleRepository {
	return &PgScheduleRepository{db: db}
}

const schCols = `id, couple_id, user_id, title, description,
	start_date, end_date, all_day, is_dday, dday_label, color,
	created_at, updated_at`

func scanSchedule(row interface{ Scan(dest ...any) error }) (*models.Schedule, error) {
	var s models.Schedule
	var endDate pgtype.Timestamptz

	if err := row.Scan(
		&s.ID, &s.CoupleID, &s.UserID, &s.Title, &s.Description,
		&s.StartDate, &endDate, &s.AllDay, &s.IsDDay, &s.DDayLabel, &s.Color,
		&s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if endDate.Valid {
		t := endDate.Time
		s.EndDate = &t
	}
	return &s, nil
}

func (r *PgScheduleRepository) GetByID(ctx context.Context, id string) (*models.Schedule, error) {
	row := r.db.QueryRow(ctx, `SELECT `+schCols+` FROM schedules WHERE id = $1`, id)
	s, err := scanSchedule(row)
	if err != nil {
		return nil, fmt.Errorf("schedule %s not found: %w", id, err)
	}
	return s, nil
}

func (r *PgScheduleRepository) ListByCouple(ctx context.Context, coupleID string) ([]models.Schedule, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+schCols+` FROM schedules WHERE couple_id = $1 ORDER BY start_date`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectScheduleRows(rows)
}

func (r *PgScheduleRepository) ListByMonth(ctx context.Context, coupleID string, year, month int) ([]models.Schedule, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	rows, err := r.db.Query(ctx,
		`SELECT `+schCols+` FROM schedules
		 WHERE couple_id = $1 AND start_date >= $2 AND start_date < $3
		 ORDER BY start_date`,
		coupleID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectScheduleRows(rows)
}

func (r *PgScheduleRepository) Create(ctx context.Context, s *models.Schedule) (*models.Schedule, error) {
	s.ID = "sch-" + uuid.NewString()
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now

	_, err := r.db.Exec(ctx,
		`INSERT INTO schedules
		 (id, couple_id, user_id, title, description, start_date, end_date,
		  all_day, is_dday, dday_label, color, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		s.ID, s.CoupleID, s.UserID, s.Title, s.Description,
		s.StartDate, s.EndDate, s.AllDay, s.IsDDay, s.DDayLabel, s.Color,
		s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	return s, nil
}

func (r *PgScheduleRepository) Update(ctx context.Context, s *models.Schedule) (*models.Schedule, error) {
	s.UpdatedAt = time.Now().UTC()
	tag, err := r.db.Exec(ctx,
		`UPDATE schedules SET
		 title=$2, description=$3, start_date=$4, end_date=$5,
		 all_day=$6, is_dday=$7, dday_label=$8, color=$9, updated_at=$10
		 WHERE id=$1`,
		s.ID, s.Title, s.Description, s.StartDate, s.EndDate,
		s.AllDay, s.IsDDay, s.DDayLabel, s.Color, s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("schedule %s not found", s.ID)
	}
	return s, nil
}

func (r *PgScheduleRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM schedules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("schedule %s not found", id)
	}
	return nil
}

func collectScheduleRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]models.Schedule, error) {
	var result []models.Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *s)
	}
	if result == nil {
		result = []models.Schedule{}
	}
	return result, rows.Err()
}
