package webwalk

import (
	"encoding/binary"
	"sync"
)

// ZoneIndex answers InZones for one fixed set of rectangles from the mapsquare the tile is in, so
// a search tests the few rectangles that touch that square instead of every one. Live 3 Oct 13:48:
// InZones was 14% of the fleet's CPU, the planner testing about 57 rectangles on every step.
type ZoneIndex struct {
	minSX, minSZ, w, h int // the mapsquare box the rectangles cover
	cells              [][]Rect
}

// NewZoneIndex indexes zones by mapsquare. An empty or nil set gives an index that contains nothing.
func NewZoneIndex(zones []Rect) *ZoneIndex {
	zi := &ZoneIndex{}
	first := true
	var maxSX, maxSZ int
	for _, r := range zones {
		if r.MinX > r.MaxX || r.MinZ > r.MaxZ || r.MaxX < 0 || r.MaxZ < 0 {
			continue
		}
		lx, lz, hx, hz := max(r.MinX, 0)>>6, max(r.MinZ, 0)>>6, r.MaxX>>6, r.MaxZ>>6
		if first {
			zi.minSX, zi.minSZ, maxSX, maxSZ = lx, lz, hx, hz
			first = false
			continue
		}
		zi.minSX, zi.minSZ = min(zi.minSX, lx), min(zi.minSZ, lz)
		maxSX, maxSZ = max(maxSX, hx), max(maxSZ, hz)
	}
	if first {
		return zi
	}
	zi.w, zi.h = maxSX-zi.minSX+1, maxSZ-zi.minSZ+1
	zi.cells = make([][]Rect, zi.w*zi.h)
	for _, r := range zones {
		if r.MinX > r.MaxX || r.MinZ > r.MaxZ || r.MaxX < 0 || r.MaxZ < 0 {
			continue
		}
		for sz := max(r.MinZ, 0) >> 6; sz <= r.MaxZ>>6; sz++ {
			for sx := max(r.MinX, 0) >> 6; sx <= r.MaxX>>6; sx++ {
				i := (sz-zi.minSZ)*zi.w + sx - zi.minSX
				zi.cells[i] = append(zi.cells[i], r)
			}
		}
	}
	return zi
}

// Contains is InZones(p, zones) for the indexed zones.
func (zi *ZoneIndex) Contains(p Point) bool {
	if zi == nil || zi.cells == nil || p.X < 0 || p.Z < 0 {
		return false
	}
	sx, sz := p.X>>6-zi.minSX, p.Z>>6-zi.minSZ
	if sx < 0 || sz < 0 || sx >= zi.w || sz >= zi.h {
		return false
	}
	for _, r := range zi.cells[sz*zi.w+sx] {
		if r.Contains(p) {
			return true
		}
	}
	return false
}

// zoneIndexCacheMax bounds how many distinct zone sets zoneIndexFor keeps; the fleet resolves only a
// handful (the automatic zones by combat level and endpoint), so the cap only guards odd callers.
const zoneIndexCacheMax = 256

var (
	zoneIndexMu    sync.Mutex
	zoneIndexCache = map[string]*ZoneIndex{}
)

// zoneIndexFor is the shared, immutable index for zones, built once per distinct set.
func zoneIndexFor(zones []Rect) *ZoneIndex {
	key := make([]byte, 0, len(zones)*12)
	for _, r := range zones {
		key = binary.AppendVarint(key, int64(r.MinX))
		key = binary.AppendVarint(key, int64(r.MaxX))
		key = binary.AppendVarint(key, int64(r.MinZ))
		key = binary.AppendVarint(key, int64(r.MaxZ))
		key = binary.AppendVarint(key, int64(r.Level))
	}
	zoneIndexMu.Lock()
	zi, ok := zoneIndexCache[string(key)]
	zoneIndexMu.Unlock()
	if ok {
		return zi
	}
	zi = NewZoneIndex(zones)
	zoneIndexMu.Lock()
	if len(zoneIndexCache) < zoneIndexCacheMax {
		zoneIndexCache[string(key)] = zi
	}
	zoneIndexMu.Unlock()
	return zi
}
