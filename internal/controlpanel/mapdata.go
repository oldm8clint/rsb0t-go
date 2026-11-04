package controlpanel

import (
	"strings"
	"sync"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
)

// This file computes the map panel's two other real-data overlays (2026-09-16 follow-up to
// the tile pyramid in docs/map_panel_plan_2026-09-16.md Part 5): POI markers (banks, altars,
// ...) and wall/door line segments, both from this project's own already-proven static loc
// index (internal/locindex) and config archive (internal/cachecfg) — the same pair every
// real bot script already queries for pathfinding, not a new or separate data source. Both
// are static (baked once from the map files, never change at runtime) so each is computed
// once, lazily, and cached for the server's lifetime — see mapDataCache below.

// poiEntry is one POI marker (see poiCategory) in real world coordinates.
type poiEntry struct {
	X        int    `json:"x"`
	Z        int    `json:"z"`
	Level    int    `json:"level"`
	Category string `json:"category"`
	Name     string `json:"name"`
}

// poiCategory classifies one loc type for /api/poi, or ("","") to skip it entirely. Matched
// the same way real bot scripts already query these fixtures where a query already exists in
// this codebase (see e.g. internal/scripts/mulerunner's own locindex.Query{Name: "Bank
// booth", Op: "Use-quickly"}) — by exact Name for a fixture with one well-known display name,
// by Op for altars, since this era's content reuses the plain name "Altar" across many
// distinct ids (Chaos altar, Altar of Guthix, Altar of nature, and every runecrafting altar
// use "Craft-rune" instead) — confirmed live 2026-09-16 by dumping every loc type whose name
// contains "altar" from this project's own real config archive (prod-config.jag): Op is the
// reliable signal here, Name is not.
func poiCategory(name string, ops [5]string) (category, label string) {
	switch name {
	case "Bank booth", "Bank chest":
		return "bank", "Bank"
	case "Anvil":
		return "anvil", "Anvil"
	case "Furnace":
		return "furnace", "Furnace"
	case "Cooking range":
		return "range", "Cooking range"
	}
	if cachecfg.OpIndex(ops, "Pray-at") != 0 {
		return "altar", name
	}
	if cachecfg.OpIndex(ops, "Craft-rune") != 0 {
		return "rc_altar", name
	}
	return "", ""
}

// wallSegment is one wall or door/gate edge, in real world tile-corner coordinates (the
// segment from (X0,Z0) to (X1,Z1) — always axis-aligned and exactly one tile long, matching
// this content revision's own wall placement grid).
type wallSegment struct {
	X0   int  `json:"x0"`
	Z0   int  `json:"z0"`
	X1   int  `json:"x1"`
	Z1   int  `json:"z1"`
	Door bool `json:"door"`
}

// wallDoorNames mirrors internal/scripts/freshstartrunner/runner.go's own doorNames exactly
// (per the account owner's explicit instruction to reuse that proven list rather than
// inventing a new door/gate detection rule) — freshstartrunner's own doc comment there
// explains why the list has several names: this era's content is not consistent about a
// door/gate's exact display name.
var wallDoorNames = []string{"Door", "Gate", "Large door", "Iron gate", "Wooden door", "Metal door"}

func isDoorName(name string) bool {
	for _, d := range wallDoorNames {
		if strings.EqualFold(d, name) {
			return true
		}
	}
	return false
}

// worldBoundMax is the largest world tile coordinate /api/poi and /api/walls ever scan —
// the top of the full addressable mapsquare grid (mx/mz 0-255, locindex's own on-disk limit;
// see locindex.go's encode/decode), not this content revision's own real extent. Scanning the
// full range costs nothing real: locindex.Index.Each looks up one map entry per mapsquare, so
// the (mostly absent) squares outside real content are cheap map misses, not real work.
const worldBoundMax = 0xff<<6 + 63

// mapDataCache holds the lazily-computed, then cached, POI and wall results — each real loc
// data set is static for the server's lifetime (baked once offline, never changes while
// running), so recomputing on every request would be pure waste for data nothing invalidates.
type mapDataCache struct {
	poiOnce  sync.Once
	poi      []poiEntry
	wallOnce sync.Once
	walls    []wallSegment
}

func (m *mapDataCache) pois(ix *locindex.Index, types *cachecfg.Config) []poiEntry {
	m.poiOnce.Do(func() {
		if ix == nil || types == nil {
			return
		}
		var out []poiEntry
		for level := 0; level < 4; level++ {
			ix.Each(0, 0, worldBoundMax, worldBoundMax, level, func(l locindex.Loc) {
				t, ok := types.Loc(l.ID)
				if !ok {
					return
				}
				category, label := poiCategory(t.Name, t.Ops)
				if category == "" {
					return
				}
				out = append(out, poiEntry{X: l.X, Z: l.Z, Level: level, Category: category, Name: label})
			})
		}
		m.poi = out
	})
	return m.poi
}

func (m *mapDataCache) wallSegments(ix *locindex.Index, types *cachecfg.Config) []wallSegment {
	m.wallOnce.Do(func() {
		if ix == nil || types == nil {
			return
		}
		m.walls = computeWalls(ix, types)
	})
	return m.walls
}

// computeWalls decodes every WALL-layer loc (locindex.Layer(shape) == 0) on level 0 into real
// edge segments, using the exact per-shape/angle geometry the engine's own collision code
// applies (Engine-TS checkout's src/engine/routefinder/CollisionEngine.ts,
// changeWallStraight/changeWallCorner/changeWallL — read directly, not guessed) so a wall
// line lands on the true tile edge a player would actually be blocked by, not an
// approximation.
//
// Two disclosed simplifications, both honest trade-offs for this session rather than gaps
// nobody noticed:
//  1. Level 0 only. The floor-color raster this overlay sits on top of (ExportTiles.ts) is
//     itself a single flattened "ground plan" per mapsquare (packWorldmap's own bridge-merge
//     logic — see docs/map_panel_plan_2026-09-16.md Part 5) with no per-level distinction
//     preserved, so there is no way to know from this Go-side data alone which real level a
//     given map pixel is actually showing at that spot; restricting to level 0 is the same
//     "ground floor, not upper floors" honesty the terrain raster already has, not a new gap
//     this overlay introduces on its own.
//  2. WALL_DIAGONAL_CORNER/WALL_SQUARE_CORNER (shapes 1 and 3 — single-tile corner posts, not
//     a wall run) are not rendered as a line; only WALL_STRAIGHT (0) and WALL_L (2) are.
//     These two shapes cover the overwhelming majority of real building outlines.
func computeWalls(ix *locindex.Index, types *cachecfg.Config) []wallSegment {
	var out []wallSegment
	ix.Each(0, 0, worldBoundMax, worldBoundMax, 0, func(l locindex.Loc) {
		if locindex.Layer(l.Shape) != 0 {
			return
		}
		t, ok := types.Loc(l.ID)
		if !ok {
			return
		}
		out = append(out, wallSegmentsFor(l.Shape, l.Angle, l.X, l.Z, isDoorName(t.Name))...)
	})
	return out
}

// wallSegmentsFor is the pure shape/angle -> edge-segment geometry, split out from
// computeWalls so it's directly unit-testable without a real cachecfg.Config (mapdata_test.go
// checks every shape/angle combination against CollisionEngine.ts's own real coordinates by
// hand). LocAngle: WEST=0, NORTH=1, EAST=2, SOUTH=3 (Engine-TS flags.ts) — angle is NOT a
// compass-order rotation index, it's this specific mapping; ported directly from
// CollisionEngine.ts's own changeWallStraight/changeWallL.
func wallSegmentsFor(shape, angle, x, z int, door bool) []wallSegment {
	var out []wallSegment
	seg := func(x0, z0, x1, z1 int) { out = append(out, wallSegment{X0: x0, Z0: z0, X1: x1, Z1: z1, Door: door}) }
	switch shape {
	case 0: // WALL_STRAIGHT
		switch angle {
		case 0: // WEST edge
			seg(x, z, x, z+1)
		case 1: // NORTH edge
			seg(x, z+1, x+1, z+1)
		case 2: // EAST edge
			seg(x+1, z, x+1, z+1)
		case 3: // SOUTH edge
			seg(x, z, x+1, z)
		}
	case 2: // WALL_L
		switch angle {
		case 0: // WEST: north + west edges
			seg(x, z+1, x+1, z+1)
			seg(x, z, x, z+1)
		case 1: // NORTH: north + east edges
			seg(x, z+1, x+1, z+1)
			seg(x+1, z, x+1, z+1)
		case 2: // EAST: south + east edges
			seg(x, z, x+1, z)
			seg(x+1, z, x+1, z+1)
		case 3: // SOUTH: south + west edges
			seg(x, z, x+1, z)
			seg(x, z, x, z+1)
		}
	}
	return out
}
