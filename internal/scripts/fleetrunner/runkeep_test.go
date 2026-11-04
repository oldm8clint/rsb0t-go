package fleetrunner

import "testing"

// A walk with only the run keeper on still gets a poll hook (a user request: the main account walked at 29% run
// energy with Strange fruit in the pack), and a Travel with everything off still gets none.
func TestTheRunKeeperAloneGivesAWalkItsPoll(t *testing.T) {
	if (&Travel{KeepRunning: true}).walkAside(&Deps{}) == nil {
		t.Fatal("KeepRunning alone: no poll hook")
	}
	if (&Travel{}).walkAside(&Deps{}) != nil {
		t.Fatal("everything off: a poll hook")
	}
}
