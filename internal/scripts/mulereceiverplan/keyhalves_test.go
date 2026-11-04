package mulereceiverplan

import "testing"

// pack builds a backpack from (slot, id) pairs.
func halves(pairs ...[2]int) []HalfSlot {
	out := make([]HalfSlot, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, HalfSlot{Slot: p[0], ID: p[1]})
	}
	return out
}

func TestPairHalvesPairsOneOfEachInSlotOrder(t *testing.T) {
	pack := halves([2]int{3, 987}, [2]int{0, 985}, [2]int{1, 985}, [2]int{9, 987})
	got := PairHalves(pack, 985, 987, 0)
	if len(got) != 2 {
		t.Fatalf("got %d pairs, want 2: %+v", len(got), got)
	}
	if got[0].From.Slot != 0 || got[0].To.Slot != 3 {
		t.Fatalf("first pair = %+v, want slots 0 and 3 (lowest first on both sides)", got[0])
	}
	if got[1].From.Slot != 1 || got[1].To.Slot != 9 {
		t.Fatalf("second pair = %+v, want slots 1 and 9", got[1])
	}
	for _, p := range got {
		if p.From.ID == p.To.ID {
			t.Fatalf("pair %+v uses two of the same half; content's join needs one of each", p)
		}
	}
}

func TestPairHalvesStopsAtTheEnginesPerTickLimit(t *testing.T) {
	var pack []HalfSlot
	for i := 0; i < 14; i++ {
		pack = append(pack, HalfSlot{Slot: i, ID: 985}, HalfSlot{Slot: 14 + i, ID: 987})
	}
	if got := PairHalves(pack, 985, 987, 0); len(got) != JoinsPerTick {
		t.Fatalf("got %d pairs with no explicit cap, want JoinsPerTick=%d", len(got), JoinsPerTick)
	}
	if got := PairHalves(pack, 985, 987, 3); len(got) != 3 {
		t.Fatalf("got %d pairs with max 3, want 3", len(got))
	}
	if got := PairHalves(pack, 985, 987, 99); len(got) != 14 {
		t.Fatalf("got %d pairs with max 99, want the 14 the pack can actually make", len(got))
	}
}

func TestPairHalvesIgnoresEverythingElseInThePack(t *testing.T) {
	pack := halves([2]int{0, 995}, [2]int{1, 985}, [2]int{2, 405}, [2]int{3, 987}, [2]int{4, 986})
	got := PairHalves(pack, 985, 987, 0)
	if len(got) != 1 || got[0].From.Slot != 1 || got[0].To.Slot != 3 {
		t.Fatalf("got %+v, want exactly the one unnoted pair (the note in slot 4 is not joinable)", got)
	}
}

func TestPairHalvesWithOnlyOneKindMakesNothing(t *testing.T) {
	pack := halves([2]int{0, 985}, [2]int{1, 985}, [2]int{2, 985})
	if got := PairHalves(pack, 985, 987, 0); len(got) != 0 {
		t.Fatalf("got %+v, want none: three of the same half join into nothing", got)
	}
}

func TestPairHalvesRefusesWhenBothIdsAreTheSame(t *testing.T) {
	// The caller resolved only one unnoted id — a content change, or a bad lookup. Pairing a
	// half with itself would send a packet content answers with "Nothing interesting happens".
	pack := halves([2]int{0, 985}, [2]int{1, 985})
	if got := PairHalves(pack, 985, 985, 0); len(got) != 0 {
		t.Fatalf("got %+v, want none when both ids are the same", got)
	}
}

func TestJoinableHalves(t *testing.T) {
	for _, tc := range []struct{ a, b, want int }{{0, 0, 0}, {5, 3, 3}, {3, 5, 3}, {7, 7, 7}} {
		if got := JoinableHalves(tc.a, tc.b); got != tc.want {
			t.Fatalf("JoinableHalves(%d,%d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestWithdrawPerHalf(t *testing.T) {
	cases := []struct {
		name                     string
		bankA, bankB, free, want int
	}{
		{"a full pack of both", 500, 500, 28, 14},
		{"the owner's 14 of each is the cap", 500, 500, 28, HalvesPerTrip},
		{"limited by the scarcer half", 500, 6, 28, 6},
		{"limited by free slots, both halves must fit", 500, 500, 9, 4},
		{"nothing banked", 0, 500, 28, 0},
		{"no room", 500, 500, 1, 0},
		{"negative room reads as none", 500, 500, -4, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithdrawPerHalf(tc.bankA, tc.bankB, tc.free); got != tc.want {
				t.Fatalf("WithdrawPerHalf(%d,%d,%d) = %d, want %d", tc.bankA, tc.bankB, tc.free, got, tc.want)
			}
		})
	}
}

func TestHalvesPerTripFillsThePackExactly(t *testing.T) {
	if HalvesPerTrip*2 != PackSlots {
		t.Fatalf("2 x HalvesPerTrip = %d, want the pack's %d slots", HalvesPerTrip*2, PackSlots)
	}
}
