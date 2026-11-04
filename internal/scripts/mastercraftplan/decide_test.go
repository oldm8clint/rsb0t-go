package mastercraftplan

import (
	"strings"
	"testing"
	"time"
)

// Cuttable uncut gems left in the account's own bank by a round that ran out of time are cut by
// the next one, whatever the mule holds now.
func TestDecidePendingCutStartsAGemRound(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	set := DefaultSettings()
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 3}) // far under the threshold
	s.Ledger.PendingCut = true
	d := Decide(set, s)
	if d.Mode != ModeGems || !strings.Contains(d.Why, "still in the bank") {
		t.Fatalf("%+v", d)
	}
	// A pause (a round that found nothing) holds it off like any gem round.
	s.Ledger.GemPauseUntil = now.Add(time.Hour)
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Errorf("paused: %+v", d)
	}
	// Gem rounds switched off: no pending cut either.
	set.GemCutting = false
	s.Ledger.GemPauseUntil = time.Time{}
	if d := Decide(set, s); d.Mode != ModeSwarm {
		t.Errorf("gems off: %+v", d)
	}
	// Products to hand over come before it.
	set = DefaultSettings()
	s.Ledger.PendingDeliver = true
	if d := Decide(set, s); d.Mode != ModeDeliver {
		t.Errorf("deliver first: %+v", d)
	}
}

func TestDecideInconclusiveWithoutData(t *testing.T) {
	now := time.Now()
	set := DefaultSettings()
	s := readySnap(now)
	s.MuleHeard = false
	if d := Decide(set, s); !d.Inconclusive || d.Mode != ModeSwarm {
		t.Errorf("mule not heard: %+v", d)
	}
	s = readySnap(now)
	s.Stock = nil
	if d := Decide(set, s); !d.Inconclusive || d.Mode != ModeSwarm {
		t.Errorf("bank not published: %+v", d)
	}
	// A bank that was read and held too little is an answer, not a lack of one.
	s = readySnap(now)
	s.Stock = stockMap(map[string]int{"Uncut sapphire": 1})
	if d := Decide(set, s); d.Inconclusive {
		t.Errorf("under the threshold is conclusive: %+v", d)
	}
	s = readySnap(now)
	s.MuleSameWorld = false
	if d := Decide(set, s); d.Inconclusive {
		t.Errorf("another world is conclusive: %+v", d)
	}
}
