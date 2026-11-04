package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Live 2026-09-24, spare102 frozen at 3032,3218,1 on the Karamja -> Port Sarim ship's deck for
// over an hour, trying the Bazaar's Ladder at 3049,3208,1 every 11 s. On the real pack the deck is
// its own 46-tile room and the ladder is not in it; the only exit is the Gangplank (loc 2084) at
// 3031,3217. The reachable picker chooses the gangplank. CONTROL: the old nearest-stair choice
// picks the unreachable ladder, which is the live loop.
func TestTheKaramjaShipDeckLeavesByItsGangplank(t *testing.T) {
	pk := realRoutePack(t)
	ix, cfg := realLocs(t)
	from := tile.Tile{X: 3032, Z: 3218, Level: 1}

	old, oldName := pickDirectionalMatch(ix, cfg, nil, from, stairNames, stairOpsDown, 25)
	if old == nil || oldName != "Ladder" || old.X != 3049 || old.Z != 3208 {
		t.Fatalf("control: the old choice should be the unreachable Ladder at 3049,3208 (the live loop), got %v %q", old, oldName)
	}
	if fixtureReachable(pk, from, *old) {
		t.Fatal("control: the Bazaar ladder should be unreachable from the ship's deck on the real pack")
	}

	if m, _ := pickReachableMatch(pk, ix, cfg, nil, from, stairNames, stairOpsDown, 25); m != nil {
		t.Fatalf("no stair is reachable from the deck, but %v was picked", m)
	}
	m, name := pickReachableMatch(pk, ix, cfg, nil, from, gangplankNames, gangplankOps, 25)
	if m == nil || m.ID != 2084 || m.X != 3031 || m.Z != 3217 {
		t.Fatalf("the deck's own gangplank (2084 at 3031,3217) was not picked: %v %q", m, name)
	}
	// And from the whole deck, not just the one tile the account froze on.
	for _, p := range []tile.Tile{{X: 3035, Z: 3214, Level: 1}, {X: 3033, Z: 3223, Level: 1}, {X: 3032, Z: 3217, Level: 1}} {
		if m, _ := pickReachableMatch(pk, ix, cfg, nil, p, gangplankNames, gangplankOps, 25); m == nil || m.ID != 2084 {
			t.Errorf("from %v the gangplank was not picked: %v", p, m)
		}
	}
}
