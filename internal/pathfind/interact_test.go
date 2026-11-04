package pathfind

import "testing"

// TestCanInteractFromRefusesADiagonal is the bug class this file exists for: a tile that is
// Chebyshev 1 from a tree but only touches it corner to corner. Every caller that gated its
// approach walk on Chebyshev <= 1 clicked from here and waited forever.
func TestCanInteractFromRefusesADiagonal(t *testing.T) {
	p := openPack()
	makeSolid(p, 40, 40)
	target := NavPoint{X: 40, Z: 40}
	diagonal := NavPoint{X: 39, Z: 39}
	if d := chebyshev(diagonal.X, diagonal.Z, target.X, target.Z); d != 1 {
		t.Fatalf("the fixture is not the case under test: the diagonal is %d tiles away, want 1", d)
	}
	if p.CanInteractFrom(diagonal, target) {
		t.Error("a diagonal approach answered interactable, which is the bug")
	}
	// The control, so the check above cannot pass by answering no to everything.
	if !p.CanInteractFrom(NavPoint{X: 39, Z: 40}, target) {
		t.Error("the tile orthogonally beside the target answered not interactable")
	}
}

// TestInteractTilesOfABlockedTargetAreItsFourCardinals pins the shape of the answer: four
// tiles, none of them a corner.
func TestInteractTilesOfABlockedTargetAreItsFourCardinals(t *testing.T) {
	p := openPack()
	makeSolid(p, 40, 40)
	tiles := p.InteractTiles(NavPoint{X: 40, Z: 40})
	if len(tiles) != 4 {
		t.Fatalf("got %d interact tiles, want the 4 cardinals: %v", len(tiles), tiles)
	}
	for _, at := range tiles {
		if at.X != 40 && at.Z != 40 {
			t.Errorf("%v is a corner tile, which the engine does not accept", at)
		}
	}
}

// TestInteractTilesOfAMultiTileLocTouchItsFootprintOrthogonally covers the case a tree actually
// is: a loc several tiles across, whose own immediate neighbours are solid too. The answer then
// comes from the blocked footprint, and it must still be tiles that TOUCH — a tile diagonal to
// the centre is fine here precisely because it is orthogonal to one of the outer footprint
// tiles, which is what the engine checks.
func TestInteractTilesOfAMultiTileLocTouchItsFootprintOrthogonally(t *testing.T) {
	p := openPack()
	solid := map[[2]int]bool{}
	makeSolid(p, 40, 40)
	solid[[2]int{40, 40}] = true
	for _, side := range cardinalSides {
		makeSolid(p, 40+side.dx, 40+side.dz)
		solid[[2]int{40 + side.dx, 40 + side.dz}] = true
	}
	tiles := p.InteractTiles(NavPoint{X: 40, Z: 40})
	if len(tiles) == 0 {
		t.Fatal("a multi-tile loc answered with nowhere to interact from")
	}
	for _, at := range tiles {
		touches := false
		for _, side := range cardinalSides {
			if solid[[2]int{at.X + side.dx, at.Z + side.dz}] {
				touches = true
				break
			}
		}
		if !touches {
			t.Errorf("%v touches no part of the footprint orthogonally", at)
		}
	}
}

// TestInteractTilesOfASealedTargetIsEmpty is the honest answer for an anchor walled in past the
// reach of its own footprint, which is what a caller needs in order to say "this cannot be
// chopped" instead of clicking forever.
func TestInteractTilesOfASealedTargetIsEmpty(t *testing.T) {
	p := openPack()
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			makeSolid(p, 40+dx, 40+dz)
		}
	}
	if tiles := p.InteractTiles(NavPoint{X: 40, Z: 40}); len(tiles) != 0 {
		t.Errorf("a target with no walkable tile touching it answered %v", tiles)
	}
}

// TestCanInteractFromAcceptsAWalkableTargetAndItsCardinals covers the other half: the server's
// reach check does not care whether the target blocks, so the diagonals are out there too.
func TestCanInteractFromAcceptsAWalkableTargetAndItsCardinals(t *testing.T) {
	p := openPack()
	target := NavPoint{X: 20, Z: 20}
	if !p.Walkable(target.X, target.Z, target.Level) {
		t.Fatal("the fixture is not the case under test: the target must be walkable")
	}
	if !p.CanInteractFrom(target, target) {
		t.Error("standing on the target answered not interactable")
	}
	if !p.CanInteractFrom(NavPoint{X: 20, Z: 21}, target) {
		t.Error("the tile orthogonally beside a walkable target answered not interactable")
	}
	if p.CanInteractFrom(NavPoint{X: 21, Z: 21}, target) {
		t.Error("a diagonal answered interactable for a walkable target")
	}
}

// TestCanInteractFromRefusesAnotherLevel pins the one answer that is not a search.
func TestCanInteractFromRefusesAnotherLevel(t *testing.T) {
	p := openPack()
	if p.CanInteractFrom(NavPoint{X: 20, Z: 20, Level: 0}, NavPoint{X: 20, Z: 21, Level: 1}) {
		t.Error("a target on another level answered interactable")
	}
}
