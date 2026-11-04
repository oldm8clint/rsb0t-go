// Package tile is the world coordinate the pure script planners share, so freshstart, donorplan
// and crafterplan agree on one type. Integration code converts gamestate positions into it.
package tile

// Tile is an absolute world tile: x and z on the map, level 0 for the ground floor.
type Tile struct {
	X     int `json:"x"`
	Z     int `json:"z"`
	Level int `json:"level"`
}

// Chebyshev is the king-move distance between two tiles, ignoring their levels, as the TS
// scripts' chebyshev helper measures it.
func Chebyshev(a, b Tile) int {
	dx, dz := abs(a.X-b.X), abs(a.Z-b.Z)
	if dx > dz {
		return dx
	}
	return dz
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
