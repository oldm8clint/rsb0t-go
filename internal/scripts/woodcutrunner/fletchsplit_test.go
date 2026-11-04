package woodcutrunner

import (
	"testing"

	"rsb0t-go/internal/loot"
)

// The split users asked for, asserted through the REAL skip filter rather than a
// restatement of it (2026-09-21: "arrow shafts are showing up in the woodcutting section
// probably should have a fletching section for all fletching materials the fleet makes").
//
// The measurement is driven with its own pack closure instead of a live session, which is what
// loot.NewWindow's signature is for: everything under test here — notAChop and loot.Window's
// reporting rule — is the same code the live camp runs.

// packAt is a Window pack reader over a map the test mutates, copied on every read the way a
// real pack snapshot is.
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

// TestAChoppedLogRecordsAsWoodcuttingAndAShaftDoesNot reproduces the live failure and fixes it in
// one test.
//
// The failure: ProgressiveWoodcutter's Draynor camp fletches, bankfletchrunner.Fletch returns as
// soon as the FIRST shaft lands while the make batch runs on for another twenty-odd, the camp
// goes back to chopping, and the remaining shafts arrive inside a window whose source is the
// tree. To the measurement they arrived while the account was chopping — which is exactly what
// the simulated pack below does.
func TestAChoppedLogRecordsAsWoodcuttingAndAShaftDoesNot(t *testing.T) {
	pack := loot.Counts{}
	var drops []loot.Drop
	w := loot.NewWindow(func(d loot.Drop) { drops = append(drops, d) }, nil, packAt(pack),
		loot.Woodcutting(nil, 1276, "Tree"), notAChop(&Deps{}))
	if w == nil {
		t.Fatal("NewWindow returned nil with a real sink and pack")
	}

	// Two logs chopped...
	pack[objLogs] = 2
	w.Got(2)
	// ...and thirty arrow shafts from a fletching batch that was still running when this window
	// opened. This is the live bug, reproduced.
	pack[objArrowShaft] = 30
	w.Flush()

	if len(drops) != 1 {
		t.Fatalf("got %d drops, want 1", len(drops))
	}
	got := drops[0]
	if got.Source.Skill != loot.SkillWoodcutting {
		t.Fatalf("source skill = %q, want %q", got.Source.Skill, loot.SkillWoodcutting)
	}
	if got.Events != 2 {
		t.Errorf("events = %d, want the 2 logs chopped", got.Events)
	}
	var logs, shafts int64
	for _, it := range got.Items {
		switch it.ID {
		case objLogs:
			logs = it.Count
		case objArrowShaft:
			shafts = it.Count
		}
	}
	if logs != 2 {
		t.Errorf("logs on the Woodcutting card = %d, want 2 — woodcutting DID produce these", logs)
	}
	if shafts != 0 {
		t.Fatalf("%d arrow shafts landed on the Woodcutting card; fletching produced them, not the tree", shafts)
	}
}

// TestTheWoodcuttingFilterStillKeepsWhatItAlwaysKept: the shaft rule is an addition, so the
// filter's existing judgements must be untouched. A nil Config is deliberate — these are the
// id-only branches, which are the ones that still work on an account whose cache never loaded.
func TestTheWoodcuttingFilterStillKeepsWhatItAlwaysKept(t *testing.T) {
	skip := notAChop(&Deps{})
	for _, id := range []int{objArrowShaft, woodcutCoins, loot.ObjCasket} {
		if !skip(id) {
			t.Errorf("obj %d should be kept off the Woodcutting card", id)
		}
	}
	// Logs are the whole point of the card and must never be filtered.
	for _, id := range []int{objLogs, 1521, 1519, 1517} {
		if skip(id) {
			t.Errorf("obj %d is a log; woodcutting produced it and it belongs on the card", id)
		}
	}
}
