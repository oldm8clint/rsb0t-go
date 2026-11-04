package gamestate

// CLOSE_MODAL is not applied where it is read. Engine-TS's CloseModalHandler only sets Player.requestModalClose, and the
// player's own turn (Player.processQueues, after the whole tick's client input has been read) closes every modal then.
// Its comment says why: "send close modal and open a new interface same tick ... the new interface will also end up
// closed". So a knife-on-logs (or any click that opens a make menu or a dialogue) the engine reads in the same tick as a
// CLOSE_MODAL opens its menu and has it closed again before the client sees it: live on 7 October 2026 the first
// ProgressiveWCFletch fletch after every Seers bank visit read "no-menu", and so did every BassOysterCaskets fletching
// round (bank, close, knife on logs).
//
// The tick counter (Tick) is the PLAYER_INFO count, so it says how many tick ends the client has seen since it sent the
// close. It must be two: a close sent after the client saw tick K is read in tick K+1's input, or in K+2's when the client's
// view of K is stale by a link delay, and the close is applied in that same tick's player turn, before PLAYER_INFO. A click
// sent after the client has seen K+2 therefore reaches the engine in K+3 at the earliest, after the close was applied.

// ModalCloseSettleTicks is how many observed ticks must pass after a CLOSE_MODAL before a click that opens a modal is safe.
const ModalCloseSettleTicks = 2

// TicksSinceModalClose reports how many ticks have been observed since this client last sent CLOSE_MODAL. ok is false when
// it never has.
func (s *State) TicksSinceModalClose() (ticks int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.closeSent {
		return 0, false
	}
	return s.scene.tick - s.closeSentTick, true
}

// ModalCloseSettled reports whether the last CLOSE_MODAL this client sent has been applied by the engine for certain, so
// a click that opens a modal can be sent: true when none was sent or ModalCloseSettleTicks ticks have passed since.
func (s *State) ModalCloseSettled() bool {
	n, ok := s.TicksSinceModalClose()
	return !ok || n >= ModalCloseSettleTicks
}
