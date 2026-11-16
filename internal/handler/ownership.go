package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/auth"
)

// OwnershipChecker answers whether a row in table belongs to the couple.
type OwnershipChecker interface {
	Owns(ctx context.Context, table, id, coupleID string) (bool, error)
}

// owned rejects requests whose {id} is not a row of the caller's couple.
// Mounted on every /{id} route so the repositories' id-only queries can
// never reach another couple's data.
func (h *Handler) owned(table string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, err := h.owner.Owns(r.Context(), table, chi.URLParam(r, "id"), auth.CoupleIDFromCtx(r.Context()))
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			if !ok {
				respondError(w, http.StatusNotFound, errors.New("not found"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
