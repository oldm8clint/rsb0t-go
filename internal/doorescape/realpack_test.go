package doorescape

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
)

func realPack(t *testing.T) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("set RSB0T_COLLISION_PACK")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// The embedded grid is the collision the fleet walks on: every tile of the region on every level has exactly the
// walkability the collision pack bakes, and every tile that can be stood on has exactly the pack's step mask (the
// pack comes from the same map data, by another route: rsb0t-main's baker). Without this the engine-side proof
// (TestAgainstTheEngine) would only prove the port against a fixture the fleet never uses.
//
// The one thing the pack leaves out is the step mask of a tile nobody can stand on: it stores none for the
// stairwell's own tiles above the ground floor (the floor has a hole there, the staircase climbs through it),
// where the engine's canTravel, which never reads the tile it starts from, would still answer. Those tiles are
// checked to be the staircases' own footprint and nothing else.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestEmbeddedGridIsTheCollisionPack(t *testing.T) {
	pk := realPack(t)
	c := castle(t)
	g := c.Grid
	f := c.FlagsAt(c.DefaultState)
	footprint := func(x, z, level int) bool {
		for _, s := range c.Stairs {
			p := s.Placement
			if p.Level == level && x >= p.X && x < p.X+s.Type.Width && z >= p.Z && z < p.Z+s.Type.Length {
				return true
			}
		}
		return false
	}
	tiles, standable, badExit, badWalk, stairwell := 0, 0, 0, 0, 0
	for level := 0; level < g.Levels; level++ {
		for z := g.Z0 + 1; z < g.Z0+g.H-1; z++ {
			for x := g.X0 + 1; x < g.X0+g.W-1; x++ {
				tiles++
				want, got := pk.Walkable(x, z, level), Enterable(f, x, z, level)
				if want != got {
					badWalk++
					if badWalk <= 8 {
						t.Errorf("tile %d,%d,%d: enterable=%v here, walkable=%v in the pack", x, z, level, got, want)
					}
				}
				wm, gm := pk.ExitMask(x, z, level), ExitMask(f, level, x, z)
				if !want {
					if wm != gm && footprint(x, z, level) {
						stairwell++
					} else if wm != gm {
						badExit++
						t.Errorf("tile %d,%d,%d (not walkable): step mask %08b here, %08b in the pack, and it is not a staircase's footprint", x, z, level, gm, wm)
					}
					continue
				}
				standable++
				if wm != gm {
					badExit++
					if badExit <= 8 {
						t.Errorf("tile %d,%d,%d: step mask %08b here, %08b in the pack", x, z, level, gm, wm)
					}
				}
			}
		}
	}
	t.Logf("%d tiles compared (%d standable): %d step masks and %d walkable bits differ; %d stairwell tiles have a mask the pack leaves out", tiles, standable, badExit, badWalk, stairwell)
	if badExit+badWalk > 0 {
		t.Fatalf("%d step masks and %d walkable bits of %d tiles differ from the collision pack", badExit, badWalk, tiles)
	}
	if standable < 1000 {
		t.Fatalf("only %d standable tiles were compared: the region is not what the test thinks", standable)
	}
}

// The catalogue's doors and stairs are what the fleet's own loc index places, with the options the loc config
// gives them: nothing the planner clicks is a guess.
//
// Needs RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG; without them this skips.
func TestCastleAgainstTheLocIndex(t *testing.T) {
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("set RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	c := castle(t)
	has := func(p Placement) bool {
		for _, l := range ix.At(p.X, p.Z, p.Level) {
			if l.ID == p.ID && l.Angle == p.Angle && l.Shape == p.Shape {
				return true
			}
		}
		return false
	}
	for _, d := range c.Doors {
		if !has(d.Closed()) {
			t.Errorf("%s: the loc index has no loc %d at %d,%d level %d, shape %d angle %d", d.Name, d.ID, d.X, d.Z, d.Level, d.Shape, d.Angle)
		}
		closed, ok1 := cfg.Loc(d.ID)
		open, ok2 := cfg.Loc(d.OpenID)
		if !ok1 || !ok2 {
			t.Errorf("%s: loc %d or %d is not in the config", d.Name, d.ID, d.OpenID)
			continue
		}
		if got := closed.Ops[d.Op-1]; got != "Open" {
			t.Errorf("%s: closed leaf %d option %d is %q, want Open", d.Name, d.ID, d.Op, got)
		}
		if got := open.Ops[d.Op-1]; got != "Close" {
			t.Errorf("%s: open leaf %d option %d is %q, want Close", d.Name, d.OpenID, d.Op, got)
		}
		if !closed.BlockWalk || !open.BlockWalk || closed.Width != 1 || closed.Length != 1 {
			t.Errorf("%s: leaf configs %+v / %+v do not block a single tile", d.Name, closed, open)
		}
	}
	for _, s := range c.Stairs {
		if !has(s.Placement) {
			t.Errorf("%s: the loc index has no loc %d at %d,%d level %d", s.Name, s.Placement.ID, s.Placement.X, s.Placement.Z, s.Placement.Level)
		}
		l, ok := cfg.Loc(s.Placement.ID)
		if !ok {
			t.Errorf("%s: loc %d is not in the config", s.Name, s.Placement.ID)
			continue
		}
		if got := l.Ops[s.Op-1]; got != s.Label {
			t.Errorf("%s: loc %d option %d is %q, the catalogue says %q", s.Name, s.Placement.ID, s.Op, got, s.Label)
		}
		if l.Width != s.Type.Width || l.Length != s.Type.Length || l.ForceApproach != s.Type.ForceApproach {
			t.Errorf("%s: type %+v, config %+v", s.Name, s.Type, l)
		}
	}
	// And the index holds no other door or staircase in the compound that the catalogue does not know.
	known := map[[4]int]bool{}
	for _, d := range c.Doors {
		known[[4]int{d.ID, d.X, d.Z, d.Level}] = true
	}
	for _, s := range c.Stairs {
		known[[4]int{s.Placement.ID, s.Placement.X, s.Placement.Z, s.Placement.Level}] = true
	}
	for level := 0; level < 4; level++ {
		r := c.Compound[level]
		ix.Each(r.MinX, r.MinZ, r.MaxX, r.MaxZ, level, func(l locindex.Loc) {
			lt, ok := cfg.Loc(l.ID)
			if !ok {
				return
			}
			isDoor := lt.Name == "Door" || lt.Name == "Large door" || lt.Name == "Gate"
			isStair := lt.Name == "Staircase" || lt.Name == "Ladder"
			if (isDoor || isStair) && !known[[4]int{l.ID, l.X, l.Z, l.Level}] {
				t.Errorf("the loc index places %s %d at %d,%d level %d in the compound and the catalogue does not know it", lt.Name, l.ID, l.X, l.Z, l.Level)
			}
		})
	}
}
