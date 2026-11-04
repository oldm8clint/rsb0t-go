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

// The Titan's ground on the real collision pack, loc index and config (RSB0T_COLLISION_PACK,
// RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG).
func titanRealGround(t *testing.T) (pk *pathfind.Pack, sight func(x, z int) fleetplan.Sight, east, west map[[2]int]bool) {
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
	ix.Each(TitanTile.X-20, TitanTile.Z-20, TitanTile.X+20, TitanTile.Z+20, 0, func(l locindex.Loc) {
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
	sight = fleetplan.BlockerSight(func(x, z int) uint8 { return pk.WallMask(x, z, 0) }, func(x, z int) bool { return blocked[[2]int{x, z}] })
	east = pk.Flood(pathfind.NavPoint{X: TitanArrival.X, Z: TitanArrival.Z}, 120)
	west = pk.Flood(pathfind.NavPoint{X: TitanPassTile.X, Z: TitanPassTile.Z}, 120)
	return pk, sight, east, west
}

// engineCast is the Engine-TS routefinder's own answer on map square m43_73 with the Titan's
// collision (blockwalk all) applied: for each ground tile x 2788 to 2804 (columns) and z 4732 down to
// 4708 (rows), 'C' where Player.inApproachDistance(10) holds for a caster on it (Chebyshev 2 to 10
// from him and rsmod's hasLineOfSight with BLOCK_NPC_AND_PLAYERS), '#' walk-blocked, 'a' next to him,
// 'l' in sight but over 10 off, '.' out of sight. Produced by running the engine's own
// src/engine/routefinder against the map files (docs/evidence/grail-titan-2026-10-13/titan_probe.ts).
var engineCast = []string{
	"##.......##CC....", // 4732
	"#...#.....##...#.",
	"#.........C##....",
	"#........C..##...",
	"##......C....####",
	"#####..CC##....ll",
	"####...C.##..Clll",
	"C###..CCCCCCCCll#",
	"CC##.CCCCCCCCClll",
	"CC###CCCCCCCCClll",
	"CCa#aCCCCCCCCClll", // 4722, his row
	"CC###CCCCCCC##..#",
	"CC###CCCCCCC##..#",
	"C.###.CCCCCCCC...",
	"#.###..C.##..Clll",
	"..###..CC##....ll",
	"..###...C........",
	"..####...C.......",
	"..####....C......",
	"...###....CC.....",
	"...######..CC..#.",
}

func engineCanCast(x, z int) bool {
	row := 4732 - z
	col := x - 2788
	if row < 0 || row >= len(engineCast) || col < 0 || col >= len(engineCast[row]) {
		return false
	}
	return engineCast[row][col] == 'C'
}

// OnTitanSide is the arrival pocket and nothing else: every tile a walk from the arrival tile
// reaches is on his side, and no tile a walk from the far end of his bridge reaches is.
func TestOnTitanSideMatchesTheRealPocket(t *testing.T) {
	_, _, east, west := titanRealGround(t)
	if len(east) < 200 || len(east) > 400 || len(west) < 1000 {
		t.Fatalf("the pocket is %d tiles and the far side %d", len(east), len(west))
	}
	for p := range east {
		if west[p] {
			t.Fatalf("%v is on both sides of the bridge", p)
		}
		if !OnTitanSide(tile.Tile{X: p[0], Z: p[1]}) {
			t.Errorf("%v is in the arrival pocket but not OnTitanSide", p)
		}
	}
	for p := range west {
		if OnTitanSide(tile.Tile{X: p[0], Z: p[1]}) {
			t.Errorf("%v is across the bridge but OnTitanSide", p)
		}
	}
	for _, p := range []tile.Tile{TitanArrival, TitanAttackStand, TitanMeleeApproach, TitanNaiveTrap} {
		if !east[[2]int{p.X, p.Z}] {
			t.Errorf("%v is not in the arrival pocket", p)
		}
	}
	for _, p := range []tile.Tile{TitanPassTile, GrailFisherman, GrailBellRing} {
		if !west[[2]int{p.X, p.Z}] {
			t.Errorf("%v is not across the bridge", p)
		}
	}
}

// The realm walks of 13 Oct 2026 ("no path from 2801,4710,0 to 2803,4705,0: unreachable", five walks
// to 2802,4706,0, the Fisherman who never opened a dialogue): the Fisherman and the bell lie across
// the river from the arrival pocket, and only the Titan's bridge joins them. The walks were right
// that there was no path.
func TestTheFishermanIsAcrossTheBridgeFromTheArrivalPocket(t *testing.T) {
	pk, _, _, _ := titanRealGround(t)
	arrival := pathfind.NavPoint{X: TitanArrival.X, Z: TitanArrival.Z}
	for _, to := range []tile.Tile{GrailFisherman, {X: 2803, Z: 4705}, {X: 2804, Z: 4704}, GrailBellRing} {
		at := pathfind.NavPoint{X: to.X, Z: to.Z}
		if r := pk.FindPath(arrival, at); r.OK {
			t.Errorf("a walk from the arrival tile to %v exists (%d steps): the Titan would not be the only way across", to, r.Steps())
		}
		if r := pk.FindPath(pathfind.NavPoint{X: TitanPassTile.X, Z: TitanPassTile.Z}, at); !r.OK {
			t.Errorf("no walk from where the victor lands to %v: %s", to, r.Reason)
		}
	}
	// The Titan stands on the middle tile of a three tile bridge, which is blocked ground itself.
	if pk.Walkable(TitanTile.X, TitanTile.Z, 0) || !pk.Walkable(TitanAttackStand.X, TitanAttackStand.Z, 0) || !pk.Walkable(TitanPassTile.X, TitanPassTile.Z, 0) {
		t.Error("the bridge is not the three tiles the study measured")
	}
}

// Why every account stood on 2793,4721: it is walkable, it touches the Titan's chasm, and it is only
// a diagonal of his attack tile, so the engine refuses an interaction from it; the attack tile is the
// one tile east of him, and the melee fallback's approach tile is one step from it.
func TestTheNaiveWalkTrapIsNotAStandAndTheMeleeApproachIsOneStepOff(t *testing.T) {
	pk, _, east, _ := titanRealGround(t)
	titan := pathfind.NavPoint{X: TitanTile.X, Z: TitanTile.Z}
	trap := pathfind.NavPoint{X: TitanNaiveTrap.X, Z: TitanNaiveTrap.Z}
	if !pk.Walkable(trap.X, trap.Z, 0) || !east[[2]int{trap.X, trap.Z}] {
		t.Fatal("the trap tile is not ground of the arrival pocket")
	}
	if pk.CanInteractFrom(trap, titan) {
		t.Error("an interaction with the Titan is accepted from the naive walk's dead end")
	}
	stand := pathfind.NavPoint{X: TitanAttackStand.X, Z: TitanAttackStand.Z}
	if !pk.CanInteractFrom(stand, titan) {
		t.Error("the attack stand does not touch the Titan")
	}
	if got := pk.InteractTiles(titan); len(got) != 2 {
		// west of him is the far side of the bridge; east of him is the one an account can reach
		t.Errorf("interact tiles %v, want the two ends of the bridge", got)
	}
	appr := pathfind.NavPoint{X: TitanMeleeApproach.X, Z: TitanMeleeApproach.Z}
	if r := pk.FindPath(appr, stand); !r.OK || r.Steps() != 1 {
		t.Errorf("approach to stand: %+v", r)
	}
	if tile.Chebyshev(TitanNaiveTrap, TitanTile) != 2 || tile.Chebyshev(TitanNaiveTrap, TitanTile) >= TitanCastMin {
		t.Error("the trap tile is two tiles off him, inside the cast minimum")
	}
}

// The cast tiles on the real pack: every one is in the arrival pocket, three to eight tiles from the
// Titan, a walk from the arrival tile reaches it, and the engine's own line of sight agrees a spell
// reaches him from it; the first is the straight run along his row, five tiles off.
func TestTitanCastTilesOnTheRealPack(t *testing.T) {
	pk, sight, east, _ := titanRealGround(t)
	cands := TitanCastTiles(func(tt tile.Tile) bool { return pk.Walkable(tt.X, tt.Z, tt.Level) && east[[2]int{tt.X, tt.Z}] }, sight)
	if len(cands) < 30 {
		t.Fatalf("only %d cast tiles", len(cands))
	}
	if cands[0].Tile != (tile.Tile{X: TitanTile.X + TitanCastPrefer, Z: TitanTile.Z}) {
		t.Errorf("first cast tile %+v, want the row tile %d east of him", cands[0], TitanCastPrefer)
	}
	arrival := pathfind.NavPoint{X: TitanArrival.X, Z: TitanArrival.Z}
	for i, c := range cands {
		if !east[[2]int{c.Tile.X, c.Tile.Z}] || c.Dist < TitanCastMin || c.Dist > TitanCastMax {
			t.Errorf("bad cast tile %+v", c)
		}
		if !engineCanCast(c.Tile.X, c.Tile.Z) {
			t.Errorf("cast tile %+v: the engine's own line of sight does not reach him from there", c)
		}
		if i%7 == 0 {
			if r := pk.FindPath(arrival, pathfind.NavPoint{X: c.Tile.X, Z: c.Tile.Z}); !r.OK {
				t.Errorf("no walk from the arrival tile to cast tile %+v: %s", c, r.Reason)
			}
		}
	}
	// And the other way: every tile the engine lets a caster cast from, three to eight off in the
	// pocket, is offered (the Go sight is not stricter than the engine's anywhere that matters).
	missed := 0
	for z := 4732; z >= 4712; z-- {
		for x := 2788; x <= 2804; x++ {
			tt := tile.Tile{X: x, Z: z}
			d := tile.Chebyshev(tt, TitanTile)
			if !engineCanCast(x, z) || d < TitanCastMin || d > TitanCastMax || !east[[2]int{x, z}] {
				continue
			}
			found := false
			for _, c := range cands {
				if c.Tile == tt {
					found = true
					break
				}
			}
			if !found {
				missed++
			}
		}
	}
	if missed > 6 {
		t.Errorf("%d tiles the engine casts from were refused", missed)
	}
	// Standing on the first tile keeps the account where it is; from the arrival tile the nearest.
	if st, ok := ChooseTitanStand(cands[0].Tile, cands); !ok || st != cands[0].Tile {
		t.Errorf("stand from the first tile: %v %v", st, ok)
	}
	st, ok := ChooseTitanStand(TitanArrival, cands)
	if !ok {
		t.Fatal("no stand from the arrival tile")
	}
	if d := tile.Chebyshev(st, TitanArrival); d > 14 {
		t.Errorf("the nearest cast tile from the arrival tile is %d off (%v)", d, st)
	}
}
