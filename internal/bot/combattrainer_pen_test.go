package bot

// The CombatTrainer at the cows rung, a fenced pen (combatplan/pen.go), on the real Lumbridge map: the account is at the rung only
// inside the fence, it walks in by the gate, it picks only the cows a walk over open ground reaches, and it walks the pen's beat when
// none is in reach. The rung used to count the road outside the fence as at the rung, and picked and waited from there.

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// penWalkClient is a webwalk.Client over a gamestate: a walk puts the account on the last tile of its path, and the pasture's gate is
// shut until it is operated.
type penWalkClient struct {
	st           *gamestate.State
	baseX, baseZ int
	now          time.Time
	opened       bool
	ops          int
}

func (c *penWalkClient) Tile() (webwalk.Point, bool) {
	x, z, level, ok := c.st.Tile()
	return webwalk.Point{X: x, Z: z, Level: level}, ok
}

func (c *penWalkClient) Walk(path []webwalk.Point, run bool) error {
	if len(path) > 0 {
		last := path[len(path)-1]
		placeAt(c.st, last.Level, last.X-c.baseX, last.Z-c.baseZ)
	}
	return nil
}

func (c *penWalkClient) FindLocs(q webwalk.LocQuery) []webwalk.LocMatch {
	if c.opened || q.At.Level != 0 {
		return nil
	}
	var out []webwalk.LocMatch
	for _, g := range []webwalk.LocMatch{{ID: 1551, X: 3253, Z: 3266, Name: "Gate", Op: 1}, {ID: 1553, X: 3253, Z: 3267, Name: "Gate", Op: 1}} {
		if penAbs(g.X-q.At.X) > q.Radius || penAbs(g.Z-q.At.Z) > q.Radius {
			continue
		}
		match := len(q.IDs) == 0
		for _, id := range q.IDs {
			match = match || id == g.ID
		}
		if match {
			out = append(out, g)
		}
	}
	return out
}

func (c *penWalkClient) OpLoc(op, x, z, id int) error {
	c.ops++
	if (id == 1551 && x == 3253 && z == 3266) || (id == 1553 && x == 3253 && z == 3267) {
		c.opened = true
	}
	return nil
}

func (c *penWalkClient) CantReachSince(time.Time) bool { return false }
func (c *penWalkClient) Delay(d time.Duration) bool    { c.now = c.now.Add(d); return true }
func (c *penWalkClient) Now() time.Time                { return c.now }

func penAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// penTrainer is a CombatTrainer at the cows rung with its account at x,z and the real map under it. moving gives it a walker that
// walks (and a gate that opens); without, the walker only plans and a walk it tries runs out its simulated timeout.
func penTrainer(t *testing.T, x, z int, moving bool) (*combatTrainerBot, *Context, *combatrunner.Deps, *apiSession, *penWalkClient, *[]string) {
	t.Helper()
	loadDoorTestData(t)
	sess := newAPISession()
	rebuildAt(sess.st, x/8, z/8)
	baseX, baseZ := (x/8-6)*8, (z/8-6)*8
	placeAt(sess.st, 0, x-baseX, z-baseZ)
	var logged []string
	ctx := apiContext(sess, func(s string) { logged = append(logged, s) })
	rung, ok := combatplan.RungByKey(combatplan.MeleeLadder(), "cows")
	if !ok {
		t.Fatal("the ladder has no cows rung")
	}
	b := &combatTrainerBot{
		rung: rung, haveRung: true,
		warnedAt:    map[string]time.Time{},
		unreachable: map[int]time.Time{},
		said:        map[string]bool{},
	}
	client := &penWalkClient{st: sess.st, baseX: baseX, baseZ: baseZ, now: time.Unix(1_000_000, 0)}
	var wc webwalk.Client = client
	if !moving {
		wc = &planOnlyClient{st: sess.st, now: time.Unix(1_000_000, 0)}
	}
	d := &combatrunner.Deps{
		Session: sess, Config: doorTestData.cfg, Pack: doorTestData.pk, Locs: doorTestData.ix,
		Walker: webwalk.NewWalker(doorTestData.planner, wc),
		Delay:  func(time.Duration) bool { return true },
		Doors:  true,
		Log:    func(s string) { logged = append(logged, s) },
	}
	return b, ctx, d, sess, client, &logged
}

func cowType(t *testing.T) int {
	t.Helper()
	ids := doorTestData.cfg.NpcIDs("Cow")
	if len(ids) == 0 || ids[0] > 2047 {
		t.Fatalf("Cow ids %v", ids)
	}
	return ids[0]
}

func acctTile(sess *apiSession) tile.Tile {
	x, z, level, _ := sess.st.Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// The road outside the fence is within the rung's leash and is not the rung. This is the reproduction: Chebyshev alone, the old
// reading, calls every one of these tiles at the rung.
func TestAtTheCowsRungMeansInsideThePen(t *testing.T) {
	for _, c := range []struct {
		name   string
		at     tile.Tile
		atRung bool
	}{
		{"inside, by the entry", tile.Tile{X: 3258, Z: 3266}, true},
		{"inside, mid pen", tile.Tile{X: 3256, Z: 3277}, true},
		{"inside, the north-west corner", tile.Tile{X: 3249, Z: 3288}, true},
		{"the road beside the gate", tile.Tile{X: 3252, Z: 3267}, false},
		{"the road south-west of it", tile.Tile{X: 3250, Z: 3258}, false},
		{"the road across the river bank", tile.Tile{X: 3244, Z: 3268}, false},
		{"north of the pen's fence", tile.Tile{X: 3256, Z: 3299}, false},
	} {
		b, ctx, d, _, _, _ := penTrainer(t, c.at.X, c.at.Z, false)
		if got := b.atRung(ctx, d); got != c.atRung {
			t.Errorf("%s (%v): atRung = %v, want %v", c.name, c.at, got, c.atRung)
		}
		if !c.atRung && c.at.Z < 3299 {
			// The reproduction: floor within the leash, which is all the old reading asked.
			if !doorTestData.pk.Walkable(c.at.X, c.at.Z, 0) || tile.Chebyshev(c.at, b.rung.Stand) > b.rung.Leash {
				t.Errorf("%s (%v): fixture: not floor within the %d leash of %v, so the distance alone would not have called it the rung", c.name, c.at, b.rung.Leash, b.rung.Stand)
			}
		}
		stray := b.strayedFromPen(ctx, d)
		if stray == c.atRung {
			t.Errorf("%s (%v): strayedFromPen = %v with atRung %v", c.name, c.at, stray, c.atRung)
		}
	}
	// Another rung is unchanged: no pen, no stray, the leash alone.
	b, ctx, d, _, _, _ := penTrainer(t, 3252, 3267, false)
	alk, _ := combatplan.RungByKey(combatplan.MeleeLadder(), "alkharid")
	b.rung = alk
	if b.penOf(d) != nil || b.strayedFromPen(ctx, d) || b.walkBeat(ctx, d) {
		t.Error("the Al Kharid rung acted as a fenced pen")
	}
}

// From the road no cow is picked, whatever stands nearest; from inside the cow nearest by the walk is, and between equals the lower
// NPC index.
func TestPickTargetAtTheCowsRungTakesTheNearestCowByTheWalk(t *testing.T) {
	// The account stands just inside the west fence. A cow on the road side of the fence is nearest by the straight line (2 tiles)
	// and is no walk; of the two inside, 3254,3258 is nearer by the walk than 3261,3270.
	b, ctx, d, sess, _, logged := penTrainer(t, 3254, 3262, false)
	typ := cowType(t)
	placeDoorNPCs(sess.st, typ, 3254, 3262, [3]int{101, 3252, 3262}, [3]int{102, 3261, 3270}, [3]int{103, 3254, 3258})
	n, ok := b.pickTarget(ctx, d)
	if !ok || n.Index != 103 {
		t.Fatalf("picked %+v (ok=%v), want the cow 103 at 3254,3258", n, ok)
	}
	if got := strings.Join(*logged, "\n"); !strings.Contains(got, "no walk over open ground reaches it") {
		t.Errorf("the cow across the fence is not reported as having no walk:\n%s", got)
	}

	// Two equal walks: the lower index.
	b2, ctx2, d2, sess2, _, _ := penTrainer(t, 3258, 3266, false)
	placeDoorNPCs(sess2.st, typ, 3258, 3266, [3]int{210, 3256, 3266}, [3]int{209, 3260, 3266})
	if n, ok := b2.pickTarget(ctx2, d2); !ok || n.Index != 209 {
		t.Fatalf("picked %+v (ok=%v) between two equal walks, want the lower index 209", n, ok)
	}

	// From the road beside the gate every cow in view is behind the fence: none is picked, though the gate is two tiles away.
	b3, ctx3, d3, sess3, _, _ := penTrainer(t, 3252, 3267, false)
	placeDoorNPCs(sess3.st, typ, 3252, 3267, [3]int{301, 3254, 3262}, [3]int{302, 3258, 3260}, [3]int{303, 3261, 3270})
	if n, ok := b3.pickTarget(ctx3, d3); ok {
		t.Fatalf("picked %+v from the road, want none: every cow is behind the fence", n)
	}
}

// The cows a pick passes over are not asked about again for the usual minute, and a cow at a spawn the pack has no walk to is passed
// over without waiting for one.
func TestPickTargetPassesOverAThePenCowItAlreadyFailed(t *testing.T) {
	b, ctx, d, sess, _, _ := penTrainer(t, 3258, 3266, false)
	typ := cowType(t)
	placeDoorNPCs(sess.st, typ, 3258, 3266, [3]int{401, 3258, 3262}, [3]int{402, 3261, 3270})
	if n, ok := b.pickTarget(ctx, d); !ok || n.Index != 401 {
		t.Fatalf("picked %+v (ok=%v), want the nearer cow 401", n, ok)
	}
	b.noteUnreachable(ctx, gamestate.NPC{Index: 401})
	if n, ok := b.pickTarget(ctx, d); !ok || n.Index != 402 {
		t.Fatalf("picked %+v (ok=%v) after 401 was passed over, want 402", n, ok)
	}
}

// travelToRung walks an account on the road in by the gate, once, and leaves one inside where it is.
func TestTravelToTheCowsRungEntersByTheGate(t *testing.T) {
	for name, at := range map[string]tile.Tile{
		"the road beside the gate":   {X: 3252, Z: 3267},
		"the road south of the gate": {X: 3250, Z: 3258},
		"north of the pen":           {X: 3256, Z: 3299},
	} {
		b, ctx, d, sess, client, logged := penTrainer(t, at.X, at.Z, true)
		if b.atRung(ctx, d) {
			t.Fatalf("%s: fixture: %v is at the rung", name, at)
		}
		if !b.travelToRung(ctx, d) {
			t.Fatalf("%s: travelToRung failed; log:\n%s", name, strings.Join(*logged, "\n"))
		}
		if !client.opened {
			t.Errorf("%s: the gate was never operated", name)
		}
		if !b.atRung(ctx, d) {
			t.Errorf("%s: the account ended at %v, not at the rung", name, acctTile(sess))
		}
		if got := strings.Join(*logged, "\n"); !strings.Contains(got, "outside the pen: walking in by the gate") {
			t.Errorf("%s: the walk in is not in the log:\n%s", name, got)
		}
	}

	in, ctx, d, sess, client, logged := penTrainer(t, 3257, 3280, true)
	if !in.atRung(ctx, d) || !in.travelToRung(ctx, d) || client.ops != 0 || acctTile(sess) != (tile.Tile{X: 3257, Z: 3280}) || len(*logged) != 0 {
		t.Errorf("an account at the rung was moved: at %v, %d gate operations, log %v", acctTile(sess), client.ops, *logged)
	}

	// Inside the fence and past the leash (the pen's south tip): it walks on to the stand.
	tip, ctx, d, sess, _, _ := penTrainer(t, 3257, 3256, true)
	if tile.Chebyshev(acctTile(sess), tip.rung.Stand) <= tip.rung.Leash {
		t.Fatalf("fixture: %v is within the leash", acctTile(sess))
	}
	if !tip.travelToRung(ctx, d) || tile.Chebyshev(acctTile(sess), tip.rung.Stand) > 3 {
		t.Errorf("an account at the pen's south tip ended at %v, not at the stand %v", acctTile(sess), tip.rung.Stand)
	}
}

// A gate that never opens fails the walk in, which Loop counts against the rung's three tries; nothing here spins.
func TestTravelToTheCowsRungFailsAtAGateThatWillNotOpen(t *testing.T) {
	b, ctx, d, sess, _, logged := penTrainer(t, 3252, 3267, false)
	if b.travelToRung(ctx, d) {
		t.Fatalf("travelToRung reported a walk in through a gate that never opened; the account is at %v", acctTile(sess))
	}
	if !strings.Contains(strings.Join(*logged, "\n"), "outside the pen: walking in by the gate") {
		t.Errorf("the attempt is not in the log:\n%s", strings.Join(*logged, "\n"))
	}
}

// With no cow in reach, a few seconds' wait is spent walking the pen's beat, once per wait, and a beat that cannot be walked is given up
// until a cow has been in reach again.
func TestTheCowsRungWalksThePensBeatWhenNoCowIsInReach(t *testing.T) {
	b, ctx, d, sess, _, logged := penTrainer(t, 3258, 3266, true)
	if b.walkBeat(ctx, d) || b.penIdleSince.IsZero() {
		t.Fatal("the beat was walked on the first idle look, or the wait was not started")
	}
	if b.walkBeat(ctx, d) {
		t.Fatal("the beat was walked before the wait was over")
	}
	pen := b.rung.Pen
	var walked []tile.Tile
	for i := 0; i < len(pen.Beat)+1; i++ {
		b.penIdleSince = time.Now().Add(-penBeatAfter - time.Second)
		before := acctTile(sess)
		if !b.walkBeat(ctx, d) {
			t.Fatalf("walk %d: the beat was not walked from %v", i, before)
		}
		after := acctTile(sess)
		walked = append(walked, after)
		if after == before {
			t.Errorf("walk %d: the account did not move from %v", i, before)
		}
		if tile.Chebyshev(before, after) <= 2 && i > 0 {
			t.Errorf("walk %d moved only %d tiles", i, tile.Chebyshev(before, after))
		}
		if b.walkBeat(ctx, d) {
			t.Fatalf("walk %d: a second walk followed at once", i)
		}
	}
	if b.penWalks != len(pen.Beat)+1 {
		t.Errorf("penWalks = %d, want %d", b.penWalks, len(pen.Beat)+1)
	}
	for _, w := range walked {
		g := b.penOf(d)
		if !g.Inside(w) {
			t.Errorf("a walk of the beat ended at %v, outside the pen", w)
		}
	}
	if got := strings.Join(*logged, "\n"); !strings.Contains(got, "walking the pen to") {
		t.Errorf("the beat is not in the log:\n%s", got)
	}

	// A walker that cannot get anywhere: three tries, then the beat is left alone, until a cow has been in reach.
	old := penBeatWalkTimeout
	penBeatWalkTimeout = 50 * time.Millisecond
	defer func() { penBeatWalkTimeout = old }()
	b, ctx, d, _, _, _ = penTrainer(t, 3258, 3266, false)
	b.walkBeat(ctx, d)
	for i := 0; i < penBeatTries; i++ {
		b.penIdleSince = time.Now().Add(-penBeatAfter - time.Second)
		if b.walkBeat(ctx, d) {
			t.Fatalf("a walk that went nowhere was counted (try %d)", i)
		}
	}
	if b.penBeatFails != penBeatTries {
		t.Fatalf("penBeatFails = %d, want %d", b.penBeatFails, penBeatTries)
	}
	b.penIdleSince = time.Now().Add(-penBeatAfter - time.Second)
	before := b.penBeatAt
	b.walkBeat(ctx, d)
	if b.penBeatAt != before {
		t.Error("the beat was tried a fourth time")
	}
	b.penFoundTarget()
	if b.penBeatFails != 0 || !b.penIdleSince.IsZero() {
		t.Errorf("a cow in reach did not reset the wait: fails %d, since %v", b.penBeatFails, b.penIdleSince)
	}
}

// rankTargetsBy ranks by the distance it is given: the walk, not the straight line.
func TestRankTargetsByTheWalk(t *testing.T) {
	const self = 5
	a := gamestate.NPC{Index: 1, X: 100, Z: 100, FaceEntity: -1} // 2 tiles away across a fence: 40 steps
	c := gamestate.NPC{Index: 2, X: 106, Z: 100, FaceEntity: -1} // 8 tiles away: 8 steps
	walk := map[int]int{1: 40, 2: 8}
	got := rankTargetsBy([]gamestate.NPC{a, c}, self, true, true, func(n gamestate.NPC) int { return walk[n.Index] })
	if len(got) != 2 || got[0].Index != 2 {
		t.Errorf("ranked %v, want the cow 8 steps away first", got)
	}
	// The straight-line ranking is the other way round, so the two are not the same function.
	if line := rankTargets([]gamestate.NPC{a, c}, tile.Tile{X: 102, Z: 100}, self, true, true); line[0].Index != 1 {
		t.Errorf("ranked by the line %v, want the cow 2 tiles away first", line)
	}
}

// Structural, because Loop and fightStep need a live account to run: Loop reaches the rung through atRung(ctx, d) and travelToRung and
// never walks to the stand by itself, fightStep leaves a pen's outside before it picks, spends an idle wait on the host's use of it
// first and the beat second, and tells the pen when a cow is in reach; pickTarget hands a pen its own pick before the door planner.
func TestTheTrainerReachesAndWorksThePenThroughItsSteps(t *testing.T) {
	raw, err := os.ReadFile("combattrainer.go")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")
	body := func(sig string) string {
		start := strings.Index(src, sig)
		if start < 0 {
			t.Fatalf("no %s in combattrainer.go", sig)
		}
		b := src[start:]
		return b[:strings.Index(b[1:], "\nfunc ")+1]
	}
	before := func(fn, body string, first, second string) {
		t.Helper()
		i, j := strings.Index(body, first), strings.Index(body, second)
		if i < 0 || j < 0 || i >= j {
			t.Errorf("%s: %q must come before %q (at %d and %d)", fn, first, second, i, j)
		}
	}

	loop := body("func (b *combatTrainerBot) Loop(")
	for _, want := range []string{"b.atRung(ctx, d)", "b.travelToRung(ctx, d)"} {
		if !strings.Contains(loop, want) {
			t.Errorf("Loop does not call %s", want)
		}
	}
	if strings.Contains(loop, "combatrunner.TravelTo(d, b.rung.Stand") {
		t.Error("Loop walks to the stand itself, which at a fenced pen can end on the road")
	}

	fight := body("func (b *combatTrainerBot) fightStep(")
	before("fightStep", fight, "b.strayedFromPen(ctx, d)", "b.pickTarget(ctx, d)")
	before("fightStep", fight, "b.embed.idle(", "b.walkBeat(ctx, d)")
	before("fightStep", fight, "b.walkBeat(ctx, d)", "b.penFoundTarget()")
	before("fightStep", fight, "b.pickTarget(ctx, d)", "b.penFoundTarget()")
	before("fightStep", fight, "b.penFoundTarget()", "b.fightOne(ctx, d, npc)")

	pick := body("func (b *combatTrainerBot) pickTarget(")
	before("pickTarget", pick, "b.penOf(d)", "rankTargets(cands")
	before("pickTarget", pick, "b.pickPenTarget(", "combatrunner.ReachPlain(d, at(n))")
}
