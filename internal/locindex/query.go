package locindex

import (
	"sort"
	"strings"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// Types resolves loc type ids; *cachecfg.Config satisfies it.
type Types interface {
	Loc(id int) (cachecfg.Loc, bool)
}

// Change is one live difference from the map files, as the server reported it with
// LOC_ADD_CHANGE (a loc added or replaced) or LOC_DEL (the layer emptied). Either one
// overrides whatever static loc occupies that tile's layer, which is how the client's
// locChangeCreate treats them.
type Change struct {
	X, Z, Level int
	ID          int
	Shape       int
	Angle       int
	Deleted     bool
}

// ChangesFrom converts gamestate's live loc records, tombstones included, into Changes.
func ChangesFrom(locs []gamestate.LocInstance) []Change {
	out := make([]Change, 0, len(locs))
	for _, l := range locs {
		out = append(out, Change{X: l.X, Z: l.Z, Level: l.Level, ID: l.Type, Shape: l.Shape, Angle: l.Rotate, Deleted: l.Deleted})
	}
	return out
}

// Query selects locs near a tile.
type Query struct {
	X, Z, Level int
	// Radius is the largest Chebyshev distance from the tile to any tile of the loc's
	// footprint.
	Radius int
	// Name matches the type name, ignoring case. When empty, IDs is used instead.
	Name string
	IDs  []int
	// Op, when set, requires the type to have this option; Match.Op is its number.
	Op string
}

// Match is one loc a query found.
type Match struct {
	Loc
	Name string
	// Op is the 1-based option number for OPLOCn, 0 when the query named no option.
	Op int
	// Distance is the Chebyshev distance from the query tile to the loc's footprint.
	Distance int
	// Live is true when the loc came from a live change rather than the map files.
	Live bool

	sq int
}

// largestLoc bounds how far a loc's origin can sit south-west of a tile its footprint
// covers; the scan rectangle is widened by it so large locs are not missed.
const largestLoc = 8

type tileLayer struct{ x, z, level, layer int }

// Find returns every matching loc within the radius, nearest first. Ties break on
// straight-line distance to the footprint, then on id and tile, so results are stable.
func (ix *Index) Find(types Types, q Query, live []Change) []Match {
	if q.Radius < 0 {
		return nil
	}
	ids := map[int]bool{}
	for _, id := range q.IDs {
		ids[id] = true
	}
	minX, minZ, maxX, maxZ := q.X-q.Radius-largestLoc, q.Z-q.Radius-largestLoc, q.X+q.Radius, q.Z+q.Radius

	changed := map[tileLayer]bool{}
	for _, c := range live {
		if c.Level == q.Level && c.X >= minX && c.X <= maxX && c.Z >= minZ && c.Z <= maxZ {
			changed[tileLayer{c.X, c.Z, c.Level, Layer(c.Shape)}] = true
		}
	}

	var out []Match
	consider := func(l Loc, isLive bool) {
		t, ok := types.Loc(l.ID)
		if !ok {
			return
		}
		if q.Name != "" {
			if !strings.EqualFold(t.Name, q.Name) {
				return
			}
		} else if !ids[l.ID] {
			return
		}
		op := 0
		if q.Op != "" {
			if op = cachecfg.OpIndex(t.Ops, q.Op); op == 0 {
				return
			}
		}
		d, sq := footprintDistance(q.X, q.Z, l, t)
		if d > q.Radius {
			return
		}
		out = append(out, Match{Loc: l, Name: t.Name, Op: op, Distance: d, Live: isLive, sq: sq})
	}

	ix.Each(minX, minZ, maxX, maxZ, q.Level, func(l Loc) {
		if !changed[tileLayer{l.X, l.Z, l.Level, Layer(l.Shape)}] {
			consider(l, false)
		}
	})
	for _, c := range live {
		if !c.Deleted && c.Level == q.Level {
			consider(Loc{ID: c.ID, X: c.X, Z: c.Z, Level: c.Level, Shape: c.Shape, Angle: c.Angle}, true)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		if a.sq != b.sq {
			return a.sq < b.sq
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Z < b.Z
	})
	return out
}

// Nearest returns the closest matching loc.
func (ix *Index) Nearest(types Types, q Query, live []Change) (Match, bool) {
	all := ix.Find(types, q, live)
	if len(all) == 0 {
		return Match{}, false
	}
	return all[0], true
}

// footprintDistance measures from a tile to the rectangle a loc covers. Width runs along
// x and length along z, swapped when the loc is rotated a quarter turn (angle 1 or 3), as
// the engine places it.
func footprintDistance(x, z int, l Loc, t cachecfg.Loc) (cheb, sq int) {
	w, h := t.Width, t.Length
	if l.Angle == 1 || l.Angle == 3 {
		w, h = h, w
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dx := axisGap(x, l.X, l.X+w-1)
	dz := axisGap(z, l.Z, l.Z+h-1)
	cheb = dx
	if dz > cheb {
		cheb = dz
	}
	return cheb, dx*dx + dz*dz
}

func axisGap(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo - v
	case v > hi:
		return v - hi
	}
	return 0
}
