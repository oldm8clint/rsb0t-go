package sessionipc

import (
	"errors"
	"net"
	"sync"
)

// StreamFrame is one PacketFrame tagged with the account it came from — the wire shape a
// real, multi-account cmd/sessionhost (see HOTRELOAD.md's Phase 1) streams to every
// connected brain. PacketFrame itself (packetstream.go) deliberately doesn't carry an
// account, since Phase 0/2's fabricated single-session demos never needed to distinguish
// one account's packets from another's over the wire — a real host holding N real
// client.Sessions does, so this is the first primitive in this package that actually
// forwards packets over a real connection rather than a caller manually calling
// StateMirror.Apply in-process (see cmd/remotebottest's applyFakeStatUpdate helper for
// exactly what this replaces).
type StreamFrame struct {
	Account string
	Opcode  int
	Payload []byte
}

// StreamHub is the host side of the packet-stream boundary: cmd/sessionhost calls Publish
// once per packet its client.Session.ReadLoop's onPacket callback observes (exactly the
// same "after the state has applied it" hook packetstream.go's own doc comment already
// describes), and ServeStream fans that out to every currently-connected brain
// subscriber — any number of them, though today only ever one at a time in practice
// (a brain generation being killed and a fresh one reconnecting, per
// TestRemoteSessionSurvivesBrainRestart's mechanism-level proof). A subscriber only ever
// sees frames published from the moment it connects onward — no replay of history — the
// same way a real client.Session's own ReadLoop only ever sees packets from connect time
// forward; a reconnecting brain is expected to build a fresh StateMirror, exactly as
// cmd/remotebarbariantest's brain-restart proof does.
type StreamHub struct {
	mu   sync.Mutex
	subs map[net.Conn]chan StreamFrame

	// SeedFrames, when set, is called once for every newly-accepted subscriber, before
	// it is added to the fan-out set, and its result is sent to that subscriber first —
	// the fix for a real gap this project found live (see cmd/sessionhost's own use of
	// this field): "no replay of history" (this type's own doc comment above) is fine
	// for most state, which a script either doesn't need immediately or re-learns from
	// the very next ordinary packet, but internal/gamestate.State.Tile() is a standing
	// exception — it's computed from mapBuildBaseX/Z, set ONLY by a REBUILD_NORMAL
	// packet (internal/gamestate/position.go's applyRebuildNormal), which fires only on
	// a zone crossing. A brain that reconnects without one ever occurring during its own
	// subscription has no way to learn the account's current tile at all: confirmed
	// live, a reconnecting cmd/remotebarbariantest generation's StateMirror computed a
	// wrong, small-number "absolute" tile (mapBuildBase defaulting to 0,0) the moment
	// any ordinary local-coordinate-bearing packet arrived, not merely an absent one.
	// cmd/sessionhost sets this to synthesize a real, protocol-correct REBUILD_NORMAL
	// PacketFrame from the host's own actual, current MapBuildBase() — replayed through
	// the SAME unmodified internal/gamestate.State.Handle path a genuine REBUILD_NORMAL
	// would use, so this file and StateMirror need no gamestate-specific knowledge at
	// all. Nil (the default) reproduces the exact no-replay behaviour this type always
	// had.
	SeedFrames func() []StreamFrame
}

// NewStreamHub builds an empty hub with no subscribers and no SeedFrames.
func NewStreamHub() *StreamHub {
	return &StreamHub{subs: make(map[net.Conn]chan StreamFrame)}
}

// subBacklog bounds how many unsent frames a slow subscriber can accumulate before it is
// dropped — generous for a burst of login packets, small enough that one wedged brain
// connection can never grow without bound and can never block Publish (and therefore the
// real ReadLoop calling it) for every other account.
const subBacklog = 4096

// Publish forwards one packet, tagged with account, to every currently-subscribed
// connection. Never blocks the caller on a slow subscriber: a full backlog drops that one
// subscriber (closing its channel, which serveSub turns into closing the connection)
// rather than stalling every other account's forwarding — the same "one broken consumer
// can't take the rest down" principle bot.Runner's own panic recovery already follows.
func (h *StreamHub) Publish(account string, f PacketFrame) {
	sf := StreamFrame{Account: account, Opcode: f.Opcode, Payload: f.Payload}
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn, ch := range h.subs {
		select {
		case ch <- sf:
		default:
			// Safe to delete/close directly here (rather than via removeSub) because
			// this whole loop already holds h.mu and is iterating h.subs itself —
			// removeSub's extra "is this still the current channel" check exists for
			// watchClose/serveSub, which run without already holding the lock.
			delete(h.subs, conn)
			close(ch)
		}
	}
}

// removeSub deletes conn's subscription and closes its channel exactly once, however the
// removal was first noticed (a full backlog in Publish, a write failure in serveSub, or
// the brain closing its end in watchClose) — guarded by re-checking h.subs under the
// lock so two of those three racing never double-close ch (which would panic).
func (h *StreamHub) removeSub(conn net.Conn, ch chan StreamFrame) {
	h.mu.Lock()
	if cur, ok := h.subs[conn]; ok && cur == ch {
		delete(h.subs, conn)
		close(ch)
	}
	h.mu.Unlock()
}

// ServeStream accepts subscriber connections on ln until it is closed, mirroring Serve's
// own accept-loop shape and shutdown semantics (net.ErrClosed on a deliberate ln.Close
// returns nil; any other Accept error is returned). Each accepted connection is added to
// the subscriber set immediately, so it never misses a frame published after it connects
// — but first, if SeedFrames is set, its frames are queued ahead of anything Publish could
// possibly add, so a subscriber always sees them before its first live frame.
func (h *StreamHub) ServeStream(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		ch := make(chan StreamFrame, subBacklog)
		if h.SeedFrames != nil {
			for _, sf := range h.SeedFrames() {
				ch <- sf
			}
		}
		h.mu.Lock()
		h.subs[conn] = ch
		h.mu.Unlock()
		go h.serveSub(conn, ch)
		go h.watchClose(conn, ch)
	}
}

// serveSub drains ch onto conn until it is closed (by Publish's backlog-drop branch, by
// watchClose noticing the brain hung up, or by this func's own write failure below), then
// removes the subscriber (a no-op via removeSub if something else already did) and closes
// conn. Symmetric with conn.go's serveConn: one subscriber's disconnect only ever ends
// its own two goroutines (this one and watchClose), never anything else's.
func (h *StreamHub) serveSub(conn net.Conn, ch chan StreamFrame) {
	defer func() {
		h.removeSub(conn, ch)
		conn.Close()
	}()
	for f := range ch {
		if err := WriteFrame(conn, f); err != nil {
			return
		}
	}
}

// watchClose detects the brain closing its end of this otherwise one-directional
// (host-to-brain) stream connection: nothing ever arrives on it in normal operation, so a
// blocking Read only ever returns once the peer closes or the connection errors — the
// same signal a bidirectional protocol would get for free from ReadFrame returning EOF,
// reproduced here with a bare Read since StreamClient never writes anything back.
func (h *StreamHub) watchClose(conn net.Conn, ch chan StreamFrame) {
	buf := make([]byte, 1)
	_, _ = conn.Read(buf)
	h.removeSub(conn, ch)
}

// StreamClient is the brain-side handle to one subscribed connection: dial it once per
// brain generation (see RemoteSession's own per-generation *Client), read StreamFrames
// off Next in a loop, and feed the ones matching this brain's account into a
// StateMirror's Apply — exactly the wiring cmd/remotebarbariantest does for real, and
// cmd/remotebottest's applyFakeStatUpdate helper stood in for by hand.
type StreamClient struct {
	conn net.Conn
}

// DialStream opens a StreamClient over network/addr — the streaming counterpart to Dial.
func DialStream(network, addr string) (*StreamClient, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return &StreamClient{conn: conn}, nil
}

// Next blocks for the next forwarded frame, for any account the host is publishing —
// callers that only care about one account should filter on StreamFrame.Account
// themselves (a real cmd/sessionhost may hold several; this proof only ever dials one at
// a time, but the wire format doesn't assume that).
func (c *StreamClient) Next() (StreamFrame, error) {
	var f StreamFrame
	if err := ReadFrame(c.conn, &f); err != nil {
		return StreamFrame{}, err
	}
	return f, nil
}

// Close closes the underlying connection.
func (c *StreamClient) Close() error { return c.conn.Close() }

// SubscriberCount reports how many brain connections are currently subscribed — used by
// cmd/sessionhost's own proof setup to wait for a brain before running a local-engine-only
// cheat sequence, so the resulting state-changing packets are actually forwarded to
// someone rather than published into an empty room (see cmd/sessionhost's own doc comment
// for why: a subscriber only ever sees frames published from the moment it connects
// onward, never a replay of history).
func (h *StreamHub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
