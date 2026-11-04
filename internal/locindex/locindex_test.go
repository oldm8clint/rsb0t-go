package locindex

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"rsb0t-go/internal/cachecfg"
)

// fakeTypes stands in for a decoded config archive.
type fakeTypes map[int]cachecfg.Loc

func (f fakeTypes) Loc(id int) (cachecfg.Loc, bool) {
	l, ok := f[id]
	return l, ok
}

var types = fakeTypes{
	2213: {ID: 2213, Name: "Bank booth", Width: 1, Length: 1, Ops: [5]string{"Use", "Use-quickly"}},
	2646: {ID: 2646, Name: "Flax", Width: 1, Length: 1, Ops: [5]string{"", "Pick"}},
	2644: {ID: 2644, Name: "Spinning wheel", Width: 1, Length: 1, Ops: [5]string{"", "Spin"}},
	1530: {ID: 1530, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
	1531: {ID: 1531, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Close"}},
	500:  {ID: 500, Name: "Big table", Width: 3, Length: 1},
}

// smartBytes writes the map files' unsigned smart.
func smartBytes(v int) []byte {
	if v < 0x80 {
		return []byte{byte(v)}
	}
	v += 0x8000
	return []byte{byte(v >> 8), byte(v)}
}

// encodeLocFile writes locs, which must be sorted by id then coord, in the l file format.
func encodeLocFile(locs []Loc) []byte {
	var b bytes.Buffer
	prevID := -1
	for i := 0; i < len(locs); {
		id := locs[i].ID
		b.Write(smartBytes(id - prevID))
		prevID = id
		prevCoord := 0
		for ; i < len(locs) && locs[i].ID == id; i++ {
			l := locs[i]
			coord := packCoord(l.X&0x3f, l.Z&0x3f, l.Level)
			b.Write(smartBytes(coord - prevCoord + 1))
			prevCoord = coord
			b.WriteByte(byte(l.Shape<<2 | l.Angle))
		}
		b.WriteByte(0)
	}
	b.WriteByte(0)
	return b.Bytes()
}

// landFile writes an m file whose only flags are link-below on the given level-1 tiles.
func landFile(linked map[[2]int]bool) []byte {
	var b bytes.Buffer
	for level := 0; level < 4; level++ {
		for x := 0; x < 64; x++ {
			for z := 0; z < 64; z++ {
				if level == 1 && linked[[2]int{x, z}] {
					b.WriteByte(49 + linkBelow)
				}
				b.WriteByte(0)
			}
		}
	}
	return b.Bytes()
}

func TestBakeSquareDecodesAndBridges(t *testing.T) {
	const mx, mz = 42, 54
	base := func(x, z int) (int, int) { return mx<<6 | x, mz<<6 | z }
	bx, bz := base(10, 20)
	fx, fz := base(10, 21)
	cx, cz := base(63, 63)
	placed := []Loc{
		{ID: 1530, X: bx, Z: bz, Level: 0, Shape: 0, Angle: 1},
		{ID: 1530, X: fx, Z: fz, Level: 0, Shape: 0, Angle: 2},
		{ID: 2213, X: bx, Z: bz, Level: 1, Shape: 10, Angle: 3},
		{ID: 4000, X: bx, Z: bz, Level: 2, Shape: 22, Angle: 1},
		{ID: 4000, X: cx, Z: cz, Level: 2, Shape: 22, Angle: 0},
	}
	m := MapFile{MX: mx, MZ: mz, Land: landFile(map[[2]int]bool{{10, 20}: true}), Loc: encodeLocFile(placed)}
	got, err := BakeSquare(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The tile 10,20 has link-below on level 1, so GameMap.loadLocations moves every loc on
	// it down a level: level 1 becomes 0, level 2 becomes 1, and level 0 would become -1,
	// which the engine drops.
	want := []Loc{
		placed[1],
		{ID: 2213, X: bx, Z: bz, Level: 0, Shape: 10, Angle: 3},
		{ID: 4000, X: bx, Z: bz, Level: 1, Shape: 22, Angle: 1},
		placed[4],
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("baked\n got %+v\nwant %+v", got, want)
	}

	kept, _ := BakeSquare(m, func(id int) bool { return id == 2213 })
	if len(kept) != 1 || kept[0].ID != 2213 {
		t.Fatalf("keep filter: %+v", kept)
	}
	if _, err := BakeSquare(MapFile{Land: m.Land, Loc: m.Loc[:3]}, nil); err == nil {
		t.Fatal("truncated loc file baked without error")
	}
}

func TestIndexRoundTripAndLookup(t *testing.T) {
	locs := []Loc{
		{ID: 2213, X: 2725, Z: 3491, Level: 0, Shape: 10},
		{ID: 2646, X: 2741, Z: 3444, Level: 0, Shape: 10, Angle: 2},
		{ID: 2644, X: 2711, Z: 3471, Level: 1, Shape: 10, Angle: 1},
		{ID: 65535, X: 16383, Z: 16383, Level: 3, Shape: 22, Angle: 3},
	}
	ix, err := New(locs)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, ix); err != nil {
		t.Fatal(err)
	}
	back, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if back.Count() != 4 || back.Squares() != 3 {
		t.Fatalf("count %d squares %d", back.Count(), back.Squares())
	}
	for _, l := range locs {
		if got := back.At(l.X, l.Z, l.Level); len(got) != 1 || got[0] != l {
			t.Errorf("At(%d,%d,%d) = %+v, want %+v", l.X, l.Z, l.Level, got, l)
		}
	}
	if got := back.At(2711, 3471, 0); got != nil {
		t.Errorf("wrong level matched: %+v", got)
	}
	if _, err := Read(bytes.NewReader([]byte("nope"))); err == nil {
		t.Error("garbage read without error")
	}
	if _, err := New([]Loc{{ID: 1, Shape: 23}}); err == nil {
		t.Error("invalid shape accepted")
	}
}

func seers(t *testing.T) *Index {
	t.Helper()
	ix, err := New([]Loc{
		{ID: 2213, X: 2725, Z: 3491, Level: 0, Shape: 10},
		{ID: 2213, X: 2728, Z: 3491, Level: 0, Shape: 10},
		{ID: 2646, X: 2740, Z: 3445, Level: 0, Shape: 10},
		{ID: 2646, X: 2741, Z: 3444, Level: 0, Shape: 10},
		{ID: 2644, X: 2711, Z: 3471, Level: 1, Shape: 10},
		{ID: 1530, X: 2720, Z: 3480, Level: 0, Shape: 0},
		{ID: 500, X: 2700, Z: 3500, Level: 0, Shape: 10, Angle: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestNearestByNameOpLevelAndRadius(t *testing.T) {
	ix := seers(t)
	m, ok := ix.Nearest(types, Query{X: 2727, Z: 3493, Level: 0, Radius: 10, Name: "bank BOOTH", Op: "use-quickly"}, nil)
	if !ok || m.X != 2728 || m.Op != 2 || m.Distance != 2 || m.Live {
		t.Fatalf("booth = %+v ok=%v", m, ok)
	}
	if _, ok := ix.Nearest(types, Query{X: 2727, Z: 3493, Level: 0, Radius: 10, Name: "Bank booth", Op: "Deposit"}, nil); ok {
		t.Error("matched a missing option")
	}
	if _, ok := ix.Nearest(types, Query{X: 2711, Z: 3471, Level: 0, Radius: 5, Name: "Spinning wheel"}, nil); ok {
		t.Error("matched a loc on another level")
	}
	if m, ok := ix.Nearest(types, Query{X: 2711, Z: 3475, Level: 1, Radius: 5, Name: "Spinning wheel", Op: "Spin"}, nil); !ok || m.Op != 2 {
		t.Errorf("wheel = %+v ok=%v", m, ok)
	}
	if _, ok := ix.Nearest(types, Query{X: 2727, Z: 3493, Level: 0, Radius: 60, Name: "Flax"}, nil); !ok {
		t.Error("flax not found within 60")
	}
	if _, ok := ix.Nearest(types, Query{X: 2727, Z: 3493, Level: 0, Radius: 40, Name: "Flax"}, nil); ok {
		t.Error("flax found outside the radius")
	}
	if all := ix.Find(types, Query{X: 2730, Z: 3445, Level: 0, Radius: 20, IDs: []int{2646}}, nil); len(all) != 2 || all[0].X != 2740 {
		t.Errorf("by id = %+v", all)
	}
}

func TestFootprintDistanceAndRotation(t *testing.T) {
	ix := seers(t)
	// The 3x1 table covers x 2700-2702 at angle 0.
	m, ok := ix.Nearest(types, Query{X: 2703, Z: 3500, Level: 0, Radius: 1, IDs: []int{500}}, nil)
	if !ok || m.Distance != 1 {
		t.Fatalf("table = %+v ok=%v", m, ok)
	}
	rotated, _ := New([]Loc{{ID: 500, X: 2700, Z: 3500, Level: 0, Shape: 10, Angle: 1}})
	if _, ok := rotated.Nearest(types, Query{X: 2703, Z: 3500, Level: 0, Radius: 1, IDs: []int{500}}, nil); ok {
		t.Error("a quarter-turned table still reached x 2702")
	}
	if m, ok := rotated.Nearest(types, Query{X: 2700, Z: 3503, Level: 0, Radius: 1, IDs: []int{500}}, nil); !ok || m.Distance != 1 {
		t.Errorf("rotated table along z = %+v ok=%v", m, ok)
	}
}

func TestLiveChangesOverrideTheMapFiles(t *testing.T) {
	ix := seers(t)
	q := Query{X: 2741, Z: 3446, Level: 0, Radius: 5, Name: "Flax", Op: "Pick"}

	// A picked flax: LOC_DEL on the ground-object layer of its tile.
	picked := []Change{{X: 2740, Z: 3445, Level: 0, Shape: 10, Deleted: true}}
	m, ok := ix.Nearest(types, q, picked)
	if !ok || m.X != 2741 || m.Z != 3444 {
		t.Fatalf("after picking the nearer flax: %+v ok=%v", m, ok)
	}
	// A deletion on a different layer of the same tile does not remove the flax.
	wall := []Change{{X: 2740, Z: 3445, Level: 0, Shape: 0, Deleted: true}}
	if m, _ := ix.Nearest(types, q, wall); m.X != 2740 {
		t.Errorf("a wall deletion removed the flax: %+v", m)
	}
	// Both picked: nothing left.
	both := append(picked, Change{X: 2741, Z: 3444, Level: 0, Shape: 10, Deleted: true})
	if _, ok := ix.Nearest(types, q, both); ok {
		t.Error("flax found after both were picked")
	}

	// A door opened: the closed door (1530) is replaced by an open one (1531) on the wall layer.
	opened := []Change{{X: 2720, Z: 3480, Level: 0, ID: 1531, Shape: 0, Angle: 1}}
	dq := Query{X: 2720, Z: 3481, Level: 0, Radius: 2, Name: "Door"}
	d, ok := ix.Nearest(types, dq, opened)
	if !ok || d.ID != 1531 || !d.Live || d.Angle != 1 {
		t.Fatalf("door after opening = %+v ok=%v", d, ok)
	}
	if _, ok := ix.Nearest(types, Query{X: 2720, Z: 3481, Level: 0, Radius: 2, Name: "Door", Op: "Open"}, opened); ok {
		t.Error("the replaced closed door is still offered")
	}
	// A loc added where the map has nothing.
	added := []Change{{X: 2745, Z: 3446, Level: 0, ID: 2646, Shape: 10}}
	all := ix.Find(types, Query{X: 2745, Z: 3446, Level: 0, Radius: 6, Name: "Flax"}, append(added, picked...))
	if len(all) != 2 || !all[0].Live || all[0].X != 2745 {
		t.Errorf("added flax = %+v", all)
	}
}

// TestFindPrefersLiveChangeOverStaticEvenWhenAdjacent reproduces, in isolation, a real
// production discrepancy investigated 2026-09-16: two live 30-account-fleet accounts opened
// Lumbridge castle "Door"s (static ids 1530 at 3208,3211,0 and 1536 at 3215,3211,0, confirmed
// against a real baked index via cmd/findloc) and every later query for the SAME door reported
// its position as one tile further along its own wall-normal (3208,3212,0 / 3215,3212,0) instead
// of the closed door's own static tile — initially suspected as a locindex coordinate bug (an
// off-by-one in Match construction or in how a wall-shape loc's origin gets read back).
//
// It is not a bug: real production logs (grep "chosen via flood-fill exclusion" across the
// fleet's own run logs) show the SAME account querying moments apart, opening the CLOSED door
// discovered at 3215,3211,0 and then immediately finding it "already open" at 3215,3212,0 — one
// tile off. That is simply where the live LOC_ADD_CHANGE this era's door-open script sends
// actually lands (this project's client decodes LOC_ADD_CHANGE's tile exactly as the server
// sends it — see gamestate.applyLocAddChange/zoneTile, which has no per-shape/angle adjustment
// to get wrong), most likely because this content's open-door replacement is authored on the
// tile the leaf swings into rather than the tile the closed wall occupies. Because a live Change
// only overrides the static map on the EXACT tile it names (see Find's own "changed" map, keyed
// by tile+layer), the two coordinates don't collide or replace one another — they coexist as two
// separate matches for the same conceptual door, exactly like tryOpenDoor's own find()/findOpen()
// pair (searching op "Open" vs op "Close") each seeing a different one. This test pins that
// coexistence down so a future change to Find/Nearest can't quietly "fix" it into a bug: the
// closed match should keep resolving at the door's own static tile, the open match one tile
// off, and Nearest should return whichever is actually closer to the query tile, the same
// distance comparison tryOpenDoor itself makes between closedMatch and openMatch.
func TestFindPrefersLiveChangeOverStaticEvenWhenAdjacent(t *testing.T) {
	ix := seers(t) // has closed Door id 1530 at 2720,3480,0, angle 0 (shape 0, a wall)
	// The open-door replacement (id 1531, op "Close") lands one tile off — 2720,3481 — not on
	// top of the closed door's own tile, mirroring the real 3208,3211->3208,3212 shift.
	opened := []Change{{X: 2720, Z: 3481, Level: 0, ID: 1531, Shape: 0, Angle: 1}}

	// The static closed door is untouched: still there, at its own tile, still offering "Open".
	closed, ok := ix.Nearest(types, Query{X: 2720, Z: 3480, Level: 0, Radius: 5, Name: "Door", Op: "Open"}, opened)
	if !ok || closed.X != 2720 || closed.Z != 3480 || closed.Live {
		t.Fatalf("closed match = %+v ok=%v, want the untouched static door at 2720,3480", closed, ok)
	}
	// The live open replacement resolves at its OWN (adjacent, not identical) tile.
	open, ok := ix.Nearest(types, Query{X: 2720, Z: 3480, Level: 0, Radius: 5, Name: "Door", Op: "Close"}, opened)
	if !ok || open.X != 2720 || open.Z != 3481 || !open.Live {
		t.Fatalf("open match = %+v ok=%v, want the live door at 2720,3481", open, ok)
	}
	// Querying from beside the LIVE tile, Nearest should prefer whichever match is actually
	// closer — the same "closedMatch vs openMatch, take the nearer" comparison tryOpenDoor makes
	// live. From 2720,3482 (beside the open tile), the open match (distance 1) beats the closed
	// one (distance 2).
	if m, ok := ix.Nearest(types, Query{X: 2720, Z: 3482, Level: 0, Radius: 5, Name: "Door", Op: "Close"}, opened); !ok || m.Z != 3481 {
		t.Fatalf("nearest from beside the live tile = %+v ok=%v, want the live door", m, ok)
	}
}

// TestRealIndex checks a baked index against the local 289 content when one is supplied:
// RSB0T_LOC_INDEX=<index file> RSB0T_CONFIG_JAG=<config archive>.
func TestRealIndex(t *testing.T) {
	path, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" || cfgPath == "" {
		t.Skip("set RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG to check a baked index")
	}
	ix, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, op    string
		x, z, level int
		radius      int
	}{
		{"Bank booth", "Use-quickly", 2725, 3493, 0, 10},
		{"Flax", "Pick", 2741, 3444, 0, 10},
		{"Spinning wheel", "Spin", 2711, 3471, 1, 5},
	} {
		m, ok := ix.Nearest(cfg, Query{X: c.x, Z: c.z, Level: c.level, Radius: c.radius, Name: c.name, Op: c.op}, nil)
		if !ok {
			t.Errorf("no %s near %d,%d,%d", c.name, c.x, c.z, c.level)
			continue
		}
		t.Logf("%s: id %d at %d,%d,%d op %d distance %d", c.name, m.ID, m.X, m.Z, m.Level, m.Op, m.Distance)
	}
}
