package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgPushRepository struct{ db *pgxpool.Pool }

func NewPgPushRepository(db *pgxpool.Pool) *PgPushRepository { return &PgPushRepository{db: db} }

func (r *PgPushRepository) SaveSubscription(ctx context.Context, userID, userAgent string, s models.PushSubscription) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (endpoint) DO UPDATE SET
		   user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh,
		   auth = EXCLUDED.auth, user_agent = EXCLUDED.user_agent`,
		"push-"+uuid.NewString(), userID, s.Endpoint, s.Keys.P256dh, s.Keys.Auth, userAgent)
	return err
}

func (r *PgPushRepository) DeleteSubscription(ctx context.Context, userID, endpoint string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2`, userID, endpoint)
	return err
}

func (r *PgPushRepository) DeleteEndpoint(ctx context.Context, endpoint string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint)
	return err
}

func (r *PgPushRepository) ListSubscriptions(ctx context.Context, userIDs []string) ([]models.PushSubscription, error) {
	rows, err := r.db.Query(ctx,
		`SELECT endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = ANY($1) ORDER BY created_at`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PushSubscription
	for rows.Next() {
		var s models.PushSubscription
		if err := rows.Scan(&s.Endpoint, &s.Keys.P256dh, &s.Keys.Auth); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ClaimDue 는 한 SQL 문에서 due 행을 잡고(SKIP LOCKED — 동시 호출이 같은 행을 못 잡는다) sent_at 을 찍는다.
// 보내기 전에 찍으므로 발송이 실패해도 다시 잡히지 않는다(한 번 놓치면 끝, push 설계서 4절).
func (r *PgPushRepository) ClaimDue(ctx context.Context, now time.Time, limit int) ([]models.DueReminder, error) {
	rows, err := r.db.Query(ctx,
		`WITH due AS (
		   SELECT id FROM push_reminders
		    WHERE due_at <= $1 AND sent_at IS NULL
		    ORDER BY due_at
		    LIMIT $2
		    FOR UPDATE SKIP LOCKED
		 )
		 UPDATE push_reminders p SET sent_at = $1
		   FROM due, schedules s
		  WHERE p.id = due.id AND s.id = p.schedule_id
		 RETURNING p.id, p.schedule_id, s.couple_id, s.title, p.kind, p.due_at`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DueReminder
	for rows.Next() {
		var d models.DueReminder
		if err := rows.Scan(&d.ID, &d.ScheduleID, &d.CoupleID, &d.Title, &d.Kind, &d.DueAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
