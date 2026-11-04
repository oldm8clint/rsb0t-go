package fleet

import (
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestADroppedSessionClosesItsSocket pins the fix for the 5 Oct psiphon2 outage: when a
// session's read loop fails, the fleet closes that session's socket. Left open, its
// keepalive kept answering for a client nobody read, so the server kept the player in the
// world and refused every relog with response 5.
//
// The server ends the client's read side without closing the connection (a close frame,
// standing in for a stalled proxy's read timeout), then watches the raw socket: only the
// client closing it ends that read.
func TestADroppedSessionClosesItsSocket(t *testing.T) {
	closed := make(chan error, 1)
	var conns atomic.Int32
	upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		if _, _, err = ws.ReadMessage(); err != nil {
			return
		}
		if err = ws.WriteMessage(websocket.BinaryMessage, make([]byte, 17)); err != nil {
			return
		}
		if _, _, err = ws.ReadMessage(); err != nil {
			return
		}
		if err = ws.WriteMessage(websocket.BinaryMessage, []byte{2, 0, 0}); err != nil {
			return
		}
		if conns.Add(1) != 1 {
			for {
				if _, _, err = ws.ReadMessage(); err != nil {
					return
				}
			}
		}
		if err = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""), time.Now().Add(time.Second)); err != nil {
			closed <- err
			return
		}
		conn := ws.UnderlyingConn()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = io.Copy(io.Discard, conn)
		closed <- err // nil is EOF: the client closed its socket
	}))
	defer server.Close()
	m := New(Config{WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), Checksums: &[9]int32{}, Modulus: big.NewInt(9999991), Exponent: big.NewInt(65537), NoAntiCheat: true})
	defer m.CloseAll()
	// Hold the reconnect that follows the drop, so only the dropped session is watched.
	m.retryWait = func(done <-chan struct{}, _ time.Duration) bool { <-done; return false }

	m.Connect("dropped_session", "fixture_password")
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("the dropped session's socket stayed open: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server never finished the login")
	}
}
