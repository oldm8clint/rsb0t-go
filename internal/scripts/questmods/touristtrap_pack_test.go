package questmods

// The Tourist Trap on the real data: the production config, the baked loc index, the collision pack and the web graph (RSB0T_CONFIG_JAG,
// RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan carries names what it says in
// the config the fleet's client reads, whether the area boxes are the pieces of ground the pack makes them (a wrong box sends an account to the wrong
// crossing), whether the customs' own loc queries find their locs where the plan says and each has a stand to operate it from, whether every walk
// the quest makes inside a piece of ground is a route the planner finds, and whether the gate, the doors, the cave, the carts and the tent are the
// crossings the planner cannot find (so that the pocket is real).

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestTouristTrapIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjTtSkin4: "Waterskin(4)", questplan.ObjTtSkin3: "Waterskin(3)", questplan.ObjTtSkin2: "Waterskin(2)", questplan.ObjTtSkin1: "Waterskin(1)",
		questplan.ObjTtSkin0: "Waterskin(0)", questplan.ObjTtShirt: "Desert shirt", questplan.ObjTtRobe: "Desert robe", questplan.ObjTtBoots: "Desert boots",
		questplan.ObjTtMetalKey: "Metal key", questplan.ObjTtCellKey: "Cell door key", questplan.ObjTtBarrel: "Barrel", questplan.ObjTtAnaBarrel: "Ana in a barrel",
		questplan.ObjTtGoodMineKey: "Wrought iron key", questplan.ObjTtSlaveShirt: "Slaves' shirt", questplan.ObjTtSlaveRobe: "Slave robe",
		questplan.ObjTtSlaveBoots: "Slave boots", questplan.ObjTtDisclaimer: "Shantay disclaimer", questplan.ObjTtDart: "Prototype dart",
		questplan.ObjTtPlans: "Technical plans", questplan.ObjTtPineapple: "Tenti pineapple", questplan.ObjTtBedobinKey: "Bedobin key",
		questplan.ObjTtDartTip: "Prototype dart tip", questplan.ObjTtPass: "Shantay pass", questplan.ObjTtPunishRock: "Rock", questplan.ObjTtFeather: "Feather",
		questplan.ObjTtBronzeDart: "Bronze dart", questplan.ObjTtKebab: "Kebab", questplan.ObjTtBronzePickax: "Bronze pickaxe", questplan.ObjBronzeBar: "Bronze bar",
		questplan.ObjHammer: "Hammer", 385: "Shark", 365: "Bass",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// The stackable ones are the stacks the plan's kit says (a pass, feathers, the dart's two forms, the reward darts).
	for _, id := range []int{questplan.ObjTtPass, questplan.ObjTtFeather, questplan.ObjTtDart, questplan.ObjTtDartTip, questplan.ObjTtBronzeDart} {
		if o, _ := cfg.Obj(id); !o.Stackable {
			t.Errorf("obj %d (%s) is not stackable", id, o.Name)
		}
	}
	for id, want := range map[int]string{
		questplan.NpcTtAna: "Ana", questplan.NpcTtSlave: "Male slave", questplan.NpcTtEscaped: "Escaping slave", questplan.NpcTtRowdySlave: "Rowdy slave",
		questplan.NpcTtMerc: "Mercenary", questplan.NpcTtCaveGuard: "Mercenary", questplan.NpcTtCaptain: "Mercenary Captain", questplan.NpcTtSiad: "Captain Siad",
		questplan.NpcTtAlShabim: "Al Shabim", questplan.NpcTtBedabin: "Bedabin Nomad", questplan.NpcTtBedabinGrd: "Bedabin Nomad Guard", questplan.NpcTtIrena: "Irena",
		questplan.NpcTtDriver: "Mine cart driver", questplan.NpcTtRowdyGuard: "Rowdy Guard", questplan.NpcTtDesertWolf: "Desert Wolf", questplan.NpcTtUgthanki: "Ugthanki",
		questplan.NpcShantay: "Shantay",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
		if foe, known := questplan.FoeByNPC(id); known && foe.Name != want {
			t.Errorf("FoeByNPC(%d) is %q, the config's npc is %q", id, foe.Name, want)
		}
	}
	// The stops' names and ids are the config's, and each offers the talk op.
	for _, stop := range []questplan.NpcStop{questplan.TtIrenaStartStop, questplan.TtIrenaRewardStop, questplan.TtCaptainStop, questplan.TtSlaveStop, questplan.TtGuardStop,
		questplan.TtAlDealStop, questplan.TtAlKeyStop, questplan.TtAlPineappleStop, questplan.TtSiadStop, questplan.TtDriverStop} {
		n, _ := cfg.Npc(stop.ID)
		if n.Name != stop.Npc || cachecfg.OpIndex(n.Ops, "Talk-to") != 1 {
			t.Errorf("stop %q: npc %d is %q offering %v", stop.Npc, stop.ID, n.Name, n.Ops)
		}
	}
	// The shop keepers trade (op 3) and the three that are attacked have an Attack op.
	for _, id := range []int{questplan.NpcShantay, questplan.NpcTtBedabin} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Trade") != 3 {
			t.Errorf("npc %d offers %v, want Trade as op 3", id, n.Ops)
		}
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocTtPass, "Shantay pass", "Go-through", 4, 1}, {questplan.LocTtGateSouth, "Gate", "Open", 1, 1}, {questplan.LocTtGateNorth, "Gate", "Open", 1, 1},
		{questplan.LocTtMineDoorL, "Mine door entrance", "Open", 1, 1}, {questplan.LocTtMineDoorR, "Mine door entrance", "Open", 1, 1},
		{questplan.LocTtExitDoorL, "Mine door entrance", "Open", 1, 1}, {questplan.LocTtExitDoorR, "Mine door entrance", "Open", 1, 1},
		{questplan.LocTtCaveIn, "Mine cave", "Walk through", 1, 1}, {questplan.LocTtCaveOut, "Mine cave", "Walk through", 1, 1},
		{questplan.LocTtLadderUp, "Ladder", "Climb-up", 1, 1}, {questplan.LocTtLadderDown, "Ladder", "Climb-down", 1, 1}, {questplan.LocTtBookcase, "Bookcase", "Search", 1, 2},
		{questplan.LocTtDesk, "Captain Siad's Desk", "Search", 2, 1}, {questplan.LocTtChest, "Chest", "Open", 1, 1}, {questplan.LocTtWindow, "Window", "Search", 1, 1},
		{questplan.LocTtRock1, "Rock", "Climb-to", 1, 1}, {questplan.LocTtRock2, "Rock", "Climb-to", 1, 1}, {questplan.LocTtRock3, "Rock", "Climb-to", 1, 1},
		{questplan.LocTtRock4, "Rock", "Climb down", 1, 1}, {questplan.LocTtWinch, "Winch", "Use", 1, 2}, {questplan.LocTtFullBarrel, "Barrel", "Search", 1, 1},
		{questplan.LocTtEmptyBarrel, "Barrel", "Search", 1, 1}, {questplan.LocTtSurfaceCart, "Mine Cart", "Search", 2, 3}, {questplan.LocTtLiftBucket, "Winch bucket", "Use", 1, 1},
		{questplan.LocTtMineCart, "Mine Cart", "Search", 1, 1}, {questplan.LocTtCellGateL, "Gate", "Open", 1, 1}, {questplan.LocTtCellGateR, "Gate", "Open", 1, 1},
		{questplan.LocTtPunishRock, "Rocks.", "Mine", 1, 1}, {questplan.LocTtAnvil, "An experimental anvil", "Use", 1, 1}, {questplan.LocTtTentDoor, "Tent door", "Walk-through", 1, 1},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || l.Width != c.w || l.Length != c.l {
			t.Errorf("loc %d is %q %dx%d (found %v), the plan says %q %dx%d", c.id, l.Name, l.Width, l.Length, ok, c.name, c.w, c.l)
			continue
		}
		if cachecfg.OpIndex(l.Ops, c.op) == 0 {
			t.Errorf("loc %d (%s) offers %v, want %q", c.id, l.Name, l.Ops, c.op)
		}
	}
	// The ops the plan sends by index: the cave's "Walk through" is op 1, the rocks' climb op 2, the anvil's Use op 1.
	if l, _ := cfg.Loc(questplan.LocTtRock1); cachecfg.OpIndex(l.Ops, "Climb-to") != 2 {
		t.Errorf("rock 1 offers %v", l.Ops)
	}
	if l, _ := cfg.Loc(questplan.LocTtRock4); cachecfg.OpIndex(l.Ops, "Climb down") != 2 {
		t.Errorf("rock 4 offers %v", l.Ops)
	}
}

// The area boxes are the pieces of ground of the pack: every tile reached on foot from a place of the quest lies in the area (and so the zone) the
// plan says, so no account is ever sent to a crossing that is not the one it stands at.
func TestTouristTrapAreasAreThePiecesOfGroundOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	flood := func(at tile.Tile, r int) map[[2]int]bool {
		return pk.Flood(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}, r)
	}
	for _, c := range []struct {
		what  string
		seed  tile.Tile
		r     int
		tiles int
		areas []questplan.TtArea
		zone  questplan.TtZone
	}{
		{"the camp's yard", questplan.TtGateIn, 100, 414, []questplan.TtArea{questplan.TtAreaCamp}, questplan.TtZoneCamp},
		{"Siad's room", tile.Tile{X: 3290, Z: 3034, Level: 1}, 60, 53, []questplan.TtArea{questplan.TtAreaCampUpper}, questplan.TtZoneCamp},
		{"the hall", questplan.TtHallExit, 100, 441, []questplan.TtArea{questplan.TtAreaHall}, questplan.TtZoneHall},
		{"the lower mine", questplan.TtCaveLower, 100, 332, []questplan.TtArea{questplan.TtAreaLower}, questplan.TtZoneLower},
		{"the deep mine", questplan.TtAnaAt, 150, 639, []questplan.TtArea{questplan.TtAreaDeep}, questplan.TtZoneDeep},
		{"the punishment mine", tile.Tile{X: 3288, Z: 9437}, 100, 92, []questplan.TtArea{questplan.TtAreaMineCell}, questplan.TtZoneNone},
		{"the tent", questplan.TtTentInside, 60, 18, []questplan.TtArea{questplan.TtAreaTent}, questplan.TtZoneOpen},
	} {
		fl := flood(c.seed, c.r)
		if len(fl) != c.tiles {
			t.Errorf("%s: the pack's ground is %d tiles, the plan was made from %d", c.what, len(fl), c.tiles)
		}
		for k := range fl {
			at := tile.Tile{X: k[0], Z: k[1], Level: c.seed.Level}
			a := questplan.TtAreaOf(at)
			ok := false
			for _, w := range c.areas {
				ok = ok || a == w
			}
			if !ok || a.ZoneOf() != c.zone {
				t.Errorf("%s: %v is reached on foot and is area %d zone %d", c.what, at, a, a.ZoneOf())
			}
		}
	}
	// The surface cell is 18 tiles, all in the cell's area; the rocks' landing tiles are outside it and in the same area.
	cell := flood(tile.Tile{X: 3285, Z: 3034}, 60)
	if len(cell) != 18 {
		t.Errorf("the surface cell is %d tiles on the pack, the plan was made from 18", len(cell))
	}
	for k := range cell {
		if a := questplan.TtAreaOf(tile.Tile{X: k[0], Z: k[1]}); a != questplan.TtAreaSurfaceCell {
			t.Errorf("the cell's tile %v is area %d", k, a)
		}
	}
	// The open desert: every tile reached from Irena is in the desert's areas, apart from the corner east of the pass's wall, which is outside content's
	// desert zones and which no walk of the quest enters.
	open := flood(questplan.TtIrena, 400)
	var odd []tile.Tile
	for k := range open {
		at := tile.Tile{X: k[0], Z: k[1]}
		switch questplan.TtAreaOf(at) {
		case questplan.TtAreaIrena, questplan.TtAreaDesert, questplan.TtAreaBedabin:
		default:
			odd = append(odd, at)
		}
	}
	for _, at := range odd {
		if !(at.X >= 3318 && at.Z >= 3136) {
			t.Errorf("the open desert's tile %v is area %d", at, questplan.TtAreaOf(at))
		}
	}
	if len(open) < 18000 {
		t.Errorf("the open desert is %d tiles", len(open))
	}
	// Shantay's side is a separate piece of ground, with the pass's north stands in it.
	north := flood(tile.Tile{X: 3304, Z: 3123}, 200)
	if !north[[2]int{questplan.TtPassNorth.X, questplan.TtPassNorth.Z}] {
		t.Errorf("the pass's north stand %v is not on Shantay's side", questplan.TtPassNorth)
	}
	for _, at := range []tile.Tile{questplan.TtIrena, questplan.TtPassSouth, questplan.TtCaptainAt, questplan.TtGateOut, questplan.TtAlAt, questplan.TtTentOutside, questplan.TtCartLanding} {
		if north[[2]int{at.X, at.Z}] {
			t.Errorf("%v is reached on foot from Shantay's side: the pass is not the way south", at)
		}
		if !open[[2]int{at.X, at.Z}] {
			t.Errorf("%v is not in the open desert's piece of ground", at)
		}
	}
	// The pieces are separate: nothing of one is reached from another.
	for _, c := range []struct {
		what     string
		from, to tile.Tile
	}{
		{"the desert from the camp", questplan.TtGateIn, questplan.TtGateOut}, {"the camp from the hall", questplan.TtHallExit, questplan.TtGateIn},
		{"the hall from the lower mine", questplan.TtCaveHall, questplan.TtCaveLower}, {"the lower mine from the deep mine", questplan.TtCaveLower, questplan.TtAnaAt},
		{"the tent from the desert", questplan.TtTentInside, questplan.TtTentOutside}, {"Siad's room from the yard", questplan.TtGateIn, tile.Tile{X: 3290, Z: 3034, Level: 1}},
	} {
		if flood(c.from, 200)[[2]int{c.to.X, c.to.Z}] && c.from.Level == c.to.Level {
			t.Errorf("%s: %v reaches %v on foot", c.what, c.from, c.to)
		}
	}
}

// Every tile of the plan that an account stands on or walks to is a floor tile of the pack, in the piece of ground the plan means.
func TestTouristTrapTilesAreFloor(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
		area questplan.TtArea
	}{
		{"Irena", questplan.TtIrena, questplan.TtAreaIrena}, {"the pass's north stand", questplan.TtPassNorth, questplan.TtAreaShantayNorth},
		{"the pass's south stand", questplan.TtPassSouth, questplan.TtAreaIrena}, {"the captain's post", questplan.TtCaptainAt, questplan.TtAreaDesert},
		{"the gate's outside", questplan.TtGateOut, questplan.TtAreaDesert}, {"the gate's inside", questplan.TtGateIn, questplan.TtAreaCamp},
		{"the slave", questplan.TtSlaveAt, questplan.TtAreaCamp}, {"the mine doors' stand", questplan.TtMineDoor, questplan.TtAreaCamp},
		{"the hall's exit stand", questplan.TtHallExit, questplan.TtAreaHall}, {"the hall's cave stand", questplan.TtCaveHall, questplan.TtAreaHall},
		{"the lower mine's cave stand", questplan.TtCaveLower, questplan.TtAreaLower}, {"the guard", questplan.TtGuardAt, questplan.TtAreaHall},
		{"Al Shabim", questplan.TtAlAt, questplan.TtAreaBedabin}, {"the tent's outside", questplan.TtTentOutside, questplan.TtAreaBedabin},
		{"the tent's inside", questplan.TtTentInside, questplan.TtAreaTent}, {"Ana", questplan.TtAnaAt, questplan.TtAreaDeep},
		{"the cart's landing", questplan.TtCartLanding, questplan.TtAreaDesert}, {"the Bedabin shop", questplan.TtBedabinShop.Anchor, questplan.TtAreaBedabin},
		{"the cell gate's inside", questplan.TtCellGate, questplan.TtAreaMineCell}, {"the pickaxe in the cell", questplan.TtCellPick[0], questplan.TtAreaMineCell},
		{"the second pickaxe", questplan.TtCellPick[1], questplan.TtAreaMineCell}, {"the cell's floor", questplan.TtCellAt, questplan.TtAreaSurfaceCell},
		{"the winch's stand", tile.Tile{X: 3280, Z: 3018}, questplan.TtAreaCamp}, {"the cart's stand", tile.Tile{X: 3286, Z: 3023}, questplan.TtAreaCamp},
		{"the desk's stand", tile.Tile{X: 3289, Z: 3033, Level: 1}, questplan.TtAreaCampUpper}, {"the bookcase's stand", tile.Tile{X: 3285, Z: 3032, Level: 1}, questplan.TtAreaCampUpper},
		{"the chest's stand", tile.Tile{X: 3293, Z: 3033, Level: 1}, questplan.TtAreaCampUpper}, {"Siad", questplan.TtSiadAt, questplan.TtAreaCampUpper},
		{"the driver", questplan.TtDriverAt, questplan.TtAreaCamp},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s %v is not a walkable tile on the real pack", c.what, c.at)
		}
		if a := questplan.TtAreaOf(c.at); a != c.area {
			t.Errorf("%s %v is area %d, the plan says %d", c.what, c.at, a, c.area)
		}
	}
	// The landing tiles of the cell's way out are not walkable ground (the content moves the account onto them).
	for _, r := range questplan.TtJailRocks {
		if questplan.TtAreaOf(r) != questplan.TtAreaSurfaceCell {
			t.Errorf("the cell's landing %v is area %d", r, questplan.TtAreaOf(r))
		}
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestTouristTrapLocQueriesFindTheirLocsAndStands(t *testing.T) {
	d := csDeps(t)
	cs := []struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stand     tile.Tile // a stand the customs use
		hasStand  bool
	}{
		{"the pass", questplan.LocTtPass, "Go-through", tile.Tile{X: 3302, Z: 3116}, 6, 3302, 3116, 0, tile.Tile{X: 3303, Z: 3115}, true},
		{"the gate (south leaf)", questplan.LocTtGateSouth, "Open", questplan.TtGateOut, 3, 3273, 3029, 0, questplan.TtGateOut, true},
		{"the gate (north leaf)", questplan.LocTtGateNorth, "Open", questplan.TtGateOut, 3, 3273, 3028, 0, tile.Tile{X: 3273, Z: 3028}, true},
		{"the gate, from inside", questplan.LocTtGateSouth, "Open", questplan.TtGateIn, 3, 3273, 3029, 0, questplan.TtGateIn, true},
		{"the mine door", questplan.LocTtMineDoorL, "Open", questplan.TtMineDoor, 3, 3301, 3036, 0, questplan.TtMineDoor, true},
		{"the exit door", questplan.LocTtExitDoorL, "Open", tile.Tile{X: 3278, Z: 9426}, 3, 3278, 9426, 0, questplan.TtHallExit, true},
		{"the cave in", questplan.LocTtCaveIn, "Walk through", tile.Tile{X: 3281, Z: 9415}, 3, 3281, 9415, 0, tile.Tile{X: 3280, Z: 9415}, true},
		{"the cave out", questplan.LocTtCaveOut, "Walk through", tile.Tile{X: 3283, Z: 9415}, 3, 3283, 9415, 0, tile.Tile{X: 3284, Z: 9415}, true},
		{"the ladder up", questplan.LocTtLadderUp, "Climb-up", questplan.TtLadder, 3, 3290, 3036, 0, tile.Tile{X: 3290, Z: 3035}, true},
		{"the ladder down", questplan.LocTtLadderDown, "Climb-down", tile.Tile{X: 3290, Z: 3036, Level: 1}, 4, 3290, 3036, 1, tile.Tile{X: 3290, Z: 3035, Level: 1}, true},
		{"the bookcase", questplan.LocTtBookcase, "Search", questplan.TtBookcase, 4, 3284, 3032, 1, tile.Tile{X: 3285, Z: 3032, Level: 1}, true},
		{"the desk", questplan.LocTtDesk, "Search", questplan.TtDesk, 4, 3290, 3033, 1, tile.Tile{X: 3289, Z: 3033, Level: 1}, true},
		{"the chest", questplan.LocTtChest, "Open", questplan.TtChest, 3, 3292, 3033, 1, tile.Tile{X: 3293, Z: 3033, Level: 1}, true},
		{"the window", questplan.LocTtWindow, "Search", questplan.TtWindowAt, 3, 3284, 3032, 0, tile.Tile{X: 3284, Z: 3032}, true},
		{"the winch", questplan.LocTtWinch, "Use", questplan.TtWinchAt, 3, 3279, 3017, 0, tile.Tile{X: 3280, Z: 3018}, true},
		{"the full barrel at the top", questplan.LocTtFullBarrel, "Search", questplan.TtTopBarrel, 1, 3278, 3017, 0, tile.Tile{X: 3278, Z: 3018}, true},
		{"the surface cart", questplan.LocTtSurfaceCart, "Search", questplan.TtSurfaceCart, 3, 3287, 3023, 0, tile.Tile{X: 3286, Z: 3023}, true},
		{"the winch bucket", questplan.LocTtLiftBucket, "Use", questplan.TtLiftAt, 3, 3292, 9423, 0, tile.Tile{X: 3292, Z: 9422}, true},
		{"the lower barrel", questplan.LocTtEmptyBarrel, "Search", questplan.TtLowerBarrel, 2, 3303, 9415, 0, tile.Tile{X: 3303, Z: 9416}, true},
		{"the lower cart", questplan.LocTtMineCart, "Search", questplan.TtLowerCart, 3, 3303, 9417, 0, tile.Tile{X: 3303, Z: 9416}, true},
		{"the deep cart", questplan.LocTtMineCart, "Search", questplan.TtDeepCart, 3, 3318, 9430, 0, tile.Tile{X: 3319, Z: 9430}, true},
		{"the cell gate", questplan.LocTtCellGateR, "Open", questplan.TtCellGate, 3, 3292, 9434, 0, questplan.TtCellGate, true},
		{"the anvil", questplan.LocTtAnvil, "Use", questplan.TtAnvil, 4, 3171, 3048, 0, tile.Tile{X: 3171, Z: 3047}, true},
		{"the tent door, from inside", questplan.LocTtTentDoor, "Walk-through", questplan.TtTentInside, 3, 3169, 3046, 0, questplan.TtTentInside, true},
		{"the tent door, from outside", questplan.LocTtTentDoor, "Walk-through", questplan.TtTentOutside, 3, 3169, 3046, 0, questplan.TtTentOutside, true},
	}
	for _, c := range cs {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d offering %q found near %v", c.what, c.id, c.op, c.near)
			continue
		}
		if m.X != c.x || m.Z != c.z || m.Level != c.lvl {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		stands := questrunner.LocStands(d, m, c.near)
		if len(stands) == 0 {
			t.Errorf("%s: no stand", c.what)
			continue
		}
		found := false
		for _, s := range stands {
			found = found || s == c.stand
			if m.Shape == 10 && s.X == m.X && s.Z == m.Z && s.Level == m.Level {
				t.Errorf("%s: its own tile %v is among the stands", c.what, s)
			}
		}
		if c.hasStand && !found {
			t.Errorf("%s: the stand %v the customs use is not among the engine's %v", c.what, c.stand, stands)
		}
	}
	// The chest's forced approach: its east side alone.
	chest, _ := questrunner.FindLoc(d, questplan.LocTtChest, "", "Open", questplan.TtChest, 3)
	if stands := questrunner.LocStands(d, chest, tile.Tile{X: 3293, Z: 3033, Level: 1}); len(stands) != 1 || stands[0] != (tile.Tile{X: 3293, Z: 3033, Level: 1}) {
		t.Errorf("the chest's stands are %v, want its east side alone", stands)
	}
	// The cell's barrels: the ones a barrel is taken from (3 beside the cart) and the one Ana is searched for in.
	if ms := ttBarrelsNear(d, questplan.TtLowerBarrel, 5); len(ms) < 3 {
		t.Errorf("%d empty barrels near the lower cart, want at least 3", len(ms))
	}
	// The rocks the way out of the punishment mine mines: plenty, each with a stand.
	if ms := ttCellRocks(d); len(ms) < 40 {
		t.Errorf("%d rocks in the punishment mine, want at least 40", len(ms))
	}
}

// ttCellRocks is the punishment mine's rocks the test counts.
func ttCellRocks(d *questrunner.Deps) []tile.Tile {
	var out []tile.Tile
	for x := 3284; x <= 3292; x++ {
		for z := 9427; z <= 9452; z++ {
			if m, ok := questrunner.FindLoc(d, questplan.LocTtPunishRock, "", "Mine", tile.Tile{X: x, Z: z}, 0); ok && m.X == x && m.Z == z {
				out = append(out, tile.Tile{X: x, Z: z})
			}
		}
	}
	return out
}

// Every walk the quest makes inside a piece of ground is a route the planner finds on the real pack and graph; and the crossings between the pieces are
// the ones the planner cannot find, so the pocket is real.
func TestTouristTrapLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	opts := webwalk.Options{AllowSpecial: false}
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// The open desert.
		{"Irena to the captain", questplan.TtIrena, questplan.TtCaptainAt}, {"the captain to the gate", questplan.TtCaptainAt, questplan.TtGateOut},
		{"the gate to Al Shabim", questplan.TtGateOut, questplan.TtAlAt}, {"Al Shabim to the gate", questplan.TtAlAt, questplan.TtGateOut},
		{"Al Shabim to the tent", questplan.TtAlAt, questplan.TtTentOutside}, {"Al Shabim to the shop", questplan.TtAlAt, questplan.TtBedabinShop.Anchor},
		{"the cart's landing to Irena", questplan.TtCartLanding, questplan.TtIrena}, {"the pass's south side to the gate", questplan.TtPassSouth, questplan.TtGateOut},
		{"Irena to the pass", questplan.TtIrena, questplan.TtPassSouth}, {"the gate to the pass", questplan.TtGateOut, questplan.TtPassSouth},
		// The camp.
		{"the gate to the slave", questplan.TtGateIn, questplan.TtSlaveAt}, {"the slave to the mine doors", questplan.TtSlaveAt, questplan.TtMineDoor},
		{"the mine doors to the gate", questplan.TtMineDoor, questplan.TtGateIn}, {"the gate to the ladder", questplan.TtGateIn, tile.Tile{X: 3290, Z: 3035}},
		{"the gate to the winch", questplan.TtGateIn, tile.Tile{X: 3280, Z: 3018}}, {"the winch to the cart", tile.Tile{X: 3280, Z: 3018}, tile.Tile{X: 3286, Z: 3023}},
		{"the cart to the driver", tile.Tile{X: 3286, Z: 3023}, questplan.TtDriverAt}, {"the mine doors to the winch", questplan.TtMineDoor, tile.Tile{X: 3280, Z: 3018}},
		// Siad's room.
		{"the ladder's top to the bookcase", tile.Tile{X: 3290, Z: 3035, Level: 1}, tile.Tile{X: 3285, Z: 3032, Level: 1}},
		{"the bookcase to Siad", tile.Tile{X: 3285, Z: 3032, Level: 1}, questplan.TtSiadAt}, {"Siad to the chest", questplan.TtSiadAt, tile.Tile{X: 3293, Z: 3033, Level: 1}},
		{"the ladder's top to the desk", tile.Tile{X: 3290, Z: 3035, Level: 1}, tile.Tile{X: 3289, Z: 3033, Level: 1}},
		// The hall, the lower mine and the deep mine.
		{"the exit doors to the guard", questplan.TtHallExit, questplan.TtGuardAt}, {"the guard to the cave", questplan.TtGuardAt, tile.Tile{X: 3280, Z: 9415}},
		{"the cave to the lift", tile.Tile{X: 3284, Z: 9415}, tile.Tile{X: 3292, Z: 9422}}, {"the lift to the barrels", tile.Tile{X: 3292, Z: 9422}, tile.Tile{X: 3303, Z: 9416}},
		{"the barrels to the cave", tile.Tile{X: 3303, Z: 9416}, tile.Tile{X: 3284, Z: 9415}}, {"the cart's landing to Ana", tile.Tile{X: 3319, Z: 9431}, questplan.TtAnaAt},
		{"Ana to the deep cart", questplan.TtAnaAt, tile.Tile{X: 3319, Z: 9430}}, {"the deep cart to the cell's gate", tile.Tile{X: 3319, Z: 9430}, tile.Tile{X: 3293, Z: 9434}},
		// The punishment mine.
		{"the cell's floor to the gate", tile.Tile{X: 3288, Z: 9437}, questplan.TtCellGate}, {"the cell's pickaxe to the gate", questplan.TtCellPick[0], questplan.TtCellGate},
		{"the cell's second pickaxe to the first", questplan.TtCellPick[1], questplan.TtCellPick[0]},
		// The tent.
		{"the tent's door to the anvil", questplan.TtTentInside, tile.Tile{X: 3171, Z: 3047}},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), opts)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-36s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The crossings the planner cannot find without a special: the pocket is real.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the desert to the camp", questplan.TtGateOut, questplan.TtGateIn}, {"the camp to the desert", questplan.TtGateIn, questplan.TtGateOut},
		{"the camp to the hall", questplan.TtMineDoor, questplan.TtHallExit}, {"the hall to the camp", questplan.TtHallExit, questplan.TtGateIn},
		{"the hall to the lower mine", questplan.TtCaveHall, questplan.TtCaveLower}, {"the lower mine to the hall", questplan.TtCaveLower, questplan.TtCaveHall},
		{"the lower mine to the deep mine", tile.Tile{X: 3303, Z: 9416}, questplan.TtAnaAt}, {"the deep mine to the lower mine", questplan.TtAnaAt, tile.Tile{X: 3303, Z: 9416}},
		{"the desert to the tent", questplan.TtTentOutside, questplan.TtTentInside}, {"the tent to the desert", questplan.TtTentInside, questplan.TtTentOutside},
		{"the shop to Irena", tile.Tile{X: 3304, Z: 3123}, questplan.TtIrena},
		{"the yard to Siad's room", questplan.TtGateIn, questplan.TtSiadAt},
	} {
		r := p.Plan(pt(l.from), pt(l.to), opts)
		if r.OK && r.End().X == l.to.X && r.End().Z == l.to.Z && r.End().Level == l.to.Level && l.from.Level == l.to.Level {
			t.Errorf("%s: the planner finds a route (cost %d) without a special: the crossing is not the module's alone", l.name, r.Cost)
		}
	}
}

// The walks inside the camp keep out of the rowdy guards' squares where the pack has a way round; the ones that cannot are named, and the module
// falls back to the plain walk for them (ttTravelRound).
func TestTouristTrapRowdyGuardsCanBeWalkedRound(t *testing.T) {
	p, pt := mcPlanner(t)
	legs := []struct {
		name     string
		from, to tile.Tile
		round    bool // a route exists that keeps out of every post's square the leg does not end beside
	}{
		{"the gate to the slave", questplan.TtGateIn, questplan.TtSlaveAt, false}, {"the slave to the mine doors", questplan.TtSlaveAt, questplan.TtMineDoor, true},
		{"the gate to the mine doors", questplan.TtGateIn, questplan.TtMineDoor, false}, {"the gate to the winch", questplan.TtGateIn, tile.Tile{X: 3280, Z: 3018}, false},
		{"the gate to the ladder", questplan.TtGateIn, tile.Tile{X: 3290, Z: 3035}, false}, {"the mine doors to the winch", questplan.TtMineDoor, tile.Tile{X: 3280, Z: 3018}, true},
		{"the exit doors to the cave", questplan.TtHallExit, tile.Tile{X: 3280, Z: 9415}, false}, {"the cave to the lift", tile.Tile{X: 3284, Z: 9415}, tile.Tile{X: 3292, Z: 9422}, true},
		{"the lift to the barrels", tile.Tile{X: 3292, Z: 9422}, tile.Tile{X: 3303, Z: 9416}, true},
		{"the cart's landing to Ana", tile.Tile{X: 3319, Z: 9431}, questplan.TtAnaAt, true},
	}
	for _, l := range legs {
		if plain := p.Plan(pt(l.from), pt(l.to), webwalk.Options{}); !plain.OK {
			t.Errorf("%s: no plain route", l.name)
			continue
		}
		round := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AvoidZones: ttAvoidRowdy(l.to)})
		if round.OK != l.round {
			t.Errorf("%s: a route round the rowdy guards is %v, the plan was made for %v (a walk with none takes the plain route and the account is priced by its hitpoints)", l.name, round.OK, l.round)
		}
	}
}

// The pocket is registered: every tile of the camp, the mines, the cells and the tent is in it; the open desert, the shop and the mainland are not.
func TestTouristTrapPocketIsRegistered(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{questplan.TtGateIn, true}, {questplan.TtSlaveAt, true}, {tile.Tile{X: 3290, Z: 3034, Level: 1}, true}, {questplan.TtHallExit, true}, {questplan.TtCaveLower, true},
		{questplan.TtAnaAt, true}, {tile.Tile{X: 3288, Z: 9437}, true}, {questplan.TtCellAt, true}, {questplan.TtTentInside, true}, {questplan.TtJailRocks[2], true},
		{questplan.TtGateOut, false}, {questplan.TtCaptainAt, false}, {questplan.TtIrena, false}, {questplan.TtAlAt, false}, {questplan.TtTentOutside, false},
		{questplan.TtPassNorth, false}, {tile.Tile{X: 3304, Z: 3123}, false}, {tile.Tile{X: 3093, Z: 3243}, false},
	} {
		name, in := questrunner.PocketAt(c.at)
		if in != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, in, c.want)
		}
	}
}
