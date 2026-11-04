package fleet

import (
	"errors"
	"sync"
	"time"

	"rsb0t-go/internal/client"
	"rsb0t-go/internal/scripts/worldhopplan"
)

// FULL-WORLD FAILOVER: an account turned away because a world is full moves to one that
// has room, instead of knocking on the full one forever.
//
// THE INCIDENT, measured on the live fleet 2026-09-22 (the numbers are in
// worldhopplan's own doc comment): 768 login rejections, every one of them response 7 —
// "This world is full. Please use a different world.", the vendored client's own strings at
// src/client/shell/Client.ts:2054. 222 accounts in game, 57 reconnecting forever, 20 queued.
// All four routes stayed healthy and kept logging accounts in throughout, which is what
// rules out both of the limits this fleet usually hits: each route still HELD 72-78 accounts
// (so not the per-address concurrent ceiling) and ordinary successes were interleaved with
// the refusals (so not a rate verdict either). The binding limit was the world's own
// population cap.
//
// WHAT WAS ALREADY RIGHT, and is worth saying because it means nothing here is a bug fix to
// broken behaviour: response 7 was already in retryableLoginError's list, so those accounts
// were never retired, and it was never in isLoginRateRejection's, so it never tripped
// loginRateGate.Penalize and never held the fleet back. The fleet degraded exactly as
// designed. It was just knocking on a door that was not going to open.
//
// WHAT THIS ADDS: three things, all small.
//
//  1. A FLEET-WIDE MEMORY of which worlds are full. One account's response 7 tells the other
//     ~300 not to bother for a while. Without it, every account rediscovers the same full
//     world for itself, which is precisely the 768 attempts.
//  2. A MOVE, not a wait. An account refused for fullness is re-pointed at a world with
//     room and retried there, rather than backing off against the full one.
//  3. RE-HOMING. The account's HOME world moves with it. This matters and is easy to get
//     wrong: without it, donorrunner's hop-home would push the account straight back into
//     the world that had no room for it, once per hand-off, forever.
//
// AND WHAT IT DELIBERATELY DOES NOT DO: invent a destination. With one configured world
// (every roster that has not been told about w2) there is nowhere to fail over TO, so
// none of this engages and the behaviour is exactly what it was. Failover needs the roster
// to say which worlds exist — "worldList": [1,2] — because nothing can safely guess that a
// world exists, and pointing real accounts at a host nobody has checked is worse than
// waiting.

// fullWorlds is the fleet-wide record of which worlds answered "full" and when.
type fullWorlds struct {
	mu   sync.Mutex
	seen map[int]time.Time
}

func newFullWorlds() *fullWorlds { return &fullWorlds{seen: map[int]time.Time{}} }

// note records that world answered response 7 just now.
func (f *fullWorlds) note(world int, at time.Time) {
	if world <= 0 {
		return
	}
	f.mu.Lock()
	f.seen[world] = at
	f.mu.Unlock()
}

// clear forgets a world, called when an account successfully logs in to it — the most
// direct possible evidence that it has room again.
func (f *fullWorlds) clear(world int) {
	f.mu.Lock()
	delete(f.seen, world)
	f.mu.Unlock()
}

// snapshot copies the record for the plan, which takes plain values.
func (f *fullWorlds) snapshot() map[int]time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[int]time.Time, len(f.seen))
	for k, v := range f.seen {
		out[k] = v
	}
	return out
}

// FullWorlds reports which worlds are currently believed full, and when each last said so.
// For the control panel and for a harness.
func (m *Manager) FullWorlds() map[int]time.Time {
	m.fullOnce.Do(func() { m.full = newFullWorlds() })
	return m.full.snapshot()
}

// failoverFields is what this file adds to Manager, embedded so fleet.go takes one line.
type failoverFields struct {
	fullOnce sync.Once
	full     *fullWorlds
}

func (m *Manager) fullWorlds() *fullWorlds {
	m.fullOnce.Do(func() { m.full = newFullWorlds() })
	return m.full
}

// isWorldFullRejection reports whether err is login response 7.
func isWorldFullRejection(err error) bool {
	var rejected *client.LoginRejectedError
	return errors.As(err, &rejected) && rejected.Response == client.WorldFullResponse
}

// noteWorldOutcome records what this login attempt learned about the world it dialled: that
// it is full, or — on success — that it has room. Called from the login path for every
// attempt, so the fleet's shared picture is built from real answers rather than guesses.
func (m *Manager) noteWorldOutcome(a *account, err error) {
	world := m.worldOf(a)
	if world <= 0 {
		world = m.FleetWorld()
	}
	switch {
	case err == nil:
		m.fullWorlds().clear(world)
	case isWorldFullRejection(err):
		m.fullWorlds().note(world, time.Now())
	}
}

// failoverToOpenWorld moves a that was just refused for fullness onto a world with room.
// Reports whether it moved; false means there was nowhere to go and the caller should fall
// back to its ordinary backoff.
//
// The move is immediate and does NOT consume a retry backoff, because it is not a retry of
// the same thing: a different world is a different question, and the answer arrives in one
// login. The fleet's own rate gate still paces it like every other login.
func (m *Manager) failoverToOpenWorld(a *account) bool {
	current := m.worldOf(a)
	if current <= 0 {
		current = m.FleetWorld()
	}
	// An account PINNED to this world is the one case where moving it is the wrong answer:
	// it is on this world because it has to be (a donor going to the mule, a swarm account
	// joining a gather), so "world 2 instead" does not serve it at all. Free a slot here
	// instead and let it retry, rather than sending it where it cannot do its job.
	//
	// Returning false deliberately: the account keeps its world and falls back to the
	// caller's ordinary backoff, which is what retries it. Room being made is a nudge, not
	// a reservation — the freed slot can be taken by any player on the server — so the
	// retry has to happen either way. See worldspace.go.
	if m.wantsRoomOn(a, current) {
		if m.MakeRoomOn(current, a.username) {
			m.alogf(a, "[%s] needs world %d and it is full — asked another account to move off it", a.username, current)
		} else {
			m.alogf(a, "[%s] needs world %d and it is full — no account could be moved off it right now, waiting", a.username, current)
		}
		return false
	}

	wm := m.worldMap()
	available := wm.Available()
	if len(available) < 2 {
		// One configured world: nothing to fail over to. Say so once rather than silently
		// doing nothing, because "the roster never listed w2" is the likeliest reason a
		// fleet is still stuck against a full world after this shipped.
		m.alogf(a, "[%s] world is full and no other world is configured — add \"worldList\": [1,2] to the roster so accounts can spread", a.username)
		return false
	}
	next, ok := worldhopplan.NextOpenWorld(current, available, m.fullWorlds().snapshot(), time.Now(),
		worldhopplan.DefaultFullWorldTTL, spreadOf(a.username))
	if !ok {
		m.alogf(a, "[%s] every configured world (%v) is full — waiting rather than knocking", a.username, available)
		return false
	}

	a.mu.Lock()
	a.world = next
	// Re-home. Without this, donorrunner's hop-home would send the account straight back
	// into the world that had no room for it after every hand-off.
	a.hop.Home, a.hop.Current = next, next
	a.mu.Unlock()
	m.alogf(a, "[%s] world %d is full — moving to world %d", a.username, current, next)
	return true
}

// spreadOf turns an account name into a small stable number, so accounts shut out of one
// world do not all choose the same replacement in the same instant and fill it. FNV-1a, 32
// bit; the exact hash does not matter, only that it is stable per account and well spread.
func spreadOf(username string) int {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(username); i++ {
		h ^= uint32(username[i])
		h *= prime
	}
	return int(h & 0x7fffffff)
}
