package fleet

import "rsb0t-go/internal/client"

// AccountPosition is one connected account's current tile.
type AccountPosition struct {
	Username string
	X, Z     int
	Level    int
}

// sessionTile is the one gamestate read Positions makes per connected session. A variable
// so tests can count it: it takes the session's state lock once.
var sessionTile = func(s *client.Session) (x, z, level int, ok bool) {
	st := s.State()
	if st == nil {
		return 0, 0, 0, false
	}
	return st.Tile()
}

// Positions reports the tile of every connected account whose position is known, appended
// to dst[:0].
//
// Cost: one gamestate read lock per CONNECTED session and nothing else. Snapshot takes each
// session's lock about eight times and copies its inventories, skills and log lines, so a
// caller that only needs positions every second (the control panel's travel heat map) uses
// this instead and adds no whole-fleet snapshots.
func (m *Manager) Positions(dst []AccountPosition) []AccountPosition {
	m.mu.RLock()
	accounts := append([]*account(nil), m.accounts...)
	m.mu.RUnlock()
	dst = dst[:0]
	for _, a := range accounts {
		a.mu.RLock()
		name, session := a.username, a.session
		a.mu.RUnlock()
		if session == nil {
			continue
		}
		if x, z, level, ok := sessionTile(session); ok {
			dst = append(dst, AccountPosition{Username: name, X: x, Z: z, Level: level})
		}
	}
	return dst
}
