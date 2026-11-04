package wcfletchrunner

import (
	"strings"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// Measuring what a ProgressiveWCFletch batch actually makes, for the Loot panel's Fletching card
// (2026-09-22, owner: "fletching loot panel: unstrung bows cut from various logs arent showing in
// the fletching panel, some bows are showing in woodcutting panel").
//
// WHY THE BOWS WERE MISSING, measured rather than guessed. Fletch (runner.go) opened no
// loot.Window at all, so no unstrung bow was ever reported as a Fletching source. The live panel
// on 2026-09-22 had exactly one Fletching source — `fletch:logs:1511`, BankFletcher's arrow
// shafts — while 47 accounts were running ProgressiveWCFletch.
//
// WHY SOME OF THEM SHOWED UP UNDER WOODCUTTING is the other half of the same hole, and it is the
// arrow-shaft bug of 2026-09-21 repeating with a different product: a bow that lands after Fletch
// has returned lands inside the next woodcutrunner.Chop call, whose window's source is the TREE.
// The same live read found `chop:oak:1281` carrying "Oak shortbow x1, Oak longbow x4" and
// `chop:willow:1308` carrying "Willow longbow x2, Willow shortbow x2" — obj ids 54, 56, 58 and
// 60, every one of them an UNSTRUNG bow. woodcutrunner.notAChop already refuses
// loot.IsFletchingProduct by id; it did not refuse these only because the twelve unstrung bow ids
// were not in that set yet. They are now (internal/loot/production.go), so the tree cannot claim
// one whatever the pack does.
//
// This is the fourth user of loot.Window and deliberately not a fourth mechanism: the rule that
// keeps a continuous activity's measurement honest — snapshot, skip what the activity cannot have
// produced, discard a window something interrupted — lives there and is explained there.
//
// THE WINDOW IS ONE MakeX BATCH. It opens after the make request is sent and closes when
// runner.go's settle says the log count has stopped falling. Banking, travel, the mule hand-off
// and the Guardian's random-event handling all happen between Fletch calls, never inside one.
//
// EVENTS ARE LOGS CONSUMED, NOT BOWS MADE, matching bankfletchrunner and loot.ActionFletch's own
// contract: loot.Source.Consumes prices a card's cost as events x the input's price (cost.go), so
// counting products would charge the card for one log per product. For a bow that happens to be
// the same number — cut_logs gives one bow per log — and it is still written as the log count,
// because the arrow-shaft rung of the same ladder gives fifteen shafts per log and would be
// fifteen times wrong. Verified against the live panel, which reads 8,428 fletch events against
// 126,435 arrow shafts: exactly 15.0 products per event, so events there are logs.
//
// NOT LIVE-PROVEN. No live engine was available to this session, and the fleet was not restarted.
// It is additive and reporting-only — a nil Loot sink returns a nil *loot.Window and every method
// on it is nil-safe — so the fletching itself is unchanged whether or not the numbers are right.

// beginFletch opens a measurement window on the LOGS being cut.
//
// Keyed on the material rather than the product, which is what makes the card symmetric with
// gathering and is loot.Production's own contract: the per-source rows then read "Oak logs",
// "Willow logs" and so on, with the bows they became as the row's items.
func beginFletch(d *Deps, logsID int, logsName string) *loot.Window {
	if d == nil {
		return nil
	}
	pack := func() loot.Counts {
		return loot.PackCounts(d.Session.State().Inventory(woodcutrunner.PlayerPack))
	}
	return loot.NewWindow(d.Loot, d.Config, pack, loot.Fletching(d.Config, logsID, logsName), notAFletch(d))
}

// notAFletch is what a knife-on-logs batch can never have given, however the pack moved.
//
// Deliberately the same shape as bankfletchrunner.notAFletch and woodcutrunner.notAChop rather
// than a cleverer rule: the id checks come FIRST, before the nil-Config return, so an account
// whose cache never loaded still gets them. Only the name checks below need a resolved obj.
func notAFletch(d *Deps) func(id int) bool {
	return func(id int) bool {
		// A random event that fired mid-batch drops its reward straight into the same pack this
		// window is measuring, so without this every Fletching row grows a Beer and a Casket.
		// Derived from the event reward table rather than a list written here, so an event added
		// there is filtered without touching this file.
		if id == fletchCoins || loot.IsEventOwnItem(id) || loot.IsEventOnlyItem(id) {
			return true
		}
		// A knife makes no caskets. The fishing filter deliberately keeps them, because big-net
		// fishing genuinely does; nothing about cutting a log does.
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
		name := strings.TrimSpace(o.Name)
		// The account's own knife and the logs themselves. Neither can RISE during a batch, so
		// loot.Gained would drop them anyway; they are named because a bank withdrawal or a mule
		// hand-out that races the settle wait is the one way they could, and a Knife on the
		// Fletching card would be the same lie as an Axe handle on the Woodcutting one.
		if strings.EqualFold(name, wcfletchplan.Knife) || strings.HasSuffix(strings.ToLower(name), " logs") || strings.EqualFold(name, woodcutLogs) {
			return true
		}
		// An axe, and the handle left when one breaks. This script chops as well as fletches, so
		// it carries an axe and takes replacements from the bank, Bob's and the mule mid-run —
		// none of which a knife made. Matched on the word rather than a list of the six metals,
		// so a tier this package does not know about is still not reported as fletched.
		lower := strings.ToLower(name)
		return strings.HasSuffix(lower, " axe") || lower == "axe" || lower == "axe handle"
	}
}

// fletchCoins is the Coins obj id, and woodcutLogs the name of the unprefixed first-rung logs.
// Both are spelled out here for the same reason bankfletchrunner spells them out: this filter has
// to hold on an account with no cache config, where a name lookup is not available at all.
const (
	fletchCoins = 995
	woodcutLogs = "Logs"
)
