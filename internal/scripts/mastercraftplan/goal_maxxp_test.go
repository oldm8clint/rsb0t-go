package mastercraftplan

import "testing"

// a user request: the bows round runs past Fletching 99 (craftPastGoal) but stops at 200M experience.
func TestBowsStopAtMaxFletchingXP(t *testing.T) {
	set := Settings{SkillGoal: 99, PastGoal: []string{string(ModeBows)}}
	s := Snapshot{FletchingLevel: 99, FletchingKnown: true, FletchingXP: MaxSkillXP - 1}
	if _, held := AtGoal(set, s, ModeBows); held {
		t.Fatal("held one experience short of the cap")
	}
	s.FletchingXP = MaxSkillXP
	if why, held := AtGoal(set, s, ModeBows); !held {
		t.Fatal("not held at the cap")
	} else if why == "" {
		t.Fatal("no reason")
	}
	// With the goal off the cap still holds it: nothing more can be earned.
	if _, held := AtGoal(Settings{}, s, ModeBows); !held {
		t.Fatal("not held at the cap with the goal off")
	}
	// The arrows and the pearl bolt tips run past 99 too (craftPastGoal) and stop at the same cap.
	set.PastGoal = []string{string(ModeBows), string(ModeArrows), string(ModePearls)}
	s.CraftingLevel = 95
	for _, m := range []Mode{ModeArrows, ModePearls} {
		if _, held := AtGoal(set, s, m); !held {
			t.Fatalf("%s not held at the Fletching cap", m)
		}
		s.FletchingXP = MaxSkillXP - 1
		if _, held := AtGoal(set, s, m); held {
			t.Fatalf("%s held under the cap although craftPastGoal names it", m)
		}
		s.FletchingXP = MaxSkillXP
	}
}
