package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
)

// 업로드 규칙 (설계서 5절). GB 는 십진(10^9) — 무료 10GB 보다 작게 잡는 쪽.
const (
	maxObjectBytes   = 100 << 20 // 파일당 100MB
	maxVideoSeconds  = 30.5
	storageWarnBytes = 8_000_000_000
	presignPutTTL    = 15 * time.Minute
	presignGetTTL    = time.Hour
)

// uploadTypes: purpose -> content type -> key extension. The extension is
// chosen here, so contentTypeOfKey can recover the type from a key later.
var uploadTypes = map[string]map[string]string{
	"diary": {"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp", "image/heic": "heic"},
	"baby": {"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp", "image/heic": "heic",
		"video/mp4": "mp4", "video/quicktime": "mov"},
}

// uploadMenus: purpose -> menu that needs edit to upload.
var uploadMenus = map[string]string{"diary": "diary", "baby": "baby_photo"}

type uploadRequest struct {
	Purpose     string  `json:"purpose"`
	ContentType string  `json:"content_type"`
	Size        int64   `json:"size"`
	DurationSec float64 `json:"duration_sec"`
}

// checkUpload validates type, size, video length and the bucket cap.
// It returns the key extension, or an HTTP status with a message.
func checkUpload(req uploadRequest, usedBytes, limitBytes int64) (string, int, error) {
	types, ok := uploadTypes[req.Purpose]
	if !ok {
		return "", http.StatusBadRequest, errors.New("알 수 없는 업로드 목적입니다")
	}
	ext, ok := types[req.ContentType]
	if !ok {
		return "", http.StatusBadRequest, errors.New("올릴 수 없는 파일 형식입니다")
	}
	if req.Size <= 0 || req.Size > maxObjectBytes {
		return "", http.StatusBadRequest, errors.New("파일은 100MB 까지 올릴 수 있습니다")
	}
	if strings.HasPrefix(req.ContentType, "video/") && (req.DurationSec <= 0 || req.DurationSec > maxVideoSeconds) {
		return "", http.StatusBadRequest, errors.New("영상은 30초 이하만 올릴 수 있습니다")
	}
	if usedBytes+req.Size > limitBytes {
		return "", http.StatusRequestEntityTooLarge, errors.New("저장 공간 한도에 도달했습니다. 관리자에게 문의하세요")
	}
	return ext, 0, nil
}

func objectKey(coupleID, purpose, ext string) string {
	return coupleID + "/" + purpose + "/" + uuid.NewString() + "." + ext
}

// keyBelongs reports whether key is a file directly under {coupleID}/{purpose}/.
// Confirm requests use it so nobody attaches another couple's object.
func keyBelongs(coupleID, purpose, key string) bool {
	if coupleID == "" {
		return false
	}
	name, ok := strings.CutPrefix(key, coupleID+"/"+purpose+"/")
	return ok && name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

// contentTypeOfKey maps a key's extension back to the content type it was issued for.
func contentTypeOfKey(purpose, key string) string {
	ext := strings.TrimPrefix(path.Ext(key), ".")
	for ct, e := range uploadTypes[purpose] {
		if e == ext {
			return ct
		}
	}
	return ""
}

// POST /api/media/upload-url — 검사 후 15분 presigned PUT 주소와 키를 준다.
// 발급 때 요청 크기로 storage_objects 에 기록해 상한 계산에 바로 넣는다.
// ponytail: 동시 발급 두 건이 같은 합계를 읽으면 상한을 조금 넘을 수 있음 — 9.5GB 여유로 흡수, 문제되면 advisory lock.
// ponytail: 발급만 받고 안 올린 키도 합계에 남는다(안전한 쪽) — 쌓이면 정리 명령 추가.
func (h *Handler) createUploadURL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req uploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	menu, ok := uploadMenus[req.Purpose]
	if !ok {
		respondError(w, http.StatusBadRequest, errors.New("알 수 없는 업로드 목적입니다"))
		return
	}
	if !permsFromCtx(ctx).Allows(true, menu) {
		respondError(w, http.StatusForbidden, errors.New("forbidden"))
		return
	}
	if h.media == nil {
		respondError(w, http.StatusServiceUnavailable, errors.New("저장소가 설정되지 않았습니다"))
		return
	}
	used, err := h.storageRepo.TotalBytes(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	ext, status, err := checkUpload(req, used, h.storageLimit)
	if err != nil {
		respondError(w, status, err)
		return
	}
	coupleID := auth.CoupleIDFromCtx(ctx)
	key := objectKey(coupleID, req.Purpose, ext)
	url, err := h.media.PresignPut(ctx, key, req.ContentType, req.Size, presignPutTTL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if err := h.storageRepo.Record(ctx, models.StorageObject{
		Key: key, CoupleID: coupleID, Purpose: req.Purpose, SizeBytes: req.Size,
	}); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"key": key, "url": url})
}

// GET /api/storage/usage (owner 전용)
func (h *Handler) storageUsage(w http.ResponseWriter, r *http.Request) {
	used, err := h.storageRepo.TotalBytes(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]int64{
		"used_bytes": used, "limit_bytes": h.storageLimit, "warn_bytes": storageWarnBytes,
	})
}
