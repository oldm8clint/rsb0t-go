package mazegraph

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// layoutLocs is MazeLayout as a Loc list, to build a Graph without the chests.
func layoutLocs() []Loc {
	locs := make([]Loc, 0, len(MazeLayout)/LayoutStride)
	for i := 0; i+LayoutStride <= len(MazeLayout); i += LayoutStride {
		locs = append(locs, Loc{LX: MazeLayout[i], LZ: MazeLayout[i+1], ID: MazeLayout[i+2], Shape: MazeLayout[i+3], Angle: MazeLayout[i+4]})
	}
	return locs
}

// TestChestsMatchTheMapFile pins Chests to content/maps/m45_71.jm2 when the content checkout is
// there (RSB0T_CONTENT_DIR, or the sibling "content" directory of the repo's parent).
func TestChestsMatchTheMapFile(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "..", "content")
	}
	f, err := os.Open(filepath.Join(dir, "maps", "m45_71.jm2"))
	if err != nil {
		t.Skipf("no content checkout (%v)", err)
	}
	defer f.Close()
	want := map[Cell]bool{}
	inLoc := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "====") {
			inLoc = line == "==== LOC ===="
			continue
		}
		if !inLoc {
			continue
		}
		// "level lx lz: id shape angle"
		head, tail, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		hf, tf := strings.Fields(head), strings.Fields(tail)
		if len(hf) != 3 || len(tf) < 2 {
			continue
		}
		id, _ := strconv.Atoi(tf[0])
		if id != ChestLocID {
			continue
		}
		level, _ := strconv.Atoi(hf[0])
		lx, _ := strconv.Atoi(hf[1])
		lz, _ := strconv.Atoi(hf[2])
		if level == 0 {
			want[Cell{X: Origin.X + lx, Z: Origin.Z + lz}] = true
		}
	}
	if len(want) != len(Chests) {
		t.Fatalf("the map has %d chests, Chests has %d", len(want), len(Chests))
	}
	for _, c := range Chests {
		if !want[c] {
			t.Errorf("chest %v is not on the map", c)
		}
	}
}

func TestEveryChestHasAStand(t *testing.T) {
	g := BuildLayoutGraph()
	if len(Chests) != 12 {
		t.Fatalf("%d chests, want 12", len(Chests))
	}
	for _, c := range Chests {
		if !g.Solid[c] {
			t.Errorf("chest %v is not solid", c)
		}
		stands := g.ChestStands(c)
		if len(stands) == 0 {
			t.Errorf("chest %v has no tile to open it from", c)
		}
		for _, s := range stands {
			if chebyshevDist(s, c) != 1 || (s.X != c.X && s.Z != c.Z) {
				t.Errorf("stand %v of chest %v is not orthogonally adjacent", s, c)
			}
			if g.WallEdge[EdgeKey(s.X, s.Z, c.X, c.Z)] {
				t.Errorf("stand %v of chest %v has a wall between", s, c)
			}
		}
	}
}

// A chest blocks its tile, but the maze must stay as solvable as before: from every open tile the
// door route to the shrine is the same with the chests solid as without (they sit in rooms, not
// in the corridors the route needs), or both fail.
func TestChestsChangeNoRoute(t *testing.T) {
	with := BuildLayoutGraph()
	without := BuildMaze(layoutLocs())
	// One SolveRoute costs ~70ms on the live laptop, so every start tile (2797 of them, 397s) is
	// opt-in: RSB0T_MAZE_EXHAUSTIVE=1. The default samples every eighth tile in each direction plus
	// every tile beside a chest, where a difference would show first. The exhaustive run on
	// 13 Oct 2026 found 0 differing routes in 2797 start tiles.
	stride := 8
	if os.Getenv("RSB0T_MAZE_EXHAUSTIVE") == "1" {
		stride = 1
	}
	nearChest := map[Cell]bool{}
	for _, ch := range Chests {
		for _, s := range with.ChestStands(ch) {
			nearChest[s] = true
		}
	}
	differ, checked := 0, 0
	for x := with.MinX; x <= with.MaxX; x++ {
		for z := with.MinZ; z <= with.MaxZ; z++ {
			c := Cell{X: x, Z: z}
			if with.Solid[c] {
				continue
			}
			if stride > 1 && !nearChest[c] && (x%stride != 0 || z%stride != 0) {
				continue
			}
			a, b := SolveRoute(with, c, Shrine), SolveRoute(without, c, Shrine)
			checked++
			if len(a) != len(b) {
				differ++
				if differ <= 5 {
					t.Logf("start %v: %d doors with chests solid, %d without", c, len(a), len(b))
				}
				continue
			}
			for i := range a {
				if a[i].Tile != b[i].Tile {
					differ++
					if differ <= 5 {
						t.Logf("start %v: door %d is %v with chests solid, %v without", c, i, a[i].Tile, b[i].Tile)
					}
					break
				}
			}
		}
	}
	t.Logf("%d start tiles checked, %d with a different route", checked, differ)
	// A start tile beside a chest may be routed round it; the four spawns, where every solve begins,
	// must not be.
	for _, sp := range Spawns {
		a, b := SelectRoute(sp), SolveRoute(without, sp, Shrine)
		if len(a) != len(b) {
			t.Fatalf("spawn %v: %d doors with the chests solid, %d without", sp, len(a), len(b))
		}
		for i := range a {
			if a[i].Tile != b[i].Tile || a[i].From != b[i].From {
				t.Errorf("spawn %v: door %d differs: %+v vs %+v", sp, i, a[i], b[i])
			}
		}
	}
}

// Along each spawn's route the rooms it passes through hold three chests (the other nine are in
// rooms the route never enters): the figure the loot policy and its docs rest on.
func TestRouteRoomsHoldThreeChestsEach(t *testing.T) {
	g := BuildLayoutGraph()
	for _, sp := range Spawns {
		cur := sp
		seen := map[Cell]bool{}
		for _, d := range append(SelectRoute(sp), DoorInfo{}) {
			room := g.Room(cur, 4000)
			for _, c := range Chests {
				for _, s := range g.ChestStands(c) {
					if room[s] {
						seen[c] = true
					}
				}
			}
			cur = d.To
		}
		if len(seen) != 3 {
			t.Errorf("spawn %v: route rooms hold %d chests %v, want 3", sp, len(seen), seen)
		}
	}
}

func TestEverySpawnIsNearAWall(t *testing.T) {
	g := BuildLayoutGraph()
	for _, sp := range Spawns {
		w, steps, ok := g.NearestWall(sp, 3)
		if !ok {
			t.Errorf("no wall within 3 steps of spawn %v", sp)
			continue
		}
		t.Logf("spawn %v: wall at %v, %d step(s)", sp, w, steps)
	}
}

func TestNearestWallPrefersTheOwnTile(t *testing.T) {
	g := BuildLayoutGraph()
	for _, d := range SelectRoute(Spawns[0]) {
		if !WallAnchoredAt(d.To) {
			continue
		}
		if w, steps, ok := g.NearestWall(d.To, 3); !ok || steps != 0 || w != d.To {
			t.Errorf("a wall is anchored on %v but NearestWall gave %v, %d, %v", d.To, w, steps, ok)
		}
	}
}

func TestRoomIsBoundedByDoors(t *testing.T) {
	g := BuildLayoutGraph()
	route := SelectRoute(Spawns[0])
	room := g.Room(Spawns[0], 4000)
	if !room[route[0].From] {
		t.Fatalf("the first door's approach tile %v is not in the spawn room", route[0].From)
	}
	if room[route[0].To] {
		t.Errorf("the tile %v across the first door is in the spawn room", route[0].To)
	}
}

func TestDistancesAreStepCounts(t *testing.T) {
	g := BuildLayoutGraph()
	d := g.Distances(Spawns[0], 4000)
	if d[Spawns[0]] != 0 {
		t.Fatalf("distance to itself = %d", d[Spawns[0]])
	}
	route := SelectRoute(Spawns[0])
	path := g.PathWithinRoom(Spawns[0], route[0].From, 0, 4000)
	if want := len(path) - 1; d[route[0].From] != want {
		t.Errorf("distance to the first approach tile = %d, PathWithinRoom walks %d", d[route[0].From], want)
	}
	if _, in := d[route[0].To]; in {
		t.Errorf("the tile across the first door %v is in the spawn room", route[0].To)
	}
}
