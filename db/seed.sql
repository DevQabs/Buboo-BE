-- 초기 데이터 시드 — Supabase SQL Editor에서 schema.sql 실행 후 1회 실행

INSERT INTO couples (id, name, monthly_budget, ledger_start_day, currency, created_at)
VALUES ('couple-001', '태국 & 다현', 2500000, 25, 'KRW', '2025-01-01T00:00:00Z')
ON CONFLICT (id) DO NOTHING;

INSERT INTO users (id, couple_id, name, email, role, avatar_color, created_at) VALUES
('user-001', 'couple-001', '김태국', 'taeguk@example.com', 'husband', '#4F46E5', '2025-01-01T00:00:00Z'),
('user-002', 'couple-001', '이다현', 'dahyun@example.com', 'wife',    '#EC4899', '2025-01-01T00:00:00Z')
ON CONFLICT (id) DO NOTHING;
