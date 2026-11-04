package questmods

// Sheep Herder on the real data: the production config, the baked loc index and the collision pack (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX,
// RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan carries names what it says in the config
// the fleet's client reads, whether the customs' own loc queries find their locs where the plan says and each has a tile to stand on,
// whether every leg the quest walks is a route the planner finds on the real pack, whether every one of the twelve sheep has a push route to the
// fence, and whether the enclosure is the pocket the module says it is.

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestSheepHerderIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjCattleProd: "Prod", questplan.ObjSheepFeed: "Feed", questplan.ObjPlagueJacket: "Plague jacket", questplan.ObjPlagueTrousers: "Plague trousers",
		questplan.ObjSheepBones(1): "Bones", questplan.ObjSheepBones(2): "Bones", questplan.ObjSheepBones(3): "Bones", questplan.ObjSheepBones(4): "Bones",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// The suit and the prod are worn from the pack by their second option (Wear, Equip): the held-op index of actions.Equip.
	for id, op := range map[int]string{questplan.ObjCattleProd: "Equip", questplan.ObjPlagueJacket: "Wear", questplan.ObjPlagueTrousers: "Wear"} {
		if o, _ := cfg.Obj(id); cachecfg.OpIndex(o.IOps, op) != 2 {
			t.Errorf("obj %d offers %v: %s is expected as the second option", id, o.IOps, op)
		}
	}
	for id, want := range map[int]string{
		questplan.NpcHalgrive: "Councillor Halgrive", questplan.NpcOrbon: "Doctor Orbon", questplan.NpcBrumty: "Farmer Brumty",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want || cachecfg.OpIndex(n.Ops, "Talk-to") == 0 {
			t.Errorf("npc %d is %q ops %v (found %v), the plan says %q", id, n.Name, n.Ops, ok, want)
		}
	}
	for n := 1; n <= 4; n++ {
		s, ok := cfg.Npc(questplan.NpcPlagueSheep(n))
		if !ok || s.Name != "Diseased sheep "+string(rune('0'+n)) || cachecfg.OpIndex(s.Ops, "Prod") != 1 || cachecfg.OpIndex(s.Ops, "Attack") != 0 {
			t.Errorf("sheep %d is %q ops %v (found %v): Prod is the first option and nothing attacks", n, s.Name, s.Ops, ok)
		}
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocPlagueGateLeft, "Gate", "Open", 1, 1}, {questplan.LocPlagueGateRight, "Gate", "Open", 1, 1}, {questplan.LocPlagueFurnace, "Furnace", "", 3, 3},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || l.Width != c.w || l.Length != c.l {
			t.Errorf("loc %d is %q %dx%d (found %v), the plan says %q %dx%d", c.id, l.Name, l.Width, l.Length, ok, c.name, c.w, c.l)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) == 0 {
			t.Errorf("loc %d (%s) offers %v, want %q", c.id, l.Name, l.Ops, c.op)
		}
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestSheepHerderLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	gate, ok := questrunner.FindLoc(d, questplan.LocPlagueGateRight, "", "Open", questplan.HerdGateOutside, 3)
	if !ok || gate.X != 2594 || gate.Z != 3361 {
		t.Fatalf("the gate is %+v (found %v)", gate, ok)
	}
	// A wall's stands are its own tile and the tile across the edge: the plan stands on each (outside to go in, inside to go out).
	stands := questrunner.LocStands(d, gate, questplan.HerdGateOutside)
	for _, want := range []tile.Tile{questplan.HerdGateOutside, questplan.HerdGateInside} {
		found := false
		for _, s := range stands {
			found = found || s == want
		}
		if !found {
			t.Errorf("the gate's stands %v lack %v", stands, want)
		}
	}
	furnace, ok := questrunner.FindLoc(d, questplan.LocPlagueFurnace, "", "", questplan.HerdFurnaceStand, 6)
	if !ok || furnace.X != questplan.HerdFurnaceAnchor.X || furnace.Z != questplan.HerdFurnaceAnchor.Z {
		t.Fatalf("the incinerator is %+v (found %v)", furnace, ok)
	}
	// forceapproach=east at angle 2 puts the legal side in the west: every stand is at x 2604, west of the 3x3.
	fs := questrunner.LocStands(d, furnace, questplan.HerdFurnaceStand)
	if len(fs) == 0 || fs[0].X != 2604 {
		t.Errorf("the incinerator's stands %v: the only legal side is the west", fs)
	}
	found := false
	for _, s := range fs {
		found = found || s == questplan.HerdFurnaceStand
	}
	if !found {
		t.Errorf("the plan's furnace stand %v is not among %v", questplan.HerdFurnaceStand, fs)
	}
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the gate's outside tile", questplan.HerdGateOutside}, {"the gate's inside tile", questplan.HerdGateInside}, {"the furnace stand", questplan.HerdFurnaceStand},
		{"the prod's spawn", questplan.HerdProdSpawn}, {"Halgrive's", questplan.HalgriveAnchor}, {"Orbon's", questplan.OrbonAnchor},
		{"sheep 1's pen spot", questplan.HerdPenSpots[0]}, {"sheep 2's pen spot", questplan.HerdPenSpots[1]}, {"sheep 3's pen spot", questplan.HerdPenSpots[2]},
		{"sheep 4's pen spot", questplan.HerdPenSpots[3]}, {"sheep 1's field", questplan.HerdSheepSpawns[0]}, {"sheep 2's field", questplan.HerdSheepSpawns[1]},
		{"sheep 3's field", questplan.HerdSheepSpawns[2]}, {"sheep 4's field", questplan.HerdSheepSpawns[3]},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not walkable on the real pack", c.what, c.at)
		}
	}
	// The prod and the barn's floor are takeable from a tile of the pen.
	if tiles := pk.InteractTiles(pathfind.NavPoint{X: questplan.HerdProdSpawn.X, Z: questplan.HerdProdSpawn.Z}); len(tiles) == 0 {
		t.Error("the prod can be taken from no tile")
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestSheepHerderLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.EastArdougneNorth
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"bank to Halgrive", bank, questplan.HalgriveAnchor}, {"bank to Orbon", bank, questplan.OrbonAnchor},
		{"Halgrive to Orbon", questplan.HalgriveAnchor, questplan.OrbonAnchor}, {"Orbon to Halgrive", questplan.OrbonAnchor, questplan.HalgriveAnchor},
		{"Orbon to the bank", questplan.OrbonAnchor, bank}, {"Halgrive to the bank", questplan.HalgriveAnchor, bank},
		{"the bank to the gate", bank, questplan.HerdGateOutside}, {"Orbon to the gate", questplan.OrbonAnchor, questplan.HerdGateOutside},
		{"the gate to Halgrive", questplan.HerdGateOutside, questplan.HalgriveAnchor},
		{"the gate in", questplan.HerdGateOutside, questplan.HerdGateInside}, {"the gate out", questplan.HerdGateInside, questplan.HerdGateOutside},
		{"the gate to the prod", questplan.HerdGateInside, questplan.HerdProdSpawn}, {"the prod to the furnace", questplan.HerdProdSpawn, questplan.HerdFurnaceStand},
		{"the furnace to the gate", questplan.HerdFurnaceStand, questplan.HerdGateInside},
		{"the gate to sheep 1's pen spot", questplan.HerdGateInside, questplan.HerdPenSpots[0]}, {"the gate to sheep 2's pen spot", questplan.HerdGateInside, questplan.HerdPenSpots[1]},
		{"the gate to sheep 3's pen spot", questplan.HerdGateInside, questplan.HerdPenSpots[2]}, {"the gate to sheep 4's pen spot", questplan.HerdGateInside, questplan.HerdPenSpots[3]},
		{"the furnace to sheep 4's pen spot", questplan.HerdFurnaceStand, questplan.HerdPenSpots[3]},
	}
	for i, f := range questplan.HerdSheepSpawns {
		legs = append(legs, struct {
			name     string
			from, to tile.Tile
		}{"Halgrive to sheep " + string(rune('1'+i)) + "'s field", questplan.HalgriveAnchor, f},
			struct {
				name     string
				from, to tile.Tile
			}{"sheep " + string(rune('1'+i)) + "'s field to the gate", f, questplan.HerdGateOutside})
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-36s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
}

// The twelve copies of the four sheep all have a push route to the gate zone on the real pack, and the cheapest route of each field is
// priced in pushes (the TS def measured 31 to 58 pushes for the shortest route and 37 to 255 with the wander).
func TestSheepHerderEveryCopyHasAPushRoute(t *testing.T) {
	pk, _, _ := csData(t)
	grid := packHerdGrid{pk: pk}
	for n := 1; n <= 4; n++ {
		mid := questplan.HerdSheepSpawns[n-1]
		var copies []questplan.HerdPos
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				copies = append(copies, questplan.HerdPos{X: mid.X + dx, Z: mid.Z + dz})
			}
		}
		routed := 0
		cheapest := 1 << 30
		for _, c := range copies {
			if !pk.Walkable(c.X, c.Z, 0) {
				continue
			}
			plan := questplan.NewHerdPlan(grid, c.X, c.Z, questplan.HerdGateZone)
			if cost, ok := plan.Cost(c.X, c.Z); ok {
				routed++
				cheapest = min(cheapest, cost)
			}
		}
		if routed < 3 {
			t.Errorf("sheep %d: only %d of the tiles round its spawn have a push route", n, routed)
		}
		t.Logf("sheep %d: %d routed tiles round %v, the cheapest %d pushes", n, routed, mid, cheapest)
		if cheapest < 20 || cheapest > 80 {
			t.Errorf("sheep %d: %d pushes on the shortest route, the def measured 31 to 58", n, cheapest)
		}
	}
	// The gate zone itself is ground, and the enclosure is the baked rectangle: no tile of the gate zone is inside it, and the tiles
	// across the fence are.
	for x := questplan.HerdGateZone.X0; x <= questplan.HerdGateZone.X1; x++ {
		for z := questplan.HerdGateZone.Z0; z <= questplan.HerdGateZone.Z1; z++ {
			if questplan.InHerdPen(tile.Tile{X: x, Z: z}) {
				t.Errorf("gate zone tile (%d,%d) is in the pen", x, z)
			}
		}
	}
	if !pk.Walkable(2593, 3361, 0) || !pk.Walkable(2594, 3361, 0) {
		t.Error("the gate zone's tiles by the gate are not ground")
	}
}

// The enclosure is a pocket: the rectangle is registered, and the tiles the planner reaches on foot from the gate's inside tile all lie in it.
func TestSheepHerderEnclosureIsARegisteredPocket(t *testing.T) {
	p, _ := mcPlanner(t)
	in := mcReachable(p, questplan.HerdGateInside, 2585, 2620, 3340, 3375, true)
	if len(in) < 150 {
		t.Fatalf("the inside reaches %d tiles on foot: the pack no longer says 184", len(in))
	}
	for _, c := range in {
		if !questplan.InHerdPen(c) {
			t.Errorf("%v is reached on foot from inside the gate and is not in the pen", c)
		}
	}
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{{questplan.HerdGateInside, true}, {questplan.HerdProdSpawn, true}, {questplan.HerdFurnaceStand, true}, {questplan.HerdPenSpots[0], true},
		{questplan.HerdGateOutside, false}, {questplan.HalgriveAnchor, false}, {questplan.EastArdougneNorth, false}} {
		name, got := questrunner.PocketAt(c.at)
		if got != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, got, c.want)
		}
	}
}
