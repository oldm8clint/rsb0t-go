package pathfind

// WallEdit is an observed cardinal wall addition/removal. Angle matches loc rotation:
// 0 west, 1 north, 2 east, 3 south. Only straight/corner walls are supported.
type WallEdit struct {
	X, Z, Level, Shape, Angle int
	Add                       bool
}

// WithWalls returns an ephemeral per-search view; the fleet's shared pack is immutable.
// Preserve non-wall restrictions and disable unsafe diagonals around rotated doors.
func (p *Pack) WithWalls(edits []WallEdit) *Pack {
	if len(edits) == 0 || p.Version < 2 {
		return p
	}
	q := *p
	// Live walls can open a way the static pack has none, so a flood remembered on p does not
	// hold here (unreach.go).
	q.unreach = nil
	q.wallOverrides = make(map[int64]byte)
	q.exitOverrides = make(map[int64]byte)
	for id, v := range p.wallOverrides {
		q.wallOverrides[id] = v
	}
	for id, v := range p.exitOverrides {
		q.exitOverrides[id] = v
	}
	for _, e := range edits {
		if e.Shape != 0 && e.Shape != 2 {
			continue
		}
		sides := []int{(e.Angle + 3) & 3}
		if e.Shape == 2 {
			sides = append(sides, e.Angle&3)
		}
		for _, side := range sides {
			x, z := e.X+dx[side], e.Z+dz[side]
			a, b := nodeID(e.X, e.Z, e.Level), nodeID(x, z, e.Level)
			am, bm := q.WallMask(e.X, e.Z, e.Level), q.WallMask(x, z, e.Level)
			if e.Add {
				am |= 1 << uint(side)
				bm |= 1 << uint((side+2)&3)
			} else {
				am &^= 1 << uint(side)
				bm &^= 1 << uint((side+2)&3)
			}
			q.wallOverrides[a] = am
			q.wallOverrides[b] = bm
		}
	}
	affected := make(map[int64]bool)
	for id := range q.wallOverrides {
		for ox := -1; ox <= 1; ox++ {
			for oz := -1; oz <= 1; oz++ {
				affected[nodeID(nodeX(id)+ox, nodeZ(id)+oz, nodeLevel(id))] = true
			}
		}
	}
	// Rebuild cardinal exits only when an observed wall change explains their old block.
	for id := range affected {
		x, z, l := nodeX(id), nodeZ(id), nodeLevel(id)
		mask := p.ExitMask(x, z, l)
		for side := 0; side < 4; side++ {
			nx, nz := x+dx[side], z+dz[side]
			back := (side + 2) & 3
			oldWall := p.WallMask(x, z, l)&(1<<uint(side)) != 0 || p.WallMask(nx, nz, l)&(1<<uint(back)) != 0
			newWall := q.WallMask(x, z, l)&(1<<uint(side)) != 0 || q.WallMask(nx, nz, l)&(1<<uint(back)) != 0
			if newWall {
				mask &^= 1 << uint(side)
			} else if oldWall && p.Walkable(x, z, l) && p.Walkable(nx, nz, l) {
				mask |= 1 << uint(side)
			}
		}
		q.exitOverrides[id] = mask
	}
	for id := range affected {
		x, z, l := nodeX(id), nodeZ(id), nodeLevel(id)
		mask := q.exitOverrides[id]
		for dir := 4; dir < 8; dir++ {
			a, b := 0, 1
			if dz[dir] < 0 {
				a = 2
			}
			if dx[dir] < 0 {
				b = 3
			}
			if q.ExitMask(x, z, l)&(1<<uint(a)) == 0 || q.ExitMask(x, z, l)&(1<<uint(b)) == 0 || q.ExitMask(x+dx[a], z+dz[a], l)&(1<<uint(b)) == 0 || q.ExitMask(x+dx[b], z+dz[b], l)&(1<<uint(a)) == 0 {
				mask &^= 1 << uint(dir)
			}
		}
		q.exitOverrides[id] = mask
	}
	return &q
}
