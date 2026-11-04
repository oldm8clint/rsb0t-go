package combatrunner

import (
	"os"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/webwalk"
)

// The real map for the door tests: the collision pack, the static loc index, the loc configs and the webwalk graph,
// loaded once. They skip without RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG, like every test on
// real data (CLAUDE.md, Commands).
type realData struct {
	pack    *pathfind.Pack
	locs    *locindex.Index
	cfg     *cachecfg.Config
	planner *webwalk.Planner
}

var (
	realOnce sync.Once
	realVal  realData
	realErr  error
)

func loadReal(t *testing.T) realData {
	t.Helper()
	pk, ix, jag := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pk == "" || ix == "" || jag == "" {
		t.Skip("set RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	realOnce.Do(func() {
		var err error
		if realVal.pack, err = pathfind.Load(pk); err != nil {
			realErr = err
			return
		}
		if realVal.locs, err = locindex.Load(ix); err != nil {
			realErr = err
			return
		}
		if realVal.cfg, err = cachecfg.LoadFile(jag); err != nil {
			realErr = err
			return
		}
		g, err := webwalk.Default()
		if err != nil {
			realErr = err
			return
		}
		realVal.planner = webwalk.NewPlanner(realVal.pack, g)
	})
	if realErr != nil {
		t.Fatal(realErr)
	}
	return realVal
}

// simSession is the least actions.Session the door code needs: a state to read the tile and the live loc changes
// from, and sends that go nowhere.
type simSession struct {
	st       *gamestate.State
	counters anticheat.Counters
}

func (s *simSession) State() *gamestate.State     { return s.st }
func (s *simSession) Cheats() *anticheat.Counters { return &s.counters }
func (s *simSession) Send(build func(*packet.Packet)) error {
	p := packet.NewWriter(256)
	build(p)
	return nil
}

func payload(build func(p *packet.Packet)) []byte {
	p := packet.NewWriter(64)
	build(p)
	return p.Data[:p.Pos]
}

type bitField struct {
	n int
	v uint32
}

// bitPack is the write side of packet.Packet.GBit, MSB first, for PLAYER_INFO (internal/randomevents' helper).
func bitPack(totalBytes int, fields ...bitField) []byte {
	buf := make([]byte, totalBytes)
	pos := 0
	for _, f := range fields {
		n, v := f.n, f.v
		for n > 0 {
			bytePos, bitOff := pos>>3, pos&7
			free := 8 - bitOff
			take := n
			if take > free {
				take = free
			}
			chunk := (v >> uint(n-take)) & ((1 << uint(take)) - 1)
			buf[bytePos] |= byte(chunk << uint(free-take))
			pos += take
			n -= take
		}
	}
	return buf
}

// simWorld is a gamestate with the player somewhere and the server's loc changes arriving as packets, as they do
// live. The build area is fixed at the first placement (a REBUILD_NORMAL clears nothing the doors need, but moving
// inside one area is all the tests do), so every tile used must lie within 48 tiles west or south and 55 east or
// north of it.
type simWorld struct {
	st           *gamestate.State
	baseX, baseZ int
}

func newSimWorld(x, z, level int) *simWorld {
	w := &simWorld{st: gamestate.New()}
	w.baseX, w.baseZ = (x/8-6)*8, (z/8-6)*8
	w.st.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) {
		p.P2(uint16(w.baseX/8 + 6))
		p.P2(uint16(w.baseZ/8 + 6))
	}))
	w.teleport(x, z, level)
	return w
}

// teleport puts the player on a tile of the build area: PLAYER_INFO's local-player teleport branch.
func (w *simWorld) teleport(x, z, level int) {
	w.st.Handle(serverprot.PLAYER_INFO, bitPack(3,
		bitField{1, 1}, bitField{2, 3}, bitField{2, uint32(level)},
		bitField{7, uint32(x - w.baseX)}, bitField{7, uint32(z - w.baseZ)},
		bitField{1, 0}, bitField{1, 0},
	))
}

// locChange is one LOC_DEL (id below zero) or LOC_ADD_CHANGE at an absolute tile.
func (w *simWorld) locChange(x, z, id, shape, angle int) {
	lx, lz := x-w.baseX, z-w.baseZ
	w.st.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) {
		p.P1(byte(lx &^ 7))
		p.P1(byte(lz &^ 7))
	}))
	pos := byte((lx&7)<<4 | lz&7)
	if id < 0 {
		w.st.Handle(serverprot.LOC_DEL, payload(func(p *packet.Packet) { p.P1(pos); p.P1(byte(shape<<2 | angle)) }))
		return
	}
	w.st.Handle(serverprot.LOC_ADD_CHANGE, payload(func(p *packet.Packet) {
		p.P1(pos)
		p.P1(byte(shape<<2 | angle))
		p.P2(uint16(id))
	}))
}

// simDoor is a shut door the simulated server opens when it is operated: open plays back what content's
// open_door (or open_double_doors_*) does to the world.
type simDoor struct {
	id, x, z, level int
	open            func(w *simWorld)
	opened          bool
	// group ties the leaves of a double door together: operating one opens all of them, as the server's
	// open_double_doors_* does.
	group string
}

// simClient is a webwalk.Client over a simWorld: movement is instant, a door opens when it is operated, and time is
// a counter the delays advance.
type simClient struct {
	w     *simWorld
	now   time.Time
	doors []*simDoor
	ops   int
}

func (c *simClient) Tile() (webwalk.Point, bool) {
	x, z, level, ok := c.w.st.Tile()
	return webwalk.Point{X: x, Z: z, Level: level}, ok
}

func (c *simClient) Walk(path []webwalk.Point, run bool) error {
	if len(path) > 0 {
		last := path[len(path)-1]
		c.w.teleport(last.X, last.Z, last.Level)
	}
	return nil
}

func (c *simClient) FindLocs(q webwalk.LocQuery) []webwalk.LocMatch {
	var out []webwalk.LocMatch
	for _, d := range c.doors {
		if d.opened || d.level != q.At.Level {
			continue
		}
		if abs(d.x-q.At.X) > q.Radius || abs(d.z-q.At.Z) > q.Radius {
			continue
		}
		match := len(q.IDs) == 0
		for _, id := range q.IDs {
			if id == d.id {
				match = true
			}
		}
		if match {
			out = append(out, webwalk.LocMatch{ID: d.id, X: d.x, Z: d.z, Level: d.level, Name: "Door", Op: 1})
		}
	}
	return out
}

func (c *simClient) OpLoc(op, x, z, id int) error {
	c.ops++
	for _, d := range c.doors {
		if !d.opened && d.x == x && d.z == z && d.id == id && d.open != nil {
			d.open(c.w)
			for _, other := range c.doors {
				if other == d || (d.group != "" && other.group == d.group) {
					other.opened = true
				}
			}
		}
	}
	return nil
}

func (c *simClient) CantReachSince(time.Time) bool { return false }
func (c *simClient) Delay(d time.Duration) bool    { c.now = c.now.Add(d); return true }
func (c *simClient) Now() time.Time                { return c.now }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
