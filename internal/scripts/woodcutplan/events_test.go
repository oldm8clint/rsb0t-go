package woodcutplan

import (
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
)

// TestEntIDsCoverEveryEntAndNothingElse pins the range against content/pack/npc.pack: 444
// macro_ent_tree1 through 452 macro_ent_magic. 443 is the last Dryad and 453 is not an Ent, so
// both edges are checked rather than the middle only.
func TestEntIDsCoverEveryEntAndNothingElse(t *testing.T) {
	for id := EntNpcFirst; id <= EntNpcLast; id++ {
		if !IsEntNpc(id) {
			t.Errorf("IsEntNpc(%d) = false, but it is in the Ent range", id)
		}
	}
	for _, id := range []int{0, 1, 437, DryadNpcLast, EntNpcFirst - 1, EntNpcLast + 1, 1000} {
		if IsEntNpc(id) {
			t.Errorf("IsEntNpc(%d) = true, but it is not an Ent", id)
		}
	}
	// The Ent range must not overlap the Dryad range, or an Ent exclusion would start
	// blocklisting tiles because a tree spirit walked past.
	if EntNpcFirst <= DryadNpcLast {
		t.Errorf("the Ent range (%d-%d) overlaps the Dryad range (%d-%d)", EntNpcFirst, EntNpcLast, DryadNpcFirst, DryadNpcLast)
	}
}

// TestDryadRangeMatchesWhatRandomeventsAlreadyHandles is a documentation test with teeth: the
// dryad ids recorded here must stay equal to the `idRange(438, 443) // Tree spirit` that
// internal/randomevents already carries, because this package deliberately does NOT handle that
// event and relies on the shared Guardian doing it. If the two ever drift, the tree spirit
// silently stops being anyone's job.
func TestDryadRangeMatchesWhatRandomeventsAlreadyHandles(t *testing.T) {
	if DryadNpcFirst != 438 || DryadNpcLast != 443 {
		t.Errorf("the Dryad range is %d-%d, but randomevents handles 438-443", DryadNpcFirst, DryadNpcLast)
	}
}

// TestEventItemNamesAreOnePerFamily pins that content gives every tier's broken axe and every
// tier's head ONE shared display name, which is what makes name matching safe for these while
// it is unsafe for the Ents (whose names impersonate the trees).
func TestEventItemNames(t *testing.T) {
	if !IsAxeHead("Axe head") || !IsAxeHead("  axe head ") {
		t.Error("IsAxeHead should match, trimmed and case-insensitively")
	}
	if !IsBrokenAxe("Broken axe") || !IsAxeHandle("Axe handle") {
		t.Error("IsBrokenAxe/IsAxeHandle should match their names")
	}
	// The three must not be confused with each other, nor with a working axe.
	for _, n := range []string{"Rune axe", "Bronze axe", "Logs", ""} {
		if IsAxeHead(n) || IsBrokenAxe(n) || IsAxeHandle(n) {
			t.Errorf("%q matched an event item", n)
		}
	}
	if IsAxe(BrokenAxe) || IsAxe(AxeHandle) || IsAxe(AxeHead) {
		t.Error("a broken axe, a handle and a head must never count as a working axe")
	}
}

// TestAxeHeadSearchRadiusCoversTheRealThrow pins the radius against the content's own throw:
// `map_findsquare(coord, 1, 7, ^map_findsquare_lineofwalk)` puts the head at most 7 tiles away.
// Searching less than that would sometimes never find it, and the head is on a timer.
func TestAxeHeadSearchRadiusCoversTheRealThrow(t *testing.T) {
	const contentMax = 7
	if AxeHeadSearchRadius < contentMax {
		t.Errorf("AxeHeadSearchRadius = %d, but content throws the head up to %d tiles", AxeHeadSearchRadius, contentMax)
	}
	if AxeHeadLifetimeTicks != 200 {
		t.Errorf("AxeHeadLifetimeTicks = %d, want 200 (^lootdrop_duration)", AxeHeadLifetimeTicks)
	}
}

func TestDecideWoodEvent(t *testing.T) {
	cases := []struct {
		name string
		s    EventSnapshot
		want WoodEvent
	}{
		{"nothing wrong", EventSnapshot{HasWorkingAxe: true}, EventNone},
		{"head already picked up, handle held", EventSnapshot{HasHandle: true, HasHead: true}, EventAxeHead},
		{"head still on the ground", EventSnapshot{HasHandle: true, HeadOnGround: true}, EventAxeHead},
		// A handle with no head anywhere is not recoverable here: the head has despawned or was
		// never seen, so this must NOT sit in a recovery loop forever. It falls through to the
		// ordinary machinery, which will find there is no axe and go buy one.
		{"handle but no head at all", EventSnapshot{HasHandle: true}, EventNone},
		// The head wins over a working axe: the head is on a two-minute timer and the axe is
		// not going anywhere, so a few seconds of lost chopping beats losing an axe outright.
		{"head off but another axe held", EventSnapshot{HasHandle: true, HeadOnGround: true, HasWorkingAxe: true}, EventAxeHead},
		{"broken axe and nothing else", EventSnapshot{HasBrokenAxe: true}, EventBrokenAxe},
		// A broken axe alongside a working one is NOT worth walking from Seers to Lumbridge
		// for; carry on chopping.
		{"broken axe but a working one too", EventSnapshot{HasBrokenAxe: true, HasWorkingAxe: true}, EventNone},
		// Both at once: the timer wins.
		{"both", EventSnapshot{HasHandle: true, HeadOnGround: true, HasBrokenAxe: true}, EventAxeHead},
	}
	for _, c := range cases {
		if got := DecideWoodEvent(c.s); got != c.want {
			t.Errorf("%s: DecideWoodEvent = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDescribeWoodEvent(t *testing.T) {
	for _, e := range []WoodEvent{EventAxeHead, EventBrokenAxe, EventNone} {
		if DescribeWoodEvent(e) == "" {
			t.Errorf("DescribeWoodEvent(%q) is empty", e)
		}
	}
}

// The tier comparison added on 2026-09-20, when the Seers -> Lumbridge trip was first measured:
// it is two paid ship fares each way for a quest-less account (cmd/webwalkplan), while five of
// the seven repairs cost less than one fare. So "is it worth going" is a comparison against what
// the account can already reach, not "have I got anything at all".
func TestDecideWoodEventWeighsTheTripAgainstTheTier(t *testing.T) {
	cases := []struct {
		name string
		s    EventSnapshot
		want WoodEvent
	}{
		{
			"a broken rune axe beats the bronze one in the pack, so go",
			EventSnapshot{HasBrokenAxe: true, BrokenAxeID: 506, HasWorkingAxe: true, HeldBest: "Bronze axe"},
			EventBrokenAxe,
		},
		{
			"a broken BRONZE axe beside a bronze axe is not worth the fares",
			EventSnapshot{HasBrokenAxe: true, BrokenAxeID: 494, HasWorkingAxe: true, HeldBest: "Bronze axe"},
			EventNone,
		},
		{
			"a broken bronze axe with nothing to chop with is worth it",
			EventSnapshot{HasBrokenAxe: true, BrokenAxeID: 494},
			EventBrokenAxe,
		},
		{
			"an axe in this account's OWN bank beats a trip across the world",
			EventSnapshot{HasBrokenAxe: true, BrokenAxeID: 502, BankedAxes: []string{"Mithril axe"}},
			EventNone,
		},
		{
			"but a banked axe that is worse than the repair does not stop the trip",
			EventSnapshot{HasBrokenAxe: true, BrokenAxeID: 506, BankedAxes: []string{"Bronze axe"}},
			EventBrokenAxe,
		},
		{
			"an unknown broken-axe id falls back to the old conservative rule",
			EventSnapshot{HasBrokenAxe: true, BrokenAxeID: 99999, HasWorkingAxe: true, HeldBest: "Bronze axe"},
			EventNone,
		},
		{
			"the axe head still wins over any repair, because it is on a timer",
			EventSnapshot{HasHandle: true, HeadOnGround: true, HasBrokenAxe: true, BrokenAxeID: 506},
			EventAxeHead,
		},
	}
	for _, c := range cases {
		if got := DecideWoodEvent(c.s); got != c.want {
			t.Errorf("%s: DecideWoodEvent = %q, want %q", c.name, got, c.want)
		}
	}
}

// Every broken hatchet's id, repaired form and price is read from the content, so this pins the
// table against a silent edit: seven axes, no pickaxes, and the three cheapest repair free.
func TestBrokenAxeTableMatchesContent(t *testing.T) {
	want := map[int]string{
		494: "Bronze axe", 496: "Iron axe", 498: "Steel axe", 500: "Black axe",
		502: "Mithril axe", 504: "Adamant axe", 506: "Rune axe",
	}
	if len(BrokenAxeIDs) != len(want) {
		t.Fatalf("BrokenAxeIDs has %d entries, want %d", len(BrokenAxeIDs), len(want))
	}
	for id, axe := range want {
		if got := RepairedAxe(id); got != axe {
			t.Errorf("RepairedAxe(%d) = %q, want %q", id, got, axe)
		}
		if !IsAxe(axe) {
			t.Errorf("%q is not on the axe ladder", axe)
		}
	}
	// The broken PICKAXES (468-478) carry to_be_fixed_by_nurmof and Bob refuses them outright.
	for id := 468; id <= 479; id++ {
		if RepairedAxe(id) != "" {
			t.Errorf("obj %d is a broken PICKAXE and must not be in the axe table", id)
		}
	}
	for _, free := range []string{"Bronze axe", "Iron axe", "Steel axe"} {
		if BrokenAxeRepairCost[free] != 0 {
			t.Errorf("%s repairs free in content, table says %d", free, BrokenAxeRepairCost[free])
		}
	}
	if BrokenAxeRepairCost["Rune axe"] != MaxBrokenAxeRepairCost {
		t.Errorf("the rune repair (%d) should be the dearest (%d)", BrokenAxeRepairCost["Rune axe"], MaxBrokenAxeRepairCost)
	}
	for _, axe := range BrokenAxeIDs {
		if _, ok := BrokenAxeRepairCost[axe]; !ok {
			t.Errorf("no repair cost recorded for %q", axe)
		}
	}
}

// The trip budget has to include the way home, or an account arrives at Bob's with a working axe
// and no fare, and the phase machine answers by pickpocketing Men in Lumbridge for an hour.
func TestRepairTripCoinsIncludesTheFareHome(t *testing.T) {
	const fare = 60
	if got, want := RepairTripCoins("Rune axe", fare), 427+2*fare; got != want {
		t.Errorf("RepairTripCoins(rune) = %d, want %d", got, want)
	}
	if got, want := RepairTripCoins("Bronze axe", fare), 2*fare; got != want {
		t.Errorf("a free repair still needs both fares: got %d, want %d", got, want)
	}
	if got := RepairTripCoins("Rune axe", -5); got != 427 {
		t.Errorf("a negative fare must not reduce the budget: got %d", got)
	}
}

func TestBetterAxe(t *testing.T) {
	if !BetterAxe("Rune axe", "Bronze axe") {
		t.Error("rune should beat bronze")
	}
	if BetterAxe("Bronze axe", "Rune axe") {
		t.Error("bronze should not beat rune")
	}
	if BetterAxe("Bronze axe", "Bronze axe") {
		t.Error("an equal axe is not better — that is what stops a pointless trip")
	}
	if !BetterAxe("Bronze axe", "") {
		t.Error("anything beats nothing")
	}
	if BetterAxe("", "") {
		t.Error("nothing does not beat nothing")
	}
}

// BobStand pointed at the Lumbridge GENERAL store until 2026-09-20, which is why no account had
// ever reached the shop: the constant was wrong and the routing was broken, and only one of them
// was visible.
func TestBobStandIsBobsAxes(t *testing.T) {
	d := DefaultSettings()
	if d.BobStand != BobsAxes {
		t.Errorf("DefaultSettings().BobStand = %v, want BobsAxes %v", d.BobStand, BobsAxes)
	}
	if BobsAxes.X != 3232 || BobsAxes.Z != 3203 || BobsAxes.Level != 0 {
		t.Errorf("BobsAxes = %v, want 3232,3203,0 (m50_50.jm2's own `0 32 3: 519` spawn row)", BobsAxes)
	}
	if !freshstart.InLumbridge(BobsAxes) {
		t.Errorf("BobsAxes %v should be inside Lumbridge", BobsAxes)
	}
}
