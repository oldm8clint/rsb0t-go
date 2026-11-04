package questmods

// The Gnome course on the real data: the production config, the baked loc index and the collision pack
// (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG) and the content checkout (RSB0T_CONTENT_DIR, or the repo's
// sibling "content" directory); each test skips without what it needs.
//
// What they hold down: the obstacle table names the locs and ops the content places; its stands are tiles the engine accepts
// the op from; each obstacle's landing is a tile the pack holds a path from to the next stand (and the tiles the movement
// crosses are NOT, which is why the laps wait for the landing); and the platforms are one strand site and one quest pocket.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

func navPoint(t tile.Tile) pathfind.NavPoint {
	return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level}
}

// Each obstacle's loc is where the table says, offers the op the table names, and its stand is a tile the engine accepts the
// op from (locreach's rectangle rules over the pack's walls) and the pack walks.
func TestGnomeObstaclesAndStandsOnTheRealData(t *testing.T) {
	d := csDeps(t)
	for i, ob := range questplan.GnomeCourse {
		for _, id := range ob.IDs {
			typ, ok := d.Config.Loc(id)
			if !ok {
				t.Errorf("obstacle %d (%s): loc %d is not in the config", i, ob.Name, id)
				continue
			}
			if typ.Name != ob.Name {
				t.Errorf("obstacle %d: loc %d is %q, the table says %q", i, id, typ.Name, ob.Name)
			}
			if cachecfg.OpIndex(typ.Ops, ob.Op) != 1 {
				t.Errorf("obstacle %d (%s): loc %d offers %v, want %q as op 1", i, ob.Name, id, typ.Ops, ob.Op)
			}
		}
		// The loc as the laps look for it, from the stand.
		m, ok := questrunner.FindLoc(d, ob.IDs[0], "", ob.Op, ob.Stand, 8)
		if !ok {
			t.Errorf("obstacle %d (%s): no loc %d within 8 tiles of its stand %v", i, ob.Name, ob.IDs[0], ob.Stand)
			continue
		}
		if m.Level != ob.Near.Level {
			t.Errorf("obstacle %d (%s): the loc is on level %d, the table says %d", i, ob.Name, m.Level, ob.Near.Level)
		}
		stands := questrunner.LocStands(d, m, ob.Stand)
		got := false
		for _, s := range stands {
			if s == ob.Stand {
				got = true
			}
		}
		if !got {
			t.Errorf("obstacle %d (%s): the stand %v is not among the tiles the engine accepts the op from: %v", i, ob.Name, ob.Stand, stands)
		}
		if !d.Pack.Walkable(ob.Stand.X, ob.Stand.Z, ob.Stand.Level) {
			t.Errorf("obstacle %d (%s): the stand %v is not walkable on the pack", i, ob.Name, ob.Stand)
		}
	}
}

// Each obstacle sets the account down on tiles the pack walks, and from every one of them the pack holds a path to the next
// obstacle's stand: the laps' approach is a short walk on the pack. The tiles the rope and the pipe carry the account over are
// not like that, which is what the landing wait is for (live 16 Oct 2026: "no path from 2481,3420,2", "no path from
// 2484,3434,0").
func TestGnomeLandingsReachTheNextStandAndTransitTilesDoNot(t *testing.T) {
	d := csDeps(t)
	course := questplan.GnomeCourse
	for i, ob := range course {
		next := course[(i+1)%len(course)]
		l := ob.Land
		for x := l.MinX; x <= l.MaxX; x++ {
			at := tile.Tile{X: x, Z: l.Z, Level: l.Level}
			if !d.Pack.Walkable(at.X, at.Z, at.Level) {
				t.Errorf("obstacle %d (%s): landing %v is not walkable on the pack", i, ob.Name, at)
				continue
			}
			if r := d.Pack.FindPath(navPoint(at), navPoint(next.Stand)); !r.OK {
				t.Errorf("obstacle %d (%s): no path from the landing %v to the next stand %v: %s", i, ob.Name, at, next.Stand, r.Reason)
			}
		}
	}
	for _, c := range []struct {
		what    string
		from    tile.Tile
		to      tile.Tile
		reached bool
	}{
		{"the rope's far half", tile.Tile{X: 2481, Z: 3420, Level: 2}, course[4].Stand, false},
		{"inside the pipe", tile.Tile{X: 2484, Z: 3434}, course[0].Stand, false},
		{"the rope's landing", tile.Tile{X: 2483, Z: 3420, Level: 2}, course[4].Stand, true},
		{"the pipe's landing", tile.Tile{X: 2484, Z: 3437}, course[0].Stand, true},
	} {
		if r := d.Pack.FindPath(navPoint(c.from), navPoint(c.to)); r.OK != c.reached {
			t.Errorf("%s: path from %v to %v = %v (%s), want %v", c.what, c.from, c.to, r.OK, r.Reason, c.reached)
		}
	}
	// The tiles the movement crosses read as the next obstacle but are no landing.
	for _, at := range []tile.Tile{{X: 2481, Z: 3420, Level: 2}, {X: 2482, Z: 3420, Level: 2}} {
		if questplan.GnomeNext(at) != 4 || questplan.GnomeLanded(3, at) {
			t.Errorf("%v: next %d landed %v", at, questplan.GnomeNext(at), questplan.GnomeLanded(3, at))
		}
	}
	// Every ground landing walks to the stronghold's gate: a lap that stops after any obstacle on the ground has stopped
	// somewhere the next quest's walk starts from.
	gate := tile.Tile{X: 2461, Z: 3385}
	for i, ob := range course {
		l := ob.Land
		if l.Level != 0 {
			continue
		}
		at := tile.Tile{X: l.MinX, Z: l.Z}
		if r := d.Pack.FindPath(navPoint(at), navPoint(gate)); !r.OK {
			t.Errorf("obstacle %d (%s): no path from the ground landing %v to the gate %v: %s", i, ob.Name, at, gate, r.Reason)
		}
	}
}

// The platforms are one strand site of the Guardian's and one pocket of the quests': the same tiles, so an account the
// Guardian would own is one every quest walk takes down first, and no ground tile is either.
func TestGnomeUpperFloorsAreOneStrandSiteAndOnePocket(t *testing.T) {
	const site = "the Gnome agility course's upper floors"
	for level := 0; level <= 3; level++ {
		for x := 2455; x <= 2500; x++ {
			for z := 3405; z <= 3450; z++ {
				at := tile.Tile{X: x, Z: z, Level: level}
				name, owned := randomevents.StrandOwner(x, z, level)
				if upper := questplan.InGnomeUpper(at); upper != (owned && name == site) {
					t.Fatalf("%v: InGnomeUpper = %v, the Guardian's catalogue owns it = %v (%q)", at, upper, owned, name)
				}
				pocket, in := questrunner.PocketAt(at)
				if questplan.InGnomeUpper(at) != (in && pocket == site) {
					t.Fatalf("%v: InGnomeUpper = %v, the quest pocket = %v (%q)", at, questplan.InGnomeUpper(at), in, pocket)
				}
			}
		}
	}
	// And the site's exit steps are the course's own obstacles, in lap order, with the stands the laps use.
	for _, c := range []struct {
		at  tile.Tile
		obs int
	}{
		{tile.Tile{X: 2473, Z: 3424, Level: 1}, 2},
		{tile.Tile{X: 2473, Z: 3420, Level: 2}, 3},
		{tile.Tile{X: 2483, Z: 3420, Level: 2}, 4},
	} {
		ob := questplan.GnomeCourse[c.obs]
		st, ok := randomevents.StrandSiteAt(c.at.X, c.at.Z, c.at.Level)
		if !ok {
			t.Errorf("%v: no strand site", c.at)
			continue
		}
		step := st.StepFor(c.at.X, c.at.Z, c.at.Level)
		if step == nil {
			t.Errorf("%v: the site has no exit step for it", c.at)
			continue
		}
		if step.LocID != ob.IDs[0] || step.LocX != ob.Near.X || step.LocZ != ob.Near.Z || step.OpLabel != ob.Op || step.Op != 1 {
			t.Errorf("%v: the Guardian's step is %s on loc %d at %d,%d (option %d), the laps' obstacle %d is %s on loc %d at %v", c.at, step.OpLabel, step.LocID, step.LocX, step.LocZ, step.Op, c.obs, ob.Op, ob.IDs[0], ob.Near)
		}
		if len(step.Stands) == 0 || step.Stands[0].X != ob.Stand.X || step.Stands[0].Z != ob.Stand.Z || step.Stands[0].Level != ob.Stand.Level {
			t.Errorf("%v: the Guardian stands on %v, the laps on %v", c.at, step.Stands, ob.Stand)
		}
	}
}

// ---- the content checkout ----

func gnomeContentDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "..", "content")
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "skill_agility", "scripts", "gnome_course.rs2")); err != nil {
		t.Skipf("no content checkout (%v)", err)
	}
	return dir
}

// rs2Blocks splits a script file into its blocks by "[trigger,name]" header line.
func rs2Blocks(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	var key string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(line, "[") {
			key = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if i := strings.Index(key, "]"); i >= 0 {
				key = key[:i]
			}
			continue
		}
		out[key] += line + "\n"
	}
	return out
}

var movecoordRe = regexp.MustCompile(`movecoord\((coord|loc_coord), (-?\d+), (-?\d+), (-?\d+)\)`)

// movecoords lists a block's movecoord calls as (base, dx, dlevel, dz).
func movecoords(block string) (out [][4]string) {
	for _, m := range movecoordRe.FindAllStringSubmatch(block, -1) {
		out = append(out, [4]string{m[1], m[2], m[3], m[4]})
	}
	return out
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// The landing boxes of questplan.GnomeCourse are what gnome_course.rs2 does: the obstacle's last movecoord (or the packed
// 0_38_53_55_28 of the branch down), applied to the stand, lands inside the box; and the loc ops and placements the table
// names are the content's (gnome_course.loc, maps/m38_53.jm2, pack/loc.pack).
func TestGnomeCourseTableMatchesContent(t *testing.T) {
	dir := gnomeContentDir(t)
	blocks := rs2Blocks(t, filepath.Join(dir, "scripts", "skill_agility", "scripts", "gnome_course.rs2"))
	course := questplan.GnomeCourse
	scripts := []string{"oploc1,gnome_log_balance1", "oploc1,obstical_net2", "oploc1,gnome_tree_branch_1", "oploc1,balancing_rope", "label,climbdown_gnome_tree_branch", "oploc1,obstical_net3", "label,enter_gnome_obstacle_pipe"}
	if len(scripts) != len(course) {
		t.Fatalf("%d scripts for %d obstacles", len(scripts), len(course))
	}
	for i, name := range scripts {
		block, ok := blocks[name]
		if !ok {
			t.Fatalf("obstacle %d: no script %q in gnome_course.rs2", i, name)
		}
		ob := course[i]
		var at tile.Tile
		switch i {
		case 4:
			if !strings.Contains(block, "0_38_53_55_28") {
				t.Fatalf("the branch down no longer lands on 0_38_53_55_28:\n%s", block)
			}
			at = tile.Tile{X: 38*64 + 55, Z: 53*64 + 28}
		case 6:
			// Two squeezes and a teleport: loc z - 1 to loc z + 2, a teleport one on, then three more.
			mcs := movecoords(block)
			var want = [][4]string{{"loc_coord", "0", "0", "-1"}, {"loc_coord", "0", "0", "2"}, {"coord", "0", "0", "1"}, {"coord", "0", "0", "1"}, {"coord", "0", "0", "3"}}
			if fmt.Sprint(mcs) != fmt.Sprint(want) || !strings.Contains(block, "p_teleport(movecoord(coord, 0, 0, 1))") {
				t.Fatalf("the pipe's movement is %v, the landing arithmetic assumes %v:\n%s", mcs, want, block)
			}
			at = tile.Tile{X: ob.Near.X, Z: ob.Near.Z + 2 + 1 + 3}
		default:
			mcs := movecoords(block)
			if len(mcs) == 0 {
				t.Fatalf("obstacle %d: no movecoord in %q:\n%s", i, name, block)
			}
			mc := mcs[len(mcs)-1]
			base := ob.Stand
			if mc[0] == "loc_coord" {
				base = ob.Near
			}
			at = tile.Tile{X: base.X + atoi(mc[1]), Z: base.Z + atoi(mc[3]), Level: base.Level + atoi(mc[2])}
			if mc[0] == "loc_coord" {
				// The branch up is movecoord(loc_coord, 0, 1, -2) from the branch's own tile.
				at.Level = ob.Near.Level + atoi(mc[2])
			}
		}
		if !ob.Land.Has(at) {
			t.Errorf("obstacle %d (%s): content lands the account on %v from %v, outside the table's box %+v", i, ob.Name, at, ob.Stand, ob.Land)
		}
	}

	// Ops and ids: pack/loc.pack names, gnome_course.loc's op1 labels.
	ids := map[string]int{}
	pf, err := os.Open(filepath.Join(dir, "pack", "loc.pack"))
	if err != nil {
		t.Fatal(err)
	}
	defer pf.Close()
	ps := bufio.NewScanner(pf)
	for ps.Scan() {
		if id, name, ok := strings.Cut(strings.TrimSpace(ps.Text()), "="); ok {
			if n, err := strconv.Atoi(id); err == nil {
				ids[name] = n
			}
		}
	}
	lf, err := os.ReadFile(filepath.Join(dir, "scripts", "skill_agility", "configs", "gnome_course.loc"))
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]string{}
	var cur string
	for _, line := range strings.Split(strings.ReplaceAll(string(lf), "\r", ""), "\n") {
		switch {
		case strings.HasPrefix(line, "["):
			cur = strings.Trim(line, "[]")
		case strings.HasPrefix(line, "op1=") && cur != "":
			ops[cur] = strings.TrimPrefix(line, "op1=")
		}
	}
	for i, c := range []struct {
		obstacle int
		name     string
		id       int
	}{
		{0, "gnome_log_balance1", 2295}, {1, "obstical_net2", 2285}, {2, "gnome_tree_branch_1", 2313}, {3, "balancing_rope", 2312},
		{4, "climbing_tree", 2314}, {4, "climbing_tree2", 2315}, {5, "obstical_net3", 2286}, {6, "loc_154", 154},
	} {
		ob := course[c.obstacle]
		if ids[c.name] != c.id {
			t.Errorf("case %d: pack/loc.pack names %s as %d, the table has %d", i, c.name, ids[c.name], c.id)
		}
		if ops[c.name] != ob.Op {
			t.Errorf("case %d: content's op1 of %s is %q, the table has %q", i, c.name, ops[c.name], ob.Op)
		}
		found := false
		for _, id := range ob.IDs {
			found = found || id == c.id
		}
		if !found {
			t.Errorf("case %d: loc %d (%s) is not among obstacle %d's ids %v", i, c.id, c.name, c.obstacle, ob.IDs)
		}
	}

	// Placements: the table's Near is a tile where the map places the obstacle's first loc (the jm2 stores a bridge's locs on the
	// level above, so levels are not compared).
	mf, err := os.Open(filepath.Join(dir, "maps", "m38_53.jm2"))
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()
	placed := map[int]map[[2]int]bool{}
	in := false
	ms := bufio.NewScanner(mf)
	for ms.Scan() {
		line := strings.TrimSpace(ms.Text())
		if strings.HasPrefix(line, "====") {
			in = line == "==== LOC ===="
			continue
		}
		head, tail, ok := strings.Cut(line, ":")
		hf := strings.Fields(head)
		tf := strings.Fields(tail)
		if !in || !ok || len(hf) != 3 || len(tf) < 2 {
			continue
		}
		id := atoi(tf[0])
		if placed[id] == nil {
			placed[id] = map[[2]int]bool{}
		}
		placed[id][[2]int{38*64 + atoi(hf[1]), 53*64 + atoi(hf[2])}] = true
	}
	for i, ob := range course {
		if !placed[ob.IDs[0]][[2]int{ob.Near.X, ob.Near.Z}] {
			t.Errorf("obstacle %d (%s): map m38_53 places loc %d at %v, not at the table's %d,%d", i, ob.Name, ob.IDs[0], placed[ob.IDs[0]], ob.Near.X, ob.Near.Z)
		}
	}
}
