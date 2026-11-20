package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	start_date, end_date, all_day, is_dday, is_anniversary, dday_label, color,
	created_at, updated_at, reminders`

func scanSchedule(row interface{ Scan(dest ...any) error }) (*models.Schedule, error) {
	var s models.Schedule
	var endDate pgtype.Timestamptz

	if err := row.Scan(
		&s.ID, &s.CoupleID, &s.UserID, &s.Title, &s.Description,
		&s.StartDate, &endDate, &s.AllDay, &s.IsDDay, &s.IsAnniversary, &s.DDayLabel, &s.Color,
		&s.CreatedAt, &s.UpdatedAt, &s.Reminders,
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
	// KST 달 경계: 하루 종일(UTC 자정 = KST 09시)도, 00:30 KST 같은 시간 지정 일정(전날 UTC)도 제 달에 든다
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, models.KST)
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
	if s.Reminders == nil {
		s.Reminders = []string{} // NOT NULL — nil 슬라이스는 NULL 로 간다
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO schedules
		 (id, couple_id, user_id, title, description, start_date, end_date,
		  all_day, is_dday, is_anniversary, dday_label, color, created_at, updated_at, reminders)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		s.ID, s.CoupleID, s.UserID, s.Title, s.Description,
		s.StartDate, s.EndDate, s.AllDay, s.IsDDay, s.IsAnniversary, s.DDayLabel, s.Color,
		s.CreatedAt, s.UpdatedAt, s.Reminders,
	); err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	if err := syncReminders(ctx, tx, s, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *PgScheduleRepository) Update(ctx context.Context, s *models.Schedule) (*models.Schedule, error) {
	s.UpdatedAt = time.Now().UTC()
	if s.Reminders == nil {
		s.Reminders = []string{}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx,
		`UPDATE schedules SET
		 title=$2, description=$3, start_date=$4, end_date=$5,
		 all_day=$6, is_dday=$7, dday_label=$8, color=$9, updated_at=$10, is_anniversary=$11, reminders=$12
		 WHERE id=$1`,
		s.ID, s.Title, s.Description, s.StartDate, s.EndDate,
		s.AllDay, s.IsDDay, s.DDayLabel, s.Color, s.UpdatedAt, s.IsAnniversary, s.Reminders,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("schedule %s not found", s.ID)
	}
	if err := syncReminders(ctx, tx, s, s.UpdatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// syncReminders 는 일정의 미발송 알림을 지우고 now 이후 것만 다시 넣는다(push 설계서 3절).
// 이미 보낸 같은 코드 행이 있으면 새 시각으로 되살린다 — 날짜를 옮기면 다시 알려야 한다.
func syncReminders(ctx context.Context, tx pgx.Tx, s *models.Schedule, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM push_reminders WHERE schedule_id = $1 AND sent_at IS NULL`, s.ID); err != nil {
		return err
	}
	for _, rm := range models.ReminderTimes(s.StartDate, s.AllDay, s.Reminders, now) {
		if _, err := tx.Exec(ctx,
			`INSERT INTO push_reminders (id, schedule_id, due_at, kind) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (schedule_id, kind) DO UPDATE SET due_at = EXCLUDED.due_at, sent_at = NULL`,
			"rem-"+uuid.NewString(), s.ID, rm.Due, rm.Kind); err != nil {
			return fmt.Errorf("push_reminders: %w", err)
		}
	}
	return nil
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
