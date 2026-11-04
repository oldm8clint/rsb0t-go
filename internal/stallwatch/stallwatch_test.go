package stallwatch

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
)

// Every test drives Assess+Plan on a fake clock in 30-second sweeps, which is what the fleet
// loop does. t0 is well after the fleet grace and the bot's own start grace, unless a test says
// otherwise.

var (
	watchStart = time.Date(2026, 9, 23, 2, 0, 0, 0, time.UTC)
	t0         = watchStart.Add(58 * time.Minute) // Acct188's loot line, 02:58:01
)

const sweep = 30 * time.Second

type sim struct {
	t     *testing.T
	now   time.Time
	cfg   Config
	obs   Obs
	hist  History
	fleet Fleet
	log   []string // every non-empty decision, for failure messages
}

func newSim(t *testing.T) *sim {
	cfg := DefaultConfig()
	cfg.Mode = ModeAct // the ladder under test; the fleet's default is observe (TestDefaultModeIsObserve)
	return &sim{
		t:   t,
		now: t0,
		cfg: cfg,
		obs: Obs{
			Username: "Acct188", Script: "ProgressiveWCFletch", InGame: true, Running: true,
			BotStartedAt: t0.Add(-2 * time.Hour), ConnectedAt: t0.Add(-2 * time.Hour), LastLineAt: t0,
			Print: Fingerprint{TileOK: true, X: 2729, Z: 3485, TotalXP: 1_000_000, PackGen: 40, WornGen: 3, HP: 30, World: 1},
		},
		fleet: Fleet{InGameBots: 299},
	}
}

// tick advances the clock by dt and runs one sweep.
func (s *sim) tick(dt time.Duration) (Assessment, Decision) {
	s.now = s.now.Add(dt)
	a, h := Assess(s.now, s.obs, s.hist, s.cfg, watchStart)
	d, h := Plan(s.now, a, s.obs, h, s.cfg, s.fleet)
	s.hist = h
	if d.Action != ActNone || d.Held != "" || a.Recovered {
		s.log = append(s.log, fmt.Sprintf("+%s %s action=%s held=%q recovered=%v", s.now.Sub(t0), a.Status, d.Action, d.Held, a.Recovered))
	}
	return a, d
}

// runUntil sweeps until a sweep's decision is want (or the limit passes) and returns when.
func (s *sim) runUntil(want Action, limit time.Duration) (time.Duration, Decision) {
	s.t.Helper()
	start := s.now
	for s.now.Sub(start) < limit {
		_, d := s.tick(sweep)
		if d.Action == want {
			return s.now.Sub(start), d
		}
		if d.Action != ActNone && d.Action != ActReport {
			s.t.Fatalf("wanted %s, got %s first; history:\n%v", want, d.Action, s.log)
		}
	}
	s.t.Fatalf("no %s within %s; history:\n%v", want, limit, s.log)
	return 0, Decision{}
}

// never sweeps for d and fails if any intervention (nudge/restart/relogin/give up) comes out.
func (s *sim) never(d time.Duration) (reports int) {
	s.t.Helper()
	end := s.now.Add(d)
	for s.now.Before(end) {
		_, dec := s.tick(sweep)
		switch dec.Action {
		case ActReport:
			reports++
		case ActNone:
		default:
			s.t.Fatalf("intervened (%s) at +%s; history:\n%v", dec.Action, s.now.Sub(t0), s.log)
		}
	}
	return reports
}

func (s *sim) progress() { s.obs.Print.TotalXP += 25 }
func (s *sim) line()     { s.obs.LastLineAt = s.now }

// TestTheStalledFixtureIsNudgedAtStallAfterAndRecovers is the live case: silent after its loot line, the
// emote mesbox open, nothing moving. Nothing happens before StallAfter; at StallAfter the first
// action is the nudge (there is a continue to press), flagged as the first sighting; and the
// progress that follows the nudge closes the ladder as "recovered by nudge". (Not blind here:
// the blind variant, which is caught sooner, is TestTheBlindFixtureIsNudgedAtBlindAfter.)
func TestTheStalledFixtureIsNudgedAtStallAfterAndRecovers(t *testing.T) {
	s := newSim(t)
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	after, d := s.runUntil(ActNudge, 30*time.Minute)
	if after < s.cfg.StallAfter || after > s.cfg.StallAfter+sweep {
		t.Fatalf("nudged after %s, want just past %s", after, s.cfg.StallAfter)
	}
	if !d.FirstSeen {
		t.Fatal("the first action of a stall must carry FirstSeen, so the diagnosis and dump are written")
	}
	// The continue restored the tabs; the bot resumes and says so.
	s.obs.ModalOpen, s.obs.CanContinue = false, false
	s.line()
	s.progress()
	a, d := s.tick(sweep)
	if !a.Recovered || a.RecoveredBy != StepNudged || d.Action != ActNone {
		t.Fatalf("after progress: %+v %+v", a, d)
	}
	if s.hist.Step != StepNone || len(s.hist.Ladders) != 1 {
		t.Fatalf("the ladder closes but is counted: step=%s ladders=%d", s.hist.Step, len(s.hist.Ladders))
	}
}

// TestTheBlindFixtureIsNudgedAtBlindAfter: Acct188 exactly -- tabs 3/4 null behind the mesbox,
// silent, still. Blindness outside any event is caught at BlindAfter, not StallAfter, and the
// first step is still the nudge, because there is a continue to press.
func TestTheBlindFixtureIsNudgedAtBlindAfter(t *testing.T) {
	s := newSim(t)
	s.obs.ModalOpen, s.obs.CanContinue, s.obs.Blind = true, true, true
	after, d := s.runUntil(ActNudge, 30*time.Minute)
	if after < s.cfg.BlindAfter || after > s.cfg.BlindAfter+2*sweep {
		t.Fatalf("nudged after %s, want just past BlindAfter %s", after, s.cfg.BlindAfter)
	}
	if !d.FirstSeen {
		t.Fatal("the first action of a stall must carry FirstSeen")
	}
}

// TestBlindAndLoggingIsStillRelogged is the one exception to "saying so is never acted on": the
// woodcutter family's blind gate logs that it cannot see (first seen, the presses, STILL BLIND),
// and none of that is a wait that ends. Blind with nothing to press, a line every sweep and no
// progress: relogged at BlindAfter, never restarted.
func TestBlindAndLoggingIsStillRelogged(t *testing.T) {
	s := newSim(t)
	s.obs.Blind = true
	start := s.now
	for s.now.Sub(start) < 30*time.Minute {
		s.line()
		_, d := s.tick(sweep)
		if d.Action == ActNone || d.Action == ActReport {
			continue
		}
		if d.Action != ActRelogin {
			t.Fatalf("wanted relogin, got %s; history:\n%v", d.Action, s.log)
		}
		if after := s.now.Sub(start); after < s.cfg.BlindAfter || after > s.cfg.BlindAfter+2*sweep {
			t.Fatalf("relogged after %s, want just past BlindAfter %s", after, s.cfg.BlindAfter)
		}
		if !strings.Contains(d.Why, "blind") {
			t.Fatalf("why = %q", d.Why)
		}
		return
	}
	t.Fatalf("a logging blind account was never relogged; history:\n%v", s.log)
}

// TestBlindButNotForLongOrProgressingIsLeftAlone: blindness that ends before BlindAfter, or a
// blind account that keeps moving (a cutscene, a ship), is not a stall while it keeps logging.
func TestBlindButNotForLongOrProgressingIsLeftAlone(t *testing.T) {
	s := newSim(t)
	s.obs.Blind = true
	for s.now.Sub(t0) < s.cfg.BlindAfter-2*sweep {
		s.line()
		s.tick(sweep)
	}
	s.obs.Blind = false
	for s.now.Sub(t0) < 40*time.Minute {
		s.line()
		if _, d := s.tick(sweep); d.Action != ActNone && d.Action != ActReport {
			t.Fatalf("intervened (%s) on a talkative account that can see again; history:\n%v", d.Action, s.log)
		}
	}

	p := newSim(t)
	p.obs.Blind = true
	for p.now.Sub(t0) < 40*time.Minute {
		p.line()
		p.obs.Print.X ^= 1 // moving every sweep
		if _, d := p.tick(sweep); d.Action != ActNone {
			t.Fatalf("acted (%s) on a blind account that is making progress; history:\n%v", d.Action, p.log)
		}
	}
}

// TestBlindAfterZeroDisablesTheBlindPath: with BlindAfter 0 a blind account is judged like any
// other, so a logging one is never acted on and a silent one waits the full StallAfter.
func TestBlindAfterZeroDisablesTheBlindPath(t *testing.T) {
	s := newSim(t)
	s.cfg.BlindAfter = 0
	s.obs.Blind = true
	for s.now.Sub(t0) < 40*time.Minute {
		s.line()
		if _, d := s.tick(sweep); d.Action != ActNone && d.Action != ActReport {
			t.Fatalf("intervened (%s) with the blind path off; history:\n%v", d.Action, s.log)
		}
	}
	q := newSim(t)
	q.cfg.BlindAfter = 0
	q.obs.Blind = true
	if after, _ := q.runUntil(ActRelogin, 30*time.Minute); after < q.cfg.StallAfter {
		t.Fatalf("relogged after %s with the blind path off, before StallAfter", after)
	}
}

// TestMuleAtItsPostIsNeverTouched: silent and motionless for hours is the mule's job.
func TestMuleAtItsPostIsNeverTouched(t *testing.T) {
	s := newSim(t)
	s.obs.Username, s.obs.Script = "mule", "MuleReceiver"
	if reports := s.never(6 * time.Hour); reports != 0 {
		t.Fatalf("the exempt mule was reported %d times", reports)
	}
	if a, _ := s.tick(sweep); a.Status != StatusExempt {
		t.Fatalf("status %s, want exempt", a.Status)
	}
}

func TestExemptByAccountName(t *testing.T) {
	s := newSim(t)
	s.cfg.Exempt = ParseExempt(" MuleReceiver , acct188 ")
	s.never(time.Hour)
}

// TestRestingAccountIsNotStalled: no lines and no movement, but hitpoints rising, including
// after a fight took them below the baseline.
func TestRestingAccountIsNotStalled(t *testing.T) {
	s := newSim(t)
	s.obs.Print.HP = 30
	s.tick(sweep)
	s.obs.Print.HP = 12 // hit hard, then rests silently
	for i := 0; i < 120; i++ {
		if i%4 == 0 && s.obs.Print.HP < 60 {
			s.obs.Print.HP++ // one hitpoint a minute, the game's own regen
		}
		if _, d := s.tick(sweep); d.Action != ActNone {
			t.Fatalf("resting account acted on (%s) at sweep %d", d.Action, i)
		}
	}
}

// TestSayingSoIsNeverActedOn: "still waiting on a random event" every 10s, a donor at the meet,
// a throttled warning -- lines keep coming and nothing moves for two hours. Reported as
// busy-without-progress (once at BusyAfter, then once per BusyEvery), never touched.
func TestSayingSoIsNeverActedOn(t *testing.T) {
	s := newSim(t)
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	var reports []time.Duration
	end := s.now.Add(2 * time.Hour)
	for s.now.Before(end) {
		s.line() // a line on every sweep, like a 10s "still waiting"
		_, d := s.tick(sweep)
		s.line()
		switch d.Action {
		case ActNone:
		case ActReport:
			reports = append(reports, s.now.Sub(t0))
		default:
			t.Fatalf("a talkative account was acted on: %s at +%s", d.Action, s.now.Sub(t0))
		}
	}
	if len(reports) != 2 || reports[0] < s.cfg.BusyAfter || reports[1]-reports[0] < s.cfg.BusyEvery {
		t.Fatalf("busy reports at %v, want two: one at %s and one %s later", reports, s.cfg.BusyAfter, s.cfg.BusyEvery)
	}
}

// TestDonorMeetWaitAndLongestHarmlessSilence: the measured harmless windows -- a donor silent at
// the meet for 5.7 minutes, a fisher silent for 9.2 -- end with a line or progress before
// StallAfter, and are never even reported.
func TestDonorMeetWaitAndLongestHarmlessSilence(t *testing.T) {
	for _, silent := range []time.Duration{5*time.Minute + 42*time.Second, 9*time.Minute + 12*time.Second} {
		s := newSim(t)
		for round := 0; round < 12; round++ {
			if reports := s.never(silent); reports != 0 {
				t.Fatalf("silent %s: reported %d times", silent, reports)
			}
			s.line()
			if round%2 == 0 {
				s.progress()
			}
		}
	}
}

// TestNoModalSkipsTheNudge: with nothing open there is nothing to press, so the ladder starts at
// the restart.
func TestNoModalSkipsTheNudge(t *testing.T) {
	s := newSim(t)
	s.runUntil(ActRestart, 30*time.Minute)
}

// TestBlindAccountIsReloggedNotRestarted is the main session's live finding (2026-09-23 05:20):
// Acct188 re-froze straight after a bot restart, and only a relog brought Acct188, Acct206 and
// Acct216 back. A blind account with nothing open to press goes straight from the stall to the
// relogin -- no restart at any point -- and, if even a fresh login leaves it blind and silent,
// to "needs attention", saying the restart was skipped rather than claiming it was tried.
func TestBlindAccountIsReloggedNotRestarted(t *testing.T) {
	s := newSim(t)
	s.obs.Blind = true
	after, d := s.runUntil(ActRelogin, 30*time.Minute) // runUntil fails on a restart first
	if after < s.cfg.BlindAfter || after > s.cfg.BlindAfter+2*sweep {
		t.Fatalf("relogged after %s, want just past BlindAfter %s", after, s.cfg.BlindAfter)
	}
	if !d.FirstSeen || !strings.Contains(d.Why, "blind") {
		t.Fatalf("the relogin must be the first sighting and say it is for blindness: %+v", d)
	}
	// The relogin: out of game, back with fresh times and its start lines -- and still blind.
	s.obs.InGame = false
	s.tick(sweep)
	s.tick(sweep)
	s.obs.InGame, s.obs.ConnectedAt, s.obs.BotStartedAt = true, s.now, s.now
	s.line()
	back := s.now
	_, d = s.runUntil(ActGiveUp, time.Hour)
	// Still blind after the fresh login: the blind path decides again, but only once the login's
	// own grace is over (StartGrace), never inside it.
	if got := s.now.Sub(back); got < s.cfg.StartGrace || got < s.cfg.BlindAfter {
		t.Fatalf("gave up %s after the relogin, inside the login grace %s or before BlindAfter %s", got, s.cfg.StartGrace, s.cfg.BlindAfter)
	}
	if d.Why != "the relogin did not bring it back (the restart was skipped: it was blind)" {
		t.Fatalf("why = %q", d.Why)
	}
	s.never(s.cfg.GiveUpHold - sweep)
}

// TestBlindWithADialogueIsNudgedThenRelogged: blind with a dialogue open (the mesbox class when
// both earlier layers missed it) is nudged first, and when the nudge does not help the next step
// is the relogin after RecheckAfter, not a restart; a relogin that works closes the ladder.
func TestBlindWithADialogueIsNudgedThenRelogged(t *testing.T) {
	s := newSim(t)
	s.obs.Blind, s.obs.ModalOpen, s.obs.CanContinue = true, true, true
	s.runUntil(ActNudge, 30*time.Minute)
	after, _ := s.runUntil(ActRelogin, 30*time.Minute)
	if after < s.cfg.RecheckAfter || after > s.cfg.RecheckAfter+sweep {
		t.Fatalf("relogin %s after the nudge, want just past %s", after, s.cfg.RecheckAfter)
	}
	s.obs.InGame = false
	s.tick(sweep)
	s.obs.InGame, s.obs.ConnectedAt, s.obs.BotStartedAt = true, s.now, s.now
	s.obs.Blind, s.obs.ModalOpen, s.obs.CanContinue = false, false, false
	s.line()
	s.tick(sweep) // back in game: the first sweep re-baselines (a new session is not progress)
	s.progress()
	a, _ := s.tick(sweep)
	if !a.Recovered || a.RecoveredBy != StepRelogged {
		t.Fatalf("after the relogin worked: %+v", a)
	}
	if s.hist.SkippedRestart {
		t.Fatal("a closed ladder must not carry its skipped restart into the next one")
	}
}

// TestBlindThenReadableFallsBackToTheRestart: an account blind at the nudge but readable by its
// next step is an ordinary stall again, and gets the ordinary restart.
func TestBlindThenReadableFallsBackToTheRestart(t *testing.T) {
	s := newSim(t)
	s.obs.Blind, s.obs.ModalOpen, s.obs.CanContinue = true, true, true
	s.runUntil(ActNudge, 30*time.Minute)
	s.obs.Blind = false
	s.runUntil(ActRestart, 30*time.Minute)
}

// TestFullLadderToGiveUp: a nudge that does not help is followed by a restart only after
// RecheckAfter; the restart's own start lines reset the silence clock, so the relogin needs
// another full StallAfter of silence; and after the relogin, the same again ends in "give up",
// after which nothing more is done for GiveUpHold.
func TestFullLadderToGiveUp(t *testing.T) {
	s := newSim(t)
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	s.runUntil(ActNudge, 30*time.Minute)
	// The nudge did nothing: same silence, still something open (say the latch is stuck).
	after, _ := s.runUntil(ActRestart, 30*time.Minute)
	if after < s.cfg.RecheckAfter || after > s.cfg.RecheckAfter+sweep {
		t.Fatalf("restart %s after the nudge, want just past %s", after, s.cfg.RecheckAfter)
	}
	// The restart: the bot stops (one idle sweep), starts again, writes its start lines.
	s.obs.Running = false
	s.tick(sweep)
	s.obs.Running, s.obs.BotStartedAt = true, s.now
	s.line()
	restartedAt := s.now
	after, _ = s.runUntil(ActRelogin, time.Hour)
	if got := s.now.Sub(restartedAt); got < s.cfg.StallAfter {
		t.Fatalf("relogin %s after the restart, before a full StallAfter of fresh silence", got)
	}
	// The relogin: out of game for a minute, back with fresh connect/start times and lines.
	s.obs.InGame = false
	s.tick(sweep)
	s.tick(sweep)
	s.obs.InGame, s.obs.ConnectedAt, s.obs.BotStartedAt = true, s.now, s.now
	s.line()
	_, d := s.runUntil(ActGiveUp, time.Hour)
	if d.Why == "" {
		t.Fatal("a give-up says why")
	}
	if s.hist.Step != StepGaveUp {
		t.Fatalf("step %s after giving up", s.hist.Step)
	}
	// Held alone for GiveUpHold, silent or not.
	s.never(s.cfg.GiveUpHold - sweep)
}

// TestGiveUpThenRecoversOnItsOwn: Acct185 came back by itself after 63 minutes when a later
// event forced a walk. Progress after a give-up clears the "needs attention" state.
func TestGiveUpThenRecoversOnItsOwn(t *testing.T) {
	s := newSim(t)
	s.cfg.Mode = ModeNudge
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	s.runUntil(ActNudge, 30*time.Minute)
	_, d := s.runUntil(ActGiveUp, 30*time.Minute)
	if d.Why != "nudge mode: the ladder stops at the nudge" {
		t.Fatalf("why = %q", d.Why)
	}
	s.never(40 * time.Minute)
	s.progress()
	a, _ := s.tick(sweep)
	if !a.Recovered || a.RecoveredBy != StepGaveUp || !s.hist.GaveUpAt.IsZero() || s.hist.Step != StepNone {
		t.Fatalf("recovery after a give-up: %+v, hist step %s gaveUpAt %v", a, s.hist.Step, s.hist.GaveUpAt)
	}
}

// TestLaddersPerAccountAreBounded: three ladders in the window, each recovered by its nudge; the
// fourth stall inside the window goes straight to "give up" rather than a fourth ladder.
func TestLaddersPerAccountAreBounded(t *testing.T) {
	s := newSim(t)
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	for i := 0; i < s.cfg.MaxLadders; i++ {
		s.runUntil(ActNudge, time.Hour)
		s.progress()
		s.line()
		if a, _ := s.tick(sweep); !a.Recovered {
			t.Fatalf("ladder %d did not close", i+1)
		}
	}
	_, d := s.runUntil(ActGiveUp, time.Hour)
	if d.Why != "it stalled again after too many ladders in the window" {
		t.Fatalf("why = %q", d.Why)
	}
	if len(s.hist.Ladders) != s.cfg.MaxLadders {
		t.Fatalf("ladders = %d; giving up is not a ladder", len(s.hist.Ladders))
	}
}

// TestCircuitBreaker: twenty accounts stalled at once is a fleet-wide cause. Report once, act on
// nobody -- and act normally again once the count falls.
func TestCircuitBreaker(t *testing.T) {
	s := newSim(t)
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	s.fleet.Stalled = 20
	s.fleet.StalledByScript = map[string]int{"ProgressiveWCFletch": 7, "ProgressiveFisher": 7, "ScarabSwarm": 6}
	s.fleet.InGameByScript = map[string]int{"ProgressiveWCFletch": 60, "ProgressiveFisher": 139, "ScarabSwarm": 100}
	for i := 0; i < int((s.cfg.StallAfter+20*time.Minute)/sweep); i++ {
		_, d := s.tick(sweep)
		if d.Action != ActNone && d.Action != ActReport {
			t.Fatalf("acted (%s) with the breaker open", d.Action)
		}
	}
	if s.hist.Step != StepNone || !s.hist.Detected {
		t.Fatalf("breaker: step %s detected %v, want detected and no step", s.hist.Step, s.hist.Detected)
	}
	s.fleet.Stalled = 1
	s.fleet.StalledByScript = map[string]int{"ProgressiveWCFletch": 1}
	if _, d := s.tick(sweep); d.Action != ActNudge || d.FirstSeen {
		t.Fatalf("breaker closed: %+v, want the nudge (not a second first sighting)", d)
	}
}

func TestBreakerThreshold(t *testing.T) {
	cfg := DefaultConfig()
	// Stalls spread over many scripts (one each), so no single script trips its own breaker.
	spread := func(n int) map[string]int {
		m := map[string]int{}
		for i := 0; i < n; i++ {
			m[fmt.Sprintf("script%d", i)] = 1
		}
		return m
	}
	for _, c := range []struct {
		stalled, inGame int
		open            bool
	}{
		{4, 50, false}, {5, 50, true}, // the floor of five
		{8, 299, false}, {9, 299, true}, // 3% of 299 rounds up to 9
		{0, 0, false},
	} {
		if got := BreakerOpen(Fleet{Stalled: c.stalled, InGameBots: c.inGame, StalledByScript: spread(c.stalled)}, cfg); got != c.open {
			t.Errorf("stalled %d of %d: open=%v, want %v", c.stalled, c.inGame, got, c.open)
		}
	}
}

// TestOneScriptsSystemicStallHoldsOnlyThatScript: ten of a hundred ScarabSwarm accounts silent at
// once (its stage-2 sailing class) trips ScarabSwarm's own breaker and nothing else -- a
// woodcutter stalled at the same time is still nudged. Two scripts tripping at once is fleet-wide.
func TestOneScriptsSystemicStallHoldsOnlyThatScript(t *testing.T) {
	cfg := DefaultConfig()
	f := Fleet{
		Stalled: 11, InGameBots: 299,
		StalledByScript: map[string]int{"ScarabSwarm": 10, "ProgressiveWCFletch": 1},
		InGameByScript:  map[string]int{"ScarabSwarm": 100, "ProgressiveWCFletch": 60, "ProgressiveFisher": 139},
	}
	if BreakerOpen(f, cfg) {
		t.Fatal("one script's stalls are not a fleet-wide fault")
	}
	if !ScriptBreakerOpen(f, "ScarabSwarm", cfg) || ScriptBreakerOpen(f, "ProgressiveWCFletch", cfg) {
		t.Fatal("ScarabSwarm's breaker, and only ScarabSwarm's, must be open")
	}
	s := newSim(t)
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	s.fleet = f
	s.runUntil(ActNudge, 30*time.Minute) // the woodcutter is still helped
	sc := newSim(t)
	sc.obs.Script = "ScarabSwarm"
	sc.fleet = f
	_, d := sc.runUntil(ActReport, 30*time.Minute)
	if d.Held != "circuit breaker: too many ScarabSwarm accounts stalled at once" {
		t.Fatalf("held = %q", d.Held)
	}
	sc.never(20 * time.Minute)

	f.StalledByScript["ProgressiveFisher"] = 6
	f.Stalled += 6
	if !BreakerOpen(f, cfg) {
		t.Fatal("two scripts tripping at once is fleet-wide")
	}
}

func TestFleetRateLimits(t *testing.T) {
	s := newSim(t)
	s.obs.ModalOpen = true
	s.fleet.ActionsInWindow = s.cfg.FleetActions
	_, d := s.runUntil(ActReport, 30*time.Minute)
	if d.Held != "fleet-wide action limit" {
		t.Fatalf("held = %q", d.Held)
	}
	s.never(10 * time.Minute) // still held, and reported only once
	s.fleet.ActionsInWindow = 0
	s.runUntil(ActNudge, time.Minute)

	// A relogin waits for the in-flight ones.
	r := newSim(t)
	r.hist.Step, r.hist.StepAt = StepRestarted, t0.Add(-time.Hour)
	r.hist.Detected = true
	r.hist.Ladders = []time.Time{t0.Add(-time.Hour)}
	r.fleet.ReloginsInFlight = r.cfg.MaxRelogins
	for i := 0; i < 40; i++ {
		if _, d := r.tick(sweep); d.Action == ActRelogin {
			t.Fatal("a relogin was started with the in-flight limit reached")
		}
	}
	r.fleet.ReloginsInFlight = 0
	if _, d := r.tick(sweep); d.Action != ActRelogin {
		t.Fatalf("got %s once the relogins finished, want the relogin", d.Action)
	}
}

func TestGrace(t *testing.T) {
	s := newSim(t)
	s.obs.BotStartedAt = t0.Add(-5 * time.Minute) // a bot just (re)started
	if a, _ := s.tick(sweep); a.Status != StatusGrace {
		t.Fatalf("status %s, want grace", a.Status)
	}
	f := newSim(t)
	f.now = watchStart.Add(time.Minute) // the fleet just started
	f.obs.LastLineAt, f.obs.BotStartedAt, f.obs.ConnectedAt = f.now.Add(-time.Hour), f.now.Add(-time.Hour), f.now.Add(-time.Hour)
	for f.now.Before(watchStart.Add(f.cfg.FleetGrace - sweep)) {
		if a, d := f.tick(sweep); a.Status != StatusGrace || d.Action != ActNone {
			t.Fatalf("inside the fleet grace: %s %s", a.Status, d.Action)
		}
	}
}

// TestObserveModeReportsOnceAndNeverActs, and notices a stall that ends by itself.
func TestObserveModeReportsOnceAndNeverActs(t *testing.T) {
	s := newSim(t)
	s.cfg.Mode = ModeObserve
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	if reports := s.never(3 * time.Hour); reports != 1 {
		t.Fatalf("observe mode reported %d times over one stall, want 1", reports)
	}
	s.progress()
	if a, _ := s.tick(sweep); !a.Recovered || a.RecoveredAfter <= 0 {
		t.Fatalf("a reported stall that ends must say so: %+v", a)
	}
}

func TestOffModeDoesNothing(t *testing.T) {
	s := newSim(t)
	s.cfg.Mode = ModeOff
	if reports := s.never(3 * time.Hour); reports != 0 {
		t.Fatalf("off mode reported %d times", reports)
	}
}

// TestNotInGameOrNotRunningIsIdle, and an operator taking over aborts an open ladder.
func TestNotInGameOrNotRunningIsIdle(t *testing.T) {
	s := newSim(t)
	s.runUntil(ActRestart, 30*time.Minute)
	s.obs.Running = false // the operator stopped it and is looking
	for i := 0; i < int(s.cfg.IdleAbort/sweep)+2; i++ {
		if a, d := s.tick(sweep); a.Status != StatusIdle || d.Action != ActNone {
			t.Fatalf("idle: %s %s", a.Status, d.Action)
		}
	}
	if s.hist.Step != StepNone {
		t.Fatalf("an idle spell past IdleAbort must drop the ladder, step is %s", s.hist.Step)
	}
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"off": ModeOff, " Observe": ModeObserve, "NUDGE": ModeNudge, "act": ModeAct} {
		if got, ok := ParseMode(in); !ok || got != want {
			t.Errorf("ParseMode(%q) = %q %v", in, got, ok)
		}
	}
	if _, ok := ParseMode("restart"); ok {
		t.Error("an unknown mode must be refused")
	}
}

// TestChangingScriptStartsAfresh: a different script on the account is not the old ladder.
func TestChangingScriptStartsAfresh(t *testing.T) {
	s := newSim(t)
	s.runUntil(ActRestart, 30*time.Minute)
	s.obs.Script, s.obs.BotStartedAt = "ProgressiveFisher", s.now
	s.line()
	s.tick(sweep)
	if s.hist.Step != StepNone || s.hist.Detected {
		t.Fatalf("new script, old ladder kept: step %s", s.hist.Step)
	}
	if len(s.hist.Ladders) != 1 {
		t.Fatal("but the per-account ladder count survives, so a script swap cannot dodge the limit")
	}
}

// TestDefaultModeIsObserve: a fleet that picks up a new build detects and reports, and acts on
// nothing, until an operator switches the mode (review2, BLOCKING).
func TestDefaultModeIsObserve(t *testing.T) {
	if got := DefaultConfig().Mode; got != ModeObserve {
		t.Fatalf("DefaultConfig().Mode = %q, want %q", got, ModeObserve)
	}
	s := newSim(t)
	s.cfg = DefaultConfig()
	s.obs.ModalOpen, s.obs.CanContinue = true, true
	if reports := s.never(3 * time.Hour); reports != 1 {
		t.Fatalf("the default mode reported %d times over one stall, want exactly 1 and no action", reports)
	}
}

// TestStallAfterClearsTheDonorMeetWait pins the stall threshold against the donor's own meet cap,
// so raising donorplan.MeetTimeoutComing again fails here instead of restarting healthy donors
// (review2, BLOCKING: the cap was raised to exactly the old 10-minute threshold).
func TestStallAfterClearsTheDonorMeetWait(t *testing.T) {
	found := false
	for _, w := range KnownSilentWaits {
		if strings.Contains(w.What, "donorplan.MeetTimeoutComing") {
			found = true
			if w.Max < donorplan.MeetTimeoutComing {
				t.Fatalf("KnownSilentWaits says the donor meet wait is %s, but donorplan.MeetTimeoutComing is %s", w.Max, donorplan.MeetTimeoutComing)
			}
		}
	}
	if !found {
		t.Fatal("KnownSilentWaits has no donorplan.MeetTimeoutComing entry")
	}
	if LongestKnownSilentWait().Max < donorplan.MeetTimeoutComing {
		t.Fatalf("longest known silent wait %s < donor meet cap %s", LongestKnownSilentWait().Max, donorplan.MeetTimeoutComing)
	}
	cfg := DefaultConfig()
	if cfg.StallAfter < donorplan.MeetTimeoutComing+SilenceMargin {
		t.Fatalf("StallAfter %s < donor meet cap %s + margin %s", cfg.StallAfter, donorplan.MeetTimeoutComing, SilenceMargin)
	}
	if cfg.StallAfter < 15*time.Minute {
		t.Fatalf("StallAfter %s, want at least 15 minutes", cfg.StallAfter)
	}
	if cfg.StallAfter < MinStallAfter() {
		t.Fatalf("StallAfter %s below MinStallAfter %s", cfg.StallAfter, MinStallAfter())
	}
}

// TestDonorWaitingOutTheMeetCapIsNeverActedOn is review2's probe 3: a donor told "on its way"
// logs once, stands still and silent for the whole donorplan.MeetTimeoutComing, then gives up.
// At every sweep phase the watchdog must leave it alone; at the old 10-minute threshold 7 of 300
// phases restarted it.
func TestDonorWaitingOutTheMeetCapIsNeverActedOn(t *testing.T) {
	for phase := 0; phase < 300; phase++ {
		s := newSim(t)
		arrived := t0
		s.obs.LastLineAt = arrived.Add(-2 * time.Second) // "says it is on its way", just before arriving
		s.hist = History{Script: s.obs.Script, Seen: true, Last: s.obs.Print, LastProgressAt: arrived}
		giveUp := arrived.Add(donorplan.MeetTimeoutComing + 700*time.Millisecond)
		s.now = arrived.Add(time.Duration(phase) * 100 * time.Millisecond)
		for s.now.Before(giveUp.Add(sweep)) { // up to and including the sweep just after the give-up line
			_, d := s.tick(sweep)
			if d.Action != ActNone {
				t.Fatalf("phase %d: %s at +%s into a %s meet wait; history:\n%v", phase, d.Action, s.now.Sub(arrived), donorplan.MeetTimeoutComing, s.log)
			}
		}
	}
}

// TestTenMinuteThrottledWarningIsNeverActedOn is review2's probe 1: an account that writes one
// throttled warning every 10 minutes (plus its loop's 1.2 s) and never moves is "saying so", and
// at the old 10-minute threshold it was restarted, then relogged.
func TestTenMinuteThrottledWarningIsNeverActedOn(t *testing.T) {
	s := newSim(t)
	gap := 10*time.Minute + 1200*time.Millisecond
	s.hist = History{Script: s.obs.Script, Seen: true, Last: s.obs.Print, LastProgressAt: t0.Add(-time.Hour)}
	nextLine := s.now.Add(gap)
	end := s.now.Add(2 * time.Hour)
	for s.now.Before(end) {
		if !s.now.Add(sweep).Before(nextLine) {
			s.obs.LastLineAt = nextLine
			nextLine = nextLine.Add(gap)
		}
		if _, d := s.tick(sweep); d.Action != ActNone && d.Action != ActReport {
			t.Fatalf("acted (%s) at +%s on an account logging every %s; history:\n%v", d.Action, s.now.Sub(t0), gap, s.log)
		}
	}
}

// TestGivenUpAccountsDoNotHoldTheBreakers is review2's probe 2: accounts the watchdog has given up
// on stay StatusStalled, and used to count toward the breakers for as long as they stayed that
// way. Five given-up WCFletch accounts must not stop a fresh WCFletch stall being nudged.
func TestGivenUpAccountsDoNotHoldTheBreakers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = ModeAct
	var f Fleet
	for i := 0; i < 5; i++ {
		s := newSim(t)
		s.obs.Username = fmt.Sprintf("gaveup%d", i)
		s.hist = History{Script: s.obs.Script, Seen: true, Last: s.obs.Print, LastProgressAt: t0.Add(-2 * time.Hour),
			Detected: true, Step: StepGaveUp, GaveUpAt: t0.Add(-time.Hour), StepAt: t0.Add(-time.Hour)}
		s.obs.LastLineAt = t0.Add(-2 * time.Hour)
		a, h := Assess(s.now, s.obs, s.hist, cfg, watchStart)
		if a.Status != StatusStalled {
			t.Fatalf("a given-up account is %s, want still stalled", a.Status)
		}
		f.Count(s.now, s.obs, a, h, cfg)
	}
	// 79 more healthy WCFletch accounts, and a big Fisher cohort.
	for i := 0; i < 79; i++ {
		f.Count(t0, Obs{Script: "ProgressiveWCFletch", InGame: true, Running: true}, Assessment{Status: StatusOK}, History{}, cfg)
	}
	for i := 0; i < 212; i++ {
		f.Count(t0, Obs{Script: "ProgressiveFisher", InGame: true, Running: true}, Assessment{Status: StatusOK}, History{}, cfg)
	}
	if f.Stalled != 0 || f.StalledByScript["ProgressiveWCFletch"] != 0 || f.GivenUp != 5 {
		t.Fatalf("given-up accounts counted as stalled: %+v", f)
	}
	if f.InGameBots != 296 || f.InGameByScript["ProgressiveWCFletch"] != 84 {
		t.Fatalf("given-up accounts are still in-game bots for the denominators: %+v", f)
	}
	if ScriptBreakerOpen(f, "ProgressiveWCFletch", cfg) || BreakerOpen(f, cfg) {
		t.Fatalf("a breaker is open on given-up accounts alone: %+v", f)
	}
	n := newSim(t)
	n.obs.Username = "fresh"
	n.obs.ModalOpen, n.obs.CanContinue = true, true
	n.fleet = f
	n.runUntil(ActNudge, 30*time.Minute)

	// Once the hold is over the account is a live stall again, and counts.
	var g Fleet
	s := newSim(t)
	s.hist = History{Script: s.obs.Script, Seen: true, Last: s.obs.Print, LastProgressAt: t0.Add(-4 * time.Hour),
		Detected: true, Step: StepGaveUp, GaveUpAt: t0.Add(-cfg.GiveUpHold - time.Minute)}
	s.obs.LastLineAt = t0.Add(-4 * time.Hour)
	a, h := Assess(s.now, s.obs, s.hist, cfg, watchStart)
	g.Count(s.now, s.obs, a, h, cfg)
	if g.Stalled != 1 || g.GivenUp != 0 {
		t.Fatalf("after GiveUpHold the account must count again: %+v", g)
	}
}

// TestGraceAfterALogin: a fresh login (or reconnect) is a grace period even when the bot itself
// has been running for hours -- the reconnect path resumes the same runner, and the first
// minutes after a login are map loads and tab resends, not a stall. (A surviving mutant in
// review2: dropping the ConnectedAt clause failed no test.)
func TestGraceAfterALogin(t *testing.T) {
	s := newSim(t)
	s.obs.BotStartedAt = t0.Add(-3 * time.Hour)
	s.obs.ConnectedAt = t0.Add(-time.Minute)
	s.obs.LastLineAt = t0.Add(-3 * time.Hour)
	s.hist = History{Script: s.obs.Script, Seen: true, Last: s.obs.Print, LastProgressAt: t0.Add(-3 * time.Hour)}
	for s.now.Before(s.obs.ConnectedAt.Add(s.cfg.StartGrace - sweep)) {
		if a, d := s.tick(sweep); a.Status != StatusGrace || d.Action != ActNone {
			t.Fatalf("+%s after the login: %s %s, want grace and nothing", s.now.Sub(s.obs.ConnectedAt), a.Status, d.Action)
		}
	}
	// And the grace ends: silent and still, it is a stall once the grace is over.
	s.tick(2 * sweep)
	if a, _ := s.tick(sweep); a.Status != StatusStalled {
		t.Fatalf("after the login grace: %s, want stalled", a.Status)
	}
}

// TestKnownSilentWaitsAndMinStallAfter: the table is what DefaultConfig is built from.
func TestKnownSilentWaitsAndMinStallAfter(t *testing.T) {
	if got, want := MinStallAfter(), LongestKnownSilentWait().Max+SilenceMargin; got != want {
		t.Fatalf("MinStallAfter = %s, want %s", got, want)
	}
	if DefaultConfig().StallAfter != MinStallAfter() {
		t.Fatalf("DefaultConfig().StallAfter = %s, want MinStallAfter %s", DefaultConfig().StallAfter, MinStallAfter())
	}
	for _, w := range KnownSilentWaits {
		if w.What == "" || w.Max <= 0 {
			t.Fatalf("a known silent wait needs a description and a bound: %+v", w)
		}
	}
}
