package questmods

import "testing"

// The pen takes CowPenMax accounts and refuses the next until one leaves.
func TestTheCowPenTakesAtMostCowPenMaxAccounts(t *testing.T) {
	var p penCount
	for i := 0; i < CowPenMax; i++ {
		if !p.enter() {
			t.Fatalf("account %d was refused below the cap", i+1)
		}
	}
	if p.enter() {
		t.Fatal("an account past the cap was let in")
	}
	p.leave()
	if !p.enter() || p.count() != CowPenMax {
		t.Fatalf("after one left: count %d, want %d and a free place", p.count(), CowPenMax)
	}
}
