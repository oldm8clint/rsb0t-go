package runecraftplan

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// contentDir is the content checkout (RSB0T_CONTENT_DIR, or the repo's sibling "content" directory); the
// tests that read it skip without one.
func contentDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "..", "content")
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "skill_runecraft", "configs", "runecraft.dbrow")); err != nil {
		t.Skipf("no content checkout (%v): set RSB0T_CONTENT_DIR", err)
	}
	return dir
}

// dbrows parses a .dbrow file into row name -> field -> first value.
func dbrows(t *testing.T, path string) map[string]map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := map[string]map[string]string{}
	var cur map[string]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			cur = map[string]string{}
			rows[line[1:len(line)-1]] = cur
		case strings.HasPrefix(line, "data=") && cur != nil:
			parts := strings.Split(line[len("data="):], ",")
			if _, seen := cur[parts[0]]; !seen && len(parts) > 1 {
				cur[parts[0]] = parts[1]
			}
		}
	}
	return rows
}

// packIDs parses content/pack/<name>.pack ("id=name" lines) into name -> id.
func packIDs(t *testing.T, path string) map[string]int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		id, name, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(id); err == nil {
			out[name] = n
		}
	}
	return out
}

// The table is content 289's: level, experience (tenths), multiplier, members flag and talisman from
// runecraft.dbrow, and the object and loc ids from the pack files.
func TestAltarTableMatchesContent(t *testing.T) {
	dir := contentDir(t)
	rows := dbrows(t, filepath.Join(dir, "scripts", "skill_runecraft", "configs", "runecraft.dbrow"))
	objs := packIDs(t, filepath.Join(dir, "pack", "obj.pack"))
	locs := packIDs(t, filepath.Join(dir, "pack", "loc.pack"))
	for _, c := range []struct {
		a                         Altar
		row, ruins, altar, portal string
		talisman, rune            string
	}{
		{Air, "runecraft_air", "airtemple_ruined", "air_altar", "airtemple_exit_portal", "air_talisman", "airrune"},
		{Body, "runecraft_body", "bodytemple_ruined", "body_altar", "bodytemple_exit_portal", "body_talisman", "bodyrune"},
		{Nature, "runecraft_nature", "naturetemple_ruined", "nature_altar", "naturetemple_exit_portal", "nature_talisman", "naturerune"},
	} {
		row := rows[c.row]
		if row == nil {
			t.Errorf("%s: no row %s in runecraft.dbrow", c.a.Name, c.row)
			continue
		}
		num := func(field string) int {
			n, err := strconv.Atoi(row[field])
			if err != nil {
				return 0
			}
			return n
		}
		if num("level") != c.a.Level || num("experience") != c.a.XP10 || num("multiplier") != c.a.Multiplier || (num("members") == 1) != c.a.Members {
			t.Errorf("%s: dbrow level %s experience %s multiplier %s members %s; table %d %d %d %v", c.a.Name,
				row["level"], row["experience"], row["multiplier"], row["members"], c.a.Level, c.a.XP10, c.a.Multiplier, c.a.Members)
		}
		if row["talisman"] != c.talisman || row["rune"] != c.rune {
			t.Errorf("%s: dbrow talisman %q rune %q", c.a.Name, row["talisman"], row["rune"])
		}
		for what, got := range map[string][2]int{
			"talisman": {objs[c.talisman], c.a.TalismanID}, "rune": {objs[c.rune], c.a.RuneID},
			"ruins": {locs[c.ruins], c.a.RuinsID}, "altar": {locs[c.altar], c.a.AltarID}, "portal": {locs[c.portal], c.a.PortalID},
		} {
			if got[0] == 0 || got[0] != got[1] {
				t.Errorf("%s %s: pack id %d, table %d", c.a.Name, what, got[0], got[1])
			}
		}
	}
	if objs["blankrune"] != EssenceID || objs["cert_blankrune"] != NotedEssenceID {
		t.Errorf("essence ids: pack %d/%d, table %d/%d", objs["blankrune"], objs["cert_blankrune"], EssenceID, NotedEssenceID)
	}
}

// The table is where the fleet's own loc index and collision pack say it is: the ruins, the altar and the
// portal are placed where the table has them (by type id, and by name and option, which is how the runner
// resolves them), each can be stood next to (locreach), the arrival tile reaches the altar in a few steps,
// and the bank the table names is the nearest of the fleet's banks to the exit by pack steps.
//
// Needs RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK; without them this skips.
func TestAltarTableAgainstTheRealData(t *testing.T) {
	ixPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if ixPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	find := func(q locindex.Query) []locindex.Match { return ix.Find(cfg, q, nil) }
	for _, a := range Altars() {
		type spot struct {
			what string
			id   int
			at   tile.Tile
			name string
			op   string
		}
		for _, sp := range []spot{
			{"ruins", a.RuinsID, a.Ruins, RuinsName, ""},
			{"altar", a.AltarID, a.AltarAt, AltarName, AltarOp},
			{"portal", a.PortalID, a.PortalAt, PortalName, PortalOp},
		} {
			byID := find(locindex.Query{X: sp.at.X, Z: sp.at.Z, Radius: RuinsRadius, IDs: []int{sp.id}})
			if len(byID) != 1 || byID[0].X != sp.at.X || byID[0].Z != sp.at.Z {
				t.Errorf("%s %s: by id %d near %v: %+v", a.Name, sp.what, sp.id, sp.at, byID)
				continue
			}
			byName := find(locindex.Query{X: sp.at.X, Z: sp.at.Z, Radius: RuinsRadius, Name: sp.name, Op: sp.op})
			if len(byName) != 1 || byName[0].ID != sp.id {
				t.Errorf("%s %s: by name %q op %q near %v: %+v", a.Name, sp.what, sp.name, sp.op, sp.at, byName)
				continue
			}
			m := byName[0]
			typ, _ := cfg.Loc(m.ID)
			l := locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length, Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}
			if _, modelled := locreach.Reached(m.X-1, m.Z, l); !modelled {
				t.Errorf("%s %s: shape %d is not one locreach models", a.Name, sp.what, m.Shape)
			}
			stands := 0
			fx, fz, fw, fh := l.Footprint()
			for _, s := range locreach.Tiles(l) {
				if s.X >= fx && s.X < fx+fw && s.Z >= fz && s.Z < fz+fh {
					continue // the footprint is solid
				}
				if pk.Walkable(s.X, s.Z, s.Level) {
					stands++
				}
			}
			if stands == 0 {
				t.Errorf("%s %s: no walkable tile beside it that the engine accepts a use from", a.Name, sp.what)
			}
		}
		// Inside: the arrival tile is walkable and a few steps from a tile beside the altar.
		if !pk.Walkable(a.Enter.X, a.Enter.Z, 0) {
			t.Errorf("%s: the arrival tile %v is not walkable", a.Name, a.Enter)
		}
		altarMatch := find(locindex.Query{X: a.AltarAt.X, Z: a.AltarAt.Z, Radius: RuinsRadius, IDs: []int{a.AltarID}})
		if len(altarMatch) == 1 {
			typ, _ := cfg.Loc(altarMatch[0].ID)
			l := locreach.Loc{X: altarMatch[0].X, Z: altarMatch[0].Z, Width: typ.Width, Length: typ.Length, Angle: altarMatch[0].Angle, Shape: altarMatch[0].Shape, ForceApproach: typ.ForceApproach}
			best := 1 << 30
			for _, s := range locreach.Tiles(l) {
				if r := pk.FindPath(pathfind.NavPoint{X: a.Enter.X, Z: a.Enter.Z}, pathfind.NavPoint{X: s.X, Z: s.Z}); r.OK {
					best = min(best, r.Steps())
				}
			}
			if best > 8 {
				t.Errorf("%s: the altar is %d steps from the arrival tile", a.Name, best)
			}
		}
		// Outside: the nearest bank by pack steps is the table's.
		if a.NoBank {
			continue
		}
		steps := func(from tile.Tile) int {
			r := pk.FindPath(pathfind.NavPoint{X: from.X, Z: from.Z}, pathfind.NavPoint{X: a.Exit.X, Z: a.Exit.Z})
			if !r.OK {
				return 1 << 30
			}
			return r.Steps()
		}
		if got := steps(a.Bank); got != a.BankSteps {
			t.Errorf("%s: %s is %d pack steps from the exit, the table says %d", a.Name, a.BankName, got, a.BankSteps)
		}
		for _, b := range questplan.Banks {
			if s := steps(b.Tile); s < a.BankSteps {
				t.Errorf("%s: %s (%d steps) is nearer the exit than the table's %s (%d)", a.Name, b.Name, s, a.BankName, a.BankSteps)
			}
		}
	}
}
