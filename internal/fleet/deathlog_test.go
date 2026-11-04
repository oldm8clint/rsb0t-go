package fleet

import (
	"strings"
	"testing"

	"rsb0t-go/internal/deathreport"
	"rsb0t-go/internal/gamestate"
)

func TestIsAlchStockIsTheOwnersList(t *testing.T) {
	for name, want := range map[string]bool{"Yew shortbow": true, "yew LONGBOW": true, " Adamant plateskirt ": true, "Bronze dagger": false, "": false} {
		if got := isAlchStock(name); got != want {
			t.Errorf("isAlchStock(%q) = %v, want %v", name, got, want)
		}
	}
}

// With no cache the ids stay readable and the line still carries the tile, the killer and the hitpoints.
func TestDeathInputWithoutACache(t *testing.T) {
	dc := gamestate.DeathContext{
		HaveTile: true, X: 2508, Z: 3097, HPPeak: 40, HPLast: 5, MaxHP: 50,
		Attackers: []gamestate.DeathActor{{Type: 111}, {Type: 111}},
		Nearby:    []gamestate.DeathActor{{Type: 7}},
		Pack:      []gamestate.SlotItem{{Slot: 0, Item: gamestate.Item{ID: 858, Count: 721}}},
	}
	line := deathreport.Line(deathInput(dc, nil))
	for _, frag := range []string{"DEATH at 2508,3097,0 (Yanille west ogres)", "killed by 2 npc 111", "HP 40/50 before, 5 left", "food left 0", "no alch stock"} {
		if !strings.Contains(line, frag) {
			t.Errorf("%q lacks %q", line, frag)
		}
	}
}
