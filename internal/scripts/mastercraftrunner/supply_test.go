package mastercraftrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

func TestBankReadsOfTheLaterRounds(t *testing.T) {
	bank := []gamestate.SlotItem{
		{Slot: 0, Item: gamestate.Item{ID: mastercraftplan.ArrowShaftID, Count: 900}},
		{Slot: 1, Item: gamestate.Item{ID: mastercraftplan.FeatherID, Count: 600}},
		{Slot: 2, Item: gamestate.Item{ID: mastercraftplan.HeadlessID, Count: 30}},
		{Slot: 3, Item: gamestate.Item{ID: 440, Count: 50}},  // iron ore
		{Slot: 4, Item: gamestate.Item{ID: 453, Count: 120}}, // coal
		{Slot: 5, Item: gamestate.Item{ID: mastercraftplan.NatureRuneID, Count: 70}},
		{Slot: 6, Item: gamestate.Item{ID: 199, Count: 12}}, // grimy guam
		{Slot: 7, Item: gamestate.Item{ID: 207, Count: 3}},  // grimy ranarr
	}
	a := ArrowOwn(bank)
	if a.Shafts != 900 || a.Feathers != 600 || a.Headless != 30 || a.HeadlessToMake() != 600 {
		t.Errorf("arrows %+v", a)
	}
	if ids := HeadlessDraw(bank); len(ids) != 2 {
		t.Errorf("headless draw %v", ids)
	}
	if ids := SteelDraw(bank); ids != nil {
		t.Errorf("steel draw %v with no tips", ids)
	}
	own := SuperheatOwn(bank)
	if own("Iron ore") != 50 || own("Coal") != 120 || own(mastercraftplan.NatureRune) != 70 {
		t.Errorf("superheat reading wrong")
	}
	h := HerbCounts(bank)
	if h[199] != 12 || h[207] != 3 || len(h) != 2 {
		t.Errorf("herbs %v", h)
	}
	if ids := HerbDraw(herbplan.Herbs)(bank); len(ids) != 1 || ids[0] != 199 {
		t.Errorf("herb draw %v, want the lowest level first", ids)
	}
}

func TestProductListsCarryTheBarsArrowsAndCleanHerbs(t *testing.T) {
	tradeable := 0
	for _, h := range herbplan.Herbs {
		if mastercraftplan.HerbTradeable(h) {
			tradeable++
		}
	}
	if len(BarProducts()) != len(mastercraftplan.Bars) || len(HerbProducts()) != tradeable || tradeable != len(herbplan.Herbs)-5 {
		t.Error("product lists short")
	}
	// The Jungle Potion herbs cannot be traded, so no hand-over draws one (live 5 Oct: 17 Rogue's
	// purse failed every deliver round).
	for _, p := range HerbProducts() {
		if strings.Contains(strings.ToLower(p.Name), "rogue") {
			t.Errorf("an untradeable herb is a product: %+v", p)
		}
	}
	if p := ArrowProducts(); len(p) != 1 || p[0].ID != mastercraftplan.SteelArrowID {
		t.Errorf("arrow products %+v", p)
	}
}
