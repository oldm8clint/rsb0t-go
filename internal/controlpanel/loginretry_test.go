package controlpanel

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"rsb0t-go/internal/fleet"
)

// Exercise the actual account-detail endpoint against a real fleet and loopback
// WebSocket login server: the completed first attempt is terminal, then the owner
// requests reconnect and a fresh handshake succeeds. No production account used.
func TestAccountReconnectAPIRecoversCompletedLoginError(t *testing.T) {
	var connections atomic.Int32
	upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		attempt := connections.Add(1)
		if _, _, err = ws.ReadMessage(); err != nil {
			return
		}
		if attempt == 1 {
			reply := make([]byte, 9)
			reply[8] = 3
			_ = ws.WriteMessage(websocket.BinaryMessage, reply)
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
		for {
			if _, _, err = ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	m := fleet.New(fleet.Config{WSURL: "ws" + strings.TrimPrefix(engine.URL, "http"), Checksums: &[9]int32{}, Modulus: big.NewInt(9999991), Exponent: big.NewInt(65537), NoAntiCheat: true})
	t.Cleanup(func() { m.CloseAll(); engine.Close() })
	m.Connect("test_api_reconnect", "fixture_password")
	src := NewFleetSource(m, []string{"test_api_reconnect"}, time.Second)
	if state := src.Accounts(nil)[0].State; state != StateError {
		t.Fatalf("first attempt state=%v", state)
	}
	h := newHarness(t, Config{}, testOptions(), src)
	start := time.Now()
	response := h.request(http.MethodPost, "/api/fleet/connect", strings.NewReader(`{"account":"test_api_reconnect"}`), http.Header{"Content-Type": {"application/json"}})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reconnect status=%d body=%s", response.StatusCode, bodyText(t, response))
	}
	if time.Since(start) > time.Second {
		t.Fatal("endpoint waited for paced login instead of acknowledging acceptance")
	}
	if state := src.Accounts(nil)[0].State; state != StateReconnecting {
		t.Fatalf("accepted request state=%v", state)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		acc := src.Accounts(nil)[0]
		if acc.State == StateIngame {
			if connections.Load() != 2 || acc.LastError != "" {
				t.Fatalf("connections=%d error=%q", connections.Load(), acc.LastError)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fresh login never succeeded: %+v", src.Accounts(nil))
}

func TestFleetStateShowsActiveLoginOrTerminalFailureAfterOldDrop(t *testing.T) {
	status := fleet.AccountStatus{Disconnected: true, DisconnectMsg: "old drop", Error: "read timed out", Connecting: true}
	if state, _, _ := fleetState(status); state != StateConnecting {
		t.Fatalf("active login hidden by old error: %v", state)
	}
	status.Connecting = false
	if state, _, _ := fleetState(status); state != StateError {
		t.Fatalf("terminal login error hidden by old drop: %v", state)
	}
}
