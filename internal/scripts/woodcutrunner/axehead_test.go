package woodcutrunner

// The axe-head random event end to end against actionsim: the axe has become an "Axe handle", the
// "Axe head" is on the ground, and RecoverAxeHead has to pick it up and use the two together,
// which content's reattach_axe turns back into the axe. Real object names and ids come from the
// game config (RSB0T_CONFIG_JAG); the content rule itself is scripted with actionsim.Combine,
// exactly as macro_event_lost_axe.rs2 states it.

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/woodcutplan"
)

func axeConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// objNamed is the first un-noted object with that exact name.
func objNamed(t *testing.T, cfg *cachecfg.Config, name string) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && !o.Noted() && strings.EqualFold(o.Name, name) {
			return int32(id)
		}
	}
	t.Fatalf("no item %q in this config", name)
	return 0
}

type axeHeadWorld struct {
	sim                       *actionsim.Session
	d                         *Deps
	handle, head, axe, logsID int32
}

// newAxeHeadWorld stands an account at a Seers tile holding pack, with the tabs loaded and the
// handle+head rule scripted.
func newAxeHeadWorld(t *testing.T, pack func(w *axeHeadWorld) []actionsim.Stack) *axeHeadWorld {
	cfg := axeConfig(t)
	w := &axeHeadWorld{
		handle: objNamed(t, cfg, woodcutplan.AxeHandle),
		head:   objNamed(t, cfg, woodcutplan.AxeHead),
		axe:    objNamed(t, cfg, woodcutplan.RuneAxe),
		logsID: objNamed(t, cfg, "Yew logs"),
	}
	w.sim = actionsim.New(actionsim.Objs{
		Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable },
	}, pack(w))
	w.sim.ShowTabs()
	w.sim.StandAt(2709, 3462)
	w.sim.Combine(w.handle, w.head, w.axe)
	var lines []string
	w.d = &Deps{
		Session: w.sim, Config: cfg, Components: actionsim.TabComponents(),
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		Log:   func(s string) { lines = append(lines, s) },
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("runner log:\n%s", strings.Join(lines, "\n"))
		}
	})
	return w
}

func (w *axeHeadWorld) packHas(id int32) int {
	n := 0
	for _, r := range w.sim.Pack() {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func (w *axeHeadWorld) mustRecover(t *testing.T) {
	t.Helper()
	ev := woodcutplan.DecideWoodEvent(EventSnapshot(w.d, nil))
	if ev != woodcutplan.EventAxeHead {
		t.Fatalf("DecideWoodEvent = %q, want %q: the handle is held and the head is on the ground", ev, woodcutplan.EventAxeHead)
	}
	res := RecoverAxeHead(w.d)
	if !res.Fixed {
		t.Fatalf("RecoverAxeHead = %+v, want Fixed", res)
	}
	if w.packHas(w.axe) != 1 || w.packHas(w.handle) != 0 || w.packHas(w.head) != 0 {
		t.Fatalf("pack = %+v, want the rune axe back and neither piece left", w.sim.Pack())
	}
	for _, g := range w.sim.Ground() {
		if int32(g[0]) == w.head {
			t.Fatalf("the head is still on the ground at %d,%d", g[1], g[2])
		}
	}
	if ev := woodcutplan.DecideWoodEvent(EventSnapshot(w.d, nil)); ev != woodcutplan.EventNone {
		t.Fatalf("after the repair DecideWoodEvent = %q, want none", ev)
	}
}

// TestRecoverAxeHeadPicksTheHeadUpAndPutsTheAxeBackTogether is the ordinary case: the handle in
// the pack, room to spare, the head on the ground.
func TestRecoverAxeHeadPicksTheHeadUpAndPutsTheAxeBackTogether(t *testing.T) {
	w := newAxeHeadWorld(t, func(w *axeHeadWorld) []actionsim.Stack {
		return []actionsim.Stack{{ID: w.handle, Count: 1}, {ID: w.logsID, Count: 1}, {ID: w.logsID, Count: 1}}
	})
	w.sim.DropOnGround(w.head, 2709, 3462)
	w.mustRecover(t)
	if w.packHas(w.logsID) != 2 {
		t.Errorf("logs = %d, want both kept: there was room, so nothing had to be dropped", w.packHas(w.logsID))
	}
}

// TestRecoverAxeHeadMakesRoomInAFullPack is Acct171 on 2026-09-22: 27 logs and the handle, no
// free slot, "the pack is full, so the head cannot be picked up" until the head despawned.
func TestRecoverAxeHeadMakesRoomInAFullPack(t *testing.T) {
	w := newAxeHeadWorld(t, func(w *axeHeadWorld) []actionsim.Stack {
		pack := []actionsim.Stack{{ID: w.handle, Count: 1}}
		for len(pack) < 28 {
			pack = append(pack, actionsim.Stack{ID: w.logsID, Count: 1})
		}
		return pack
	})
	w.sim.DropOnGround(w.head, 2709, 3462)
	w.mustRecover(t)
	if got := w.packHas(w.logsID); got != 26 {
		t.Errorf("logs = %d, want 26: exactly one dropped for the head (and the joined axe takes the handle's slot)", got)
	}
}

// TestRecoverAxeHeadTakesAWornHandleOff: the axe was wielded, so content put the handle in the
// right hand, and reattach_axe reads both pieces from the inventory.
func TestRecoverAxeHeadTakesAWornHandleOff(t *testing.T) {
	w := newAxeHeadWorld(t, func(w *axeHeadWorld) []actionsim.Stack {
		return []actionsim.Stack{{ID: w.logsID, Count: 1}}
	})
	w.sim.Wear(actionsim.RightHand, w.handle)
	w.sim.DropOnGround(w.head, 2709, 3462)
	w.mustRecover(t)
	if len(w.sim.Worn()) != 0 {
		t.Errorf("worn = %+v, want the handle taken off", w.sim.Worn())
	}
}

// TestRecoverAxeHeadWithTheHeadAlreadyHeld: a pass that picked the head up but was cut short
// before the join goes straight to the join.
func TestRecoverAxeHeadWithTheHeadAlreadyHeld(t *testing.T) {
	w := newAxeHeadWorld(t, func(w *axeHeadWorld) []actionsim.Stack {
		return []actionsim.Stack{{ID: w.handle, Count: 1}, {ID: w.head, Count: 1}}
	})
	w.mustRecover(t)
}

// TestRecoverAxeHeadGivesUpOnAFullPackOfNothingSpare: a full pack with nothing it may drop says
// so, and drops nothing.
func TestRecoverAxeHeadGivesUpOnAFullPackOfNothingSpare(t *testing.T) {
	cfg := axeConfig(t)
	coins := objNamed(t, cfg, "Coins")
	knife := objNamed(t, cfg, "Knife")
	w := newAxeHeadWorld(t, func(w *axeHeadWorld) []actionsim.Stack {
		pack := []actionsim.Stack{{ID: w.handle, Count: 1}, {ID: coins, Count: 500}}
		for len(pack) < 28 {
			pack = append(pack, actionsim.Stack{ID: knife, Count: 1})
		}
		return pack
	})
	w.sim.DropOnGround(w.head, 2709, 3462)
	res := RecoverAxeHead(w.d)
	if res.Fixed || !strings.Contains(res.Reason, "pack is full") {
		t.Fatalf("RecoverAxeHead = %+v, want a pack-full refusal", res)
	}
	if len(w.sim.Pack()) != 28 {
		t.Fatalf("pack has %d slots, want all 28 kept: knives and coins are not spare", len(w.sim.Pack()))
	}
}
