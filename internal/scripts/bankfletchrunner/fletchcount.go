package bankfletchrunner

import (
	"strings"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/bankfletchplan"
)

// Measuring what a fletching batch actually makes, for the Loot panel's Fletching card
// (2026-09-21, owner: "arrow shafts are showing up in the woodcutting section probably should
// have a fletching section for all fletching materials the fleet makes").
//
// This is the third user of loot.Window and deliberately not a third mechanism: the rule that
// keeps a continuous activity's measurement honest — snapshot, skip what the activity cannot have
// produced, discard a window something interrupted — lives there and is explained there. What is
// specific to fletching is only what is below.
//
// IT LIVES HERE RATHER THAN IN THE CALLERS. Fletch has two: BankFletcher (internal/bot/
// bankfletcher.go) and ProgressiveWoodcutter's Draynor start camp (woodcutrunner.FletchShafts,
// which calls this package unchanged). Both make the same product the same way, so measuring
// inside Fletch means one recorder rather than two that can drift — and it is also the only
// place that knows when the batch has settled, which is the whole reason the shafts were landing
// on the wrong card in the first place (see internal/loot/production.go's header).
//
// THE WINDOW IS ONE MakeX BATCH. It opens after the make request is sent and closes when the
// pack has stopped moving (runner.go's settle). Banking, travel and the Guardian's random-event
// handling all happen between Fletch calls, never inside one.
//
// Events is LOGS CONSUMED, not shafts made. This comment said the opposite until 2026-09-22 and
// was simply wrong about its own code: runner.go calls window.Got(cut) with the fall in the
// MATERIAL count, and says so in its own comment. The live panel settles it — the one Fletching
// source on it read 8,428 events against 126,435 arrow shafts, exactly 15.0 shafts per event,
// which is content's `data=shafts,15` and so one event per LOG. loot.ActionFletch's contract
// requires it: the panel prices a card's cost as events x the input's price (loot/cost.go), so
// counting shafts would charge fifteen logs for every one cut. The shafts are not lost — they
// are the Drop's Items, exactly where a tree's logs are.
//
// NOT LIVE-PROVEN. No live engine was available to this session. It is additive and
// reporting-only (a nil Loot sink records nothing and every loot.Window method is nil-safe), but
// the numbers it produces are unverified against a running account. The settle wait in runner.go
// is NOT reporting-only and does change what the script does — see its own comment.

// beginFletch opens a measurement window on the material being cut.
func beginFletch(d *Deps, materialID int32, material string) *loot.Window {
	if d == nil {
		return nil
	}
	pack := func() loot.Counts { return loot.PackCounts(d.Session.State().Inventory(PlayerPack)) }
	return loot.NewWindow(d.Loot, d.Config, pack, loot.Fletching(d.Config, int(materialID), material), notAFletch(d))
}

// notAFletch is what a fletching batch can never have given, however the pack moved.
func notAFletch(d *Deps) func(id int) bool {
	return func(id int) bool {
		// A random event that fired mid-batch drops its reward straight into the same pack this
		// window is measuring. Derived from the event reward table rather than a list written
		// here, so an event added there is filtered without touching this file — the same two
		// calls woodcutrunner.notAChop makes, and for the same reason.
		if id == fletchCoins || loot.IsEventOwnItem(id) || loot.IsEventOnlyItem(id) {
			return true
		}
		if id == loot.ObjCasket {
			return true
		}
		if d.Config == nil {
			return false
		}
		o, ok := d.Config.Obj(id)
		if !ok {
			return false
		}
		// The account's own knife, and the material itself. Neither can RISE during a batch, so
		// Gained would drop them anyway; they are named because a bank withdrawal that races the
		// settle wait is the one way they could, and a Knife on the Fletching card would be the
		// same lie as an Axe handle on the Woodcutting one.
		name := strings.TrimSpace(o.Name)
		return strings.EqualFold(name, bankfletchplan.Knife) || strings.HasSuffix(strings.ToLower(name), " logs") || strings.EqualFold(name, bankfletchplan.Logs)
	}
}

const fletchCoins = 995
