package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
)

// Live 2026-09-19: HandleHazard held the Guardian for 36s on every whirlpool (530 waits in one
// run, by every account at the spot) although content's whirlpool never attacks or teleports.
// acct6 died when its own Strange plant turned angry during one of those waits. The handler
// must step clear and return at once, leaving the whirlpool deferred for its lifetime.
func TestWhirlpoolHandlerReturnsWithoutWaitingItOut(t *testing.T) {
	s := &safeRefSession{state: overlapState()}
	g := NewGuardian(s, nil)
	defer g.Stop()
	ev := newEvent(KindHazard, "whirlpool")
	ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ = 9, 403, 3201, 3200

	start := time.Now()
	if err := HandleHazard(s, &pathfind.Pack{}, ev, nil); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("whirlpool handler blocked the Guardian for %s", took)
	}
	g.mu.Lock()
	until := g.retryUntil[eventSignature(ev)]
	g.mu.Unlock()
	if time.Until(until) < whirlpoolLifetime-5*time.Second {
		t.Fatalf("whirlpool not deferred for its lifetime: retry in %s", time.Until(until))
	}
}
