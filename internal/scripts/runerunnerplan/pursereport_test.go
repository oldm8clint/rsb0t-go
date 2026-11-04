package runerunnerplan

import (
	"strings"
	"testing"
)

// The lines the owner reads to see the whole-stack ask worked: what was held, asked, received and spent.
func TestPurseFundedSaysWhatWasAskedAndReceived(t *testing.T) {
	line := PurseFunded(12000000, 1137480, 10000, 11219284, 11219284)
	for _, want := range []string{"purse FUNDED", "12000000 coins held", "bill of 1137480", "reserve of 10000", "10852520 to spare", "asked=11219284", "received=11219284", "stays in this account's bank"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q lacks %q", line, want)
		}
	}
	// A top-up that was not needed says it asked for nothing.
	if line := PurseFunded(1581543, 1170696, 10000, 0, 0); !strings.Contains(line, "asked=0 received=0") {
		t.Errorf("no-ask line %q", line)
	}
}

func TestRoundPurseLine(t *testing.T) {
	line := RoundPurse{Round: 301, Received: 11219284, Spent: 1230000, Ammo: 61500, Bank: 9500000, Pack: 480000}.String()
	for _, want := range []string{"purse round=301", "received=11219284", "spent=1230000", "runes_and_staffs=1168500", "ammo=61500", "kept=9980000 (bank 9500000 + pack 480000)", "no coin goes back to the mule"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q lacks %q", line, want)
		}
	}
}

// NoteTopUp totals the coins the mule gave, all-time and for the round, and a new round clears the round's.
func TestNoteTopUpCountsTheRound(t *testing.T) {
	st := NewState()
	st.NoteTopUp(1000000)
	st.NoteTopUp(0)
	st.NoteTopUp(-5)
	st.NoteTopUp(130077)
	if st.TopUpReceived != 1130077 || st.RoundTopUp != 1130077 {
		t.Fatalf("received %d round %d", st.TopUpReceived, st.RoundTopUp)
	}
	st.StartRound(DeliveryRing(1), 0, st.RoundStarted)
	if st.RoundTopUp != 0 || st.TopUpReceived != 1130077 {
		t.Errorf("a new round: round %d total %d", st.RoundTopUp, st.TopUpReceived)
	}
}
