package fleet

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/worldhopplan"
)

// MAKING ROOM ON A WORLD — the live half. worldhopplan/roomplan.go holds the rules and the
// reasoning; this file owns the bookkeeping and performs the hop.
//
// The owner's instruction, 2026-09-22: "if they need to go to world 1, accounts log out and
// hop to world 2/3 to make space". That is exactly what this does, and the two words that
// carry the design are NEED and SPACE.
//
// NEED is a pin. worldfailover.go already answers "this world is full" well for an account
// that only wants to be somewhere — it moves it. That answer is wrong for an account that
// needs a PARTICULAR world: a donor walking to the mule, or a swarm account joining a
// gather one floor above the Kalphite Queen. Those accounts must be told apart from the
// rest, and a pin is how they say so. Nothing is pinned by default, so a fleet that never
// pins anything behaves exactly as it did before this file existed.
//
// SPACE is made by asking somebody else to leave. Which somebody is worldhopplan's
// decision, not this file's — see ChooseEvictee's own rules and, in particular, why the
// mule can never be the one that moves.
//
// WHAT THIS CANNOT DO, said plainly because it bounds every caller. It cannot see a world's
// real population; the live server's cap counts every player on the server and this fleet knows only
// its own accounts. It cannot reserve the freed slot either — between the evictee logging
// out and the waiting account logging in, anybody on the internet may take it. So this is a
// nudge that makes success likelier, never a guarantee, and every caller must still tolerate
// being refused again.

// worldSpaceFields is what this file adds to Manager, embedded so fleet.go takes one line —
// the same shape failoverFields uses.
type worldSpaceFields struct {
	spaceOnce sync.Once
	space     *worldSpace
}

// worldSpace is the fleet-wide record of who needs which world and who has recently been
// asked to leave one.
type worldSpace struct {
	mu sync.Mutex
	// pinned maps a username to the world it must stay on.
	pinned map[string]int
	// evicted maps a username to when it was last asked to leave a world, so the same
	// account is not chosen over and over (roomplan rule 4).
	evicted map[string]time.Time
	// lastAsk maps a world to when room was last made on it, pacing the asks.
	lastAsk map[int]time.Time
}

func newWorldSpace() *worldSpace {
	return &worldSpace{
		pinned:  map[string]int{},
		evicted: map[string]time.Time{},
		lastAsk: map[int]time.Time{},
	}
}

func (m *Manager) worldSpace() *worldSpace {
	m.spaceOnce.Do(func() { m.space = newWorldSpace() })
	return m.space
}

// PinWorld records that username needs to be on world: it will not be moved off it to make
// room for anybody else, and when it is refused for fullness the fleet tries to free a slot
// there instead of sending it somewhere else.
//
// This is what the mule should use, and what a donor heading for the mule's world should use
// for as long as the hand-off lasts. world 0 clears the pin, so a caller with a
// "wherever the mule is" number can pass it straight through without a special case.
func (m *Manager) PinWorld(username string, world int) error {
	if m.find(username) == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	s := m.worldSpace()
	s.mu.Lock()
	defer s.mu.Unlock()
	if world <= 0 {
		delete(s.pinned, username)
		return nil
	}
	s.pinned[username] = world
	return nil
}

// UnpinWorld releases username's pin. Safe to call when it has none.
func (m *Manager) UnpinWorld(username string) {
	s := m.worldSpace()
	s.mu.Lock()
	delete(s.pinned, username)
	s.mu.Unlock()
}

// PinnedWorld reports the world username must stay on, and whether it is pinned at all.
func (m *Manager) PinnedWorld(username string) (int, bool) {
	s := m.worldSpace()
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.pinned[username]
	return w, ok
}

// noteEvicted records that username was just asked to leave a world.
func (s *worldSpace) noteEvicted(username string, at time.Time) {
	s.mu.Lock()
	s.evicted[username] = at
	s.mu.Unlock()
}

// candidates builds the plan's view of every account the fleet is holding. Takes each
// account's own lock briefly and nothing else, so it is safe to call from a login path.
func (m *Manager) roomCandidates() []worldhopplan.RoomCandidate {
	s := m.worldSpace()
	s.mu.Lock()
	pinned := make(map[string]int, len(s.pinned))
	for k, v := range s.pinned {
		pinned[k] = v
	}
	evicted := make(map[string]time.Time, len(s.evicted))
	for k, v := range s.evicted {
		evicted[k] = v
	}
	s.mu.Unlock()

	m.mu.Lock()
	accts := make([]*account, len(m.accounts))
	copy(accts, m.accounts)
	m.mu.Unlock()

	out := make([]worldhopplan.RoomCandidate, 0, len(accts))
	for _, a := range accts {
		a.mu.RLock()
		world, hopping, inGame := a.world, a.hopping, a.session != nil
		a.mu.RUnlock()
		if world <= 0 {
			world = m.FleetWorld()
		}
		_, isPinned := pinned[a.username]
		out = append(out, worldhopplan.RoomCandidate{
			Username:    a.username,
			World:       world,
			Pinned:      isPinned,
			Hopping:     hopping,
			InGame:      inGame,
			LastEvicted: evicted[a.username],
			Spread:      spreadOf(a.username),
		})
	}
	return out
}

// WorldCensus reports how many of this fleet's accounts are currently in game on each
// world. It is the fleet's own headcount, NOT the world's population — every other player
// on the server is invisible to it — so it answers "how much of this world is us", which is
// the only part the fleet can do anything about.
func (m *Manager) WorldCensus() map[int]int {
	out := map[int]int{}
	for _, c := range m.roomCandidates() {
		if c.InGame {
			out[c.World]++
		}
	}
	return out
}

// MakeRoomOn asks one movable account to leave world so that forWho can get in. Reports
// whether an account was actually asked to move.
//
// false is an ordinary answer, not a fault: the world may hold only pinned accounts, every
// movable one may have been asked too recently, or room may simply have been made a moment
// ago and not yet taken effect. The caller waits and tries again.
func (m *Manager) MakeRoomOn(world int, forWho string) bool {
	if world <= 0 {
		return false
	}
	// With one configured world there is nowhere to send an evictee, so asking anybody to
	// leave would log an account out to no purpose.
	if !m.WorldsAreDistinct() || len(m.worldMap().Available()) < 2 {
		return false
	}

	now := time.Now()
	s := m.worldSpace()
	s.mu.Lock()
	if !worldhopplan.CanAskAgain(s.lastAsk[world], now, worldhopplan.DefaultMakeRoomInterval) {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	cands := m.roomCandidates()
	evictee, ok := worldhopplan.ChooseEvictee(world, cands, now, worldhopplan.DefaultRevisitCooldown)
	if !ok {
		return false
	}

	// Where the evictee goes. Any configured world that is not this one and is not
	// believed full — the same question NextOpenWorld already answers for failover, asked
	// with the evictee's own spread so two evictions do not pile onto one world.
	target, found := worldhopplan.NextOpenWorld(world, m.worldMap().Available(),
		m.fullWorlds().snapshot(), now, worldhopplan.DefaultFullWorldTTL, evictee.Spread)
	if !found {
		// Every other world is full too. Moving the evictee would just relocate the
		// problem and cost a working account its script.
		return false
	}

	s.mu.Lock()
	s.lastAsk[world] = now
	s.mu.Unlock()
	s.noteEvicted(evictee.Username, now)

	reason := fmt.Sprintf("making room on world %d for %s", world, forWho)
	if err := m.HopWorld(evictee.Username, target, reason); err != nil {
		m.logf("[%s] could not be moved off world %d to make room for %s: %v",
			evictee.Username, world, forWho, err)
		return false
	}
	m.logf("[%s] moving off world %d to world %d to make room for %s",
		evictee.Username, world, target, forWho)
	return true
}

// wantsRoomOn reports whether a, having just been refused for fullness on `world`, is an
// account that should have room made for it rather than being moved elsewhere. Only a pin
// says so — see worldhopplan.RoomIsNeeded.
func (m *Manager) wantsRoomOn(a *account, world int) bool {
	pinnedWorld, pinned := m.PinnedWorld(a.username)
	return worldhopplan.RoomIsNeeded(pinnedWorld, pinned, world)
}
