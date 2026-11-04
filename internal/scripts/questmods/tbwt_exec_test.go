package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/webwalk"
)

// The executors' paths that need no map and no npc: the early returns that must not send an account anywhere, the refusals that must
// say why, and the glue's two rules that are not in the plan (the walker's rectangles, the hunt's queue).

func tbFullPack() []actionsim.Stack {
	var full []actionsim.Stack
	for i := 0; i < 28; i++ {
		full = append(full, actionsim.Stack{ID: int32(1000 + i), Count: 1})
	}
	return full
}

func tbLog(w *execWorld) string { return strings.Join(*w.log, "\n") }

func TestTbwtExecutorsFinishAtOnceWithTheirProductInThePack(t *testing.T) {
	for _, c := range []struct {
		name string
		fn   func(w *execWorld) bool
		have int
	}{
		{"Karambwanji", func(w *execWorld) bool { return tbFish(w.e, questplan.Step{Qty: 1}) }, questplan.TbObjRawKarambwanji},
		{"a banana", func(w *execWorld) bool { return tbBanana(w.e, questplan.Step{}) }, questplan.ObjBanana},
		{"a sliced banana", func(w *execWorld) bool { return tbBanana(w.e, questplan.Step{}) }, questplan.TbObjSlicedBanana},
		{"the rum", func(w *execWorld) bool { return tbRum(w.e, questplan.Step{}) }, questplan.ObjKaramjaRum},
		{"the burnt bones", func(w *execWorld) bool { return tbBurn(w.e, questplan.Step{}) }, questplan.TbObjBurntJogreBones},
		{"the skin", func(w *execWorld) bool { return tbSkin(w.e, questplan.Step{}) }, questplan.TbObjMonkeySkin},
	} {
		w := newExecWorld(actionsim.Stack{ID: int32(c.have), Count: 2})
		if !c.fn(w) {
			t.Errorf("%s: an executor with its product in the pack failed: %v", c.name, *w.log)
		}
	}
	// A cook with nothing to cook is done; so is a walk to where the account stands.
	w := newExecWorld()
	if !tbCook(w.e, questplan.Step{Qty: questplan.TbObjRawKarambwan}) {
		t.Errorf("a cook of an item the pack does not hold failed: %v", *w.log)
	}
	w.sim.StandAt(questplan.TbTamayuAnchor.X, questplan.TbTamayuAnchor.Z)
	if !tbGo(w.e.D, questplan.TbTamayuAnchor, 3) {
		t.Error("a walk to where the account stands failed")
	}
}

func TestTbwtExecutorsRefuseAndSayWhy(t *testing.T) {
	for _, c := range []struct {
		name string
		pack []actionsim.Stack
		fn   func(w *execWorld) bool
		say  string
	}{
		{"fishing with no net", nil, func(w *execWorld) bool { return tbFish(w.e, questplan.Step{Qty: 10}) }, "no small fishing net"},
		{"a banana with no room", tbFullPack(), func(w *execWorld) bool { return tbBanana(w.e, questplan.Step{}) }, "no room for a banana"},
		{"a rum with no coins", []actionsim.Stack{{ID: questplan.CoinsID, Count: 3}}, func(w *execWorld) bool { return tbRum(w.e, questplan.Step{}) }, "coins in the pack"},
		{"burning with no bones", nil, func(w *execWorld) bool { return tbBurn(w.e, questplan.Step{}) }, "no Jogre bones"},
		{"skinning with no room", tbFullPack(), func(w *execWorld) bool { return tbSkin(w.e, questplan.Step{}) }, "no room for the skin"},
		{"skinning with no corpse", nil, func(w *execWorld) bool { return tbSkin(w.e, questplan.Step{}) }, "to give"},
		{"a spear that does not exist", nil, func(w *execWorld) bool { return tbPoison(w.e, questplan.Step{Qty: 4}) }, "no spear with the id 4"},
	} {
		w := newExecWorld(c.pack...)
		if c.fn(w) {
			t.Errorf("%s: succeeded", c.name)
		}
		if !strings.Contains(tbLog(w), c.say) {
			t.Errorf("%s: the log lacks %q: %v", c.name, c.say, *w.log)
		}
	}
}

// The island's rectangles reach the walker as its own type, one for each hunter, on the ground level, in the plan's order.
func TestTbwtAvoidRectsAreTheWalkersOwn(t *testing.T) {
	rects, want := tbAvoidRects(), questplan.TbwtAvoid()
	if len(rects) != len(want) || len(rects) == 0 {
		t.Fatalf("%d walker rectangles for %d plan rectangles", len(rects), len(want))
	}
	for i, r := range rects {
		w := want[i]
		if r.MinX != w.MinX || r.MaxX != w.MaxX || r.MinZ != w.MinZ || r.MaxZ != w.MaxZ || r.Level != w.Level {
			t.Fatalf("rectangle %d: %+v, plan %+v", i, r, w)
		}
	}
	// A spawn is inside its own rectangle and a tile two pads away is not.
	h := questplan.TbwtHostiles()[0]
	at := func(x, z int) webwalk.Point { return webwalk.Point{X: x, Z: z, Level: 0} }
	if !rects[0].Contains(at(h.Tile.X, h.Tile.Z)) || rects[0].Contains(at(h.Tile.X+2*questplan.TbwtPadOf(h.NPC)+2, h.Tile.Z)) {
		t.Errorf("rectangle 0 %+v for %v", rects[0], h)
	}
}

// Only one account of the process watches Tamayu's hunt at a time: the sealed map's hunter and beast are shared. A second account
// leaves the queue after tbHuntQueue without a word to Tamayu, and the lock is the first one's to release.
func TestTbwtTheHuntQueuesBehindAnotherWatcher(t *testing.T) {
	was := tbHuntQueue
	tbHuntQueue = 40 * time.Millisecond
	defer func() { tbHuntQueue = was }()
	w := newExecWorld()
	w.sim.StandAt(questplan.TbTamayuAnchor.X, questplan.TbTamayuAnchor.Z)
	tbHuntMu.Lock()
	ok := tbTamayuHunt(w.e, questplan.Step{Qty: 1})
	if ok || !strings.Contains(tbLog(w), "leaving the queue") {
		t.Errorf("a hunt behind another watcher: %v %v", ok, *w.log)
	}
	if tbHuntMu.TryLock() {
		t.Fatal("the queued account released a lock it did not hold")
	}
	tbHuntMu.Unlock()
	// With the queue empty the hunt goes on past it (to Tamayu, who is not there) and leaves the lock free behind it.
	w = newExecWorld()
	w.sim.StandAt(questplan.TbTamayuAnchor.X, questplan.TbTamayuAnchor.Z)
	tbTamayuHunt(w.e, questplan.Step{Qty: 1})
	if strings.Contains(tbLog(w), "leaving the queue") {
		t.Errorf("a hunt with no one in front waited: %v", *w.log)
	}
	if !tbHuntMu.TryLock() {
		t.Fatal("the hunt kept the lock after it returned")
	}
	tbHuntMu.Unlock()
}

// The wait for a hunt is shorter than the slot's park for a failed leg, so a queue never outlasts the errand's own retry.
func TestTbwtTheQueueIsShorterThanTheErrandsPark(t *testing.T) {
	if tbHuntQueue <= 0 || tbHuntQueue > questplan.TbLegPark {
		t.Errorf("queue %s, leg park %s", tbHuntQueue, questplan.TbLegPark)
	}
}
