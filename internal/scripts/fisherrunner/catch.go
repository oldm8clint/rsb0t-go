package fisherrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fisherplan"
)

// Measuring what a fishing spot actually yields, for the Loot panel's Fishing card
// (2026-09-20, owner: "track all fish fished from the various fishing spot npcs ... the raws
// caskets leather etc everything from every fishing spot").
//
// The rule this follows — snapshot the pack, skip what the activity cannot have produced, and
// DISCARD the window outright when something interrupted it — lives in loot.Window now, because
// chopping and mining need exactly the same thing. Read that type's doc comment for why a
// continuous activity needs a boundary and what taint is for. Everything below is the part that
// is specific to fishing: which window to open, what a catch is, and what a fishing spot can
// never have given.
//
// WHAT THE WINDOW IS
//
// One hold of one spot, inside runner.go's inner loop: it opens when the hold starts and closes
// when the hold ends (the pack fills, the catch stalls, a whirlpool or a random event
// interrupts, the target or deadline is reached), with a flush every 30s so a long uninterrupted
// hold reports while it runs rather than only at the end.
//
// Events is CATCHES, not trips, because that is what users asked to track and it is what
// makes a per-hour rate mean something. It is counted from the raw-fish total the hold loop
// already watches, so a Big net haul that lands a casket and no fish reports the casket with no
// catch against it rather than inventing one. A script that drops part of its catch (Deps.Junk)
// reports neither the dropped items nor the dropped raw fish as catches (notACatchNamed,
// keptRawFish).

// catchFlushEvery bounds how long a productive hold can run before it reports, so the panel
// moves while an account is fishing instead of only when it banks.
const catchFlushEvery = 30 * time.Second

// beginCatch opens a measurement window on the spot npc type being held. A Deps with no Loot
// sink returns nil, and loot.Window is nil-safe throughout.
func beginCatch(d *Deps, spotType int) *loot.Window {
	if d == nil {
		return nil
	}
	pack := func() loot.Counts { return loot.PackCounts(d.Session.State().Inventory(PlayerPack)) }
	return loot.NewWindow(d.Loot, d.Config, pack, loot.Fishing(d.Config, spotType), notACatch(d))
}

// notACatch is what a fishing spot can never have given, however the pack moved, and what the
// script throws back (Deps.Junk).
func notACatch(d *Deps) func(id int) bool {
	nameOf := func(id int) (string, bool) {
		if d.Config == nil {
			return "", false
		}
		o, ok := d.Config.Obj(id)
		return o.Name, ok
	}
	return notACatchNamed(nameOf, d.Junk)
}

// notACatchNamed is notACatch with the obj name lookup passed in, so the rule is testable
// without a cache.
//
// The junk is skipped as well (owner, 2026-10-04: "bassoystercaskets shouldnt track the items it
// drops"). BassOysterCaskets drops its Raw mackerel, Raw cod, Leather boots, Leather gloves and
// Seaweed as the pack fills (junk.go), so they never reach the bank and are not what the account
// earns; recording them put them on the Loot panel's Fishing card and in its value per hour. junk
// is nil for ProgressiveFisher, which banks everything it catches and so still records it all.
func notACatchNamed(nameOf func(id int) (string, bool), junk func(name string) bool) func(id int) bool {
	return func(id int) bool {
		// A random event firing mid-catch drops its reward into the pack this window measures,
		// which put Strange fruit and Kebabs on fishing-spot cards (owner, 2026-09-21). Caskets
		// are deliberately NOT filtered: loot.gatherableAnyway keeps them because big-net
		// fishing genuinely produces them, and they are the fleet's largest loot line.
		if id == coinsObj || loot.IsEventOwnItem(id) || loot.IsEventOnlyItem(id) {
			return true
		}
		name, ok := nameOf(id)
		if !ok {
			return false
		}
		return fishingKit[strings.ToLower(name)] || (junk != nil && junk(name))
	}
}

// isKeptCatch reports whether an item called name counts as one catch on the Loot panel: a raw
// fish the script keeps. A raw fish junk names (BassOysterCaskets' Raw mackerel and Raw cod) is
// dropped, so it is no more a catch on the card than it is an item there.
func isKeptCatch(name string, junk func(name string) bool) bool {
	return fisherplan.IsRawFish(name) && (junk == nil || !junk(name))
}

// keptRawFish is CurrentRawFish counting only the raw fish isKeptCatch keeps: the catch count a
// hold reports as the window's events. For a Deps without Junk it is CurrentRawFish.
func keptRawFish(d *Deps) int {
	if d.Junk == nil {
		return CurrentRawFish(d)
	}
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && isKeptCatch(obj.Name, d.Junk) {
			n += max(int(it.Count), 1)
		}
	}
	return n
}

// The account's own kit. A net, rod, harpoon or pot arriving in the pack is a re-buy, a bank
// withdrawal, or the tool a Big fish stole being picked back up — never something a fishing spot
// gave (owner, 2026-09-20: "net shouldnt be included"). Bait and feathers are bought, not caught.
//
// By NAME rather than by id: the rungs in fisherplan name their tools the same way
// (fisherplan.Rungs, and RecoverLostTool takes rung.Tool), every one of these has several obj
// ids in content 289, and a name survives a content change that renumbers them.
var fishingKit = map[string]bool{
	"small fishing net": true, "big fishing net": true, "harpoon": true, "lobster pot": true,
	"fishing rod": true, "fly fishing rod": true, "oily fishing rod": true,
	"fishing bait": true, "feather": true,
}

// caughtSome records n more catches, and flushes if this hold has been running long enough that
// the panel should see it now.
func caughtSome(c *loot.Window, n int) {
	if c == nil || n <= 0 {
		return
	}
	c.Got(n)
	if c.Due(catchFlushEvery) {
		c.Flush()
	}
}

// coinsObj is the coin obj id. Fishing pays none, so a coin gain inside a hold came from
// somewhere else and is not the spot's loot.
const coinsObj = 995
