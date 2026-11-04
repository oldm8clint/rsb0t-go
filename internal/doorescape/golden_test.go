package doorescape

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// goldenFile is test/fixtures/castle_collision_golden.json, captured by castle_collision_reference_vector.ts from
// the REAL Engine-TS collision code (StepValidator.canTravel, ReachStrategy.reached, CollisionEngine's own flag
// arithmetic) over the 289 map data.
type goldenFile struct {
	Region       goldenRegion  `json:"region"`
	DigestRegion goldenRegion  `json:"digestRegion"`
	ReachRadius  int           `json:"reachRadius"`
	Doors        []goldenLoc   `json:"doors"`
	Stairs       []goldenLoc   `json:"stairs"`
	LocTypes     map[string]gT `json:"locTypes"`
	Targets      []goldenLoc   `json:"targets"`
	Approach     map[string][][]string
	States       []goldenState `json:"states"`
}

type goldenRegion struct{ X0, Z0, X1, Z1, Levels int }

type goldenLoc struct{ ID, X, Z, Level, Angle, Shape int }

type gT struct {
	Name          string `json:"name"`
	Width, Length int
	Blockwalk     bool
	Blockrange    bool
	Active        int
	ForceApproach int `json:"forceapproach"`
}

type goldenState struct {
	Leaves string `json:"leaves"`
	Exits  string `json:"exits"`
	Reach  string `json:"reach"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	b, err := os.ReadFile("../../test/fixtures/castle_collision_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g goldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// fnv is FNV-1a over bytes, as the generator's Fnv.
type fnv uint32

func newFnv() fnv         { return 0x811c9dc5 }
func (h *fnv) add(b byte) { *h = (*h ^ fnv(b)) * 0x01000193 }
func (h fnv) hex() string { return fmt.Sprintf("%08x", uint32(h)) }

func stateFromLeaves(t *testing.T, leaves string, doors int) State {
	t.Helper()
	if len(leaves) != 2*doors {
		t.Fatalf("leaves %q is not 2 per door (%d doors)", leaves, doors)
	}
	var s State
	for i := 0; i < doors; i++ {
		s = s.With(i, leaves[2*i] == '1', leaves[2*i+1] == '1')
	}
	return s
}

func castle(t *testing.T) *Catalog {
	t.Helper()
	c, err := Castle()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The catalogue is the map's own placement of every door and staircase inside the castle: nothing missed,
// nothing invented, every angle and shape as placed.
func TestCatalogueIsTheMapsPlacements(t *testing.T) {
	g := loadGolden(t)
	c := castle(t)
	if len(g.Doors) != len(c.Doors) {
		t.Fatalf("the map places %d doors in the castle, the catalogue has %d", len(g.Doors), len(c.Doors))
	}
	for i, d := range c.Doors {
		m := g.Doors[i]
		if m.ID != d.ID || m.X != d.X || m.Z != d.Z || m.Level != d.Level || m.Angle != d.Angle || m.Shape != d.Shape {
			t.Errorf("door %d %q: catalogue %+v, map %+v", i, d.Name, d, m)
		}
		if lt := g.LocTypes[fmt.Sprint(d.OpenID)]; lt.Name != "Door" || !lt.Blockwalk {
			t.Errorf("door %q: open leaf %d is %+v in the config, want a walking-blocking Door", d.Name, d.OpenID, lt)
		}
		if lt := g.LocTypes[fmt.Sprint(d.ID)]; lt.Name != "Door" || !lt.Blockwalk || lt.Width != 1 || lt.Length != 1 {
			t.Errorf("door %q: closed leaf %d is %+v in the config, want a 1x1 walking-blocking Door", d.Name, d.ID, lt)
		}
	}
	// Every staircase op in the catalogue is on a staircase the map places, from one level to the next.
	placed := map[goldenLoc]bool{}
	for _, s := range g.Stairs {
		placed[s] = true
	}
	for _, s := range c.Stairs {
		p := s.Placement
		if !placed[goldenLoc{p.ID, p.X, p.Z, p.Level, p.Angle, p.Shape}] {
			t.Errorf("stair %q is not placed by the map: %+v", s.Name, p)
		}
		lt := g.LocTypes[fmt.Sprint(p.ID)]
		if lt.Width != s.Type.Width || lt.Length != s.Type.Length || lt.ForceApproach != s.Type.ForceApproach {
			t.Errorf("stair %q: type %+v, config %+v", s.Name, s.Type, lt)
		}
		if s.Up == (s.Land.Level < p.Level) {
			t.Errorf("stair %q: Up=%v but it lands on level %d from %d", s.Name, s.Up, s.Land.Level, p.Level)
		}
	}
}

// The leaves land where the script puts them: the open leaf of every door is the closed leaf's own tile moved by
// door_open and turned a quarter, and closing it (door_close) is the exact reverse.
func TestOpenLeafIsWhereTheScriptPutsIt(t *testing.T) {
	c := castle(t)
	// door_close's offset by the OPEN leaf's angle, door_procs.rs2 [proc,door_close], straight then diagonal.
	closeOffset := func(angle, shape int) (int, int) {
		if shape == ShapeWallStraight {
			return [4]int{0, 1, 0, -1}[angle], [4]int{1, 0, -1, 0}[angle]
		}
		return [4]int{1, 0, -1, 0}[angle], [4]int{0, -1, 0, 1}[angle]
	}
	for _, d := range c.Doors {
		o := d.Open()
		if o.Angle != (d.Angle+1)%4 || o.Shape != d.Shape || o.Level != d.Level {
			t.Errorf("%s: open leaf %+v is not the closed leaf turned a quarter", d.Name, o)
		}
		dx, dz := closeOffset(o.Angle, o.Shape)
		if o.X+dx != d.X || o.Z+dz != d.Z {
			t.Errorf("%s: closing the open leaf at %d,%d moves it by %d,%d to %d,%d, not back to %d,%d", d.Name, o.X, o.Z, dx, dz, o.X+dx, o.Z+dz, d.X, d.Z)
		}
		if (o.Angle+3)%4 != d.Angle {
			t.Errorf("%s: closing the open leaf does not restore the angle", d.Name)
		}
	}
	// What the fleet's own log saw: after the closed leaves at 3292,3931, 3294,3932 and 3283,3931 were operated, a
	// "Door" was reported at 3293,3931, 3294,3931 and 3284,3931 ("... is already open").
	want := map[[2]int][2]int{{3292, 3931}: {3293, 3931}, {3294, 3932}: {3294, 3931}, {3283, 3931}: {3284, 3931}}
	n := 0
	for _, d := range c.Doors {
		if w, ok := want[[2]int{d.X, d.Z}]; ok {
			n++
			if o := d.Open(); o.X != w[0] || o.Z != w[1] {
				t.Errorf("%s: open leaf at %d,%d, the fleet log saw it at %d,%d", d.Name, o.X, o.Z, w[0], w[1])
			}
		}
	}
	if n != len(want) {
		t.Errorf("only %d of the %d doors the log saw are in the catalogue", n, len(want))
	}
	// The diagonal doors push an account standing on the leaf's tile one tile aside: the main door's leaf lands
	// on 3279,3938 and pushes east to 3280,3938, which is where the fleet's walker found its account.
	for _, d := range c.Doors {
		if d.X == 3279 && d.Z == 3939 {
			p, ok := d.Push()
			if !ok || p != (Tile{3280, 3938, 0}) {
				t.Errorf("main door push = %v %v, want 3280,3938,0", p, ok)
			}
			if o := d.Open(); o.X != 3279 || o.Z != 3938 {
				t.Errorf("main door's open leaf at %d,%d, want 3279,3938", o.X, o.Z)
			}
		}
		if _, ok := d.Push(); ok != d.Diagonal() {
			t.Errorf("%s: pushes=%v but diagonal=%v", d.Name, ok, d.Diagonal())
		}
	}
}

// TestAgainstTheEngine is the proof the port is the engine: for 200 door states (all closed, all open, each
// door alone, every pair, and 120 random ones, including leaves both present or both gone) the eight-direction
// step mask of every tile of the castle's region on every level, and every reach answer around every door
// leaf and staircase, hash to what Engine-TS's own StepValidator and ReachStrategy computed over the same map
// with the doors' loc_del and loc_add applied by CollisionEngine.
func TestAgainstTheEngine(t *testing.T) {
	g := loadGolden(t)
	c := castle(t)
	r := g.DigestRegion
	bad := 0
	for n, st := range g.States {
		s := stateFromLeaves(t, st.Leaves, len(c.Doors))
		f := c.FlagsAt(s)
		h := newFnv()
		for level := 0; level < g.Region.Levels; level++ {
			for z := r.Z0; z <= r.Z1; z++ {
				for x := r.X0; x <= r.X1; x++ {
					h.add(ExitMask(f, level, x, z))
				}
			}
		}
		if h.hex() != st.Exits {
			bad++
			t.Errorf("state %d (%s): step masks hash to %s, the engine's to %s", n, st.Leaves, h.hex(), st.Exits)
		}
		rh := newFnv()
		for _, tg := range g.Targets {
			lt := g.LocTypes[fmt.Sprint(tg.ID)]
			p := Placement{ID: tg.ID, X: tg.X, Z: tg.Z, Level: tg.Level, Angle: tg.Angle, Shape: tg.Shape}
			typ := LocType{Width: lt.Width, Length: lt.Length, ForceApproach: lt.ForceApproach}
			for dz := -g.ReachRadius; dz <= g.ReachRadius; dz++ {
				for dx := -g.ReachRadius; dx <= g.ReachRadius; dx++ {
					if Reached(f, tg.Level, tg.X+dx, tg.Z+dz, p, typ) {
						rh.add(1)
					} else {
						rh.add(0)
					}
				}
			}
		}
		if rh.hex() != st.Reach {
			bad++
			t.Errorf("state %d (%s): reach answers hash to %s, the engine's to %s", n, st.Leaves, rh.hex(), st.Reach)
		}
		if bad > 10 {
			t.Fatal("too many mismatches")
		}
	}
	if len(g.States) < 200 {
		t.Errorf("the golden file holds %d states, want 200", len(g.States))
	}
}

// The readable half of the same proof: the approach tiles of every door leaf and staircase in the two uniform
// states, as the engine lists them (the approach tables are in the golden file as "x,z" strings).
func TestApproachTilesAreTheEngines(t *testing.T) {
	g := loadGolden(t)
	c := castle(t)
	for name, s := range map[string]State{"closed": c.DefaultState, "open": allOpen(c)} {
		f := c.FlagsAt(s)
		for i, tg := range g.Targets {
			lt := g.LocTypes[fmt.Sprint(tg.ID)]
			p := Placement{ID: tg.ID, X: tg.X, Z: tg.Z, Level: tg.Level, Angle: tg.Angle, Shape: tg.Shape}
			typ := LocType{Width: lt.Width, Length: lt.Length, ForceApproach: lt.ForceApproach}
			var got []string
			for dz := -g.ReachRadius; dz <= g.ReachRadius; dz++ {
				for dx := -g.ReachRadius; dx <= g.ReachRadius; dx++ {
					if Reached(f, tg.Level, tg.X+dx, tg.Z+dz, p, typ) {
						got = append(got, fmt.Sprintf("%d,%d", tg.X+dx, tg.Z+dz))
					}
				}
			}
			want := append([]string(nil), g.Approach[name][i]...)
			sort.Strings(got)
			sort.Strings(want)
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("%s state, loc %d at %d,%d (level %d): reach from %v, the engine's %v", name, tg.ID, tg.X, tg.Z, tg.Level, got, want)
			}
		}
	}
}

func allOpen(c *Catalog) State {
	var s State
	for i := range c.Doors {
		s = s.With(i, false, true)
	}
	return s
}
