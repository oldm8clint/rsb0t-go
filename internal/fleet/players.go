package fleet

import (
	"sort"
	"strings"

	"rsb0t-go/internal/client"
	"rsb0t-go/internal/gamestate"
)

// SeenPlayer is one other (non-fleet) player that at least one of the fleet's own
// connected sessions can currently see in its PLAYER_INFO view.
type SeenPlayer struct {
	Name     string `json:"name"`
	X        int    `json:"x"`
	Z        int    `json:"z"`
	Level    int    `json:"level"`
	InCombat bool   `json:"inCombat,omitempty"`
	// SeenBy is how many of the fleet's sessions see this player right now.
	SeenBy int `json:"seenBy"`
}

// SeenPlayers is the union of every connected session's view of other players.
//
// Coverage is ONLY what the fleet can see: the server sends each session the players
// within its own view distance, so a player nowhere near one of our accounts is simply
// absent. Observers says how many sessions contributed, so a consumer can say so.
type SeenPlayers struct {
	Players   []SeenPlayer `json:"players"`
	Observers int          `json:"observers"`
	// Unnamed counts players seen before their appearance block (and so their name)
	// arrived; they cannot be de-duplicated across sessions, so they are left out.
	Unnamed int `json:"unnamed"`
}

// sessionPlayers is the one gamestate read SeenPlayers makes per connected session. A
// variable so tests can count it: that read takes the session's state lock, the same lock
// its packet read loop holds, so the cost is exactly one lock per session per call.
var sessionPlayers = func(s *client.Session) []gamestate.Player {
	st := s.State()
	if st == nil {
		return nil
	}
	return st.Players()
}

// PlayerNameKey is the comparison form of a player or account name: case-insensitive, and
// '_' equal to ' ' (the display form gamestate reports turns '_' into a space).
func PlayerNameKey(name string) string {
	return strings.TrimSpace(strings.ToLower(strings.Replace(name, "_", " ", -1)))
}

// SeenPlayers collects every other player the fleet's connected sessions can see,
// de-duplicated by name, excluding the fleet's own accounts (connected or not — an own
// account seen by another session is still ours).
//
// Cost: one gamestate read lock per CONNECTED session and nothing else — no whole-fleet
// status snapshot (which takes each session's lock about eight times). Callers that may
// be asked repeatedly should still coalesce, as the control panel does.
func (m *Manager) SeenPlayers() SeenPlayers {
	m.mu.RLock()
	accounts := append([]*account(nil), m.accounts...)
	m.mu.RUnlock()

	own := make(map[string]bool, len(accounts))
	views := make([][]gamestate.Player, 0, len(accounts))
	for _, a := range accounts {
		a.mu.RLock()
		own[PlayerNameKey(a.username)] = true
		session := a.session
		a.mu.RUnlock()
		if session == nil {
			continue
		}
		views = append(views, sessionPlayers(session))
	}
	return MergeSeenPlayers(views, own)
}

// MergeSeenPlayers merges per-session player views. Pure, so the de-duplication and the
// own-account exclusion are tested without live sessions. A player seen by several
// sessions keeps the first position reported (they describe the same tick to within the
// sessions' packet timing) and counts every session that saw it.
func MergeSeenPlayers(views [][]gamestate.Player, own map[string]bool) SeenPlayers {
	out := SeenPlayers{Players: []SeenPlayer{}, Observers: len(views)}
	index := map[string]int{}
	for _, view := range views {
		seenHere := map[string]bool{}
		for _, p := range view {
			key := PlayerNameKey(p.Name)
			if key == "" {
				out.Unnamed++
				continue
			}
			if own[key] || seenHere[key] {
				continue
			}
			seenHere[key] = true
			if i, ok := index[key]; ok {
				out.Players[i].SeenBy++
				out.Players[i].InCombat = out.Players[i].InCombat || p.InCombat
				continue
			}
			index[key] = len(out.Players)
			out.Players = append(out.Players, SeenPlayer{Name: p.Name, X: p.X, Z: p.Z, Level: p.Level, InCombat: p.InCombat, SeenBy: 1})
		}
	}
	sort.Slice(out.Players, func(i, j int) bool { return PlayerNameKey(out.Players[i].Name) < PlayerNameKey(out.Players[j].Name) })
	return out
}
