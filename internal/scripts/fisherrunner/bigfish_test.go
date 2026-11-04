package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/tile"
)

// Covers the Big fish random-event recovery this task adds (RecoverLostTool and its pure
// helpers countByName/nearestGroundItemNamed) against synthetic state — see
// internal/randomevents/bigfish_test.go's own doc comment for why this was not proven via
// a live reproduction (no known cheat forces this specific event in the local test
// engine; it fires on a random roll during ordinary fishing). This is the honest
// unit-tested half the task asks for when a live proof isn't available.

// fakeObjTypes is a trivial synthetic actions.ObjTypes, the same role
// randomevents' fakeResolver plays for NPC names — standing in for *cachecfg.Config
// without building a real cache archive.
type fakeObjTypes map[int]string

func (f fakeObjTypes) Obj(id int) (cachecfg.Obj, bool) {
	name, ok := f[id]
	if !ok {
		return cachecfg.Obj{}, false
	}
	return cachecfg.Obj{ID: id, Name: name}, true
}

func TestCountByNameMatchesCaseInsensitiveAndSumsStacks(t *testing.T) {
	types := fakeObjTypes{100: "Small fishing net", 200: "Coins"}
	items := []gamestate.SlotItem{
		{Slot: 0, Item: gamestate.Item{ID: 100, Count: 1}},
		{Slot: 1, Item: gamestate.Item{ID: 200, Count: 50}},
		{Slot: 5, Item: gamestate.Item{ID: 100, Count: 0}}, // a non-stackable item's own zero-count slot still counts as 1
	}
	if got := countByName(items, types, "small fishing net"); got != 2 {
		t.Fatalf("countByName = %d, want 2", got)
	}
	if got := countByName(items, types, "Harpoon"); got != 0 {
		t.Fatalf("countByName for an absent item = %d, want 0", got)
	}
}

func TestCountByNameNilTypes(t *testing.T) {
	items := []gamestate.SlotItem{{Item: gamestate.Item{ID: 100, Count: 1}}}
	if got := countByName(items, nil, "Small fishing net"); got != 0 {
		t.Fatalf("countByName with nil types = %d, want 0", got)
	}
}

func TestNearestGroundItemNamedPicksClosestMatch(t *testing.T) {
	types := fakeObjTypes{50: "Small fishing net", 60: "Raw shrimps"}
	anchor := tile.Tile{X: 2845, Z: 3431, Level: 0}
	items := []gamestate.GroundItem{
		{ID: 60, X: 2845, Z: 3432, Level: 0}, // wrong name
		{ID: 50, X: 2850, Z: 3431, Level: 0}, // right name, distance 5
		{ID: 50, X: 2847, Z: 3431, Level: 0}, // right name, distance 2 — nearest
		{ID: 50, X: 2845, Z: 3431, Level: 1}, // right name, wrong level
	}
	got, ok := nearestGroundItemNamed(items, types, anchor, "small fishing net", 10)
	if !ok {
		t.Fatalf("expected a match")
	}
	if got.X != 2847 || got.Z != 3431 || got.Level != 0 {
		t.Fatalf("nearestGroundItemNamed = %+v, want the distance-2 stack at 2847,3431,0", got)
	}
}

func TestNearestGroundItemNamedRespectsRadius(t *testing.T) {
	types := fakeObjTypes{50: "Harpoon"}
	anchor := tile.Tile{X: 2845, Z: 3431, Level: 0}
	items := []gamestate.GroundItem{{ID: 50, X: 2845, Z: 3442, Level: 0}} // distance 11
	if _, ok := nearestGroundItemNamed(items, types, anchor, "Harpoon", 10); ok {
		t.Fatalf("expected no match outside radius")
	}
	if _, ok := nearestGroundItemNamed(items, types, anchor, "Harpoon", 11); !ok {
		t.Fatalf("expected a match exactly at radius")
	}
}

func TestNearestGroundItemNamedNilTypes(t *testing.T) {
	items := []gamestate.GroundItem{{ID: 50, X: 0, Z: 0, Level: 0}}
	if _, ok := nearestGroundItemNamed(items, nil, tile.Tile{}, "Harpoon", 10); ok {
		t.Fatalf("expected no match with nil types")
	}
}

// RecoverLostTool/toolCount/findGroundTool themselves are NOT unit-tested here: like every
// other d.Config-touching function in this file (FindSpot, BuyTool, FindBone's own
// boneburierrunner twin, ...), they're typed against the concrete *cachecfg.Config, which
// has no exported constructor and unexported fields — this project's own established
// convention is to prove that glue live (cmd/fishtest-style) rather than hand-roll a fake
// cache archive for it. countByName/nearestGroundItemNamed above are the actual matching
// logic pulled out specifically so IT can still be unit-tested without that archive; what's
// left untested here is thin plumbing (read live inventory/ground items, call the pure
// helper, log).
