package pathfind

import (
	"bytes"
	"testing"
)

// islandsPack is one mapsquare (50,50) holding two walkable 10x10 islands, (0-9,0-9) and
// (20-29,0-9) in local tiles, with every step between walkable neighbours open.
func islandsPack(t *testing.T) *Pack {
	t.Helper()
	walkable := func(x, z int) bool {
		return z >= 0 && z < 10 && ((x >= 0 && x < 10) || (x >= 20 && x < 30))
	}
	exit := make([]byte, exitBytes)
	walk := make([]byte, walkBytes)
	wall := make([]byte, wallBytes)
	for x := 0; x < 64; x++ {
		for z := 0; z < 64; z++ {
			if !walkable(x, z) {
				continue
			}
			idx := x*mapsquareTiles + z
			walk[idx>>3] |= 1 << uint(idx&7)
			var mask byte
			for d := 0; d < 8; d++ {
				nx, nz := x+dx[d], z+dz[d]
				if walkable(nx, nz) && walkable(nx, z) && walkable(x, nz) {
					mask |= 1 << uint(d)
				}
			}
			exit[idx] = mask
		}
	}
	var b bytes.Buffer
	b.Write([]byte{'L', 'C', 'N', 'V', 2, 1, 0, 0, 1, 0})
	b.Write([]byte{50, 50, 0b0001})
	b.Write(exit)
	b.Write(walk)
	b.Write(wall)
	p, err := Parse(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnreachableIsRememberedExactly(t *testing.T) {
	p := islandsPack(t)
	base := 50 << 6
	a1, a2 := NavPoint{X: base + 1, Z: base + 1}, NavPoint{X: base + 8, Z: base + 8}
	b1, b2 := NavPoint{X: base + 22, Z: base + 2}, NavPoint{X: base + 27, Z: base + 7}

	first := p.FindPath(a1, b1)
	if first.OK || first.Reason != "unreachable" || first.Expanded == 0 {
		t.Fatalf("first search: %+v", first)
	}
	// Another start on the same island, another goal on the other: answered from memory.
	if r := p.FindPath(a2, b2); r.OK || r.Reason != "unreachable" || r.Expanded != 0 {
		t.Fatalf("remembered search: %+v", r)
	}
	// A goal on the start's own island is still searched and found.
	if r := p.FindPath(a2, a1); !r.OK {
		t.Fatalf("same island: %+v", r)
	}
	// From the other island nothing is remembered yet: searched, then remembered.
	if r := p.FindPath(b1, a1); r.OK || r.Expanded == 0 {
		t.Fatalf("other island first: %+v", r)
	}
	if r := p.FindPath(b2, a2); r.OK || r.Expanded != 0 {
		t.Fatalf("other island remembered: %+v", r)
	}
	// A pack with live walls never uses what the static pack remembered.
	walled := p.WithWalls([]WallEdit{{X: base + 5, Z: base + 5, Shape: 0, Angle: 0, Add: true}})
	if walled == p {
		t.Fatal("WithWalls returned the same pack; the test proves nothing")
	}
	if r := walled.FindPath(a2, b2); r.OK || r.Expanded == 0 {
		t.Fatalf("walled pack answered from the static pack's memory: %+v", r)
	}
	// An avoid predicate is never answered from memory.
	if r := p.FindPathAvoiding(a2, b2, func(int, int, int) bool { return false }); r.OK || r.Expanded == 0 {
		t.Fatalf("avoiding search answered from memory: %+v", r)
	}
}
