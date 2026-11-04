package questplan

import (
	"math"
	"strings"
	"testing"
	"time"
)

func gtSnap(stage, prayer int) Snapshot {
	s := Snapshot{
		Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
		FreeSlots: 20, Failed: map[string]bool{}, Ran: map[string]bool{}, Blocked: map[string]bool{},
		Levels: map[string]int{"prayer": prayer, "magic": 50, "attack": 60, "defence": 50, "hitpoints": 44},
	}
	return s
}

// The gate states what the demon is and what this account has, in one line fit for a log.
func TestGTDemonReadyNamesTheDemonAndTheAccount(t *testing.T) {
	v := GTDemonReady(gtSnap(120, 37))
	if v.OK {
		t.Fatalf("Prayer 37 passed: %+v", v)
	}
	for _, want := range []string{"Black Demon", "157 hitpoints", "Protect from Melee", "Prayer 43", "has 37"} {
		if !strings.Contains(v.Why, want) {
			t.Errorf("the reason does not say %q: %s", want, v.Why)
		}
	}
	if strings.Contains(v.Why, "\n") {
		t.Errorf("the reason is not one line: %q", v.Why)
	}
}

// With Protect from Melee but no code that holds it, the quest still waits, and says that is why.
func TestGTDemonReadyWaitsForTheFightThatIsNotBuilt(t *testing.T) {
	if GTDemonFightBuilt {
		t.Skip("the Protect from Melee fight is built: this test is about the day it was not")
	}
	v := GTDemonReady(gtSnap(120, 60))
	if v.OK || !strings.Contains(v.Why, "does not do yet") || !strings.Contains(v.Why, "Prayer 60") {
		t.Fatalf("%+v", v)
	}
	// An unread level is Prayer 0, never a pass.
	if v := GTDemonReady(Snapshot{}); v.OK {
		t.Fatalf("an unread snapshot passed: %+v", v)
	}
}

// Every stage below the demon waits for a day behind the gate, from not started to the trapdoor,
// and none of them is a step that walks anywhere.
func TestGrandTreeSkipsForADayBelowTheDemon(t *testing.T) {
	stages := []int{0, GTStarted, GTHazelmere, GTRelayed, GTGlough, GTPrisoner, GTSpokenChar, GTJournal, GTReleased,
		GTLumber, GTClue, GTPlans, GTTwigs, GTTrapdoor}
	for _, st := range stages {
		s := gtSnap(st, 37)
		if st == 0 {
			s.Journal = StatusNotStarted
		}
		step := DecideGrandTree(s)
		if step.Kind != StepWait || step.ParkFor != 24*time.Hour || !strings.Contains(step.Reason, "Black Demon") {
			t.Errorf("stage %d: %s (park %s) %q", st, step.Describe(), step.ParkFor, step.Reason)
		}
		if why := ValidateStep(step); why != "" {
			t.Errorf("stage %d malformed: %s", st, why)
		}
	}
}

// Past the demon the quest goes on whatever the account's Prayer: stage 140 on has no demon in the
// caves (only stage 130 replays the cutscene), and a finished quest is done.
func TestGrandTreeGoesOnPastTheDemon(t *testing.T) {
	for _, st := range []int{GTDemon, GTDaconia} {
		step := DecideGrandTree(gtSnap(st, 1))
		if step.Kind == StepWait {
			t.Errorf("stage %d waits: %q", st, step.Reason)
		}
	}
	done := gtSnap(0, 1)
	done.Journal = StatusComplete
	if step := DecideGrandTree(done); step.Kind != StepDone {
		t.Errorf("complete: %s", step.Describe())
	}
}

// One death at the demon skips the quest for a week.
func TestGrandTreeDemonStepHasADeathCap(t *testing.T) {
	c, ok := GrandTree.StepDeathCaps[CustomGTDemon]
	if !ok || c.Max != 1 || c.Window != 7*24*time.Hour || c.Skip != 7*24*time.Hour {
		t.Fatalf("%+v %v", c, ok)
	}
}

// The demon against the accounts that died to it, by the content's own arithmetic. These are the
// numbers the file comment quotes: they must not drift from the model.
func TestTheBlackDemonCannotBeFoughtByHand(t *testing.T) {
	demon := MeleeFoe{Name: "Black Demon", Attack: 145, Strength: 148, Defence: 152, Magic: 1, Hitpoints: GTDemonHitpoints, AttackTicks: 4}
	if got := npcMaxHit(demon.Strength); got != GTDemonMaxHit {
		t.Fatalf("maximum hit %d, want %d", got, GTDemonMaxHit)
	}
	// Defence 40, no armour: 84% of its swings land.
	x := MeleeExchange(MeleeFighter{Attack: 55, Strength: 55, Defence: 40, Magic: 50}, demon)
	if hit := rollHit((demon.Attack+9)*64, (40+8)*64); math.Abs(hit-0.844) > 0.005 {
		t.Errorf("its hit chance against Defence 40 is %.3f, the comment says 84%%", hit)
	}
	t.Logf("Attack 55 Strength 55 Defence 40: kill %.0f ticks (%.1f minutes), loss %.0f hitpoints", x.KillTicks, x.KillTicks*0.6/60, x.DamageTaken)
	if x.KillTicks*0.6/60 < 9 || x.DamageTaken < 1000 {
		t.Errorf("melee should be hopeless: %.1f minutes, %.0f hitpoints lost", x.KillTicks*0.6/60, x.DamageTaken)
	}
}
