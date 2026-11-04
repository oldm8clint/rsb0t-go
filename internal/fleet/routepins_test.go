package fleet

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPlanRoutePinsKeepsExistingAndBalances(t *testing.T) {
	routes := []RouteCap{{ID: "direct", Max: 3}, {ID: "warp", Max: 3}, {ID: "psiphon", Max: 3}}
	existing := map[string]string{"a": "warp", "b": "warp", "gone": "direct", "c": "removed-route"}
	pins := PlanRoutePins(existing, []string{"A", "b", "c", "d", "e", "f", "D"}, routes)
	if pins["a"] != "warp" || pins["b"] != "warp" {
		t.Fatalf("existing pins must be kept: %v", pins)
	}
	if _, ok := pins["gone"]; ok {
		t.Fatal("pins for accounts no longer in the roster must be dropped")
	}
	if len(pins) != 6 {
		t.Fatalf("want 6 pins (d listed twice), got %d: %v", len(pins), pins)
	}
	load := map[string]int{}
	for _, id := range pins {
		load[id]++
	}
	if load["direct"] != 2 || load["warp"] != 2 || load["psiphon"] != 2 {
		t.Fatalf("unbalanced: %v", load)
	}
}

func TestPlanRoutePinsWhenFullStillPins(t *testing.T) {
	pins := PlanRoutePins(nil, []string{"a", "b", "c"}, []RouteCap{{ID: "x", Max: 1}, {ID: "y", Max: 1}})
	if len(pins) != 3 {
		t.Fatalf("every account must get a pin even when routes are full: %v", pins)
	}
	if len(PlanRoutePins(nil, []string{"a"}, nil)) != 0 {
		t.Fatal("no routes, no pins")
	}
}

func TestPinKeyNormalises(t *testing.T) {
	if PinKey("Acct  12 ") != "acct 12" || PinKey("ACCT12") != "acct12" {
		t.Fatal("PinKey must lower-case and collapse spaces")
	}
}

func TestRoutePinsFileRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pins.json")
	if got, err := LoadRoutePins(p); err != nil || len(got) != 0 {
		t.Fatalf("missing file must load empty: %v %v", got, err)
	}
	want := map[string]string{"b": "warp", "a": "direct"}
	if err := SaveRoutePins(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRoutePins(p)
	if err != nil || len(got) != 2 || got["a"] != "direct" || got["b"] != "warp" {
		t.Fatalf("round trip: %v %v", got, err)
	}
}

// TestAcquireHonoursPin checks a pinned account only ever takes its own route, even when an
// earlier route has room, and waits rather than moving when its route cannot admit it.
func TestAcquireHonoursPin(t *testing.T) {
	cfg := &RoutePool{Direct: DirectRoute{Enabled: true, Max: 5}}
	p := newRoutePool(cfg, newLoginRateGate(), "ws://localhost/", nil)
	p.routes = append(p.routes, &route{id: "second", kind: "direct", host: "second", direct: true, max: 5, health: routeHealthy, responses: map[int]int64{}, gate: newLoginRateGate()})
	p.pins = map[string]string{"pinned": "second"}
	a := &account{username: "Pinned"}
	r, ok := p.acquire(make(chan struct{}), a, func() bool { return false }, func(bool) {})
	if !ok || r.id != "second" {
		t.Fatalf("pinned account took %v, want second", r)
	}
	free := &account{username: "free"}
	r2, ok := p.acquire(make(chan struct{}), free, func() bool { return false }, func(bool) {})
	if !ok || r2.id != "direct" {
		t.Fatalf("unpinned account took %v, want the first route with room", r2)
	}
	p.routes[1].health = routeUnhealthy
	p.routes[1].unhealthyUntil = p.now().Add(1 << 40)
	other := &account{username: "pinned"}
	done := make(chan struct{})
	go func() { time.Sleep(50 * time.Millisecond); close(done) }()
	_, ok = p.acquire(done, other, func() bool { return false }, func(bool) {})
	if ok {
		t.Fatal("a pinned account must wait for its own route rather than fail over")
	}
}

// A route whose Max dropped keeps the pins it can hold; the rest move to the route with the most room.
func TestPlanRoutePinsOverCap(t *testing.T) {
	existing := map[string]string{"a": "warp", "b": "warp", "c": "warp", "d": "warp"}
	got := PlanRoutePins(existing, []string{"a", "b", "c", "d"}, []RouteCap{{ID: "direct", Max: 10}, {ID: "warp", Max: 2}})
	if got["a"] != "warp" || got["b"] != "warp" || got["c"] != "direct" || got["d"] != "direct" {
		t.Errorf("got %v", got)
	}
}
