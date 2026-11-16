-- 우리집 3단계: R2 저장소 사용량 기록 · 아기 탭 (설계서 5·6절)
-- Supabase SQL Editor 에서 한 번 실행한다. 다시 실행해도 안전하다(추가만 함).
BEGIN;

-- 업로드 주소를 발급할 때 요청 크기로 기록한다. 버킷 합계가 STORAGE_LIMIT_BYTES 를 넘지 않게 하는 근거.
CREATE TABLE IF NOT EXISTS storage_objects (
    object_key TEXT PRIMARY KEY,           -- {couple_id}/{purpose}/{uuid}.{ext}
    couple_id  TEXT NOT NULL,
    purpose    TEXT NOT NULL,              -- diary|baby
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS babies (
    couple_id  TEXT PRIMARY KEY,           -- 그룹당 한 명
    name       TEXT NOT NULL,
    birth_date DATE NOT NULL,
    sex        TEXT NOT NULL CHECK (sex IN ('male', 'female')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS baby_moments (
    id         TEXT PRIMARY KEY,
    couple_id  TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    date       DATE NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_baby_moments_couple_date ON baby_moments(couple_id, date DESC);

CREATE TABLE IF NOT EXISTS baby_media (
    id           TEXT PRIMARY KEY,
    moment_id    TEXT NOT NULL REFERENCES baby_moments(id) ON DELETE CASCADE,
    couple_id    TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('image', 'video')),
    object_key   TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes   BIGINT NOT NULL,
    sort_order   INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_baby_media_moment ON baby_media(moment_id);
-- 한 객체를 두 기록이 가리키면 한쪽 삭제가 다른 쪽을 깨뜨린다
CREATE UNIQUE INDEX IF NOT EXISTS idx_baby_media_key ON baby_media(object_key);

CREATE TABLE IF NOT EXISTS baby_growth (
    id          TEXT PRIMARY KEY,
    couple_id   TEXT NOT NULL,
    measured_on DATE NOT NULL,
    height_cm   NUMERIC(5,1),
    weight_kg   NUMERIC(5,2),
    head_cm     NUMERIC(5,1),
    memo        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_baby_growth_couple ON baby_growth(couple_id, measured_on);

COMMIT;

-- 확인: 다섯 테이블이 보여야 한다
-- SELECT table_name FROM information_schema.tables
-- WHERE table_schema = 'public'
--   AND table_name IN ('storage_objects', 'babies', 'baby_moments', 'baby_media', 'baby_growth');
