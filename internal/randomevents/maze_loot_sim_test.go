package randomevents

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents/mazegraph"
	"rsb0t-go/internal/randomevents/mazeloot"
)

// setMazeFeatures switches the reward hold and chest looting for one test and restores them (and
// the process-wide MOVE_OPCLICK verdict) after it.
func setMazeFeatures(t *testing.T, stall, loot bool) {
	t.Helper()
	prevS, prevL := mazeStallEnabled, mazeLootEnabled
	prevW, prevR := mazeOpWalkWorked.Load(), mazeOpWalkRefused.Load()
	mazeStallEnabled, mazeLootEnabled = stall, loot
	mazeOpWalkWorked.Store(0)
	mazeOpWalkRefused.Store(0)
	t.Cleanup(func() {
		mazeStallEnabled, mazeLootEnabled = prevS, prevL
		mazeOpWalkWorked.Store(prevW)
		mazeOpWalkRefused.Store(prevR)
	})
}

// newClassicSim is the replay the first tests were written against: the older engine model
// (sim.legacy) and the solver as it was before the reward hold and chest looting.
func newClassicSim(t *testing.T, spawn mazegraph.Cell, run bool) *mazeSim {
	t.Helper()
	setMazeFeatures(t, false, false)
	m := newMazeSim(t, spawn, run)
	m.legacy = true
	return m
}

// useItemNames makes itemName resolve the replay's prize ids, as the live resolver does.
func useItemNames(t *testing.T) {
	t.Helper()
	names := map[int]string{}
	for _, p := range simChestPrizes {
		names[p.id] = p.name
	}
	prev := currentResolver()
	SetNameResolver(fakeResolver{objNames: names})
	t.Cleanup(func() { SetNameResolver(prev) })
}

func linesWith(lines []string, sub string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return out
}

// rewardFailureLines are the log lines of a solve that did not go cleanly: a step that failed, a
// door that did not open, a chest that gave nothing. ("stalled N ticks at P%" is the reward hold
// reporting, not a failure.)
func rewardFailureLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		for _, bad := range []string{"could not stand", "walk leg", "no open-floor", "no reaction", "refused", "re-routing", "gave nothing", "did not move", "put up no dialogue", "no crossing", "send error"} {
			if strings.Contains(l, bad) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// rewardRun is one replayed solve's outcome.
type rewardRun struct {
	err     error
	lines   []string
	touched time.Duration
	sim     *mazeSim
}

func (r rewardRun) log() string { return strings.Join(r.lines, "\n") }

// runReward replays one solve on the reference engine with the given features on. mod adjusts the
// simulator before the solve (engine variant, a chest already emptied ...).
func runReward(t *testing.T, sp mazegraph.Cell, run bool, lat time.Duration, jitterSeed uint64, mod func(*mazeSim), stall, loot bool) rewardRun {
	t.Helper()
	useItemNames(t)
	setMazeFeatures(t, stall, loot)
	m := newMazeSim(t, sp, run)
	if mod != nil {
		mod(m)
	}
	m.feedPack() // a mod may have changed what the backpack holds; the client has it before the maze
	m.latency = lat
	if jitterSeed != 0 {
		m.jitter = rand.New(rand.NewPCG(jitterSeed, uint64(lat)))
	}
	err, lines, touched := m.solve()
	return rewardRun{err: err, lines: lines, touched: touched, sim: m}
}

func (r rewardRun) must(t *testing.T) {
	t.Helper()
	if r.err != nil {
		t.Fatalf("solve failed: %v\n%s", r.err, r.log())
	}
	if !r.sim.out {
		t.Fatalf("never left the maze\n%s", r.log())
	}
}

// TestMazeSimHoldsTheRewardFromEverySpawn is the claim of the whole change: on the reference
// engine, walking each leg with a dialogue up leaves one point lost per door, where the classic
// solver loses one per five ticks of walking. Replayed from every spawn, walking and running,
// over a spread of one-way latencies.
func TestMazeSimHoldsTheRewardFromEverySpawn(t *testing.T) {
	for _, lat := range []time.Duration{0, 120 * time.Millisecond} {
		for _, run := range []bool{true} {
			for _, sp := range mazegraph.Spawns {
				t.Run(fmt.Sprintf("%v_run=%v_lat=%v", sp, run, lat), func(t *testing.T) {
					classic := runReward(t, sp, run, lat, 0, nil, false, false)
					classic.must(t)
					held := runReward(t, sp, run, lat, 0, nil, true, false)
					held.must(t)
					if f := rewardFailureLines(held.lines); len(f) > 0 {
						t.Errorf("failed steps:\n%s\n%s", strings.Join(f, "\n"), held.log())
					}
					doors := len(held.sim.crossings)
					// One decrement per door window, plus at most two for the first leg and the statue.
					if got := len(held.sim.fires); got > doors+2 {
						t.Errorf("%d decrements for %d doors", got, doors)
					}
					if held.sim.rewardPct < classic.sim.rewardPct+4 {
						t.Errorf("held reward %d%% against the classic %d%%: the hold gained under 4 points\n%s",
							held.sim.rewardPct, classic.sim.rewardPct, held.log())
					}
					if held.sim.opClicksIn == 0 {
						t.Error("no MOVE_OPCLICK walk was sent")
					}
					t.Logf("classic %d%% in %.0fs (%d decrements), held %d%% in %.0fs (%d decrements, %d doors)",
						classic.sim.rewardPct, classic.touched.Seconds(), len(classic.sim.fires),
						held.sim.rewardPct, held.touched.Seconds(), len(held.sim.fires), doors)
				})
			}
		}
	}
}

// With chests on as well, the three chests in the rooms the route passes through are all opened,
// each costing about a point.
func TestMazeSimLootsTheChestsOnTheRoute(t *testing.T) {
	for _, lat := range []time.Duration{0, 120 * time.Millisecond} {
		for _, sp := range mazegraph.Spawns {
			t.Run(fmt.Sprintf("%v_lat=%v", sp, lat), func(t *testing.T) {
				r := runReward(t, sp, true, lat, 0, nil, true, true)
				r.must(t)
				if f := rewardFailureLines(r.lines); len(f) > 0 {
					t.Errorf("failed steps:\n%s\n%s", strings.Join(f, "\n"), r.log())
				}
				if got := len(r.sim.prizes); got != 3 {
					t.Errorf("%d prizes, want 3\n%s", got, r.log())
				}
				looted := linesWith(r.lines, "looted chest at")
				if len(looted) != 3 {
					t.Errorf("%d 'looted chest' lines, want 3: %v", len(looted), looted)
				}
				for _, l := range looted {
					if !strings.Contains(l, " x ") || !strings.Contains(l, "reward ") {
						t.Errorf("a looted line should name the prize and the reward: %q", l)
					}
				}
				doors := len(r.sim.crossings)
				if got := len(r.sim.fires); got > doors+3+2 {
					t.Errorf("%d decrements for %d doors and 3 chests", got, doors)
				}
				if r.sim.rewardPct < 70 {
					t.Errorf("reward %d%% after looting three chests; the hold should keep it above 70\n%s", r.sim.rewardPct, r.log())
				}
				t.Logf("reward %d%%, %d decrements, %d doors, %d prizes in %.0fs", r.sim.rewardPct, len(r.sim.fires), doors, len(r.sim.prizes), r.touched.Seconds())
			})
		}
	}
}

// actionsWalkTowardsHome is the account's walker clicking toward home, south-west of the maze.
func actionsWalkTowardsHome(m *mazeSim) error {
	return actions.Walk(m, []pathfind.NavPoint{{X: m.pos.X, Z: m.pos.Z}, {X: m.pos.X - 9, Z: m.pos.Z - 9}}, true)
}

// actionsWalkTo is one ordinary walk to a tile, sent as the solver sends it.
func actionsWalkTo(m *mazeSim, c mazegraph.Cell) error {
	return actions.Walk(m, []pathfind.NavPoint{{X: c.X, Z: c.Z}}, true)
}

// actionsClickOwnWall is the walker "opening" the shut wall it stalled against.
func actionsClickOwnWall(m *mazeSim) error {
	return actions.OpLoc(m, 1, m.pos.X, m.pos.Z, mazegraph.WallID)
}

// The same replays on a server whose ticks land up to 250ms late, over many seeds: the timing the
// clean replays never hit. The solve must still succeed, hold the reward at about a point a door,
// and never fall behind the classic solver run through the same jitter.
func TestMazeSimHoldsTheRewardWithLateTicks(t *testing.T) {
	for seed := uint64(1); seed <= 2; seed++ {
		for _, lat := range []time.Duration{160 * time.Millisecond} {
			for _, sp := range mazegraph.Spawns {
				t.Run(fmt.Sprintf("%v_seed=%d_lat=%v", sp, seed, lat), func(t *testing.T) {
					classic := runReward(t, sp, seed%2 == 0, lat, seed, nil, false, false)
					classic.must(t)
					r := runReward(t, sp, seed%2 == 0, lat, seed, nil, true, true)
					r.must(t)
					if f := rewardFailureLines(r.lines); len(f) > 0 {
						t.Errorf("failed steps:\n%s\n%s", strings.Join(f, "\n"), r.log())
					}
					if len(r.sim.prizes) != 3 {
						t.Errorf("%d prizes, want 3\n%s", len(r.sim.prizes), r.log())
					}
					if r.sim.rewardPct+4 < classic.sim.rewardPct {
						t.Errorf("hold+loot %d%% against the classic %d%%\n%s", r.sim.rewardPct, classic.sim.rewardPct, r.log())
					}
					doors := len(r.sim.crossings)
					if got := len(r.sim.fires); got > doors+3+3 {
						t.Errorf("%d decrements for %d doors and 3 chests\n%s", got, doors, r.log())
					}
				})
			}
		}
	}
}

// A server that does not honour a walk with a dialogue up -- here the reference engine with
// MOVE_OPCLICK dropped, and the older model, where the dialogue blocks every click and the packet
// is unknown -- must not strand or slow the account: the first walk that does not move it ends the
// hold, and the rest of the solve is the classic one.
func TestMazeSimFallsBackWhenTheWalkIsRefused(t *testing.T) {
	variants := map[string]func(*mazeSim){
		"opclick dropped": func(m *mazeSim) { m.refuseOpWalk = true },
		"older model":     func(m *mazeSim) { m.legacy = true },
	}
	for name, mod := range variants {
		for _, sp := range mazegraph.Spawns {
			t.Run(fmt.Sprintf("%s/%v", name, sp), func(t *testing.T) {
				classic := runReward(t, sp, true, 40*time.Millisecond, 0, func(m *mazeSim) { m.legacy = true }, false, false)
				classic.must(t)
				r := runReward(t, sp, true, 40*time.Millisecond, 0, mod, true, true)
				r.must(t)
				if len(linesWith(r.lines, "did not move the account")) == 0 {
					t.Errorf("no line says the walk with the dialogue up was refused\n%s", r.log())
				}
				if r.sim.opClicksIn != 0 {
					t.Errorf("the server acted on %d MOVE_OPCLICKs it should have dropped", r.sim.opClicksIn)
				}
				if r.sim.opClicksOut > 2 {
					t.Errorf("%d MOVE_OPCLICKs were dropped before the solver gave up on them; one or two is the bound", r.sim.opClicksOut)
				}
				// The refused hold costs ticks the timer was running and, with the hold gone, chests
				// within mazeNoHoldDetour; it must not cost more than that.
				if r.sim.rewardPct+8 < classic.sim.rewardPct {
					t.Errorf("fallback reward %d%% against the classic %d%%\n%s", r.sim.rewardPct, classic.sim.rewardPct, r.log())
				}
				t.Logf("%s: classic %d%% in %.0fs, fallback %d%% in %.0fs (%d prizes)",
					name, classic.sim.rewardPct, classic.touched.Seconds(), r.sim.rewardPct, r.touched.Seconds(), len(r.sim.prizes))
			})
		}
	}
}

// Three accounts in a row see their MOVE_OPCLICK walk refused with none ever answered: the
// process stops trying, so the fourth never clicks a wall and never sends one.
func TestMazeSimStopsTryingAfterRepeatedRefusals(t *testing.T) {
	sp := mazegraph.Spawns[0]
	setMazeFeatures(t, true, false)
	useItemNames(t)
	for i := 1; i <= mazeOpWalkGiveUp; i++ {
		m := newMazeSim(t, sp, true)
		m.refuseOpWalk = true
		err, lines, _ := m.solve()
		if err != nil {
			t.Fatalf("solve %d failed: %v\n%s", i, err, strings.Join(lines, "\n"))
		}
		if m.opClicksOut == 0 {
			t.Fatalf("solve %d never tried a walk with the dialogue up", i)
		}
	}
	if opWalkUsable() {
		t.Fatal("opWalkUsable() still true after the refusals")
	}
	m := newMazeSim(t, sp, true)
	m.refuseOpWalk = true
	err, lines, _ := m.solve()
	if err != nil {
		t.Fatalf("solve failed: %v\n%s", err, strings.Join(lines, "\n"))
	}
	if m.opClicksOut != 0 || m.opClicksIn != 0 || m.wallClicks != 0 {
		t.Errorf("after giving up the solver still tried: %d opclicks dropped, %d acted on, %d wall clicks", m.opClicksOut, m.opClicksIn, m.wallClicks)
	}
}

// One answered walk is enough to keep trying for good: refusals after it never switch the
// process off (the server may drop one for an unrelated reason, such as a delayed account).
func TestOpWalkVerdict(t *testing.T) {
	setMazeFeatures(t, true, true)
	if !opWalkUsable() {
		t.Fatal("a fresh process must try")
	}
	mazeOpWalkRefused.Store(mazeOpWalkGiveUp - 1)
	if !opWalkUsable() {
		t.Error("one short of the give-up count must still try")
	}
	mazeOpWalkRefused.Store(mazeOpWalkGiveUp)
	if opWalkUsable() {
		t.Error("the give-up count with no answer must stop")
	}
	mazeOpWalkWorked.Store(1)
	if !opWalkUsable() {
		t.Error("an answered walk keeps the process trying")
	}
}

// Another account in the maze (it is not an instance) has already emptied a chest on the route: its
// loc is no longer the closed chest, the click finds nothing, and the solver gives it up after its
// tries and carries on with the rest.
func TestMazeSimGivesUpAnEmptiedChest(t *testing.T) {
	sp := mazegraph.Spawns[0]
	emptied := mazegraph.Chests[0] // (2890,4599), the first chest on this spawn's route
	r := runReward(t, sp, true, 0, 0, func(m *mazeSim) { m.chestEmpty[emptied] = 1 << 30 }, true, true)
	r.must(t)
	gave := linesWith(r.lines, "gave nothing")
	if len(gave) != mazeChestTries {
		t.Errorf("%d 'gave nothing' lines, want %d:\n%s", len(gave), mazeChestTries, r.log())
	}
	if len(linesWith(r.lines, "looted chest at")) != 2 || len(r.sim.prizes) != 2 {
		t.Errorf("want the other two chests looted, got %d lines and %d prizes\n%s", len(linesWith(r.lines, "looted chest at")), len(r.sim.prizes), r.log())
	}
	if len(r.sim.crossings) != 16 {
		t.Errorf("%d doors crossed, want 16", len(r.sim.crossings))
	}
}

// Below mazeChestMinPct the reward is worth more than the next prize: no more chest detours, but
// the hold and the solve go on.
func TestMazeSimStopsLootingWhenTheRewardIsLow(t *testing.T) {
	r := runReward(t, mazegraph.Spawns[0], true, 0, 0, func(m *mazeSim) { m.pct = mazeChestMinPct - 10 }, true, true)
	r.must(t)
	if len(r.sim.prizes) != 0 {
		t.Errorf("%d chests opened at %d%%\n%s", len(r.sim.prizes), mazeChestMinPct-10, r.log())
	}
	if len(linesWith(r.lines, "not detouring for more chests")) != 1 {
		t.Errorf("want exactly one line saying why\n%s", r.log())
	}
	if len(r.sim.fires) > len(r.sim.crossings)+2 {
		t.Errorf("%d decrements for %d doors: the hold should still be on", len(r.sim.fires), len(r.sim.crossings))
	}
}

// With looting off nothing changes about the chests: none opened, none walked to.
func TestMazeSimLootOffOpensNothing(t *testing.T) {
	r := runReward(t, mazegraph.Spawns[3], true, 0, 0, nil, true, false)
	r.must(t)
	if len(r.sim.prizes) != 0 || len(linesWith(r.lines, "chest at")) != 0 {
		t.Errorf("chest activity with looting off: %d prizes\n%s", len(r.sim.prizes), r.log())
	}
}

// The account's own script walking and clicking walls alongside the solver (the 3 October
// failure) must change nothing about a hold: its clicks are dropped, the dialogue the solver
// holds is not closed by them, and the solve completes.
func TestMazeSimHoldSurvivesTheScriptWalkingAlongside(t *testing.T) {
	for _, sp := range mazegraph.Spawns {
		t.Run(fmt.Sprint(sp), func(t *testing.T) {
			r := runReward(t, sp, true, 0, 0, func(m *mazeSim) {
				m.interfere = func(m *mazeSim) {
					switch m.tick % 6 {
					case 0:
						_ = actionsWalkTowardsHome(m)
					case 3:
						_ = actionsClickOwnWall(m)
					}
				}
			}, true, true)
			r.must(t)
			if r.sim.scriptArrived != 0 {
				t.Errorf("%d of the script's clicks reached the server\n%s", r.sim.scriptArrived, r.log())
			}
			if f := rewardFailureLines(r.lines); len(f) > 0 {
				t.Errorf("failed steps:\n%s\n%s", strings.Join(f, "\n"), r.log())
			}
		})
	}
}

// The client keeps the last text the overlay was given, so the previous maze's final percentage
// is still on screen when the next one starts. It must not be read as this maze's reward: a low
// leftover would stop the chest detours before the timer has even been set.
func TestMazeSimIgnoresTheLastMazesPercentage(t *testing.T) {
	r := runReward(t, mazegraph.Spawns[0], true, 0, 0, func(m *mazeSim) {
		m.feedPctText("31%")
	}, true, true)
	r.must(t)
	if len(r.sim.prizes) != 3 {
		t.Errorf("%d prizes with a stale 31%% on screen, want 3\n%s", len(r.sim.prizes), r.log())
	}
	if len(linesWith(r.lines, "not detouring")) != 0 {
		t.Errorf("a stale percentage stopped the chest detours\n%s", r.log())
	}
}

// The replay's model of the timer, pinned against the engine sources it was written from
// (mazeloot's doc comment): a NORMAL timer does not run while a dialogue is up, stays due, and
// runs once -- not once per missed interval -- when the player can be accessed again.
func TestMazeSimModelsTheNormalTimer(t *testing.T) {
	setMazeFeatures(t, false, false)
	sp := mazegraph.Spawns[3]
	m := newMazeSim(t, sp, true)
	wall, _, ok := mazegraph.BuildLayoutGraph().NearestWall(sp, 3)
	if !ok {
		t.Fatal("no wall near the spawn")
	}
	tickMs := func(n int) { m.Sleep(time.Duration(n) * simTick) }

	// The briefing is up: the mapzone script has not run, so there is no timer at all.
	tickMs(20)
	if m.timerOn || m.pct != 100 {
		t.Fatalf("with the briefing up the timer is on=%v at %d%%", m.timerOn, m.pct)
	}
	// Walk to the wall (a walk closes the briefing on the reference engine) and click it.
	if err := actionsWalkTo(m, wall); err != nil {
		t.Fatal(err)
	}
	tickMs(3)
	if err := actions.OpLoc(m, 1, wall.X, wall.Z, mazegraph.WallID); err != nil {
		t.Fatal(err)
	}
	tickMs(2)
	if !m.modal {
		t.Fatal("the wall click put up no dialogue")
	}
	before := len(m.fires)
	// A long wait with the dialogue up: not one decrement.
	tickMs(60)
	if len(m.fires) != before {
		t.Fatalf("the timer ran %d times with a dialogue up", len(m.fires)-before)
	}
	// Close it by clicking: the overdue timer runs once on that tick, and not again for 5 ticks.
	if err := actions.OpLoc(m, 1, wall.X, wall.Z, mazegraph.WallID); err != nil {
		t.Fatal(err)
	}
	tickMs(1)
	if got := len(m.fires) - before; got != 1 {
		t.Fatalf("%d decrements on release after 60 held ticks, want exactly 1", got)
	}
	// The click that closed the dialogue opened a new one (the wall again): nothing more runs.
	tickMs(30)
	if got := len(m.fires) - before; got != 1 {
		t.Fatalf("%d decrements by the end, want 1", got)
	}
}

// A server that does not answer a click on a maze wall (the loc is not what the layout says): the
// solver notes it, stops trying to hold the reward for that solve and finishes the classic way.
func TestMazeSimWallClickIgnored(t *testing.T) {
	for _, sp := range mazegraph.Spawns {
		t.Run(fmt.Sprint(sp), func(t *testing.T) {
			classic := runReward(t, sp, true, 0, 0, func(m *mazeSim) { m.legacy = true }, false, false)
			classic.must(t)
			r := runReward(t, sp, true, 0, 0, func(m *mazeSim) { m.dropWall = true }, true, false)
			r.must(t)
			if len(linesWith(r.lines, "put up no dialogue")) != 1 {
				t.Errorf("want one line saying the wall click did nothing\n%s", r.log())
			}
			if r.sim.wallClicks > 2 {
				t.Errorf("%d wall clicks sent; two attempts is the bound", r.sim.wallClicks)
			}
			if r.sim.opClicksIn+r.sim.opClicksOut != 0 {
				t.Errorf("MOVE_OPCLICK sent without a dialogue to keep up")
			}
			if r.sim.rewardPct+3 < classic.sim.rewardPct {
				t.Errorf("reward %d%% against the classic %d%%\n%s", r.sim.rewardPct, classic.sim.rewardPct, r.log())
			}
		})
	}
}

// A chest another player opened a moment ago is shown to every client in range as the empty
// chest. The solver sees it and leaves it without a click; the rest of the route's chests are
// looted as usual, and when the 100 ticks are up the chest is a closed one again.
func TestMazeSimSkipsAChestSomeoneElseEmptied(t *testing.T) {
	sp := mazegraph.Spawns[0]
	emptied := mazegraph.Chests[0] // (2890,4599), the first chest on this spawn's route
	r := runReward(t, sp, true, 0, 0, func(m *mazeSim) { m.emptyChest(emptied, true) }, true, true)
	r.must(t)
	if len(linesWith(r.lines, "is empty (someone else")) != 1 {
		t.Errorf("want one line saying the chest was left\n%s", r.log())
	}
	if len(linesWith(r.lines, "gave nothing")) != 0 {
		t.Errorf("a chest seen empty was clicked anyway\n%s", r.log())
	}
	if r.sim.chestClicks != 2 || len(r.sim.prizes) != 2 {
		t.Errorf("%d chest clicks, %d prizes; want the other two chests only\n%s", r.sim.chestClicks, len(r.sim.prizes), r.log())
	}
}

// A chest that was emptied long enough ago to have refilled is looted: the client was told it was
// empty, then told it was closed again, and the second message wins.
func TestMazeSimLootsAChestThatRefilled(t *testing.T) {
	sp := mazegraph.Spawns[0]
	refilled := mazegraph.Chests[0]
	r := runReward(t, sp, true, 0, 0, func(m *mazeSim) {
		m.emptyChest(refilled, true)
		m.chestEmpty[refilled] = 3 // full again from tick 3, before the account gets there
	}, true, true)
	r.must(t)
	if len(r.sim.prizes) != 3 || len(linesWith(r.lines, "is empty")) != 0 {
		t.Errorf("%d prizes; a refilled chest should be looted like any other\n%s", len(r.sim.prizes), r.log())
	}
}

// A server on which MOVE_OPCLICK moves the account but closes the dialogue anyway holds nothing:
// the solver sees the dialogue gone after the walk, stops holding for that solve and does not keep
// clicking walls to open dialogues that every walk closes.
func TestMazeSimHoldLostToTheWalkIsGivenUp(t *testing.T) {
	for _, sp := range mazegraph.Spawns {
		t.Run(fmt.Sprint(sp), func(t *testing.T) {
			classic := runReward(t, sp, true, 0, 0, func(m *mazeSim) { m.legacy = true }, false, false)
			classic.must(t)
			r := runReward(t, sp, true, 0, 0, func(m *mazeSim) { m.opClickCloses = true }, true, false)
			r.must(t)
			if len(linesWith(r.lines, "closed it")) != 1 {
				t.Errorf("want one line saying the walk closed the dialogue\n%s", r.log())
			}
			if r.sim.opClicksIn > 2 || r.sim.wallClicks > 3 {
				t.Errorf("%d MOVE_OPCLICKs and %d wall clicks: the solver kept trying", r.sim.opClicksIn, r.sim.wallClicks)
			}
			if r.sim.rewardPct+3 < classic.sim.rewardPct {
				t.Errorf("reward %d%% against the classic %d%%\n%s", r.sim.rewardPct, classic.sim.rewardPct, r.log())
			}
		})
	}
}

// A full backpack has no room for a chest's prize: the chests would cost the points and give
// nothing, so the solver leaves them (and says so once) but still holds the reward.
func TestMazeSimFullBackpackOpensNoChests(t *testing.T) {
	r := runReward(t, mazegraph.Spawns[0], true, 0, 0, func(m *mazeSim) {
		m.ballast = 28
		m.feedPack()
	}, true, true)
	r.must(t)
	if r.sim.chestClicks != 0 || len(r.sim.prizes) != 0 {
		t.Errorf("%d chest clicks with a full backpack\n%s", r.sim.chestClicks, r.log())
	}
	if len(linesWith(r.lines, "free slot(s)")) != 1 {
		t.Errorf("want exactly one line saying why\n%s", r.log())
	}
	if len(r.sim.fires) > len(r.sim.crossings)+2 {
		t.Errorf("%d decrements for %d doors: the hold should still be on", len(r.sim.fires), len(r.sim.crossings))
	}
}

// registerSimArchive registers the interface archive the live fleet has whenever it fetched it at
// startup (fleet.go StartBot, controlpanel main.go), narrowed to the three dialogues the replay opens, each with its
// "click here to continue" child. With it dismissOnce presses that button (RESUME_PAUSEBUTTON)
// instead of sending CLOSE_MODAL, and the client's own modal state stays open until the server's
// IF_CLOSE arrives -- a path every replay without an archive skips.
func registerSimArchive(t *testing.T) {
	t.Helper()
	comps := archive.Components{}
	for _, id := range []int{simBriefingID, simMesboxID, simObjboxID} {
		comps[id] = &archive.Component{ID: id, Children: []int{id + 1000}}
		comps[id+1000] = &archive.Component{ID: id + 1000, ButtonType: archive.ButtonContinue}
	}
	prev := currentInterfaceArchive()
	SetInterfaceArchive(comps)
	t.Cleanup(func() { SetInterfaceArchive(prev) })
}

// With the interface archive registered the held dialogue is dismissed by its continue button,
// which does not clear the client's modal state: the door click that follows used to find the
// dialogue still "open", log the door as refused, dismiss it again and click a second time (a tick
// or two lost per door, on a door that may already be swinging). The replay must be the same solve
// as one without the archive: no refused door, the same doors, the same reward.
func TestMazeSimHoldWithInterfaceArchive(t *testing.T) {
	for _, lat := range []time.Duration{0, 120 * time.Millisecond} {
		for _, sp := range mazegraph.Spawns {
			t.Run(fmt.Sprintf("%v_lat=%v", sp, lat), func(t *testing.T) {
				plain := runReward(t, sp, true, lat, 0, nil, true, true)
				plain.must(t)
				registerSimArchive(t)
				r := runReward(t, sp, true, lat, 0, nil, true, true)
				r.must(t)
				if f := rewardFailureLines(r.lines); len(f) > 0 {
					t.Errorf("failed steps with the archive registered:\n%s\n%s", strings.Join(f, "\n"), r.log())
				}
				if got, want := len(linesWith(r.lines, "dismissing open modal")), len(linesWith(plain.lines, "dismissing open modal")); got != want {
					t.Errorf("%d 'dismissing open modal' lines with the archive, %d without\n%s", got, want, r.log())
				}
				if got, want := len(r.sim.crossings), len(plain.sim.crossings); got != want {
					t.Errorf("%d door crossings with the archive, %d without", got, want)
				}
				if r.sim.rewardPct != plain.sim.rewardPct || len(r.sim.fires) != len(plain.sim.fires) {
					t.Errorf("reward %d%% (%d decrements) with the archive, %d%% (%d) without\n%s",
						r.sim.rewardPct, len(r.sim.fires), plain.sim.rewardPct, len(plain.sim.fires), r.log())
				}
				if len(r.lines) > len(plain.lines)+2 {
					t.Errorf("%d log lines with the archive against %d without: something is being redone", len(r.lines), len(plain.lines))
				}
			})
		}
	}
}

// The same on a server whose ticks land late, which is where a second click on a door that has
// already opened loses the route: every spawn, a spread of latencies and seeds.
func TestMazeSimHoldWithInterfaceArchiveLateTicks(t *testing.T) {
	registerSimArchive(t)
	for seed := uint64(1); seed <= 2; seed++ {
		for _, lat := range []time.Duration{20 * time.Millisecond, 160 * time.Millisecond, 350 * time.Millisecond} {
			for _, sp := range mazegraph.Spawns {
				t.Run(fmt.Sprintf("%v_seed=%d_lat=%v", sp, seed, lat), func(t *testing.T) {
					r := runReward(t, sp, seed%2 == 0, lat, seed, nil, true, true)
					r.must(t)
					if f := rewardFailureLines(r.lines); len(f) > 0 {
						t.Errorf("failed steps:\n%s\n%s", strings.Join(f, "\n"), r.log())
					}
					if len(linesWith(r.lines, "walking normally")) > 0 {
						t.Errorf("the hold was given up on\n%s", r.log())
					}
				})
			}
		}
	}
}

// A chest's prize can take a backpack slot and so can each of the final reward's three rolls, and
// what does not fit is dropped on the ground where nothing picks it up. So a chest is opened only
// while it leaves the reward its slots: with 4 free it is opened (leaving 3), with 3 it is not. The
// spawn's route passes three chests whose prizes (air, water, earth runes) are each a new slot.
func TestMazeSimChestsLeaveRoomForTheReward(t *testing.T) {
	for ballast := 0; ballast <= 28; ballast++ {
		t.Run(fmt.Sprintf("ballast=%d", ballast), func(t *testing.T) {
			r := runReward(t, mazegraph.Spawns[0], true, 0, 0, func(m *mazeSim) { m.ballast = ballast }, true, true)
			r.must(t)
			free := 28 - ballast
			want := free - mazeloot.RewardSlots
			if want < 0 {
				want = 0
			}
			if want > 3 {
				want = 3
			}
			if got := len(r.sim.prizes); got != want {
				t.Errorf("%d chests opened with %d slots free, want %d\n%s", got, free, want, r.log())
			}
			distinct := map[int]bool{}
			for _, p := range r.sim.prizes {
				distinct[p.id] = true
			}
			left := free - len(distinct)
			if free >= mazeloot.RewardSlots && left < mazeloot.RewardSlots {
				t.Errorf("%d slots left for the reward after the chests, want at least %d\n%s", left, mazeloot.RewardSlots, r.log())
			}
			wantLine := 0
			if want < 3 {
				wantLine = 1
			}
			if got := len(linesWith(r.lines, "free slot(s)")); got != wantLine {
				t.Errorf("%d lines saying the backpack has no room, want %d\n%s", got, wantLine, r.log())
			}
			if f := rewardFailureLines(r.lines); len(f) > 0 {
				t.Errorf("failed steps:\n%s", strings.Join(f, "\n"))
			}
		})
	}
}

// The reserve does not depend on the hold: a solver that cannot hold the reward keeps it too.
func TestMazeSimChestsLeaveRoomWithoutTheHold(t *testing.T) {
	r := runReward(t, mazegraph.Spawns[0], true, 0, 0, func(m *mazeSim) { m.ballast = 25 }, false, true)
	r.must(t)
	if r.sim.chestClicks != 0 || len(r.sim.prizes) != 0 {
		t.Errorf("%d chest clicks with 3 slots free\n%s", r.sim.chestClicks, r.log())
	}
}
