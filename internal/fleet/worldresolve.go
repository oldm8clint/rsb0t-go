package fleet

import (
	"fmt"
	"strings"
	"sync"

	"rsb0t-go/internal/worlds"
)

// This file is per-account world resolution: which URL an account's login actually dials.
//
// The whole mechanism is one string. The login block carries no world number (grep
// internal/login — nothing writes one), and w2.example.invalid's /crc0 checksums and RSA modulus
// are byte-identical to w1's (measured 2026-09-22), so a world is purely a different
// client.Config.WSURL. Everything here exists to make sure that string is resolved from the
// ACCOUNT rather than from the fleet, at every site that builds a session — including the
// reconnect path, which is the one that would otherwise silently drag an account back to the
// fleet default after a drop.
//
// It lives in its own file rather than in fleet.go because fleet.go is another session's
// claim (COLLAB rule 4): the edits there are kept to the few lines that genuinely cannot
// live anywhere else.

// worldMap returns the Manager's world map, building the derived default on first use when
// the Config did not supply one. A fleet that never mentions worlds gets a Map whose only
// world is its own wsURL's, so Endpoints(0) hands back exactly the bytes it always used.
func (m *Manager) worldMap() *worlds.Map {
	m.worldOnce.Do(func() {
		if m.cfg.Worlds != nil {
			m.worlds = m.cfg.Worlds
			return
		}
		ws := m.cfg.WSURL
		if strings.TrimSpace(ws) == "" {
			// A Manager built with no WSURL at all (several tests, internal/controlpanel's
			// own fixtures) never dials anything. Give it a placeholder rather than
			// panicking or returning nil for every caller to check.
			ws = "ws://unconfigured.invalid/"
		}
		w, err := worlds.New(ws, m.cfg.HTTPBase, nil)
		if err != nil {
			// worlds.New only fails on an empty wsURL or a bad explicit entry, neither of
			// which can happen on this branch; the fallback keeps the type non-nil anyway.
			w, _ = worlds.New("ws://unconfigured.invalid/", "", nil)
		}
		m.worlds = w
	})
	return m.worlds
}

// FleetWorld is the world the fleet's own wsURL names — where an account that names no
// world of its own ends up.
func (m *Manager) FleetWorld() int { return m.worldMap().FleetWorld() }

// WorldsAreDistinct reports whether this deployment can actually put two accounts on two
// different worlds (internal/worlds.Map.Distinct). False against a local Engine-TS, where
// every world number resolves to the same URL.
//
// Exported because a harness must be able to say so out loud: a "hop" on a deployment that
// is not distinct is a logout and a login on the identical URL, which proves the
// logout/login cycle and NOTHING about two worlds.
func (m *Manager) WorldsAreDistinct() bool { return m.worldMap().Distinct() }

// worldOf returns the world a is on, with 0 meaning "the fleet's own".
func (m *Manager) worldOf(a *account) int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.world
}

// WorldOf returns the world the named account is configured for, or 0 when this Manager has
// no such account. An account that named no world reports the fleet's own world rather than
// 0, so a caller never has to know which case it is in.
//
// This is the resolver a donor script uses to answer "which world is the mule on" without
// hardcoding it — see Config.WorldOf's wiring in StartBot. It is exact for an account in
// THIS process, which is every account in the fleet; it cannot answer for a mule run
// somewhere else, and bus presence (internal/mulebus) is what covers that case.
func (m *Manager) WorldOf(username string) int {
	a := m.find(username)
	if a == nil {
		return 0
	}
	w := m.worldOf(a)
	if w <= 0 {
		return m.FleetWorld()
	}
	return w
}

// endpointsFor resolves the WebSocket URL and HTTP base a's next login must use. Called
// from loginOnce, so the initial connect, an operator's ConnectExisting, reconnectLoop's
// automatic retry and a world hop all go through the same one answer — the reconnect path
// being the one that matters, since an account that came back on the fleet default would be
// silently on the wrong world with nothing reporting it.
func (m *Manager) endpointsFor(a *account) (wsURL, httpBase string) {
	e, err := m.worldMap().Endpoints(m.worldOf(a))
	if err != nil {
		// A world outside the valid range cannot reach this point: SetWorld and
		// ConnectWorld both reject one. Fall back to the fleet's own endpoints rather
		// than dialling an empty string.
		return m.cfg.WSURL, m.cfg.HTTPBase
	}
	return e.WSURL, e.HTTPBase
}

// SetWorld changes which world an account's NEXT login dials. It does not itself move a
// connected account: the session already open stays on the world it logged in to until it
// is logged out and back in (HopWorld does exactly that). Rejects an out-of-range world
// rather than clamping it, for the same reason internal/worlds does — an operator who wrote
// it believes the account moved.
func (m *Manager) SetWorld(username string, world int) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	n, err := m.worldMap().Normalise(world)
	if err != nil {
		return fmt.Errorf("fleet: %s: %w", username, err)
	}
	a.mu.Lock()
	a.world = n
	if a.hop.Home == 0 {
		a.hop.Home = n
	}
	a.mu.Unlock()
	return nil
}

// HomeWorldOf is the world the roster gave this account: where a hop returns it to. 0 for
// an unknown account.
func (m *Manager) HomeWorldOf(username string) int {
	a := m.find(username)
	if a == nil {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.hop.Home > 0 {
		return a.hop.Home
	}
	return m.FleetWorld()
}

// WorldCounts reports how many accounts this Manager holds per world, for a startup line
// and for the control panel. Keyed by the resolved world number, never 0.
func (m *Manager) WorldCounts() map[int]int {
	m.mu.RLock()
	accounts := append([]*account(nil), m.accounts...)
	m.mu.RUnlock()
	out := map[int]int{}
	fleetWorld := m.FleetWorld()
	for _, a := range accounts {
		w := m.worldOf(a)
		if w <= 0 {
			w = fleetWorld
		}
		out[w]++
	}
	return out
}

// worldFields is the state this file adds to Manager. Embedded rather than added as three
// loose fields on Manager itself, to keep the diff on fleet.go (another session's claim) to
// a single embedded line.
type worldFields struct {
	worldOnce sync.Once
	worlds    *worlds.Map
	// hopTimer is the server's world-hop timer as the fleet has learned it (hoptimer.go), and
	// loginBusy the cached answer to "is the login path busy" (worldhome.go).
	hopTimer  hopTimerState
	loginBusy loginBusyCache
}
