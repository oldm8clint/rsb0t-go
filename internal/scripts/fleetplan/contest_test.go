package fleetplan

import (
	"testing"
	"time"
)

// The client-side half is AJ's rule, reused: a Cow another player is fighting is not ours; our own
// target's flickering face does not make it someone else's.
func TestTargetContestedIsTheFighterRule(t *testing.T) {
	cases := []struct {
		name                                    string
		ours, inCombat, targetsMe, targetsOther bool
		want                                    bool
	}{
		{"idle cow", false, false, false, false, false},
		{"facing another player", false, false, false, true, true},
		{"in combat, not with me", false, true, false, false, true},
		{"in combat with me", false, true, true, false, false},
		{"my own target between hits", true, true, false, false, false},
	}
	for _, c := range cases {
		if got := TargetContested(c.ours, c.inCombat, c.targetsMe, c.targetsOther); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if !SaidContested("blah Someone else is fighting that. blah") || SaidContested("You attack the cow") {
		t.Fatal("SaidContested")
	}
}

// Two swarm accounts picking the same free Cow in the same tick: the board gives it to one.
func TestTargetBoardSettlesTheSameTickRace(t *testing.T) {
	b := NewTargetBoard()
	t0 := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)
	if !b.Claim(1, 55, "acct1", t0, TargetClaimTTL) {
		t.Fatal("the first claim wins")
	}
	if b.Claim(1, 55, "acct2", t0, TargetClaimTTL) {
		t.Fatal("the second account must not get the same Cow")
	}
	if !b.ClaimedByOther(1, 55, "acct2", t0) || b.ClaimedByOther(1, 55, "acct1", t0) || !b.HeldBy(1, 55, "acct1", t0) {
		t.Fatal("who holds it")
	}
	if b.ClaimedByOther(2, 55, "acct2", t0) {
		t.Fatal("world 2's Cow 55 is another Cow")
	}
	b.Release(1, 55, "acct2") // not its claim: no effect
	if !b.ClaimedByOther(1, 55, "acct2", t0) {
		t.Fatal("only the holder can release")
	}
	b.Release(1, 55, "acct1")
	if !b.Claim(1, 55, "acct2", t0, TargetClaimTTL) {
		t.Fatal("released: free again")
	}
	// A dead or stopped holder cannot keep it.
	if !b.Claim(1, 55, "acct3", t0.Add(TargetClaimTTL+time.Second), TargetClaimTTL) {
		t.Fatal("an expired claim is no claim")
	}
	b.Skip(1, 77, "acct1", t0.Add(TargetSkipFor))
	if !b.Skipped(1, 77, "acct1", t0) || b.Skipped(1, 77, "acct2", t0) || b.Skipped(1, 77, "acct1", t0.Add(TargetSkipFor)) {
		t.Fatal("a skip is per account and bounded")
	}
}
