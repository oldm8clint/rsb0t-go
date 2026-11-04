package actions

import (
	"sync"
	"testing"
)

// fromOtherGoroutine runs f on a goroutine of its own and waits for it: the account's script, as opposed to the holder.
func fromOtherGoroutine(f func()) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f()
	}()
	wg.Wait()
}

// sendEveryInteraction sends one of each click a HoldInput hold drops, through s, and returns how many calls were made.
func sendEveryInteraction(s Session) int {
	n := 0
	call := func(err error) {
		n++
		if err != nil {
			panic(err)
		}
	}
	call(Walk(s, holdWalk, true))
	call(WalkOpClick(s, holdWalk, true))
	call(OpLoc(s, 1, 2585, 3418, 2213))
	call(OpLocU(s, 2585, 3418, 2213, 995, 0, 3214))
	call(OpNpc(s, 1, 10015))
	call(OpNpcU(s, 10015, 995, 0, 3214))
	call(OpObj(s, 3, 2586, 3418, 995))
	call(OpObjU(s, 2586, 3418, 995, 995, 0, 3214))
	call(OpHeld(s, 4, 995, 0, 3214)) // a rub
	call(OpHeld(s, 2, 995, 0, 3214)) // an equip
	call(OpHeldU(s, 995, 0, 3214, 996, 1, 3214))
	return n
}

// TestHoldInputDropsEveryOtherGoroutinesInteractionAndSendsTheHolders: the Guardian's handler runs on the poll goroutine, the
// account's script on its own, and the plant incident was the second one's clicks replacing the first one's.
func TestHoldInputDropsEveryOtherGoroutinesInteractionAndSendsTheHolders(t *testing.T) {
	f := newFakeSession()
	hold := HoldInput(f, nil, nil)
	defer hold.Release()

	var calls int
	fromOtherGoroutine(func() { calls = sendEveryInteraction(f) })
	if f.sendCount() != 0 {
		t.Fatalf("%d of %d clicks from another goroutine went out through the held session", f.sendCount(), calls)
	}
	if int(hold.Dropped()) != calls {
		t.Errorf("Dropped = %d, want %d", hold.Dropped(), calls)
	}

	// The holder's own goroutine sends as usual, through the account's own session.
	if n := sendEveryInteraction(f); f.sendCount() != n {
		t.Fatalf("the holder's clicks: %d sent of %d", f.sendCount(), n)
	}
}

// TestHoldInputLetsAnAccountEat: option 1 of a held item is Eat, Drink and Open, and an account whose input is held must still
// be able to eat; everything else about the held item (a rub, an equip, a drop) is dropped.
func TestHoldInputLetsAnAccountEat(t *testing.T) {
	f := newFakeSession()
	hold := HoldInput(f, nil, nil)
	defer hold.Release()
	fromOtherGoroutine(func() {
		if err := Eat(f, 333, 4, 3214); err != nil {
			t.Error(err)
		}
		if err := OpHeld(f, 4, 1707, 2, 3214); err != nil {
			t.Error(err)
		}
	})
	if f.sendCount() != 1 {
		t.Fatalf("%d clicks went out, want only the eat", f.sendCount())
	}
}

// TestHoldInputExemptGoroutineAndEnd: the goroutine the holder exempts (the one holding the account's action lock) sends as usual,
// and the hold ends by itself when active says so.
func TestHoldInputExemptGoroutineAndEnd(t *testing.T) {
	f := newFakeSession()
	exempt, active := false, true
	hold := HoldInput(f, func() bool { return active }, func() bool { return exempt })
	defer hold.Release()

	fromOtherGoroutine(func() { _ = OpNpc(f, 1, 1) })
	if f.sendCount() != 0 {
		t.Fatal("held click went out")
	}
	exempt = true
	fromOtherGoroutine(func() { _ = OpNpc(f, 1, 1) })
	if f.sendCount() != 1 {
		t.Fatalf("the exempt goroutine's click was dropped: %d sent", f.sendCount())
	}
	exempt, active = false, false
	fromOtherGoroutine(func() { _ = OpNpc(f, 1, 1) })
	if f.sendCount() != 2 {
		t.Fatalf("an ended hold still dropped a click: %d sent", f.sendCount())
	}
	active = true // a lapsed hold stays lapsed
	fromOtherGoroutine(func() { _ = OpNpc(f, 1, 1) })
	if f.sendCount() != 3 {
		t.Fatalf("a lapsed hold came back: %d sent", f.sendCount())
	}
}

// TestHoldMovementStillLeavesInteractionsAlone: the maze hold (HoldMovement) has only ever dropped a Walk and an OpLoc; the new
// interaction clicks are the Guardian's hold's business alone.
func TestHoldMovementStillLeavesInteractionsAlone(t *testing.T) {
	f := newFakeSession()
	hold := HoldMovement(f, nil)
	defer hold.Release()
	_ = OpNpc(f, 1, 1)
	_ = OpHeld(f, 4, 995, 0, 3214)
	if f.sendCount() != 2 {
		t.Fatalf("a movement hold dropped an interaction click: %d sent", f.sendCount())
	}
	_ = OpLoc(f, 1, 2585, 3418, 2213)
	if f.sendCount() != 2 {
		t.Fatal("a movement hold let an OpLoc through")
	}
}

func TestMovementHeldByAgreesWithTheHold(t *testing.T) {
	f := newFakeSession()
	hold := HoldInput(f, nil, nil)
	defer hold.Release()
	if MovementHeldBy(f) {
		t.Fatal("the holder's own goroutine reads itself as held")
	}
	fromOtherGoroutine(func() {
		if !MovementHeldBy(f) {
			t.Error("another goroutine does not read the hold")
		}
	})
}
