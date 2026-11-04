package questmods

// Tai Bwo Wannai Trio on the real data: the production config, the baked loc index, the collision pack and the web graph
// (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them, and the ones that read the maps' npc
// spawns also want RSB0T_CONTENT). The questions are whether every id the plan carries names what it says in the config the fleet's
// client reads, whether the customs' own loc queries find their locs where the plans say and each has a tile to stand on, whether
// every walk the quest makes is a route round the hunters on the real pack, whether the fishing spots have a shore to stand on, and
// whether the places the plan describes in its comments (the Jogre pen's loneliest Jogre, the sealed map of the hunt) are what the
// maps make them.

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestTbwtIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.TbObjNet: "Small fishing net", questplan.TbObjPestle: "Pestle and mortar", questplan.TbObjNatureRune: "Nature rune",
		questplan.TbObjSlicedBanana: "Sliced banana", questplan.TbObjRumSlicedBanana: "Karamjan rum", questplan.TbObjVessel: "Karambwan vessel",
		questplan.TbObjVesselLoaded: "Karambwan vessel", questplan.TbObjManual: "Crafting manual", questplan.TbObjRawKarambwanji: "Raw karambwanji",
		questplan.TbObjKarambwanjiPaste: "Karambwanji paste", questplan.TbObjRawKarambwan: "Raw karambwan", questplan.TbObjPoorKarambwan: "Cooked karambwan",
		questplan.TbObjBurntKarambwan: "Burnt karambwan", questplan.TbObjPoisonPaste: "Karambwan paste", questplan.TbObjMonkeyCorpse: "Monkey corpse",
		questplan.TbObjMonkeySkin: "Monkey skin", questplan.TbObjSandwich: "Seaweed sandwich", questplan.TbObjJogreBones: "Jogre bones",
		questplan.TbObjBurntJogreBones: "Burnt jogre bones", questplan.TbObjBurntBonesInPaste: "Pasty jogre bones", questplan.TbObjMarinatedBones: "Marinated j' bones",
		questplan.ObjKnife: "Knife", questplan.ObjSeaweed: "Seaweed", questplan.ObjFireRune: "Fire rune", questplan.ObjAirRune: "Air rune",
		questplan.ObjChaosRune: "Chaos rune", questplan.ObjBanana: "Banana", questplan.ObjKaramjaRum: "Karamjan rum", tbObjRawShrimp: "Raw shrimps",
		3032: "Agility potion(4)", 3034: "Agility potion(3)", 3036: "Agility potion(2)", 3038: "Agility potion(1)",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	for _, sp := range questplan.TbSpears {
		o, ok := cfg.Obj(sp.ID)
		k, kok := cfg.Obj(sp.PoisonID)
		if !ok || !kok || o.Name != sp.Name || k.Name != sp.Name+"(kp)" {
			t.Errorf("%s: obj %d is %q and obj %d is %q", sp.Name, sp.ID, o.Name, sp.PoisonID, k.Name)
		}
	}
	// Three names are shared by several objects, which is why every check of the module is by id: the three vessels and rums and pastes.
	byName := map[string][]int{}
	for _, id := range []int{questplan.TbObjVessel, questplan.TbObjVesselLoaded, questplan.ObjKaramjaRum, questplan.TbObjRumSlicedBanana, questplan.TbObjPoisonPaste} {
		o, _ := cfg.Obj(id)
		byName[o.Name] = append(byName[o.Name], id)
	}
	if len(byName["Karambwan vessel"]) != 2 || len(byName["Karamjan rum"]) != 2 {
		t.Errorf("the shared names moved: %v", byName)
	}
	// The bones carry a Bury op: no step of this module buries them, and the keep list keeps them out of the eat floor's reach.
	for _, id := range []int{questplan.TbObjJogreBones, questplan.TbObjBurntJogreBones, questplan.TbObjBurntBonesInPaste, questplan.TbObjMarinatedBones} {
		if o, _ := cfg.Obj(id); cachecfg.OpIndex(actions.HeldOps(o.IOps), "Bury") == 0 {
			t.Errorf("obj %d (%s) offers %v: the plan's comments say a Bury op", id, o.Name, o.IOps)
		}
	}
	for id, c := range map[int]struct {
		name  string
		level int
		size  int
		op    string
	}{
		questplan.TbNpcTimfraku: {"Timfraku", 0, 1, "Talk-to"}, questplan.TbNpcTiadeche: {"Tiadeche", 0, 1, "Talk-to"}, questplan.TbNpcTinsay: {"Tinsay", 0, 1, "Talk-to"},
		questplan.TbNpcTamayu: {"Tamayu", 0, 1, "Talk-to"}, questplan.TbNpcLubufu: {"Lubufu", 0, 1, "Talk-to"}, questplan.TbNpcShaikahan: {"The Shaikahan", 83, 3, "Attack"},
		questplan.TbNpcMonkey: {"Monkey", 3, 1, "Attack"}, questplan.TbNpcJiminua: {"Jiminua", 0, 1, "Talk-to"}, questplan.TbNpcKarambwanjiSpot: {"Fishing spot", 0, 1, "Net"},
		questplan.TbNpcKarambwanSpotA: {"Fishing spot", 0, 1, "Fish"}, questplan.TbNpcKarambwanSpotB: {"Fishing spot", 0, 1, "Fish"},
		questplan.TbNpcKarambwanSpotC: {"Fishing spot", 0, 1, "Fish"}, questplan.NpcJogre: {"Jogre", 53, 2, "Attack"},
		questplan.NpcJungleSpider: {"Jungle spider", 44, 0, "Attack"}, questplan.NpcTribesman: {"Tribesman", 32, 0, "Attack"}, questplan.NpcGuardDog: {"Guard dog", 44, 0, "Attack"},
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != c.name || n.Level != c.level || (c.size > 0 && n.Size != c.size) || !strings.Contains(strings.Join(n.Ops[:], "|"), c.op) {
			t.Errorf("npc %d is %q level %d size %d ops %v (found %v), the plan says %q level %d size %d with %s", id, n.Name, n.Level, n.Size, n.Ops, ok, c.name, c.level, c.size, c.op)
		}
	}
	for _, c := range []struct {
		id       int
		name, op string
	}{
		{questplan.TbLocFire, "Fire", ""}, {questplan.TbLocLadder, "Ladder", "Climb-up"}, {2073, "Banana Tree", "Search"},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || (c.op != "" && !strings.Contains(strings.Join(l.Ops[:], "|"), c.op)) {
			t.Errorf("loc %d is %q %v (found %v), the plan says %q %q", c.id, l.Name, l.Ops, ok, c.name, c.op)
		}
	}
	for _, id := range questplan.BananaTreeLocs {
		if l, ok := cfg.Loc(id); !ok || l.Name != "Banana Tree" {
			t.Errorf("banana tree loc %d is %q", id, l.Name)
		}
	}
	// The spells the module casts exist under the names it uses, with the runes and the levels it provisions for.
	if _, ok := actions.CombatSpellComponent("Wind Bolt"); !ok {
		t.Error("Wind Bolt is no combat spell the client knows")
	}
	if _, ok := actions.ItemSpellComponent("Superheat Item"); !ok {
		t.Error("Superheat Item is no item spell the client knows")
	}
	if s, ok := actions.SpellByName("Wind Bolt"); !ok || s.Level != 17 || len(s.Runes) != 2 {
		t.Errorf("Wind Bolt: %+v", s)
	} else {
		for _, r := range s.Runes {
			if (r.Name != "Air rune" || r.Count != 2) && (r.Name != "Chaos rune" || r.Count != 1) {
				t.Errorf("Wind Bolt takes %d %s: the kit asks for two air runes and a chaos rune a cast", r.Count, r.Name)
			}
		}
	}
}

// The customs' own loc queries find their locs where the plans say, and each has a tile to stand on.
func TestTbwtLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
	}{
		{"the jungle fire's west tile", questplan.TbLocFire, "", questplan.TbFire, 6, 2789, 3048, 0},
		{"the ladder to Timfraku's floor", questplan.TbLocLadder, "Climb-up", tile.Tile{X: 2782, Z: 3087}, 4, 2782, 3087, 0},
	} {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d near %v", c.what, c.id, c.near)
			continue
		}
		if m.X != c.x || m.Z != c.z || m.Level != c.lvl {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		walkable := 0
		stands := questrunner.LocStands(d, m, c.near)
		for _, s := range stands {
			if d.Pack.Walkable(s.X, s.Z, s.Level) {
				walkable++
			}
		}
		if walkable == 0 {
			t.Errorf("%s at %d,%d has no walkable tile the engine accepts an op from (%v)", c.what, m.X, m.Z, stands)
		}
	}
	// The jungle fire is two tiles wide, and the second tile is the same loc id.
	if m, ok := questrunner.FindLoc(d, questplan.TbLocFire, "", "", tile.Tile{X: 2790, Z: 3048}, 0); !ok || m.X != 2790 {
		t.Errorf("the fire's east tile: %+v %v", m, ok)
	}
	// The grove has bearing trees (the same table Pirate's Treasure picks from) within reach of the grove's middle.
	found := 0
	for _, id := range questplan.BananaTreeLocs {
		if _, ok := questrunner.FindLoc(d, id, "", "Search", questplan.BananaGrove, 12); ok {
			found++
		}
	}
	if found == 0 {
		t.Errorf("no bearing banana tree within 12 of %v", questplan.BananaGrove)
	}
	// Every fishing spot has a shore the engine accepts an op on it from, and the lake's spots are on the plan's tiles.
	for _, at := range append(append([]tile.Tile(nil), questplan.TbLakeShoals...), tile.Tile{X: 2768, Z: 3165}, tile.Tile{X: 2912, Z: 3119}, tile.Tile{X: 2899, Z: 3119}) {
		stands := fluffsStands(d, at)
		ok := false
		for _, s := range stands {
			if d.Pack.Walkable(s.X, s.Z, s.Level) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("the fishing spot at %v has no walkable shore tile (%v)", at, stands)
		}
	}
}

// tbTargets are every tile a walk of the quest aims at.
func tbTargets() map[string]tile.Tile {
	out := map[string]tile.Tile{
		"Timfraku": questplan.TbTimfrakuAnchor, "Lubufu": questplan.TbLubufuAnchor, "Tiadeche": questplan.TbTiadecheAnchor, "Tamayu": questplan.TbTamayuAnchor,
		"Tinsay": questplan.TbTinsayAnchor, "the monkeys": questplan.TbMonkeys, "the jungle fire": questplan.TbFire, "the Jogre pen": questplan.TbJogreStage,
		"Zambo": questplan.ZamboShop.Anchor, "the banana grove": questplan.BananaGrove, "Lubufu's Karambwan spot": {X: 2768, Z: 3165}, "Jiminua": questplan.TbJiminuaShop.Anchor,
	}
	for i, s := range questplan.TbLakeShoals {
		out["the lake's shoal "+string(rune('A'+i))] = s
	}
	return out
}

// None of the tiles a walk aims at is inside a rectangle the walks keep out of, and the ground a walk starts on is not either: the
// planner never steps into a zone from outside, so a destination inside one is unreachable.
func TestTbwtTheWalkTargetsLieOutsideTheAvoidedGround(t *testing.T) {
	rects := tbAvoidRects()
	starts := tbTargets()
	starts["the ship's gangplank at Musa Point"] = tile.Tile{X: 2954, Z: 3147}
	for name, at := range starts {
		for _, r := range rects {
			if r.Contains(webwalk.Point{X: at.X, Z: at.Z, Level: at.Level}) {
				t.Errorf("%s (%v) lies inside the avoided rectangle %+v", name, at, r)
			}
		}
	}
	if want := len(questplan.TbwtHostiles()); len(rects) != want || want < 80 {
		t.Errorf("%d avoided rectangles for %d hostiles", len(rects), want)
	}
	// The rectangles reach the walker as its own type, one for each hunter on the ground level, in the plan's order.
	for i, r := range questplan.TbwtAvoid() {
		w := rects[i]
		if r.MinX != w.MinX || r.MaxX != w.MaxX || r.MinZ != w.MinZ || r.MaxZ != w.MaxZ || r.Level != w.Level {
			t.Fatalf("rectangle %d: %+v, plan %+v", i, w, r)
		}
	}
}

// Every walk the quest makes on the island exists round the hunters: from where the ships and the banks put the account down, from the
// village to each stand, and back. This is the check that "avoid the hunters" is a route and not a wish.
func TestTbwtEveryWalkExistsRoundTheHunters(t *testing.T) {
	p, pt := mcPlanner(t)
	zones := tbAvoidRects()
	targets := tbTargets()
	sources := map[string]tile.Tile{
		"the East Ardougne bank": questplan.ArdougneSouthBank, "Port Sarim": {X: 3029, Z: 3217}, "Musa Point": {X: 2954, Z: 3147}, "the village": {X: 2800, Z: 3085},
	}
	plan := func(from, to tile.Tile) webwalk.Route {
		return p.Plan(pt(from), pt(to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
	}
	for name, to := range targets {
		for from, at := range sources {
			if at == to {
				continue
			}
			r := plan(at, to)
			if !r.OK {
				t.Errorf("%s to %s: no route round the hunters (%s)", from, name, r.Reason)
				continue
			}
			t.Logf("%-22s to %-24s cost %4d", from, name, r.Cost)
		}
		for _, back := range []struct {
			n string
			t tile.Tile
		}{{"Timfraku", questplan.TbTimfrakuAnchor}, {"the Ardougne bank", questplan.ArdougneSouthBank}} {
			if back.t == to {
				continue
			}
			if r := plan(to, back.t); !r.OK {
				t.Errorf("%s back to %s: no route round the hunters (%s)", name, back.n, r.Reason)
			}
		}
	}
	// The legs between stands that the quest runs in a row: the brothers in the order of the legs, and the fire between them.
	legs := [][2]string{{"Timfraku", "the lake's shoal A"}, {"the lake's shoal A", "Lubufu"}, {"Lubufu", "Tiadeche"}, {"Tiadeche", "Tamayu"}, {"Tamayu", "the jungle fire"},
		{"the jungle fire", "Tamayu"}, {"Tamayu", "Tiadeche"}, {"Tiadeche", "Zambo"}, {"Zambo", "the banana grove"}, {"the banana grove", "Tinsay"}, {"Tinsay", "the monkeys"},
		{"the monkeys", "Tamayu"}, {"Tamayu", "Tinsay"}, {"Tinsay", "the Jogre pen"}, {"the Jogre pen", "the jungle fire"}, {"the jungle fire", "Tinsay"}, {"Tinsay", "Lubufu"},
		{"Lubufu", "Tinsay"}, {"Lubufu", "Tiadeche"}, {"Tiadeche", "Timfraku"}}
	for _, l := range legs {
		if r := plan(targets[l[0]], targets[l[1]]); !r.OK {
			t.Errorf("%s to %s: no route round the hunters (%s)", l[0], l[1], r.Reason)
		}
	}
	// And the plain walks cross the hunters' ground, which is why there are rectangles at all: the Jogre pen from Tamayu.
	plain := p.Plan(pt(targets["Tamayu"]), pt(targets["the Jogre pen"]), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	avoided := plan(targets["Tamayu"], targets["the Jogre pen"])
	if !plain.OK || !avoided.OK || avoided.Cost < plain.Cost {
		t.Errorf("the pen's plain route costs %d and the avoiding one %d (%v %v)", plain.Cost, avoided.Cost, plain.OK, avoided.OK)
	}
}

// No walk of the quest, from the village or the ships to a stand, passes within a spider's, tribesman's, dog's or Jogre's own box (the
// rectangles are the walker's: this checks the routes it found against the plan's own rectangles, tile by tile).
func TestTbwtTheRoutesStayOutOfTheRectangles(t *testing.T) {
	p, pt := mcPlanner(t)
	zones := tbAvoidRects()
	for name, to := range tbTargets() {
		r := p.Plan(pt(questplan.TbTimfrakuAnchor), pt(to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
		if !r.OK {
			continue // the previous test reports it
		}
		for _, c := range tilesOnRoute(r, 0) {
			for _, z := range zones {
				if z.Contains(webwalk.Point{X: c.X, Z: c.Z, Level: c.Level}) {
					t.Errorf("the route from Timfraku to %s passes %v, inside %+v", name, c, z)
					break
				}
			}
		}
	}
}

// The comments about the Jogre pen are the maps': the target is the loneliest Jogre (the nearest other is seven tiles off or more),
// and the nearest spider is fourteen tiles from it.
func TestTbwtTheJogrePenIsWhatThePlanSays(t *testing.T) {
	spawns := spawnsOf(t, 2890, 2950, 3020, 3080)
	target := questplan.TbJogreTarget
	nearestJogre, nearestSpider := 1<<30, 1<<30
	for _, j := range spawns[questplan.NpcJogre] {
		if j != target {
			nearestJogre = min(nearestJogre, tile.Chebyshev(j, target))
		}
	}
	for _, s := range spawns[questplan.NpcJungleSpider] {
		nearestSpider = min(nearestSpider, tile.Chebyshev(s, target))
	}
	if nearestJogre < 7 || nearestSpider < 14 {
		t.Errorf("the target's nearest other Jogre is %d tiles off and its nearest spider %d: the plan says 7 and 14", nearestJogre, nearestSpider)
	}
	// Every other Jogre of the pen has a neighbour within five tiles or is the second-loneliest: the pick is worth making.
	crowded := 0
	for _, j := range spawns[questplan.NpcJogre] {
		for _, o := range spawns[questplan.NpcJogre] {
			if o != j && tile.Chebyshev(o, j) <= tbCrowd {
				crowded++
				break
			}
		}
	}
	if crowded < 4 {
		t.Errorf("only %d of the pen's Jogres have a neighbour within %d tiles", crowded, tbCrowd)
	}
	// The stage is on walkable ground, ten tiles from the target and in line of the target's approach.
	_, _, _ = csData(t)
	d := csDeps(t)
	if !d.Pack.Walkable(questplan.TbJogreStage.X, questplan.TbJogreStage.Z, 0) {
		t.Errorf("the pen's stage %v is not walkable", questplan.TbJogreStage)
	}
	if got := tile.Chebyshev(questplan.TbJogreStage, target); got < 8 || got > 12 {
		t.Errorf("the stage is %d tiles from the target", got)
	}
	// The fight walks the last stretch over the hunters' ground, so a plain route from the stage to the target must exist.
	p, pt := mcPlanner(t)
	if r := p.Plan(pt(questplan.TbJogreStage), pt(target), webwalk.Options{AllowSpecial: true, AllowNPC: true}); !r.OK {
		t.Errorf("no route from the stage to the target Jogre: %s", r.Reason)
	}
}

// The hunt's sealed map is not ground the surface graph reaches, and the places it carries the account to and from are the plan's: the
// account lands inside InTbwtCutscene (both camera positions) and comes back beside Tamayu on walkable ground.
func TestTbwtTheCutsceneIsASealedMapAndComesBackBesideTamayu(t *testing.T) {
	p, pt := mcPlanner(t)
	for _, at := range []tile.Tile{{X: 2496 + 24, Z: 4544 + 24}, {X: 2496 + 41, Z: 4544 + 24}} {
		if !questplan.InTbwtCutscene(at) {
			t.Errorf("the cutscene's landing %v is outside InTbwtCutscene", at)
		}
		if r := p.Plan(pt(questplan.TbTamayuAnchor), pt(at), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("the planner finds a walk from Tamayu to the cutscene's landing %v (cost %d): the map is not sealed", at, r.Cost)
		}
	}
	back := tile.Tile{X: 44*64 + 32, Z: 47*64 + 31}
	d := csDeps(t)
	if !d.Pack.Walkable(back.X, back.Z, 0) || tile.Chebyshev(back, questplan.TbTamayuAnchor) > 10 {
		t.Errorf("the hunt returns the account to %v, which is not walkable ground beside Tamayu", back)
	}
	if questplan.InTbwtCutscene(back) || questplan.InTbwtCutscene(questplan.TbTamayuAnchor) {
		t.Error("the island is inside InTbwtCutscene")
	}
}
