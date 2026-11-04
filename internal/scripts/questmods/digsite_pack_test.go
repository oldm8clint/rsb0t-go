package questmods

// The Digsite Quest on the real data: the production config, the baked loc index, the collision pack and the web graph (RSB0T_CONFIG_JAG,
// RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan carries names what it says
// in the config the fleet's client reads, whether the customs' own loc queries find their locs where the plan says and each has a tile the
// engine accepts the op from, whether every leg the quest walks is a route the planner finds on the real pack (and the shafts the one
// it cannot), whether the four caves' rectangles are the regions the pack makes them, and whether the skeletons the quest prices stand where the
// plan says they do.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestDigsiteIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.DigObjSpecimenJar: "Specimen jar", questplan.DigObjSpecimenBrush: "Specimen brush", questplan.DigObjSampleGreen: "Rock sample 1",
		questplan.DigObjSampleOrange: "Rock sample 2", questplan.DigObjSamplePurple: "Rock sample 3", questplan.DigObjTrowel: "Trowel",
		questplan.DigObjTrayEmpty: "Panning tray", questplan.DigObjTrayMud: "Panning tray", questplan.DigObjTalisman: "Talisman of zaros",
		questplan.DigObjPlainLetter: "Unstamped letter", questplan.DigObjStampedLetter: "Stamped letter", questplan.DigObjCert1: "Level 1 certificate",
		questplan.DigObjExpertScroll: "Invitation letter", questplan.DigObjStoneTablet: "Stone tablet", questplan.DigObjPowder: "Chemical powder",
		questplan.DigObjNitrate: "Ammonium nitrate", questplan.DigObjLiquid: "Unidentified liquid", questplan.DigObjNitro: "Nitroglycerin",
		questplan.DigObjGroundCharcoal: "Ground charcoal", questplan.DigObjPreMix: "Mixed chemicals", questplan.DigObjPostMix: "Mixed chemicals",
		questplan.DigObjCompound: "Chemical compound", questplan.DigObjArceniaRoot: "Arcenia root", questplan.DigObjChestKey: "Chest key",
		questplan.DigObjVial: "Vial", questplan.DigObjPestle: "Pestle and mortar", questplan.DigObjTinderbox: "Tinderbox", questplan.DigObjCharcoal: "Charcoal",
		questplan.DigObjRope: "Rope", questplan.DigObjChisel: "Chisel", questplan.DigObjOpal: "Opal", questplan.DigObjUncutOpal: "Uncut opal",
		questplan.DigObjTea: "Cup of tea",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// The ops the executors send: the tray's Search (op 2), the Drop default that the junk list is dropped with, the tea's Drink.
	if o, _ := cfg.Obj(questplan.DigObjTrayMud); cachecfg.OpIndex(o.IOps, "Search") != 2 {
		t.Errorf("the full tray offers %v: Search is expected as the second op", o.IOps)
	}
	for _, id := range append(questplan.DigJunkIDs(), questplan.DigVolatileIDs()...) {
		if o, ok := cfg.Obj(id); !ok || o.Noted() || o.Name == "" {
			t.Errorf("object %d of the junk or volatile list is %q (found %v)", id, o.Name, ok)
		}
	}
	for id, want := range map[int]string{
		questplan.DigNpcWorkman: "Digsite workman", questplan.DigNpcCaveWorkman: "Digsite workman", questplan.DigNpcGreen: "Student", questplan.DigNpcOrange: "Student",
		questplan.DigNpcPurple: "Student", questplan.DigNpcExaminer: "Examiner", questplan.DigNpcExpert: "Archaeological expert", questplan.DigNpcGuide: "Panning guide",
		questplan.DigNpcCurator: "Curator", questplan.DigNpcTeaSeller: "Tea seller", questplan.PlagueNpcJatix: "Jatix", questplan.NpcVarrockGenKpr: "Shop keeper",
		questplan.NpcManorSkeleton: "Skeleton",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	for _, id := range []int{questplan.DigNpcWorkman, questplan.DigNpcCaveWorkman} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Steal-from") != 3 {
			t.Errorf("workman %d offers %v: Steal-from is expected as the third op", id, n.Ops)
		}
	}
	for _, id := range []int{questplan.DigNpcExaminer, questplan.DigNpcExpert, questplan.DigNpcCurator, questplan.DigNpcGuide, questplan.DigNpcGreen, questplan.DigNpcOrange, questplan.DigNpcPurple} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Talk-to") != 1 || cachecfg.OpIndex(n.Ops, "Attack") != 0 {
			t.Errorf("npc %d (%s) offers %v: the quest talks to it and never attacks", id, n.Name, n.Ops)
		}
	}
	for _, id := range []int{questplan.DigNpcTeaSeller, questplan.PlagueNpcJatix} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Trade") != 3 {
			t.Errorf("shop npc %d offers %v: Trade is expected as the third op", id, n.Ops)
		}
	}
	// The skeleton is attackable (the cave is priced for it) and the only npc of the quest that is.
	if n, _ := cfg.Npc(questplan.NpcManorSkeleton); cachecfg.OpIndex(n.Ops, "Attack") != 2 {
		t.Errorf("the skeleton offers %v", n.Ops)
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.DigLocMainWinch, "Winch", "Operate", 1, 2}, {questplan.DigLocPrivateWinch, "Winch", "Operate", 1, 2}, {questplan.DigLocRopeWest, "Rope", "Climb-up", 1, 1},
		{questplan.DigLocRopeEast, "Rope", "Climb-up", 1, 1}, {questplan.DigLocSacks, "Sacks", "Search", 1, 1}, {questplan.DigLocSampleBush, "Bush", "Search", 1, 1},
		{questplan.DigLocBarrel, "Barrel", "Search", 1, 1}, {questplan.DigLocChestOpen, "Chest", "Search", 1, 1}, {questplan.DigLocChestShut, "Chest", "Open", 1, 1},
		{questplan.DigLocBrick, "Brick", "Search", 1, 1}, {questplan.DigLocPanning, "Panning point", "Look", 1, 1}, {questplan.DigLocSoil1, "Soil", "", 1, 1},
		{questplan.DigLocSoil2, "Soil", "", 1, 1}, {questplan.DigLocSoil3, "Soil", "", 1, 1},
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
func TestDigsiteLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	cs := []struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
	}{
		{"the main winch", questplan.DigLocMainWinch, "Operate", questplan.DigMainWinch, 8, 3352, 3417, 0},
		{"the private winch", questplan.DigLocPrivateWinch, "Operate", questplan.DigPrivateWinch, 8, 3370, 3428, 0},
		{"the east shaft's rope", questplan.DigLocRopeEast, "Climb-up", questplan.DigEastLanding, 40, 3370, 9827, 0},
		{"the west shaft's rope", questplan.DigLocRopeWest, "Climb-up", questplan.DigWestLanding, 40, 3353, 9817, 0},
		{"the altar cave's rope", questplan.DigLocRopeEast, "Climb-up", questplan.DigAltarEastLanding, 40, 3370, 9763, 0},
		{"the altar cave's west rope", questplan.DigLocRopeWest, "Climb-up", questplan.DigAltarWestLanding, 40, 3353, 9753, 0},
		{"the sample sacks", questplan.DigLocSacks, "Search", questplan.DigSacksAnchor, 8, 3359, 3398, 0},
		{"the sample bush", questplan.DigLocSampleBush, "Search", questplan.DigBushAnchor, 8, 3357, 3372, 0},
		{"the chest", questplan.DigLocChestShut, "Open", questplan.DigChestAnchor, 5, 3374, 3378, 0},
		{"the barrel", questplan.DigLocBarrel, "Search", questplan.DigBarrelAnchor, 6, 3364, 3378, 0},
		{"the bricks", questplan.DigLocBrick, "Search", questplan.DigBrickStand, 4, 3379, 9825, 0},
		{"the panning point", questplan.DigLocPanning, "Look", questplan.DigPanAnchor, 10, 3379, 3377, 0},
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
			t.Errorf("%s has no stand", c.what)
		}
		walkable := 0
		for _, s := range stands {
			if d.Pack.Walkable(s.X, s.Z, s.Level) {
				walkable++
			}
		}
		if walkable == 0 {
			t.Errorf("%s: none of the stands %v is walkable", c.what, stands)
		}
	}
	// The bricks are reached from the one tile the blast accepts (north of them), which is among their stands.
	brick, _ := questrunner.FindLoc(d, questplan.DigLocBrick, "", "Search", questplan.DigBrickStand, 4)
	if stands := questrunner.LocStands(d, brick, questplan.DigBrickStand); !slices.Contains(stands, questplan.DigBrickStand) {
		t.Errorf("the stands of the bricks %v do not include the blast's tile %v", stands, questplan.DigBrickStand)
	}
	if !d.Pack.Walkable(questplan.DigBrickStand.X, questplan.DigBrickStand.Z, 0) {
		t.Error("the blast's tile is not walkable")
	}
	// The soil: both zones hold soil locs of the three kinds (the trowel's targets), the stand is within a tile of one, and every soil a query near the
	// stand returns is inside the zone (the customs refuse any other).
	for _, z := range []questplan.DigZone{questplan.DigLevel3Zone, questplan.DigTrainingZone} {
		inside := 0
		for x := z.MinX; x <= z.MaxX; x++ {
			for zz := z.MinZ; zz <= z.MaxZ; zz++ {
				for _, id := range []int{questplan.DigLocSoil1, questplan.DigLocSoil2, questplan.DigLocSoil3} {
					if m, ok := questrunner.FindLoc(d, id, "", "", tile.Tile{X: x, Z: zz}, 0); ok && m.X == x && m.Z == zz {
						inside++
					}
				}
			}
		}
		if inside < 20 {
			t.Errorf("zone %+v holds %d soil locs, want a site's worth", z, inside)
		}
		m, ok := questrunner.FindLoc(d, 0, "Soil", "", z.Stand, 6)
		if !ok || !z.Contains(tile.Tile{X: m.X, Z: m.Z}) {
			t.Errorf("the soil nearest the stand %v of zone %+v is %v (found %v)", z.Stand, z, m, ok)
		}
		if tile.Chebyshev(z.Stand, tile.Tile{X: m.X, Z: m.Z}) > 1 {
			t.Errorf("the soil nearest the stand %v is %d,%d, more than a tile away", z.Stand, m.X, m.Z)
		}
		if !d.Pack.Walkable(z.Stand.X, z.Stand.Z, 0) {
			t.Errorf("the stand %v of zone %+v is not walkable", z.Stand, z)
		}
	}
}

// Every spawn and stand of the plan is a floor tile, and each ground item can be taken from a tile.
func TestDigsiteTilesAreFloorOnTheRealPack(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the Examiner's", questplan.DigExaminerAnchor}, {"the expert's", questplan.DigExpertAnchor}, {"the guide's", questplan.DigGuideAnchor},
		{"the Curator's", questplan.DigCuratorAnchor}, {"the tea seller's", questplan.DigTeaSellerAnchor}, {"the camp's", questplan.DigStudentsAnchor},
		{"the workmen's", questplan.DigWorkmenAnchor}, {"the pan stand", questplan.DigPanAnchor}, {"the east landing", questplan.DigEastLanding},
		{"the west landing", questplan.DigWestLanding}, {"the altar landing", questplan.DigAltarLanding}, {"the altar east landing", questplan.DigAltarEastLanding},
		{"the altar west landing", questplan.DigAltarWestLanding}, {"the cave workman's", questplan.DigCaveWorkmanAnchor}, {"the brick stand", questplan.DigBrickStand},
		{"the Varrock east bank", questplan.VarrockEastBank}, {"the tea shop's stand", questplan.DigTeaShop.Anchor}, {"Jatix's stand", questplan.DigJatixShop.Anchor},
		{"the general store's stand", questplan.VarrockGenStore.Anchor}, {"the level 3 site's stand", questplan.DigLevel3Zone.Stand},
		{"the training site's stand", questplan.DigTrainingZone.Stand},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not a walkable tile on the real pack", c.what, c.at)
		}
	}
	// The tray lies on a tile nothing walks onto; it is taken from the stand beside it, which the pack accepts.
	if pk.Walkable(questplan.DigTraySpawn.X, questplan.DigTraySpawn.Z, 0) {
		t.Errorf("the tray's spawn %v is walkable: the stand beside it would be a detour for nothing", questplan.DigTraySpawn)
	}
	if !slices.ContainsFunc(pk.InteractTiles(pathfind.NavPoint{X: questplan.DigTraySpawn.X, Z: questplan.DigTraySpawn.Z}), func(n pathfind.NavPoint) bool {
		return n.X == questplan.DigTrayStand.X && n.Z == questplan.DigTrayStand.Z
	}) || !pk.Walkable(questplan.DigTrayStand.X, questplan.DigTrayStand.Z, 0) {
		t.Errorf("the tray's stand %v is not a walkable tile the spawn can be taken from", questplan.DigTrayStand)
	}
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{{"the tray", questplan.DigTraySpawn}, {"the tablet", questplan.DigTabletSpawn}, {"the first root", questplan.DigRootSpawns[0]}, {"the second root", questplan.DigRootSpawns[1]}} {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: c.at.X, Z: c.at.Z, Level: c.at.Level}); len(tiles) == 0 {
			t.Errorf("the ground spawn of %s at %v can be taken from no tile", c.what, c.at)
		}
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestDigsiteLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.VarrockEastBank
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// Varrock and the way east.
		{"bank to the Curator", bank, questplan.DigCuratorAnchor}, {"the Curator to the bank", questplan.DigCuratorAnchor, bank},
		{"the Curator to the Examiner", questplan.DigCuratorAnchor, questplan.DigExaminerAnchor},
		{"bank to the Examiner", bank, questplan.DigExaminerAnchor}, {"the Examiner to the bank", questplan.DigExaminerAnchor, bank},
		{"bank to the tea seller", bank, questplan.DigTeaShop.Anchor}, {"the tea seller to the camp", questplan.DigTeaShop.Anchor, questplan.DigStudentsAnchor},
		{"bank to Varrock's general store", bank, questplan.VarrockGenStore.Anchor},
		{"the general store to the camp", questplan.VarrockGenStore.Anchor, questplan.DigStudentsAnchor},
		{"bank to Jatix", bank, questplan.DigJatixShop.Anchor}, {"Jatix to the bank", questplan.DigJatixShop.Anchor, bank},
		{"Jatix to the main winch", questplan.DigJatixShop.Anchor, questplan.DigMainWinch},
		// The Exam Centre and the site.
		{"the Examiner to the expert", questplan.DigExaminerAnchor, questplan.DigExpertAnchor}, {"the expert to the Examiner", questplan.DigExpertAnchor, questplan.DigExaminerAnchor},
		{"the Examiner to the camp", questplan.DigExaminerAnchor, questplan.DigStudentsAnchor}, {"the camp to the Examiner", questplan.DigStudentsAnchor, questplan.DigExaminerAnchor},
		{"the camp to the expert", questplan.DigStudentsAnchor, questplan.DigExpertAnchor}, {"the expert to the camp", questplan.DigExpertAnchor, questplan.DigStudentsAnchor},
		{"the camp to the workmen", questplan.DigStudentsAnchor, questplan.DigWorkmenAnchor}, {"the workmen to the bush", questplan.DigWorkmenAnchor, questplan.DigBushAnchor},
		{"the bush to the tray's stand", questplan.DigBushAnchor, questplan.DigTrayStand}, {"the tray's stand to the panning point", questplan.DigTrayStand, questplan.DigPanAnchor},
		{"the panning point to the guide", questplan.DigPanAnchor, questplan.DigGuideAnchor}, {"the panning point to the camp", questplan.DigPanAnchor, questplan.DigStudentsAnchor},
		{"the camp to the sacks", questplan.DigStudentsAnchor, questplan.DigSacksAnchor}, {"the sacks to the level 3 site", questplan.DigSacksAnchor, questplan.DigLevel3Zone.Stand},
		{"the camp to the level 3 site", questplan.DigStudentsAnchor, questplan.DigLevel3Zone.Stand},
		{"the level 3 site to the expert", questplan.DigLevel3Zone.Stand, questplan.DigExpertAnchor},
		{"the level 3 site to the workmen", questplan.DigLevel3Zone.Stand, questplan.DigWorkmenAnchor},
		{"the camp to the training site", questplan.DigStudentsAnchor, questplan.DigTrainingZone.Stand},
		{"the training site to the chest", questplan.DigTrainingZone.Stand, questplan.DigChestAnchor}, {"the chest to the barrel", questplan.DigChestAnchor, questplan.DigBarrelAnchor},
		{"the barrel to the expert", questplan.DigBarrelAnchor, questplan.DigExpertAnchor}, {"the chest to the expert", questplan.DigChestAnchor, questplan.DigExpertAnchor},
		{"the camp to the main winch", questplan.DigStudentsAnchor, questplan.DigMainWinch}, {"the camp to the private winch", questplan.DigStudentsAnchor, questplan.DigPrivateWinch},
		{"the private winch to the main winch", questplan.DigPrivateWinch, questplan.DigMainWinch},
		// Where the ropes put the account down: beside the winches (loc 2352 to 3370,3427 and loc 2353 to 3354,3417).
		{"the west rope's exit to the camp", tile.Tile{X: 3370, Z: 3427}, questplan.DigStudentsAnchor},
		{"the east rope's exit to the camp", tile.Tile{X: 3354, Z: 3417}, questplan.DigStudentsAnchor},
		{"the east rope's exit to the bank", tile.Tile{X: 3354, Z: 3417}, bank}, {"the west rope's exit to the bank", tile.Tile{X: 3370, Z: 3427}, bank},
		{"the east rope's exit to the Examiner", tile.Tile{X: 3354, Z: 3417}, questplan.DigExaminerAnchor},
		// The shafts, each inside its own pocket.
		{"the east landing to the brick stand", questplan.DigEastLanding, questplan.DigBrickStand},
		{"the brick stand to the east rope", questplan.DigBrickStand, tile.Tile{X: 3370, Z: 9828}},
		{"the west landing to the cave workman", questplan.DigWestLanding, questplan.DigCaveWorkmanAnchor},
		{"the west landing to the first root", questplan.DigWestLanding, questplan.DigRootSpawns[0]}, {"the west landing to the second root", questplan.DigWestLanding, questplan.DigRootSpawns[1]},
		{"the cave workman to the west rope", questplan.DigCaveWorkmanAnchor, tile.Tile{X: 3353, Z: 9818}},
		{"the altar landing to the tablet", questplan.DigAltarLanding, questplan.DigTabletSpawn}, {"the tablet to the altar rope", questplan.DigTabletSpawn, questplan.DigAltarEastLanding},
		{"the altar east landing to the tablet", questplan.DigAltarEastLanding, questplan.DigTabletSpawn},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-42s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The winches are the only way INTO the shafts: no walk joins the surface to any of them, nor one shaft to another. (The way out is in the graph:
	// each shaft's rope is a baked edge, so the walker climbs out by itself too, and the registered pockets climb it first.)
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the bank to the east shaft", bank, questplan.DigEastLanding}, {"the camp to the east shaft", questplan.DigStudentsAnchor, questplan.DigEastLanding},
		{"the camp to the west shaft", questplan.DigStudentsAnchor, questplan.DigWestLanding}, {"the camp to the altar cave", questplan.DigStudentsAnchor, questplan.DigAltarLanding},
		{"the east shaft to the west shaft", questplan.DigEastLanding, questplan.DigWestLanding}, {"the east shaft to the altar cave", questplan.DigEastLanding, questplan.DigAltarLanding},
		{"the altar cave to the east shaft", questplan.DigAltarLanding, questplan.DigEastLanding}, {"the west shaft to the altar's west shaft", questplan.DigWestLanding, questplan.DigAltarWestLanding},
		{"the east shaft to the tablet", questplan.DigBrickStand, questplan.DigTabletSpawn},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): the winch and the blast are not the only ways in", l.name, r.Cost)
		}
	}
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the east shaft to the camp", questplan.DigEastLanding, questplan.DigStudentsAnchor}, {"the west shaft to the camp", questplan.DigWestLanding, questplan.DigStudentsAnchor},
		{"the altar cave to the camp", questplan.DigAltarLanding, questplan.DigStudentsAnchor}, {"the altar cave's west shaft to the camp", questplan.DigAltarWestLanding, questplan.DigStudentsAnchor},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); !r.OK {
			t.Errorf("%s: no route (%s): the rope's edge is not in the graph", l.name, r.Reason)
		}
	}
}

// The pockets' rectangles are the regions the pack makes them: every tile the planner reaches on foot from a landing lies inside its
// rectangle, the counts are the pack's (189, 52, 426 and 52), and the rectangles are disjoint.
func TestDigsiteThePocketsAreTheRegionsOfThePack(t *testing.T) {
	pack, _, _ := csData(t)
	p, _ := mcPlanner(t)
	type pocket struct {
		name       string
		landing    tile.Tile
		in         func(tile.Tile) bool
		region     questplan.DigRegion
		wantTiles  int
		minX, maxX int
		minZ, maxZ int
	}
	pockets := []pocket{
		{"east shaft", questplan.DigEastLanding, questplan.InDigShaftEast, questplan.DigShaftEast, 189, 3330, 3400, 9800, 9860},
		{"west shaft", questplan.DigWestLanding, questplan.InDigShaftWest, questplan.DigShaftWest, 52, 3330, 3400, 9800, 9860},
		{"altar cave", questplan.DigAltarLanding, questplan.InDigAltarEast, questplan.DigAltarEast, 426, 3330, 3400, 9730, 9800},
		{"altar cave's west shaft", questplan.DigAltarWestLanding, questplan.InDigAltarWest, questplan.DigAltarWest, 52, 3330, 3400, 9730, 9800},
	}
	for _, pk := range pockets {
		tiles := mcReachable(p, pk.landing, pk.minX, pk.maxX, pk.minZ, pk.maxZ, true)
		t.Logf("%s reaches %d tiles", pk.name, len(tiles))
		if len(tiles) != pk.wantTiles {
			t.Errorf("%s reaches %d tiles, the pack said %d when the rectangles were drawn", pk.name, len(tiles), pk.wantTiles)
		}
		for _, c := range tiles {
			if !pk.in(c) || questplan.DigRegionOf(c) != pk.region {
				t.Errorf("%v is reached on foot in the %s and is not in its rectangle (region %q)", c, pk.name, questplan.DigRegionOf(c))
			}
		}
		if !slices.ContainsFunc(tiles, func(c tile.Tile) bool { return c == pk.landing }) {
			t.Errorf("the %s's landing %v is not reached from itself", pk.name, pk.landing)
		}
	}
	// The sites the plan stands on in a pocket are reached from its landing.
	east := mcReachable(p, questplan.DigEastLanding, 3330, 3400, 9800, 9860, true)
	west := mcReachable(p, questplan.DigWestLanding, 3330, 3400, 9800, 9860, true)
	altar := mcReachable(p, questplan.DigAltarLanding, 3330, 3400, 9730, 9800, true)
	for _, c := range []struct {
		what string
		set  []tile.Tile
		at   tile.Tile
	}{
		{"the brick stand", east, questplan.DigBrickStand}, {"the cave workman", west, questplan.DigCaveWorkmanAnchor},
		{"the first root", west, questplan.DigRootSpawns[0]}, {"the second root", west, questplan.DigRootSpawns[1]},
		{"the altar's rope landing", altar, questplan.DigAltarEastLanding},
	} {
		if !slices.Contains(c.set, c.at) {
			t.Errorf("%s at %v is not reached from its pocket's landing", c.what, c.at)
		}
	}
	// The tablet's spawn: its tile or a tile it can be taken from is reached.
	reached := false
	for _, n := range pack.InteractTiles(pathfind.NavPoint{X: questplan.DigTabletSpawn.X, Z: questplan.DigTabletSpawn.Z}) {
		if slices.Contains(altar, tile.Tile{X: n.X, Z: n.Z}) {
			reached = true
		}
	}
	if !reached && !slices.Contains(altar, questplan.DigTabletSpawn) {
		t.Errorf("the tablet at %v cannot be taken from any tile the altar cave reaches", questplan.DigTabletSpawn)
	}
	// The pockets are registered, and none of the surface places is.
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{{questplan.DigEastLanding, true}, {questplan.DigBrickStand, true}, {questplan.DigWestLanding, true}, {questplan.DigAltarLanding, true}, {questplan.DigTabletSpawn, true},
		{questplan.DigAltarWestLanding, true}, {questplan.DigMainWinch, false}, {questplan.DigPrivateWinch, false}, {questplan.DigStudentsAnchor, false}, {questplan.VarrockEastBank, false}} {
		name, in := questrunner.PocketAt(c.at)
		if in != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, in, c.want)
		}
	}
}

// digNPCRows are the NPC rows of one map file of a Content checkout (RSB0T_CONTENT_DIR), as tiles with their npc id.
func digNPCRows(t *testing.T, square string, sx, sz int) map[tile.Tile]int {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT_DIR")
	if root == "" {
		t.Skip("RSB0T_CONTENT_DIR not set")
	}
	b, err := os.ReadFile(filepath.Join(root, "maps", square+".jm2"))
	if err != nil {
		t.Skipf("no map %s: %v", square, err)
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	i := strings.Index(text, "==== NPC ====")
	if i < 0 {
		t.Fatalf("%s has no NPC section", square)
	}
	body := text[i+len("==== NPC ===="):]
	if j := strings.Index(body, "\n===="); j >= 0 {
		body = body[:j]
	}
	out := map[tile.Tile]int{}
	row := regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)`)
	for _, line := range strings.Split(body, "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		x, _ := strconv.Atoi(m[2])
		z, _ := strconv.Atoi(m[3])
		id, _ := strconv.Atoi(m[4])
		out[tile.Tile{X: sx*64 + x, Z: sz*64 + z}] = id
	}
	return out
}

// THE SKELETONS. The altar cave's map holds ten of npc 90 (the level 25 unarmed skeleton), all inside the cave the pack makes of it, and four
// within five tiles of the tablet; the map of the cave before the blast holds the same ten rows, and not one of them stands on a tile the east
// or the west shaft reaches: they are in rock. That is why only the stage 8 visit is priced (questplan.DigAltarFit), and the east shaft's is not.
func TestDigsiteTheSkeletonsStandWhereThePlanPricesThem(t *testing.T) {
	p, _ := mcPlanner(t)
	skeletons := func(rows map[tile.Tile]int) []tile.Tile {
		var out []tile.Tile
		for at, id := range rows {
			if id == questplan.NpcManorSkeleton {
				out = append(out, at)
			}
		}
		return out
	}
	after := skeletons(digNPCRows(t, "m52_152", 52, 152))
	before := skeletons(digNPCRows(t, "m52_153", 52, 153))
	if len(after) != 10 || len(before) != 10 {
		t.Fatalf("%d skeletons in the altar cave's map and %d in the pre-blast map, want 10 and 10", len(after), len(before))
	}
	altar := mcReachable(p, questplan.DigAltarLanding, 3330, 3400, 9730, 9800, true)
	east := mcReachable(p, questplan.DigEastLanding, 3330, 3400, 9800, 9860, true)
	west := mcReachable(p, questplan.DigWestLanding, 3330, 3400, 9800, 9860, true)
	onFloor := 0
	for _, s := range after {
		if slices.Contains(altar, s) {
			onFloor++
		}
	}
	if onFloor < 8 {
		t.Errorf("%d of the altar cave's ten skeletons stand on a tile the cave reaches", onFloor)
	}
	for _, s := range before {
		if slices.Contains(east, s) || slices.Contains(west, s) {
			t.Errorf("a pre-blast skeleton stands at %v, which a shaft reaches: that visit would have to be priced too", s)
		}
	}
	near := 0
	for _, s := range after {
		if tile.Chebyshev(s, questplan.DigTabletSpawn) <= 5 {
			near++
		}
	}
	if near < 3 {
		t.Errorf("%d skeletons within five tiles of the tablet", near)
	}
}
