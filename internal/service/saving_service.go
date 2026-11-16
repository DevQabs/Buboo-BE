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
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
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
	usdKRW    func(context.Context) (float64, error) // 저축 경유 USD 매수 환율 조회 (PriceService.FetchUSDKRW)
}

func NewSavingService(
	txRepo repository.TransactionRepository,
	stockRepo repository.StockRepository,
	stxRepo repository.StockTransactionRepository,
	assetRepo repository.OtherAssetRepository,
	usdKRW func(context.Context) (float64, error),
) *SavingService {
	return &SavingService{
		txRepo:    txRepo,
		stockRepo: stockRepo,
		stxRepo:   stxRepo,
		assetRepo: assetRepo,
		usdKRW:    usdKRW,
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
		fx       float64
	)

	if link.LinkAssetID != "" {
		// ── Add to existing stock ────────────────────────────────────────────────
		asset, err := s.stockRepo.GetByID(ctx, link.LinkAssetID)
		if err != nil {
			return fmt.Errorf("stock %s not found: %w", link.LinkAssetID, err)
		}
		if asset.CoupleID != tx.CoupleID {
			return fmt.Errorf("stock %s not found", link.LinkAssetID)
		}
		if link.AddStockQty <= 0 || link.AddStockPrice <= 0 {
			return fmt.Errorf("add_stock_qty and add_stock_price must be > 0")
		}

		qty = link.AddStockQty
		price = link.AddStockPrice

		fx, err = ResolveBuyFX(ctx, asset.Currency, 0, s.usdKRW)
		if err != nil {
			return err
		}
		asset.AvgKRWPrice = KRWAvgAfterBuy(asset.Currency,
			asset.Quantity, asset.AveragePrice, asset.AvgKRWPrice, qty, price, fx)

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

		cur := strings.ToUpper(strings.TrimSpace(link.NewStockCurrency))
		if cur == "" {
			cur = "USD"
		}
		var err error
		fx, err = ResolveBuyFX(ctx, cur, 0, s.usdKRW)
		if err != nil {
			return err
		}
		newAsset := &models.StockAsset{
			CoupleID:     tx.CoupleID,
			UserID:       tx.UserID,
			Symbol:       strings.ToUpper(strings.TrimSpace(link.NewStockSymbol)),
			Exchange:     strings.ToUpper(strings.TrimSpace(link.NewStockExchange)),
			Name:         link.NewStockName,
			Quantity:     link.NewStockQty,
			AveragePrice: link.NewStockPrice,
			AvgKRWPrice:  KRWAvgAfterBuy(cur, 0, 0, 0, link.NewStockQty, link.NewStockPrice, fx),
			Currency:     cur,
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
		CoupleID:         tx.CoupleID,
		UserID:           tx.UserID,
		StockAssetID:     assetID,
		Symbol:           symbol,
		Exchange:         exchange,
		Name:             name,
		Type:             models.StockTxBuy,
		Quantity:         qty,
		Price:            price,
		Currency:         currency,
		AvgPriceAtTx:     avgAfter,
		ExchangeRateAtTx: fx,
		Source:           models.StockTxSourceSaving,
		Memo:             tx.Memo,
		ExecutedAt:       tx.Date,
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
		if asset.CoupleID != tx.CoupleID {
			return fmt.Errorf("other asset %s not found", link.LinkAssetID)
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

// RevertSaving 은 저축 거래를 지울 때 연결 자산을 저축 전으로 되돌린다.
// 연결 자산이 이미 지워졌으면 되돌릴 곳이 없으니 넘어간다.
func (s *SavingService) RevertSaving(ctx context.Context, tx *models.Transaction) error {
	l := tx.SavingLink
	if tx.Type != "saving" || l == nil || l.LinkAssetID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if l.Kind != models.SavingKindStock {
		return s.shiftOtherAsset(ctx, tx.CoupleID, l.LinkAssetID, -tx.Amount)
	}
	qty, price := l.AddStockQty, l.AddStockPrice
	if qty == 0 { // 저축이 새로 만든 종목
		qty, price = l.NewStockQty, l.NewStockPrice
	}
	_, err := s.stockRepo.Trade(ctx, l.LinkAssetID, tx.CoupleID, func(a *models.StockAsset) (*models.StockTransaction, error) {
		left := a.Quantity - qty
		if left > 0 {
			if a.Currency == "USD" {
				// 저축 매수 때 남긴 환율로 원화 평단을 되돌린다. 기록이 없으면(원화 평단 도입 전 저축) 그대로 둔다.
				fx, err := s.savingBuyFX(ctx, tx, a, qty, price)
				if err != nil {
					return nil, err
				}
				if fx > 0 {
					a.AvgKRWPrice = (a.AvgKRWPrice*a.Quantity - price*qty*fx) / left
				}
			}
			a.AveragePrice = (a.AveragePrice*a.Quantity - price*qty) / left
			if a.Currency != "USD" {
				a.AvgKRWPrice = a.AveragePrice
			}
		}
		a.Quantity = left // 0 이하면 Trade 가 종목을 지운다
		return nil, nil
	})
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	return err
}

// savingBuyFX 는 저축 거래가 USD 종목을 살 때 매매 기록에 남긴 환율이다. 없으면 0.
// 저축 거래와 매매 기록은 id 로 이어지지 않아 종목·수량·단가·날짜로 찾는다.
func (s *SavingService) savingBuyFX(ctx context.Context, tx *models.Transaction, a *models.StockAsset, qty, price float64) (float64, error) {
	rows, err := s.stxRepo.ListBySymbol(ctx, tx.CoupleID, a.Symbol)
	if err != nil {
		return 0, err
	}
	day := tx.Date.UTC().Format(time.DateOnly)
	for _, r := range rows {
		if r.Source == models.StockTxSourceSaving && r.StockAssetID == a.ID && r.Type == models.StockTxBuy &&
			r.Quantity == qty && r.Price == price && r.ExecutedAt.UTC().Format(time.DateOnly) == day {
			return r.ExchangeRateAtTx, nil
		}
	}
	return 0, nil
}

// ResizeSaving 은 저축 금액을 고칠 때 연결된 예금·일반 자산을 차액만큼 옮긴다.
// 주식 저축은 산 주식 수가 금액과 따로 저장돼 있어 자산을 건드리지 않는다.
func (s *SavingService) ResizeSaving(ctx context.Context, old *models.Transaction, newAmount int64) error {
	l := old.SavingLink
	if l == nil || l.LinkAssetID == "" || l.Kind == models.SavingKindStock || newAmount == old.Amount {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shiftOtherAsset(ctx, old.CoupleID, l.LinkAssetID, newAmount-old.Amount)
}

func (s *SavingService) shiftOtherAsset(ctx context.Context, coupleID, id string, delta int64) error {
	a, err := s.assetRepo.GetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.CoupleID != coupleID {
		return nil
	}
	a.ValueKRW += delta
	_, err = s.assetRepo.Update(ctx, a)
	return err
}
