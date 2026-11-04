package worldhopplan

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

// base is a view of an account at home on world 1, in a deployment that really has worlds.
func base() View {
	return View{
		State:    State{Home: 1, Current: 1},
		Now:      t0,
		Distinct: true,
	}
}

func TestStaysWhenAlreadyOnTheMulesWorld(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 1, true
	if d := Decide(v); d.Action != ActionStay {
		t.Fatalf("got %s (%s), want stay", d.Action, d.Why)
	}
}

func TestHopsToTheMulesWorld(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	d := Decide(v)
	if d.Action != ActionHop || d.World != 2 {
		t.Fatalf("got %s world %d (%s), want hop to 2", d.Action, d.World, d.Why)
	}
}

// The local-engine guard: without distinct per-world URLs a hop changes nothing, so it must
// be refused in the plan rather than performed and reported as a two-world proof.
func TestNeverHopsWhenTheDeploymentHasOnlyOneWorld(t *testing.T) {
	v := base()
	v.Distinct = false
	v.WantWorld, v.WantKnown = 2, true
	d := Decide(v)
	if d.Action != ActionStay {
		t.Fatalf("got %s (%s), want stay on a single-world deployment", d.Action, d.Why)
	}
}

// "I do not know where the mule is" must not become "the mule is on world 1".
func TestUnknownMuleWorldStaysPutAtHome(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 0, false
	if d := Decide(v); d.Action != ActionStay {
		t.Fatalf("got %s (%s), want stay", d.Action, d.Why)
	}
}

// ...but an account already away from home with no idea where the mule is should come back,
// because home is where its bank and its script's world are.
func TestUnknownMuleWorldSendsAnAwayAccountHome(t *testing.T) {
	v := base()
	v.State.Current = 2
	v.WantKnown = false
	d := Decide(v)
	if d.Action != ActionHome || d.World != 1 {
		t.Fatalf("got %s world %d (%s), want home to 1", d.Action, d.World, d.Why)
	}
}

func TestMinIntervalBlocksAnImmediateSecondHop(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.LastAt = t0.Add(-10 * time.Second)
	d := Decide(v)
	if d.Action != ActionWait {
		t.Fatalf("got %s (%s), want wait", d.Action, d.Why)
	}
	if d.Wait <= 0 || d.Wait > DefaultLimits.MinInterval {
		t.Fatalf("wait = %s, want in (0, %s]", d.Wait, DefaultLimits.MinInterval)
	}
}

// The flapping bound: once the hop budget is spent the account lands, it does not hop again.
func TestHopBudgetIsBoundedAndLandsTheAccountHome(t *testing.T) {
	v := base()
	// Away from home and wanting to come back, with the hop budget already spent. This is
	// the flapping shape: the account must land, not keep bouncing.
	v.State.Current = 2
	v.WantWorld, v.WantKnown = 1, true
	for i := 0; i < DefaultLimits.MaxHops; i++ {
		v.State.Recent = append(v.State.Recent, t0.Add(-time.Duration(i+1)*time.Minute))
	}
	v.State.LastAt = t0.Add(-DefaultLimits.MinInterval - time.Second)
	d := Decide(v)
	if d.Action != ActionHome || d.World != 1 {
		t.Fatalf("got %s world %d (%s), want one last hop home", d.Action, d.World, d.Why)
	}
	// And once that home leg is spent too, it lands where it stands rather than hopping.
	v.State.HomeLegSpent = true
	d = Decide(v)
	if d.Action != ActionGiveUpHere || d.World != 2 {
		t.Fatalf("got %s world %d (%s), want give-up-here on 2", d.Action, d.World, d.Why)
	}
}

// Hops outside the window do not count against the budget.
func TestOldHopsFallOutOfTheWindow(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	for i := 0; i < DefaultLimits.MaxHops+3; i++ {
		v.State.Recent = append(v.State.Recent, t0.Add(-DefaultLimits.Window-time.Duration(i+1)*time.Minute))
	}
	if d := Decide(v); d.Action != ActionHop {
		t.Fatalf("got %s (%s), want hop — every recorded hop is older than the window", d.Action, d.Why)
	}
}

// A failed leg retries after RetryWait, and only up to MaxAttempts.
func TestFailedLegRetriesThenLands(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.Leg, v.State.Last = 2, OutcomeFailed
	v.State.Attempts = 1
	v.State.LastAt = t0.Add(-time.Second)
	if d := Decide(v); d.Action != ActionWait {
		t.Fatalf("got %s (%s), want wait before a retry", d.Action, d.Why)
	}
	v.State.LastAt = t0.Add(-DefaultLimits.RetryWait - time.Second)
	d := Decide(v)
	if d.Action != ActionHop || d.World != 2 {
		t.Fatalf("got %s world %d (%s), want a retry hop to 2", d.Action, d.World, d.Why)
	}
	// Attempts exhausted, still at home: just work at home.
	v.State.Attempts = DefaultLimits.MaxAttempts
	d = Decide(v)
	if d.Action != ActionGiveUpHere || d.World != 1 {
		t.Fatalf("got %s world %d (%s), want give-up-here at home", d.Action, d.World, d.Why)
	}
}

// The livelock case the owner named: an account that cannot get back to its own world must
// end up somewhere useful, not flap.
func TestCannotGetHomeLandsOnTheWorldItIsOn(t *testing.T) {
	v := base()
	v.State.Current = 2
	v.State.Leg = 1 // the home leg
	v.State.Attempts = DefaultLimits.MaxAttempts
	v.State.Last = OutcomeFailed
	d := Decide(v)
	if d.Action != ActionGiveUpHere || d.World != 2 {
		t.Fatalf("got %s world %d (%s), want give-up-here on world 2", d.Action, d.World, d.Why)
	}
}

// Response 5 buys a wait, never an immediate retry, and a repeat stops the account.
func TestResponse5WaitsThenStopsTheAccount(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.Leg, v.State.Last, v.State.Attempts = 2, OutcomeAlreadyLoggedIn, 1
	v.State.LastAt = t0.Add(-time.Second)
	d := Decide(v)
	if d.Action != ActionWait {
		t.Fatalf("got %s (%s), want a wait after response 5", d.Action, d.Why)
	}
	if d.Wait <= DefaultLimits.RetryWait {
		t.Fatalf("response 5 wait %s should be longer than the ordinary retry wait %s", d.Wait, DefaultLimits.RetryWait)
	}
	v.State.Attempts = 2
	d = Decide(v)
	if d.Action != ActionStop {
		t.Fatalf("got %s (%s), want stop on a repeat response 5", d.Action, d.Why)
	}
}

// A connection ceiling is not this account's fault and not something a retry fixes: it
// lands and keeps earning rather than queueing for a slot it cannot free.
func TestConnectionCeilingLandsRatherThanRetrying(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.Leg, v.State.Last, v.State.Attempts = 2, OutcomeCeiling, 1
	d := Decide(v)
	if d.Action != ActionGiveUpHere || d.World != 1 {
		t.Fatalf("got %s world %d (%s), want give-up-here at home", d.Action, d.World, d.Why)
	}
}

func TestTerminalLoginFailureStops(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.Leg, v.State.Last, v.State.Attempts = 2, OutcomeTerminal, 1
	if d := Decide(v); d.Action != ActionStop {
		t.Fatalf("got %s (%s), want stop", d.Action, d.Why)
	}
}

// A full round trip: hop out, trade, hop home, and the state ends where it started.
func TestRoundTripReturnsToTheStartingState(t *testing.T) {
	s := State{Home: 1, Current: 1}
	s = Begin(s, 2, t0)
	s = Attempted(s, OutcomeOK, t0.Add(10*time.Second))
	if s.Current != 2 || s.Leg != 0 {
		t.Fatalf("after hopping out: current %d leg %d", s.Current, s.Leg)
	}
	s = Begin(s, 1, t0.Add(5*time.Minute))
	s = Attempted(s, OutcomeOK, t0.Add(5*time.Minute+10*time.Second))
	if s.Current != 1 || s.Leg != 0 || s.HomeLegSpent {
		t.Fatalf("after hopping home: %+v", s)
	}
	if got := hopsIn(s.Recent, t0.Add(6*time.Minute), DefaultLimits.Window); got != 2 {
		t.Fatalf("a round trip is 2 hops, counted %d", got)
	}
}

// State.Recent must not grow without bound on an account running for a day.
func TestRecentIsTrimmedToTheWindow(t *testing.T) {
	s := State{Home: 1, Current: 1}
	now := t0
	for i := 0; i < 500; i++ {
		now = now.Add(time.Minute)
		s = Begin(s, 2, now)
		s = Attempted(s, OutcomeOK, now)
		s.Current = 1 // pretend it came back, so the next Begin is a fresh hop
	}
	if len(s.Recent) > int(DefaultLimits.Window/time.Minute)+2 {
		t.Fatalf("Recent grew to %d entries; it should be bounded by the window", len(s.Recent))
	}
}

func TestMuleWorldTakesTheNewestFreshAnnouncement(t *testing.T) {
	seen := []Presence{
		{Name: "mulecon", World: 1, At: t0.Add(-20 * time.Second)},
		{Name: "mulecon", World: 2, At: t0.Add(-5 * time.Second)},
		{Name: "someoneelse", World: 7, At: t0},
	}
	w, ok := MuleWorld("mulecon", seen, t0, DefaultLimits.MuleWorldTTL)
	if !ok || w != 2 {
		t.Fatalf("got %d,%v want 2,true", w, ok)
	}
}

func TestMuleWorldIgnoresStaleAnnouncements(t *testing.T) {
	seen := []Presence{{Name: "mulecon", World: 2, At: t0.Add(-10 * time.Minute)}}
	if _, ok := MuleWorld("mulecon", seen, t0, DefaultLimits.MuleWorldTTL); ok {
		t.Fatal("a ten-minute-old announcement should not be believed")
	}
}

// The single most important case in this file: a heartbeat that carries no world is a
// publisher that never mentioned one, NOT a publisher on world 1.
func TestMuleWorldZeroIsUnknownNotWorldOne(t *testing.T) {
	seen := []Presence{{Name: "mulecon", World: 0, At: t0}}
	w, ok := MuleWorld("mulecon", seen, t0, DefaultLimits.MuleWorldTTL)
	if ok || w != 0 {
		t.Fatalf("got %d,%v want 0,false — a missing world is unknown, not world 1", w, ok)
	}
}

func TestMuleWorldMatchesNamesTheWayTheUserhashDoes(t *testing.T) {
	seen := []Presence{{Name: "Mule_Con", World: 2, At: t0}}
	if w, ok := MuleWorld("mule con", seen, t0, DefaultLimits.MuleWorldTTL); !ok || w != 2 {
		t.Fatalf("got %d,%v want 2,true", w, ok)
	}
	if _, ok := MuleWorld("mulecon", seen, t0, DefaultLimits.MuleWorldTTL); ok {
		t.Fatal("different-length names must not match")
	}
}

func TestLimitsDefaultsFillOnlyZeroFields(t *testing.T) {
	l := Limits{MaxHops: 9}.withDefaults()
	if l.MaxHops != 9 {
		t.Errorf("MaxHops overwritten: %d", l.MaxHops)
	}
	if l.MaxAttempts != DefaultLimits.MaxAttempts || l.Window != DefaultLimits.Window {
		t.Errorf("defaults not filled: %+v", l)
	}
}

// The server's own hop timer: a wait with a number attached, costing the leg nothing.
func TestHopTimerWaitsTheSeconds(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.Leg, v.State.Last = 2, OutcomeHopTimer
	v.State.LastAt = t0.Add(-10 * time.Second)
	v.HopTimerWait = 31 * time.Second

	d := Decide(v)
	if d.Action != ActionWait {
		t.Fatalf("got %s (%s), want wait", d.Action, d.Why)
	}
	if d.Wait != 21*time.Second {
		t.Fatalf("wait = %s, want the 21s left of the server's 31", d.Wait)
	}
	// Once it has elapsed the hop resumes on the same leg.
	v.State.LastAt = t0.Add(-40 * time.Second)
	if d := Decide(v); d.Action != ActionHop || d.World != 2 {
		t.Fatalf("got %s world %d (%s), want a hop to 2 once the timer expired", d.Action, d.World, d.Why)
	}
}

// With no number from the server, the engine's own 45s default applies.
func TestHopTimerFallsBackToTheEngineDefault(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.Leg, v.State.Last, v.State.LastAt = 2, OutcomeHopTimer, t0
	d := Decide(v)
	if d.Action != ActionWait || d.Wait != DefaultHopTimer {
		t.Fatalf("got %s wait %s, want a %s wait", d.Action, d.Wait, DefaultHopTimer)
	}
}

// A hop timer must not spend the leg's attempts: three of them at 45s each would end a hop
// that was only ever being asked to wait.
func TestHopTimerDoesNotConsumeTheLegsAttempts(t *testing.T) {
	s := Begin(State{Home: 1, Current: 1}, 2, t0)
	for i := 0; i < 5; i++ {
		s = Attempted(s, OutcomeHopTimer, t0.Add(time.Duration(i)*time.Minute))
	}
	if s.Attempts != 0 {
		t.Fatalf("five hop timers spent %d attempts, want 0", s.Attempts)
	}
	if s.Leg != 2 {
		t.Fatalf("the leg was abandoned: %d", s.Leg)
	}
}

// A full world is a reason to be elsewhere, never a wait and never a rate verdict.
func TestAFullWorldLandsTheAccountRatherThanQueueing(t *testing.T) {
	v := base()
	v.WantWorld, v.WantKnown = 2, true
	v.State.Leg, v.State.Last, v.State.Attempts = 2, OutcomeWorldFull, 1
	d := Decide(v)
	if d.Action != ActionGiveUpHere || d.World != 1 {
		t.Fatalf("got %s world %d (%s), want give-up-here at home", d.Action, d.World, d.Why)
	}

	// And an account whose own home world has filled up while it was away stays where it is
	// and works, rather than bouncing off a full world forever.
	v.State.Current, v.State.Leg = 2, 1
	d = Decide(v)
	if d.Action != ActionGiveUpHere || d.World != 2 {
		t.Fatalf("got %s world %d (%s), want give-up-here on world 2", d.Action, d.World, d.Why)
	}
}

// ---------------------------------------------------------------------------
// Full-world failover (the live 2026-09-22 incident: 768 response-7 rejections)
// ---------------------------------------------------------------------------

func TestNextOpenWorldSkipsTheFullOneAndTheCurrentOne(t *testing.T) {
	full := map[int]time.Time{1: t0.Add(-5 * time.Second)}
	got, ok := NextOpenWorld(1, []int{1, 2, 3}, full, t0, DefaultFullWorldTTL, 0)
	if !ok {
		t.Fatal("worlds 2 and 3 have room; something must be chosen")
	}
	if got == 1 {
		t.Fatal("chose the world that just refused us")
	}
	if got != 2 && got != 3 {
		t.Fatalf("chose world %d, which is not in the available list", got)
	}
}

func TestNextOpenWorldRefusesWhenEveryWorldIsFull(t *testing.T) {
	full := map[int]time.Time{1: t0, 2: t0, 3: t0}
	if _, ok := NextOpenWorld(1, []int{1, 2, 3}, full, t0, DefaultFullWorldTTL, 0); ok {
		t.Fatal("every world is full; the caller must wait, not be handed a destination")
	}
}

func TestNextOpenWorldRefusesWithOnlyOneWorld(t *testing.T) {
	if _, ok := NextOpenWorld(1, []int{1}, nil, t0, DefaultFullWorldTTL, 0); ok {
		t.Fatal("a single-world deployment has nowhere to fail over to")
	}
}

// A world that was full ten minutes ago is worth trying again — otherwise one busy evening
// would take a world out of service permanently.
func TestAStaleFullMarkIsIgnored(t *testing.T) {
	full := map[int]time.Time{2: t0.Add(-10 * time.Minute)}
	got, ok := NextOpenWorld(1, []int{1, 2}, full, t0, DefaultFullWorldTTL, 0)
	if !ok || got != 2 {
		t.Fatalf("got %d,%v want 2,true — the mark is older than the TTL", got, ok)
	}
}

// THE STAMPEDE TEST. ~300 accounts shut out of world 1 must not all pick world 2 in the same
// instant and fill it. spread rotates each account's starting choice.
func TestSpreadDistributesAccountsAcrossTheOpenWorlds(t *testing.T) {
	available := []int{1, 2, 3, 4}
	counts := map[int]int{}
	for i := 0; i < 400; i++ {
		got, ok := NextOpenWorld(1, available, nil, t0, DefaultFullWorldTTL, i)
		if !ok {
			t.Fatal("three worlds have room")
		}
		counts[got]++
	}
	if len(counts) != 3 {
		t.Fatalf("400 accounts landed on %d distinct worlds (%v), want all 3 open ones", len(counts), counts)
	}
	for w, n := range counts {
		if n < 100 || n > 200 {
			t.Errorf("world %d took %d of 400 accounts, which is not an even spread (%v)", w, n, counts)
		}
	}
}

// The same account must make the same choice twice, or a retry loop would wander.
func TestSpreadIsStableForOneAccount(t *testing.T) {
	a, _ := NextOpenWorld(1, []int{1, 2, 3}, nil, t0, DefaultFullWorldTTL, 12345)
	b, _ := NextOpenWorld(1, []int{1, 2, 3}, nil, t0, DefaultFullWorldTTL, 12345)
	if a != b {
		t.Fatalf("the same account chose %d then %d", a, b)
	}
}

func TestAnyWorldOpen(t *testing.T) {
	if !AnyWorldOpen(1, []int{1, 2}, nil, t0, DefaultFullWorldTTL) {
		t.Error("world 2 has room")
	}
	if AnyWorldOpen(1, []int{1, 2}, map[int]time.Time{2: t0}, t0, DefaultFullWorldTTL) {
		t.Error("world 2 is full and world 1 is where we already are")
	}
}

// THE BUG THE LIVE BOUNDS PROOF CAUGHT (2026-09-22), pinned here so it cannot come back.
//
// Begin STARTS A LEG: it zeroes Attempts and records a hop against the window budget. The
// live layer was calling it on every pass of its loop, including on a RETRY of the leg
// already in progress — so Attempts never climbed, "this leg is out of attempts" never
// fired, and cmd/worldhoptest -unreachable logged "retrying world 2" for 3m20s straight.
//
// This test asserts both halves of the rule: attempts accumulate without Begin, and a Begin
// in the middle throws them away. The second half is what makes the first half fragile, and
// is why the rule belongs in a test rather than only in a comment.
func TestAttemptsAccumulateWithoutBeginAndAreLostWithIt(t *testing.T) {
	s := Begin(State{Home: 1, Current: 1}, 2, t0)
	for i := 0; i < DefaultLimits.MaxAttempts; i++ {
		s = Attempted(s, OutcomeFailed, t0.Add(time.Duration(i+1)*time.Minute))
	}
	if s.Attempts != DefaultLimits.MaxAttempts {
		t.Fatalf("attempts = %d, want %d", s.Attempts, DefaultLimits.MaxAttempts)
	}
	// With the leg exhausted the plan must land the account, not hop again.
	v := base()
	v.State = s
	v.WantWorld, v.WantKnown = 2, true
	v.Now = t0.Add(10 * time.Minute)
	if d := Decide(v); d.Action == ActionHop {
		t.Fatalf("an exhausted leg was told to hop again (%s)", d.Why)
	}

	// And this is the mistake, reproduced: a Begin on a retry throws the count away, so the
	// "out of attempts" rule can never fire however many times the leg has failed. The live
	// layer must therefore call Begin only when the DESTINATION changes, which is what
	// internal/fleet's runHop now checks.
	reset := Begin(s, 2, t0.Add(11*time.Minute))
	if reset.Attempts != 0 {
		t.Fatalf("Begin left %d attempts; it is documented to start a leg", reset.Attempts)
	}
	if reset.Last != OutcomeNone {
		t.Fatalf("Begin left the last outcome as %q, so the retry branch would still answer", reset.Last)
	}
	// Each Begin also spends one of the window's hops, which is the second reason calling it
	// per attempt is wrong: three retries would look like three separate hops.
	if hopsIn(reset.Recent, t0.Add(11*time.Minute), DefaultLimits.Window) != 2 {
		t.Fatalf("two Begins recorded %d hops, want 2", hopsIn(reset.Recent, t0.Add(11*time.Minute), DefaultLimits.Window))
	}
}
