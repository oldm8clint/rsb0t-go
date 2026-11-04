package bot

// The Nature gate that could not hear its carriers (live 18 Oct 2026, mastercrafter_gatedeaf.go).

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/pathfind/packtest"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// gatePackStacks is the gate pack the other gate tests use: the talisman, the Natures held and the fare.
func gatePackStacks() []actionsim.Stack {
	return []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}
}

// lastMove decodes the last walk packet the session sent: the waypoints in it, absolute.
func lastMove(sim *actionsim.Session) ([]pathfind.NavPoint, bool) {
	sent := sim.Sent()
	for i := len(sent) - 1; i >= 0; i-- {
		b := sent[i]
		if len(b) >= 7 && int(b[0]) == clientprot.MOVE_GAMECLICK {
			x, z := int(b[3])<<8|int(b[4]), int(b[5])<<8|int(b[6])
			wps := []pathfind.NavPoint{{X: x, Z: z}}
			for k := 7; k+1 < len(b); k += 2 {
				wps = append(wps, pathfind.NavPoint{X: x + int(int8(b[k])), Z: z + int(int8(b[k+1]))})
			}
			return wps, true
		}
	}
	return nil, false
}

func gateWorldOn(t *testing.T, r *masterRig, pk *pathfind.Pack) *masterNatureWorld {
	t.Helper()
	w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.stand, w.haveStand = runecraftplan.GateStandTile, true })
	w.d.Pack = pk
	return w
}

func natureGatePack(t *testing.T) *pathfind.Pack {
	t.Helper()
	pk, err := packtest.NatureGate()
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// THE STAND CLICK. From the tile west of the pillar the lone waypoint on the stand parks him against it (the server walks it greedily),
// so the one click carries the pack's route; from the portal's exit tile the lone click arrives and is sent as it always was.
func TestTheStandClickIsTheRoutedWayWhenTheLoneWaypointWouldParkHim(t *testing.T) {
	pk := natureGatePack(t)
	stand := runecraftplan.GateStandTile
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(2864, 3020)
	w := gateWorldOn(t, r, pk)
	w.gateIdle()
	if n := movesSent(r.sim); n != 1 {
		t.Fatalf("%d walk packets, want 1", n)
	}
	wps, ok := lastMove(r.sim)
	if !ok || len(wps) < 2 {
		t.Fatalf("the stand click carried %v: a lone waypoint, which parks him", wps)
	}
	from := pathfind.NavPoint{X: 2864, Z: 3020}
	if end, arrived := pk.EngineWalk(from, wps, 64); !arrived || end != (pathfind.NavPoint{X: stand.X, Z: stand.Z}) {
		t.Fatalf("the server walks the click %v to %v (arrived %v), want the stand %v", wps, end, arrived, stand)
	}
	// The control: the old click, a lone waypoint on the stand, does park him.
	if end, arrived := pk.EngineWalk(from, []pathfind.NavPoint{{X: stand.X, Z: stand.Z}}, 64); arrived || end != from {
		t.Fatalf("the lone click from the incident tile ended at %v (arrived %v): the fixture does not reproduce the stall", end, arrived)
	}
	if !r.logged("the stand click from 2864,3020 would park him at 2864,3020") || !r.logged("sending the routed way to the stand (2867,3020)") {
		t.Fatalf("log: %v", r.logs)
	}
	// The line is not repeated every click.
	w.standClick = time.Now().Add(-2 * standClickEvery)
	w.standRouted = false
	w.gateIdle()
	n := 0
	for _, l := range r.logs {
		if strings.Contains(l, "would park him") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d lines about the routed click, want 1: %v", n, r.logs)
	}
}

func TestTheStandClickFromTheExitTileStaysTheLoneClick(t *testing.T) {
	pk := natureGatePack(t)
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := gateWorldOn(t, r, pk)
	w.gateIdle()
	wps, ok := lastMove(r.sim)
	if !ok || len(wps) != 1 || wps[0].X != runecraftplan.GateStandTile.X || wps[0].Z != runecraftplan.GateStandTile.Z {
		t.Fatalf("the click from the exit tile was %v, want the lone stand waypoint", wps)
	}
	if r.logged("would park him") {
		t.Fatalf("log: %v", r.logs)
	}
}

// A routed click is several tiles of walking: while he is on his way the next click waits, so a second one from a tile the client has
// not caught up with does not walk him back.
func TestARoutedStandClickIsNotRepeatedWhileHeWalks(t *testing.T) {
	pk := natureGatePack(t)
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(2864, 3020)
	w := gateWorldOn(t, r, pk)
	w.gateIdle()
	if !w.standRouted {
		t.Fatal("the first click was not routed")
	}
	// Two ticks later he has taken a step: no new click.
	r.sim.StandAt(2864, 3021)
	w.standClick = time.Now().Add(-2 * standClickEvery)
	w.gateIdle()
	if n := movesSent(r.sim); n != 1 {
		t.Fatalf("%d walk packets while he walks the route, want 1", n)
	}
	// Still on the same tile as the click was made from a few seconds on: the click is sent again.
	r.sim.StandAt(2864, 3020)
	w.standClick = time.Now().Add(-2 * standClickEvery)
	w.gateIdle()
	if n := movesSent(r.sim); n != 2 {
		t.Fatalf("%d walk packets, want the repeat from the unmoved tile", n)
	}
}

// Clicks that stopped working (a wall the pack does not know) hand the walk to the routed walker once they have left him on the same tile
// standNoMoveClicks times; the walker runs between GateSteps, not inside the idle poll.
func TestStandClicksThatDoNotMoveHimHandTheWalkToTheWalker(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(2864, 3020)
	w := gateWorldOn(t, r, nil) // no pack: the blind lone click, which the simulated server never walks
	var walked []tile.Tile
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool {
		walked = append(walked, dest)
		r.sim.StandAt(dest.X, dest.Z)
		return true
	}
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	for i := 0; i < standNoMoveClicks+1; i++ {
		w.standClick = time.Now().Add(-2 * standClickEvery)
		w.gateIdle()
	}
	if !w.deaf.standEsc {
		t.Fatalf("%d clicks left him on the same tile and the walker is not due", standNoMoveClicks+1)
	}
	if len(walked) != 0 {
		t.Fatalf("the walker ran inside the idle poll: %v", walked)
	}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if len(walked) != 1 || walked[0] != runecraftplan.GateStandTile {
		t.Fatalf("the walker went to %v, want the stand", walked)
	}
	if !r.logged("the stand click did not move him (at 2864,3020,0") || w.deaf.standEsc || w.deaf.standNoMove != 0 {
		t.Fatalf("log %v esc %v noMove %d", r.logs, w.deaf.standEsc, w.deaf.standNoMove)
	}
	// A click that DID move him in between starts the count afresh.
	r.sim.StandAt(2864, 3020)
	w.standClick = time.Time{}
	w.deaf.standNoMove = 0
	w.standClickAt = tile.Tile{}
	for i := 0; i < standNoMoveClicks; i++ {
		w.standClick = time.Now().Add(-2 * standClickEvery)
		w.gateIdle()
		r.sim.StandAt(2864+i%2, 3020) // moved a tile since the click
		w.standClickAt = tile.Tile{X: 2863, Z: 3020}
	}
	if w.deaf.standEsc {
		t.Fatal("clicks that moved him handed the walk to the walker")
	}
}

// The first click of a cycle is not "the click he did not move after": the last was made from the same exit tile twenty seconds ago.
func TestAStaleClickDoesNotCountAsOneThatDidNotMoveHim(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := gateWorldOn(t, r, natureGatePack(t))
	for i := 0; i < 2*standNoMoveClicks; i++ {
		w.standClick = time.Now().Add(-20 * time.Second) // the last cycle's click, from this same tile
		w.standClickAt = runecraftplan.Nature.Exit
		w.gateIdle()
	}
	if w.deaf.standEsc || w.deaf.standNoMove != 0 {
		t.Fatalf("stale clicks counted: esc %v noMove %d", w.deaf.standEsc, w.deaf.standNoMove)
	}
}

// ---- carriers waiting and no request line ----

func publishWaiting(r *masterRig, names ...string) {
	for _, n := range names {
		r.ctx.Bus.Publish(mulebus.CarrierMessage(n, "main", runecraftplan.GateWaitTile, mulebus.CarrierWaiting, runecraftplan.CarrierOffer))
	}
}

// quietFor back-dates the gate's silence: carriers have been waiting, and no request line has come, for d.
func quietFor(w *masterNatureWorld, d time.Duration) {
	w.deaf.lastGate = time.Now()
	w.deaf.since = time.Now().Add(-d)
	w.deaf.waitingSince = time.Now().Add(-d)
	w.carriersAt = time.Time{}
}

func TestACrafterOffTheStandWithCarriersWaitingAndNoRequestLineIsWalkedOntoIt(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(2864, 3020)
	w := gateWorldOn(t, r, natureGatePack(t))
	var walked []tile.Tile
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool {
		walked = append(walked, dest)
		r.sim.StandAt(dest.X, dest.Z)
		return true
	}
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	publishWaiting(r, "acct1", "acct2")
	quietFor(w, 30*time.Second)
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if len(walked) != 1 || walked[0] != runecraftplan.GateStandTile {
		t.Fatalf("walked %v, want one walk to the stand; log %v", walked, r.logs)
	}
	for _, want := range []string{"gate deaf: 2 carrier(s) waiting with a load and no request line in", "he is not on the stand", "I stand at 2864,3020,0", "off the stand",
		"a lone stand click from here would park him at 2864,3020"} {
		if !r.logged(want) {
			t.Fatalf("no log line with %q: %v", want, r.logs)
		}
	}
	if w.deaf.walks != 1 {
		t.Fatalf("%d corrective walks", w.deaf.walks)
	}
	// On the stand nothing is walked, and the quiet clock started over with the walk.
	if time.Since(w.deaf.since) > time.Second {
		t.Fatalf("the quiet clock was not restarted by the walk: %s", time.Since(w.deaf.since))
	}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if len(walked) != 1 {
		t.Fatalf("walked again straight away: %v", walked)
	}
}

func TestACrafterOnTheStandWithCarriersWaitingAndNoRequestLineSaysSoAndWalksNowhere(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w := gateWorldOn(t, r, natureGatePack(t))
	walks := 0
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool { walks++; return true }
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	publishWaiting(r, "acct1")
	quietFor(w, 45*time.Second)
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if walks != 0 || !r.logged("gate deaf: 1 carrier(s) waiting with a load, no request line in") || !r.logged("and he is on the stand") || !r.logged("modals main/side/chat") {
		t.Fatalf("walks %d, log %v", walks, r.logs)
	}
	// Said once, not every two seconds.
	r.logs = nil
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if len(r.logs) != 0 {
		t.Fatalf("said again at once: %v", r.logs)
	}
}

func TestAQuietGateWithNoCarrierWaitingIsHealthy(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(2864, 3020)
	w := gateWorldOn(t, r, natureGatePack(t))
	walks := 0
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool { walks++; return true }
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	quietFor(w, time.Hour)
	// A carrier in transit (not waiting) and one waiting far from the gate are not at it.
	r.ctx.Bus.Publish(mulebus.CarrierMessage("acct1", "main", runecraftplan.GateWaitTile, mulebus.CarrierTransit, 25))
	r.ctx.Bus.Publish(mulebus.CarrierMessage("acct2", "main", tile.Tile{X: 2809, Z: 3441}, mulebus.CarrierWaiting, 25))
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if walks != 0 || r.logged("gate deaf") {
		t.Fatalf("walks %d log %v", walks, r.logs)
	}
	// Carriers that have only just come to wait are given the time too.
	publishWaiting(r, "acct3")
	w.deaf.waitingSince = time.Now()
	w.carriersAt = time.Time{}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if walks != 0 || r.logged("gate deaf") {
		t.Fatalf("a carrier that just arrived was a deaf gate: walks %d log %v", walks, r.logs)
	}
}

// A request line read, a trade, and a return from the temple each start the silence afresh.
func TestARequestLineATradeAndAReturnStartTheSilenceAfresh(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	r.sim.ShowPlayers("Acct 1")
	w := gateWorldOn(t, r, natureGatePack(t))
	w.set.Runners = []string{"acct_1"}
	w.watch = runecraftrunner.NewGateWatcher(w.d)
	quietFor(w, 10*time.Minute)
	r.ctx.Chat.Record("Acct 1:tradereq:")
	// GateStep reads the line (and answers it: the simulated server refuses the answer or opens nothing; the point is the read).
	runecraftrunner.GateStep(w.d, w.watch, runecraftrunner.GateOptions{Allowed: w.allowed, Wait: 50 * time.Millisecond, OnStand: func() bool { return false }})
	line := w.watch.LastRequest()
	if line.IsZero() {
		t.Fatal("the watcher did not note the request line")
	}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if !w.deaf.since.Equal(line) || !w.deaf.waitingSince.IsZero() || w.deaf.walks != 0 {
		t.Fatalf("since %v (line %v) waitingSince %v walks %d", w.deaf.since, line, w.deaf.waitingSince, w.deaf.walks)
	}
	// A trade (any result but nothing) starts it afresh.
	quietFor(w, 10*time.Minute)
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived})
	if time.Since(w.deaf.since) > time.Second {
		t.Fatalf("a trade did not restart the quiet: %s", time.Since(w.deaf.since))
	}
	// So does a return to the gate after the temple (Gate() was not run for longer than gateEntryGap).
	quietFor(w, 10*time.Minute)
	w.deaf.lastGate = time.Now().Add(-time.Minute)
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if time.Since(w.deaf.since) > time.Second || r.logged("gate deaf") {
		t.Fatalf("a return to the gate did not restart the quiet: since %s ago, log %v", time.Since(w.deaf.since), r.logs)
	}
}

// A gate with no trade says how it stands: the first line after ten minutes, then every ten.
func TestAGateWithNoTradeForTenMinutesSaysHowItStands(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(2864, 3020)
	w := gateWorldOn(t, r, natureGatePack(t))
	w.started = time.Now().Add(-25 * time.Minute)
	w.deaf.lastGate = time.Now()
	w.deaf.since = time.Now()
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if !r.logged("no trade for 25m0s: 0 carrier(s) on the bus, 0 waiting at the gate with a load; last request line none read this round; I stand at 2864,3020,0") || !r.logged("off the stand") {
		t.Fatalf("log: %v", r.logs)
	}
	r.logs = nil
	w.deaf.lastGate = time.Now()
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if len(r.logs) != 0 {
		t.Fatalf("the no-trade line came twice: %v", r.logs)
	}
	// A trade a minute ago: no line.
	w.deaf.quietAt = time.Time{}
	w.lastTrade = time.Now().Add(-time.Minute)
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if len(r.logs) != 0 {
		t.Fatalf("a line for a gate that traded a minute ago: %v", r.logs)
	}
}

// Gate() runs the watch after every trading step.
func TestGateRunsTheWatchAfterEveryStep(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := gateWorldOn(t, r, natureGatePack(t))
	if res := w.Gate(); !res.OK {
		t.Fatalf("%+v", res)
	}
	if w.deaf.lastGate.IsZero() {
		t.Fatal("Gate() did not run the watch")
	}
}

// ---- the ladder: a gate that stays deaf past its walks ends its round ----

// sentOpcode counts the packets of one opcode the session has sent.
func sentOpcode(sim *actionsim.Session, op int) int {
	n := 0
	for _, b := range sim.Sent() {
		if len(b) > 0 && int(b[0]) == op {
			n++
		}
	}
	return n
}

// stubWalks replaces the routed walker with one that records its destinations and puts the simulated account there.
func stubWalks(t *testing.T, r *masterRig) *[]tile.Tile {
	t.Helper()
	var walked []tile.Tile
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool {
		walked = append(walked, dest)
		r.sim.StandAt(dest.X, dest.Z)
		return true
	}
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	return &walked
}

// On the stand with carriers waiting and no request line: three minutes in, the gate nudges (closes the open chat box, forgets the lines
// it read, steps off the stand and back); six minutes in, with the nudge having had its time, it latches the end of the round, and Gate()
// then fails with the reason until runGateLoop ends the round. It does not end a round it has not nudged, and a request line or a trade
// starts the ladder afresh.
func TestADeafCrafterOnTheStandIsNudgedAndThenTheRoundEnds(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w := gateWorldOn(t, r, natureGatePack(t))
	walked := stubWalks(t, r)
	r.sim.OpenChat(4242) // a chat box left open stops the trade screens
	publishWaiting(r, "acct1", "acct2")

	// Two minutes in: a warning, no nudge yet.
	quietFor(w, 2*time.Minute)
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if w.deaf.nudged || len(*walked) != 0 || sentOpcode(r.sim, clientprot.CLOSE_MODAL) != 0 || !r.logged("gate deaf: 2 carrier(s) waiting with a load, no request line in") {
		t.Fatalf("nudged at two minutes: %+v walked %v log %v", w.deaf, *walked, r.logs)
	}

	// Four minutes in: the nudge.
	quietFor(w, 4*time.Minute)
	w.deaf.notedAt = time.Time{}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if !w.deaf.nudged || w.deaf.endWhy != "" {
		t.Fatalf("nudged %v endWhy %q", w.deaf.nudged, w.deaf.endWhy)
	}
	if sentOpcode(r.sim, clientprot.CLOSE_MODAL) != 1 {
		t.Fatalf("the open chat box was not closed: %d CLOSE_MODAL", sentOpcode(r.sim, clientprot.CLOSE_MODAL))
	}
	if len(*walked) != 2 || (*walked)[1] != runecraftplan.GateStandTile || (*walked)[0] == runecraftplan.GateStandTile {
		t.Fatalf("the step off the stand and back: %v", *walked)
	}
	for _, want := range []string{"WARNING: gate deaf: 2 carrier(s) waiting with a load, no request line in", "nudging him", "gate deaf: nudge: closed the open modal main/side/chat -1/-1/4242",
		"forgot the request lines read so far", "stepped to ", "(arrived true) and back (arrived true)"} {
		if !r.logged(want) {
			t.Fatalf("no log line with %q: %v", want, r.logs)
		}
	}

	// Nudged a second ago and a long quiet: not ended yet, the nudge is not repeated.
	r.logs = nil
	quietFor(w, 7*time.Minute)
	w.deaf.nudged, w.deaf.nudgedAt, w.deaf.notedAt = true, time.Now().Add(-time.Second), time.Now()
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if w.deaf.endWhy != "" || len(*walked) != 2 {
		t.Fatalf("ended or nudged again with the nudge a second old: %q walked %v", w.deaf.endWhy, *walked)
	}

	// The nudge has had its time: the round is latched to end.
	quietFor(w, 7*time.Minute)
	w.deaf.nudged, w.deaf.nudgedAt, w.deaf.notedAt = true, time.Now().Add(-3*time.Minute), time.Time{}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if !strings.HasPrefix(w.deaf.endWhy, "nobody could ask the gate: 2 carrier(s) waiting with a load, no request line in 7m0s") || !strings.Contains(w.deaf.endWhy, "ending the round") {
		t.Fatalf("endWhy %q", w.deaf.endWhy)
	}
	if !r.logged("WARNING: gate deaf: 2 carrier(s) waiting with a load, no request line in 7m0s, and he is on the stand, and the nudge 3m0s ago did not bring one: ending the round") {
		t.Fatalf("log %v", r.logs)
	}
	// Gate() now fails at once, before it answers anybody, and keeps failing; a trade restarts the ladder but does not undo the latch.
	for i := 0; i < 3; i++ {
		if res := w.Gate(); res.OK || !strings.Contains(res.Why, "nobody could ask the gate") {
			t.Fatalf("Gate() %d: %+v", i, res)
		}
	}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived})
	if w.deaf.endWhy == "" || w.deaf.nudged {
		t.Fatalf("a trade must restart the ladder (nudged %v) and keep the latched end (%q)", w.deaf.nudged, w.deaf.endWhy)
	}
}

// The latch ends the round through the loop's own failure path: three failures of the trading stage in a row, a passing failure, and
// the reason is the deaf gate's.
func TestADeafGateEndsTheRoundThroughRunGateLoop(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w := gateWorldOn(t, r, natureGatePack(t))
	w.deaf.endWhy = "nobody could ask the gate: 2 carrier(s) waiting with a load, no request line in 7m0s"
	var lines []string
	res := runGateLoop(noPause{w}, func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }, 50)
	if !res.Transient || res.Done || res.Stuck || !strings.Contains(res.Reason, "trade failed 3 times in a row: nobody could ask the gate") {
		t.Fatalf("%+v", res)
	}
	if res.Steps != 3 {
		t.Fatalf("%d steps, want the three failures only", res.Steps)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "WARNING: gate trade failed (1 in a row): nobody could ask the gate") {
		t.Fatalf("lines %v", lines)
	}
}

// noPause is the live gate world without the three-second pauses between failed steps, reading the state of a crafter standing outside
// the temple with his talisman and nothing to craft: the trading stage.
type noPause struct{ *masterNatureWorld }

func (noPause) Pause(time.Duration) {}
func (noPause) Prepare() bool       { return true }
func (noPause) Read() runecraftplan.GateSnapshot {
	return runecraftplan.GateSnapshot{Level: 60, Members: true, Where: runecraftplan.RegionOutside, Talisman: true}
}

// A gate that is off the stand and past its corrective walks climbs the same ladder, without the step off and back (he is not on the
// stand to step from).
func TestADeafCrafterOffTheStandAfterTheWalksIsNudgedAndEnded(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(2864, 3020)
	w := gateWorldOn(t, r, natureGatePack(t))
	walked := stubWalks(t, r)
	publishWaiting(r, "acct1")
	quietFor(w, 4*time.Minute)
	w.deaf.walks = runecraftplan.GateDeafWalks
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if !w.deaf.nudged || len(*walked) != 0 {
		t.Fatalf("nudged %v, walked %v, log %v", w.deaf.nudged, *walked, r.logs)
	}
	if !r.logged("2 walk(s) onto the stand did not bring one: nudging him") || !r.logged("gate deaf: nudge: closed the open modal main/side/chat") || !r.logged("forgot the request lines read so far; now at 2864,3020,0") {
		t.Fatalf("log %v", r.logs)
	}
	quietFor(w, 7*time.Minute)
	w.deaf.walks, w.deaf.nudged, w.deaf.nudgedAt, w.deaf.notedAt = runecraftplan.GateDeafWalks, true, time.Now().Add(-3*time.Minute), time.Time{}
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if !strings.Contains(w.deaf.endWhy, "2 walk(s) onto the stand did not bring one") {
		t.Fatalf("endWhy %q, log %v", w.deaf.endWhy, r.logs)
	}
}

// A quiet gate that nobody waits at never climbs the ladder, however long it stands; carriers that are merely in transit do not count.
func TestAQuietGateWithNobodyWaitingNeverEndsItsRound(t *testing.T) {
	r := newNatureRig(t, gatePackStacks(), nil, nil)
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w := gateWorldOn(t, r, natureGatePack(t))
	walked := stubWalks(t, r)
	r.ctx.Bus.Publish(mulebus.CarrierMessage("acct1", "main", runecraftplan.GateWaitTile, mulebus.CarrierTransit, 25))
	quietFor(w, 10*time.Hour)
	w.deaf.nudged, w.deaf.nudgedAt = true, time.Now().Add(-time.Hour)
	w.gateWatch(runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing})
	if w.deaf.endWhy != "" || len(*walked) != 0 || r.logged("gate deaf") {
		t.Fatalf("endWhy %q walked %v log %v", w.deaf.endWhy, *walked, r.logs)
	}
	if res := w.Gate(); !res.OK {
		t.Fatalf("Gate() failed with nobody waiting: %+v", res)
	}
}
