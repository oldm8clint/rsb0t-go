package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
)

func TestNoEscapeMemoryIsForTheSameTileAndThreatAndLapses(t *testing.T) {
	now := time.Now()
	n := fleeNoEscape{x: 3041, z: 9588, level: 0, ax: 3040, az: 9588, at: now}
	for _, c := range []struct {
		name          string
		x, z, level   int
		ax, az        int
		after         time.Duration
		wantRemembers bool
	}{
		{"same tile, same threat, just now", 3041, 9588, 0, 3040, 9588, 0, true},
		{"a tile away", 3042, 9589, 0, 3040, 9588, time.Second, true},
		{"the threat shuffled a tile or two", 3041, 9588, 0, 3042, 9589, time.Second, true},
		{"the account moved two tiles", 3043, 9588, 0, 3040, 9588, time.Second, false},
		{"the threat is three tiles from where it was", 3041, 9588, 0, 3043, 9588, time.Second, false},
		{"another level", 3041, 9588, 1, 3040, 9588, time.Second, false},
		{"after the memory lapses", 3041, 9588, 0, 3040, 9588, fleeNoEscapeMemory, false},
		{"just inside the memory", 3041, 9588, 0, 3040, 9588, fleeNoEscapeMemory - time.Millisecond, true},
	} {
		if got := n.matches(c.x, c.z, c.level, c.ax, c.az, now.Add(c.after)); got != c.wantRemembers {
			t.Errorf("%s: matches = %v, want %v", c.name, got, c.wantRemembers)
		}
	}
}

func TestNoEscapeIsRememberedPerAccountAndForgotten(t *testing.T) {
	a := &overlapSession{state: overlapState()}
	b := &overlapSession{state: overlapState()}
	if noEscapeRemembered(a, 10, 10, 0, 11, 10) {
		t.Fatal("remembered with nothing noted")
	}
	noteNoEscape(a, 10, 10, 0, 11, 10)
	if !noEscapeRemembered(a, 10, 10, 0, 11, 10) {
		t.Fatal("a plan that found nowhere to go was not remembered")
	}
	if noEscapeRemembered(b, 10, 10, 0, 11, 10) {
		t.Fatal("one account's dead end was remembered for another")
	}
	forgetNoEscape(a)
	if noEscapeRemembered(a, 10, 10, 0, 11, 10) {
		t.Fatal("remembered after a way out was found")
	}
}

func TestRouteKeepsToChecksEveryTileOfEveryLeg(t *testing.T) {
	route := []pathfind.NavPoint{{X: 0, Z: 0}, {X: 5, Z: 0}, {X: 5, Z: 5}, {X: 8, Z: 8}}
	if !routeKeepsTo(route, func(x, z, level int) bool { return false }) {
		t.Fatal("a predicate that refuses nothing refused the route")
	}
	// The first tile is the account's own and is never tested.
	if !routeKeepsTo(route, func(x, z, level int) bool { return x == 0 && z == 0 }) {
		t.Fatal("the account's own tile was held against the route")
	}
	for _, tile := range [][2]int{{3, 0}, {5, 0}, {5, 3}, {7, 7}, {8, 8}} {
		tile := tile
		if routeKeepsTo(route, func(x, z, level int) bool { return x == tile[0] && z == tile[1] }) {
			t.Errorf("tile %v on the route was not noticed", tile)
		}
	}
	if !routeKeepsTo(route[:1], func(x, z, level int) bool { return true }) {
		t.Fatal("a route of one tile has nothing to refuse")
	}
}

// The preferred distance, then each shorter rung, never the same or a longer one.
func TestFleeShorterRungsAreDescendingAndBelowThePreferred(t *testing.T) {
	prev := 1 << 30
	for _, r := range fleeShorterRungs {
		if r >= prev {
			t.Fatalf("rungs %v are not strictly descending", fleeShorterRungs)
		}
		prev = r
	}
	if fleeShorterRungs[len(fleeShorterRungs)-1] < 4 {
		t.Fatal("below 4 tiles an escape is not meaningfully different from where the account is")
	}
}
