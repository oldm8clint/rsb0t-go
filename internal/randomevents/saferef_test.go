package randomevents

import (
	"testing"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
)

// safeRefSession is the smallest session this lookup needs — safeRefFor only ever uses the
// value as a registry key, never calls through it.
type safeRefSession struct {
	state    *gamestate.State
	counters anticheat.Counters
}

func (s *safeRefSession) State() *gamestate.State         { return s.state }
func (s *safeRefSession) Cheats() *anticheat.Counters     { return &s.counters }
func (s *safeRefSession) Send(func(*packet.Packet)) error { return nil }

// Live-found 2026-09-18 (4/4 reproducible, from the Scarab Swarm Stage 1 work): every flee
// path in this package hardcoded CatherbyBankRef as its destination/bias, so a fresh
// Lumbridge account that tripped "in combat with no tracked event" was thrown ~400 tiles to
// the Catherby bank and stranded there. A per-account safe reference fixes that — but it
// must keep defaulting to CatherbyBankRef, because every ProgressiveFisher caller that
// never sets one relies on exactly today's behavior.
func TestSafeRefForDefaultsToCatherbyAndHonoursAnOverride(t *testing.T) {
	unregistered := &safeRefSession{state: gamestate.New()}
	if got := safeRefFor(unregistered); got != CatherbyBankRef {
		t.Fatalf("a session with no Guardian = %v, want the CatherbyBankRef default %v", got, CatherbyBankRef)
	}

	s := &safeRefSession{state: gamestate.New()}
	g := NewGuardian(s, nil)
	defer g.Stop()

	if got := safeRefFor(s); got != CatherbyBankRef {
		t.Fatalf("a Guardian that never set SafeRef = %v, want the CatherbyBankRef default %v", got, CatherbyBankRef)
	}

	// The real shape of the incident: a Lumbridge-based script pinning its own home.
	lumbridge := pathfind.NavPoint{X: 3222, Z: 3218, Level: 0}
	g.SafeRef = &lumbridge
	if got := safeRefFor(s); got != lumbridge {
		t.Fatalf("SafeRef override = %v, want %v", got, lumbridge)
	}
	if CatherbyBankRef == lumbridge {
		t.Fatal("test is vacuous: the override equals the default")
	}
}

// The teleport guard is the part that protects every script that never sets a SafeRef at
// all, so it has to hold on raw distance alone.
func TestFarTeleportGuardRejectsACrossMapJump(t *testing.T) {
	// The real live pair: pickpocketing at Lumbridge, "safe point" at the Catherby bank.
	lumbridgeX, lumbridgeZ := 3222, 3218
	if d := chebyshev(lumbridgeX, lumbridgeZ, CatherbyBankRef.X, CatherbyBankRef.Z); d <= farTeleportGuard {
		t.Fatalf("Lumbridge->Catherby is %d tiles, which no longer exceeds farTeleportGuard %d — "+
			"the cross-map teleport this guard exists to stop would be allowed again", d, farTeleportGuard)
	}
	// An account genuinely working near its own safe point must still be allowed to use it.
	nearX, nearZ := CatherbyBankRef.X+10, CatherbyBankRef.Z-12
	if d := chebyshev(nearX, nearZ, CatherbyBankRef.X, CatherbyBankRef.Z); d > farTeleportGuard {
		t.Fatalf("a tile %d tiles from the safe point is treated as far; that would disable the "+
			"teleport for the Catherby accounts it was built for", d)
	}
}
