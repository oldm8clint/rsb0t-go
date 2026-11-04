package randomevents

import (
	"encoding/json"
	"math/rand"
	"os"
	"testing"
	"time"

	"rsb0t-go/internal/doorescape"
)

func castleCatalog(t *testing.T) *doorescape.Catalog {
	t.Helper()
	cat, err := doorescape.Castle()
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// goldenDoorStates are the door states the Engine-TS reference vector was generated over
// (castle_collision_reference_vector.ts): all shut, all open, each door alone, every pair, 120 at random, with
// leaves both there or both gone among them.
func goldenDoorStates(t *testing.T, cat *doorescape.Catalog) (states []doorescape.State, leaves []string) {
	t.Helper()
	b, err := os.ReadFile("../../test/fixtures/castle_collision_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct{ States []struct{ Leaves string } }
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	for _, st := range g.States {
		if len(st.Leaves) != 2*len(cat.Doors) {
			t.Fatalf("golden leaves %q are not two per door (%d doors)", st.Leaves, len(cat.Doors))
		}
		var s doorescape.State
		for i := range cat.Doors {
			s = s.With(i, st.Leaves[2*i] == '1', st.Leaves[2*i+1] == '1')
		}
		states = append(states, s)
		leaves = append(leaves, st.Leaves)
	}
	return states, leaves
}

// quietCastle is the tick budget of an escape in a castle nobody else uses, and the least an account on open
// ground inside the walls is left alone for: escapeStayLimit of 45 seconds, in 600 ms ticks.
const (
	stayTicks     = 75
	interiorTicks = 80 // the slowest way out of a room or a floor, the roof's three staircases and the door
	openTicks     = stayTicks + 30
)

// From every tile of the castle on every level, with the ladder driving the real escape rung against the engine's
// own rules in a closed loop (the account's tile, the doors' leaves, the walk queue and the pending loc op at 600 ms
// a tick, the client seeing the doors one tick late), the account gets out: no door is clicked twice in a castle
// nobody else uses, none is ever shut, no click is ever refused as unreachable, and the room's way out is found
// in a bounded number of ticks. The tiles with no way out at all are the sealed pockets: the ladder says so on
// its first poll and clicks nothing.
func TestEveryTileOfTheCastleEscapesInTheClosedLoop(t *testing.T) {
	cat := castleCatalog(t)
	var interiorMax, openMax, opsMax, driven, sealed int
	for _, tl := range enterableTiles(cat, cat.DefaultState) {
		w := newCastleWorld(t, tl, cat.DefaultState)
		r := driveEscape(t, w, 1500, nil)
		if !cat.Plan(tl, doorescape.Options{State: cat.DefaultState}).OK {
			sealed++
			if !r.gaveUp || r.acted != 0 || r.ticks > 1 || w.opClicks != 0 {
				t.Errorf("sealed tile %v: gaveUp %v after %d ticks with %d actions and %d clicks, want an immediate give-up with none", tl, r.gaveUp, r.ticks, r.acted, w.opClicks)
			}
			if r.ladder.Active(w.now()) {
				t.Errorf("sealed tile %v: the ladder still holds the scripts after giving up", tl)
			}
			continue
		}
		driven++
		if !r.escaped || r.gaveUp {
			t.Errorf("tile %v: not out after %d ticks (gaveUp %v) at %v, %d ops, %d walk clicks, %d op clicks", tl, r.ticks, r.gaveUp, w.pos, w.opsDone, w.walkClicks, w.opClicks)
			continue
		}
		if cat.Interior(tl.X, tl.Z, tl.Level) {
			if r.ticks > interiorMax {
				interiorMax = r.ticks
			}
			if r.ticks > interiorTicks {
				t.Errorf("tile %v: %d ticks to get out of a room, want at most %d", tl, r.ticks, interiorTicks)
			}
		} else {
			if r.ticks > openMax {
				openMax = r.ticks
			}
			if r.firstAct < stayTicks || r.ticks > openTicks {
				t.Errorf("open ground %v: first acted at tick %d and out at tick %d, want left alone for %d ticks and out within %d", tl, r.firstAct, r.ticks, stayTicks, openTicks)
			}
		}
		if w.opsDone > opsMax {
			opsMax = w.opsDone
		}
		if w.cantReach != 0 || w.closes != 0 || w.pushes != 0 {
			t.Errorf("tile %v: %d refused clicks, %d doors shut, %d pushes in a quiet castle, want none", tl, w.cantReach, w.closes, w.pushes)
		}
		for door, n := range w.doorClicks {
			if n > 1 {
				t.Errorf("tile %v: the %s was clicked %d times in a quiet castle", tl, cat.Doors[door].Name, n)
			}
		}
	}
	if sealed != 45 || driven < 380 {
		t.Errorf("%d tiles driven and %d sealed, want at least 380 and exactly 45 (doorescape's list of sealed pockets)", driven, sealed)
	}
	if opsMax > 4 {
		t.Errorf("an escape took %d loc ops, the roof's three staircases and the main door make four", opsMax)
	}
	t.Logf("%d tiles driven out, %d sealed pockets given up on at once; slowest room %d ticks, slowest open-ground escort %d ticks, most loc ops %d", driven, sealed, interiorMax, openMax, opsMax)
}

// The same from every door state the golden file holds, sampled across them (every account in a different state of
// the castle, with the leaves both there or both gone among them), with other accounts using the doors at random
// all the while: every tile that has a plan in its state gets out, in a bounded time, with the client a tick or two
// behind the server.
func TestEveryTileEscapesFromAnyDoorStateWhileOthersUseTheDoors(t *testing.T) {
	cat := castleCatalog(t)
	states, leaves := goldenDoorStates(t, cat)
	tiles := enterableTiles(cat, cat.DefaultState)
	rng := rand.New(rand.NewSource(7))
	runs, closes, noPlan, maxTicks := 0, 0, 0, 0
	for n, s := range states {
		if n%7 != 0 {
			continue
		}
		for k, tl := range tiles {
			if k%5 != n%5 {
				continue
			}
			if !cat.Plan(tl, doorescape.Options{State: s}).OK {
				// Whatever the doors are doing, a tile with no way out is one of the sealed pockets.
				if cat.Plan(tl, doorescape.Options{State: cat.DefaultState}).OK {
					t.Errorf("door state %d (%s), tile %v: no plan, though a quiet castle has one", n, leaves[n], tl)
				}
				noPlan++
				continue
			}
			w := newCastleWorld(t, tl, s)
			w.lag = 1 + n%3
			r := driveEscape(t, w, 3000, func(int) {
				if rng.Intn(40) == 0 {
					w.toggle(rng.Intn(len(cat.Doors)))
				}
			})
			runs++
			closes += w.closes
			if !r.escaped {
				t.Errorf("door state %d (%s), tile %v: not out after %d ticks (gaveUp %v) at %v, %d ops, %d op clicks, %d refused", n, leaves[n], tl, r.ticks, r.gaveUp, w.pos, w.opsDone, w.opClicks, w.cantReach)
				continue
			}
			if r.ticks > maxTicks {
				maxTicks = r.ticks
			}
			// A leaf that walls an account in is waited out (escapeTrapWait, 600 ticks) before a door is shut, and
			// another account using the doors can start that wait again.
			if r.ticks > 1500 {
				t.Errorf("door state %d, tile %v: %d ticks to get out, want a trap wait or two at most with the doors in use", n, tl, r.ticks)
			}
		}
	}
	if runs < 1500 {
		t.Errorf("only %d runs: the sample is not what the test thinks", runs)
	}

	t.Logf("%d runs out of %d door states, all out, slowest %d ticks; %d doors shut on the way; %d sealed (state, tile) pairs skipped", runs, (len(states)+6)/7, maxTicks, closes, noPlan)
}

// A door that never opens (a locked gate, a quest door: the click lands and nothing changes) is tried from each stand
// it can be tried from, then routed around, and the account is out through another door; when every door of the
// castle is like that the ladder gives up the visit boundedly, after five rounds and at most nine failed clicks a
// round (the sleeps between them are the long part), and holds the scripts no more once it has.
func TestALockedDoorIsTriedThenRoutedAroundAndAllLockedGivesUpBoundedly(t *testing.T) {
	cat := castleCatalog(t)
	stairsRoom := doorescape.Tile{X: 3281, Z: 3935, Level: 0}
	main := doorByName(t, cat, "main door (diagonal)")

	w := newCastleWorld(t, stairsRoom, cat.DefaultState)
	w.locked[main] = true
	r := driveEscape(t, w, 1500, nil)
	if !r.escaped || r.gaveUp {
		t.Fatalf("with the main door locked: not out after %d ticks (gaveUp %v) at %v", r.ticks, r.gaveUp, w.pos)
	}
	if n := w.doorClicks[main]; n < 1 || n > 3 {
		t.Errorf("the locked main door was clicked %d times, want it tried from a stand or two and then given up on", n)
	}
	if w.opsDone-w.doorClicks[main] < 2 {
		t.Errorf("the account got out with %d ops beyond the locked door, want it routed through other doors", w.opsDone-w.doorClicks[main])
	}
	if r.ticks > 400 {
		t.Errorf("out after %d ticks with the main door locked, want a few hundred at most", r.ticks)
	}
	t.Logf("main door locked: out in %d ticks, %d clicks on the locked door, %d ops in all", r.ticks, w.doorClicks[main], w.opsDone)

	all := newCastleWorld(t, stairsRoom, cat.DefaultState)
	for i := range cat.Doors {
		all.locked[i] = true
	}
	r = driveEscape(t, all, 4000, nil)
	if r.escaped || !r.gaveUp {
		t.Fatalf("with every door locked: escaped %v, gaveUp %v after %d ticks, want the ladder to say it is stranded", r.escaped, r.gaveUp, r.ticks)
	}
	// Five rounds of at most nine failed clicks each (a few more may be in flight when a round ends).
	if limit := (len(strandBackoff)+1)*strandClicksPerRound + strandClicksPerRound; all.opsDone > limit {
		t.Errorf("%d clicks reached doors before the ladder said it was stranded, want at most %d", all.opsDone, limit)
	}
	// The sleeps between the rounds are most of the time; a failed click takes a walk and strandClickEvery (under
	// eight seconds); a tick is 0.6 s.
	var sleeps int64
	for _, d := range strandBackoff {
		sleeps += int64(d.Seconds())
	}
	if limit := int((sleeps + int64(5*strandClicksPerRound*8)) * 10 / 6); r.ticks > limit {
		t.Errorf("said it was stranded after %d ticks, want at most %d", r.ticks, limit)
	}
	if r.ladder.Active(all.now()) {
		t.Error("the ladder still holds the scripts after saying it is stranded")
	}
	first, firstOps := r.ticks, all.opsDone
	t.Logf("every door locked: stranded after %d ticks and %d clicks", first, firstOps)

	// It does not stop for good: a way out exists in the model, so an hour on it has looked again, boundedly.
	long := newCastleWorld(t, stairsRoom, cat.DefaultState)
	for i := range cat.Doors {
		long.locked[i] = true
	}
	r = driveEscapeThrough(t, long, 6000, nil, false)
	if r.escaped || r.giveUps < 2 {
		t.Errorf("an hour with every door locked: escaped %v with %d give-ups, want it still trying after the first", r.escaped, r.giveUps)
	}
	if limit := 3 * ((len(strandBackoff)+1)*strandClicksPerRound + strandClicksPerRound); long.opsDone > limit {
		t.Errorf("%d clicks reached doors in an hour with every door locked, want at most %d", long.opsDone, limit)
	}
	t.Logf("an hour with every door locked: %d give-ups, %d clicks in all", r.giveUps, long.opsDone)
}

// trappedTile finds a door that, open on its own, leaves an account on some tile with no way out that shuts no door:
// the leaf walls the account in.
func trappedTile(t *testing.T, cat *doorescape.Catalog) (door int, tile doorescape.Tile) {
	t.Helper()
	for i := range cat.Doors {
		s := cat.DefaultState.With(i, false, true)
		for _, tl := range enterableTiles(cat, s) {
			if !cat.Plan(tl, doorescape.Options{State: s, NoClose: true}).OK && cat.Plan(tl, doorescape.Options{State: s}).OK {
				return i, tl
			}
		}
	}
	t.Fatal("no tile of the castle is walled in by a single open leaf: this test no longer proves the policy")
	return 0, doorescape.Tile{}
}

// No door is shut while waiting would do. An open leaf that walls an account in (here the iron cell door's, with
// the account in the cell) goes back by itself five minutes after it was opened: the account does nothing, with the
// scripts let go, and walks out when it has. A leaf that something keeps open is shut, once, only after the trap
// wait, and the account is out.
func TestNoDoorIsShutWhileWaitingWouldDo(t *testing.T) {
	cat := castleCatalog(t)
	door, tile := trappedTile(t, cat)
	open := cat.DefaultState.With(door, false, true)

	// The leaf reverts at tick 500, as the engine's despawn timer does.
	w := newCastleWorld(t, tile, open)
	w.revertAt[door] = 500
	r := driveEscape(t, w, 1500, nil)
	if !r.escaped || r.gaveUp {
		t.Fatalf("%s open, account at %v: not out after %d ticks (gaveUp %v) at %v", cat.Doors[door].Name, tile, r.ticks, r.gaveUp, w.pos)
	}
	// Once the leaf is back the room is shut like any other, and the way out is its doors; none is shut.
	if w.closes != 0 || r.firstAct < 500 {
		t.Errorf("the leaf went back by itself at tick 500, yet %d doors were shut and the first walk or click was at tick %d", w.closes, r.firstAct)
	}
	if r.ticks > 500+120 {
		t.Errorf("out at tick %d, want soon after the leaf went back at tick 500", r.ticks)
	}
	if r.busy > r.ticks-r.firstAct+1 {
		t.Errorf("the scripts were held for %d polls, but the ladder only worked the last %d", r.busy, r.ticks-r.firstAct+1)
	}
	t.Logf("%s open, account at %v: waited, leaf reverted at 500, first acted at tick %d, out at tick %d", cat.Doors[door].Name, tile, r.firstAct, r.ticks)

	// The leaf is kept open: shut after the wait, and not before.
	w = newCastleWorld(t, tile, open)
	r = driveEscape(t, w, 3000, nil)
	if !r.escaped || r.gaveUp {
		t.Fatalf("a kept-open leaf: not out after %d ticks (gaveUp %v) at %v", r.ticks, r.gaveUp, w.pos)
	}
	trapTicks := int(escapeTrapWait / (600 * time.Millisecond))
	if w.closes != 1 || r.firstAct < trapTicks || r.ticks > trapTicks+120 {
		t.Errorf("a kept-open leaf: %d doors shut, first act at tick %d, out at tick %d; want one door shut no earlier than tick %d and the account out soon after", w.closes, r.firstAct, r.ticks, trapTicks)
	}
	t.Logf("a kept-open leaf: shut once at tick %d, out at tick %d", r.firstAct, r.ticks)
}

// The state a cohort can leave behind: the main door opened by the first accounts out and the east chamber's south door
// opened by an account that went east. Together their leaves wall 83 tiles of the ground floor in (the engine model
// says so; no log line does), and the way out is to wait: the engine puts both leaves back within five minutes, and no
// account shuts a door meanwhile.
func TestTheMainDoorAndTheEastChamberDoorOpenTogetherAreWaitedOut(t *testing.T) {
	cat := castleCatalog(t)
	main := doorByName(t, cat, "main door (diagonal)")
	chamber := doorByName(t, cat, "east chamber south door")
	open := cat.DefaultState.With(main, false, true).With(chamber, false, true)
	var trapped []doorescape.Tile
	for _, tl := range enterableTiles(cat, open) {
		if !cat.Plan(tl, doorescape.Options{State: open, NoClose: true}).OK && cat.Plan(tl, doorescape.Options{State: open}).OK {
			trapped = append(trapped, tl)
		}
	}
	if len(trapped) != 83 {
		t.Fatalf("%d tiles are walled in by the two open leaves, want the 83 the model reported", len(trapped))
	}
	driven := 0
	for k, tl := range trapped {
		if k%8 != 0 {
			continue
		}
		w := newCastleWorld(t, tl, open)
		w.revertAt[main], w.revertAt[chamber] = 500, 540
		r := driveEscape(t, w, 1500, nil)
		driven++
		if !r.escaped || r.gaveUp {
			t.Errorf("tile %v: not out after %d ticks (gaveUp %v) at %v", tl, r.ticks, r.gaveUp, w.pos)
			continue
		}
		if w.closes != 0 || r.firstAct < 500 || r.ticks > 500+200 {
			t.Errorf("tile %v: %d doors shut, first acted at tick %d, out at tick %d; want none shut, nothing done before the first leaf went back at tick 500, and out soon after", tl, w.closes, r.firstAct, r.ticks)
		}
	}
	t.Logf("%d of the %d walled-in tiles driven: all waited for the leaves and walked out, no door shut", driven, len(trapped))
}

func doorByName(t *testing.T, cat *doorescape.Catalog, name string) int {
	t.Helper()
	for i, d := range cat.Doors {
		if d.Name == name {
			return i
		}
	}
	t.Fatalf("no door named %q", name)
	return -1
}

// An account on open ground inside the compound's walls (a courtyard) can walk out by itself: for escapeStayLimit it
// is left alone, not held, with the scripts running; past that it is escorted out by a plan that uses no door.
func TestAnAccountOnOpenGroundInsideTheWallsIsLeftAloneThenEscorted(t *testing.T) {
	cat := castleCatalog(t)
	for _, tl := range []doorescape.Tile{{X: 3277, Z: 3937, Level: 0}, {X: 3297, Z: 3935, Level: 0}, {X: 3276, Z: 3934, Level: 0}} {
		if cat.Interior(tl.X, tl.Z, tl.Level) || cat.Free(tl.X, tl.Z, tl.Level) || !cat.Inside(tl.X, tl.Z, tl.Level) {
			t.Fatalf("%v is not open ground inside the walls: this test picked the wrong tile", tl)
		}
		w := newCastleWorld(t, tl, cat.DefaultState)
		r := driveEscape(t, w, 600, nil)
		if !r.escaped || r.gaveUp {
			t.Errorf("%v: not out after %d ticks (gaveUp %v) at %v", tl, r.ticks, r.gaveUp, w.pos)
			continue
		}
		if r.firstAct < stayTicks {
			t.Errorf("%v: the ladder acted at tick %d, before the %d-tick stay limit", tl, r.firstAct, stayTicks)
		}
		if r.busy > r.ticks-r.firstAct+1 {
			t.Errorf("%v: the scripts were held for %d polls but the escort took only %d", tl, r.busy, r.ticks-r.firstAct+1)
		}
		if w.opsDone != 0 || w.closes != 0 {
			t.Errorf("%v: an escort across open ground used %d loc ops", tl, w.opsDone)
		}
	}
}

// Door leaves that are simply gone (the closed and the open leaf both deleted, as between a despawn and a respawn)
// are no obstacle and no click target: the ladder walks, and clicks nothing.
func TestACastleWithNoDoorLeavesAtAllIsWalkedOut(t *testing.T) {
	var none doorescape.State // no leaf of any door: the map's doors are simply not there
	for _, tl := range []doorescape.Tile{{X: 3282, Z: 3936, Level: 3}, {X: 3281, Z: 3935, Level: 0}, {X: 3285, Z: 3930, Level: 0}} {
		w := newCastleWorld(t, tl, none)
		r := driveEscape(t, w, 600, nil)
		if !r.escaped {
			t.Errorf("%v: not out after %d ticks with every door gone, at %v", tl, r.ticks, w.pos)
		}
		if w.doorClicks[0]+w.doorClicks[1]+w.doorClicks[2] != 0 {
			t.Errorf("%v: clicked a door that is not there", tl)
		}
	}
}

// The control: what the castle's ladder did before the escape plan, on the same world. The old catalogue owned the
// floors above (x 3277-3294, z 3925-3940) and the ground floor's 15-tile stairs room only, so an account anywhere
// else in the castle had no ladder at all, and for the rest the ladder's fixed steps (walk to a stand tile, click,
// walk through) are driven here against the engine's rules, with the walk to each stand given a perfect
// pathfinder over the doors as they stand (better than the live pack overlay the old code had). The Walker/script
// side of the old trouble (their own door crossings) is not modelled.
func TestControlTheOldLadderLeftTilesWithNoWayOut(t *testing.T) {
	cat := castleCatalog(t)
	old := fixedStepsCastle()
	// The castle as the first account of a cohort finds it, and as the second one does: the door it opened stands
	// open for five minutes (the engine's despawn timer), and every account that comes down behind it meets it so.
	names := []string{"every door shut", "the main door already open"}
	states := map[string]doorescape.State{
		names[0]: cat.DefaultState,
		names[1]: cat.DefaultState.With(doorByName(t, cat, "main door (diagonal)"), false, true),
	}

	tiles := enterableTiles(cat, cat.DefaultState)
	uncovered := 0
	for _, tl := range tiles {
		if !old.Contains(tl.X, tl.Z, tl.Level) {
			uncovered++
		}
	}
	if uncovered < 150 {
		t.Errorf("the old catalogue left only %d of %d castle tiles outside every site, the fix was for many more than that", uncovered, len(tiles))
	}

	oldStuckTotal, newStuckTotal := 0, 0
	for _, name := range names {
		s := states[name]
		var oldStuck, covered, oldStuckCovered, newStuck, reachable int
		for _, tl := range tiles {
			if !cat.Plan(tl, doorescape.Options{State: s}).OK {
				continue // sealed in every model
			}
			reachable++
			isCovered := old.Contains(tl.X, tl.Z, tl.Level)
			if isCovered {
				covered++
			}
			// An account outside every site has no ladder at all; inside one it has the fixed steps.
			if !isCovered || !driveOldLadder(t, newCastleWorld(t, tl, s), old, 4000) {
				oldStuck++
				if isCovered {
					oldStuckCovered++
				}
			}
			if r := driveEscape(t, newCastleWorld(t, tl, s), 3000, nil); !r.escaped {
				newStuck++
				t.Errorf("%s: the escape plan is stuck at %v", name, tl)
			}
		}
		oldStuckTotal += oldStuck
		newStuckTotal += newStuck
		t.Logf("%-32s %3d tiles with a way out: old ladder stuck on %3d (%3d of the %3d it covered), escape plan stuck on %d", name, reachable, oldStuck, oldStuckCovered, covered, newStuck)
	}
	if oldStuckTotal == 0 || newStuckTotal != 0 {
		t.Errorf("old ladder stuck %d times, escape plan %d: the control no longer shows the old trouble", oldStuckTotal, newStuckTotal)
	}
}

// driveOldLadder runs the fixed-step ladder (the Guardian before the escape plan) against the world and reports
// whether the account got out of the castle. It follows recoverStrand's old rungs: a walk toward the next stand
// tile (a path over the doors as they stand when there is one, otherwise the bare two-point click), a click on the
// step's loc (a door that already stands open is not clicked), a bare two-point walk through the door; an account
// that is outside every site is not the ladder's any more.
func driveOldLadder(t *testing.T, w *castleWorld, site StrandSite, maxTicks int) bool {
	t.Helper()
	var l StrandLadder
	for i := 0; i < maxTicks; i++ {
		w.Tick()
		if w.outside() {
			return true
		}
		o := StrandObs{Now: w.now(), X: w.pos.X, Z: w.pos.Z, Level: w.pos.Level, HP: 30, MaxHP: 30, HPKnown: true}
		if site.Contains(w.pos.X, w.pos.Z, w.pos.Level) {
			s := site
			o.Site = &s
		}
		a := l.Next(o)
		if o.Now.Before(l.sleepUntil) {
			w.sleepThrough(l.sleepUntil)
		}
		switch a.Rung {
		case RungWalk:
			from := w.pos
			stand := doorescape.Tile{X: a.Stand.X, Z: a.Stand.Z, Level: a.Stand.Level}
			if path, ok := bfsPath(w, from, stand); ok {
				w.walk(append([]doorescape.Tile{from}, path...))
			} else {
				w.walk([]doorescape.Tile{from, stand})
			}
		case RungClick:
			st := a.Step
			if st.Kind == StepDoor {
				if i := w.doorAt(st.LocX, st.LocZ); i >= 0 && !w.clientState().ClosedLeaf(i) {
					continue // "the door at x,z is already open": the next poll walks through it
				}
			}
			w.opLoc(st.Op, st.LocX, st.LocZ, st.LocID)
		case RungThrough:
			w.walk([]doorescape.Tile{w.pos, {X: a.Stand.X, Z: a.Stand.Z, Level: a.Stand.Level}})
		case RungGiveUp:
			return false
		}
		if l.GaveUp() {
			return false
		}
	}
	return false
}

// doorAt is the catalogue index of the door whose closed leaf stands at x, z on the account's level, or -1.
func (w *castleWorld) doorAt(x, z int) int {
	for i, d := range w.cat.Doors {
		if d.Level == w.pos.Level && d.X == x && d.Z == z {
			return i
		}
	}
	return -1
}

// bfsPath is a shortest path over the doors as they stand now, the way a perfect pathfinder over the live overlay
// would find one, without the start tile.
func bfsPath(w *castleWorld, from, to doorescape.Tile) ([]doorescape.Tile, bool) {
	if from == to {
		return nil, true
	}
	f := w.flags()
	g := w.cat.Grid
	start := g.Index(from.X, from.Z, from.Level)
	goal := g.Index(to.X, to.Z, to.Level)
	if start < 0 || goal < 0 {
		return nil, false
	}
	// The stands are close to the account; the search stays in a box around the two.
	const margin = 10
	minX, maxX := min(from.X, to.X)-margin, max(from.X, to.X)+margin
	minZ, maxZ := min(from.Z, to.Z)-margin, max(from.Z, to.Z)+margin
	prev := make([]int32, g.Len())
	for i := range prev {
		prev[i] = -2
	}
	prev[start] = -1
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == goal {
			var rev []doorescape.Tile
			for at := cur; at != start; at = int(prev[at]) {
				rev = append(rev, g.TileAt(at))
			}
			out := make([]doorescape.Tile, len(rev))
			for i := range rev {
				out[len(rev)-1-i] = rev[i]
			}
			return out, true
		}
		t := g.TileAt(cur)
		for d := 0; d < 8; d++ {
			if !doorescape.CanStep(f, t.Level, t.X, t.Z, doorescape.DX[d], doorescape.DZ[d]) {
				continue
			}
			nx, nz := t.X+doorescape.DX[d], t.Z+doorescape.DZ[d]
			if nx < minX || nx > maxX || nz < minZ || nz > maxZ {
				continue
			}
			n := g.Index(nx, nz, t.Level)
			if n >= 0 && prev[n] == -2 {
				prev[n] = int32(cur)
				queue = append(queue, n)
			}
		}
	}
	return nil, false
}
