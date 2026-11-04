package mastercraftrunner

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// The arrow round's bank reads (mastercraftplan/arrows.go has the content facts; MakeStack does the
// work).

// ArrowOwn reads the arrow round's items out of a bank reading.
func ArrowOwn(banked []gamestate.SlotItem) mastercraftplan.ArrowHeld {
	n := func(id int32) int { return int(actions.CountByID(banked, id)) }
	return mastercraftplan.ArrowHeld{
		Shafts: n(mastercraftplan.ArrowShaftID), Feathers: n(mastercraftplan.FeatherID),
		Headless: n(mastercraftplan.HeadlessID), Tips: n(mastercraftplan.ArrowTipsID), Arrows: n(mastercraftplan.SteelArrowID),
	}
}

// HeadlessDraw draws every Arrow shaft and Feather (two stacks) when the bank holds both.
func HeadlessDraw(bank []gamestate.SlotItem) []int32 {
	if actions.CountByID(bank, mastercraftplan.ArrowShaftID) > 0 && actions.CountByID(bank, mastercraftplan.FeatherID) > 0 {
		return []int32{mastercraftplan.ArrowShaftID, mastercraftplan.FeatherID}
	}
	return nil
}

// SteelDraw draws every Headless arrow and Steel arrowtips when the bank holds both.
func SteelDraw(bank []gamestate.SlotItem) []int32 {
	if actions.CountByID(bank, mastercraftplan.HeadlessID) > 0 && actions.CountByID(bank, mastercraftplan.ArrowTipsID) > 0 {
		return []int32{mastercraftplan.HeadlessID, mastercraftplan.ArrowTipsID}
	}
	return nil
}

// ArrowProducts is what an arrow round hands the mule: the Steel arrows.
func ArrowProducts() []Product {
	return []Product{{ID: mastercraftplan.SteelArrowID, Name: mastercraftplan.SteelArrow}}
}

// ArrowLeftovers is what goes back to the mule after an arrow round: the shafts or feathers the
// other ran out against (they make nothing alone). Headless arrows stay for the tips a shop trip
// buys.
func ArrowLeftovers() []Product {
	return []Product{
		{ID: mastercraftplan.ArrowShaftID, Name: mastercraftplan.ArrowShaft},
		{ID: mastercraftplan.FeatherID, Name: mastercraftplan.Feather},
	}
}
