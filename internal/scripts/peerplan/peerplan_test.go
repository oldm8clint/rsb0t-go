package peerplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/tile"
)

func at(x, z int) tile.Tile { return tile.Tile{X: x, Z: z} }

// chebCost is a DELIBERATELY WRONG cost function, used only where the test's subject is
// something other than travel: it is what this package refuses to use for real, and naming it
// here keeps that visible.
func chebCost(from, to tile.Tile) (int, bool) { return tile.Chebyshev(from, to), true }

// ---- 1. what counts as "extra" ----

// TestSpareBronzeAxeIsTheHeadlineCase is the live case this feature was built for: a
// woodcutter bought a rune axe today and banked the bronze axe it superseded. The bronze is
// spare; the rune it is working with is not.
func TestSpareBronzeAxeIsTheHeadlineCase(t *testing.T) {
	r := DefaultReserve()
	p := Peer{
		Account: "Acct218",
		Bank:    []Holding{{Item: "Bronze axe", Count: 1}},
		Held:    []Holding{{Item: "Rune axe", Count: 1}},
	}
	if got := Spare(p, "Bronze axe", r); got != 1 {
		t.Fatalf("a banked bronze axe superseded by a rune axe in hand should be spare: got %d, want 1", got)
	}
	// And the rune axe it is actually using is NOT spare, even though it is the best rung.
	if got := Spare(p, "Rune axe", r); got != 0 {
		t.Fatalf("the axe the account is working with must never be spare: got %d, want 0", got)
	}
}

// TestSpareRefusesTheOnlyOne is THE negative control: a peer holding exactly one of the item,
// with nothing better, must refuse. "A spare is a SECOND one, not the only one."
func TestSpareRefusesTheOnlyOne(t *testing.T) {
	r := DefaultReserve()
	for _, tc := range []struct {
		name string
		p    Peer
		item string
	}{
		{"one banked bronze axe and nothing better", Peer{Account: "a", Bank: []Holding{{Item: "Bronze axe", Count: 1}}}, "Bronze axe"},
		{"one banked small net and nothing better", Peer{Account: "a", Bank: []Holding{{Item: "Small fishing net", Count: 1}}}, "Small fishing net"},
		{"one banked and none in hand, of an unladdered item", Peer{Account: "a",
			Bank: []Holding{{Item: "Tinderbox", Count: 1}}}, "Tinderbox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Spare(tc.p, tc.item, r); got != 0 {
				t.Fatalf("Spare = %d, want 0 — a holder must never be stripped of its only one", got)
			}
			d := DecideServe(tc.p, "asker", tc.item, 1, r)
			if d.Verdict != Refuse {
				t.Fatalf("DecideServe = %v (%s), want Refuse", d.Verdict, d.Reason)
			}
		})
	}
}

// TestASecondOneIsSpareEvenWithoutALadder pins the other side of the same rule, because the
// first draft of the test above got it wrong and it is worth stating: an account holding a
// tinderbox in hand AND one in the bank has TWO, so the banked one is a second one and is
// spare. "Never the only one" is a floor of one, not a ban on sharing an unladdered item.
func TestASecondOneIsSpareEvenWithoutALadder(t *testing.T) {
	r := DefaultReserve()
	p := Peer{Account: "a",
		Bank: []Holding{{Item: "Tinderbox", Count: 1}},
		Held: []Holding{{Item: "Tinderbox", Count: 1}}}
	if got := Spare(p, "Tinderbox", r); got != 1 {
		t.Fatalf("Spare = %d, want 1 — two tinderboxes means the banked one is a genuine spare", got)
	}
}

// TestSpareIsCappedByWhatIsActuallyBanked: an account whose only spare is in its own pack has
// nothing this mechanism can move, because a hand-out is withdrawn from a bank booth.
func TestSpareIsCappedByWhatIsActuallyBanked(t *testing.T) {
	r := DefaultReserve()
	p := Peer{Account: "a", Held: []Holding{{Item: "Bronze axe", Count: 3}}, Bank: []Holding{{Item: "Rune axe", Count: 1}}}
	if got := Spare(p, "Bronze axe", r); got != 0 {
		t.Fatalf("Spare = %d, want 0: three bronze axes in the PACK are not withdrawable", got)
	}
	// Move one to the bank and it becomes available.
	p.Bank = append(p.Bank, Holding{Item: "Bronze axe", Count: 2})
	if got := Spare(p, "Bronze axe", r); got != 2 {
		t.Fatalf("Spare = %d, want 2 banked bronze axes once they are actually in the bank", got)
	}
}

// TestSpareKeepsTheBestRungOnly: duplicates of the best rung are spare, the best rung itself
// keeps one.
func TestSpareKeepsTheBestRungOnly(t *testing.T) {
	r := DefaultReserve()
	p := Peer{Account: "a", Bank: []Holding{{Item: "Rune axe", Count: 3}, {Item: "Iron axe", Count: 2}}}
	if got := Spare(p, "Rune axe", r); got != 2 {
		t.Fatalf("Spare(rune) = %d, want 2 — keep one of the best rung, the other two are spare", got)
	}
	if got := Spare(p, "Iron axe", r); got != 2 {
		t.Fatalf("Spare(iron) = %d, want 2 — every superseded rung is fully spare", got)
	}
}

// TestCoinReserveCannotDriftBelowTheFloatsItIsDerivedFrom pins ReserveCoins against the two
// existing constants, so raising either cannot silently leave this behind.
func TestCoinReserveCannotDriftBelowTheFloatsItIsDerivedFrom(t *testing.T) {
	if ReserveCoins < fisherplan.DeathFloatCoins {
		t.Fatalf("ReserveCoins %d is below fisherplan.DeathFloatCoins %d", ReserveCoins, fisherplan.DeathFloatCoins)
	}
	if ReserveCoins < donorplan.DefaultTravelReserve {
		t.Fatalf("ReserveCoins %d is below donorplan.DefaultTravelReserve %d", ReserveCoins, donorplan.DefaultTravelReserve)
	}
	if ReserveCoins < fisherplan.DeathFloatCoins+donorplan.DefaultTravelReserve {
		t.Fatalf("ReserveCoins %d must cover BOTH floats (%d + %d): an account needs its banked recovery money AND its pocket fare",
			ReserveCoins, fisherplan.DeathFloatCoins, donorplan.DefaultTravelReserve)
	}
}

// TestCoinsKeepTheReserve: coins above the float are spare, coins at or below it are not.
func TestCoinsKeepTheReserve(t *testing.T) {
	r := DefaultReserve()
	p := Peer{Account: "a", Bank: []Holding{{Item: CoinsItem, Count: ReserveCoins + 5000}}}
	if got := Spare(p, CoinsItem, r); got != 5000 {
		t.Fatalf("Spare(coins) = %d, want 5000 above the %d float", got, ReserveCoins)
	}
	p.Bank = []Holding{{Item: CoinsItem, Count: ReserveCoins}}
	if got := Spare(p, CoinsItem, r); got != 0 {
		t.Fatalf("Spare(coins) = %d, want 0 — an account at exactly its death float parts with nothing", got)
	}
}

// TestNamedKeepBeatsTheFamilyRule: an operator protecting a specific rung makes the holder
// MORE careful, never less.
func TestNamedKeepBeatsTheFamilyRule(t *testing.T) {
	r := DefaultReserve()
	r.Keep["bronze axe"] = 2
	p := Peer{Account: "a", Bank: []Holding{{Item: "Bronze axe", Count: 3}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	if got := Spare(p, "Bronze axe", r); got != 1 {
		t.Fatalf("Spare = %d, want 1: superseded, but the operator asked for two to be kept", got)
	}
}

// ---- 2. nearest by what ----

// TestNearestUsesRealTravelCostNotStraightLine is the second negative control the brief asks
// for: an account must NOT be sent across the world to a "nearer" peer that is actually
// further in travel time.
//
// The fixture is the real shape of this map. `island` is 40 tiles away in a straight line but
// across water — a fare and a boat, which webwalk.DefaultCost prices at 36 run-tile units for
// the KindShip edge alone. `overland` is 60 tiles away but walkable. Chebyshev picks the
// island and is wrong; the injected route cost picks overland and is right.
func TestNearestUsesRealTravelCostNotStraightLine(t *testing.T) {
	me := at(2800, 3430)
	island := Peer{Account: "islander", At: at(2840, 3430), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	overland := Peer{Account: "walker", At: at(2860, 3430), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}

	// A real planner: the island leg costs its 40 tiles PLUS a ship crossing.
	route := func(from, to tile.Tile) (int, bool) {
		d := tile.Chebyshev(from, to)
		if to == island.At {
			return d + 36 + 36, true // out and back across the water
		}
		return d, true
	}

	// Sanity: the straight-line answer really does prefer the island, so this test is
	// measuring a genuine difference rather than agreeing with itself.
	if tile.Chebyshev(me, island.At) >= tile.Chebyshev(me, overland.At) {
		t.Fatal("fixture is wrong: the island must be strictly nearer in a straight line")
	}

	req := Request{Asker: "me", At: me, Located: true, Item: "Bronze axe", Qty: 1}
	c := Choose(req, []Peer{island, overland}, DefaultReserve(), route, Options{MuleCost: NoMuleCost})
	if c.Verdict != AskPeer {
		t.Fatalf("verdict = %v (%s), want AskPeer", c.Verdict, c.Reason)
	}
	best, _ := c.Best()
	if best.Peer.Account != "walker" {
		t.Fatalf("chose %q at cost %d; want the overland peer — a ship crossing is not 40 tiles",
			best.Peer.Account, best.Cost)
	}

	// The same fixture under the straight-line function picks the WRONG one, which is the
	// mistake this design question exists to avoid.
	cheb := Choose(req, []Peer{island, overland}, DefaultReserve(), chebCost, Options{MuleCost: NoMuleCost})
	wrong, _ := cheb.Best()
	if wrong.Peer.Account != "islander" {
		t.Fatalf("expected the straight-line cost to pick the island (that is the bug being avoided), got %q", wrong.Peer.Account)
	}
}

// TestUnreachablePeerIsDroppedNotGuessed: a peer the planner cannot route to is excluded.
func TestUnreachablePeerIsDroppedNotGuessed(t *testing.T) {
	unreachable := Peer{Account: "marooned", At: at(2801, 3430), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	noRoute := func(from, to tile.Tile) (int, bool) { return 0, false }
	c := Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{unreachable}, DefaultReserve(), noRoute, Options{MuleCost: 500})
	if c.Verdict != GoToMule {
		t.Fatalf("verdict = %v (%s), want GoToMule — an unreachable peer is not a candidate", c.Verdict, c.Reason)
	}
}

// TestTooFarFallsBackToTheMule: past MaxCost the account keeps working / uses the mule.
func TestTooFarFallsBackToTheMule(t *testing.T) {
	far := Peer{Account: "distant", At: at(9000, 9000), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	c := Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{far}, DefaultReserve(), chebCost, Options{MuleCost: 83})
	if c.Verdict != GoToMule {
		t.Fatalf("verdict = %v (%s), want GoToMule", c.Verdict, c.Reason)
	}
}

// ---- 3. contention ----

// TestBusyHolderIsExcludedAndTheNextIsOffered: an account serves one trade at a time, and a
// caller that loses a turn must have somewhere else to go in the SAME pass.
func TestBusyHolderIsExcludedAndTheNextIsOffered(t *testing.T) {
	near := Peer{Account: "near", At: at(2805, 3430), Located: true, Busy: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	next := Peer{Account: "next", At: at(2815, 3430), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	c := Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{near, next}, DefaultReserve(), chebCost, Options{MuleCost: NoMuleCost})
	if c.Verdict != AskPeer {
		t.Fatalf("verdict = %v (%s)", c.Verdict, c.Reason)
	}
	if len(c.Candidates) != 1 || c.Candidates[0].Peer.Account != "next" {
		t.Fatalf("candidates = %+v, want only the free peer", c.Candidates)
	}
}

// TestCandidatesAreRankedSoContendersAgree: two accounts deciding in the same instant must
// produce the SAME order, so they contend predictably instead of scattering.
func TestCandidatesAreRankedSoContendersAgree(t *testing.T) {
	mk := func(name string, x int) Peer {
		return Peer{Account: name, At: at(x, 3430), Located: true,
			Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	}
	peers := []Peer{mk("charlie", 2830), mk("alpha", 2810), mk("bravo", 2820)}
	req := func(name string) Request {
		return Request{Asker: name, At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1}
	}
	a := Choose(req("me1"), peers, DefaultReserve(), chebCost, Options{MuleCost: NoMuleCost})
	// Same peers, different slice order: the answer must not depend on it.
	shuffled := []Peer{peers[1], peers[2], peers[0]}
	b := Choose(req("me2"), shuffled, DefaultReserve(), chebCost, Options{MuleCost: NoMuleCost})
	if len(a.Candidates) != 3 || len(b.Candidates) != 3 {
		t.Fatalf("want 3 candidates each, got %d and %d", len(a.Candidates), len(b.Candidates))
	}
	for i := range a.Candidates {
		if a.Candidates[i].Peer.Account != b.Candidates[i].Peer.Account {
			t.Fatalf("ranking disagrees at %d: %q vs %q", i, a.Candidates[i].Peer.Account, b.Candidates[i].Peer.Account)
		}
	}
	if a.Candidates[0].Peer.Account != "alpha" {
		t.Fatalf("nearest should rank first, got %q", a.Candidates[0].Peer.Account)
	}
}

// TestBusyRetryMatchesTheMarketStallLesson: losing a turn is not a refusal.
func TestBusyRetryMatchesTheMarketStallLesson(t *testing.T) {
	if BusyRetry != 90*time.Second {
		t.Fatalf("BusyRetry = %v; woodcutteraxe.go's axeBusyRetry is 90s for the same situation and the two should read as one number", BusyRetry)
	}
	if TurnTTL <= BusyRetry {
		t.Fatalf("TurnTTL %v must outlast BusyRetry %v, or a turn lapses under an asker that is still walking", TurnTTL, BusyRetry)
	}
}

// ---- 4. who interrupts whom ----

// TestAMidTaskHolderLosesToAnEquallyDistantIdleOne.
func TestAMidTaskHolderLosesToAnEquallyDistantIdleOne(t *testing.T) {
	busy := Peer{Account: "chopping", At: at(2810, 3430), Located: true, Working: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	idle := Peer{Account: "atbank", At: at(2810, 3431), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	c := Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{busy, idle}, DefaultReserve(), chebCost, Options{MuleCost: NoMuleCost})
	best, _ := c.Best()
	if best.Peer.Account != "atbank" {
		t.Fatalf("chose %q; an equally distant holder standing at a bank should win", best.Peer.Account)
	}
}

// TestAPeerThatMerelyTiesTheMuleIsNotWorthInterrupting.
func TestAPeerThatMerelyTiesTheMuleIsNotWorthInterrupting(t *testing.T) {
	p := Peer{Account: "tie", At: at(2883, 3430), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	// 83 tiles away, exactly the live Seers->Catherby mule walk.
	c := Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{p}, DefaultReserve(), chebCost, Options{MuleCost: 83})
	if c.Verdict != GoToMule {
		t.Fatalf("verdict = %v (%s), want GoToMule — a draw is not a win", c.Verdict, c.Reason)
	}
	// Bring it well inside the margin and it becomes worth it.
	p.At = at(2820, 3430)
	c = Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{p}, DefaultReserve(), chebCost, Options{MuleCost: 83})
	if c.Verdict != AskPeer {
		t.Fatalf("verdict = %v (%s), want AskPeer at 20 tiles against the mule's 83", c.Verdict, c.Reason)
	}
}

// TestStaleRowsAreNotWalkedOn.
func TestStaleRowsAreNotWalkedOn(t *testing.T) {
	stale := Peer{Account: "stale", At: at(2805, 3430), Located: true, Age: DefaultMaxAge + time.Minute,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	c := Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{stale}, DefaultReserve(), chebCost, Options{MuleCost: 83})
	if c.Verdict != GoToMule {
		t.Fatalf("verdict = %v (%s), want GoToMule — an hour-old bank reading is a guess", c.Verdict, c.Reason)
	}
}

// TestNeverAsksItself.
func TestNeverAsksItself(t *testing.T) {
	me := Peer{Account: "me", At: at(2800, 3430), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 2}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	c := Choose(Request{Asker: "ME", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		[]Peer{me}, DefaultReserve(), chebCost, Options{MuleCost: 83})
	if c.Verdict != GoToMule {
		t.Fatalf("verdict = %v (%s) — an account must never be ranked against itself, whatever the case of its name", c.Verdict, c.Reason)
	}
}

// ---- the holder's answer ----

func TestDecideServeClampsAndSaysSo(t *testing.T) {
	r := DefaultReserve()
	p := Peer{Account: "holder", Bank: []Holding{{Item: "Bronze axe", Count: 2}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	d := DecideServe(p, "asker", "Bronze axe", 5, r)
	if d.Verdict != Serve {
		t.Fatalf("verdict = %v (%s), want Serve", d.Verdict, d.Reason)
	}
	if d.Qty != 2 {
		t.Fatalf("Qty = %d, want 2 — clamped to the actual spare, not the 5 asked for", d.Qty)
	}
	if len(d.Reason) > 80 {
		t.Fatalf("reason is %d chars; mulebus.ReasonMax is 80", len(d.Reason))
	}
}

func TestDecideServeDistinguishesNoneFromOnlyMine(t *testing.T) {
	r := DefaultReserve()
	none := DecideServe(Peer{Account: "h"}, "asker", "Bronze axe", 1, r)
	only := DecideServe(Peer{Account: "h", Bank: []Holding{{Item: "Bronze axe", Count: 1}}}, "asker", "Bronze axe", 1, r)
	if none.Verdict != Refuse || only.Verdict != Refuse {
		t.Fatalf("both should refuse: %v / %v", none.Verdict, only.Verdict)
	}
	if none.Reason == only.Reason {
		t.Fatalf("both refusals read %q; 'I have none' and 'I only have the one I am using' need different operator responses", none.Reason)
	}
}

func TestDecideServeRefusesItself(t *testing.T) {
	r := DefaultReserve()
	p := Peer{Account: "holder", Bank: []Holding{{Item: "Bronze axe", Count: 5}}}
	if d := DecideServe(p, "HOLDER", "Bronze axe", 1, r); d.Verdict != Refuse {
		t.Fatalf("verdict = %v, want Refuse", d.Verdict)
	}
}

// TestNothingToDoWhenThereIsNoPeerAndNoMule.
func TestNothingToDoWhenThereIsNoPeerAndNoMule(t *testing.T) {
	c := Choose(Request{Asker: "me", At: at(2800, 3430), Located: true, Item: "Bronze axe", Qty: 1},
		nil, DefaultReserve(), chebCost, Options{MuleCost: NoMuleCost})
	if c.Verdict != NothingToDo {
		t.Fatalf("verdict = %v (%s), want NothingToDo", c.Verdict, c.Reason)
	}
}

// TestNoPositionFixFallsBackRatherThanGuessing.
func TestNoPositionFixFallsBackRatherThanGuessing(t *testing.T) {
	p := Peer{Account: "p", At: at(2805, 3430), Located: true,
		Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}}}
	c := Choose(Request{Asker: "me", Item: "Bronze axe", Qty: 1}, []Peer{p}, DefaultReserve(), chebCost, Options{MuleCost: 83})
	if c.Verdict != GoToMule {
		t.Fatalf("verdict = %v (%s), want GoToMule", c.Verdict, c.Reason)
	}
}

// ---- the fleet-scale case, as a decision rather than a claim ----

// TestTheDozenSpareAxesReachTheNinetyFourAskers is the acceptance test stated as the fleet
// actually is: 94 accounts need a bronze axe, 12 have one spare, one mule has none. It asserts
// the two things that make the feature work at that scale — every asker finds a holder, and
// they do NOT all pick the same one once turn-taking is applied.
func TestTheDozenSpareAxesReachTheNinetyFourAskers(t *testing.T) {
	r := DefaultReserve()
	var holders []Peer
	for i := 0; i < 12; i++ {
		holders = append(holders, Peer{
			Account: "holder" + itoa(i), At: at(2800+i*3, 3430), Located: true,
			Bank: []Holding{{Item: "Bronze axe", Count: 1}}, Held: []Holding{{Item: "Rune axe", Count: 1}},
		})
	}
	// Turn-taking, modelled the way peerbank.Directory enforces it: one asker per holder.
	taken := map[string]bool{}
	served, mule := 0, 0
	for i := 0; i < 94; i++ {
		me := Request{Asker: "asker" + itoa(i), At: at(2790+i%20, 3430), Located: true, Item: "Bronze axe", Qty: 1}
		peers := make([]Peer, len(holders))
		copy(peers, holders)
		for j := range peers {
			peers[j].Busy = taken[peers[j].Account]
		}
		// MuleCost NoMuleCost: the live mule holds no axe at all, which is the case.
		c := Choose(me, peers, r, chebCost, Options{MuleCost: NoMuleCost})
		if best, ok := c.Best(); ok && c.Verdict == AskPeer {
			taken[best.Peer.Account] = true
			served++
		} else {
			mule++
		}
	}
	if served != 12 {
		t.Fatalf("served %d askers in one round, want 12 — one per holder, which is what one-trade-at-a-time means", served)
	}
	if mule != 82 {
		t.Fatalf("%d askers fell back, want 82", mule)
	}
	// The point of the ranking: the 12 served did NOT all pick the same holder.
	if len(taken) != 12 {
		t.Fatalf("the served askers used %d distinct holders, want 12 — all picking one holder is the bottleneck rebuilt", len(taken))
	}
}
