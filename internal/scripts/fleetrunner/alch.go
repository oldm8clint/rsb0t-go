package fleetrunner

import (
	"sort"
	"strings"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/thieverrunner"
)

// ObjCost is an obj's content cost (a note carries its item's), 0 when unknown.
func ObjCost(d *Deps, id int32) int {
	if d.Config == nil {
		return 0
	}
	if o, ok := d.Config.Obj(int(id)); ok {
		return int(o.Cost)
	}
	return 0
}

// NamedByCostDesc is the items of d's pack or bank (items) whose obj carries name, the dearest first: the
// obj a High Alchemy cast pays most for (a strung bow before the unstrung one of its name, an item
// before its note, which carries the same cost), in slot order among equals.
func NamedByCostDesc(d *Deps, items []gamestate.SlotItem, name string) []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for _, it := range items {
		if it.Count > 0 && strings.EqualFold(ObjName(d, it.ID), name) {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return ObjCost(d, out[i].ID) > ObjCost(d, out[j].ID) })
	return out
}

// FireStaffWorn is a Staff of fire (or any fire staff Alcher knows) wielded now.
func FireStaffWorn(d *Deps) bool {
	for _, it := range WornItems(d) {
		if alchplan.IsFireStaff(it.Name) {
			return true
		}
	}
	return false
}

// NotedID is the bank-note obj of item id, 0 when it has none.
func NotedID(d *Deps, id int32) int32 {
	if d.Config == nil {
		return 0
	}
	o, ok := d.Config.Obj(int(id))
	if !ok {
		return 0
	}
	for _, cand := range d.Config.ObjIDs(o.Name) {
		if co, ok := d.Config.Obj(cand); ok && co.CertLink == int(id) {
			return int32(cand)
		}
	}
	return 0
}

// FireStaffInPack is a fire staff carried, not wielded.
func FireStaffInPack(d *Deps) bool {
	for _, it := range PackItems(d) {
		if alchplan.IsFireStaff(it.Name) {
			return true
		}
	}
	return false
}

// WieldFireStaffFromPack wields a fire staff the pack carries (a mule hand-over, or one just drawn), and reports
// whether one is worn afterwards. The weapon it replaces lands in the pack: the next bank pass banks it.
func WieldFireStaffFromPack(d *Deps) bool {
	if FireStaffWorn(d) {
		return true
	}
	if d.Components == nil || d.Config == nil {
		return false
	}
	var staff gamestate.SlotItem
	found := false
	for _, it := range PackSlotItems(d) {
		if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && alchplan.IsFireStaff(o.Name) {
			staff, found = it, true
			break
		}
	}
	if !found {
		return false
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	if err := eq.EquipItemAndWait(d.Config, staff.ID, thieverrunner.Waiter(Thiev(d))); err != nil {
		d.Logf("endgame: wielding the %s: %v", ObjName(d, staff.ID), err)
		return false
	}
	return FireStaffWorn(d)
}
