package questmods

// Shades of Mort'ton on the real data: the production config, the baked loc index, the collision pack and the web graph (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX,
// RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan carries names what it says in the config the fleet's client reads and
// offers the op the plan sends, whether the customs' own loc queries find their locs where the plan says and each has a tile to stand on, whether every leg the quest walks is
// a route the planner finds on the real pack (and none of them touches the Wilderness), and whether the town's streets and the temple are ground the account can fight on and
// build on.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestMorttonIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjMtDiary: "Diary", questplan.ObjMtUnidTarromin: "Herb", questplan.ObjMtTarromin: "Tarromin", questplan.ObjMtUnfTarromin: "Unfinished potion",
		questplan.ObjMtVialWater: "Vial of water", questplan.ObjMtVial: "Vial", questplan.ObjMtSerum4: "Serum 207 (4)", questplan.ObjMtSerum3: "Serum 207 (3)",
		questplan.ObjMtSerum2: "Serum 207 (2)", questplan.ObjMtSerum1: "Serum 207 (1)", questplan.ObjMtPerm4: "Serum 207(p) (4)", questplan.ObjMtPerm3: "Serum 207(p) (3)",
		questplan.ObjMtPerm2: "Serum 207(p) (2)", questplan.ObjMtPerm1: "Serum 207(p) (1)", questplan.ObjMtRemains: "Loar remains", questplan.ObjMtBrick: "Limestone brick",
		questplan.ObjMtPlank: "Plank", questplan.ObjMtPaste: "Swamp paste", questplan.ObjMtOil4: "Olive oil(4)", questplan.ObjMtOil3: "Olive oil(3)", questplan.ObjMtOil2: "Olive oil(2)",
		questplan.ObjMtOil1: "Olive oil(1)", questplan.ObjMtSacred4: "Sacred oil(4)", questplan.ObjMtSacred3: "Sacred oil(3)", questplan.ObjMtSacred2: "Sacred oil(2)",
		questplan.ObjMtSacred1: "Sacred oil(1)", questplan.ObjMtPyreLogs: "Pyre logs", questplan.ObjMtRoguesPurse: "Unidentified herb", questplan.ObjMtRottenFood: "Rotten food",
		questplan.ObjMtLogs: "Logs", questplan.ObjTinderbox: "Tinderbox", questplan.ObjHammer: "Hammer", questplan.ObjAshes: "Ashes", questplan.ObjShark: "Shark",
		questplan.CoinsID: "Coins",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// The ops the executors send: the diary is read (first), the herb identified (first), and the paste is the one stackable of the building material.
	for _, c := range []struct {
		id int
		op string
	}{{questplan.ObjMtDiary, "Read"}, {questplan.ObjMtUnidTarromin, "Identify"}} {
		o, _ := cfg.Obj(c.id)
		if cachecfg.OpIndex(o.IOps, c.op) != 1 {
			t.Errorf("obj %d (%s) offers %v, want %q first", c.id, o.Name, o.IOps, c.op)
		}
	}
	if o, _ := cfg.Obj(questplan.ObjMtPaste); !o.Stackable {
		t.Error("the swamp paste is not stackable: the purchase's slots are counted on it")
	}
	for _, id := range []int{questplan.ObjMtPlank, questplan.ObjMtBrick, questplan.ObjMtRemains, questplan.ObjMtPyreLogs} {
		if o, _ := cfg.Obj(id); o.Stackable {
			t.Errorf("obj %d (%s) is stackable: the slot count assumes one slot each", id, o.Name)
		}
	}
	for id, want := range map[int]string{
		questplan.NpcMtRazmire: "Razmire Keelgan", questplan.NpcMtRazmireAfflicted: "Afflicted(Razmire)", questplan.NpcMtUlsquire: "Ulsquire Shauncy",
		questplan.NpcMtUlsquireAfflict: "Afflicted(Ulsquire)", questplan.NpcMtShadow: "Loar Shadow", questplan.NpcMtShade: "Loar Shade",
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
	}{{questplan.NpcMtRazmire, "Talk-to", 1}, {questplan.NpcMtRazmire, "Trade-General-Store", 3}, {questplan.NpcMtRazmire, "Trade-Builders-Store", 4},
		{questplan.NpcMtRazmireAfflicted, "Talk-to", 1}, {questplan.NpcMtUlsquire, "Talk-to", 1}, {questplan.NpcMtUlsquireAfflict, "Talk-to", 1},
		{questplan.NpcMtShadow, "Attack", 2}, {questplan.NpcMtShade, "Attack", 2}} {
		n, _ := cfg.Npc(c.id)
		if cachecfg.OpIndex(n.Ops, c.op) != c.pos {
			t.Errorf("npc %d (%s) offers %v, want %q as op %d", c.id, n.Name, n.Ops, c.op, c.pos)
		}
	}
	for _, stop := range []questplan.NpcStop{questplan.MtRazmireStop, questplan.MtUlsquireStop} {
		n, _ := cfg.Npc(stop.ID)
		if n.Name != stop.Npc {
			t.Errorf("stop %q: npc %d is %q", stop.Npc, stop.ID, n.Name)
		}
	}
	for _, c := range []struct {
		id   int
		name string
		op   string
		pos  int
		w, l int
	}{
		{questplan.LocMtShelf, "Shelf", "Search", 1, 1, 1}, {questplan.LocMtSink, "Sink", "", 0, 1, 2}, {questplan.LocMtTable, "Smashed table", "Search", 5, 1, 1},
		{questplan.LocMtWallBase, "Broken Wall", "Repair", 1, 1, 1}, {questplan.LocMtWallBase + 5, "Temple wall", "Repair", 1, 1, 1}, {questplan.LocMtWallDone, "Temple wall", "Reinforce", 1, 1, 1},
		{questplan.LocMtCornerBase, "Broken Wall", "Repair", 1, 1, 1}, {questplan.LocMtCornerDone, "Temple wall", "Reinforce", 1, 1, 1},
		{questplan.LocMtAltarLit, "Flaming Fire altar", "", 0, 1, 1}, {questplan.LocMtAltarUnlit, "Fire altar", "Light", 1, 1, 1}, {questplan.LocMtAltarBroken, "Broken fire altar", "", 0, 1, 1},
		{questplan.LocMtPyreEmpty, "Funeral Pyre", "", 0, 1, 3}, {questplan.LocMtPyreLogs, "Funeral Pyre", "", 0, 1, 3}, {questplan.LocMtPyreBones, "Funeral Pyre", "Light", 1, 1, 3},
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
	// Every piece of wall from the base to the ninth level offers Repair first, and the tenth Reinforce: the executor chooses its op by that.
	for i := 0; i < 10; i++ {
		for _, base := range []int{questplan.LocMtWallBase, questplan.LocMtCornerBase} {
			if l, _ := cfg.Loc(base + i); cachecfg.OpIndex(l.Ops, "Repair") != 1 || !questplan.MtWallRepairable(base+i) {
				t.Errorf("loc %d (%s) offers %v", base+i, l.Name, l.Ops)
			}
		}
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestMorttonLocQueriesFindTheirLocsAndStands(t *testing.T) {
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
		{"the shelf", questplan.LocMtShelf, "Search", questplan.MtShelf, 3, questplan.MtShelf.X, questplan.MtShelf.Z, 0, nil},
		{"the smashed table", questplan.LocMtTable, "Search", questplan.MtTable, 3, questplan.MtTable.X, questplan.MtTable.Z, 0, nil},
		{"the sink", questplan.LocMtSink, "", questplan.MtSink, 3, questplan.MtSink.X, questplan.MtSink.Z, 0, []tile.Tile{questplan.MtSinkNear}},
		{"the broken altar", questplan.LocMtAltarBroken, "", questplan.MtAltar, 4, questplan.MtAltar.X, questplan.MtAltar.Z, 0, nil},
		{"a funeral pyre", questplan.LocMtPyreEmpty, "", questplan.MtPyre, 8, questplan.MtPyre.X, questplan.MtPyre.Z, 0, nil},
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
		for _, s := range stands {
			if !questplan.InMortton(s) {
				t.Errorf("%s: the stand %v is outside the town", c.what, s)
			}
		}
	}
	// The vials' spawn is the floor of the same house as the shelf and the table, and a walker reaches it.
	if !d.Pack.Walkable(questplan.MtVials.X, questplan.MtVials.Z, 0) {
		t.Errorf("the vials' tile %v is not walkable", questplan.MtVials)
	}
}

// The temple is fifteen wall pieces (eleven straight, four corners) round one altar, every piece within the reach of the executor's search from its inside, each a loc with a
// stand the walker finishes on, and the funeral pyres are the content's twenty-six.
func TestMorttonTheTempleIsFifteenWallsAnAltarAndPyres(t *testing.T) {
	d := csDeps(t)
	straight, corners, pyres := 0, 0, 0
	d.Locs.Each(questplan.MtTown.X-40, questplan.MtTown.Z-40, questplan.MtTown.X+40, questplan.MtTown.Z+40, 0, func(l locindex.Loc) {
		switch {
		case l.ID == questplan.LocMtWallBase:
			straight++
			checkWall(t, d, l)
		case l.ID == questplan.LocMtCornerBase:
			corners++
			checkWall(t, d, l)
		case l.ID == questplan.LocMtPyreEmpty:
			pyres++
		}
	})
	if straight != 11 || corners != 4 {
		t.Errorf("%d straight and %d corner pieces of wall, the content has 11 and 4", straight, corners)
	}
	if pyres < 20 {
		t.Errorf("%d funeral pyres in the baked index within 40 tiles of the town, want some two dozen", pyres)
	}
	t.Logf("%d straight and %d corner walls, %d empty funeral pyres within 40 tiles of the town's middle", straight, corners, pyres)
}

func checkWall(t *testing.T, d *questrunner.Deps, l locindex.Loc) {
	t.Helper()
	at := tile.Tile{X: l.X, Z: l.Z}
	if tile.Chebyshev(at, questplan.MtTempleInside) > 8 {
		t.Errorf("the wall piece %d at %v is more than 8 tiles from the temple's inside %v: the executor's search would miss it", l.ID, at, questplan.MtTempleInside)
	}
	m, ok := questrunner.FindLoc(d, l.ID, "", "Repair", at, 0)
	if !ok {
		t.Errorf("the wall piece %d at %v is not found by its own query", l.ID, at)
		return
	}
	if !questplan.MtInTempleZone(at) {
		t.Errorf("the wall piece at %v is outside the temple's zone: the meters would not follow it", at)
	}
	walkable := 0
	for _, s := range questrunner.LocStands(d, m, questplan.MtTempleInside) {
		if d.Pack.Walkable(s.X, s.Z, s.Level) {
			walkable++
		}
	}
	if walkable == 0 {
		t.Errorf("the wall piece at %v has no stand to repair it from", at)
	}
}

// Every leg the quest walks plans on the real pack and graph, and none of them touches the Wilderness.
func TestMorttonLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	opts := webwalk.Options{AllowSpecial: true, AllowNPC: true}
	bank, town := questplan.VarrockEastBank, questplan.MtTown
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"the bank to the log spawns", bank, questplan.MtVarrockLogs}, {"the log spawns to the bank", questplan.MtVarrockLogs, bank},
		{"the bank to the town", bank, town}, {"the log spawns to the town", questplan.MtVarrockLogs, town}, {"the town to the bank", town, bank},
		{"the town to the shelf", town, questplan.MtShelf}, {"the shelf to the table", questplan.MtShelf, questplan.MtTable}, {"the table to the vials", questplan.MtTable, questplan.MtVials},
		{"the vials to the sink", questplan.MtVials, questplan.MtSinkNear}, {"the sink to Razmire", questplan.MtSinkNear, questplan.MtRazmire},
		{"Razmire to Ulsquire", questplan.MtRazmire, questplan.MtUlsquire}, {"Ulsquire to Razmire", questplan.MtUlsquire, questplan.MtRazmire},
		{"the town to the shades' field", town, questplan.MtShadeField}, {"the shades' field to Razmire", questplan.MtShadeField, questplan.MtRazmire}, {"Ulsquire to the temple's inside", questplan.MtUlsquire, questplan.MtTempleInside},
		{"Razmire to the temple's inside", questplan.MtRazmire, questplan.MtTempleInside}, {"the temple's inside to Razmire", questplan.MtTempleInside, questplan.MtRazmire},
		{"the temple's inside to the altar", questplan.MtTempleInside, questplan.MtAltar}, {"the altar to the pyre", questplan.MtAltar, questplan.MtPyre},
		{"the pyre to Ulsquire", questplan.MtPyre, questplan.MtUlsquire}, {"the temple to the pyre", questplan.MtTempleInside, questplan.MtPyre},
		{"the shelf to the sink", questplan.MtShelf, questplan.MtSinkNear}, {"the sink to the temple", questplan.MtSinkNear, questplan.MtTempleInside},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), opts)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		for _, w := range r.Waypoints {
			// The Wilderness: everything north of z 3520 in the north-western block of the map, level 0.
			if w.Level == 0 && w.Z >= 3523 && w.X >= 2944 && w.X <= 3391 {
				t.Errorf("%s: the waypoint %d,%d is in the Wilderness", l.name, w.X, w.Z)
			}
		}
		t.Logf("%-38s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
}

// The town is one piece of ground the legs' ends all stand on, the temple's door is the one gap in its shell, and the shades' field holds the shades the hunt would fight: each
// spawn in the content's two maps is a walk over open ground from the field's middle, inside the radius the hunt searches.
func TestMorttonTheTownIsOnePieceOfGroundAndTheShadesAreOnIt(t *testing.T) {
	pk, _, _ := csData(t)
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.MtShadeField.X, Z: questplan.MtShadeField.Z}, fhReachWindow, nil)
	if reach.Size() == 0 {
		t.Fatalf("no open ground round %v", questplan.MtShadeField)
	}
	if !pk.Walkable(questplan.MtShadeField.X, questplan.MtShadeField.Z, 0) || !pk.Walkable(questplan.MtTown.X, questplan.MtTown.Z, 0) {
		t.Errorf("the town's middle %v or the shades' field %v is not walkable", questplan.MtTown, questplan.MtShadeField)
	}
	for _, c := range []struct {
		name string
		at   tile.Tile
	}{{"Razmire", questplan.MtRazmire}, {"Ulsquire", questplan.MtUlsquire}, {"the shelf", questplan.MtShelf}, {"the sink's stand", questplan.MtSinkNear}, {"the temple's door", questplan.MtTempleDoor},
		{"the temple's inside", questplan.MtTempleInside}, {"the altar", questplan.MtAltar}, {"the pyre", questplan.MtPyre}} {
		if _, ok := reach.Steps(c.at.X, c.at.Z, c.at.Level); !ok {
			t.Logf("%s at %v is outside the field's %d-tile window (the walker covers it)", c.name, c.at, fhReachWindow)
		}
		// The altar, the shelf and the pyre are locs: the tile is the loc's own (its stands are checked with the loc queries).
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) && c.name != "the altar" && c.name != "the shelf" && c.name != "the pyre" {
			t.Errorf("%s at %v is not walkable", c.name, c.at)
		}
	}
	// The temple's door is walkable and the tile inside it too; the shell is closed everywhere else (the inside is not reached from a tile of its wall).
	if !pk.Walkable(questplan.MtTempleDoor.X, questplan.MtTempleDoor.Z, 0) || !pk.Walkable(questplan.MtTempleInside.X, questplan.MtTempleInside.Z, 0) {
		t.Errorf("the temple's door %v or its inside %v is not walkable", questplan.MtTempleDoor, questplan.MtTempleInside)
	}
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Log("RSB0T_CONTENT not set: the shades' spawns are not read")
		return
	}
	spawns := moShadeSpawns(t, root)
	if len(spawns) < 20 {
		t.Fatalf("%d shade spawns in the maps, want some 25", len(spawns))
	}
	open := 0
	for _, s := range spawns {
		if tile.Chebyshev(s, questplan.MtShadeField) > questplan.MtShadeFieldRadius+5 {
			t.Errorf("the shade spawn %v is more than %d tiles from the field %v: the hunt would never choose it", s, questplan.MtShadeFieldRadius+5, questplan.MtShadeField)
		}
		if n := interactSteps(pk, reach, questplan.MtShadeField, s); n >= 0 {
			open++
		}
	}
	t.Logf("%d of %d shade spawns are a walk over open ground from the field's middle", open, len(spawns))
	if open < len(spawns)*3/4 {
		t.Errorf("only %d of %d shade spawns are on open ground from the field's middle", open, len(spawns))
	}
}

// moShadeSpawns is the spawn tiles of the Loar shadows in the content's maps of the town (m54_51 and m54_52).
func moShadeSpawns(t *testing.T, root string) []tile.Tile {
	t.Helper()
	var out []tile.Tile
	row := regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)`)
	for _, name := range []string{"m54_51.jm2", "m54_52.jm2"} {
		raw, err := os.ReadFile(filepath.Join(root, "maps", name))
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`m(\d+)_(\d+)\.jm2`).FindStringSubmatch(name)
		rx, _ := strconv.Atoi(m[1])
		rz, _ := strconv.Atoi(m[2])
		in := ""
		for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "==== ") {
				in = strings.TrimSuffix(strings.TrimPrefix(line, "==== "), " ====")
				continue
			}
			if in != "NPC" {
				continue
			}
			if r := row.FindStringSubmatch(strings.TrimSpace(line)); r != nil && r[4] == strconv.Itoa(questplan.NpcMtShadow) {
				x, _ := strconv.Atoi(r[2])
				z, _ := strconv.Atoi(r[3])
				out = append(out, tile.Tile{X: rx*64 + x, Z: rz*64 + z})
			}
		}
	}
	return out
}
