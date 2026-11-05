package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// The executors' paths that need no map and no npc: the early returns that must not send an account anywhere, the refusals that say why, and
// the item-on-item rungs of the pie on the simulated pack.

func TestSquireCombineMakesTheShellAndTheUncookedPie(t *testing.T) {
	// The dough on the dish makes the shell; the berries on the shell make the uncooked pie; each takes one of each and gives one.
	w := newExecWorld(actionsim.Stack{ID: questplan.ObjSqPastryDough, Count: 1}, actionsim.Stack{ID: questplan.ObjSqPieDish, Count: 1},
		actionsim.Stack{ID: questplan.ObjSqRedberries, Count: 1})
	w.sim.Combine(questplan.ObjSqPastryDough, questplan.ObjSqPieDish, questplan.ObjSqPieShell)
	w.sim.Combine(questplan.ObjSqRedberries, questplan.ObjSqPieShell, questplan.ObjSqUncookedPie)
	shell := questplan.Step{Custom: questplan.CustomSquireShell, ItemID: questplan.ObjSqPastryDough, TargetID: questplan.ObjSqPieDish, ExpectItemID: questplan.ObjSqPieShell}
	if !squireCombine(w.e, shell) || w.held(questplan.ObjSqPieShell) != 1 || w.held(questplan.ObjSqPastryDough) != 0 || w.held(questplan.ObjSqPieDish) != 0 {
		t.Fatalf("the shell: pack %+v, log %v", w.sim.Pack(), *w.log)
	}
	// Made already: nothing is spent.
	if !squireCombine(w.e, shell) || w.held(questplan.ObjSqPieShell) != 1 {
		t.Errorf("a second shell step disturbed the pack: %+v", w.sim.Pack())
	}
	berries := questplan.Step{Custom: questplan.CustomSquireBerries, ItemID: questplan.ObjSqRedberries, TargetID: questplan.ObjSqPieShell, ExpectItemID: questplan.ObjSqUncookedPie}
	if !squireCombine(w.e, berries) || w.held(questplan.ObjSqUncookedPie) != 1 || w.held(questplan.ObjSqPieShell) != 0 || w.held(questplan.ObjSqRedberries) != 0 {
		t.Fatalf("the uncooked pie: pack %+v, log %v", w.sim.Pack(), *w.log)
	}
}

func TestSquireCombineRefusesAndSaysWhy(t *testing.T) {
	// A step that names no item, target or product is a bug in the plan, and says so.
	w := newExecWorld()
	if squireCombine(w.e, questplan.Step{Custom: questplan.CustomSquireShell}) || !strings.Contains(w.logged(), "no item, target or product") {
		t.Errorf("an incomplete step: %v", *w.log)
	}
	// An item the pack lacks: the use is refused before anything is sent.
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjSqPieDish, Count: 1})
	if squireCombine(w.e, questplan.Step{Custom: questplan.CustomSquireShell, ItemID: questplan.ObjSqPastryDough, TargetID: questplan.ObjSqPieDish, ExpectItemID: questplan.ObjSqPieShell}) {
		t.Errorf("a shell without dough was made")
	}
	if !strings.Contains(w.logged(), "is not in the pack") {
		t.Errorf("the log lacks the refusal: %v", *w.log)
	}
	// A use that makes nothing (the rule is not scripted, as a refused berry at Cooking 9 is) fails after its tries and says what it made nothing of.
	was := squireCombineWait
	squireCombineWait = 20 * time.Millisecond
	defer func() { squireCombineWait = was }()
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjSqRedberries, Count: 1}, actionsim.Stack{ID: questplan.ObjSqPieShell, Count: 1})
	if squireCombine(w.e, questplan.Step{Custom: questplan.CustomSquireBerries, ItemID: questplan.ObjSqRedberries, TargetID: questplan.ObjSqPieShell, ExpectItemID: questplan.ObjSqUncookedPie}) {
		t.Errorf("an unscripted use succeeded")
	}
	if !strings.Contains(w.logged(), "made no") || w.held(questplan.ObjSqRedberries) != 1 || w.held(questplan.ObjSqPieShell) != 1 {
		t.Errorf("the pack %+v or the log lack the refusal: %v", w.sim.Pack(), *w.log)
	}
}

func TestSquireDoughMakesTheDoughAndFinishesAtOnceWithItInThePack(t *testing.T) {
	w := newExecWorld(actionsim.Stack{ID: questplan.ObjSqPotOfFlour, Count: 1}, actionsim.Stack{ID: questplan.ObjSqBucketWater, Count: 1})
	w.sim.Combine(questplan.ObjSqPotOfFlour, questplan.ObjSqBucketWater, questplan.ObjSqPastryDough)
	if !squireDough(w.e, questplan.Step{}) || w.held(questplan.ObjSqPastryDough) != 1 {
		t.Fatalf("no dough: pack %+v, log %v", w.sim.Pack(), *w.log)
	}
	// With dough in the pack nothing is used.
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjSqPastryDough, Count: 1})
	if !squireDough(w.e, questplan.Step{}) {
		t.Errorf("an account holding dough was refused")
	}
	// No flour: the refusal is said.
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjSqBucketWater, Count: 1})
	if squireDough(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "is not in the pack") {
		t.Errorf("flourless dough: %v", *w.log)
	}
}

func TestSquireUnwieldTakesTheSwordOff(t *testing.T) {
	w := newExecWorld()
	w.sim.SetWearPos(questplan.ObjSqSword, actionsim.RightHand)
	w.sim.Wear(actionsim.RightHand, questplan.ObjSqSword)
	if !squireUnwield(w.e, questplan.Step{}) || w.held(questplan.ObjSqSword) != 1 {
		t.Fatalf("the sword is still worn: pack %+v worn %+v, log %v", w.sim.Pack(), w.sim.Worn(), *w.log)
	}
	if len(w.sim.Worn()) != 0 {
		t.Errorf("worn %+v", w.sim.Worn())
	}
	// Not worn: nothing to do.
	if !squireUnwield(w.e, questplan.Step{}) {
		t.Errorf("a pack-held sword was refused")
	}
}

func TestSquireExecutorsFinishAtOnceWithTheirProductInThePack(t *testing.T) {
	for _, c := range []struct {
		name string
		fn   func(w *execWorld) bool
		have int
	}{
		{"the bucket of water", func(w *execWorld) bool { return squireFill(w.e, questplan.Step{}) }, questplan.ObjSqBucketWater},
		{"the cooked pie", func(w *execWorld) bool { return squireCook(w.e, questplan.Step{}) }, questplan.ObjSqRedberryPie},
		{"the burnt pie", func(w *execWorld) bool { return squireCook(w.e, questplan.Step{}) }, questplan.ObjSqBurntPie},
		{"the portrait", func(w *execWorld) bool { return squirePortrait(w.e, questplan.Step{}) }, questplan.ObjSqPortrait},
		{"the ore", func(w *execWorld) bool { return squireBlurite(w.e, questplan.Step{}) }, questplan.ObjSqBluriteOre},
	} {
		w := newExecWorld(actionsim.Stack{ID: int32(c.have), Count: 1})
		if !c.fn(w) {
			t.Errorf("%s: an executor with its product in the pack failed: %v", c.name, *w.log)
		}
	}
	// An emptied dish needs no burnt pie.
	w := newExecWorld()
	if !squireEmpty(w.e, questplan.Step{}) {
		t.Errorf("emptying nothing failed")
	}
}

func TestSquireExecutorsRefuseAndSayWhy(t *testing.T) {
	for _, c := range []struct {
		name string
		pack []actionsim.Stack
		fn   func(w *execWorld) bool
		say  string
	}{
		{"filling with no bucket", nil, func(w *execWorld) bool { return squireFill(w.e, questplan.Step{}) }, "no bucket in the pack"},
		{"cooking with no pie", nil, func(w *execWorld) bool { return squireCook(w.e, questplan.Step{}) }, "no uncooked pie"},
		// An account with nothing to eat is refused before it is walked anywhere near the ladder.
		{"the dungeon with no food", []actionsim.Stack{{ID: questplan.ObjBronzePickaxe, Count: 1}}, func(w *execWorld) bool { return squireBlurite(w.e, questplan.Step{}) },
			"hitpoints of food in the pack after the top-up"},
		{"the dungeon with no pickaxe", []actionsim.Stack{{ID: questplan.ObjShark, Count: 14}}, func(w *execWorld) bool { return squireBlurite(w.e, questplan.Step{}) },
			"no pickaxe in the pack"},
	} {
		w := newExecWorld(c.pack...)
		w.stand(questplan.SqThurgoAnchor)
		if c.fn(w) {
			t.Errorf("%s: succeeded", c.name)
		}
		if !strings.Contains(w.logged(), c.say) {
			t.Errorf("%s: the log lacks %q: %v", c.name, c.say, *w.log)
		}
	}
}

// The tiles are ordered by their distance from the account.
func TestSquireNearestOrdersByDistance(t *testing.T) {
	got := squireNearest(tile.Tile{X: 2970, Z: 3200}, questplan.SqWaterSpots, func(w questplan.SqWaterSpot) tile.Tile { return w.Loc })
	if len(got) != len(questplan.SqWaterSpots) || got[0].Name != "Rimmington's sink" || got[len(got)-1].Name != "the Varrock palace kitchen's sink" {
		t.Errorf("from Rimmington the water is ordered %v", got)
	}
	// The table itself is untouched.
	if questplan.SqWaterSpots[0].Name != "Rimmington's sink" {
		t.Errorf("the table was reordered")
	}
}
