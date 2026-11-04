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

// The Black Demon's cave on the real collision pack (skipped without RSB0T_COLLISION_PACK,
// RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG). The threat model is scarabplan's magicgiants one
// (magicgiants_pack_test.go), copied because it lives in a test file: a hostile NPC keeps a melee
// target on a tile only while the tile is within its maxrange+1 of its spawn AND some tile its body
// can stand on is orthogonally beside it with no wall between. Its body positions are every one a
// breadth-first search over the pack's steps reaches, which is MORE than the engine's naively
// pathing NPCs reach, so a tile this model calls safe IS safe. The question it answers for
// grandtree.go's file comment: is there a tile near the arrival tile that the demon cannot reach and
// a spell can?

const (
	demonSpawnX, demonSpawnZ = 2479, 9867 // glough.rs2 $demon_spawn 0_38_154_47_11
	demonSize                = 3          // quest_grandtree.npc size=3
	demonLeash               = 21         // maxrange=20, plus one (Npc.targetWithinMaxRange)
	arrivalX, arrivalZ       = 2491, 9864 // where the trapdoor puts the account (live logs 5 Oct)
)

var gtDX = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
var gtDZ = [8]int{1, 0, -1, 0, 1, -1, -1, 1}

func gtAbs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func gtWallBetween(pk *pathfind.Pack, ax, az, bx, bz int) bool {
	var from, toward byte
	switch {
	case bx == ax+1:
		from, toward = 1<<1, 1<<3
	case bx == ax-1:
		from, toward = 1<<3, 1<<1
	case bz == az+1:
		from, toward = 1<<0, 1<<2
	case bz == az-1:
		from, toward = 1<<2, 1<<0
	default:
		return false
	}
	return pk.WallMask(ax, az, 0)&from != 0 || pk.WallMask(bx, bz, 0)&toward != 0
}

// gtBodyCanStep: a body of `size` with its south-west tile at (x,z) may step in direction d when
// every tile of the new footprint is walkable and no wall stands on a face a new tile is entered
// through from a body tile beside it (more permissive than the engine's step rule).
func gtBodyCanStep(pk *pathfind.Pack, x, z, size, d int) bool {
	ddx, ddz := gtDX[d], gtDZ[d]
	nx0, nz0 := x+ddx, z+ddz
	inOld := func(px, pz int) bool { return px >= x && px < x+size && pz >= z && pz < z+size }
	inNew := func(px, pz int) bool { return px >= nx0 && px < nx0+size && pz >= nz0 && pz < nz0+size }
	for i := 0; i < size; i++ {
		for j := 0; j < size; j++ {
			bx, bz := nx0+i, nz0+j
			if inOld(bx, bz) {
				continue
			}
			if !pk.Walkable(bx, bz, 0) {
				return false
			}
			if ddx != 0 {
				if ax := bx - ddx; (inOld(ax, bz) || inNew(ax, bz)) && gtWallBetween(pk, ax, bz, bx, bz) {
					return false
				}
			}
			if ddz != 0 {
				if az := bz - ddz; (inOld(bx, az) || inNew(bx, az)) && gtWallBetween(pk, bx, az, bx, bz) {
					return false
				}
			}
		}
	}
	return true
}

func gtBodyPositions(pk *pathfind.Pack, sx, sz, size, bound int) map[[2]int]bool {
	start := [2]int{sx, sz}
	seen := map[[2]int]bool{start: true}
	q := [][2]int{start}
	for len(q) > 0 {
		c := q[0]
		q = q[1:]
		for d := 0; d < 8; d++ {
			n := [2]int{c[0] + gtDX[d], c[1] + gtDZ[d]}
			if seen[n] || max(gtAbs(n[0]-sx), gtAbs(n[1]-sz)) > bound {
				continue
			}
			if gtBodyCanStep(pk, c[0], c[1], size, d) {
				seen[n] = true
				q = append(q, n)
			}
		}
	}
	return seen
}

// gtMeleeBeside: a body at south-west p stands orthogonally beside t with no wall between.
func gtMeleeBeside(pk *pathfind.Pack, p [2]int, size int, t tile.Tile) bool {
	sides := []struct {
		dx, dz       int
		from, toward byte
	}{{0, 1, 1 << 0, 1 << 2}, {1, 0, 1 << 1, 1 << 3}, {0, -1, 1 << 2, 1 << 0}, {-1, 0, 1 << 3, 1 << 1}}
	for i := 0; i < size; i++ {
		for j := 0; j < size; j++ {
			fx, fz := p[0]+i, p[1]+j
			for _, s := range sides {
				if fx+s.dx == t.X && fz+s.dz == t.Z && pk.WallMask(fx, fz, 0)&s.from == 0 && pk.WallMask(t.X, t.Z, 0)&s.toward == 0 {
					return true
				}
			}
		}
	}
	return false
}

func TestBlackDemonHasNoSafeSpotAnAccountCanReach(t *testing.T) {
	packPath, locPath, cfgPath := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if packPath == "" || locPath == "" || cfgPath == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(locPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	blocked := map[[2]int]bool{}
	ix.Each(demonSpawnX-40, demonSpawnZ-40, demonSpawnX+40, demonSpawnZ+40, 0, func(l locindex.Loc) {
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

	// 1. The demon's body cannot leave a chamber round its spawn.
	pos := gtBodyPositions(pk, demonSpawnX, demonSpawnZ, demonSize, demonLeash+demonSize)
	minX, maxX, minZ, maxZ := 1<<30, -1, 1<<30, -1
	for p := range pos {
		minX, maxX = min(minX, p[0]), max(maxX, p[0]+demonSize-1)
		minZ, maxZ = min(minZ, p[1]), max(maxZ, p[1]+demonSize-1)
	}
	t.Logf("the body reaches %d positions, footprint x %d-%d, z %d-%d", len(pos), minX, maxX, minZ, maxZ)
	if len(pos) < 40 || len(pos) > 120 || maxX-minX > 30 || maxZ-minZ > 20 {
		t.Fatalf("the demon's chamber is not the small pocket the file comment describes: %d positions, x %d-%d z %d-%d", len(pos), minX, maxX, minZ, maxZ)
	}

	// 2. The tiles the account reaches from the arrival tile, and which of them the body can melee.
	type node struct{ x, z int }
	steps := map[node]int{{arrivalX, arrivalZ}: 0}
	q := []node{{arrivalX, arrivalZ}}
	for len(q) > 0 {
		c := q[0]
		q = q[1:]
		if steps[c] >= 30 {
			continue
		}
		for d := 0; d < 8; d++ {
			if pk.ExitMask(c.x, c.z, 0)&(1<<uint(d)) == 0 {
				continue
			}
			n := node{c.x + gtDX[d], c.z + gtDZ[d]}
			if _, seen := steps[n]; !seen {
				steps[n] = steps[c] + 1
				q = append(q, n)
			}
		}
	}
	leash := func(x, z int) bool {
		dx, dz := gtAbs(x-demonSpawnX), gtAbs(z-demonSpawnZ)
		return max(dx, dz) <= demonLeash && !(dx == demonLeash && dz == demonLeash)
	}
	unsafe := map[node]bool{}
	for n := range steps {
		if !leash(n.x, n.z) {
			continue
		}
		for p := range pos {
			if gtMeleeBeside(pk, p, demonSize, tile.Tile{X: n.x, Z: n.z}) {
				unsafe[n] = true
				break
			}
		}
	}
	if !unsafe[node{arrivalX, arrivalZ}] {
		t.Fatalf("the arrival tile %d,%d is outside the demon's reach: the premise of the quest's cave is wrong", arrivalX, arrivalZ)
	}

	// 3. The fewest steps on tiles the body can melee on any route to each tile (0-1 BFS).
	exposed := map[node]int{{arrivalX, arrivalZ}: 0}
	dq := []node{{arrivalX, arrivalZ}}
	for len(dq) > 0 {
		c := dq[0]
		dq = dq[1:]
		for d := 0; d < 8; d++ {
			if pk.ExitMask(c.x, c.z, 0)&(1<<uint(d)) == 0 {
				continue
			}
			n := node{c.x + gtDX[d], c.z + gtDZ[d]}
			if _, in := steps[n]; !in {
				continue
			}
			cost := 0
			if unsafe[n] {
				cost = 1
			}
			if old, ok := exposed[n]; !ok || exposed[c]+cost < old {
				exposed[n] = exposed[c] + cost
				if cost == 0 {
					dq = append([]node{n}, dq...)
				} else {
					dq = append(dq, n)
				}
			}
		}
	}

	// 4. Safe tiles (out of the body's reach, inside the leash so the demon keeps its target) and how
	// much of the demon a spell from each sees.
	nearSafe, nearSeen := 1<<30, 0
	bestExposure, bestAt := 1<<30, tile.Tile{}
	safe := 0
	for n, walk := range steps {
		if unsafe[n] || !leash(n.x, n.z) {
			continue
		}
		safe++
		from := tile.Tile{X: n.x, Z: n.z}
		seen := 0
		for p := range pos {
			if fleetplan.SafeCastable(from, tile.Tile{X: p[0], Z: p[1]}, demonSize, sight) {
				seen++
			}
		}
		if seen >= 1 && walk < nearSafe {
			nearSafe, nearSeen = walk, seen
		}
		if seen >= 5 && exposed[n] < bestExposure {
			bestExposure, bestAt = exposed[n], from
		}
	}
	t.Logf("%d safe tiles within 30 steps; the nearest from which a spell sees any body position is %d steps away and sees %d of %d; the fewest steps on melee-reachable tiles to a safe tile that sees 5 or more is %d (at %d,%d)",
		safe, nearSafe, nearSeen, len(pos), bestExposure, bestAt.X, bestAt.Z)
	if safe == 0 {
		t.Fatal("no safe tile at all within 30 steps: the model is broken, not the demon")
	}
	if nearSeen > 2 {
		t.Errorf("a safe tile %d steps from the arrival tile sees %d of %d body positions: a safespot may exist, the file comment is wrong", nearSafe, nearSeen, len(pos))
	}
	// Nine ticks at a run is the least exposure that matters: the demon is five tiles from the arrival
	// tile when the cutscene lets the account go and swings every four ticks.
	if bestExposure < 10 {
		t.Errorf("a safe tile that sees the demon is only %d melee-reachable steps from the arrival tile (at %d,%d): a safespot may exist", bestExposure, bestAt.X, bestAt.Z)
	}
}
