package webwalk

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/randomevents"
)

// The ferries leave an account on a ship deck, a level-1 pocket the collision bake joins to the dock only by the
// gangplank. Live on 16 Oct 2026 (run/logs of the 01:02 fleet and the 09:01 one) the Walker turned its own walks away
// from that deck: 33 quest parks in eight hours and 20 more in the next ninety minutes read "3 walks in this
// run ended stranded (...: stranded in the Ardougne ship deck: the Guardian's escape ladder owns the way out, not this
// walk)", every one for an account whose planned route off the deck (hop [gangplank] Cross loc=2086@2683,3269) was
// found 334 times in the same logs. Two faults ended a walk on the deck and the Walker then read the deck as a strand
// site: the ferry's dialogue budget ran out 1 s before the landing (18 of 409 Brimhaven to Ardougne crossings, all at
// exactly 24 s), so the walk replanned from the dock and was thrown onto the deck mid-route; and the crossing used up
// what was left of the walk's deadline (8 walks).

// deckSite is the Ardougne ship deck as the strand catalogue has it and the graph's gangplank edge for it.
var (
	ardougneDeck      = Point{X: 2683, Z: 3268, Level: 1}
	ardougneDockLand  = Point{X: 2683, Z: 3271, Level: 0}
	ardougneGangplank = Edge{
		From: ardougneDeck, To: ardougneDockLand, Kind: KindGangplank, Cost: 8,
		LocName: "Gangplank", Action: "Cross", LocID: 2086, OpenLocID: -1, LocX: 2683, LocZ: 3269, LandsOnTile: true,
	}
)

// deckWorld is the Ardougne deck over a fake world: the deck at level 1, a dock at level 0 and the gangplank between
// them, with refuse making the gangplank answer nothing.
func deckWorld(t *testing.T, me Point, refuse bool) (*fakeWorld, *Walker) {
	t.Helper()
	g := newGrid().fill(2676, 3260, 2690, 3268, 1).fill(2670, 3271, 2695, 3290, 0)
	f, w := strandWorld(t, g, []Edge{ardougneGangplank}, me)
	f.ladders = append(f.ladders, &fakeLadder{
		loc: Point{X: 2683, Z: 3269, Level: 1}, stand: ardougneDeck, landing: ardougneDockLand,
		id: 2086, name: "Gangplank", op: "Cross", refuse: refuse,
	})
	return f, w
}

// The Guardian keeps the decks as backstop sites (an account that logged in on one), but they are not sites a script's
// walk is refused from.
func TestAShipDeckIsAStrandSiteButNotOneTheWalkerRefuses(t *testing.T) {
	for _, deck := range []Point{
		{X: 2683, Z: 3268, Level: 1}, // Ardougne, where the Brimhaven ferry lands
		{X: 2776, Z: 3235, Level: 1}, // Brimhaven, where the 3 Oct moderator stood
		{X: 2956, Z: 3143, Level: 1}, // Musa Point, where the Port Sarim ferry lands
	} {
		site, owned := randomevents.StrandOwner(deck.X, deck.Z, deck.Level)
		if !owned {
			t.Fatalf("%s is no longer a catalogued site: the Guardian has lost its backstop for an account that logs in on a deck", deck)
		}
		if held, ok := randomevents.StrandHoldsWalks(deck.X, deck.Z, deck.Level); ok {
			t.Errorf("%s (%s) is held against the Walker's own walks as %q", deck, site, held)
		}
	}
	// And the sites that are the Guardian's still are.
	for _, p := range []Point{
		{X: 2718, Z: 9816, Level: 0}, // the dragon's lair
		{X: 3282, Z: 3936, Level: 3}, // Rogues' Castle's roof
		{X: 2475, Z: 3419, Level: 2}, // the Gnome course's upper floor
	} {
		if _, ok := randomevents.StrandHoldsWalks(p.X, p.Z, p.Level); !ok {
			t.Errorf("%s is no longer held against the Walker's walks", p)
		}
	}
}

// A walk that begins on the deck is planned like any other and leaves by the gangplank at once: no refusal, no five
// minutes of "stranded", no wait for the Guardian's MinStay.
func TestAWalkFromTheArdougneDeckLeavesByTheGangplankAtOnce(t *testing.T) {
	f, w := deckWorld(t, Point{X: 2685, Z: 3266, Level: 1}, false)
	dest := Point{X: 2690, Z: 3280, Level: 0}
	start := f.now
	res := w.WalkTo(dest, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("walk from the deck: %+v", res)
	}
	if strings.Join(f.ops, ",") != "2086" {
		t.Errorf("ops = %v, want the gangplank only", f.ops)
	}
	if took := f.now.Sub(start); took >= strandRefuseWait {
		t.Errorf("leaving the deck took %v, as long as a refusal wait (%v)", took, strandRefuseWait)
	}
}

// A walk that fails on the deck for any reason (here the gangplank not answering) is an ordinary failure: it does not
// name the deck a strand site, and the next walk from there is planned and tried again instead of being refused for
// strandRetryEvery.
func TestAFailedWalkOnTheDeckDoesNotMakeItAStrandSite(t *testing.T) {
	f, w := deckWorld(t, ardougneDeck, true)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s) }
	dest := Point{X: 2690, Z: 3280, Level: 0}

	res := w.WalkTo(dest, WalkOptions{})
	if res.Arrived() {
		t.Fatalf("the gangplank refuses, yet the walk arrived: %+v", res)
	}
	if strings.Contains(res.Reason, "stranded in") || strings.Contains(res.Reason, "escape ladder owns") {
		t.Fatalf("a walk failing on the deck was read as a strand: %+v", res)
	}

	// The gangplank answers again; five seconds later (not five minutes) the walk goes.
	f.ladders[0].refuse = false
	f.now = f.now.Add(10 * time.Second)
	w.unreachableAt = nil // the memo of the walk that ended unreachable is a separate rule, tested in unreachablememo_test.go
	res = w.WalkTo(dest, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("second walk: %+v\n%s", res, strings.Join(logs, "\n"))
	}
	for _, l := range logs {
		if strings.Contains(l, "escape ladder owns") || strings.Contains(l, "a walk from here failed") {
			t.Errorf("the deck refused a walk: %s", l)
		}
	}
}

// A ferry's wait is not walking: a walk whose budget the crossing used up is not timed out on the deck it lands on.
func TestAShipCrossingDoesNotUseUpTheWalksDeadline(t *testing.T) {
	// A pier at level 0 west of the deck, the ferry to the deck, the deck's gangplank to the dock.
	g := newGrid().fill(2676, 3260, 2690, 3268, 1).fill(2670, 3271, 2695, 3290, 0).fill(2600, 3271, 2610, 3275, 0)
	pier := Point{X: 2605, Z: 3273, Level: 0}
	ferry := Edge{
		From: pier, To: ardougneDeck, Kind: KindShip, Cost: 36, LocName: "Customs officer", Action: "Pay-fare",
		LocID: -1, OpenLocID: -1, LandsOnTile: true,
	}
	f, w := strandWorld(t, g, []Edge{ferry, ardougneGangplank}, pier)
	f.ladders = append(f.ladders, &fakeLadder{
		loc: Point{X: 2683, Z: 3269, Level: 1}, stand: ardougneDeck, landing: ardougneDockLand,
		id: 2086, name: "Gangplank", op: "Cross",
	})
	sailed := time.Duration(0)
	w.NPCHop = func(e *Edge) bool {
		before := f.now
		f.Delay(90 * time.Second) // the dialogue and the sailing
		sailed = f.now.Sub(before)
		f.me = e.To
		return true
	}
	w.SpecialHop = w.NPCHop

	res := w.WalkTo(Point{X: 2690, Z: 3280, Level: 0}, WalkOptions{Timeout: time.Minute})
	if sailed < 90*time.Second {
		t.Fatalf("the ferry never sailed (%v): the walk did not take it: %+v", sailed, res)
	}
	if !res.Arrived() {
		t.Fatalf("a one minute walk with a ninety second ferry: %+v", res)
	}
	if strings.Contains(res.Reason, "timed out") {
		t.Errorf("the walk timed out: %+v", res)
	}
}

// What the credit is not: a walk that is slow on the road still times out, and a ferry only buys maxCrossCredit.
func TestOnlyShipCrossingsAreCreditedAgainstTheDeadline(t *testing.T) {
	g := newGrid().fill(0, 0, 40, 4, 0).fill(60, 0, 100, 4, 0)
	door := Edge{From: Point{X: 40, Z: 2}, To: Point{X: 60, Z: 2}, Kind: KindOther, Cost: 10, LocName: "Barge", Action: "Board", LocID: -1, OpenLocID: -1}
	f, w := strandWorld(t, g, []Edge{door}, Point{X: 40, Z: 2})
	w.NPCHop = func(e *Edge) bool {
		f.Delay(10 * time.Minute) // a crossing that is not a ship earns no credit
		f.me = e.To
		return true
	}
	res := w.WalkTo(Point{X: 100, Z: 4}, WalkOptions{Timeout: time.Minute})
	if res.Arrived() || !strings.Contains(res.Reason, "timed out") {
		t.Errorf("a ten minute non-ship crossing in a one minute walk: %+v, want timed out", res)
	}
}
