package webwalk

import "strings"

// The slash-tool question, and why it is answered by item name.
//
// Content's general_use/scripts/web.rs2 gates a slashable web (loc 733, "Web", op "Slash")
// two ways: the menu op calls ~slash_checker, which wants a worn weapon carrying a
// slashattack_anim param, and the use-on branch accepts a plain `knife` or any item whose
// slashattack_anim is not human_unarmedpunch. Neither param reaches this client: the config
// archive it reads carries names, ops and sizes, not obj params. So the answer here is the
// same display-name approximation the TS client settled on in
// src/bot/event/webwalk/slashTool.ts, ported so the two agree.
//
// It matters because the Varrock sewer Moss giants have exactly one route: planning from the
// manhole ladder with the web at 3210,9898 excluded leaves the whole western sewer
// unreachable (cmd/webwalkplan -avoidrect, 2026-09-18). An account with no blade cannot get
// there, and a planner that does not know it will route one into a hop it can never run.

// slashWeaponWords are the display-name families that carry a slashattack_anim in content.
var slashWeaponWords = []string{
	"scimitar", "longsword", "sword", "dagger", "battleaxe", "2h", "two-handed",
	"claws", "whip", "scythe", "halberd", "machete", "abyssal", "silverlight",
	"excalibur", "darklight",
}

// thrownKnifeMetals name the throwing knives, which are separate objs from content's `knife`
// and do not slash a web.
var thrownKnifeMetals = []string{"bronze", "iron", "steel", "black", "mithril", "adamant", "rune", "dragon"}

// KnifeName is the exact display name of content's `knife` obj (946). A "Bronze knife" is a
// different obj and does not count.
const KnifeName = "Knife"

// IsContentKnifeName reports whether a display name is content's plain `knife`.
func IsContentKnifeName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), KnifeName)
}

// IsSlashWeaponName reports whether a display name looks like a blade that can cut a web.
// The plain Knife is deliberately not matched here; IsContentKnifeName handles it.
func IsSlashWeaponName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "knife" {
		return false
	}
	for _, metal := range thrownKnifeMetals {
		if n == metal+" knife" {
			return false
		}
	}
	for _, word := range slashWeaponWords {
		if containsWord(n, word) {
			return true
		}
	}
	return false
}

// containsWord reports whether lower holds word on its own, bounded by anything that is not a
// letter or digit. Plain strings.Contains would match "swordfish" for "sword".
func containsWord(lower, word string) bool {
	for i := 0; i+len(word) <= len(lower); i++ {
		if lower[i:i+len(word)] != word {
			continue
		}
		if i > 0 && isNameChar(lower[i-1]) {
			continue
		}
		if end := i + len(word); end < len(lower) && isNameChar(lower[end]) {
			continue
		}
		return true
	}
	return false
}

func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// HasSlashTool reports whether any of these carried or worn item names can cut a web.
func HasSlashTool(names []string) bool {
	for _, n := range names {
		if IsContentKnifeName(n) || IsSlashWeaponName(n) {
			return true
		}
	}
	return false
}
