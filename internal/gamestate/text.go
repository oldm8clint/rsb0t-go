package gamestate

// TextKnown is Text plus whether IF_SETTEXT has set comID at all, so a caller can tell text the
// server cleared to "" (which the client then shows as nothing) from text never set (where the
// interface archive's own text still shows).
func (s *State) TextKnown(comID int) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	text, ok := s.texts[comID]
	return text, ok
}
