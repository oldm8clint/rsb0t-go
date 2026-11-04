package consolidateplan

import (
	"testing"
	"time"
)

func TestFreshStartIsOwedOnceByStage3And5(t *testing.T) {
	before := FreshStartAt.Add(-time.Hour)
	after := FreshStartAt.Add(time.Minute)
	if !FreshStartOwed(before, 5) || !FreshStartOwed(before, 3) || !FreshStartOwed(time.Time{}, 5) {
		t.Fatal("a Stage 3 or 5 account with an older (or no) hand-off owes the trip")
	}
	if FreshStartOwed(after, 5) {
		t.Fatal("a hand-off after the fresh start owes nothing more")
	}
	for _, st := range []int{1, 2, 6} {
		if FreshStartOwed(before, st) {
			t.Fatalf("stage %d owes the trip", st)
		}
	}
}

func TestFreshStartHandsOverAnythingOverTheKeep(t *testing.T) {
	s := FreshStart(Defaults())
	p := PlanDeposit(s, Holdings{Coins: 50_010, Runes: map[string]int{"Air rune": 3}}, Keep{Coins: 50_000}, 0)
	if !p.Walk || p.Coins != 10 {
		t.Fatalf("plan %+v: want a walk with the 10 coins over the keep", p)
	}
}
