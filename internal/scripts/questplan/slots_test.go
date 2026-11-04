package questplan

import (
	"testing"
	"time"
)

const mn = time.Minute

// A 40 minute quest gets 50 minutes, a 15 minute one keeps the 20 minute base, and an id with no
// estimate (a training rung, a quest LadderFacts does not know) keeps the base.
func TestQuestSlotIsTheLargerOfTheBaseAndTheEstimatePlusAQuarter(t *testing.T) {
	cases := []struct {
		id   string
		want time.Duration
	}{
		{"waterfall", 50 * mn},              // 40
		{"pip", 50 * mn},                    // 40
		{"vampire", 37*mn + 30*time.Second}, // 30 -> 37m30s
		{"murder", 25 * mn},                 // 20 -> 25
		{"hetty", 31*mn + 15*time.Second},   // 25 -> 31m15s
		{"runemysteries", 20 * mn},          // 10
		{"cows20", 20 * mn},                 // training rung, no estimate
		{"nosuchquest", 20 * mn},
		{"dragon", 150 * mn}, // 120
	}
	for _, c := range cases {
		if got := QuestSlot(c.id, 20*mn); got != c.want {
			t.Errorf("QuestSlot(%q) = %v, want %v", c.id, got, c.want)
		}
	}
	// The base is the floor whatever it is set to.
	if got := QuestSlot("waterfall", 90*mn); got != 90*mn {
		t.Errorf("a 90 minute base must stay: got %v", got)
	}
	if got := QuestSlot("waterfall", 0); got != 50*mn {
		t.Errorf("with no base the estimate rules: got %v", got)
	}
}

// The live settings (budget 45, base 20) cannot hold a 50 minute slot, so the budget is raised to
// the longest slot and no further; a budget already large enough is left alone.
func TestLadderBudgetIsRaisedOnlyToTheLongestSlot(t *testing.T) {
	ids := []string{"waterfall", "runemysteries", "pip", "hetty", "vampire", "murder"}
	eff, raised := LadderBudget(ids, 20*mn, 45*mn)
	if eff != 50*mn || !raised {
		t.Errorf("budget 45 with a 50 minute slot: got %v raised=%v, want 50m true", eff, raised)
	}
	eff, raised = LadderBudget(ids, 20*mn, 360*mn)
	if eff != 360*mn || raised {
		t.Errorf("a budget that already fits must stay: got %v raised=%v", eff, raised)
	}
	eff, raised = LadderBudget([]string{"hetty", "murder"}, 20*mn, 45*mn)
	if eff != 45*mn || raised {
		t.Errorf("no slot over the budget: got %v raised=%v", eff, raised)
	}
	if eff, raised = LadderBudget(nil, 20*mn, 45*mn); eff != 45*mn || raised {
		t.Errorf("an empty ladder keeps the budget: got %v raised=%v", eff, raised)
	}
}

// A quest that fits runs its whole slot, one that fits half runs what is left and is marked
// clipped, and one that fits less than half is not started.
func TestLadderSlotClipsOnlyDownToHalf(t *testing.T) {
	cases := []struct {
		name       string
		slot, left time.Duration
		run        time.Duration
		clipped    bool
		ok         bool
	}{
		{"fits", 50 * mn, 50 * mn, 50 * mn, false, true},
		{"plenty left", 25 * mn, 50 * mn, 25 * mn, false, true},
		{"a hair short of the budget it is the size of", 50 * mn, 50*mn - 200*time.Millisecond, 50*mn - 200*time.Millisecond, false, true},
		{"29 seconds short is still not a cut", 50 * mn, 50*mn - 29*time.Second, 50*mn - 29*time.Second, false, true},
		{"a minute short is a cut", 50 * mn, 49 * mn, 49 * mn, true, true},
		{"just over half", 50 * mn, 26 * mn, 26 * mn, true, true},
		{"exactly half", 50 * mn, 25 * mn, 25 * mn, true, true},
		{"under half", 50 * mn, 24 * mn, 0, true, false},
		{"the old behaviour ran a 6 minute stub of a 20 minute slot", 20 * mn, 6 * mn, 0, true, false},
		{"nothing left", 20 * mn, 0, 0, false, false},
		{"no slot", 0, 10 * mn, 0, false, false},
	}
	for _, c := range cases {
		run, clipped, ok := LadderSlot(c.slot, c.left)
		if run != c.run || clipped != c.clipped || ok != c.ok {
			t.Errorf("%s: LadderSlot(%v,%v) = %v,%v,%v want %v,%v,%v", c.name, c.slot, c.left, run, clipped, ok, c.run, c.clipped, c.ok)
		}
	}
}

// Every rung of the owner's ladder that has an estimate fits the budget the live settings raise
// to (nothing but Dragon Slayer and Fremennik, which are not proven, needs more).
func TestEveryProvenRungFitsTheRaisedBudget(t *testing.T) {
	proven := []string{"waterfall", "runemysteries", "pip", "hetty", "vampire", "murder"}
	eff, _ := LadderBudget(proven, 20*mn, 45*mn)
	for _, id := range proven {
		if s := QuestSlot(id, 20*mn); s > eff {
			t.Errorf("%s: slot %v is over the effective budget %v", id, s, eff)
		}
	}
}
