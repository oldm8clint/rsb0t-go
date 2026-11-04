package gamestate

import (
	"testing"
	"time"

	"rsb0t-go/internal/protocol/serverprot"
)

// A decoder panic must not leave the state locked: client.handleSafely recovers it, and the next
// reader would otherwise block for good.
func TestHandlePanicReleasesTheLock(t *testing.T) {
	s := New()
	func() {
		defer func() { _ = recover() }()
		s.Handle(serverprot.IF_OPENMAIN_SIDE, nil)
	}()
	done := make(chan struct{})
	go func() { s.Self(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the state stayed locked after a decoder panic")
	}
}
