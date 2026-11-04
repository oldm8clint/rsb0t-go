package questmods

// Druidic Ritual and Jungle Potion on the real data: the production config, the baked loc index, the collision pack and the web
// graph (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them, and the ones that read the maps'
// npc spawns also want RSB0T_CONTENT). The questions are whether every id the plans carry names what it says in the config the
// fleet's client reads, whether the customs' loc queries find their locs where the plans say and each has a tile to stand on,
// whether every leg the quests walk is a route the planner finds, whether the jungle's walks exist round every spider and
// tribesman and end outside the rectangles that fence them, and whether the pockets (the cauldron's room, the cave) are the regions
// the pack makes them and the cave's run meets the jogres the price counts.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestDruidJungleIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjRawBeef: "Raw beef", questplan.ObjRawChicken: "Raw chicken", questplan.ObjRawRatMeat: "Raw rat meat", questplan.ObjRawBearMeat: "Raw bear meat",
		questplan.ObjEnchantedBeef: "Enchanted beef", questplan.ObjEnchantedChicken: "Enchanted chicken",
		questplan.ObjEnchantedRatMeat: "Enchanted rat", questplan.ObjEnchantedBearMeat: "Enchanted bear",
		questplan.ObjShark: "Shark", 373: "Swordfish", 379: "Lobster",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	for _, m := range questplan.DruidMeats {
		if raw, _ := cfg.Obj(m.Raw); raw.Name != m.RawName {
			t.Errorf("the %s meat is named %q in the plan and %q in the config", m.Name, m.RawName, raw.Name)
		}
		if ench, _ := cfg.Obj(m.Enchanted); ench.Name != m.EnchantedName {
			t.Errorf("the enchanted %s meat (obj %d) is %q in the config and %q in the plan", m.Name, m.Enchanted, ench.Name, m.EnchantedName)
		}
	}
	// The herbs: the unidentified one offers Identify, the clean one is its own object, and the names are Trufitus's.
	for _, h := range questplan.JungleHerbs {
		u, ok := cfg.Obj(h.Unid)
		if !ok || u.Name != "Unidentified herb" {
			t.Errorf("herb %s: obj %d is %q (found %v), every unidentified herb is an Unidentified herb", h.Name, h.Unid, u.Name, ok)
		}
		if cachecfg.OpIndex(actions.HeldOps(u.IOps), "Identify") != 1 {
			t.Errorf("herb %s: obj %d (%s) offers %v, want Identify as its first op", h.Name, h.Unid, u.Name, u.IOps)
		}
		c, ok := cfg.Obj(h.Clean)
		if !ok || !strings.EqualFold(strings.ReplaceAll(c.Name, "'", ""), strings.ReplaceAll(h.Name, "'", "")) {
			t.Errorf("herb %s: obj %d is %q (found %v)", h.Name, h.Clean, c.Name, ok)
		}
	}
	for id, c := range map[int]struct {
		name  string
		level int
	}{
		questplan.NpcKaqemeex: {"Kaqemeex", 0}, questplan.NpcSanfew: {"Sanfew", 0}, questplan.NpcTrufitus: {"Trufitus", 0},
		questplan.NpcSuitOfArmour: {"Suit of armour", 19}, questplan.NpcBrownBear: {"Bear", 21}, questplan.NpcGiantRat: {"Giant rat", 6},
		questplan.NpcGiantRatCub: {"Giant rat", 3}, questplan.NpcChicken: {"Chicken", 1}, questplan.NpcDairyCow: {"Cow", 2},
		questplan.NpcJogre: {"Jogre", 53}, questplan.NpcJungleSpider: {"Jungle spider", 44}, questplan.NpcTribesman: {"Tribesman", 32},
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != c.name || (c.level > 0 && n.Level != c.level) {
			t.Errorf("npc %d is %q level %d (found %v), the plan says %q level %d", id, n.Name, n.Level, ok, c.name, c.level)
		}
	}
	if n, _ := cfg.Npc(questplan.NpcJogre); n.Size != 2 {
		t.Errorf("the jogre is size %d: the avoidance and the price assume the 2x2 footprint the content gives it", n.Size)
	}
	for _, c := range []struct {
		id       int
		name, op string
		opIdx    int
	}{
		{questplan.LocCauldronOfThunder, "Cauldron of Thunder", "", 0}, {questplan.LocCauldronDoor, "Prison door", "Open", 1},
		{questplan.LocCauldronDoorLeft, "Prison door", "Open", 1}, {questplan.LocSuitOfArmourLoc, "Suit of armour", "", 0},
		{questplan.JungleHerbs[0].Loc, "Marshy jungle vine", "Search", 2}, {questplan.JungleHerbs[1].Loc, "Palm tree", "Search", 2},
		{questplan.JungleHerbs[2].Loc, "Scorched earth", "Search", 2}, {questplan.JungleHerbs[3].Loc, "Rock", "Search", 2},
		{questplan.JungleHerbs[4].Loc, "Fungus covered Cavern wall", "Search", 2}, {questplan.LocJunglePothole, "Rocks", "Search", 2},
		{questplan.LocJungleHandHolds, "Hand holds", "Climb", 1}, {1759, "Ladder", "Climb-down", 1}, {1755, "Ladder", "Climb-up", 1},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name {
			t.Errorf("loc %d is %q (found %v), the plan says %q", c.id, l.Name, ok, c.name)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) != c.opIdx {
			t.Errorf("loc %d (%s) offers %v, want %q as op %d", c.id, l.Name, l.Ops, c.op, c.opIdx)
		}
	}
}

// The customs' own loc queries find their locs where the plans say, and each has a tile the engine accepts an op from.
func TestDruidJungleLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	type want struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile // tiles the plan relies on, each of which must be among the accepted stands
	}
	cs := []want{
		{"the cauldron", questplan.LocCauldronOfThunder, "", questplan.CauldronStand, 6, 2893, 9831, 0, []tile.Tile{questplan.CauldronStand}},
		{"the Prison door", questplan.LocCauldronDoor, "Open", questplan.CauldronDoorOutside, 3, 2889, 9831, 0, []tile.Tile{questplan.CauldronDoorOutside}},
		{"the Prison door's left leaf", questplan.LocCauldronDoorLeft, "Open", questplan.CauldronDoorOutside, 3, 2889, 9830, 0, nil},
		{"the north suit of armour", questplan.LocSuitOfArmourLoc, "", questplan.CauldronDoorOutside, 8, questplan.SuitNorth.X, questplan.SuitNorth.Z, 0, nil},
		{"the surface ladder", 1759, "Climb-down", questplan.DruidDungeonSurface, 3, 2884, 3397, 0, []tile.Tile{questplan.DruidDungeonSurface}},
		{"the dungeon's ladder up", 1755, "Climb-up", questplan.DruidDungeonFoot, 3, 2884, 9797, 0, []tile.Tile{questplan.DruidDungeonFoot}},
		{"the Rocks", questplan.LocJunglePothole, "Search", questplan.JunglePotholeStand, 6, 2824, 3118, 0, []tile.Tile{questplan.JunglePotholeStand}},
		{"the Hand holds", questplan.LocJungleHandHolds, "Climb", questplan.JungleHandHoldsStand, 3, 2830, 9522, 0, []tile.Tile{questplan.JungleHandHoldsStand}},
	}
	for _, h := range questplan.JungleHerbs {
		cs = append(cs, want{"the " + h.Name + " loc", h.Loc, "Search", h.At, 3, h.At.X, h.At.Z, h.At.Level, []tile.Tile{h.Stand}})
	}
	for _, c := range cs {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d found near %d,%d,%d", c.what, c.id, c.near.X, c.near.Z, c.near.Level)
			continue
		}
		if m.X != c.x || m.Z != c.z || m.Level != c.lvl {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		stands := questrunner.LocStands(d, m, c.near)
		if len(stands) == 0 {
			t.Errorf("%s at %d,%d,%d has no tile the engine accepts an op from", c.what, m.X, m.Z, m.Level)
			continue
		}
		walkable := 0
		for _, s := range stands {
			if d.Pack.Walkable(s.X, s.Z, s.Level) {
				walkable++
			}
		}
		if walkable == 0 {
			t.Errorf("%s: none of the %d stands is walkable: %v", c.what, len(stands), stands)
		}
		for _, s := range c.stands {
			found := false
			for _, got := range stands {
				if got == s {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: %v is not among the stands %v", c.what, s, stands)
			}
		}
	}
	// The door's suits stand where prison_doors.rs2 says (0_45_153_7_40 and 0_45_153_7_37): the loc index holds both.
	for _, at := range []tile.Tile{questplan.SuitNorth, questplan.SuitSouth} {
		m, ok := questrunner.FindLoc(d, questplan.LocSuitOfArmourLoc, "", "", at, 1)
		if !ok || m.X != at.X || m.Z != at.Z {
			t.Errorf("no suit of armour loc at %v (found %+v %v)", at, m, ok)
		}
	}
}

// Every leg the two quests walk plans on the real pack and graph.
func TestDruidJungleLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.FaladorWestBank
	draynor := questplan.DraynorBank
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"bank to Kaqemeex", bank, questplan.KaqemeexAnchor}, {"bank to Sanfew", bank, questplan.SanfewAnchor},
		{"Sanfew to Kaqemeex", questplan.SanfewAnchor, questplan.KaqemeexAnchor}, {"Kaqemeex to Sanfew", questplan.KaqemeexAnchor, questplan.SanfewAnchor},
		{"bank to the dungeon's ladder", bank, questplan.DruidDungeonSurface}, {"bank to the cauldron's door", bank, questplan.CauldronDoorOutside},
		{"the dungeon's foot to the cauldron's door", questplan.DruidDungeonFoot, questplan.CauldronDoorOutside},
		{"the cauldron's door to the room", questplan.CauldronDoorOutside, questplan.CauldronStand},
		{"the room to the door", questplan.CauldronStand, questplan.CauldronDoorOutside},
		{"the room to Sanfew", questplan.CauldronStand, questplan.SanfewAnchor}, {"the door to the ladder", questplan.CauldronDoorOutside, questplan.DruidDungeonSurface},
		{"Wydin to the bear", questplan.WydinShop.Anchor, questplan.DruidMeats[0].Field}, {"Draynor to the bear", draynor, questplan.DruidMeats[0].Field},
		{"bank to Wydin", bank, questplan.WydinShop.Anchor}, {"Wydin to Draynor", questplan.WydinShop.Anchor, draynor},
	}
	for i, m := range questplan.DruidMeats {
		legs = append(legs, struct {
			name     string
			from, to tile.Tile
		}{"bank to the " + m.Name + " field", bank, m.Field}, struct {
			name     string
			from, to tile.Tile
		}{"the " + m.Name + " field to the Falador bank", m.Field, bank})
		if i > 0 {
			legs = append(legs, struct {
				name     string
				from, to tile.Tile
			}{"the " + questplan.DruidMeats[i-1].Name + " field to the " + m.Name + " field", questplan.DruidMeats[i-1].Field, m.Field})
		}
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-46s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
}

// The cauldron's room is the region of the pack: every tile the planner reaches on foot from the cauldron is in InCauldronRoom (the
// door's own column, x 2889, included: the door script lands the account on the threshold) and nothing else is.
func TestDruidTheCauldronRoomIsTheRegionOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.CauldronStand.X, Z: questplan.CauldronStand.Z}, 12, nil)
	n := 0
	reach.Each(func(x, z, level, steps int) bool {
		n++
		if at := (tile.Tile{X: x, Z: z, Level: level}); !questplan.InCauldronRoom(at) {
			t.Errorf("%v is reached on foot from the cauldron and is outside InCauldronRoom", at)
		}
		return true
	})
	if n != 55 {
		t.Errorf("the cauldron reaches %d tiles: the pack no longer says 55", n)
	}
	// The rectangle is the room's bounding box: its corners hold floor the cauldron does not reach (other rooms' corners), which no
	// walk of the quest stands on. The way in from the ladder is none of it.
	p, pt := mcPlanner(t)
	r := p.Plan(pt(questplan.DruidDungeonFoot), pt(questplan.CauldronDoorOutside), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !r.OK {
		t.Fatalf("the way from the ladder to the door: %s", r.Reason)
	}
	for _, c := range tilesOnRoute(r, 0) {
		if questplan.InCauldronRoom(c) {
			t.Errorf("%v, on the way from the ladder to the door, is in InCauldronRoom", c)
		}
		if !questplan.InTaverleyDungeon(c) {
			t.Errorf("%v, on the way from the ladder to the door, is not in InTaverleyDungeon", c)
		}
	}
	// The corridor outside is not the room, and the threshold is.
	for _, at := range []tile.Tile{questplan.CauldronDoorOutside, questplan.DruidDungeonFoot, questplan.SuitNorth, questplan.SuitSouth} {
		if questplan.InCauldronRoom(at) {
			t.Errorf("%v (the corridor) is in InCauldronRoom", at)
		}
		if !questplan.InTaverleyDungeon(at) {
			t.Errorf("%v is not in InTaverleyDungeon", at)
		}
	}
	for _, at := range []tile.Tile{questplan.CauldronStand, questplan.CauldronDoorInside, questplan.CauldronDoorThreshold} {
		if !questplan.InCauldronRoom(at) {
			t.Errorf("%v is outside InCauldronRoom", at)
		}
	}
	// The threshold is the door's own tile, a stand the door is opened from, and the corridor tile is the other.
	d := csDeps(t)
	m, ok := questrunner.FindLoc(d, questplan.LocCauldronDoor, "", "Open", questplan.CauldronDoorOutside, 3)
	if !ok || m.X != questplan.CauldronDoorThreshold.X || m.Z != questplan.CauldronDoorThreshold.Z {
		t.Fatalf("the door leaf is %+v (%v)", m, ok)
	}
	stands := questrunner.LocStands(d, m, questplan.CauldronDoorOutside)
	if !(len(stands) == 2 && stands[0] == questplan.CauldronDoorOutside && stands[1] == questplan.CauldronDoorThreshold) {
		t.Errorf("the door is opened from %v, the plan says the corridor tile and the threshold", stands)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The jungle
// ---------------------------------------------------------------------------------------------------------------------

// jungleTargets are every tile a walk on the island aims at.
func jungleTargets() map[string]tile.Tile {
	out := map[string]tile.Tile{"Trufitus": questplan.TrufitusAnchor, "the Rocks": questplan.JunglePotholeStand}
	for _, h := range questplan.JungleHerbs {
		if !h.Underground {
			out[h.Name] = h.Stand
		}
	}
	return out
}

// None of the tiles a walk aims at is inside a rectangle the walks keep out of, and the ground a walk starts on (the ship's
// landing) is not either: the planner never steps into a zone from outside, so a destination inside one is unreachable.
func TestJungleTheWalkTargetsLieOutsideTheAvoidedGround(t *testing.T) {
	rects := jungleAvoidRects()
	starts := jungleTargets()
	starts["the ship's gangplank at Musa Point"] = tile.Tile{X: 2954, Z: 3147}
	for name, at := range starts {
		for _, r := range rects {
			if r.Contains(webwalk.Point{X: at.X, Z: at.Z, Level: at.Level}) {
				t.Errorf("%s (%v) lies inside the avoided rectangle %+v", name, at, r)
			}
		}
	}
	if want := len(questplan.JungleHostiles()); len(rects) != want || want < 40 {
		t.Errorf("%d avoided rectangles for %d hostiles", len(rects), want)
	}
}

// Every walk the quest makes on the island exists round the hunters: from the ship, from Port Sarim, from Trufitus to each stand
// and back, from stand to stand. This is the check that the plan's "avoid the spiders and tribesmen" is a route and not a wish.
func TestJungleEveryWalkExistsRoundTheHunters(t *testing.T) {
	p, pt := mcPlanner(t)
	zones := jungleAvoidRects()
	targets := jungleTargets()
	port := tile.Tile{X: 3029, Z: 3217}
	musa := tile.Tile{X: 2954, Z: 3147}
	plan := func(from, to tile.Tile) webwalk.Route {
		return p.Plan(pt(from), pt(to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
	}
	for name, to := range targets {
		for _, from := range []struct {
			n string
			t tile.Tile
		}{{"Port Sarim", port}, {"Musa Point", musa}, {"Trufitus", questplan.TrufitusAnchor}} {
			if from.t == to {
				continue
			}
			r := plan(from.t, to)
			if !r.OK {
				t.Errorf("%s to %s: no route round the hunters (%s)", from.n, name, r.Reason)
				continue
			}
			t.Logf("%-12s to %-14s cost %4d", from.n, name, r.Cost)
		}
		if name != "Trufitus" {
			if r := plan(to, questplan.TrufitusAnchor); !r.OK {
				t.Errorf("%s back to Trufitus: no route round the hunters (%s)", name, r.Reason)
			}
		}
	}
	// And the plain walks cross the hunters' ground, which is why there are rectangles at all: Trufitus to the vine passes a
	// tribesman's spawn.
	plain := p.Plan(pt(questplan.TrufitusAnchor), pt(questplan.JungleHerbs[0].Stand), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	avoided := plan(questplan.TrufitusAnchor, questplan.JungleHerbs[0].Stand)
	if !plain.OK || !avoided.OK || avoided.Cost <= plain.Cost {
		t.Errorf("the vine's plain route costs %d and the avoiding one %d (%v %v): the swamp's hunters are no longer on the plain way", plain.Cost, avoided.Cost, plain.OK, avoided.OK)
	}
}

// reachRect is the bounding box of the ground a flood from at reaches.
func reachRect(pk *pathfind.Pack, at tile.Tile, radius int) (minX, maxX, minZ, maxZ, n int) {
	minX, minZ = 1<<30, 1<<30
	pk.ReachFrom(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}, radius, nil).Each(func(x, z, level, steps int) bool {
		n++
		minX, maxX, minZ, maxZ = min(minX, x), max(maxX, x), min(minZ, z), max(maxZ, z)
		return true
	})
	return
}

// The cave is the region of the pack: everything the planner reaches on foot from where the Rocks set the account down lies inside
// InJungleCave, the rogues purse and the Hand holds are in it, and no walk from the surface reaches it.
func TestJungleTheCaveIsAPocketOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	p, pt := mcPlanner(t)
	minX, maxX, minZ, maxZ, n := reachRect(pk, questplan.JungleCaveArrival, 70)
	t.Logf("the cave reaches %d tiles, x %d-%d z %d-%d", n, minX, maxX, minZ, maxZ)
	if n < 100 {
		t.Fatalf("the cave reaches only %d tiles from %v", n, questplan.JungleCaveArrival)
	}
	for _, at := range []tile.Tile{{X: minX, Z: minZ}, {X: maxX, Z: maxZ}, {X: minX, Z: maxZ}, {X: maxX, Z: minZ}, questplan.JungleCaveArrival,
		questplan.JungleHandHoldsStand, questplan.JungleHerbs[4].Stand} {
		if !questplan.InJungleCave(at) {
			t.Errorf("%v is in the cave and outside InJungleCave", at)
		}
	}
	// The whole flood is inside the rectangle.
	pk.ReachFrom(pathfind.NavPoint{X: questplan.JungleCaveArrival.X, Z: questplan.JungleCaveArrival.Z}, 70, nil).Each(func(x, z, level, steps int) bool {
		if !questplan.InJungleCave(tile.Tile{X: x, Z: z, Level: level}) {
			t.Errorf("%d,%d is reached on foot from the cave's foot and is outside InJungleCave", x, z)
		}
		return true
	})
	// The wall and the Hand holds are one flood from the foot, so the run exists.
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.JungleCaveArrival.X, Z: questplan.JungleCaveArrival.Z}, 70, nil)
	for _, at := range []tile.Tile{questplan.JungleHerbs[4].Stand, questplan.JungleHandHoldsStand} {
		if !reach.CanReach(pathfind.NavPoint{X: at.X, Z: at.Z}) {
			t.Errorf("the cave's foot does not reach %v on foot", at)
		}
	}
	// The Rocks and the Hand holds set the account down on ground that exists (p_telejump onto an unwalkable tile would strand it), and
	// from the landing above the way on is a route round the hunters.
	for _, at := range []tile.Tile{questplan.JungleCaveArrival, questplan.JungleCaveSurface} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("the landing %v is not walkable", at)
		}
	}
	if r := p.Plan(pt(questplan.JungleCaveSurface), pt(questplan.TrufitusAnchor), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: jungleAvoidRects()}); !r.OK {
		t.Errorf("the Hand holds' landing to Trufitus: no route (%s)", r.Reason)
	}
	// No way out the planner finds, and none in: the pocket is real.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the cave's foot to the Falador bank", questplan.JungleCaveArrival, questplan.FaladorWestBank},
		{"Trufitus to the cave's foot", questplan.TrufitusAnchor, questplan.JungleCaveArrival},
		{"the Rocks to the wall", questplan.JunglePotholeStand, questplan.JungleHerbs[4].Stand},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): the cave is not a pocket", l.name, r.Cost)
		}
	}
}

// spawnsOf reads the NPC rows of the content maps over the box (RSB0T_CONTENT; the test skips without it).
func spawnsOf(t *testing.T, x0, x1, z0, z1 int) map[int][]tile.Tile {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to read the npc spawns")
	}
	out := map[int][]tile.Tile{}
	for mx := x0 >> 6; mx <= x1>>6; mx++ {
		for mz := z0 >> 6; mz <= z1>>6; mz++ {
			b, err := os.ReadFile(filepath.Join(root, "maps", fmt.Sprintf("m%d_%d.jm2", mx, mz)))
			if err != nil {
				continue
			}
			in := false
			for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
				if strings.HasPrefix(line, "==== ") {
					in = line == "==== NPC ===="
					continue
				}
				if !in {
					continue
				}
				var lvl, lx, lz, id int
				if n, _ := fmt.Sscanf(line, "%d %d %d: %d", &lvl, &lx, &lz, &id); n == 4 {
					out[id] = append(out[id], tile.Tile{X: mx*64 + lx, Z: mz*64 + lz, Level: lvl})
				}
			}
		}
	}
	return out
}

// tilesOnLine is every tile on the straight stretches between a route's waypoints, on one level.
func tilesOnRoute(r webwalk.Route, level int) []tile.Tile {
	var out []tile.Tile
	for i := 1; i < len(r.Waypoints); i++ {
		a, b := r.Waypoints[i-1].Point, r.Waypoints[i].Point
		if a.Level != level || b.Level != level {
			continue
		}
		dx, dz := b.X-a.X, b.Z-a.Z
		steps := max(absInt(dx), absInt(dz))
		for k := 0; k <= steps; k++ {
			x, z := a.X, a.Z
			if steps > 0 {
				x += dx * k / steps
				z += dz * k / steps
			}
			out = append(out, tile.Tile{X: x, Z: z, Level: level})
		}
	}
	return out
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// The run to the rogues purse and back meets the jogres the price counts: the planned way from the Hand holds to the wall and
// back passes within two tiles of four jogre spawns (JungleCaveJogres) and within four of six, and the cave holds twelve.
func TestJungleCaveRouteMeetsTheJogres(t *testing.T) {
	p, pt := mcPlanner(t)
	jogres := spawnsOf(t, 2800, 2880, 9440, 9540)[questplan.NpcJogre]
	if len(jogres) != 12 {
		t.Fatalf("the cave's squares hold %d jogres, the plan's comments and price say twelve", len(jogres))
	}
	within := func(route []tile.Tile, d int) int {
		n := 0
		for _, j := range jogres {
			for _, c := range route {
				if max(absInt(c.X-j.X), absInt(c.Z-j.Z)) <= d {
					n++
					break
				}
			}
		}
		return n
	}
	in := p.Plan(pt(questplan.JungleCaveArrival), pt(questplan.JungleHerbs[4].Stand), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	out := p.Plan(pt(questplan.JungleHerbs[4].Stand), pt(questplan.JungleHandHoldsStand), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !in.OK || !out.OK {
		t.Fatalf("the cave's run: in %v (%s), out %v (%s)", in.OK, in.Reason, out.OK, out.Reason)
	}
	both := append(tilesOnRoute(in, 0), tilesOnRoute(out, 0)...)
	two, four := within(both, 2), within(both, 4)
	t.Logf("the run (%d tiles in, %d out) passes within 2 tiles of %d jogres and within 4 of %d", len(tilesOnRoute(in, 0)), len(tilesOnRoute(out, 0)), two, four)
	if two < questplan.JungleCaveJogres || four < two || four > 8 {
		t.Errorf("the run passes %d jogres within 2 tiles and %d within 4: the price assumes %d engaged (and the plan says six within 4)", two, four, questplan.JungleCaveJogres)
	}
	// The run is short enough for the price's swings: a hundred tiles at most, in and out.
	if len(both) > 130 {
		t.Errorf("the cave's run is %d tiles", len(both))
	}
}

// The planned way down the Taverley dungeon's corridor and back (the planner's own line, from the ladder's foot to the tile the door is
// opened from) passes within DruidCorridorReach tiles of exactly the unarmed skeletons the price counts (DruidCorridorSkeletons), four
// of them on their spawn tiles; the sixth stands three tiles off it and the armed one five from the door, and the poison spiders no
// nearer than nine, which is the plan's reason for leaving each out.
func TestDruidDungeonRouteMeetsTheSkeletons(t *testing.T) {
	p, pt := mcPlanner(t)
	sp := spawnsOf(t, 2860, 2910, 9780, 9850)
	in := p.Plan(pt(questplan.DruidDungeonFoot), pt(questplan.CauldronDoorOutside), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	out := p.Plan(pt(questplan.CauldronDoorOutside), pt(questplan.DruidDungeonFoot), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !in.OK || !out.OK {
		t.Fatalf("the corridor's route: in %v (%s), out %v (%s)", in.OK, in.Reason, out.OK, out.Reason)
	}
	// closest is how near the line comes to a spawn, in a direction.
	closest := func(route []tile.Tile, at tile.Tile) int {
		best := 999
		for _, c := range route {
			best = min(best, max(absInt(c.X-at.X), absInt(c.Z-at.Z)))
		}
		return best
	}
	ri, ro := tilesOnRoute(in, 0), tilesOnRoute(out, 0)
	if len(ri) == 0 || len(ro) == 0 || len(ri) > 60 || len(ro) > 60 {
		t.Fatalf("the corridor's line is %d tiles in and %d out", len(ri), len(ro))
	}
	priced := map[tile.Tile]bool{}
	for _, at := range questplan.DruidCorridorSkeletons {
		priced[at] = true
	}
	engaged := 0
	for _, at := range sp[questplan.NpcManorSkeleton] {
		if at.Level != 0 {
			continue
		}
		cin, cout := closest(ri, at), closest(ro, at)
		t.Logf("unarmed skeleton at %v: the line passes %d tiles off going in and %d coming out", at, cin, cout)
		if near := cin <= questplan.DruidCorridorReach && cout <= questplan.DruidCorridorReach; near != priced[at] {
			t.Errorf("the unarmed skeleton at %v is %d tiles off the line (in) and %d (out): priced %v, within reach %v", at, cin, cout, priced[at], near)
		}
		if priced[at] {
			engaged++
		}
	}
	if engaged != len(questplan.DruidCorridorSkeletons) {
		t.Errorf("the map has %d of the %d priced skeletons", engaged, len(questplan.DruidCorridorSkeletons))
	}
	// The ones left out: the sixth unarmed skeleton three tiles off the line, the armed one five from it.
	if c := closest(ri, tile.Tile{X: 2882, Z: 9826}); c != 3 {
		t.Errorf("the sixth unarmed skeleton is %d tiles off the line, the plan says three", c)
	}
	if armed := sp[92]; len(armed) != 1 || closest(ri, armed[0]) != 5 {
		t.Errorf("the armed skeleton %v is %d tiles off the line, the plan says five", armed, closest(ri, tile.Tile{X: 2884, Z: 9836}))
	}
	for _, at := range sp[134] {
		if at.Level == 0 && at.X >= 2860 && closest(ri, at) < 9 {
			t.Errorf("a poison spider at %v is %d tiles off the line, the plan says no nearer than nine", at, closest(ri, at))
		}
	}
	// The hall is nine tiles wide (x 2880 to 2888) between the skeletons' spawns, so there is room to bend the walk round them and the
	// plan's reason for not doing it stands: the west wall is four tiles from the nearest poison spider's spawn (2876,9806).
	pk, _, _ := csData(t)
	for z := 9810; z <= 9826; z += 4 {
		for x := 2879; x <= 2889; x++ {
			if want := x >= 2880 && x <= 2888; pk.Walkable(x, z, 0) != want {
				t.Errorf("the hall at %d,%d is walkable %v, the plan's width of nine says %v", x, z, !want, want)
			}
		}
	}
	if !slices.Contains(sp[134], tile.Tile{X: 2876, Z: 9806}) {
		t.Error("no poison spider at 2876,9806")
	}
}
