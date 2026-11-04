package sessionipc

import (
	"encoding/json"
	"net"
	"testing"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func TestClientServerRoundTrip(t *testing.T) {
	ln := listen(t)
	disp := NewDispatcher()
	calls := 0
	disp.Register("Ping", func(string, json.RawMessage) error {
		calls++
		return nil
	})
	go Serve(ln, disp)

	client, err := Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	res, err := client.Call(ActionCall{Account: "bob", Name: "Ping"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.OK() {
		t.Fatalf("expected success, got %+v", res)
	}
	if calls != 1 {
		t.Fatalf("expected handler to run once, got %d", calls)
	}
}

func TestClientServerUnknownAction(t *testing.T) {
	ln := listen(t)
	go Serve(ln, NewDispatcher())

	client, err := Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	res, err := client.Call(ActionCall{Name: "Nope"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.OK() {
		t.Fatalf("expected an error result for an unregistered action")
	}
}

// TestServerSurvivesClientReconnect is the whole point of the split described in
// HOTRELOAD.md, proven at the transport layer: the host side (this Server/Dispatcher,
// standing in for the process that would hold a live client.Session) keeps running —
// and keeps its own accumulated state (calls, below) — across a brain-side Client
// disconnecting and a brand-new Client dialing back in. That has to be true before
// anything else about the design matters, since "restart the brain to pick up a code
// fix without touching the session" is exactly this sequence one level up.
func TestServerSurvivesClientReconnect(t *testing.T) {
	ln := listen(t)
	disp := NewDispatcher()
	calls := 0
	disp.Register("Ping", func(string, json.RawMessage) error {
		calls++
		return nil
	})
	go Serve(ln, disp)

	const generations = 3
	for i := 0; i < generations; i++ {
		client, err := Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("dial (brain generation %d): %v", i, err)
		}
		if _, err := client.Call(ActionCall{Name: "Ping"}); err != nil {
			t.Fatalf("call (brain generation %d): %v", i, err)
		}
		client.Close() // simulates that generation of the brain process exiting
	}
	if calls != generations {
		t.Fatalf("expected %d calls to have reached the still-running host, got %d", generations, calls)
	}
}

func TestServeStopsOnListenerClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ln, NewDispatcher()) }()
	ln.Close()
	if err := <-done; err != nil {
		t.Fatalf("expected Serve to return nil on a deliberate listener close, got %v", err)
	}
}
