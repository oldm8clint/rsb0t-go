package questrunner

import "rsb0t-go/internal/actions"

// StripWorn takes every worn piece off in ONE burst, five Remove clicks to a game tick, and confirms once on the worn
// panel (twopertick-1012: a quest's bank-side strip took one piece off, slept 600 ms, and took the next, up to eleven
// of them). Remove is an inventory button with no delay (content player/scripts/equip.rs2 [inv_button1,wornitems:wear]),
// and the worn panel never closes up, so every slot named from one reading is still right when the engine reaches it.
// A piece a full pack refuses stays on; the caller reads WornCounts to see whether it did.
func StripWorn(d *Deps) actions.EquipBatch {
	eq := actions.NewEquipment(d.Session, d.Components)
	var ids []int32
	for _, it := range eq.Worn() {
		if it.ID > 0 {
			ids = append(ids, it.ID)
		}
	}
	r := eq.UnequipMany(ids, actions.DelayWaiter(d.Delay))
	if r.Wanted > 0 {
		d.logf("%s", r.Log("took off"))
	}
	return r
}

// EquipMany wears each held item of ids in one burst (five Wield/Wear clicks a tick, confirmed once on the worn
// panel, the stragglers worn again one at a time) and returns how many of ids are worn afterwards.
func EquipMany(d *Deps, ids []int) int {
	if len(ids) == 0 || !PackVisible(d) {
		return 0
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	ids32 := make([]int32, 0, len(ids))
	for _, id := range ids {
		ids32 = append(ids32, int32(id))
	}
	r := eq.EquipMany(d.Config, ids32, waiterFor(d))
	d.logf("%s", r.Log("wore"))
	worn := WornCounts(d)
	n := 0
	for _, id := range ids {
		if worn[id] > 0 {
			n++
		}
	}
	return n
}
