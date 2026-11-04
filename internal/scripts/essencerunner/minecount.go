package essencerunner

import (
	"strings"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/essenceplan"
)

// Measuring what a rock actually yields, for the Loot panel's Mining card (2026-09-20, owner:
// "loot panel to also include mining and woodcutting and any other gathering skill").
//
// The third user of loot.Window, and the same rule rather than a third mechanism: snapshot, skip
// what the activity cannot have produced, and DISCARD the window when something interrupted it.
// That type's doc comment explains why a continuous activity needs a boundary at all.
//
// THE WINDOW IS ONE MINE CALL, from the click that starts the swing to whichever reason ends it.
// Banking, the portal, pickaxe buying and the Guardian's random-event handling all happen
// between calls, so nothing they put in the pack lands inside one.
//
// WHAT MINING RECORDS HERE, STATED NARROWLY: this package mines RUNE ESSENCE, and nothing else.
// EssenceMiner is the only registered mining script on the fleet, so the Mining card shows
// essence and the rocks that give it. Ore from a normal rock is not recorded because no script
// mines one; the day one does, it opens a window the same way and its rocks join the same card,
// because loot.Mining keys on the loc type and the card groups on the skill.
//
// TAINTED: leaving the mine. "left-mine" means the account is no longer where it was mining —
// a teleport, a random event, a death. Whatever the pack looks like after that did not come out
// of the rock.
//
// NOT LIVE-PROVEN. No live engine was available to this session. It is additive and
// reporting-only (a nil Loot sink records nothing, and every loot.Window method is nil-safe), so
// the mining itself is unchanged whether or not the measurement is right — but the numbers are
// unverified against a running account.

// beginMine opens a measurement window on the rock loc type being mined.
func beginMine(d *Deps, locType int) *loot.Window {
	if d == nil {
		return nil
	}
	pack := func() loot.Counts { return loot.PackCounts(d.Session.State().Inventory(PlayerPack)) }
	return loot.NewWindow(d.Loot, d.Config, pack, loot.Mining(d.Config, locType, essenceplan.EssenceRock), notMined(d))
}

// notMined is what a rock can never have given, however the pack moved.
func notMined(d *Deps) func(id int) bool {
	return func(id int) bool {
		if id == mineCoins || loot.IsEventOwnItem(id) {
			return true
		}
		if d.Config == nil {
			return false
		}
		o, ok := d.Config.Obj(id)
		if !ok {
			return false
		}
		// The account's own pickaxe, arriving from the bank or Nurmof's shop mid-run. Matched on
		// the word rather than a list of the metals, so a tier this package does not know about
		// is still not recorded as something a rock produced.
		name := strings.ToLower(o.Name)
		return strings.HasSuffix(name, " pickaxe") || name == "pickaxe"
	}
}

const mineCoins = 995
