package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func validBaby(b models.Baby) error {
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("이름을 입력해 주세요")
	}
	if !validDate(b.BirthDate) {
		return errors.New("생일 형식이 잘못됐습니다")
	}
	if b.Sex != "male" && b.Sex != "female" {
		return errors.New("성별은 male 또는 female 입니다")
	}
	return nil
}

func validGrowth(g *models.BabyGrowth) error {
	if !validDate(g.MeasuredOn) {
		return errors.New("측정일 형식이 잘못됐습니다")
	}
	if g.HeightCm == nil && g.WeightKg == nil && g.HeadCm == nil {
		return errors.New("키·몸무게·머리둘레 중 하나는 입력해 주세요")
	}
	// 열 자릿수에 맞춰 먼저 반올림한다: 키·머리둘레 NUMERIC(5,1), 몸무게 NUMERIC(5,2).
	// 몸무게 열은 999.99 까지라 반올림한 값이 1000 이상이면 넘친다 — 세 값 모두 0 초과 1000 미만만 받는다.
	for _, f := range []struct {
		v     *float64
		scale float64
	}{{g.HeightCm, 10}, {g.WeightKg, 100}, {g.HeadCm, 10}} {
		if f.v == nil {
			continue
		}
		*f.v = math.Round(*f.v*f.scale) / f.scale
		if *f.v <= 0 || *f.v >= 1000 {
			return errors.New("측정값이 범위를 벗어났습니다")
		}
	}
	return nil
}

// GET /api/baby — 미등록이면 null
func (h *Handler) getBaby(w http.ResponseWriter, r *http.Request) {
	b, err := h.babyRepo.Get(r.Context(), auth.CoupleIDFromCtx(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, b)
}

// PUT /api/baby (owner)
func (h *Handler) putBaby(w http.ResponseWriter, r *http.Request) {
	var b models.Baby
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	if err := validBaby(b); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.babyRepo.Put(r.Context(), auth.CoupleIDFromCtx(r.Context()), b); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, b)
}

type momentRequest struct {
	Date     string   `json:"date"`
	Content  string   `json:"content"`
	Media    []string `json:"media"`     // 새 기록: 올린 순서대로 키
	AddMedia []string `json:"add_media"` // 수정: 덧붙일 키
}

// mediaFromKeys confirms each uploaded key and builds rows for it.
func (h *Handler) mediaFromKeys(ctx context.Context, coupleID string, keys []string) ([]models.BabyMedia, error) {
	out := make([]models.BabyMedia, 0, len(keys))
	for _, k := range keys {
		o, err := h.confirmUpload(ctx, coupleID, "baby", k)
		if err != nil {
			return nil, err
		}
		ct := contentTypeOfKey("baby", k)
		kind := "image"
		if strings.HasPrefix(ct, "video/") {
			kind = "video"
		}
		out = append(out, models.BabyMedia{Key: k, Kind: kind, ContentType: ct, SizeBytes: o.SizeBytes})
	}
	return out, nil
}

// momentSaveError: 이미 다른 기록에 붙은 키는 409, 그 밖은 500.
func momentSaveError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrMediaKeyUsed) {
		respondError(w, http.StatusConflict, err)
		return
	}
	respondError(w, http.StatusInternalServerError, err)
}

func (h *Handler) withMediaURLs(ctx context.Context, m *models.BabyMoment) {
	for i := range m.Media {
		m.Media[i].URL = h.mediaURL(ctx, m.Media[i].Key)
	}
}

func (h *Handler) listMoments(w http.ResponseWriter, r *http.Request) {
	ms, err := h.babyRepo.ListMoments(r.Context(), auth.CoupleIDFromCtx(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range ms {
		h.withMediaURLs(r.Context(), &ms[i])
	}
	respondJSON(w, http.StatusOK, ms)
}

func (h *Handler) createMoment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)
	var req momentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if !validDate(req.Date) {
		respondError(w, http.StatusBadRequest, errors.New("날짜 형식이 잘못됐습니다"))
		return
	}
	media, err := h.mediaFromKeys(ctx, coupleID, req.Media)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	m := &models.BabyMoment{UserID: auth.UserIDFromCtx(ctx), Date: req.Date, Content: req.Content, Media: media}
	if err := h.babyRepo.CreateMoment(ctx, coupleID, m); err != nil {
		momentSaveError(w, err)
		return
	}
	if len(media) > 0 {
		go h.notifyBabyMedia(context.WithoutCancel(ctx), coupleID, auth.UserIDFromCtx(ctx), m.ID, mediaWord(media))
	}
	h.withMediaURLs(ctx, m)
	respondJSON(w, http.StatusCreated, m)
}

func (h *Handler) updateMoment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)
	id := chi.URLParam(r, "id")
	var req momentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if !validDate(req.Date) {
		respondError(w, http.StatusBadRequest, errors.New("날짜 형식이 잘못됐습니다"))
		return
	}
	add, err := h.mediaFromKeys(ctx, coupleID, req.AddMedia)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	m := &models.BabyMoment{ID: id, Date: req.Date, Content: req.Content}
	if err := h.babyRepo.UpdateMoment(ctx, coupleID, m, add); err != nil {
		momentSaveError(w, err)
		return
	}
	if len(add) > 0 {
		go h.notifyBabyMedia(context.WithoutCancel(ctx), coupleID, auth.UserIDFromCtx(ctx), id, mediaWord(add))
	}
	updated, err := h.babyRepo.GetMoment(ctx, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	h.withMediaURLs(ctx, updated)
	respondJSON(w, http.StatusOK, updated)
}

// DELETE /api/baby/moments/{id} — 딸린 사진·영상 객체를 먼저 지운다.
func (h *Handler) deleteMoment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := h.babyRepo.GetMoment(ctx, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	keys := make([]string, len(m.Media))
	for i, md := range m.Media {
		keys[i] = md.Key
	}
	if err := h.removeObjects(ctx, keys); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if err := h.babyRepo.DeleteMoment(ctx, m.ID); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	md, err := h.babyRepo.GetMedia(ctx, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	if err := h.removeObjects(ctx, []string{md.Key}); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if err := h.babyRepo.DeleteMedia(ctx, md.ID); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listGrowth(w http.ResponseWriter, r *http.Request) {
	gs, err := h.babyRepo.ListGrowth(r.Context(), auth.CoupleIDFromCtx(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, gs)
}

func decodeGrowth(r *http.Request) (models.BabyGrowth, error) {
	var g models.BabyGrowth
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		return g, err
	}
	return g, validGrowth(&g)
}

func (h *Handler) createGrowth(w http.ResponseWriter, r *http.Request) {
	g, err := decodeGrowth(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.babyRepo.CreateGrowth(r.Context(), auth.CoupleIDFromCtx(r.Context()), &g); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, g)
}

func (h *Handler) updateGrowth(w http.ResponseWriter, r *http.Request) {
	g, err := decodeGrowth(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	g.ID = chi.URLParam(r, "id")
	if err := h.babyRepo.UpdateGrowth(r.Context(), &g); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, g)
}

func (h *Handler) deleteGrowth(w http.ResponseWriter, r *http.Request) {
	if err := h.babyRepo.DeleteGrowth(r.Context(), chi.URLParam(r, "id")); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
