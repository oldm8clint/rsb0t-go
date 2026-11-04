package runecraftrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/pathfind/packtest"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestPlanStandWalkIsTheLoneClickWhereTheServersGreedyWalkArrives(t *testing.T) {
	pk := gatePack(t)
	stand := runecraftplan.GateStandTile
	if p := PlanStandWalk(pk, stand, stand); p.Kind != StandWalkNone || len(p.Waypoints) != 0 {
		t.Fatalf("on the stand: %+v", p)
	}
	for _, from := range []tile.Tile{runecraftplan.Nature.Exit, {X: 2867, Z: 3021}, {X: 2870, Z: 3015}, {X: 2866, Z: 3019}} {
		p := PlanStandWalk(pk, from, stand)
		if p.Kind != StandWalkLone || len(p.Waypoints) != 1 || p.Waypoints[0] != navPoint(stand) {
			t.Errorf("from %v: %+v, want the lone click on the stand", from, p)
		}
	}
	// Without a pack there is nothing to ask: the lone click, as before.
	if p := PlanStandWalk(nil, tile.Tile{X: 2864, Z: 3020}, stand); p.Kind != StandWalkBlind || len(p.Waypoints) != 1 {
		t.Fatalf("no pack: %+v", p)
	}
	// Another level is not a walk at all.
	if p := PlanStandWalk(pk, tile.Tile{X: 2864, Z: 3020, Level: 1}, stand); p.Kind != StandWalkBlind {
		t.Fatalf("another level: %+v", p)
	}
}

func TestPlanStandWalkRoutesRoundThePillar(t *testing.T) {
	pk := gatePack(t)
	stand := runecraftplan.GateStandTile
	// The incident tile, and the line the event monster's flee ended on.
	for _, from := range []tile.Tile{{X: 2864, Z: 3020}, {X: 2862, Z: 3022}, {X: 2859, Z: 3022}, {X: 2860, Z: 3020}, {X: 2864, Z: 3019}} {
		p := PlanStandWalk(pk, from, stand)
		if end, arrived := pk.EngineWalk(navPoint(from), []pathfind.NavPoint{navPoint(stand)}, standWalkSteps); arrived {
			continue // a tile the lone click serves: the real-pack test lists which
		} else if p.Kind != StandWalkRouted || p.Parks != (tile.Tile{X: end.X, Z: end.Z}) || len(p.Waypoints) < 2 {
			t.Errorf("from %v: %+v (the lone click parks at %v)", from, p, end)
			continue
		}
		if end, arrived := pk.EngineWalk(navPoint(from), p.Waypoints, standWalkSteps); !arrived || end != navPoint(stand) {
			t.Errorf("from %v the routed click %v ended at %v", from, p.Waypoints, end)
		}
	}
	p := PlanStandWalk(pk, tile.Tile{X: 2864, Z: 3020}, stand)
	if p.Kind != StandWalkRouted || p.Parks != (tile.Tile{X: 2864, Z: 3020}) {
		t.Fatalf("the incident tile: %+v", p)
	}
}

// A stand the pack has no way to is the lone click, said as blind with where it parks him: the caller says so.
func TestPlanStandWalkWithNoRouteIsBlind(t *testing.T) {
	stand := runecraftplan.GateStandTile
	pk, err := packtest.Ground(2855, 3010, 2880, 3030, func(x, z int) bool {
		// The stand walled in on all eight sides.
		return x >= stand.X-1 && x <= stand.X+1 && z >= stand.Z-1 && z <= stand.Z+1 && (x != stand.X || z != stand.Z)
	})
	if err != nil {
		t.Fatal(err)
	}
	p := PlanStandWalk(pk, tile.Tile{X: 2862, Z: 3020}, stand)
	if p.Kind != StandWalkBlind || len(p.Waypoints) != 1 || p.Parks == (tile.Tile{}) {
		t.Fatalf("%+v", p)
	}
}

func TestApproachTilePicksTheNearestSideOfTheCrafterThatCanBeWalkedTo(t *testing.T) {
	pk := gatePack(t)
	crafter := tile.Tile{X: 2864, Z: 3020}
	wait := runecraftplan.GateWaitTile
	ap, ok := ApproachTile(pk, wait, crafter, wait)
	if !ok || ap.Tile != (tile.Tile{X: 2864, Z: 3021}) || ap.Steps != 3 {
		t.Fatalf("from the wait tile: %+v (ok %v), want 2864,3021 in 3 steps", ap, ok)
	}
	if !pk.OperableFrom(navPoint(ap.Tile), navPoint(crafter)) {
		t.Fatalf("%v cannot trade with %v", ap.Tile, crafter)
	}
	if end, arrived := pk.EngineWalk(navPoint(wait), ap.Waypoints, 32); !arrived || end != navPoint(ap.Tile) {
		t.Fatalf("the route ends at %v (arrived %v)", end, arrived)
	}
	// From the west the near side is the west.
	ap, ok = ApproachTile(pk, tile.Tile{X: 2861, Z: 3020}, crafter, wait)
	if !ok || ap.Tile != (tile.Tile{X: 2863, Z: 3020}) || ap.Steps != 2 {
		t.Fatalf("from the west: %+v (ok %v)", ap, ok)
	}
	// Already on a tile that can trade: nothing to move to.
	if _, ok := ApproachTile(pk, tile.Tile{X: 2864, Z: 3021}, crafter, wait); ok {
		t.Fatal("a carrier already beside him was offered another tile")
	}
	// No pack, or another level: nothing.
	if _, ok := ApproachTile(nil, wait, crafter, wait); ok {
		t.Fatal("an approach without a pack")
	}
	if _, ok := ApproachTile(pk, tile.Tile{X: 2867, Z: 3021, Level: 1}, crafter, wait); ok {
		t.Fatal("an approach across levels")
	}
	// A crafter nobody can get beside (walled in): none, and the tiles listed are empty.
	boxed, err := packtest.Ground(2855, 3010, 2880, 3030, func(x, z int) bool {
		return x >= 2863 && x <= 2865 && z >= 3019 && z <= 3021 && (x != 2864 || z != 3020)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ApproachTile(boxed, wait, crafter, wait); ok {
		t.Fatal("an approach tile beside a walled-in crafter")
	}
	if got := ApproachTiles(boxed, crafter); len(got) != 0 {
		t.Fatalf("tiles beside a walled-in crafter: %v", got)
	}
	if got := ApproachTiles(pk, crafter); len(got) != 3 {
		t.Fatalf("tiles beside the incident tile (the pillar takes the fourth): %v", got)
	}
}

func TestAskHeardTakesICantReachThatAsAnAnswerAndAsksSoonAgain(t *testing.T) {
	d := &fleetrunner.Deps{Chat: chatlog.New()}
	cursor := d.Chat.Total()
	var h askHeard
	if h.any() {
		t.Fatal("a fresh askHeard has heard something")
	}
	d.Chat.Record("I can't reach that!")
	h.hear(d, "Crafter", &cursor)
	if !h.cantReach || !h.any() || h.executed || h.busy {
		t.Fatalf("%+v", h)
	}
	if got := h.gap(); got != scaledDown(runecraftplan.GateRequestEvery) {
		t.Fatalf("the gap after I can't reach that is %s, want the busy gap", got)
	}
	// The other words are heard as before.
	h = askHeard{}
	d.Chat.Record("Sending trade offer...")
	h.hear(d, "Crafter", &cursor)
	if !h.executed || h.cantReach {
		t.Fatalf("%+v", h)
	}
}

func TestGateWatcherNotesTheLastRequestLineOfACarrierOnly(t *testing.T) {
	d := &fleetrunner.Deps{Chat: chatlog.New()}
	w := NewGateWatcher(d)
	if !w.LastRequest().IsZero() {
		t.Fatal("a request line before any")
	}
	d.Chat.Record("Stranger:tradereq:")
	w.poll(d, func(n string) bool { return n == "Acct 1" }, time.Now(), 1)
	if !w.LastRequest().IsZero() {
		t.Fatal("a stranger's line counted as a carrier's")
	}
	at := time.Now()
	d.Chat.Record("Acct 1:tradereq:")
	w.poll(d, func(n string) bool { return n == "Acct 1" }, at, 2)
	if !w.LastRequest().Equal(at) {
		t.Fatalf("last request %v, want %v", w.LastRequest(), at)
	}
	// Reset forgets the requests, not when the last line was read: the gate's silence is measured across a trip to the temple by its owner.
	w.Reset(d)
	if !w.LastRequest().Equal(at) {
		t.Fatal("Reset cleared the last request time")
	}
}

// "I can't reach that!" moves a carrier at once, off a tile that cannot trade, to a tile beside him; the walk is one click.
func TestACarrierToldItCannotReachHimWalksToAnotherSideAtOnce(t *testing.T) {
	pk := gatePack(t)
	sim := actionsim.New(gateObjs(), nil)
	wait := runecraftplan.GateWaitTile
	sim.StandAt(wait.X, wait.Z)
	sim.ShowPlayersAt(actionsim.PlayerAt{Name: "Crafter", DX: 2864 - wait.X, DZ: 3020 - wait.Z})
	var lines []string
	d := &fleetrunner.Deps{Session: sim, Chat: chatlog.New(), Pack: pk, Components: actionsim.Components(), Delay: func(time.Duration) bool { return false },
		Log: func(s string) { lines = append(lines, s) }}
	var app approachState
	trade := actions.NewTrade(sim)
	var res OfferResult
	res.Requests = 1
	crafter := tile.Tile{X: 2864, Z: 3020}
	if got, _ := crafterAt(d, "Crafter"); got != crafter {
		t.Fatalf("the crafter is seen at %v", got)
	}
	moved := app.consider(d, trade, OfferOptions{Crafter: "Crafter", Stand: runecraftplan.GateStandTile}, &res, crafter, askHeard{cantReach: true}, time.Now(), time.Now())
	if !moved || res.Moves != 1 || res.MovedTo != (tile.Tile{X: 2864, Z: 3021}) || !strings.Contains(res.MoveWhy, "can't reach") {
		t.Fatalf("moved %v, %+v", moved, res)
	}
	sent := sim.Sent()
	if len(sent) == 0 || int(sent[len(sent)-1][0]) != clientprot.MOVE_GAMECLICK {
		t.Fatalf("no walk packet sent: %v", sent)
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "walking 3 step(s) to 2864,3021 beside him") {
		t.Fatalf("log: %v", lines)
	}
	// On a tile that can trade it never moves, whatever it heard.
	sim.StandAt(2864, 3021)
	sim.PlacePlayers(actionsim.PlayerAt{Name: "Crafter", DX: 0, DZ: -1})
	var res2 OfferResult
	res2.Requests = 5
	if app.consider(d, trade, OfferOptions{Crafter: "Crafter"}, &res2, crafter, askHeard{cantReach: true}, time.Now(), time.Now()) || res2.Moves != 0 {
		t.Fatalf("a carrier beside him moved: %+v", res2)
	}
}

func TestACarrierWithNoPackNeverMoves(t *testing.T) {
	sim := actionsim.New(gateObjs(), nil)
	d := &fleetrunner.Deps{Session: sim, Chat: chatlog.New(), Delay: func(time.Duration) bool { return false }}
	var app approachState
	var res OfferResult
	res.Requests = 9
	if app.consider(d, actions.NewTrade(sim), OfferOptions{Crafter: "Crafter"}, &res, tile.Tile{X: 2864, Z: 3020}, askHeard{cantReach: true}, time.Now(), time.Now()) {
		t.Fatal("moved without a pack")
	}
}

// Silence is counted once per request: a look that comes round again at once (a request that could not be sent) is not a second silent
// request, and a carrier moves only after ApproachAsks of them.
func TestSilenceIsCountedOncePerRequestBeforeACarrierMoves(t *testing.T) {
	pk := gatePack(t)
	sim := actionsim.New(gateObjs(), nil)
	wait := runecraftplan.GateWaitTile
	sim.StandAt(wait.X, wait.Z)
	sim.ShowPlayersAt(actionsim.PlayerAt{Name: "Crafter", DX: 2864 - wait.X, DZ: 3020 - wait.Z})
	d := &fleetrunner.Deps{Session: sim, Chat: chatlog.New(), Pack: pk, Components: actionsim.Components(), Delay: func(time.Duration) bool { return false }}
	crafter := tile.Tile{X: 2864, Z: 3020}
	now := time.Now()
	app := approachState{stillAt: crafter, stillSince: now.Add(-10 * time.Second)}
	trade := actions.NewTrade(sim)
	o := OfferOptions{Crafter: "Crafter", Stand: runecraftplan.GateStandTile}
	res := OfferResult{Requests: 1}
	first := now.Add(-4 * time.Second)
	for i := 0; i < 3; i++ {
		if app.consider(d, trade, o, &res, crafter, askHeard{}, first, now) {
			t.Fatalf("moved on look %d of the same silent request", i+1)
		}
	}
	if app.silentAsks != 1 {
		t.Fatalf("%d silent requests counted for one request", app.silentAsks)
	}
	res.Requests = 2
	if !app.consider(d, trade, o, &res, crafter, askHeard{}, now, now) || res.Moves != 1 {
		t.Fatalf("no move after two silent requests: silent %d, %+v", app.silentAsks, res)
	}
	// News of any kind starts the count over.
	app = approachState{stillAt: crafter, stillSince: now.Add(-10 * time.Second), silentAsks: 5}
	sim.StandAt(wait.X, wait.Z)
	if app.consider(d, trade, o, &res, crafter, askHeard{executed: true}, now, now) || app.silentAsks != 0 {
		t.Fatalf("moved although the game said the request ran: silent %d", app.silentAsks)
	}
}

// The nudge steps the crafter off his stand and back: the tile is one a carrier could trade from (beside the stand, no wall between), the
// one nearest the portal's exit, and there is none without a pack.
func TestWiggleTileIsABesideTileNearTheExit(t *testing.T) {
	pk := gatePack(t)
	stand := runecraftplan.GateStandTile
	w, ok := WiggleTile(pk, stand, runecraftplan.Nature.Exit)
	if !ok || w == stand || !pk.CanInteractFrom(navPoint(w), navPoint(stand)) {
		t.Fatalf("wiggle tile %v ok %v", w, ok)
	}
	for _, other := range ApproachTiles(pk, stand) {
		if tile.Chebyshev(other, runecraftplan.Nature.Exit) < tile.Chebyshev(w, runecraftplan.Nature.Exit) {
			t.Errorf("%v is nearer the exit than the chosen %v", other, w)
		}
	}
	if _, ok := WiggleTile(nil, stand, runecraftplan.Nature.Exit); ok {
		t.Error("a wiggle tile without a pack")
	}
}
