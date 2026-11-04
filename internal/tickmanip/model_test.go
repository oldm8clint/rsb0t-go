package tickmanip

import (
	"math/rand/v2"
	"reflect"
	"testing"
)

// rollTicks replays a plan and returns the arm ticks the roll fired on.
func rollTicks(sk Skill, p Plan, ticks int) []int { return Replay(sk, p, ticks).RollTicks() }

// every returns first, first+step, ... below limit.
func every(first, step, limit int) []int {
	var out []int
	for t := first; t < limit; t += step {
		out = append(out, t)
	}
	return out
}

// TestNormalCadence pins the unmanipulated roll ticks of each script, derived by hand from the
// content and cross-checked against the lead's table: one roll per 4 ticks cutting, 5 fly fishing,
// 6 net and 6 harpoon, the first one after the click's own set-up ticks.
func TestNormalCadence(t *testing.T) {
	cases := []struct {
		sk    Skill
		first int
		step  int
	}{
		{Woodcutting, 3, 4},    // woodcut.rs2: click sets delay = clock+3, roll on that tick, then every 4
		{FlyFishing, 4, 5},     // freshfish.rs2: delay = clock+4, roll at +4, then op4 resets +4 one tick later
		{NetFishing, 4, 6},     // saltfish.rs2: +4 for the first roll, then op4 resets +5 one tick later
		{HarpoonFishing, 5, 6}, // memberfish.rs2: +5 for the first roll, then +5 one tick later
	}
	for _, c := range cases {
		got := rollTicks(c.sk, NormalPlan(), 60)
		want := every(c.first, c.step, 60)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%v normal: rolls at %v, want %v", c.sk, got, want)
		}
	}
}

// TestManipCadence is the claim under test: the recommended batch makes every script roll every
// third tick, from tick 2.
func TestManipCadence(t *testing.T) {
	for _, sk := range Skills {
		tr := Replay(sk, Recommended(sk), 60)
		want := every(2, 3, 60)
		if got := tr.RollTicks(); !reflect.DeepEqual(got, want) {
			t.Errorf("%v manip: rolls at %v, want %v", sk, got, want)
		}
		if n := tr.Leaks(); n != 0 {
			t.Errorf("%v manip leaked %d queued fletches (a log consumed each)", sk, n)
		}
	}
}

// TestKnifeAloneFletches is why the click matters: a knife pair with nothing after it lets the
// queued fletch_logs run two ticks later, consuming the log and paying Fletching XP.
func TestKnifeAloneFletches(t *testing.T) {
	only := func(tick int) []Step {
		if tick == 0 {
			return Knife()
		}
		return nil
	}
	tr := Replay(NetFishing, only, 6)
	for _, r := range tr.Results {
		if r.Fletched != (r.Tick == 2) {
			t.Errorf("tick %d: Fletched=%v, want it only on tick 2", r.Tick, r.Fletched)
		}
	}
}

// TestClickCancelsTheQueue: the same knife pair followed by any interrupt before the queue is due
// never fletches, whichever interrupt it is.
func TestClickCancelsTheQueue(t *testing.T) {
	for _, interrupt := range []Step{Click, MoveOwn, Horsey} {
		it := interrupt
		p := func(tick int) []Step {
			switch tick {
			case 0:
				return Knife()
			case 1:
				return []Step{it}
			}
			return nil
		}
		if n := Replay(NetFishing, p, 8).Leaks(); n != 0 {
			t.Errorf("knife then %v on the next tick still fletched %d time(s)", it, n)
		}
	}
}

// TestEarlyLoneClickNeverRollsAtANet: the lead's "one tick early does nothing", taken to its end. A lone
// click read while %action_delay = map_clock + 1 matches no branch and ends the interaction, and the
// tick after it nobody clicks, so no roll ever fires at a net or a harpoon spot.
func TestEarlyLoneClickNeverRollsAtANet(t *testing.T) {
	for _, sk := range []Skill{NetFishing, HarpoonFishing} {
		p := ManipPlan(ManipOptions{Period: 3, LoneClick: 1})
		if got := rollTicks(sk, p, 90); len(got) != 0 {
			t.Errorf("%v with the lone click one tick early rolled at %v, want never", sk, got)
		}
	}
}

// TestLateLoneClickRestartsTheNormalCycle: a lone click one tick late finds the varp below the clock
// and starts the ordinary cycle, so the roll comes back at the normal 6-tick spacing, never faster.
// (The knife-only batch here also lets the queued fletch run at tick 2: that is the leak the click in
// a real batch prevents.)
func TestLateLoneClickRestartsTheNormalCycle(t *testing.T) {
	p := ManipPlan(ManipOptions{Period: 60, LoneClick: 3, NoClick: true})
	tr := Replay(NetFishing, p, 40)
	got := tr.RollTicks()
	// batch at 0 (delay = 2), click at 3: delay < clock => delay = 3+4 = 7, cast at 4, roll at 7;
	// then op4 loops: reset at 8 => delay 13, roll at 13, then 19.
	want := []int{7, 13, 19}
	if len(got) < 3 || !reflect.DeepEqual(got[:3], want) {
		t.Errorf("late lone click: rolls at %v, want it to start with %v", got, want)
	}
	if tr.Leaks() != 1 {
		t.Errorf("the knife-only batch should leak exactly one fletch, leaked %d", tr.Leaks())
	}
}

// TestFlyBatchOnTheRollTickThrowsTheRollAway: freshfish op1 does not roll when delay == clock (only
// op4 does), so a batch read ON the roll tick replaces the op4 that would have rolled.
func TestFlyBatchOnTheRollTickThrowsTheRollAway(t *testing.T) {
	p := func(tick int) []Step {
		if tick == 0 || tick == 2 { // the second batch lands on the roll tick of the first
			return append(Knife(), Click)
		}
		return nil
	}
	got := rollTicks(FlyFishing, p, 12)
	if len(got) != 1 || got[0] != 7 {
		t.Errorf("fly batch on the roll tick: rolls at %v, want only tick 7 (the tick-2 roll is lost)", got)
	}
}

// TestWoodBatchOnTheRollTickKeepsTheRoll: woodcutting's op1 does roll when delay == clock, so the same
// early batch loses nothing there.
func TestWoodBatchOnTheRollTickKeepsTheRoll(t *testing.T) {
	p := func(tick int) []Step {
		if tick == 0 || tick == 2 {
			return append(Knife(), Click)
		}
		return nil
	}
	got := rollTicks(Woodcutting, p, 12)
	if len(got) < 1 || got[0] != 2 {
		t.Errorf("wood batch on the roll tick: rolls at %v, want the tick-2 roll kept", got)
	}
}

// TestLateBatchIsNeverWorseThanNormalForFly: a fly batch one tick late (it lands on the tick after the
// roll) finds the varp already reset by op4 and changes nothing, so the roll falls back to the normal
// 5-tick spacing instead of being lost.
func TestLateBatchFallsBackToNormal(t *testing.T) {
	late := Shift(Recommended(FlyFishing), 1)
	got := rollTicks(FlyFishing, late, 60)
	if len(got) == 0 {
		t.Fatal("a one-tick-late fly plan never rolled")
	}
	// It must still roll at least as often as the unmanipulated cycle over the same window.
	normal := Replay(FlyFishing, NormalPlan(), 60).Rolls()
	if len(got) < normal-1 {
		t.Errorf("late fly plan rolled %d times in 60 ticks, fewer than the %d of a plain click", len(got), normal)
	}
}

// TestInterruptsAreHarmless: a toy horsey or a walk-click on the player's own tile before the knife
// changes nothing, because OpHeldU already clears the pending action itself.
func TestInterruptsAreHarmless(t *testing.T) {
	for _, sk := range Skills {
		base := rollTicks(sk, Recommended(sk), 60)
		for _, prefix := range [][]Step{{MoveOwn}, {Horsey}} {
			o := ManipOptions{Period: 3, Prefix: prefix}
			if sk == NetFishing || sk == HarpoonFishing {
				o.LoneClick = 2
			}
			if got := rollTicks(sk, ManipPlan(o), 60); !reflect.DeepEqual(got, base) {
				t.Errorf("%v with prefix %v: rolls at %v, want the plain manip's %v", sk, prefix, got, base)
			}
		}
	}
}

// TestPeriodSweep: of every batch period from 1 to 8, only periods that keep the knife landing the tick
// after a roll beat the natural cycle, and none gets two rolls closer than 3 ticks.
func TestPeriodSweep(t *testing.T) {
	for _, sk := range Skills {
		best := 0
		for period := 1; period <= 8; period++ {
			o := ManipOptions{Period: period}
			if sk == NetFishing || sk == HarpoonFishing {
				o.LoneClick = 2
			}
			tr := Replay(sk, ManipPlan(o), 240)
			for _, gap := range tr.Intervals() {
				if gap < 3 {
					t.Errorf("%v period %d: two rolls %d ticks apart (< 3 is impossible)", sk, period, gap)
				}
			}
			if tr.Rolls() > best {
				best = tr.Rolls()
			}
		}
		if want := Replay(sk, Recommended(sk), 240).Rolls(); best != want {
			t.Errorf("%v: best period yields %d rolls in 240 ticks, the recommended plan %d", sk, best, want)
		}
	}
}

// TestNoRollCloserThanThreeTicks is the theorem behind the 3-tick floor, checked over random plans of
// every step the lab sends: a roll needs %action_delay = map_clock, the roll tick leaves it at the
// clock, and the earliest anything can set it again is the next tick, to clock + 2 (the knife) or more.
func TestNoRollCloserThanThreeTicks(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	pool := []func() []Step{
		func() []Step { return Knife() },
		func() []Step { return []Step{Click} },
		func() []Step { return []Step{MoveOwn} },
		func() []Step { return []Step{Horsey} },
		func() []Step { return append(Knife(), Click) },
	}
	for trial := 0; trial < 300; trial++ {
		batches := make(map[int][]Step)
		for tick := 0; tick < 120; tick++ {
			if rng.IntN(100) < 40 {
				n := 1 + rng.IntN(2)
				for i := 0; i < n; i++ {
					batches[tick] = append(batches[tick], pool[rng.IntN(len(pool))]()...)
				}
			}
		}
		for _, sk := range Skills {
			tr := Replay(sk, func(tick int) []Step { return batches[tick] }, 120)
			for _, gap := range tr.Intervals() {
				if gap < 3 {
					t.Fatalf("trial %d %v: rolls %d ticks apart in a random plan", trial, sk, gap)
				}
			}
		}
	}
}

// TestUserEventLimit: the engine reads five accepted user events a tick and leaves the rest in the
// socket, so a batch of seven loses nothing, it is read over two ticks, in order.
func TestUserEventLimit(t *testing.T) {
	sim := NewSim(Woodcutting, baseClock)
	batch := []Step{MoveOwn, MoveOwn, MoveOwn, MoveOwn, MoveOwn, MoveOwn, Click}
	r0 := sim.Tick(0, batch)
	if len(r0.Read) != 5 || r0.Deferred != 2 {
		t.Fatalf("tick 0 read %d steps, deferred %d; want 5 and 2", len(r0.Read), r0.Deferred)
	}
	r1 := sim.Tick(1, nil)
	if len(r1.Read) != 2 || r1.Read[1] != Click {
		t.Fatalf("tick 1 read %v, want the two left-over steps ending in the click", r1.Read)
	}
	if r1.Ran != OpLoc1 {
		t.Errorf("the click read on tick 1 should have run oploc1 that tick, ran %v", r1.Ran)
	}
}

// TestRefusedPacketsCostNoBudget: a Make-1 with no dialogue open is refused, and a refusal counts
// against the CLIENT_EVENT budget, not the five user events.
func TestRefusedPacketsCostNoBudget(t *testing.T) {
	sim := NewSim(Woodcutting, baseClock)
	batch := []Step{MakeShafts, MakeShafts, MakeShafts, MakeShafts, MakeShafts, MakeShafts, Click}
	r := sim.Tick(0, batch)
	if r.Refused != 6 || r.Deferred != 0 || r.Ran != OpLoc1 {
		t.Errorf("refused=%d deferred=%d ran=%v, want 6, 0 and oploc1", r.Refused, r.Deferred, r.Ran)
	}
}

// TestSplitKnifeAcrossTicks: if the make button lands a tick after the knife (a packet split by the
// connection), the dialogue is still open at the end of the first tick, so the interaction is blocked
// for that tick, and the pair still works afterwards: the varp is set when the BUTTON is read.
func TestSplitKnifeAcrossTicks(t *testing.T) {
	sim := NewSim(FlyFishing, baseClock)
	r0 := sim.Tick(0, []Step{KnifeUse})
	if r0.Ran != OpNone {
		t.Errorf("tick 0 ran %v with a dialogue open", r0.Ran)
	}
	r1 := sim.Tick(1, []Step{MakeShafts, Click})
	if r1.Ran != OpNpc1 {
		t.Errorf("tick 1 ran %v, want opnpc1", r1.Ran)
	}
	if r1.DelayAfter != 2 {
		t.Errorf("clock+2 was set on tick 1, so the varp should read two ticks out, got %d", r1.DelayAfter)
	}
	var rolls []int
	for tick := 2; tick < 6; tick++ {
		if r := sim.Tick(tick, nil); r.Roll {
			rolls = append(rolls, tick)
		}
	}
	if len(rolls) == 0 || rolls[0] != 3 {
		t.Errorf("rolls after the split pair at %v, want the first on tick 3 (button tick + 2)", rolls)
	}
}

// TestClockIndependence: only differences matter, so a different starting clock replays identically.
func TestClockIndependence(t *testing.T) {
	a := Replay(FlyFishing, Recommended(FlyFishing), 40).RollTicks()
	sim := NewSim(FlyFishing, 987654)
	var b []int
	for tick := 0; tick < 40; tick++ {
		if r := sim.Tick(tick, Recommended(FlyFishing)(tick)); r.Roll {
			b = append(b, tick)
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("rolls depend on the starting clock: %v vs %v", a, b)
	}
}
