package runerunner

import (
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Delivery is one trade's outcome: donorrunner's result and the runes that left the pack.
type Delivery struct {
	donorrunner.Result
	Delivered map[string]int
}

// Deliver trades the listed items already in the pack (Resolved.DeliverItems: the runes, and
// Staffs of air when bought) to the mule at its post, with runerunnerplan.MuleRules: only those
// cross, minds first, and coins, the blade and
// anything else never go on the screen (no coins-first rule, no CoinsName split). rows and drawn
// are the withdrawal's own counts (WithdrawItems). rd must already carry the world hooks the
// caller wants (bot.WireDonorWorlds, with HomeWorld cleared so the ring keeps every other hop).
func Deliver(d *fleetrunner.Deps, rd *donorrunner.Deps, mule string, post tile.Tile, rows, drawn int, items []string) Delivery {
	rules := runerunnerplan.MuleRules(items)
	packRows := func() []donorplan.ItemRow { return thieverrunner.PackSlotRows(fleetrunner.Thiev(d)) }
	before := PackCounts(d, items)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: mule, Stand: post, RowsInBank: rows, Drawn: drawn,
		Load: func() []donorplan.LoadGroup {
			return donorplan.PlanLoad(packRows(), donorplan.DefaultMuleFreeSlots, rules)
		},
		HaulLeft: func() int { return donorplan.HaulOf(packRows(), rules) },
	})
	out := Delivery{Result: res, Delivered: map[string]int{}}
	after := PackCounts(d, items)
	for _, r := range items {
		if n := before[r] - after[r]; n > 0 {
			out.Delivered[r] = n
		}
	}
	return out
}
