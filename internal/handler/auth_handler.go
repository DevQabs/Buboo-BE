package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
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
		if h.rejectForeignInvite(w, r, req.InviteCode, user) {
			return
		}
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
			if h.rejectForeignInvite(w, r, req.InviteCode, user) {
				return
			}
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

	// 3. Invite code flow: consume the invite and create the member atomically
	if req.InviteCode != "" {
		created, err := h.inviteRepo.RedeemInvite(ctx, req.InviteCode, models.User{
			ID:        uuid.New().String(),
			Name:      models.CapName(req.Nickname), // 비면 초대에 적힌 이름, 그다음 메일 앞부분
			Email:     req.GoogleEmail,
			GoogleSub: req.GoogleSub,
		}, nameFromEmail(req.GoogleEmail))
		if errors.Is(err, repository.ErrInviteInvalid) {
			respondError(w, http.StatusBadRequest, err)
			return
		}
		if errors.Is(err, repository.ErrUserExists) {
			respondError(w, http.StatusConflict, err)
			return
		}
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
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

// rejectForeignInvite 는 초대 코드를 들고 온 기존 계정이 초대한 그룹이 아닌 다른 그룹
// 사람이면 409 를 쓰고 true 를 돌려준다. 그대로 로그인시키면 초대받은 그룹이 아닌
// 원래 그룹으로 들어가 버린다. 이미 쓴·만료된 코드면 평소처럼 로그인시킨다.
func (h *Handler) rejectForeignInvite(w http.ResponseWriter, r *http.Request, code string, user *models.User) bool {
	if code == "" {
		return false
	}
	inv, err := h.inviteRepo.GetInviteByCode(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return true
	}
	if inv.CoupleID != user.CoupleID {
		respondError(w, http.StatusConflict, errorf("이미 다른 그룹에 가입된 계정입니다"))
		return true
	}
	return false
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
		AvatarColor: models.DefaultAvatarColor(req.Role),
		IsOwner:     true,
	}
	createdUser, err := h.userRepo.CreateUser(ctx, newUser)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// Generate initial invite for partner
	invite, err := h.inviteRepo.CreateInvite(ctx, createdCouple.ID, createdUser.ID, "spouse", "")
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

	respondJSON(w, http.StatusOK, map[string]any{
		"couple_id":   invite.CoupleID,
		"couple_name": couple.Name,
		"preset":      invite.Preset,
		"nickname":    invite.Nickname,
		"expires_at":  invite.ExpiresAt,
	})
}

// ─────────────────────────────────────────────
//  POST /api/auth/invite  (protected)
//  Generate a new invite code for the caller's couple.
// ─────────────────────────────────────────────

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Preset   string `json:"preset"`
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.Preset == "" {
		req.Preset = "spouse"
	}
	if _, ok := models.PresetLevels(req.Preset); !ok {
		respondError(w, http.StatusBadRequest, errorf("preset must be spouse|grandparent|custom"))
		return
	}
	nickname := strings.TrimSpace(req.Nickname)
	if utf8.RuneCountInString(nickname) > 30 {
		respondError(w, http.StatusBadRequest, errorf("nickname must be 30 characters or fewer"))
		return
	}

	invite, err := h.inviteRepo.CreateInvite(r.Context(),
		auth.CoupleIDFromCtx(r.Context()), auth.UserIDFromCtx(r.Context()), req.Preset, nickname)
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

func errorf(msg string) error {
	return &simpleError{msg}
}

type simpleError struct{ s string }

func (e *simpleError) Error() string { return e.s }
