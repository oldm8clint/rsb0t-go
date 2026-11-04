package controlpanel

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	sseHello   = "hello"
	sseStatus  = "status"
	sseEvent   = "event"
	sseMemory  = "memory"
	sseDropped = "dropped"
)

var (
	sseRetry = []byte("retry: 3000\n\n")
	ssePing  = []byte(": ping\n\n")
)

var newTicker = time.NewTicker

// sseFrame encodes one Server-Sent Events message. JSON frames normally contain no CR/LF,
// so take a fast path that avoids ReplaceAll/Split allocations. CR/LF input is normalized
// without creating an intermediate copy.
func sseFrame(event string, id uint64, data []byte) []byte {
	var b bytes.Buffer
	b.Grow(len(data) + len(event) + 32)
	if id > 0 {
		b.WriteString("id: ")
		var digits [20]byte // Maximum decimal width of a uint64.
		b.Write(strconv.AppendUint(digits[:0], id, 10))
		b.WriteByte('\n')
	}
	if event != "" {
		b.WriteString("event: ")
		b.WriteString(event)
		b.WriteByte('\n')
	}
	if !bytes.ContainsAny(data, "\r\n") {
		b.WriteString("data: ")
		b.Write(data)
		b.WriteString("\n\n")
		return b.Bytes()
	}
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\r' && data[i] != '\n' {
			continue
		}
		b.WriteString("data: ")
		b.Write(data[start:i])
		b.WriteByte('\n')
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			i++
		}
		start = i + 1
	}
	b.WriteString("data: ")
	b.Write(data[start:])
	b.WriteString("\n\n")
	return b.Bytes()
}

func eventFrame(ev Event) []byte {
	data, _ := json.Marshal(ev)
	return sseFrame(sseEvent, ev.Seq, data)
}

type helloBody struct {
	Version    int    `json:"version"`
	ServerTime int64  `json:"serverTime"`
	TickMs     int64  `json:"tickMs"`
	Last       uint64 `json:"last"`
}

type connKey struct{}

func rememberConn(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}

type streamWriter struct {
	w       http.ResponseWriter
	conn    net.Conn
	timeout time.Duration
	err     error
}

func (s *streamWriter) arm() {
	if s.conn != nil {
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.timeout))
	}
}

func (s *streamWriter) write(frame []byte) {
	if s.err != nil {
		return
	}
	s.arm()
	_, s.err = s.w.Write(frame)
}

// flushResponse is http.ResponseController.Flush(), written out by hand because the repo must
// still build on Go 1.19.3 (see CLAUDE.md's Go version section) and ResponseController arrived
// in Go 1.20. The logic is the one ResponseController uses: unwrap the writer chain, prefer a
// FlushError() error method over a plain Flush(), and report an unflushable writer instead of
// silently succeeding. On Go 1.20+ this is the same code path the controller took, so the
// stall detection flush() documents below is unchanged there. On Go 1.19 the standard
// library's own response writer has no FlushError, so a flush-time error is seen one frame
// later, when bufio's sticky error surfaces from the next Write, rather than immediately —
// later, but still an end to the stream instead of the forever-silent loop this replaced.
func flushResponse(w http.ResponseWriter) error {
	for {
		switch v := w.(type) {
		case interface{ FlushError() error }:
			return v.FlushError()
		case http.Flusher:
			v.Flush()
			return nil
		case interface{ Unwrap() http.ResponseWriter }:
			w = v.Unwrap()
		default:
			return http.ErrNotSupported
		}
	}
}

// flush pushes buffered bytes to the wire. This has to go through flushResponse above
// rather than the older http.Flusher (Flush() with no return value) that this code used to
// call directly: with a plain Flusher, a write that fails or times out partway through the
// flush — the actual point a chunked response's bytes reach the socket — is invisible here,
// so s.err stays nil, out.flush() below never reports a problem, and the per-stream loop in
// handleStream keeps calling arm()+flush() forever on a connection that will never accept
// another byte. The browser's EventSource then never sees the response end, so it never
// fires onerror and never reconnects — the tab just silently stops receiving *everything*
// (events, status, memory, even keepalive pings, since they all flow through this same
// flush call), looking "stuck" until someone manually reloads the page. Live-observed
// 2026-09-16 against both the real 179-account fleet and a local `-fake` reproduction: one
// stream stopped advancing entirely (packet counts included) while every other stream/the
// server's own /api/events kept moving, with no reconnect ever logged for it.
func (s *streamWriter) flush() error {
	if s.err != nil {
		return s.err
	}
	s.arm()
	if err := flushResponse(s.w); err != nil {
		s.err = err
	}
	return s.err
}

func (srv *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		writeError(w, http.StatusInternalServerError, "this connection cannot stream")
		return
	}
	after := lastEventID(r)
	st, replay, last, err := srv.hub.join(after)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer srv.hub.leave(st)

	conn, _ := r.Context().Value(connKey{}).(net.Conn)
	if conn != nil {
		defer func() { _ = conn.SetWriteDeadline(time.Time{}) }()
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	out := &streamWriter{w: w, conn: conn, timeout: srv.opts.writeTimeout}
	hello, _ := json.Marshal(helloBody{Version: APIVersion, ServerTime: srv.opts.now().UnixMilli(), TickMs: srv.opts.tick.Milliseconds(), Last: last})
	out.write(sseRetry)
	out.write(sseFrame(sseHello, 0, hello))
	for _, ev := range replay {
		out.write(eventFrame(ev))
	}
	replay = nil
	if out.flush() != nil {
		return
	}

	keepAlive := time.NewTicker(srv.opts.keepAlive)
	defer keepAlive.Stop()
	var seenStatus, seenMemory uint64
	for {
		select {
		case <-r.Context().Done():
			return
		case <-srv.hub.done:
			return
		case frame := <-st.queue:
			out.write(frame)
		drain:
			for i := 0; i < srv.opts.queue; i++ {
				select {
				case more := <-st.queue:
					out.write(more)
				default:
					break drain
				}
			}
		case <-st.wake:
			status, statusGen, memory, memGen := srv.hub.latest()
			if statusGen != seenStatus && status != nil {
				out.write(status)
				seenStatus = statusGen
			}
			if memGen != seenMemory && memory != nil {
				out.write(memory)
				seenMemory = memGen
			}
		case <-keepAlive.C:
			out.write(ssePing)
		}
		if n := atomic.SwapUint64(&st.dropped, 0); n > 0 {
			out.write(sseFrame(sseDropped, 0, []byte(`{"count":`+strconv.FormatUint(n, 10)+`}`)))
		}
		if out.flush() != nil {
			return
		}
	}
}

func lastEventID(r *http.Request) uint64 {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("since")
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
