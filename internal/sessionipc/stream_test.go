package sessionipc

import (
	"net"
	"testing"
	"time"
)

// TestStreamHubDeliversToSubscriber proves the basic round trip: a Publish call reaches a
// StreamClient dialed against ServeStream, tagged with the right account.
func TestStreamHubDeliversToSubscriber(t *testing.T) {
	hub := NewStreamHub()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = hub.ServeStream(ln) }()

	client, err := DialStream("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// Publish before Next is called at all to prove ServeStream's accept-then-subscribe
	// ordering doesn't drop a frame published shortly after connect (a real race a naive
	// "subscribe after the first read" implementation could lose).
	deadline := time.Now().Add(2 * time.Second)
	for {
		hub.mu.Lock()
		n := len(hub.subs)
		hub.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the subscriber to register")
		}
		time.Sleep(5 * time.Millisecond)
	}
	hub.Publish("acct1", PacketFrame{Opcode: 42, Payload: []byte{1, 2, 3}})

	f, err := client.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Account != "acct1" || f.Opcode != 42 || len(f.Payload) != 3 || f.Payload[0] != 1 {
		t.Fatalf("got %+v, want account=acct1 opcode=42 payload=[1 2 3]", f)
	}
}

// TestStreamHubMultipleSubscribers proves every currently-connected subscriber gets the
// same published frame — the fan-out a brain-restart's overlap window (old generation
// still tearing down while a new one is already dialing) depends on.
func TestStreamHubMultipleSubscribers(t *testing.T) {
	hub := NewStreamHub()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = hub.ServeStream(ln) }()

	c1, err := DialStream("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	defer c1.Close()
	c2, err := DialStream("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer c2.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		hub.mu.Lock()
		n := len(hub.subs)
		hub.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for both subscribers to register")
		}
		time.Sleep(5 * time.Millisecond)
	}
	hub.Publish("acct1", PacketFrame{Opcode: 7})

	for i, c := range []*StreamClient{c1, c2} {
		f, err := c.Next()
		if err != nil {
			t.Fatalf("subscriber %d Next: %v", i, err)
		}
		if f.Account != "acct1" || f.Opcode != 7 {
			t.Fatalf("subscriber %d got %+v", i, f)
		}
	}
}

// TestStreamHubUnsubscribeOnClose proves a brain closing its connection (a deliberate
// restart) removes it from the hub's subscriber set — the streaming counterpart to
// conn.go's TestServerSurvivesClientReconnect, so a dead brain connection can never
// accumulate in Publish's loop or leak a goroutine.
func TestStreamHubUnsubscribeOnClose(t *testing.T) {
	hub := NewStreamHub()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = hub.ServeStream(ln) }()

	client, err := DialStream("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		hub.mu.Lock()
		n := len(hub.subs)
		hub.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the subscriber to register")
		}
		time.Sleep(5 * time.Millisecond)
	}
	client.Close()

	deadline = time.Now().Add(2 * time.Second)
	for {
		hub.mu.Lock()
		n := len(hub.subs)
		hub.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber was never removed after closing its connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
