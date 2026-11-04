package bot

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// TestMuleUpgradeOnlyOnKnownBetterStock: an account chopping with a mithril axe is due the mule's
// Rune axe when the mule's published bank shows one, and never when nothing is published (a blind
// upgrade ask is the 2026-09-21 walking loop), nor when the mule only has worse axes.
func TestMuleUpgradeOnlyOnKnownBetterStock(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, []peerbank.Holding{
		{Item: woodcutplan.RuneAxe, Count: 45}, {Item: "Bronze axe", Count: 3},
	}, nil, false)
	b := testWoodcutterBot("mule")
	ctx := ctxWithDirectory(dir)

	if !b.muleUpgradeAvailable(ctx, "Mithril axe") {
		t.Fatal("a mithril-axe account is not due the mule's Rune axe")
	}
	if ask, _ := b.muleAxeLadderWhere(ctx, axesBetterThan("Mithril axe")); !reflect.DeepEqual(ask, []string{woodcutplan.RuneAxe}) {
		t.Fatalf("upgrade ladder = %v, want [Rune axe]: never ask for a worse axe than the one in hand", ask)
	}
	if b.muleUpgradeAvailable(ctx, woodcutplan.RuneAxe) {
		t.Fatal("a rune-axe account is due an upgrade")
	}
	if b.muleUpgradeAvailable(ctx, "") {
		t.Fatal("an axeless account went down the upgrade path; orderStep's own axeless ask covers it")
	}

	blind := &Context{Username: "acct134", Log: func(string) {}}
	if b.muleUpgradeAvailable(blind, "Mithril axe") {
		t.Fatal("an upgrade was due with nothing published: that is the blind walk to Catherby")
	}

	// A cooling-down ask is not due either.
	b.axeAsk = &axeAsk{nextAllowed: time.Now().Add(time.Minute)}
	if b.muleUpgradeAvailable(ctx, "Mithril axe") {
		t.Fatal("an upgrade was due inside the ask's cooldown")
	}
}

// TestBrokenAxeReplacementAsksForAtLeastTheRepairedTier: a broken rune axe is replaced only by a
// rune axe; a broken mithril one by mithril or better. A worse axe would leave the account still
// wanting Bob's, and ask the mule twice.
func TestBrokenAxeReplacementAsksForAtLeastTheRepairedTier(t *testing.T) {
	dir := peerbank.New()
	dir.Publish("mule", tile.Tile{}, false, []peerbank.Holding{
		{Item: woodcutplan.RuneAxe, Count: 45}, {Item: "Mithril axe", Count: 2}, {Item: "Bronze axe", Count: 3},
	}, nil, false)
	b := testWoodcutterBot("mule")
	ctx := ctxWithDirectory(dir)
	if ask, _ := b.muleAxeLadderWhere(ctx, axesAtLeast(woodcutplan.RuneAxe)); !reflect.DeepEqual(ask, []string{woodcutplan.RuneAxe}) {
		t.Fatalf("for a broken rune axe, ask = %v, want [Rune axe]", ask)
	}
	if ask, _ := b.muleAxeLadderWhere(ctx, axesAtLeast("Mithril axe")); !reflect.DeepEqual(ask, []string{woodcutplan.RuneAxe, "Mithril axe"}) {
		t.Fatalf("for a broken mithril axe, ask = %v, want [Rune axe Mithril axe]", ask)
	}
	// And the replacement ends the event: holding a rune axe, a broken rune axe is not worth Bob.
	ev := woodcutplan.DecideWoodEvent(woodcutplan.EventSnapshot{
		HasBrokenAxe: true, BrokenAxeID: 506, HasWorkingAxe: true, HeldBest: woodcutplan.RuneAxe,
	})
	if ev != woodcutplan.EventNone {
		t.Fatalf("with the mule's rune axe in hand, DecideWoodEvent = %q, want none", ev)
	}
}
