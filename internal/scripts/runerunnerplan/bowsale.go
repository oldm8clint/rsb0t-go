package runerunnerplan

import "sort"

// Unstrung bows: sold at a general store on the route, or alched, or kept for stringing.
//
// a user request: "lower level unstrung bows could be sold at general stores in the shop purchasing routes".
// The earlier rule of the same day (alch list): an unstrung bow is sold or alched ONLY if the account cannot string it, or it is not
// the best stringing option, and the best return goes first.
//
// The prices are sell.go's, read from content: a general store pays 40% of an item's cost for the first unit (Varrock's and Arhein's,
// 30 a unit less after it; Aemad's 20, Port Khazard's 10), and High Level Alchemy 60% of it less the Nature rune it burns (cost 20,
// runes.obj). So a cast beats a sale by half again whenever the account can cast, and a sale is the way out for a bow the account cannot
// alch (no Magic 55, no Nature rune, no fire staff and no fire runes) and cannot string. PlanBowDisposal answers per stack.
//
// This file is the decision only. RuneRunner has no sell station and the unstrung bows reach it from the mule, not from a shop, so the
// station and the kit ask that would use it are not built (docs/runerunner_ammo_sell_2026-10-13.md's note on selling stands).

// BowStack is a stack of one unstrung bow the account holds.
type BowStack struct {
	Name  string
	Cost  int
	Count int
	// Strings is true when the account can string this bow (Fletching level, a bowstring supply) and it is among the best stringing
	// options: such a stack is kept, not sold or alched.
	Strings bool
}

// Bow disposal actions.
const (
	BowKeep = "keep"
	BowAlch = "alch"
	BowSell = "sell"
)

// BowDisposal is what to do with a stack.
type BowDisposal struct {
	Name   string
	Units  int
	Action string
	// Outlet is the store key for a sale, "" otherwise.
	Outlet string
	// Return is the coins the whole stack brings, PerUnit its average.
	Return, PerUnit int
}

// NatureCost is the coins one Nature rune is worth to the cast that burns it (runes.obj cost).
const NatureCost = 20

// PlanBowDisposal decides every stack: kept for stringing, alched (canAlch: Magic, a Nature rune and a fire source in hand) or sold at the
// best of the outlets, and returns them best return per unit first. An alch stack is sold instead when the best sale of the whole stack
// pays more than the casts net of their Nature runes; a stack nothing can move (no cast, no outlet) is left out.
func PlanBowDisposal(bows []BowStack, canAlch bool, outlets []SellOutlet) []BowDisposal {
	var out []BowDisposal
	for _, b := range bows {
		if b.Count <= 0 || b.Cost <= 0 {
			continue
		}
		if b.Strings {
			out = append(out, BowDisposal{Name: b.Name, Units: b.Count, Action: BowKeep})
			continue
		}
		alch := 0
		if canAlch {
			alch = max(AlchValue(b.Cost)-NatureCost, 0) * b.Count
		}
		bestOutlet, sale := "", 0
		for _, o := range outlets {
			if !o.AllStock {
				continue
			}
			if t := SaleTotal(b.Cost, o.BuyMult, o.Delta, -1, 0, b.Count); t > sale {
				bestOutlet, sale = o.Key, t
			}
		}
		switch {
		case alch > 0 && alch >= sale:
			out = append(out, BowDisposal{Name: b.Name, Units: b.Count, Action: BowAlch, Return: alch, PerUnit: alch / b.Count})
		case sale > 0:
			out = append(out, BowDisposal{Name: b.Name, Units: b.Count, Action: BowSell, Outlet: bestOutlet, Return: sale, PerUnit: sale / b.Count})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Kept stacks last; then the best return per unit first.
		if (out[i].Action == BowKeep) != (out[j].Action == BowKeep) {
			return out[j].Action == BowKeep
		}
		return out[i].PerUnit > out[j].PerUnit
	})
	return out
}
