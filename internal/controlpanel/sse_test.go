package controlpanel

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSSEFrameFormat(t *testing.T) {
	cases := []struct {
		event string
		id    uint64
		data  string
		want  string
	}{
		{"event", 7, `{"a":1}`, "id: 7\nevent: event\ndata: {\"a\":1}\n\n"},
		{"status", 0, "one\ntwo\r\nthree\rfour", "event: status\ndata: one\ndata: two\ndata: three\ndata: four\n\n"},
		{"", 0, "", "data: \n\n"},
		{"event", 100, "x", "id: 100\nevent: event\ndata: x\n\n"},
		{"event", ^uint64(0), "x", "id: 18446744073709551615\nevent: event\ndata: x\n\n"},
	}
	for _, c := range cases {
		if got := string(sseFrame(c.event, c.id, []byte(c.data))); got != c.want {
			t.Errorf("sseFrame(%q, %d, %q) = %q, want %q", c.event, c.id, c.data, got, c.want)
		}
	}
	// A newline inside an event's text is escaped by JSON, so the frame stays one data line.
	frame := string(eventFrame(Event{Seq: 3, Text: "a\nb\r\nc"}))
	if strings.Count(frame, "data: ") != 1 || !strings.HasPrefix(frame, "id: 3\nevent: event\n") || !strings.HasSuffix(frame, "\n\n") {
		t.Errorf("event frame %q", frame)
	}
}

// flushErrWriter answers a real ResponseWriter's Write calls normally but fails every
// flush — standing in for a connection whose write deadline is expiring during the actual
// socket write, which on a genuine http.Flusher (Flush() with no return value) is exactly
// the failure streamWriter.flush() used to be unable to see at all (see that method's own
// doc comment). flushResponse prefers a FlushError() error method over plain Flush(), so
// implementing only that here — deliberately no Flush() — proves the code goes through
// that path rather than happening to still work via the old one.
type flushErrWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (w *flushErrWriter) FlushError() error { return w.err }

func TestStreamWriterSurfacesFlushError(t *testing.T) {
	sentinel := errors.New("write: connection timed out")
	w := &flushErrWriter{ResponseRecorder: httptest.NewRecorder(), err: sentinel}
	out := &streamWriter{w: w, timeout: time.Second}
	out.write([]byte("data: x\n\n"))
	if out.err != nil {
		t.Fatalf("write set an error before any flush: %v", out.err)
	}
	if err := out.flush(); !errors.Is(err, sentinel) {
		t.Fatalf("flush() = %v, want the underlying flush error surfaced (%v)", err, sentinel)
	}
	// Once flush has failed, the stream is done: every later write/flush is a no-op that
	// keeps reporting the same error, exactly what makes handleStream's loop `return`
	// instead of silently looping forever (see the loop's `if out.flush() != nil { return }`).
	out.write([]byte("data: y\n\n"))
	if err := out.flush(); !errors.Is(err, sentinel) {
		t.Fatalf("flush() after failure = %v, want it to keep reporting %v", err, sentinel)
	}
}

func TestStreamSendsHelloReplayStatusAndLiveEvents(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	for i := 0; i < 3; i++ {
		h.srv.hub.publish(Event{Account: "fisher01", Text: "before"})
	}
	r, res, _ := h.openStream(nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream answered %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	m := r.next()
	if m.event != sseHello {
		t.Fatalf("first message %q, want hello", m.event)
	}
	var hello helloBody
	if err := json.Unmarshal([]byte(m.data), &hello); err != nil || hello.Last != 3 || hello.Version != APIVersion {
		t.Fatalf("hello %s (%v)", m.data, err)
	}
	for i := 1; i <= 3; i++ {
		m = r.next()
		if m.event != sseEvent || m.id != strconv.Itoa(i) {
			t.Fatalf("replay %d: %+v", i, m)
		}
	}
	seen := map[string]bool{}
	for !seen[sseStatus] || !seen[sseMemory] {
		seen[r.next().event] = true
	}
	h.srv.hub.publish(Event{Account: "fisher01", Text: "live"})
	m = r.until(sseEvent)
	var ev Event
	if err := json.Unmarshal([]byte(m.data), &ev); err != nil || m.id != "4" || ev.Text != "live" {
		t.Fatalf("live event %+v (%v)", m, err)
	}
}

func TestStreamResumesAfterLastEventID(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	for i := 0; i < 5; i++ {
		h.srv.hub.publish(Event{Text: "x"})
	}
	r, _, _ := h.openStream(http.Header{"Last-Event-Id": {"3"}})
	if m := r.next(); m.event != sseHello {
		t.Fatalf("first message %q", m.event)
	}
	for _, want := range []string{"4", "5"} {
		if m := r.next(); m.event != sseEvent || m.id != want {
			t.Fatalf("got %+v, want event %s", m, want)
		}
	}
}

func TestStreamReplaysEverythingAfterARestart(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	h.srv.hub.publish(Event{Text: "x"})
	h.srv.hub.publish(Event{Text: "y"})
	// An id past the hub's newest seq came from an earlier process.
	r, _, _ := h.openStream(http.Header{"Last-Event-Id": {"999"}})
	r.next()
	for _, want := range []string{"1", "2"} {
		if m := r.next(); m.id != want {
			t.Fatalf("got %+v, want event %s", m, want)
		}
	}
}

func TestStreamCountIsCapped(t *testing.T) {
	opts := testOptions()
	opts.maxStreams = 1
	h := newHarness(t, Config{}, opts, &staticSource{})
	r, _, _ := h.openStream(nil)
	r.next()
	res := h.get("/api/stream")
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second stream answered %d, want 503", res.StatusCode)
	}
}

func TestUnchangedStatusIsNotResent(t *testing.T) {
	src := &staticSource{accounts: []AccountStatus{{Name: "fisher01", State: StateIngame}}}
	h := newHarness(t, Config{}, testOptions(), src)
	r, _, _ := h.openStream(nil)
	statuses, memories := 0, 0
	for memories < 6 {
		switch r.next().event {
		case sseStatus:
			statuses++
		case sseMemory:
			memories++
		}
	}
	if statuses != 1 {
		t.Fatalf("%d status frames over %d ticks of an unchanged fleet, want 1", statuses, memories)
	}
}

func TestSlowStreamDropsInsteadOfQueueing(t *testing.T) {
	opts := testOptions()
	opts.queue = 8
	hub := newHub(&staticSource{}, Config{EventHistory: 64, MemoryHistory: 10}, opts)
	defer hub.close()
	st, _, _, err := hub.join(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		hub.publish(Event{Text: "x"})
	}
	if len(st.queue) != 8 {
		t.Errorf("queue holds %d frames, want its cap of 8", len(st.queue))
	}
	if d := atomic.LoadUint64(&st.dropped); d != 992 {
		t.Errorf("%d dropped, want 992", d)
	}
}

func TestDroppedCountReachesTheBrowser(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	r, _, _ := h.openStream(nil)
	r.next()
	waitFor(t, "the stream to join", func() bool { return h.srv.hub.streamCount() == 1 })
	h.srv.hub.mu.Lock()
	for st := range h.srv.hub.streams {
		atomic.AddUint64(&st.dropped, 5)
	}
	h.srv.hub.mu.Unlock()
	h.srv.hub.publish(Event{Text: "wakes the writer"})
	if m := r.until(sseDropped); m.data != `{"count":5}` {
		t.Fatalf("dropped frame %q", m.data)
	}
}

func TestIdleHubNeverSamplesTheSource(t *testing.T) {
	src := &staticSource{}
	h := newHarness(t, Config{}, testOptions(), src)
	for i := 0; i < 100; i++ {
		h.srv.hub.publish(Event{Text: "x"})
	}
	time.Sleep(100 * time.Millisecond)
	if n := src.samples(); n != 0 {
		t.Fatalf("sampled %d times with no stream open", n)
	}
	_, _, stop := h.openStream(nil)
	waitFor(t, "a sample", func() bool { return src.samples() > 0 })
	stop()
	waitFor(t, "the stream to leave", func() bool { return h.srv.hub.streamCount() == 0 })
	time.Sleep(30 * time.Millisecond)
	after := src.samples()
	time.Sleep(150 * time.Millisecond)
	if n := src.samples(); n != after {
		t.Fatalf("still sampling after the last stream left: %d then %d", after, n)
	}
}
