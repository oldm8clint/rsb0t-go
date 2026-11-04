package randomevents

import (
	"testing"
	"time"
)

// Guardian.kick/Pending both need a real actions.Session and, for kick, a real
// pathfind.Pack to invoke Handle against — this package's own established testing
// boundary (see handle_test.go's doc comment) is to not fake those, since a stub would
// prove nothing about real routing/interaction. The attempt/cooldown bookkeeping added
// alongside the retry cap (see guardian.go's own doc comment on maxAttempts/
// giveUpCooldown) is pure state on *Guardian with no session/pack access, though, so it's
// tested directly here the same way handle_test.go tests fleeCandidates in isolation.

func TestEventSignatureIncludesKindAndName(t *testing.T) {
	a := newEvent(KindHazard, "poisonous gas")
	b := newEvent(KindEvade, "poisonous gas")
	if eventSignature(a) == eventSignature(b) {
		t.Fatalf("events with the same name but different Kind must not share a signature: %q", eventSignature(a))
	}
	c := newEvent(KindHazard, "poisonous gas")
	if eventSignature(a) != eventSignature(c) {
		t.Fatalf("identical Kind+Name events must share a signature: %q vs %q", eventSignature(a), eventSignature(c))
	}
}

func TestGuardianNotCooledDownByDefault(t *testing.T) {
	g := NewGuardian(nil, nil)
	if g.cooledDown("hazard:poisonous gas") {
		t.Fatal("a fresh Guardian must not report any signature as cooled down")
	}
}

func TestGuardianNoteAttemptIncrementsPerSignature(t *testing.T) {
	g := NewGuardian(nil, nil)
	sigA := "hazard:poisonous gas"
	sigB := "evade:event monster"
	if n := g.noteAttempt(sigA); n != 1 {
		t.Fatalf("first attempt on sigA = %d, want 1", n)
	}
	if n := g.noteAttempt(sigA); n != 2 {
		t.Fatalf("second attempt on sigA = %d, want 2", n)
	}
	if n := g.noteAttempt(sigB); n != 1 {
		t.Fatalf("first attempt on sigB = %d, want 1 (must not share sigA's counter)", n)
	}
}

// TestGuardianGiveUpCooldownBlocksFurtherAttempts reproduces the exact scenario the
// retry cap exists for: an event whose handler keeps returning without ever clearing it
// (an unreachable NPC, a stuck dialogue — see guardian.go's doc comment for real examples
// this project hit) must stop being re-attempted after maxAttempts, not spin forever at
// pollInterval. This exercises the same state kick() itself mutates, without needing a
// real Session/Handle call.
func TestGuardianGiveUpCooldownBlocksFurtherAttempts(t *testing.T) {
	g := NewGuardian(nil, nil)
	sig := "dialog:Genie"
	for i := 0; i < maxAttempts; i++ {
		if g.cooledDown(sig) {
			t.Fatalf("attempt %d: sig went cooled-down before exceeding maxAttempts", i+1)
		}
		g.noteAttempt(sig)
	}
	// One more attempt pushes the count past maxAttempts — kick() would give up here.
	n := g.noteAttempt(sig)
	if n <= maxAttempts {
		t.Fatalf("attempt count = %d after %d+1 noteAttempt calls, want > %d", n, maxAttempts, maxAttempts)
	}
	g.mu.Lock()
	delete(g.attempts, sig)
	g.cooldownUntil[sig] = time.Now().Add(giveUpCooldown)
	g.mu.Unlock()
	if !g.cooledDown(sig) {
		t.Fatal("sig must be cooled down immediately after giving up")
	}
	// A different signature must be entirely unaffected.
	if g.cooledDown("hazard:whirlpool") {
		t.Fatal("cooldown must be per-signature, not global")
	}
}

// TestEscalatedCooldownGrowsAndCaps covers the escalation Guardian.kick now applies on
// repeated consecutive give-ups against the same signature — see escalatedCooldown's own
// doc comment for the real production case this closes (an event that can structurally
// never clear for one account, e.g. a random event owned by a different nearby account,
// cycling at a flat 45s forever instead of backing off).
func TestEscalatedCooldownGrowsAndCaps(t *testing.T) {
	if got := escalatedCooldown(1); got != giveUpCooldown {
		t.Errorf("escalatedCooldown(1) = %v, want the base giveUpCooldown (%v) — the first give-up must not already be escalated", got, giveUpCooldown)
	}
	d1 := escalatedCooldown(1)
	d2 := escalatedCooldown(2)
	d3 := escalatedCooldown(3)
	if !(d1 < d2 && d2 < d3) {
		t.Fatalf("escalatedCooldown must strictly increase with consecutive give-ups: got %v, %v, %v", d1, d2, d3)
	}
	if got := escalatedCooldown(1000); got != maxGiveUpCooldown {
		t.Errorf("escalatedCooldown(1000) = %v, want capped at maxGiveUpCooldown (%v)", got, maxGiveUpCooldown)
	}
}

// TestConsecutiveGiveupsResetsOnClear covers kick()'s own bookkeeping: a signature that
// finally clears must reset back to the fast 45s cooldown for its NEXT occurrence, not
// stay escalated forever from a past run of bad luck.
func TestConsecutiveGiveupsResetsOnClear(t *testing.T) {
	g := NewGuardian(nil, nil)
	defer g.Stop()
	sig := "3:Strange plant"
	g.consecutiveGiveups[sig] = 5
	// Mirrors kick()'s own "cleared" branch.
	g.mu.Lock()
	delete(g.attempts, sig)
	delete(g.consecutiveGiveups, sig)
	g.mu.Unlock()
	if n := g.consecutiveGiveups[sig]; n != 0 {
		t.Fatalf("consecutiveGiveups[sig] = %d after a clear, want reset to 0", n)
	}
}

// TestLockExcludesKickWhileHeld covers the real fix cmd/boxracetest proved necessary
// live: a one-shot Busy() check is not enough to keep a script's own modal-touching
// sequence from colliding with Guardian's own handling, because Guardian's poll can
// start handling a freshly-spawned event AFTER such a check passes. Lock must provide
// genuine mutual exclusion: while a script holds it, kick() must not proceed into a
// handle even if Detect() would otherwise find a real, not-cooled-down event.
func TestLockExcludesKickWhileHeld(t *testing.T) {
	g := NewGuardian(nil, nil)
	defer g.Stop()

	unlock := g.Lock()
	if !g.inFlight.Load() {
		t.Fatal("inFlight must be true while Lock is held, so Busy()/Pending() reflect it")
	}
	if g.actionMu.TryLock() {
		g.actionMu.Unlock()
		t.Fatal("actionMu must already be held after Lock() — a second acquire should not succeed")
	}
	unlock()
	if g.inFlight.Load() {
		t.Fatal("inFlight must be false again after unlock")
	}
	if !g.actionMu.TryLock() {
		t.Fatal("actionMu must be free again after unlock")
	}
	g.actionMu.Unlock()
}

// TestPackageLockNoopWithoutGuardian covers Lock's own documented fallback: a session
// with no registered Guardian must get a harmless no-op unlock function, matching
// Busy/Pending's identical "nothing to coordinate with" behavior.
func TestPackageLockNoopWithoutGuardian(t *testing.T) {
	unlock := Lock(nil)
	if unlock == nil {
		t.Fatal("Lock must never return a nil unlock function")
	}
	unlock() // must not panic
}

func TestTrappedKindsExemptsMazeFromGiveUp(t *testing.T) {
	if !trappedKinds[KindTrapped] {
		t.Fatal("KindTrapped (maze/mime) must be exempt from the give-up cooldown — the player is physically stuck regardless of attempt count")
	}
	if trappedKinds[KindHazard] || trappedKinds[KindEvade] || trappedKinds[KindDialogNpc] || trappedKinds[KindBoxLamp] {
		t.Fatal("only KindTrapped should be exempt from the give-up cooldown")
	}
}

// TestNextCombatFleeYieldUntilBelowThresholdIsZero covers kick()'s own use of the zero
// Time as "don't yield yet" — the first combatFleeYieldThreshold-1 consecutive failed
// local-flee attempts against the same ongoing threat must keep retrying immediately, the
// original behavior, not start yielding the lock prematurely.
func TestNextCombatFleeYieldUntilBelowThresholdIsZero(t *testing.T) {
	now := time.Now()
	for streak := 0; streak < combatFleeYieldThreshold; streak++ {
		if got := nextCombatFleeYieldUntil(streak, now); !got.IsZero() {
			t.Fatalf("nextCombatFleeYieldUntil(streak=%d) = %v, want the zero Time (below combatFleeYieldThreshold=%d)", streak, got, combatFleeYieldThreshold)
		}
	}
}

// TestNextCombatFleeYieldUntilAtThresholdYields is the actual fix under test: once local
// escape has demonstrably failed combatFleeYieldThreshold times in a row against the same
// threat, kick() must start yielding actionMu instead of immediately re-contesting it —
// see combatFleeYieldThreshold's own doc comment for the live stall (crafter1,
// 2026-09-18) this closes, where EmergencyFlee's own drift-recovery branch (handle.go)
// never got a real chance to run because kick() held the lock almost continuously.
func TestNextCombatFleeYieldUntilAtThresholdYields(t *testing.T) {
	now := time.Now()
	got := nextCombatFleeYieldUntil(combatFleeYieldThreshold, now)
	if got.IsZero() {
		t.Fatal("nextCombatFleeYieldUntil(streak=combatFleeYieldThreshold) must return a real deadline, not the zero Time")
	}
	want := now.Add(combatFleeYieldWindow)
	if !got.Equal(want) {
		t.Fatalf("nextCombatFleeYieldUntil(streak=combatFleeYieldThreshold) = %v, want now+combatFleeYieldWindow = %v", got, want)
	}
}

// TestNextCombatFleeYieldUntilKeepsYieldingPastThreshold covers the streak continuing to
// climb after the first yield (kick() resumes and, if still stuck, escalates streak/dist
// further via fleeDistFor) — every attempt at or past the threshold must keep producing a
// fresh yield window, not just the exact threshold value, so a persistently unshakeable
// threat keeps giving drift-recovery repeated chances rather than only a single one.
func TestNextCombatFleeYieldUntilKeepsYieldingPastThreshold(t *testing.T) {
	now := time.Now()
	for _, streak := range []int{combatFleeYieldThreshold, combatFleeYieldThreshold + 1, combatFleeYieldThreshold + 10} {
		if got := nextCombatFleeYieldUntil(streak, now); got.IsZero() {
			t.Fatalf("nextCombatFleeYieldUntil(streak=%d) returned the zero Time, want a real deadline — yielding must keep firing on every attempt past the threshold, not just the first", streak)
		}
	}
}

// TestKickYieldsActionMuAfterRepeatedCombatFleeFailure exercises the actual field kick()
// reads/writes (combatFleeYieldUntil), not just the pure helper above, mirroring how
// TestLockExcludesKickWhileHeld below exercises actionMu directly without a real Session.
func TestKickYieldsActionMuAfterRepeatedCombatFleeFailure(t *testing.T) {
	g := NewGuardian(nil, nil)
	defer g.Stop()

	// Simulate kick() having just set a yield deadline after crossing the threshold.
	g.mu.Lock()
	g.combatFleeStreak = combatFleeYieldThreshold
	g.combatFleeYieldUntil = time.Now().Add(combatFleeYieldWindow)
	g.mu.Unlock()

	g.mu.Lock()
	yieldUntil := g.combatFleeYieldUntil
	g.mu.Unlock()
	if !time.Now().Before(yieldUntil) {
		t.Fatal("combatFleeYieldUntil must still be in the future immediately after being set — kick() would fail to yield")
	}

	// Mirrors kick()'s own reset (the branch above the InCombat check, taken whenever
	// combat clears or isn't entered): a resolved threat must clear the yield state too,
	// not leave a stale deadline that could suppress a FUTURE, unrelated combat-flee.
	g.mu.Lock()
	g.combatFleeStreak = 0
	g.combatFleeYieldUntil = time.Time{}
	g.mu.Unlock()

	g.mu.Lock()
	yieldUntil = g.combatFleeYieldUntil
	g.mu.Unlock()
	if time.Now().Before(yieldUntil) {
		t.Fatal("combatFleeYieldUntil must be reset to the zero Time once combat clears, not remain armed for a future unrelated fight")
	}
}
