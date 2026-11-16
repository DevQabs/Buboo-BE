package models

import "slices"

// Menus are the permission keys, in screen order (설계서 3절).
var Menus = []string{"baby_photo", "baby_growth", "ledger", "schedule", "diary", "assets", "roadmap"}

const (
	LevelHidden = "hidden"
	LevelView   = "view"
	LevelEdit   = "edit"
)

// Permissions is what one member may see and change. The owner bypasses Menus.
// A menu with no entry is hidden.
type Permissions struct {
	IsOwner bool              `json:"is_owner"`
	Menus   map[string]string `json:"menus"`
}

// Allows reports whether a request may pass. Reads need view or edit on any
// of menus; writes need edit on menus[0], so a list readable from several
// menus (e.g. /stocks from the ledger) is still only writable from its own.
// Called with no menus, only the owner passes.
func (p Permissions) Allows(write bool, menus ...string) bool {
	if p.IsOwner {
		return true
	}
	if write {
		return len(menus) > 0 && p.Menus[menus[0]] == LevelEdit
	}
	for _, m := range menus {
		if l := p.Menus[m]; l == LevelView || l == LevelEdit {
			return true
		}
	}
	return false
}

// Effective returns every menu's level (owner: all edit), for API responses.
func (p Permissions) Effective() map[string]string {
	out := make(map[string]string, len(Menus))
	for _, m := range Menus {
		switch {
		case p.IsOwner:
			out[m] = LevelEdit
		case p.Menus[m] != "":
			out[m] = p.Menus[m]
		default:
			out[m] = LevelHidden
		}
	}
	return out
}

func ValidMenu(m string) bool { return slices.Contains(Menus, m) }

func ValidLevel(l string) bool { return l == LevelHidden || l == LevelView || l == LevelEdit }

// PresetLevels returns the permission rows a member gets when joining with an
// invite preset. false for an unknown preset.
func PresetLevels(preset string) (map[string]string, bool) {
	switch preset {
	case "spouse":
		out := make(map[string]string, len(Menus))
		for _, m := range Menus {
			out[m] = LevelEdit
		}
		return out, true
	case "grandparent":
		return map[string]string{"baby_photo": LevelEdit, "baby_growth": LevelEdit}, true
	case "custom":
		return map[string]string{}, true
	}
	return nil, false
}

// RoleForPreset picks the joining member's role. A spouse takes the free one
// of husband/wife; when both are taken, or for any other preset, "family" —
// so a third member never duplicates a spouse role.
func RoleForPreset(preset string, taken map[string]bool) string {
	if preset == "spouse" {
		if !taken["husband"] {
			return "husband"
		}
		if !taken["wife"] {
			return "wife"
		}
	}
	return "family"
}

func DefaultAvatarColor(role string) string {
	switch role {
	case "wife":
		return "#FDA4AF"
	case "family":
		return "#fbbf24"
	}
	return "#0F4C81"
}

// Member is a user with their effective menu levels, for the owner's settings screen.
type Member struct {
	User
	Menus map[string]string `json:"menus"`
}
