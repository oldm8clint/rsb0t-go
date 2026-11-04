package controlpanel

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"rsb0t-go/internal/fleet"
)

// Other (non-fleet) players for the GPU account map.
//
// Every connected session already decodes the players around it from PLAYER_INFO. The
// union across the fleet covers every area one of our accounts is standing in — and
// nothing else: the server only sends a session the players within its own view distance.
// The panel says so; nothing here pretends to be a world-wide player list.

// SeenPlayersSource is optional. A Source that can report the other players its sessions
// see implements it; GET /api/players answers 501 for one that cannot.
type SeenPlayersSource interface {
	SeenPlayers() SeenPlayersReport
}

// SeenPlayer is one other player, as /api/players reports it.
type SeenPlayer struct {
	Name     string `json:"name"`
	X        int    `json:"x"`
	Z        int    `json:"z"`
	Level    int    `json:"level"`
	InCombat bool   `json:"inCombat,omitempty"`
	SeenBy   int    `json:"seenBy"`
}

// SeenPlayersReport is the whole answer: the players, and how many of our sessions
// contributed (the honest measure of coverage).
type SeenPlayersReport struct {
	Players   []SeenPlayer `json:"players"`
	Observers int          `json:"observers"`
	Unnamed   int          `json:"unnamed"`
}

// maxSeenPlayers bounds the response. A few hundred sessions each seeing up to the
// engine's own per-view player cap would still fit comfortably.
const maxSeenPlayers = 5000

// defaultSeenPlayersTTL coalesces map panels (several tabs, several map panels, the
// panel's own poll) onto one pass over the fleet. That pass takes ONE gamestate lock per
// connected session — not a status snapshot, which takes each about eight times — and is
// only ever taken when a browser asks.
const defaultSeenPlayersTTL = time.Second

// seenPlayersCache is FleetSource's coalescing window for SeenPlayers, the same shape as
// cachedSnapshot (snapshotcache.go): no background refresh, the mutex released around the
// read itself so a slow session never serializes every reader.
type seenPlayersCache struct {
	read func() fleet.SeenPlayers
	ttl  time.Duration
	mu   sync.Mutex
	at   time.Time
	val  SeenPlayersReport
}

func (c *seenPlayersCache) get(now func() time.Time) SeenPlayersReport {
	ttl := c.ttl
	if ttl <= 0 {
		ttl = defaultSeenPlayersTTL
	}
	c.mu.Lock()
	if c.at.IsZero() || now().Sub(c.at) >= ttl {
		c.mu.Unlock()
		raw := c.read()
		taken := now()
		report := SeenPlayersReport{Players: make([]SeenPlayer, 0, len(raw.Players)), Observers: raw.Observers, Unnamed: raw.Unnamed}
		for _, p := range raw.Players {
			report.Players = append(report.Players, SeenPlayer{Name: p.Name, X: p.X, Z: p.Z, Level: p.Level, InCombat: p.InCombat, SeenBy: p.SeenBy})
		}
		c.mu.Lock()
		if c.at.IsZero() || taken.After(c.at) {
			c.val, c.at = report, taken
		}
	}
	out := c.val
	c.mu.Unlock()
	return out
}

// SeenPlayers implements SeenPlayersSource from the live fleet. A FleetSource built
// without a manager (tests' newFleetSource with no reader) reports nothing seen.
func (f *FleetSource) SeenPlayers() SeenPlayersReport {
	if f.seen.read == nil {
		return SeenPlayersReport{Players: []SeenPlayer{}}
	}
	return f.seen.get(f.now)
}

// SeenPlayers implements SeenPlayersSource for the simulated fleet: a few SYNTHETIC
// strangers beside each in-game fake account, so the map's other-player layer can be
// exercised under -fake. Like every -fake value, none of this is a real player.
func (f *FakeSource) SeenPlayers() SeenPlayersReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := SeenPlayersReport{Players: []SeenPlayer{}}
	for i, a := range f.accounts {
		if a.State != StateIngame || a.Pos == nil {
			continue
		}
		out.Observers++
		for j := 0; j < 3; j++ {
			out.Players = append(out.Players, SeenPlayer{
				Name: fmt.Sprintf("Stranger %d-%d", i+1, j+1),
				X:    a.Pos.X + 3*j - 3, Z: a.Pos.Z + 2 - j, Level: a.Pos.Level, SeenBy: 1,
			})
		}
	}
	return out
}

func (s *Server) handleSeenPlayers(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	src, ok := s.src.(SeenPlayersSource)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not report the players its sessions see")
		return
	}
	report := src.SeenPlayers()
	truncated := false
	if len(report.Players) > maxSeenPlayers {
		report.Players, truncated = report.Players[:maxSeenPlayers], true
	}
	writeJSON(w, http.StatusOK, struct {
		SeenPlayersReport
		Truncated bool   `json:"truncated"`
		Coverage  string `json:"coverage"`
	}{report, truncated, "only players within view distance of one of this fleet's connected accounts; not the whole world"})
}
