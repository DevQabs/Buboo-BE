package models

import "testing"

func TestAllows(t *testing.T) {
	ledgerView := Permissions{Menus: map[string]string{"ledger": LevelView}}
	ledgerEdit := Permissions{Menus: map[string]string{"ledger": LevelEdit}}
	assetsEdit := Permissions{Menus: map[string]string{"assets": LevelEdit}}

	cases := []struct {
		name  string
		p     Permissions
		write bool
		menus []string
		want  bool
	}{
		{"owner 는 행 없이도 쓰기 통과", Permissions{IsOwner: true}, true, []string{"ledger"}, true},
		{"메뉴 없는 검사(owner 전용)는 owner 통과", Permissions{IsOwner: true}, true, nil, true},
		{"메뉴 없는 검사(owner 전용)는 구성원 거부", assetsEdit, false, nil, false},
		{"보기로 읽기 통과", ledgerView, false, []string{"ledger"}, true},
		{"보기로 쓰기 거부", ledgerView, true, []string{"ledger"}, false},
		{"나열한 메뉴 중 하나만 보여도 읽기 통과", ledgerView, false, []string{"assets", "ledger"}, true},
		{"쓰기는 첫 메뉴 편집만 인정", ledgerEdit, true, []string{"assets", "ledger"}, false},
		{"첫 메뉴 편집이면 쓰기 통과", assetsEdit, true, []string{"assets", "ledger"}, true},
		{"행 없는 메뉴는 숨김", ledgerView, false, []string{"diary"}, false},
		{"hidden 행은 숨김", Permissions{Menus: map[string]string{"diary": LevelHidden}}, false, []string{"diary"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.Allows(c.write, c.menus...); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestEffective(t *testing.T) {
	got := Permissions{Menus: map[string]string{"ledger": LevelView}}.Effective()
	if len(got) != len(Menus) {
		t.Fatalf("메뉴 7개를 모두 채워야 한다: got %d", len(got))
	}
	if got["ledger"] != LevelView || got["diary"] != LevelHidden {
		t.Errorf("ledger=view, 나머지는 hidden: got %v", got)
	}
	for m, l := range (Permissions{IsOwner: true}).Effective() {
		if l != LevelEdit {
			t.Errorf("owner 는 전부 edit: %s=%s", m, l)
		}
	}
}

func TestPresetLevels(t *testing.T) {
	spouse, ok := PresetLevels("spouse")
	if !ok || len(spouse) != len(Menus) {
		t.Fatalf("배우자는 7개 전부: ok=%v len=%d", ok, len(spouse))
	}
	for m, l := range spouse {
		if l != LevelEdit {
			t.Errorf("배우자 %s=%s, edit 이어야 한다", m, l)
		}
	}
	gp, ok := PresetLevels("grandparent")
	if !ok || len(gp) != 2 || gp["baby_photo"] != LevelEdit || gp["baby_growth"] != LevelEdit {
		t.Errorf("조부모는 아기 두 메뉴만 edit: got %v", gp)
	}
	custom, ok := PresetLevels("custom")
	if !ok || len(custom) != 0 {
		t.Errorf("직접 설정은 행 없음(전부 hidden): got %v", custom)
	}
	if _, ok := PresetLevels("boss"); ok {
		t.Error("모르는 묶음은 거부해야 한다")
	}
}

func TestRoleForPreset(t *testing.T) {
	cases := []struct {
		name   string
		preset string
		taken  map[string]bool
		want   string
	}{
		{"빈 그룹 배우자는 husband", "spouse", map[string]bool{}, "husband"},
		{"husband 가 있으면 wife", "spouse", map[string]bool{"husband": true}, "wife"},
		{"둘 다 있으면 family (역할 중복 방지)", "spouse", map[string]bool{"husband": true, "wife": true}, "family"},
		{"조부모는 family", "grandparent", map[string]bool{}, "family"},
		{"직접 설정은 family", "custom", map[string]bool{"husband": true}, "family"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RoleForPreset(c.preset, c.taken); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestDefaultAvatarColor(t *testing.T) {
	for role, want := range map[string]string{"husband": "#0F4C81", "wife": "#FDA4AF", "family": "#fbbf24"} {
		if got := DefaultAvatarColor(role); got != want {
			t.Errorf("%s: got %s, want %s", role, got, want)
		}
	}
}
