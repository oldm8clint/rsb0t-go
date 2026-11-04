package fleet

import (
	"fmt"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/pathfind"
)

// WithSession runs fn on the account's EXISTING connected session, the same one its bot and
// the capture/stream observation read — it never connects, logs in or duplicates a session.
// It exists for the control panel's opt-in stream client input (internal/controlpanel's
// streaminput.go), which validates every action and only uses this while it holds an
// exclusive input lease with the account's bot stopped. An offline account is an error.
func (m *Manager) WithSession(username string, fn func(s *client.Session) error) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("unknown account")
	}
	a.mu.RLock()
	s := a.session
	a.mu.RUnlock()
	if s == nil {
		return fmt.Errorf("account is offline")
	}
	return fn(s)
}

// CollisionPack is the static collision pack the fleet was configured with (nil when none),
// for routing a stream client click the way the game client routes one.
func (m *Manager) CollisionPack() *pathfind.Pack { return m.cfg.Pack }

// Components is the interface archive the fleet was configured with (nil when none), so a
// stream client interface click (an inventory option, a bank button, a dialogue option) can
// be checked against the component it names before it is sent.
func (m *Manager) Components() archive.Components { return m.cfg.Components }
