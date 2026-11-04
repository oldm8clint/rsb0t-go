package fleetrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetplan"
)

// A pack of 721 noted Yew shortbows and 225 Yew longbows (the 8 Oct 2026 death) leaves a bank visit with at most the
// visit's budget of casts, the dearest slots first, and the runes stay.
func TestStockKeeperBanksTheSurplusAndKeepsATripsWorth(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	short := objOf(t, cfg, "Yew shortbow", true)
	long := objOf(t, cfg, "Yew longbow", true)
	plate := objOf(t, cfg, "Adamant platebody", true)
	lobster := objOf(t, cfg, "Lobster", false)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{
		{ID: short, Count: 721}, {ID: long, Count: 225}, {ID: plate, Count: 40}, {ID: nature, Count: 300}, {ID: fire, Count: 900}, {ID: lobster, Count: 3},
	})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	var logged []string
	d := &Deps{Session: sim, Config: cfg, Log: func(s string) { logged = append(logged, s) }}
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}

	keeps := tr.StockKeeper(d, 60, "a safespot pen")
	var kept []string
	for _, it := range PackSlotItems(d) {
		if keeps(it) {
			kept = append(kept, ObjName(d, it.ID))
		}
	}
	// Neither bow stack fits 60 casts, so both go to the bank; the casting runes stay, the lobster is not kit, and the
	// platebody is not on the default list (no stock, and nothing the kit keeps).
	got := strings.Join(kept, ",")
	if strings.Contains(got, "Yew shortbow") || strings.Contains(got, "Yew longbow") {
		t.Fatalf("kept %s: the 946 casts of bows must go to the bank at a budget of 60", got)
	}
	if !strings.Contains(got, "Nature rune") || !strings.Contains(got, "Fire rune") {
		t.Fatalf("kept %s: the casting runes stay", got)
	}
	if strings.Contains(got, "Lobster") {
		t.Fatalf("kept %s: food is not the kit", got)
	}
	if len(logged) == 0 || !strings.Contains(logged[0], "alch stock: carrying") || !strings.Contains(logged[0], "the rest stays banked") {
		t.Fatalf("log = %q, want the alch stock line", logged)
	}

	// A budget of 800 keeps the dearer of the two bow stacks that fit (the longbow pays 768 a cast, the shortbow 480).
	big := tr.StockKeeper(d, 800, "the walks")
	kept = nil
	for _, it := range PackSlotItems(d) {
		if big(it) {
			kept = append(kept, ObjName(d, it.ID))
		}
	}
	got = strings.Join(kept, ",")
	if !strings.Contains(got, "Yew longbow") || strings.Contains(got, "Yew shortbow") {
		t.Fatalf("budget 800 kept %s, want the longbows (225) and not the 721 shortbows", got)
	}

	// No budget: nothing of the stock, the runes still stay.
	none := tr.StockKeeper(d, 0, "the melee trainer")
	for _, it := range PackSlotItems(d) {
		name := ObjName(d, it.ID)
		if tr.IsAlchStock(name) && none(it) {
			t.Fatalf("budget 0 kept %s", name)
		}
	}
	var nilTravel *Travel
	if nilTravel.StockKeeper(d, 100, "x")(PackSlotItems(d)[0]) {
		t.Error("a nil Travel kept something")
	}
}

func TestAlchKeepsNoStockKeepsTheRunesAndNotTheBows(t *testing.T) {
	cfg := travelTestConfig(t)
	sim := actionsim.New(actionsim.Objs{}, nil)
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	d := &Deps{Session: sim, Config: cfg}
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	for name, want := range map[string]bool{"Nature rune": true, "Fire rune": true, "Yew shortbow": false, "Adamant plateskirt": false, "Lobster": false} {
		if got := tr.AlchKeepsNoStock(d, name); got != want {
			t.Errorf("AlchKeepsNoStock(%q) = %v, want %v", name, got, want)
		}
	}
}
