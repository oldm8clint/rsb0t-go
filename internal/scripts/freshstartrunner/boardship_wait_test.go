package freshstartrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// The live failure these guard, measured 2026-09-23 on build 3dfb543a and reproduced by
// cmd/sailtest: a boarding dialogue knocked onto its refusal branch ("Does Karamja have unusual
// customs then?" -> "I'm not that sort of customs officer.") closed with the fare still in the
// pack, and BoardShip reported a sail on its way because "a dialogue we drove has closed".
// SailLeg then waited 35 seconds for a teleport nobody had paid for — 201 times in 40 minutes.

// boardDeps is a session with nothing on screen. Its Delay refuses, as a stopping bot's does, so
// the settle and screen waits end at once instead of spinning out their full windows.
func boardDeps() *Deps {
	return &Deps{Session: &sailThrottleSession{}, Delay: func(time.Duration) bool { return false }}
}

// THE CONTROL. origin/main returned (true, true) here: a closed dialogue after an answer was
// taken as a sail. Nothing was paid, so it must not be.
func TestAClosedBoardingDialogueIsNotASailByItself(t *testing.T) {
	step := 2
	done, boarded := BoardShip(boardDeps(), freshstart.LegDestination, &step)
	if boarded {
		t.Fatal("a boarding dialogue that closed with the fare still in the pack was reported as a sail " +
			"on its way; SailLeg would now wait 35s for a teleport nobody paid for")
	}
	if done {
		t.Fatal("reported nothing left to do at a dock where boarding just failed; it must board again")
	}
	if step != 0 {
		t.Fatalf("step = %d after the dialogue ended, want 0 so the next dialogue is counted from scratch", step)
	}
}

func TestAnUnpaidCloseCountsAsAFailedAttempt(t *testing.T) {
	d := boardDeps()
	a := &boardAttempt{}
	a.begin(0, tile.Tile{X: 2772, Z: 3234}, time.Now())
	a.sawDialogue = true
	a.step = 1
	done, boarded := boardShip(d, freshstart.LegDestination, a)
	if done || boarded {
		t.Fatalf("unpaid close gave done=%v boarded=%v, want false/false", done, boarded)
	}
	if a.fails != 1 {
		t.Fatalf("fails = %d, want 1 — the fallback counts these", a.fails)
	}
	if !a.talkedAt.IsZero() || a.inDialogue() {
		t.Fatal("the attempt was not ended; the next Talk-to would be judged against this one")
	}
}

// And the other half: a close after the fare left the pack IS the sail, and it clears the
// failure streak.
func TestAClosedDialogueThatTookTheFareIsASail(t *testing.T) {
	d := boardDeps()
	a := &boardAttempt{fails: 3}
	a.begin(shipFare, tile.Tile{X: 2772, Z: 3234}, time.Now()) // held 30 at the Talk-to, 0 now
	a.sawDialogue = true
	a.step = 3
	done, boarded := boardShip(d, freshstart.LegDestination, a)
	if !done || !boarded {
		t.Fatalf("paid close gave done=%v boarded=%v, want true/true", done, boarded)
	}
	if a.fails != 0 {
		t.Fatalf("fails = %d after a paid fare, want 0", a.fails)
	}
}

func TestFareTakenNeedsEvidence(t *testing.T) {
	for _, c := range []struct {
		name          string
		before, after int
		main          int
		msgs          string
		want          bool
	}{
		{"coins left the pack", 60, 30, -1, "", true},
		{"coins unchanged, refusal branch", 30, 30, -1, "", false},
		{"no snapshot, nothing else", -1, 30, -1, "", false},
		{"ship interface open", -1, 30, shipJourneyInterface, "", true},
		{"pay message (customs)", -1, 30, -1, "You pay 30 coins and board the ship.\n", true},
		{"pay message (sailors)", -1, 30, -1, "You pay the 30 coins...\n", true},
		{"some other main modal", 30, 30, 1234, "I can't reach that!\n", false},
		{"lost less than a fare", 45, 30, -1, "", false},
	} {
		if got := fareTaken(c.before, c.after, c.main, c.msgs); got != c.want {
			t.Errorf("%s: fareTaken = %v, want %v", c.name, got, c.want)
		}
	}
}

// A sail lands on a deck one level up and 70-90 tiles away; stepping up to a wandering
// officer is a few tiles on the same level.
func TestSailLanded(t *testing.T) {
	dock := freshstart.BrimhavenPier
	if !sailLanded(dock, freshstart.ArdougneArrival) {
		t.Error("the Ardougne deck after a Brimhaven fare is a landing")
	}
	if !sailLanded(tile.Tile{X: 3027, Z: 3218}, tile.Tile{X: 2956, Z: 3143, Level: 1}) {
		t.Error("the Karamja deck after a Port Sarim fare is a landing")
	}
	if sailLanded(dock, tile.Tile{X: dock.X + 1, Z: dock.Z - 4}) {
		t.Error("walking up to the officer is not a landing")
	}
	if sailLanded(dock, dock) {
		t.Error("standing still is not a landing")
	}
}

func TestCrossedSince(t *testing.T) {
	brim, ardy := freshstart.BrimhavenPier, tile.Tile{X: 2683, Z: 3271}
	if !crossedSince(brim, ardy, freshstart.LegDestination) {
		t.Error("Brimhaven -> Ardougne is across the destination leg")
	}
	if crossedSince(brim, tile.Tile{X: 2773, Z: 3230}, freshstart.LegDestination) {
		t.Error("still on the Brimhaven dock is not across")
	}
	sarim, musa := tile.Tile{X: 3027, Z: 3218}, tile.Tile{X: 2956, Z: 3146}
	if !crossedSince(sarim, musa, freshstart.LegPier) {
		t.Error("Port Sarim -> Musa Point is across the pier leg")
	}
	if crossedSince(sarim, tile.Tile{X: 3028, Z: 3221}, freshstart.LegPier) {
		t.Error("still at Port Sarim is not across")
	}
}

func TestFallbackBackoffGrowsAndIsCapped(t *testing.T) {
	prev := time.Duration(0)
	for n := 1; n <= 10; n++ {
		b := fallbackBackoff(n)
		if b < prev {
			t.Fatalf("backoff shrank at %d: %v < %v", n, b, prev)
		}
		if b > sailFallbackBackoffMax {
			t.Fatalf("backoff %v at %d exceeds the cap %v", b, n, sailFallbackBackoffMax)
		}
		prev = b
	}
	if fallbackBackoff(0) != sailFallbackBackoff {
		t.Fatal("a zero count must still back off")
	}
}

// The arrival box ("The ship arrives at Ardougne.") is a dialogue that opens with no Talk-to
// in flight. It must not later read as a boarding dialogue that closed without the fare.
func TestOnlyADialogueAfterOurTalkToCounts(t *testing.T) {
	var a boardAttempt
	a.noteDialogue()
	if a.inDialogue() {
		t.Fatal("a dialogue with no Talk-to in flight was counted as a boarding dialogue")
	}
	a.begin(30, tile.Tile{}, time.Now())
	a.noteDialogue()
	if !a.inDialogue() {
		t.Fatal("the dialogue our own Talk-to opened was not counted")
	}
	a.fails = 2
	a.end()
	if a.inDialogue() || !a.talkedAt.IsZero() {
		t.Fatal("end left the attempt open")
	}
	if a.fails != 2 {
		t.Fatalf("end cleared the failure streak (%d); only a paid fare or a landing may", a.fails)
	}
}

// The waits are what stop the double click. They must be long enough to cover the proxy
// routes' round trip plus a few ticks of walking, and the fallback must not be reachable in
// fewer attempts than the old loop took per single failure.
func TestBoardingWaitsCoverAProxyRoundTrip(t *testing.T) {
	if boardTalkWait < 4*time.Second {
		t.Errorf("boardTalkWait %v cannot cover a few 600ms ticks of walking plus a ~1s round trip", boardTalkWait)
	}
	if boardScreenWait < 2*time.Second {
		t.Errorf("boardScreenWait %v is shorter than a proxy round trip plus a tick", boardScreenWait)
	}
	if boardFareSettle < 1200*time.Millisecond {
		t.Errorf("boardFareSettle %v is under two ticks: the IF_OPENMAIN and the coins can arrive a tick after the IF_CLOSE", boardFareSettle)
	}
	if sailFallbackAfter < 2 {
		t.Errorf("sailFallbackAfter %d gives up on boarding after a single miss", sailFallbackAfter)
	}
}
