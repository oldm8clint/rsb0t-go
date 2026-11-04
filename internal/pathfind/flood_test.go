package pathfind

import "testing"

// Flood takes in the open ground inside its radius and nothing beyond it, leaves out a walled-off
// tile and a solid one, and answers nothing from a solid start.
func TestFloodIsTheConnectedGroundInsideTheRadius(t *testing.T) {
	p := openPack()
	cutOffTile(p, 31, 30)
	makeSolid(p, 29, 30)
	got := p.Flood(NavPoint{X: 30, Z: 30}, 3)
	if want := 7*7 - 2; len(got) != want {
		t.Fatalf("flooded %d tiles, want %d", len(got), want)
	}
	for _, k := range [][2]int{{31, 30}, {29, 30}, {34, 30}, {30, 26}} {
		if got[k] {
			t.Fatalf("%v is flooded", k)
		}
	}
	for _, k := range [][2]int{{30, 30}, {33, 33}, {27, 27}, {32, 30}} {
		if !got[k] {
			t.Fatalf("%v is not flooded", k)
		}
	}
	if n := len(p.Flood(NavPoint{X: 29, Z: 30}, 3)); n != 0 {
		t.Fatalf("a solid start flooded %d tiles", n)
	}
}
