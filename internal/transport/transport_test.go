package transport

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeReader feeds a fixed sequence of "WebSocket messages" (arbitrary byte chunks) to
// the pump on demand, one per ReadMessage call, blocking on an empty channel once
// exhausted (simulating an idle connection) until told to error out (simulating close).
type fakeReader struct {
	messages chan []byte
	errCh    chan error
}

func newFakeReader() *fakeReader {
	return &fakeReader{messages: make(chan []byte, 16), errCh: make(chan error, 1)}
}

func (f *fakeReader) push(msg []byte)     { f.messages <- msg }
func (f *fakeReader) closeWith(err error) { f.errCh <- err }

func (f *fakeReader) ReadMessage() (int, []byte, error) {
	select {
	case msg := <-f.messages:
		return 0, msg, nil
	case err := <-f.errCh:
		return 0, nil, err
	}
}

type fakeWriter struct {
	mu     sync.Mutex
	sent   [][]byte
	closes int
}

func (f *fakeWriter) WriteMessage(messageType int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	f.sent = append(f.sent, cp)
	return nil
}
func (f *fakeWriter) Close() error { f.mu.Lock(); defer f.mu.Unlock(); f.closes++; return nil }

// TestReadFullSpansMultipleMessages is the core behavior this package exists to port:
// a read for N bytes must transparently consume across several underlying WebSocket
// messages, exactly like ClientStream's WebSocketReader does via its event queue.
func TestReadFullSpansMultipleMessages(t *testing.T) {
	r := newFakeReader()
	s := newStream(r, &fakeWriter{})
	defer s.Close()

	r.push([]byte{1, 2})
	r.push([]byte{3})
	r.push([]byte{4, 5, 6})

	dst := make([]byte, 6)
	if err := s.ReadFull(dst); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	want := []byte{1, 2, 3, 4, 5, 6}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("byte %d: got %d, want %d (full got=%v want=%v)", i, dst[i], want[i], dst, want)
		}
	}
}

// TestReadByteOneAtATime checks the single-byte Read path across message boundaries.
func TestReadByteOneAtATime(t *testing.T) {
	r := newFakeReader()
	s := newStream(r, &fakeWriter{})
	defer s.Close()

	r.push([]byte{0xaa})
	r.push([]byte{0xbb, 0xcc})

	for _, want := range []byte{0xaa, 0xbb, 0xcc} {
		got, err := s.ReadByte()
		if err != nil {
			t.Fatalf("ReadByte: %v", err)
		}
		if got != want {
			t.Fatalf("got %#x, want %#x", got, want)
		}
	}
}

// TestReadBlocksThenUnblocksOnLateData checks that a read waiting on data that hasn't
// arrived yet correctly wakes up once it does, rather than timing out or racing.
func TestReadBlocksThenUnblocksOnLateData(t *testing.T) {
	r := newFakeReader()
	s := newStream(r, &fakeWriter{})
	defer s.Close()

	done := make(chan error, 1)
	dst := make([]byte, 3)
	go func() { done <- s.ReadFull(dst) }()

	select {
	case <-done:
		t.Fatal("ReadFull returned before any data was pushed")
	case <-time.After(50 * time.Millisecond):
		// expected: still blocked
	}

	r.push([]byte{7, 8, 9})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ReadFull: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFull never unblocked after data arrived")
	}
	if dst[0] != 7 || dst[1] != 8 || dst[2] != 9 {
		t.Fatalf("got %v, want [7 8 9]", dst)
	}
}

// TestReadTimesOutWhenNoDataArrives exercises the same "stalled login" scenario the
// original's 30s WebSocketReader timeout exists for, using a shrunk timeout so the test
// itself stays fast.
func TestReadTimesOutWhenNoDataArrives(t *testing.T) {
	r := newFakeReader()
	s := newStream(r, &fakeWriter{})
	s.readTimeout = 30 * time.Millisecond
	defer s.Close()

	err := s.ReadFull(make([]byte, 1))
	if !errors.Is(err, ErrReadTimeout) {
		t.Fatalf("got %v, want ErrReadTimeout", err)
	}
}

// TestReadReturnsErrClosedAfterRemoteClose mirrors ClientStream surfacing the socket's
// own close/error event to any pending or future read.
func TestReadReturnsErrClosedAfterRemoteClose(t *testing.T) {
	r := newFakeReader()
	s := newStream(r, &fakeWriter{})

	done := make(chan error, 1)
	go func() { done <- s.ReadFull(make([]byte, 1)) }()

	r.closeWith(errors.New("connection reset"))

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after the remote closed with no data delivered")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFull never returned after the reader errored")
	}

	// A read started after the close must also fail immediately, not hang.
	if _, err := s.ReadByte(); err == nil {
		t.Fatal("expected ReadByte to fail immediately on an already-closed stream")
	}
}

// TestWriteRejectsOversizedPayload mirrors WebSocketWriter throwing when len > limit-100.
func TestWriteRejectsOversizedPayload(t *testing.T) {
	r := newFakeReader()
	w := &fakeWriter{}
	s := newStream(r, w)
	defer s.Close()

	ok := make([]byte, writeLimit-101)
	if err := s.Write(ok); err != nil {
		t.Fatalf("expected a payload just under the limit to succeed, got %v", err)
	}

	tooBig := make([]byte, writeLimit-99)
	if err := s.Write(tooBig); !errors.Is(err, ErrWriteTooLarge) {
		t.Fatalf("got %v, want ErrWriteTooLarge", err)
	}
}

// TestWriteSendsExactBytes confirms Write hands the frame writer exactly what was given.
func TestWriteSendsExactBytes(t *testing.T) {
	r := newFakeReader()
	w := &fakeWriter{}
	s := newStream(r, w)
	defer s.Close()

	payload := []byte{1, 2, 3, 4}
	if err := s.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Give the (synchronous, but let's be safe) write a moment; fakeWriter.WriteMessage
	// is called inline by Write itself, so this should already be visible.
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.sent) != 1 {
		t.Fatalf("got %d sent messages, want 1", len(w.sent))
	}
	for i, b := range payload {
		if w.sent[0][i] != b {
			t.Fatalf("byte %d: got %d, want %d", i, w.sent[0][i], b)
		}
	}
}

func TestCloseReleasesSocketAfterReadFailure(t *testing.T) {
	r := newFakeReader()
	w := &fakeWriter{}
	s := newStream(r, w)
	r.closeWith(errors.New("remote read failed"))
	if _, err := s.ReadByte(); err == nil {
		t.Fatal("expected read failure")
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.Close() }()
	}
	wg.Wait()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closes != 1 {
		t.Fatalf("socket closes=%d want 1", w.closes)
	}
}
