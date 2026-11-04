package combatplan

import (
	"strings"
	"testing"
)

func TestNeedsBankNamesTheRuleThatDecided(t *testing.T) {
	cases := []struct {
		name string
		in   BankNeed
		want BankReason
	}{
		{
			name: "an empty pack of food with a bank that might still have some",
			in:   BankNeed{Food: 0, FoodPerTrip: 6, PackUsed: 4, PackSize: 28, HaveRung: true},
			want: BankOutOfFood,
		},
		{
			name: "no food and a bank already known empty is not a trip",
			in:   BankNeed{Food: 0, FoodPerTrip: 6, PackUsed: 4, PackSize: 28, HaveRung: true, FoodEmpty: true},
			want: BankNotNeeded,
		},
		{
			name: "down to the reserve breaks off while there is still something to eat",
			in:   BankNeed{Food: 2, FoodPerTrip: 6, PackUsed: 4, PackSize: 28, HaveRung: true},
			want: BankAtReserve,
		},
		{
			name: "above the reserve with room to spare keeps fighting",
			in:   BankNeed{Food: 3, FoodPerTrip: 6, PackUsed: 4, PackSize: 28, HaveRung: true},
			want: BankNotNeeded,
		},
		{
			name: "a full pack is a trip",
			in:   BankNeed{Food: 3, FoodPerTrip: 6, PackUsed: 28, PackSize: 28, HaveRung: true},
			want: BankPackFull,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			need, why := NeedsBank(c.in)
			if why != c.want {
				t.Fatalf("reason = %q, want %q", why, c.want)
			}
			if need != (c.want != BankNotNeeded) {
				t.Fatalf("need = %v with reason %q", need, why)
			}
		})
	}
}

// The spin, stated as a test. These are the numbers the 2026-09-19 giants run actually logged:
// three Trout held against a wanted load of six, and a pack that every trip left full. Before
// PackStuck existed, NeedsBank answered true here forever and the run made 273 trips against 20
// kills.
func TestAFullPackThatCannotBeEmptiedStopsAskingForTheBank(t *testing.T) {
	spinning := BankNeed{Food: 3, FoodPerTrip: 6, PackUsed: 28, PackSize: 28, HaveRung: true}

	need, why := NeedsBank(spinning)
	if !need || why != BankPackFull {
		t.Fatalf("the first trip should be due on a full pack, got need=%v why=%q", need, why)
	}

	// The trip happens and frees nothing, which is what the log recorded every time.
	trip := BankTrip{SlotsBefore: 28, SlotsAfter: 28, Withdrew: 0}
	if TripMadeProgress(trip) {
		t.Fatal("a trip that freed no slot and withdrew nothing must not count as progress")
	}
	if !PackIsStuck(trip, 28) {
		t.Fatal("a full pack that the deposit could not shrink is stuck")
	}

	spinning.PackStuck = true
	if need, why = NeedsBank(spinning); need {
		t.Fatalf("a stuck pack must stop asking for the bank, got need=%v why=%q", need, why)
	}
}

// The food rules must still win over a stuck pack: running out of food is the one thing a stuck
// pack should never be allowed to suppress, because the bank is the only place food comes from.
func TestAStuckPackStillLetsTheFoodRulesFire(t *testing.T) {
	stuck := BankNeed{Food: 0, FoodPerTrip: 6, PackUsed: 28, PackSize: 28, HaveRung: true, PackStuck: true}
	if need, why := NeedsBank(stuck); !need || why != BankOutOfFood {
		t.Fatalf("out of food must still bank, got need=%v why=%q", need, why)
	}

	stuck.Food = 2
	if need, why := NeedsBank(stuck); !need || why != BankAtReserve {
		t.Fatalf("the reserve must still bank, got need=%v why=%q", need, why)
	}
}

// The guard that does not care which pair of rules deadlocked.
//
// Five zero-progress bank loops, five different pairs, one shape: trips accumulating while the
// melee XP holds still. The measured counts were 29 trips in five minutes, 273 in sixteen, 43 in
// eight and 351 across a run — every one of them far past this limit.
func TestBankLoopEndsARunThatBanksWithoutEarningAnything(t *testing.T) {
	loop := BankLoop{
		TripsSinceXP: BankLoopLimit - 1,
		Limit:        BankLoopLimit,
		Why:          BankAtReserve,
		Food:         4,
		BreakOff:     20,
		PackUsed:     28,
		PackSize:     28,
	}
	if stop, reason := BankLoopExhausted(loop); stop {
		t.Fatalf("one trip below the limit must not stop the run: %q", reason)
	}

	loop.TripsSinceXP = BankLoopLimit
	stop, reason := BankLoopExhausted(loop)
	if !stop {
		t.Fatalf("%d trips in a row with no melee XP has to end the run", loop.TripsSinceXP)
	}
	// The reason is the whole point: a run that stops promptly and says why beats one that spins
	// for 44 minutes and then stops. Every reading that decided it has to be in the sentence.
	for _, want := range []string{"bank trips in a row", string(BankAtReserve), "4 bite", "20", "28/28"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the stop reason does not carry %q: %s", want, reason)
		}
	}
	// And it is far under every loop this script has actually grown, or it would not have caught
	// any of them.
	for _, measured := range []int{29, 43, 273, 351} {
		if BankLoopLimit >= measured {
			t.Errorf("a limit of %d cannot catch a %d-trip loop", BankLoopLimit, measured)
		}
	}
}

// A run that is earning never sees this rule, which is what keeps it from being a second way to
// stop a healthy account.
func TestBankLoopSaysNothingWhileTheRunIsEarning(t *testing.T) {
	for trips := 0; trips < BankLoopLimit; trips++ {
		if stop, reason := BankLoopExhausted(BankLoop{TripsSinceXP: trips, Limit: BankLoopLimit}); stop {
			t.Fatalf("%d trip(s) since the last XP stopped the run: %q", trips, reason)
		}
	}
	// A zero limit is a caller mistake, not a licence to stop on the first trip.
	if stop, _ := BankLoopExhausted(BankLoop{TripsSinceXP: 1, Limit: 0}); stop {
		t.Fatal("an unset limit must fall back to BankLoopLimit, not to zero")
	}
}

func TestTripMadeProgress(t *testing.T) {
	if !TripMadeProgress(BankTrip{SlotsBefore: 28, SlotsAfter: 20, Withdrew: 0}) {
		t.Fatal("freeing eight slots is progress")
	}
	if !TripMadeProgress(BankTrip{SlotsBefore: 10, SlotsAfter: 10, Withdrew: 6}) {
		t.Fatal("withdrawing six is progress")
	}
	if TripMadeProgress(BankTrip{SlotsBefore: 10, SlotsAfter: 12, Withdrew: 0}) {
		t.Fatal("a pack that grew and gained nothing wanted is not progress")
	}
}

// PackIsStuck must not fire on a pack that simply has room: a trip that deposits nothing because
// there was nothing to deposit is perfectly healthy.
func TestPackIsStuckOnlyWhenThePackIsActuallyFull(t *testing.T) {
	if PackIsStuck(BankTrip{SlotsBefore: 4, SlotsAfter: 4, Withdrew: 6}, 28) {
		t.Fatal("a four-slot pack is not stuck")
	}
	if !PackIsStuck(BankTrip{SlotsBefore: 28, SlotsAfter: 28}, 28) {
		t.Fatal("a full pack the deposit did not shrink is stuck")
	}
	if PackIsStuck(BankTrip{SlotsBefore: 28, SlotsAfter: 21}, 28) {
		t.Fatal("a pack the deposit did shrink is not stuck")
	}
}
