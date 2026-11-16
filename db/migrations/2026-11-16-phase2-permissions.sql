-- 우리집 2단계: 메뉴 권한 · 초대 묶음
-- Supabase SQL Editor 에서 한 번 실행한다. 백엔드 배포보다 먼저.
-- 기존 구성원 백필은 member_permissions 가 비어 있을 때(첫 실행)만 돈다.

BEGIN;

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_owner BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX IF NOT EXISTS users_one_owner_per_couple ON users (couple_id) WHERE is_owner;

CREATE TABLE IF NOT EXISTS member_permissions (
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    menu    TEXT NOT NULL,  -- baby_photo|baby_growth|ledger|schedule|diary|assets|roadmap
    level   TEXT NOT NULL CHECK (level IN ('hidden', 'view', 'edit')),
    PRIMARY KEY (user_id, menu)
);

ALTER TABLE couple_invites ADD COLUMN IF NOT EXISTS preset   TEXT NOT NULL DEFAULT 'spouse';
ALTER TABLE couple_invites ADD COLUMN IF NOT EXISTS nickname TEXT NOT NULL DEFAULT '';

-- 그룹마다 가장 먼저 가입한 사람이 owner (이미 owner 가 있는 그룹은 건너뜀)
UPDATE users u SET is_owner = TRUE
WHERE u.id = (
        SELECT u2.id FROM users u2
        WHERE u2.couple_id = u.couple_id
        ORDER BY u2.created_at, u2.id
        LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM users o WHERE o.couple_id = u.couple_id AND o.is_owner);

-- 나머지 기존 구성원은 7개 메뉴 전부 edit (지금과 같은 경험)
INSERT INTO member_permissions (user_id, menu, level)
SELECT u.id, m.menu, 'edit'
FROM users u
CROSS JOIN (VALUES ('baby_photo'), ('baby_growth'), ('ledger'), ('schedule'),
                   ('diary'), ('assets'), ('roadmap')) AS m (menu)
WHERE NOT u.is_owner
  AND NOT EXISTS (SELECT 1 FROM member_permissions)
ON CONFLICT (user_id, menu) DO NOTHING;

COMMIT;

-- 확인: 그룹마다 owner 1명(menus 0), 나머지는 menus 7
-- SELECT u.couple_id, u.name, u.role, u.is_owner, COUNT(mp.menu) AS menus
-- FROM users u LEFT JOIN member_permissions mp ON mp.user_id = u.id
-- GROUP BY u.id ORDER BY u.couple_id, u.created_at;
