package questmods

// The Elemental Workshop on the real data: the production config, the baked loc index, the collision pack and the web graph
// (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan
// carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where the plan
// says and each has a tile to stand on in the piece of ground it is used from, whether every leg the quest walks is a route the planner
// finds on the real pack, whether the smithy's wall and the stairs are the crossings the planner cannot find (so that the pocket is real),
// and whether the pocket's rectangles are the ground the pack makes them.

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestElementalWorkshopIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjEwBook: "Battered book", questplan.ObjEwKey: "Battered key", questplan.ObjEwBowl: "A stone bowl", questplan.ObjEwBowlFull: "A stone bowl",
		questplan.ObjEwShield: "Elemental shield", questplan.ObjEwOre: "Elemental ore", questplan.ObjEwBar: "Elemental metal", questplan.ObjEwNeedle: "Needle",
		questplan.ObjEwThread: "Thread", questplan.ObjEwLeather: "Leather", questplan.ObjEwCoal: "Coal", questplan.ObjHammer: "Hammer", questplan.ObjKnife: "Knife",
		questplan.ObjShark: "Shark", questplan.ObjBronzePickaxe: "Bronze pickaxe", 1267: "Iron pickaxe", 1269: "Steel pickaxe", 1271: "Adamant pickaxe",
		1273: "Mithril pickaxe", 1275: "Rune pickaxe",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	if o, _ := cfg.Obj(questplan.ObjEwBook); cachecfg.OpIndex(o.IOps, "Read") != 1 {
		t.Errorf("the book offers %v: Read is expected as the first op", o.IOps)
	}
	for _, id := range []int{questplan.ObjEwNeedle, questplan.ObjEwThread} {
		if o, _ := cfg.Obj(id); !o.Stackable {
			t.Errorf("obj %d is not stackable: the kit line says it is", id)
		}
	}
	for _, id := range []int{questplan.ObjEwCoal, questplan.ObjEwLeather, questplan.ObjHammer, questplan.ObjEwBowl, questplan.ObjEwOre} {
		if o, _ := cfg.Obj(id); o.Stackable {
			t.Errorf("obj %d is stackable: the plan counts its slots one by one", id)
		}
	}
	// The rock's elemental and the standing ones: attackable, size 1, level 35.
	for _, id := range []int{questplan.NpcEwEarthRock, 1020} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != "Earth elemental" || cachecfg.OpIndex(n.Ops, "Attack") == 0 || n.Level != 35 || n.Size != 1 {
			t.Errorf("npc %d is %q level %d size %d offering %v", id, n.Name, n.Level, n.Size, n.Ops)
		}
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocEwBookcase, "Bookcase", "Search", 1, 2}, {questplan.LocEwWallRight, "Odd looking wall", "Push", 1, 1}, {questplan.LocEwWallLeft, "Odd looking wall", "Push", 1, 1},
		{questplan.LocEwStairsDown, "Staircase", "Climb-down", 2, 2}, {questplan.LocEwStairsUp, "Staircase", "Climb-up", 2, 2},
		{questplan.LocEwBoxBowl, "Crate", "Search", 1, 1}, {questplan.LocEwBoxNeedle, "Crate", "Search", 1, 1}, {questplan.LocEwBoxLeather, "Boxes", "Search", 1, 1},
		{questplan.LocEwWorkbench, "Workbench", "", 3, 1}, {questplan.LocEwRock, "Pile of Rock", "Mine", 1, 1}, {questplan.LocEwValveFirst, "Water Valve", "Turn", 1, 1},
		{questplan.LocEwValveSecond, "Water Valve", "Turn", 1, 1}, {questplan.LocEwWaterLever, "Lever", "Pull", 1, 1}, {questplan.LocEwAirLever, "Lever", "Pull", 1, 1},
		{questplan.LocEwBellowsL, "Bellows", "Fix", 2, 5}, {questplan.LocEwBellowsR, "Bellows", "Fix", 2, 5}, {questplan.LocEwFurnace, "Furnace", "", 3, 3},
		{questplan.LocEwTrough, "Lava trough", "", 1, 5},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || l.Width != c.w || l.Length != c.l {
			t.Errorf("loc %d is %q %dx%d (found %v), the plan says %q %dx%d", c.id, l.Name, l.Width, l.Length, ok, c.name, c.w, c.l)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) != 1 {
			t.Errorf("loc %d (%s) offers %v, want %q first", c.id, l.Name, l.Ops, c.op)
		}
	}
	// The lever after a pull is another loc with no op (the customs wait for the Pull form).
	if l, _ := cfg.Loc(3417); l.Name != "Lever" || cachecfg.OpIndex(l.Ops, "Pull") != 0 {
		t.Errorf("the pulled lever is %q offering %v", l.Name, l.Ops)
	}
	// The wall's open forms offer nothing.
	for _, id := range []int{3392, 3393} {
		if l, _ := cfg.Loc(id); l.Name != "Odd looking wall" || cachecfg.OpIndex(l.Ops, "Push") != 0 {
			t.Errorf("the open wall %d is %q offering %v", id, l.Name, l.Ops)
		}
	}
}

// ewLocCase is one loc query of the customs and the stands it must have.
type ewLocCase struct {
	what      string
	id        int
	op        string
	near      tile.Tile
	radius    int
	x, z, lvl int
	stands    []tile.Tile
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestElementalWorkshopLocQueriesFindTheirLocsAndStands(t *testing.T) {
	d := csDeps(t)
	pk := d.Pack
	// The workshop's ground: what is reached on foot from the stairs' landing.
	ground := pk.Flood(pathfind.NavPoint{X: questplan.EwArrive.X, Z: questplan.EwArrive.Z, Level: 0}, 80)
	cs := []ewLocCase{
		{"the bookcase", questplan.LocEwBookcase, "Search", questplan.EwBookcaseAt, 3, 2716, 3481, 0, []tile.Tile{questplan.EwBookcaseStand}},
		{"the wall's east half", questplan.LocEwWallRight, "Push", questplan.EwWallAt, 3, 2710, 3495, 0, []tile.Tile{questplan.EwWallOutside, questplan.EwWallInside}},
		{"the wall's west half", questplan.LocEwWallLeft, "Push", questplan.EwWallAt, 3, 2709, 3495, 0, []tile.Tile{{X: 2709, Z: 3495}, {X: 2709, Z: 3496}}},
		{"the stairs down", questplan.LocEwStairsDown, "Climb-down", questplan.EwStairsDownAt, 3, 2710, 3497, 0, []tile.Tile{questplan.EwStairsDownStand}},
		{"the stairs up", questplan.LocEwStairsUp, "Climb-up", questplan.EwStairsUpAt, 3, 2714, 9887, 0, []tile.Tile{questplan.EwArrive}},
		{"the bowl's crate", questplan.LocEwBoxBowl, "Search", questplan.EwBoxBowlAt, 3, 2717, 9894, 0, []tile.Tile{{X: 2717, Z: 9893}}},
		{"the needle's crate", questplan.LocEwBoxNeedle, "Search", questplan.EwBoxNeedleAt, 3, 2700, 9887, 0, []tile.Tile{{X: 2701, Z: 9887}}},
		{"the leather's boxes", questplan.LocEwBoxLeather, "Search", questplan.EwBoxLeatherAt, 3, 2724, 9894, 0, []tile.Tile{{X: 2724, Z: 9893}}},
		{"the first valve", questplan.LocEwValveFirst, "Turn", questplan.EwValveFirstAt, 3, 2726, 9908, 0, []tile.Tile{{X: 2726, Z: 9907}}},
		{"the second valve", questplan.LocEwValveSecond, "Turn", questplan.EwValveSecondAt, 3, 2713, 9908, 0, []tile.Tile{{X: 2713, Z: 9907}}},
		{"the water lever", questplan.LocEwWaterLever, "Pull", questplan.EwWaterLeverAt, 3, 2722, 9906, 0, []tile.Tile{{X: 2722, Z: 9905}}},
		{"the air lever", questplan.LocEwAirLever, "Pull", questplan.EwAirLeverAt, 3, 2734, 9887, 0, []tile.Tile{{X: 2733, Z: 9887}}},
		{"the bellows", questplan.LocEwBellowsL, "Fix", questplan.EwBellowsAt, 3, 2734, 9882, 0, []tile.Tile{{X: 2733, Z: 9882}}},
		{"the furnace", questplan.LocEwFurnace, "", questplan.EwFurnaceAt, 4, 2724, 9874, 0, []tile.Tile{{X: 2723, Z: 9875}}},
		{"the trough", questplan.LocEwTrough, "", questplan.EwTroughAt, 4, 2714, 9871, 0, []tile.Tile{{X: 2714, Z: 9872}}},
		{"the workbench", questplan.LocEwWorkbench, "", questplan.EwWorkbenchAt, 8, 2717, 9887, 0, []tile.Tile{{X: 2716, Z: 9888}}},
		{"the rock", questplan.LocEwRock, "Mine", questplan.EwRockAt, 4, 2706, 9904, 0, []tile.Tile{{X: 2705, Z: 9904}, {X: 2707, Z: 9904}}},
	}
	for _, c := range cs {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d found near %v", c.what, c.id, c.near)
			continue
		}
		if m.X != c.x || m.Z != c.z || m.Level != c.lvl {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		stands := questrunner.LocStands(d, m, c.near)
		if len(stands) == 0 {
			t.Errorf("%s: no stand", c.what)
		}
		for _, want := range c.stands {
			found := false
			for _, got := range stands {
				found = found || got == want
			}
			if !found {
				t.Errorf("%s: %v is not among the stands %v", c.what, want, stands)
			}
			if !pk.Walkable(want.X, want.Z, want.Level) {
				t.Errorf("%s: the plan's stand %v is not walkable", c.what, want)
			}
		}
		// A workshop loc has a stand on the workshop's ground (the only ground the account is on when it uses it); the outside ones do not.
		if questplan.EwRegionOf(tile.Tile{X: m.X, Z: m.Z}) == questplan.EwWorkshop {
			onGround := false
			for _, s := range stands {
				onGround = onGround || ground[[2]int{s.X, s.Z}]
			}
			if !onGround {
				t.Errorf("%s: no stand of %v is on the ground reached from the stairs' landing", c.what, stands)
			}
		}
	}
	// The wall opens from outside to the key alone: the plan stands on the wall's own row (the engine's check_axis_locactive), and the
	// secret room's tile is where it lands the account.
	if questplan.EwWallOutside.Z != 3495 || questplan.EwWallInside.Z != 3496 {
		t.Errorf("the wall's outside %v and inside %v", questplan.EwWallOutside, questplan.EwWallInside)
	}
	// The rock's neighbours: the 28 locs are Pile of Rock, one id, so the nearest to EwRockAt is the plan's own.
	rock, _ := questrunner.FindLoc(d, questplan.LocEwRock, "", "Mine", questplan.EwRockAt, 4)
	if rock.X != questplan.EwRockAt.X || rock.Z != questplan.EwRockAt.Z {
		t.Errorf("the nearest rock to the plan's is at %d,%d", rock.X, rock.Z)
	}
	// Both trough locs are Lava trough; the nearest to EwTroughAt is its own. Both workbenches are the same id.
	for _, id := range []int{questplan.LocEwTrough, questplan.LocEwWorkbench} {
		if ms := d.Locs.Find(d.Config, locindex.Query{X: questplan.EwArrive.X, Z: questplan.EwArrive.Z, Level: 0, Radius: 40, IDs: []int{id}}, nil); len(ms) != 2 {
			t.Errorf("loc %d: %d placed in the workshop, want 2", id, len(ms))
		}
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestElementalWorkshopLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	opts := webwalk.Options{AllowSpecial: true, AllowNPC: true}
	bank := questplan.SeersBank
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// Seers' Village.
		{"the bank to the bookcase", bank, questplan.EwBookcaseStand}, {"the bookcase to the bank", questplan.EwBookcaseStand, bank},
		{"the bookcase to the smithy's wall", questplan.EwBookcaseStand, questplan.EwWallOutside}, {"the bank to the smithy's wall", bank, questplan.EwWallOutside},
		{"the wall to the bank", questplan.EwWallOutside, bank},
		// The shops (the hammer and a pickaxe, the thread).
		{"the bank to Arhein's store", bank, questplan.ArheinStore.Anchor}, {"Arhein's store to the bank", questplan.ArheinStore.Anchor, bank},
		{"the bank to Dommik's store", bank, questplan.DommikShop.Anchor}, {"Dommik's store to the bank", questplan.DommikShop.Anchor, bank},
		{"Dommik's store to the wall", questplan.DommikShop.Anchor, questplan.EwWallOutside},
		// The secret room.
		{"the stairs' stand to the wall", questplan.EwStairsDownStand, questplan.EwWallInside}, {"the wall to the stairs' stand", questplan.EwWallInside, questplan.EwStairsDownStand},
		// The workshop, from the stairs' landing round the machine and back.
		{"the arrival to the first valve", questplan.EwArrive, tile.Tile{X: 2726, Z: 9907}}, {"the first valve to the second", tile.Tile{X: 2726, Z: 9907}, tile.Tile{X: 2713, Z: 9907}},
		{"the second valve to the lever", tile.Tile{X: 2713, Z: 9907}, tile.Tile{X: 2722, Z: 9905}}, {"the lever to the leather's boxes", tile.Tile{X: 2722, Z: 9905}, tile.Tile{X: 2724, Z: 9893}},
		{"the arrival to the needle's crate", questplan.EwArrive, tile.Tile{X: 2701, Z: 9887}}, {"the needle's crate to the bellows", tile.Tile{X: 2701, Z: 9887}, tile.Tile{X: 2733, Z: 9882}},
		{"the arrival to the bowl's crate", questplan.EwArrive, tile.Tile{X: 2717, Z: 9893}}, {"the bowl's crate to the trough", tile.Tile{X: 2717, Z: 9893}, tile.Tile{X: 2714, Z: 9872}},
		{"the trough to the furnace", tile.Tile{X: 2714, Z: 9872}, tile.Tile{X: 2723, Z: 9875}}, {"the furnace to the rock", tile.Tile{X: 2723, Z: 9875}, tile.Tile{X: 2705, Z: 9904}},
		{"the rock to the air lever", tile.Tile{X: 2705, Z: 9904}, tile.Tile{X: 2733, Z: 9887}}, {"the air lever to the furnace", tile.Tile{X: 2733, Z: 9887}, tile.Tile{X: 2723, Z: 9875}},
		{"the furnace to the workbench", tile.Tile{X: 2723, Z: 9875}, tile.Tile{X: 2716, Z: 9888}}, {"the workbench to the stairs up", tile.Tile{X: 2716, Z: 9888}, tile.Tile{X: 2713, Z: 9888}},
		{"the rock to the stairs up", tile.Tile{X: 2705, Z: 9904}, tile.Tile{X: 2713, Z: 9888}},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), opts)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-36s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The wall and the stairs are the crossings the planner cannot find, in either direction: the secret room and the workshop are pockets.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the village to the secret room", bank, questplan.EwWallInside}, {"the wall's outside to the secret room", questplan.EwWallOutside, questplan.EwWallInside},
		{"the secret room to the village", questplan.EwWallInside, bank}, {"the secret room to the wall's outside", questplan.EwWallInside, questplan.EwWallOutside},
		{"the village to the workshop", bank, questplan.EwArrive}, {"the secret room to the workshop", questplan.EwStairsDownStand, questplan.EwArrive},
		{"the workshop to the secret room", questplan.EwArrive, questplan.EwStairsDownStand}, {"the workshop to the village", questplan.EwArrive, bank},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), opts); r.OK && r.End().X == l.to.X && r.End().Z == l.to.Z {
			t.Errorf("%s: the planner finds a route (cost %d): the crossing is not the module's alone", l.name, r.Cost)
		}
	}
}

// The pocket's rectangles are the ground the pack makes them: the workshop's rectangle holds every tile reached on foot from the stairs'
// landing (771) and the secret room's every tile reached from its stairs (5), and nothing of the village is in either.
func TestElementalWorkshopThePocketIsTheGroundOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	workshop := pk.Flood(pathfind.NavPoint{X: questplan.EwArrive.X, Z: questplan.EwArrive.Z, Level: 0}, 90)
	room := pk.Flood(pathfind.NavPoint{X: questplan.EwStairsDownStand.X, Z: questplan.EwStairsDownStand.Z, Level: 0}, 20)
	t.Logf("the workshop is %d tiles, the secret room %d", len(workshop), len(room))
	if len(workshop) != 771 || len(room) != 5 {
		t.Fatalf("the workshop is %d tiles and the secret room %d: the pack no longer says 771 and 5", len(workshop), len(room))
	}
	for k := range workshop {
		if got := questplan.EwRegionOf(tile.Tile{X: k[0], Z: k[1]}); got != questplan.EwWorkshop {
			t.Errorf("%v is reached on foot in the workshop and is region %d", k, got)
		}
	}
	for k := range room {
		if got := questplan.EwRegionOf(tile.Tile{X: k[0], Z: k[1]}); got != questplan.EwSecretRoom {
			t.Errorf("%v is reached on foot in the secret room and is region %d", k, got)
		}
	}
	// The wall's outside row and the village are the surface.
	for _, at := range []tile.Tile{questplan.EwWallOutside, {X: 2709, Z: 3495}, questplan.SeersBank, questplan.EwBookcaseStand, {X: 2709, Z: 3499}} {
		if questplan.EwRegionOf(at) != questplan.EwSurface {
			t.Errorf("%v is not the surface", at)
		}
	}
	// Every spawn and stand of the plan is a floor tile.
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the bookcase's stand", questplan.EwBookcaseStand}, {"the wall's outside", questplan.EwWallOutside}, {"the wall's inside", questplan.EwWallInside},
		{"the secret room's landing", questplan.EwSecretArrive}, {"the stairs' stand", questplan.EwStairsDownStand}, {"the workshop's landing", questplan.EwArrive},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not a walkable tile on the real pack", c.what, c.at)
		}
	}
	// The bank, the shops and the village are not in the pocket; the workshop and the secret room are, and registered.
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{questplan.EwArrive, true}, {questplan.EwRockAt, true}, {questplan.EwWallInside, true}, {questplan.EwSecretArrive, true},
		{questplan.SeersBank, false}, {questplan.EwWallOutside, false}, {questplan.EwBookcaseStand, false}, {questplan.ArheinStore.Anchor, false},
		{questplan.DommikShop.Anchor, false},
	} {
		name, in := questrunner.PocketAt(c.at)
		if in != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, in, c.want)
		}
	}
}
