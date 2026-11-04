package controlpanel

import (
	"reflect"
	"testing"
)

func TestPoiCategory(t *testing.T) {
	cases := []struct {
		name     string
		ops      [5]string
		wantCat  string
		wantName string
	}{
		{"Bank booth", [5]string{"Use", "Use-quickly"}, "bank", "Bank"},
		{"Bank chest", [5]string{"Use"}, "bank", "Bank"},
		{"Anvil", [5]string{}, "anvil", "Anvil"},
		{"Furnace", [5]string{"", "Smelt"}, "furnace", "Furnace"},
		{"Cooking range", [5]string{}, "range", "Cooking range"},
		// Altars share the plain name "Altar" across many distinct ids in this era's
		// content (confirmed live against the real config archive — see this file's own
		// poiCategory doc comment) — Op is what actually distinguishes them, not Name.
		{"Chaos altar", [5]string{"Pray-at", "Check"}, "altar", "Chaos altar"},
		{"Altar of Guthix", [5]string{"Pray-at"}, "altar", "Altar of Guthix"},
		{"Altar", [5]string{"Craft-rune"}, "rc_altar", "Altar"},
		// Not a POI at all: an ordinary loc with none of the matched names/ops.
		{"Oak tree", [5]string{"Chop down"}, "", ""},
		{"Door", [5]string{"Open"}, "", ""},
	}
	for _, c := range cases {
		gotCat, gotName := poiCategory(c.name, c.ops)
		if gotCat != c.wantCat || gotName != c.wantName {
			t.Errorf("poiCategory(%q, %v) = (%q, %q), want (%q, %q)", c.name, c.ops, gotCat, gotName, c.wantCat, c.wantName)
		}
	}
}

func TestIsDoorName(t *testing.T) {
	for _, name := range []string{"Door", "door", "GATE", "Large door", "Iron gate", "Wooden door", "Metal door"} {
		if !isDoorName(name) {
			t.Errorf("isDoorName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"Wall", "Fence", "Bank booth", "Doorway", ""} {
		if isDoorName(name) {
			t.Errorf("isDoorName(%q) = true, want false", name)
		}
	}
}

// TestWallSegmentsFor checks every real shape/angle combination computeWalls decodes against
// CollisionEngine.ts's own real coordinates (changeWallStraight/changeWallL, read directly
// from the Engine-TS checkout — see wallSegmentsFor's own doc comment), by hand, tile (10,20).
func TestWallSegmentsFor(t *testing.T) {
	const x, z = 10, 20
	cases := []struct {
		name         string
		shape, angle int
		want         []wallSegment
	}{
		{"straight west", 0, 0, []wallSegment{{X0: x, Z0: z, X1: x, Z1: z + 1}}},
		{"straight north", 0, 1, []wallSegment{{X0: x, Z0: z + 1, X1: x + 1, Z1: z + 1}}},
		{"straight east", 0, 2, []wallSegment{{X0: x + 1, Z0: z, X1: x + 1, Z1: z + 1}}},
		{"straight south", 0, 3, []wallSegment{{X0: x, Z0: z, X1: x + 1, Z1: z}}},
		{"L west (north+west)", 2, 0, []wallSegment{
			{X0: x, Z0: z + 1, X1: x + 1, Z1: z + 1},
			{X0: x, Z0: z, X1: x, Z1: z + 1},
		}},
		{"L north (north+east)", 2, 1, []wallSegment{
			{X0: x, Z0: z + 1, X1: x + 1, Z1: z + 1},
			{X0: x + 1, Z0: z, X1: x + 1, Z1: z + 1},
		}},
		{"L east (south+east)", 2, 2, []wallSegment{
			{X0: x, Z0: z, X1: x + 1, Z1: z},
			{X0: x + 1, Z0: z, X1: x + 1, Z1: z + 1},
		}},
		{"L south (south+west)", 2, 3, []wallSegment{
			{X0: x, Z0: z, X1: x + 1, Z1: z},
			{X0: x, Z0: z, X1: x, Z1: z + 1},
		}},
		// Corner posts (shapes 1, 3) are deliberately not rendered as a line — see
		// wallSegmentsFor/computeWalls's own doc comment on that disclosed simplification.
		{"diagonal corner shape (unrendered)", 1, 0, nil},
		{"square corner shape (unrendered)", 3, 0, nil},
		// A non-wall shape (e.g. 9, WALL_DIAGONAL / centrepiece) produces nothing either;
		// computeWalls itself filters these out before calling wallSegmentsFor via
		// locindex.Layer, so this only guards wallSegmentsFor's own switch defaulting safely.
		{"non-wall shape", 9, 0, nil},
	}
	for _, c := range cases {
		got := wallSegmentsFor(c.shape, c.angle, x, z, false)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: wallSegmentsFor(%d,%d,%d,%d,false) = %+v, want %+v", c.name, c.shape, c.angle, x, z, got, c.want)
		}
	}
}

func TestWallSegmentsForDoorFlag(t *testing.T) {
	got := wallSegmentsFor(0, 0, 5, 5, true)
	if len(got) != 1 || !got[0].Door {
		t.Errorf("wallSegmentsFor(..., true) = %+v, want one segment with Door=true", got)
	}
}

// Note: computeWalls' own glue (locindex.Index.Each iteration, locindex.Layer filtering, and
// resolving each loc's name via *cachecfg.Config for isDoorName) is not covered by a
// same-package unit test here — cachecfg.Config has no lightweight synthetic constructor (only
// real archive parsing; see internal/cachecfg/cachecfg_test.go), and this file intentionally
// doesn't loosen computeWalls' signature to a bare locindex.Types interface just for
// testability, since *cachecfg.Config passed through an interface-typed parameter reintroduces
// a real nil-pointer-in-non-nil-interface hazard at every real call site (server.go's
// s.cfg.LocTypes is a concrete *cachecfg.Config, and mapDataCache's own nil guards rely on
// comparing that concrete pointer directly). wallSegmentsFor/poiCategory/isDoorName above
// cover the actual hand-derived, error-prone logic (the CollisionEngine.ts geometry and the
// door/altar classification rules); computeWalls' own remaining glue was instead verified live
// against this project's real 936k-loc "-all" bake in a headless browser session (see
// docs/map_panel_plan_2026-09-16.md's own follow-up section for the walkthrough).
