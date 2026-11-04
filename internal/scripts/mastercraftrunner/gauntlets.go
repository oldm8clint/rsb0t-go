package mastercraftrunner

// The goldsmith gauntlets' I/O (mastercraftplan/gauntlets.go has the content facts and the decision).

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// isGauntlets reports whether the object is the goldsmith gauntlets, by id and by the cache's name.
func isGauntlets(d *fleetrunner.Deps, id int32) bool {
	if id == mastercraftplan.GoldGauntletsID {
		return true
	}
	if d.Config != nil {
		if o, ok := d.Config.Obj(int(id)); ok && !o.Noted() && strings.EqualFold(o.Name, mastercraftplan.GoldGauntlets) {
			return true
		}
	}
	return false
}

// GauntletsWorn reports whether the goldsmith gauntlets are worn (the check superheat.rs2 makes:
// inv_total(worn, gauntlets_of_goldsmithing)).
func GauntletsWorn(d *fleetrunner.Deps) bool {
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, it := range eq.Worn() {
		if isGauntlets(d, it.ID) {
			return true
		}
	}
	return false
}

// GauntletsIn is the id of the goldsmith gauntlets a list holds (a bank or the pack), 0 for none.
func GauntletsIn(d *fleetrunner.Deps, items []gamestate.SlotItem) int32 {
	for _, it := range items {
		if it.Count != 0 && isGauntlets(d, it.ID) {
			return it.ID
		}
	}
	return 0
}

// GauntletState reads the gauntlets: worn, and how many the pack and the given bank reading hold.
func GauntletState(d *fleetrunner.Deps, bank []gamestate.SlotItem) (worn bool, inPack, inBank int) {
	count := func(items []gamestate.SlotItem) int {
		n := 0
		for _, it := range items {
			if it.Count != 0 && isGauntlets(d, it.ID) {
				n += max(1, int(it.Count))
			}
		}
		return n
	}
	return GauntletsWorn(d), count(packItems(d)), count(bank)
}

// WearGauntlets wears the goldsmith gauntlets from the pack, or from the bank (the bank opened at post for
// a one-item withdrawal) when the pack holds none. It returns why when they are not worn after.
func WearGauntlets(d *fleetrunner.Deps, post tile.Tile) (why string) {
	eq := actions.NewEquipment(d.Session, d.Components)
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	id := GauntletsIn(d, packItems(d))
	if id == 0 {
		bank, ok := OpenBank(d, post)
		if !ok || bank == nil {
			return "the bank would not open for the gauntlets"
		}
		id = GauntletsIn(d, bank.Items())
		if id != 0 {
			before := packCount(d, id)
			if err := bank.WithdrawOne(id); err == nil {
				w.DelayUntil(func() bool { return packCount(d, id) > before }, 3*time.Second)
			}
		}
		_ = bank.Close()
		d.Delay(600 * time.Millisecond)
		if id == 0 || packCount(d, id) == 0 {
			return "no gauntlets in the pack or the bank"
		}
	}
	if err := eq.EquipItemAndWait(d.Config, id, w); err != nil {
		return "wearing the gauntlets: " + err.Error()
	}
	if !GauntletsWorn(d) {
		return "the gauntlets did not show as worn"
	}
	return ""
}
