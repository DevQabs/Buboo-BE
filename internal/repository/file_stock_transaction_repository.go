// file_stock_transaction_repository.go — append-only JSON log for stock buy/sell events.
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/models"
)

// FileStockTransactionRepository implements StockTransactionRepository using stock_transactions.json.
// Records are append-only — never updated or deleted.
type FileStockTransactionRepository struct {
	mu       sync.RWMutex
	filePath string
}

func NewFileStockTransactionRepository(filePath string) *FileStockTransactionRepository {
	return &FileStockTransactionRepository{filePath: filePath}
}

func (r *FileStockTransactionRepository) load() (*models.StockTransactionsFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading stock_transactions: %w", err)
	}
	var f models.StockTransactionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing stock_transactions: %w", err)
	}
	if f.Transactions == nil {
		f.Transactions = make([]models.StockTransaction, 0)
	}
	return &f, nil
}

func (r *FileStockTransactionRepository) save(f *models.StockTransactionsFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing stock_transactions: %w", err)
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileStockTransactionRepository) Create(_ context.Context, tx *models.StockTransaction) (*models.StockTransaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tx.ID = "stx-" + uuid.NewString()
	tx.CreatedAt = now
	if tx.ExecutedAt.IsZero() {
		tx.ExecutedAt = now
	}

	f.Transactions = append(f.Transactions, *tx)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return tx, nil
}

func (r *FileStockTransactionRepository) ListByCouple(_ context.Context, coupleID string) ([]models.StockTransaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.StockTransaction, 0)
	for i := len(f.Transactions) - 1; i >= 0; i-- {
		if f.Transactions[i].CoupleID == coupleID {
			result = append(result, f.Transactions[i])
		}
	}
	return result, nil
}

func (r *FileStockTransactionRepository) ListBySymbol(_ context.Context, coupleID, symbol string) ([]models.StockTransaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.StockTransaction, 0)
	for i := len(f.Transactions) - 1; i >= 0; i-- {
		t := f.Transactions[i]
		if t.CoupleID == coupleID && t.Symbol == symbol {
			result = append(result, t)
		}
	}
	return result, nil
}

func (r *FileStockTransactionRepository) HasSellInYear(_ context.Context, coupleID, symbol string, year int) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return false, err
	}
	for _, t := range f.Transactions {
		if t.CoupleID == coupleID &&
			t.Symbol == symbol &&
			t.Type == models.StockTxSell &&
			t.ExecutedAt.Year() == year {
			return true, nil
		}
	}
	return false, nil
}

func (r *FileStockTransactionRepository) AnnualSummary(_ context.Context, coupleID string, year int) ([]models.SymbolTaxSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}

	// Aggregate SELL transactions by symbol
	type agg struct {
		exchange    string
		sellCount   int
		realizedPnL float64
	}
	bySymbol := make(map[string]*agg)

	for _, t := range f.Transactions {
		if t.CoupleID != coupleID || t.Type != models.StockTxSell || t.ExecutedAt.Year() != year {
			continue
		}
		a, ok := bySymbol[t.Symbol]
		if !ok {
			a = &agg{exchange: t.Exchange}
			bySymbol[t.Symbol] = a
		}
		a.sellCount++
		a.realizedPnL += t.RealizedPnL
	}

	const taxRate = 0.22
	result := make([]models.SymbolTaxSummary, 0, len(bySymbol))
	for symbol, a := range bySymbol {
		taxable := a.realizedPnL
		if taxable < 0 {
			taxable = 0
		}
		result = append(result, models.SymbolTaxSummary{
			Symbol:       symbol,
			Exchange:     a.exchange,
			SellCount:    a.sellCount,
			RealizedPnL:  a.realizedPnL,
			EstimatedTax: taxable * taxRate,
		})
	}
	return result, nil
}
