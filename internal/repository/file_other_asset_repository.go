// file_other_asset_repository.go — JSON-file-backed implementation of OtherAssetRepository.
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

// FileOtherAssetRepository implements OtherAssetRepository using other_assets.json.
type FileOtherAssetRepository struct {
	mu       sync.RWMutex
	filePath string
}

func NewFileOtherAssetRepository(filePath string) *FileOtherAssetRepository {
	return &FileOtherAssetRepository{filePath: filePath}
}

func (r *FileOtherAssetRepository) load() (*models.OtherAssetsFile, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading other_assets file: %w", err)
	}
	var f models.OtherAssetsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing other_assets file: %w", err)
	}
	return &f, nil
}

func (r *FileOtherAssetRepository) save(f *models.OtherAssetsFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing other_assets: %w", err)
	}
	return os.WriteFile(r.filePath, data, 0644)
}

func (r *FileOtherAssetRepository) GetByID(_ context.Context, id string) (*models.OtherAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for _, a := range f.OtherAssets {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("asset %s not found", id)
}

func (r *FileOtherAssetRepository) ListByCouple(_ context.Context, coupleID string) ([]models.OtherAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.OtherAsset, 0)
	for _, a := range f.OtherAssets {
		if a.CoupleID == coupleID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (r *FileOtherAssetRepository) ListByUser(_ context.Context, coupleID, userID string) ([]models.OtherAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.OtherAsset, 0)
	for _, a := range f.OtherAssets {
		if a.CoupleID == coupleID && a.UserID == userID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (r *FileOtherAssetRepository) ListByType(_ context.Context, coupleID string, assetType models.OtherAssetType) ([]models.OtherAsset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	result := make([]models.OtherAsset, 0)
	for _, a := range f.OtherAssets {
		if a.CoupleID == coupleID && a.AssetType == assetType {
			result = append(result, a)
		}
	}
	return result, nil
}

func (r *FileOtherAssetRepository) Create(_ context.Context, asset *models.OtherAsset) (*models.OtherAsset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	asset.ID = "asset-" + uuid.NewString()
	asset.CreatedAt = now
	asset.UpdatedAt = now

	f.OtherAssets = append(f.OtherAssets, *asset)
	if err := r.save(f); err != nil {
		return nil, err
	}
	return asset, nil
}

func (r *FileOtherAssetRepository) Update(_ context.Context, asset *models.OtherAsset) (*models.OtherAsset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return nil, err
	}
	for i, existing := range f.OtherAssets {
		if existing.ID == asset.ID {
			asset.UpdatedAt = time.Now().UTC()
			asset.CreatedAt = existing.CreatedAt // 생성일 보존
			asset.CoupleID = existing.CoupleID  // 커플 ID 변경 방지
			f.OtherAssets[i] = *asset
			if err := r.save(f); err != nil {
				return nil, err
			}
			return asset, nil
		}
	}
	return nil, fmt.Errorf("asset %s not found", asset.ID)
}

func (r *FileOtherAssetRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := r.load()
	if err != nil {
		return err
	}
	for i, a := range f.OtherAssets {
		if a.ID == id {
			f.OtherAssets = append(f.OtherAssets[:i], f.OtherAssets[i+1:]...)
			return r.save(f)
		}
	}
	return fmt.Errorf("asset %s not found", id)
}
