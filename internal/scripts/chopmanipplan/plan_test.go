package chopmanipplan

import (
	"math/rand"
	"testing"
)

// sim is the engine's side of a chop, reduced to the two scripts that matter, run in the engine's own order inside a
// tick: the player's packets first (World.processClientsIn), then the player's interaction (processPlayers).
//
//	cut_logs.rs2:83-84   knife: if %action_delay < map_clock { %action_delay = map_clock + 2 }, animation 1248
//	woodcut.rs2:55-59    first click: if %action_delay < map_clock { %action_delay = map_clock + 3; p_oploc(1) }
//	woodcut.rs2:84       after the axe check: swing animation; roll when %action_delay == map_clock; p_oploc(3)
//	woodcut.rs2:118-120  every later tick: re-arm to +3 when stale, roll when due
//
// The fletching animation wins the tick's animation over the chop animation (the lab saw 1248 at every batch tick).
type sim struct {
	v           int // %action_delay
	interacting bool
	retry1      bool // the p_oploc(1) the first click leaves behind runs once more next tick
	rolls       []int
}

func (s *sim) roll(t int) { s.rolls = append(s.rolls, t) }

// step runs tick t. batch means a [knife, Make 1, close, click] batch is read in this tick, click a plain tree click.
func (s *sim) step(t int, batch, click bool) (anim int) {
	anim = NoAnim
	if batch {
		if s.v < t {
			s.v = t + 2
		}
		anim = FletchAnim
	}
	switch {
	case batch || click:
		s.interacting = true
		s.retry1 = false
		if s.v < t {
			s.v = t + 3
			s.retry1 = true
			return anim
		}
		if anim == NoAnim {
			anim = 867
		}
		if s.v == t {
			s.roll(t)
		}
	case s.interacting && s.retry1:
		s.retry1 = false
		if s.v < t {
			s.v = t + 3
			s.retry1 = true
			return anim
		}
		anim = 867
		if s.v == t {
			s.roll(t)
		}
	case s.interacting:
		if s.v < t {
			s.v = t + 3
		} else if s.v == t {
			anim = 867
			s.roll(t)
		}
	}
	return anim
}

// outcome of a run.
type outcome struct {
	rolls  []int
	stats  Stats
	ctl    *Controller
	reads  []int // the ticks batches were read in
	writes []int
}

// gaps are the ticks between consecutive rolls.
func (o outcome) gaps() []int {
	var g []int
	for i := 1; i < len(o.rolls); i++ {
		g = append(g, o.rolls[i]-o.rolls[i-1])
	}
	return g
}

func meanInt(v []int) float64 {
	if len(v) == 0 {
		return 0
	}
	t := 0
	for _, x := range v {
		t += x
	}
	return float64(t) / float64(len(v))
}

// run drives a controller against the sim for ticks ticks after tick start. late(i) is how many ticks after the next
// tick the i-th batch written is read (0: in the tick after the PLAYER_INFO it follows), or -1 when it is never read.
func run(ctl *Controller, start, ticks int, late func(i int) int) outcome {
	s := &sim{}
	reads := map[int]bool{}
	var out outcome
	out.ctl = ctl
	n := 0
	schedule := func(after int) {
		d := late(n)
		n++
		out.writes = append(out.writes, after)
		if d < 0 {
			return
		}
		reads[after+1+d] = true
		out.reads = append(out.reads, after+1+d)
	}
	click := map[int]bool{}
	if ctl.BeginChop(start) {
		schedule(start)
		ctl.Sent(start)
	} else {
		click[start+1] = true
	}
	for t := start + 1; t <= start+ticks; t++ {
		anim := s.step(t, reads[t], click[t])
		if ctl.Observe(Obs{N: t, Anim: anim}) {
			schedule(t)
			ctl.Sent(t)
		}
	}
	out.rolls = s.rolls
	out.stats = ctl.Stats()
	return out
}

func onTime(int) int { return 0 }

func fixed(k int) func(int) int { return func(int) int { return k } }

// tail returns the last k gaps.
func tail(g []int, k int) []int {
	if len(g) < k {
		return g
	}
	return g[len(g)-k:]
}

func allGaps(t *testing.T, label string, g []int, want int) {
	t.Helper()
	for i, x := range g {
		if x != want {
			t.Fatalf("%s: gap %d is %d, want %d (gaps %v)", label, i, x, want, g)
		}
	}
}

func TestPlainChopRollsEveryFourTicksAndTheFirstAtThree(t *testing.T) {
	// The sim has to reproduce the lab before it is allowed to judge the controller: 24.2 logs per 100 ticks at a
	// 4-tick period, the first roll 3 ticks after the click (woodA: first catch mean t+3.0, catch gaps 4x151).
	ctl := New(Settings{Enabled: false})
	o := run(ctl, 100, 200, onTime)
	if len(o.rolls) < 40 || o.rolls[0] != 104 {
		t.Fatalf("first roll at %v of %d, want 104 (click read at 101, first roll 3 ticks on)", o.rolls[:min(3, len(o.rolls))], len(o.rolls))
	}
	allGaps(t, "plain", o.gaps(), PlainPeriod)
	if o.stats.Batches != 0 {
		t.Fatalf("a disabled controller wrote %d batches", o.stats.Batches)
	}
}

func TestOnTimeBatchesRollEveryThreeTicksFromTheSecondTick(t *testing.T) {
	// The lab's manip arm: the opening batch is read at start+1 (first roll 2 ticks on), each roll then triggers a batch
	// read in the stale tick, and every later gap is 3 (woodB: catch gaps 3x108).
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 300, onTime)
	if o.rolls[0] != 103 {
		t.Fatalf("first roll at %d, want 103: the opening batch re-arms the varp to +2", o.rolls[0])
	}
	allGaps(t, "on time", o.gaps(), Period)
	st := o.stats
	if st.Lost != 0 || st.Late1 != 0 || st.LateMore != 0 || st.Early != 0 || ctl.Lead() != 0 {
		t.Fatalf("an on-time run was not clean: %s", st.Line())
	}
	if st.OnTime < st.Batches-1 || st.Lateness[0] < st.Batches-1 {
		t.Fatalf("%d of %d batches on target, link delay 0 for %d: at most one may still be in flight", st.OnTime, st.Batches, st.Lateness[0])
	}
	if st.Gaps == 0 || st.ShortGaps != st.Gaps {
		t.Fatalf("%d of %d swing gaps were 3 ticks, want all", st.ShortGaps, st.Gaps)
	}
	if got, plain := len(o.rolls), 300/PlainPeriod; got < plain*4/3-3 {
		t.Fatalf("%d rolls in 300 ticks against %d plain: the 4/3 gain is missing", got, plain)
	}
}

// The live fleet on 6 October: most proxied accounts read every batch exactly one tick late. Written right after the swing it
// follows, such a batch lands in the tick after the stale one and moves nothing. Written one tick EARLIER (after the
// PLAYER_INFO of the tick before the swing it is aimed at) it lands in the stale tick.
func TestASteadyOneLateLinkIsAimedOneTickEarlyAndGainsTheFullThree(t *testing.T) {
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 400, fixed(1))
	if ctl.Lead() != 1 {
		t.Fatalf("the aim is %d after 400 ticks on a link that is one tick late: %s", ctl.Lead(), o.stats.Line())
	}
	allGaps(t, "one late, steady state", tail(o.gaps(), 60), Period)
	if mean := meanInt(o.gaps()); mean > 3.1 {
		t.Fatalf("mean swing gap %.2f on a steady one-late link, want about 3 (gaps %v)", mean, o.gaps()[:min(12, len(o.gaps()))])
	}
	st := o.stats
	if st.Lateness[1] < st.Batches-2 || st.Lost != 0 {
		t.Fatalf("link delay not measured as one tick: %s", st.Line())
	}
	if st.OnTime < st.Batches-4 {
		t.Fatalf("only %d of %d batches were read in the tick aimed at: %s", st.OnTime, st.Batches, st.Line())
	}
}

func TestASteadyTwoLateLinkIsAimedTwoTicksEarly(t *testing.T) {
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 400, fixed(2))
	if ctl.Lead() != 2 {
		t.Fatalf("the aim is %d on a link that is two ticks late: %s", ctl.Lead(), o.stats.Line())
	}
	allGaps(t, "two late, steady state", tail(o.gaps(), 60), Period)
	if o.stats.Lost != 0 {
		t.Fatalf("batches were lost on a steady link: %s", o.stats.Line())
	}
}

func TestAThreeLateLinkIsAimedAtTheMostAndIsHarmless(t *testing.T) {
	// Beyond MaxLead a link cannot be pipelined with one batch in flight. The batches land late and move nothing, the
	// swings stay at their plain period, and the controller neither stops nor writes two at once.
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 400, fixed(3))
	if ctl.Lead() != 2 {
		t.Fatalf("the aim is %d, want the most (2): %s", ctl.Lead(), o.stats.Line())
	}
	if mean := meanInt(o.gaps()); mean < float64(PlainPeriod)-0.05 {
		t.Fatalf("mean gap %.2f below the plain %d on a link no batch can use", mean, PlainPeriod)
	}
	if o.stats.Batches < 30 {
		t.Fatalf("only %d batches in 400 ticks: the controller stopped trying", o.stats.Batches)
	}
	writes := o.writes
	for i := 1; i < len(writes); i++ {
		if writes[i]-writes[i-1] < 2 {
			t.Fatalf("two batches written %d tick(s) apart: %v", writes[i]-writes[i-1], writes[:min(20, len(writes))])
		}
	}
}

func TestTheLinkGetsSlowerAndTheAimFollows(t *testing.T) {
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 200, func(i int) int {
		if i < 30 {
			return 0
		}
		return 1
	})
	if ctl.Lead() != 1 {
		t.Fatalf("the aim is %d after the link slowed by one tick: %s", ctl.Lead(), o.stats.Line())
	}
	if o.stats.Retimes != 1 {
		t.Fatalf("%d re-aims for one change of the link: %s", o.stats.Retimes, o.stats.Line())
	}
	allGaps(t, "after the change", tail(o.gaps(), 25), Period)
}

func TestJitterBetweenOneAndTwoLateStaysAimedAtOneAndGainsWhatItCan(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	late := func(int) int {
		if rng.Float64() < 0.65 {
			return 1
		}
		return 2
	}
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 3000, late)
	mean := meanInt(o.gaps())
	if mean >= float64(PlainPeriod) || mean > 3.7 {
		t.Fatalf("mean gap %.2f with 65%% of batches one late and the rest two: want clearly under the plain 4 and about 3.5 (stats: %s)", mean, o.stats.Line())
	}
	if ctl.Lead() != 1 {
		t.Fatalf("aim %d, want 1 (the commoner delay)", ctl.Lead())
	}
}

func TestJitterAroundAnOnTimeLinkRecoversFromEveryLateBatch(t *testing.T) {
	// 70% of batches on time, the rest one or two ticks late: the reactive rule keeps writing after every swing.
	rng := rand.New(rand.NewSource(7))
	late := func(int) int {
		switch r := rng.Float64(); {
		case r < 0.70:
			return 0
		case r < 0.90:
			return 1
		default:
			return 2
		}
	}
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 3000, late)
	mean := meanInt(o.gaps())
	if mean >= float64(PlainPeriod) || mean < float64(Period) {
		t.Fatalf("mean roll gap %.2f, want between %d and %d", mean, Period, PlainPeriod)
	}
	if mean > 3.5 {
		t.Fatalf("mean roll gap %.2f with 70%% of batches on time: should recover from every late one", mean)
	}
	if ctl.Lead() != 0 {
		t.Fatalf("a mostly on-time link was aimed %d early", ctl.Lead())
	}
}

func TestTheOpeningBatchIsFollowedThreeTicksLaterWhateverTheLink(t *testing.T) {
	// Once the link's delay is known the second batch is written 3 ticks after the opening one: the first swing comes 2
	// ticks after the opening batch is read, and the batch for it is written `delay` ticks before that.
	for _, d := range []int{1, 2} {
		ctl := New(DefaultSettings())
		run(ctl, 100, 60, fixed(d)) // learns the delay
		o := run(ctl, 300, 40, fixed(d))
		if len(o.writes) < 2 || o.writes[0] != 300 || o.writes[1] != 303 {
			t.Fatalf("delay %d: batches written after ticks %v, want 300 then 303", d, o.writes[:min(4, len(o.writes))])
		}
		if o.rolls[0] != 300+1+d+2 {
			t.Fatalf("delay %d: first roll at %d, want %d", d, o.rolls[0], 300+1+d+2)
		}
	}
}

func TestALostBatchIsReAimedNotAStop(t *testing.T) {
	// One batch in four never reaches the engine. The controller never gives up: it keeps writing and keeps most of
	// the gain.
	n := 0
	ctl := New(DefaultSettings())
	o := run(ctl, 100, 2000, func(int) int {
		n++
		if n%4 == 0 {
			return -1
		}
		return 1
	})
	if o.stats.Lost == 0 {
		t.Fatalf("no batch was counted lost: %s", o.stats.Line())
	}
	if mean := meanInt(o.gaps()); mean > 3.7 {
		t.Fatalf("mean gap %.2f with a quarter of the batches lost: the controller should keep going (%s)", mean, o.stats.Line())
	}
}

func TestNoRunOfLostBatchesSwitchesTheControllerOff(t *testing.T) {
	// Nothing is ever read, so no chop gets going; each chop writes its opening batch and the runner starts another.
	ctl := New(DefaultSettings())
	for i := 0; i < 60; i++ {
		run(ctl, 100+i*50, 20, fixed(-1))
		if !ctl.BeginChop(100 + i*50 + 40) {
			t.Fatalf("no opening batch asked for after %d lost batches", i+1)
		}
	}
	if ctl.Stats().Lost == 0 {
		t.Fatal("nothing counted lost")
	}
}

// ---- one batch in flight ----

func TestNoSecondBatchWhileOneIsInFlight(t *testing.T) {
	for _, lead := range []int{0, 1, 2} {
		ctl := New(DefaultSettings())
		ctl.lead = lead
		ctl.BeginChop(10)
		ctl.Sent(10)
		for n := 11; n <= 13; n++ {
			// Swings, and in every tick that could be the one a batch is due in, while the batch is unconfirmed.
			if ctl.Observe(Obs{N: n, Anim: 867}) {
				t.Fatalf("lead %d: asked for a batch at tick %d with one still unconfirmed", lead, n)
			}
			if !ctl.InFlight() {
				t.Fatalf("lead %d: the batch in flight was forgotten at tick %d", lead, n)
			}
		}
	}
	// Confirmed on time, then the next swing triggers a batch.
	ctl := New(DefaultSettings())
	ctl.BeginChop(10)
	ctl.Sent(10)
	ctl.Observe(Obs{N: 11, Anim: FletchAnim})
	if ctl.InFlight() {
		t.Fatal("a confirmed batch is still in flight")
	}
	if !ctl.Observe(Obs{N: 13, Anim: 867}) {
		t.Fatal("a swing after the confirmation did not trigger a batch")
	}
}

// The lab's yew camp: the last tree fell with the batch written after its final swing still on its way, the next chop
// began in the same tick on an adjacent tree, and its opening batch was read in the same tick as the first. An opening
// batch must wait for the one in flight.
func TestNoOpeningBatchWhileThePreviousChopsBatchIsStillInFlight(t *testing.T) {
	ctl := New(DefaultSettings())
	if !ctl.BeginChop(10) {
		t.Fatal("no opening batch for the first chop")
	}
	ctl.Sent(10) // the batch the chop wrote after its last swing
	if ctl.BeginChop(10) {
		t.Fatal("an opening batch was asked for in the tick the previous chop's batch was written")
	}
	if ctl.BeginChop(13) {
		t.Fatal("an opening batch was asked for before the previous batch could have been read and confirmed")
	}
	if !ctl.BeginChop(14) {
		t.Fatal("a batch nobody watched for three ticks past its read tick blocked every later chop")
	}
	if ctl.Stats().Lost != 0 || ctl.Stats().OnTime != 0 {
		t.Fatalf("a batch nobody watched was counted: %s", ctl.Stats().Line())
	}
}

func TestThePreviousChopsBatchIsConfirmedByTheNextChopsTickFeed(t *testing.T) {
	ctl := New(DefaultSettings())
	ctl.BeginChop(10)
	ctl.Sent(10)
	if ctl.BeginChop(10) {
		t.Fatal("an opening batch beside one in flight")
	}
	// The new chop's feed sees the fletching animation in the tick the old batch was read in.
	ctl.Observe(Obs{N: 11, Anim: FletchAnim})
	if got := ctl.Stats(); got.OnTime != 1 || got.Lost != 0 {
		t.Fatalf("the old batch was not confirmed on time: %s", got.Line())
	}
	if !ctl.Observe(Obs{N: 12, Anim: 867}) {
		t.Fatal("the first swing after the confirmation did not trigger a batch")
	}
}

func TestSilenceAboutACarriedBatchIsNotALoss(t *testing.T) {
	ctl := New(DefaultSettings())
	ctl.BeginChop(10)
	ctl.Sent(10)
	// The next chop begins after the batch's read tick (11) but inside its confirmation window: whatever animation
	// confirmed it came before this chop's feed began, so quiet ticks say nothing about it.
	if ctl.BeginChop(11) {
		t.Fatal("an opening batch beside one that may not have been read")
	}
	for n := 12; n <= 20; n++ {
		ctl.Observe(Obs{N: n, Anim: NoAnim})
	}
	if got := ctl.Stats(); got.Lost != 0 {
		t.Fatalf("a batch the new chop never watched was counted lost: %s", got.Line())
	}
	if !ctl.Observe(Obs{N: 21, Anim: 867}) {
		t.Fatal("the next swing did not trigger a batch once the carried one was cleared")
	}
}

// fakeClock is a millisecond clock the tests move by hand.
type fakeClock struct{ ms int64 }

func (f *fakeClock) now() int64 { return f.ms }

// A client that stalls for seconds is told the ticks it missed in a burst. Counting those ticks as the time a batch had to
// arrive in declared it lost the moment it was written and let a second batch out beside it (the lab's crowded-oak leak
// under load: two batches read in one tick, the second tree click a tick late, the carrier fletched). With the clock a
// batch is lost only when 2.4 s have passed as well.
func TestATickBurstNeitherLosesABatchNorLetsASecondOneOut(t *testing.T) {
	clk := &fakeClock{}
	ctl := New(DefaultSettings())
	ctl.SetClock(clk.now)
	ctl.BeginChop(10)
	clk.ms = 1000
	ctl.Sent(10) // written late, after a stall
	for n := 11; n <= 15; n++ {
		clk.ms += 15 // the missed PLAYER_INFOs arrive back to back
		if ctl.Observe(Obs{N: n, Anim: 867}) {
			t.Fatalf("a second batch was asked for at tick %d with the first only %d ms old", n, clk.ms-1000)
		}
	}
	if got := ctl.Stats(); got.Lost != 0 {
		t.Fatalf("a batch %d ms old was counted lost: %s", clk.ms-1000, got.Line())
	}
	clk.ms = 1000 + 2400
	if ctl.Observe(Obs{N: 16, Anim: NoAnim}) {
		t.Fatal("asked for a batch with no swing to follow")
	}
	if got := ctl.Stats(); got.Lost != 1 {
		t.Fatalf("a batch 2.4 s and 6 ticks old was not counted lost: %s", got.Line())
	}
	clk.ms += 600
	if !ctl.Observe(Obs{N: 17, Anim: 867}) {
		t.Fatal("the first swing after the loss did not ask for a batch")
	}
}

func TestBeginChopWaitsForTheClockAsWellAsTheTicks(t *testing.T) {
	clk := &fakeClock{}
	ctl := New(DefaultSettings())
	ctl.SetClock(clk.now)
	ctl.BeginChop(10)
	ctl.Sent(10)
	clk.ms = 500
	if ctl.BeginChop(15) {
		t.Fatal("an opening batch 5 ticks but only 500 ms after the last: it may still be on its way")
	}
	clk.ms = 2400
	if !ctl.BeginChop(15) {
		t.Fatal("an opening batch was still refused once 2.4 s and the ticks had passed")
	}
	if ctl.Stats().Lost != 0 {
		t.Fatal("the cleared carried batch was counted lost")
	}
}

func TestNonChopAnimationsAreNotRolls(t *testing.T) {
	ctl := New(DefaultSettings())
	ctl.BeginChop(10)
	for _, a := range []int{NoAnim, -1, 829, 1365, 401, 713} {
		if ctl.Observe(Obs{N: 20, Anim: a}) {
			t.Fatalf("animation %d was taken for a roll", a)
		}
	}
	for _, a := range []int{867, 869, 871, 873, 875, 877, 879} {
		c := New(DefaultSettings())
		if !c.Observe(Obs{N: 5, Anim: a}) {
			t.Fatalf("axe animation %d was not taken for a roll", a)
		}
	}
}

func TestDisabledNeverWrites(t *testing.T) {
	ctl := New(Settings{Enabled: false})
	if ctl.BeginChop(1) {
		t.Fatal("a disabled controller asked for an opening batch")
	}
	if ctl.Observe(Obs{N: 5, Anim: 867}) {
		t.Fatal("a disabled controller asked for a batch")
	}
}

func TestSettingsNormaliseAndKeepEnabledAsGiven(t *testing.T) {
	c := New(Settings{Enabled: true})
	d := DefaultSettings()
	if c.cfg.History != d.History || c.cfg.MaxLead != d.MaxLead || c.cfg.ConfirmTicks != d.ConfirmTicks || c.cfg.LostAfterMS != d.LostAfterMS || !c.cfg.Enabled {
		t.Fatalf("zero fields did not take their defaults: %+v", c.cfg)
	}
	if New(Settings{}).cfg.Enabled {
		t.Fatal("a zero Settings must stay disabled")
	}
	if got := New(Settings{Enabled: true, MaxLead: 9}).cfg.MaxLead; got != 3 {
		t.Fatalf("MaxLead 9 normalised to %d, want the cap 3", got)
	}
}

// A slow tree is not a stuck one: an axe animation, or the fletch animation that replaces it in a batch's tick, seen
// within SwingFresh ticks counts as chopping; anything else, or anything older, does not.
func TestSwingingNeedsAFreshChopOrFletchAnimation(t *testing.T) {
	for _, c := range []struct {
		name         string
		seq, at, now int
		want         bool
	}{
		{"rune axe this tick", 867, 100, 100, true},
		{"bronze axe a swing ago", 879, 96, 100, true},
		{"the fletch animation of a batch tick", FletchAnim, 99, 100, true},
		{"exactly two plain periods old", 867, 92, 100, true},
		{"one tick past the window", 867, 91, 100, false},
		{"the reset a felling swing shows", -1, 100, 100, false},
		{"no animation yet", -1, 0, 100, false},
		{"a walking animation", 819, 100, 100, false},
		{"an animation from a later tick", 867, 105, 100, false},
	} {
		if got := Swinging(c.seq, c.at, c.now); got != c.want {
			t.Errorf("%s: Swinging(%d, %d, %d) = %v, want %v", c.name, c.seq, c.at, c.now, got, c.want)
		}
	}
	for _, a := range []int{867, 869, 871, 873, 875, 877, 879} {
		if !Swinging(a, 10, 12) {
			t.Errorf("axe animation %d is not a swing", a)
		}
	}
}
