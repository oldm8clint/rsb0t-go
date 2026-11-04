package fleetrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The mule's post and the Feldip ogre pens: the walk the teleport to the mule was first built for.
var (
	hookPens   = webwalk.Point{X: 2512, Z: 3104}
	hookPost   = webwalk.Point{X: 2809, Z: 3441}
	hookCamelt = webwalk.Point{X: 2757, Z: 3478}
)

// hookOutcome is what the simulated server does with one cast: land the account (the default), or leave it where it stands and
// say something (or nothing).
type hookOutcome struct {
	stay bool
	say  string
}

// hookRig is an account at the Feldip pens with Magic 56, a Staff of air worn and ten law runes, whose casts the test scripts.
type hookRig struct {
	t      *testing.T
	cfg    *cachecfg.Config
	sim    *actionsim.Session
	d      *Deps
	tr     *Travel
	chat   *chatlog.Log
	clock  time.Time
	logs   []string
	castAt []time.Time
	script []hookOutcome
	camCom int

	plans    int
	planCost int
	planOK   bool
	// cl, when set, is the Walker's client: a landing moves it too.
	cl *tickClient
}

func newHookRig(t *testing.T, tele fleetplan.TeleTravelSettings) *hookRig {
	t.Helper()
	cfg := travelTestConfig(t)
	r := &hookRig{t: t, cfg: cfg, chat: chatlog.New(), clock: time.Unix(2_000_000, 0), planCost: 60, planOK: true}
	r.camCom, _ = actions.TeleportComponent("Camelot Teleport")
	r.sim = actionsim.New(certObjs(cfg), nil)
	r.sim.ShowTabs()
	runningAt(r.sim)
	simPlace(r.sim.State(), hookPens.X, hookPens.Z)
	setStat(r.sim, fleetplan.MagicStat, 200000, 56)
	r.sim.Give(objOf(t, cfg, "Law rune", false), 10)
	r.sim.Wear(actionsim.RightHand, objOf(t, cfg, "Staff of air", false))
	r.d = &Deps{Session: r.sim, Config: cfg, Components: actionsim.TabComponents(), Chat: r.chat,
		Log:   func(s string) { r.logs = append(r.logs, s) },
		Delay: func(d time.Duration) bool { r.clock = r.clock.Add(d); return true }}
	tele.Enabled = true
	r.tr = &Travel{Tele: tele.Normalised(), Members: true, Now: func() time.Time { return r.clock },
		MuleLaws: func() (int, bool) { return 33673, true }} // the mule's stock on 13 Oct: the cheapest price
	r.d.Travel = r.tr
	r.sim.OnSend = func(b []byte) {
		if len(b) != 3 || b[0] != byte(clientprot.IF_BUTTON) || int(b[1])<<8|int(b[2]) != r.camCom {
			return
		}
		r.castAt = append(r.castAt, r.clock)
		out := hookOutcome{}
		if n := len(r.castAt) - 1; n < len(r.script) {
			out = r.script[n]
		}
		if out.say != "" {
			r.chat.Record(out.say)
		}
		if !out.stay {
			simPlace(r.sim.State(), hookCamelt.X, hookCamelt.Z)
			if r.cl != nil {
				r.cl.me = hookCamelt
			}
		}
	}
	return r
}

// query is the BeforeFollow query for a walk from the pens to the post whose route costs hereCost, and whose landings cost
// r.planCost to walk from.
func (r *hookRig) query(hereCost int) webwalk.FollowQuery {
	route := func(from webwalk.Point, cost int, ok bool) webwalk.Route {
		return webwalk.Route{OK: ok, Cost: cost, Waypoints: []webwalk.Step{{Point: from}, {Point: hookPost}}}
	}
	return webwalk.FollowQuery{
		Here: hookPens, Dest: hookPost, Route: route(hookPens, hereCost, true),
		PlanFrom: func(from webwalk.Point) webwalk.Route {
			r.plans++
			return route(from, r.planCost, r.planOK)
		},
		PlanKey: func() uint64 { return 7 },
	}
}

func (r *hookRig) hook() func(webwalk.FollowQuery) webwalk.BeforeFollowResult {
	return r.tr.TeleportHook(func() *Deps { return r.d })
}

func (r *hookRig) logged(sub string) bool {
	for _, l := range r.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// THE OWNER'S WORD, 13 Oct 2026: teleports are considered on every walk, not only the one to the mule. With any-walk on, a walk
// that is not the mule's casts when the planner's own price says it saves ticks.
func TestHookCastsFromAnyWalkNotJustTheMule(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	res := r.hook()(r.query(450))
	if !res.Moved || !res.Planned || len(r.castAt) != 1 {
		t.Fatalf("result %+v after %d cast(s), want one cast that moved the account", res, len(r.castAt))
	}
	if r.tr.Teleports != 1 || r.tr.Saved < 100 {
		t.Errorf("counters: %d teleports saving %d ticks", r.tr.Teleports, r.tr.Saved)
	}
	if !r.logged("teleport travel: Camelot Teleport from 2512,3104 toward 2809,3441: saves ") || !r.logged("landing walk") {
		t.Errorf("log %q", r.logs)
	}
	if r.tr.lastTele.Landing != (tile.Tile{X: 2757, Z: 3478}) || r.tr.teleCasts.Count(r.clock, time.Hour) != 1 {
		t.Errorf("last cast %+v", r.tr.lastTele)
	}
	if r.plans != 1 {
		t.Errorf("%d landings priced, want 1", r.plans)
	}
}

// With any-walk off (the 6 Oct rollback) only the walk to the mule's post casts, by the same planner price.
func TestWalkToMuleWithAnyWalkOffStillCastsAndOtherWalksDoNot(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{})
	res := r.hook()(r.query(450))
	if res.Moved || len(r.castAt) != 0 || r.plans != 0 {
		t.Fatalf("an ordinary walk with any-walk off: %+v, %d casts, %d plans", res, len(r.castAt), r.plans)
	}
	cast := r.tr.ForMuleWalk(func() bool { return r.hook()(r.query(450)).Moved })
	if !cast || len(r.castAt) != 1 {
		t.Fatalf("the walk to the mule: moved %v after %d cast(s)", cast, len(r.castAt))
	}
	// Travel off, or no staff of casts at all: nothing.
	off := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	off.tr.Tele.Enabled = false
	if off.hook()(off.query(450)).Moved || len(off.castAt) != 0 {
		t.Error("teleTravel off still cast")
	}
}

// A short walk answers before anything is priced or read: the route's own cost says a run of it is no more than a cast.
func TestHookDeclinesShortWalksWithoutPricingAnything(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	for _, cost := range []int{40, 59} {
		res := r.hook()(r.query(cost))
		if res.Moved || res.Planned || r.plans != 0 || len(r.castAt) != 0 {
			t.Fatalf("a walk costing %d: %+v, %d plans, %d casts", cost, res, r.plans, len(r.castAt))
		}
	}
	if len(r.logs) != 0 {
		t.Errorf("a short walk said %q", r.logs)
	}
}

// THE SILENT DROP. A delayed player (its own walk alchemy cast, a rub) drops the spell click with no message, and the old
// code read that as a refusal and blocked the spell for an hour. The click waits for the delay to end.
func TestHookWaitsForTheSettleAfterAnAlchCast(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	tick := 600 * time.Millisecond
	r.tr.lastCast = r.clock.Add(-2 * tick) // an alchemy cast two ticks ago
	if !r.hook()(r.query(450)).Moved {
		t.Fatal("no cast")
	}
	if gap := r.castAt[0].Sub(r.tr.lastCast); gap < 5*tick {
		t.Errorf("the spell click went %v after the alchemy cast, want at least 5 ticks (%v)", gap, 5*tick)
	}
	// A rub two ticks ago holds it the same way.
	r = newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.tr.lastRub = r.clock.Add(-1 * tick)
	if !r.hook()(r.query(450)).Moved {
		t.Fatal("no cast after a rub")
	}
	if gap := r.castAt[0].Sub(r.tr.lastRub); gap < 5*tick {
		t.Errorf("the spell click went %v after the rub, want at least 5 ticks", gap)
	}
}

// A cast that moves nothing and says nothing is a dropped click or an AFK event with no NPC yet: settle, recast once, and never
// block the spell for an hour.
func TestSilentDropRetriesOnceAndDoesNotBlock(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.script = []hookOutcome{{stay: true}}
	res := r.hook()(r.query(450))
	if !res.Moved || len(r.castAt) != 2 {
		t.Fatalf("%+v after %d cast(s), want the recast to land", res, len(r.castAt))
	}
	if r.tr.blocked("Camelot Teleport") || len(r.tr.teleBlocked) != 0 {
		t.Error("a silent drop blocked the spell")
	}
	if !r.logged("silent drop: settled and recast") {
		t.Errorf("log %q", r.logs)
	}
	if r.tr.Teleports != 1 {
		t.Errorf("%d teleports counted", r.tr.Teleports)
	}
	// Twice in a row: a short pause, not an hour, and the walk goes on foot.
	r = newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.script = []hookOutcome{{stay: true}, {stay: true}}
	if res := r.hook()(r.query(450)); res.Moved || len(r.castAt) != 2 {
		t.Fatalf("two dropped casts: %+v after %d", res, len(r.castAt))
	}
	until := r.tr.teleBlocked["Camelot Teleport"]
	if pause := until.Sub(r.clock); pause <= 0 || pause > 3*time.Minute {
		t.Errorf("paused for %v, want about two minutes", pause)
	}
	r.clock = r.clock.Add(3 * time.Minute)
	if r.tr.blocked("Camelot Teleport") {
		t.Error("the pause outlived three minutes")
	}
}

// An AFK random event takes the cast's turn: a message nobody here knows (or an event in view), no move. The Guardian is waited
// out and the cast tried once more; the flag is spent, so the second lands.
func TestAfkEventRetriesAfterTheGuardianSettles(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.script = []hookOutcome{{stay: true, say: "You have been given a mysterious box!"}}
	res := r.hook()(r.query(450))
	if !res.Moved || len(r.castAt) != 2 {
		t.Fatalf("%+v after %d cast(s)", res, len(r.castAt))
	}
	if !r.logged("afk event: one recast") {
		t.Errorf("log %q", r.logs)
	}
	if len(r.tr.teleBlocked) != 0 {
		t.Error("an AFK event blocked the spell")
	}
}

// A refusal that will not change in minutes blocks the spell for an hour, and the next decision does not cast it.
func TestPersistentRefusalBlocks60Minutes(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.script = []hookOutcome{{stay: true, say: "You haven't learnt how to cast this spell yet."}}
	if res := r.hook()(r.query(450)); res.Moved || len(r.castAt) != 1 {
		t.Fatalf("%+v after %d cast(s): a persistent refusal is not retried", res, len(r.castAt))
	}
	if !r.tr.blocked("Camelot Teleport") {
		t.Fatal("the spell is not blocked")
	}
	if res := r.hook()(r.query(450)); res.Moved || len(r.castAt) != 1 {
		t.Errorf("the blocked spell was cast again: %+v after %d", res, len(r.castAt))
	}
	r.clock = r.clock.Add(61 * time.Minute)
	if r.tr.blocked("Camelot Teleport") {
		t.Error("the block outlived an hour")
	}
	if !r.logged("did not land (quest:") {
		t.Errorf("log %q", r.logs)
	}
}

// A refusal for want of runes blocks the spell until the pack holds more of that rune.
func TestRunesRefusalBlocksUntilTheCountRises(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.script = []hookOutcome{{stay: true, say: "You do not have enough Law Runes to cast this spell."}}
	if res := r.hook()(r.query(450)); res.Moved {
		t.Fatalf("%+v", res)
	}
	if res := r.hook()(r.query(450)); res.Moved || len(r.castAt) != 1 {
		t.Fatalf("the spell was cast again with the same runes: %+v after %d", res, len(r.castAt))
	}
	r.sim.Give(objOf(t, r.cfg, "Law rune", false), 1)
	if res := r.hook()(r.query(450)); !res.Moved || len(r.castAt) != 2 {
		t.Errorf("one more law rune did not lift the block: %+v after %d", res, len(r.castAt))
	}
}

// A refusal for level holds until Magic rises.
func TestLevelRefusalBlocksUntilMagicRises(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.script = []hookOutcome{{stay: true, say: "Your Magic level is not high enough for this spell."}}
	r.hook()(r.query(450))
	if res := r.hook()(r.query(450)); res.Moved || len(r.castAt) != 1 {
		t.Fatalf("cast again at the same level: %+v after %d", res, len(r.castAt))
	}
	setStat(r.sim, fleetplan.MagicStat, 300000, 57)
	if res := r.hook()(r.query(450)); !res.Moved {
		t.Errorf("Magic 57 did not lift a refusal at 56: %+v", res)
	}
}

func TestClassifyRefusal(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines []string
		event bool
		want  refusal
		rune  string
	}{
		{"members", []string{"You need to be on a members' server to use this spell."}, false, refusedMembers, ""},
		{"level", []string{"Your Magic level is not high enough for this spell."}, false, refusedLevel, ""},
		{"law runes", []string{"You do not have enough Law Runes to cast this spell."}, false, refusedRunes, "Law rune"},
		{"air runes, any case", []string{"you do not have enough AIR runes to cast this spell."}, false, refusedRunes, "Air rune"},
		{"plague city", []string{"You must have completed Plague City to use this spell."}, false, refusedQuest, ""},
		{"watchtower", []string{"You need to have completed the Watchtower Quest to use this spell."}, false, refusedQuest, ""},
		{"not learnt", []string{"You haven't learnt how to cast this spell yet."}, false, refusedQuest, ""},
		{"the Wilderness", []string{"A mysterious force blocks your teleport spell!"}, false, refusedWilderness, ""},
		{"a refusal among other lines", []string{"You feel a gust.", "Your Magic level is not high enough for this spell."}, false, refusedLevel, ""},
		{"a message nobody knows is an event", []string{"A box falls out of the sky."}, false, refusedEvent, ""},
		{"an event in view and no message", nil, true, refusedEvent, ""},
		{"a known refusal beats an event in view", []string{"Your Magic level is not high enough for this spell."}, true, refusedLevel, ""},
		{"nothing at all", nil, false, refusedUnknown, ""},
		{"blank lines", []string{"", "   "}, false, refusedUnknown, ""},
	} {
		got, rune := classifyRefusal(c.lines, c.event)
		if got != c.want || rune != c.rune {
			t.Errorf("%s: %v %q, want %v %q", c.name, got, rune, c.want, c.rune)
		}
	}
	// Neither quiet class is an hour's block.
	if refusedEvent.retry() == "" || refusedUnknown.retry() == "" {
		t.Error("a quiet class has no retry")
	}
}

// A landing's price is remembered per landing, walk and coin band for ten minutes; one with no route is remembered for two.
func TestLandCostMemoisedByZoneHashAndNegativeCached2Minutes(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.planCost = 80 // 130 units from here at full energy is 65 ticks; 80 from the landing is 46: it saves 19, under the margin
	h := r.hook()
	for i := 0; i < 3; i++ {
		if res := h(r.query(130)); res.Moved || len(r.castAt) != 0 {
			t.Fatalf("a cast that saves too little went out: %+v", res)
		}
	}
	if r.plans != 1 {
		t.Errorf("%d planner runs for three decisions about the same walk, want 1 (the memo)", r.plans)
	}
	if !r.logged("declined (saves too little)") {
		t.Errorf("log %q", r.logs)
	}
	// Another walk (a different plan key) is priced again.
	q := r.query(130)
	q.PlanKey = func() uint64 { return 8 }
	h(q)
	if r.plans != 2 {
		t.Errorf("%d planner runs after a different walk, want 2", r.plans)
	}
	// A landing with no route is cached for two minutes only.
	r = newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.planOK = false
	h = r.hook()
	h(r.query(450))
	h(r.query(450))
	if r.plans != 1 {
		t.Errorf("%d planner runs for two decisions with an unreachable landing, want 1", r.plans)
	}
	r.clock = r.clock.Add(3 * time.Minute)
	r.planOK = true
	if res := h(r.query(450)); !res.Moved {
		t.Errorf("an unreachable landing was still remembered after three minutes: %+v", res)
	}
	if r.plans != 2 {
		t.Errorf("%d planner runs, want 2", r.plans)
	}
}

// A cast the account could not pay for is a demand signal, once, when it would have saved a lot.
func TestHookRecordsTheMissedSavingOfAnUnpayableCast(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.sim.Remove(objOf(t, r.cfg, "Law rune", false), 10) // no laws left
	h := r.hook()
	if res := h(r.query(450)); res.Moved || len(r.castAt) != 0 {
		t.Fatalf("cast with no laws: %+v", res)
	}
	if got := r.tr.MissedTicks(); got < 200 {
		t.Errorf("missed %d ticks, want the lower bound of a Camelot cast (about 250)", got)
	}
	h(r.query(450)) // the same walk again within the spacing: one entry
	first := r.tr.MissedTicks()
	h(r.query(450))
	if r.tr.MissedTicks() != first {
		t.Errorf("one walk's repeated decisions added entries: %d then %d", first, r.tr.MissedTicks())
	}
	r.clock = r.clock.Add(3*time.Hour + time.Minute)
	if r.tr.MissedTicks() != 0 {
		t.Error("a missed saving outlived its window")
	}
}

// The loop guard: standing at the landing the hook declines, and a walk that was just cast for is not cast for again.
func TestHookNeverCastsTwiceForOneWalk(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	h := r.hook()
	if !h(r.query(450)).Moved {
		t.Fatal("the first walk did not cast")
	}
	// At the landing, 52 tiles from the post, with the route's cost down to that.
	q := r.query(52)
	q.Here = hookCamelt
	if res := h(q); res.Moved || len(r.castAt) != 1 {
		t.Errorf("a second decision at the landing: %+v after %d cast(s)", res, len(r.castAt))
	}
}

// A cast resets the walk's previous tile, so the 300-tile jump is not counted as walked and does not feed the zone trigger.
func TestCastResetsWalkPrevAndTheWalkLog(t *testing.T) {
	r := newHookRig(t, fleetplan.TeleTravelSettings{AnyWalk: true})
	r.tr.walkPrev, r.tr.walkPrevOK = tile.Tile{X: 2512, Z: 3104}, true
	if !r.hook()(r.query(450)).Moved {
		t.Fatal("no cast")
	}
	if r.tr.walkPrevOK {
		t.Error("walkPrevOK survived the cast: the next poll would count the jump as walked tiles")
	}
}

// hookWalk is a Walker over a walkable block of the map holding the pens, Camelot and the post, whose client moves with the
// simulated session.
func hookWalk(t *testing.T, tele fleetplan.TeleTravelSettings, muleLaws int, install bool) (*hookRig, *Deps) {
	t.Helper()
	r := newHookRig(t, tele)
	cl := &tickClient{me: hookPens, now: r.clock}
	r.cl = cl
	r.d.Delay = cl.Delay
	r.tr.Now = cl.Now
	r.tr.MuleLaws = func() (int, bool) { return muleLaws, true }
	w := webwalk.NewWalker(webwalk.NewPlanner(flatPlane{2400, 2900, 3000, 3500}, &webwalk.Graph{}), cl)
	r.d.Walker = w
	r.d.Pack = nil
	if install {
		r.tr.InstallTeleportHook(w, func() *Deps { return r.d })
	}
	return r, r.d
}

// THE OLD CHECK IS NOT A SECOND OPINION. With the hook on the Walker, scarabrunner.WalkTo leaves the Chebyshev estimate alone: a
// walk the hook declines (the mule's laws are low, so only the mule walk may cast) is not cast by the old check either.
func TestWalkToNoLongerCastsTwice(t *testing.T) {
	post := tile.Tile{X: hookPost.X, Z: hookPost.Z}
	// Control: no hook on the Walker, the old check casts.
	r, d := hookWalk(t, fleetplan.TeleTravelSettings{AnyWalk: true}, 500, false)
	if !WalkTo(d, post, 3, time.Hour) {
		t.Fatal("the control walk did not arrive")
	}
	if len(r.castAt) != 1 {
		t.Fatalf("the old pre-walk check cast %d time(s) with no hook installed, want 1", len(r.castAt))
	}
	// With the hook installed the old check stays out of it.
	r, d = hookWalk(t, fleetplan.TeleTravelSettings{AnyWalk: true}, 500, true)
	if !WalkTo(d, post, 3, time.Hour) {
		t.Fatal("the hooked walk did not arrive")
	}
	if len(r.castAt) != 0 {
		t.Errorf("the old pre-walk check cast %d time(s) beside the hook's decision", len(r.castAt))
	}
	// And with the laws plentiful the hook casts once, replans from the landing and walks the rest.
	r, d = hookWalk(t, fleetplan.TeleTravelSettings{AnyWalk: true}, 33673, true)
	if !WalkTo(d, post, 3, time.Hour) {
		t.Fatal("the walk did not arrive")
	}
	if len(r.castAt) != 1 {
		t.Errorf("%d casts, want 1", len(r.castAt))
	}
	if r.cl.me.X < 2780 {
		t.Errorf("ended at %s", r.cl.me)
	}
}

// A quest leg decides before it walks (BeforeTravel): the preview prices the walk through the Walker, casts once, and the
// Walker's own hook, finding the account at the landing, does not cast again.
func TestPreviewCastMovesOnceAndTheWalkersHookDeclines(t *testing.T) {
	r, d := hookWalk(t, fleetplan.TeleTravelSettings{AnyWalk: true}, 33673, true)
	post := tile.Tile{X: hookPost.X, Z: hookPost.Z}
	if !r.tr.PreviewCast(d, d.Walker, post) {
		t.Fatalf("the preview did not cast (log %q)", r.logs)
	}
	if len(r.castAt) != 1 || r.cl.me != hookCamelt {
		t.Fatalf("%d casts, ended at %s", len(r.castAt), r.cl.me)
	}
	res := d.Walker.WalkTo(webwalk.Point{X: post.X, Z: post.Z}, webwalk.WalkOptions{Radius: 3, Timeout: time.Hour})
	if !res.Arrived() || len(r.castAt) != 1 {
		t.Errorf("walk %+v after %d casts, want arrival with no second cast", res, len(r.castAt))
	}
}
