package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
)

type permsKey struct{}

// loadPermissions reads the caller's menu levels from the DB on every request.
// They are not in the JWT: tokens live 30 days and an owner's change must apply
// at once. A token whose user left the group is refused here.
func (h *Handler) loadPermissions(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		p, err := h.permRepo.Get(ctx, auth.UserIDFromCtx(ctx), auth.CoupleIDFromCtx(ctx))
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		if p == nil {
			respondError(w, http.StatusForbidden, errors.New("not a member"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, permsKey{}, *p)))
	})
}

func permsFromCtx(ctx context.Context) models.Permissions {
	p, _ := ctx.Value(permsKey{}).(models.Permissions)
	return p
}

// requireMenu: GET/HEAD need view+ on any of menus, other methods need edit on
// menus[0]. With no menus only the owner passes. Mount after loadPermissions.
func requireMenu(menus ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			write := r.Method != http.MethodGet && r.Method != http.MethodHead
			if !permsFromCtx(r.Context()).Allows(write, menus...) {
				respondError(w, http.StatusForbidden, errors.New("forbidden"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GET /api/me/permissions
func (h *Handler) myPermissions(w http.ResponseWriter, r *http.Request) {
	p := permsFromCtx(r.Context())
	respondJSON(w, http.StatusOK, map[string]any{"is_owner": p.IsOwner, "menus": p.Effective()})
}
