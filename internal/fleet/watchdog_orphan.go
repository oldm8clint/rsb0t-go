package fleet

import (
	"fmt"
	"time"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/stallwatch"
)

// The watchdog's other duty besides stalls: never leave an account in game with its bot stopped
// when nobody meant it to be. Two parts, one rule.
//
//   - A restart finishes its own job. It stops the bot, waits for the old runner to end, and
//     starts the same script with the same settings -- however long the old bot takes to wind down
//     (a ScarabSwarm prayer ladder took two minutes on 18 October 2026, against a 15 second wait
//     that used to give up and leave nothing running). It never starts a second copy beside a live
//     one: StartBot refuses one, and this waits for the old runner to be finished first.
//   - A safety net under every other way a bot ends up left stopped (stallwatch.OrphanPlan has the
//     rules and the reasons): an account in game whose bot has been stopped for OrphanAfter and was
//     not stopped by an operator, finished for good, or ended by the script itself is started again
//     with the script and settings it already had. It never changes which script an account runs.

// startBot is Manager.StartBot, or the test's stand-in.
func (w *watchdog) startBot(username, script string, settings map[string]interface{}) error {
	if w.startFn != nil {
		return w.startFn(username, script, settings)
	}
	return w.m.StartBot(username, script, settings)
}

// orphanObs reads one account's bot the way the safety net judges it. runner and rs are the
// account's runner and its Status (zero without one); stopBy is account.stopBy.
func orphanObs(o stallwatch.Obs, script string, runner *bot.Runner, rs bot.Status, stopBy stopSource) stallwatch.OrphanObs {
	ob := stallwatch.OrphanObs{Username: o.Username, Script: script, InGame: o.InGame, ConnectedAt: o.ConnectedAt}
	if runner == nil || script == "" {
		return ob
	}
	ob.HasBot = true
	ob.Winding = rs.State == bot.StateRunning || rs.State == bot.StateStopping || rs.State == bot.StateIdle || !runner.Finished()
	ob.StoppedAt = rs.StoppedAt
	ob.Errored = rs.State == bot.StateError
	ob.StopCalled = runner.StoppedFromOutside()
	ob.Stop = stopBy.kind()
	_, ob.Finished = runner.FinishRequested()
	return ob
}

// sweepOrphans runs the safety net over the accounts a sweep observed that have a bot (running or
// not; the pure plan sorts them). act is the mode being act; in observe and nudge a due start is
// reported once and not taken.
func (w *watchdog) sweepOrphans(now time.Time, mode stallwatch.Mode, cfg stallwatch.Config, cands []watchObs) {
	if cfg.OrphanAfter <= 0 || len(cands) == 0 {
		return
	}
	startsLeft := cfg.OrphanPerSweep
	if startsLeft <= 0 {
		startsLeft = 3
	}
	act := mode == stallwatch.ModeAct
	for _, o := range cands {
		name := o.obs.Username
		w.mu.Lock()
		if w.orphans == nil {
			w.orphans = map[string]stallwatch.OrphanHistory{}
		}
		h, had := w.orphans[name]
		busy := w.inFlight[name]
		w.mu.Unlock()
		if busy {
			continue // an action of ours is already running for this account
		}
		d, nh := stallwatch.OrphanPlan(now, o.orphan, h, cfg, act, startsLeft)
		w.mu.Lock()
		if had || len(nh.Starts) > 0 || !nh.SeenAt.IsZero() || !nh.ReportedSince.IsZero() || !nh.LimitedAt.IsZero() {
			w.orphans[name] = nh
		}
		w.mu.Unlock()
		a := o.a
		switch d.Verdict {
		case stallwatch.OrphanStart:
			startsLeft--
			w.mu.Lock()
			w.counts.Orphans++
			w.mu.Unlock()
			w.begin(name)
			go w.orphanStart(a, o.runner, o.script, d)
		case stallwatch.OrphanHeld:
			if d.Report {
				w.m.wlogf(a, "safety net: %s has been stopped for %s on an account that is in game, and %s -- it would be started again, but %s", o.script, d.Idle.Round(time.Second), d.Why, d.Held)
			}
		case stallwatch.OrphanLimited:
			why := fmt.Sprintf("NEEDS ATTENTION: %s has been stopped in game and started again by the safety net %d times already inside %s -- it keeps ending (%s); leaving it alone",
				o.script, cfg.OrphanMaxStarts, cfg.LadderWindow, d.Why)
			if d.Report {
				w.m.wlogf(a, "safety net: %s", why)
			}
			w.mu.Lock()
			w.status[name] = WatchdogStatus{State: "needs_attention", Since: fmtTime(now), Step: "safety net", Detail: why}
			w.mu.Unlock()
		}
	}
}

// orphanStart is the safety net's start of one account's script. It owns the account (begin) until
// it returns, so the stall ladder does not act on it at the same time.
func (w *watchdog) orphanStart(a *account, runner *bot.Runner, script string, d stallwatch.OrphanDecision) {
	defer w.end(a.username)
	if runner == nil || script == "" {
		return
	}
	w.m.wlogf(a, "safety net: %s has been stopped for %s on an account that is in game, and %s -- starting %s again with the settings it had",
		script, d.Idle.Round(time.Second), d.Why, script)
	if w.startAgain(a, runner, script, runner.Settings(), "safety net") {
		w.m.wlogf(a, "safety net: %s is running again -- rechecking after %s of fresh silence", script, w.cfg.StallAfter)
	}
}

// startAgain starts script on a with settings after the old runner is finished -- the one place
// the watchdog starts a bot it stopped or found stopped -- and says in the account's log why when it
// does not. It checks, at the moment of starting and not when the decision was made:
//
//   - no other bot has been started on the account meanwhile (a second copy beside a live one is
//     the one thing this must never make; StartBot refuses it too);
//   - an operator has not stopped it meanwhile (their stop outranks the watchdog's);
//   - the account is in game. If it is mid-reconnect instead (the connection dropped while the old
//     bot was stopping, as on 18 October 2026) the reconnect is told to start the script when the
//     login lands (account.resumeScript); a hop restarts its own script; an account that is logged
//     out is left to whatever logs it back in, and to the safety net after that.
//
// It returns true only when StartBot succeeded.
func (w *watchdog) startAgain(a *account, runner *bot.Runner, script string, settings map[string]interface{}, what string) bool {
	a.mu.RLock()
	cur, session := a.runner, a.session
	pending := a.reconnecting || a.connecting
	hopping, deliberate := a.hopping, a.deliberateDisconnect
	by := a.stopBy
	a.mu.RUnlock()
	switch {
	case cur != runner:
		state := "none"
		if cur != nil {
			state = string(cur.Status().State)
		}
		w.m.wlogf(a, "%s: another bot (%s) was started on the account meanwhile; not starting a second copy beside it", what, state)
		return false
	case by == stopOperator:
		w.m.wlogf(a, "%s: an operator stopped the bot meanwhile; leaving %s stopped", what, script)
		return false
	case hopping:
		w.m.wlogf(a, "%s: a world hop has the account now and restarts %s itself", what, script)
		return false
	case session == nil && pending && !deliberate:
		a.mu.Lock()
		armed := a.runner == runner && a.session == nil && !a.deliberateDisconnect && a.stopBy != stopOperator
		if armed {
			a.resumeScript = true
		}
		a.mu.Unlock()
		if armed {
			w.m.wlogf(a, "%s: the connection dropped while the old bot was stopping; %s starts again as soon as the reconnect lands", what, script)
		} else {
			w.m.wlogf(a, "%s: the connection dropped and the account was stopped or taken over meanwhile; not starting %s", what, script)
		}
		return false
	case session == nil:
		w.m.wlogf(a, "%s: the account is not in game; not starting %s (the safety net starts it once the account is back in game)", what, script)
		return false
	}
	if err := w.startBot(a.username, script, settings); err != nil {
		w.m.wlogf(a, "%s: starting %s again failed: %v", what, script, err)
		return false
	}
	return true
}

// abortedWhy says who took the account out of a dropped ladder, from what the fleet knows. The
// watchdog's own stop is never described as somebody else's (Assessment.AbortedOwnStop).
func abortedWhy(o stallwatch.OrphanObs) string {
	switch o.Stop {
	case stallwatch.StopOperator:
		return "an operator stopped it"
	case stallwatch.StopHop:
		return "a world hop took the account"
	}
	return "the account left the game or its bot ended on its own"
}
