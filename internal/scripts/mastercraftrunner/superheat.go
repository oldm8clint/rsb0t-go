package mastercraftrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The superheat round's I/O (mastercraftplan/superheat.go has the content facts and the pace).

// Stat indexes (the engine's PlayerStat order: magic 6, smithing 13).
const (
	MagicStat    = 6
	SmithingStat = 13
)

// SuperheatSpell is the spell's name as actions.ItemSpellComponent knows it.
const SuperheatSpell = "Superheat Item"

func statOf(d *fleetrunner.Deps, stat int) (level, xp int) {
	if sk, ok := d.Session.State().Skill(stat); ok {
		return sk.Level, sk.XP
	}
	return 0, 0
}

// FireStaffWorn reports whether a fire staff (alchplan.FireStaffs) is worn.
func FireStaffWorn(d *fleetrunner.Deps) bool {
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, it := range eq.Worn() {
		if o, ok := d.Config.Obj(int(it.ID)); ok && alchplan.IsFireStaff(o.Name) {
			return true
		}
	}
	return false
}

// FireStaffIn is the id of the first fire staff a list holds (a bank or the pack), 0 for none.
func FireStaffIn(d *fleetrunner.Deps, items []gamestate.SlotItem) int32 {
	for _, it := range items {
		if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && alchplan.IsFireStaff(o.Name) {
			return it.ID
		}
	}
	return 0
}

// KeepRunes keeps the spell's runes in the pack through a deposit.
func KeepRunes(id int32) bool {
	return id == mastercraftplan.NatureRuneID || id == mastercraftplan.FireRuneID
}

// WieldFireStaff takes a fire staff out of an open bank (or the pack) and wields it, returning the
// weapon it replaced (0 when none), for the round to put back at its end.
func WieldFireStaff(d *fleetrunner.Deps, post tile.Tile) (staff int32, previous int32, why string) {
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, it := range eq.Worn() {
		if it.Slot == weaponSlot {
			previous = it.ID
		}
	}
	staff = FireStaffIn(d, packItems(d))
	if staff == 0 {
		bank, ok := OpenBank(d, post)
		if !ok || bank == nil {
			return 0, previous, "the bank would not open for the fire staff"
		}
		staff = FireStaffIn(d, bank.Items())
		if staff != 0 {
			before := packCount(d, staff)
			if err := bank.WithdrawOne(staff); err == nil {
				thieverrunner.Waiter(fleetrunner.Thiev(d)).DelayUntil(func() bool { return packCount(d, staff) > before }, 3*time.Second)
			}
		}
		_ = bank.Close()
		d.Delay(600 * time.Millisecond)
		if staff == 0 || packCount(d, staff) == 0 {
			return 0, previous, "no fire staff in the pack or the bank"
		}
	}
	if err := eq.EquipItemAndWait(d.Config, staff, thieverrunner.Waiter(fleetrunner.Thiev(d))); err != nil {
		return staff, previous, "wielding the fire staff: " + err.Error()
	}
	if !FireStaffWorn(d) {
		return staff, previous, "the fire staff did not show as worn"
	}
	return staff, previous, ""
}

// Rewield wields id again when the pack holds it (the weapon a fire staff replaced).
func Rewield(d *fleetrunner.Deps, id int32) {
	if id == 0 || packCount(d, id) == 0 {
		return
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	if err := eq.EquipItemAndWait(d.Config, id, thieverrunner.Waiter(fleetrunner.Thiev(d))); err != nil {
		logf(d, "mastercrafter: superheat: wielding %d again: %v", id, err)
	}
}

// weaponSlot is the worn inventory's weapon slot (the engine's wearpos 3).
const weaponSlot = 3

// BarProducts are the bars a superheat round hands the mule.
func BarProducts() []Product {
	var out []Product
	for _, b := range mastercraftplan.Bars {
		out = append(out, Product{ID: b.BarID, Name: b.Name})
	}
	return out
}

// SuperheatOwn reads a superheat round's items out of a bank reading, by name.
func SuperheatOwn(banked []gamestate.SlotItem) mastercraftplan.SuperheatStock {
	byName := map[string]int{
		mastercraftplan.NatureRune: int(actions.CountByID(banked, mastercraftplan.NatureRuneID)),
		mastercraftplan.FireRune:   int(actions.CountByID(banked, mastercraftplan.FireRuneID)),
		mastercraftplan.Coal:       int(actions.CountByID(banked, mastercraftplan.CoalID)),
	}
	for _, b := range mastercraftplan.Bars {
		byName[b.Ore] = int(actions.CountByID(banked, b.OreID))
		if b.SecondPer > 0 && b.Second != mastercraftplan.Coal {
			byName[b.Second] = int(actions.CountByID(banked, b.SecondID))
		}
	}
	return func(item string) int { return byName[item] }
}
