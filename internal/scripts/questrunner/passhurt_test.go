package questrunner

import "testing"

// A pass ends after a run that lost a fight or left the account under half its hitpoints, so the next
// quest never starts hurt (live 16 Oct 2026: Merlin at 7/53, then the Agility rung at 4/53, then a death).
func TestPassEndsHurt(t *testing.T) {
	for _, c := range []struct {
		name    string
		r       Result
		hp, max int
		ends    bool
	}{
		{"lost a fight", Result{Outcome: OutcomeParked, Reason: "lost a fight: Khazard commander: hitpoints 14/53 are at the eat line (14) with 0 food left"}, 40, 53, true},
		{"lost fights capped", Result{Outcome: OutcomeParked, Reason: "2 lost fight(s) inside 24h0m0s, the quest's cap is 2: skipping the quest for 24h0m0s"}, 30, 53, true},
		{"under half", Result{Outcome: OutcomeParked, Reason: "no progress for 8 ticks on \"x\""}, 7, 53, true},
		{"complete but hurt", Result{Outcome: OutcomeComplete}, 20, 53, true},
		{"healthy park", Result{Outcome: OutcomeParked, Reason: "its walks cannot get there"}, 50, 53, false},
		{"exactly half", Result{Outcome: OutcomeTimeout}, 26, 52, false},
		{"unread hitpoints", Result{Outcome: OutcomeComplete}, 0, 0, false},
	} {
		if got := passEndsHurt(c.r, c.hp, c.max) != ""; got != c.ends {
			t.Errorf("%s: ends %v, want %v", c.name, got, c.ends)
		}
	}
}
