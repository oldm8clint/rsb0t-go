package bot

import (
	"errors"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fisherplan"
)

// fakeWorlds is a fleet seen through a Context's world hooks.
type fakeWorlds struct {
	home    int
	hops    []int
	refuse  error
	hopping bool
}

func (f *fakeWorlds) ctx(current int, settings map[string]interface{}) *Context {
	return &Context{
		Username: "fisher7", Settings: settings, Log: func(string) {},
		CurrentWorld: current, HomeWorld: 2,
		SetHomeWorld:  func(w int) error { f.home = w; return nil },
		HopWorld:      func(w int, _ string) error { f.hops = append(f.hops, w); return f.refuse },
		HopInProgress: func() bool { return f.hopping },
	}
}

// TestHomeWorldSettingAcceptsTheControlPanelsRange: 1-255 is a world, anything else is "none".
func TestHomeWorldSettingAcceptsTheControlPanelsRange(t *testing.T) {
	for _, c := range []struct {
		v    interface{}
		want int
	}{{nil, 0}, {1.0, 1}, {3, 3}, {255.0, 255}, {0.0, 0}, {-1.0, 0}, {256.0, 0}, {"1", 0}} {
		m := map[string]interface{}{}
		if c.v != nil {
			m["homeWorld"] = c.v
		}
		if got := homeWorldSetting(m); got != c.want {
			t.Errorf("homeWorld %v = %d, want %d", c.v, got, c.want)
		}
	}
}

// TestHomeWorldMovesAnAccountSwitchedFromAnotherWorld is the runtime switch: a ScarabSwarm account
// on world 2 (home world 2, set by ScarabSwarm) becomes a fisher with homeWorld 1. The fleet's home
// is changed to 1 at once, and the first Loop asks to hop there.
func TestHomeWorldMovesAnAccountSwitchedFromAnotherWorld(t *testing.T) {
	f := &fakeWorlds{}
	ctx := f.ctx(2, map[string]interface{}{"homeWorld": 1.0})
	var h homeWorld
	h.start(ctx, "fisher")
	if f.home != 1 {
		t.Fatalf("fleet home = %d, want 1: donorrunner's hop home after a hand-off goes to the fleet's home", f.home)
	}
	now := time.Now()
	if !h.step(ctx, "fisher", now) {
		t.Fatal("step = false on world 2 with home 1, want a hop")
	}
	if len(f.hops) != 1 || f.hops[0] != 1 {
		t.Fatalf("hops = %v, want [1]", f.hops)
	}
	// While the fleet runs the hop, the script waits rather than asking again.
	f.hopping = true
	if !h.step(ctx, "fisher", now.Add(time.Second)) || len(f.hops) != 1 {
		t.Fatalf("hops = %v, want still one while a hop is in progress", f.hops)
	}
}

// TestHomeWorldDoesNothingAtHomeOrUnset: on its home world, or with no homeWorld, nothing moves.
func TestHomeWorldDoesNothingAtHomeOrUnset(t *testing.T) {
	f := &fakeWorlds{}
	var h homeWorld
	ctx := f.ctx(1, map[string]interface{}{"homeWorld": 1.0})
	h.start(ctx, "fisher")
	if h.step(ctx, "fisher", time.Now()) || len(f.hops) != 0 {
		t.Fatalf("hops = %v on its home world, want none", f.hops)
	}
	ctx = f.ctx(2, map[string]interface{}{})
	f.home = 0
	h.start(ctx, "fisher")
	if f.home != 0 || h.step(ctx, "fisher", time.Now()) || len(f.hops) != 0 {
		t.Fatalf("an account with no homeWorld moved: home %d, hops %v", f.home, f.hops)
	}
}

// TestHomeWorldARefusedHopWaitsBeforeAskingAgain: a refusal (the fleet's own hop bounds) is not a
// loop. The account works where it is and asks again after homeWorldRetry.
func TestHomeWorldARefusedHopWaitsBeforeAskingAgain(t *testing.T) {
	f := &fakeWorlds{refuse: errors.New("too many hops")}
	ctx := f.ctx(3, map[string]interface{}{"homeWorld": 1.0})
	var h homeWorld
	h.start(ctx, "wcfletch")
	now := time.Now()
	if h.step(ctx, "wcfletch", now) {
		t.Fatal("step = true for a refused hop, want false: the account carries on working")
	}
	if h.step(ctx, "wcfletch", now.Add(time.Minute)) || len(f.hops) != 1 {
		t.Fatalf("hops = %v a minute later, want one: the retry waits %s", f.hops, homeWorldRetry)
	}
	h.step(ctx, "wcfletch", now.Add(homeWorldRetry+time.Second))
	if len(f.hops) != 2 {
		t.Fatalf("hops = %v after the retry wait, want two", f.hops)
	}
}

// TestHomeWorldOutsideAFleetIsInert: a cmd/ tool or test Context has no hooks at all.
func TestHomeWorldOutsideAFleetIsInert(t *testing.T) {
	ctx := &Context{Settings: map[string]interface{}{"homeWorld": 2.0}, Log: func(string) {}}
	var h homeWorld
	h.start(ctx, "fisher")
	if h.step(ctx, "fisher", time.Now()) {
		t.Fatal("step = true with no fleet behind the Context")
	}
}

// TestFisherAutoSharksOffPinsBigNet is the owner's big-net gem fishing at Fishing 90+: without the
// pin the ladder harpoons sharks there (no gems) and the hand-off never counts as at target.
func TestFisherAutoSharksOffPinsBigNet(t *testing.T) {
	on := fisherTargetSetting(map[string]interface{}{"target": "Big net"})
	off := fisherTargetSetting(map[string]interface{}{"target": "Big net", "autoSharks": false})
	if got := fisherplan.CurrentRung(95, on).Label; got != fisherplan.SharkRung {
		t.Fatalf("autoSharks default: Fishing 95 rung = %q, want sharks (unchanged behaviour)", got)
	}
	if got := fisherplan.CurrentRung(95, off).Label; got != "Big net" {
		t.Fatalf("autoSharks false: Fishing 95 rung = %q, want Big net", got)
	}
	if !fisherplan.AtTarget(95, off) {
		t.Fatal("autoSharks false: Fishing 95 is not at target, so the hand-off would never start")
	}
	if !fisherplan.SupportsGuildRung(fisherplan.CurrentRung(95, off)) {
		t.Fatal("big net is not a Guild rung, so a pinned 68+ account would stay at Catherby")
	}
}
