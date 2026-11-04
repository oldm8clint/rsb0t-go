package webwalk

import "testing"

// The box must never hide a wall StepWalled would find: every step from every tile around a
// few walls answers the same with and without the box.
func TestWallBoxNeverHidesAWall(t *testing.T) {
	walls := map[Point]byte{
		{X: 10, Z: 10}: WallNorth,
		{X: 14, Z: 12}: WallEast | WallSouth,
		{X: 12, Z: 16}: WallWest,
	}
	box := wallBoxOf(walls)
	for x := 0; x < 30; x++ {
		for z := 0; z < 30; z++ {
			p := Point{X: x, Z: z}
			for d := 0; d < 8; d++ {
				want := StepWalled(walls, p, dirX[d], dirZ[d])
				got := box.near(p) && StepWalled(walls, p, dirX[d], dirZ[d])
				if got != want {
					t.Fatalf("step %d,%d from %v: boxed %v, plain %v", dirX[d], dirZ[d], p, got, want)
				}
			}
		}
	}
	if wallBoxOf(nil).near(Point{}) {
		t.Fatal("an empty box is near nothing")
	}
}
