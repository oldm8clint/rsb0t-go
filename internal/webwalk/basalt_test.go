package webwalk

import (
	"strings"
	"testing"
	"time"
)

// The expected rocks, from content/scripts/quests/quest_horror/scripts/basalt_rocks.rs2 (branch 289), each
// [oploc1,horror_jumping_spotN] @jump_basalt($start, $dest, $final_dest, $dir) with its packed coordinates worked out:
// the loc id is 4549 + N, the jumper stands on $start (the loc's own tile) and lands on $final_dest.
var contentRocks = []struct {
	spot      int
	locID     int
	from, to  Point
	northward bool
}{
	{1, 4550, Point{X: 2522, Z: 3595}, Point{X: 2522, Z: 3598}, true},
	{2, 4551, Point{X: 2522, Z: 3597}, Point{X: 2522, Z: 3594}, false},
	{3, 4552, Point{X: 2522, Z: 3600}, Point{X: 2522, Z: 3603}, true},
	{4, 4553, Point{X: 2522, Z: 3602}, Point{X: 2522, Z: 3599}, false},
	{5, 4554, Point{X: 2518, Z: 3611}, Point{X: 2515, Z: 3611}, true},
	{6, 4555, Point{X: 2516, Z: 3611}, Point{X: 2518, Z: 3610}, false},
	{7, 4556, Point{X: 2514, Z: 3613}, Point{X: 2514, Z: 3616}, true},
	{8, 4557, Point{X: 2514, Z: 3615}, Point{X: 2514, Z: 3612}, false},
	{9, 4558, Point{X: 2514, Z: 3617}, Point{X: 2514, Z: 3620}, true},
	{10, 4559, Point{X: 2514, Z: 3619}, Point{X: 2514, Z: 3616}, false},
}

func basaltEdge(t *testing.T, g *Graph, locID int) *Edge {
	t.Helper()
	for i := range g.Edges {
		if g.Edges[i].LocID == locID {
			return &g.Edges[i]
		}
	}
	t.Fatalf("no edge for loc %d in the graph", locID)
	return nil
}

// TestBasaltRocksMatchContent pins the ten rocks to the script they were read from: where each is jumped from and
// lands, which two roll dice and where a miss leaves the jumper, and that nothing else is gated. Before basalt.go
// all ten carried Agility 35 and none carried a Slip, so an account on the north shore with Agility 20 (or before
// its stats had synced) had no route home that content forbids it, and the walker could not tell the two rocks that
// hurt from the eight that cannot.
func TestBasaltRocksMatchContent(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	slips := 0
	for _, want := range contentRocks {
		e := basaltEdge(t, g, want.locID)
		if e.From != want.from || e.To != want.to || e.LocX != want.from.X || e.LocZ != want.from.Z || e.Action != "Jump-across" || e.Kind != KindShortcut {
			t.Errorf("spot %d: edge is %s, want %s -> %s operated at its own tile", want.spot, e.String(), want.from, want.to)
		}
		if e.Requires == nil || !e.Requires.Members {
			t.Errorf("spot %d: not members-only", want.spot)
		}
		gated := e.Requires != nil && len(e.Requires.Skills) > 0
		switch want.spot {
		case 3: // northbound dice: the one gate that stays, at the odds below which nobody should go over
			if !gated || e.Requires.Skills[0].Name != "agility" || e.Requires.Skills[0].Level != 35 {
				t.Errorf("spot 3 (the northbound dice rock) lost its Agility 35 gate: %+v", e.Requires)
			}
			if e.Slip == nil || e.Slip.Lands != (Point{X: 2521, Z: 3595}) {
				t.Errorf("spot 3 slip = %+v, want a miss swept to 2521,3595", e.Slip)
			}
		case 8: // southbound dice: the way home, never gated
			if gated {
				t.Errorf("spot 8 (the southbound dice rock, the way home) is gated: %+v", e.Requires.Skills)
			}
			if e.Slip == nil || e.Slip.Lands != (Point{X: 2515, Z: 3619}) {
				t.Errorf("spot 8 slip = %+v, want a miss swept to 2515,3619", e.Slip)
			}
		default:
			if gated {
				t.Errorf("spot %d is gated on %+v, but content rolls nothing there", want.spot, e.Requires.Skills)
			}
			if e.Slip != nil || e.failureHurts() {
				t.Errorf("spot %d cannot slip but is priced and guarded as if it could (slip %+v)", want.spot, e.Slip)
			}
		}
		if e.Slip != nil {
			slips++
			if e.Slip.Skill != "agility" || e.Slip.Low != 5 || e.Slip.High != 255 || e.Slip.MaxHit != 10 {
				t.Errorf("spot %d slip = %+v, want stat_random(agility, 5, 255) and up to 10 damage", want.spot, e.Slip)
			}
			if !e.failureHurts() || e.slipHPLimit() != 11 {
				t.Errorf("spot %d: failureHurts %v, hitpoint limit %d, want true and 11", want.spot, e.failureHurts(), e.slipHPLimit())
			}
		}
	}
	if slips != 2 {
		t.Errorf("%d rocks carry a slip, want exactly the two content rolls dice for", slips)
	}
}

// TestATripOverTheBasaltRocksCostsWhatItDid keeps every walk's choice between the rocks and the road where it was: a
// trip is five jumps, passes exactly one rock that can slip, and the five together cost the 800 that five rocks at
// 160 each did before the rocks were priced one by one.
func TestATripOverTheBasaltRocksCostsWhatItDid(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, north := range []bool{true, false} {
		total, dice, n := 0, 0, 0
		for _, want := range contentRocks {
			if want.northward != north {
				continue
			}
			e := basaltEdge(t, g, want.locID)
			total += e.Cost
			n++
			if e.Slip != nil {
				dice++
			}
		}
		if n != 5 || dice != 1 || total != 800 {
			t.Errorf("northbound=%v: %d jumps, %d of them dice, costing %d; want 5, 1 and 800", north, n, dice, total)
		}
	}
}

// TestSlipChanceIsTheEnginesStatRandom checks the odds against engine/script/handlers/PlayerOps.ts STAT_RANDOM:
// floor(low*(99-L)/98) + floor(high*(L-1)/98) + 1 out of 256, and 100% at 99.
func TestSlipChanceIsTheEnginesStatRandom(t *testing.T) {
	s := Slip{Skill: "agility", Low: 5, High: 255, MaxHit: 10}
	for level, want := range map[int]int{1: 6, 20: 54, 35: 92, 40: 105, 50: 130, 60: 155, 99: 256} {
		if got := s.Chance(level) * 256; got != float64(want) {
			t.Errorf("level %d: %v/256, want %d/256", level, got, want)
		}
	}
	if s.Chance(0) != s.Chance(1) || s.Chance(150) != 1 {
		t.Errorf("out-of-range levels are not clamped: %v %v", s.Chance(0), s.Chance(150))
	}
}

// -------- the walker --------

// rockWorld is a fakeWorld with one slipping rock across a gap of water. The north shore is x 20..30, the south shore
// x 0..17 and x 18..19 is sea; the rock is the loc at the north shore's stand (20,2), which jumps to (17,2). Its dice
// come from rolls, one entry per jump: true carries the jumper over, false is a miss that sweeps it to slipTo for
// the next entry of damage.
type rockWorld struct {
	*fakeWorld
	rolls   []bool
	damage  []int
	slipTo  Point
	landing Point
	stand   Point
	locID   int
	jumps   []int // the hitpoints at each jump, -1 when not watched
	hp      *hpClock
}

func (r *rockWorld) FindLocs(q LocQuery) []LocMatch {
	out := r.fakeWorld.FindLocs(q)
	at := r.stand
	if at.Level == q.At.Level && Chebyshev(at, q.At) <= q.Radius {
		wanted := len(q.IDs) == 0
		for _, id := range q.IDs {
			wanted = wanted || id == r.locID
		}
		for _, n := range q.Names {
			wanted = wanted || strings.EqualFold(n, "Basalt rock")
		}
		if wanted && (q.Op == "" || strings.EqualFold(q.Op, "Jump-across")) {
			out = append(out, LocMatch{ID: r.locID, X: at.X, Z: at.Z, Level: at.Level, Name: "Basalt rock", Op: 1})
		}
	}
	return out
}

func (r *rockWorld) OpLoc(op, x, z, id int) error {
	if id != r.locID {
		return r.fakeWorld.OpLoc(op, x, z, id)
	}
	r.fakeWorld.ops = append(r.fakeWorld.ops, "rock")
	if r.fakeWorld.me != r.stand {
		r.fakeWorld.cantAt = append(r.fakeWorld.cantAt, r.fakeWorld.now)
		return nil
	}
	hp := -1
	if r.hp != nil {
		hp = r.hp.read(r.fakeWorld.now)
	}
	r.jumps = append(r.jumps, hp)
	i := len(r.jumps) - 1
	if i < len(r.rolls) && !r.rolls[i] {
		r.fakeWorld.me, r.fakeWorld.path = r.slipTo, nil
		if r.hp != nil {
			r.hp.hurt(r.fakeWorld.now, r.damage[i])
		}
		return nil
	}
	r.fakeWorld.me, r.fakeWorld.path = r.landing, nil
	return nil
}

// hpClock is hitpoints that come back one a minute (content's timer health_regen, 100 ticks), as a fake client
// reports them.
type hpClock struct {
	hp, max int
	next    time.Time
	lowest  int
}

func (c *hpClock) read(now time.Time) int {
	for c.hp < c.max && !now.Before(c.next) {
		c.hp++
		c.next = c.next.Add(time.Minute)
	}
	return c.hp
}

func (c *hpClock) hurt(now time.Time, dmg int) {
	c.read(now)
	if c.hp >= c.max {
		c.next = now.Add(time.Minute)
	}
	c.hp = max(c.hp-dmg, 0)
	c.lowest = min(c.lowest, c.hp)
}

// watchedRock is a rockWorld whose client can say how many hitpoints the account has (CombatClient).
type watchedRock struct{ *rockWorld }

func (w watchedRock) CombatLevel() (int, bool) { return 57, true }
func (w watchedRock) InCombat() bool           { return false }
func (w watchedRock) HP() (int, int, bool) {
	return w.hp.read(w.fakeWorld.now), w.hp.max, true
}
func (w watchedRock) RunEnabled() bool         { return false }
func (w watchedRock) SetRun(bool) error        { return nil }
func (w watchedRock) DiedSince(time.Time) bool { return w.hp.hp <= 0 }
func (w watchedRock) Attackers(int) []Point    { return nil }

func newRockGraph() (*grid, *Graph, *Edge) {
	col := newGrid().fill(0, 0, 17, 4, 0).fill(20, 0, 30, 4, 0)
	slip := &Slip{Skill: "agility", Low: 5, High: 255, MaxHit: 10, Lands: Point{X: 22, Z: 2}}
	g := &Graph{Edges: []Edge{{
		From: Point{X: 20, Z: 2}, To: Point{X: 17, Z: 2}, Kind: KindShortcut, Cost: 760, LocName: "Basalt rock", Action: "Jump-across",
		LocID: 4557, OpenLocID: -1, LocX: 20, LocZ: 2, LandsOnTile: true, Requires: &Requires{Members: true}, Slip: slip,
	}}}
	return col, g, &g.Edges[0]
}

func newRockWorld(t *testing.T, rolls []bool, damage []int) (*rockWorld, *Graph, *Edge) {
	col, g, e := newRockGraph()
	f := newFakeWorld(t, col, Point{X: 26, Z: 2})
	return &rockWorld{fakeWorld: f, rolls: rolls, damage: damage, slipTo: e.Slip.Lands, landing: e.To, stand: e.From, locID: e.LocID}, g, e
}

func rockKey(e *Edge) Point { return Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level} }

// TestAFarShoreAccountRestsAndRetriesASlippingRockUntilItIsHome is 14 Oct 2026 in miniature. The account stands on the
// north shore of the only way home, a rock that sweeps a miss back to that shore for 1-10 hitpoints. Four misses in a
// row used to ban the rock for 30 minutes "even if nothing else reaches" home (and the strikes outlived the ban), so
// the account was stranded; the guard that refused at 11 hitpoints counted as a strike too. Now it rests until a slip
// is survivable and goes on, never jumping at 11 hitpoints or under and never banning the way home.
func TestAFarShoreAccountRestsAndRetriesASlippingRockUntilItIsHome(t *testing.T) {
	// 30 hitpoints: two misses of 10 leave 10 (a rest), a miss of 6 leaves 6 (a rest), a miss of 5 leaves 7 (a rest),
	// and the fifth jump lands. That is four strikes in a row, the number that banned the rock.
	rw, g, e := newRockWorld(t, []bool{false, false, false, false, true}, []int{10, 10, 6, 5, 0})
	rw.hp = &hpClock{hp: 30, max: 30, lowest: 30}
	f := watchedRock{rw}
	w := NewWalker(NewPlanner(rw.fakeWorld.plan, g), f)
	var lines []string
	w.Log = func(s string) { lines = append(lines, s); t.Log(s) }
	start := rw.fakeWorld.now
	res := w.WalkTo(Point{X: 3, Z: 2}, WalkOptions{Timeout: 4 * time.Hour})
	if !res.Arrived() {
		t.Fatalf("the account never got home: %+v", res)
	}
	if rw.hp.lowest <= 0 {
		t.Errorf("the account fell to %d hitpoints", rw.hp.lowest)
	}
	for i, hp := range rw.jumps {
		if hp <= e.slipHPLimit() {
			t.Errorf("jump %d was made on %d hitpoints, at or under the %d one slip can take", i+1, hp, e.slipHPLimit())
		}
	}
	if len(rw.jumps) != 5 {
		t.Errorf("%d jumps, want the four misses and the landing", len(rw.jumps))
	}
	if w.Banned(rockKey(e)) || w.Strikes(rockKey(e)) != 0 {
		t.Errorf("the way home was left banned or struck: banned %v, strikes %d", w.Banned(rockKey(e)), w.Strikes(rockKey(e)))
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{"slipped, swept back to 22,2,0", "resting at", "rested "} {
		if !strings.Contains(all, want) {
			t.Errorf("no %q line in the log:\n%s", want, all)
		}
	}
	for _, bad := range []string{"out of every plan", "did not resolve", "giving up after"} {
		if strings.Contains(all, bad) {
			t.Errorf("the log has %q:\n%s", bad, all)
		}
	}
	if took := rw.fakeWorld.now.Sub(start); took < 10*time.Minute {
		t.Errorf("home in %v: the account cannot have rested from 10, 6 and 7 hitpoints at a hitpoint a minute", took)
	}
}

// TestASlipSpendsNoRepath: a miss is the crossing working as content wrote it, so nine of them in a row (a 1 in 100
// streak at a 40% roll) must not use up the walk's five repaths or trip the give-up that a crossing which never
// answers earns after six refusals, and none of them waits out the eight seconds the walker gives a click to land
// when the dice have already put the account on a known tile.
func TestASlipSpendsNoRepath(t *testing.T) {
	rw, g, _ := newRockWorld(t, []bool{false, false, false, false, false, false, false, false, false, true}, []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 0})
	rw.hp = &hpClock{hp: 200, max: 200, lowest: 200}
	w := NewWalker(NewPlanner(rw.fakeWorld.plan, g), watchedRock{rw})
	var lines []string
	w.Log = func(s string) { lines = append(lines, s) }
	start := rw.fakeWorld.now
	res := w.WalkTo(Point{X: 3, Z: 2}, WalkOptions{Timeout: time.Hour})
	if !res.Arrived() {
		t.Fatalf("result = %+v\n%s", res, strings.Join(lines, "\n"))
	}
	all := strings.Join(lines, "\n")
	if strings.Contains(all, "repathing") {
		t.Errorf("a slip spent a repath:\n%s", all)
	}
	if strings.Contains(all, "did not resolve") {
		t.Errorf("a slip was waited out as an unresolved click:\n%s", all)
	}
	if strings.Contains(all, "crossings in a row") {
		t.Errorf("a run of slips was given up on as a crossing that never answers:\n%s", all)
	}
	if n := strings.Count(all, "slipped, swept back"); n != 9 {
		t.Errorf("%d slips logged, want 9:\n%s", n, all)
	}
	if took := rw.fakeWorld.now.Sub(start); took > 5*time.Minute {
		t.Errorf("nine slips and the landing took %v of walking time", took)
	}
}

// TestARestingWalkFailsAtItsDeadlineWithoutStrikes: an account that cannot yet take a slip waits at the rock for as
// long as the walk lasts, and then the walk fails for its caller. Resting is not a refusal: nothing is struck or banned,
// so the next walk starts with the way home exactly as it was.
func TestARestingWalkFailsAtItsDeadlineWithoutStrikes(t *testing.T) {
	rw, g, e := newRockWorld(t, nil, nil)
	rw.hp = &hpClock{hp: 3, max: 45, next: rw.fakeWorld.now.Add(time.Minute), lowest: 3}
	w := NewWalker(NewPlanner(rw.fakeWorld.plan, g), watchedRock{rw})
	var lines []string
	w.Log = func(s string) { lines = append(lines, s) }
	res := w.WalkTo(Point{X: 3, Z: 2}, WalkOptions{Timeout: 3 * time.Minute})
	if res.Arrived() || res.Outcome != OutcomeFailed {
		t.Fatalf("result = %+v, want a failed walk", res)
	}
	if len(rw.jumps) != 0 {
		t.Errorf("jumped %d times on 3 hitpoints", len(rw.jumps))
	}
	if w.Strikes(rockKey(e)) != 0 || w.Banned(rockKey(e)) {
		t.Errorf("resting struck or banned the way home: strikes %d, banned %v", w.Strikes(rockKey(e)), w.Banned(rockKey(e)))
	}
	all := strings.Join(lines, "\n")
	if !strings.Contains(all, "resting at 3/45 hitpoints") || strings.Contains(all, "not jumping") {
		t.Errorf("the log does not say the account rested:\n%s", all)
	}
}

// TestASlippingRockIsStillBannedWhenTheClientCannotWatchHitpoints: keeping a rock that slips in the plans is only safe
// for a walker that can see what the slips cost, so without hitpoints the old rule stands.
func TestASlippingRockIsStillBannedWhenTheClientCannotWatchHitpoints(t *testing.T) {
	rw, g, e := newRockWorld(t, []bool{false, false, false, false, false, false, false, false}, nil)
	w := NewWalker(NewPlanner(rw.fakeWorld.plan, g), rw) // no CombatClient
	w.Log = func(s string) { t.Log(s) }
	w.WalkTo(Point{X: 3, Z: 2}, WalkOptions{Timeout: 10 * time.Minute})
	if !w.Banned(rockKey(e)) {
		t.Errorf("a slipping rock the walker cannot watch hitpoints for was not banned after %d strikes (%d)", shortcutBanStrikes, w.Strikes(rockKey(e)))
	}
}

// TestASlippingRockThatIsTheOnlyWayHomeIsNotBanned is noteRefusal's rule on its own: with hitpoints to watch, four
// misses in a row at the only way home leave it in the plans (and count towards giving the walk up), exactly as a door
// does; with a way round it, they ban it for shortcutBanFor.
func TestASlippingRockThatIsTheOnlyWayHomeIsNotBanned(t *testing.T) {
	rw, g, e := newRockWorld(t, nil, nil)
	rw.hp = &hpClock{hp: 45, max: 45, lowest: 45}
	w := NewWalker(NewPlanner(rw.fakeWorld.plan, g), watchedRock{rw})
	w.Log = func(s string) { t.Log(s) }
	rw.fakeWorld.me = Point{X: 22, Z: 2}
	key := rockKey(e)
	dest := Point{X: 3, Z: 2}
	avoid := map[Point]bool{}
	opt := w.searchOptions(Options{}, avoid)
	for i := 0; i < shortcutBanStrikes+1; i++ {
		w.noteRefusal(e, avoid, dest, opt)
	}
	if w.Banned(key) || avoid[key] {
		t.Errorf("the only way home was banned (%v) or avoided (%v) after %d misses", w.Banned(key), avoid[key], shortcutBanStrikes+1)
	}
	if w.onlyWay[key] == 0 {
		t.Errorf("the misses were not counted towards giving the walk up")
	}
}

func TestASlippingRockWithAWayRoundIsBannedAfterItsStrikes(t *testing.T) {
	rw, g, e := newRockWorld(t, nil, nil)
	// A second, plain way home: a bridge over the sea.
	rw.fakeWorld.plan.fill(18, 0, 19, 0, 0)
	rw.fakeWorld.plan.fill(17, 0, 17, 0, 0).fill(20, 0, 20, 0, 0)
	rw.hp = &hpClock{hp: 45, max: 45, lowest: 45}
	w := NewWalker(NewPlanner(rw.fakeWorld.plan, g), watchedRock{rw})
	w.Log = func(s string) { t.Log(s) }
	rw.fakeWorld.me = Point{X: 22, Z: 2}
	key := rockKey(e)
	avoid := map[Point]bool{}
	opt := w.searchOptions(Options{}, avoid)
	for i := 0; i < shortcutBanStrikes; i++ {
		w.noteRefusal(e, avoid, Point{X: 3, Z: 2}, opt)
	}
	if !w.Banned(key) || !avoid[key] {
		t.Errorf("a slipping rock with a bridge beside it was not banned: banned %v, avoided %v", w.Banned(key), avoid[key])
	}
}

// TestAnAccountThatCanNeverTakeASlipDoesNotWaitForOne: an account whose hitpoints are full at 10 can rest all day
// and still not survive the worst slip, so waiting would only spend the walk's whole timeout. It is refused at once,
// as the guard always did, and nothing is struck or banned.
func TestAnAccountThatCanNeverTakeASlipDoesNotWaitForOne(t *testing.T) {
	rw, g, e := newRockWorld(t, nil, nil)
	rw.hp = &hpClock{hp: 10, max: 10, lowest: 10}
	w := NewWalker(NewPlanner(rw.fakeWorld.plan, g), watchedRock{rw})
	var lines []string
	w.Log = func(s string) { lines = append(lines, s) }
	start := rw.fakeWorld.now
	res := w.WalkTo(Point{X: 3, Z: 2}, WalkOptions{Timeout: time.Hour})
	if res.Arrived() {
		t.Fatalf("arrived with 10 hitpoints over a rock that deals up to 10: %+v", res)
	}
	if len(rw.jumps) != 0 {
		t.Errorf("jumped %d times", len(rw.jumps))
	}
	if took := rw.fakeWorld.now.Sub(start); took > 20*time.Minute {
		t.Errorf("waited %v for hitpoints that cannot come", took)
	}
	if w.Strikes(rockKey(e)) != 0 || w.Banned(rockKey(e)) {
		t.Errorf("struck or banned: strikes %d, banned %v", w.Strikes(rockKey(e)), w.Banned(rockKey(e)))
	}
	if all := strings.Join(lines, "\n"); !strings.Contains(all, "not jumping on 10/10 hitpoints") {
		t.Errorf("the log does not say why:\n%s", all)
	}
}
