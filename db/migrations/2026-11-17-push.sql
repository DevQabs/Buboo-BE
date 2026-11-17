-- 우리집 웹 푸시 마이그레이션 (설계서 2026-11-17-push-design.md 2절).
-- 실행: Supabase SQL Editor 에서 한 번에. 여러 번 실행해도 결과가 같다.

-- 1. 기기별 구독. endpoint 하나 = 브라우저 하나.
CREATE TABLE IF NOT EXISTS push_subscriptions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  endpoint TEXT NOT NULL UNIQUE,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_push_subs_user ON push_subscriptions(user_id);

-- 2. 일정 알림 시점 코드(at, m10, h1, d0_9, d1_9). 빈 배열 = 알림 없음.
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS reminders TEXT[] NOT NULL DEFAULT '{}';

-- 3. 보낼 일정 알림. 일정 저장 때 미발송 행을 지우고 다시 넣는다. 일정을 지우면 같이 지워진다.
CREATE TABLE IF NOT EXISTS push_reminders (
  id TEXT PRIMARY KEY,
  schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
  due_at TIMESTAMPTZ NOT NULL,
  kind TEXT NOT NULL,
  sent_at TIMESTAMPTZ,
  UNIQUE (schedule_id, kind)
);
CREATE INDEX IF NOT EXISTS idx_push_reminders_due ON push_reminders(due_at) WHERE sent_at IS NULL;

-- 4. Supabase 자동 API(anon 키)로는 못 읽게 RLS 를 켠다. 정책이 없으니 API 접근은 모두 막히고,
--    백엔드(postgres 소유자 연결)와 pg_cron 은 소유자라 영향 없다.
ALTER TABLE push_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE push_reminders ENABLE ROW LEVEL SECURITY;

-- 확인: 두 줄(push_reminders, push_subscriptions)과 reminders 컬럼 한 줄
-- SELECT table_name FROM information_schema.tables WHERE table_name IN ('push_subscriptions','push_reminders') ORDER BY 1;
-- SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'schedules' AND column_name = 'reminders';
