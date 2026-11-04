package fleet

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/mulestatus"
)

// AccountPhase is one account's current phase as the control panel's time split samples it:
// whether it is in game, the script running and the phase that script last reported
// (bot.Context.SetPhase), and what the random-event guardian is handling, if anything.
type AccountPhase struct {
	Username string
	InGame   bool
	Script   string
	// Running is true while a script's runner is running (or stopping).
	Running bool
	Phase   string
	Since   time.Time
	// Event names what the guardian is handling now ("" when nothing), and since when.
	Event      string
	EventSince time.Time
}

// Phases reports every account's phase, appended to dst[:0].
//
// Cost: each account's own lock once and each runner's report mutex once — no gamestate lock,
// no snapshot — so sampling the whole fleet every second (internal/controlpanel's phase
// recorder does) adds nothing measurable next to Positions.
func (m *Manager) Phases(dst []AccountPhase) []AccountPhase {
	m.mu.RLock()
	accounts := append([]*account(nil), m.accounts...)
	m.mu.RUnlock()
	now := time.Now()
	dst = dst[:0]
	for _, a := range accounts {
		a.mu.RLock()
		p := AccountPhase{Username: a.username, InGame: a.session != nil, Script: a.script}
		runner := a.runner
		a.mu.RUnlock()
		if runner != nil {
			switch runner.Status().State {
			case bot.StateRunning, bot.StateStopping:
				p.Running = true
				p.Phase, p.Since = runner.Phase()
			}
		}
		p.Event, p.EventSince = a.guardianAct.Current(now)
		dst = append(dst, p)
	}
	return dst
}

// MuleStatus is one account's published MuleReceiver status (see internal/mulestatus).
type MuleStatus = mulestatus.Status

// MuleStatuses reports the status of every account whose running script published a
// *mulestatus.Tracker — in practice every MuleReceiver. A stopped mule's last status is kept
// while its runner is the account's latest, so the dashboard still shows what it did.
func (m *Manager) MuleStatuses() []MuleStatus {
	m.mu.RLock()
	accounts := append([]*account(nil), m.accounts...)
	m.mu.RUnlock()
	var out []MuleStatus
	for _, a := range accounts {
		a.mu.RLock()
		name, runner := a.username, a.runner
		a.mu.RUnlock()
		if runner == nil {
			continue
		}
		t, ok := runner.Published().(*mulestatus.Tracker)
		if !ok || t == nil {
			continue
		}
		st := t.Snapshot()
		if st.Account == "" {
			st.Account = name
		}
		out = append(out, st)
	}
	return out
}

// GuardianActivity follows one Guardian's log lines to know what it is handling right now.
//
// Why log lines rather than a flag inside internal/randomevents: Guardian.kick already logs a
// start line and exactly one terminal line for every handled event, and internal/fleet already
// reads those lines (parseRandomEvent) for the panel's outcome list. Reading the same lines
// keeps the random-event package untouched. The limits: the blind in-combat flee has no
// terminal line, so it counts for combatFleeFor after its own line and then lapses; and an
// open "handling" with no terminal line (a panic) lapses after handlingCap.
type GuardianActivity struct {
	mu    sync.Mutex
	name  string
	since time.Time
	until time.Time // zero while open-ended
}

const (
	// combatFleeFor is the combat flee's own walk plus its 36s watch (randomevents.kick).
	combatFleeFor = 40 * time.Second
	// handlingCap bounds an event whose terminal line never came.
	handlingCap = 10 * time.Minute
)

func (g *GuardianActivity) Reset() {
	g.mu.Lock()
	g.name, g.since, g.until = "", time.Time{}, time.Time{}
	g.mu.Unlock()
}

// Note reads one Guardian log line.
func (g *GuardianActivity) Note(msg string, now time.Time) {
	if strings.HasPrefix(msg, "in combat with no tracked event — fleeing") {
		g.mu.Lock()
		if g.name == "" || !g.until.IsZero() {
			if g.name == "" {
				g.since = now
			}
			g.name = "combat flee"
			g.until = now.Add(combatFleeFor)
		}
		g.mu.Unlock()
		return
	}
	if !strings.HasPrefix(msg, randomEventPrefix) {
		return
	}
	rest := msg[len(randomEventPrefix):]
	i := strings.Index(rest, " — ")
	if i < 0 {
		return
	}
	name, tail := rest[:i], rest[i+len(" — "):]
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case tail == "handling":
		g.name, g.since, g.until = name, now, time.Time{}
	case tail == "cleared", tail == "still present after handling",
		strings.HasPrefix(tail, "handler error: "), strings.HasPrefix(tail, "rejected for this account"):
		g.name, g.since, g.until = "", time.Time{}, time.Time{}
	}
}

// Current is what the Guardian is handling at now, if anything.
func (g *GuardianActivity) Current(now time.Time) (string, time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.name == "" {
		return "", time.Time{}
	}
	if (!g.until.IsZero() && now.After(g.until)) || now.Sub(g.since) > handlingCap {
		g.name, g.since, g.until = "", time.Time{}, time.Time{}
		return "", time.Time{}
	}
	return g.name, g.since
}
