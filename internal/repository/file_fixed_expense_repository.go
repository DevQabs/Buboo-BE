// file_fixed_expense_repository.go implements FixedExpenseRepository
// using a local JSON file as the backing store.
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

// FileFixedExpenseRepository is the JSON-file-backed implementation.
type FileFixedExpenseRepository struct {
	mu       sync.RWMutex
	filePath string
}

// NewFileFixedExpenseRepository constructs the repository for the given JSON path.
func NewFileFixedExpenseRepository(filePath string) *FileFixedExpenseRepository {
	return &FileFixedExpenseRepository{filePath: filePath}
}

func (r *FileFixedExpenseRepository) load() (*models.FixedExpensesFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading fixed_expenses file: %w", err)
	}
	var f models.FixedExpensesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing fixed_expenses file: %w", err)
	}
	if f.FixedExpenses == nil {
		f.FixedExpenses = make([]models.FixedExpense, 0)
	}
	return &f, nil
}

func (r *FileFixedExpenseRepository) save(f *models.FixedExpensesFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing fixed_expenses: %w", err)
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileFixedExpenseRepository) GetByID(_ context.Context, id string) (*models.FixedExpense, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, fe := range f.FixedExpenses {
		if fe.ID == id {
			return &fe, nil
		}
	}
	return nil, fmt.Errorf("fixed expense %s not found", id)
}

func (r *FileFixedExpenseRepository) ListByCouple(_ context.Context, coupleID string) ([]models.FixedExpense, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.FixedExpense, 0)
	for _, fe := range f.FixedExpenses {
		if fe.CoupleID == coupleID {
			result = append(result, fe)
		}
	}
	return result, nil
}

func (r *FileFixedExpenseRepository) Create(_ context.Context, fe *models.FixedExpense) (*models.FixedExpense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	fe.ID = "fe-" + uuid.NewString()
	fe.IsActive = true
	fe.CreatedAt = now
	fe.UpdatedAt = now
	if fe.Currency == "" {
		fe.Currency = "KRW"
	}

	f.FixedExpenses = append(f.FixedExpenses, *fe)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return fe, nil
}

func (r *FileFixedExpenseRepository) Update(_ context.Context, fe *models.FixedExpense) (*models.FixedExpense, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for i, existing := range f.FixedExpenses {
		if existing.ID == fe.ID {
			fe.UpdatedAt = time.Now().UTC()
			fe.CreatedAt = existing.CreatedAt
			f.FixedExpenses[i] = *fe
			if err := r.save(f); err != nil {
				return nil, err
			}
			return fe, nil
		}
	}
	return nil, fmt.Errorf("fixed expense %s not found", fe.ID)
}

func (r *FileFixedExpenseRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return err
	}
	for i, fe := range f.FixedExpenses {
		if fe.ID == id {
			f.FixedExpenses = append(f.FixedExpenses[:i], f.FixedExpenses[i+1:]...)
			return r.save(f)
		}
	}
	return fmt.Errorf("fixed expense %s not found", id)
}
