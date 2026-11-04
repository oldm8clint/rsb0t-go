package fishmanipplan

import (
	"math/rand"
	"strings"
	"testing"

	"rsb0t-go/internal/tickmanip"
)

// The engine side, from internal/tickmanip's model of the fishing scripts: a write made right after the PLAYER_INFO of
// tick k is read in tick k+1+late(k).
type rig struct {
	sim     *tickmanip.Sim
	ctl     *Controller
	late    func(send int) int
	pending map[int][]tickmanip.Step // read tick -> the steps read then
	t0      int
	rolls   []int
	leaks   int
	maxRead int // most user events the engine was asked to read in a tick
	written []Action
}

func newRig(skill tickmanip.Skill, s Settings, late func(send int) int) *rig {
	return &rig{sim: tickmanip.NewSim(skill, 5000), ctl: New(s), late: late, pending: map[int][]tickmanip.Step{}, t0: 100}
}

func steps(a Action) []tickmanip.Step {
	switch a {
	case Batch:
		return append(append(tickmanip.Knife(), tickmanip.CloseModal), tickmanip.Click)
	case Lone:
		return []tickmanip.Step{tickmanip.Click}
	}
	return nil
}

// write queues action a, written after tick n.
func (r *rig) write(a Action, n int) {
	r.ctl.Sent(a, n)
	at := n + 1 + r.late(n)
	r.pending[at] = append(r.pending[at], steps(a)...)
	r.written = append(r.written, a)
}

// run opens a hold at tick t0 (an account that was not fishing: the varp is stale) and runs the pattern for n ticks.
func (r *rig) run(n int) {
	if r.ctl.Begin(r.t0 - 1) {
		r.write(Batch, r.t0-1)
	}
	for t := r.t0; t < r.t0+n; t++ {
		batch := r.pending[t]
		delete(r.pending, t)
		res := r.sim.Tick(t, batch)
		if len(res.Read)+res.Deferred > r.maxRead {
			r.maxRead = len(res.Read) + res.Deferred
		}
		if res.Roll {
			r.rolls = append(r.rolls, t)
		}
		if res.Fletched {
			r.leaks++
		}
		anim := NoAnim
		if res.AnimFletch {
			anim = FletchAnim
		}
		if a := r.ctl.Observe(Obs{N: t, Anim: anim}); a != None {
			r.write(a, t)
		}
	}
}

func baseline(skill tickmanip.Skill, n int) int {
	return tickmanip.Replay(skill, tickmanip.NormalPlan(), n).Rolls()
}

// flips is a link whose lateness is 0 and, with probability p on each write, 1.
func flips(seed int64, p float64) func(int) int {
	rng := rand.New(rand.NewSource(seed))
	return func(int) int {
		if rng.Float64() < p {
			return 1
		}
		return 0
	}
}

// randomLate is a link whose lateness is drawn evenly from 0 .. n-1.
func randomLate(seed int64, n int) func(int) int {
	rng := rand.New(rand.NewSource(seed))
	return func(int) int { return rng.Intn(n) }
}

// A batch, a lone click two ticks after it is read and the next batch the tick after the roll: one roll every three ticks at
// a net, a big net's spot and a harpoon spot, from the opening click, at any steady link lateness.
func TestPatternRollsEveryThreeTicks(t *testing.T) {
	for _, sk := range []tickmanip.Skill{tickmanip.NetFishing, tickmanip.HarpoonFishing} {
		for late := 0; late <= 2; late++ {
			late := late
			r := newRig(sk, DefaultSettings(), func(int) int { return late })
			r.run(600)
			base := baseline(sk, 600)
			if len(r.rolls) < 195 || len(r.rolls) < 19*base/10 {
				t.Errorf("%s, link %d tick(s) late: %d rolls in 600 ticks, want about 200 (baseline %d)", sk, late, len(r.rolls), base)
			}
			for i := 6; i < len(r.rolls); i++ {
				if g := r.rolls[i] - r.rolls[i-1]; g != 3 {
					t.Errorf("%s, link %d late: roll %d came %d ticks after the last, want 3", sk, late, i, g)
					break
				}
			}
			if r.leaks != 0 {
				t.Errorf("%s, link %d late: %d carrier logs fletched", sk, late, r.leaks)
			}
			if r.maxRead > tickmanip.UserEventLimit {
				t.Errorf("%s, link %d late: %d user events due in one tick, the engine reads %d", sk, late, r.maxRead, tickmanip.UserEventLimit)
			}
			st := r.ctl.Stats()
			if st.Lost != 0 || st.Confirmed < 190 || st.Spacing3 != st.Spacings || st.Lateness[late] != st.Confirmed || st.Lead != late {
				t.Errorf("%s, link %d late: stats %s", sk, late, st.Line())
			}
		}
	}
}

// A link three ticks late confirms a batch only after the next is due: the pattern slows to one batch per confirmation and
// still never has two batches in flight.
func TestAVerySlowLinkSlowsThePatternAndNeverMergesBatches(t *testing.T) {
	r := newRig(tickmanip.NetFishing, DefaultSettings(), func(int) int { return 3 })
	r.run(600)
	if r.leaks != 0 || r.maxRead > tickmanip.UserEventLimit {
		t.Errorf("leaks %d, most events due in one tick %d", r.leaks, r.maxRead)
	}
	if n, base := len(r.rolls), baseline(tickmanip.NetFishing, 600); n < 12*base/10 {
		t.Errorf("%d rolls against the plain click's %d", n, base)
	}
}

// THE REASON THE LOOP IS CLOSED. A link whose lateness flips between 0 and 1 reads some batches a tick early or late of the
// pattern. Writing by a fixed schedule loses a third of a net's rolls and three quarters of a harpoon's to flips in 2% of the
// writes; anchored on each batch's own read tick a flip costs the roll it hits and no more.
func TestAFlippingLinkLosesOnlyTheRollsItHits(t *testing.T) {
	for _, sk := range []tickmanip.Skill{tickmanip.NetFishing, tickmanip.HarpoonFishing} {
		base := baseline(sk, 900)
		for _, c := range []struct {
			p        float64
			wantX100 int // least rolls, as a percentage of the plain click's
		}{{0.02, 170}, {0.05, 140}, {0.10, 130}} {
			r := newRig(sk, DefaultSettings(), flips(3, c.p))
			r.run(900)
			if got := len(r.rolls) * 100 / base; got < c.wantX100 {
				t.Errorf("%s, lateness flipping in %.0f%% of writes: %d rolls in 900 ticks = %d%% of the plain click's %d, want at least %d%%", sk, 100*c.p, len(r.rolls), got, base, c.wantX100)
			}
			if r.leaks != 0 || r.maxRead > tickmanip.UserEventLimit {
				t.Errorf("%s, flips %.2f: leaks %d, most events due in one tick %d", sk, c.p, r.leaks, r.maxRead)
			}
		}
	}
}

// Batches that never show their animation are lost after the ticks and the clock have passed, the pattern goes on
// meanwhile, and MaxLost in a row stop the hold's manipulation.
func TestLostBatchesStopTheHold(t *testing.T) {
	now := int64(0)
	c := New(Settings{Enabled: true, MaxLost: 3})
	c.SetClock(func() int64 { return now })
	if !c.Begin(99) {
		t.Fatal("no opening batch")
	}
	c.Sent(Batch, 99)
	// Ticks pass in a burst with no real time: nothing is lost yet, and no second batch goes out.
	for n := 100; n < 130; n++ {
		if a := c.Observe(Obs{N: n, Anim: NoAnim}); a == Batch {
			t.Fatalf("a second batch at tick %d with the first neither confirmed nor lost", n)
		}
	}
	if c.Stats().Lost != 0 || !c.InFlight() {
		t.Fatalf("lost %d, in flight %v, with no real time passed", c.Stats().Lost, c.InFlight())
	}
	// Real time passes: the batch is lost and the next goes out; none of them is ever read.
	for n := 130; n < 400 && !c.Stopped(); n++ {
		now += 600
		if a := c.Observe(Obs{N: n, Anim: NoAnim}); a == Batch {
			c.Sent(Batch, n)
		}
	}
	if !c.Stopped() || c.Stats().Lost != 3 || c.Stats().Stops != 1 {
		t.Fatalf("stopped %v after %d lost batches, stops %d; want it stopped after 3", c.Stopped(), c.Stats().Lost, c.Stats().Stops)
	}
	if a := c.Observe(Obs{N: 500, Anim: NoAnim}); a != None {
		t.Errorf("a stopped hold asked for %v", a)
	}
	// The next hold starts afresh.
	if !c.Begin(600) || c.Stopped() {
		t.Error("the next hold did not start afresh")
	}
}

// The lone click is written for the roll two ticks after the batch's read tick, the next batch for the tick after it, once
// each; a confirmation or a burst that arrives after the lone click's tick sends none for that batch.
func TestLoneClickAndNextBatchFollowTheBatchsReadTick(t *testing.T) {
	c := New(DefaultSettings())
	c.Begin(9)
	c.Sent(Batch, 9)
	var got []Action
	for n := 10; n <= 13; n++ {
		anim := NoAnim
		if n == 10 {
			anim = FletchAnim // read right after it was written: the link is not late
		}
		a := c.Observe(Obs{N: n, Anim: anim})
		got = append(got, a)
		if a != None {
			c.Sent(a, n)
		}
	}
	want := []Action{None, Lone, Batch, None}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("actions after ticks 10..13 = %v, want %v", got, want)
		}
	}
	// A client told its ticks in a burst: the lone click's tick has gone by when the controller hears of it.
	late := New(DefaultSettings())
	late.Begin(19)
	late.Sent(Batch, 19)
	if a := late.Observe(Obs{N: 20, Anim: FletchAnim}); a != None {
		t.Fatalf("tick 20: %v", a)
	}
	if a := late.Observe(Obs{N: 23, Anim: NoAnim}); a != Batch {
		t.Fatalf("tick 23, the lone click's tick long gone: %v, want the next batch", a)
	}
	if late.Stats().Lones != 0 {
		t.Errorf("%d lone clicks for a batch whose roll tick had passed", late.Stats().Lones)
	}
}

// The aim moves to the lateness most of the last batches showed, one tick at a time, and writes go that many ticks early.
func TestAimFollowsTheLinksLateness(t *testing.T) {
	c := New(DefaultSettings())
	c.Begin(9)
	c.Sent(Batch, 9)
	// Read two ticks after the next one: late by 1 after 9+1.
	c.Observe(Obs{N: 11, Anim: FletchAnim})
	if c.Lead() != 1 {
		t.Fatalf("lead %d after a batch read one tick late, want 1", c.Lead())
	}
	// The roll is at 11+2=13; the lone click is written after the PLAYER_INFO of 12 (13-1) less the aim: tick 11.
	if a := c.Observe(Obs{N: 11, Anim: NoAnim}); a != Lone {
		t.Errorf("with the aim at 1 the lone click is written after tick 11, got %v", a)
	}
}

// A batch a previous hold left in flight blocks the opening batch of the next until it is settled; silence about it is
// not counted as a loss.
func TestCarriedBatchBlocksTheOpeningAndIsNeverALoss(t *testing.T) {
	c := New(DefaultSettings())
	c.Begin(9)
	c.Sent(Batch, 9)
	if c.Begin(10) {
		t.Fatal("an opening batch with one still in flight")
	}
	for n := 11; n <= 20; n++ {
		c.Observe(Obs{N: n, Anim: NoAnim})
	}
	if c.InFlight() || c.Stats().Lost != 0 {
		t.Fatalf("in flight %v, lost %d after the carried batch grew old", c.InFlight(), c.Stats().Lost)
	}
	if !c.Begin(30) {
		t.Error("no opening batch once the carried one was settled")
	}
}

// A hold that finds the account still fishing from the last one goes on with the schedule while it is fresh.
func TestResumeGoesOnWithAFreshScheduleOnly(t *testing.T) {
	c := New(DefaultSettings())
	if c.Resume(5) {
		t.Fatal("a schedule from nothing")
	}
	c.Begin(9)
	c.Sent(Batch, 9)
	if !c.Resume(12) {
		t.Error("no resume three ticks after a batch")
	}
	if c.Resume(40) {
		t.Error("a resume 31 ticks after the last batch")
	}
	if New(Settings{}).Resume(1) {
		t.Error("a disabled controller resumed")
	}
}

func TestDisabledNeverWrites(t *testing.T) {
	c := New(Settings{Enabled: false})
	if c.Begin(1) {
		t.Fatal("a disabled controller asked for an opening batch")
	}
	if a := c.Observe(Obs{N: 5, Anim: FletchAnim}); a != None {
		t.Fatalf("a disabled controller asked for %v", a)
	}
}

func TestAliveFollowsTheLastConfirmedBatch(t *testing.T) {
	c := New(DefaultSettings())
	c.Begin(9)
	c.Sent(Batch, 9)
	if c.Alive(10) {
		t.Error("alive before any batch was confirmed")
	}
	c.Observe(Obs{N: 10, Anim: FletchAnim})
	for n, want := range map[int]bool{10: true, 12: true, 16: true, 17: false} {
		if got := c.Alive(n); got != want {
			t.Errorf("Alive(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestSettingsNormaliseAndKeepEnabledAsGiven(t *testing.T) {
	c := New(Settings{Enabled: true})
	d := DefaultSettings()
	if c.cfg.History != d.History || c.cfg.MaxLead != d.MaxLead || c.cfg.ConfirmTicks != d.ConfirmTicks || c.cfg.LostAfterMS != d.LostAfterMS || c.cfg.MaxLost != d.MaxLost || !c.cfg.Enabled {
		t.Fatalf("zero fields did not take their defaults: %+v", c.cfg)
	}
	if New(Settings{}).cfg.Enabled {
		t.Fatal("a zero Settings must stay disabled")
	}
	if got := New(Settings{Enabled: true, MaxLead: 9}).cfg.MaxLead; got != 3 {
		t.Fatalf("MaxLead 9 normalised to %d, want the cap 3", got)
	}
}

func TestStatsLineSaysWhatItCounted(t *testing.T) {
	r := newRig(tickmanip.NetFishing, DefaultSettings(), func(int) int { return 1 })
	r.run(60)
	line := r.ctl.Stats().Line()
	for _, want := range []string{"batches (", "confirmed", "lone clicks", "link delay 0/1/2/3+ ticks late 0/", "aiming 1 tick(s) early", "batch spacings were 3 ticks"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
}

// A link whose lateness is all over the place gains nothing from the pattern (the model: lateness drawn from 0, 1 and 2 at
// random leaves a net 30% below the plain click), so the hold stops once the last Window spacings have no regular value; a
// link that flips now and then, or is steadily slow, keeps it.
func TestIrregularSpacingsStopTheHoldAndRegularOnesDoNot(t *testing.T) {
	for _, c := range []struct {
		name     string
		late     func(int) int
		wantStop bool
	}{
		{"lateness 0, 1 or 2 at random", randomLate(5, 3), true},
		{"a quarter of the writes late", flips(3, 0.25), false},
		{"one write in twenty late", flips(3, 0.05), false},
		{"always three ticks late (every spacing 4)", func(int) int { return 3 }, false},
	} {
		r := newRig(tickmanip.NetFishing, DefaultSettings(), c.late)
		r.run(900)
		if got := r.ctl.Stopped(); got != c.wantStop {
			t.Errorf("%s: stopped %v (%q), want %v; %s", c.name, got, r.ctl.StopReason(), c.wantStop, r.ctl.Stats().Line())
		}
		if c.wantStop && !strings.Contains(r.ctl.StopReason(), "no regular value") {
			t.Errorf("%s: reason %q", c.name, r.ctl.StopReason())
		}
	}
}

// Continue takes a fresh schedule over and treats a batch in flight as one the new feed may not have watched.
func TestContinueMarksTheBatchInFlightAsCarried(t *testing.T) {
	c := New(DefaultSettings())
	if c.Continue(10) {
		t.Fatal("continued with no schedule")
	}
	c.Begin(10)
	c.Sent(Batch, 10)
	if !c.Continue(12) {
		t.Fatal("did not continue a schedule two ticks old")
	}
	// Silence long enough to call a batch lost is no loss for a carried one.
	for n := 13; n < 20; n++ {
		c.Observe(Obs{N: n, Anim: NoAnim})
	}
	if s := c.Stats(); s.Lost != 0 {
		t.Errorf("a carried batch counted as lost: %+v", s)
	}
	if c.Continue(40) {
		t.Error("continued a schedule thirty ticks old")
	}
}
