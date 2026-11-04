package fleet

import (
	"fmt"
	"sync/atomic"
	"time"
)

// A script's say in where an account lives, and what it may know about the fleet's hops. Added
// for ScarabSwarm's world-2 cohort ("scarabw2", 2026-09-23): users ruled that the Scarab
// accounts live on world 2 and hop to world 1 only to trade with the one mule. Until now only the
// roster's "world" could make world 2 an account's HOME (SetWorld sets Home only while it is
// still 0), and the roster is the owner's file with the shared password in it, so a cohort switch
// could not say it at all. Each of these is wired onto bot.Context in StartBot and is nil-safe
// there.

// SetHomeWorld makes world username's home for the hop bookkeeping: where a failed hop lands it,
// where HopHome sends it and what worldhopplan counts as home. It does not move a connected
// account and does not change which world its next login dials (SetWorld does that). It refuses
// a world this deployment does not list, for the same reason SetWorld refuses an out-of-range one:
// an operator who wrote "3" believes the account moved, and world 3 no longer exists.
func (m *Manager) SetHomeWorld(username string, world int) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	n, err := m.listedWorld(world)
	if err != nil {
		return fmt.Errorf("fleet: %s: %w", username, err)
	}
	a.mu.Lock()
	changed := a.hop.Home != n
	a.hop.Home = n
	a.mu.Unlock()
	if changed {
		m.alogf(a, "[%s] home world is now %d", username, n)
	}
	return nil
}

// listedWorld normalises world and refuses one this deployment does not list.
func (m *Manager) listedWorld(world int) (int, error) {
	n, err := m.worldMap().Normalise(world)
	if err != nil {
		return 0, err
	}
	for _, w := range m.worldMap().Available() {
		if w == n {
			return n, nil
		}
	}
	return 0, fmt.Errorf("world %d is not one this deployment has (%v)", n, m.worldMap().Available())
}

// StartWorld is the world a process start should first log an account in on, given the
// roster's world for it and the settings its script will start with: the script's own
// "homeWorld" when it names a world this deployment lists, else roster. A cohort whose saved
// script assignment says homeWorld 2 then lands on world 2 at a restart, instead of logging in
// on world 1 and hopping home (two more logins each, at the worst moment, in the restart ramp).
// Scripts that set no homeWorld, and a homeWorld this deployment does not have, change nothing.
func (m *Manager) StartWorld(roster int, settings map[string]interface{}) int {
	w := 0
	switch v := settings["homeWorld"].(type) {
	case float64:
		w = int(v)
	case int:
		w = v
	}
	if w <= 0 {
		return roster
	}
	n, err := m.listedWorld(w)
	if err != nil {
		return roster
	}
	return n
}

// Hopping reports whether a world hop is running for username right now. A hop outlives the
// script that asked for it — the fleet stops that script at the logout and starts a new one
// after the login — so the new script needs this to tell "my hop is still under way" from "my
// hop gave up and left me here".
func (m *Manager) Hopping(username string) bool {
	a := m.find(username)
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.hopping
}

// loginBusyConnecting is how many accounts may be inside a login at once before the fleet counts
// as busy. A handful of ordinary reconnects is steady state; a restart ramp is dozens.
const loginBusyConnecting = 3

// loginBusyEvery bounds how often LoginBusy walks the roster: every Scarab account asks on its
// own loop, and the answer does not need to be fresher than this.
const loginBusyEvery = time.Second

// loginBusyCache is the last LoginBusy answer, fleet-wide.
type loginBusyCache struct {
	at   atomic.Int64 // unix nanos of the last walk
	busy atomic.Bool
}

// LoginBusy reports whether the fleet's login path is busy: more than a handful of accounts inside
// a login at once, past their route's admission (at its login gate or in the attempt). A script
// uses it to avoid starting a hop (two more logins) on top of a restart ramp. Accounts parked
// against the per-address ceiling wait in their own backoff, not in a login, so they do not make
// the fleet look busy for hours.
//
// Neither do accounts queued for a route slot: they send nothing to the server, and a hop keeps
// its own account's slot. They used to count, and a route that stays unhealthy keeps its share of
// the roster queued for as long as it is down. Live on 6 Oct 2026 psiphon2 refused new handshakes
// from 05:20: 27 accounts queued for its free slots, LoginBusy held true, and every world-2 and
// world-3 account's mule errand waited "the fleet's logins are busy" (425 times in 45 minutes),
// 47 of them idle in Stage 5's magic half for 10-38 minutes each. A restart ramp still counts: its
// admitted accounts wait at their routes' login gates, inside a login.
func (m *Manager) LoginBusy() bool {
	c := &m.loginBusy
	now := time.Now()
	if last := c.at.Load(); last != 0 && now.Sub(time.Unix(0, last)) < loginBusyEvery {
		return c.busy.Load()
	}
	m.mu.RLock()
	accounts := append([]*account(nil), m.accounts...)
	m.mu.RUnlock()
	inLogin := 0
	for _, a := range accounts {
		a.mu.RLock()
		if a.connecting && !a.routeWait {
			inLogin++
		}
		a.mu.RUnlock()
	}
	busy := inLogin > loginBusyConnecting
	c.busy.Store(busy)
	c.at.Store(now.UnixNano())
	return busy
}

// applyPinSetting pins username to the world its script's "pinWorld" setting names (0 or absent
// leaves any pin alone). This is how the mule says it must stay on world 1: a full world 1 then
// evicts somebody else rather than failing the mule over to world 2 and sending every donor
// after it (worldspace.go). Called by StartBot BEFORE it takes the account lock — PinWorld
// takes the Manager's, and the two must never nest in that order.
func (m *Manager) applyPinSetting(username string, settings map[string]interface{}) {
	w := 0
	switch v := settings["pinWorld"].(type) {
	case float64:
		w = int(v)
	case int:
		w = v
	}
	if w <= 0 {
		return
	}
	if err := m.PinWorld(username, w); err == nil {
		m.logf("[%s] pinned to world %d by its script settings", username, w)
	}
}
