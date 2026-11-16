package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/yourname/couple-app/internal/models"
)

func membersFixture() (*Handler, *fakePerms) {
	perms := &fakePerms{perms: map[string]*models.Permissions{"owner": ownerPerms, "mom": grandPerms}}
	users := []models.User{
		{ID: "owner", CoupleID: "c1", Name: "남편", IsOwner: true},
		{ID: "mom", CoupleID: "c1", Name: "친정엄마", Role: "family"},
		{ID: "stranger", CoupleID: "c2", Name: "남"},
	}
	return newPermHandler(perms, users), perms
}

func TestListMembers(t *testing.T) {
	h, _ := membersFixture()
	rec := do(h, "owner", http.MethodGet, "/api/members", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner 는 목록을 본다: got %d", rec.Code)
	}
	var got []models.Member
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("같은 그룹 2명만: got %d", len(got))
	}
	if !got[0].IsOwner || got[0].Menus["diary"] != "edit" {
		t.Errorf("owner 는 전체 edit 로 보인다: got %+v", got[0])
	}
	if got[1].Menus["ledger"] != "hidden" {
		t.Errorf("행 없는 메뉴는 hidden 으로 채운다: got %v", got[1].Menus)
	}
}

func TestPutMemberPermissions(t *testing.T) {
	cases := []struct {
		name   string
		target string
		body   string
		want   int
	}{
		{"owner(자기) 권한은 못 바꾼다", "owner", `{"menus":{"ledger":"view"}}`, http.StatusBadRequest},
		{"다른 그룹 사람은 404", "stranger", `{"menus":{"ledger":"view"}}`, http.StatusNotFound},
		{"없는 사람은 404", "nobody", `{"menus":{"ledger":"view"}}`, http.StatusNotFound},
		{"모르는 메뉴는 400", "mom", `{"menus":{"fridge":"view"}}`, http.StatusBadRequest},
		{"모르는 단계는 400", "mom", `{"menus":{"ledger":"admin"}}`, http.StatusBadRequest},
		{"menus 없으면 400", "mom", `{}`, http.StatusBadRequest},
		{"정상 변경", "mom", `{"menus":{"ledger":"view","diary":"hidden"}}`, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, perms := membersFixture()
			rec := do(h, "owner", http.MethodPut, "/api/members/"+c.target+"/permissions", c.body)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
			if c.want == http.StatusOK {
				want := map[string]string{"ledger": "view", "diary": "hidden"}
				if !reflect.DeepEqual(perms.replaced, want) {
					t.Errorf("저장된 값: got %v", perms.replaced)
				}
				if !strings.Contains(rec.Body.String(), `"ledger":"view"`) {
					t.Errorf("응답은 7개 메뉴 전체: got %s", rec.Body.String())
				}
			} else if perms.replaced != nil {
				t.Errorf("거부된 요청은 저장하지 않는다: got %v", perms.replaced)
			}
		})
	}
}

func TestRemoveMember(t *testing.T) {
	h, perms := membersFixture()
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/members"},
		{http.MethodPut, "/api/members/mom/permissions"},
		{http.MethodDelete, "/api/members/mom"},
	} {
		if got := do(h, "mom", c.method, c.path, `{"menus":{}}`).Code; got != http.StatusForbidden {
			t.Errorf("owner 가 아니면 %s %s 는 403: got %d", c.method, c.path, got)
		}
	}
	if got := do(h, "owner", http.MethodDelete, "/api/members/owner", "").Code; got != http.StatusBadRequest {
		t.Errorf("owner 는 내보낼 수 없다: got %d", got)
	}
	if got := do(h, "owner", http.MethodDelete, "/api/members/stranger", "").Code; got != http.StatusNotFound {
		t.Errorf("다른 그룹 사람은 404: got %d", got)
	}
	if perms.removed != "" {
		t.Fatalf("거부된 요청은 지우지 않는다: got %q", perms.removed)
	}
	if got := do(h, "owner", http.MethodDelete, "/api/members/mom", "").Code; got != http.StatusNoContent {
		t.Fatalf("구성원 내보내기: got %d", got)
	}
	if perms.removed != "mom" {
		t.Errorf("지운 사람: got %q", perms.removed)
	}
}
