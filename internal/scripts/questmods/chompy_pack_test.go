package questmods

// Big Chompy Bird Hunting on the real data: the production config, the baked loc index, the collision pack and the web graph (RSB0T_CONFIG_JAG,
// RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan carries names what it says in
// the config the fleet's client reads and offers the op the plan sends, whether the customs' own loc queries find their locs where the plan says and each has a
// tile to stand on, whether every leg the quest walks is a route the planner finds on the real pack, whether the routes keep clear of the wolves, and whether
// the wolves, the toads and the bait clearing are ground the account can fight, catch and bait on.

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestChompyIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjChLogs: "Achey tree logs", questplan.ObjChShaft: "Ogre arrow shaft", questplan.ObjChFlighted: "Flighted ogre arrow", questplan.ObjChWolfBones: "Wolf bones",
		questplan.ObjChTips: "Wolfbone arrowtips", questplan.ObjChArrow: "Ogre arrow", questplan.ObjChBow: "Ogre bow", questplan.ObjChBellowsEmpty: "Ogre bellows",
		questplan.ObjChBellows3: "Ogre bellows (3)", questplan.ObjChBellows2: "Ogre bellows (2)", questplan.ObjChBellows1: "Ogre bellows (1)", questplan.ObjChToad: "Bloated toad",
		questplan.ObjChRawChompy: "Raw chompy", questplan.ObjChRuinedChompy: "Ruined chompy", questplan.ObjChSeasonedChomp: "Seasoned chompy", questplan.ObjChFeather: "Feather",
		questplan.ObjChChisel: "Chisel", questplan.ObjKnife: "Knife", questplan.ObjChPotato: "Potato", questplan.ObjChOnion: "Onion", questplan.ObjChCabbage: "Cabbage",
		questplan.ObjChTomato: "Tomato", questplan.ObjChDoogle: "Doogle leaves", questplan.ObjChEqua: "Equa leaves", questplan.CoinsID: "Coins", questplan.ObjShark: "Shark",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// What is worn: the bow and the arrows offer Wield as their second op, and the bloated toad's first is Drop (the placement op).
	for _, id := range []int{questplan.ObjChBow, questplan.ObjChArrow} {
		o, _ := cfg.Obj(id)
		if cachecfg.OpIndex(o.IOps, "Wield") != 2 {
			t.Errorf("obj %d (%s) offers %v, want Wield as the second", id, o.Name, o.IOps)
		}
	}
	if o, _ := cfg.Obj(questplan.ObjChToad); cachecfg.OpIndex(o.IOps, "Drop") != 1 {
		t.Errorf("the bloated toad offers %v, want Drop first", o.IOps)
	}
	for id, want := range map[int]string{
		questplan.NpcChRantz: "Rantz", questplan.NpcChFycie: "Fycie", questplan.NpcChBugs: "Bugs", questplan.NpcChToad: "Swamp toad", questplan.NpcChBloatedToad: "Bloated Toad",
		questplan.NpcChChompy: "Chompy bird", questplan.NpcChChompyDead: "Chompy bird", questplan.NpcFeldipWolf: "Wolf",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	for _, c := range []struct {
		id  int
		op  string
		pos int
	}{{questplan.NpcChRantz, "Talk", 1}, {questplan.NpcChBugs, "Talk", 1}, {questplan.NpcChFycie, "Talk", 1}, {questplan.NpcChChompy, "Attack", 5},
		{questplan.NpcChChompyDead, "Pluck", 4}, {questplan.NpcFeldipWolf, "Attack", 2}} {
		n, _ := cfg.Npc(c.id)
		if cachecfg.OpIndex(n.Ops, c.op) != c.pos {
			t.Errorf("npc %d (%s) offers %v, want %q as op %d", c.id, n.Name, n.Ops, c.op, c.pos)
		}
	}
	for _, stop := range []questplan.NpcStop{questplan.ChRantzStop, questplan.ChBugsStop, questplan.ChFycieStop} {
		n, _ := cfg.Npc(stop.ID)
		if n.Name != stop.Npc {
			t.Errorf("stop %q: npc %d is %q", stop.Npc, stop.ID, n.Name)
		}
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
		pos      int
	}{
		{questplan.LocChChestShut, "Locked Ogre chest", "Unlock", 2, 1, 1}, {questplan.LocChChestOpen, "Unlocked Ogre chest", "Search", 2, 1, 1},
		{questplan.LocChSpitEmpty, "Ogre spit-roast", "", 2, 1, 0}, {questplan.LocChBubbles, "Swamp bubbles", "", 1, 1, 0}, {questplan.LocChAchey, "Achey Tree", "Chop", 2, 2, 1},
		{questplan.LocChPotato, "Potato", "Pick", 1, 1, 2}, {questplan.LocChCabbage, "cabbage", "Pick", 1, 1, 2}, {questplan.LocChOnion, "Onion", "Pick", 1, 1, 2},
		{questplan.LocChCaveMouth, "Cave entrance", "Enter", 3, 2, 1},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || l.Width != c.w || l.Length != c.l {
			t.Errorf("loc %d is %q %dx%d (found %v), the plan says %q %dx%d", c.id, l.Name, l.Width, l.Length, ok, c.name, c.w, c.l)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) != c.pos {
			t.Errorf("loc %d (%s) offers %v, want %q as op %d", c.id, l.Name, l.Ops, c.op, c.pos)
		}
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestChompyLocQueriesFindTheirLocsAndStands(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile
	}{
		{"the locked chest", questplan.LocChChestShut, "Unlock", questplan.ChChestAt, 4, 2637, 9398, 0, []tile.Tile{questplan.ChChestNear}},
		{"the swamp bubble", questplan.LocChBubbles, "", questplan.ChBubbleAt, 1, 2598, 2963, 0, []tile.Tile{questplan.ChBubbleStand}},
		{"the spit", questplan.LocChSpitEmpty, "", questplan.ChSpitAt, 4, 2630, 2990, 0, []tile.Tile{questplan.ChSpitNear}},
		{"the cave mouth", questplan.LocChCaveMouth, "Enter", tile.Tile{X: 2630, Z: 2998}, 6, 2629, 2998, 0, nil},
		{"the potato patch", questplan.LocChPotato, "Pick", questplan.ChPotatoPatch, 10, 2640, 2958, 0, nil},
		{"the cabbage", questplan.LocChCabbage, "Pick", questplan.ChCabbagePatch, 10, 2645, 2958, 0, nil},
		{"the onion patch", questplan.LocChOnion, "Pick", questplan.ChOnionPatch, 10, 2583, 2965, 0, nil},
		{"the achey grove", questplan.LocChAchey, "Chop", questplan.ChGrove, 14, 2626, 2974, 0, nil},
	} {
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
			if !d.Pack.Walkable(want.X, want.Z, want.Level) {
				t.Errorf("%s: the plan's stand %v is not walkable", c.what, want)
			}
		}
	}
	// Only the bubble at the pool's lip has a stand on ground the walker can finish on: its five neighbours have none (their stands are the pool).
	lipStands := 0
	for _, off := range [][2]int{{0, 2}, {2, 2}, {-1, 3}, {2, 4}, {1, 4}} {
		at := tile.Tile{X: questplan.ChBubbleAt.X + off[0], Z: questplan.ChBubbleAt.Z + off[1]}
		if m, ok := questrunner.FindLoc(d, questplan.LocChBubbles, "", "", at, 0); ok {
			for _, s := range questrunner.LocStands(d, m, at) {
				if d.Pack.Walkable(s.X, s.Z, s.Level) {
					lipStands++
				}
			}
		}
	}
	t.Logf("swamp bubbles other than the lip's with a walkable stand: %d tile(s)", lipStands)
	// The open chest is the live form of the shut one: the same footprint, found by its own id once the rock is off (live loc change), never in the baked index.
	open, _ := d.Config.Loc(questplan.LocChChestOpen)
	if open.Width != 2 || open.Length != 1 {
		t.Errorf("the open chest is %dx%d", open.Width, open.Length)
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestChompyLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	opts := webwalk.Options{AllowSpecial: true, AllowNPC: true}
	bank, rantz := questplan.YanilleBank, questplan.ChRantzAnchor
	cave := tile.Tile{X: 2630, Z: 2997}
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"the bank to Rantz", bank, rantz}, {"Rantz to the bank", rantz, bank},
		{"Rantz to the achey grove", rantz, questplan.ChGrove}, {"the grove to Rantz", questplan.ChGrove, rantz},
		{"the grove to the cave mouth", questplan.ChGrove, cave}, {"Rantz to the cave mouth", rantz, cave},
		{"the cave's landing to the chest", tile.Tile{X: 2647, Z: 9379}, questplan.ChChestNear}, {"the chest to Bugs", questplan.ChChestNear, questplan.ChBugsAnchor},
		{"Bugs to Fycie", questplan.ChBugsAnchor, questplan.ChFycieAnchor}, {"Fycie to the cave's exit", questplan.ChFycieAnchor, tile.Tile{X: 2647, Z: 9378}},
		{"Rantz to the chest (through the cave)", rantz, questplan.ChChestNear}, {"the chest to Rantz", questplan.ChChestNear, rantz},
		{"Rantz to the pool's lip", rantz, questplan.ChBubbleStand}, {"the lip to the clearing", questplan.ChBubbleStand, questplan.ChBaitSpot},
		{"the clearing to Rantz", questplan.ChBaitSpot, rantz}, {"Rantz to the clearing", rantz, questplan.ChBaitSpot}, {"the clearing to the lip", questplan.ChBaitSpot, questplan.ChBubbleStand},
		{"the clearing to the spit", questplan.ChBaitSpot, questplan.ChSpitNear}, {"the spit to Rantz", questplan.ChSpitNear, rantz},
		{"Rantz to the wolves' field", rantz, questplan.ChWolfField}, {"the wolves' field to Rantz", questplan.ChWolfField, rantz},
		{"the wolves' field to the bank", questplan.ChWolfField, bank}, {"the bank to the wolves' field", bank, questplan.ChWolfField},
		{"Rantz to the potato patch", rantz, questplan.ChPotatoPatch}, {"the potato patch to the cabbage", questplan.ChPotatoPatch, questplan.ChCabbagePatch},
		{"the cabbage to the equa leaves", questplan.ChCabbagePatch, questplan.ChEquaSpawn}, {"the equa leaves to the onion patch", questplan.ChEquaSpawn, questplan.ChOnionPatch},
		{"the onion patch to the tomato", questplan.ChOnionPatch, questplan.ChTomatoSpawn}, {"the tomato to the doogle leaves", questplan.ChTomatoSpawn, questplan.ChDoogleSpawn},
		{"the doogle leaves to the spit", questplan.ChDoogleSpawn, questplan.ChSpitNear}, {"the doogle leaves to Rantz", questplan.ChDoogleSpawn, rantz},
		// The shops of the kit: the feathers' (Port Sarim) and the axe's (Aemad's, in Ardougne), from the bank and from Rantz.
		{"the bank to Gerrant", bank, questplan.FluffsSardineShop.Anchor}, {"Gerrant to Rantz", questplan.FluffsSardineShop.Anchor, rantz},
		{"the bank to Aemad", bank, questplan.AemadShop.Anchor}, {"Aemad to Rantz", questplan.AemadShop.Anchor, rantz},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), opts)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-40s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
}

// chHostileLegs are the legs of the quest that start or end outside the enclave, by name: the pool, the west patches and the road from the bank.
func chHostileLegs() []struct {
	name     string
	from, to tile.Tile
} {
	rantz, bank, clearing := questplan.ChRantzAnchor, questplan.YanilleBank, questplan.ChBaitSpot
	return []struct {
		name     string
		from, to tile.Tile
	}{
		{"the bank to Rantz", bank, rantz}, {"Rantz to the bank", rantz, bank},
		{"Rantz to the pool's lip", rantz, questplan.ChBubbleStand}, {"the lip to Rantz", questplan.ChBubbleStand, rantz},
		{"the lip to the clearing", questplan.ChBubbleStand, clearing}, {"the clearing to the lip", clearing, questplan.ChBubbleStand},
		{"the cabbage to the equa leaves", questplan.ChCabbagePatch, questplan.ChEquaSpawn},
		{"the equa leaves to the onion patch", questplan.ChEquaSpawn, questplan.ChOnionPatch}, {"the onion patch to the tomato", questplan.ChOnionPatch, questplan.ChTomatoSpawn},
		{"the tomato to the doogle leaves", questplan.ChTomatoSpawn, questplan.ChDoogleSpawn}, {"the doogle leaves to the spit", questplan.ChDoogleSpawn, questplan.ChSpitNear},
		{"Rantz to the onion patch", rantz, questplan.ChOnionPatch}, {"the onion patch to Rantz", questplan.ChOnionPatch, rantz},
		{"Rantz to the tomato", rantz, questplan.ChTomatoSpawn}, {"Rantz to the doogle leaves", rantz, questplan.ChDoogleSpawn}, {"the doogle leaves to Rantz", questplan.ChDoogleSpawn, rantz},
		{"the clearing to the doogle leaves", clearing, questplan.ChDoogleSpawn}, {"the doogle leaves to the clearing", questplan.ChDoogleSpawn, clearing},
		{"the wolves' field to Rantz", questplan.ChWolfField, rantz}, {"Rantz to the wolves' field", rantz, questplan.ChWolfField},
		{"the bank to the wolves' field", bank, questplan.ChWolfField}, {"the bank to the pool's lip", bank, questplan.ChBubbleStand},
	}
}

// chRouteTouchesAHostile reports how many waypoints of the route lie within a wolf's or an ogre's reach of its spawn, and the first of them.
func chRouteTouchesAHostile(r webwalk.Route) (n int, first tile.Tile) {
	for _, w := range r.Waypoints {
		if at := (tile.Tile{X: w.X, Z: w.Z, Level: w.Level}); questplan.ChHostileNear(at) {
			if n == 0 {
				first = at
			}
			n++
		}
	}
	return n, first
}

// The enclave is clear of every wolf and ogre of the hills (the reach of the easternmost spawn stops a column short of it), and the walks between its places stay in it: every
// ordered pair of the quest's places in the east plans on the real pack and graph with no waypoint within a hostile's reach. The comment this test replaced said the pool's lip was the
// one place the account meets the wolves, and checked only these legs; the legs below it, which it left out, are the ones that are not clear (TestChompyHostileLegsAreExposed).
func TestChompyEnclaveLegsKeepClearOfEveryHostile(t *testing.T) {
	p, pt := mcPlanner(t)
	opts := webwalk.Options{AllowSpecial: true, AllowNPC: true}
	places := []struct {
		name string
		at   tile.Tile
	}{
		{"Rantz", questplan.ChRantzAnchor}, {"the grove", questplan.ChGrove}, {"the clearing", questplan.ChBaitSpot}, {"the spit", questplan.ChSpitNear},
		{"the potato patch", questplan.ChPotatoPatch}, {"the cabbage patch", questplan.ChCabbagePatch}, {"the equa leaves", questplan.ChEquaSpawn}, {"the cave mouth", tile.Tile{X: 2630, Z: 2997}},
	}
	legs := 0
	for _, from := range places {
		if !questplan.ChInEnclave(from.at) {
			t.Errorf("%s at %v is outside the enclave", from.name, from.at)
		}
		for _, to := range places {
			if from.name == to.name {
				continue
			}
			legs++
			r := p.Plan(pt(from.at), pt(to.at), opts)
			if !r.OK {
				t.Errorf("%s to %s: no route (%s)", from.name, to.name, r.Reason)
				continue
			}
			if questplan.ChLegExposed(from.at, to.at) {
				t.Errorf("%s to %s is called exposed though both ends are in the enclave", from.name, to.name)
			}
			if n, first := chRouteTouchesAHostile(r); n != 0 {
				t.Errorf("%s to %s: %d waypoint(s) within a hostile's reach, the first at %v", from.name, to.name, n, first)
			}
		}
	}
	t.Logf("%d legs inside the enclave, none within a hostile's reach", legs)
	// The pool's lip is the exposed place of the wolves: the nearest wolf is within the reach of a wander.
	nearest := 1 << 30
	for _, w := range questplan.ChWolfSpawns {
		nearest = min(nearest, tile.Chebyshev(questplan.ChBubbleStand, w))
	}
	if nearest > questplan.ChWolfReach {
		t.Errorf("the pool's lip is %d tiles from the nearest wolf: the price need not cover it", nearest)
	}
}

// Every leg of the quest that really passes within a hostile's reach is called exposed, so the executor walks it with TravelCautious and a top-up (the predicate is conservative: it may
// call a leg exposed that keeps clear, never the other way). The legs the review named are among them: the way from the equa leaves to the onion patch has a waypoint on a wolf's own
// spawn tile, the way from Rantz to the doogle leaves one on an ogre's, and the onion patch and the tomato spawn are one tile from another ogre; and so is the road from the bank.
func TestChompyHostileLegsAreExposed(t *testing.T) {
	p, pt := mcPlanner(t)
	opts := webwalk.Options{AllowSpecial: true, AllowNPC: true}
	mustTouch := map[string]bool{
		"the bank to Rantz": true, "Rantz to the bank": true, "Rantz to the pool's lip": true, "the lip to Rantz": true, "the lip to the clearing": true, "the clearing to the lip": true,
		"the equa leaves to the onion patch": true, "Rantz to the doogle leaves": true, "the doogle leaves to Rantz": true, "the bank to the pool's lip": true,
		"the wolves' field to Rantz": true, "Rantz to the wolves' field": true,
	}
	touching, exposedOnly := 0, 0
	for _, l := range chHostileLegs() {
		r := p.Plan(pt(l.from), pt(l.to), opts)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		n, first := chRouteTouchesAHostile(r)
		exposed := questplan.ChLegExposed(l.from, l.to)
		switch {
		case n > 0 && !exposed:
			t.Errorf("%s: %d waypoint(s) within a hostile's reach (the first at %v) and the leg is not called exposed", l.name, n, first)
		case n > 0:
			touching++
		case exposed:
			exposedOnly++
		}
		if mustTouch[l.name] && n == 0 {
			t.Errorf("%s: no waypoint within a hostile's reach; the review found one", l.name)
		}
		t.Logf("%-38s waypoints %3d, within a hostile's reach %3d, exposed %v", l.name, len(r.Waypoints), n, exposed)
	}
	t.Logf("%d legs pass within a hostile's reach, %d more are called exposed without", touching, exposedOnly)
	// The waypoint on a wolf's spawn and the waypoint on an ogre's: the review's two examples, found on the real pack.
	onSpawn := func(from, to tile.Tile, spawns []tile.Tile) bool {
		r := p.Plan(pt(from), pt(to), opts)
		for _, w := range r.Waypoints {
			for _, sp := range spawns {
				if tile.Chebyshev(tile.Tile{X: w.X, Z: w.Z}, sp) <= 1 {
					return true
				}
			}
		}
		return false
	}
	if !onSpawn(questplan.ChEquaSpawn, questplan.ChOnionPatch, questplan.ChWolfSpawns) {
		t.Error("the way from the equa leaves to the onion patch no longer passes a wolf's spawn: the review's example moved")
	}
	if !onSpawn(questplan.ChRantzAnchor, questplan.ChDoogleSpawn, questplan.ChOgreSpawns) {
		t.Error("the way from Rantz to the doogle leaves no longer passes an ogre's spawn: the review's example moved")
	}
}

// The wolves are ground the account can fight: each spawn is a tile the engine accepts a melee attack on from the field's own flood, and the field's flood holds
// the pool's lip and the clearing (the hunt does not strand the account).
func TestChompyTheWolvesAreOnOpenGround(t *testing.T) {
	pk, _, _ := csData(t)
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.ChWolfField.X, Z: questplan.ChWolfField.Z}, fhReachWindow, nil)
	if reach.Size() == 0 {
		t.Fatalf("no open ground round %v", questplan.ChWolfField)
	}
	open := 0
	for _, w := range []tile.Tile{{X: 2602, Z: 2955}, {X: 2605, Z: 2963}, {X: 2607, Z: 2967}, {X: 2610, Z: 2958}, {X: 2610, Z: 2961}, {X: 2610, Z: 2965}} {
		if n := interactSteps(pk, reach, questplan.ChWolfField, w); n >= 0 {
			open++
		} else {
			t.Errorf("the wolf at %v is not a walk over open ground from the field", w)
		}
	}
	if open != 6 {
		t.Errorf("%d of 6 wolves reachable", open)
	}
	if _, ok := reach.Steps(questplan.ChBubbleStand.X, questplan.ChBubbleStand.Z, questplan.ChBubbleStand.Level); !ok {
		t.Errorf("the pool's lip %v is not on the wolves' open ground", questplan.ChBubbleStand)
	}
}

// The bait clearing is ground the account can stand on and be pushed off, a long way from the wolves; its middle is a walk from Rantz; it lies inside twenty tiles of him (he fires
// only within twenty).
func TestChompyTheBaitClearingIsWalkableGroundNearRantz(t *testing.T) {
	pk, _, _ := csData(t)
	walkable := 0
	for x := questplan.ChBaitZone.MinX; x <= questplan.ChBaitZone.MaxX; x++ {
		for z := questplan.ChBaitZone.MinZ; z <= questplan.ChBaitZone.MaxZ; z++ {
			if pk.Walkable(x, z, 0) {
				walkable++
			}
			if tile.Chebyshev(tile.Tile{X: x, Z: z}, questplan.ChRantzAnchor) >= 20 {
				t.Errorf("the clearing tile %d,%d is 20 or more tiles from Rantz: he would not fire", x, z)
			}
		}
	}
	if walkable < 40 {
		t.Errorf("%d walkable tiles in the 9 by 10 clearing", walkable)
	}
	if !pk.Walkable(questplan.ChBaitSpot.X, questplan.ChBaitSpot.Z, 0) || !questplan.ChInBaitZone(questplan.ChBaitSpot) {
		t.Errorf("the bait spot %v is not walkable ground in the clearing", questplan.ChBaitSpot)
	}
	// A chompy comes within ten tiles of the bait: the open ground round the spot is wide enough to run a bird down on.
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.ChBaitSpot.X, Z: questplan.ChBaitSpot.Z}, 12, nil)
	if reach.Size() < 100 {
		t.Errorf("only %d tiles of open ground within 12 of the bait", reach.Size())
	}
}

// Rantz's cave is joined to the world by the graph's own crossings in both directions (the cave mouth and the exits), the children and the chest stand inside one
// piece of ground, and the way in lands where the plan says.
func TestChompyTheCaveIsOnePieceOfGroundWithTheGraphsCrossings(t *testing.T) {
	pk, _, _ := csData(t)
	flood := pk.Flood(pathfind.NavPoint{X: 2647, Z: 9379}, 40)
	for _, c := range []struct {
		name string
		at   tile.Tile
	}{{"the chest's stand", questplan.ChChestNear}, {"Bugs", questplan.ChBugsAnchor}, {"Fycie", questplan.ChFycieAnchor}} {
		if !flood[[2]int{c.at.X, c.at.Z}] {
			t.Errorf("%s at %v is not on the ground the cave's landing reaches", c.name, c.at)
		}
		if !questplan.ChInCave(c.at) {
			t.Errorf("%s at %v is outside ChInCave", c.name, c.at)
		}
	}
	for k := range flood {
		if !questplan.ChInCave(tile.Tile{X: k[0], Z: k[1]}) {
			t.Errorf("%v of the cave is outside ChInCave", k)
		}
	}
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	p := webwalk.NewPlanner(pk, g)
	in := p.EdgesFrom(webwalk.Point{X: 2630, Z: 2997})
	out := p.EdgesFrom(webwalk.Point{X: 2647, Z: 9379})
	foundIn, foundOut := false, false
	for _, e := range in {
		foundIn = foundIn || (e.To.X == 2647 && e.To.Z == 9379)
	}
	for _, e := range out {
		foundOut = foundOut || (e.To.X == 2630 && e.To.Z == 2997)
	}
	if !foundIn || !foundOut {
		t.Errorf("the graph's crossings: in %v out %v (edges %d and %d)", foundIn, foundOut, len(in), len(out))
	}
}
