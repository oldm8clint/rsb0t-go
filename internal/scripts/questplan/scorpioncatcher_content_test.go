package questplan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Scorpion Catcher decision against the content checkout (skipped without one: RSB0T_CONTENT_DIR, or the
// sibling "content" directory of the repo's parent).
//
// Live 15-16 Oct 2026 every account that parked on "talk to Thormac" (15 of 15 with a bank on file, 17 accounts in
// all, 500+ attempts and not one past that step) held one empty scorpion cage in the BANK. Thormac's script
// answers such an account with "go and speak to the Seers" and offers the lost-cage line only to one that owns no
// cage anywhere, so the bot asked, was told the same thing and parked after eight ticks, every run. These tests
// pin the two facts the decision rests on to the content itself.
func scorpContentFile(t *testing.T, rel ...string) string {
	t.Helper()
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "..", "content")
	}
	b, err := os.ReadFile(filepath.Join(append([]string{dir}, rel...)...))
	if err != nil {
		t.Skipf("no content checkout (%v)", err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// thormac_how_goes offers the lost-cage line only when neither the pack nor the bank holds a cage of the
// scorpcage category, and every other case falls through to "not yet", which gives nothing.
func TestThormacOffersALostCageOnlyToAnAccountWithNoneAnywhere(t *testing.T) {
	script := scorpContentFile(t, "scripts", "areas", "area_seers", "scripts", "thormac.rs2")
	_, how, ok := strings.Cut(script, "[label,thormac_how_goes]")
	if !ok {
		t.Fatal("thormac.rs2 has no thormac_how_goes label")
	}
	how, _, _ = strings.Cut(how, "[label,thormac_lost_cage]")
	guard := "if(inv_totalcat(inv, scorpcage) = 0 & inv_totalcat(bank, scorpcage) = 0) {"
	if !strings.Contains(how, guard) {
		t.Fatalf("the lost-cage menu is no longer guarded by %q:\n%s", guard, how)
	}
	_, after, _ := strings.Cut(how, guard)
	menu, rest, _ := strings.Cut(after, "} else {")
	if !strings.Contains(menu, "I've lost my cage.") || !strings.Contains(rest, "@thormac_not_yet") {
		t.Errorf("the guarded branch is not the lost-cage menu, or the else is not the plain answer:\n%s", how)
	}
	// The menu texts the stop prefers are the script's own.
	for _, want := range ThormacStop.Prefer {
		if !strings.Contains(script, `"`+strings.TrimSuffix(want, ".")) {
			t.Errorf("Thormac's script has no line %q", want)
		}
	}
}

// All eight cage states are objects of the scorpcage category the bank check counts, under the ids the module uses.
func TestScorpionCageIdsAreTheContentsCageCategory(t *testing.T) {
	pack := scorpContentFile(t, "pack", "obj.pack")
	names := map[string]int{}
	for _, line := range strings.Split(pack, "\n") {
		if id, name, ok := strings.Cut(line, "="); ok && strings.HasPrefix(name, "scorpioncage") {
			var n int
			if _, err := fmt.Sscanf(id, "%d", &n); err == nil {
				names[name] = n
			}
		}
	}
	want := map[string]int{
		"scorpioncageempty": CageEmpty, "scorpioncagea": CageA, "scorpioncageab": CageAB, "scorpioncageac": CageAC,
		"scorpioncageb": CageB, "scorpioncagebc": CageBC, "scorpioncagec": CageC, "scorpioncagefull": CageFull,
	}
	for name, id := range want {
		if names[name] != id {
			t.Errorf("%s is object %d in the pack, the module says %d", name, names[name], id)
		}
	}
	if len(names) != len(scorpAllCages) {
		t.Errorf("the pack has %d scorpioncage objects, the module lists %d", len(names), len(scorpAllCages))
	}
	cfg := scorpContentFile(t, "scripts", "quests", "quest_scorpcatcher", "configs", "quest_scorpcatcher.obj")
	if n := strings.Count(cfg, "category=scorpcage"); n != len(scorpAllCages) {
		t.Errorf("%d cages are in the scorpcage category, the module lists %d", n, len(scorpAllCages))
	}
}
