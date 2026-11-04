package mastercraftplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/herbplan"
)

// At the goal a training round is passed over for the next one, the rounds in craftPastGoal run on, and a level not yet
// read never holds a round off.
func TestTheSkillGoalPassesOverMaxedRounds(t *testing.T) {
	set := orderSettings()
	set.SkillGoal = 99
	s := readySnapshot(map[string]int{"Maple longbow": 500, "Bow string": 500, "Iron ore": 600, "Coal": 1200,
		"Nature rune": 600, "Fire rune": 2400, HerbName(herbplan.Herbs[0]): 2000})
	s.FletchingLevel, s.FletchingKnown = 99, true
	s.MagicLevel, s.SmithingLevel = 99, 72
	s.HerbloreLevel, s.HerbloreKnown = 67, true

	// Fletching 99: the bow round is passed over; superheat still trains Smithing 72.
	d := Decide(set, s)
	if d.Mode != ModeSuperheat {
		t.Fatalf("got %s (%s), want superheat with Fletching at the goal", d.Mode, d.Why)
	}
	// Smithing 99 too: herbs (Herblore 67) is next; the reason names the held rounds.
	s.SmithingLevel = 99
	d = Decide(set, s)
	if d.Mode != ModeHerbs {
		t.Fatalf("got %s (%s), want herbs", d.Mode, d.Why)
	}
	// Herblore 99: nothing trains, ScarabSwarm, and the log line says why.
	s.HerbloreLevel = 99
	d = Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "bows: Fletching 99 is at the craftSkillGoal 99") {
		t.Fatalf("got %s (%s), want ScarabSwarm with the goal named", d.Mode, d.Why)
	}
	// The owner's exception: bows run past the goal.
	set.PastGoal = []string{"bows"}
	if d := Decide(set, s); d.Mode != ModeBows {
		t.Fatalf("got %s (%s), want bows past the goal", d.Mode, d.Why)
	}
	// A level not read yet never holds a round off.
	set.PastGoal = nil
	s.FletchingKnown = false
	if _, held := AtGoal(set, s, ModeBows); held {
		t.Error("an unread Fletching level held the bow round off")
	}
	// The goal off holds nothing.
	set.SkillGoal = 0
	s.FletchingKnown = true
	if _, held := AtGoal(set, s, ModeBows); held {
		t.Error("craftSkillGoal 0 held the bow round off")
	}
}

func TestSkillGoalSettings(t *testing.T) {
	set, err := DecodeSettings(map[string]interface{}{KeySkillGoal: "99", KeyPastGoal: "bows, arrows"})
	if err != nil || set.SkillGoal != 99 || len(set.PastGoal) != 2 || set.PastGoal[1] != "arrows" {
		t.Fatalf("decoded %d %v, %v", set.SkillGoal, set.PastGoal, err)
	}
	for _, bad := range []map[string]interface{}{{KeySkillGoal: 100}, {KeySkillGoal: -1}, {KeyPastGoal: "nature"}} {
		if _, err := DecodeSettings(bad); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}
