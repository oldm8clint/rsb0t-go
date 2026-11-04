package stallwatch

import "time"

// The safety net: the other half of "an account is stuck". Assess and Plan judge a RUNNING bot that
// has stopped making progress. This judges the opposite: an account that is in game with a bot that
// is NOT running, when nobody meant it to be stopped.
//
// WHY IT EXISTS. 18 October 2026, 10:46: the watchdog's restart asked a ScarabSwarm bot to stop. The
// bot took two minutes to wind down (a prayer ladder), the restart gave up after 15 seconds ("NOT
// starting a second copy beside it"), the connection dropped and came back while the old bot was
// still stopping, and the reconnect did not start the script because the stop had cleared its resume
// flag. When the bot finally ended nothing was left to start it: the account stood in game for about
// eight hours with botState "stopped", and the watchdog -- which only ever looks at a running bot --
// recorded "ladder dropped ... (an operator or a hop took over)" fifteen minutes in and never looked
// again, until a random event that no bot answered killed it. A second account was found in the same
// state at 18:55.
//
// The restart now finishes its own job (internal/fleet/watchdog.go, restart). This is the net under
// every other way a bot can end up left stopped: a failed resume after a reconnect, a hop that
// could not restart its script, a start that raced a logout.
//
// WHAT IT NEVER TOUCHES. A bot somebody meant to stop:
//
//   - an operator's explicit stop or disconnect (the panel's POST /api/bot/stop, a bulk stop, a
//     stream take-over): StopOperator, until the next start of any script on the account;
//   - a script that ended itself: it finished its job (a clean stop nobody asked for), declared
//     itself finished for good (Finished), or returned an error during THIS login session. An
//     error from an earlier session is different: it is what a dropped connection does to a
//     script mid-send, and the reconnect that followed did not restart it, so it is retried once
//     per login (the retry's own error, in the new session, is then left alone);
//   - an account that is not in game (logging in, reconnecting, hopping, deliberately logged out):
//     whoever owns that transition restarts the script.
//
// It acts only after OrphanAfter of the bot being stopped IN GAME (the clock restarts at each
// login, so a hop's or a relogin's own restart is never raced), at most OrphanMaxStarts times per
// account inside LadderWindow, and at most OrphanPerSweep per sweep fleet-wide. It never changes
// which script an account runs: the caller starts the script the account already had, with the
// settings it already had.

// StopKind is who asked the fleet to stop an account's bot, as far as the fleet could tell.
type StopKind int

const (
	// StopNone is no request through the fleet: the script ended itself, or the connection dropped
	// under it (the read loop's own stop), or nothing has run.
	StopNone StopKind = iota
	// StopOperator is an operator's explicit stop or disconnect.
	StopOperator
	// StopWatchdog is the watchdog's own restart or relogin.
	StopWatchdog
	// StopHop is a world hop's logout.
	StopHop
)

func (k StopKind) String() string {
	switch k {
	case StopOperator:
		return "an operator"
	case StopWatchdog:
		return "the watchdog"
	case StopHop:
		return "a world hop"
	}
	return "nobody"
}

// OrphanObs is one account's bot as the safety net reads it on one sweep.
type OrphanObs struct {
	Username string
	Script   string
	// InGame is Obs.InGame: a live session that is not logging in, reconnecting or hopping, and was
	// not deliberately disconnected.
	InGame bool
	// HasBot is a bot having been started on this account in this process, so the script it ran is
	// known. An account no bot was ever started on is left alone: it may be connected by hand.
	HasBot bool
	// Winding is the bot running, or told to stop and not yet finished (its OnStop still running).
	Winding bool
	// StoppedAt is when the run loop ended; ConnectedAt when the current login session began.
	StoppedAt   time.Time
	ConnectedAt time.Time
	// Errored is the run having ended with an error return rather than a clean stop.
	Errored bool
	// StopCalled is the fleet having asked the bot to stop (Runner.Stop), whoever it was: false
	// means the script ended itself.
	StopCalled bool
	Stop       StopKind
	// Finished is the script having declared itself finished for good (Context.RequestFinish).
	Finished bool
}

// OrphanVerdict is OrphanPlan's answer.
type OrphanVerdict int

const (
	// OrphanNone is not an orphan: nothing to do, nothing to say.
	OrphanNone OrphanVerdict = iota
	// OrphanWait is an orphan that has not been stopped long enough yet (or for which this sweep's
	// start budget is spent).
	OrphanWait
	// OrphanStart is an orphan to start now; the start is already counted in the history.
	OrphanStart
	// OrphanHeld is an orphan that would be started if the mode allowed it (observe, nudge). Report
	// says whether to write the line: once per spell.
	OrphanHeld
	// OrphanLimited is an orphan the net has started OrphanMaxStarts times already inside
	// LadderWindow: it keeps ending, and an operator should look. Report is once per window.
	OrphanLimited
)

func (v OrphanVerdict) String() string {
	return [...]string{"none", "wait", "start", "held", "limited"}[v]
}

// OrphanHistory is what the net remembers about one account between sweeps.
type OrphanHistory struct {
	// Starts are when the net started this account's script, pruned to LadderWindow.
	Starts []time.Time
	// SeenAt is when this spell was first seen, for a stop with no timestamp to count from.
	SeenAt time.Time
	// ReportedSince is the spell's start that was last reported as held, so observe mode says it once.
	ReportedSince time.Time
	// LimitedAt is when the per-window cap was last reported.
	LimitedAt time.Time
}

// OrphanDecision is OrphanPlan's answer with its reasoning.
type OrphanDecision struct {
	Verdict OrphanVerdict
	// Why says in words why the bot is judged left stopped: who stopped it and what did not
	// restart it. Set for every verdict but OrphanNone.
	Why string
	// Idle is how long the bot has been stopped in game.
	Idle time.Duration
	// Report is whether the caller should write a line for this verdict (always for OrphanStart).
	Report bool
	// Held says why OrphanWait/OrphanHeld/OrphanLimited did not start it.
	Held string
}

// orphanCause is the sentence that says why this bot is judged left stopped, for the log line.
func orphanCause(o OrphanObs) string {
	switch {
	case o.Stop == StopWatchdog:
		return "the watchdog's own restart or relogin stopped it and nothing started it again"
	case o.Stop == StopHop:
		return "a world hop stopped it and nothing started it again"
	case o.StopCalled:
		return "the fleet stopped it (a dropped connection, or a resume that failed) and nothing started it again"
	case o.Errored:
		return "its last run ended with an error in an earlier login session and the reconnect did not start it again"
	}
	return "nothing started it again"
}

// OrphanPlan decides whether one account's stopped bot is an orphan to start again. act is the mode
// allowing the start (ModeAct); startsLeft is how many starts this sweep may still make fleet-wide.
// It records a start it returns in the history it hands back, so the caller must keep that history
// whether or not the start succeeds. Pure.
func OrphanPlan(now time.Time, o OrphanObs, h OrphanHistory, cfg Config, act bool, startsLeft int) (OrphanDecision, OrphanHistory) {
	h.Starts = pruneLadders(h.Starts, now, cfg.LadderWindow)
	none := func() (OrphanDecision, OrphanHistory) {
		h.SeenAt, h.ReportedSince = time.Time{}, time.Time{}
		return OrphanDecision{}, h
	}
	if cfg.OrphanAfter <= 0 || cfg.Mode == ModeOff {
		return none()
	}
	switch {
	case !o.InGame, !o.HasBot, o.Winding, o.Finished, o.Stop == StopOperator:
		return none()
	case !o.StopCalled && !o.Errored:
		return none() // the script ended itself cleanly: it is done
	case !o.StopCalled && o.Errored && o.StoppedAt.After(o.ConnectedAt):
		return none() // it failed in this very session: its own terminal error
	}
	// The clock counts from the later of the stop and the login: a bot left stopped across a
	// reconnect gets a fresh OrphanAfter in game, so a hop's or a relogin's own restart is never raced.
	since := o.StoppedAt
	if o.ConnectedAt.After(since) {
		since = o.ConnectedAt
	}
	if since.IsZero() {
		if h.SeenAt.IsZero() {
			h.SeenAt = now
		}
		since = h.SeenAt
	}
	d := OrphanDecision{Why: orphanCause(o), Idle: now.Sub(since)}
	if d.Idle < cfg.OrphanAfter {
		d.Verdict = OrphanWait
		return d, h
	}
	if cfg.OrphanMaxStarts > 0 && len(h.Starts) >= cfg.OrphanMaxStarts {
		d.Verdict = OrphanLimited
		d.Held = "started that many times already inside the ladder window and it keeps ending"
		if h.LimitedAt.IsZero() || now.Sub(h.LimitedAt) >= cfg.LadderWindow {
			h.LimitedAt = now
			d.Report = true
		}
		return d, h
	}
	if !act {
		d.Verdict = OrphanHeld
		d.Held = "the watchdog is not in act mode"
		if !h.ReportedSince.Equal(since) {
			h.ReportedSince = since
			d.Report = true
		}
		return d, h
	}
	if startsLeft <= 0 {
		d.Verdict = OrphanWait
		d.Held = "this sweep's start budget is spent; the rest wait for the next one"
		return d, h
	}
	d.Verdict, d.Report = OrphanStart, true
	h.Starts = append(h.Starts, now)
	h.ReportedSince = time.Time{}
	return d, h
}
