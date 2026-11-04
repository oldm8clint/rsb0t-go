package sessionipc

import (
	"errors"
	"net"
	"sync"
)

// Client is the brain-side handle to one host connection: it sends ActionCalls and
// reads back ActionResults, one at a time (Call blocks for the matching reply) — the
// simplest correct thing to prove the wire format and dispatch round trip end to end. A
// real integration is free to pipeline multiple in-flight calls behind this same
// framing later without changing Dispatcher/Handler at all.
type Client struct {
	conn net.Conn
	mu   sync.Mutex
}

// Dial opens a Client over network/addr (e.g. "tcp", "127.0.0.1:PORT"). A later phase
// can add a Windows named-pipe transport (see HOTRELOAD.md) behind the same Client/
// Server API — nothing above this line would need to change.
func Dial(network, addr string) (*Client, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

// NewClient wraps an already-established connection as a Client.
func NewClient(conn net.Conn) *Client {
	return &Client{conn: conn}
}

// Call sends call and waits for its ActionResult. Safe for concurrent use — calls are
// serialised onto the one connection in the order Call was invoked.
func (c *Client) Call(call ActionCall) (ActionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := WriteFrame(c.conn, call); err != nil {
		return ActionResult{}, err
	}
	var res ActionResult
	if err := ReadFrame(c.conn, &res); err != nil {
		return ActionResult{}, err
	}
	return res, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error { return c.conn.Close() }

// Serve accepts connections on ln until it is closed, handling each on its own
// goroutine — the host side of the action-call boundary. It returns nil once ln is
// closed deliberately (net.ErrClosed), or any other Accept error otherwise.
func Serve(ln net.Listener, disp *Dispatcher) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go serveConn(conn, disp)
	}
}

// serveConn answers ActionCalls on one connection until it errors or the peer closes
// it, then closes the connection. A brain process disconnecting (a deliberate restart,
// or a crash) only ever ends its own serveConn goroutine — every other connection, and
// the listener itself, are unaffected, which is the whole property this scaffolding
// exists to prove (see conn_test.go's TestServerSurvivesClientReconnect).
func serveConn(conn net.Conn, disp *Dispatcher) {
	defer conn.Close()
	for {
		var call ActionCall
		if err := ReadFrame(conn, &call); err != nil {
			// A real host would log a non-EOF error here (a malformed frame, a
			// connection dropped mid-call); this scaffolding has no logger of its
			// own yet, matching HOTRELOAD.md's "not wired up" scope for this slice.
			return
		}
		res := disp.Dispatch(call)
		if err := WriteFrame(conn, res); err != nil {
			return
		}
	}
}
