package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
)

// GET /api/members (owner)
func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)
	users, err := h.userRepo.ListUsers(ctx, coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	levels, err := h.permRepo.ListByCouple(ctx, coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]models.Member, 0, len(users))
	for _, u := range users {
		p := models.Permissions{IsOwner: u.IsOwner, Menus: levels[u.ID]}
		out = append(out, models.Member{User: u, Menus: p.Effective()})
	}
	respondJSON(w, http.StatusOK, out)
}

// memberTarget resolves {id} to a non-owner member of the caller's couple,
// writing the error response itself when it cannot.
func (h *Handler) memberTarget(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	users, err := h.userRepo.ListUsers(r.Context(), auth.CoupleIDFromCtx(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return nil, false
	}
	id := chi.URLParam(r, "id")
	for i := range users {
		if users[i].ID != id {
			continue
		}
		if users[i].IsOwner {
			respondError(w, http.StatusBadRequest, errors.New("owner 의 권한은 바꾸거나 내보낼 수 없습니다"))
			return nil, false
		}
		return &users[i], true
	}
	respondError(w, http.StatusNotFound, errors.New("not found"))
	return nil, false
}

// PUT /api/members/{id}/permissions (owner). Body carries the full map; a
// missing menu becomes hidden.
func (h *Handler) putMemberPermissions(w http.ResponseWriter, r *http.Request) {
	target, ok := h.memberTarget(w, r)
	if !ok {
		return
	}
	var req struct {
		Menus map[string]string `json:"menus"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.Menus == nil {
		respondError(w, http.StatusBadRequest, errors.New("menus required"))
		return
	}
	for m, l := range req.Menus {
		if !models.ValidMenu(m) || !models.ValidLevel(l) {
			respondError(w, http.StatusBadRequest, errors.New("unknown menu or level: "+m+"="+l))
			return
		}
	}
	if err := h.permRepo.Replace(r.Context(), target.ID, req.Menus); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"menus": models.Permissions{Menus: req.Menus}.Effective()})
}

// DELETE /api/members/{id} (owner). The member's records stay; their token
// stops working at loadPermissions.
func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	target, ok := h.memberTarget(w, r)
	if !ok {
		return
	}
	if err := h.permRepo.RemoveMember(r.Context(), target.ID); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
