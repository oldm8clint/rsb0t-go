package rangecook

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

func testConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func itemID(t *testing.T, cfg *cachecfg.Config, name string) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && !o.Noted() && strings.EqualFold(o.Name, name) {
			return int32(id)
		}
	}
	t.Fatalf("no item %q in this config", name)
	return 0
}

func testSim(cfg *cachecfg.Config, pack []actionsim.Stack) *actionsim.Session {
	return actionsim.New(actionsim.Objs{
		Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable },
	}, pack)
}

func fastDelay(time.Duration) bool { time.Sleep(time.Millisecond); return true }

func stackCount(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// The range room's bounds: every tile the mule was left on in the room counts; the door's own tile
// and the bank side do not (cmd/doordiag's 13-tile component).
func TestOnCatherbyRangeSide(t *testing.T) {
	for _, p := range []tile.Tile{CatherbyDoorInside, {X: 2817, Z: 3443}, {X: 2818, Z: 3442}} {
		if !OnCatherbyRangeSide(p) {
			t.Errorf("%v: want range side", p)
		}
	}
	for _, p := range []tile.Tile{CatherbyDoorTile, {X: 2809, Z: 3441}, {X: 2828, Z: 3441}, {X: 2817, Z: 3443, Level: 1}} {
		if OnCatherbyRangeSide(p) {
			t.Errorf("%v: want bank side", p)
		}
	}
}

// The range half against a simulated range: every use takes one raw fish and leaves a cooked or a
// burnt one in its place (content's cook_item: inv_del then inv_add), until none is left.
func TestCookOutCooksThePackAndStops(t *testing.T) {
	cfg := testConfig(t)
	raw, cooked, burnt := itemID(t, cfg, "Raw shrimps"), itemID(t, cfg, "Shrimps"), itemID(t, cfg, "Burnt fish")
	pack := make([]actionsim.Stack, 6)
	for i := range pack {
		pack[i] = actionsim.Stack{ID: raw, Count: 1}
	}
	sim := testSim(cfg, pack)
	uses := 0
	sim.OnSend = func(b []byte) {
		// OPLOCU is the last 13 bytes of the send (an anticheat packet may come first).
		if len(b) < 13 || int(b[len(b)-13]) != 184 {
			return
		}
		uses++
		sim.Remove(raw, 1)
		if uses%2 == 0 {
			sim.Give(burnt, 1)
		} else {
			sim.Give(cooked, 1)
		}
	}
	cd := &Deps{Session: sim, Config: cfg, Delay: fastDelay}
	rng := locindex.Match{Loc: locindex.Loc{ID: 2728, X: 2817, Z: 3444, Angle: 1, Shape: 10}, Name: "Range"}
	if got := CookOut(cd, rng, "Raw shrimps", 30, nil); got != 6 {
		t.Fatalf("used %d, want 6", got)
	}
	p := sim.Pack()
	if stackCount(p, raw) != 0 || stackCount(p, cooked) != 3 || stackCount(p, burnt) != 3 {
		t.Errorf("pack after cooking: %v", p)
	}
	// A stop request ends the loop between uses.
	sim2 := testSim(cfg, pack)
	used := 0
	sim2.OnSend = func(b []byte) {
		if len(b) >= 13 && int(b[len(b)-13]) == 184 {
			used++
			sim2.Remove(raw, 1)
			sim2.Give(cooked, 1)
		}
	}
	cd.Session = sim2
	if got := CookOut(cd, rng, "Raw shrimps", 30, func() bool { return used >= 2 }); got != 2 {
		t.Errorf("with a stop after two uses: used %d", got)
	}
}

// A stand-in for the engine's cook_item at 40 ms a tick, to pin the cadence CookOut is for (content cook_item does
// p_delay(1) before it deletes the raw item: a use read at tick T consumes at T+2, uses read at T+1 and T+2 are
// refused, the next is accepted at T+3). The sim's backpack is pushed to the client inside the tick that consumes, as the
// engine sends it, after that tick's packets were read. A loop that writes the moment the backpack shows the raw item
// gone is read at T+3 and cooks every 3 ticks with nothing refused; the loop this replaced (a 100 ms poll and a 600 ms
// pause) was read at T+4.
func TestCookOutIsReadInTheTickAfterTheConsumptionAndNothingIsRefused(t *testing.T) {
	cfg := testConfig(t)
	raw, cooked := itemID(t, cfg, "Raw shrimps"), itemID(t, cfg, "Shrimps")
	const loads = 9
	pack := make([]actionsim.Stack, loads)
	for i := range pack {
		pack[i] = actionsim.Stack{ID: raw, Count: 1}
	}
	sim := testSim(cfg, pack)

	var mu sync.Mutex
	pending, refused := 0, 0
	var consumed []int
	sim.OnSend = func(b []byte) {
		if len(b) >= 13 && int(b[len(b)-13]) == 184 { // OPLOCU, the last 13 bytes of the send
			mu.Lock()
			pending++
			mu.Unlock()
		}
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		tickLen := 40 * time.Millisecond
		next := time.NewTicker(tickLen)
		defer next.Stop()
		tick, acceptAt := 0, 0
		consumeAt := map[int]bool{}
		for {
			select {
			case <-done:
				return
			case <-next.C:
			}
			tick++
			mu.Lock()
			for ; pending > 0; pending-- { // processClientsIn
				if tick >= acceptAt {
					acceptAt = tick + 3
					consumeAt[tick+2] = true
				} else {
					refused++
				}
			}
			mu.Unlock()
			if consumeAt[tick] { // processPlayers, then the inventory goes out after PLAYER_INFO
				mu.Lock()
				consumed = append(consumed, tick)
				mu.Unlock()
				sim.Remove(raw, 1)
				sim.Give(cooked, 1)
			}
		}
	}()

	cd := &Deps{Session: sim, Config: cfg, Delay: func(d time.Duration) bool { time.Sleep(d); return true }}
	rng := locindex.Match{Loc: locindex.Loc{ID: 2728, X: 2817, Z: 3444, Angle: 1, Shape: 10}, Name: "Range"}
	if got := CookOut(cd, rng, "Raw shrimps", 30, nil); got != loads {
		t.Fatalf("cooked %d of %d", got, loads)
	}
	mu.Lock()
	defer mu.Unlock()
	if refused != 0 {
		t.Errorf("%d uses were written before the previous cook had finished and refused", refused)
	}
	if len(consumed) != loads {
		t.Fatalf("the engine stand-in consumed %d, want %d", len(consumed), loads)
	}
	span := consumed[len(consumed)-1] - consumed[0]
	perCook := float64(span) / float64(len(consumed)-1)
	// 3.0 when every write lands in its tick; a scheduler stall can push one write into the next tick (4), never more
	// than one in four of them here. The loop this replaced could not be below 4.
	if perCook > 3.3 {
		t.Errorf("one cook every %.2f ticks (consumed in ticks %v), want 3", perCook, consumed)
	}
}

// ---- the door crossing under a stop ----

// A stopping bot answers every Delay false, so the walk to the door fails at its first poll. That is the bot stopping,
// not a door that did not take the account across, and CrossCatherbyDoor says so (WhyStopped) so no caller retries it,
// counts it as an abandoned load or writes it down as one. Live 14 Oct 2026: 15 of 16 "could not reach 2816,3439,0 /
// 2816,3438,0 beside the Catherby range's own door" lines carried the second the fleet was stopped, the other the second
// a connection dropped.
func TestCrossCatherbyDoorSaysStoppedNotAFailedDoor(t *testing.T) {
	stopping := true
	walks := 0
	d := &Deps{
		Session:  actionsim.New(actionsim.Objs{}, nil),
		Delay:    func(time.Duration) bool { return !stopping },
		WalkNear: func(tile.Tile, int, time.Duration) bool { walks++; return false },
	}
	if ok, why := CrossCatherbyDoor(d); ok || why != WhyStopped || walks != 1 {
		t.Fatalf("stopping: ok %v why %q after %d walks, want false %q after one", ok, why, walks, WhyStopped)
	}
	// The same failed walk of a live bot is the door's, with the tile it could not reach.
	stopping = false
	ok, why := CrossCatherbyDoor(d)
	if ok || why == WhyStopped || !strings.Contains(why, "could not reach 2816,3438,0 beside the Catherby range's own door") {
		t.Fatalf("running: ok %v why %q, want the unreachable door tile named", ok, why)
	}
}

// ---- the real map: the route is right, and so is the failure to find one behind the counter ----

// The Catherby bank, its range and the 14 Oct 2026 log (build d5362bb2, 79 self-cook loads through the door and no
// failure of it): the door the cooks cross is loc 1530 at 2816,3438, shut on its north edge, and it is the only way
// into the 13-tile room the range stands in. Every walkable bank tile (2807..2812, 3436..3441) walks to the door's own
// tile on the static pack, none walks into the room (the door is baked shut, which is what the crossing exists for),
// and from inside every stand tile of the range is reachable. The "no path" lines that surrounded the 14 Oct stops
// were two other things, neither of them the route:
//
//   - 2807..2812,3443: the north stands of the bank booths (z 3442), behind the counter, which WalkToUse tries after
//     the south stand fails (a stopping bot fails the south stand too);
//   - 2815,3450 / 2816,3449 / 2814,3449: three of the four approach tiles of the "Door" at 2815,3449, a house door 12
//     tiles away that the door hunt picks because it is the nearest closed door (the fourth, 2815,3448, is on the
//     street and reachable from the bank).
//
// Needs RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG.
func TestCatherbyBankReachesTheDoorAndOnlyTheDoor(t *testing.T) {
	packPath, ixPath, cfgPath := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if packPath == "" || ixPath == "" || cfgPath == "" {
		t.Skip("set RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	nav := func(x, z int) pathfind.NavPoint { return pathfind.NavPoint{X: x, Z: z} }
	reaches := func(from, to pathfind.NavPoint) bool { return pk.FindPath(from, to).OK }

	// The door is the loc the crossing says it is.
	door, ok := ix.Nearest(cfg, locindex.Query{X: CatherbyDoorTile.X, Z: CatherbyDoorTile.Z, Radius: 1, Name: catherbyDoorName, Op: "Open"}, nil)
	if !ok || door.X != CatherbyDoorTile.X || door.Z != CatherbyDoorTile.Z || door.ID != 1530 {
		t.Fatalf("the Door at %v is %+v (found %v), want loc 1530", CatherbyDoorTile, door, ok)
	}
	rng, ok := ix.Nearest(cfg, locindex.Query{X: 2817, Z: 3444, Radius: 1, Name: RangeLocName}, nil)
	if !ok {
		t.Fatal("no Range at 2817,3444")
	}
	rl, ok := ReachLoc(cfg, rng)
	if !ok {
		t.Fatal("the Range has no config")
	}
	stands := locreach.Tiles(rl)
	if len(stands) == 0 {
		t.Fatal("the Range has no stand tile")
	}
	for _, s := range stands {
		// The range's own two footprint tiles are listed as stands too (reachedLoc accepts them) but nobody can stand
		// on them: only the walkable ones are the room's.
		if pk.Walkable(s.X, s.Z, s.Level) && !OnCatherbyRangeSide(tile.Tile{X: s.X, Z: s.Z, Level: s.Level}) {
			t.Errorf("stand tile %v is outside the range room", s)
		}
		if !reaches(nav(CatherbyDoorInside.X, CatherbyDoorInside.Z), nav(s.X, s.Z)) {
			t.Errorf("stand tile %v is not reachable from inside the door", s)
		}
	}

	bankTiles := 0
	for x := 2807; x <= 2812; x++ {
		for z := 3436; z <= 3441; z++ {
			if !pk.Walkable(x, z, 0) {
				continue
			}
			from := nav(x, z)
			if !reaches(nav(2810, 3437), from) {
				continue // not part of the open street: a booth, a plant, a wall
			}
			bankTiles++
			if !reaches(from, nav(CatherbyDoorTile.X, CatherbyDoorTile.Z)) {
				t.Errorf("bank tile %d,%d cannot walk to the door's own tile %v", x, z, CatherbyDoorTile)
			}
			if reaches(from, nav(CatherbyDoorInside.X, CatherbyDoorInside.Z)) {
				t.Errorf("bank tile %d,%d reaches the inside of the door on the static pack: the control failed", x, z)
			}
			// Behind the counter (the booths stand on z 3442): the north stand of a booth.
			for bx := 2807; bx <= 2812; bx++ {
				if reaches(from, nav(bx, 3443)) {
					t.Errorf("bank tile %d,%d reaches %d,3443 behind the counter", x, z, bx)
				}
			}
			// The other door the log chased: only its south approach tile is on the street.
			if !reaches(from, nav(2815, 3448)) {
				t.Errorf("bank tile %d,%d cannot reach 2815,3448, the street side of the Door at 2815,3449", x, z)
			}
			for _, c := range [][2]int{{2815, 3450}, {2816, 3449}, {2814, 3449}} {
				if reaches(from, nav(c[0], c[1])) {
					t.Errorf("bank tile %d,%d reaches %d,%d inside the house behind the Door at 2815,3449", x, z, c[0], c[1])
				}
			}
		}
	}
	if bankTiles < 20 {
		t.Fatalf("only %d walkable bank-side tiles were checked: the test is not looking at the bank", bankTiles)
	}
	// Each open booth is answered from the tile straight below it (the south stand), which every bank tile reaches;
	// any other stand it lists is out of the street's reach.
	for _, bx := range []int{2807, 2809, 2810, 2811} {
		booth := nav(bx, 3442)
		if !pk.CanInteractFrom(nav(bx, 3441), booth) {
			t.Errorf("the booth at %d,3442 does not answer from %d,3441 below it", bx, bx)
		}
		for _, s := range pk.InteractTiles(booth) {
			if s.Z == 3441 {
				continue
			}
			if reaches(nav(2810, 3437), s) {
				t.Errorf("the booth at %d,3442 lists the stand %d,%d, which the street reaches: the counter's far side is not what this test thinks", bx, s.X, s.Z)
			}
		}
	}
	t.Logf("%d bank-side tiles walk to the door at %v (loc %d); the range at %d,%d has %d stand tile(s) %v, all reachable from %v", bankTiles, CatherbyDoorTile, door.ID, rng.X, rng.Z, len(stands), stands, CatherbyDoorInside)
}
