package woodcutrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// Robbing a Man for a ship fare, when one is standing closer than the mule.
//
// The owner's instruction (2026-09-21): "if they have no caskets and no coins, they should find
// a nearby man if its closer than walking all the way to the mule and queuing". That is the
// third rung of the fare ladder -- own bank, then banked caskets, then a Man -- and the mule
// stays the last resort rather than the first.
//
// It matters because of what the mule costs, which is not the walk. the mule takes ONE trade
// partner at a time, and the fleet measured 1.51 trades a minute against 5.0 distinct donors a
// minute: a 3.3x shortfall, so a 60-coin fare is 83 tiles each way AND a queue behind the whole
// cohort. A Man two tiles away carries 3-25 coins a pocket and no queue at all.
//
// The method is thieverplan's own "Man" rung rather than a second description of a Man here:
// it already carries the name list (Woman counts), the stun ticks and the loot list, all of it
// ported from the TS script and verified. Only the ANCHOR is replaced, because that rung is
// anchored on the Varrock courtyard and this is a woodcutter standing wherever it is standing.

// robAttempts bounds one funding burst. Content's Man pocket pays 3-25 coins, so a 60-coin fare
// wants a handful of successes; this allows for the misses and the 8-tick stuns between them
// without ever becoming a Thieving session.
const robAttempts = 25

// robLeash is how far a target may be and still count as "nearby". Deliberately small: the
// whole claim being made is that this is cheaper than the mule, and a target 30 tiles away
// through a door is not obviously cheaper than anything.
const robLeash = 12

// RobNearbyForCoins pickpockets a nearby Man or Woman until the pack holds need coins, and
// returns the coins in hand at the end. It does nothing and reports what is already held when
// there is no target in sight, or when the mule is closer than the nearest one.
//
// It never WALKS to find a Man. Targets have to be visible from where the account already is,
// which is what keeps the comparison against the mule honest -- see woodcutplan.PreferRobbing.
func RobNearbyForCoins(d *Deps, need int, muleStand tile.Tile) int {
	if d == nil || d.Config == nil || d.Session == nil {
		return 0
	}
	if CoinsHeld(d) >= need {
		return CoinsHeld(d)
	}
	m, ok := thieverplan.MethodByLabel("Man")
	if !ok {
		return CoinsHeld(d)
	}
	here := currentTile(d)
	m.Anchor, m.Leash = here, robLeash

	target, found := nearestRobTarget(d, m, here)
	if !found {
		return CoinsHeld(d)
	}
	if !woodcutplan.PreferRobbing(tile.Chebyshev(here, target), tile.Chebyshev(here, muleStand)) {
		return CoinsHeld(d)
	}
	d.logf("fare: a %s is %d tiles away and %q is %d — robbing rather than queuing",
		m.NPC, tile.Chebyshev(here, target), "the mule", tile.Chebyshev(here, muleStand))

	td := thief(d)
	for i := 0; i < robAttempts && CoinsHeld(d) < need; i++ {
		// Re-anchor every attempt: a Man wanders, and so does the account when it steps to one.
		m.Anchor = currentTile(d)
		res := thieverrunner.Pickpocket(td, m)
		if res.Reason == "no-target" {
			break
		}
		if !d.Delay(600 * time.Millisecond) {
			break
		}
	}
	return CoinsHeld(d)
}

// nearestRobTarget reports the tile of the nearest visible NPC the method accepts. It goes
// through thieverplan's own ChooseTarget rather than scanning here, so "who counts as a target"
// is answered in exactly one place -- including the Pickpocket-op check, which matters because
// some Men in content carry no such option at all.
func nearestRobTarget(d *Deps, m thieverplan.Method, here tile.Tile) (tile.Tile, bool) {
	names := thieverplan.NPCNames(m)
	var best tile.Tile
	bestDist := m.Leash + 1
	for _, n := range d.Session.State().NPCs() {
		npcType, found := d.Config.Npc(n.Type)
		if !found {
			continue
		}
		matched := false
		for _, name := range names {
			if strings.EqualFold(strings.TrimSpace(npcType.Name), strings.TrimSpace(name)) {
				matched = true
				break
			}
		}
		if !matched || cachecfg.OpIndex(npcType.Ops, thieverplan.PickpocketOp) == 0 {
			continue
		}
		t := tile.Tile{X: n.X, Z: n.Z, Level: n.Level}
		if dist := tile.Chebyshev(here, t); dist < bestDist {
			best, bestDist = t, dist
		}
	}
	return best, bestDist <= m.Leash
}

// thief adapts d to thieverrunner.Deps — the same field-for-field copy fresh() already does for
// freshstartrunner, and for the same reason.
func thief(d *Deps) *thieverrunner.Deps {
	return &thieverrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: thieverrunner.Delayer(d.Delay),
		Log: d.Log, Loot: d.Loot,
	}
}
