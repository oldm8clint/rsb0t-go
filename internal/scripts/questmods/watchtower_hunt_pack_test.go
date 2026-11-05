package questmods

// The Watch Tower's hunters on the real data (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG; each test skips without them): the enclave's visits are the
// walks they name, the hunters said to be sealed in a region of their own are, and the surface routes keep out of the reach of the hunters said to be far.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// wtRouteTiles is the tiles of the A* route between two tiles of the same level, corner to corner expanded one tile a step (the pack's FindPath returns the
// corners).
func wtRouteTiles(t *testing.T, pk *pathfind.Pack, a, b tile.Tile) []tile.Tile {
	t.Helper()
	r := pk.FindPath(pathfind.NavPoint{X: a.X, Z: a.Z, Level: a.Level}, pathfind.NavPoint{X: b.X, Z: b.Z, Level: b.Level})
	if !r.OK || len(r.Waypoints) == 0 {
		t.Fatalf("no route from %v to %v (%s)", a, b, r.Reason)
	}
	var out []tile.Tile
	cx, cz, lvl := r.Waypoints[0].X, r.Waypoints[0].Z, r.Waypoints[0].Level
	out = append(out, tile.Tile{X: cx, Z: cz, Level: lvl})
	for _, w := range r.Waypoints[1:] {
		for cx != w.X || cz != w.Z {
			switch {
			case cx < w.X:
				cx++
			case cx > w.X:
				cx--
			}
			switch {
			case cz < w.Z:
				cz++
			case cz > w.Z:
				cz--
			}
			out = append(out, tile.Tile{X: cx, Z: cz, Level: lvl})
		}
	}
	return out
}

// wtMeasure counts a visit: the ticks, the walked tiles and the hunter-ticks of the dragons, the demon and the chieftains.
type wtMeasure struct {
	ticks, walk, dragon, demon, chief int
	// used is every tile the visit walks or stands on.
	used map[tile.Tile]bool
}

func (m *wtMeasure) mark(at tile.Tile) {
	if m.used == nil {
		m.used = map[tile.Tile]bool{}
	}
	m.used[at] = true
}

func (m *wtMeasure) walked(tiles []tile.Tile) {
	for _, at := range tiles {
		m.mark(at)
		m.ticks++
		m.walk++
		d, e, c := questplan.WtHuntersCovering(at)
		m.dragon, m.demon, m.chief = m.dragon+d, m.demon+e, m.chief+c
	}
}

func (m *wtMeasure) stood(at tile.Tile, ticks int) {
	m.mark(at)
	m.ticks += ticks
	d, e, c := questplan.WtHuntersCovering(at)
	m.dragon, m.demon, m.chief = m.dragon+d*ticks, m.demon+e*ticks, m.chief+c*ticks
}

// wtMeasureVisits walks the enclave's four visits on the real pack: the A* route between the visit's points, tile by tile, with the stands added.
func wtMeasureVisits(t *testing.T) (exitM, rockM, robeM, workM wtMeasure) {
	t.Helper()
	pk, _, _ := csData(t)
	arrive, exit, rock, robe := questplan.WtEnclaveArrive, questplan.WtEnclaveExit, questplan.WtRockStand, questplan.WtRobe
	route := func(a, b tile.Tile) []tile.Tile { return wtRouteTiles(t, pk, a, b) }

	exitM.walked(route(arrive, exit))
	exitM.stood(exit, questplan.WtExitOpTicks)

	rockM.walked(route(arrive, rock))
	rockM.stood(rock, questplan.WtRockStandTicks)
	rockM.walked(route(rock, exit))
	rockM.stood(exit, questplan.WtExitOpTicks)

	robeM.walked(route(arrive, robe))
	robeM.stood(robe, questplan.WtShamanStandTicks)
	robeM.walked(route(robe, exit))
	robeM.stood(exit, questplan.WtExitOpTicks)

	// The nearest-first tour of the six shaman spawns from the arrival, as the executor takes it, then the rock and the way out.
	left := append(append([]tile.Tile(nil), questplan.WtShamanClear[:]...), questplan.WtShamanNear[:]...)
	cur := arrive
	for len(left) > 0 {
		bi, bl := -1, 1<<30
		var bw []tile.Tile
		for i, s := range left {
			if w := route(cur, s); len(w) < bl {
				bi, bl, bw = i, len(w), w
			}
		}
		workM.walked(bw)
		workM.stood(left[bi], questplan.WtShamanStandTicks)
		cur = left[bi]
		left = append(left[:bi], left[bi+1:]...)
	}
	workM.walked(route(cur, rock))
	workM.stood(rock, questplan.WtRockStandTicks)
	workM.walked(route(rock, exit))
	workM.stood(exit, questplan.WtExitOpTicks)
	return exitM, rockM, robeM, workM
}

// The plan's visits are the walks they name, measured: the A* route on the real pack tile by tile, the stands added, the hunters of each tile counted by the plan's
// own reach. The plan's numbers are ceilings within a third of the measurement (and a few ticks for the small ones), so that a re-bake of the pack that moves a route
// is seen here and a number that has drifted loose is too.
func TestWatchtowerEnclaveVisitsAreTheWalksTheyName(t *testing.T) {
	exitM, rockM, robeM, workM := wtMeasureVisits(t)
	for _, c := range []struct {
		v questplan.WtVisit
		m wtMeasure
	}{{questplan.WtVisitExit, exitM}, {questplan.WtVisitRock, rockM}, {questplan.WtVisitRobe, robeM}, {questplan.WtVisitWork, workM}} {
		t.Logf("%-46s walk %3d (plan %3d)  ticks %3d (%3d)  dragon %3d (%3d)  demon %3d (%3d)  chief %3d (%3d)", c.v.Name, c.m.walk, c.v.Walk, c.m.ticks, c.v.Ticks,
			c.m.dragon, c.v.Dragon, c.m.demon, c.v.Demon, c.m.chief, c.v.Chief)
		if c.v.Walk != c.m.walk {
			t.Errorf("%s: the walk is %d tiles on the pack, the plan says %d", c.v.Name, c.m.walk, c.v.Walk)
		}
		for _, f := range []struct {
			what       string
			plan, real int
		}{{"ticks", c.v.Ticks, c.m.ticks}, {"dragon-ticks", c.v.Dragon, c.m.dragon}, {"demon-ticks", c.v.Demon, c.m.demon}, {"chieftain-ticks", c.v.Chief, c.m.chief}} {
			if f.plan < f.real || f.plan > f.real+f.real/3+6 {
				t.Errorf("%s: %s are %d on the pack, the plan says %d (a ceiling within a third and six)", c.v.Name, f.what, f.real, f.plan)
			}
		}
	}
}

// The hunters the plan calls sealed are in regions of their own: they are not in the arrival tile's region, and no tile the module's visits walk or stand on is within a
// sealed hunter's hunt range of any tile it can wander to. The ones the plan calls connected are in the account's region, and so are the shamans, the
// rock, the robe and the exit.
func TestWatchtowerEnclaveSealedHuntersAreSealed(t *testing.T) {
	pk, _, _ := csData(t)
	a := questplan.WtEnclaveArrive
	account := pk.Flood(pathfind.NavPoint{X: a.X, Z: a.Z}, 120)
	if len(account) < 1000 {
		t.Fatalf("the enclave's region is only %d tiles", len(account))
	}
	in := func(at tile.Tile) bool { return account[[2]int{at.X, at.Z}] }
	for what, spots := range map[string][]tile.Tile{
		"dragon": questplan.WtEnclaveDragons, "demon": questplan.WtEnclaveDemons, "skeleton": questplan.WtEnclaveSkeletons,
		"clear shaman": questplan.WtShamanClear[:], "near shaman": questplan.WtShamanNear[:], "chieftain": questplan.WtEnclaveChieftains,
		"rock": {questplan.WtRockStand}, "robe": {questplan.WtRobe}, "exit": {questplan.WtEnclaveExit},
	} {
		for _, at := range spots {
			if !in(at) {
				t.Errorf("the %s at %v is not in the arrival tile's region", what, at)
			}
		}
	}
	exitM, rockM, robeM, workM := wtMeasureVisits(t)
	used := map[tile.Tile]bool{}
	for _, m := range []wtMeasure{exitM, rockM, robeM, workM} {
		for at := range m.used {
			used[at] = true
		}
	}
	check := func(what string, spawn tile.Tile, wander, hunt int) {
		if in(spawn) {
			t.Errorf("the sealed %s at %v is in the arrival tile's region", what, spawn)
			return
		}
		own := pk.Flood(pathfind.NavPoint{X: spawn.X, Z: spawn.Z}, wander)
		best := 1 << 30
		for o := range own {
			for at := range used {
				if d := tile.Chebyshev(tile.Tile{X: o[0], Z: o[1]}, at); d < best {
					best = d
				}
			}
		}
		t.Logf("the sealed %s at %v wanders over %d tiles, %d from the nearest tile the visits use (hunt range %d)", what, spawn, len(own), best, hunt)
		// The engine finds a target within huntrange of the hunter's south-west tile (HuntIterator: distanceToSW), and a hunter in another region cannot walk to
		// what it finds: the claim is that nothing the module walks on is found at all.
		if best <= hunt {
			t.Errorf("the sealed %s at %v can find an account %d tiles away on the module's routes (hunt range %d)", what, spawn, best, hunt)
		}
	}
	for _, at := range questplan.WtEnclaveSealedDemons {
		check("greater demon", at, 6, 3)
	}
	for _, at := range questplan.WtEnclaveSealedBats {
		check("giant bat", at, 8, 2)
	}
}

// The surface routes of the quest keep out of the reach of the hunters the plan calls far: each leg's A* route, tile by tile, is further than the hunter's reach
// from its spawn. The legs are the ones with no door, ladder or special edge in between (the pack's FindPath is the walker's own search without those).
func TestWatchtowerSurfaceRoutesKeepOutOfTheFarHunters(t *testing.T) {
	pk, _, _ := csData(t)
	bank := questplan.WtBank
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"bank to Og", bank, questplan.WtOgAnchor}, {"bank to the branch", bank, questplan.WtBranchStand}, {"bank to the camp's cave", bank, questplan.WtCampCaveStand},
		{"Og to the camp's cave", questplan.WtOgAnchor, questplan.WtCampCaveStand}, {"the camp's cave to the bank", questplan.WtCampCaveStand, bank},
		{"bank to the relic gate", bank, questplan.WtRelicGateStand}, {"bank to the enclave guard", bank, questplan.WtEnclaveGuard},
		{"bank to cave 3's mouth", bank, questplan.WtCaveStands[2]}, {"bank to cave 4's mouth", bank, questplan.WtCaveStands[3]}, {"bank to cave 5's mouth", bank, questplan.WtCaveStands[4]},
		{"bank to cave 2's mouth", bank, questplan.WtCaveStands[1]}, {"bank to cave 1's mouth", bank, questplan.WtCaveStands[0]}, {"bank to the stall", bank, questplan.WtStallStand},
	}
	for _, l := range legs {
		r := pk.FindPath(pathfind.NavPoint{X: l.from.X, Z: l.from.Z}, pathfind.NavPoint{X: l.to.X, Z: l.to.Z})
		if !r.OK {
			t.Logf("%s: no plain route (%s): not checked", l.name, r.Reason)
			continue
		}
		tiles := wtRouteTiles(t, pk, l.from, l.to)
		nearest := 1 << 30
		for _, at := range tiles {
			for _, f := range questplan.WtFarSpawns {
				reach := questplan.WtHunterTable[f.ID].Reach
				d := tile.Chebyshev(at, f.At)
				nearest = min(nearest, d-reach)
				if d <= reach {
					t.Errorf("%s: the route passes %v, %d from the %s at %v (reach %d)", l.name, at, d, questplan.WtHunterTable[f.ID].Name, f.At, reach)
					break
				}
			}
		}
		t.Logf("%-32s %3d tiles, %d beyond the nearest far hunter's reach", l.name, len(tiles), nearest)
	}
}

// wtSurfaceOgres lists the ogres (npc 114 and 115) of the module's surface squares that stand on the open surface (not in the island or the camp), from the maps in
// RSB0T_CONTENT_DIR; the test skips without them.
func wtSurfaceOgres(t *testing.T) []tile.Tile {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT_DIR")
	if root == "" {
		t.Skip("RSB0T_CONTENT_DIR not set")
	}
	var out []tile.Tile
	for _, sq := range questplan.WtMapSquares[:6] {
		b, err := os.ReadFile(filepath.Join(root, "maps", fmt.Sprintf("m%d_%d.jm2", sq[0], sq[1])))
		if err != nil {
			t.Fatal(err)
		}
		in := false
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "==== ") {
				in = line == "==== NPC ===="
				continue
			}
			head, tail, ok := strings.Cut(line, ":")
			if !in || !ok {
				continue
			}
			var l, x, z int
			if n, _ := fmt.Sscanf(head, "%d %d %d", &l, &x, &z); n != 3 || l != 0 {
				continue
			}
			f := strings.Fields(tail)
			if id, _ := strconv.Atoi(f[0]); id == questplan.WtNpcOgre || id == questplan.WtNpcOgre2 {
				if at := (tile.Tile{X: sq[0]<<6 + x, Z: sq[1]<<6 + z}); questplan.WtAreaOf(at) == questplan.WtAreaMain {
					out = append(out, at)
				}
			}
		}
	}
	return out
}

// The sites' roads are the real routes': the ogres of the open surface whose reach covers each tile of the A* route from the bank to the site, summed and doubled
// for the way back, within a third (and ten) under the plan's number.
func TestWatchtowerSiteRoadsAreTheRoutesTheyName(t *testing.T) {
	pk, _, _ := csData(t)
	ogres := wtSurfaceOgres(t)
	for _, c := range []struct {
		site questplan.WtSite
		to   tile.Tile
	}{{questplan.WtSiteOg, questplan.WtOgAnchor}, {questplan.WtSiteBranch, questplan.WtBranchStand}, {questplan.WtSiteCaveMouth, questplan.WtCampCaveStand}} {
		ogreTicks := 0
		for _, at := range wtRouteTiles(t, pk, questplan.WtBank, c.to) {
			for _, o := range ogres {
				if tile.Chebyshev(at, o) <= questplan.WtOgreReach {
					ogreTicks++
				}
			}
		}
		real := 2 * ogreTicks
		t.Logf("%-28s the road is %3d ogre-ticks there and back (plan %3d)", c.site.Name, real, c.site.Road)
		if c.site.Road < real || c.site.Road > real+real/3+10 {
			t.Errorf("%s: the road is %d ogre-ticks on the pack, the plan says %d (a ceiling within a third and ten)", c.site.Name, real, c.site.Road)
		}
	}
}

// No route in the enclave is fenced from the dragons: from the arrival tile, every shaman, the rock, the robe and the exit are reached only through tiles in a dragon's
// reach (the search that avoids them finds nothing), so the enclave cannot be made safe by choosing stands or routes, and the price is the walk it is. The arrival tile,
// the rock's stand and the robe are outside every dragon's reach (the quiet places), but the ways to them are not.
func TestWatchtowerNoRouteInTheEnclaveIsFencedFromTheDragons(t *testing.T) {
	pk, _, _ := csData(t)
	covered := func(x, z, level int) bool {
		d, _, _ := questplan.WtHuntersCovering(tile.Tile{X: x, Z: z, Level: level})
		return d > 0
	}
	a := questplan.WtEnclaveArrive
	from := pathfind.NavPoint{X: a.X, Z: a.Z}
	if covered(a.X, a.Z, 0) {
		t.Fatalf("the arrival tile %v is in a dragon's reach", a)
	}
	targets := map[string]tile.Tile{"the exit": questplan.WtEnclaveExit, "the robe": questplan.WtRobe, "the rock": questplan.WtRockStand}
	for i, s := range questplan.WtShamanClear {
		targets[fmt.Sprintf("clear shaman %d", i+1)] = s
	}
	for i, s := range questplan.WtShamanNear {
		targets[fmt.Sprintf("near shaman %d", i+1)] = s
	}
	for what, to := range targets {
		if plain := pk.FindPath(from, pathfind.NavPoint{X: to.X, Z: to.Z}); !plain.OK {
			t.Errorf("%s: no plain route either (%s)", what, plain.Reason)
			continue
		}
		fenced := pk.FindPathAvoiding(from, pathfind.NavPoint{X: to.X, Z: to.Z}, covered)
		if what == "the robe" || what == "the rock" {
			// Their stands are outside the reach, but the way to them is not.
			if covered(to.X, to.Z, 0) {
				t.Errorf("%s at %v is in a dragon's reach", what, to)
			}
		}
		if fenced.OK {
			// A route that avoids the dragons would make the enclave safe by walking: the price would be wrong to charge the whole walk.
			t.Errorf("%s: a route round the dragons' reach exists (%d corners): the visits' prices count tiles that need not be walked", what, len(fenced.Waypoints))
		}
	}
}
