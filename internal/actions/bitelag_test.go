package actions

import (
	"testing"
	"time"
)

// The bite ledger keeps the latest evidence that the route is slow (LastBiteLag), because the hitpoints an account reads, and the
// damage they show, are as old as the route is slow. LIVE 14 Oct 2026 20:08-20:13: three ScarabSwarm accounts died thieving
// Paladins and a Hero inside fleet-wide windows of "the bite took 3.2 s / 3.8 s to show" and bites that never showed.

func TestAQuickBiteLeavesNoLag(t *testing.T) {
	r := newBiteRig(30, 53)
	r.latency = 1200 * time.Millisecond
	EatBite(r.env(), r.req("Cake"))
	if lag, ok := LastBiteLag(r.f.state); ok {
		t.Fatalf("a 1.2 s bite left lag %+v", lag)
	}
	r.latency = 2300 * time.Millisecond
	EatBite(r.env(), r.req("Cake"))
	if _, ok := LastBiteLag(r.f.state); ok {
		t.Fatal("a 2.3 s bite counted as lag: the line is BiteLagAfter, 3 s (the wait polls every 150 ms)")
	}
}

func TestASlowBiteIsLag(t *testing.T) {
	r := newBiteRig(30, 53)
	r.latency = 3800 * time.Millisecond // the 14 Oct 20:13 Bass
	if !EatBite(r.env(), r.req("Bass")) {
		t.Fatal("a bite that showed at 3.8 s was called failed")
	}
	lag, ok := LastBiteLag(r.f.state)
	if !ok || lag.Lost || lag.Took < 3*time.Second || lag.Took > 5*time.Second || !lag.At.Equal(r.now()) {
		t.Fatalf("lag = %+v ok %v at %v, want the 3.8 s bite noted now", lag, ok, r.now())
	}
}

func TestABiteThatHasNotShownAfterTheWaitIsLagAndALateLandingIsToo(t *testing.T) {
	r := newBiteRig(13, 53)
	r.latency = 8 * time.Second
	EatBite(r.env(), r.req("Cake")) // false at six seconds, in flight
	lag, ok := LastBiteLag(r.f.state)
	if !ok || lag.Lost || lag.Took < 5*time.Second {
		t.Fatalf("after the unconfirmed wait: lag %+v ok %v, want the six-second wait noted, not lost", lag, ok)
	}
	r.Delay(3 * time.Second)
	if !EatBite(r.env(), r.req("Cake")) {
		t.Fatal("the late landing was not credited")
	}
	lag, _ = LastBiteLag(r.f.state)
	if lag.Lost || lag.Took < 7*time.Second || lag.Took > 10*time.Second {
		t.Fatalf("after the credit: lag %+v, want the 8 s landing", lag)
	}
}

func TestABiteThatNeverShowedIsLostLag(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Bass"))
	EatBite(r.env(), r.req("Bass")) // waits out the window and calls it lost
	lag, ok := LastBiteLag(r.f.state)
	if !ok || !lag.Lost || lag.Took < BiteWatchWindow-time.Second {
		t.Fatalf("lag = %+v ok %v, want a lost bite after about %s", lag, ok, BiteWatchWindow)
	}
}

func TestSettleBiteNotesALateLandingAndALostBite(t *testing.T) {
	r := newBiteRig(13, 53)
	r.latency = 8 * time.Second
	EatBite(r.env(), r.req("Bread"))
	r.Delay(3 * time.Second)
	SettleBite(r.env())
	if lag, ok := LastBiteLag(r.f.state); !ok || lag.Lost || lag.Took < 7*time.Second {
		t.Fatalf("a late landing settled: lag %+v ok %v", lag, ok)
	}

	lost := newBiteRig(30, 53)
	lost.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(lost.env(), lost.req("Bread"))
	lost.Delay(BiteWatchWindow + time.Second)
	SettleBite(lost.env())
	if lag, ok := LastBiteLag(lost.f.state); !ok || !lag.Lost {
		t.Fatalf("a lost bite settled: lag %+v ok %v", lag, ok)
	}
}

func TestBiteLagIsPerSessionAndNilSafe(t *testing.T) {
	a, b := newBiteRig(30, 53), newBiteRig(30, 53)
	a.latency = 4 * time.Second
	EatBite(a.env(), a.req("Cake"))
	if _, ok := LastBiteLag(a.f.state); !ok {
		t.Fatal("no lag for the session that was slow")
	}
	if _, ok := LastBiteLag(b.f.state); ok {
		t.Fatal("another session's bite is lag here")
	}
	if _, ok := LastBiteLag(nil); ok {
		t.Fatal("a nil state has lag")
	}
}
