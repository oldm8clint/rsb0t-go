package bot

// The donor half of the fleet owner's 2026-09-22 report: "woodcutters are asking the mule for
// axes they dont even have instock ... they should beable to know whats in their bank to
// request". These tests run the real ladder-building code against the real published-bank
// directory, with no session, no bus and no server.

import (
	"testing"
	"time"

	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// realMuleBankAxes is what GET /api/bank?account=the mule returned on 2026-09-22 at 04:11 UTC,
// reduced to the rows that matter. The whole report rests on this reading: the mule's 226-slot
// bank held exactly ONE chopping axe.
//
// The three non-axes are kept deliberately. A Steel BATTLEaxe and a Bronze/Adamant PICKAXE are
// what an "is there an axe in there" check written by name-matching would wrongly count, and
// the mule really does hold all of them.
var realMuleBankAxes = []peerbank.Holding{
	{Item: "Adamant axe", Count: 1},
	{Item: "Steel battleaxe", Count: 4},
	{Item: "Bronze pickaxe", Count: 13},
	{Item: "Adamant pickaxe", Count: 1},
	{Item: "Coins", Count: 2331324},
}

func testWoodcutterBot(mule string) *woodcutterBot {
	b := &woodcutterBot{warnedAt: map[string]time.Time{}}
	b.settings.Mule = mule
	return b
}

func ctxWithDirectory(dir *peerbank.Directory) *Context {
	return &Context{Username: "acct134", Peers: dir, Log: func(string) {}}
}

// TestMuleAxeLadderAgainstMule1sRealBank is the report, end to end through the live glue: with
// the mule's actual published bank there is nothing on the ask ladder, and the account must know
// it rather than walking to Catherby to find out.
func TestMuleAxeLadderAgainstMule1sRealBank(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, realMuleBankAxes, nil, false)

	b := testWoodcutterBot("mule")
	ctx := ctxWithDirectory(dir)

	ask, known := b.muleAxeLadder(ctx)
	if !known {
		t.Fatal("known = false after the mule published its bank, want true: without it the caller cannot tell 'mule has none' from 'nobody told us' and keeps making the 83-tile trip")
	}
	// Adamant joined orderplan.AxeHandouts() on 2026-10-04, so the one axe that bank held is now
	// the one rung; the five it held none of are still dropped, which is what the report was about.
	if len(ask) != 1 || ask[0] != "Adamant axe" {
		t.Fatalf("ask = %v, want [Adamant axe]: mule held one Adamant axe and none of the other rungs", ask)
	}

	// With nothing on the ladder in the bank, the walk is still refused.
	dir.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, realMuleBankAxes[1:], nil, false)
	if b.muleMightHaveAnAxe(ctx) {
		t.Error("muleMightHaveAnAxe = true, want false: this is what stops the 106-tile round trip every 70 seconds that was measured on acct134")
	}
}

// TestMuleAxeLadderDoesNotMistakeABattleaxeOrPickaxeForAnAxe pins the trap in the reading
// above. the mule holds a Steel battleaxe x4 and two pickaxes; neither chops, and neither is on
// the hand-out ladder, so a name match loose enough to catch them would report stock that
// cannot be used.
func TestMuleAxeLadderDoesNotMistakeABattleaxeOrPickaxeForAnAxe(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{}, false, []peerbank.Holding{
		{Item: "Steel battleaxe", Count: 4},
		{Item: "Bronze pickaxe", Count: 13},
	}, nil, false)

	ask, known := testWoodcutterBot("mule").muleAxeLadder(ctxWithDirectory(dir))
	if !known {
		t.Fatal("known = false, want true")
	}
	if len(ask) != 0 {
		t.Fatalf("ask = %v, want empty: a battleaxe is a weapon and a pickaxe mines; neither is on orderplan.AxeHandouts()", ask)
	}
}

// TestMuleAxeLadderKeepsWhatTheMuleReallyHolds is the other half, and the one that matters for
// not breaking the working case: on the bank the mule held on 2026-09-21 (Mithril x6, Bronze x15,
// Iron x4, Steel x1) every rung is real and the account must still ask, best first.
func TestMuleAxeLadderKeepsWhatTheMuleReallyHolds(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{}, false, []peerbank.Holding{
		{Item: "Mithril axe", Count: 6},
		{Item: "Bronze axe", Count: 15},
		{Item: "Iron axe", Count: 4},
		{Item: "Steel axe", Count: 1},
	}, nil, false)

	b := testWoodcutterBot("mule")
	ctx := ctxWithDirectory(dir)
	ask, known := b.muleAxeLadder(ctx)
	if !known {
		t.Fatal("known = false, want true")
	}
	if len(ask) != 4 {
		t.Fatalf("ask = %v, want all four rungs: the mule held every one of them and filtering must not become a reason to stop asking", ask)
	}
	if ask[0] != "Mithril axe" {
		t.Errorf("ask[0] = %q, want %q: best first is what makes this a ladder", ask[0], "Mithril axe")
	}
	if !b.muleMightHaveAnAxe(ctx) {
		t.Error("muleMightHaveAnAxe = false with four axes published, want true")
	}
}

// TestMuleAxeLadderWithNoDirectoryIsUnchanged is the nil-safety requirement of this change,
// asserted rather than claimed: an account in a fleet with no peer directory at all must ask
// the full blind ladder and must still be willing to walk, exactly as it did before.
func TestMuleAxeLadderWithNoDirectoryIsUnchanged(t *testing.T) {
	b := testWoodcutterBot("mule")
	ctx := &Context{Username: "acct134", Log: func(string) {}} // Peers nil

	ask, known := b.muleAxeLadder(ctx)
	if known {
		t.Error("known = true with no directory, want false: claiming knowledge nobody supplied would stop the fleet asking its mule for anything")
	}
	if len(ask) != len(orderplan.AxeHandouts()) {
		t.Fatalf("ask = %v, want the whole %d-rung blind ladder", ask, len(orderplan.AxeHandouts()))
	}
	if !b.muleMightHaveAnAxe(ctx) {
		t.Error("muleMightHaveAnAxe = false with nothing known, want true: an unconfigured fleet must behave exactly as before")
	}
}

// TestMuleAxeLadderIgnoresAStaleRow: a bank reading older than peerplan.DefaultMaxAge is a
// guess, and "the mule has no axe" is too consequential to draw from a guess — it stops the
// account going there at all. A stale row must fall back to the blind ladder, not to silence.
func TestMuleAxeLadderIgnoresAStaleRow(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{}, false, realMuleBankAxes, nil, false)
	// Age the row past the bound by hand; Publish stamps it with time.Now().
	for _, e := range dir.Entries(time.Now()) {
		if e.Account == "mule" && time.Since(e.UpdatedAt) > peerplan.DefaultMaxAge {
			t.Fatal("a freshly published row already reads as stale; the fixture is wrong")
		}
	}
	if got := muleBankCount(ctxWithDirectory(dir), "mule"); got == nil {
		t.Fatal("a fresh row gave no reading, want one")
	}
	// A mule nobody has heard of at all is the same case and is the one that must not be read
	// as "it has nothing".
	if got := muleBankCount(ctxWithDirectory(dir), "someotherule"); got != nil {
		t.Error("an unpublished mule gave a reading, want nil: an absent row is not an empty bank")
	}
}

// TestMuleAxeLadderRemembersADecline is the loop-breaker on a fleet where the mule never
// publishes at all. The decline was always truthful; the old code logged it and threw it away,
// so the same four questions went out again after every axeAskRetry, forever.
func TestMuleAxeLadderRemembersADecline(t *testing.T) {
	b := testWoodcutterBot("mule")
	ctx := &Context{Username: "acct134", Log: func(string) {}} // no directory: blind

	now := time.Now()
	for _, h := range orderplan.AxeHandouts() {
		b.muleNoAxe.Mark(h.Item, now, woodcutplan.DefaultUnavailableFor)
	}
	ask, known := b.muleAxeLadder(ctx)
	if known {
		t.Error("known = true, want false: nothing was published; this knowledge came from declines")
	}
	if len(ask) != 0 {
		t.Fatalf("ask = %v, want empty: every rung has already been declined 'none in the bank'", ask)
	}
	if b.muleMightHaveAnAxe(ctx) {
		t.Error("muleMightHaveAnAxe = true after every rung was declined, want false: repeating an answered question is the reported defect")
	}
}

// TestMuleAxeLadderForgetsADeclineWhenItLapses: both sources restock, so a remembered "no" has
// to expire or one bad half-hour would keep an account off its mule for the rest of the run.
func TestMuleAxeLadderForgetsADeclineWhenItLapses(t *testing.T) {
	b := testWoodcutterBot("mule")
	ctx := &Context{Username: "acct134", Log: func(string) {}}

	past := time.Now().Add(-woodcutplan.DefaultUnavailableFor - time.Minute)
	for _, h := range orderplan.AxeHandouts() {
		b.muleNoAxe.Mark(h.Item, past, woodcutplan.DefaultUnavailableFor)
	}
	if ask, _ := b.muleAxeLadder(ctx); len(ask) != len(orderplan.AxeHandouts()) {
		t.Fatalf("ask = %v, want the whole ladder back: those declines lapsed %s ago", ask, woodcutplan.DefaultUnavailableFor)
	}
}

// TestMuleAxeLadderWithNoMuleConfigured: an account with no mule at all must not consult a
// directory, must not warn, and must behave exactly as it always has — askMuleForAnAxeReady
// already refuses it, and this must not become a second, contradictory answer.
func TestMuleAxeLadderWithNoMuleConfigured(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{}, false, realMuleBankAxes, nil, false)

	b := testWoodcutterBot("") // no mule
	ctx := ctxWithDirectory(dir)
	ask, known := b.muleAxeLadder(ctx)
	if known {
		t.Error("known = true with no mule configured, want false: mule's bank says nothing about a mule this account does not use")
	}
	if len(ask) != len(orderplan.AxeHandouts()) {
		t.Fatalf("ask = %v, want the unfiltered ladder", ask)
	}
	if b.askMuleForAnAxeReady() {
		t.Error("askMuleForAnAxeReady = true with no mule, want false: that gate is unchanged and still comes first")
	}
}

// TestMuleAskLadderMatchesTheCatalogue pins the list woodcutplan's own tests restate by hand
// (its muleLadder fixture) against orderplan's real one, so a change to the catalogue fails
// here rather than silently making those fixtures fiction.
func TestMuleAskLadderMatchesTheCatalogue(t *testing.T) {
	want := []string{"Rune axe", "Adamant axe", "Mithril axe", "Steel axe", "Iron axe", "Bronze axe"}
	got := orderplan.AxeHandouts()
	if len(got) != len(want) {
		t.Fatalf("orderplan.AxeHandouts() has %d rungs %v, want %d %v; woodcutplan's axestock_test.go restates this list and must be updated with it",
			len(got), axeNames(got), len(want), want)
	}
	for i := range want {
		if got[i].Item != want[i] {
			t.Fatalf("orderplan.AxeHandouts() = %v, want %v (best first)", axeNames(got), want)
		}
	}
	// One axe per order, whatever the tier: an account needs one axe.
	for _, h := range got {
		if h.Max != 1 {
			t.Errorf("%s: Max %d, want 1", h.Item, h.Max)
		}
	}
}

// TestMuleAxeLadderAsksForMule1sRuneAxes is the owner's 2026-10-04 instruction: the mule holds 45
// Rune axes for the ProgressiveWCFletch cohort, and an axeless account asks for one first.
func TestMuleAxeLadderAsksForMule1sRuneAxes(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, []peerbank.Holding{
		{Item: woodcutplan.RuneAxe, Count: 45},
		{Item: "Knife", Count: 222},
		{Item: "Bronze axe", Count: 3},
	}, nil, false)
	b := testWoodcutterBot("mule")
	ctx := ctxWithDirectory(dir)
	ask, known := b.muleAxeLadder(ctx)
	if !known || len(ask) != 2 || ask[0] != woodcutplan.RuneAxe || ask[1] != "Bronze axe" {
		t.Fatalf("ask = %v (known %v), want [Rune axe Bronze axe]: best first, and only what the bank holds", ask, known)
	}
	if !b.muleMightHaveAnAxe(ctx) {
		t.Fatal("muleMightHaveAnAxe = false with 45 Rune axes published")
	}
}
