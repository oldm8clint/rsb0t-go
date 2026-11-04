package orderplan

import (
	"testing"
	"time"
)

// The lab arms of 2026-09-23 (hofdonor1/hofmule1, hofdonor3/hofmule3), replayed through the rule:
// every refused arm asked for more non-stackable slots than the donor had free, and every
// delivered arm did not. The six-item cap ScarabSwarm uses is wrong in both directions (6 into 5
// free is refused; 13 into 18 free is fine).
func TestFitAskToPack(t *testing.T) {
	for _, c := range []struct {
		name                  string
		qty                   int
		stackable, item, note bool
		free, want            int
	}{
		{"10 Cake into 9 free (arm B, refused live)", 10, false, false, false, 9, 9},
		{"6 Cake into 5 free (arm F: the six-cap gets this wrong)", 6, false, false, false, 5, 5},
		{"9 Cake into 9 free (arm C, delivered live)", 9, false, false, false, 9, 9},
		{"13 Cake into 18 free (arm E, delivered live)", 13, false, false, false, 18, 13},
		{"10 Cake into a full pack", 10, false, false, false, 0, 0},
		{"29 Cake travel as one stack of notes", 29, false, false, false, 1, 29},
		{"29 Cake into a full pack already holding noted cake", 29, false, false, true, 0, 29},
		{"29 Cake into a full pack with no noted cake", 29, false, false, false, 0, 0},
		{"10 Fire rune into a full pack already holding fire runes", 10, true, true, false, 0, 10},
		{"10 Fire rune into a full pack with none held", 10, true, false, false, 0, 0},
		{"60 coins, coins held, pack full", 60, true, true, false, 0, 60},
		{"a stackable needs only one slot", 150000, true, false, false, 1, 150000},
		{"nothing asked", 0, false, false, false, 20, 0},
	} {
		if got := FitAskToPack(c.qty, c.stackable, c.item, c.note, c.free); got != c.want {
			t.Errorf("%s: FitAskToPack(%d, stackable=%v, item=%v, note=%v, free=%d) = %d, want %d",
				c.name, c.qty, c.stackable, c.item, c.note, c.free, got, c.want)
		}
	}
}

func TestTravelsAsNotesMatchesTheMulesRule(t *testing.T) {
	if TravelsAsNotes(NotesAbove, false) || !TravelsAsNotes(NotesAbove+1, false) || TravelsAsNotes(NotesAbove+1, true) {
		t.Error("notes are for more than a full pack of an item that does not stack, and nothing else")
	}
}

// All four strings content prints (trade.rs2, the giver's and the receiver's side of each check),
// split by side, and the ordinary trade lines that must never read as a refusal.
func TestTradeSpaceRefusal(t *testing.T) {
	for _, c := range []struct {
		line        string
		partner, me bool
	}{
		{"Other player doesn't have enough inventory space for this trade.", true, false},
		{"Other player doesn't have enough inventory space for this trade", true, false},
		{"You don't have enough inventory space for this trade", false, true},
		{"You don't have enough inventory space for this trade.", false, true},
		{"  You don't have enough inventory space for this trade  ", false, true},
		{"Other player declined trade.", false, false},
		{"Accepted trade.", false, false},
		{"Sending trade offer...", false, false},
		{"Hofmule1:tradereq:", false, false},
		{"", false, false},
	} {
		p, m := TradeSpaceRefusal(c.line)
		if p != c.partner || m != c.me {
			t.Errorf("TradeSpaceRefusal(%q) = (%v, %v), want (%v, %v)", c.line, p, m, c.partner, c.me)
		}
	}
	if len(DeclineNoRoom) > 80 {
		t.Errorf("DeclineNoRoom is %d bytes; mulebus.ReasonMax truncates at 80", len(DeclineNoRoom))
	}
}

func TestStaleRepeat(t *testing.T) {
	served := time.Date(2026, 9, 22, 15, 43, 43, 0, time.UTC)
	for _, c := range []struct {
		name string
		text string
		now  time.Time
		want bool
	}{
		{"the re-publish polled a second later (acct64, 15:43:44)", "need 15 Cake", served.Add(time.Second), true},
		{"case and spacing do not make it a new order", " NEED 15 cake ", served.Add(5 * time.Second), true},
		{"the donor's next item is a new order", "need 50 Chaos rune", served.Add(2 * time.Second), false},
		{"a different quantity is a new order", "need 1 Cake", served.Add(2 * time.Second), false},
		{"the same ask well after the window is a new order", "need 15 Cake", served.Add(RepeatWindow + time.Second), false},
	} {
		if got := StaleRepeat("need 15 Cake", served, c.text, c.now); got != c.want {
			t.Errorf("%s: StaleRepeat = %v, want %v", c.name, got, c.want)
		}
	}
	if StaleRepeat("need 15 Cake", time.Time{}, "need 15 Cake", served) {
		t.Error("nothing served yet: nothing is stale")
	}
}
