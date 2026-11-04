package bankfletchrunner

import (
	"testing"

	"rsb0t-go/internal/loot"
)

// The other half of the split asserted in woodcutrunner/fletchsplit_test.go: a shaft that a
// batch made has to land SOMEWHERE, and that somewhere is the Fletching card. Keeping it off the
// Woodcutting card and then dropping it on the floor would be a different bug with the same
// screenshot.
//
// Driven with its own pack closure rather than a live session — loot.NewWindow takes one for
// exactly this reason — so the code under test is the real notAFletch and the real
// loot.Window reporting rule.

func packAt(p loot.Counts) func() loot.Counts {
	return func() loot.Counts {
		cp := loot.Counts{}
		for id, n := range p {
			cp[id] = n
		}
		return cp
	}
}

const (
	objLogs       = 1511
	objArrowShaft = loot.ObjArrowShaft
)

// TestAFletchedShaftRecordsAsFletching, keyed on the MATERIAL, because that is the row the card
// breaks down by — the same way a tree is the row on the Woodcutting card.
func TestAFletchedShaftRecordsAsFletching(t *testing.T) {
	// A pack that already holds the knife and a batch of logs when the window opens, so the
	// baseline is the real one: only what the batch ADDS may be reported.
	pack := loot.Counts{objLogs: 28}
	var drops []loot.Drop
	w := loot.NewWindow(func(d loot.Drop) { drops = append(drops, d) }, nil, packAt(pack),
		loot.Fletching(nil, objLogs, "Logs"), notAFletch(&Deps{}))
	if w == nil {
		t.Fatal("NewWindow returned nil with a real sink and pack")
	}

	// The batch runs: the logs go, the shafts arrive. 28 logs x 15 = 420 shafts.
	delete(pack, objLogs)
	pack[objArrowShaft] = 420
	// Events are the 28 MATERIALS CONSUMED, not the 420 products, because the panel prices this
	// card's cost as events x the price of a log (internal/loot/cost.go).
	w.Got(28)
	w.Flush()

	if len(drops) != 1 {
		t.Fatalf("got %d drops, want 1", len(drops))
	}
	got := drops[0]
	if got.Source.Skill != loot.SkillFletching || got.Source.Kind != loot.KindProduction {
		t.Fatalf("source = %+v, want a Fletching production source", got.Source)
	}
	if got.Source.Name != "Logs" {
		t.Errorf("source name = %q, want the material the batch consumed", got.Source.Name)
	}
	if got.Events != 28 {
		t.Errorf("events = %d, want the 28 logs cut (loot.ActionFletch counts materials consumed)", got.Events)
	}
	// And the cost model has to be able to price exactly that, or the netting users asked for
	// multiplies the wrong number.
	cost, ok := got.Source.Consumes()
	if !ok || cost.Name != "Logs" || cost.Per != 1 {
		t.Fatalf("Consumes() = %+v, %v; want one Logs per event", cost, ok)
	}
	var shafts int64
	for _, it := range got.Items {
		if it.ID == objArrowShaft {
			shafts = it.Count
		}
		if it.ID == objLogs {
			t.Errorf("the logs CONSUMED were reported as a gain of %d", it.Count)
		}
	}
	if shafts != 420 {
		t.Fatalf("arrow shafts on the Fletching card = %d, want 420", shafts)
	}
}

// TestAnInterruptedBatchIsNotAttributedToFletching. A random event that fires mid-batch drops its
// reward into the same pack this window is measuring; loot.Window's taint rule is what stops a
// Strange box being filed under Fletching, and the whole window is discarded rather than
// part-reported. This asserts the rule is actually reachable from here.
func TestAnInterruptedBatchIsNotAttributedToFletching(t *testing.T) {
	pack := loot.Counts{objLogs: 28}
	var drops []loot.Drop
	w := loot.NewWindow(func(d loot.Drop) { drops = append(drops, d) }, nil, packAt(pack),
		loot.Fletching(nil, objLogs, "Logs"), notAFletch(&Deps{}))

	pack[objArrowShaft] = 60
	w.Got(4)
	w.Taint()
	w.Flush()

	if len(drops) != 0 {
		t.Fatalf("a tainted window reported %d drops; it must report none", len(drops))
	}
}

// TestTheFletchingFilterKeepsOutWhatABatchCannotHaveMade. A nil Config is deliberate: these are
// the id-only branches, the ones that still work on an account whose cache never loaded.
func TestTheFletchingFilterKeepsOutWhatABatchCannotHaveMade(t *testing.T) {
	skip := notAFletch(&Deps{})
	for _, id := range []int{fletchCoins, loot.ObjCasket} {
		if !skip(id) {
			t.Errorf("obj %d should be kept off the Fletching card", id)
		}
	}
	// The product itself must never be filtered — it is the entire content of the card.
	if skip(objArrowShaft) {
		t.Error("the arrow shaft is what fletching made; it must be reported")
	}
}
