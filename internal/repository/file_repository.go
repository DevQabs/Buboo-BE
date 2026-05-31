// file_repository.go implements TransactionRepository and StockRepository
// using local JSON files as the backing store.
//
// When you're ready to migrate to PostgreSQL, create a `postgres_repository.go`
// that satisfies the same interfaces and swap it in at the wire-up point (main.go).
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/models"
)

// ─────────────────────────────────────────────
//  FileTransactionRepository
// ─────────────────────────────────────────────

// FileTransactionRepository is the JSON-file-backed implementation of
// TransactionRepository. A sync.RWMutex makes it safe for concurrent reads
// (multiple goroutines serving HTTP requests at the same time).
type FileTransactionRepository struct {
	mu       sync.RWMutex
	filePath string
}

// NewFileTransactionRepository constructs the repository for the given JSON path.
func NewFileTransactionRepository(filePath string) *FileTransactionRepository {
	return &FileTransactionRepository{filePath: filePath}
}

// load reads and parses the JSON file. Caller must hold at least a read lock.
func (r *FileTransactionRepository) load() (*models.TransactionsFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading transactions file: %w", err)
	}
	var f models.TransactionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing transactions file: %w", err)
	}
	return &f, nil
}

// save writes the in-memory state back to the JSON file atomically.
// Caller must hold the write lock.
func (r *FileTransactionRepository) save(f *models.TransactionsFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing transactions: %w", err)
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileTransactionRepository) GetByID(_ context.Context, id string) (*models.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, tx := range f.Transactions {
		if tx.ID == id {
			return &tx, nil
		}
	}
	return nil, fmt.Errorf("transaction %s not found", id)
}

func (r *FileTransactionRepository) ListByCouple(_ context.Context, coupleID string) ([]models.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.Transaction, 0)
	for _, tx := range f.Transactions {
		if tx.CoupleID == coupleID {
			result = append(result, tx)
		}
	}
	// newest first
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date.After(result[j].Date)
	})
	return result, nil
}

func (r *FileTransactionRepository) ListByMonth(_ context.Context, coupleID string, year, month int) ([]models.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.Transaction, 0)
	for _, tx := range f.Transactions {
		if tx.CoupleID != coupleID {
			continue
		}
		if tx.Date.Year() == year && int(tx.Date.Month()) == month {
			result = append(result, tx)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date.After(result[j].Date)
	})
	return result, nil
}

func (r *FileTransactionRepository) ListByUser(_ context.Context, coupleID, userID string) ([]models.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.Transaction, 0)
	for _, tx := range f.Transactions {
		if tx.CoupleID == coupleID && tx.UserID == userID {
			result = append(result, tx)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date.After(result[j].Date)
	})
	return result, nil
}

func (r *FileTransactionRepository) Create(_ context.Context, tx *models.Transaction) (*models.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tx.ID = "txn-" + uuid.NewString()
	tx.CreatedAt = now
	tx.UpdatedAt = now

	f.Transactions = append(f.Transactions, *tx)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return tx, nil
}

func (r *FileTransactionRepository) Update(_ context.Context, tx *models.Transaction) (*models.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for i, existing := range f.Transactions {
		if existing.ID == tx.ID {
			tx.UpdatedAt = time.Now().UTC()
			tx.CreatedAt = existing.CreatedAt // preserve original
			f.Transactions[i] = *tx
			if err := r.save(f); err != nil {
				return nil, err
			}
			return tx, nil
		}
	}
	return nil, fmt.Errorf("transaction %s not found", tx.ID)
}

func (r *FileTransactionRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return err
	}
	for i, tx := range f.Transactions {
		if tx.ID == id {
			f.Transactions = append(f.Transactions[:i], f.Transactions[i+1:]...)
			return r.save(f)
		}
	}
	return fmt.Errorf("transaction %s not found", id)
}

func (r *FileTransactionRepository) MonthlySummary(ctx context.Context, coupleID string, year, month int) (*models.MonthlySummary, error) {
	txns, err := r.ListByMonth(ctx, coupleID, year, month)
	if err != nil {
		return nil, err
	}
	var income, expense int64
	for _, tx := range txns {
		switch tx.Type {
		case "income":
			income += tx.Amount
		case "expense":
			expense += tx.Amount
		}
	}
	return &models.MonthlySummary{
		Year:         year,
		Month:        month,
		TotalIncome:  income,
		TotalExpense: expense,
		Balance:      income - expense,
	}, nil
}

func (r *FileTransactionRepository) SummaryByDateRange(_ context.Context, coupleID string, start, end time.Time) (*models.MonthlySummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}

	var income, expense int64
	for _, tx := range f.Transactions {
		if tx.CoupleID != coupleID {
			continue
		}
		d := tx.Date
		if (d.Equal(start) || d.After(start)) && d.Before(end) {
			switch tx.Type {
			case "income":
				income += tx.Amount
			case "expense":
				expense += tx.Amount
			}
		}
	}
	return &models.MonthlySummary{
		Year:         start.Year(),
		Month:        int(start.Month()),
		TotalIncome:  income,
		TotalExpense: expense,
		Balance:      income - expense,
	}, nil
}

// ─────────────────────────────────────────────
//  FileStockRepository
// ─────────────────────────────────────────────

// FileStockRepository is the JSON-file-backed implementation of StockRepository.
type FileStockRepository struct {
	mu       sync.RWMutex
	filePath string
}

func NewFileStockRepository(filePath string) *FileStockRepository {
	return &FileStockRepository{filePath: filePath}
}

func (r *FileStockRepository) load() (*models.StockAssetsFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading stocks file: %w", err)
	}
	var f models.StockAssetsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing stocks file: %w", err)
	}
	return &f, nil
}

func (r *FileStockRepository) save(f *models.StockAssetsFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileStockRepository) GetByID(_ context.Context, id string) (*models.StockAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, a := range f.StockAssets {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("stock asset %s not found", id)
}

func (r *FileStockRepository) ListByCouple(_ context.Context, coupleID string) ([]models.StockAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.StockAsset, 0)
	for _, a := range f.StockAssets {
		if a.CoupleID == coupleID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (r *FileStockRepository) ListByUser(_ context.Context, coupleID, userID string) ([]models.StockAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.StockAsset, 0)
	for _, a := range f.StockAssets {
		if a.CoupleID == coupleID && a.UserID == userID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (r *FileStockRepository) Create(_ context.Context, asset *models.StockAsset) (*models.StockAsset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	asset.ID = "stock-" + uuid.NewString()
	asset.CreatedAt = now
	asset.UpdatedAt = now

	f.StockAssets = append(f.StockAssets, *asset)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return asset, nil
}

func (r *FileStockRepository) Update(_ context.Context, asset *models.StockAsset) (*models.StockAsset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for i, existing := range f.StockAssets {
		if existing.ID == asset.ID {
			asset.UpdatedAt = time.Now().UTC()
			asset.CreatedAt = existing.CreatedAt
			f.StockAssets[i] = *asset
			if err := r.save(f); err != nil {
				return nil, err
			}
			return asset, nil
		}
	}
	return nil, fmt.Errorf("stock asset %s not found", asset.ID)
}

func (r *FileStockRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return err
	}
	for i, a := range f.StockAssets {
		if a.ID == id {
			f.StockAssets = append(f.StockAssets[:i], f.StockAssets[i+1:]...)
			return r.save(f)
		}
	}
	return fmt.Errorf("stock asset %s not found", id)
}

func (r *FileStockRepository) GetPriceSnapshot(_ context.Context, symbol string) (*models.PriceSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, snap := range f.PriceSnapshots {
		if snap.Symbol == symbol {
			return &snap, nil
		}
	}
	return nil, fmt.Errorf("price snapshot for %s not found", symbol)
}

func (r *FileStockRepository) UpsertPriceSnapshot(_ context.Context, snap *models.PriceSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return err
	}
	for i, existing := range f.PriceSnapshots {
		if existing.Symbol == snap.Symbol {
			f.PriceSnapshots[i] = *snap
			return r.save(f)
		}
	}
	f.PriceSnapshots = append(f.PriceSnapshots, *snap)
	return r.save(f)
}
