// Package repository defines the data-access interfaces for the application.
// By depending on these interfaces (not concrete implementations), the
// business layer remains completely DB-agnostic.
//
// Current implementation: FileRepository (JSON files)
// Future implementation:   PostgresRepository (drop-in replacement)
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

// ErrDuplicate 는 유니크 제약에 걸린 저장이다. 고정비를 같은 날 두 번 반영하면 난다.
var ErrDuplicate = errors.New("duplicate")

// ErrNotFound 는 대상 행이 없다(이미 지웠거나 다른 그룹 것).
var ErrNotFound = errors.New("not found")

// ─────────────────────────────────────────────
//  Transaction Repository
// ─────────────────────────────────────────────

// TransactionRepository abstracts all persistence operations for Transaction.
// Any struct that implements this interface can be used as the data source,
// making it trivial to swap JSON files for PostgreSQL (or any other store).
type TransactionRepository interface {
	// GetByID returns a single transaction by its ID.
	GetByID(ctx context.Context, id string) (*models.Transaction, error)

	// ListByCouple returns all transactions for a couple, newest first.
	ListByCouple(ctx context.Context, coupleID string) ([]models.Transaction, error)

	// ListByMonth returns transactions for a couple in a given year/month.
	ListByMonth(ctx context.Context, coupleID string, year, month int) ([]models.Transaction, error)

	// ListByUser returns transactions created by a specific user.
	ListByUser(ctx context.Context, coupleID, userID string) ([]models.Transaction, error)

	// ListByDateRange returns transactions for a couple in an arbitrary date range [start, end).
	ListByDateRange(ctx context.Context, coupleID string, start, end time.Time) ([]models.Transaction, error)

	// Create persists a new transaction and returns the saved entity.
	Create(ctx context.Context, tx *models.Transaction) (*models.Transaction, error)

	// Update replaces an existing transaction. Returns error if not found.
	Update(ctx context.Context, tx *models.Transaction) (*models.Transaction, error)

	// Delete removes a transaction by ID.
	Delete(ctx context.Context, id string) error

	// MonthlySummary computes income/expense totals for a given month.
	MonthlySummary(ctx context.Context, coupleID string, year, month int) (*models.MonthlySummary, error)

	// SummaryByDateRange computes income/expense totals for an arbitrary date range [start, end).
	SummaryByDateRange(ctx context.Context, coupleID string, start, end time.Time) (*models.MonthlySummary, error)
}

// ─────────────────────────────────────────────
//  Stock Asset Repository
// ─────────────────────────────────────────────

// StockRepository abstracts all persistence operations for StockAsset.
type StockRepository interface {
	// GetByID returns a single stock asset by its ID.
	GetByID(ctx context.Context, id string) (*models.StockAsset, error)

	// ListByCouple returns all stock assets for a couple.
	ListByCouple(ctx context.Context, coupleID string) ([]models.StockAsset, error)

	// ListByUser returns stock assets owned by a specific user.
	ListByUser(ctx context.Context, coupleID, userID string) ([]models.StockAsset, error)

	// Create persists a new stock asset entry.
	Create(ctx context.Context, asset *models.StockAsset) (*models.StockAsset, error)

	// Update replaces an existing stock asset.
	Update(ctx context.Context, asset *models.StockAsset) (*models.StockAsset, error)

	// Reorder rewrites sort_order from the position of each ID in the slice.
	Reorder(ctx context.Context, coupleID string, ids []string) error

	// Trade locks the row (SELECT ... FOR UPDATE), lets apply change it, then in the
	// same DB transaction saves it — or deletes it when quantity reaches 0 — and
	// appends the log apply returns (nil = no log). Any error rolls everything back.
	Trade(ctx context.Context, id, coupleID string, apply func(a *models.StockAsset) (*models.StockTransaction, error)) (*models.StockAsset, error)

	// Delete removes a stock asset by ID.
	Delete(ctx context.Context, id string) error

	// GetPriceSnapshot returns the latest stored price for a symbol.
	// In MVP this reads from stocks.json; later it calls a real price API.
	GetPriceSnapshot(ctx context.Context, symbol string) (*models.PriceSnapshot, error)

	// ListPriceSnapshots fetches many symbols in one query.
	ListPriceSnapshots(ctx context.Context, symbols []string) (map[string]models.PriceSnapshot, error)

	// UpsertPriceSnapshot saves or updates a price snapshot.
	UpsertPriceSnapshot(ctx context.Context, snap *models.PriceSnapshot) error
}

// ─────────────────────────────────────────────
//  User / Couple Repository
// ─────────────────────────────────────────────

// UserRepository abstracts user and couple lookups.
type UserRepository interface {
	// GetUser returns a user by ID.
	GetUser(ctx context.Context, userID string) (*models.User, error)

	// ListUsers returns all users for a couple.
	ListUsers(ctx context.Context, coupleID string) ([]models.User, error)

	// GetUserByGoogleSub looks up a user by Google OAuth sub (stable identifier).
	// Returns nil, nil if not found.
	GetUserByGoogleSub(ctx context.Context, sub string) (*models.User, error)

	// GetUserByEmail looks up a user by email (migration fallback).
	// Returns nil, nil if not found.
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)

	// CreateUser inserts a new user and returns the saved entity.
	CreateUser(ctx context.Context, u *models.User) (*models.User, error)

	// CreateCouple inserts a new couple and returns the saved entity.
	CreateCouple(ctx context.Context, c *models.Couple) (*models.Couple, error)

	// UpdateUserGoogleSub links a google_sub to an existing user.
	UpdateUserGoogleSub(ctx context.Context, userID, sub string) error

	// GetCouple returns the couple entity.
	GetCouple(ctx context.Context, coupleID string) (*models.Couple, error)

	// UpdateCouple updates mutable couple fields (monthly_budget, ledger_start_day).
	UpdateCouple(ctx context.Context, coupleID string, monthlyBudget int64, ledgerStartDay int) (*models.Couple, error)
}

// InviteRepository manages couple invite codes.
type InviteRepository interface {
	// GetInviteByCode returns a valid (not expired, not used) invite.
	GetInviteByCode(ctx context.Context, code string) (*models.Invite, error)

	// CreateInvite generates a new invite carrying a permission preset
	// (spouse|grandparent|custom) and a display name for the joiner.
	CreateInvite(ctx context.Context, coupleID, createdBy, preset, nickname string) (*models.Invite, error)

	// RedeemInvite consumes the invite and creates the member with the preset's
	// role and permissions in one transaction. ErrInviteInvalid when the code is
	// used, expired or unknown; ErrUserExists when the email or Google account
	// already belongs to another user. u.Name may be empty: the invite's
	// nickname, then fallbackName, fill it.
	RedeemInvite(ctx context.Context, code string, u models.User, fallbackName string) (*models.User, error)

	// ListInvitesByCouple returns all active (unused, non-expired) invites for a couple.
	ListInvitesByCouple(ctx context.Context, coupleID string) ([]models.Invite, error)
}

// PermissionRepository stores per-member menu levels (설계서 4.1).
type PermissionRepository interface {
	// Get returns the caller's permissions, or nil, nil when the user is not in
	// that couple any more (removed member holding a still-valid token).
	Get(ctx context.Context, userID, coupleID string) (*models.Permissions, error)

	// ListByCouple returns userID -> menu -> level for every member with rows.
	ListByCouple(ctx context.Context, coupleID string) (map[string]map[string]string, error)

	// Replace overwrites all of a member's rows. Missing or hidden menus get no row.
	Replace(ctx context.Context, userID string, levels map[string]string) error

	// RemoveMember deletes a non-owner user; their rows go by ON DELETE CASCADE.
	RemoveMember(ctx context.Context, userID string) error
}

// StorageRepository records every uploaded object so the app can keep the
// whole bucket under STORAGE_LIMIT_BYTES (R2 has no cap of its own).
type StorageRepository interface {
	// Record inserts a row; an existing key is left as is.
	Record(ctx context.Context, o models.StorageObject) error
	// ReserveWithinCap records o only if the bucket total stays <= limit,
	// atomically with the check. false means the cap would be exceeded.
	ReserveWithinCap(ctx context.Context, o models.StorageObject, limit int64) (bool, error)
	// Get returns nil, nil when the key was never issued.
	Get(ctx context.Context, key string) (*models.StorageObject, error)
	Delete(ctx context.Context, keys []string) error
	// TotalBytes sums the whole bucket (all couples — the free 10GB is per account).
	TotalBytes(ctx context.Context) (int64, error)
}

// BabyRepository stores the couple's one baby, photo moments and growth records.
type BabyRepository interface {
	Get(ctx context.Context, coupleID string) (*models.Baby, error) // nil, nil when not registered
	Put(ctx context.Context, coupleID string, b models.Baby) error

	ListMoments(ctx context.Context, coupleID string) ([]models.BabyMoment, error)
	GetMoment(ctx context.Context, id string) (*models.BabyMoment, error)
	// CreateMoment inserts m and m.Media in one transaction and fills their IDs.
	CreateMoment(ctx context.Context, coupleID string, m *models.BabyMoment) error
	// UpdateMoment updates date/content and appends add after the existing media.
	UpdateMoment(ctx context.Context, coupleID string, m *models.BabyMoment, add []models.BabyMedia) error
	DeleteMoment(ctx context.Context, id string) error // media rows go by ON DELETE CASCADE
	GetMedia(ctx context.Context, id string) (*models.BabyMedia, error)
	DeleteMedia(ctx context.Context, id string) error

	ListGrowth(ctx context.Context, coupleID string) ([]models.BabyGrowth, error)
	CreateGrowth(ctx context.Context, coupleID string, g *models.BabyGrowth) error
	UpdateGrowth(ctx context.Context, g *models.BabyGrowth) error
	DeleteGrowth(ctx context.Context, id string) error
}

// ─────────────────────────────────────────────
//  Filter helpers (used by service / handler layers)
// ─────────────────────────────────────────────

// TransactionFilter carries optional query parameters for list endpoints.
type TransactionFilter struct {
	UserID    string    // filter by specific user
	Type      string    // "income" | "expense" | "" (all)
	Category  string    // filter by category
	StartDate time.Time // inclusive lower bound
	EndDate   time.Time // inclusive upper bound
	IsFixed   *bool     // nil = all, true/false = fixed/variable
}

// ─────────────────────────────────────────────
//  StockTransaction Repository (Immutable Log)
// ─────────────────────────────────────────────

// StockTransactionRepository stores immutable buy/sell events.
// Records are append-only — never updated or deleted.
type StockTransactionRepository interface {
	// Create appends a new transaction record.
	Create(ctx context.Context, tx *models.StockTransaction) (*models.StockTransaction, error)

	// ListByCouple returns all transactions for a couple, newest first.
	ListByCouple(ctx context.Context, coupleID string) ([]models.StockTransaction, error)

	// ListBySymbol returns all transactions for a specific symbol.
	ListBySymbol(ctx context.Context, coupleID, symbol string) ([]models.StockTransaction, error)

	// HasSellInYear returns true if there is at least one SELL transaction
	// for the given symbol in the given calendar year.
	HasSellInYear(ctx context.Context, coupleID, symbol string, year int) (bool, error)

	// AnnualSummary computes realized P&L grouped by symbol for a given year.
	AnnualSummary(ctx context.Context, coupleID string, year int) ([]models.SymbolTaxSummary, error)

	// AnnualGainsByUser nets realized P&L per person for a given year.
	// 양도소득세는 인별 과세이므로 기본공제 250만원도 사람마다 따로 적용된다.
	AnnualGainsByUser(ctx context.Context, coupleID string, year int) ([]models.UserGainRow, error)
}

// ─────────────────────────────────────────────
//  DividendEvent Repository
// ─────────────────────────────────────────────

// DividendRepository abstracts persistence for dividend payout records.
type DividendRepository interface {
	// GetByID returns a single dividend event by its ID.
	GetByID(ctx context.Context, id string) (*models.DividendEvent, error)

	// ListByCouple returns all dividend events for a couple, newest payment_date first.
	ListByCouple(ctx context.Context, coupleID string) ([]models.DividendEvent, error)

	// ListByYear returns dividend events whose payment_date falls within the given year.
	ListByYear(ctx context.Context, coupleID string, year int) ([]models.DividendEvent, error)

	// Create persists a new dividend event.
	Create(ctx context.Context, d *models.DividendEvent) (*models.DividendEvent, error)

	// MarkApplied sets is_applied=true and remembers the ledger transaction it created.
	MarkApplied(ctx context.Context, id, txID string) error

	// Delete removes a dividend event by ID.
	Delete(ctx context.Context, id string) error
}

// ─────────────────────────────────────────────
//  FixedExpense Repository
// ─────────────────────────────────────────────

// FixedExpenseRepository abstracts persistence for recurring expense templates.
type FixedExpenseRepository interface {
	// GetByID returns a single fixed expense by its ID.
	GetByID(ctx context.Context, id string) (*models.FixedExpense, error)

	// ListByCouple returns all fixed expenses for a couple (active and inactive).
	ListByCouple(ctx context.Context, coupleID string) ([]models.FixedExpense, error)

	// Create persists a new fixed expense template.
	Create(ctx context.Context, fe *models.FixedExpense) (*models.FixedExpense, error)

	// Update replaces an existing fixed expense template.
	Update(ctx context.Context, fe *models.FixedExpense) (*models.FixedExpense, error)

	// Delete removes a fixed expense template by ID.
	Delete(ctx context.Context, id string) error
}

// ─────────────────────────────────────────────
//  Schedule Repository
// ─────────────────────────────────────────────

type ScheduleRepository interface {
	GetByID(ctx context.Context, id string) (*models.Schedule, error)
	ListByCouple(ctx context.Context, coupleID string) ([]models.Schedule, error)
	ListByMonth(ctx context.Context, coupleID string, year, month int) ([]models.Schedule, error)
	Create(ctx context.Context, s *models.Schedule) (*models.Schedule, error)
	Update(ctx context.Context, s *models.Schedule) (*models.Schedule, error)
	Delete(ctx context.Context, id string) error
}

// PushRepository stores web push subscriptions and hands out due schedule reminders (push 설계서 2·4절).
type PushRepository interface {
	// SaveSubscription upserts by endpoint; the endpoint moves to userID.
	SaveSubscription(ctx context.Context, userID, userAgent string, s models.PushSubscription) error
	// DeleteSubscription removes the endpoint only when it belongs to userID.
	DeleteSubscription(ctx context.Context, userID, endpoint string) error
	// DeleteEndpoint removes a subscription the push service reported gone (404/410).
	DeleteEndpoint(ctx context.Context, endpoint string) error
	ListSubscriptions(ctx context.Context, userIDs []string) ([]models.PushSubscription, error)
	// ClaimDue stamps sent_at on up to limit due, unsent reminders and returns them.
	// A row is returned at most once, even if sending it later fails.
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]models.DueReminder, error)
}

// ─────────────────────────────────────────────
//  Diary Repository
// ─────────────────────────────────────────────

type DiaryRepository interface {
	GetByID(ctx context.Context, id string) (*models.DiaryEntry, error)
	ListByCouple(ctx context.Context, coupleID string) ([]models.DiaryEntry, error)
	ListByMonth(ctx context.Context, coupleID string, year, month int) ([]models.DiaryEntry, error)
	GetByDate(ctx context.Context, coupleID, date string) (*models.DiaryEntry, error)
	Create(ctx context.Context, d *models.DiaryEntry) (*models.DiaryEntry, error)
	Update(ctx context.Context, d *models.DiaryEntry) (*models.DiaryEntry, error)
	AddPhoto(ctx context.Context, id, filename string) error
	DeletePhoto(ctx context.Context, id, filename string) error
	Delete(ctx context.Context, id string) error
}

// ─────────────────────────────────────────────
//  Category Repository
// ─────────────────────────────────────────────

type CategoryRepository interface {
	Get(ctx context.Context, coupleID string) (*models.Categories, error)
	Update(ctx context.Context, coupleID string, cats *models.Categories) (*models.Categories, error)
}

// ─────────────────────────────────────────────
//  OtherAsset Repository
// ─────────────────────────────────────────────

// OtherAssetRepository abstracts persistence for non-stock assets
// (real estate, deposits, crypto, vehicles, etc.).
type OtherAssetRepository interface {
	// GetByID returns a single asset by its ID.
	GetByID(ctx context.Context, id string) (*models.OtherAsset, error)

	// ListByCouple returns all other assets for a couple.
	ListByCouple(ctx context.Context, coupleID string) ([]models.OtherAsset, error)

	// ListByUser returns assets owned by a specific user.
	ListByUser(ctx context.Context, coupleID, userID string) ([]models.OtherAsset, error)

	// ListByType returns assets filtered by asset type.
	ListByType(ctx context.Context, coupleID string, assetType models.OtherAssetType) ([]models.OtherAsset, error)

	// Create persists a new asset entry.
	Create(ctx context.Context, asset *models.OtherAsset) (*models.OtherAsset, error)

	// Update replaces an existing asset entry.
	Update(ctx context.Context, asset *models.OtherAsset) (*models.OtherAsset, error)

	// Reorder rewrites sort_order from the position of each ID in the slice.
	Reorder(ctx context.Context, coupleID string, ids []string) error

	// Delete removes an asset by ID.
	Delete(ctx context.Context, id string) error
}

// RoadmapRepository는 목표 순자산 플랜과 실적 스냅샷을 다룬다.
type RoadmapRepository interface {
	// ActiveGoal returns the couple's current goal, or nil if none is set.
	ActiveGoal(ctx context.Context, coupleID string) (*models.RoadmapGoal, error)
	UpsertGoal(ctx context.Context, g *models.RoadmapGoal) (*models.RoadmapGoal, error)

	// Assumptions returns the assumptions attached to a goal, or nil if none.
	Assumptions(ctx context.Context, goalID string) (*models.RoadmapAssumptions, error)
	UpsertAssumptions(ctx context.Context, a *models.RoadmapAssumptions) (*models.RoadmapAssumptions, error)

	// UpsertSnapshot records this month's net worth, replacing the same month.
	UpsertSnapshot(ctx context.Context, s *models.NetWorthSnapshot) (*models.NetWorthSnapshot, error)

	// MonthlyContributions sums savings and net stock purchases per month.
	MonthlyContributions(ctx context.Context, coupleID string, from time.Time) (map[string]int64, error)
	ListSnapshots(ctx context.Context, coupleID string) ([]models.NetWorthSnapshot, error)

	// Baseline returns the frozen plan for a goal, or nil if none.
	Baseline(ctx context.Context, goalID string) (*models.RoadmapBaseline, error)
	SaveBaseline(ctx context.Context, b *models.RoadmapBaseline) error
	// DeleteBaseline drops the frozen plan so the next view rebuilds it.
	DeleteBaseline(ctx context.Context, goalID string) error
}
