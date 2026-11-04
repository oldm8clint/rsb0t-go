package fleetrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
)

// packSlotOf is the pack row holding id.
func packSlotOf(t *testing.T, d *Deps, id int32) gamestate.SlotItem {
	t.Helper()
	for _, it := range PackSlotItems(d) {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("object %d is not in the pack", id)
	return gamestate.SlotItem{}
}

// CastOn (CX-1, the gate's casts on the temple's legs) writes the clear-move and the named spell in ONE write, on the slot it is told, and
// tells the walk alchemy's clock; it casts nothing for a spell it does not know.
func TestCastOnWritesTheClearMoveAndTheNamedSpellInOneWrite(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Magic longbow", true)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	d := &Deps{Session: sim, Config: cfg}
	bow := packSlotOf(t, d, bowNote)
	tr := &Travel{}
	if err := tr.CastOn(d, "Low Level Alchemy", bow); err != nil {
		t.Fatalf("CastOn: %v", err)
	}
	sent := sim.Sent()
	if len(sent) != 1 || len(sent[0]) != opheldu+9 {
		t.Fatalf("%d writes, want one of an OPHELDU and an OPHELDT: %v", len(sent), sent)
	}
	casts := castsSent(sim)
	if len(casts) != 1 || casts[0].obj != int(bowNote) || casts[0].slot != bow.Slot || casts[0].spell != 1162 {
		t.Errorf("casts %+v, want Low Level Alchemy (1162) on the note in slot %d", casts, bow.Slot)
	}
	if clears := clearsSent(sim); len(clears) != 1 || clears[0][0] != int(bowNote) || clears[0][1] == bow.Slot {
		t.Errorf("clear-moves %v: want the bow note named at a slot other than %d", clears, bow.Slot)
	}
	if tr.gapOver(time.Now()) {
		t.Error("the walk alchemy's clock does not know about the cast")
	}
	// High Level Alchemy is the other spell the gate casts.
	if err := (&Travel{}).CastOn(d, "High Level Alchemy", bow); err != nil {
		t.Fatalf("CastOn high: %v", err)
	}
	if got := castsSent(sim); len(got) != 2 || got[1].spell != 1178 {
		t.Errorf("casts %+v, want the second to be High Level Alchemy (1178)", got)
	}
	// A spell it does not know is nothing sent; no Travel is an error.
	writes := len(sim.Sent())
	if err := tr.CastOn(d, "Fire Strike", bow); err == nil || len(sim.Sent()) != writes {
		t.Errorf("an unknown spell: %v, %d writes after %d", err, len(sim.Sent()), writes)
	}
	var none *Travel
	if err := none.CastOn(d, "Low Level Alchemy", bow); err == nil || len(sim.Sent()) != writes {
		t.Errorf("no Travel: %v, %d writes after %d", err, len(sim.Sent()), writes)
	}
}
