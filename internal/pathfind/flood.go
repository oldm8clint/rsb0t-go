package pathfind

// Flood is every tile a walk from `from` reaches without straying more than radius tiles
// (Chebyshev) from it, by the same exit masks FindPath steps by: the connected ground around a
// tile in one pass, where asking FindPath about each tile in turn expands the whole region again
// for every tile it cannot reach. from is included when it is walkable. Keys are {x, z} on from's
// level.
func (p *Pack) Flood(from NavPoint, radius int) map[[2]int]bool {
	out := map[[2]int]bool{}
	if radius < 0 || !p.Walkable(from.X, from.Z, from.Level) {
		return out
	}
	start := [2]int{from.X, from.Z}
	out[start] = true
	queue := [][2]int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		mask := p.ExitMask(cur[0], cur[1], from.Level)
		for dir := 0; dir < 8; dir++ {
			if mask&(1<<uint(dir)) == 0 {
				continue
			}
			next := [2]int{cur[0] + dx[dir], cur[1] + dz[dir]}
			if out[next] || chebyshev(next[0], next[1], from.X, from.Z) > radius {
				continue
			}
			out[next] = true
			queue = append(queue, next)
		}
	}
	return out
}
