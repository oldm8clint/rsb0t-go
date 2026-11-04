package questmods

// The workmen's pockets cost a hitpoint a miss, and damage_self can kill (pickpocket_digworkman1). digPocketLoop is the loop that steals from
// them (digPickpocket builds its live world), and these tests run it on a world of its own: an account with hitpoints and food, whose attempts
// the test scripts, and a park the loop asks for. What they prove is the loop's own: it never steals at or under its hitpoint floor, it eats
// there when it has food and rests (parks the quest, with the reason and a time) when it has none, it will not start a stretch it cannot take a
// miss in, and it ends at the budget, the gate and the run. The Engine.Run test at the end drives the real custom through the engine on the
// simulated session, to the park a hurt account with nothing to eat gets.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// pocketWorld is the account the loop steals as: its hitpoints, the sharks in its pack (each heals up to its maximum), and what each attempt
// comes to. Every miss costs one hitpoint, as the content's does.
type pocketWorld struct {
	t         *testing.T
	hp, maxHP int
	sharks    int
	// thieving is the base level the loop reads (0 unread).
	thieving int
	// outcome is what the nth attempt (from 0) comes to: a miss when nil.
	outcome func(n int) digPocketTry
	// got is the point at which the pack holds the item: after that many picks (0 never).
	gotAfter int
	// eatFails makes every bite fail although the pack holds food.
	eatFails bool
	// overAfter ends the budget after that many attempts (0: never; the test then relies on the loop ending itself).
	overAfter int
	// unreadHP makes the vitals read 0 of 0.
	unreadHP bool
	// arriveFails makes the walk to the workmen fail.
	arriveFails bool

	attempts, picks, bites int
	arrived, begun         bool
	hpAtAttempt            []int
	minHP                  int
	parks                  []pocketPark
	logs                   []string
	tidies                 int
}

type pocketPark struct {
	reason string
	skip   time.Duration
}

func newPocketWorld(t *testing.T, hp, maxHP int) *pocketWorld {
	return &pocketWorld{t: t, hp: hp, maxHP: maxHP, thieving: 40, minHP: hp}
}

func (w *pocketWorld) have() bool { return w.gotAfter > 0 && w.picks >= w.gotAfter }

func (w *pocketWorld) ops() digPocketOps {
	return digPocketOps{
		have:     w.have,
		thieving: func() int { return w.thieving },
		arrive:   func() bool { w.arrived = true; return !w.arriveFails },
		begin:    func() { w.begun = true },
		over:     func() bool { return w.overAfter > 0 && w.attempts >= w.overAfter },
		vitals: func() (int, int, int) {
			if w.unreadHP {
				return 0, 0, w.sharks * 20
			}
			return w.hp, w.maxHP, w.sharks * 20
		},
		bite: func(hp int) bool {
			if w.eatFails || w.sharks == 0 {
				return false
			}
			w.sharks--
			w.bites++
			w.hp = w.maxHP
			return true
		},
		park: func(reason string, skip time.Duration) { w.parks = append(w.parks, pocketPark{reason, skip}) },
		tidy: func() { w.tidies++ },
		try: func() digPocketTry {
			w.hpAtAttempt = append(w.hpAtAttempt, w.hp)
			n := w.attempts
			w.attempts++
			res := digPocketStunned
			if w.outcome != nil {
				res = w.outcome(n)
			}
			switch res {
			case digPocketStunned:
				w.hp--
				if w.hp < w.minHP {
					w.minHP = w.hp
				}
				if w.hp <= 0 {
					w.t.Fatalf("the account died to the pocket after %d attempts", w.attempts)
				}
			case digPocketPicked:
				w.picks++
			}
			return res
		},
		log: func(format string, args ...any) { w.logs = append(w.logs, fmt.Sprintf(format, args...)) },
	}
}

// An account with nothing to eat steals down to its floor and no lower, then rests: it is parked for the time the hitpoints take to come back,
// with the reason, and the pocket is never tried again at the floor.
func TestThePocketLoopStopsAtItsFloorWithNothingToEat(t *testing.T) {
	for _, c := range []struct {
		maxHP int
		// attempts is the misses from full to the floor: the hitpoints over it.
		attempts int
	}{{10, 6}, {12, 8}, {20, 15}, {30, 22}, {40, 30}, {70, 52}, {99, 74}} {
		t.Run(fmt.Sprintf("%d hitpoints", c.maxHP), func(t *testing.T) {
			w := newPocketWorld(t, c.maxHP, c.maxHP)
			res := digPocketLoop(w.ops())
			floor := questplan.DigPocketFloor(c.maxHP)
			if want := c.maxHP - floor; w.attempts != want || c.attempts != want {
				t.Errorf("%d attempts, want the %d hitpoints over the floor %d (table says %d)", w.attempts, want, floor, c.attempts)
			}
			if res.ok || !res.rested || !res.started {
				t.Errorf("result %+v, want started, rested and not ok", res)
			}
			if w.hp != floor || w.minHP != floor {
				t.Errorf("the account ended at %d (lowest %d), want the floor %d", w.hp, w.minHP, floor)
			}
			for i, hp := range w.hpAtAttempt {
				if hp <= floor {
					t.Errorf("attempt %d began at %d hitpoints, at or under the floor %d", i, hp, floor)
				}
			}
			if len(w.parks) != 1 {
				t.Fatalf("%d parks, want one: %+v", len(w.parks), w.parks)
			}
			p := w.parks[0]
			if p.skip < questplan.DigPocketRestMin || p.skip > questplan.DigPocketRestMax {
				t.Errorf("parked for %s, outside [%s, %s]", p.skip, questplan.DigPocketRestMin, questplan.DigPocketRestMax)
			}
			if p.skip != questplan.DigPocketRestFor(floor, c.maxHP) {
				t.Errorf("parked for %s, want %s", p.skip, questplan.DigPocketRestFor(floor, c.maxHP))
			}
			for _, want := range []string{"hitpoint", fmt.Sprintf("%d of %d", floor, c.maxHP), "no food", "resting"} {
				if !strings.Contains(p.reason, want) {
					t.Errorf("the park reason %q lacks %q", p.reason, want)
				}
			}
			if len(w.logs) != 1 || !strings.Contains(w.logs[0], "resting") {
				t.Errorf("log %v: want one line saying the account rests", w.logs)
			}
		})
	}
}

// With food the loop eats at the floor and goes on; with the food gone it rests, still never stealing at the floor.
func TestThePocketLoopEatsAtItsFloorAndGoesOn(t *testing.T) {
	w := newPocketWorld(t, 10, 10)
	w.sharks = 2
	res := digPocketLoop(w.ops())
	// Six misses to the floor, a shark, six more, a shark, six more, and the rest.
	if w.attempts != 18 || w.bites != 2 {
		t.Errorf("%d attempts and %d bites, want 18 and 2", w.attempts, w.bites)
	}
	if !res.rested || len(w.parks) != 1 || res.ok {
		t.Errorf("result %+v, parks %+v: want one rest once the food ran out", res, w.parks)
	}
	if w.minHP < questplan.DigPocketFloor(10) {
		t.Errorf("the account fell to %d, under the floor %d", w.minHP, questplan.DigPocketFloor(10))
	}
	for i, hp := range w.hpAtAttempt {
		if hp <= questplan.DigPocketFloor(10) {
			t.Errorf("attempt %d began at %d hitpoints", i, hp)
		}
	}
}

// A bite that does not take (the food is in the pack but cannot be eaten) is not retried for ever: the loop rests like an account without food.
func TestThePocketLoopRestsWhenABiteDoesNotTake(t *testing.T) {
	w := newPocketWorld(t, 10, 10)
	w.sharks = 3
	w.eatFails = true
	res := digPocketLoop(w.ops())
	if !res.rested || len(w.parks) != 1 || w.attempts != 6 {
		t.Errorf("result %+v, %d parks, %d attempts: want a rest at the floor after six", res, len(w.parks), w.attempts)
	}
	if w.bites != 0 {
		t.Errorf("%d bites", w.bites)
	}
}

// A stretch is not started from the floor, or (with nothing to eat) from under the resume line: no walk to the workmen, no attempt, a park.
func TestThePocketLoopWillNotStartHurt(t *testing.T) {
	for _, c := range []struct {
		name         string
		hp, max, eat int
		starts       bool
	}{
		{"at the floor, nothing to eat", 4, 10, 0, false},
		{"under the floor", 2, 10, 0, false},
		{"over the floor but under the resume line", 6, 10, 0, false},
		{"at the resume line", 7, 10, 0, true},
		{"over the floor, under the resume line, with food", 6, 10, 1, true},
		{"at the floor with food: eat first", 4, 10, 1, true},
		{"half of a bigger account", 34, 70, 0, false},
		{"half of a bigger account, one more", 35, 70, 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPocketWorld(t, c.hp, c.max)
			w.sharks = c.eat
			w.overAfter = 1
			res := digPocketLoop(w.ops())
			if c.starts {
				if !w.arrived || !w.begun || w.attempts == 0 || len(w.parks) != 0 {
					t.Errorf("arrived %v begun %v attempts %d parks %+v: want a start", w.arrived, w.begun, w.attempts, w.parks)
				}
				return
			}
			if w.arrived || w.begun || w.attempts != 0 || res.started {
				t.Errorf("arrived %v begun %v attempts %d: a hurt account walked to the workmen", w.arrived, w.begun, w.attempts)
			}
			if !res.rested || len(w.parks) != 1 || w.parks[0].skip < questplan.DigPocketRestMin {
				t.Errorf("result %+v, parks %+v: want one rest of at least %s", res, w.parks, questplan.DigPocketRestMin)
			}
		})
	}
}

// A pick that fills the pack ends the loop at once, without a rest; the budget, the run and the content's gate end it too.
func TestThePocketLoopEnds(t *testing.T) {
	// The third pick is the rope: misses, a pick, two misses, two picks.
	w := newPocketWorld(t, 40, 40)
	w.gotAfter = 3
	w.outcome = func(n int) digPocketTry {
		if n == 0 || n == 2 || n == 3 {
			return digPocketStunned
		}
		return digPocketPicked
	}
	res := digPocketLoop(w.ops())
	if !res.ok || res.picks != 3 || res.stuns != 3 || len(w.parks) != 0 || w.attempts != 6 {
		t.Errorf("result %+v after %d attempts with parks %+v: want a rope after six attempts and no park", res, w.attempts, w.parks)
	}

	// Already held: nothing is tried, nothing is walked to.
	w = newPocketWorld(t, 10, 10)
	w.gotAfter = 1
	w.picks = 1
	if res := digPocketLoop(w.ops()); !res.ok || w.arrived || w.attempts != 0 {
		t.Errorf("result %+v: an item already held was stolen toward", res)
	}

	// The budget.
	w = newPocketWorld(t, 70, 70)
	w.overAfter = 5
	if res := digPocketLoop(w.ops()); res.ok || res.rested || w.attempts != 5 || len(w.parks) != 0 {
		t.Errorf("result %+v after %d attempts: want the budget to end the loop quietly", res, w.attempts)
	}

	// The run ends during an attempt.
	w = newPocketWorld(t, 70, 70)
	w.outcome = func(n int) digPocketTry {
		if n == 2 {
			return digPocketOver
		}
		return digPocketIdle
	}
	if res := digPocketLoop(w.ops()); res.ok || w.attempts != 3 || len(w.parks) != 0 {
		t.Errorf("result %+v after %d attempts: want the run's end to end the loop", res, w.attempts)
	}

	// The content's gate parks for the level.
	w = newPocketWorld(t, 70, 70)
	w.outcome = func(n int) digPocketTry { return digPocketGated }
	if res := digPocketLoop(w.ops()); res.ok || w.attempts != 1 || len(w.parks) != 1 || w.parks[0].skip != questplan.DigSkillParkFor ||
		!strings.Contains(w.parks[0].reason, "Thieving 25") {
		t.Errorf("result %+v after %d attempts, parks %+v: want the level's park", res, w.attempts, w.parks)
	}

	// A level read under the content's own parks before the walk.
	w = newPocketWorld(t, 70, 70)
	w.thieving = 24
	if res := digPocketLoop(w.ops()); res.ok || w.arrived || len(w.parks) != 1 || w.parks[0].skip != questplan.DigSkillParkFor {
		t.Errorf("result %+v, parks %+v: want Thieving 24 parked before the walk", res, w.parks)
	}
	// An unread level goes on.
	w = newPocketWorld(t, 70, 70)
	w.thieving = 0
	w.overAfter = 2
	if digPocketLoop(w.ops()); w.attempts != 2 || len(w.parks) != 0 {
		t.Errorf("an unread Thieving level stopped the loop: %d attempts, parks %+v", w.attempts, w.parks)
	}

	// The walk to the workmen fails: no attempt, no park (the step fails and the engine's watchdog decides).
	w = newPocketWorld(t, 70, 70)
	w.arriveFails = true
	if res := digPocketLoop(w.ops()); res.ok || res.started || w.attempts != 0 || len(w.parks) != 0 {
		t.Errorf("result %+v: want a failed walk to end the loop", res)
	}

	// Hitpoints not read yet judge nothing: the loop goes on to its budget.
	w = newPocketWorld(t, 70, 70)
	w.unreadHP = true
	w.overAfter = 4
	if res := digPocketLoop(w.ops()); res.rested || w.attempts != 4 || len(w.parks) != 0 {
		t.Errorf("result %+v after %d attempts: unread hitpoints stopped the loop", res, w.attempts)
	}
}

// The loop tidies (clears a modal and drops the loot) before every attempt.
func TestThePocketLoopTidiesBeforeEachAttempt(t *testing.T) {
	w := newPocketWorld(t, 70, 70)
	w.overAfter = 5
	digPocketLoop(w.ops())
	if w.tidies != 5 {
		t.Errorf("%d tidies for %d attempts", w.tidies, w.attempts)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Through the engine.
// ---------------------------------------------------------------------------------------------------------------------

// dgHurt gives the rig's hitpoints: a maximum seen, then the current level.
func dgHurt(r *csRig, hp, maxHP int) {
	osSeed(r, gamestate.HitpointsStat, maxHP)
	if hp != maxHP {
		p := packet.NewWriter(6)
		p.P1(byte(gamestate.HitpointsStat))
		p.P4(int32(levels.XPAtLevel(maxHP)))
		p.P1(byte(hp))
		r.sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	}
}

// newPocketRig is a Digsite rig for an account with the given hitpoints and nothing to eat, at the first exam's pocket.
func newPocketRig(t *testing.T, hp, maxHP int) *dgRig {
	r := &dgRig{csRig: newCSRig(t, questplan.DigExaminerAnchor), counts: map[string]int{}}
	r.stageKnown = true
	r.setStage(questplan.DigFirstExam)
	osSeed(r.csRig, dgThieving, 40)
	osSeed(r.csRig, dgAgility, 30)
	osSeed(r.csRig, dgHerblore, 25)
	dgHurt(r.csRig, hp, maxHP)
	return r
}

// applyRealPocket plays the rig's world except for the pocket, which is the module's own custom.
func (r *dgRig) applyRealPocket(step questplan.Step) bool {
	if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomDigPickpocket {
		return digCustomPickpocket(r.e, step)
	}
	return r.apply(step)
}

// A hurt account with nothing to eat is sent for food first (the kit finds none), and then parked by the real pocket custom with the reason,
// before any walk to the workmen: through Engine.Run on the simulated session.
func TestAHurtAccountIsParkedByTheRealPocketThroughTheEngine(t *testing.T) {
	r := newPocketRig(t, 5, 10)
	if hp, max := questrunner.Hitpoints(r.e.D); hp != 5 || max != 10 {
		t.Fatalf("the rig has %d of %d hitpoints, want 5 of 10", hp, max)
	}
	res := r.run(Digsite(), r.applyRealPocket)
	if res.Outcome != questrunner.OutcomeParked {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.log)
	}
	for _, want := range []string{"hitpoints", "5 of 10", "no food", "resting"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("the park reason %q lacks %q", res.Reason, want)
		}
	}
	// The food was tried first, once, and the pocket came after it.
	var kit, pocket = -1, -1
	for i, d := range r.described() {
		switch {
		case strings.HasPrefix(d, "provision "+questplan.KitDigPocketFood):
			kit = i
		case strings.HasPrefix(d, questplan.CustomDigPickpocket):
			pocket = i
		}
	}
	if kit < 0 || pocket < kit {
		t.Errorf("steps %v: want the pocket food kit and then the pocket", r.described())
	}
	for _, l := range r.log {
		if strings.Contains(l, "workmen were not reached") {
			t.Errorf("the account set out for the workmen while hurt: %v", r.log)
		}
	}
}

// The same account at full hitpoints and the same empty bank is not parked by the pocket's guard: it goes to the workmen.
func TestAFitAccountIsNotParkedByThePocketGuard(t *testing.T) {
	w := newPocketWorld(t, 10, 10)
	w.overAfter = 3
	digPocketLoop(w.ops())
	if w.attempts != 3 || len(w.parks) != 0 {
		t.Errorf("a fit account made %d attempts with parks %+v", w.attempts, w.parks)
	}
}
