package questplan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// The imp sites on the real data: Content 289's maps (RSB0T_CONTENT_DIR, or RSB0T_CONTENT) and the fleet's collision pack
// (RSB0T_COLLISION_PACK). Each test skips without its data.

func impContentRoot(t *testing.T) string {
	t.Helper()
	for _, env := range []string{"RSB0T_CONTENT_DIR", "RSB0T_CONTENT"} {
		if root := os.Getenv(env); root != "" {
			return root
		}
	}
	t.Skip("set RSB0T_CONTENT_DIR to a Content checkout to check the imp sites against its maps")
	return ""
}

var (
	impMapName  = regexp.MustCompile(`m(\d+)_(\d+)\.jm2$`)
	impNpcLine  = regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)$`)
	impNpcIDStr = strconv.Itoa(NpcImp)
)

// contentImpSpawns is every imp spawn in the maps (npc 708), by absolute tile.
func contentImpSpawns(t *testing.T) []tile.Tile {
	t.Helper()
	root := impContentRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "maps", "m*_*.jm2"))
	if err != nil || len(files) == 0 {
		t.Skipf("no maps under %s", root)
	}
	var out []tile.Tile
	for _, f := range files {
		m := impMapName.FindStringSubmatch(filepath.Base(f))
		if m == nil {
			continue
		}
		mx, _ := strconv.Atoi(m[1])
		mz, _ := strconv.Atoi(m[2])
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		inNpcs := false
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "====") {
				inNpcs = strings.TrimSpace(line) == "==== NPC ===="
				continue
			}
			if !inNpcs {
				continue
			}
			if p := impNpcLine.FindStringSubmatch(strings.TrimSpace(line)); p != nil && p[4] == impNpcIDStr {
				lvl, _ := strconv.Atoi(p[1])
				x, _ := strconv.Atoi(p[2])
				z, _ := strconv.Atoi(p[3])
				out = append(out, tile.Tile{X: mx*64 + x, Z: mz*64 + z, Level: lvl})
			}
		}
	}
	return out
}

func tileSet(ts []tile.Tile) map[tile.Tile]bool {
	m := map[tile.Tile]bool{}
	for _, t := range ts {
		m[t] = true
	}
	return m
}

// Every site's spawns are the content's, all of them: the table cannot drift from the maps it was read from.
func TestImpSitesSpawnsAreTheContents(t *testing.T) {
	all := contentImpSpawns(t)
	if len(all) != 47 {
		t.Logf("content has %d imp spawns (the 16 Oct 2026 study counted 47)", len(all))
	}
	for _, s := range ImpSites {
		sq := [2]int{s.Stand.X / 64, s.Stand.Z / 64}
		var want []tile.Tile
		for _, a := range all {
			if [2]int{a.X / 64, a.Z / 64} == sq {
				want = append(want, a)
			}
		}
		got, wantSet := tileSet(s.Spawns), tileSet(want)
		if len(got) != len(s.Spawns) {
			t.Errorf("%s lists a spawn twice", s.ID)
		}
		for sp := range wantSet {
			if !got[sp] {
				t.Errorf("%s: content spawns an imp at %v that the site does not list", s.ID, sp)
			}
		}
		for sp := range got {
			if !wantSet[sp] {
				t.Errorf("%s lists %v, where content spawns no imp", s.ID, sp)
			}
		}
	}
}

// The sites are the places content holds enough imps to feed an account, and the one such place left out is left out for
// its reason. A mapsquare with five or more imp spawns is a place; every other spawn stands alone or in a pair or a trio.
func TestImpSitesAreTheContentsClusters(t *testing.T) {
	all := contentImpSpawns(t)
	perSquare := map[[2]int]int{}
	for _, a := range all {
		perSquare[[2]int{a.X / 64, a.Z / 64}]++
	}
	var places []string
	for sq, n := range perSquare {
		if n >= 5 {
			places = append(places, fmt.Sprintf("m%d_%d:%d", sq[0], sq[1], n))
		}
	}
	sort.Strings(places)
	// Ardougne south (m41_50), Yanille north (m40_48) and the Karamja volcano (m44_49).
	want := []string{"m40_48:8", "m41_50:9", "m44_49:8"}
	if strings.Join(places, " ") != strings.Join(want, " ") {
		t.Fatalf("content's places with five or more imps are %v, want %v: the sites in impfields.go were read from those", places, want)
	}
	for _, s := range ImpSites {
		sq := fmt.Sprintf("m%d_%d:%d", s.Stand.X/64, s.Stand.Z/64, len(s.Spawns))
		found := false
		for _, p := range places {
			found = found || p == sq
		}
		if !found {
			t.Errorf("%s (%s) is not one of the content's places %v", s.ID, sq, places)
		}
	}
	// What is left out: Karamja's m44_49, across the sea; no site may stand there.
	for _, s := range ImpSites {
		if s.Stand.X/64 == 44 && s.Stand.Z/64 == 49 {
			t.Errorf("%s is the Karamja volcano, which is left out (impfields.go says why)", s.ID)
		}
	}
}

func impPack(t *testing.T) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// An account can stand on each site's stand and on every spawn, and from the stand every spawn is a short walk over open
// ground: the patrol, the open-ground test and the leash all rest on that.
func TestImpSitesAreWalkableFromTheirStand(t *testing.T) {
	pk := impPack(t)
	for _, s := range ImpSites {
		if !pk.Walkable(s.Stand.X, s.Stand.Z, s.Stand.Level) {
			t.Errorf("%s: the stand %v is not a tile an account can stand on", s.ID, s.Stand)
		}
		reach := pk.ReachFrom(pathfind.NavPoint{X: s.Stand.X, Z: s.Stand.Z, Level: s.Stand.Level}, 60, nil)
		for _, sp := range s.Spawns {
			if !pk.Walkable(sp.X, sp.Z, sp.Level) {
				t.Errorf("%s: the spawn %v cannot be stood on", s.ID, sp)
			}
			if n, ok := reach.Steps(sp.X, sp.Z, sp.Level); !ok || n > 40 {
				t.Errorf("%s: the spawn %v is %d steps from the stand over open ground (reachable %v), want at most 40", s.ID, sp, n, ok)
			}
			if !s.Has(sp) {
				t.Errorf("%s: the spawn %v is outside the site's own leash", s.ID, sp)
			}
		}
	}
}

// The ground an imp's teleport can drop it on and a walk over open ground cannot reach, which is what PickImp refuses: the
// monastery's rooms behind its doors beside the Ardougne strip, the Fight Arena's yard north of the Yanille one. Both
// were the farm's walks (the monastery door was crossed twenty times an hour by one account).
func TestImpPocketsAreNotOpenGround(t *testing.T) {
	pk := impPack(t)
	cases := []struct {
		site   ImpSite
		at     tile.Tile
		reach  bool
		reason string
	}{
		{ImpSiteArdougne, tile.Tile{X: 2634, Z: 3222}, true, "the strip's middle"},
		{ImpSiteArdougne, tile.Tile{X: 2639, Z: 3206}, true, "a spawn"},
		{ImpSiteArdougne, tile.Tile{X: 2606, Z: 3216}, false, "the monastery's north room"},
		{ImpSiteArdougne, tile.Tile{X: 2610, Z: 3208}, false, "the monastery's main hall"},
		{ImpSiteArdougne, tile.Tile{X: 2604, Z: 3212}, true, "the open ground beside the monastery's wall"},
		{ImpSiteYanille, tile.Tile{X: 2616, Z: 3126}, true, "a spawn"},
		{ImpSiteYanille, tile.Tile{X: 2600, Z: 3141}, false, "the Fight Arena's yard"},
		{ImpSiteYanille, tile.Tile{X: 2610, Z: 3143}, false, "the Fight Arena's yard"},
	}
	for _, c := range cases {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s (%v) is not a walkable tile in the pack: the case proves nothing", c.reason, c.at)
			continue
		}
		reach := pk.ReachFrom(pathfind.NavPoint{X: c.site.Stand.X, Z: c.site.Stand.Z, Level: c.site.Stand.Level}, 40, nil)
		if _, ok := reach.Steps(c.at.X, c.at.Z, c.at.Level); ok != c.reach {
			t.Errorf("%s: %v reachable from the %s stand over open ground = %v, want %v", c.reason, c.at, c.site.ID, ok, c.reach)
		}
	}
}
