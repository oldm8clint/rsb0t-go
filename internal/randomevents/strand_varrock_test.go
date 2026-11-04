package randomevents

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
)

// The tiles the three accounts of the 14 Oct 2026 log (build d5362bb2, 04:05-04:45) stood on in the Varrock sewers' north cavern: one
// resumed at login on 3202,9909 and was held 33 minutes (684 void hops from 3202-3206,9908-9910), two passing through on their way to
// the moss giants were caught at 3237-3239,9907 and taken to the PvP world.
var cavernLogTiles = [][2]int{{3202, 9909}, {3203, 9910}, {3204, 9909}, {3205, 9908}, {3206, 9908}, {3237, 9907}, {3239, 9907}}

// sewerRouteTiles are the tiles of the moss-giants route the walker takes straight after the cavern (the web at 3210,9898, then 3210,9897,
// 3210,9896 and 3210,9895 on the way west to 3159,9900), which the first fix left in the pocket: the review of 14 Oct 2026 found every
// account that finished the cavern caught on its next poll, on the real pack and with the Guardian's own recoverStrand. They are the
// moss giants' ground, a piece of floor of 552 tiles of which these three lie in the pocket's box.
var sewerRouteTiles = [][2]int{{3210, 9897}, {3210, 9896}, {3210, 9895}}

// sewerSideTiles are one tile each of the other pieces of sewer floor in the box that no summon put anyone on, with the size of the piece
// the real pack joins them to (the real-pack test checks it): the manhole ladder's 599-tile piece, a 411-tile piece, the 41-tile room
// behind the door 3245,9916 (two tiles), and the two fragments (6 tiles, 1 tile).
var sewerSideTiles = [][2]int{{3231, 9895}, {3233, 9895}, {3239, 9913}, {3238, 9915}, {3222, 9898}, {3202, 9911}}

var sewerSideSizes = map[[2]int]int{{3231, 9895}: 599, {3233, 9895}: 411, {3239, 9913}: 41, {3238, 9915}: 41, {3222, 9898}: 6, {3202, 9911}: 1}

// sewerFloorTiles is every tile the tests above name: all of it is the sewers' own floor and none of it the summon's.
func sewerFloorTiles() [][2]int {
	var all [][2]int
	all = append(all, cavernLogTiles...)
	all = append(all, sewerRouteTiles...)
	all = append(all, sewerSideTiles...)
	return all
}

// summonGroundTiles are tiles of the arrival's own ground inside the box, each checked against the pack by the real-pack test: the
// arrival, the corridor north of the cavern, and the ground beside the cavern's rows.
var summonGroundTiles = [][2]int{{3217, 9910}, {3217, 9915}, {3205, 9913}, {3237, 9916}, {3236, 9916}, {3211, 9918}, {3223, 9918}, {3235, 9918}, {3207, 9919}, {3219, 9919}, {3231, 9919}}

func TestTheSewerFloorIsNotAStrandSite(t *testing.T) {
	for _, c := range sewerFloorTiles() {
		if !onVarrockSewerFloor(c[0], c[1], 0) {
			t.Errorf("%d,%d is not in the sewer floor table", c[0], c[1])
		}
		if s, ok := StrandSiteAt(c[0], c[1], 0); ok {
			t.Errorf("%d,%d is in the strand site %q", c[0], c[1], s.Name)
		}
		if name, ok := PocketAt(c[0], c[1], 0); ok {
			t.Errorf("%d,%d is a pocket (%q): the walker would plan from it with the zones off", c[0], c[1], name)
		}
		if name, ok := StrandOwner(c[0], c[1], 0); ok {
			t.Errorf("%d,%d is owned by the Guardian's ladder (%q)", c[0], c[1], name)
		}
		if name, ok := StrandPlanned(c[0], c[1], 0); ok {
			t.Errorf("%d,%d is planned by the Guardian (%q): the walker would refuse to start from it", c[0], c[1], name)
		}
	}
	// Level 1 above it is nothing of the sort, and the table only speaks for level 0.
	if onVarrockSewerFloor(3203, 9910, 1) {
		t.Error("the sewer floor table answers for another level")
	}
	// The summon's own ground is still the pocket: the arrival tile, the corridor north of the cavern, the ground beside the cavern's rows.
	for _, c := range summonGroundTiles {
		if onVarrockSewerFloor(c[0], c[1], 0) {
			t.Errorf("%d,%d (the summon's ground) is in the sewer floor table", c[0], c[1])
		}
		s, ok := StrandSiteAt(c[0], c[1], 0)
		if !ok || !s.PvP || s.Name != "the underground pocket below Varrock (3217,9910 summons)" {
			t.Errorf("%d,%d = %q %v, want the Varrock pocket", c[0], c[1], s.Name, ok)
		}
	}
}

// The sewer floor is not in the dungeon strip either, which holds an account once its drift check has found no walk home (and from
// the floor the raw pack never has one: the way out is doors and a web).
func TestTheSewerFloorIsNotInTheNoWayHomeStripEither(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	NoteNoWayHome(sess)
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for _, c := range sewerFloorTiles() {
		if s, ok := g.siteAtLocked(c[0], c[1], 0, now); ok {
			t.Errorf("%d,%d is held as %q", c[0], c[1], s.Name)
		}
	}
	// The strip still holds the moss giants' neighbours: x 3155, z 9919 is the strip's.
	if s, ok := g.siteAtLocked(3155, 9919, 0, now); !ok || !s.NoWayHomeOnly {
		t.Errorf("3155,9919 = %q %v, want the strip", s.Name, ok)
	}
}

// The account that resumed in the cavern in the live log, end to end up to the packet: an account standing in the cavern costs the Guardian nothing, holds no script
// and sends nothing; two others walking through are neither held nor taken to the PvP world. The same for the tiles the walk reaches after the web and
// for the rest of the sewer floor in the box (the review of 14 Oct 2026 caught the first fix taking the account at 3210,9897, 3210,9896 and 3210,9895,
// then a hop to the PvP world within three seconds).
func TestAnAccountOnTheSewerFloorIsLeftToItsScript(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	var logs []string
	g.Log = func(s string) { logs = append(logs, s) }
	var mu sync.Mutex
	var hops []int
	g.World = func() int { return 2 }
	g.HopWorld = func(world int, reason string) error {
		mu.Lock()
		hops = append(hops, world)
		mu.Unlock()
		return nil
	}
	for _, c := range sewerFloorTiles() {
		setTileExact(sess.state, c[0], c[1], 0)
		if g.recoverStrand() {
			t.Errorf("%d,%d: the strand recovery took the poll", c[0], c[1])
		}
		if g.Busy() || strandHolds(sess) {
			t.Errorf("%d,%d: the account is held", c[0], c[1])
		}
	}
	if sess.count() != 0 {
		t.Errorf("%d packets were sent to accounts the walker owns", sess.count())
	}
	if len(hops) != 0 {
		t.Errorf("a world hop to %v was requested for an account walking through the sewers", hops)
	}
	if len(logs) != 0 {
		t.Errorf("the Guardian logged: %v", logs)
	}
}

// ---- the void walk's net progress ----

func TestVoidWalkProgressIgnoresJitter(t *testing.T) {
	var v voidWalk
	// The live account: a straight distance of 134 or 135 tiles from the rally, a tile or two nearer or farther every hop.
	v.progress(135)
	for i := 0; i < voidFlatHops-1; i++ {
		v.progress(134 - i%2)
		if v.boxed {
			t.Fatalf("boxed after %d flat hops, want %d", v.flat, voidFlatHops)
		}
	}
	v.progress(134)
	if !v.boxed || !v.slow() {
		t.Fatalf("after %d hops without progress: boxed %v slow %v", voidFlatHops, v.boxed, v.slow())
	}
	if v.stuckCount() < voidStuckSlow {
		t.Errorf("a boxed-in walk reports stuck %d, want at least %d", v.stuckCount(), voidStuckSlow)
	}
	// Real progress ends it.
	v.progress(120)
	if v.boxed || v.flat != 0 || v.slow() || v.best != 120 {
		t.Errorf("after real progress: boxed %v flat %d best %d", v.boxed, v.flat, v.best)
	}
	// A walk that makes progress forever is never boxed in.
	var w voidWalk
	for d := 300; d > 0; d -= 10 {
		w.progress(d)
	}
	if w.boxed || w.flat != 0 {
		t.Errorf("a walk that gained ten tiles a hop was boxed in: flat %d", w.flat)
	}
}

func TestRouteLengthIsTheSumOfItsSegments(t *testing.T) {
	if got := routeLength(nil); got != 0 {
		t.Errorf("routeLength(nil) = %d", got)
	}
	route := []pathfind.NavPoint{{X: 3217, Z: 9910}, {X: 3217, Z: 9920}, {X: 3207, Z: 9930}, {X: 3200, Z: 9930}}
	if got := routeLength(route); got != 10+10+7 {
		t.Errorf("routeLength = %d, want 27", got)
	}
}

// ---- the hand-back ----

// jitterHops drives the Guardian's void walk the way the live account did: a hop whenever one is due, the account a tile or two
// off where the last hop left it. It returns the number of hops sent.
func jitterHops(t *testing.T, g *Guardian, sess *recordingSession, polls int) {
	t.Helper()
	tiles := [][2]int{{3217, 9910}, {3218, 9911}, {3219, 9910}, {3218, 9909}}
	for i := 0; i < polls; i++ {
		c := tiles[i%len(tiles)]
		setTileExact(sess.state, c[0], c[1], 0)
		g.void.at = time.Now().Add(-time.Hour) // the last hop has had its time
		g.recoverStrand()
	}
}

// THE LIVE LOOP, 14 Oct 2026: 684 void hops in 33 minutes, because any move at all reset the stuck count and the position never
// stopped changing by a tile. A HandBack site stops after voidFlatHops hops without net progress: the hold is let go, the ladder takes
// over (a teleport if the account can pay for one, otherwise it gives the account back to its script), and no further hop is sent.
func TestAWalkThatGoesNowhereIsHandedBackToTheLadder(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	var logs []string
	g.Log = func(s string) { logs = append(logs, s) }
	now := time.Unix(5000, 0)
	prev := strandClock
	strandClock = func() time.Time { return now }
	t.Cleanup(func() { strandClock = prev })

	jitterHops(t, g, sess, voidFlatHops+1)
	if g.void.flat < voidFlatHops {
		t.Fatalf("flat = %d after %d hops, want at least %d (jitter must not count as progress)", g.void.flat, voidFlatHops+1, voidFlatHops)
	}
	if !g.Busy() {
		t.Fatal("not held while the walk is still trying")
	}
	sent := sess.count()
	if sent < voidFlatHops {
		t.Fatalf("%d hops sent, want about %d", sent, voidFlatHops+1)
	}

	// The next poll hands the account back.
	setTileExact(sess.state, 3217, 9910, 0)
	g.recoverStrand()
	g.mu.Lock()
	handed, held := g.handedBack, g.strandHold.site
	g.mu.Unlock()
	if handed != "the underground pocket below Varrock (3217,9910 summons)" || held != "" {
		t.Fatalf("handedBack %q, strandHold %q: want the site handed back and no hold", handed, held)
	}
	all := strings.Join(logs, "\n")
	if !strings.Contains(all, "made no net progress in") || !strings.Contains(all, "the PvP rally at 3092,10044") {
		t.Errorf("no hand-back line naming the rally:\n%s", all)
	}

	// The ladder's grace passes, and with no teleport to cast it gives the account back to its script for the rest of the visit.
	now = now.Add(10 * time.Second)
	for i := 0; i < 3; i++ {
		g.void.at = time.Now().Add(-time.Hour)
		g.recoverStrand()
		now = now.Add(5 * time.Second)
	}
	if g.Busy() || strandHolds(sess) {
		t.Fatal("the account is still held after the ladder gave up")
	}
	if n := sess.count(); n != sent {
		t.Errorf("%d packets after the hand-back, %d before: hops continued", n, sent)
	}
	all = strings.Join(logs, "\n")
	if !strings.Contains(all, "is its script's again") {
		t.Errorf("no line saying the account is its script's again:\n%s", all)
	}
	if strings.Contains(all, "STRANDED") {
		t.Errorf("a hand-back is not a STRANDED account that needs an operator:\n%s", all)
	}
	hops := strings.Count(all, "void hop")
	if hops > 8 {
		t.Errorf("%d void-hop lines: the log is still being filled by the loop", hops)
	}

	// Leaving the site closes the visit: the next time in the site is a fresh one.
	setTileExact(sess.state, 3222, 3218, 0)
	g.recoverStrand()
	g.mu.Lock()
	handed = g.handedBack
	g.mu.Unlock()
	if handed != "" {
		t.Errorf("handedBack = %q after the account left the site", handed)
	}

	// The next visit is a fresh one: the PvP escape announces itself again and sends its first hop, the walk is not boxed in.
	logs = nil
	before := sess.count()
	setTileExact(sess.state, 3217, 9910, 0)
	g.recoverStrand()
	g.mu.Lock()
	handed, held = g.handedBack, g.strandHold.site
	g.mu.Unlock()
	if handed != "" || held == "" || g.void.boxed || g.void.flat != 0 {
		t.Errorf("a new visit began with handedBack %q, hold %q, boxed %v, flat %d", handed, held, g.void.boxed, g.void.flat)
	}
	if sess.count() != before+1 || !strings.Contains(strings.Join(logs, "\n"), "PvP escape") {
		t.Errorf("a new visit sent %d packets and logged %v, want its first hop and the PvP escape's arrival line", sess.count()-before, logs)
	}
}

// A site that is not marked HandBack keeps the old behaviour: the walk goes slow (a hop a minute, the account waits for its Genie or
// Old Man) but never hands the account to the ladder.
func TestAWalkInAnOrdinaryPvPSiteGoesSlowNotBack(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	// The underground Wilderness catch-all.
	tiles := [][2]int{{3100, 10300}, {3101, 10301}, {3102, 10300}, {3101, 10299}}
	for i := 0; i < voidFlatHops+3; i++ {
		c := tiles[i%len(tiles)]
		setTileExact(sess.state, c[0], c[1], 0)
		g.void.at = time.Now().Add(-time.Hour)
		g.recoverStrand()
	}
	if !g.void.boxed || !g.void.slow() {
		t.Fatalf("a walk that went nowhere is not slow: flat %d boxed %v", g.void.flat, g.void.boxed)
	}
	g.mu.Lock()
	handed, held := g.handedBack, g.strandHold.site
	g.mu.Unlock()
	if handed != "" || held == "" {
		t.Errorf("handedBack %q, hold %q: the catch-all must keep holding its accounts for their fail-teleport", handed, held)
	}
	// And the hop that follows waits the long time, not three seconds.
	g.void.at = time.Now().Add(-voidHopWait - time.Second)
	before := sess.count()
	g.recoverStrand()
	if sess.count() != before {
		t.Error("a boxed-in walk hopped again after three seconds")
	}
}

// The web's own tile is blocked on the baked pack, so the table read off the pack leaves it out, yet an account that slashed the web
// stands on it on its way through: it is the sewers' floor, not the pocket.
func TestTheSewerWebTileIsNotAStrandSite(t *testing.T) {
	if !onVarrockSewerFloor(varrockSewerWebX, varrockSewerWebZ, 0) {
		t.Fatal("the web's tile is not on the sewer floor")
	}
	if s, ok := StrandSiteAt(varrockSewerWebX, varrockSewerWebZ, 0); ok {
		t.Errorf("the web's tile is in the strand site %q", s.Name)
	}
	if name, ok := StrandOwner(varrockSewerWebX, varrockSewerWebZ, 0); ok {
		t.Errorf("the web's tile is owned by the Guardian's ladder (%q)", name)
	}
	if onVarrockSewerFloor(varrockSewerWebX, varrockSewerWebZ, 1) {
		t.Error("the web's tile answers for another level")
	}
}
