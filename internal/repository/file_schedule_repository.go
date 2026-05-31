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

type FileScheduleRepository struct {
	mu       sync.RWMutex
	filePath string
}

func NewFileScheduleRepository(filePath string) *FileScheduleRepository {
	return &FileScheduleRepository{filePath: filePath}
}

func (r *FileScheduleRepository) load() (*models.SchedulesFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.SchedulesFile{Schedules: []models.Schedule{}}, nil
		}
		return nil, fmt.Errorf("reading schedules file: %w", err)
	}
	var f models.SchedulesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing schedules file: %w", err)
	}
	if f.Schedules == nil {
		f.Schedules = []models.Schedule{}
	}
	return &f, nil
}

func (r *FileScheduleRepository) save(f *models.SchedulesFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing schedules: %w", err)
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileScheduleRepository) GetByID(_ context.Context, id string) (*models.Schedule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, s := range f.Schedules {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("schedule %s not found", id)
}

func (r *FileScheduleRepository) ListByCouple(_ context.Context, coupleID string) ([]models.Schedule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.Schedule, 0)
	for _, s := range f.Schedules {
		if s.CoupleID == coupleID {
			result = append(result, s)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartDate.Before(result[j].StartDate)
	})
	return result, nil
}

func (r *FileScheduleRepository) ListByMonth(_ context.Context, coupleID string, year, month int) ([]models.Schedule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	result := make([]models.Schedule, 0)
	for _, s := range f.Schedules {
		if s.CoupleID == coupleID && !s.StartDate.Before(start) && s.StartDate.Before(end) {
			result = append(result, s)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartDate.Before(result[j].StartDate)
	})
	return result, nil
}

func (r *FileScheduleRepository) Create(_ context.Context, s *models.Schedule) (*models.Schedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s.ID = "sch-" + uuid.NewString()
	s.CreatedAt = now
	s.UpdatedAt = now
	f.Schedules = append(f.Schedules, *s)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *FileScheduleRepository) Update(_ context.Context, s *models.Schedule) (*models.Schedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for i, existing := range f.Schedules {
		if existing.ID == s.ID {
			s.UpdatedAt = time.Now().UTC()
			s.CreatedAt = existing.CreatedAt
			s.CoupleID = existing.CoupleID
			f.Schedules[i] = *s
			if err := r.save(f); err != nil {
				return nil, err
			}
			return s, nil
		}
	}
	return nil, fmt.Errorf("schedule %s not found", s.ID)
}

func (r *FileScheduleRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return err
	}
	for i, s := range f.Schedules {
		if s.ID == id {
			f.Schedules = append(f.Schedules[:i], f.Schedules[i+1:]...)
			return r.save(f)
		}
	}
	return fmt.Errorf("schedule %s not found", id)
}
