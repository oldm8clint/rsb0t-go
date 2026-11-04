package retaliateplan

import (
	"sort"

	"rsb0t-go/internal/scripts/tile"
)

// Rect is an inclusive tile rectangle on one level.
type Rect struct {
	MinX, MinZ, MaxX, MaxZ int
}

// Contains reports whether t is inside r (level 0 only: the site is on the ground floor).
func (r Rect) Contains(t tile.Tile) bool {
	return t.Level == 0 && t.X >= r.MinX && t.X <= r.MaxX && t.Z >= r.MinZ && t.Z <= r.MaxZ
}

// Site is one place the method can run: oaks, two zero-damage hitters, and the multi-combat zone that joins them.
type Site struct {
	Name string
	// Tree and Op are the loc's display name and its chop option, Logs the item it gives (the only log a clear drops).
	Tree, Op, Logs string
	// Anchor is the middle of the trees; TreeRadius is how far from it a tree may be and still count.
	Anchor     tile.Tile
	TreeRadius int
	// Multi is the multi-combat zones (engine maps/multiway.csv, 8x8 zones) the fight tile must be inside: a second monster
	// is refused in single combat (npc_combat.rs2 npc_check_notcombat, which looks at the MONSTER's own tile).
	Multi Rect
	// RatName is the hitter's NPC name, Rats its two spawn tiles, RatLeash how far from its spawn (Chebyshev) the stand tile
	// may be for the rat to keep fighting: the engine drops a fight at maxrange + 1 = 18 (wanderrange 15 + 2, research doc
	// section 2.5), so 17 or less.
	RatName  string
	Rats     [2]tile.Tile
	RatLeash int
	// Staging is where the rats are woken from: both inside spell range, each far enough that it has not hit by the time the
	// second is cast.
	Staging tile.Tile
	// Bank is the nearest bank (Falador east), used only when the settings say to bank.
	Bank tile.Tile
	// Preferred are the stand tiles tried first, in order, when they pass every check.
	Preferred []tile.Tile
}

// Falador is docs/two_tick_retaliate_research_2026-11-04.md section 3: the oaks south of the Falador gate, in the
// multi-combat zones x 2992-3007, z 3304-3327, with the two farm rats at (3015,3295) and (3019,3296).
var Falador = Site{
	Name: SiteFalador, Tree: "Oak", Op: "Chop down", Logs: "Oak logs",
	Anchor: tile.Tile{X: 3003, Z: 3311}, TreeRadius: 9,
	Multi:   Rect{MinX: 2992, MinZ: 3304, MaxX: 3007, MaxZ: 3327},
	RatName: "Rat", Rats: [2]tile.Tile{{X: 3015, Z: 3295}, {X: 3019, Z: 3296}}, RatLeash: 17,
	Staging:   tile.Tile{X: 3009, Z: 3303},
	Bank:      tile.Tile{X: 3012, Z: 3357},
	Preferred: []tile.Tile{{X: 3003, Z: 3312}, {X: 3004, Z: 3312}, {X: 3002, Z: 3312}},
}

// SiteByName returns the site a Settings.Site names.
func SiteByName(name string) (Site, bool) {
	if name == SiteFalador {
		return Falador, true
	}
	return Site{}, false
}

// InMultiway reports whether t is in the site's multi-combat zones.
func (s Site) InMultiway(t tile.Tile) bool { return s.Multi.Contains(t) }

// RatReaches reports whether a rat spawned at spawn can still follow a fight to stand (Chebyshev, within RatLeash).
func (s Site) RatReaches(spawn, stand tile.Tile) bool {
	return tile.Chebyshev(spawn, stand) <= s.RatLeash
}

// StandCand is one candidate stand tile and what the runner found out about it from the collision pack and the loc index.
type StandCand struct {
	Tile tile.Tile
	// Trees is how many of the site's oaks the tile can chop (locreach and the pack's CanInteractFrom).
	Trees int
	// Walkable is the collision pack's verdict on standing there.
	Walkable bool
	// Open is how many orthogonal neighbours a rat could stand on: two rats need two.
	Open int
}

// ChooseStand picks the stand tile: it must be walkable, chop at least one tree, be in the multi-combat zone, be inside both
// rats' leash, and have two open neighbours. Of those the most trees win, then the most open neighbours, then the site's
// preference order, then the tile nearest the anchor. ok is false when none passes.
func (s Site) ChooseStand(cands []StandCand) (tile.Tile, bool) {
	rank := func(t tile.Tile) int {
		for i, p := range s.Preferred {
			if p == t {
				return i
			}
		}
		return len(s.Preferred)
	}
	var good []StandCand
	for _, c := range cands {
		if !c.Walkable || c.Trees < 1 || c.Open < 2 || !s.InMultiway(c.Tile) {
			continue
		}
		if !s.RatReaches(s.Rats[0], c.Tile) || !s.RatReaches(s.Rats[1], c.Tile) {
			continue
		}
		good = append(good, c)
	}
	if len(good) == 0 {
		return tile.Tile{}, false
	}
	sort.SliceStable(good, func(i, j int) bool {
		a, b := good[i], good[j]
		if a.Trees != b.Trees {
			return a.Trees > b.Trees
		}
		if a.Open != b.Open {
			return a.Open > b.Open
		}
		if ra, rb := rank(a.Tile), rank(b.Tile); ra != rb {
			return ra < rb
		}
		return tile.Chebyshev(a.Tile, s.Anchor) < tile.Chebyshev(b.Tile, s.Anchor)
	})
	return good[0].Tile, true
}

// NudgeTiles are the tiles a re-phase steps to and back from: walkable tiles inside the multi-combat zone 3 to 6 tiles from
// the stand tile, so the rats have to re-approach, nearest first, rotated by try so each attempt takes a different route
// (rotation keeps the order deterministic and testable). walkable is the collision pack's answer.
func (s Site) NudgeTiles(stand tile.Tile, try int, walkable func(tile.Tile) bool) []tile.Tile {
	var out []tile.Tile
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			t := tile.Tile{X: stand.X + dx, Z: stand.Z + dz}
			d := tile.Chebyshev(stand, t)
			if d < 3 || d > 6 || !s.InMultiway(t) || !walkable(t) {
				continue
			}
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := tile.Chebyshev(stand, out[i]), tile.Chebyshev(stand, out[j])
		if di != dj {
			return di < dj
		}
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Z < out[j].Z
	})
	if len(out) == 0 {
		return nil
	}
	k := (try * 5) % len(out)
	return append(append([]tile.Tile(nil), out[k:]...), out[:k]...)
}
