package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// fenceWorld is one synthetic 64x64 mapsquare shaped like the farm north of East Ardougne: open
// ground (well over largeComponentThreshold tiles), a fenced pen whose only way in is a two-tile
// stile in its west fence, and a sealed house.
//
//	pen:   interior x 50..60, z 50..60, fence ring on x 49/61 and z 49/61
//	stile: loc origin 48,55, one tile wide and two long, placed at angle 1, so its footprint is
//	       48..49,55 (the fence tile and the one outside it); outside stand 47,55, inside 50,55
//	house: interior x 20..24, z 20..24, walled ring on x 19/25 and z 19/25, no door
const (
	penStileX, penStileZ = 48, 55
	penStileID           = 993
)

func fenceWorld(t *testing.T) *pathfind.Pack {
	t.Helper()
	blocked := func(x, z int) bool {
		ring := func(lo, hi int) bool {
			return (x == lo || x == hi) && z >= lo && z <= hi || (z == lo || z == hi) && x >= lo && x <= hi
		}
		return ring(49, 61) || ring(19, 25) || x == penStileX && z == penStileZ
	}
	walkable := func(x, z int) bool { return x >= 0 && x < 64 && z >= 0 && z < 64 && !blocked(x, z) }
	dxs := [8]int{0, 1, 0, -1, 1, 1, -1, -1}
	dzs := [8]int{1, 0, -1, 0, 1, -1, -1, 1}
	exit := make([]byte, 4096)
	walk := make([]byte, 512)
	for x := 0; x < 64; x++ {
		for z := 0; z < 64; z++ {
			if !walkable(x, z) {
				continue
			}
			idx := x*64 + z
			walk[idx>>3] |= 1 << uint(idx&7)
			for d := 0; d < 8; d++ {
				nx, nz := x+dxs[d], z+dzs[d]
				if !walkable(nx, nz) {
					continue
				}
				if d >= 4 && (!walkable(x+dxs[d], z) || !walkable(x, z+dzs[d])) {
					continue // no cutting a fence corner
				}
				exit[idx] |= 1 << uint(d)
			}
		}
	}
	raw := append([]byte{'L', 'C', 'N', 'V', 1, 1, 0, 0, 1, 0}, []byte{0, 0, 0b0001}...)
	raw = append(raw, exit...)
	raw = append(raw, walk...)
	pk, err := pathfind.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// stileTypes is the one loc type the tests need: content's fullstyle (length=2).
type stileTypes struct{}

func (stileTypes) Loc(id int) (cachecfg.Loc, bool) {
	if id != penStileID {
		return cachecfg.Loc{}, false
	}
	return cachecfg.Loc{ID: id, Name: "Stile", Width: 1, Length: 2, BlockWalk: true, Active: true, Ops: [5]string{"Climb-over"}}, true
}

func penStile() locindex.Match {
	return locindex.Match{Loc: locindex.Loc{ID: penStileID, X: penStileX, Z: penStileZ, Shape: 10, Angle: 1}, Name: "Stile", Op: 1}
}

func hasTile(ts []tile.Tile, want tile.Tile) bool {
	for _, t := range ts {
		if t == want {
			return true
		}
	}
	return false
}

// A two-tile stile has a tile beside each of its ends. The old search tried only the four tiles
// beside the loc's origin, which for the real sheep-pen stile left out the one tile inside the pen.
func TestClimbStandsCoverBothEndsOfATwoTileStile(t *testing.T) {
	stands := climbStands(stileTypes{}, penStile())
	for _, want := range []tile.Tile{{X: 47, Z: 55}, {X: 50, Z: 55}} {
		if !hasTile(stands, want) {
			t.Errorf("stands %v are missing %v", stands, want)
		}
	}
	for _, s := range stands {
		if s.Z == penStileZ && (s.X == 48 || s.X == 49) {
			t.Errorf("stands %v include the stile's own footprint %v", stands, s)
		}
	}
	// Without the loc's type the set degrades to the origin's neighbours, as before -- and that set
	// has no tile inside the pen, which is exactly how four live accounts were shut in.
	if hasTile(climbStands(nil, penStile()), tile.Tile{X: 50, Z: 55}) {
		t.Error("the origin-only fallback unexpectedly reaches the far end of the stile")
	}
}

// The live bug: walking from open ground to somewhere the stile does not lead (a sealed house),
// the recovery must not climb into the pen.
func TestClimbRefusesAStileIntoADeadEnd(t *testing.T) {
	pk := fenceWorld(t)
	from := tile.Tile{X: 40, Z: 55}
	house := tile.Tile{X: 22, Z: 22}
	_, pocket := inPocket(pk, from)
	if pocket {
		t.Fatal("setup: open ground read as a pocket")
	}
	plan := planClimb(pk, from, house, climbStands(stileTypes{}, penStile()), pocket)
	if plan.ok {
		t.Fatalf("climbs into the pen on the way to the house (%s)", plan.why)
	}
}

// ...while a destination that really is across the stile is still climbed to.
func TestClimbTakesAStileThatLeadsToTheDestination(t *testing.T) {
	pk := fenceWorld(t)
	from := tile.Tile{X: 40, Z: 55}
	plan := planClimb(pk, from, tile.Tile{X: 55, Z: 55}, climbStands(stileTypes{}, penStile()), false)
	if !plan.ok || len(plan.near) == 0 || plan.near[0] != (tile.Tile{X: 47, Z: 55}) {
		t.Fatalf("plan %+v, want a climb from 47,55", plan)
	}
}

// From inside the pen the stile is the way out, from the far corner too, and whether or not the
// destination can be reached from the other side: open ground is always better than the pen.
func TestClimbLeavesThePenFromAnywhereInIt(t *testing.T) {
	pk := fenceWorld(t)
	stile := penStile()
	for _, tc := range []struct {
		name       string
		from, dest tile.Tile
	}{
		{"beside the stile, to open ground", tile.Tile{X: 51, Z: 55}, tile.Tile{X: 10, Z: 50}},
		{"the far corner, to open ground", tile.Tile{X: 60, Z: 60}, tile.Tile{X: 10, Z: 50}},
		{"the far corner, to the sealed house", tile.Tile{X: 60, Z: 60}, tile.Tile{X: 22, Z: 22}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			room, pocket := inPocket(pk, tc.from)
			if !pocket {
				t.Fatal("setup: the pen did not read as a pocket")
			}
			if r := climbSearchRadius(tc.from, room, pocket); tile.Chebyshev(tc.from, tile.Tile{X: 49, Z: 55}) > r {
				t.Fatalf("search radius %d does not reach the stile from %v", r, tc.from)
			}
			plan := planClimb(pk, tc.from, tc.dest, climbStands(stileTypes{}, stile), pocket)
			if !plan.ok || !hasTile(plan.near, tile.Tile{X: 50, Z: 55}) {
				t.Fatalf("plan %+v, want a climb out from 50,55", plan)
			}
			// The old approach set leaves no reachable tile at all from in here.
			if old := planClimb(pk, tc.from, tc.dest, climbStands(nil, stile), pocket); old.ok || len(old.near) != 0 {
				t.Fatalf("control: the origin-only stands found a way out (%+v)", old)
			}
		})
	}
}

// The real sheep pen north of East Ardougne, on the real pack, loc index and config. Skips without
// RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG.
func TestTheArdougneSheepPenStileOnTheRealPack(t *testing.T) {
	pk := realRoutePack(t)
	ix, cfg := realLocs(t)
	m, ok := ix.Nearest(cfg, locindex.Query{X: 2647, Z: 3375, Radius: 1, Name: "Stile", Op: "Climb-over"}, nil)
	if !ok || m.X != 2647 || m.Z != 3375 {
		t.Fatalf("no Stile at 2647,3375 (got %+v, %v)", m, ok)
	}
	stands := climbStands(cfg, m)
	yard := tile.Tile{X: 2640, Z: 3365}
	for _, tc := range []struct {
		name       string
		from, dest tile.Tile
		want       bool
	}{
		// acct57, Acct225 and Acct226: from the yard to a Farmer inside the farmhouse.
		{"yard to the farmhouse", yard, tile.Tile{X: 2649, Z: 3364}, false},
		// Acct217: from the yard to a Farmer in the wheat field.
		{"yard to the wheat field", yard, tile.Tile{X: 2655, Z: 3354}, false},
		// acct57 now, in the pen's far corner, walking to the rung's farmyard anchor.
		{"pen corner to the farmyard", tile.Tile{X: 2659, Z: 3362}, tile.Tile{X: 2643, Z: 3370}, true},
		// Acct225 now, beside the stile inside the pen, walking to the East Ardougne bank.
		{"pen to the bank", tile.Tile{X: 2650, Z: 3374}, tile.Tile{X: 2655, Z: 3283}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			room, pocket := inPocket(pk, tc.from)
			if r := climbSearchRadius(tc.from, room, pocket); tile.Chebyshev(tc.from, tile.Tile{X: 2648, Z: 3375}) > r {
				t.Fatalf("search radius %d from %v does not reach the stile", r, tc.from)
			}
			plan := planClimb(pk, tc.from, tc.dest, stands, pocket)
			if plan.ok != tc.want {
				t.Fatalf("climb=%v (%s), want %v; stands %v near %v", plan.ok, plan.why, tc.want, stands, plan.near)
			}
			if tc.want && !hasTile(plan.near, tile.Tile{X: 2649, Z: 3375}) {
				t.Fatalf("the way out is not from 2649,3375: near %v", plan.near)
			}
		})
	}
}
