package actions

import (
	"testing"

	"rsb0t-go/internal/pathfind"
)

// A corner more than 127 tiles from the start used to be sent as a wrapped signed byte: 130 tiles
// east arrived as 126 tiles west. The walk now stops at the furthest tile of that leg it can send.
func TestSendableWaypointsNeverWrapAnOffset(t *testing.T) {
	start := pathfind.NavPoint{X: 3000, Z: 3200}
	got := SendableWaypoints([]pathfind.NavPoint{start, {X: 3050, Z: 3200}, {X: 3130, Z: 3200}, {X: 3130, Z: 3300}})
	if len(got) != 3 || got[2] != (pathfind.NavPoint{X: 3127, Z: 3200}) {
		t.Fatalf("got %v, want the second leg cut at 3127,3200", got)
	}
	for _, p := range got {
		if d := p.X - start.X; d > 127 || d < -127 {
			t.Fatalf("%v is %d tiles from the start", p, d)
		}
	}
	// A diagonal leg is cut on the diagonal.
	diag := SendableWaypoints([]pathfind.NavPoint{start, {X: 2800, Z: 3000}})
	if len(diag) != 2 || diag[1] != (pathfind.NavPoint{X: 2873, Z: 3073}) {
		t.Fatalf("diagonal: %v", diag)
	}
	// In range: untouched. And the old corner cap still applies.
	short := []pathfind.NavPoint{start, {X: 3100, Z: 3200}}
	if got := SendableWaypoints(short); len(got) != 2 || got[1] != short[1] {
		t.Fatalf("in range: %v", got)
	}
	long := make([]pathfind.NavPoint, 40)
	for i := range long {
		long[i] = pathfind.NavPoint{X: 3000 + i%2, Z: 3200 + i}
	}
	if got := SendableWaypoints(long); len(got) != maxWaypoints {
		t.Fatalf("corner cap: %d", len(got))
	}
}

// Walk sends only what SendableWaypoints keeps, so no offset byte wraps.
func TestWalkSendsNoWrappedOffset(t *testing.T) {
	s := newFakeSession()
	if err := Walk(s, []pathfind.NavPoint{{X: 3000, Z: 3200}, {X: 3200, Z: 3200}}, false); err != nil {
		t.Fatal(err)
	}
	b := s.lastSent()
	// opcode, size, ctrl, x(2), z(2), then one corner.
	if len(b) != 9 || int8(b[7]) != 127 || int8(b[8]) != 0 {
		t.Fatalf("packet % x, want one corner at +127,0", b)
	}
}
