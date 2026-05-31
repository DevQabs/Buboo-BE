-- 부부 자산 관리 앱 — PostgreSQL Schema
-- Supabase SQL Editor에서 실행

CREATE TABLE IF NOT EXISTS couples (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    monthly_budget BIGINT NOT NULL DEFAULT 0,
    ledger_start_day INT NOT NULL DEFAULT 1,
    currency TEXT NOT NULL DEFAULT 'KRW',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL,
    avatar_color TEXT NOT NULL DEFAULT '#4F46E5',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS transactions (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    type TEXT NOT NULL,
    amount BIGINT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'KRW',
    category TEXT NOT NULL DEFAULT '',
    subcategory TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    memo TEXT NOT NULL DEFAULT '',
    date TIMESTAMPTZ NOT NULL,
    payment_method TEXT NOT NULL DEFAULT '',
    is_fixed BOOLEAN NOT NULL DEFAULT FALSE,
    tags TEXT[] NOT NULL DEFAULT '{}',
    location JSONB,
    fixed_expense_id TEXT,
    saving_link JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_transactions_couple_date ON transactions(couple_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_transactions_couple_type ON transactions(couple_id, type);

CREATE TABLE IF NOT EXISTS stock_assets (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    name_en TEXT NOT NULL DEFAULT '',
    quantity DOUBLE PRECISION NOT NULL DEFAULT 0,
    average_price DOUBLE PRECISION NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'USD',
    sector TEXT NOT NULL DEFAULT '',
    memo TEXT NOT NULL DEFAULT '',
    logo_url TEXT,
    purchased_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS price_snapshots (
    symbol TEXT PRIMARY KEY,
    exchange TEXT NOT NULL DEFAULT '',
    price DOUBLE PRECISION NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'USD',
    change DOUBLE PRECISION NOT NULL DEFAULT 0,
    change_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
    snapshotted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS stock_transactions (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    stock_asset_id TEXT NOT NULL DEFAULT '',
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    quantity DOUBLE PRECISION NOT NULL,
    price DOUBLE PRECISION NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USD',
    avg_price_at_tx DOUBLE PRECISION NOT NULL DEFAULT 0,
    realized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    memo TEXT NOT NULL DEFAULT '',
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_stx_couple_symbol ON stock_transactions(couple_id, symbol);
CREATE INDEX IF NOT EXISTS idx_stx_couple_executed ON stock_transactions(couple_id, executed_at DESC);

CREATE TABLE IF NOT EXISTS other_assets (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    asset_type TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    value_krw BIGINT NOT NULL DEFAULT 0,
    cost_krw BIGINT NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'KRW',
    is_liability BOOLEAN NOT NULL DEFAULT FALSE,
    is_locked BOOLEAN NOT NULL DEFAULT FALSE,
    location JSONB,
    maturity_date TIMESTAMPTZ,
    interest_rate DOUBLE PRECISION,
    crypto_symbol TEXT,
    crypto_qty DOUBLE PRECISION,
    loan_type TEXT NOT NULL DEFAULT '',
    payment_day INT NOT NULL DEFAULT 0,
    memo TEXT NOT NULL DEFAULT '',
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Migration: add loan columns if table already exists
ALTER TABLE other_assets ADD COLUMN IF NOT EXISTS loan_type TEXT NOT NULL DEFAULT '';
ALTER TABLE other_assets ADD COLUMN IF NOT EXISTS payment_day INT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS fixed_expenses (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    owner TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'spending',
    title TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    amount BIGINT NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'KRW',
    cycle TEXT NOT NULL DEFAULT 'monthly',
    day_of_month INT NOT NULL DEFAULT 1,
    day_of_week INT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    memo TEXT NOT NULL DEFAULT '',
    saving_link JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS dividends (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    stock_asset_id TEXT NOT NULL DEFAULT '',
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    quantity DOUBLE PRECISION NOT NULL DEFAULT 0,
    amount_per_share DOUBLE PRECISION NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'USD',
    total_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    tax_rate DOUBLE PRECISION NOT NULL DEFAULT 0.154,
    after_tax_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    usd_krw_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    amount_krw BIGINT NOT NULL DEFAULT 0,
    ex_dividend_date TIMESTAMPTZ,
    payment_date TIMESTAMPTZ NOT NULL,
    is_applied BOOLEAN NOT NULL DEFAULT FALSE,
    memo TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dividends_couple_payment ON dividends(couple_id, payment_date DESC);

CREATE TABLE IF NOT EXISTS schedules (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    start_date TIMESTAMPTZ NOT NULL,
    end_date TIMESTAMPTZ,
    all_day BOOLEAN NOT NULL DEFAULT TRUE,
    is_dday BOOLEAN NOT NULL DEFAULT FALSE,
    dday_label TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT 'indigo',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_schedules_couple_start ON schedules(couple_id, start_date);

CREATE TABLE IF NOT EXISTS diaries (
    id TEXT PRIMARY KEY,
    couple_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    date TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    photos TEXT[] NOT NULL DEFAULT '{}',
    mood TEXT NOT NULL DEFAULT 'normal',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(couple_id, date)
);
CREATE INDEX IF NOT EXISTS idx_diaries_couple_date ON diaries(couple_id, date DESC);
