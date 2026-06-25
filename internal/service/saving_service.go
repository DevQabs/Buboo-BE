// saving_service.go coordinates atomic writes across transaction + asset files.
//
// Atomicity strategy (file-based store):
//  1. Acquire service-level mutex — serializes all saving ops.
//  2. Create transaction record (writes transactions.json).
//  3. Update or create asset record (writes stocks.json or other_assets.json).
//  4. Append StockTransaction log if stock kind (non-fatal, consistent with buyStock).
//  5. On step-3 failure: delete the transaction from step 2 (best-effort rollback).
package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

// SavingService coordinates atomic writes across the transaction, stock, and
// other-asset repositories. One mutex serializes all saving operations so no
// two concurrent requests can interleave their multi-file writes.
type SavingService struct {
	mu        sync.Mutex
	txRepo    repository.TransactionRepository
	stockRepo repository.StockRepository
	stxRepo   repository.StockTransactionRepository
	assetRepo repository.OtherAssetRepository
}

func NewSavingService(
	txRepo repository.TransactionRepository,
	stockRepo repository.StockRepository,
	stxRepo repository.StockTransactionRepository,
	assetRepo repository.OtherAssetRepository,
) *SavingService {
	return &SavingService{
		txRepo:    txRepo,
		stockRepo: stockRepo,
		stxRepo:   stxRepo,
		assetRepo: assetRepo,
	}
}

// ApplySavingRequest carries all inputs needed for ApplySaving.
type ApplySavingRequest struct {
	CoupleID       string
	UserID         string
	AmountKRW      int64
	Title          string
	Memo           string
	Date           time.Time
	PaymentMethod  string
	IsFixed        bool
	FixedExpenseID *string
	Tags           []string
	Link           models.SavingLink
}

// ApplySaving atomically records a saving transaction and updates the linked asset.
// Returns the created transaction on success.
func (s *SavingService) ApplySaving(ctx context.Context, req ApplySavingRequest) (*models.Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	date := req.Date
	if date.IsZero() {
		date = time.Now().UTC()
	}
	tags := req.Tags
	if len(tags) == 0 {
		tags = []string{"저축"}
	}

	link := req.Link // local copy — applyStock/applyOtherAsset may set LinkAssetID

	tx := &models.Transaction{
		CoupleID:       req.CoupleID,
		UserID:         req.UserID,
		Type:           "saving",
		Amount:         req.AmountKRW,
		Currency:       "KRW",
		Category:       "저축/투자",
		Title:          req.Title,
		Memo:           req.Memo,
		Date:           date,
		PaymentMethod:  req.PaymentMethod,
		IsFixed:        req.IsFixed,
		Tags:           tags,
		FixedExpenseID: req.FixedExpenseID,
		SavingLink:     &link,
	}

	// Step 1: write transaction
	created, err := s.txRepo.Create(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("saving tx create: %w", err)
	}

	// Step 2: update asset
	var assetErr error
	switch link.Kind {
	case models.SavingKindStock:
		assetErr = s.applyStock(ctx, created, &link)
	case models.SavingKindDeposit, models.SavingKindGeneral:
		assetErr = s.applyOtherAsset(ctx, created, &link, req.AmountKRW)
	default:
		assetErr = fmt.Errorf("unknown saving kind: %q", link.Kind)
	}

	if assetErr != nil {
		// Rollback: remove the transaction we just wrote
		if delErr := s.txRepo.Delete(ctx, created.ID); delErr != nil {
			fmt.Printf("warn: rollback failed for tx %s: %v\n", created.ID, delErr)
		}
		return nil, fmt.Errorf("asset update: %w", assetErr)
	}

	// Backfill LinkAssetID into the stored transaction if it was resolved during creation
	if created.SavingLink == nil || created.SavingLink.LinkAssetID != link.LinkAssetID {
		created.SavingLink = &link
		if _, err := s.txRepo.Update(ctx, created); err != nil {
			fmt.Printf("warn: could not backfill saving_link.link_asset_id on tx %s: %v\n", created.ID, err)
		}
	}

	return created, nil
}

func (s *SavingService) applyStock(ctx context.Context, tx *models.Transaction, link *models.SavingLink) error {
	var (
		symbol   string
		exchange string
		name     string
		currency string
		qty      float64
		price    float64
		avgAfter float64
		assetID  string
	)

	if link.LinkAssetID != "" {
		// ── Add to existing stock ────────────────────────────────────────────────
		asset, err := s.stockRepo.GetByID(ctx, link.LinkAssetID)
		if err != nil {
			return fmt.Errorf("stock %s not found: %w", link.LinkAssetID, err)
		}
		if link.AddStockQty <= 0 || link.AddStockPrice <= 0 {
			return fmt.Errorf("add_stock_qty and add_stock_price must be > 0")
		}

		qty = link.AddStockQty
		price = link.AddStockPrice

		// Weighted-average cost basis
		totalCost := asset.AveragePrice*asset.Quantity + price*qty
		newQty := asset.Quantity + qty
		asset.AveragePrice = totalCost / newQty
		asset.Quantity = newQty
		avgAfter = asset.AveragePrice

		if _, err := s.stockRepo.Update(ctx, asset); err != nil {
			return fmt.Errorf("stock update: %w", err)
		}

		symbol, exchange, name, currency, assetID = asset.Symbol, asset.Exchange, asset.Name, asset.Currency, asset.ID
	} else {
		// ── Create new stock asset ───────────────────────────────────────────────
		if link.NewStockSymbol == "" || link.NewStockExchange == "" || link.NewStockName == "" {
			return fmt.Errorf("new_stock_symbol, new_stock_exchange, new_stock_name required for new stock")
		}
		if link.NewStockQty <= 0 || link.NewStockPrice <= 0 {
			return fmt.Errorf("new_stock_qty and new_stock_price must be > 0")
		}

		cur := link.NewStockCurrency
		if cur == "" {
			cur = "USD"
		}
		newAsset := &models.StockAsset{
			CoupleID:     tx.CoupleID,
			UserID:       tx.UserID,
			Symbol:       strings.ToUpper(strings.TrimSpace(link.NewStockSymbol)),
			Exchange:     strings.ToUpper(strings.TrimSpace(link.NewStockExchange)),
			Name:         link.NewStockName,
			Quantity:     link.NewStockQty,
			AveragePrice: link.NewStockPrice,
			Currency:     strings.ToUpper(strings.TrimSpace(cur)),
			Sector:       link.NewStockSector,
			PurchasedAt:  tx.Date,
		}
		created, err := s.stockRepo.Create(ctx, newAsset)
		if err != nil {
			return fmt.Errorf("stock create: %w", err)
		}

		qty = link.NewStockQty
		price = link.NewStockPrice
		avgAfter = link.NewStockPrice
		symbol, exchange, name, currency = created.Symbol, created.Exchange, created.Name, created.Currency
		assetID = created.ID
		link.LinkAssetID = assetID // backfill for tx record
	}

	// Step 3: append immutable stock transaction log (non-fatal)
	stx := &models.StockTransaction{
		CoupleID:     tx.CoupleID,
		UserID:       tx.UserID,
		StockAssetID: assetID,
		Symbol:       symbol,
		Exchange:     exchange,
		Name:         name,
		Type:         models.StockTxBuy,
		Quantity:     qty,
		Price:        price,
		Currency:     currency,
		AvgPriceAtTx: avgAfter,
		Memo:         tx.Memo,
		ExecutedAt:   tx.Date,
	}
	if _, err := s.stxRepo.Create(ctx, stx); err != nil {
		fmt.Printf("warn: stx log failed for saving tx %s: %v\n", tx.ID, err)
	}
	return nil
}

func (s *SavingService) applyOtherAsset(ctx context.Context, tx *models.Transaction, link *models.SavingLink, amountKRW int64) error {
	if link.LinkAssetID != "" {
		// ── Add to existing other asset ──────────────────────────────────────────
		asset, err := s.assetRepo.GetByID(ctx, link.LinkAssetID)
		if err != nil {
			return fmt.Errorf("other asset %s not found: %w", link.LinkAssetID, err)
		}
		asset.ValueKRW += amountKRW
		if _, err := s.assetRepo.Update(ctx, asset); err != nil {
			return fmt.Errorf("other asset update: %w", err)
		}
		return nil
	}

	// ── Create new other asset ───────────────────────────────────────────────────
	assetType := link.NewAssetType
	if assetType == "" {
		assetType = models.AssetTypeDeposit
	}
	assetName := link.NewAssetName
	if assetName == "" {
		assetName = tx.Title
	}

	newAsset := &models.OtherAsset{
		CoupleID:   tx.CoupleID,
		UserID:     tx.UserID,
		AssetType:  assetType,
		Name:       assetName,
		ValueKRW:   amountKRW,
		CostKRW:    amountKRW,
		Currency:   "KRW",
		AcquiredAt: tx.Date,
	}
	created, err := s.assetRepo.Create(ctx, newAsset)
	if err != nil {
		return fmt.Errorf("other asset create: %w", err)
	}
	link.LinkAssetID = created.ID // backfill for tx record
	return nil
}
