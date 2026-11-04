package client

import (
	"errors"
	"testing"
	"time"
)

// A closed stop channel makes ReadLoop return before it touches the stream at all, which is
// what lets these tests exercise the guard on a bare Session with no socket behind it. A
// regression that moved the stream read ahead of the stop check would crash here rather than
// pass quietly.

// TestReadLoopSingleReaderIsUnchanged pins the only pattern the live fleet uses: one loop per
// session, and another loop allowed once the previous one has returned. internal/fleet's
// reconnect path (startReadLoop) depends on the second half of that — the guard must release on
// return, not latch for the life of the session.
func TestReadLoopSingleReaderIsUnchanged(t *testing.T) {
	s := &Session{}
	stop := make(chan struct{})
	close(stop)

	for i := 1; i <= 3; i++ {
		if err := s.ReadLoop(stop, nil); err != nil {
			t.Fatalf("read loop %d on a session with no other reader: %v", i, err)
		}
		if s.reading.Load() {
			t.Fatalf("read loop %d left the reader flag set, so the next loop would be refused", i)
		}
	}
}

// TestReadLoopRefusesASecondReader is the guard itself: with one reader already running, the
// second gets a named error immediately instead of interleaving with it inside ReadPacket and
// wedging every goroutine on the gamestate lock with nothing printed.
func TestReadLoopRefusesASecondReader(t *testing.T) {
	s := &Session{}
	// Stand in for a first reader that is mid-loop. Done directly rather than by starting a
	// real loop because a real one needs a live stream to block on, and the thing under test
	// is what the second caller gets back, not how the first one blocks.
	s.reading.Store(true)

	stop := make(chan struct{})
	close(stop)

	done := make(chan error, 1)
	go func() { done <- s.ReadLoop(stop, nil) }()

	select {
	case err := <-done:
		if !errors.Is(err, ErrReaderAlreadyRunning) {
			t.Fatalf("second reader got %v, want ErrReaderAlreadyRunning", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second reader neither returned nor errored — it entered the loop, which is the silent deadlock this guard exists to prevent")
	}

	// The refused call must not have cleared the running reader's own flag.
	if !s.reading.Load() {
		t.Fatal("the refused second reader cleared the first reader's flag, which would let a third in")
	}
}
