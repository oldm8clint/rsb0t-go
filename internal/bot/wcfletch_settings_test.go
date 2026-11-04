package bot

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/wcfletchrunner"
)

func TestWCFletchSettingsDefaults(t *testing.T) {
	got := wcfletchSettings(map[string]interface{}{}).Normalised()
	if !got.Fletch {
		t.Error("fletch should default ON — a ProgressiveWCFletch that does not fletch is its sibling")
	}
	if got.NormalLogs != wcfletchplan.NormalShafts {
		t.Errorf("normalLogs = %q, want %q (the fleet owner's own instruction)", got.NormalLogs, wcfletchplan.NormalShafts)
	}
	if got.TreeGate != wcfletchplan.GateStartAt {
		t.Errorf("treeGate = %q, want %q so both scripts chop in the same places", got.TreeGate, wcfletchplan.GateStartAt)
	}
	if got.MuleAt != wcfletchplan.DefaultMuleAt {
		t.Errorf("muleAtFletched = %d, want %d", got.MuleAt, wcfletchplan.DefaultMuleAt)
	}
	if !got.SpreadRungOne {
		t.Error("spreadRungOne should default ON — with it off, every rung-1 account queues at the one 12-tree Seers camp")
	}
	if !got.SpreadMaple {
		t.Error("spreadMaple should default ON — with it off, every maple-rung account queues at the one four-tree Seers maple camp")
	}
	if !got.MapleOverflow {
		t.Error("mapleOverflow should default ON — the maple camps together feed about a third of the cohort")
	}
	if !got.SpreadYew {
		t.Error("spreadYew should default ON — with it off, every yew-rung account queues at the church camp's three yews, which gave 23 accounts 45 bows an account an hour")
	}
	if !got.ChopManip {
		t.Error("chopManip should default ON for ProgressiveWCFletch — the lab measured x1.36 logs per 100 ticks")
	}
}

func TestWCFletchSettingsOverrides(t *testing.T) {
	got := wcfletchSettings(map[string]interface{}{
		"fletch":         false,
		"normalLogs":     "bows",
		"treeGate":       "content",
		"muleAtFletched": 250,
		"spreadRungOne":  false,
		"spreadMaple":    false,
		"mapleOverflow":  false,
		"spreadYew":      false,
		"chopManip":      false,
	}).Normalised()
	if got.Fletch {
		t.Error("fletch=false was not applied")
	}
	if got.NormalLogs != wcfletchplan.NormalBows {
		t.Errorf("normalLogs = %q, want %q", got.NormalLogs, wcfletchplan.NormalBows)
	}
	if got.TreeGate != wcfletchplan.GateContent {
		t.Errorf("treeGate = %q, want %q", got.TreeGate, wcfletchplan.GateContent)
	}
	if got.MuleAt != 250 {
		t.Errorf("muleAtFletched = %d, want 250", got.MuleAt)
	}
	if got.SpreadRungOne {
		t.Error("spreadRungOne=false was not applied")
	}
	if got.SpreadMaple || got.MapleOverflow {
		t.Errorf("spreadMaple=false / mapleOverflow=false were not applied: %v / %v", got.SpreadMaple, got.MapleOverflow)
	}
	if got.SpreadYew {
		t.Error("spreadYew=false was not applied")
	}
	if got.ChopManip {
		t.Error("chopManip=false was not applied")
	}
}

// TestWCFletchIsRegistered: the script has to be reachable by name from fleet.config.json, which
// is the ONLY way an operator can put an account on it.
func TestWCFletchIsRegistered(t *testing.T) {
	b, ok := New("ProgressiveWCFletch")
	if !ok || b == nil {
		t.Fatal(`Register("ProgressiveWCFletch") did not take — no account can be put on this script`)
	}
	if _, isFletch := b.(*wcfletchBot); !isFletch {
		t.Fatalf("ProgressiveWCFletch resolved to %T", b)
	}
	// And its sibling must still be there, unchanged: users want both running.
	if w, ok := New("ProgressiveWoodcutter"); !ok {
		t.Error("ProgressiveWoodcutter is no longer registered")
	} else if _, isWood := w.(*woodcutterBot); !isWood {
		t.Errorf("ProgressiveWoodcutter resolved to %T", w)
	}
}

// TestWCFletchFeaturesFletching pins the skill the accounts panel shows for this script. An
// entry missing here is what made twelve live woodcutters show no XP at all on 2026-09-21.
func TestWCFletchFeaturesFletching(t *testing.T) {
	got, ok := PrimarySkill("ProgressiveWCFletch")
	if !ok || got != "Fletching" {
		t.Errorf("PrimarySkill(ProgressiveWCFletch) = %q ok=%v, want %q", got, ok, "Fletching")
	}
}

// TestFletchingStatIsFletching pins wcfletchrunner's own stat index against the protocol table,
// rather than leaving 9 as a number nobody checked. Getting it wrong would read some other
// skill's level and drive the whole ladder off it — silently, because every skill reports a
// plausible number.
func TestFletchingStatIsFletching(t *testing.T) {
	if got := gamestate.SkillName(wcfletchrunner.FletchingStat); got != "Fletching" {
		t.Errorf("stat %d is %q, want Fletching", wcfletchrunner.FletchingStat, got)
	}
}
