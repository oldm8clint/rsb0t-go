package questmods

// The Observatory Quest on the real data: the production config, the baked loc index, the collision pack and the web graph
// (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan
// carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where the plan
// says and each has a tile to stand on, whether every leg the quest walks is a route the planner finds on the real pack, whether the legs the
// customs exist for (the ladder behind the assistant's warning, the locked gate) are the ones the planner cannot find, whether the
// regions the pockets name are the regions the pack makes them, and whether the goblin guard can be struck from where the quest stands.

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestObservatoryIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjObsKeepKey: "Keep key", questplan.ObjLensMould: "Lens mould", questplan.ObjLens: "Lens", questplan.ObjPlank: "Plank",
		questplan.ObjBronzeBar: "Bronze bar", questplan.ObjMoltenGlass: "Molten glass", questplan.ObjBucketOfSand: "Bucket of sand",
		questplan.ObjSodaAsh: "Soda ash", questplan.ObjSeaweed: "Seaweed", questplan.ObjCopperOre: "Copper ore", questplan.ObjTinOre: "Tin ore",
		questplan.ObjShark: "Shark",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	for id, want := range map[int]string{
		questplan.NpcObsAssistant: "Observatory assistant", questplan.NpcObsProfessor: "Observatory professor",
		questplan.NpcObsProfessorDome: "Observatory professor", questplan.NpcGoblinGuard: "Goblin guard",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	if g, _ := cfg.Npc(questplan.NpcGoblinGuard); g.Level != 42 || g.Size != 1 {
		t.Errorf("the goblin guard is level %d size %d: the plan says 42 and one tile", g.Level, g.Size)
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocObsReceptionLadder, "Ladder", "Climb-down", 1, 1}, {questplan.LocObsCavernExit, "Ladder", "Climb-up", 1, 1},
		{questplan.LocObsDomeLadderUp, "Ladder", "Climb-up", 1, 1}, {questplan.LocObsDomeLadderDown, "Ladder", "Climb-down", 1, 1},
		{questplan.LocObsKeyChestShut, "Chest", "Open", 1, 1}, {questplan.LocObsKeyChestOpen, "Chest", "Search", 1, 1},
		{questplan.LocObsGateLeft, "Keep gate", "Open", 1, 1}, {questplan.LocObsGateRight, "Keep gate", "Open", 1, 1},
		{questplan.LocObsTelescope, "Telescope", "Use", 1, 3}, {questplan.LocObsSack, "Sack", "Search", 1, 1},
		{questplan.LocObsFurnace, "Furnace", "Smelt", 3, 3}, {questplan.LocObsRange, "Range", "", 1, 2},
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
	// The chest's search is op 2 of the open one and the shut one has only Open: the customs address them by name.
	if l, _ := cfg.Loc(questplan.LocObsKeyChestOpen); cachecfg.OpIndex(l.Ops, "Search") != 2 {
		t.Errorf("the open chest offers %v: Search is expected as op 2", l.Ops)
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestObservatoryLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	cs := []struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile // tiles the plan relies on, each of which must be among the accepted stands
	}{
		{"the reception ladder", questplan.LocObsReceptionLadder, "Climb-down", questplan.ObsReceptionLadderStand, 4, 2444, 3191, 0, []tile.Tile{questplan.ObsReceptionLadderStand}},
		{"the cavern's way up", questplan.LocObsCavernExit, "Climb-up", questplan.ObsCavernLanding, 4, 2420, 9460, 0, []tile.Tile{questplan.ObsCavernLanding}},
		{"the dome ladder up", questplan.LocObsDomeLadderUp, "Climb-up", questplan.ObsDomeLadderFoot, 4, 2423, 9439, 0, []tile.Tile{questplan.ObsDomeLadderFoot}},
		{"the dome's ladder down", questplan.LocObsDomeLadderDown, "Climb-down", questplan.ObsDomeArrive, 6, 2440, 3164, 0, []tile.Tile{questplan.ObsDomeArrive}},
		{"the key chest", questplan.LocObsKeyChestShut, "Open", questplan.ObsKeyChestStand, 4, 2428, 9419, 0, []tile.Tile{questplan.ObsKeyChestStand}},
		{"the keep gate's left half", questplan.LocObsGateLeft, "Open", questplan.ObsGateNorth, 4, 2390, 9457, 0, []tile.Tile{questplan.ObsGateNorth, questplan.ObsGateSouth}},
		{"the sack", questplan.LocObsSack, "Search", questplan.ObsSackStand, 4, 2389, 9454, 0, []tile.Tile{questplan.ObsSackStand}},
		{"the telescope", questplan.LocObsTelescope, "Use", questplan.ObsTelescopeAt, 6, 2440, 3157, 0, nil},
		{"the furnace", questplan.LocObsFurnace, "Smelt", questplan.ObsFurnaceAt, 6, 2601, 3310, 0, nil},
		{"the range", questplan.LocObsRange, "", questplan.ObsRangeAt, 6, 2617, 3317, 0, nil},
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
	// The key chest has exactly one stand (forceapproach=north turned by its rotation), and no look-alike chest is within reach of it:
	// the six others spawn a poisonous spider, and the quest finds its chest by id.
	chest, _ := questrunner.FindLoc(d, questplan.LocObsKeyChestShut, "", "Open", questplan.ObsKeyChestStand, 4)
	if got := questrunner.LocStands(d, chest, questplan.ObsKeyChestStand); len(got) != 1 || got[0] != questplan.ObsKeyChestStand {
		t.Errorf("the key chest's stands are %v, want only %v", got, questplan.ObsKeyChestStand)
	}
	for _, spider := range []int{2191, 2192, 2193} {
		if m, near := questrunner.FindLoc(d, spider, "", "Open", questplan.ObsKeyChestStand, 6); near {
			t.Errorf("a spider chest (loc %d) stands at %d,%d, within 6 of the key chest's stand", spider, m.X, m.Z)
		}
	}
	// The sack's only stand is the keep's column; the telescope's stands are all within 7 of the dome professor's spawn.
	sack, _ := questrunner.FindLoc(d, questplan.LocObsSack, "", "Search", questplan.ObsSackStand, 4)
	if got := questrunner.LocStands(d, sack, questplan.ObsSackStand); len(got) != 1 || got[0] != questplan.ObsSackStand {
		t.Errorf("the sack's stands are %v, want only %v", got, questplan.ObsSackStand)
	}
	scope, _ := questrunner.FindLoc(d, questplan.LocObsTelescope, "", "Use", questplan.ObsTelescopeAt, 6)
	for _, s := range questrunner.LocStands(d, scope, questplan.ObsTelescopeAt) {
		if tile.Chebyshev(s, questplan.ObsDomeProfessor) > 7 {
			t.Errorf("the telescope stand %v is more than 7 tiles from the dome professor at %v: the telescope would not answer", s, questplan.ObsDomeProfessor)
		}
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestObservatoryLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.EastArdougneNorth
	legs := []struct {
		name     string
		from, to tile.Tile
		walkOnly bool
	}{
		// The surface.
		{"bank to the professor", bank, questplan.ObsProfessorAnchor, false}, {"the professor to the bank", questplan.ObsProfessorAnchor, bank, false},
		{"the professor to the reception ladder", questplan.ObsProfessorAnchor, questplan.ObsReceptionLadderStand, true},
		{"bank to the outpost's planks", bank, questplan.ObsPlankCentre, false}, {"the planks to the bank", questplan.ObsPlankCentre, bank, false},
		{"bank to the outpost's fourth plank", bank, questplan.ObsPlankSpawns[3], false},
		{"bank to the sand spawn", bank, questplan.ObsSandSpawn, false}, {"the sand to the professor", questplan.ObsSandSpawn, questplan.ObsProfessorAnchor, false},
		{"the sand to the furnace", questplan.ObsSandSpawn, questplan.ArdougneFurnace, false},
		{"bank to the furnace", bank, questplan.ArdougneFurnace, false}, {"the furnace to the range", questplan.ArdougneFurnace, questplan.ObsRangeAt, false},
		{"the range to the furnace", questplan.ObsRangeAt, questplan.ArdougneFurnace, false}, {"the range to the professor", questplan.ObsRangeAt, questplan.ObsProfessorAnchor, false},
		{"the furnace to the professor", questplan.ArdougneFurnace, questplan.ObsProfessorAnchor, false},
		// The cavern, on foot.
		{"the landing to the key chest", questplan.ObsCavernLanding, questplan.ObsKeyChestStand, true},
		{"the key chest to the gate", questplan.ObsKeyChestStand, questplan.ObsGateNorth, true},
		{"the landing to the gate", questplan.ObsCavernLanding, questplan.ObsGateNorth, true}, {"the gate to the landing", questplan.ObsGateNorth, questplan.ObsCavernLanding, true},
		{"the keep's gate to the sack", questplan.ObsGateSouth, questplan.ObsSackStand, true}, {"the sack to the keep's gate", questplan.ObsSackStand, questplan.ObsGateSouth, true},
		{"the landing to the dome ladder", questplan.ObsCavernLanding, questplan.ObsDomeLadderFoot, true},
		{"the dome ladder to the landing", questplan.ObsDomeLadderFoot, questplan.ObsCavernLanding, true},
		// The dome.
		{"the dome's ladder to the telescope", questplan.ObsDomeArrive, tile.Tile{X: 2441, Z: 3158}, true},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true, WalkOnly: l.walkOnly})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-40s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The ladder down is a p_telejump behind the assistant's warning, which the graph marks as a runtime guard and leaves out: the
	// custom's leg is the only way down. If a re-bake ever makes it reachable, the walker would take it without answering the assistant.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the surface to the cavern", questplan.ObsProfessorAnchor, questplan.ObsCavernLanding},
		{"the reception ladder's stand to the cavern", questplan.ObsReceptionLadderStand, questplan.ObsKeyChestStand},
		{"the surface to the dome", questplan.ObsProfessorAnchor, questplan.ObsDomeArrive},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): the scripted way is not the only way", l.name, r.Cost)
		}
	}
	// The gate IS in the graph, in both directions (the keep is reachable at cost 7 from its north side): a walker that took it would meet
	// "The gate is locked." for an account the key has not unlocked it for. That is why the quest never Travels into the keep: the crossing
	// is the custom's (the key on the gate, or Open and the chest on a locked answer), and the walks that follow it stay inside.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the gate's north side to the sack", questplan.ObsGateNorth, questplan.ObsSackStand},
		{"the keep to the gate's north side", questplan.ObsSackStand, questplan.ObsGateNorth},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); !r.OK {
			t.Errorf("%s: no route (%s): the gate has left the graph, and the pocket's Leave is no longer a fallback but the only way", l.name, r.Reason)
		}
	}
	// And the other direction out of each place the quest leaves an account in: the graph has the cavern's ladder up, the dome's ladder
	// down and the gate from the south, so the pockets are the fallback and not the first choice.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the cavern to the bank", questplan.ObsCavernLanding, bank}, {"the dome to the bank", questplan.ObsDomeArrive, bank},
		{"the keep to the bank", questplan.ObsSackStand, bank},
	} {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route in the graph (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-40s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
}

// The regions the pockets name are the regions the pack makes them: every tile reached on foot from the sack is in the keep, every tile
// reached on foot from the dome's ladder is in the dome, and no tile of either is reached on foot from the side the account comes from.
func TestObservatoryThePocketsAreTheRegionsOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	p, _ := mcPlanner(t)
	keep := mcReachable(p, questplan.ObsSackStand, 2380, 2400, 9440, 9465, true)
	t.Logf("the keep reaches %d tiles", len(keep))
	if len(keep) < 6 || len(keep) > 60 {
		t.Fatalf("the keep reaches %d tiles on foot from the sack", len(keep))
	}
	for _, k := range keep {
		if !questplan.InObsKeep(k) {
			t.Errorf("%v is reached from the sack on foot and is not in InObsKeep", k)
		}
	}
	if len(keep) != 16 {
		t.Errorf("the keep is %d tiles, the plan says 16 (x 2389..2394, z 9453..9457)", len(keep))
	}
	for _, o := range mcReachable(p, questplan.ObsGateNorth, 2380, 2400, 9440, 9465, true) {
		if questplan.InObsKeep(o) {
			t.Errorf("%v is reached on foot from the gate's north side and is in the keep", o)
		}
	}
	dome := mcReachable(p, questplan.ObsDomeArrive, 2425, 2455, 3145, 3175, true)
	t.Logf("the dome reaches %d tiles", len(dome))
	if len(dome) < 30 {
		t.Fatalf("the dome reaches %d tiles on foot from its ladder", len(dome))
	}
	for _, c := range dome {
		if !questplan.InObsDome(c) {
			t.Errorf("%v is reached from the dome's ladder and is not in InObsDome", c)
		}
	}
	for _, o := range mcReachable(p, questplan.ObsProfessorAnchor, 2425, 2455, 3145, 3175, true) {
		if questplan.InObsDome(o) {
			t.Errorf("%v is reached on foot from the reception and is in the dome", o)
		}
	}
	// Every spawn and stand of the plan is a floor tile.
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the professor's", questplan.ObsProfessorAnchor}, {"the reception ladder's stand", questplan.ObsReceptionLadderStand}, {"the landing", questplan.ObsCavernLanding},
		{"the key chest's stand", questplan.ObsKeyChestStand}, {"the gate's north", questplan.ObsGateNorth}, {"the gate's south", questplan.ObsGateSouth},
		{"the guard's spawn", questplan.ObsGuardSpawn}, {"the sack's stand", questplan.ObsSackStand}, {"the dome ladder's foot", questplan.ObsDomeLadderFoot},
		{"the dome's arrival", questplan.ObsDomeArrive}, {"the sand spawn", questplan.ObsSandSpawn}, {"the dome professor's", questplan.ObsDomeProfessor},
		{"the furnace's west stand", tile.Tile{X: 2600, Z: 3310}}, {"the range's stand", tile.Tile{X: 2616, Z: 3317}},
		{"the planks' middle tile", questplan.ObsPlankCentre}, {"the fourth plank's", questplan.ObsPlankSpawns[3]},
		// The tiles the content's crossings set the account on (p_telejump 0_37_147_52_51, ~climb_ladder 0_38_49_12_56, 0_38_49_8_29 and
		// 0_37_147_55_30; the gate's p_teleport of one tile is the gate's two): an exact move onto an unwalkable tile strands an account.
		{"the cavern's landing (0_37_147_52_51)", tile.Tile{X: 2420, Z: 9459}}, {"the reception's (0_38_49_12_56)", tile.Tile{X: 2444, Z: 3192}},
		{"the dome's (0_38_49_8_29)", tile.Tile{X: 2440, Z: 3165}}, {"the dome ladder down's (0_37_147_55_30)", tile.Tile{X: 2423, Z: 9438}},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not a walkable tile on the real pack", c.what, c.at)
		}
	}
	for _, at := range append([]tile.Tile{questplan.ObsSandSpawn}, questplan.ObsPlankSpawns...) {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}); len(tiles) == 0 {
			t.Errorf("the ground spawn at %v can be taken from no tile", at)
		}
	}
}

// The goblin guard can be struck from where the quest stands: a melee attack reaches his spawn from the gate's north side by a walk
// over open ground, so the fight is not the kind that sends "I can't reach that!" for its whole budget.
func TestObservatoryTheGuardIsReachedByAMeleeAttackFromTheGate(t *testing.T) {
	pk, _, _ := csData(t)
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.ObsGateNorth.X, Z: questplan.ObsGateNorth.Z, Level: questplan.ObsGateNorth.Level}, goblinReachWindow, nil)
	if reach.Size() == 0 {
		t.Fatal("no open ground round the gate's north side")
	}
	steps := interactSteps(pk, reach, questplan.ObsGateNorth, questplan.ObsGuardSpawn)
	if steps < 0 || steps > 4 {
		t.Errorf("the guard's spawn %v is %d steps from a tile a melee attack reaches it from, standing at %v", questplan.ObsGuardSpawn, steps, questplan.ObsGateNorth)
	}
	// From inside the keep the gate's wall is between: no melee tile of his is reached on foot, which is why he is killed first.
	inside := pk.ReachFrom(pathfind.NavPoint{X: questplan.ObsSackStand.X, Z: questplan.ObsSackStand.Z, Level: questplan.ObsSackStand.Level}, goblinReachWindow, nil)
	if n := interactSteps(pk, inside, questplan.ObsSackStand, questplan.ObsGuardSpawn); n >= 0 {
		t.Errorf("the guard's spawn is %d steps from the sack on foot: the keep's gate does not wall it off", n)
	}
}

// The quest's pockets are registered, and the surface tiles are in none.
func TestObservatoryPocketsAreRegistered(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{{questplan.ObsSackStand, true}, {questplan.ObsDomeArrive, true}, {questplan.ObsTelescopeAt, true}, {questplan.ObsGateNorth, false},
		{questplan.ObsCavernLanding, false}, {questplan.ObsProfessorAnchor, false}, {questplan.EastArdougneNorth, false}} {
		name, in := questrunner.PocketAt(c.at)
		if in != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, in, c.want)
		}
	}
}
