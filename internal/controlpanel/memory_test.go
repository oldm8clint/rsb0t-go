package controlpanel

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const kb = 1 << 10

func TestHeapStaysBoundedWithNoStream(t *testing.T) {
	src := &staticSource{}
	srv, err := newServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, src, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	srv.hub.start()
	defer srv.hub.close()

	// Longer than maxTextBytes, so every kept event is a cut copy.
	text := strings.Repeat("x", 600)
	pump := func(n int) {
		for i := 0; i < n; i++ {
			srv.hub.publish(Event{Account: "fisher01", Kind: "log", Text: text})
		}
	}
	pump(2 * DefaultEventHistory)
	before := heapAfterGC()
	pump(200000)
	after := heapAfterGC()
	growth := int64(after) - int64(before)
	t.Logf("no stream: heap %d KB before, %d KB after 200000 more events, growth %d KB", before/kb, after/kb, growth/kb)
	if growth > 256*kb {
		t.Fatalf("heap grew %d KB while events were pumped with nobody watching", growth/kb)
	}
	if n := src.samples(); n != 0 {
		t.Fatalf("the source was sampled %d times with no stream", n)
	}
}

// stalledStream opens a stream over a raw connection that never reads.
func stalledStream(t *testing.T, opts options) (*Server, *stream, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv, err := start(ctx, Config{Enabled: true, Listen: "127.0.0.1:0"}, &staticSource{}, opts)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(4 * kb)
	}
	fmt.Fprintf(conn, "GET /api/stream HTTP/1.1\r\nHost: %s\r\n\r\n", srv.Addr())
	waitFor(t, "the stream to join", func() bool { return srv.hub.streamCount() == 1 })
	var st *stream
	srv.hub.mu.Lock()
	for s := range srv.hub.streams {
		st = s
	}
	srv.hub.mu.Unlock()
	return srv, st, func() {
		conn.Close()
		cancel()
		select {
		case <-srv.Done():
		case <-time.After(10 * time.Second):
			t.Error("server did not stop")
		}
	}
}

func TestHeapStaysBoundedWithAStalledStream(t *testing.T) {
	opts := testOptions()
	opts.writeTimeout = time.Hour
	opts.tick = time.Hour
	opts.shutdownGrace = 200 * time.Millisecond
	srv, st, stop := stalledStream(t, opts)
	defer stop()

	text := strings.Repeat("y", 500)
	deadline := time.Now().Add(30 * time.Second)
	for atomic.LoadUint64(&st.dropped) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the stalled stream never fell behind")
		}
		for i := 0; i < 1000; i++ {
			srv.hub.publish(Event{Account: "fisher01", Text: text})
		}
	}
	before := heapAfterGC()
	for i := 0; i < 200000; i++ {
		srv.hub.publish(Event{Account: "fisher01", Text: text})
	}
	after := heapAfterGC()
	growth := int64(after) - int64(before)
	t.Logf("stalled stream: heap %d KB before, %d KB after 200000 more events, growth %d KB, %d dropped, %d frames queued", before/kb, after/kb, growth/kb, atomic.LoadUint64(&st.dropped), len(st.queue))
	if growth > 512*kb {
		t.Fatalf("heap grew %d KB behind a stalled stream", growth/kb)
	}
	if srv.hub.streamCount() != 1 {
		t.Fatal("the stream left; this test is about one that stays connected and stalled")
	}
}

func TestStalledStreamIsDisconnected(t *testing.T) {
	opts := testOptions()
	opts.writeTimeout = 200 * time.Millisecond
	opts.shutdownGrace = 200 * time.Millisecond
	srv, _, stop := stalledStream(t, opts)
	defer stop()
	text := strings.Repeat("z", 500)
	deadline := time.Now().Add(30 * time.Second)
	for srv.hub.streamCount() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("a stream that stopped reading was never disconnected")
		}
		for i := 0; i < 1000; i++ {
			srv.hub.publish(Event{Text: text})
		}
	}
}

// TestFootprint measures what the dashboard costs; run with -v to read the numbers.
func TestFootprint(t *testing.T) {
	src := NewFakeSource(50, 1)
	for i := 0; i < 60; i++ {
		src.Step()
	}
	goroutines := runtime.NumGoroutine()
	base := heapAfterGC()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := start(ctx, Config{Enabled: true, Listen: "127.0.0.1:0"}, src, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	idle := heapAfterGC()
	idleGoroutines := runtime.NumGoroutine() - goroutines

	// Why: every line is its own string, as a real fleet's are; a shared one would measure nothing.
	for i := 0; i < DefaultEventHistory+DefaultLogHistory; i++ {
		kind := KindLog
		if i%2 == 0 {
			kind = "state"
		}
		srv.hub.publish(Event{Account: "fisher01", Kind: kind, Text: fmt.Sprintf("%s %08d", strings.Repeat("w", 111), i)})
	}
	ringFull := heapAfterGC()
	for i := 0; i < DefaultEventHistory+DefaultLogHistory; i++ {
		kind := KindLog
		if i%2 == 0 {
			kind = "state"
		}
		srv.hub.publish(Event{Account: fmt.Sprintf("%s%02d", strings.Repeat("a", maxNameBytes-2), i%100), Kind: kind, Text: fmt.Sprintf("%s %08d", strings.Repeat("v", maxTextBytes+40), i)})
	}
	ringWorst := heapAfterGC()

	res, err := http.Get("http://" + srv.Addr() + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	waitFor(t, "a status frame", func() bool {
		_, gen, _, _ := srv.hub.latest()
		return gen > 0
	})
	// Why: 300 real ticks outlast Windows' coarse timer within a test, so the ring is filled directly.
	for i := 0; i < DefaultMemoryHistory; i++ {
		srv.hub.sample(nil)
	}
	streaming := heapAfterGC()
	status, _, _, _ := srv.hub.latest()

	t.Logf("server started, no browser: +%d KB heap, +%d goroutines", (int64(idle)-int64(base))/kb, idleGoroutines)
	t.Logf("event and log rings full (%d each) of 120-byte lines: +%d KB; of lines cut to %d bytes: +%d KB", DefaultEventHistory, (int64(ringFull)-int64(idle))/kb, maxTextBytes, (int64(ringWorst)-int64(idle))/kb)
	t.Logf("one open stream, 50 accounts, memory ring full (%d samples): +%d KB over the worst ring; status frame %d bytes", DefaultMemoryHistory, (int64(streaming)-int64(ringWorst))/kb, len(status))
	if total := int64(streaming) - int64(base); total > 2048*kb {
		t.Fatalf("dashboard footprint %d KB", total/kb)
	}
	cancel()
	<-srv.Done()
}
