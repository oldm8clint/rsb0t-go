package questmods

// The Watch Tower on the real data: the production config, the baked loc index and the collision pack (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX,
// RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every tile the plan names is ground, whether the executors' own loc
// queries (the id, the op, the tile and the radius they use) find their locs where the plan says and each has a tile to stand on, whether the plan's
// area tables are the pack's own sealed regions, and whether every leg the quest walks is a route the planner finds on the real pack and graph.

import (
	"fmt"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Every tile the plan names as somewhere to stand, arrive or look is ground.
func TestWatchtowerTilesAreGroundAndTheirLocsAreThere(t *testing.T) {
	d := csDeps(t)
	pk := d.Pack
	ground := map[string]tile.Tile{
		"the wall's stand": questplan.WtWallStand, "the wall's landing": questplan.WtWallLanding, "the tower's ladder stand": questplan.WtTowerLadderStand,
		"the first floor's stand": questplan.WtFirstFloorStand, "the wizard's floor": questplan.WtWizardAnchor, "the lever's stand": questplan.WtLeverStand,
		"the top ladder's stand": questplan.WtTopLadderStand, "the low ladder's stand": questplan.WtLowLadderStand, "the bush's stand": questplan.WtBushStand,
		"Og": questplan.WtOgAnchor, "the branch's stand": questplan.WtBranchStand, "the island's landing": questplan.WtIslandLanding,
		"Grew": questplan.WtGrewAnchor, "the swing's stand": questplan.WtSwingStand, "the swing's landing": questplan.WtSwingLanding,
		"the camp cave's stand": questplan.WtCampCaveStand, "the camp's arrival": questplan.WtCampArrival, "Toban": questplan.WtTobanAnchor, "Gorad": questplan.WtGoradAnchor,
		"the relic gate's stand": questplan.WtRelicGateStand, "the relic gate's inside": questplan.WtRelicGateInside, "the stall's stand": questplan.WtStallStand,
		"the wall guard": questplan.WtWallGuardAnchor, "the battlement's west": questplan.WtBattlementWest, "the battlement's east": questplan.WtBattlementEast,
		"the jump's stand": questplan.WtJumpStand, "the jump's landing": questplan.WtJumpLanding, "the jump back's stand": questplan.WtJumpBackStand,
		"the City guard": questplan.WtCityGuardAnchor, "the east gate's stand": questplan.WtEastGateStand, "the east gate's inside": questplan.WtEastGateInside,
		"the enclave guard": questplan.WtEnclaveGuard, "the dark landing": questplan.WtDarkLanding, "the nightshade of cave 2": questplan.WtNightshade2,
		"the nightshade of cave 6": questplan.WtNightshade6, "the enclave's arrival": questplan.WtEnclaveArrive, "the enclave's exit": questplan.WtEnclaveExit,
		"the rock's stand": questplan.WtRockStand, "the mirror's landing": questplan.WtMirrorLanding,
	}
	for i, b := range questplan.WtBerries {
		ground[fmt.Sprintf("berry bush %d", i+1)] = b
	}
	for i := range questplan.WtCaveStands {
		ground[fmt.Sprintf("cave %d's mouth stand", i+1)] = questplan.WtCaveStands[i]
		ground[fmt.Sprintf("cave %d's landing", i+1)] = questplan.WtCaveLandings[i]
		ground[fmt.Sprintf("cave %d's exit", i+1)] = questplan.WtCaveExits[i]
		ground[fmt.Sprintf("cave %d's skavid", i+1)] = questplan.WtCaveTalker[i]
	}
	for i, s := range questplan.WtShamanClear {
		ground[fmt.Sprintf("clear shaman %d", i+1)] = s
	}
	for i, s := range questplan.WtShamanNear {
		ground[fmt.Sprintf("near shaman %d", i+1)] = s
	}
	for what, at := range ground {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s at %v is not ground", what, at)
		}
	}
	// The candle lies on a table (the table is not ground): it is taken from a tile beside it.
	beside := false
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			beside = beside || pk.Walkable(questplan.WtCandle.X+dx, questplan.WtCandle.Z+dz, questplan.WtCandle.Level)
		}
	}
	if !beside {
		t.Errorf("the candle at %v has no ground beside it", questplan.WtCandle)
	}

	// The executors' own queries, as they make them.
	cave := func(i int) tile.Tile { return questplan.WtCaveStands[i] }
	for _, c := range []struct {
		what   string
		id     int
		op     string
		near   tile.Tile
		radius int
	}{
		{"the climbing rocks", questplan.LocWtClimbRocks, "Climb-up", tile.Tile{X: 2548, Z: 3119}, 4},
		{"the tower's ground ladder", questplan.LocWtTowerLadder, "Climb-up", tile.Tile{X: 2544, Z: 3111}, 4},
		{"the first floor's ladder up", questplan.LocWtLadderUp, "Climb-up", questplan.WtFirstFloorStand, 3},
		{"the wizard floor's ladder down", questplan.LocWtLadderDown, "Climb-down", questplan.WtWizardAnchor, 12},
		{"the mirror's ladder down", questplan.LocWtLadderDown, "Climb-down", questplan.WtMirrorLanding, 12},
		{"the first floor's ladder down", questplan.LocWtLadderTop, "Climb-down", questplan.WtLowLadderStand, 12},
		{"the mirror's first floor ladder down", questplan.LocWtLadderTop, "Climb-down", tile.Tile{X: 2929, Z: 4712, Level: 1}, 12},
		{"the nail bush", questplan.LocWtNailBush, "Search", questplan.WtBush, 4},
		{"Toban's cave", questplan.LocWtTobanCave, "Enter", questplan.WtCampCave, 4},
		{"Toban's ladder", questplan.LocWtTobanLadder, "Climb-down", questplan.WtCampLadder, 6},
		{"Toban's chest", questplan.LocWtTobanChest, "", questplan.WtCampChest, 4},
		{"the swing's tree", questplan.LocWtBranch, "", questplan.WtBranch, 4},
		{"the swing back", questplan.LocWtSwingBack, "Swing-on", tile.Tile{X: 2511, Z: 3090}, 4},
		{"the relic gate", questplan.LocWtGateRelic, "", questplan.WtRelicGateStand, 3},
		{"the cake stall", questplan.LocWtCakeStall, "Steal-From", questplan.WtStall, 3},
		{"the battlement", questplan.LocWtBattlement, "Climb-over", tile.Tile{X: 2507, Z: 3011}, 4},
		{"the jump in", questplan.LocWtJumpIn, "Jump-From", tile.Tile{X: 2530, Z: 3026}, 3},
		{"the jump out", questplan.LocWtJumpOut, "Jump-From", tile.Tile{X: 2531, Z: 3029}, 3},
		{"the east gate", questplan.LocWtGateEast, "Open", tile.Tile{X: 2550, Z: 3028}, 3},
		{"the enclave's cave", questplan.LocWtEnclaveExit, "Enter", questplan.WtEnclaveCave, 4},
		{"the Rock of Dalgroth", questplan.LocWtRock, "Mine", tile.Tile{X: 2590, Z: 9449}, 4},
		{"the lever", questplan.LocWtLever, "Pull", tile.Tile{X: 2543, Z: 3115, Level: 2}, 4},
		{"the dark cave's rope", questplan.LocWtDarkRope, "Climb", questplan.WtDarkLanding, wtDarkRopeReach},
		{"cave 1's mouth", questplan.LocWtCaveIn[0], "Enter", cave(0), 6}, {"cave 2's mouth", questplan.LocWtCaveIn[1], "Enter", cave(1), 6},
		{"cave 3's mouth", questplan.LocWtCaveIn[2], "Enter", cave(2), 6}, {"cave 4's mouth", questplan.LocWtCaveIn[3], "Enter", cave(3), 6},
		{"cave 5's mouth", questplan.LocWtCaveIn[4], "Enter", cave(4), 6}, {"cave 6's mouth", questplan.LocWtCaveIn[5], "Enter", cave(5), 6},
		{"cave 1's exit", questplan.LocWtCaveOut[0], "Leave", questplan.WtCaveLandings[0], 24}, {"cave 2's exit", questplan.LocWtCaveOut[1], "Leave", questplan.WtCaveLandings[1], 24},
		{"cave 3's exit", questplan.LocWtCaveOut[2], "Leave", questplan.WtCaveLandings[2], 24}, {"cave 4's exit", questplan.LocWtCaveOut[3], "Leave", questplan.WtCaveLandings[3], 24},
		{"cave 5's exit", questplan.LocWtCaveOut[4], "Leave", questplan.WtCaveLandings[4], 24}, {"cave 6's exit", questplan.LocWtCaveOut[5], "Leave", questplan.WtCaveLandings[5], 24},
	} {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d offering %q within %d of %v", c.what, c.id, c.op, c.radius, c.near)
			continue
		}
		stands := questrunner.LocStands(d, m, c.near)
		usable := 0
		for _, s := range stands {
			if pk.Walkable(s.X, s.Z, s.Level) {
				usable++
			}
		}
		if usable == 0 {
			t.Errorf("%s at %d,%d,%d: stands %v, none of them ground", c.what, m.X, m.Z, m.Level, stands)
			continue
		}
		t.Logf("%-32s loc %d at %d,%d,%d, %d stand(s): %v", c.what, c.id, m.X, m.Z, m.Level, usable, stands)
	}
}

// The plan's area tables are the pack's own sealed regions: every tile a region's flood reaches is that region to WtAreaOf, and the other way round.
func TestWatchtowerAreasMatchTheFloodFill(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		name string
		seed tile.Tile
		want questplan.WtArea
	}{
		{"the lower city", tile.Tile{X: 2510, Z: 3012}, questplan.WtAreaLowerCity},
		{"the ground behind the east gate", questplan.WtEastGateInside, questplan.WtAreaEastInside},
		{"the City guard's side", questplan.WtCityGuardAnchor, questplan.WtAreaCityGuard},
		{"Toban's camp", questplan.WtCampArrival, questplan.WtAreaCamp},
		{"Grew's island", questplan.WtGrewAnchor, questplan.WtAreaIsland},
	} {
		flood := pk.Flood(pathfind.NavPoint{X: c.seed.X, Z: c.seed.Z, Level: c.seed.Level}, 80)
		if len(flood) < 20 {
			t.Errorf("%s: the flood from %v reached only %d tiles", c.name, c.seed, len(flood))
		}
		wrong := 0
		for k := range flood {
			if got := questplan.WtAreaOf(tile.Tile{X: k[0], Z: k[1]}); got != c.want {
				if wrong++; wrong <= 5 {
					t.Errorf("%s: the tile %d,%d is reached from %v but WtAreaOf says %s", c.name, k[0], k[1], c.seed, got)
				}
			}
		}
		// And no tile of the region is outside the flood (the tables were cut from it).
		missing := 0
		for x := 2500; x <= 2590; x++ {
			for z := 3000; z <= 3050; z++ {
				if questplan.WtAreaOf(tile.Tile{X: x, Z: z}) != c.want || c.want == questplan.WtAreaCamp || c.want == questplan.WtAreaIsland {
					continue
				}
				if !flood[[2]int{x, z}] {
					if missing++; missing <= 5 {
						t.Errorf("%s: WtAreaOf puts %d,%d in the region but the flood from %v does not reach it", c.name, x, z, c.seed)
					}
				}
			}
		}
		t.Logf("%-34s %d tiles", c.name, len(flood))
	}
	// The camp and the island are rectangles: the flood stays inside them.
	for _, c := range []struct {
		name string
		seed tile.Tile
		want questplan.WtArea
	}{
		{"Toban's camp", questplan.WtCampArrival, questplan.WtAreaCamp}, {"Grew's island", questplan.WtGrewAnchor, questplan.WtAreaIsland},
	} {
		for k := range pk.Flood(pathfind.NavPoint{X: c.seed.X, Z: c.seed.Z}, 80) {
			if questplan.WtAreaOf(tile.Tile{X: k[0], Z: k[1]}) != c.want {
				t.Errorf("%s: the tile %d,%d is outside the plan's rectangle", c.name, k[0], k[1])
				break
			}
		}
	}
	// The underground: the caves and the enclave are the planes' two rectangles, and the landings and exits are in their own.
	for i, l := range questplan.WtCaveLandings {
		if questplan.WtAreaOf(l) != questplan.WtAreaCaves || questplan.WtCaveAt(l) != i+1 {
			t.Errorf("cave %d's landing %v is %s, cave %d", i+1, l, questplan.WtAreaOf(l), questplan.WtCaveAt(l))
		}
	}
	for what, at := range map[string]tile.Tile{"the enclave's arrival": questplan.WtEnclaveArrive, "the enclave's exit": questplan.WtEnclaveExit, "the rock": questplan.WtRockStand, "the robe": questplan.WtRobe} {
		if questplan.WtAreaOf(at) != questplan.WtAreaEnclave {
			t.Errorf("%s at %v is %s", what, at, questplan.WtAreaOf(at))
		}
	}
	for what, at := range map[string]tile.Tile{"the mirror's landing": questplan.WtMirrorLanding} {
		if questplan.WtAreaOf(at) != questplan.WtAreaMirror {
			t.Errorf("%s at %v is %s", what, at, questplan.WtAreaOf(at))
		}
	}
	for what, at := range map[string]tile.Tile{"the wizard's floor": questplan.WtWizardAnchor, "the first floor": questplan.WtFirstFloorStand, "the lever": questplan.WtLeverStand} {
		if questplan.WtAreaOf(at) != questplan.WtAreaTower {
			t.Errorf("%s at %v is %s", what, at, questplan.WtAreaOf(at))
		}
	}
}

// Every leg the quest walks is a route the planner finds on the real pack and graph, the surface legs also with the chieftains' ground fenced off
// where the module fences it.
func TestWatchtowerLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.WtBank
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// The tower.
		{"bank to the wall", bank, questplan.WtWallStand}, {"bank to the ground ladder", bank, questplan.WtTowerLadderStand},
		{"bank to the bush", bank, questplan.WtBushStand}, {"the bush to the wall", questplan.WtBushStand, questplan.WtWallStand},
		{"the bush to the ground ladder", questplan.WtBushStand, questplan.WtTowerLadderStand}, {"the bush to the bank", questplan.WtBushStand, bank},
		{"the first floor to the ladder up", questplan.WtWallLanding, questplan.WtFirstFloorStand},
		{"the wizard's floor to the lever", questplan.WtWizardAnchor, questplan.WtLeverStand},
		{"the lit candle to the bank", questplan.WtCandle, bank},
		// The tribes.
		{"bank to Og", bank, questplan.WtOgAnchor}, {"Og to the bank", questplan.WtOgAnchor, bank},
		{"bank to the branch", bank, questplan.WtBranchStand}, {"Og to the branch", questplan.WtOgAnchor, questplan.WtBranchStand},
		{"the swing's landing to the bank", questplan.WtSwingLanding, bank}, {"the swing's landing to Og", questplan.WtSwingLanding, questplan.WtOgAnchor},
		{"Og to the camp's cave", questplan.WtOgAnchor, questplan.WtCampCaveStand}, {"the bank to the camp's arrival (the cave is a graph hop)", bank, questplan.WtCampArrival}, {"bank to the camp's cave", bank, questplan.WtCampCaveStand},
		{"the camp's cave to Og", questplan.WtCampCaveStand, questplan.WtOgAnchor},
		// In the pockets (the engine leaves a pocket by its own exit first; these are the walks inside).
		{"the island's landing to Grew", questplan.WtIslandLanding, questplan.WtGrewAnchor}, {"Grew to the swing back", questplan.WtGrewAnchor, questplan.WtSwingStand},
		{"Grew to each berry bush (1)", questplan.WtGrewAnchor, questplan.WtBerries[0]}, {"Grew to each berry bush (2)", questplan.WtGrewAnchor, questplan.WtBerries[1]},
		{"Grew to each berry bush (3)", questplan.WtGrewAnchor, questplan.WtBerries[2]}, {"Grew to each berry bush (4)", questplan.WtGrewAnchor, questplan.WtBerries[3]},
		{"the camp's arrival to Toban", questplan.WtCampArrival, questplan.WtTobanAnchor}, {"the camp's arrival to the chest", questplan.WtCampArrival, questplan.WtCampChest},
		{"the camp's arrival to Gorad", questplan.WtCampArrival, questplan.WtGoradAnchor}, {"Gorad to the camp's ladder", questplan.WtGoradAnchor, questplan.WtCampLadder},
		{"Toban to the camp's ladder", questplan.WtTobanAnchor, questplan.WtCampLadder},
		// Gu'Tanoth.
		{"bank to the relic gate", bank, questplan.WtRelicGateStand}, {"Og to the relic gate", questplan.WtOgAnchor, questplan.WtRelicGateStand},
		{"inside the relic gate to the stall", questplan.WtRelicGateInside, questplan.WtStallStand},
		{"inside the relic gate to the wall guard", questplan.WtRelicGateInside, questplan.WtWallGuardAnchor},
		{"the jump's stand to the stall", questplan.WtJumpStand, questplan.WtStallStand},
		{"the jump's landing to the City guard", questplan.WtJumpLanding, questplan.WtCityGuardAnchor},
		{"the City guard to the east gate's stand", questplan.WtCityGuardAnchor, questplan.WtEastGateStand},
		{"the east gate's inside to cave 6's mouth", questplan.WtEastGateInside, questplan.WtCaveStands[5]},
		{"bank to the enclave guard", bank, questplan.WtEnclaveGuard}, {"the enclave guard to the bank", questplan.WtEnclaveGuard, bank},
		// The caves' mouths.
		{"bank to cave 1's mouth", bank, questplan.WtCaveStands[0]}, {"bank to cave 2's mouth", bank, questplan.WtCaveStands[1]},
		{"bank to cave 3's mouth", bank, questplan.WtCaveStands[2]}, {"bank to cave 4's mouth", bank, questplan.WtCaveStands[3]},
		{"bank to cave 5's mouth", bank, questplan.WtCaveStands[4]},
		// Inside the caves and the enclave.
		{"cave 1: landing to its skavid", questplan.WtCaveLandings[0], questplan.WtCaveTalker[0]},
		{"cave 2: landing to its skavid", questplan.WtCaveLandings[1], questplan.WtCaveTalker[1]},
		{"cave 2: landing to the nightshade", questplan.WtCaveLandings[1], questplan.WtNightshade2},
		{"cave 3: landing to its skavid", questplan.WtCaveLandings[2], questplan.WtCaveTalker[2]},
		{"cave 4: landing to its skavid", questplan.WtCaveLandings[3], questplan.WtCaveTalker[3]},
		{"cave 5: landing to its skavid", questplan.WtCaveLandings[4], questplan.WtCaveTalker[4]},
		{"cave 6: landing to its skavid", questplan.WtCaveLandings[5], questplan.WtCaveTalker[5]},
		{"cave 6: landing to the nightshade", questplan.WtCaveLandings[5], questplan.WtNightshade6},
		{"the enclave's arrival to the rock", questplan.WtEnclaveArrive, questplan.WtRockStand},
		{"the enclave's arrival to its cave", questplan.WtEnclaveArrive, questplan.WtEnclaveExit},
		{"the rock to the enclave's cave", questplan.WtRockStand, questplan.WtEnclaveExit},
		{"the rock to the robe", questplan.WtRockStand, questplan.WtRobe},
	}
	for i, s := range questplan.WtShamanClear {
		legs = append(legs, struct {
			name     string
			from, to tile.Tile
		}{fmt.Sprintf("the enclave's arrival to clear shaman %d", i+1), questplan.WtEnclaveArrive, s})
	}
	for i, s := range questplan.WtShamanNear {
		legs = append(legs, struct {
			name     string
			from, to tile.Tile
		}{fmt.Sprintf("the enclave's arrival to near shaman %d", i+1), questplan.WtEnclaveArrive, s})
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-52s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}

	// The surface walks keep out of the chieftains' ground when a route round it exists; where none does the module takes the plain walk and the stand is
	// priced by the decision. Both kinds are named, so a re-bake that changes one is seen.
	var fenced []webwalk.Rect
	for _, r := range questplan.WtAvoid() {
		fenced = append(fenced, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	for _, l := range []struct {
		name     string
		from, to tile.Tile
		want     bool
	}{
		{"bank to Og", bank, questplan.WtOgAnchor, true}, {"bank to the branch", bank, questplan.WtBranchStand, true},
		{"bank to the camp's cave", bank, questplan.WtCampCaveStand, true}, {"bank to the relic gate", bank, questplan.WtRelicGateStand, true},
		{"bank to cave 3's mouth", bank, questplan.WtCaveStands[2], true}, {"bank to cave 4's mouth", bank, questplan.WtCaveStands[3], true},
		{"bank to cave 5's mouth", bank, questplan.WtCaveStands[4], true},
		// The enclave guard stands in the middle of the chieftains' country: no route round it, so the plain walk is taken and the decision prices it.
		{"bank to the enclave guard", bank, questplan.WtEnclaveGuard, false},
	} {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: fenced})
		if r.OK != l.want {
			t.Errorf("%s: a fenced route exists = %v (cost %d, %s), want %v", l.name, r.OK, r.Cost, r.Reason, l.want)
		}
	}

	// The dark cave (no map or no light at a cave's mouth) has one way out, a climbing rope far from the landing: it is in reach of the executor's search
	// and a stand beside it is a route.
	d := csDeps(t)
	rope, ok := questrunner.FindLoc(d, questplan.LocWtDarkRope, "", "Climb", questplan.WtDarkLanding, wtDarkRopeReach)
	if !ok {
		t.Fatalf("no climbing rope within %d of the dark landing %v", wtDarkRopeReach, questplan.WtDarkLanding)
	}
	routed := false
	for _, s := range questrunner.LocStands(d, rope, questplan.WtDarkLanding) {
		if r := p.Plan(pt(questplan.WtDarkLanding), pt(s), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			routed = true
			break
		}
	}
	if !routed {
		t.Errorf("no stand beside the climbing rope at %d,%d is a route from the dark landing", rope.X, rope.Z)
	}
}

// The places the customs exist for are the places the planner cannot reach on foot: a gate crossed by hand, caves entered by a teleporting mouth, an
// island reached by a swing that uses a rope up, an enclave a guard lets one run into. If a re-bake ever makes one of them reachable, the walker would take
// it and the custom's leg is the wrong tool; this says so.
func TestWatchtowerTheScriptedWaysAreNotOnTheGraph(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.WtBank
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the ground behind the east gate to the outside", questplan.WtEastGateInside, questplan.WtEastGateStand},
		{"cave 6's mouth to the bank", questplan.WtCaveStands[5], bank},
		{"the bank to cave 6's mouth", bank, questplan.WtCaveStands[5]},
		{"the bank to Grew's island", bank, questplan.WtGrewAnchor},
		{"the bank to the first cave's landing", bank, questplan.WtCaveLandings[0]},
		{"the bank to the enclave's arrival", bank, questplan.WtEnclaveArrive},
		{"the bank to the mirror tower", bank, questplan.WtMirrorLanding},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): the scripted way is not the only way", l.name, r.Cost)
		}
	}
}
