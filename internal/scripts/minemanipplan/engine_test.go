package minemanipplan

import (
	"fmt"
	"math"
	"testing"

	"rsb0t-go/internal/scripts/chopmanipplan"
)

// gateGap is actions.ClickGapTicks: the fewest ticks between two clicks on the same rock (internal/actions/clickgate.go). It is
// restated here because this package imports nothing of the client; internal/scripts/essencerunner's tests assert the two agree.
const gateGap = 2

// run is one simulated stretch of mining: a client reacting to the PLAYER_INFO of each tick, the packets it writes after
// tick t read by the engine in tick t+1+lag, the engine of engine.go.
type run struct {
	e       *Engine
	ctl     *chopmanipplan.Controller
	seen    []Seen
	writes  []int // the ticks batches were written after
	reads   []int // the ticks they were read in
	clicks  []int // the ticks plain rock clicks were read in
	answers int
}

type runCfg struct {
	pick       Pickaxe
	manip      bool
	ticks      int
	lag        func(i int) int // ticks after the next tick that the i-th packet group written is read in
	levelEvery int
	// noKit makes the client write batches though the pack holds no knife and no log.
	noKit bool
	// pack runs the real 28-slot pack (a load); without it the pack never fills, for steady-state rates.
	pack bool
}

// resumeWait is the ticks after an answered chat closes that the client waits for the interrupted interaction to swing again
// before it clicks the rock itself.
const resumeWait = 6

func fixedLag(k int) func(int) int { return func(int) int { return k } }

func simulate(c runCfg) *run {
	kit := c.manip && !c.noKit
	e := NewEngine(c.pick, kit)
	e.LevelEvery = c.levelEvery
	e.Unbounded = !c.pack
	if c.lag == nil {
		c.lag = fixedLag(0)
	}
	r := &run{e: e}
	if c.manip {
		s := ControllerSettings(c.pick)
		s.Enabled = true
		r.ctl = chopmanipplan.New(s)
	}
	sched := map[int][]Event{}
	groups := 0
	write := func(after int, evs []Event, batch bool) {
		at := after + 1 + c.lag(groups)
		groups++
		sched[at] = append(sched[at], evs...)
		if batch {
			r.writes = append(r.writes, after)
			r.reads = append(r.reads, at)
		} else {
			r.clicks = append(r.clicks, at)
		}
	}
	open := func(after int) {
		if r.ctl != nil && r.ctl.BeginChop(after) {
			write(after, Batch, true)
			r.ctl.Sent(after)
			return
		}
		write(after, []Event{ClickRock}, false)
	}
	open(0)
	prev, pressed := ModalNone, false
	resumeBy, lastEssence := 0, 0
	for t := 1; t <= c.ticks; t++ {
		s := e.Step(sched[t])
		delete(sched, t)
		r.seen = append(r.seen, s)
		if IsPickAnim(s.Anim) {
			resumeBy = 0 // the interaction the chat interrupted carried on by itself: it needs no click
		}
		switch {
		case s.Modal == ModalChat && !pressed:
			pressed = true
			write(t, []Event{Continue}, false)
			r.answers++
		case prev == ModalChat && s.Modal == ModalNone && pressed:
			pressed = false
			resumeBy = t + resumeWait // the chat we answered is gone; click only if no swing comes back
		}
		if resumeBy != 0 && t >= resumeBy {
			resumeBy = 0
			open(t)
		}
		prev = s.Modal
		roll := s.Essence > lastEssence
		lastEssence = s.Essence
		if r.ctl != nil && r.ctl.Observe(chopmanipplan.Obs{N: t, Anim: s.Anim, Roll: roll}) {
			write(t, Batch, true)
			r.ctl.Sent(t)
		}
	}
	return r
}

func (r *run) gaps() []int {
	var g []int
	for i := 1; i < len(r.e.Rolls); i++ {
		g = append(g, r.e.Rolls[i]-r.e.Rolls[i-1])
	}
	return g
}

func (r *run) maxEvents() int {
	m := 0
	for _, n := range r.e.Events {
		m = max(m, n)
	}
	return m
}

func perHundred(rolls, ticks int) float64 { return 100 * float64(rolls) / float64(ticks) }

func allEqual(t *testing.T, label string, got []int, want int) {
	t.Helper()
	for i, g := range got {
		if g != want {
			t.Fatalf("%s: gap %d is %d, want %d (gaps %v)", label, i, g, want, got[:min(len(got), 14)])
		}
	}
}

// ---- the content, tick by tick ----

func TestPlainEssenceRollsAtRatePlusOneForEveryPickaxe(t *testing.T) {
	// mining.rs2:65-69 arms the varp to +rate on the first click, 107-112 re-arms it in every stale tick: the first essence is
	// rate ticks after the click and then one every rate+1.
	for _, p := range Pickaxes {
		r := simulate(runCfg{pick: p, ticks: 600})
		if got := r.e.Rolls[0]; got != 1+p.FirstRoll() {
			t.Errorf("%s: first essence at tick %d, want %d (the click is read in tick 1, rate %d)", p.Item, got, 1+p.FirstRoll(), p.Rate)
		}
		allEqual(t, p.Item+" plain", r.gaps(), p.Period())
		if want := 600 / p.Period(); len(r.e.Rolls) < want-1 || len(r.e.Rolls) > want+1 {
			t.Errorf("%s: %d essence in 600 ticks, want about %d", p.Item, len(r.e.Rolls), want)
		}
	}
}

func TestEveryRollIsAnEssenceAndNothingIsEverLost(t *testing.T) {
	// get_ore_essence (mining.rs2:197-207) has no success roll and no depletion: rolls and essence are the same count.
	r := simulate(runCfg{pick: Pickaxes[5], ticks: 400})
	if r.e.Essence != len(r.e.Rolls) || r.e.Essence == 0 {
		t.Fatalf("%d essence for %d rolls", r.e.Essence, len(r.e.Rolls))
	}
}

func TestTheDocsPickaxeRatesAreTheContentsRatesPlusOne(t *testing.T) {
	// docs/runecraft_p2_2026-10-13.md: "620 / 690 / 780 / 900 / 1,060 / 1,290" an hour, bronze to rune, from 27 essence a load
	// at (rate+1) ticks and 45 ticks around it. pickaxes.obj: mining_rate 7, 6, 5, 4, 3, 2.
	doc := map[string]float64{"Bronze pickaxe": 620, "Iron pickaxe": 690, "Steel pickaxe": 780, "Mithril pickaxe": 900, "Adamant pickaxe": 1060, "Rune pickaxe": 1290}
	rate := map[string]int{"Bronze pickaxe": 7, "Iron pickaxe": 6, "Steel pickaxe": 5, "Mithril pickaxe": 4, "Adamant pickaxe": 3, "Rune pickaxe": 2}
	for _, p := range Pickaxes {
		if p.Rate != rate[p.Item] {
			t.Errorf("%s: mining_rate %d, content says %d", p.Item, p.Rate, rate[p.Item])
		}
		// The doc's figure counts the first essence a tick late (27 x (rate+1)); the exact first roll is rate ticks on.
		docTicks := 27*p.Period() + OverheadTicks
		got := EssencePerHour(p, false)
		if math.Abs(got-doc[p.Item])/doc[p.Item] > 0.01 {
			t.Errorf("%s: %.1f an hour, the doc says %.0f", p.Item, got, doc[p.Item])
		}
		if math.Abs(27/float64(docTicks)*6000-doc[p.Item])/doc[p.Item] > 0.005 {
			t.Errorf("%s: the doc's own formula gives %.1f, not %.0f", p.Item, 27/float64(docTicks)*6000, doc[p.Item])
		}
	}
}

// ---- the manipulation ----

func TestABatchInEveryStaleTickRollsEveryThreeTicksFromTheSecondForEveryPickaxeThatGains(t *testing.T) {
	for _, p := range Pickaxes {
		if !p.Worth() {
			continue
		}
		r := simulate(runCfg{pick: p, manip: true, ticks: 600})
		if got := r.e.Rolls[0]; got != 1+FirstRollManip {
			t.Errorf("%s: first essence at tick %d, want %d (the opening batch is read in tick 1 and re-arms the stale varp to +2)", p.Item, got, 1+FirstRollManip)
		}
		allEqual(t, p.Item+" with batches", r.gaps(), ManipPeriod)
		st := r.ctl.Stats()
		if st.Lost != 0 || st.Late1 != 0 || st.LateMore != 0 || st.Early != 0 {
			t.Errorf("%s: an on-time run was not clean: %s", p.Item, st.Line())
		}
		if r.e.Leaks != 0 {
			t.Errorf("%s: %d carrier log(s) were fletched", p.Item, r.e.Leaks)
		}
	}
}

func TestEssencePerHundredTicksWithAndWithoutTheBatch(t *testing.T) {
	// The measured table (printed with -v): essence per 100 ticks over 3,000 ticks, no level-ups, no latency.
	t.Logf("%-16s %6s %6s %6s %12s", "pickaxe", "rate", "plain", "batch", "x (theory)")
	for _, p := range Pickaxes {
		plain := simulate(runCfg{pick: p, ticks: 3000})
		manip := simulate(runCfg{pick: p, manip: true, ticks: 3000})
		pp, mp := perHundred(len(plain.e.Rolls), 3000), perHundred(len(manip.e.Rolls), 3000)
		if !p.Worth() {
			// A rune pickaxe's own period is the floor: the controller is not run for it, and one that is is no faster.
			if mp > pp*1.02 {
				t.Errorf("%s: %.2f with batches against %.2f plain: a floor-period pickaxe cannot gain", p.Item, mp, pp)
			}
			t.Logf("%-16s %6d %6.2f %6.2f %12s", p.Item, p.Rate, pp, mp, "1.00 (none)")
			continue
		}
		ratio := mp / pp
		t.Logf("%-16s %6d %6.2f %6.2f %6.2f (%.2f)", p.Item, p.Rate, pp, mp, ratio, p.Gain())
		if math.Abs(ratio-p.Gain())/p.Gain() > 0.03 {
			t.Errorf("%s: gain %.3f against the theory %.3f", p.Item, ratio, p.Gain())
		}
		if mp < 33.0 || mp > 33.5 {
			t.Errorf("%s: %.2f essence per 100 ticks with batches, want 33.3 whatever the pickaxe", p.Item, mp)
		}
	}
}

func TestALoadOfEssenceWithAndWithoutTheKit(t *testing.T) {
	// The kit costs two pack slots: 25 essence a load against 27, and MineTicks/LoadTicks/EssencePerHour agree with the engine.
	t.Logf("%-16s %9s %9s %9s %9s", "pickaxe", "plain ticks", "kit ticks", "plain /h", "kit /h")
	for _, p := range Pickaxes {
		plain := simulate(runCfg{pick: p, ticks: 400, pack: true})
		wantPlain := LoadSize(true, false)
		if got := plain.e.Rolls[wantPlain-1] - 1; got != MineTicks(p, wantPlain, false) {
			t.Errorf("%s: %d ticks to the %dth essence plain, MineTicks says %d", p.Item, got, wantPlain, MineTicks(p, wantPlain, false))
		}
		if !p.Worth() {
			t.Logf("%-16s %9d %9s %9.0f %9s", p.Item, MineTicks(p, wantPlain, false), "-", EssencePerHour(p, false), "(no kit)")
			continue
		}
		manip := simulate(runCfg{pick: p, manip: true, ticks: 400, pack: true})
		wantKit := LoadSize(true, true)
		if wantKit != 25 || wantPlain != 27 {
			t.Fatalf("loads %d and %d, want 25 and 27", wantKit, wantPlain)
		}
		if got := manip.e.Rolls[wantKit-1] - 1; got != MineTicks(p, wantKit, true) {
			t.Errorf("%s: %d ticks to the %dth essence with batches, MineTicks says %d", p.Item, got, wantKit, MineTicks(p, wantKit, true))
		}
		if EssencePerHour(p, true) <= EssencePerHour(p, false) {
			t.Errorf("%s: %.0f an hour with the kit against %.0f without: a pickaxe that gains must pay for its two slots", p.Item, EssencePerHour(p, true), EssencePerHour(p, false))
		}
		t.Logf("%-16s %9d %9d %9.0f %9.0f", p.Item, MineTicks(p, wantPlain, false), MineTicks(p, wantKit, true), EssencePerHour(p, false), EssencePerHour(p, true))
	}
	// Past bronze the pickaxe stops mattering: every tier that gains makes the same 1,260 an hour with the kit.
	for _, p := range Pickaxes {
		if p.Worth() {
			if got := EssencePerHour(p, true); math.Abs(got-1260) > 5 {
				t.Errorf("%s: %.0f an hour with the kit, want about 1,260 whatever the pickaxe", p.Item, got)
			}
		}
	}
	// And the rune pickaxe without a kit (1,290 by the doc) is still the best way to mine.
	if best := EssencePerHour(Pickaxes[0], false); best <= EssencePerHour(Pickaxes[1], true) {
		t.Errorf("rune %.0f an hour is not above an adamant kit's %.0f", best, EssencePerHour(Pickaxes[1], true))
	}
}

func TestABatchWithoutAKitIsJustAClick(t *testing.T) {
	// No knife or no log: the knife-on-log opens nothing, Make 1 and CLOSE_MODAL do nothing, and the click is read: the plain
	// timer. (The runner never writes a batch without a kit; this is what one costs if it does.)
	p := Pickaxes[5]
	r := simulate(runCfg{pick: p, manip: true, noKit: true, ticks: 400})
	if got := r.e.Rolls[0]; got != 1+p.FirstRoll() {
		t.Errorf("first essence at tick %d, want %d: the batch moved the varp without a carrier", got, 1+p.FirstRoll())
	}
	allEqual(t, "no kit", r.gaps(), p.Period())
}

func TestABatchReadInAnyOtherTickMovesNothing(t *testing.T) {
	// cut_logs.rs2:83 only re-arms a STALE varp. A batch in the roll tick or in the middle of the wait finds it armed, and
	// the click it carries only restarts the interaction (the varp keeps its value), so the roll is where it was.
	p := Pickaxes[5]
	e := NewEngine(p, true)
	e.Step([]Event{ClickRock}) // tick 1: armed to 1+7
	for i := 0; i < 3; i++ {
		e.Step(nil)
	}
	before := e.Delay
	e.Step(Batch) // tick 5, mid-wait
	if e.Delay != before {
		t.Fatalf("a batch mid-wait moved the varp from %d to %d", before, e.Delay)
	}
	for e.Essence == 0 {
		e.Step(nil)
	}
	if e.Rolls[0] != 1+p.FirstRoll() {
		t.Fatalf("first essence at %d, want %d", e.Rolls[0], 1+p.FirstRoll())
	}
	if e.Leaks != 0 {
		t.Fatalf("%d leaks", e.Leaks)
	}
}

func TestTheFletchNeverRunsWhenTheBatchLandsWhole(t *testing.T) {
	for _, lag := range []int{0, 1, 2} {
		r := simulate(runCfg{pick: Pickaxes[5], manip: true, ticks: 900, lag: fixedLag(lag)})
		if r.e.Leaks != 0 || !r.e.Kit {
			t.Errorf("lag %d: %d leaks, kit kept %v", lag, r.e.Leaks, r.e.Kit)
		}
	}
}

func TestALinkThatDeliversOneOrTwoTicksLateIsAimedEarlyAndKeepsThePeriodFromTheFirstTierOn(t *testing.T) {
	// chopmanipplan's lead logic (a batch written k ticks before the roll it follows lands in the stale tick) over the mining
	// timer. Bronze is the hard case: its unprimed period is 8, so a batch that is not confirmed is aimed 8 ticks on.
	for _, lag := range []int{1, 2} {
		r := simulate(runCfg{pick: Pickaxes[5], manip: true, ticks: 900, lag: fixedLag(lag)})
		if got := r.ctl.Lead(); got != lag {
			t.Errorf("lag %d: aim %d: %s", lag, got, r.ctl.Stats().Line())
		}
		g := r.gaps()
		allEqual(t, fmt.Sprintf("lag %d steady state", lag), g[len(g)-80:], ManipPeriod)
		if mean := meanOf(g); mean > 3.15 {
			t.Errorf("lag %d: mean gap %.2f, want about 3 (%v)", lag, mean, g[:min(len(g), 16)])
		}
		if r.e.Leaks != 0 {
			t.Errorf("lag %d: %d carrier log(s) fletched", lag, r.e.Leaks)
		}
	}
}

func meanOf(v []int) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0
	for _, x := range v {
		s += x
	}
	return float64(s) / float64(len(v))
}

func TestAUselessLinkCostsNothingAgainstPlain(t *testing.T) {
	// A batch that arrives after the stale tick moves nothing. Even with a link so slow that none ever lands in time the
	// account mines at its own period, not below it.
	p := Pickaxes[5]
	plain := simulate(runCfg{pick: p, ticks: 1200})
	late := simulate(runCfg{pick: p, manip: true, ticks: 1200, lag: fixedLag(4)})
	if got, base := len(late.e.Rolls), len(plain.e.Rolls); got < base-2 {
		t.Errorf("%d essence in 1200 ticks on a useless link against %d plain", got, base)
	}
	if late.e.Leaks != 0 {
		t.Errorf("%d carrier log(s) fletched", late.e.Leaks)
	}
}

// ---- how many packets, and how often ----

func TestClickRateStaysUnderTheEnginesFiveAndTheGate(t *testing.T) {
	for _, p := range Pickaxes {
		if !p.Worth() {
			continue
		}
		for _, lag := range []int{0, 1, 2, 3} {
			r := simulate(runCfg{pick: p, manip: true, ticks: 1500, lag: fixedLag(lag)})
			if m := r.maxEvents(); m > BatchEvents {
				t.Errorf("%s lag %d: %d events read in one tick, a batch is %d and the engine reads %d", p.Item, lag, m, BatchEvents, EngineEventsPerTick)
			}
			// One batch in flight: no two batches are read closer than the controller's stale-tick spacing, and never
			// closer than the click gate's.
			for i := 1; i < len(r.reads); i++ {
				if d := r.reads[i] - r.reads[i-1]; d < gateGap {
					t.Errorf("%s lag %d: batches read %d tick(s) apart at %d (gate %d)", p.Item, lag, d, r.reads[i], gateGap)
				}
			}
			// The rock is clicked once per batch: at most one click per ManipPeriod ticks in steady state.
			if per := float64(len(r.reads)) / 1500 * 100; per > 100.0/ManipPeriod+1 {
				t.Errorf("%s lag %d: %.1f batches (rock clicks) per 100 ticks, the bound is %.1f", p.Item, lag, per, 100.0/ManipPeriod+1)
			}
			// Packets per 100 ticks: 4 per batch, at most 1.34 batches.
			total := 0
			for _, n := range r.e.Events {
				total += n
			}
			if per := float64(total) / 1500 * 100; per > BatchEvents*(100.0/ManipPeriod+1) {
				t.Errorf("%s lag %d: %.1f packets per 100 ticks", p.Item, lag, per)
			}
		}
	}
}

func TestPlainMiningClicksOnceForAWholeLoad(t *testing.T) {
	r := simulate(runCfg{pick: Pickaxes[5], ticks: 1500})
	if len(r.clicks) != 1 || len(r.reads) != 0 {
		t.Fatalf("plain mining clicked %d times and wrote %d batches in 1500 ticks, want one click", len(r.clicks), len(r.reads))
	}
}

// ---- chats and a full pack ----

func TestALevelUpChatIsOpenedTheTickAfterTheRollAndBlocksTheInteractionUntilClosed(t *testing.T) {
	p := Pickaxes[5]
	e := NewEngine(p, false)
	e.LevelEvery = 1
	e.Step([]Event{ClickRock})
	var chatAt, rollAt int
	for i := 0; i < 40 && chatAt == 0; i++ {
		s := e.Step(nil)
		if e.Essence == 1 && rollAt == 0 {
			rollAt = s.N
		}
		if s.Modal == ModalChat {
			chatAt = s.N
		}
	}
	if chatAt != rollAt+1 {
		t.Fatalf("essence at tick %d, level-up chat at %d: the engine queue opens it in the next tick", rollAt, chatAt)
	}
	for i := 0; i < 30; i++ {
		e.Step(nil)
	}
	if e.Essence != 1 {
		t.Fatalf("%d essence with the chat left open: a chat must block the interaction (canAccess)", e.Essence)
	}
	e.Step([]Event{Continue})
	for i := 0; i < 20 && e.Essence < 2; i++ {
		e.Step(nil)
	}
	if e.Essence < 2 {
		t.Fatalf("mining did not resume after Continue")
	}
}

func TestLevelUpsCostABatchRunAlmostNothingAndNeverStopIt(t *testing.T) {
	// Low Mining: a level-up every 5 essence. The batch read in the tick the chat opens has already moved the varp, so the
	// Continue written when the chat is seen (read the tick after) loses nothing; the re-click after it is an opening batch.
	for _, p := range Pickaxes {
		if !p.Worth() {
			continue
		}
		plain := simulate(runCfg{pick: p, ticks: 1500, levelEvery: 5})
		manip := simulate(runCfg{pick: p, manip: true, ticks: 1500, levelEvery: 5})
		if manip.e.Chats < 15 || manip.answers < manip.e.Chats-1 {
			t.Fatalf("%s: %d chats, %d answered", p.Item, manip.e.Chats, manip.answers)
		}
		pp, mp := perHundred(len(plain.e.Rolls), 1500), perHundred(len(manip.e.Rolls), 1500)
		if mp < pp*0.99 {
			t.Errorf("%s: %.2f per 100 ticks with batches and level-ups against %.2f plain", p.Item, mp, pp)
		}
		// The most a chat can cost the batch run is the gap between two rolls it spans: never more than a plain period.
		for i, g := range manip.gaps() {
			if g > p.Period()+ManipPeriod {
				t.Errorf("%s: gap %d between two essence is %d ticks across a level-up", p.Item, i, g)
				break
			}
		}
		if manip.e.Leaks != 0 {
			t.Errorf("%s: %d carrier log(s) fletched across level-ups", p.Item, manip.e.Leaks)
		}
		t.Logf("%-16s level-up every 5 essence: %.2f per 100 ticks plain, %.2f with batches (%d chats)", p.Item, pp, mp, manip.e.Chats)
	}
}

func TestAFullPackEndsTheInteractionWithOneBoxAndAskForNothingMore(t *testing.T) {
	p := Pickaxes[5]
	// The 25th essence is at tick 1+2+3*24 = 75; the next tick opens the box.
	r := simulate(runCfg{pick: p, manip: true, ticks: 76, pack: true})
	if want := LoadSize(true, true); r.e.Essence != want {
		t.Fatalf("%d essence in a full pack with the kit, want %d", r.e.Essence, want)
	}
	if r.e.Boxes != 1 {
		t.Fatalf("%d message boxes, want the one the next firstswing/continue opens", r.e.Boxes)
	}
	if last := r.e.Rolls[len(r.e.Rolls)-1]; last != 1+MineTicks(p, 25, true) {
		t.Fatalf("last essence at %d, want %d", last, 1+MineTicks(p, 25, true))
	}
}

func TestASlowAnswerToALevelUpCostsAtMostOneStalePeriodAndStillBeatsPlain(t *testing.T) {
	// A Continue read two or more ticks after the chat opens finds the varp stale: the interrupted interaction re-arms it to
	// +rate itself (mining_continue), and the next roll is a plain period away. That is the worst a chat can do to a batch run,
	// and the run is back on the 3-tick cadence from the roll after it.
	for _, p := range []Pickaxe{Pickaxes[5], Pickaxes[3], Pickaxes[1]} {
		for _, lag := range []int{1, 2, 3} {
			plain := simulate(runCfg{pick: p, ticks: 1500, levelEvery: 8, lag: fixedLag(lag)})
			manip := simulate(runCfg{pick: p, manip: true, ticks: 1500, levelEvery: 8, lag: fixedLag(lag)})
			pp, mp := perHundred(len(plain.e.Rolls), 1500), perHundred(len(manip.e.Rolls), 1500)
			if mp <= pp {
				t.Errorf("%s lag %d: %.2f per 100 ticks with batches and level-ups against %.2f plain", p.Item, lag, mp, pp)
			}
			worst := 0
			for _, g := range manip.gaps() {
				worst = max(worst, g)
			}
			if worst > p.Period()+ManipPeriod {
				t.Errorf("%s lag %d: a gap of %d ticks between two essence across a level-up (plain period %d)", p.Item, lag, worst, p.Period())
			}
			if manip.e.Leaks != 0 {
				t.Errorf("%s lag %d: %d carrier log(s) fletched", p.Item, lag, manip.e.Leaks)
			}
		}
	}
}
