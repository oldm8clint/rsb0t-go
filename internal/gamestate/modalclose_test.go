package gamestate

import "testing"

func TestModalCloseSettlesAfterTwoTicks(t *testing.T) {
	s := New()
	if !s.ModalCloseSettled() {
		t.Fatal("a client that never closed a modal has nothing to settle")
	}
	if _, ok := s.TicksSinceModalClose(); ok {
		t.Fatal("no close was sent, yet a tick count was reported")
	}
	idlePlayerInfo(s)
	s.LocalCloseModal()
	if n, ok := s.TicksSinceModalClose(); !ok || n != 0 {
		t.Fatalf("right after the close: %d, %v, want 0, true", n, ok)
	}
	if s.ModalCloseSettled() {
		t.Fatal("settled in the tick the close was sent: a click now is read in the same tick and closed with it")
	}
	idlePlayerInfo(s)
	if s.ModalCloseSettled() {
		t.Fatal("settled after one tick: the close may have been read a tick late on a stale view")
	}
	idlePlayerInfo(s)
	if !s.ModalCloseSettled() {
		t.Fatal("not settled after two ticks")
	}
	// A second close starts it again.
	s.LocalCloseModal()
	if s.ModalCloseSettled() {
		t.Fatal("a fresh close did not restart the settle")
	}
}
