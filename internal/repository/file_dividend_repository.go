// file_dividend_repository.go implements DividendRepository using local JSON files.
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

// FileDividendRepository is the JSON-file-backed implementation.
type FileDividendRepository struct {
	mu       sync.RWMutex
	filePath string
}

// NewFileDividendRepository constructs the repository for the given JSON path.
func NewFileDividendRepository(filePath string) *FileDividendRepository {
	return &FileDividendRepository{filePath: filePath}
}

func (r *FileDividendRepository) load() (*models.DividendEventsFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading dividends file: %w", err)
	}
	var f models.DividendEventsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing dividends file: %w", err)
	}
	if f.Dividends == nil {
		f.Dividends = make([]models.DividendEvent, 0)
	}
	return &f, nil
}

func (r *FileDividendRepository) save(f *models.DividendEventsFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing dividends: %w", err)
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileDividendRepository) GetByID(_ context.Context, id string) (*models.DividendEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, d := range f.Dividends {
		if d.ID == id {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("dividend event %s not found", id)
}

func (r *FileDividendRepository) ListByCouple(_ context.Context, coupleID string) ([]models.DividendEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.DividendEvent, 0)
	for _, d := range f.Dividends {
		if d.CoupleID == coupleID {
			result = append(result, d)
		}
	}
	// newest payment_date first
	sort.Slice(result, func(i, j int) bool {
		return result[i].PaymentDate.After(result[j].PaymentDate)
	})
	return result, nil
}

func (r *FileDividendRepository) ListByYear(_ context.Context, coupleID string, year int) ([]models.DividendEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.DividendEvent, 0)
	for _, d := range f.Dividends {
		if d.CoupleID == coupleID && d.PaymentDate.Year() == year {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PaymentDate.After(result[j].PaymentDate)
	})
	return result, nil
}

func (r *FileDividendRepository) Create(_ context.Context, d *models.DividendEvent) (*models.DividendEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	d.ID = "div-" + uuid.NewString()
	d.IsApplied = false
	d.CreatedAt = now
	d.UpdatedAt = now

	f.Dividends = append(f.Dividends, *d)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *FileDividendRepository) MarkApplied(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return err
	}
	for i, d := range f.Dividends {
		if d.ID == id {
			f.Dividends[i].IsApplied = true
			f.Dividends[i].UpdatedAt = time.Now().UTC()
			return r.save(f)
		}
	}
	return fmt.Errorf("dividend event %s not found", id)
}

func (r *FileDividendRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return err
	}
	for i, d := range f.Dividends {
		if d.ID == id {
			f.Dividends = append(f.Dividends[:i], f.Dividends[i+1:]...)
			return r.save(f)
		}
	}
	return fmt.Errorf("dividend event %s not found", id)
}
