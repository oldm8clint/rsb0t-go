package fleet

import (
	"errors"
	"fmt"
	"time"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/scripts/worldhopplan"
)

// This file is the live half of world hopping. internal/scripts/worldhopplan is the pure
// half and holds every decision; nothing here chooses anything.
//
// WHAT A HOP IS, exactly. A world is a different wsURL and nothing else (see
// worldresolve.go), so a hop is: log the account out, wait until the session is genuinely
// gone, point it at the other world's URL, log it back in, and start its script again. Both
// halves of that are ordinary fleet machinery — the logout is Manager.Disconnect's own path
// and the login is loginOnce, which means a hop queues behind the fleet-wide login-rate gate
// and takes a route-pool slot exactly like any other login. That is deliberate and is the
// first rule in the brief: a hop must not bypass the pacing.
//
// THE THREE THINGS THAT HAD TO BE GOT RIGHT.
//
// 1. THE SCRIPT DOES NOT SURVIVE. Read startReadLoop's cleanup: when a session goes, a
//    running bot is stopped, and the account's script is restarted afterwards by calling
//    StartBot again — which builds a FRESH bot.Context and a FRESH bot.Runner. Nothing in a
//    script's own memory crosses a hop. What does cross is the settings map (taken from the
//    old Runner and handed to the new one here, the same way retryAccount's resume does) and
//    anything internal/settings persisted to disk. A hop is therefore a restart with an
//    intent, and any script that hops must be able to pick up from its own persisted state.
//    Stated here because it is the single most likely thing for a later caller to assume
//    wrongly.
//
// 2. RESPONSE 5 IS THE HAZARD. "Already logged in" means the old session is still up on the
//    server. hopLeg does not just fire the new login off after Logout returns: it waits for
//    the account's own read loop to clear a.session (so this process agrees the socket is
//    closed), and then waits hopSettle on top, because the engine's own view of a session
//    lags the socket. If response 5 comes back anyway, the plan gives it a long wait and
//    then STOPS the account rather than retrying into it — CLAUDE.md's rule, and the
//    opposite of flapping.
//
// 3. IT MUST NOT LIVELOCK. Every terminal branch of worldhopplan.Decide lands the account
//    somewhere it can work: at home, or on the world it is stranded on, or logged out for an
//    operator. runHop never loops without the plan saying so, and hopBudget bounds the whole
//    thing in wall-clock time as a second, independent stop.

// hopSettle is how long to wait after this process has seen the old session close before
// attempting the login on the other world.
//
// It is 5 seconds and NOT the 45-second hop timer, deliberately. Two different waits are at
// work and conflating them would be wrong in both directions:
//
//   - THIS one exists so the server has finished tearing the old session down. The world
//     node answers response 5 while `logoutRequests.has(username)` is still true, and the
//     login server answers response 5 while `account.logged_in` is still set — both are
//     "the old session has not finished going", both clear in well under a second after a
//     clean logout, and neither is a timer.
//   - The SERVER'S OWN HOP TIMER (response 21, hopTime, 45s by default and counted from the
//     logout) is a separate, stated wait. Sleeping it here would make every hop take 45
//     seconds even on a server that does not enforce it — a lab engine, where every account
//     is staff and staff are exempt — and would still be wrong if the real value is not the
//     default. So it is NOT slept blindly: the hop attempts the login, and if the server
//     answers 21 the plan waits the number the server itself stated.
//
// The cost of that choice is one refused login attempt per hop on a server that does
// enforce the timer. That is the right trade against a guess: the refusal is cheap, it is
// explicitly retryable (see retryableLoginError), and it converts an unknown into the
// server's own number.
const hopSettle = 5 * time.Second

// hopSessionGoneTimeout bounds the wait for this process to observe its own session closing.
// Session.Logout falls back to closing the socket once its reply timeout elapses, so this
// only has to outlast that.
const hopSessionGoneTimeout = 45 * time.Second

// hopBudget is the wall-clock bound on one whole HopWorld call, legs and waits included. A
// second, independent stop beside the plan's own attempt and hop bounds: if anything in here
// blocks in a way the plan cannot see, the hop ends rather than running forever.
const hopBudget = 10 * time.Minute

// hopState is the per-account hop bookkeeping this file adds. Embedded on account (one line
// in fleet.go) to keep the diff on another session's claimed file minimal.
type hopState struct {
	// world is the world this account's NEXT login dials; 0 means the fleet's own.
	world int
	// hop is the plan's own state, which must outlive a script restart — a hop stops the
	// bot, so nothing the script holds can carry it.
	hop worldhopplan.State
	// hopping guards against two hops at once on one account.
	hopping bool
	// hopLimits overrides worldhopplan.DefaultLimits for this account (SetHopBudget); the zero
	// value changes nothing. A rotating RuneRunner plans up to maxHopsPerHour hops (12) and the
	// default four in thirty minutes refused 18 of 77 of them live on 7 October 2026.
	hopLimits worldhopplan.Limits
	// hopLog is the most recent hop outcome line, for the panel and for a harness.
	hopLog string
	// hopTimerWait is how long the SERVER last said to wait after a response 21 (the
	// world-hop timer), taken from that response's own payload byte. 0 until one is seen,
	// which is also the state on any server that never sends it — a lab engine, where every
	// account is staff and staff are exempt.
	hopTimerWait time.Duration
	// hopGoneAt is when this account's session last closed for a hop: the moment the server's
	// hop timer counts from, so a retried leg waits out the REST of it rather than all of it
	// again (hoptimer.go).
	hopGoneAt time.Time
	// hopRetryAfter is the wait a response 21 stated to reconnectLoop, consumed by its next
	// sleep (hoptimer.go).
	hopRetryAfter time.Duration
}

// ErrHopInProgress is returned when a second hop is requested while one is running.
var ErrHopInProgress = errors.New("fleet: a world hop is already in progress for this account")

// ErrOneWorld is returned when a hop is requested on a deployment whose worlds all resolve
// to the same URL — a local Engine-TS, or any host with no wN label and no explicit
// per-world entry. Refused rather than performed, so a single-world logout/login cycle can
// never be reported as a two-world hop.
var ErrOneWorld = errors.New("fleet: this deployment has only one world, so a hop would change nothing")

// HopWorld moves username to world: log out, settle, log back in on that world's URL, and
// restart whatever script was running. It returns as soon as the hop is accepted; the hop
// itself runs in the background and its progress is visible through Snapshot (World,
// HopLog) exactly as a reconnect's is.
//
// reason is for the log only.
func (m *Manager) HopWorld(username string, world int, reason string) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	if !m.WorldsAreDistinct() {
		return ErrOneWorld
	}
	target, err := m.worldMap().Normalise(world)
	if err != nil {
		return fmt.Errorf("fleet: %s: %w", username, err)
	}
	a.mu.Lock()
	if a.hopping {
		a.mu.Unlock()
		return ErrHopInProgress
	}
	current := a.world
	if current <= 0 {
		current = m.FleetWorld()
	}
	if a.hop.Home == 0 {
		a.hop.Home = current
	}
	a.hop.Current = current
	if target == current {
		a.mu.Unlock()
		return nil // already there; not an error, so a caller can ask unconditionally
	}
	a.hopping = true
	a.mu.Unlock()

	go m.runHop(a, target, reason)
	return nil
}

// SetHopBudget lets a script that plans its own hops raise the fleet's flapping bound for its
// account: at most maxHops hops in any window. Both are clamped to a sane range (1-30 hops, a
// window of five minutes to an hour), and a zero maxHops restores the default. The two-minute
// gap between hops and every other bound are unchanged; worldhopplan.State keeps only the last
// thirty minutes of hops, so a longer window counts those.
func (m *Manager) SetHopBudget(username string, maxHops int, window time.Duration) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	var l worldhopplan.Limits
	if maxHops > 0 {
		if maxHops > 30 {
			maxHops = 30
		}
		if window < 5*time.Minute {
			window = 5 * time.Minute
		}
		if window > time.Hour {
			window = time.Hour
		}
		l.MaxHops, l.Window = maxHops, window
	}
	a.mu.Lock()
	changed := a.hopLimits != l
	a.hopLimits = l
	a.mu.Unlock()
	if changed {
		m.alogf(a, "[%s] hop budget is now %d in %s", username, l.MaxHops, l.Window)
	}
	return nil
}

// HopHome sends username back to the world its roster entry gave it.
func (m *Manager) HopHome(username, reason string) error {
	home := m.HomeWorldOf(username)
	if home == 0 {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	return m.HopWorld(username, home, reason)
}

// runHop drives worldhopplan.Decide until it says to stop. Every branch here does exactly
// what the plan said and nothing more.
func (m *Manager) runHop(a *account, target int, reason string) {
	defer func() {
		a.mu.Lock()
		// However this request ended, the next one is decided afresh (worldhopplan.Ended):
		// a give-up must not answer every later request for the life of the process.
		a.hop = worldhopplan.Ended(a.hop)
		a.hopping = false
		a.mu.Unlock()
	}()

	username := a.username
	deadline := time.Now().Add(hopBudget)
	m.alogf(a, "[%s] world hop to %d requested: %s", username, target, reason)

	want, wantKnown := target, true
	for {
		if time.Now().After(deadline) {
			m.noteHop(a, fmt.Sprintf("world hop abandoned after %s — leaving the account where it is", hopBudget))
			m.ensureWorking(a)
			return
		}
		select {
		case <-m.stop:
			return
		default:
		}

		a.mu.RLock()
		st := a.hop
		a.mu.RUnlock()

		a.mu.RLock()
		timerWait := a.hopTimerWait
		limits := a.hopLimits
		a.mu.RUnlock()

		d := worldhopplan.Decide(worldhopplan.View{
			State:        st,
			WantWorld:    want,
			WantKnown:    wantKnown,
			Now:          time.Now(),
			Distinct:     m.WorldsAreDistinct(),
			HopTimerWait: timerWait,
			Limits:       limits,
		})

		switch d.Action {
		case worldhopplan.ActionStay:
			m.noteHop(a, "world hop: "+d.Why)
			m.ensureWorking(a)
			return

		case worldhopplan.ActionWait:
			if !waitLoginRetry(m.stop, d.Wait) {
				return
			}

		case worldhopplan.ActionHop, worldhopplan.ActionHome:
			m.noteHop(a, "world hop: "+d.Why)
			a.mu.Lock()
			// Begin STARTS A LEG; it resets the attempt counter and records a hop against
			// the window budget. So it must run only when the destination actually changes
			// — a RETRY of the leg already in progress is not a new leg.
			//
			// Getting this wrong made the hop unbounded, and the bounds proof caught it:
			// cmd/worldhoptest -unreachable ran for 3m20s logging "retrying world 2" over
			// and over, because every retry called Begin, every Begin set Attempts back to
			// 0, and the "this leg is out of attempts" rule therefore never fired. The hop
			// budget could not save it either, since the retry branch answers before the
			// budget is consulted. One line, and it defeated every bound in the plan.
			if a.hop.Leg != d.World {
				a.hop = worldhopplan.Begin(a.hop, d.World, time.Now())
			}
			a.mu.Unlock()
			out := m.hopLeg(a, d.World)
			a.mu.Lock()
			a.hop = worldhopplan.Attempted(a.hop, out, time.Now())
			done := out == worldhopplan.OutcomeOK
			a.mu.Unlock()
			if done {
				m.noteHop(a, fmt.Sprintf("world hop: now on world %d", d.World))
				if d.World == want {
					return
				}
				// Landed somewhere that was not the ask (the plan sent it home): done.
				return
			}
			// A failed leg that has spent its attempts must not be restarted by the next
			// pass, so mark it and let the plan land the account.
			a.mu.RLock()
			spent := a.hop.Attempts >= worldhopplan.DefaultLimits.MaxAttempts
			a.mu.RUnlock()
			if spent && d.Action == worldhopplan.ActionHome {
				a.mu.Lock()
				a.hop = worldhopplan.LegSpent(a.hop)
				a.mu.Unlock()
			}
			// After a failed outward leg the account's own world is where it wants to be.
			if d.Action == worldhopplan.ActionHop {
				want, wantKnown = m.HomeWorldOf(username), true
			}

		case worldhopplan.ActionGiveUpHere:
			m.noteHop(a, "world hop: "+d.Why)
			m.ensureWorking(a)
			return

		case worldhopplan.ActionStop:
			m.noteHop(a, "world hop: "+d.Why)
			// CLAUDE.md's rule for response 5 is to stop the account rather than retry.
			// Disconnect is the operator-visible form of that: the dashboard shows it as
			// manually disconnected with a reconnect button, and reconnectLoop will not
			// pull it back in.
			if err := m.disconnect(username, stopHop); err != nil {
				m.alogf(a, "[%s] world hop: stopping the account: %v", username, err)
			}
			return
		}
	}
}

// hopLeg performs one leg: log out, wait for the session to be genuinely gone, switch the
// world, log back in, restart the script. It classifies the login result into the plan's own
// Outcome vocabulary and never decides anything itself.
func (m *Manager) hopLeg(a *account, world int) worldhopplan.Outcome {
	username := a.username

	// What to restart afterwards. Taken before the logout, because Disconnect stops the bot.
	a.mu.RLock()
	script, runner := a.script, a.runner
	wasConnected := a.session != nil
	a.mu.RUnlock()
	var settings map[string]interface{}
	if runner != nil {
		settings = runner.Settings()
	}

	// 1. Log out. Disconnect sets deliberateDisconnect, which is what keeps reconnectLoop
	//    from racing this and logging the account straight back in on the OLD world — the
	//    bug this whole sequence would otherwise have.
	if err := m.disconnect(username, stopHop); err != nil {
		// "not connected" is fine: the hop then starts from a logged-out account.
		m.alogf(a, "[%s] world hop: logout: %v", username, err)
	}

	// 2. Wait for this process to agree the session is closed, then settle. Response 5 is
	//    what happens if this is skipped.
	if !m.waitSessionGone(a, hopSessionGoneTimeout) {
		m.alogf(a, "[%s] world hop: the old session did not close within %s — not attempting the new login", username, hopSessionGoneTimeout)
		m.clearDeliberate(a)
		return worldhopplan.OutcomeFailed
	}
	// The server's hop timer counts from the logout, so remember when that was: a retried leg
	// (already logged out) must wait out the rest of the timer, not all of it again.
	a.mu.Lock()
	if wasConnected || a.hopGoneAt.IsZero() {
		a.hopGoneAt = time.Now()
	}
	goneAt := a.hopGoneAt
	a.mu.Unlock()
	settle := hopSettle
	if w := m.hopTimerPrewait(goneAt); w > settle {
		// A response 21 has already told this fleet how long the timer is; dialling before it
		// runs out only spends an attempt to be told again (hoptimer.go).
		m.alogf(a, "[%s] world hop: waiting %s for the server's world-hop timer before dialling world %d", username, w.Round(time.Second), world)
		settle = w
	}
	if !waitLoginRetry(m.stop, settle) {
		return worldhopplan.OutcomeFailed
	}

	// 3. Point the account at the other world and clear the deliberate flag so loginOnce
	//    and startReadLoop will act. Both under one lock so no reader sees a half-switch.
	a.mu.Lock()
	prevWorld, prevCurrent := a.world, a.hop.Current
	a.world = world
	a.hop.Current = world
	a.deliberateDisconnect = false
	a.lastErr = ""
	a.mu.Unlock()

	// 4. Log in on the new world. loginOnce is the same path every other login takes, so
	//    the fleet-wide gate and the route pool both apply.
	err := m.loginOnce(a)
	if err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		// The login failed, so the account is NOT on the new world. Point it back where it
		// was: a plan that now gives up (ActionGiveUpHere after a full world or a connection
		// ceiling) hands the account to ConnectExisting, which dials a.world, and landing it
		// on the world that just refused it is the opposite of "give up here". A retried leg
		// sets the target again at step 3.
		a.world, a.hop.Current = prevWorld, prevCurrent
		a.mu.Unlock()
		m.alogf(a, "[%s] world hop to %d failed: %v", username, world, err)
		out, wait := classifyHopError(err)
		if wait > 0 {
			a.mu.Lock()
			a.hopTimerWait = wait
			a.mu.Unlock()
			m.learnHopTimer(time.Since(goneAt), wait)
		}
		return out
	}
	m.startReadLoop(a)
	if script != "" {
		if err := m.StartBot(username, script, settings); err != nil {
			m.alogf(a, "[%s] world hop: restarting %q on world %d failed: %v", username, script, world, err)
		}
	}
	return worldhopplan.OutcomeOK
}

// clearDeliberate puts an account back into a state something else can recover from when a
// leg never got as far as a login: the deliberate-disconnect flag is what stops
// reconnectLoop, so clearing it hands the account to the ordinary recovery path rather than
// leaving it parked with nobody retrying it.
func (m *Manager) clearDeliberate(a *account) {
	a.mu.Lock()
	a.deliberateDisconnect = false
	a.mu.Unlock()
}

// waitSessionGone blocks until a has no session, or the timeout elapses. Returns false on
// timeout or fleet shutdown.
func (m *Manager) waitSessionGone(a *account, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		a.mu.RLock()
		gone := a.session == nil && !a.connecting
		a.mu.RUnlock()
		if gone {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		if !waitLoginRetry(m.stop, 100*time.Millisecond) {
			return false
		}
	}
}

// ensureWorking gets an account that a hop left logged out back into the game on whatever
// world it is now pointed at, and restarts its script. This is what "land somewhere useful"
// actually means: the plan can decide to stop hopping, but it must never leave the account
// sitting logged out with nobody retrying it.
func (m *Manager) ensureWorking(a *account) {
	a.mu.RLock()
	connected := a.session != nil
	script := a.script
	runner := a.runner
	a.mu.RUnlock()
	if connected {
		if script != "" && (runner == nil || runner.Status().State != bot.StateRunning) {
			var settings map[string]interface{}
			if runner != nil {
				settings = runner.Settings()
			}
			if err := m.StartBot(a.username, script, settings); err != nil {
				m.alogf(a, "[%s] world hop: restarting %q failed: %v", a.username, script, err)
			}
		}
		return
	}
	// Not connected: hand it to the ordinary reconnect path, which retries indefinitely
	// with the fleet's own pacing rather than this file inventing a second retry loop. The
	// leg's Disconnect cleared resumeScript, so set it again: retryAccount restarts the
	// script only when it is set, and without this a hop that gave up left the account
	// logged back in with nothing running (review revW, 2026-09-23).
	a.mu.Lock()
	a.deliberateDisconnect = false
	if a.script != "" && a.runner != nil {
		a.resumeScript = true
	}
	a.mu.Unlock()
	if err := m.ConnectExisting(a.username); err != nil {
		m.alogf(a, "[%s] world hop: returning the account to the login queue: %v", a.username, err)
	}
}

// classifyHopError maps a login error onto the plan's Outcome vocabulary. The three cases
// that are NOT "an ordinary failure, try again" each have a documented reason in
// loginceiling.go or in worldhopplan's package doc.
func classifyHopError(err error) (worldhopplan.Outcome, time.Duration) {
	var rejected *client.LoginRejectedError
	if errors.As(err, &rejected) {
		switch rejected.Response {
		case client.AlreadyLoggedInResponse:
			return worldhopplan.OutcomeAlreadyLoggedIn, 0
		case connectionCeilingResponse:
			return worldhopplan.OutcomeCeiling, 0
		case client.WorldFullResponse:
			return worldhopplan.OutcomeWorldFull, 0
		case client.HopTimerResponse:
			// The server states the seconds. Prefer them over any default: hopTime is
			// configurable, and this engine's own WorldConfig.ts reads its fallback from
			// NODE_MAX_NPCS, so the real value is not predictable.
			if rejected.RetrySeconds > 0 {
				return worldhopplan.OutcomeHopTimer, time.Duration(rejected.RetrySeconds) * time.Second
			}
			return worldhopplan.OutcomeHopTimer, 0
		}
	}
	if retryableLoginError(err) {
		return worldhopplan.OutcomeFailed, 0
	}
	return worldhopplan.OutcomeTerminal, 0
}

// noteHop records a hop line on the account's own activity log and on its status.
func (m *Manager) noteHop(a *account, line string) {
	a.mu.Lock()
	a.hopLog = line
	a.mu.Unlock()
	m.alogf(a, "[%s] %s", a.username, line)
}

// HopLog returns the most recent world-hop line for an account, empty when it has never
// hopped. For a harness and for the panel.
func (m *Manager) HopLog(username string) string {
	a := m.find(username)
	if a == nil {
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.hopLog
}
