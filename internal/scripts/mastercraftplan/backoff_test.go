package mastercraftplan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// Defect 1: a round that fails for a passing reason waits 30 minutes, then 60, then 120, then
// 180 at most, and a round that works starts the count again.
func TestBackoffForDoublesToTheCap(t *testing.T) {
	set := orderSettings()
	want := []time.Duration{0, 30 * time.Minute, time.Hour, 2 * time.Hour, 3 * time.Hour, 3 * time.Hour, 3 * time.Hour}
	for fails, w := range want {
		if got := BackoffFor(fails, set); got != w {
			t.Errorf("after %d failure(s): %s, want %s", fails, got, w)
		}
	}
	set.Backoff, set.BackoffMax = 10*time.Minute, 25*time.Minute
	if BackoffFor(1, set) != 10*time.Minute || BackoffFor(2, set) != 20*time.Minute || BackoffFor(3, set) != 25*time.Minute {
		t.Error("custom base and cap")
	}
}

func TestLedgerFailAndSucceed(t *testing.T) {
	set := orderSettings()
	var l Ledger
	if _, backed := l.BackedOff(ModeGems, t0); backed || l.Fails(ModeGems) != 0 {
		t.Fatal("a fresh ledger is not backed off")
	}
	u1 := l.Fail(ModeGems, t0, set)
	if !u1.Equal(t0.Add(30*time.Minute)) || l.Fails(ModeGems) != 1 {
		t.Fatalf("first failure: until %v fails %d", u1, l.Fails(ModeGems))
	}
	if until, backed := l.BackedOff(ModeGems, t0.Add(29*time.Minute)); !backed || !until.Equal(u1) {
		t.Fatalf("29 minutes in: %v %v", until, backed)
	}
	if _, backed := l.BackedOff(ModeGems, t0.Add(30*time.Minute)); backed {
		t.Fatal("the wait is over at its end")
	}
	u2 := l.Fail(ModeGems, t0.Add(31*time.Minute), set)
	if !u2.Equal(t0.Add(31*time.Minute + time.Hour)) {
		t.Fatalf("second failure waits an hour: until %v", u2)
	}
	// Each mode has its own count.
	if l.Fails(ModeBows) != 0 || l.Fails(ModeDeliver) != 0 {
		t.Fatal("another mode's count moved")
	}
	l.Succeed(ModeGems)
	if l.Fails(ModeGems) != 0 {
		t.Fatal("a round that worked must clear the count")
	}
	if _, backed := l.BackedOff(ModeGems, t0.Add(time.Minute)); backed {
		t.Fatal("a round that worked must clear the wait")
	}
	l.Succeed(ModeBows) // nothing to clear: no panic
	var nilLed *Ledger
	if nilLed.Fails(ModeGems) != 0 {
		t.Fatal("nil ledger")
	}
	if _, backed := nilLed.BackedOff(ModeGems, t0); backed {
		t.Fatal("nil ledger")
	}
}

// The backoff is in MasterCrafter.json: it survives a restart.
func TestLedgerBackoffSurvivesJSON(t *testing.T) {
	set := orderSettings()
	var l Ledger
	l.Fail(ModeBows, t0, set)
	l.Fail(ModeBows, t0, set)
	l.Fail(ModeDeliver, t0, set)
	data, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var back Ledger
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Fails(ModeBows) != 2 || back.Fails(ModeDeliver) != 1 {
		t.Fatalf("%s", data)
	}
	if until, backed := back.BackedOff(ModeBows, t0.Add(time.Minute)); !backed || !until.Equal(t0.Add(time.Hour)) {
		t.Fatalf("until %v backed %v", until, backed)
	}
}

func TestDecideBacksOffEveryMode(t *testing.T) {
	set := orderSettings()
	s := readySnap(t0)
	// Gems backed off: the bows are next.
	s.Ledger.Fail(ModeGems, t0, set)
	d := Decide(set, s)
	if d.Mode != ModeBows {
		t.Fatalf("gems backing off: %+v", d)
	}
	// Both backed off: ScarabSwarm, and the reason says why.
	s.Ledger.Fail(ModeBows, t0, set)
	d = Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "gem rounds are backing off") || !strings.Contains(d.Why, "bow rounds are backing off") {
		t.Fatalf("both backing off: %+v", d)
	}
	// 31 minutes on, the first failure's wait is over for both.
	s.Now = t0.Add(31 * time.Minute)
	if d := Decide(set, s); d.Mode != ModeGems {
		t.Fatalf("wait over: %+v", d)
	}
	// An uncut-gem leftover is a gem round, and a backoff holds that too.
	s = readySnap(t0)
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 3})
	s.Ledger.PendingCut = true
	s.Ledger.Fail(ModeGems, t0, set)
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Fatalf("a pending cut must wait out a gem backoff: %+v", d)
	}
}

// Defect 2: a hand-over that keeps failing no longer outranks gems and bows with no wait.
func TestDecideDeliverBacksOffAndDoesNotBlockCrafting(t *testing.T) {
	set := orderSettings()
	s := readySnap(t0)
	s.Ledger.PendingDeliver = true
	if d := Decide(set, s); d.Mode != ModeDeliver {
		t.Fatalf("a pending hand-over with no failures yet: %+v", d)
	}
	s.Ledger.Fail(ModeDeliver, t0, set)
	d := Decide(set, s)
	if d.Mode != ModeGems {
		t.Fatalf("hand-over backing off must not block the gems: %+v", d)
	}
	s.Now = t0.Add(30 * time.Minute)
	if d := Decide(set, s); d.Mode != ModeDeliver {
		t.Fatalf("the hand-over is tried again once its wait is over: %+v", d)
	}
	// With nothing else ready the reason names the wait.
	s = readySnap(t0)
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 1})
	s.Ledger.PendingDeliver = true
	s.Ledger.Fail(ModeDeliver, t0, set)
	d = Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "hand-over rounds are backing off") {
		t.Fatalf("%+v", d)
	}
}

func TestDecideGivesUpAfterTheLimitOfFailedHandovers(t *testing.T) {
	set := orderSettings() // 3
	s := readySnap(t0)
	s.Ledger.PendingDeliver = true
	for i := 0; i < 2; i++ {
		s.Ledger.Fail(ModeDeliver, t0, set)
	}
	if s.Ledger.HandoverGaveUp(set) {
		t.Fatal("two failures are not three")
	}
	s.Ledger.Fail(ModeDeliver, t0, set)
	if !s.Ledger.HandoverGaveUp(set) {
		t.Fatal("three failures in a row must give up")
	}
	// Even long after the wait, with gems ready and a hand-over pending, no round starts.
	s.Now = t0.Add(48 * time.Hour)
	d := Decide(set, s)
	if d.Mode != ModeSwarm || !d.GaveUp || !strings.Contains(d.Why, "GIVEN UP") || !strings.Contains(d.Why, "restart") {
		t.Fatalf("%+v", d)
	}
	// Whether or not the mule is heard.
	s.MuleHeard = false
	if d := Decide(set, s); !d.GaveUp {
		t.Fatalf("not heard: %+v", d)
	}
	// 0 never gives up.
	set.MaxHandoverFailures = 0
	s.MuleHeard = true
	if s.Ledger.HandoverGaveUp(set) || Decide(set, s).GaveUp {
		t.Fatal("a limit of 0 must never give up")
	}
}

func TestClearGiveUpOnRestartKeepsTheWait(t *testing.T) {
	set := orderSettings()
	var l Ledger
	if l.ClearGiveUp(set) {
		t.Fatal("nothing to clear")
	}
	for i := 0; i < 3; i++ {
		l.Fail(ModeDeliver, t0, set)
	}
	until, _ := l.BackedOff(ModeDeliver, t0)
	if !l.ClearGiveUp(set) {
		t.Fatal("a suspension must clear")
	}
	if l.Fails(ModeDeliver) != 0 || l.HandoverGaveUp(set) {
		t.Fatal("the count starts again")
	}
	if u, backed := l.BackedOff(ModeDeliver, t0.Add(time.Minute)); !backed || !u.Equal(until) {
		t.Fatalf("the wait the failures earned is kept: %v %v (was %v)", u, backed, until)
	}
	// Below the limit a restart changes nothing (the doubling carries over).
	var m Ledger
	m.Fail(ModeDeliver, t0, set)
	if m.ClearGiveUp(set) || m.Fails(ModeDeliver) != 1 {
		t.Fatal("below the limit nothing is cleared")
	}
}

// Defect 4: the account must be on the surface, on the mule's side of White Wolf Mountain, and at
// 80% hitpoints (the default) before ScarabSwarm is stopped.
func TestSafePointRefusesUndergroundAndFarFromTheMule(t *testing.T) {
	ok := SafeInput{TileKnown: true, HP: 40, MaxHP: 40}
	in := ok
	in.Underground = true
	if safe, why := SafePoint(in, 80); safe || !strings.Contains(why, "underground") {
		t.Errorf("underground: %v %q", safe, why)
	}
	in = ok
	in.FarFromMule = true
	if safe, why := SafePoint(in, 80); safe || !strings.Contains(why, "White Wolf Mountain") {
		t.Errorf("far from the mule: %v %q", safe, why)
	}
}

func TestSafePointHitpointsDefaultIs80(t *testing.T) {
	if orderSettings().SafeHpPercent != 80 {
		t.Fatalf("default %d%%, want 80", orderSettings().SafeHpPercent)
	}
	pct := orderSettings().SafeHpPercent
	in := SafeInput{TileKnown: true, HP: 31, MaxHP: 40} // 77.5%
	if safe, why := SafePoint(in, pct); safe || !strings.Contains(why, "hitpoints") {
		t.Errorf("31/40 at the default floor: %v %q", safe, why)
	}
	in.HP = 32 // exactly 80%
	if safe, _ := SafePoint(in, pct); !safe {
		t.Error("32/40 is 80%")
	}
}
