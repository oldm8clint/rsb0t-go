package actions

import (
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

var holdWalk = []pathfind.NavPoint{{X: 2900, Z: 4570}, {X: 2891, Z: 4561}}

func TestHoldMovementDropsTheAccountsOwnClicksAndSendsTheHolders(t *testing.T) {
	f := newFakeSession()
	hold := HoldMovement(f, nil)
	defer hold.Release()

	// The script's clicks, through the account's own session: dropped without an error,
	// the way the server drops a delayed player's click.
	if err := Walk(f, holdWalk, true); err != nil {
		t.Fatalf("held Walk returned %v, want nil", err)
	}
	if err := OpLoc(f, 1, 2888, 4574, 3626); err != nil {
		t.Fatalf("held OpLoc returned %v, want nil", err)
	}
	if f.sendCount() != 0 {
		t.Fatalf("%d packet(s) went out through the held session", f.sendCount())
	}
	if hold.Dropped() != 2 {
		t.Errorf("Dropped = %d, want 2", hold.Dropped())
	}

	// The holder's own clicks go out.
	if err := Walk(hold.Owner, holdWalk, true); err != nil {
		t.Fatal(err)
	}
	if err := OpLoc(hold.Owner, 1, 2898, 4600, 3628); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 2 || f.sent[0][0] != clientprot.MOVE_GAMECLICK {
		t.Fatalf("the holder's clicks: %d sent, first opcode %d", f.sendCount(), f.sent[0][0])
	}
	// Anything else through the account's own session is untouched.
	if err := CloseModal(f); err != nil || f.sendCount() != 3 {
		t.Fatalf("CloseModal through the held session: err %v, %d sent", err, f.sendCount())
	}

	hold.Release()
	if err := Walk(f, holdWalk, true); err != nil || f.sendCount() != 4 {
		t.Fatalf("after Release the account's own Walk: err %v, %d sent, want it sent", err, f.sendCount())
	}
}

func TestHoldMovementEndsWhenNoLongerActive(t *testing.T) {
	f := newFakeSession()
	inMaze := true
	hold := HoldMovement(f, func() bool { return inMaze })
	defer hold.Release()
	_ = Walk(f, holdWalk, false)
	if f.sendCount() != 0 {
		t.Fatal("held while active")
	}
	inMaze = false
	_ = Walk(f, holdWalk, false)
	if f.sendCount() != 1 {
		t.Fatalf("still held once active() is false: %d sent", f.sendCount())
	}
	inMaze = true // a lapsed hold stays lapsed
	_ = Walk(f, holdWalk, false)
	if f.sendCount() != 2 {
		t.Fatalf("a lapsed hold came back: %d sent", f.sendCount())
	}
}

func TestHoldMovementReplacedAndScopedToItsSession(t *testing.T) {
	f, other := newFakeSession(), newFakeSession()
	first := HoldMovement(f, nil)
	second := HoldMovement(first.Owner, nil) // re-taken through the owner: same account
	defer second.Release()
	first.Release() // no longer the session's hold: must not lift the second
	_ = Walk(f, holdWalk, false)
	if f.sendCount() != 0 {
		t.Fatal("releasing a replaced hold lifted the current one")
	}
	if second.Dropped() != 1 || first.Dropped() != 0 {
		t.Errorf("Dropped: first %d second %d, want 0 and 1", first.Dropped(), second.Dropped())
	}
	_ = Walk(other, holdWalk, false)
	if other.sendCount() != 1 {
		t.Fatal("a hold on one account held another")
	}
}
