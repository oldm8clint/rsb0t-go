package fleetrunner

import (
	"sync"
	"time"

	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// PickSeek remembers which spawns an account has walked to look for a pickpocket rung's NPCs (thieverplan.SeekTarget).
// One lives on the bot and every Deps it builds points at it, so the memory outlives the Deps. The zero value is
// ready; a nil *PickSeek forgets nothing and remembers nothing.
type PickSeek struct {
	mu   sync.Mutex
	went map[tile.Tile]time.Time
}

func (s *PickSeek) wentAt(t tile.Tile) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.went[t]
	return at, ok
}

func (s *PickSeek) note(t tile.Tile, at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.went == nil {
		s.went = map[tile.Tile]time.Time{}
	}
	s.went[t] = at
}

// seekWalkTimeout bounds one walk to a spawn: the farthest is the market's width away.
const seekWalkTimeout = 90 * time.Second

// seekRung walks an account that sees none of m's NPCs to the nearest spawn it has not just looked at (live 11 Oct
// 2026: 21,864 "nothing to rob" lines; the account stopped at the Knight leash's edge by the bank, out of sight of
// every Knight, and polled an empty list). It reports whether it walked. A rung with no spawn table, or an account
// that has been to every spawn inside thieverplan.SeekMemory, does not walk, and the caller waits and polls as before.
func seekRung(d *Deps, m thieverplan.Method) bool {
	here := CurrentTile(d)
	if here == nil || here.Level != m.Anchor.Level {
		return false
	}
	spawns := thieverplan.SeekSpawns(m)
	if len(spawns) == 0 {
		return false
	}
	now := time.Now()
	target, ok := thieverplan.SeekTarget(spawns, *here, d.Seek.wentAt, now)
	if !ok {
		return false
	}
	d.Seek.note(target, now)
	d.Logf("pickpocket: no %s in sight from %s: walking to its spawn at %d,%d (%d tiles; %d spawns in the leash)",
		m.Label, HereIs(d), target.X, target.Z, tile.Chebyshev(*here, target), len(spawns))
	return freshstartrunner.WalkWithWalker(Fresh(d), target, thieverplan.SeekArrive-1, seekWalkTimeout)
}
