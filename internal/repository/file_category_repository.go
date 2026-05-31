package repository

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"github.com/yourname/couple-app/internal/models"
)

type FileCategoryRepository struct {
	path string
	mu   sync.RWMutex
}

func NewFileCategoryRepository(path string) *FileCategoryRepository {
	return &FileCategoryRepository{path: path}
}

func (r *FileCategoryRepository) Get(_ context.Context, _ string) (*models.Categories, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	data, err := os.ReadFile(r.path)
	if err != nil {
		return &models.Categories{
			Expense: defaultExpenseCategories,
			Income:  defaultIncomeCategories,
		}, nil
	}
	var cats models.Categories
	if err := json.Unmarshal(data, &cats); err != nil {
		return &models.Categories{Expense: defaultExpenseCategories, Income: defaultIncomeCategories}, nil
	}
	return &cats, nil
}

func (r *FileCategoryRepository) Update(_ context.Context, _ string, cats *models.Categories) (*models.Categories, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := json.MarshalIndent(cats, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(r.path, data, 0644); err != nil {
		return nil, err
	}
	return cats, nil
}
