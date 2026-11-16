-- 4단계: 일정 기념일(D+로 계속 세기). 설계서 7절 D-day 규칙.
-- 실행: Supabase SQL Editor. 여러 번 실행해도 안전하다.
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS is_anniversary BOOLEAN NOT NULL DEFAULT FALSE;

-- 확인: 한 줄, data_type=boolean, column_default=false
-- SELECT column_name, data_type, column_default
--   FROM information_schema.columns
--  WHERE table_name = 'schedules' AND column_name = 'is_anniversary';
