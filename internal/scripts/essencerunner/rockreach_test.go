package essencerunner

import (
	"testing"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// The tile the teleport set a miner down on, one tile from the rock's origin, is not one the engine mines from (live 15 Oct
// 2026: 17 minutes of unanswered clicks there), and the rock has stand tiles that are.
func TestTheLandingBesideTheRockIsNotWhereItIsMinedFrom(t *testing.T) {
	cfg := loadConfig(t)
	locs := loadLocs(t)
	d := &Deps{Config: cfg, Locs: locs}
	here := tile.Tile{X: 2924, Z: 4847}
	m, ok := locs.Nearest(cfg, locindex.Query{X: here.X, Z: here.Z, Level: 0, Radius: 20, Name: essenceplan.EssenceRock, Op: essenceplan.MineOp}, nil)
	if !ok {
		t.Fatal("no essence rock near the landing")
	}
	l, ok := rockLoc(d, m)
	if !ok {
		t.Fatalf("no loc model for the rock %d at %d,%d", m.ID, m.X, m.Z)
	}
	reached, modelled := locreach.Reached(here.X, here.Z, l)
	if !modelled {
		t.Fatalf("the rock's shape %d is not modelled", m.Shape)
	}
	if reached {
		t.Fatalf("the landing %d,%d reaches the rock at %d,%d (%dx%d): the live miner should then have mined", here.X, here.Z, m.X, m.Z, l.Width, l.Length)
	}
	if tile.Chebyshev(here, tile.Tile{X: m.X, Z: m.Z}) > 1 {
		t.Logf("the landing is %d tiles from the rock's origin", tile.Chebyshev(here, tile.Tile{X: m.X, Z: m.Z}))
	}
	n := 0
	for _, s := range locreach.Tiles(l) {
		if r, _ := locreach.Reached(s.X, s.Z, l); r {
			n++
		}
	}
	if n == 0 {
		t.Fatal("the rock has no stand tile")
	}
}
