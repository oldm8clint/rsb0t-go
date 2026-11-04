package doorescape

import "sync"

// Rogues' Castle (mapsquare 51,61, Wilderness level 53), from maps/m51_61.jm2 and content's door and stair
// scripts. castle_collision_reference_vector.ts lists the doors and stairs the map places inside the castle and
// the golden fixture holds them; TestCatalogueIsTheMapsPlacements compares this table to that list, and
// TestCastleAgainstTheLocIndex to the fleet's own loc index and the loc configs.
//
// Ten doors on the ground floor and two on the first (the second and third floors have none), in the order of
// the golden list (level, then x, then z): the order is the bit order of a State.
var castleDoors = []Door{
	{Name: "west room south door", Level: 0, ID: 1530, X: 3276, Z: 3929, Shape: 0, Angle: 3, OpenID: 1531, Op: 1},
	{Name: "west room north door", Level: 0, ID: 1530, X: 3276, Z: 3933, Shape: 0, Angle: 1, OpenID: 1531, Op: 1},
	{Name: "main door (diagonal)", Level: 0, ID: 1530, X: 3279, Z: 3939, Shape: 9, Angle: 2, OpenID: 1531, Op: 1},
	{Name: "stairs room east door (diagonal)", Level: 0, ID: 1530, X: 3282, Z: 3934, Shape: 9, Angle: 0, OpenID: 1531, Op: 1},
	{Name: "range room east door", Level: 0, ID: 1530, X: 3283, Z: 3931, Shape: 0, Angle: 2, OpenID: 1531, Op: 1},
	{Name: "north cell door (iron)", Level: 0, ID: 1539, X: 3285, Z: 3938, Shape: 0, Angle: 3, OpenID: 1540, Op: 1},
	{Name: "hall north door (iron)", Level: 0, ID: 1539, X: 3286, Z: 3935, Shape: 0, Angle: 1, OpenID: 1540, Op: 1},
	{Name: "hall east door", Level: 0, ID: 1530, X: 3288, Z: 3930, Shape: 0, Angle: 2, OpenID: 1531, Op: 1},
	{Name: "east room east door", Level: 0, ID: 1530, X: 3292, Z: 3931, Shape: 0, Angle: 2, OpenID: 1531, Op: 1},
	{Name: "east chamber south door", Level: 0, ID: 1530, X: 3294, Z: 3932, Shape: 0, Angle: 3, OpenID: 1531, Op: 1},
	{Name: "first floor west room door", Level: 1, ID: 1530, X: 3290, Z: 3931, Shape: 0, Angle: 2, OpenID: 1531, Op: 1},
	{Name: "first floor north-east room door", Level: 1, ID: 1530, X: 3293, Z: 3934, Shape: 0, Angle: 0, OpenID: 1531, Op: 1},
}

// The staircase column at 3280,3936. content's stairs.rs2 sets every climb down at 3282,3936 one level
// lower (p_telejump), the climb up from the ground floor at 3282,3936 one up, and the climb up from the second
// floor at 3281,3936 on the roof. 1738 and 1739 are 2x2, 1740 the 1x1 top which only its east side may use
// (forceapproach 13: north, south and west refused).
var castleStairs = []Stair{
	{Name: "ground floor stairs up", Placement: Placement{ID: 1738, X: 3280, Z: 3936, Level: 0, Shape: ShapeCentrepiece}, Type: LocType{Width: 2, Length: 2}, Op: 1, Label: "Climb-up", Up: true, Land: Tile{3282, 3936, 1}},
	{Name: "first floor stairs up", Placement: Placement{ID: 1739, X: 3280, Z: 3936, Level: 1, Shape: ShapeCentrepiece}, Type: LocType{Width: 2, Length: 2}, Op: 2, Label: "Climb-up", Up: true, Land: Tile{3282, 3936, 2}},
	{Name: "first floor stairs down", Placement: Placement{ID: 1739, X: 3280, Z: 3936, Level: 1, Shape: ShapeCentrepiece}, Type: LocType{Width: 2, Length: 2}, Op: 3, Label: "Climb-down", Land: Tile{3282, 3936, 0}},
	{Name: "second floor stairs up", Placement: Placement{ID: 1739, X: 3280, Z: 3936, Level: 2, Shape: ShapeCentrepiece}, Type: LocType{Width: 2, Length: 2}, Op: 2, Label: "Climb-up", Up: true, Land: Tile{3281, 3936, 3}},
	{Name: "second floor stairs down", Placement: Placement{ID: 1739, X: 3280, Z: 3936, Level: 2, Shape: ShapeCentrepiece}, Type: LocType{Width: 2, Length: 2}, Op: 3, Label: "Climb-down", Land: Tile{3282, 3936, 1}},
	{Name: "roof stairs down", Placement: Placement{ID: 1740, X: 3280, Z: 3936, Level: 3, Shape: ShapeCentrepiece}, Type: LocType{Width: 1, Length: 1, ForceApproach: 13}, Op: 1, Label: "Climb-down", Land: Tile{3282, 3936, 2}},
}

// CastleCompound is, per level, the rectangle a ladder acts in. On the ground it is the castle's walls and the
// courtyards inside them (3275-3298 x 3925-3939: the main door's doorway is 3279,3939 and the first tile of the
// open ground beyond it, 3279,3940, is out); above it the floors go a little further north (the north-east
// tower's rooms reach z 3941). An account that has walked out of it is out.
var CastleCompound = [4]Rect{
	{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3939},
	{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3941},
	{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3941},
	{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3941},
}

var (
	castleOnce sync.Once
	castleCat  *Catalog
	castleErr  error
)

// Castle is the Rogues' Castle catalogue over the embedded collision grid.
func Castle() (*Catalog, error) {
	castleOnce.Do(func() {
		g, err := CastleGrid()
		if err != nil {
			castleErr = err
			return
		}
		castleCat, castleErr = NewCatalog(g, castleDoors, castleStairs, CastleCompound)
	})
	return castleCat, castleErr
}
