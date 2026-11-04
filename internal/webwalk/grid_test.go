package webwalk

// grid is a synthetic Collision for tests: walkable tiles plus blocked tile-to-tile steps,
// with RS-style diagonals (a diagonal step needs both orthogonal steps free).
type grid struct {
	walk    map[Point]bool
	blocked map[[2]Point]bool
}

func newGrid() *grid { return &grid{walk: map[Point]bool{}, blocked: map[[2]Point]bool{}} }

// fill marks a rectangle walkable.
func (g *grid) fill(x0, z0, x1, z1, level int) *grid {
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			g.walk[Point{X: x, Z: z, Level: level}] = true
		}
	}
	return g
}

// wall blocks the step between two orthogonal neighbours, both ways.
func (g *grid) wall(a, b Point) *grid {
	g.blocked[[2]Point{a, b}] = true
	g.blocked[[2]Point{b, a}] = true
	return g
}

func (g *grid) step(a, b Point) bool {
	return g.walk[a] && g.walk[b] && !g.blocked[[2]Point{a, b}]
}

func (g *grid) Walkable(x, z, level int) bool { return g.walk[Point{X: x, Z: z, Level: level}] }

func (g *grid) ExitMask(x, z, level int) byte {
	a := Point{X: x, Z: z, Level: level}
	if !g.walk[a] {
		return 0
	}
	var m byte
	for d := 0; d < 8; d++ {
		b := Point{X: x + dirX[d], Z: z + dirZ[d], Level: level}
		ok := g.step(a, b)
		if ok && dirX[d] != 0 && dirZ[d] != 0 {
			hx := Point{X: x + dirX[d], Z: z, Level: level}
			hz := Point{X: x, Z: z + dirZ[d], Level: level}
			ok = g.step(a, hx) && g.step(hx, b) && g.step(a, hz) && g.step(hz, b)
		}
		if ok {
			m |= 1 << uint(d)
		}
	}
	return m
}

func (g *grid) WallMask(x, z, level int) byte { return 0 }
