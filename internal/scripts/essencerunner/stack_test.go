package essencerunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/essenceplan"
)

const (
	ess      = int32(essenceplan.EssenceID)
	essNote  = int32(essenceplan.NotedEssenceID)
	bronzePk = int32(1265)
	coinsID  = int32(995)
)

func stackObjs() actionsim.Objs {
	return actionsim.Objs{
		Stackable: func(id int32) bool { return id == coinsID },
		NoteOf: func(id int32) int32 {
			if id == ess {
				return essNote
			}
			return 0
		},
		UnnoteOf: func(id int32) int32 {
			if id == essNote {
				return ess
			}
			return 0
		},
	}
}

func stackRig(t *testing.T, pack, bank []actionsim.Stack) (*actionsim.Session, *Deps, *actions.Bank) {
	t.Helper()
	sim := actionsim.New(stackObjs(), pack)
	sim.OpenBank(bank)
	d := &Deps{Session: sim, Components: actionsim.Components(), Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	return sim, d, actions.NewBank(sim, actionsim.Components())
}

func countOf(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func slotsUsed(rows []actionsim.Stack) int {
	n := 0
	for _, r := range rows {
		if r.ID != 0 && r.Count > 0 {
			n++
		}
	}
	return n
}

// 5,000 of the bank's 6,200 essence come out as ONE note stack, one pack slot, and the bank is left in item
// mode with the rest.
func TestDrawNoteStackTakesOneStackOfNotes(t *testing.T) {
	sim, d, bank := stackRig(t, []actionsim.Stack{{ID: bronzePk, Count: 1}},
		[]actionsim.Stack{{ID: coinsID, Count: 90_000}, {ID: ess, Count: 6200}})
	res := DrawNoteStack(d, bank, 5000)
	if res.Drawn != 5000 || res.Why != "" || res.NoteID != essNote || res.Held != 6200 || res.Asked != 5000 {
		t.Fatalf("%+v", res)
	}
	pack := sim.Pack()
	if countOf(pack, essNote) != 5000 || countOf(pack, ess) != 0 || slotsUsed(pack) != 2 {
		t.Fatalf("pack %v: want the pickaxe and one note stack", pack)
	}
	if b := sim.Bank(); countOf(b, ess) != 1200 || countOf(b, coinsID) != 90_000 {
		t.Fatalf("bank %v", b)
	}
	if bank.NoteMode() {
		t.Error("the bank must be left in item mode")
	}
}

// Asking for more than the bank holds draws what it holds, with Withdraw-All.
func TestDrawNoteStackAllItHolds(t *testing.T) {
	sim, d, bank := stackRig(t, nil, []actionsim.Stack{{ID: ess, Count: 777}})
	res := DrawNoteStack(d, bank, 150_000)
	if res.Drawn != 777 || res.Why != "" || res.Held != 777 {
		t.Fatalf("%+v", res)
	}
	if countOf(sim.Pack(), essNote) != 777 || countOf(sim.Bank(), ess) != 0 || bank.NoteMode() {
		t.Fatalf("pack %v bank %v", sim.Pack(), sim.Bank())
	}
}

// The pack needs one free slot; a bank with no essence has nothing to draw; nothing asked draws nothing. None of
// them touches the bank.
func TestDrawNoteStackRefusals(t *testing.T) {
	full := make([]actionsim.Stack, 28)
	for i := range full {
		full[i] = actionsim.Stack{ID: int32(2000 + i), Count: 1}
	}
	sim, d, bank := stackRig(t, full, []actionsim.Stack{{ID: ess, Count: 500}})
	if res := DrawNoteStack(d, bank, 100); res.Drawn != 0 || !strings.Contains(res.Why, "free slot") {
		t.Errorf("full pack: %+v", res)
	}
	if countOf(sim.Bank(), ess) != 500 {
		t.Errorf("bank %v", sim.Bank())
	}
	_, d, bank = stackRig(t, nil, []actionsim.Stack{{ID: coinsID, Count: 5}})
	if res := DrawNoteStack(d, bank, 100); res.Drawn != 0 || res.Why == "" || res.Held != 0 {
		t.Errorf("no essence: %+v", res)
	}
	_, d, bank = stackRig(t, nil, []actionsim.Stack{{ID: ess, Count: 5}})
	if res := DrawNoteStack(d, bank, 0); res.Drawn != 0 || res.Why == "" {
		t.Errorf("none asked: %+v", res)
	}
}

// A draw adds to a note stack already in the pack (an earlier hand-over that did not finish) and counts only
// what this draw brought.
func TestDrawNoteStackCountsOnlyWhatItDrew(t *testing.T) {
	sim, d, bank := stackRig(t, []actionsim.Stack{{ID: essNote, Count: 40}}, []actionsim.Stack{{ID: ess, Count: 1000}})
	res := DrawNoteStack(d, bank, 300)
	if res.Drawn != 300 || res.Why != "" {
		t.Fatalf("%+v", res)
	}
	if countOf(sim.Pack(), essNote) != 340 {
		t.Fatalf("pack %v", sim.Pack())
	}
}
