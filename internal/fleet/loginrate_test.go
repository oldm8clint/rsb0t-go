package fleet

import (
	"testing"
	"time"
)

func TestLoginGateQueuedWaiterRechecksPenalty(t *testing.T) {
	g := &loginRateGate{tokens: make(chan struct{}, 1), stop: make(chan struct{})}
	done := make(chan struct{})
	result := make(chan bool, 1)
	go func() { result <- g.Wait(done) }()
	// With no token, the caller is already waiting when the server rejects
	// another account. Simulate a token that was concurrently in flight.
	time.Sleep(20 * time.Millisecond)
	g.Penalize()
	g.tokens <- struct{}{}
	select {
	case allowed := <-result:
		t.Fatalf("queued waiter escaped penalty: %v", allowed)
	case <-time.After(60 * time.Millisecond):
	}
	close(done)
	if <-result {
		t.Fatal("cancelled waiter attempted login")
	}
}

func TestLoginGateRefillDoesNotAccumulateDuringPenalty(t *testing.T) {
	g := newLoginRateGate()
	defer g.Close()
	g.Penalize()
	time.Sleep(loginRateInterval + 100*time.Millisecond)
	if got := len(g.tokens); got != 0 {
		t.Fatalf("penalized gate accumulated %d tokens", got)
	}
}

func TestLoginGateResumesAfterQuietWindow(t *testing.T) {
	g := &loginRateGate{tokens: make(chan struct{}, 1), stop: make(chan struct{}), penalizedUntil: time.Now().Add(50 * time.Millisecond)}
	defer g.Close()
	result := make(chan bool, 1)
	go func() { result <- g.Wait(make(chan struct{})) }()
	// A token admitted after the short fixture cooldown must allow progress.
	time.Sleep(70 * time.Millisecond)
	g.tokens <- struct{}{}
	select {
	case allowed := <-result:
		if !allowed {
			t.Fatal("expired penalty prevented login")
		}
	case <-time.After(time.Second):
		t.Fatal("expired penalty did not resume")
	}
}

func TestLoginGateCloseReleasesTokenAndPenaltyWaiters(t *testing.T) {
	for _, penalty := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "penalty"}[penalty], func(t *testing.T) {
			g := &loginRateGate{tokens: make(chan struct{}), stop: make(chan struct{})}
			if penalty {
				g.Penalize()
			}
			result := make(chan bool, 1)
			go func() { result <- g.Wait(make(chan struct{})) }()
			g.Close()
			select {
			case allowed := <-result:
				if allowed {
					t.Fatal("closed gate allowed login")
				}
			case <-time.After(time.Second):
				t.Fatal("closed gate retained waiter")
			}
		})
	}
}
