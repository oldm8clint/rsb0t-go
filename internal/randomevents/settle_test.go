package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/protocol/clientprot"
)

// A stop that finds an account in combat gets it out first (settle.go). Fixtures: the account at 20 of 53 hitpoints with Bass in the
// pack, a Moss giant (type 1234 here) facing it, as the two accounts that were stopped mid-fight on 14 and 15 Oct.

func shortSettleClocks(t *testing.T) {
	t.Helper()
	maxWait, poll, quiet := settleMaxWait, settlePoll, settleQuiet
	walkDeadline, walkPoll, eatWait := fleeWalkDeadline, fleeWalkPollInterval, fleeEatConfirmWait
	settleMaxWait, settlePoll, settleQuiet = 600*time.Millisecond, 5*time.Millisecond, 40*time.Millisecond
	fleeWalkDeadline, fleeWalkPollInterval, fleeEatConfirmWait = 30*time.Millisecond, 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() {
		settleMaxWait, settlePoll, settleQuiet = maxWait, poll, quiet
		fleeWalkDeadline, fleeWalkPollInterval, fleeEatConfirmWait = walkDeadline, walkPoll, eatWait
	})
}

func settleFood(t *testing.T) {
	t.Helper()
	const bass = 365
	withResolver(t, foodResolver{fakeResolver: fakeResolver{objNames: map[int]string{bass: "Bass"}}, objOps: map[int][5]string{bass: {"Eat"}}})
}

func TestSettleDoesNothingForAnAccountOutOfCombat(t *testing.T) {
	settleFood(t)
	s := combatSession()
	NewGuardian(s, nil)
	sent := 0
	s.onSend = func(byte) { sent++ }
	start := time.Now()
	if !SettleBeforeLogout(s, nil, nil) {
		t.Fatal("an account that was not fighting was reported still fighting")
	}
	if sent != 0 || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("sent %d packets in %s for an account that was not fighting", sent, time.Since(start))
	}
}

func TestSettleEatsAndWaitsOutTheServersCombatHold(t *testing.T) {
	settleFood(t)
	shortSettleClocks(t)
	s := combatSession()
	g := NewGuardian(s, nil)
	t.Cleanup(g.unregister)
	g.CombatExpected = true // ScarabSwarm's, for the whole of its run
	setHP(s.state, 20, 53)
	setFullInventory(s.state, 3214, [2]int32{365, 5})
	underAttackBy(s, 77, 1234)
	ate := 0
	s.onSend = func(op byte) {
		if int(op) == clientprot.OPHELD1 {
			ate++
			// The bite lands and the giant loses the account: out of combat from here.
			clearNPCs(s.state)
			forgetCombat(s)
		}
	}
	var logs []string
	start := time.Now()
	if !SettleBeforeLogout(s, nil, func(l string) { logs = append(logs, l) }) {
		t.Fatalf("not out of combat: %v", logs)
	}
	if ate != 1 {
		t.Fatalf("%d bites, want 1: %v", ate, logs)
	}
	if time.Since(start) < settleQuiet {
		t.Fatalf("returned after %s, before the quiet time %s: the server's 16-tick hold would still have been on", time.Since(start), settleQuiet)
	}
	if g.CombatExpected {
		t.Fatal("combat is still expected: the walking flee stands down at once for such an account")
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "stop: in combat at 20/53") || !strings.Contains(joined, "out of combat for") {
		t.Fatalf("log:\n%s", joined)
	}
}

// A fight that does not end is given up on at the limit, and the logout goes ahead as it always did.
func TestSettleGivesUpOnAFightThatDoesNotEnd(t *testing.T) {
	settleFood(t)
	shortSettleClocks(t)
	s := combatSession()
	g := NewGuardian(s, nil)
	t.Cleanup(g.unregister)
	setHP(s.state, 50, 53) // nothing to eat for
	underAttackBy(s, 77, 1234)
	var logs []string
	start := time.Now()
	if SettleBeforeLogout(s, nil, func(l string) { logs = append(logs, l) }) {
		t.Fatal("reported out of combat with the giant still on the account")
	}
	if took := time.Since(start); took < settleMaxWait || took > 5*time.Second {
		t.Fatalf("took %s, want about %s", took, settleMaxWait)
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "still in combat at 50/53") || !strings.Contains(joined, "logging out anyway") {
		t.Fatalf("log:\n%s", joined)
	}
}
