package fisherplan

import (
	"reflect"
	"testing"
)

// The drop list is exactly the Big net's low-value catch, by content 289's own display names.
func TestBassOysterJunkIsTheBigNetsLowValueCatch(t *testing.T) {
	for _, name := range []string{"Raw mackerel", "Raw cod", "Leather boots", "Leather gloves", "Seaweed", "raw MACKEREL", " Seaweed ",
		"Empty oyster", " empty OYSTER "} { // the shell an opened oyster leaves is thrown away too (owner, 6 Oct)
		if !IsBassOysterJunk(name) {
			t.Errorf("%q is not dropped", name)
		}
	}
	for _, name := range []string{
		"Raw bass", "Oyster", "Casket", // the catch the script is for
		"Oyster pearl", "Oyster pearls", "Pearl bolttips", // what an oyster is worth keeping as
		"Big fishing net", "Coins", "Half of a key", "Crystal key", "Uncut sapphire", "Lobster", "Chisel",
		"Raw shark", "", // an unlisted catch is kept, never guessed at
	} {
		if IsBassOysterJunk(name) {
			t.Errorf("%q would be dropped", name)
		}
	}
}

func TestJunkDropWaitsForANearlyFullPack(t *testing.T) {
	cases := []struct {
		free, junk int
		want       bool
	}{
		{0, 5, true},   // full: fishing has stopped anyway
		{4, 1, true},   // the threshold
		{5, 20, false}, // room left: keep fishing, one re-click per pack of junk
		{0, 0, false},  // full of valuables: bank instead
		{28, 0, false},
	}
	for _, c := range cases {
		if got := JunkDropDue(c.free, c.junk); got != c.want {
			t.Errorf("JunkDropDue(free %d, junk %d) = %v, want %v", c.free, c.junk, got, c.want)
		}
	}
}

func TestJunkSlotsKeepsTheToolTheCatchAndNotes(t *testing.T) {
	names := []string{"Big fishing net", "Raw mackerel", "Raw bass", "Oyster", "Leather boots", "Casket", "Raw cod", "Seaweed", "Leather gloves", "Raw mackerel", "Coins"}
	noted := make([]bool, len(names))
	noted[9] = true // a noted stack left from a hand-off is never dropped
	if got, want := JunkSlots(names, noted, IsBassOysterJunk), []int{1, 4, 6, 7, 8}; !reflect.DeepEqual(got, want) {
		t.Fatalf("JunkSlots = %v, want %v", got, want)
	}
	if got := JunkSlots(nil, nil, IsBassOysterJunk); got != nil {
		t.Fatalf("an empty pack drops %v", got)
	}
}

// Caskets, then key halves, then the mule: opening caskets is what makes the halves, and the
// trade carries what both produced.
func TestHandoffOpensCasketsThenJoinsKeysThenTrades(t *testing.T) {
	want := []HandoffPrep{PrepOpenBanked, PrepJoinKeyHalves, PrepTrade}
	if got := HandoffPrepOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("HandoffPrepOrder = %v, want %v", got, want)
	}
}

func TestBassOysterTargetIsThePinnedBigNet(t *testing.T) {
	target := BassOysterTarget()
	if base, pinned := PinnedTarget(target); base != "Big net" || !pinned {
		t.Fatalf("target %q: base %q pinned %v, want the pinned Big net", target, base, pinned)
	}
	for _, level := range []int{16, 68, 90, 99} {
		if r := CurrentRung(level, target); r.Label != "Big net" {
			t.Errorf("Fishing %d fishes %q, want the Big net", level, r.Label)
		}
	}
	if !SupportsGuildRung(CurrentRung(99, target)) {
		t.Error("the Big net rung is not one the Fishing Guild offers")
	}
}
