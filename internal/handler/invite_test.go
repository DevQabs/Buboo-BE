package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

type fakeInvites struct {
	repository.InviteRepository
	preset, nickname string
	redeemErr        error
	redeemed         *models.User
	fallback         string
}

func (f *fakeInvites) CreateInvite(_ context.Context, coupleID, _, preset, nickname string) (*models.Invite, error) {
	f.preset, f.nickname = preset, nickname
	return &models.Invite{Code: "ABCD2345", CoupleID: coupleID, Preset: preset, Nickname: nickname}, nil
}

func (f *fakeInvites) RedeemInvite(_ context.Context, _ string, u models.User, fallbackName string) (*models.User, error) {
	if f.redeemErr != nil {
		return nil, f.redeemErr
	}
	f.redeemed, f.fallback = &u, fallbackName
	u.CoupleID, u.Role = "c1", "family"
	return &u, nil
}

func (f *fakeUsers) GetUserByGoogleSub(context.Context, string) (*models.User, error) {
	return nil, nil
}
func (f *fakeUsers) GetUserByEmail(context.Context, string) (*models.User, error) { return nil, nil }

func TestCreateInvite(t *testing.T) {
	cases := []struct {
		name, body, wantPreset, wantNick string
		want                             int
	}{
		{"본문 없으면 배우자", "", "spouse", "", http.StatusCreated},
		{"조부모 + 이름", `{"preset":"grandparent","nickname":" 친정엄마 "}`, "grandparent", "친정엄마", http.StatusCreated},
		{"모르는 묶음 거부", `{"preset":"boss"}`, "", "", http.StatusBadRequest},
		{"이름 30자 초과 거부", `{"nickname":"` + strings.Repeat("가", 31) + `"}`, "", "", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := &fakeInvites{}
			h := newPermHandler(&fakePerms{perms: map[string]*models.Permissions{"owner": ownerPerms}}, nil)
			h.inviteRepo = inv
			rec := do(h, "owner", http.MethodPost, "/api/auth/invite", c.body)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
			if inv.preset != c.wantPreset || inv.nickname != c.wantNick {
				t.Errorf("저장 값: preset=%q nickname=%q", inv.preset, inv.nickname)
			}
		})
	}
}

func TestAuthCallbackInvite(t *testing.T) {
	body := `{"google_sub":"g1","google_email":"mom@example.com","invite_code":"ABCD2345"}`
	run := func(inv *fakeInvites) int {
		h := newPermHandler(&fakePerms{}, nil)
		h.inviteRepo = inv
		h.internalSecret = []byte("s3cret")
		return do(h, "", http.MethodPost, "/api/auth/callback", body).Code
	}

	inv := &fakeInvites{}
	if got := run(inv); got != http.StatusOK {
		t.Fatalf("정상 가입: got %d", got)
	}
	if inv.redeemed.GoogleSub != "g1" || inv.redeemed.Name != "" || inv.fallback != "mom" {
		t.Errorf("이름은 비워 넘기고(초대 이름 우선) 메일 앞부분을 예비로: %+v fallback=%q", inv.redeemed, inv.fallback)
	}
	if got := run(&fakeInvites{redeemErr: repository.ErrInviteInvalid}); got != http.StatusBadRequest {
		t.Errorf("쓴 초대·만료 초대는 400: got %d", got)
	}
	if got := run(&fakeInvites{redeemErr: repository.ErrUserExists}); got != http.StatusConflict {
		t.Errorf("이미 다른 계정이 쓰는 메일·구글 계정은 409: got %d", got)
	}
	if got := run(&fakeInvites{redeemErr: errors.New("db down")}); got != http.StatusInternalServerError {
		t.Errorf("DB 오류는 500: got %d", got)
	}
}
