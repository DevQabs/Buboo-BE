// Package repository defines the data-access interfaces for the application.
// By depending on these interfaces (not concrete implementations), the
// business layer remains completely DB-agnostic.
//
// Current implementation: FileRepository (JSON files)
// Future implementation:   PostgresRepository (drop-in replacement)
package repository

import (
	"context"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

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

	// Delete removes a stock asset by ID.
	Delete(ctx context.Context, id string) error

	// GetPriceSnapshot returns the latest stored price for a symbol.
	// In MVP this reads from stocks.json; later it calls a real price API.
	GetPriceSnapshot(ctx context.Context, symbol string) (*models.PriceSnapshot, error)

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

	// ListUsers returns all users.
	ListUsers(ctx context.Context) ([]models.User, error)

	// GetCouple returns the couple entity (MVP: there is only one couple).
	GetCouple(ctx context.Context, coupleID string) (*models.Couple, error)

	// UpdateCouple updates mutable couple fields (monthly_budget, ledger_start_day).
	// Both fields are always overwritten; callers should pass the current value for fields they do not intend to change.
	UpdateCouple(ctx context.Context, coupleID string, monthlyBudget int64, ledgerStartDay int) (*models.Couple, error)
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

	// MarkApplied sets is_applied=true after generating a ledger entry.
	MarkApplied(ctx context.Context, id string) error

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

	// Delete removes an asset by ID.
	Delete(ctx context.Context, id string) error
}
