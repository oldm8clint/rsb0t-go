package woodcutrunner

import (
	"strings"

	"rsb0t-go/internal/loot"
)

// Measuring what a tree actually yields, for the Loot panel's Woodcutting card (2026-09-20,
// owner: "loot panel to also include mining and woodcutting and any other gathering skill").
//
// This is the second user of loot.Window, and deliberately not a second mechanism: the rule that
// keeps a continuous activity's measurement honest — snapshot, skip what the activity cannot
// have produced, and DISCARD the window when something interrupted it — lives there and is
// explained there. What is specific to chopping is only what is below.
//
// THE WINDOW IS ONE CHOP CALL. It opens on the click that starts the swing and closes when that
// call returns, for any of its reasons. That is a naturally bounded stretch: banking, travel,
// axe buying and the Guardian's random-event handling all happen between calls, never inside
// one, so nothing they put in the pack can be attributed to the tree.
//
// Events is LOGS, counted from the log-slot total Chop already watches for its own stall
// timeout. Logs are unstackable on this revision, so a slot is a log.
//
// TAINTED: the Ent. An Ent replacing the tree is a random event, and the engine has already put
// the account on it by the time Chop notices; what the pack does from then until the next click
// is the event's business, not the tree's.
//
// NOT LIVE-PROVEN. The fishing equivalent has run on the real fleet; this has not — there was no
// live engine available to this session. It is additive and reporting-only (a nil Loot sink
// records nothing and every loot.Window method is nil-safe), so the chopping itself is unchanged
// whether or not the measurement is right, but the numbers it produces are unverified against a
// running account.

// beginChop opens a measurement window on the tree loc type being chopped.
func beginChop(d *Deps, locType int, fallbackName string) *loot.Window {
	if d == nil {
		return nil
	}
	pack := func() loot.Counts { return loot.PackCounts(d.Session.State().Inventory(PlayerPack)) }
	return loot.NewWindow(d.Loot, d.Config, pack, loot.Woodcutting(d.Config, locType, fallbackName), notAChop(d))
}

// notAChop is what a tree can never have given, however the pack moved.
func notAChop(d *Deps) func(id int) bool {
	return func(id int) bool {
		// A random event that fired while the account was chopping drops its reward straight
		// into the same pack this window is measuring, so without this every tree card grows a
		// Beer, a Kebab and a Strange fruit. Observed live on the fleet (2026-09-21): an oak
		// card reading "Oak logs x8048, Casket x1, Axe handle x1, Beer x1".
		//
		// IsEventOnlyItem is derived from the event reward table rather than a list written
		// here, so an event added there is filtered without touching this file.
		if id == woodcutCoins || loot.IsEventOwnItem(id) || loot.IsEventOnlyItem(id) {
			return true
		}
		// A tree produces no caskets. The fishing filter deliberately keeps them, because big-net
		// fishing genuinely does.
		if id == loot.ObjCasket {
			return true
		}
		// A tree produces no ARROW SHAFTS or UNSTRUNG BOWS either. Woodcutting made the LOGS;
		// FLETCHING made the rest, and they have their own card (internal/loot/production.go).
		// They reached this one because a fletch that returns before its MakeX batch has finished
		// leaves the remaining products to land inside the NEXT chop window — observed live by the
		// owner for shafts on 2026-09-21 (the Draynor start camp) and for bows on 2026-09-22
		// (ProgressiveWCFletch: `chop:oak:1281` was carrying "Oak shortbow x1, Oak longbow x4").
		// The runners settling their batches before returning is the real fix; this is the belt to
		// that pair of braces, and it holds whatever the pack does for whatever reason.
		//
		// Checked BEFORE the nil-Config return below, and BY ID, so an account whose cache never
		// loaded still keeps them off the Woodcutting card. By id is also the only check that can
		// work for a bow at all: in content 289 the unstrung and the strung bow of a species share
		// a display name, so the NAME "Oak shortbow" cannot tell a fletched bow from a bought one.
		if loot.IsFletchingProduct(id, "") {
			return true
		}
		if d.Config == nil {
			return false
		}
		o, ok := d.Config.Obj(id)
		if !ok {
			return false
		}
		if loot.IsFletchingProduct(id, o.Name) {
			return true
		}
		// The account's own axe, arriving from the bank, Bob's or the market mid-run. Matched on
		// the word rather than a list of the six metals, so a tier this package does not know
		// about is still not recorded as something a tree produced.
		name := strings.ToLower(o.Name)
		// An AXE HANDLE is what is left when an axe breaks: damage, not loot, and it showed up
		// on live tree cards as "Axe handle x1" (owner, 2026-09-21).
		return strings.HasSuffix(name, " axe") || name == "axe" || name == "axe handle"
	}
}

const woodcutCoins = 995
