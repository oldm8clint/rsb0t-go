package questmods

// Biohazard on the real data: the production config, the baked loc index, the collision pack and the web graph (RSB0T_CONFIG_JAG,
// RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them, through csData). The questions: whether every id the plan
// carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where the plan
// says and each has a tile to stand on that the engine accepts the op from, whether every walk the quest asks for is a route the planner
// finds, whether the areas the decision tells apart are the regions the pack makes, whether the places the quest crosses by script (the
// headquarters' doors, the plague wall) are the places the planner cannot cross on its own (which is what the module's legs and its
// pocket are for), whether the walks keep clear of the hunters of West Ardougne and the dogs of the market, and whether the gate that
// searches is on the graph (it is, so the module never lets the walker cross it).

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestBiohazardIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for _, c := range []struct {
		id       int
		name, op string
	}{
		{questplan.BioObjEthenea, "Ethanea", ""}, {questplan.BioObjLiquidHoney, "Liquid honey", ""}, {questplan.BioObjBroline, "Sulphuric broline", ""},
		{questplan.BioObjSample, "Plague sample", ""}, {questplan.BioObjTouchPaper, "Touch paper", ""}, {questplan.BioObjDistillator, "Distillator", ""},
		{questplan.BioObjBirdfeed, "Bird feed", ""}, {questplan.BioObjKey, "Key", ""}, {questplan.BioObjPigeons, "Pigeon cage", "Open"},
		{questplan.BioObjEmptyCage, "Pigeon cage", ""}, {questplan.BioObjPriestGown, "Priest gown", "Wear"}, {questplan.BioObjPriestRobe, "Priest gown", "Wear"},
		{questplan.BioObjDoctorGown, "Doctors' gown", "Wear"}, {questplan.BioObjRottenApples, "Rotten apples", "Eat"}, {questplan.CoinsID, "Coins", ""},
		{questplan.PlagueObjShark, "Shark", "Eat"},
	} {
		o, ok := cfg.Obj(c.id)
		if !ok || o.Name != c.name || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", c.id, o.Name, ok, o.Noted(), c.name)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(actions.HeldOps(o.IOps), c.op) == 0 {
			t.Errorf("obj %d (%s) offers %v, want a %q op", c.id, o.Name, o.IOps, c.op)
		}
	}
	for id, want := range map[int]string{
		questplan.BioNpcElena: "Elena", questplan.BioNpcJerico: "Jerico", questplan.BioNpcOmart: "Omart", questplan.BioNpcKilron: "Kilron",
		questplan.BioNpcChemist: "Chemist", questplan.BioNpcHopsRim: "Hops", questplan.BioNpcHopsVar: "Hops", questplan.BioNpcDeVinciRim: "DeVinci",
		questplan.BioNpcDeVinciVar: "DeVinci", questplan.BioNpcChancyRim: "Chancy", questplan.BioNpcChancyVar: "Chancy", questplan.BioNpcGuidor: "Guidor",
		questplan.BioNpcGuidorsWife: "Guidor's wife", questplan.BioNpcGuard: "Guard", questplan.BioNpcThessalia: "Thessalia", questplan.BioNpcLathas: "King Lathas",
		questplan.NpcSickMourner: "Mourner", questplan.BioNpcMournerDown: "Mourner", questplan.BioNpcMournerLow: "Mourner", questplan.BioNpcMournerArm: "Mourner",
		questplan.BioNpcZombie: "Zombie", questplan.BioNpcZombieArmed: "Zombie", questplan.BioNpcGhost: "Ghost",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	for id, level := range map[int]int{questplan.NpcSickMourner: 13, questplan.BioNpcMournerDown: 24, questplan.BioNpcMournerLow: 12, questplan.BioNpcMournerArm: 18,
		questplan.BioNpcZombie: 13, questplan.BioNpcZombieArmed: 24, questplan.BioNpcGhost: 19} {
		if n, _ := cfg.Npc(id); n.Level != level {
			t.Errorf("npc %d (%s) is level %d, the plan says %d", id, n.Name, n.Level, level)
		}
	}
	// The sick mourner can be talked to and attacked, the dinner's mourners too: the plan talks only to the first and never attacks the others.
	if n, _ := cfg.Npc(questplan.NpcSickMourner); cachecfg.OpIndex(n.Ops, "Talk-to") == 0 || cachecfg.OpIndex(n.Ops, "Attack") == 0 {
		t.Errorf("the sick mourner offers %v", n.Ops)
	}
	for _, c := range []struct {
		id       int
		name, op string
	}{
		{questplan.BioLocJericoCupboardShut, "Cupboard", "Open"}, {questplan.BioLocJericoCupboardOpen, "Cupboard", "Search"}, {questplan.BioLocWatchtower, "Watchtower", "Investigate"},
		{questplan.BioLocCauldron, "Cauldron", ""}, {questplan.BioLocNurseCupboardShut, "Cupboard", "Open"}, {questplan.BioLocNurseCupboardOpen, "Cupboard", "Search"},
		{questplan.BioLocHQDoor, "Door", "Open"}, {questplan.BioLocHQUpDoor, "Door", "Open"}, {questplan.BioLocHQGateRight, "Gate", "Open"}, {questplan.BioLocHQGateLeft, "Gate", "Open"},
		{questplan.BioLocHQCrate, "Crate", "Search"}, {questplan.BioLocHQStairs, "Staircase", "Climb-up"}, {questplan.BioLocYardFence, "Fence", "Climb-over"},
		{questplan.BioLocVarrockGateA, "Gate", "Open"}, {questplan.BioLocVarrockGateB, "Gate", "Open"}, {questplan.BioLocGuidorDoor, "Door", "Open"},
		{questplan.PlagueLocManholeShut, "Manhole", "Open"}, {questplan.PlagueLocManholeOpen, "Manhole", "Climb-down"}, {questplan.PlagueLocMudPile, "Mud pile", "Climb"},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name {
			t.Errorf("loc %d is %q (found %v), the plan says %q", c.id, l.Name, ok, c.name)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) == 0 {
			t.Errorf("loc %d (%s) offers %v, want %q", c.id, l.Name, l.Ops, c.op)
		}
	}
	// The cupboards are searched from one side only (the forced approach), the crate and the door from any.
	for _, id := range []int{questplan.BioLocJericoCupboardShut, questplan.BioLocNurseCupboardShut} {
		if l, _ := cfg.Loc(id); l.ForceApproach == 0 {
			t.Errorf("loc %d has no forced approach in the config", id)
		}
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts an op from.
func TestBiohazardLocQueriesFindTheirLocsAndTheirStands(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct {
		what      string
		id        int
		name, op  string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile // tiles the plan relies on, each of which must be among the accepted stands
	}{
		{"Jerico's cupboard", questplan.BioLocJericoCupboardShut, "", "Open", questplan.BioJericoCupboard, 3, 2611, 3326, 0, []tile.Tile{{X: 2611, Z: 3325}}},
		{"the watch tower's east wall", questplan.BioLocWatchtower, "", "", questplan.BioWatchtower, 6, 2563, 3304, 0, []tile.Tile{{X: 2563, Z: 3304}}},
		{"the cauldron", questplan.BioLocCauldron, "", "", questplan.BioCauldron, 6, 2543, 3332, 0, nil},
		{"the nurse's cupboard", questplan.BioLocNurseCupboardShut, "", "Open", questplan.BioNurseCupboard, 3, 2517, 3276, 0, nil},
		{"the street door", questplan.BioLocHQDoor, "", "Open", questplan.BioHQDoorOutside, 3, 2551, 3320, 0, []tile.Tile{questplan.BioHQDoorOutside, questplan.BioHQDoorInside}},
		{"the door upstairs", questplan.BioLocHQUpDoor, "", "Open", tile.Tile{X: 2547, Z: 3325, Level: 1}, 3, 2547, 3325, 1,
			[]tile.Tile{{X: 2547, Z: 3325, Level: 1}, {X: 2546, Z: 3325, Level: 1}}},
		{"the left leaf of the gate", questplan.BioLocHQGateLeft, "", "Open", questplan.BioGate, 3, 2551, 3326, 1, []tile.Tile{questplan.BioGate, {X: 2552, Z: 3326, Level: 1}}},
		{"the crate", questplan.BioLocHQCrate, "", "Search", questplan.BioCrate, 3, 2554, 3327, 1, []tile.Tile{questplan.BioCrateStand}},
		{"Varrock's gate, north leaf", questplan.BioLocVarrockGateB, "", "Open", questplan.BioGateOutside, 3, 3264, 3406, 0, []tile.Tile{questplan.BioGateOutside, {X: 3264, Z: 3406}}},
		{"Varrock's gate from inside", questplan.BioLocVarrockGateB, "", "Open", questplan.BioGateInside, 3, 3264, 3406, 0, []tile.Tile{{X: 3264, Z: 3406}}},
		{"Guidor's door", questplan.BioLocGuidorDoor, "", "Open", questplan.BioGuidorDoor, 3, 3282, 3382, 0, []tile.Tile{questplan.BioGuidorDoor, {X: 3283, Z: 3382}}},
	} {
		m, ok := questrunner.FindLoc(d, c.id, c.name, c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc found near %d,%d,%d", c.what, c.near.X, c.near.Z, c.near.Level)
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
	// The tower's east wall has a stand inside the pigeon box (so the bag is thrown from where the cage is opened).
	m, _ := questrunner.FindLoc(d, questplan.BioLocWatchtower, "", "", questplan.BioWatchtower, 6)
	in := false
	for _, s := range questrunner.LocStands(d, m, questplan.BioWatchtower) {
		if questplan.BioPigeonZone.Contains(s) && d.Pack.Walkable(s.X, s.Z, s.Level) {
			in = true
		}
	}
	if !in {
		t.Error("no stand of the watch tower's wall is inside the pigeon box")
	}
	if !questplan.BioPigeonZone.Contains(bioPigeonStand) || !d.Pack.Walkable(bioPigeonStand.X, bioPigeonStand.Z, bioPigeonStand.Level) {
		t.Errorf("the pigeon stand %v is not a walkable tile inside the box", bioPigeonStand)
	}
	// Both cupboards open and search from a tile south of them (angle 1 turns the forced approach east to south).
	for _, c := range []struct {
		id   int
		near tile.Tile
		want tile.Tile
	}{{questplan.BioLocJericoCupboardShut, questplan.BioJericoCupboard, tile.Tile{X: 2611, Z: 3325}}, {questplan.BioLocNurseCupboardShut, questplan.BioNurseCupboard, tile.Tile{X: 2517, Z: 3275}}} {
		m, _ := questrunner.FindLoc(d, c.id, "", "Open", c.near, 3)
		stands := questrunner.LocStands(d, m, c.near)
		if len(stands) == 0 || stands[0] != c.want && !containsTile(stands, c.want) {
			t.Errorf("cupboard %d: stands %v, want %v among them", c.id, stands, c.want)
		}
	}
}

func containsTile(ts []tile.Tile, q tile.Tile) bool {
	for _, t := range ts {
		if t == q {
			return true
		}
	}
	return false
}

// Every walk the quest asks for is a route the planner finds on the real pack and graph.
func TestBiohazardLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := tile.Tile{X: 2616, Z: 3332}
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// East Ardougne.
		{"bank to Elena", bank, questplan.BioElenaAnchor}, {"bank to Jerico", bank, questplan.BioJericoAnchor},
		{"bank to Jerico's cupboard", bank, tile.Tile{X: 2611, Z: 3325}}, {"Jerico to the watch tower", questplan.BioJericoAnchor, questplan.BioWatchtower},
		{"the cupboard to the watch tower", tile.Tile{X: 2611, Z: 3325}, questplan.BioWatchtower}, {"Jerico to the pigeons", questplan.BioJericoAnchor, questplan.BioPigeonSpawns[0]},
		{"the pigeons to the box", questplan.BioPigeonSpawns[0], bioPigeonStand}, {"the box to Omart", bioPigeonStand, questplan.BioOmartAnchor},
		{"bank to Omart", bank, questplan.BioOmartAnchor}, {"the east landing to Elena", questplan.BioEastLanding, questplan.BioElenaAnchor},
		{"the east landing to the bank", questplan.BioEastLanding, bank}, {"bank to King Lathas", bank, questplan.BioLathas}, {"King Lathas to Elena", questplan.BioLathas, questplan.BioElenaAnchor},
		{"Elena to the bank", questplan.BioElenaAnchor, bank},
		// West Ardougne, west of the wall.
		{"the west landing to the apples", questplan.BioWestLanding, questplan.BioApples}, {"the apples to the cauldron", questplan.BioApples, questplan.BioCauldronAt},
		{"the west landing to the cauldron", questplan.BioWestLanding, questplan.BioCauldronAt}, {"the yard to the nurse's cupboard", questplan.BioCauldronAt, tile.Tile{X: 2517, Z: 3275}},
		{"the west landing to the nurse's cupboard", questplan.BioWestLanding, tile.Tile{X: 2517, Z: 3275}},
		{"the cupboard to the street door", tile.Tile{X: 2517, Z: 3275}, questplan.BioHQDoorOutside}, {"the yard to the street door", questplan.BioCauldronAt, questplan.BioHQDoorOutside},
		{"the west landing to the street door", questplan.BioWestLanding, questplan.BioHQDoorOutside}, {"the street door to Kilron", questplan.BioHQDoorOutside, questplan.BioKilronAnchor},
		{"the cupboard to Kilron", tile.Tile{X: 2517, Z: 3275}, questplan.BioKilronAnchor}, {"the apples to Kilron", questplan.BioApples, questplan.BioKilronAnchor},
		// Inside the headquarters.
		{"inside the door to the stairs", questplan.BioHQDoorInside, tile.Tile{X: 2544, Z: 3325}},
		{"inside the door to the sick mourners' room", questplan.BioHQDoorInside, questplan.BioSickRoom},
		{"the sick mourners' room to the gate", questplan.BioSickRoom, questplan.BioGate}, {"the gate to the crate", questplan.BioGate, questplan.BioCrateStand},
		{"the crate to inside the door", questplan.BioCrateStand, questplan.BioHQDoorInside}, {"the room to inside the door", questplan.BioSickRoom, questplan.BioHQDoorInside},
		// Rimmington, Varrock and the roads between.
		{"bank to the Chemist", bank, questplan.BioChemist}, {"the Chemist to the boys", questplan.BioChemist, questplan.BioHopsRim},
		{"the boys to the Chemist", questplan.BioHopsRim, questplan.BioChemist}, {"the boys to Thessalia", questplan.BioHopsRim, questplan.BioThessalia},
		{"Thessalia to the gate", questplan.BioThessalia, questplan.BioGateOutside}, {"the boys to the gate", questplan.BioHopsRim, questplan.BioGateOutside},
		{"the Chemist to the gate", questplan.BioChemist, questplan.BioGateOutside},
		{"the gate to the Donkey", questplan.BioGateInside, questplan.BioDonkey}, {"the Donkey to the boys at the inn", questplan.BioDonkey, questplan.BioChancyVar},
		{"the Donkey to Hops at the inn", questplan.BioDonkey, questplan.BioHopsVar}, {"the Donkey to DeVinci at the inn", questplan.BioDonkey, questplan.BioDeVinciVar},
		{"the Donkey to Guidor's door", questplan.BioDonkey, questplan.BioGuidorDoor}, {"Guidor's door to Guidor", questplan.BioGuidorDoor, questplan.BioGuidor},
		{"Guidor to the gate (out)", questplan.BioGuidor, questplan.BioGateOutside}, {"the Donkey to the gate (out)", questplan.BioDonkey, questplan.BioGateOutside},
		{"the gate to the east bank of Varrock", questplan.BioGateOutside, tile.Tile{X: 3253, Z: 3420}},
		{"the quarter to Elena", questplan.BioGuidor, questplan.BioElenaAnchor}, {"the quarter to Elena by the gate", questplan.BioGateInside, questplan.BioElenaAnchor},
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

// The places the module crosses by script are the places the planner cannot cross on its own: this is why each has a leg, and why the
// headquarters is a pocket. If a re-bake ever makes one of them reachable, the walker would take it and the leg's own door would be
// the wrong tool; this says so. Varrock's gate is the one that IS on the graph (a hop from the tile outside, and from the leaf's own
// tile inside): the module never lets the walker cross it inward, because the walker would not drive the guard's search.
func TestBiohazardTheScriptedWaysAreNotOnTheGraph(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := tile.Tile{X: 2616, Z: 3332}
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the street to inside the headquarters", questplan.BioHQDoorOutside, questplan.BioHQDoorInside},
		{"the yard to inside the headquarters", questplan.BioCauldronAt, questplan.BioHQDoorInside},
		{"inside the headquarters to the street", questplan.BioHQDoorInside, questplan.BioHQDoorOutside},
		{"the sick mourners' room to the street", questplan.BioSickRoom, questplan.BioHQDoorOutside},
		{"the city to the mainland", questplan.BioWestLanding, bank},
		{"the city's apples to the mainland", questplan.BioApples, bank},
		{"the sewer's pipe end to the mainland", tile.Tile{X: 2530, Z: 9703}, bank},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d, hops %d): the scripted way is not the only way", l.name, r.Cost, len(r.Hops()))
		}
	}
	// The graph does have a way IN from the mainland, which only a spade and a gas mask in the pack pass and which works only at the
	// stages of Plague City: two baked hops of a ported TS walk (the mud patch's dig and the sewer pipe's search), item-gated. The module
	// never asks for a destination across the wall from the mainland: the ladder is Omart's, and an account that is across is walked
	// there by Omart and brought back by Kilron (or the manhole).
	for _, from := range []tile.Tile{bank, questplan.BioEastLanding} {
		r := p.Plan(pt(from), pt(questplan.BioWestLanding), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK || len(r.Hops()) != 2 {
			t.Fatalf("%v to the west landing: %v (%d hops)", from, r.OK, len(r.Hops()))
		}
		for i, want := range []struct{ loc, action string }{{"Mud patch", "Dig"}, {"Sewer pipe", "Search"}} {
			if h := r.Hops()[i]; h.LocName != want.loc || h.Action != want.action || h.Requires == nil || len(h.Requires.Items) != 1 {
				t.Errorf("%v: hop %d is %+v, want an item-gated %s %s", from, i, h, want.loc, want.action)
			}
		}
	}
	// Inside the headquarters the planner does have the hall's inner door, the stairs and the door and gate upstairs (they open for a
	// doctor with a key): the three hops from the hall to the room, and the gate from the room to the crate.
	r := p.Plan(pt(questplan.BioHQDoorInside), pt(questplan.BioSickRoom), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !r.OK || len(r.Hops()) != 3 {
		t.Fatalf("the hall to the room: %v (%d hops)", r.OK, len(r.Hops()))
	}
	for i, want := range []struct{ loc, action string }{{"Door", "Open"}, {"Staircase", "Climb-up"}, {"Door", "Open"}} {
		if h := r.Hops()[i]; h.LocName != want.loc || h.Action != want.action {
			t.Errorf("hop %d is %s %s, want %s %s", i, h.LocName, h.Action, want.loc, want.action)
		}
	}
	if r := p.Plan(pt(questplan.BioSickRoom), pt(questplan.BioCrateStand), webwalk.Options{AllowSpecial: true, AllowNPC: true}); !r.OK || len(r.Hops()) != 1 || r.Hops()[0].LocID != questplan.BioLocHQGateLeft {
		t.Errorf("the room to the crate: %v (hops %+v)", r.OK, r.Hops())
	}
	// The fence is a graph hop both ways (the yard has no other door the planner can use).
	for _, c := range []struct{ from, to tile.Tile }{{questplan.BioWestLanding, questplan.BioCauldronAt}, {questplan.BioCauldronAt, questplan.BioHQDoorOutside}} {
		r := p.Plan(pt(c.from), pt(c.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK || len(r.Hops()) != 1 || r.Hops()[0].LocID != questplan.BioLocYardFence {
			t.Errorf("%v to %v: %v (hops %+v)", c.from, c.to, r.OK, r.Hops())
		}
	}
	// Varrock's gate: inward from the tile outside the leaf, outward from the leaf's own tile.
	in := p.Plan(pt(questplan.BioGateOutside), pt(questplan.BioGateInside), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	out := p.Plan(pt(questplan.BioGateInside), pt(questplan.BioGateOutside), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !in.OK || len(in.Hops()) != 1 || in.Hops()[0].From != (webwalk.Point{X: 3263, Z: 3406}) || !out.OK || len(out.Hops()) != 1 || out.Hops()[0].From != (webwalk.Point{X: 3264, Z: 3406}) {
		t.Errorf("the gate: in %v %+v, out %v %+v", in.OK, in.Hops(), out.OK, out.Hops())
	}
	// Without the gate there is no way into the quarter: every route to the Donkey from the mainland goes through a leaf.
	for _, from := range []tile.Tile{questplan.BioThessalia, questplan.BioGateOutside, bank} {
		r := p.Plan(pt(from), pt(questplan.BioDonkey), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		through := false
		for _, h := range r.Hops() {
			if h.LocID == questplan.BioLocVarrockGateA || h.LocID == questplan.BioLocVarrockGateB {
				through = true
			}
		}
		if !r.OK || !through {
			t.Errorf("%v to the Donkey: %v, through a gate leaf %v", from, r.OK, through)
		}
	}
}

// The areas the decision tells apart are the pack's components: every walkable tile the planner reaches on foot from the hall lies in the
// headquarters box on its floor, from the street in West Ardougne, from the Donkey in the quarter, and the mainland the planner reaches
// from Elena's door is never any of them.
func TestBiohazardAreasAreTheCollisionPacksComponents(t *testing.T) {
	pk, _, _ := csData(t)
	flood := func(at tile.Tile, r int) map[[2]int]bool {
		return pk.Flood(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}, r)
	}
	check := func(name string, at tile.Tile, r int, want questplan.BioArea, minTiles int) {
		t.Helper()
		f := flood(at, r)
		if len(f) < minTiles {
			t.Errorf("%s: %d tiles reached, the plan says at least %d", name, len(f), minTiles)
		}
		for k := range f {
			if got := questplan.BioAreaOf(tile.Tile{X: k[0], Z: k[1], Level: at.Level}); got != want {
				t.Errorf("%s: %v is reached on foot and the plan calls it %v, not %v", name, k, got, want)
				return
			}
		}
	}
	check("the hall, east of its inner door", questplan.BioHQDoorInside, 40, questplan.BioHQ, 36)
	check("the hall, the stairs' room", tile.Tile{X: 2544, Z: 3325}, 40, questplan.BioHQ, 16)
	check("upstairs, the stairs' room", tile.Tile{X: 2544, Z: 3326, Level: 1}, 40, questplan.BioHQUp, 13)
	check("upstairs, the sick mourners' room", questplan.BioSickRoom, 40, questplan.BioHQUp, 37)
	check("upstairs, behind the gate", questplan.BioCrateStand, 40, questplan.BioHQUp, 5)
	check("the quarter", questplan.BioDonkey, 200, questplan.BioQuarter, 451)
	check("Guidor's room", questplan.BioGuidor, 20, questplan.BioQuarter, 10)
	for k := range flood(questplan.BioGuidor, 20) {
		if !questplan.InBioGuidorsRoom(tile.Tile{X: k[0], Z: k[1]}) {
			t.Errorf("%v is reached on foot from Guidor and is outside his room", k)
		}
	}
	check("the city's streets", questplan.BioWestLanding, 160, questplan.BioWest, 4500)
	check("the yard", questplan.BioCauldronAt, 60, questplan.BioWest, 69)
	// The mainland round Elena is none of those.
	east := flood(tile.Tile{X: 2616, Z: 3332}, 120)
	for k := range east {
		if got := questplan.BioAreaOf(tile.Tile{X: k[0], Z: k[1]}); got != questplan.BioEast {
			t.Errorf("%v is reached on foot from the north bank and the plan calls it %v", k, got)
			break
		}
	}
	if len(east) < 20000 {
		t.Errorf("only %d tiles reached from the bank", len(east))
	}
	// Rimmington's whole component (the Chemist's house and the boys) is the mainland.
	rim := flood(questplan.BioHopsRim, 120)
	for k := range rim {
		if got := questplan.BioAreaOf(tile.Tile{X: k[0], Z: k[1]}); got != questplan.BioEast {
			t.Errorf("%v is reached on foot from the boys and the plan calls it %v", k, got)
			break
		}
	}
	// The rest of the places the plan names are floor.
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the west landing", questplan.BioWestLanding}, {"the east landing", questplan.BioEastLanding}, {"the apples", questplan.BioApples},
		{"the cauldron's yard tile", questplan.BioCauldronAt}, {"the street door's own tile", questplan.BioHQDoorOutside}, {"inside the door", questplan.BioHQDoorInside},
		{"the sick room", questplan.BioSickRoom}, {"the gate's west tile", questplan.BioGate}, {"the crate's stand", questplan.BioCrateStand},
		{"the pigeon stand", bioPigeonStand}, {"the pigeons 1", questplan.BioPigeonSpawns[0]}, {"the pigeons 2", questplan.BioPigeonSpawns[1]}, {"the pigeons 3", questplan.BioPigeonSpawns[2]},
		{"outside the gate", questplan.BioGateOutside}, {"inside the gate", questplan.BioGateInside}, {"the Donkey", questplan.BioDonkey}, {"Guidor's door", questplan.BioGuidorDoor},
		{"Guidor", questplan.BioGuidor}, {"Thessalia", questplan.BioThessalia}, {"the Chemist", questplan.BioChemist}, {"Hops", questplan.BioHopsRim},
		{"DeVinci", questplan.BioDeVinciRim}, {"Chancy", questplan.BioChancyRim}, {"Hops at the inn", questplan.BioHopsVar}, {"DeVinci at the inn", questplan.BioDeVinciVar},
		{"Chancy at the inn", questplan.BioChancyVar}, {"Elena", questplan.BioElenaAnchor}, {"Jerico", questplan.BioJericoAnchor}, {"Omart", questplan.BioOmartAnchor},
		{"Kilron", questplan.BioKilronAnchor}, {"King Lathas", questplan.BioLathas},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not walkable on the real pack", c.what, c.at)
		}
	}
}

// Every leg a custom step walks ends on a tile an op can be sent from, and the ground spawns can be taken from some tile.
func TestBiohazardGroundSpawnsAndTalksCanBeReached(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the apples", questplan.BioApples}, {"a pigeon cage 1", questplan.BioPigeonSpawns[0]}, {"a pigeon cage 2", questplan.BioPigeonSpawns[1]}, {"a pigeon cage 3", questplan.BioPigeonSpawns[2]},
	} {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: c.at.X, Z: c.at.Z, Level: c.at.Level}); len(tiles) == 0 {
			t.Errorf("the %s spawn at %v can be taken from no tile", c.what, c.at)
		}
	}
	// Every talk can be opened from some tile: the npcs' spawns, the anchors the stops name.
	for _, stop := range []questplan.NpcStop{questplan.BioElenaStart, questplan.BioJericoStop, questplan.BioOmartStop, questplan.BioKilronStop, questplan.BioMournerStop,
		questplan.BioChemistStop, questplan.BioGuidorStop, questplan.BioLathasStop} {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: stop.Anchor.X, Z: stop.Anchor.Z, Level: stop.Anchor.Level}); len(tiles) == 0 {
			t.Errorf("%s at %v can be talked to from no tile", stop.Npc, stop.Anchor)
		}
	}
	for _, er := range questplan.BioErrands {
		for _, stop := range []questplan.NpcStop{er.Rim, er.Var} {
			if tiles := pk.InteractTiles(pathfind.NavPoint{X: stop.Anchor.X, Z: stop.Anchor.Z, Level: stop.Anchor.Level}); len(tiles) == 0 {
				t.Errorf("%s at %v can be talked to from no tile", stop.Npc, stop.Anchor)
			}
		}
	}
}

// route tiles of a plan, corner to corner.
func bioRouteTiles(r webwalk.Route) []tile.Tile { return plagueRouteTiles(r) }

// No walk of the quest in West Ardougne goes where a zombie or a ghost of the city's west end can notice it: the closest any route comes
// to a hunter's wander box is more than its notice range.
func TestBiohazardWestWalksKeepClearOfTheHunters(t *testing.T) {
	p, pt := mcPlanner(t)
	cupboard := tile.Tile{X: 2518, Z: 3275}
	zones := bioHunterRects()
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the landing to the apples", questplan.BioWestLanding, questplan.BioApples}, {"the landing to the cupboard", questplan.BioWestLanding, cupboard},
		{"the cupboard to the landing", cupboard, questplan.BioWestLanding}, {"the cupboard to the street door", cupboard, questplan.BioHQDoorOutside},
		{"the cupboard to Kilron", cupboard, questplan.BioKilronAnchor}, {"the landing to the street door", questplan.BioWestLanding, questplan.BioHQDoorOutside},
		{"the apples to the cauldron", questplan.BioApples, questplan.BioCauldronAt}, {"the yard to the cupboard", questplan.BioCauldronAt, cupboard},
		{"the yard to Kilron", questplan.BioCauldronAt, questplan.BioKilronAnchor}, {"the street door to Kilron", questplan.BioHQDoorOutside, questplan.BioKilronAnchor},
		{"the apples to the street door", questplan.BioApples, questplan.BioHQDoorOutside},
	} {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
		if !r.OK {
			t.Errorf("%s: no route round the hunters (%s)", l.name, r.Reason)
			continue
		}
		tiles := bioRouteTiles(r)
		for _, h := range questplan.BioWestHunters {
			reach := h.Reach()
			for _, q := range tiles {
				if reach.Contains(q) {
					t.Errorf("%s: the route passes %v, inside the reach %+v of the %s at %v", l.name, q, reach, h.Name, h.Spawn)
					break
				}
			}
		}
	}
	// The zones are what keeps the lane clear: the plain route from the hut to the street door passes inside the armed zombie's reach.
	plain := p.Plan(pt(cupboard), pt(questplan.BioHQDoorOutside), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	inside := false
	for _, q := range bioRouteTiles(plain) {
		for _, h := range questplan.BioWestHunters {
			if h.Reach().Contains(q) {
				inside = true
			}
		}
	}
	if !plain.OK || !inside {
		t.Logf("the plain route from the hut to the street door no longer enters a hunter's reach (ok %v): the zones are not what keeps it clear", plain.OK)
	}
	// And the places the quest stands to work are outside every reach.
	for _, at := range []tile.Tile{cupboard, {X: 2517, Z: 3275}, {X: 2519, Z: 3275}, questplan.BioApples, questplan.BioWestLanding, questplan.BioKilronAnchor, questplan.BioHQDoorOutside, questplan.BioCauldronAt} {
		for _, h := range questplan.BioWestHunters {
			if h.Reach().Contains(at) {
				t.Errorf("%v is inside the reach of the %s at %v", at, h.Name, h.Spawn)
			}
		}
	}
}

// East Ardougne's market holds four guard dogs (level 44, hunting accounts of combat 88 or less one tile from where they stand). The
// quest's walks between the bank, Elena's house, Jerico's, the pigeons, the tower and Omart do not pass within a dog's reach.
func TestBiohazardEastWalksKeepClearOfTheMarketDogs(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := tile.Tile{X: 2616, Z: 3332}
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"bank to Elena", bank, questplan.BioElenaAnchor}, {"Elena to the bank", questplan.BioElenaAnchor, bank}, {"bank to Jerico", bank, questplan.BioJericoAnchor},
		{"bank to Jerico's cupboard", bank, tile.Tile{X: 2611, Z: 3325}}, {"Jerico to the pigeons", questplan.BioJericoAnchor, questplan.BioPigeonSpawns[0]},
		{"the pigeons to the tower", questplan.BioPigeonSpawns[0], bioPigeonStand}, {"the tower to Omart", bioPigeonStand, questplan.BioOmartAnchor},
		{"Omart to Elena", questplan.BioOmartAnchor, questplan.BioElenaAnchor}, {"Omart to the bank", questplan.BioOmartAnchor, bank},
		{"the east landing to Elena", questplan.BioEastLanding, questplan.BioElenaAnchor}, {"bank to Omart", bank, questplan.BioOmartAnchor},
		{"Elena to Omart", questplan.BioElenaAnchor, questplan.BioOmartAnchor}, {"Elena to King Lathas", questplan.BioElenaAnchor, questplan.BioLathas},
		{"bank to King Lathas", bank, questplan.BioLathas}, {"King Lathas to Elena", questplan.BioLathas, questplan.BioElenaAnchor},
	} {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		if d, at := plagueClosest(bioRouteTiles(r), questplan.PlagueMarketDogs); d < questplan.PlagueDogReach {
			t.Errorf("%s passes %d tiles from a market dog's spawn, at %v: under its reach of %d", l.name, d, at, questplan.PlagueDogReach)
		}
	}
}

// The headquarters pocket is registered, covers both floors, and the street and the sewer are Plague City's.
func TestBiohazardPocketsAreRegistered(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{questplan.BioHQDoorInside, true}, {tile.Tile{X: 2544, Z: 3325}, true}, {questplan.BioSickRoom, true}, {questplan.BioCrateStand, true},
		{questplan.BioWestLanding, true}, {questplan.BioApples, true}, {questplan.BioCauldronAt, true}, {tile.Tile{X: 2562, Z: 9737}, true},
		{questplan.BioEastLanding, false}, {tile.Tile{X: 2616, Z: 3332}, false}, {questplan.BioDonkey, false},
	} {
		name, in := questrunner.PocketAt(c.at)
		if in != c.want {
			t.Errorf("PocketAt(%v) = %q, %v; want %v", c.at, name, in, c.want)
		}
	}
	// The headquarters' pocket is checked before Plague City's (init order: biohazard.go before plague.go), on both floors, because Plague
	// City's own way home cannot leave the hall.
	for _, at := range []tile.Tile{questplan.BioSickRoom, questplan.BioHQDoorInside, {X: 2544, Z: 3325}, questplan.BioCrateStand} {
		if name, _ := questrunner.PocketAt(at); name != "the Mourners' headquarters" {
			t.Errorf("%v is in %q, want the headquarters' pocket", at, name)
		}
	}
}
