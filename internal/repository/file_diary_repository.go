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

type FileDiaryRepository struct {
	mu       sync.RWMutex
	filePath string
}

func NewFileDiaryRepository(filePath string) *FileDiaryRepository {
	return &FileDiaryRepository{filePath: filePath}
}

func (r *FileDiaryRepository) load() (*models.DiariesFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.DiariesFile{Diaries: []models.DiaryEntry{}}, nil
		}
		return nil, fmt.Errorf("reading diaries file: %w", err)
	}
	var f models.DiariesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing diaries file: %w", err)
	}
	if f.Diaries == nil {
		f.Diaries = []models.DiaryEntry{}
	}
	return &f, nil
}

func (r *FileDiaryRepository) save(f *models.DiariesFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing diaries: %w", err)
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileDiaryRepository) GetByID(_ context.Context, id string) (*models.DiaryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, d := range f.Diaries {
		if d.ID == id {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("diary %s not found", id)
}

func (r *FileDiaryRepository) GetByDate(_ context.Context, coupleID, date string) (*models.DiaryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, d := range f.Diaries {
		if d.CoupleID == coupleID && d.Date == date {
			return &d, nil
		}
	}
	return nil, nil
}

func (r *FileDiaryRepository) ListByCouple(_ context.Context, coupleID string) ([]models.DiaryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.DiaryEntry, 0)
	for _, d := range f.Diaries {
		if d.CoupleID == coupleID {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date > result[j].Date
	})
	return result, nil
}

func (r *FileDiaryRepository) ListByMonth(_ context.Context, coupleID string, year, month int) ([]models.DiaryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%04d-%02d", year, month)
	result := make([]models.DiaryEntry, 0)
	for _, d := range f.Diaries {
		if d.CoupleID == coupleID && len(d.Date) >= 7 && d.Date[:7] == prefix {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date > result[j].Date
	})
	return result, nil
}

func (r *FileDiaryRepository) Create(_ context.Context, d *models.DiaryEntry) (*models.DiaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	d.ID = "diary-" + uuid.NewString()
	if d.Photos == nil {
		d.Photos = []string{}
	}
	d.CreatedAt = now
	d.UpdatedAt = now
	f.Diaries = append(f.Diaries, *d)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *FileDiaryRepository) Update(_ context.Context, d *models.DiaryEntry) (*models.DiaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for i, existing := range f.Diaries {
		if existing.ID == d.ID {
			d.UpdatedAt = time.Now().UTC()
			d.CreatedAt = existing.CreatedAt
			d.CoupleID = existing.CoupleID
			d.Photos = existing.Photos
			f.Diaries[i] = *d
			if err := r.save(f); err != nil {
				return nil, err
			}
			return d, nil
		}
	}
	return nil, fmt.Errorf("diary %s not found", d.ID)
}

func (r *FileDiaryRepository) AddPhoto(_ context.Context, id, filename string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return err
	}
	for i, d := range f.Diaries {
		if d.ID == id {
			f.Diaries[i].Photos = append(f.Diaries[i].Photos, filename)
			f.Diaries[i].UpdatedAt = time.Now().UTC()
			return r.save(f)
		}
	}
	return fmt.Errorf("diary %s not found", id)
}

func (r *FileDiaryRepository) DeletePhoto(_ context.Context, id, filename string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return err
	}
	for i, d := range f.Diaries {
		if d.ID == id {
			photos := make([]string, 0, len(d.Photos))
			for _, p := range d.Photos {
				if p != filename {
					photos = append(photos, p)
				}
			}
			f.Diaries[i].Photos = photos
			f.Diaries[i].UpdatedAt = time.Now().UTC()
			return r.save(f)
		}
	}
	return fmt.Errorf("diary %s not found", id)
}

func (r *FileDiaryRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.load()
	if err != nil {
		return err
	}
	for i, d := range f.Diaries {
		if d.ID == id {
			f.Diaries = append(f.Diaries[:i], f.Diaries[i+1:]...)
			return r.save(f)
		}
	}
	return fmt.Errorf("diary %s not found", id)
}
