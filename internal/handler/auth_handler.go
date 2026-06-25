package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
)

// ─────────────────────────────────────────────
//  POST /api/auth/callback
//  Called by NextAuth jwt callback after Google OAuth.
// ─────────────────────────────────────────────

type authCallbackRequest struct {
	GoogleEmail string `json:"google_email"`
	GoogleSub   string `json:"google_sub"`
	InviteCode  string `json:"invite_code,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
}

func (h *Handler) authCallback(w http.ResponseWriter, r *http.Request) {
	var req authCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.GoogleSub == "" {
		respondError(w, http.StatusBadRequest, errorf("google_sub required"))
		return
	}

	ctx := r.Context()

	// 1. Look up by google_sub (normal subsequent login)
	user, err := h.userRepo.GetUserByGoogleSub(ctx, req.GoogleSub)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if user != nil {
		token, err := auth.IssueToken(h.jwtSecret, user.ID, user.CoupleID, user.Role)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"access_token": token, "user": user})
		return
	}

	// 2. Migration fallback: look up by email, link google_sub
	if req.GoogleEmail != "" {
		user, err = h.userRepo.GetUserByEmail(ctx, req.GoogleEmail)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		if user != nil {
			if err := h.userRepo.UpdateUserGoogleSub(ctx, user.ID, req.GoogleSub); err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			user.GoogleSub = req.GoogleSub
			token, err := auth.IssueToken(h.jwtSecret, user.ID, user.CoupleID, user.Role)
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			respondJSON(w, http.StatusOK, map[string]any{"access_token": token, "user": user})
			return
		}
	}

	// 3. Invite code flow: create user and link to couple
	if req.InviteCode != "" {
		invite, err := h.inviteRepo.GetInviteByCode(ctx, req.InviteCode)
		if err != nil || invite == nil {
			respondError(w, http.StatusBadRequest, errorf("invalid or expired invite code"))
			return
		}

		// Determine which role is still available
		role := ""
		if invite.Role != nil {
			role = *invite.Role
		} else {
			existing, _ := h.userRepo.ListUsers(ctx, invite.CoupleID)
			taken := map[string]bool{}
			for _, u := range existing {
				taken[u.Role] = true
			}
			if !taken["husband"] {
				role = "husband"
			} else {
				role = "wife"
			}
		}

		nickname := req.Nickname
		if nickname == "" {
			nickname = nameFromEmail(req.GoogleEmail)
		}
		newUser := &models.User{
			ID:          uuid.New().String(),
			CoupleID:    invite.CoupleID,
			Name:        nickname,
			Email:       req.GoogleEmail,
			GoogleSub:   req.GoogleSub,
			Role:        role,
			AvatarColor: defaultColor(role),
		}
		created, err := h.userRepo.CreateUser(ctx, newUser)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		_ = h.inviteRepo.MarkInviteUsed(ctx, invite.ID, created.ID)

		token, err := auth.IssueToken(h.jwtSecret, created.ID, created.CoupleID, created.Role)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"access_token": token, "user": created})
		return
	}

	// 4. No existing user, no invite — prompt setup
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "new_user"})
}

// ─────────────────────────────────────────────
//  POST /api/auth/setup
//  First-time couple creation.
// ─────────────────────────────────────────────

type authSetupRequest struct {
	GoogleEmail string `json:"google_email"`
	GoogleSub   string `json:"google_sub"`
	CoupleName  string `json:"couple_name"`
	Role        string `json:"role"` // "husband" | "wife"
	Nickname    string `json:"nickname,omitempty"`
}

func (h *Handler) authSetup(w http.ResponseWriter, r *http.Request) {
	var req authSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.GoogleSub == "" || req.CoupleName == "" || (req.Role != "husband" && req.Role != "wife") {
		respondError(w, http.StatusBadRequest, errorf("google_sub, couple_name, and role (husband|wife) required"))
		return
	}

	ctx := r.Context()

	// Guard: reject if user already exists
	existing, _ := h.userRepo.GetUserByGoogleSub(ctx, req.GoogleSub)
	if existing != nil {
		respondError(w, http.StatusConflict, errorf("user already registered"))
		return
	}

	// Create couple
	couple := &models.Couple{
		ID:             uuid.New().String(),
		Name:           req.CoupleName,
		MonthlyBudget:  0,
		LedgerStartDay: 1,
		Currency:       "KRW",
	}
	createdCouple, err := h.userRepo.CreateCouple(ctx, couple)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// Create user
	setupNickname := req.Nickname
	if setupNickname == "" {
		setupNickname = nameFromEmail(req.GoogleEmail)
	}
	newUser := &models.User{
		ID:          uuid.New().String(),
		CoupleID:    createdCouple.ID,
		Name:        setupNickname,
		Email:       req.GoogleEmail,
		GoogleSub:   req.GoogleSub,
		Role:        req.Role,
		AvatarColor: defaultColor(req.Role),
	}
	createdUser, err := h.userRepo.CreateUser(ctx, newUser)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// Generate initial invite for partner
	invite, err := h.inviteRepo.CreateInvite(ctx, createdCouple.ID, createdUser.ID, nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	token, err := auth.IssueToken(h.jwtSecret, createdUser.ID, createdCouple.ID, createdUser.Role)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"access_token": token,
		"user":         createdUser,
		"couple":       createdCouple,
		"invite_code":  invite.Code,
	})
}

// ─────────────────────────────────────────────
//  GET /api/auth/invite/{code}
//  Returns invite info for the invite acceptance page.
// ─────────────────────────────────────────────

func (h *Handler) getInviteByCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	ctx := r.Context()

	invite, err := h.inviteRepo.GetInviteByCode(ctx, code)
	if err != nil || invite == nil {
		respondError(w, http.StatusNotFound, errorf("invite not found or expired"))
		return
	}

	couple, err := h.userRepo.GetCouple(ctx, invite.CoupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	existingUsers, _ := h.userRepo.ListUsers(ctx, invite.CoupleID)
	taken := map[string]bool{}
	for _, u := range existingUsers {
		taken[u.Role] = true
	}

	available := []string{}
	if !taken["husband"] {
		available = append(available, "husband")
	}
	if !taken["wife"] {
		available = append(available, "wife")
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"couple_id":       invite.CoupleID,
		"couple_name":     couple.Name,
		"available_roles": available,
		"expires_at":      invite.ExpiresAt,
	})
}

// ─────────────────────────────────────────────
//  POST /api/auth/invite  (protected)
//  Generate a new invite code for the caller's couple.
// ─────────────────────────────────────────────

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	coupleID := auth.CoupleIDFromCtx(r.Context())
	userID := auth.UserIDFromCtx(r.Context())

	invite, err := h.inviteRepo.CreateInvite(r.Context(), coupleID, userID, nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, invite)
}

// ─────────────────────────────────────────────
//  helpers
// ─────────────────────────────────────────────

func nameFromEmail(email string) string {
	if email == "" {
		return "사용자"
	}
	for i, c := range email {
		if c == '@' {
			return email[:i]
		}
	}
	return email
}

func defaultColor(role string) string {
	if role == "wife" {
		return "#FDA4AF"
	}
	return "#0F4C81"
}

func errorf(msg string) error {
	return &simpleError{msg}
}

type simpleError struct{ s string }

func (e *simpleError) Error() string { return e.s }
