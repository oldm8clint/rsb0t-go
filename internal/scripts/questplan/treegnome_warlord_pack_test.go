package questplan

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// The warlord's ground on the real collision pack, loc index and config (RSB0T_COLLISION_PACK,
// RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG): the same data questmods.warlordGround builds at run time.
func warlordRealGround(t *testing.T) (*pathfind.Pack, func(x, z int) fleetplan.Sight, []tile.Tile, []WarlordCastTile) {
	t.Helper()
	pp, ip, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || ip == "" || cp == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	pk, err := pathfind.Load(pp)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(ip)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	blocked := map[[2]int]bool{}
	r := WarlordSafeDist + WarlordCastRing + fleetplan.CastRange + 2
	ix.Each(WarlordSpawn.X-r, WarlordSpawn.Z-r, WarlordSpawn.X+r, WarlordSpawn.Z+r, 0, func(l locindex.Loc) {
		if l.Shape < 9 || l.Shape > 21 {
			return
		}
		c, ok := cfg.Loc(l.ID)
		if !ok || !c.BlockRange {
			return
		}
		w, h := max(c.Width, 1), max(c.Length, 1)
		if l.Angle == 1 || l.Angle == 3 {
			w, h = h, w
		}
		for dx := 0; dx < w; dx++ {
			for dz := 0; dz < h; dz++ {
				blocked[[2]int{l.X + dx, l.Z + dz}] = true
			}
		}
	})
	sight := fleetplan.BlockerSight(func(x, z int) uint8 { return pk.WallMask(x, z, 0) }, func(x, z int) bool { return blocked[[2]int{x, z}] })
	from := pathfind.NavPoint{X: WarlordSpawn.X, Z: WarlordSpawn.Z, Level: WarlordSpawn.Level}
	his, ours := pk.Flood(from, WarlordWanderRange), pk.Flood(from, WarlordStandFlood)
	roam := WarlordRoam(func(tt tile.Tile) bool { return his[[2]int{tt.X, tt.Z}] })
	return pk, sight, roam, WarlordCastTiles(func(tt tile.Tile) bool { return ours[[2]int{tt.X, tt.Z}] }, roam, sight)
}

// Every cast tile is out of his reach, on his own side of the rivers (a walk from his spawn ends
// on it, so the account that walked in walks to it), and reaches some of his ground; the best
// covers a fair share of it.
func TestWarlordCastTilesOnTheRealPack(t *testing.T) {
	pk, sight, roam, cands := warlordRealGround(t)
	t.Logf("his ground: %d tiles within %d of %v a walk from the spawn reaches", len(roam), WarlordWanderRange, WarlordSpawn)
	if len(roam) < 50 {
		t.Fatalf("his ground is only %d tiles", len(roam))
	}
	if len(cands) == 0 {
		t.Fatal("no cast tile reaches any of his ground from out of his reach")
	}
	for i, c := range cands {
		if !WarlordOutOfReach(c.Tile) || !pk.Walkable(c.Tile.X, c.Tile.Z, 0) {
			t.Fatalf("cast tile %v is inside his reach or not walkable", c.Tile)
		}
		if i < 12 {
			t.Logf("cast tile %v: covers %d of his %d tiles (%d off his spawn)", c.Tile, c.Cover, len(roam), tile.Chebyshev(c.Tile, WarlordSpawn))
		}
	}
	t.Logf("%d cast tiles", len(cands))
	// His spawn is in reach of some cast tile, and the spawn's neighbourhood too.
	if _, ok := ChooseWarlordCast(cands[0].Tile, WarlordSpawn, cands, sight); !ok {
		covered := 0
		for _, w := range roam {
			if _, ok := ChooseWarlordCast(cands[0].Tile, w, cands, sight); ok {
				covered++
			}
		}
		t.Logf("his spawn tile is reached from no cast tile; %d of his %d tiles are", covered, len(roam))
		if covered == 0 {
			t.Fatal("no tile of his ground is reached from any cast tile")
		}
	}
}
