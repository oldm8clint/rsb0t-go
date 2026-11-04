package stallwatch

import (
	"strings"
	"testing"
	"time"
)

// The safety net's rules (orphan.go), on a fake clock. orphanBase is the 18 October 2026 account:
// in game on a login that began before the bot ended, whose ScarabSwarm the watchdog's own restart
// stopped and nothing started again.

func orphanBase() OrphanObs {
	return OrphanObs{
		Username: "orphan1", Script: "ScarabSwarm", InGame: true, HasBot: true,
		StoppedAt:   t0.Add(-10 * time.Minute),
		ConnectedAt: t0.Add(-30 * time.Minute),
		StopCalled:  true, Stop: StopWatchdog,
	}
}

func orphanCfg() Config {
	cfg := DefaultConfig()
	cfg.Mode = ModeAct
	return cfg
}

func plan(o OrphanObs, h OrphanHistory, cfg Config, now time.Time) (OrphanDecision, OrphanHistory) {
	return OrphanPlan(now, o, h, cfg, cfg.Mode == ModeAct, cfg.OrphanPerSweep)
}

// TestOrphanTheWatchdogStoppedIsStartedAfterOrphanAfter is the incident: stopped by the watchdog's
// own restart, in game, nothing running. Nothing before OrphanAfter; a start at OrphanAfter that
// says whose stop it was.
func TestOrphanTheWatchdogStoppedIsStartedAfterOrphanAfter(t *testing.T) {
	cfg := orphanCfg()
	o := orphanBase()
	o.StoppedAt = t0
	var h OrphanHistory
	var d OrphanDecision
	for _, after := range []time.Duration{0, time.Minute, cfg.OrphanAfter - time.Second} {
		if d, h = plan(o, h, cfg, t0.Add(after)); d.Verdict != OrphanWait {
			t.Fatalf("+%s: %v, want wait", after, d.Verdict)
		}
	}
	d, h = plan(o, h, cfg, t0.Add(cfg.OrphanAfter))
	if d.Verdict != OrphanStart || !d.Report {
		t.Fatalf("at OrphanAfter: %+v, want a reported start", d)
	}
	if !strings.Contains(d.Why, "the watchdog's own restart or relogin stopped it") {
		t.Fatalf("why = %q, want the watchdog named", d.Why)
	}
	if len(h.Starts) != 1 {
		t.Fatalf("the start must be recorded: %v", h.Starts)
	}
}

// TestOrphanNeverTouchesABotSomebodyMeantToStop: every stop that is somebody's decision, and every
// account the net has no business with.
func TestOrphanNeverTouchesABotSomebodyMeantToStop(t *testing.T) {
	cfg := orphanCfg()
	now := t0.Add(8 * time.Hour) // however long it has been
	for _, c := range []struct {
		name string
		edit func(*OrphanObs)
	}{
		{"an operator's explicit stop", func(o *OrphanObs) { o.Stop = StopOperator }},
		{"a script that ended itself cleanly", func(o *OrphanObs) { o.StopCalled, o.Stop = false, StopNone }},
		{"a script that failed in this very session", func(o *OrphanObs) {
			o.StopCalled, o.Stop, o.Errored = false, StopNone, true
			o.StoppedAt, o.ConnectedAt = t0.Add(-time.Hour), t0.Add(-2*time.Hour)
		}},
		{"a script that declared itself finished for good", func(o *OrphanObs) { o.Finished = true }},
		{"an account that is not in game (hopping, reconnecting, logged out)", func(o *OrphanObs) { o.InGame = false }},
		{"an account no bot was ever started on", func(o *OrphanObs) { o.HasBot = false }},
		{"a bot still running or winding down", func(o *OrphanObs) { o.Winding = true }},
	} {
		o := orphanBase()
		c.edit(&o)
		if d, _ := plan(o, OrphanHistory{}, cfg, now); d.Verdict != OrphanNone {
			t.Errorf("%s: %v (%s), want none", c.name, d.Verdict, d.Why)
		}
	}
	// Mode off, and the net switched off by OrphanAfter 0.
	off := cfg
	off.OrphanAfter = 0
	if d, _ := plan(orphanBase(), OrphanHistory{}, off, now); d.Verdict != OrphanNone {
		t.Errorf("OrphanAfter 0: %v", d.Verdict)
	}
	off = cfg
	off.Mode = ModeOff
	if d, _ := plan(orphanBase(), OrphanHistory{}, off, now); d.Verdict != OrphanNone {
		t.Errorf("mode off: %v", d.Verdict)
	}
}

// TestOrphanStopsThatAreNotAnOperatorsAreStarted: a world hop that could not restart its script, a
// dropped connection whose resume failed, and an error from an EARLIER session (what a dropped
// connection does to a script mid-send) are all left-stopped bots.
func TestOrphanStopsThatAreNotAnOperatorsAreStarted(t *testing.T) {
	cfg := orphanCfg()
	now := t0.Add(time.Hour)
	for _, c := range []struct {
		name string
		edit func(*OrphanObs)
		why  string
	}{
		{"a hop", func(o *OrphanObs) { o.Stop = StopHop }, "a world hop stopped it"},
		{"a dropped connection", func(o *OrphanObs) { o.Stop = StopNone }, "dropped connection, or a resume that failed"},
		{"an error from an earlier session", func(o *OrphanObs) {
			o.StopCalled, o.Stop, o.Errored = false, StopNone, true
			o.StoppedAt, o.ConnectedAt = t0.Add(-3*time.Hour), t0.Add(-2*time.Hour)
		}, "ended with an error in an earlier login session"},
	} {
		o := orphanBase()
		c.edit(&o)
		d, _ := plan(o, OrphanHistory{}, cfg, now)
		if d.Verdict != OrphanStart || !strings.Contains(d.Why, c.why) {
			t.Errorf("%s: %v %q, want a start whose reason contains %q", c.name, d.Verdict, d.Why, c.why)
		}
	}
}

// TestOrphanClockRestartsAtEachLogin: a bot left stopped across a reconnect gets a fresh
// OrphanAfter in game, so a hop's or a relogin's own restart (a minute or two after the login) is
// never raced by the net.
func TestOrphanClockRestartsAtEachLogin(t *testing.T) {
	cfg := orphanCfg()
	o := orphanBase()
	o.StoppedAt = t0.Add(-8 * time.Hour)
	o.ConnectedAt = t0.Add(-time.Minute) // logged in a minute ago
	if d, _ := plan(o, OrphanHistory{}, cfg, t0); d.Verdict != OrphanWait {
		t.Fatalf("a minute after the login: %v, want wait", d.Verdict)
	}
	if d, _ := plan(o, OrphanHistory{}, cfg, t0.Add(cfg.OrphanAfter)); d.Verdict != OrphanStart {
		t.Fatalf("OrphanAfter after the login: %v, want start", d.Verdict)
	}
}

// TestOrphanNeedsAnOrphanAfterEvenWithoutATimestamp: a stop with no timestamp counts from first sight.
func TestOrphanNeedsAnOrphanAfterEvenWithoutATimestamp(t *testing.T) {
	cfg := orphanCfg()
	o := orphanBase()
	o.StoppedAt, o.ConnectedAt = time.Time{}, time.Time{}
	d, h := plan(o, OrphanHistory{}, cfg, t0)
	if d.Verdict != OrphanWait || h.SeenAt.IsZero() {
		t.Fatalf("first sight: %v seenAt %v", d.Verdict, h.SeenAt)
	}
	if d, _ = plan(o, h, cfg, t0.Add(cfg.OrphanAfter)); d.Verdict != OrphanStart {
		t.Fatalf("OrphanAfter after first sight: %v", d.Verdict)
	}
}

// TestOrphanObserveReportsOncePerSpellAndStartsNothing: in observe and nudge modes a due start is
// said once and not taken; a new spell is said again.
func TestOrphanObserveReportsOncePerSpellAndStartsNothing(t *testing.T) {
	cfg := orphanCfg()
	cfg.Mode = ModeObserve
	o := orphanBase()
	var h OrphanHistory
	reports := 0
	for i := 0; i < 20; i++ {
		var d OrphanDecision
		d, h = plan(o, h, cfg, t0.Add(time.Duration(i)*30*time.Second))
		if d.Verdict != OrphanHeld {
			t.Fatalf("sweep %d: %v, want held", i, d.Verdict)
		}
		if d.Report {
			reports++
		}
	}
	if reports != 1 || len(h.Starts) != 0 {
		t.Fatalf("reports=%d starts=%d, want one report and no start", reports, len(h.Starts))
	}
	// It starts running again, then is stopped again: a new spell, said again.
	run := o
	run.Winding = true
	_, h = plan(run, h, cfg, t0.Add(time.Hour))
	o.StoppedAt = t0.Add(time.Hour)
	d, h := plan(o, h, cfg, t0.Add(time.Hour+cfg.OrphanAfter))
	if d.Verdict != OrphanHeld || !d.Report {
		t.Fatalf("a new spell: %+v, want a fresh report", d)
	}
}

// TestOrphanIsCappedPerAccountAndPerSweep: an account that keeps ending is started OrphanMaxStarts
// times inside LadderWindow and then reported, once, for an operator; the start budget of one sweep
// is spent; the cap is not forever.
func TestOrphanIsCappedPerAccountAndPerSweep(t *testing.T) {
	cfg := orphanCfg()
	o := orphanBase()
	var h OrphanHistory
	var d OrphanDecision
	now := t0
	for i := 0; i < cfg.OrphanMaxStarts; i++ {
		now = now.Add(cfg.OrphanAfter + time.Minute)
		if d, h = plan(o, h, cfg, now); d.Verdict != OrphanStart {
			t.Fatalf("start %d: %v", i+1, d.Verdict)
		}
	}
	now = now.Add(cfg.OrphanAfter + time.Minute)
	if d, h = plan(o, h, cfg, now); d.Verdict != OrphanLimited || !d.Report {
		t.Fatalf("past the cap: %+v, want a reported limit", d)
	}
	if d, h = plan(o, h, cfg, now.Add(time.Minute)); d.Verdict != OrphanLimited || d.Report {
		t.Fatalf("the limit repeats: %+v, want it said once", d)
	}
	if d, _ = plan(o, h, cfg, now.Add(cfg.LadderWindow+time.Hour)); d.Verdict != OrphanStart {
		t.Fatalf("after the window: %v, want a start again", d.Verdict)
	}
	// This sweep's budget.
	if d, hh := OrphanPlan(t0.Add(time.Hour), orphanBase(), OrphanHistory{}, cfg, true, 0); d.Verdict != OrphanWait || len(hh.Starts) != 0 {
		t.Fatalf("no budget: %+v starts %v, want wait and no start recorded", d, hh.Starts)
	}
}

// TestOrphanHistoryResetsWhenItIsNoLongerAnOrphan: a spell that ends (the bot is running again)
// clears the held-report memory but keeps the starts, which are what the cap counts.
func TestOrphanHistoryResetsWhenItIsNoLongerAnOrphan(t *testing.T) {
	cfg := orphanCfg()
	d, h := plan(orphanBase(), OrphanHistory{}, cfg, t0.Add(time.Hour))
	if d.Verdict != OrphanStart {
		t.Fatalf("setup: %v", d.Verdict)
	}
	run := orphanBase()
	run.Winding = true
	_, h = plan(run, h, cfg, t0.Add(2*time.Hour))
	if len(h.Starts) != 1 || !h.ReportedSince.IsZero() || !h.SeenAt.IsZero() {
		t.Fatalf("history after the bot ran again: %+v", h)
	}
}

// TestAssessNeverBlamesAnOperatorForTheWatchdogsOwnStop: the 18 October line "ladder dropped ... (an
// operator or a hop took over)" was written for a stop the watchdog had requested itself. When the
// bot is idle because the watchdog stopped it, the dropped ladder is flagged AbortedOwnStop and not
// Aborted; any other idle spell is still Aborted.
func TestAssessNeverBlamesAnOperatorForTheWatchdogsOwnStop(t *testing.T) {
	for _, own := range []bool{true, false} {
		s := newSim(t)
		s.runUntil(ActRestart, 30*time.Minute)
		s.obs.Running = false
		s.obs.OwnStop = own
		var aborted, abortedOwn bool
		for i := 0; i < int(s.cfg.IdleAbort/sweep)+2; i++ {
			a, _ := s.tick(sweep)
			aborted = aborted || a.Aborted
			abortedOwn = abortedOwn || a.AbortedOwnStop
		}
		if s.hist.Step != StepNone {
			t.Fatalf("own=%v: the ladder is still open past IdleAbort (step %s)", own, s.hist.Step)
		}
		if own && (aborted || !abortedOwn) {
			t.Fatalf("own stop: Aborted=%v AbortedOwnStop=%v, want only the own-stop flag", aborted, abortedOwn)
		}
		if !own && (!aborted || abortedOwn) {
			t.Fatalf("someone else's stop: Aborted=%v AbortedOwnStop=%v, want only Aborted", aborted, abortedOwn)
		}
	}
}

func TestStopKindNames(t *testing.T) {
	for k, want := range map[StopKind]string{StopNone: "nobody", StopOperator: "an operator", StopWatchdog: "the watchdog", StopHop: "a world hop"} {
		if k.String() != want {
			t.Errorf("%d = %q, want %q", k, k.String(), want)
		}
	}
	if got := DefaultConfig(); got.OrphanAfter != 5*time.Minute || got.OrphanMaxStarts != 3 || got.OrphanPerSweep != 3 {
		t.Fatalf("defaults = %v %d %d", got.OrphanAfter, got.OrphanMaxStarts, got.OrphanPerSweep)
	}
}
