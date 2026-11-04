package questmods

// The cow farm's loop (runCowFarm) replayed against a simulated pasture: a clock the loop's waits and the fights advance, cows that
// respawn 54 seconds after they die, a gate the walk in has to be asked for, and, in the pack tests, the real pen's geometry for
// "inside" and "a walk over open ground". The replay starts where the live log's accounts stood: on the road outside the gate.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// simCow is one cow of the simulated pen.
type simCow struct {
	index int
	at    tile.Tile
	// deadUntil is when the cow is back; the zero time is a cow that is alive.
	deadUntil time.Time
	// busy is a cow another player is fighting.
	busy bool
}

// simPen is the cowWorld of the tests.
type simPen struct {
	t     *testing.T
	clock time.Time
	pos   tile.Tile
	cows  []*simCow
	// inside and steps are the pen's geometry: the real pack's (ground) or a stand-in.
	inside func(tile.Tile) bool
	steps  func(here, t tile.Tile) int

	want, bones int
	// killTime is what a fight takes, enterTime and walkTime what the walks do; enterOK is whether the gate lets the account in.
	killTime, enterTime, walkTime time.Duration
	enterOK                       bool
	// result overrides a fight's end (nil: a win after killTime).
	result func(c entityquery.Entity) cowFight
	// stopAfter makes Quiet false after that many calls (0: never).
	stopAfter int

	// What happened.
	enters, walks, fights, quiets int
	fought                        []tile.Tile // where each fought cow stood
	outsideFights, unreachable    int
	walked                        []tile.Tile
	log                           []string
}

func newSimPen(t *testing.T, start tile.Tile) *simPen {
	return &simPen{t: t, clock: time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC), pos: start, enterOK: true,
		killTime: questplan.CowKillSeconds * time.Second, enterTime: 90 * time.Second, walkTime: 8 * time.Second}
}

// withSpawns puts a live cow on each tile.
func (p *simPen) withSpawns(spawns []tile.Tile) *simPen {
	for i, sp := range spawns {
		p.cows = append(p.cows, &simCow{index: 100 + i, at: sp})
	}
	return p
}

func (p *simPen) Now() time.Time { return p.clock }
func (p *simPen) Wait(d time.Duration) bool {
	p.clock = p.clock.Add(d)
	return true
}
func (p *simPen) Quiet() bool {
	p.quiets++
	return p.stopAfter == 0 || p.quiets <= p.stopAfter
}
func (p *simPen) Done() bool          { return p.bones >= p.want }
func (p *simPen) Room() bool          { return true }
func (p *simPen) Here() tile.Tile     { return p.pos }
func (p *simPen) Self() int           { return 5 }
func (p *simPen) Progress() string    { return fmt.Sprintf("%d of %d bones taken", p.bones, p.want) }
func (p *simPen) Crowd() int          { return 0 }
func (p *simPen) Loose(tile.Tile) int { return 0 }
func (p *simPen) Logf(format string, args ...any) {
	p.log = append(p.log, fmt.Sprintf(format, args...))
}
func (p *simPen) Inside(here tile.Tile) bool  { return p.inside(here) }
func (p *simPen) Steps(here, t tile.Tile) int { return p.steps(here, t) }

// alive reports a cow that is there now.
func (p *simPen) alive(c *simCow) bool { return !p.clock.Before(c.deadUntil) }

// Cows are the live cows within the 15 tiles the client sees.
func (p *simPen) Cows() []entityquery.Entity {
	var out []entityquery.Entity
	for _, c := range p.cows {
		if !p.alive(c) || tile.Chebyshev(p.pos, c.at) > 15 {
			continue
		}
		e := entityquery.Entity{Kind: entityquery.KindNpc, Index: c.index, ID: 81, Name: "Cow", X: c.at.X, Z: c.at.Z, FaceEntity: -1}
		if c.busy {
			e.InCombat, e.FaceEntity = true, 32768+9
		}
		out = append(out, e)
	}
	return out
}

func (p *simPen) Enter() bool {
	p.enters++
	if !p.enterOK {
		p.clock = p.clock.Add(20 * time.Second)
		return false
	}
	p.clock = p.clock.Add(p.enterTime)
	p.pos = questplan.CowPastureStand
	return true
}

func (p *simPen) WalkTo(t tile.Tile) bool {
	p.walks++
	p.walked = append(p.walked, t)
	p.clock = p.clock.Add(p.walkTime)
	p.pos = t
	return true
}

func (p *simPen) cowAt(c entityquery.Entity) *simCow {
	for _, sc := range p.cows {
		if sc.index == c.Index {
			return sc
		}
	}
	p.t.Fatalf("a fight against a cow (%d) the pen does not have", c.Index)
	return nil
}

func (p *simPen) Fight(c entityquery.Entity) cowFight {
	p.fights++
	at := tile.Tile{X: c.X, Z: c.Z}
	p.fought = append(p.fought, at)
	if !p.inside(p.pos) {
		p.outsideFights++
	}
	if p.steps(p.pos, at) < 0 {
		p.unreachable++
	}
	if p.result != nil {
		fr := p.result(c)
		p.clock = p.clock.Add(fr.Took)
		return fr
	}
	sc := p.cowAt(c)
	p.clock = p.clock.Add(p.killTime)
	sc.deadUntil = p.clock.Add(questplan.CowRespawnSeconds * time.Second)
	p.pos = sc.at
	return cowFight{Result: questrunner.FightWon, Took: p.killTime}
}

func (p *simPen) Collect(entityquery.Entity) int {
	p.bones++
	p.clock = p.clock.Add(3 * time.Second)
	return 1
}

// say is every line the loop logged.
func (p *simPen) say() string { return strings.Join(p.log, "\n") }

// rectPen is a pen without geometry: a rectangle (the pen's bounding box with the west fence at x 3252|3253) in which anything is a
// walk away, and outside which nothing is.
func rectPen(p *simPen) *simPen {
	in := func(t tile.Tile) bool { return t.X >= 3253 && t.X <= 3265 && t.Z >= 3255 && t.Z <= 3298 }
	p.inside = in
	p.steps = func(here, t tile.Tile) int {
		if !in(here) || !in(t) {
			return -1
		}
		return tile.Chebyshev(here, t)
	}
	return p
}

// ---------------------------------------------------------------------------------------------------------
// The replay: where the live log's accounts stood.
// ---------------------------------------------------------------------------------------------------------

// From the road outside the gate, with the ten cows in their places, a round for Demon Slayer's 25 bones walks in once, fights only
// inside the pen, and finishes in its window. The farm it replaces stood at 3252,3267 for fourteen minutes and killed nothing.
func TestCowFarmReplayFromTheRoadOutsideTheGate(t *testing.T) {
	d := csDeps(t)
	g := &cowGround{pk: d.Pack}
	start := tile.Tile{X: 3252, Z: 3267}
	p := newSimPen(t, start).withSpawns(questplan.CowPastureSpawns)
	p.inside, p.steps, p.want = g.inside, g.stepsTo, questplan.DemonBonesNeeded

	if p.inside(start) {
		t.Fatalf("the road tile %v reads as inside the pen: the replay would prove nothing", start)
	}
	window := questplan.DemonBonesWindow()
	tally := runCowFarm(p, "demon bones", cowSpec{Budget: window, MaxFailures: demonBonesMaxFailures})

	if tally.End != cowEndGoal || p.bones != questplan.DemonBonesNeeded {
		t.Fatalf("the round ended on %q with %d of %d bones in %s (window %s):\n%s", tally.End, p.bones, p.want, tally.Elapsed, window, p.say())
	}
	if p.enters != 1 || tally.Entries != 1 {
		t.Errorf("walks in by the gate: %d (tally %d), want 1", p.enters, tally.Entries)
	}
	if p.outsideFights != 0 || p.unreachable != 0 {
		t.Errorf("%d fights began outside the pen and %d against a cow no walk over open ground reaches", p.outsideFights, p.unreachable)
	}
	if tally.Won != questplan.DemonBonesNeeded || p.fights != tally.Won || tally.Bones != questplan.DemonBonesNeeded {
		t.Errorf("kills %d, fights %d, bones %d, want %d of each", tally.Won, p.fights, tally.Bones, questplan.DemonBonesNeeded)
	}
	if tally.Elapsed >= window {
		t.Errorf("the round took %s of its %s window", tally.Elapsed, window)
	}
	// Alone in the pen with the cows' kill time at the plan's figure, the round must come in under the solo estimate plus the walk in.
	solo := questplan.CowFarmEstimate(questplan.DemonBonesNeeded, 1) + p.enterTime + 3*time.Minute
	if tally.Elapsed > solo {
		t.Errorf("the round took %s, over the plan's solo estimate %s", tally.Elapsed, solo)
	}
	t.Logf("25 bones in %s (window %s, solo estimate %s): %d kills, %d stops of the beat", tally.Elapsed.Round(time.Second), window,
		questplan.CowFarmEstimate(questplan.DemonBonesNeeded, 1).Round(time.Second), tally.Won, tally.Sweeps)
}

// The same replay for a rung's stint (no bones to count: a goal that is never met), from the other side of the stand: the stint ends on
// its budget, having killed about what the plan says one account kills alone in that time.
func TestCowFarmReplayOfARungsStintYieldsWhatThePlanSays(t *testing.T) {
	d := csDeps(t)
	g := &cowGround{pk: d.Pack}
	p := newSimPen(t, tile.Tile{X: 3247, Z: 3262}).withSpawns(questplan.CowPastureSpawns) // the old rungs' walk (within 12) ended here
	p.inside, p.steps, p.want = g.inside, g.stepsTo, 1<<30

	budget := 20 * time.Minute
	tally := runCowFarm(p, "cows", cowSpec{Budget: budget, MaxFailures: cowsMaxFailures})
	if tally.End != cowEndBudget {
		t.Fatalf("the stint ended on %q:\n%s", tally.End, p.say())
	}
	// What the plan says one account kills alone in the stint, less the walk in (90 seconds here) and a tenth for the beat.
	want := questplan.CowExpectedKills(budget-p.enterTime, 1)
	if tally.Won < want-want/10 {
		t.Errorf("a stint of %s killed %d cows alone in the pen, the plan expects %d (a tenth under is allowed)", budget, tally.Won, want)
	}
	if p.outsideFights != 0 || p.unreachable != 0 {
		t.Errorf("%d fights outside the pen, %d unreachable", p.outsideFights, p.unreachable)
	}
	// Of 712 stints on record 399 made nothing and the mean was 7 bones: a stint of the new farm alone in the pen is several times it.
	if tally.Won < 30 {
		t.Errorf("%d kills in %s: the stint should kill dozens", tally.Won, budget)
	}
	t.Logf("%d kills in %s alone (plan: %d), %d stops of the beat", tally.Won, budget, want, tally.Sweeps)
}

// With every cow dead the account walks the pasture's beat, in order, and fights what it finds: it does not stand where it looked.
func TestCowFarmWalksTheBeatWhenNoCowIsInView(t *testing.T) {
	d := csDeps(t)
	g := &cowGround{pk: d.Pack}
	p := newSimPen(t, questplan.CowPastureStand).withSpawns(questplan.CowPastureSpawns)
	p.inside, p.steps, p.want = g.inside, g.stepsTo, 6
	// Only the two far cows of the north-east and the north-west are alive; the rest come back in a minute.
	for _, c := range p.cows {
		if c.at != (tile.Tile{X: 3261, Z: 3292}) && c.at != (tile.Tile{X: 3243, Z: 3295}) {
			c.deadUntil = p.clock.Add(10 * time.Minute)
		}
	}
	tally := runCowFarm(p, "demon bones", cowSpec{Budget: 25 * time.Minute, MaxFailures: 20})
	if tally.Sweeps == 0 || len(p.walked) == 0 {
		t.Fatalf("no walk along the pasture: %+v\n%s", tally, p.say())
	}
	for i, w := range p.walked {
		found := false
		for _, stop := range questplan.CowPastureSweep {
			if stop == w {
				found = true
			}
		}
		if !found {
			t.Errorf("walk %d went to %v, which is not a stop of the beat", i, w)
		}
	}
	if tally.Won == 0 {
		t.Errorf("the beat found no cow in %s:\n%s", tally.Elapsed, p.say())
	}
	if !strings.Contains(p.say(), "walking the pasture to") {
		t.Errorf("the log does not say the account walks the pasture:\n%s", p.say())
	}
	t.Logf("%d kills, %d stops walked, %s", tally.Won, tally.Sweeps, tally.Elapsed.Round(time.Second))
}

// A cow the account stands in the pen and can see over the fence, on the road, is not fought: nothing but cows beyond the fence in view
// means the beat, and no attack is sent.
func TestCowFarmDoesNotFightACowBehindTheFence(t *testing.T) {
	d := csDeps(t)
	g := &cowGround{pk: d.Pack}
	p := newSimPen(t, questplan.CowPastureStand).withSpawns([]tile.Tile{{X: 3250, Z: 3262}, {X: 3249, Z: 3266}})
	p.inside, p.steps, p.want = g.inside, g.stepsTo, 3
	for _, c := range p.cows {
		if g.stepsTo(p.pos, c.at) >= 0 {
			t.Fatalf("the cow at %v is a walk from the stand: not a cow behind the fence", c.at)
		}
	}
	tally := runCowFarm(p, "demon bones", cowSpec{Budget: 12 * time.Minute, MaxFailures: 20})
	if p.fights != 0 {
		t.Errorf("%d fights against cows behind the fence", p.fights)
	}
	if tally.End != cowEndBudget || tally.Sweeps == 0 {
		t.Errorf("ended on %q after %d sweeps, want the budget after a walk of the pasture", tally.End, tally.Sweeps)
	}
}

// ---------------------------------------------------------------------------------------------------------
// The loop's rules, with no geometry.
// ---------------------------------------------------------------------------------------------------------

// A gate that will not let the account in is tried cowEnterTries times and the round is given up, quickly and without a fight.
func TestCowFarmGivesUpWhenTheGateWillNotLetItIn(t *testing.T) {
	p := rectPen(newSimPen(t, tile.Tile{X: 3252, Z: 3267}).withSpawns(questplan.CowPastureSpawns))
	p.want, p.enterOK = 25, false
	tally := runCowFarm(p, "demon bones", cowSpec{Budget: 30 * time.Minute, MaxFailures: 20})
	if tally.End != cowEndGate || p.enters != cowEnterTries || p.fights != 0 {
		t.Errorf("ended on %q after %d walks in and %d fights, want %q after %d and 0\n%s", tally.End, p.enters, p.fights, cowEndGate, cowEnterTries, p.say())
	}
	if tally.Elapsed > 5*time.Minute {
		t.Errorf("giving up took %s", tally.Elapsed)
	}
}

// An account the walk left on the road (the walker stopped short of the gate) is seen to be outside and walks in again.
func TestCowFarmWalksInAgainWhenTheFirstWalkEndsOutside(t *testing.T) {
	p := rectPen(newSimPen(t, tile.Tile{X: 3240, Z: 3267}).withSpawns(questplan.CowPastureSpawns))
	p.want = 3
	w := &shortEnter{simPen: p}
	tally := runCowFarm(w, "cows", cowSpec{Budget: 20 * time.Minute, MaxFailures: 20})
	if tally.End != cowEndGoal || tally.Entries != 2 || w.calls != 2 {
		t.Errorf("ended on %q after %d walks in (%d asked), want the goal after 2\n%s", tally.End, tally.Entries, w.calls, p.say())
	}
	if p.outsideFights != 0 {
		t.Errorf("%d fights began outside the pen", p.outsideFights)
	}
}

// shortEnter is a pen whose first walk in reports an arrival and leaves the account where it was.
type shortEnter struct {
	*simPen
	calls int
}

func (s *shortEnter) Enter() bool {
	s.calls++
	if s.calls == 1 {
		s.clock = s.clock.Add(30 * time.Second)
		return true
	}
	return s.simPen.Enter()
}

// A fight given up leaves the cow alone for the skip, and the account fights another; a cow another player is fighting is not chosen;
// a lost fight is retried and, twenty in a row, ends the round.
func TestCowFarmSkipsWhatItGaveUpAndWhatIsBusy(t *testing.T) {
	p := rectPen(newSimPen(t, questplan.CowPastureStand))
	near := &simCow{index: 1, at: tile.Tile{X: 3259, Z: 3267}}
	busy := &simCow{index: 2, at: tile.Tile{X: 3260, Z: 3266}, busy: true}
	far := &simCow{index: 3, at: tile.Tile{X: 3262, Z: 3270}}
	p.cows = []*simCow{near, busy, far}
	p.want = 1
	var order []int
	p.result = func(c entityquery.Entity) cowFight {
		order = append(order, c.Index)
		switch c.Index {
		case 1:
			return cowFight{Result: questrunner.FightGaveUp, Why: "no blow has landed on it", Skip: demonCowSkipFor, Took: 31 * time.Second}
		}
		return cowFight{Result: questrunner.FightWon, Took: 20 * time.Second}
	}
	tally := runCowFarm(p, "demon bones", cowSpec{Budget: 10 * time.Minute, MaxFailures: 20})
	if fmt.Sprint(order) != "[1 3]" {
		t.Errorf("fought cows %v, want [1 3]: the near one first, given up, then the far one, never the busy one", order)
	}
	if tally.GaveUp != 1 || tally.Won != 1 || tally.End != cowEndGoal {
		t.Errorf("tally %+v", tally)
	}
	if !strings.Contains(p.say(), "gave up on the cow at 3259,3267 after 31s: no blow has landed on it") {
		t.Errorf("the give-up is not in the log:\n%s", p.say())
	}
}

func TestCowFarmEndsAfterTwentyLostFightsInARow(t *testing.T) {
	p := rectPen(newSimPen(t, questplan.CowPastureStand).withSpawns([]tile.Tile{{X: 3259, Z: 3267}}))
	p.want = 5
	p.result = func(entityquery.Entity) cowFight { return cowFight{Result: questrunner.FightLost} }
	tally := runCowFarm(p, "cows", cowSpec{Budget: time.Hour, MaxFailures: 20})
	if tally.End != cowEndFailed || p.fights != 20 {
		t.Errorf("ended on %q after %d fights, want %q after 20", tally.End, p.fights, cowEndFailed)
	}
}

func TestCowFarmStopsWhenTheBotDoes(t *testing.T) {
	p := rectPen(newSimPen(t, questplan.CowPastureStand).withSpawns(questplan.CowPastureSpawns))
	p.want, p.stopAfter = 25, 3
	tally := runCowFarm(p, "cows", cowSpec{Budget: time.Hour, MaxFailures: 20})
	if tally.End != cowEndStopped {
		t.Errorf("ended on %q, want %q", tally.End, cowEndStopped)
	}
}

// Live, 18 October 2026, an account looked for a cow 23,142 times in a minute and a half at the fleet's stop: the idle wait was refused
// (the bot was stopping) and the old loop went round again. A refused wait ends the round, whatever the round was doing.
func TestCowFarmDoesNotSpinWhenItsWaitsAreRefused(t *testing.T) {
	for name, start := range map[string]tile.Tile{"outside the pen": {X: 3252, Z: 3267}, "inside with no cow in view": questplan.CowPastureStand} {
		p := rectPen(newSimPen(t, start))
		p.want, p.enterOK = 25, false
		w := &refusedWaits{simPen: p}
		tally := runCowFarm(w, "demon bones", cowSpec{Budget: time.Hour, MaxFailures: 20})
		if tally.End != cowEndStopped || w.waits > 2 {
			t.Errorf("%s: ended on %q after %d waits, want %q after at most 2", name, tally.End, w.waits, cowEndStopped)
		}
	}
}

// refusedWaits is a pen whose every wait is refused.
type refusedWaits struct {
	*simPen
	waits int
}

func (r *refusedWaits) Wait(time.Duration) bool {
	r.waits++
	return false
}

func TestCowFarmDoesNothingWhenItAlreadyHasWhatItCameFor(t *testing.T) {
	p := rectPen(newSimPen(t, tile.Tile{X: 3252, Z: 3267}).withSpawns(questplan.CowPastureSpawns))
	p.want, p.bones = 25, 25
	tally := runCowFarm(p, "demon bones", cowSpec{Budget: time.Hour, MaxFailures: 20})
	if tally.End != cowEndGoal || p.enters != 0 || p.fights != 0 || p.walks != 0 {
		t.Errorf("ended on %q after %d entries, %d fights, %d walks", tally.End, p.enters, p.fights, p.walks)
	}
}

// The round says what it yields every three minutes, in the words the log was read in: kills, rate, the goal and the crowd.
func TestCowFarmSaysItsYield(t *testing.T) {
	p := rectPen(newSimPen(t, questplan.CowPastureStand).withSpawns(questplan.CowPastureSpawns))
	p.want = 1 << 30
	runCowFarm(p, "demon bones", cowSpec{Budget: 7 * time.Minute, MaxFailures: 20})
	lines := 0
	for _, l := range p.log {
		if strings.HasPrefix(l, "demon bones: 3m") || strings.HasPrefix(l, "demon bones: 6m") {
			lines++
			if !strings.Contains(l, "cows killed (") || !strings.Contains(l, " a minute)") || !strings.Contains(l, "bones taken") {
				t.Errorf("yield line %q lacks the kills, the rate or the goal", l)
			}
		}
	}
	if lines != 2 {
		t.Errorf("%d yield lines in 7 minutes, want 2 (at 3 and 6):\n%s", lines, p.say())
	}
}

// The beat skips the stops the account is next to and wraps round.
func TestNextSweepSkipsNearStopsAndWraps(t *testing.T) {
	beat := []tile.Tile{{X: 0, Z: 0}, {X: 10, Z: 0}, {X: 20, Z: 0}}
	from := 0
	// At the first stop: the second.
	if got, ok := nextSweep(beat, &from, tile.Tile{X: 1, Z: 0}); !ok || got != beat[1] || from != 2 {
		t.Errorf("got %v %v (index %d), want the second stop and index 2", got, ok, from)
	}
	if got, ok := nextSweep(beat, &from, tile.Tile{X: 1, Z: 0}); !ok || got != beat[2] || from != 3 {
		t.Errorf("got %v %v (index %d), want the third stop", got, ok, from)
	}
	// The next is the first, which the account is next to: it goes on to the second.
	if got, ok := nextSweep(beat, &from, tile.Tile{X: 1, Z: 0}); !ok || got != beat[1] {
		t.Errorf("got %v %v, want the second stop after wrapping past the near first", got, ok)
	}
	// Every stop near: none.
	if _, ok := nextSweep([]tile.Tile{{X: 0, Z: 0}, {X: 2, Z: 2}}, new(int), tile.Tile{X: 1, Z: 1}); ok {
		t.Error("a stop was chosen though every one is within reach of the account")
	}
}
