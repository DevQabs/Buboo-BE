// user_repository.go — JSON-file-backed implementation of UserRepository.
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/yourname/couple-app/internal/models"
)

// FileUserRepository implements UserRepository using users.json.
type FileUserRepository struct {
	mu       sync.RWMutex
	filePath string
}

func NewFileUserRepository(filePath string) *FileUserRepository {
	return &FileUserRepository{filePath: filePath}
}

func (r *FileUserRepository) load() (*models.UsersFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading users file: %w", err)
	}
	var f models.UsersFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing users file: %w", err)
	}
	return &f, nil
}

func (r *FileUserRepository) GetUser(_ context.Context, userID string) (*models.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, u := range f.Users {
		if u.ID == userID {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("user %s not found", userID)
}

func (r *FileUserRepository) GetUserByGoogleSub(_ context.Context, _ string) (*models.User, error) {
	return nil, fmt.Errorf("not supported by FileUserRepository")
}

func (r *FileUserRepository) GetUserByEmail(_ context.Context, _ string) (*models.User, error) {
	return nil, fmt.Errorf("not supported by FileUserRepository")
}

func (r *FileUserRepository) CreateUser(_ context.Context, _ *models.User) (*models.User, error) {
	return nil, fmt.Errorf("not supported by FileUserRepository")
}

func (r *FileUserRepository) CreateCouple(_ context.Context, _ *models.Couple) (*models.Couple, error) {
	return nil, fmt.Errorf("not supported by FileUserRepository")
}

func (r *FileUserRepository) UpdateUserGoogleSub(_ context.Context, _, _ string) error {
	return fmt.Errorf("not supported by FileUserRepository")
}

func (r *FileUserRepository) ListUsers(_ context.Context, _ string) ([]models.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	return f.Users, nil
}

func (r *FileUserRepository) GetCouple(_ context.Context, coupleID string) (*models.Couple, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	if f.Couple.ID == coupleID {
		return &f.Couple, nil
	}
	return nil, fmt.Errorf("couple %s not found", coupleID)
}

func (r *FileUserRepository) UpdateCouple(_ context.Context, coupleID string, monthlyBudget int64, ledgerStartDay int) (*models.Couple, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	if f.Couple.ID != coupleID {
		return nil, fmt.Errorf("couple %s not found", coupleID)
	}
	f.Couple.MonthlyBudget = monthlyBudget
	if ledgerStartDay >= 1 && ledgerStartDay <= 28 {
		f.Couple.LedgerStartDay = ledgerStartDay
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serializing users file: %w", err)
	}
	if err := os.WriteFile(r.filePath, data, 0644); err != nil {
		return nil, fmt.Errorf("writing users file: %w", err)
	}
	return &f.Couple, nil
}
