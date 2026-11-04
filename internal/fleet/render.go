package fleet

import (
	"fmt"
	"rsb0t-go/internal/gamestate"
)

func (m *Manager) RenderSnapshot(username string) (gamestate.RenderSnapshot, error) {
	a := m.find(username)
	if a == nil {
		return gamestate.RenderSnapshot{}, fmt.Errorf("unknown account")
	}
	a.mu.RLock()
	s := a.session
	a.mu.RUnlock()
	if s == nil {
		return gamestate.RenderSnapshot{}, fmt.Errorf("account is offline")
	}
	view, ok := s.State().RenderSnapshot()
	if !ok {
		return view, fmt.Errorf("account scene is unavailable")
	}
	return view, nil
}
