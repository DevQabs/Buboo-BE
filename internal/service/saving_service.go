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
	"math"
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
	rates     USDKRWRates // 저축 경유 USD 매수 환율(저축 날짜 기준)
}

func NewSavingService(
	txRepo repository.TransactionRepository,
	stockRepo repository.StockRepository,
	stxRepo repository.StockTransactionRepository,
	assetRepo repository.OtherAssetRepository,
	rates USDKRWRates,
) *SavingService {
	return &SavingService{
		txRepo:    txRepo,
		stockRepo: stockRepo,
		stxRepo:   stxRepo,
		assetRepo: assetRepo,
		rates:     rates,
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
	if link.LinkAssetID != "" {
		// ── Add to existing stock ────────────────────────────────────────────────
		// 직접 매수(buyStock)와 같은 길: 환율(네트워크)은 잠금 전에 정하고, 잔고 갱신과
		// 매수 기록은 Trade 한 트랜잭션에서 한다. 기록이 실패하면 저축도 실패한다.
		if link.AddStockQty <= 0 || link.AddStockPrice <= 0 {
			return fmt.Errorf("add_stock_qty and add_stock_price must be > 0")
		}
		pre, err := s.stockRepo.GetByID(ctx, link.LinkAssetID)
		if err != nil {
			return fmt.Errorf("stock %s not found: %w", link.LinkAssetID, err)
		}
		if pre.CoupleID != tx.CoupleID {
			return fmt.Errorf("stock %s not found", link.LinkAssetID)
		}
		fx, err := ResolveTradeFX(ctx, pre.Currency, tx.Date, s.rates)
		if err != nil {
			return err
		}
		qty, price := link.AddStockQty, link.AddStockPrice
		_, err = s.stockRepo.Trade(ctx, link.LinkAssetID, tx.CoupleID, func(asset *models.StockAsset) (*models.StockTransaction, error) {
			asset.AvgKRWPrice = KRWAvgAfterBuy(asset.Currency,
				asset.Quantity, asset.AveragePrice, asset.AvgKRWPrice, qty, price, fx)
			newQty := asset.Quantity + qty
			asset.AveragePrice = (asset.AveragePrice*asset.Quantity + price*qty) / newQty
			asset.Quantity = newQty
			return &models.StockTransaction{
				CoupleID:         tx.CoupleID,
				UserID:           tx.UserID,
				StockAssetID:     asset.ID,
				Symbol:           asset.Symbol,
				Exchange:         asset.Exchange,
				Name:             asset.Name,
				Type:             models.StockTxBuy,
				Quantity:         qty,
				Price:            price,
				Currency:         asset.Currency,
				AvgPriceAtTx:     asset.AveragePrice,
				ExchangeRateAtTx: fx,
				Source:           models.StockTxSourceSaving,
				Memo:             tx.Memo,
				ExecutedAt:       tx.Date,
			}, nil
		})
		if err != nil {
			return fmt.Errorf("stock update: %w", err)
		}
		return nil
	}
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
	fx, err := ResolveTradeFX(ctx, cur, tx.Date, s.rates)
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

	link.LinkAssetID = created.ID // backfill for tx record

	// Step 3: append immutable stock transaction log (non-fatal)
	stx := &models.StockTransaction{
		CoupleID:         tx.CoupleID,
		UserID:           tx.UserID,
		StockAssetID:     created.ID,
		Symbol:           created.Symbol,
		Exchange:         created.Exchange,
		Name:             created.Name,
		Type:             models.StockTxBuy,
		Quantity:         link.NewStockQty,
		Price:            link.NewStockPrice,
		Currency:         created.Currency,
		AvgPriceAtTx:     link.NewStockPrice,
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

// ErrRevertAfterSell 은 저축으로 산 뒤에 판 기록이 있어 저축을 지울 수 없다는 뜻이다.
// 지금 잔고에서 저축 매수분만 빼면 평단이 틀어진다(10주@100 + 저축 10주@200 = 20주@150,
// 5주 매도 → 15주@150, 저축을 빼면 5주@50). 핸들러는 409 로 돌려준다.
var ErrRevertAfterSell = errors.New("이 저축으로 산 뒤 매도한 기록이 있어 삭제할 수 없어요. 매매 기록을 먼저 정정해 주세요")

// RevertSaving 은 저축 거래를 지우고 연결 자산을 저축 전으로 되돌린다. del 이 거래를 지운다.
// 주식 저축은 종목을 잠근 채 되돌릴 수 있는지 먼저 보고, 거래를 지운 뒤 같은 DB 트랜잭션에서
// 잔고를 되돌리고 저축이 남긴 매수 기록을 지운다. 되돌릴 수 없으면(ErrRevertAfterSell)
// 거래도 지우지 않는다. 거래를 먼저 지우므로 원복이 실패해도 다시 눌러 두 번 원복되는 일이 없다.
// 연결 자산이 이미 지워졌으면 되돌릴 곳이 없으니 거래만 지운다.
func (s *SavingService) RevertSaving(ctx context.Context, tx *models.Transaction, del func(context.Context) error) error {
	l := tx.SavingLink
	if tx.Type != "saving" || l == nil || l.LinkAssetID == "" {
		return del(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if l.Kind != models.SavingKindStock {
		if err := del(ctx); err != nil {
			return err
		}
		return s.shiftOtherAsset(ctx, tx.CoupleID, l.LinkAssetID, -tx.Amount)
	}
	qty, price := l.AddStockQty, l.AddStockPrice
	if qty == 0 { // 저축이 새로 만든 종목
		qty, price = l.NewStockQty, l.NewStockPrice
	}
	deleted := false
	_, err := s.stockRepo.Rewrite(ctx, l.LinkAssetID, tx.CoupleID, func(a *models.StockAsset, txs []models.StockTransaction) (repository.StockTxChanges, error) {
		var ch repository.StockTxChanges
		if a == nil { // 다 팔아 종목이 없다 — 되돌릴 곳이 없다
			return ch, nil
		}
		buy := savingBuy(txs, tx, qty, price)
		if sellAfter(txs, tx, buy) {
			return ch, ErrRevertAfterSell
		}
		if err := del(ctx); err != nil {
			return ch, err
		}
		deleted = true
		left := a.Quantity - qty
		if left > 0 {
			// 저축 매수 때 남긴 환율로 원화 평단을 되돌린다. 기록이 없으면(원화 평단 도입 전 저축) 그대로 둔다.
			if a.Currency == "USD" && buy != nil && buy.ExchangeRateAtTx > 0 {
				a.AvgKRWPrice = revertAvg(a.AvgKRWPrice, a.Quantity, price*buy.ExchangeRateAtTx, qty)
			}
			a.AveragePrice = revertAvg(a.AveragePrice, a.Quantity, price, qty)
			if a.Currency != "USD" {
				a.AvgKRWPrice = a.AveragePrice
			}
		}
		a.Quantity = max(left, 0) // 0 이면 Rewrite 가 종목을 지운다
		if buy != nil {
			ch.Delete = []string{buy.ID}
		}
		return ch, nil
	})
	if errors.Is(err, repository.ErrNotFound) {
		err = nil
	}
	if err == nil && !deleted {
		return del(ctx)
	}
	return err
}

// revertAvg 는 평단 avg 로 qty 주를 가진 상태에서 단가 price 로 산 n 주를 뺀 평단이다.
// 계산이 0 이하로 나오면(기록 밖에서 수량·평단을 고친 경우) 되돌릴 근거가 없으니 지금 평단을 둔다.
func revertAvg(avg, qty, price, n float64) float64 {
	v := (avg*qty - price*n) / (qty - n)
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return avg
	}
	return v
}

// savingBuy 는 저축 거래가 산 매수 기록이다. 없으면 nil.
// 저축 거래와 매매 기록은 id 로 이어지지 않아 출처·수량·단가로 찾는다. 거래 날짜는
// 나중에 고칠 수 있어 조건에 넣지 않고, 여러 건이면 거래 날짜에 가장 가까운 기록을 쓴다.
func savingBuy(txs []models.StockTransaction, tx *models.Transaction, qty, price float64) *models.StockTransaction {
	var found *models.StockTransaction
	best := time.Duration(math.MaxInt64)
	for i, r := range txs {
		if r.Source != models.StockTxSourceSaving || r.Type != models.StockTxBuy || r.Quantity != qty || r.Price != price {
			continue
		}
		if d := r.ExecutedAt.Sub(tx.Date).Abs(); d < best {
			found, best = &txs[i], d
		}
	}
	return found
}

// sellAfter 는 저축 매수 뒤에 매도가 있었는지 본다. 기록 순서(created_at)든 거래일(executed_at)이든
// 뒤에 있으면 뒤로 본다. 매수 기록이 없으면 저축 거래의 날짜·생성 시각을 기준으로 쓴다.
func sellAfter(txs []models.StockTransaction, tx *models.Transaction, buy *models.StockTransaction) bool {
	at, created := tx.Date, tx.CreatedAt
	if buy != nil {
		at, created = buy.ExecutedAt, buy.CreatedAt
	}
	for _, r := range txs {
		if r.Type == models.StockTxSell && (r.ExecutedAt.After(at) || r.CreatedAt.After(created)) {
			return true
		}
	}
	return false
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
