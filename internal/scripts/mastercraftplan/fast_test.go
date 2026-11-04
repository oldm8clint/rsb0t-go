package mastercraftplan

import (
	"testing"

	"rsb0t-go/internal/scripts/levels"
)

// Every chunk is five events, and the booth op is always the fifth of its chunk: the chunk
// boundary is the tick boundary, so the bank opens in the booth's tick and the deposit and the
// withdrawal are the first events of the next.
func TestFastChunksPutTheBoothLastInItsChunk(t *testing.T) {
	const loads = 4
	if FastEvents(loads) != 4*30-1 || FastChunks(loads) != 24 {
		t.Fatalf("%d events, %d chunks", FastEvents(loads), FastChunks(loads))
	}
	booths, deposits := 0, 0
	for k := 0; k < FastChunks(loads); k++ {
		chunk := FastChunkAt(k, loads, 0)
		if k < FastChunks(loads)-1 && len(chunk) != FastChunk {
			t.Fatalf("chunk %d has %d events", k, len(chunk))
		}
		for i, ev := range chunk {
			switch ev.Kind {
			case FastBooth:
				booths++
				if i != FastChunk-1 {
					t.Fatalf("chunk %d: the booth op is event %d", k, i+1)
				}
			case FastDeposit:
				deposits++
				if i != 0 || chunk[1].Kind != FastWithdraw {
					t.Fatalf("chunk %d: the deposit is event %d", k, i+1)
				}
			}
		}
	}
	if booths != loads-1 || deposits != loads {
		t.Fatalf("%d booth ops, %d deposits", booths, deposits)
	}
	// The last chunk is the last load's last four uses, no booth op.
	last := FastChunkAt(FastChunks(loads)-1, loads, 0)
	if len(last) != 4 || last[3].Kind != FastUse || last[3].Slot != 27 {
		t.Fatalf("%+v", last)
	}
}

// The uses name every slot but the chisel's, ascending: the slots Withdraw-All fills.
func TestFastUsesNameEverySlotButTheChisels(t *testing.T) {
	for _, chisel := range []int{0, 5, 27} {
		var slots []int
		for i := 0; i < FastLoadEvents; i++ {
			if ev := FastEventAt(i, chisel); ev.Kind == FastUse {
				slots = append(slots, ev.Slot)
			}
		}
		if len(slots) != FastLoadUses {
			t.Fatalf("chisel %d: %d uses", chisel, len(slots))
		}
		for i, s := range slots {
			if s == chisel || (i > 0 && s <= slots[i-1]) {
				t.Fatalf("chisel %d: slots %v", chisel, slots)
			}
		}
		if dep := FastEventAt(0, chisel); dep.Slot != slots[0] {
			t.Fatalf("chisel %d: the deposit names slot %d", chisel, dep.Slot)
		}
	}
}

func TestFastUsesIn(t *testing.T) {
	for _, c := range []struct{ chunks, want int }{{0, 0}, {1, 3}, {2, 8}, {5, 23}, {6, 27}, {7, 30}, {12, 54}, {100, 4 * 27}} {
		if got := FastUsesIn(c.chunks, 4); got != c.want {
			t.Errorf("FastUsesIn(%d) = %d, want %d", c.chunks, got, c.want)
		}
	}
}

// A level gained by one of a load's last four cuts (its booth tick) makes that load the run's last.
func TestFastLoadsStopsBeforeALevelInABoothTick(t *testing.T) {
	const ruby = 850
	at := func(cut int) int { return levels.XPAtLevel(91) - ((cut-1)*ruby+400)/10 } // cut reaches 91
	for _, c := range []struct{ cut, want int }{
		{52, 2},  // load 2, use 25: booth tick
		{51, 2},  // use 24: booth tick, the first cut of it
		{50, 10}, // use 23: the tick before the booth's
		{45, 10}, // use 18
		{24, 1},  // load 1's booth tick
		{28, 1},  // load 2's first cut, but the displayed XP's floor may mean cut 27: guarded
		{29, 10}, // load 2's second cut
	} {
		if got := FastLoads(10, at(c.cut), ruby); got != c.want {
			t.Errorf("level at cut %d: %d loads, want %d", c.cut, got, c.want)
		}
	}
	if FastLoads(10, levels.XPAtLevel(99), ruby) != 10 {
		t.Error("at 99 nothing is gained")
	}
	if FastLoads(0, 0, ruby) != 0 || FastCutsSeen(170, ruby) != 2 || FastCutsSeen(0, ruby) != 0 {
		t.Error("edges")
	}
}

// The bow load, in the owner's order (6 Oct 2026): Withdraw-All of the strings, Deposit-All of the
// strung bows, Withdraw-All of the unstrung bows, 14 uses, two fillers and the booth op: 20 events,
// four ticks, the booth op last in its tick with the last two uses before the fillers. No Withdraw-X
// and no count dialog.
func TestBowStream(t *testing.T) {
	s := BowStream
	if !s.Valid() || s.LoadEvents() != 20 || s.Ticks() != 4 || s.BoothUses() != 2 {
		t.Fatalf("%d events, %d ticks, %d uses in the booth tick", s.LoadEvents(), s.Ticks(), s.BoothUses())
	}
	if ChiselStream.BoothUses() != 4 || ChiselStream.Ticks() != 6 {
		t.Fatalf("the chisel stream: %d uses in the booth tick, %d ticks", ChiselStream.BoothUses(), ChiselStream.Ticks())
	}
	want := []FastKind{FastWithdraw, FastDeposit, FastWithdraw, FastUse, FastUse}
	for i, k := range want {
		if ev := s.EventAt(i); ev.Kind != k || (k == FastUse && ev.Use != i-3) || (k != FastUse && ev.Head != i) {
			t.Fatalf("event %d: %+v", i, ev)
		}
	}
	for i := 0; i < s.LoadEvents(); i++ {
		if k := s.EventAt(i).Kind; k == FastWithdrawX || k == FastResume {
			t.Fatalf("event %d is a Withdraw-X or its count: a steady load opens no count dialog", i)
		}
	}
	for k := 0; k < s.Chunks(3); k++ {
		for i, ev := range s.ChunkAt(k, 3) {
			if ev.Kind == FastBooth && i != FastChunk-1 {
				t.Fatalf("chunk %d: the booth op is event %d", k, i+1)
			}
		}
	}
	if ev := s.EventAt(17); ev.Kind != FastPad {
		t.Fatalf("event 17: %+v", ev)
	}
	if ev := s.EventAt(18); ev.Kind != FastPad {
		t.Fatalf("event 18: %+v", ev)
	}
	if ev := s.EventAt(27); ev.Kind != FastUse || ev.Use != 4 || ev.Load != 1 {
		t.Fatalf("event 27: %+v", ev)
	}
	for _, c := range []struct{ chunks, want int }{{1, 2}, {2, 7}, {3, 12}, {4, 14}, {5, 16}, {8, 28}} {
		if got := s.UsesIn(c.chunks, 2); got != c.want {
			t.Errorf("UsesIn(%d) = %d, want %d", c.chunks, got, c.want)
		}
	}
	// A level by a booth tick's use (a load's uses 13 and 14) ends the run with that load.
	const yew = 750
	at := func(use int) int { return levels.XPAtLevel(81) - ((use-1)*yew+100)/10 }
	for _, c := range []struct{ use, want int }{{28, 2}, {27, 2}, {26, 10}, {15, 1}, {14, 1}, {13, 1}, {12, 10}, {16, 10}} {
		if got := s.Loads(10, at(c.use), yew); got != c.want {
			t.Errorf("level at use %d: %d loads, want %d", c.use, got, c.want)
		}
	}
}
