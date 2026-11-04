package doorescape

import "fmt"

// A door in this engine is a loc that CHANGES: content/scripts/doors/scripts/door_procs.rs2's open_door deletes
// the closed leaf (loc_del) and adds the open leaf (loc_add) on a neighbouring tile, turned a quarter, for 500
// ticks, after which the closed leaf is back and the open one gone; close_door does the reverse. So the
// collision of a door has two halves that are present or absent on their own, and that is how the state of a
// door is kept here: whether the closed leaf is there and whether the open leaf is there. The two honest
// states are (closed, no open) and (open, no closed). Both present and both absent are what a close racing a
// respawn leaves behind on the server and are modelled too, because a client's loc changes can read that way.

// door_open's offset from the closed leaf's own tile to the open leaf's, by the closed leaf's angle (flags.ts
// LocAngle: 0 west, 1 north, 2 east, 3 south): door_procs.rs2 [proc,door_open]. A straight wall's leaf swings
// onto the tile on the side the wall faces; a diagonal one moves its block onto the tile a quarter turn away.
func openOffset(angle, shape int) (dx, dz int) {
	if shape == ShapeWallStraight {
		return [4]int{-1, 0, 1, 0}[angle&3], [4]int{0, 1, 0, -1}[angle&3]
	}
	return [4]int{0, 1, 0, -1}[angle&3], [4]int{1, 0, -1, 0}[angle&3]
}

// pushOpen is door_open_move_player_out_of_way: where open_door sets an account down when it stands on the
// tile the DIAGONAL leaf is about to land on (a straight leaf is a wall and never needs it). It is applied to
// the closed leaf's angle, before the leaf is added.
func pushOpen(angle int) (dx, dz int) {
	return [4]int{-1, 0, 1, 0}[angle&3], [4]int{0, 1, 0, -1}[angle&3]
}

// pushClose is door_close_move_player_out_of_way: where close_door sets an account down when it stands on the
// tile the DIAGONAL closed leaf is about to land back on. It is applied to the OPEN leaf's angle, and it moves
// the account diagonally (a different offset from pushOpen's).
func pushClose(openAngle int) (dx, dz int) {
	return [4]int{1, 1, -1, -1}[openAngle&3], [4]int{1, -1, -1, 1}[openAngle&3]
}

// Door is one door of the building, as the map places it closed, with the leaf it opens into.
type Door struct {
	Name  string
	Level int
	// ID is the closed leaf's loc id, X and Z its tile, Shape and Angle its placement.
	ID, X, Z, Shape, Angle int
	// OpenID is the id of the open leaf (loc_param next_loc_stage).
	OpenID int
	// Op is the option number that opens the closed leaf and closes the open one (op1: "Open" / "Close").
	Op int
}

// Closed is the closed leaf's placement.
func (d Door) Closed() Placement {
	return Placement{ID: d.ID, X: d.X, Z: d.Z, Level: d.Level, Angle: d.Angle & 3, Shape: d.Shape}
}

// Open is the open leaf's placement: where open_door puts it.
func (d Door) Open() Placement {
	dx, dz := openOffset(d.Angle, d.Shape)
	return Placement{ID: d.OpenID, X: d.X + dx, Z: d.Z + dz, Level: d.Level, Angle: (d.Angle + 1) & 3, Shape: d.Shape}
}

// Diagonal reports whether the door is a shape-9 loc that blocks its whole tile.
func (d Door) Diagonal() bool { return d.Shape == ShapeWallDiagonal }

// Push is where an account standing on the open leaf's tile is set down when the door is opened, and whether
// it applies (only a diagonal door pushes).
func (d Door) Push() (Tile, bool) {
	if !d.Diagonal() {
		return Tile{}, false
	}
	o := d.Open()
	dx, dz := pushOpen(d.Angle)
	return Tile{X: o.X + dx, Z: o.Z + dz, Level: d.Level}, true
}

// PushClose is where an account standing on the closed leaf's own tile is set down when the open leaf is closed,
// and whether it applies (only a diagonal door pushes). That tile is the doorway: a diagonal door's account that
// stood in it while it was open is sent to the far corner.
func (d Door) PushClose() (Tile, bool) {
	if !d.Diagonal() {
		return Tile{}, false
	}
	dx, dz := pushClose(d.Open().Angle)
	return Tile{X: d.X + dx, Z: d.Z + dz, Level: d.Level}, true
}

// Stair is one staircase op: a loc, the option to run on it, and where the account lands.
type Stair struct {
	Name      string
	Placement Placement
	Type      LocType
	Op        int
	// Label is the option's text in the config, for the log and for the check against it.
	Label string
	// Up is true for a climb up. The planner uses only the climbs down.
	Up   bool
	Land Tile
}

// tileBits is flag bits on one tile.
type tileBits struct {
	X, Z, Level int
	Bits        uint32
}

// leafBits are the movement flags a door leaf adds to the collision map, from CollisionEngine.changeWall for a
// straight wall (the leaf's face on its own tile and the opposite face on the neighbour it faces) and
// changeLoc for a diagonal one (LOC on its tile). All the doors' types block walking.
func leafBits(p Placement) []tileBits {
	if p.Shape == ShapeWallDiagonal {
		return []tileBits{{p.X, p.Z, p.Level, Loc}}
	}
	switch p.Angle & 3 {
	case 0:
		return []tileBits{{p.X, p.Z, p.Level, WallWest}, {p.X - 1, p.Z, p.Level, WallEast}}
	case 1:
		return []tileBits{{p.X, p.Z, p.Level, WallNorth}, {p.X, p.Z + 1, p.Level, WallSouth}}
	case 2:
		return []tileBits{{p.X, p.Z, p.Level, WallEast}, {p.X + 1, p.Z, p.Level, WallWest}}
	default:
		return []tileBits{{p.X, p.Z, p.Level, WallSouth}, {p.X, p.Z - 1, p.Level, WallNorth}}
	}
}

// State is every door's leaves: bit 2i is "door i's closed leaf is present", bit 2i+1 "door i's open leaf is
// present". At most 16 doors.
type State uint32

// MaxDoors is how many doors a State holds.
const MaxDoors = 16

// Closed is door i as the map has it: the closed leaf, no open leaf.
func Closed(i int) State { return 1 << uint(2*i) }

// ClosedLeaf reports whether door i's closed leaf is present.
func (s State) ClosedLeaf(i int) bool { return s&(1<<uint(2*i)) != 0 }

// OpenLeaf reports whether door i's open leaf is present.
func (s State) OpenLeaf(i int) bool { return s&(1<<uint(2*i+1)) != 0 }

// With returns the state with door i's leaves set.
func (s State) With(i int, closedLeaf, openLeaf bool) State {
	s &^= 3 << uint(2*i)
	if closedLeaf {
		s |= 1 << uint(2*i)
	}
	if openLeaf {
		s |= 2 << uint(2*i)
	}
	return s
}

// IsOpen is the honest open state of door i: the open leaf, no closed one.
func (s State) IsOpen(i int) bool { return s.OpenLeaf(i) && !s.ClosedLeaf(i) }

// IsShut is the honest closed state of door i: the closed leaf, no open one.
func (s State) IsShut(i int) bool { return s.ClosedLeaf(i) && !s.OpenLeaf(i) }

// Catalog is a building's doors and stairs over its collision grid, and what follows from them.
type Catalog struct {
	Grid   *Grid
	Doors  []Door
	Stairs []Stair
	// Compound is, per level, the rectangle (inclusive) a ladder acts inside; a tile outside it is out.
	Compound [4]Rect

	// delta is, per grid slot, the doors' bits on it.
	delta map[int][]delta
	// open is the open-ground component of the static map: level-0 tiles that connect, with every door as the
	// map has it, to the grid's edge.
	open []bool
	// DefaultState is every door closed.
	DefaultState State
}

// Rect is an inclusive rectangle of tiles.
type Rect struct{ MinX, MaxX, MinZ, MaxZ int }

// Contains reports whether the tile is inside.
func (r Rect) Contains(x, z int) bool {
	return x >= r.MinX && x <= r.MaxX && z >= r.MinZ && z <= r.MaxZ
}

type delta struct {
	door int
	// closed are the bits the closed leaf holds on this tile (removed when it is absent), open the bits the
	// open leaf adds (set when it is present).
	closed, open uint32
}

// NewCatalog builds the catalogue's derived data.
func NewCatalog(g *Grid, doors []Door, stairs []Stair, compound [4]Rect) (*Catalog, error) {
	if len(doors) > MaxDoors {
		return nil, fmt.Errorf("doorescape: %d doors, at most %d", len(doors), MaxDoors)
	}
	c := &Catalog{Grid: g, Doors: doors, Stairs: stairs, Compound: compound, delta: map[int][]delta{}}
	for i, d := range doors {
		c.DefaultState |= Closed(i)
		add := func(bits []tileBits, closed bool) error {
			for _, b := range bits {
				slot := g.Index(b.X, b.Z, b.Level)
				if slot < 0 {
					return fmt.Errorf("doorescape: door %q has a leaf tile %d,%d,%d outside the grid", d.Name, b.X, b.Z, b.Level)
				}
				ds := c.delta[slot]
				found := false
				for k := range ds {
					if ds[k].door == i {
						found = true
						if closed {
							ds[k].closed |= b.Bits
						} else {
							ds[k].open |= b.Bits
						}
					}
				}
				if !found {
					nd := delta{door: i}
					if closed {
						nd.closed = b.Bits
					} else {
						nd.open = b.Bits
					}
					ds = append(ds, nd)
				}
				c.delta[slot] = ds
			}
			return nil
		}
		if err := add(leafBits(d.Closed()), true); err != nil {
			return nil, err
		}
		if err := add(leafBits(d.Open()), false); err != nil {
			return nil, err
		}
	}
	c.classify()
	return c, nil
}

// FlagsAt is the collision flags with the doors in state s: the map's flags, the bits of every absent closed
// leaf removed, then the bits of every present open leaf added (the engine's own loc_del and loc_add).
func (c *Catalog) FlagsAt(s State) FlagsFunc {
	return func(x, z, level int) uint32 {
		slot := c.Grid.Index(x, z, level)
		if slot < 0 {
			return blockAll
		}
		f := c.Grid.flags[slot]
		ds := c.delta[slot]
		if len(ds) == 0 {
			return f
		}
		for _, d := range ds {
			if !s.ClosedLeaf(d.door) {
				f &^= d.closed
			}
		}
		for _, d := range ds {
			if s.OpenLeaf(d.door) {
				f |= d.open
			}
		}
		return f
	}
}

// classify finds the open ground: the level-0 tiles the grid's edge reaches over the static map.
func (c *Catalog) classify() {
	g := c.Grid
	f := c.FlagsAt(c.DefaultState)
	c.open = make([]bool, g.Len())
	var queue []int
	push := func(x, z int) {
		i := g.Index(x, z, 0)
		if i < 0 || c.open[i] || !Enterable(f, x, z, 0) {
			return
		}
		c.open[i] = true
		queue = append(queue, i)
	}
	for x := g.X0; x < g.X0+g.W; x++ {
		push(x, g.Z0)
		push(x, g.Z0+g.H-1)
	}
	for z := g.Z0; z < g.Z0+g.H; z++ {
		push(g.X0, z)
		push(g.X0+g.W-1, z)
	}
	for len(queue) > 0 {
		t := g.TileAt(queue[0])
		queue = queue[1:]
		for d := 0; d < 8; d++ {
			if !CanStep(f, 0, t.X, t.Z, DX[d], DZ[d]) {
				continue
			}
			push(t.X+DX[d], t.Z+DZ[d])
		}
	}
}

// Inside reports whether the tile is in the compound.
func (c *Catalog) Inside(x, z, level int) bool {
	return level >= 0 && level < len(c.Compound) && level < c.Grid.Levels && c.Compound[level].Contains(x, z)
}

// Open reports whether the tile is open ground: level 0 and joined to the world with every door as the map
// has it. A tile in the compound that is not is a room, a doorway or a pocket.
func (c *Catalog) Open(x, z, level int) bool {
	i := c.Grid.Index(x, z, level)
	return i >= 0 && level == 0 && c.open[i]
}

// Interior reports whether the tile is in the compound and is not open ground: the rooms, the doorways and
// every upper floor. This is where an account is walled in until a door or a staircase lets it out.
func (c *Catalog) Interior(x, z, level int) bool {
	return c.Inside(x, z, level) && !c.Open(x, z, level)
}

// Free reports whether the tile is somewhere an account has got out to: open ground outside the compound.
func (c *Catalog) Free(x, z, level int) bool {
	return !c.Inside(x, z, level) && c.Open(x, z, level)
}

// LocChange is one live loc record of a client (a LOC_ADD_CHANGE, or a LOC_DEL tombstone with Deleted set),
// with the client's own layer rule: at most one per tile and layer.
type LocChange struct {
	X, Z, Level int
	Type        int
	Shape       int
	Rotate      int
	Deleted     bool
}

// layerOf is internal/locindex's shapeToLayer: walls and wall decorations, ground objects, ground decoration.
func layerOf(shape int) int {
	switch {
	case shape >= 0 && shape <= 3:
		return 0
	case shape >= 4 && shape <= 8:
		return 1
	case shape == 22:
		return 3
	case shape >= 9 && shape <= 21:
		return 2
	}
	return -1
}

// StateFromChanges reads every door's leaves off a client's live loc changes. A leaf the changes say nothing
// about is as the map has it: the closed one there, the open one not. A tombstone or another loc on the
// closed leaf's tile and layer is the closed leaf gone; the open leaf is present when a record of its id sits
// on its tile and layer. Changes on another level than a door's say nothing about it (a client holds only its
// own level's), so that door is read as the map has it.
func (c *Catalog) StateFromChanges(changes []LocChange) State {
	s := c.DefaultState
	for i, d := range c.Doors {
		closedLeaf, openLeaf := true, false
		cl, op := d.Closed(), d.Open()
		for _, ch := range changes {
			if ch.Level != d.Level {
				continue
			}
			if ch.X == cl.X && ch.Z == cl.Z && layerOf(ch.Shape) == layerOf(cl.Shape) {
				if ch.Deleted || ch.Type != cl.ID {
					closedLeaf = false
				}
			}
			if ch.X == op.X && ch.Z == op.Z && layerOf(ch.Shape) == layerOf(op.Shape) && !ch.Deleted && ch.Type == op.ID {
				openLeaf = true
			}
		}
		s = s.With(i, closedLeaf, openLeaf)
	}
	return s
}
