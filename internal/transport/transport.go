// Package transport ports rsb0t-main's src/client/io/ClientStream.ts: a byte-stream
// abstraction (arbitrary-length reads that transparently span WebSocket message
// boundaries) layered over the browser WebSocket API's message framing.
//
// Go's approach differs in mechanism but not behavior: instead of the TS original's
// queue-of-events-plus-callback machinery (needed because JS has no blocking read), a
// background goroutine drains the socket into a byte buffer, and Read/ReadFull block
// (with the same 30s timeout as the original's WebSocketReader) until enough data
// arrives or the deadline passes.
package transport

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ReadTimeout mirrors WebSocketReader's 30000ms read timeout.
const ReadTimeout = 30 * time.Second

// writeLimit mirrors WebSocketWriter's 5000-byte limit (with the same 100-byte margin).
const writeLimit = 5000

var (
	// ErrReadTimeout mirrors a stalled WebSocketReader.waitForEvent rejecting on timeout.
	ErrReadTimeout = errors.New("transport: read timed out")
	// ErrClosed mirrors reading from a stream whose remote end (or local Close) has closed it.
	ErrClosed = errors.New("transport: stream closed")
	// ErrWriteTooLarge mirrors WebSocketWriter throwing when len > limit-100.
	ErrWriteTooLarge = errors.New("transport: write exceeds size limit")
)

// frameReader is the subset of *websocket.Conn this package actually uses, so tests can
// substitute a fake message source instead of a real socket.
type frameReader interface {
	ReadMessage() (messageType int, p []byte, err error)
}

// frameWriter is the write-side counterpart of frameReader.
type frameWriter interface {
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// Stream is the Go equivalent of ClientStream: dial once, then Write/Read/ReadFull like
// a plain byte stream regardless of how the underlying transport chunked the bytes.
type Stream struct {
	reader frameReader
	writer frameWriter

	mu        sync.Mutex
	buf       bytes.Buffer
	closed    bool
	err       error
	notify    chan struct{}
	closeOnce sync.Once
	closeErr  error

	lastDataAt  time.Time
	readTimeout time.Duration // defaults to ReadTimeout; shrunk in tests to exercise the timeout path quickly
}

// Dial opens a WebSocket connection with the "binary" subprotocol — matching
// ClientStream.openSocket's `new WebSocket(url, 'binary')` — and starts the Stream's
// background read pump. url should be a full ws:// or wss:// address.
func Dial(url string) (*Stream, error) {
	dialer := websocket.Dialer{Subprotocols: []string{"binary"}}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return newStream(conn, conn), nil
}

// DialOptions changes how DialWith reaches the server. The zero value is exactly Dial.
type DialOptions struct {
	// NetDialContext opens the TCP connection the WebSocket (and, for a wss URL, TLS on top
	// of it, end to end) runs over — for example a tunnel through a proxy. Nil dials directly.
	NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// HandshakeTimeout bounds the connect, any proxy handshake, TLS and the WebSocket
	// upgrade together. Zero sets no bound, as Dial does.
	HandshakeTimeout time.Duration
	// Header adds request headers to the WebSocket upgrade (for example User-Agent and
	// Origin). Nil sends the library's defaults, as Dial does.
	Header http.Header
}

// DialWith is Dial with a custom TCP dialer and/or a handshake bound. With zero options it
// is Dial.
func DialWith(url string, opts DialOptions) (*Stream, error) {
	if opts.NetDialContext == nil && opts.HandshakeTimeout <= 0 && opts.Header == nil {
		return Dial(url)
	}
	dialer := websocket.Dialer{
		Subprotocols:     []string{"binary"},
		NetDialContext:   opts.NetDialContext,
		HandshakeTimeout: opts.HandshakeTimeout,
	}
	ctx := context.Background()
	if opts.HandshakeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.HandshakeTimeout)
		defer cancel()
	}
	conn, _, err := dialer.DialContext(ctx, url, opts.Header)
	if err != nil {
		return nil, err
	}
	return newStream(conn, conn), nil
}

func newStream(r frameReader, w frameWriter) *Stream {
	s := &Stream{
		reader:      r,
		writer:      w,
		notify:      make(chan struct{}),
		lastDataAt:  time.Now(),
		readTimeout: ReadTimeout,
	}
	go s.pump()
	return s
}

// pump mirrors WebSocketReader.onmessage: every inbound binary message's bytes are
// appended to the buffer, and anything blocked in ReadFull is woken to re-check it.
func (s *Stream) pump() {
	for {
		_, data, err := s.reader.ReadMessage()
		s.mu.Lock()
		if err != nil {
			s.closed = true
			if s.err == nil {
				s.err = err
			}
			s.wakeLocked()
			s.mu.Unlock()
			return
		}
		s.buf.Write(data)
		s.lastDataAt = time.Now()
		s.wakeLocked()
		s.mu.Unlock()
	}
}

// wakeLocked closes and replaces the notify channel; callers must hold s.mu.
func (s *Stream) wakeLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

// Available mirrors the `available` getter: bytes currently buffered and unread.
func (s *Stream) Available() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

// MsSinceData mirrors msSinceData: how long since the server last sent anything.
func (s *Stream) MsSinceData() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return time.Duration(1<<63 - 1) // "positive infinity" equivalent
	}
	return time.Since(s.lastDataAt)
}

// ReadByte reads and returns a single byte, blocking until one is available or the read
// times out. Mirrors `read()`.
func (s *Stream) ReadByte() (byte, error) {
	var b [1]byte
	if err := s.ReadFull(b[:]); err != nil {
		return 0, err
	}
	return b[0], nil
}

// ReadFull reads exactly len(dst) bytes, blocking across as many underlying WebSocket
// messages as needed, and returns ErrReadTimeout if 30s pass without enough data
// arriving. Mirrors `readBytes()`.
func (s *Stream) ReadFull(dst []byte) error {
	need := len(dst)
	got := 0
	deadline := time.Now().Add(s.readTimeout)

	for got < need {
		s.mu.Lock()
		if s.buf.Len() > 0 {
			n, _ := s.buf.Read(dst[got:need])
			got += n
			s.mu.Unlock()
			continue
		}
		if s.closed {
			err := s.err
			s.mu.Unlock()
			if err == nil {
				err = ErrClosed
			}
			return err
		}
		ch := s.notify
		s.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ErrReadTimeout
		}
		select {
		case <-ch:
			// state changed (data arrived or closed) — loop and re-check.
		case <-time.After(remaining):
			return ErrReadTimeout
		}
	}
	return nil
}

// Write sends src as one binary WebSocket message. Mirrors WebSocketWriter.write,
// including its size-limit guard (len must be < limit-100).
func (s *Stream) Write(src []byte) error {
	if len(src) > writeLimit-100 {
		return ErrWriteTooLarge
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil // mirrors `if (this.dummy || this.remoteClosed) return;`
	}
	return s.writer.WriteMessage(websocket.BinaryMessage, src)
}

// Close mirrors ClientStream.close: stop reading further data and release the socket.
func (s *Stream) Close() error {
	// A read failure marks the stream closed but does not release the socket.
	// Close the transport exactly once even if the read pump failed first.
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			s.wakeLocked()
		}
		s.mu.Unlock()
		s.closeErr = s.writer.Close()
	})
	return s.closeErr
}
