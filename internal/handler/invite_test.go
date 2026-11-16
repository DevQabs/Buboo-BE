package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

type fakeInvites struct {
	repository.InviteRepository
	preset, nickname string
	redeemErr        error
	redeemed         *models.User
	fallback         string
	invite           *models.Invite // GetInviteByCode 결과 (nil = 없는 코드)
}

func (f *fakeInvites) GetInviteByCode(_ context.Context, code string) (*models.Invite, error) {
	if f.invite == nil || f.invite.Code != code {
		return nil, fmt.Errorf("scan invite: %w", pgx.ErrNoRows)
	}
	return f.invite, nil
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

func (f *fakeUsers) GetUserByGoogleSub(_ context.Context, sub string) (*models.User, error) {
	for i := range f.users {
		if f.users[i].GoogleSub == sub {
			return &f.users[i], nil
		}
	}
	return nil, nil
}

func (f *fakeUsers) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	for i := range f.users {
		if f.users[i].Email == email {
			return &f.users[i], nil
		}
	}
	return nil, nil
}

func (f *fakeUsers) UpdateUserGoogleSub(context.Context, string, string) error { return nil }

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

func TestAuthCallbackInvite_이미있는계정(t *testing.T) {
	invite := &models.Invite{Code: "ABCD2345", CoupleID: "c1"}
	cases := []struct {
		name string
		user models.User
		want int
	}{
		{"구글 계정이 다른 그룹", models.User{ID: "u9", CoupleID: "other", GoogleSub: "g1"}, http.StatusConflict},
		{"메일이 다른 그룹", models.User{ID: "u9", CoupleID: "other", Email: "mom@example.com"}, http.StatusConflict},
		{"같은 그룹이면 로그인", models.User{ID: "u9", CoupleID: "c1", GoogleSub: "g1"}, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newPermHandler(&fakePerms{}, []models.User{c.user})
			inv := &fakeInvites{invite: invite}
			h.inviteRepo = inv
			h.internalSecret = []byte("s3cret")
			rec := do(h, "", http.MethodPost, "/api/auth/callback",
				`{"google_sub":"g1","google_email":"mom@example.com","invite_code":"ABCD2345"}`)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, c.want, rec.Body)
			}
			if c.want == http.StatusConflict && !strings.Contains(rec.Body.String(), "이미 다른 그룹에 가입된 계정입니다") {
				t.Errorf("안내 문구: %s", rec.Body)
			}
			if inv.redeemed != nil {
				t.Error("이미 있는 계정으로 초대를 썼다")
			}
		})
	}
}

func TestAuthCallbackInvite_이름은30자로자른다(t *testing.T) {
	h := newPermHandler(&fakePerms{}, nil)
	inv := &fakeInvites{}
	h.inviteRepo = inv
	h.internalSecret = []byte("s3cret")
	rec := do(h, "", http.MethodPost, "/api/auth/callback",
		`{"google_sub":"g1","invite_code":"ABCD2345","nickname":"  `+strings.Repeat("가", 40)+` "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d (%s)", rec.Code, rec.Body)
	}
	if inv.redeemed.Name != strings.Repeat("가", 30) {
		t.Errorf("이름은 앞뒤 공백을 빼고 30자: %q", inv.redeemed.Name)
	}
}
