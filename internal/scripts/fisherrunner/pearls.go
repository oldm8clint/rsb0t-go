package fisherrunner

import (
	"strings"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/pearlplan"
)

// THE CHISEL for cutting pearls into bolt tips while fishing (a user request: "bringing a chisel with
// them"). It is carried only once the cut can be made (Fletching 41, cutPearls on) so that it takes a
// pack slot only when it earns it, kept through every bank deposit from then on, drawn from the account's
// own bank when the pack has none, and got once, for a coin or two, from Arhein's store in Catherby
// (content's [arheinstore] stocks two Chisels and restocks them) or from the mule when it holds one.
//
// The rules for when a cycle cuts are fisherplan.PlanCuts; this file is what keeps the chisel in the pack.

// cutting reports whether the pearl cut applies to this account right now: the setting is on and
// Fletching has reached the level a pearl is cut at. A nil Cycle never cuts.
func (c *Cycle) cutting(d *Deps) bool {
	return c != nil && c.Opts.CutPearls && FletchingLevel(d) >= pearlplan.Level
}

// Cutting is the exported form of cutting, for the bot's decisions.
func Cutting(d *Deps) bool { return d.Cycle.cutting(d) }

// keepsChisel reports whether a bank deposit leaves an item called name in the pack because it is the
// chisel the pearl cut needs. A nil Cycle keeps nothing.
func (c *Cycle) keepsChisel(d *Deps, name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), fisherplan.Chisel) && c.cutting(d)
}

// ChiselHeld reports whether the pack holds an unnoted chisel.
func ChiselHeld(d *Deps) bool {
	_, ok := fisherplan.ChiselIn(cycleItems(d))
	return ok
}

// KnifeHeld reports whether the pack holds an unnoted knife.
func KnifeHeld(d *Deps) bool {
	for _, it := range cycleItems(d) {
		if !it.Noted && strings.EqualFold(strings.TrimSpace(it.Name), fisherplan.KnifeName) {
			return true
		}
	}
	return false
}

// DrawChisel draws one chisel from an open bank when the cut applies and the pack holds none, and
// reports whether one was drawn.
func DrawChisel(d *Deps, bank *actions.Bank) bool {
	if !d.Cycle.cutting(d) || ChiselHeld(d) {
		return false
	}
	if WithdrawOneOf(d, bank, fisherplan.Chisel) {
		d.logf("pearl tips: drew a chisel from the bank")
		return true
	}
	// No chisel anywhere: carry the coins to buy one at Arhein's (a user request: "bassoystercasket
	// should get its own chisel if it doesnt have one"). Live, the buy after a hand-off clicked with an
	// empty purse and nothing arrived.
	if have := Coins(d); have < ChiselCoins {
		if err := WithdrawXByName(d, bank, "Coins", ChiselCoins-have); err != nil {
			d.logf("pearl tips: no chisel in the pack or the bank, and no coins drawn to buy one: %v", err)
		} else {
			d.logf("pearl tips: no chisel in the pack or the bank — drew %d coins to buy one at Arhein's", ChiselCoins-have)
		}
	}
	return false
}

// ChiselCoins is the purse a chisel is bought with: Arhein's price with room to spare.
const ChiselCoins = 10
