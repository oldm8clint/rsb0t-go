package freshstartrunner

import (
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// Plan through nearby operable closed doors, then open only the first blocked
// crossing on that route. This avoids choosing the same open interior door while
// the actual exit lies through another room. Planning never sends its route.
func pickPlannedDoor(d *Deps, from, dest tile.Tile) (locindex.Match, string, bool, bool) {
	if d.Pack == nil || d.Locs == nil || d.Config == nil || from.Level != dest.Level {
		return locindex.Match{}, "", false, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	var candidates []locindex.Match
	seen := make(map[[3]int]bool)
	for _, name := range doorNames {
		for _, op := range doorOps {
			for _, m := range d.Locs.Find(d.Config, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: 48, Name: name, Op: op}, live) {
				if m.Shape != 0 && m.Shape != 2 || guardianOwned(m) {
					continue
				}
				info, ok := d.Config.Loc(m.ID)
				if !ok || !info.BlockWalk || cachecfg.OpIndex(info.Ops, op) == 0 {
					continue
				}
				key := [3]int{m.X, m.Z, m.Shape}
				if seen[key] {
					continue
				}
				seen[key] = true
				candidates = append(candidates, m)
			}
		}
	}
	if len(candidates) == 0 || len(candidates) > 128 {
		return locindex.Match{}, "", false, false
	}
	real := liveNavigationPack(d)
	var edits []pathfind.WallEdit
	for _, m := range candidates {
		edits = append(edits, pathfind.WallEdit{X: m.X, Z: m.Z, Level: m.Level, Shape: m.Shape, Angle: m.Angle})
	}
	route := real.WithWalls(edits).FindPathAvoiding(pathfind.NavPoint{X: from.X, Z: from.Z, Level: from.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level}, inDangerZone)
	if !route.OK {
		return locindex.Match{}, "", false, false
	}
	dirs := [][2]int{{0, 1}, {1, 0}, {0, -1}, {-1, 0}, {1, 1}, {1, -1}, {-1, -1}, {-1, 1}}
	for i := 1; i < len(route.Waypoints); i++ {
		cur, end := route.Waypoints[i-1], route.Waypoints[i]
		sx, sz := sign(end.X-cur.X), sign(end.Z-cur.Z)
		dir := -1
		for n, v := range dirs {
			if v[0] == sx && v[1] == sz {
				dir = n
				break
			}
		}
		if dir < 0 {
			continue
		}
		for cur.X != end.X || cur.Z != end.Z {
			nx, nz := cur.X+sx, cur.Z+sz
			if real.ExitMask(cur.X, cur.Z, cur.Level)&(1<<uint(dir)) == 0 {
				for _, m := range candidates {
					sides := []int{(m.Angle + 3) & 3}
					if m.Shape == 2 {
						sides = append(sides, m.Angle&3)
					}
					for _, side := range sides {
						wx, wz := m.X+dirs[side][0], m.Z+dirs[side][1]
						if cur.X == m.X && cur.Z == m.Z && nx == wx && nz == wz || nx == m.X && nz == m.Z && cur.X == wx && cur.Z == wz {
							return m, m.Name, false, true
						}
					}
				}
				return locindex.Match{}, "", false, false
			}
			cur.X, cur.Z = nx, nz
		}
	}
	return locindex.Match{}, "", false, false
}
func sign(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}
