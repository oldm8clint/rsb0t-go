package runecraftrunner

// The trade handshake on a MAP (live 18 Oct 2026): the engine model of gate_engine_test.go with the sides standing on tiles, so a request
// is an op that runs only as the engine runs it: at once from an orthogonally adjacent tile, otherwise after the asker's naive walk
// (pathfind.Pack.NaiveApproach), and never, silently, when that walk ends against a solid tile. The crafter in these tests stands where
// the event monster's flee left him, on the far side of the Ruined Pillar from his stand.

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/pathfind/packtest"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

type engGeom struct {
	pack *pathfind.Pack
	pos  []tile.Tile
	// parked counts the requests the engine never ran (the asker's walk ended against a wall); ran those it did.
	parked, ran atomic.Int32
}

// gatePack is the gate in miniature (packtest.NatureGate): open ground in its real coordinates with the Ruined Pillar at 2865,3020 and
// the Mysterious stone at 2866,3022 as solid tiles, the stand at 2867,3020 and the wait tile at 2867,3021.
func gatePack(t *testing.T) *pathfind.Pack {
	t.Helper()
	pk, err := packtest.NatureGate()
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// place puts the sides on the map (side 0 the crafter) and shows each the others where they stand.
func (e *tradeEngine) place(pk *pathfind.Pack, pos ...tile.Tile) {
	if len(pos) != len(e.sides) {
		panic("place: one tile per side")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.geom = &engGeom{pack: pk, pos: append([]tile.Tile(nil), pos...)}
	for i := range e.sides {
		e.reshowLocked(i)
	}
}

// reshowLocked re-sends side i its own tile and the others' offsets from it.
func (e *tradeEngine) reshowLocked(i int) {
	g := e.geom
	s := e.sides[i]
	s.sim.StandAt(g.pos[i].X, g.pos[i].Z)
	// The crafter sees carrier k at index k; a carrier sees only the crafter, at index 1.
	if i == 0 {
		var ps []actionsim.PlayerAt
		for k := 1; k < len(e.sides); k++ {
			ps = append(ps, actionsim.PlayerAt{Name: e.sides[k].name, DX: g.pos[k].X - g.pos[0].X, DZ: g.pos[k].Z - g.pos[0].Z})
		}
		s.sim.PlacePlayers(ps...)
		return
	}
	s.sim.PlacePlayers(actionsim.PlayerAt{Name: e.sides[0].name, DX: g.pos[0].X - g.pos[i].X, DZ: g.pos[0].Z - g.pos[i].Z})
}

func navOf(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

// geomReaches says the op by side x on side y runs: from where x stands at once, or after the engine's naive walk, which moves x. A
// walk that dead-ends parks x where it stopped and the op is dropped without a word.
func (e *tradeEngine) geomReaches(x, y int) bool {
	g := e.geom
	if g == nil {
		return true
	}
	end, reached := g.pack.NaiveApproach(navOf(g.pos[x]), navOf(g.pos[y]), 48)
	to := tile.Tile{X: end.X, Z: end.Z}
	if to != g.pos[x] {
		g.pos[x] = to
		e.reshowLocked(x)
		// The other side sees x where it now is.
		e.reshowLocked(y)
	}
	if !reached {
		g.parked.Add(1)
		e.log = append(e.log, e.stamp()+" "+e.sides[x].name+" parked at "+fmt.Sprint(to)+" against a wall; the op never ran")
		return false
	}
	g.ran.Add(1)
	return true
}

// geomMove applies a MOVE_GAMECLICK to the side that sent it: the waypoints walked as the server walks them.
func (e *tradeEngine) geomMove(from int, b []byte) {
	g := e.geom
	if g == nil || len(b) < 7 {
		return
	}
	startX, startZ := int(b[3])<<8|int(b[4]), int(b[5])<<8|int(b[6])
	wps := []pathfind.NavPoint{{X: startX, Z: startZ}}
	for i := 7; i+1 < len(b); i += 2 {
		wps = append(wps, pathfind.NavPoint{X: startX + int(int8(b[i])), Z: startZ + int(int8(b[i+1]))})
	}
	end, _ := g.pack.EngineWalk(navOf(g.pos[from]), wps, 64)
	to := tile.Tile{X: end.X, Z: end.Z}
	if to == g.pos[from] {
		return
	}
	g.pos[from] = to
	e.reshowLocked(from)
	for k := range e.sides {
		if k != from {
			e.reshowLocked(k)
		}
	}
}

// runGeometry runs a crafter standing at crafterAt and carriers on carrierAt, each with the pack (or without: the carrier as it was)
// until `want` trades are received or the deadline.
func runGeometry(t *testing.T, pk *pathfind.Pack, crafterAt tile.Tile, carrierAt []tile.Tile, carrierPack *pathfind.Pack, want int32, attempt, deadline time.Duration) (*tradeEngine, *engineRun, []OfferResult) {
	t.Helper()
	withEngineClock(t)
	names := make([]string, len(carrierAt))
	for i := range names {
		names[i] = "Swarm " + string(rune('1'+i))
	}
	e := newTradeEngine(t, "Crafter", names...)
	e.sides[0].d.Pack = pk
	for _, s := range e.sides[1:] {
		s.d.Pack = carrierPack
		s.d.Log = func(string) {}
	}
	e.place(pk, append([]tile.Tile{crafterAt}, carrierAt...)...)
	e.start()
	defer e.close()
	r := &engineRun{}
	var stop atomic.Bool
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []OfferResult
	wg.Add(1)
	go func() { defer wg.Done(); e.crafterLoop(r, &stop, 8) }()
	for i := range names {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := e.sides[i+1]
			for !stop.Load() {
				res := TradeWithCrafter(s.d, OfferOptions{Crafter: "Crafter", Mode: OfferEssence, Timeout: attempt, Stand: runecraftplan.GateStandTile})
				mu.Lock()
				results = append(results, res)
				mu.Unlock()
				if res.Done {
					r.trades.Add(1)
					time.Sleep(14 * engTick)
					s.sim.Give(essID, runecraftplan.CarrierOffer-countOf(s.sim.Pack(), essID))
				}
			}
		}()
	}
	limit := time.Now().Add(deadline)
	for r.received.Load() < want && time.Now().Before(limit) {
		time.Sleep(10 * time.Millisecond)
	}
	stop.Store(true)
	e.halt.Store(true)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return e, r, append([]OfferResult(nil), results...)
}

// THE INCIDENT, reproduced. The crafter stands west of the pillar at 2864,3020; the carriers wait on the wait tile east of it. The
// carrier as it was (no pack, so it only ever asks from where it stands) asks and asks and is never answered: not one trade, every
// request silently dropped by the engine, and the carrier hears nothing.
func TestCarriersAsTheyWereNeverTradeWithACrafterWestOfThePillar(t *testing.T) {
	pk := gatePack(t)
	crafter := tile.Tile{X: 2864, Z: 3020}
	wait := runecraftplan.GateWaitTile
	e, r, results := runGeometry(t, pk, crafter, []tile.Tile{wait, wait}, nil, 1, 30*engTick, 6*time.Second)
	if r.received.Load() != 0 {
		t.Fatalf("the model traded %d times with a crafter the engine cannot be asked: it does not reproduce the stall\nlog %v", r.received.Load(), e.log)
	}
	if e.geom.parked.Load() < 4 {
		t.Fatalf("only %d requests were parked against the pillar", e.geom.parked.Load())
	}
	if e.geom.ran.Load() != 0 {
		t.Fatalf("%d request(s) ran", e.geom.ran.Load())
	}
	for _, res := range results {
		if res.Done || res.Heard || res.Moves != 0 {
			t.Fatalf("a carrier as it was heard something or moved: %+v", res)
		}
	}
	// What the carrier could see of it: requests, no word.
	silent := 0
	for _, res := range results {
		if res.NoScreen && res.Silent {
			silent++
		}
	}
	if silent == 0 {
		t.Fatalf("no attempt ended silent (the requests were sent, nothing was heard): %+v", results)
	}
	t.Logf("%d attempt(s) ended silent; %d requests parked; position of the first carrier %v; reason %q", silent, e.geom.parked.Load(), e.geom.pos[1], results[0].Reason)
	if e.geom.pos[1].X != 2866 || e.geom.pos[1].Z != 3020 {
		t.Errorf("the carrier ended at %v, want 2866,3020 against the pillar (where the live carriers were logged)", e.geom.pos[1])
	}
}

// The same gate with the carriers as they are now: after two requests with no word, and a crafter who has not moved, each walks to the
// open side of him, asks from there, and the trades run.
func TestCarriersWalkToTheOpenSideOfACrafterWestOfThePillarAndTrade(t *testing.T) {
	pk := gatePack(t)
	crafter := tile.Tile{X: 2864, Z: 3020}
	wait := runecraftplan.GateWaitTile
	e, r, results := runGeometry(t, pk, crafter, []tile.Tile{wait, wait}, pk, 4, 120*engTick, 40*time.Second)
	if r.received.Load() < 4 {
		t.Fatalf("only %d trades received; failures %v; parked %d ran %d\nlog %v", r.received.Load(), r.failures, e.geom.parked.Load(), e.geom.ran.Load(), e.log)
	}
	moves := 0
	var why string
	for _, res := range results {
		moves += res.Moves
		if res.Moves > 0 {
			why = res.MoveWhy
			if !pk.OperableFrom(navOf(res.MovedTo), navOf(crafter)) {
				t.Errorf("a carrier moved to %v, which cannot trade with %v", res.MovedTo, crafter)
			}
		}
	}
	if moves == 0 {
		t.Fatal("no carrier moved")
	}
	if !strings.Contains(why, "without a word") {
		t.Errorf("the reason for moving was %q", why)
	}
	if e.geom.parked.Load() > 6 {
		t.Errorf("%d requests were parked before the carriers moved, want a few", e.geom.parked.Load())
	}
	t.Logf("%d trades, %d move(s), %d parked request(s); a carrier's reason: %s", r.received.Load(), moves, e.geom.parked.Load(), why)
}

// A crafter on his stand is asked from the wait tile as always: nobody moves, nothing is parked, the trades run.
func TestCarriersOnTheWaitTileDoNotMoveForACrafterOnHisStand(t *testing.T) {
	pk := gatePack(t)
	e, r, results := runGeometry(t, pk, runecraftplan.GateStandTile, []tile.Tile{runecraftplan.GateWaitTile, runecraftplan.GateWaitTile}, pk, 4, 120*engTick, 40*time.Second)
	if r.received.Load() < 4 {
		t.Fatalf("only %d trades; failures %v\nlog %v", r.received.Load(), r.failures, e.log)
	}
	for _, res := range results {
		if res.Moves != 0 {
			t.Fatalf("a carrier on the wait tile moved for a crafter on his stand: %+v", res)
		}
	}
	if e.geom.parked.Load() != 0 {
		t.Fatalf("%d request(s) parked", e.geom.parked.Load())
	}
}
