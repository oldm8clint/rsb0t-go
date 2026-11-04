package bot

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/wcfletchrunner"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// worldRig is one yew-rung account as the world spread sees it: a bot, a Context with the fleet's world hooks
// (fakeWorlds, homeworld_test.go) and a session at Woodcutting 90 and Fletching 81.
//
// Each test gives its accounts worlds nobody else uses (the registry is process-wide) and releases them after.
type worldRig struct {
	b     *wcfletchBot
	ctx   *Context
	d     *woodcutrunner.Deps
	sim   *actionsim.Session
	f     *fakeWorlds
	lines *[]string
	busy  bool
	who   string
}

func setSkillLevel(sim *actionsim.Session, stat, level int) {
	xp := levels.XPAtLevel(level)
	sim.State().Handle(serverprot.UPDATE_STAT, []byte{byte(stat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(level)})
}

// newWorldRig builds an account on `current`. worlds is its yewWorlds setting and home its homeWorld setting.
// mule names the account's mule ("" for none).
func newWorldRig(t *testing.T, who string, current int, worlds string, home int, mule string) *worldRig {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{}, nil)
	setSkillLevel(sim, woodcutrunner.WoodcuttingStat, 90)
	setSkillLevel(sim, wcfletchrunner.FletchingStat, 81)
	r := &worldRig{sim: sim, f: &fakeWorlds{}, who: who}
	var lines []string
	r.lines = &lines
	settings := map[string]interface{}{"homeWorld": float64(home), "yewWorlds": worlds}
	if mule != "" {
		settings["mule"] = mule
	}
	r.ctx = r.f.ctx(current, settings)
	r.ctx.Username = who
	r.ctx.Log = func(s string) { lines = append(lines, s) }
	r.ctx.HomeWorld = home
	r.ctx.LoginBusy = func() bool { return r.busy }
	cfg := wcfletchSettings(settings)
	cfg.Wood = wcfletchplan.DefaultSettings().Wood
	cfg.Wood.Mule = mule
	r.b = &wcfletchBot{cfg: cfg.Normalised()}
	r.d = &woodcutrunner.Deps{Session: sim}
	t.Cleanup(func() {
		wcfletchplan.SharedWorldSpread().Release(who)
		wcfletchplan.SharedHandoffTrips().End(who)
	})
	return r
}

// start is what OnStart does for the world: the settings' home, then the assignment.
func (r *worldRig) start() {
	r.b.home.start(r.ctx, "wcfletch")
	r.b.yewWorldStart(r.ctx, r.d)
}

func (r *worldRig) said(sub string) bool {
	for _, l := range *r.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func (r *worldRig) lastHop() int {
	if len(r.f.hops) == 0 {
		return 0
	}
	return r.f.hops[len(r.f.hops)-1]
}

// THE DEAL, end to end on the bot: three accounts of a cohort that a relaunch logged in on world 71 are dealt
// 71, 72, 73. The first stays; the others make their assigned world the fleet's home at once (so a hand-off's
// hop home goes there) and the first Loop hops them.
func TestYewWorldStartDealsTheCohortOverTheWorlds(t *testing.T) {
	tag := yewTestTag()
	var rigs []*worldRig
	for i := 0; i < 3; i++ {
		r := newWorldRig(t, fmt.Sprintf("Deal%d_%s", i, tag), 71, "71,72,73", 71, "mule")
		r.start()
		rigs = append(rigs, r)
	}
	for i, want := range []int{71, 72, 73} {
		r := rigs[i]
		if r.b.yewW.assigned != want {
			t.Fatalf("account %d assigned world %d, want %d (%v)", i, r.b.yewW.assigned, want, *r.lines)
		}
		if r.f.home != want || r.ctx.HomeWorld != want {
			t.Errorf("account %d: fleet home %d, ctx.HomeWorld %d, want %d: donorrunner hops home to it after a hand-off", i, r.f.home, r.ctx.HomeWorld, want)
		}
		hopped := r.b.home.step(r.ctx, "wcfletch", time.Now())
		if want == 71 && (hopped || len(r.f.hops) != 0) {
			t.Errorf("the account already on its world was hopped: %v", r.f.hops)
		}
		if want != 71 && (!hopped || r.lastHop() != want) {
			t.Errorf("account %d: step = %v, hops %v, want a hop to %d", i, hopped, r.f.hops, want)
		}
		if !r.said("yew world spread:") {
			t.Errorf("account %d said nothing about its world: %v", i, *r.lines)
		}
	}
	if !rigs[1].said("going to world 72 from world 71") || !rigs[0].said("staying on world 71") {
		t.Errorf("the log should say where each one went: %v / %v", *rigs[0].lines, *rigs[1].lines)
	}
}

// THE RESTART A HOP CAUSES: the script that starts on world 72 reads homeWorld 71 from the settings again, and
// must not hop the account back. The adoption straight after homeWorld.start is what stops it.
func TestAHopRestartDoesNotUndoTheAssignment(t *testing.T) {
	tag := yewTestTag()
	first := newWorldRig(t, "Keep1_"+tag, 74, "74,75", 74, "mule")
	first.start()
	second := newWorldRig(t, "Keep2_"+tag, 74, "74,75", 74, "mule")
	second.start()
	if second.b.yewW.assigned != 75 {
		t.Fatalf("the second account should be dealt world 75: %d", second.b.yewW.assigned)
	}
	if !second.b.home.step(second.ctx, "wcfletch", time.Now()) || second.lastHop() != 75 {
		t.Fatalf("first run: want a hop to 75, hops %v", second.f.hops)
	}
	// The fleet restarts the script on world 75: a fresh bot, a fresh Context, the same settings (homeWorld 74).
	again := newWorldRig(t, second.who, 75, "74,75", 74, "mule")
	again.start()
	if again.b.yewW.assigned != 75 {
		t.Fatalf("the restarted run was assigned %d", again.b.yewW.assigned)
	}
	for i := 0; i < 3; i++ {
		again.b.yewWorldStep(again.ctx, again.d, time.Now())
		if again.b.home.step(again.ctx, "wcfletch", time.Now()) || len(again.f.hops) != 0 {
			t.Fatalf("pass %d: the restarted account was hopped away from its world: %v", i, again.f.hops)
		}
	}
	if again.f.home != 75 {
		t.Errorf("the fleet's home was left at %d after the restart, want 75", again.f.home)
	}
	// And the lone account of the first run is not disturbed either.
	if first.b.home.step(first.ctx, "wcfletch", time.Now()) {
		t.Error("the account on its world hopped")
	}
}

// THE HAND-OFF TRIP: an account dealt world 76 hops to the mule on world 74, and the script that starts there
// stays (the trip), resumes the hand-off, and goes home when the hand-off is over.
func TestAHandoffTripStaysOnTheMulesWorldThenGoesHome(t *testing.T) {
	tag := yewTestTag()
	other := newWorldRig(t, "Trip0_"+tag, 74, "74,76", 74, "mule")
	other.start() // holds world 74
	r := newWorldRig(t, "Trip1_"+tag, 74, "74,76", 74, "mule")
	r.start()
	if r.b.yewW.assigned != 76 {
		t.Fatalf("assigned %d, want 76", r.b.yewW.assigned)
	}
	// It hopped to 76 and works there. A hand-off falls due: the trip begins, Run hops to the mule on 74.
	r = newWorldRig(t, r.who, 76, "74,76", 74, "mule")
	r.start()
	if r.b.handoffPending {
		t.Fatal("a hand-off pending with no trip")
	}
	r.b.tripBegin(r.ctx)
	r.b.tripEnd(r.ctx, true, false, false) // Run returned: only hopping to the mule's world
	if !wcfletchplan.SharedHandoffTrips().On(r.who, time.Now()) {
		t.Fatal("a run that only hopped to the mule's world ended the trip")
	}
	// The script on the mule's world.
	r = newWorldRig(t, r.who, 74, "74,76", 74, "mule")
	r.start()
	if !r.b.handoffPending || !r.said("resuming the hand-off to") {
		t.Errorf("the restarted script did not resume the hand-off: pending %v, %v", r.b.handoffPending, *r.lines)
	}
	if r.f.home != 76 || r.ctx.HomeWorld != 76 {
		t.Errorf("fleet home %d, ctx.HomeWorld %d: the hop home after the trade goes to the assigned world 76", r.f.home, r.ctx.HomeWorld)
	}
	if r.b.home.step(r.ctx, "wcfletch", time.Now()) || len(r.f.hops) != 0 {
		t.Fatalf("hopped away from the mule's world before the trade: %v", r.f.hops)
	}
	if !r.said("staying until it is done") {
		t.Errorf("the trip should be said: %v", *r.lines)
	}
	// The trade is made; donorrunner hops home and the trip ends.
	r.b.tripEnd(r.ctx, true, true, false)
	r.b.yewWorldStep(r.ctx, r.d, time.Now())
	if !r.b.home.step(r.ctx, "wcfletch", time.Now()) || r.lastHop() != 76 {
		t.Errorf("after the trip the account should be sent home to 76, hops %v", r.f.hops)
	}
	// A hand-off given up ends the trip as well; the user's stop does; one that only hopped does not.
	for _, c := range []struct {
		hopping, traded, gaveUp, stillOn bool
	}{{false, false, true, false}, {true, false, true, false}, {false, false, false, false}, {true, false, false, true}} {
		wcfletchplan.SharedHandoffTrips().Begin(r.who, time.Now())
		r.b.tripEnd(r.ctx, c.hopping, c.traded, c.gaveUp)
		if got := wcfletchplan.SharedHandoffTrips().On(r.who, time.Now()); got != c.stillOn {
			t.Errorf("tripEnd(hopping %v, traded %v, gaveUp %v): trip stands = %v, want %v", c.hopping, c.traded, c.gaveUp, got, c.stillOn)
		}
	}
}

// A hop is held back while the login path is busy with a restart ramp (up to homeWorldCalmWait), but only an
// adopted home waits: the settings' own homeWorld hops at once, as before.
func TestAnAdoptedHopWaitsForAQuietLoginPath(t *testing.T) {
	tag := yewTestTag()
	dealtAway := func(who string, here, there int) *worldRig {
		holder := newWorldRig(t, "Holder"+who, here, fmt.Sprintf("%d,%d", here, there), here, "mule")
		holder.start()
		r := newWorldRig(t, who, here, fmt.Sprintf("%d,%d", here, there), here, "mule")
		r.start()
		if r.b.yewW.assigned != there {
			t.Fatalf("%s was dealt world %d, want %d", who, r.b.yewW.assigned, there)
		}
		return r
	}
	now := time.Now()

	// A busy login path holds the hop for homeWorldCalmWait, then lets it go anyway.
	r := dealtAway("Calm1_"+tag, 77, 78)
	r.busy = true
	if r.b.home.step(r.ctx, "wcfletch", now) || len(r.f.hops) != 0 {
		t.Fatalf("hopped during a login ramp: %v", r.f.hops)
	}
	if r.b.home.step(r.ctx, "wcfletch", now.Add(homeWorldCalmWait-time.Minute)) {
		t.Error("still hopping inside the wait")
	}
	if !r.b.home.step(r.ctx, "wcfletch", now.Add(homeWorldCalmWait+time.Second)) || r.lastHop() != 78 {
		t.Errorf("never hopped after the wait: %v", r.f.hops)
	}

	// A path that goes quiet lets it hop at once.
	c := dealtAway("Calm2_"+tag, 89, 90)
	c.busy = true
	if c.b.home.step(c.ctx, "wcfletch", now) {
		t.Fatal("hopped during a login ramp")
	}
	c.busy = false
	if !c.b.home.step(c.ctx, "wcfletch", now.Add(time.Second)) || c.lastHop() != 90 {
		t.Errorf("a quiet login path should hop at once: %v", c.f.hops)
	}

	// The settings' own home does not wait, as before.
	var h homeWorld
	f := &fakeWorlds{}
	ctx := f.ctx(2, map[string]interface{}{"homeWorld": 1.0})
	ctx.LoginBusy = func() bool { return true }
	h.start(ctx, "fisher")
	if !h.step(ctx, "fisher", now) {
		t.Error("the settings' homeWorld waited for the login ramp")
	}
}

// Off the yew rung the assignment is given back and the settings' home is the home again; back on it,
// a new assignment is made.
func TestOffTheYewRungGivesTheWorldBack(t *testing.T) {
	tag := yewTestTag()
	r := newWorldRig(t, "Rung1_"+tag, 79, "79,80", 79, "mule")
	r.start()
	if r.b.yewW.assigned == 0 {
		t.Fatal("no assignment on the yew rung")
	}
	setSkillLevel(r.sim, woodcutrunner.WoodcuttingStat, 60)
	setSkillLevel(r.sim, wcfletchrunner.FletchingStat, 52)
	r.b.yewWorldStep(r.ctx, r.d, time.Now())
	if r.b.yewW.assigned != 0 || !r.said("off the yew rung") {
		t.Errorf("the maple rung kept a yew world: %d %v", r.b.yewW.assigned, *r.lines)
	}
	if _, ok := wcfletchplan.SharedWorldSpread().World(r.who, time.Now()); ok {
		t.Error("the registry still holds the place")
	}
	if r.b.home.home != 79 || r.b.home.calm {
		t.Errorf("the settings' home should be the home again: %+v", r.b.home)
	}
	setSkillLevel(r.sim, woodcutrunner.WoodcuttingStat, 90)
	setSkillLevel(r.sim, wcfletchrunner.FletchingStat, 81)
	r.b.yewWorldStep(r.ctx, r.d, time.Now())
	if r.b.yewW.assigned == 0 {
		t.Error("back on the yew rung and not assigned")
	}
}

// A fleet that would not take the world as a home, or none behind the Context, switches the spread off for the
// run and leaves the account where the settings put it.
func TestARefusedHomeSwitchesTheSpreadOffForTheRun(t *testing.T) {
	tag := yewTestTag()
	a := newWorldRig(t, "Ref0_"+tag, 81, "81,82", 81, "mule")
	a.start()
	r := newWorldRig(t, "Ref1_"+tag, 81, "81,82", 81, "mule")
	r.ctx.SetHomeWorld = func(int) error { return errors.New("world 82 is not one this deployment has") }
	r.start()
	if r.b.yewW.assigned != 0 || !r.b.yewW.refused || !r.said("would not take world 82") {
		t.Errorf("refused home: assigned %d refused %v %v", r.b.yewW.assigned, r.b.yewW.refused, *r.lines)
	}
	if _, ok := wcfletchplan.SharedWorldSpread().World(r.who, time.Now()); ok {
		t.Error("a refused account still holds a place that other accounts count")
	}
	n := len(*r.lines)
	r.b.yewWorldStep(r.ctx, r.d, time.Now())
	if len(*r.lines) != n {
		t.Error("a refused run kept trying")
	}
	// No fleet at all.
	n2 := newWorldRig(t, "Ref2_"+tag, 81, "81,82", 81, "mule")
	n2.ctx.SetHomeWorld = nil
	n2.start()
	if !n2.b.yewW.refused || !n2.said("no fleet behind this account") {
		t.Errorf("no fleet: %v", *n2.lines)
	}
}

// The setting switches it off, and a list of fewer than two worlds spreads nothing.
func TestYewWorldSpreadSettings(t *testing.T) {
	tag := yewTestTag()
	r := newWorldRig(t, "Off0_"+tag, 83, "83,84", 83, "mule")
	r.b.cfg.SpreadYewWorlds = false
	r.start()
	if r.b.yewW.on || r.b.yewW.assigned != 0 || r.f.home != 83 {
		t.Errorf("spreadYewWorlds=false still spread: %+v home %d", r.b.yewW, r.f.home)
	}
	r.b.yewWorldStep(r.ctx, r.d, time.Now())
	if _, ok := wcfletchplan.SharedWorldSpread().World(r.who, time.Now()); ok {
		t.Error("a switched-off spread holds a place")
	}
	one := newWorldRig(t, "Off1_"+tag, 83, "83", 83, "mule")
	one.start()
	if one.b.yewW.on || !one.said("fewer than two worlds") {
		t.Errorf("one world: %+v %v", one.b.yewW, *one.lines)
	}
	noYewBoard := newWorldRig(t, "Off2_"+tag, 83, "83,84", 83, "mule")
	noYewBoard.b.cfg.SpreadYew = false
	noYewBoard.start()
	if noYewBoard.b.yewW.on {
		t.Error("spreadYew=false must switch the world spread off with it")
	}
	got := wcfletchSettings(map[string]interface{}{"spreadYewWorlds": false, "yewWorlds": "2,3"}).Normalised()
	if got.SpreadYewWorlds || got.YewWorlds != "2,3" {
		t.Errorf("settings not read: %+v", got)
	}
	def := wcfletchSettings(map[string]interface{}{}).Normalised()
	if !def.SpreadYewWorlds || def.YewWorlds != "1,2,3" {
		t.Errorf("defaults: spread %v over %q", def.SpreadYewWorlds, def.YewWorlds)
	}
}

// THE COUNT OF IDLE SECONDS: passes that found no yew are added, the unbroken spell is said once it has lasted
// long enough and then at most every stumpLogEvery, and work ends the spell.
func TestNoteChopPassCountsIdleSecondsAndSaysSoOnce(t *testing.T) {
	tag := yewTestTag()
	r := newWorldRig(t, "Idle0_"+tag, 85, "85,86", 85, "mule")
	tier := wcfletchplan.YewCamps[0].Tier()
	r.b.idleClock.Worked(time.Now())
	stumps := func() int {
		n := 0
		for _, l := range *r.lines {
			if strings.Contains(l, "is a stump on world 85") {
				n++
			}
		}
		return n
	}
	r.b.noteChopPass(r.ctx, r.d, tier, "no-tree", 8*time.Second)
	r.b.noteChopPass(r.ctx, r.d, tier, "no-tree", 8*time.Second)
	if stumps() != 0 {
		t.Fatalf("said so after 16 s: %v", *r.lines)
	}
	r.b.noteChopPass(r.ctx, r.d, tier, "no-tree", 8*time.Second)
	if stumps() != 1 || !r.said("is a stump on world 85 (24s so far; they regrow in about 111s)") {
		t.Fatalf("want one line after 24 s: %v", *r.lines)
	}
	r.b.noteChopPass(r.ctx, r.d, tier, "no-tree", 8*time.Second)
	if stumps() != 1 {
		t.Errorf("said so again inside %s: %v", stumpLogEvery, *r.lines)
	}
	r.b.noteChopPass(r.ctx, r.d, tier, "full", time.Second)
	if r.b.idleClock.Spell() != 0 {
		t.Error("a pass that chopped did not end the spell")
	}
	// Excluded trees are idle time of their own kind.
	r.b.stumpLogAt = time.Time{}
	r.b.noteChopPass(r.ctx, r.d, tier, "all-blocked", 30*time.Second)
	if !r.said("is excluded on world 85") {
		t.Errorf("an excluded spell should say excluded: %v", *r.lines)
	}
	rep := r.b.idleClock.Take(time.Now().Add(time.Minute))
	if rep.Stump != 32*time.Second || rep.Excluded != 30*time.Second {
		t.Errorf("report %+v, want 32s of stumps and 30s excluded", rep)
	}
}

// The quarter-hour report: the account's own line with the counters of the window, and a move to a lighter
// world for the account that lost enough of it.
func TestReportYewIdleSaysTheWindowAndRebalances(t *testing.T) {
	tag := yewTestTag()
	var others []*worldRig
	for i := 0; i < 4; i++ {
		o := newWorldRig(t, fmt.Sprintf("Rep%d_%s", i, tag), 87, "87,88", 87, "mule")
		o.start()
		others = append(others, o)
	}
	// Dealt 87, 88, 87, 88. Put the whole cohort on 87 by hand: the account asking is the idle one.
	reg := wcfletchplan.SharedWorldSpread()
	for _, o := range others[1:] {
		reg.Release(o.who)
		reg.Assign(o.who, []int{87}, 87, nil, time.Now())
	}
	idle := others[0]
	idle.b.chopped, idle.b.fletched = 120, 118
	start := time.Now()
	idle.b.idleClock.Worked(start)
	idle.b.idleClock.Pass(wcfletchplan.IdleStumps, 5*time.Minute, start)
	idle.b.reportYewIdle(idle.ctx, idle.d, start.Add(time.Minute))
	if idle.said("idle over the last") {
		t.Fatal("reported before the window was up")
	}
	idle.b.reportYewIdle(idle.ctx, idle.d, start.Add(wcfletchplan.IdleReportEvery))
	for _, want := range []string{"idle over the last 15 min on world 87", "300s waiting = 33.3% of 900s", "120 logs chopped, 118 cut into bows or shafts", "moving to world 88"} {
		if !idle.said(want) {
			t.Errorf("%q is missing from %v", want, *idle.lines)
		}
	}
	if w, _ := reg.World(idle.who, time.Now()); w != 88 {
		t.Errorf("the idle account is on world %d in the registry, want 88", w)
	}
	// The next Loop pass makes it the script's home and hops.
	idle.b.yewWorldStep(idle.ctx, idle.d, time.Now())
	if !idle.b.home.step(idle.ctx, "wcfletch", time.Now()) || idle.lastHop() != 88 {
		t.Errorf("not hopped to 88: %v", idle.f.hops)
	}
	// The window restarted: the counters are deltas.
	idle.b.chopped, idle.b.fletched = 150, 140
	idle.b.idleClock.Pass(wcfletchplan.IdleStumps, time.Second, start.Add(wcfletchplan.IdleReportEvery))
	idle.b.reportYewIdle(idle.ctx, idle.d, start.Add(2*wcfletchplan.IdleReportEvery))
	if !idle.said("30 logs chopped, 22 cut into bows or shafts") {
		t.Errorf("the second window should report its own counters: %v", *idle.lines)
	}
}
