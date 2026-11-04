package hostileavoid

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The catalog is the whole point of this package, so it is asserted rather than trusted. Every
// number here was read out of the content and engine checkouts — see the package doc comment for
// the file each one came from. A content bump that changes one of these should fail here loudly
// instead of quietly changing how far a bot runs away.
func TestCatalogHoldsTheContentConfirmedNumbers(t *testing.T) {
	want := map[int]Facts{
		78: {Type: 78, Name: "Giant bat", Level: 27, HuntRange: 2, MaxRange: 10, MaxHit: 3},
		96: {Type: 96, Name: "White wolf", Level: 25, HuntRange: 2, MaxRange: 5, MaxHit: 3},
		97: {Type: 97, Name: "White wolf", Level: 38, HuntRange: 2, MaxRange: 5, MaxHit: 4},
		// aggressive_melee, not cowardly: no combat level is too strong for it to hunt (all.hunt).
		917: {Type: 917, Name: "Jail guard", Level: 26, HuntRange: 5, MaxRange: 7, MaxHit: 3, NoLevelCap: true},
	}
	if len(Catalog) != len(want) {
		t.Fatalf("catalog has %d entries, want %d", len(Catalog), len(want))
	}
	for id, w := range want {
		got, ok := Known(id)
		if !ok {
			t.Fatalf("npc %d missing from the catalog", id)
		}
		if got != w {
			t.Errorf("npc %d: got %+v, want %+v", id, got, w)
		}
	}
	if _, ok := Known(86); ok {
		t.Error("Giant rat (86) is catalogued; only species with content evidence belong here")
	}
}

// huntmode=cowardly resolves to check_nottoostrong=outside_wilderness, which the engine applies
// as "skip this player when combatLevel > vislevel*2". The boundary is the interesting part: it
// is why this only ever kills low-level accounts.
func TestHuntsPlayerStopsAboveTwiceTheNpcLevel(t *testing.T) {
	bat := Catalog[78]
	if !bat.HuntsPlayer(3) {
		t.Error("a combat level 3 account must be hunted by a giant bat")
	}
	if !bat.HuntsPlayer(54) {
		t.Error("combat level 54 is exactly twice the giant bat's 27 and is still hunted")
	}
	if bat.HuntsPlayer(55) {
		t.Error("combat level 55 is past twice the giant bat's level and must be ignored")
	}
	if !bat.HuntsPlayer(0) {
		t.Error("an unknown combat level must be treated as hunted, not as safe")
	}
}

func TestRangesAddTheirMargins(t *testing.T) {
	bat := Catalog[78]
	if got := bat.NoticeRange(2); got != 4 {
		t.Errorf("giant bat notice range = %d, want 4 (hunt range 2 + margin 2)", got)
	}
	if got := bat.NoticeRange(DefaultConfig().NoticeMargin); got != 8 {
		t.Errorf("giant bat notice range at the default margin = %d, want 8", got)
	}
	if got := bat.BreakOffRange(3); got != 13 {
		t.Errorf("giant bat break-off = %d, want 13 (chase limit 10 + margin 3)", got)
	}
	wolf := Catalog[97]
	if got := wolf.BreakOffRange(3); got != 8 {
		t.Errorf("white wolf break-off = %d, want 8 (chase limit 5 + margin 3)", got)
	}
}

func TestDecideEvadesInsideTheNoticeRingAndIgnoresOutsideIt(t *testing.T) {
	cfg := DefaultConfig()
	// A giant bat hunts at 2, and the default margin of 6 reacts at 8 so there is room to turn
	// away before it engages — see DefaultConfig's own comment for the live runs behind that
	// number. So 8 evades and 9 does not.
	at := func(dist int) Snapshot {
		return Snapshot{X: 2755, Z: 3402, CombatLevel: 3, NPCs: []NPC{{Index: 1, Type: 78, X: 2755 + dist, Z: 3402}}}
	}
	got := Decide(cfg, at(8))
	if !got.Evade {
		t.Fatal("a giant bat 8 tiles away must trigger a break-off")
	}
	if got.Facts.Name != "Giant bat" || got.BreakOff != 13 {
		t.Errorf("got %+v, want the giant bat's facts and a break-off of 13", got)
	}
	if got.Reason != "inside its hunt range" {
		t.Errorf("reason = %q", got.Reason)
	}
	if Decide(cfg, at(9)).Evade {
		t.Error("a giant bat 9 tiles away is outside the notice ring and must be walked past")
	}
}

func TestDecideIgnoresUncataloguedNpcsAndOtherLevels(t *testing.T) {
	cfg := DefaultConfig()
	snap := Snapshot{X: 100, Z: 100, CombatLevel: 3, NPCs: []NPC{{Index: 1, Type: 86, X: 101, Z: 100}}}
	if Decide(cfg, snap).Evade {
		t.Error("a giant rat is not catalogued and must not trigger an evade")
	}
	upstairs := Snapshot{X: 100, Z: 100, CombatLevel: 3, NPCs: []NPC{{Index: 1, Type: 78, X: 101, Z: 100, Level: 1}}}
	if Decide(cfg, upstairs).Evade {
		t.Error("a hostile on another level cannot reach the account and must be ignored")
	}
}

func TestDecideIgnoresAHostileTooWeakToHuntButNotOneAlreadyLockedOn(t *testing.T) {
	cfg := DefaultConfig()
	// Combat level 60 is past twice the level-25 wolf's own level, so it never picks this
	// account up in the first place.
	strong := Snapshot{X: 2846, Z: 3477, CombatLevel: 60, NPCs: []NPC{{Index: 7, Type: 96, X: 2847, Z: 3477}}}
	if Decide(cfg, strong).Evade {
		t.Error("a wolf that cannot hunt this account must not cause an evade")
	}
	strong.NPCs[0].TargetsUs = true
	got := Decide(cfg, strong)
	if !got.Evade {
		t.Fatal("a wolf already locked on must be broken off from whatever the account's level")
	}
	if got.Reason != "it has locked on" {
		t.Errorf("reason = %q, want the locked-on reason", got.Reason)
	}
}

func TestDecidePicksTheNearestThreat(t *testing.T) {
	snap := Snapshot{X: 2850, Z: 3480, CombatLevel: 3, NPCs: []NPC{
		{Index: 1, Type: 97, X: 2853, Z: 3480},
		{Index: 2, Type: 96, X: 2851, Z: 3480},
	}}
	got := Decide(DefaultConfig(), snap)
	if !got.Evade || got.Threat.Index != 2 {
		t.Fatalf("got threat %+v, want the nearer wolf at index 2", got.Threat)
	}
}

func TestDecideOffWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	snap := Snapshot{X: 0, Z: 0, CombatLevel: 3, NPCs: []NPC{{Index: 1, Type: 78, X: 1, Z: 0}}}
	if Decide(cfg, snap).Evade {
		t.Error("a disabled config must never evade")
	}
}

func TestEscapeCandidatesRingTheAccountFarthestFromTheThreatFirst(t *testing.T) {
	// Threat directly east; the best escape is directly west.
	got := EscapeCandidates(100, 100, 0, 104, 100, 8)
	if len(got) != 8 {
		t.Fatalf("got %d candidates, want 8 compass points", len(got))
	}
	for _, p := range got {
		if d := Chebyshev(p.X, p.Z, 100, 100); d != 8 {
			t.Errorf("candidate %+v is %d tiles away, want the requested ring distance 8", p, d)
		}
		if p.Level != 0 {
			t.Errorf("candidate %+v left the account's level", p)
		}
	}
	if got[0].X != 92 {
		t.Errorf("first candidate is %+v, want the one directly away from the threat (x=92)", got[0])
	}
	prev := Chebyshev(got[0].X, got[0].Z, 104, 100)
	for _, p := range got[1:] {
		d := Chebyshev(p.X, p.Z, 104, 100)
		if d > prev {
			t.Fatalf("candidates are not ordered farthest-from-threat first: %+v", got)
		}
		prev = d
	}
}

func TestClearedNeedsRealSeparationAndTreatsAVanishedNpcAsClear(t *testing.T) {
	threat := NPC{Index: 4, Type: 97, X: 2850, Z: 3480}
	near := Snapshot{X: 2856, Z: 3480, NPCs: []NPC{{Index: 4, Type: 97, X: 2850, Z: 3480}}}
	if Cleared(near, threat, 8) {
		t.Error("6 tiles is still inside the wolf's 8-tile break-off and is not clear")
	}
	far := Snapshot{X: 2859, Z: 3480, NPCs: []NPC{{Index: 4, Type: 97, X: 2850, Z: 3480}}}
	if !Cleared(far, threat, 8) {
		t.Error("9 tiles is past the break-off and must count as clear")
	}
	if !Cleared(Snapshot{X: 2856, Z: 3480}, threat, 8) {
		t.Error("a threat no longer in view must count as clear")
	}
}

func TestAnyNearSpotsACataloguedHostileOnly(t *testing.T) {
	snap := Snapshot{NPCs: []NPC{{Index: 1, Type: 96, X: 50, Z: 50}, {Index: 2, Type: 86, X: 10, Z: 10}}}
	if !AnyNear(snap, 52, 50, 0, 3) {
		t.Error("a wolf 2 tiles away must be reported")
	}
	if AnyNear(snap, 60, 50, 0, 3) {
		t.Error("a wolf 10 tiles away is outside the radius")
	}
	if AnyNear(snap, 11, 10, 0, 3) {
		t.Error("an uncatalogued npc must never be reported")
	}
}

func TestOutclassesAtTwiceTheAccountsCombat(t *testing.T) {
	bat := Facts{Name: "Giant bat", Level: 27}
	if !bat.Outclasses(3) || !bat.Outclasses(13) || !bat.Outclasses(0) {
		t.Error("a level-27 bat outclasses combat 3, 13 and an unknown level")
	}
	if bat.Outclasses(14) || bat.Outclasses(40) {
		t.Error("a level-27 bat does not outclass combat 14 or 40")
	}
}

// The Draynor jail guard hunts every account within five tiles whatever its combat level (aggressive_melee has
// check_nottoostrong=off): the cowardly rule that skips a player stronger than twice the npc's level does not apply.
func TestJailGuardHuntsEveryCombatLevel(t *testing.T) {
	f, ok := Known(917)
	if !ok || f.Name != "Jail guard" || f.Level != 26 || f.HuntRange != 5 || !f.NoLevelCap {
		t.Fatalf("Known(917) = %+v %v", f, ok)
	}
	for _, combat := range []int{0, 3, 52, 63, 126} {
		if !f.HuntsPlayer(combat) {
			t.Errorf("a combat-%d account is not hunted by a Jail guard", combat)
		}
	}
	// ...where a White wolf of the same era leaves a strong account alone.
	wolf, _ := Known(96)
	if wolf.HuntsPlayer(63) {
		t.Error("a combat-63 account is hunted by a level-25 White wolf")
	}
	// A combat-63 account on the road three tiles from the guard that killed one is told to break off.
	snap := Snapshot{X: 3107, Z: 3234, CombatLevel: 63, NPCs: []NPC{{Index: 1, Type: 917, X: 3109, Z: 3237}}}
	act := Decide(DefaultConfig(), snap)
	if !act.Evade || act.Facts.Name != "Jail guard" {
		t.Fatalf("Decide = %+v: the account is not told to break off from a Jail guard 3 tiles away", act)
	}
	if act.BreakOff < f.NoticeRange(DefaultConfig().NoticeMargin)+1 {
		t.Errorf("break-off ring %d is inside the notice ring", act.BreakOff)
	}
	// Outclasses is about being too weak to push through, and a level-26 guard never out-levels combat 63.
	if f.Outclasses(63) {
		t.Error("a level-26 guard outclasses combat 63")
	}
}

// blockOf returns the key=value lines of the [name] block of a content config file.
func blockOf(t *testing.T, path, name string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%s not readable: %v", path, err)
	}
	out := map[string]string{}
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			in = line == "["+name+"]"
		case in:
			if k, v, ok := strings.Cut(line, "="); ok {
				if _, seen := out[k]; !seen { // param= lines repeat; the first is enough here
					out[k] = v
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no [%s] block in %s", name, path)
	}
	return out
}

// The Jail guard's catalog entry against the content that defines it, and the hunt mode that makes it a
// NoLevelCap npc. Skipped without RSB0T_CONTENT_DIR.
func TestJailGuardCatalogMatchesContent(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		t.Skip("RSB0T_CONTENT_DIR not set")
	}
	npc := blockOf(t, filepath.Join(dir, "scripts", "areas", "area_draynor", "configs", "draynor.npc"), "jailguard")
	f, ok := Known(917)
	if !ok {
		t.Fatal("npc 917 is not in the catalog")
	}
	atoi := func(k string) int {
		n, err := strconv.Atoi(npc[k])
		if err != nil {
			t.Fatalf("[jailguard] %s = %q", k, npc[k])
		}
		return n
	}
	if npc["name"] != f.Name || atoi("vislevel") != f.Level || atoi("huntrange") != f.HuntRange {
		t.Errorf("catalog %+v against content name %q level %d huntrange %d", f, npc["name"], atoi("vislevel"), atoi("huntrange"))
	}
	// Neither wanderrange nor maxrange is set on the block: the engine defaults are wanderrange 5 and maxrange
	// wanderrange+2 (cache/config/NpcType.ts).
	if _, set := npc["maxrange"]; set {
		t.Errorf("[jailguard] now sets maxrange %q: the catalog's MaxRange %d is derived", npc["maxrange"], f.MaxRange)
	}
	wander := 5
	if _, set := npc["wanderrange"]; set {
		wander = atoi("wanderrange")
	}
	if wander+2 != f.MaxRange {
		t.Errorf("catalog MaxRange %d against wanderrange %d (+2)", f.MaxRange, wander)
	}
	// The hunt mode: aggressive_melee takes check_nottoostrong=off, so there is no combat level too strong for it
	// (the cowardly mode of the wolves and bats skips a player stronger than twice its level).
	if npc["huntmode"] != "aggressive_melee" {
		t.Fatalf("[jailguard] huntmode = %q", npc["huntmode"])
	}
	hunt := filepath.Join(dir, "scripts", "_unpack", "225", "all.hunt")
	if mode := blockOf(t, hunt, "aggressive_melee"); mode["check_nottoostrong"] != "off" {
		t.Errorf("aggressive_melee check_nottoostrong = %q: the Jail guard would skip strong accounts after all", mode["check_nottoostrong"])
	}
	if mode := blockOf(t, hunt, "cowardly"); mode["check_nottoostrong"] == "off" {
		t.Error("cowardly no longer caps by level: the wolves and bats entries would be wrong")
	}
	if !f.NoLevelCap {
		t.Error("the catalog entry is not NoLevelCap")
	}
	// ...and the pack id.
	pack, err := os.ReadFile(filepath.Join(dir, "pack", "npc.pack"))
	if err != nil {
		t.Skipf("npc.pack: %v", err)
	}
	if !strings.Contains(strings.ReplaceAll(string(pack), "\r", ""), "\n917=jailguard\n") {
		t.Error("npc 917 is not jailguard in npc.pack")
	}
}
