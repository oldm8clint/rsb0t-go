package mastercraftplan

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/pearlplan"
)

// pearlSnap is a snapshot where the mule holds 100,000 oyster pearls and 600,000 Bolts, 600 uncut
// sapphires and 500 bow-and-string pairs: every round is ready.
func pearlSnap(now time.Time) Snapshot {
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{
		"Uncut sapphire": 600, "Chisel": 1,
		"Maple longbow": 500, "Bow string": 500,
		"Oyster pearl": 99000, "Oyster pearls": 1000, "Bolts": 600000,
	})
	return s
}

// The owner's order: gems first (3 Oct 2026), then stringing before pearls and pearl bolts (5 Oct
// 2026: "the main account should prioritize bow making over oyster chiseling").
func TestDecideGemsThenBowsThenPearls(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	s := pearlSnap(now)
	if d := Decide(set, s); d.Mode != ModeGems {
		t.Fatalf("gems first: %+v", d)
	}
	set.GemCutting = false
	if d := Decide(set, s); d.Mode != ModeBows {
		t.Fatalf("bows before pearls: %+v", d)
	}
	// Pearls an earlier round left in the bank still wait for the bows.
	s.OwnPearl = pearlplan.Held{Pearl: 270}
	if d := Decide(set, s); d.Mode != ModeBows {
		t.Fatalf("bows before a pearl resume: %+v", d)
	}
	s.OwnPearl = pearlplan.Held{}
	set.BowStringing = false
	d := Decide(set, s)
	if d.Mode != ModePearls || d.Pearls != 100000 || d.Resume {
		t.Fatalf("then pearls: %+v", d)
	}
}

func TestDecidePearlThreshold(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	set.GemCutting, set.BowStringing = false, false
	s := pearlSnap(now)
	s.Stock = stockMap(map[string]int{"Oyster pearl": 99999, "Bolts": 600000})
	d := Decide(set, s)
	if d.Mode != ModeSwarm || !strings.Contains(d.Why, "under the threshold 100000") {
		t.Fatalf("99,999 pearls: %+v", d)
	}
	set.PearlThreshold = 50000
	if d := Decide(set, s); d.Mode != ModePearls {
		t.Fatalf("threshold 50,000: %+v", d)
	}
	// Fletching under 41: no pearl round, said why.
	s.FletchingLevel = 40
	if d := Decide(set, s); d.Mode != ModeSwarm || !strings.Contains(d.Why, "41") {
		t.Fatalf("Fletching 40: %+v", d)
	}
	// A free world cannot fletch.
	s.FletchingLevel = 60
	set.MembersWorld = false
	if d := Decide(set, s); d.Mode == ModePearls {
		t.Fatalf("free world: %+v", d)
	}
	// Paused after a fruitless round, backing off after a failed one.
	set.MembersWorld = true
	s.Ledger.PearlPauseUntil = now.Add(time.Hour)
	if d := Decide(set, s); d.Mode != ModeSwarm || !strings.Contains(d.Why, "pearl rounds are paused") {
		t.Fatalf("paused: %+v", d)
	}
	s.Ledger.PearlPauseUntil = time.Time{}
	s.Ledger.Fail(ModePearls, now, set)
	if d := Decide(set, s); d.Mode != ModeSwarm || !strings.Contains(d.Why, "backing off") {
		t.Fatalf("backing off: %+v", d)
	}
}

// Pearls, tips and Bolts in this account's own bank count with the mule's toward the owner's
// thresholds (6 Oct 2026), and a round this account's bank funds alone says so (Resume): it may
// follow another round straight away.
func TestDecidePearlResumeFromOwnBank(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	set := orderSettings()
	set.PearlThreshold, set.BoltThreshold = 20000, 20000
	s := readySnap(now)
	s.Stock = stockMap(map[string]int{"Bolts": 5000, "Uncut sapphire": 600})
	s.OwnPearl = pearlplan.Held{Pearl: 20000}
	d := Decide(set, s)
	if d.Mode != ModeGems {
		t.Fatalf("gems still come first: %+v", d)
	}
	set.GemCutting = false
	d = Decide(set, s)
	if d.Mode != ModePearls || !d.Resume || d.Pearls != 20000 {
		t.Fatalf("own pearls: %+v", d)
	}
	s.OwnPearl = pearlplan.Held{Pearl: 270}
	if d := Decide(set, s); d.Mode == ModePearls {
		t.Fatalf("270 pearls are under 20,000: %+v", d)
	}
	s.OwnPearl = pearlplan.Held{Tips: 600, Bolts: 15000}
	if d := Decide(set, s); d.Mode != ModePearls || d.Resume {
		t.Fatalf("own tips and 20,000 Bolts over the two banks: %+v", d)
	}
	s.OwnPearl = pearlplan.Held{Tips: 600, Bolts: 20000}
	if d := Decide(set, s); d.Mode != ModePearls || !d.Resume {
		t.Fatalf("own tips and 20,000 Bolts of its own: %+v", d)
	}
	s.Stock = stockMap(map[string]int{})
	s.OwnPearl = pearlplan.Held{Tips: 600}
	if d := Decide(set, s); d.Mode == ModePearls {
		t.Fatalf("own tips and no Bolts anywhere: %+v", d)
	}
}

func TestPlanPearlAsk(t *testing.T) {
	stock := stockMap(map[string]int{"Oyster pearl": 100000, "Oyster pearls": 20, "Bolts": 600480, "Chisel": 3})
	a := PlanPearlAsk(stock, pearlplan.Held{}, true)
	want := []Line{{"Oyster pearls", 20}, {"Oyster pearl", 100000}, {"Bolts", 600480}}
	if !reflect.DeepEqual(a.Lines, want) || a.Pearls != 100020 || a.Bolts != 600480 || a.Why != "" {
		t.Fatalf("%+v", a)
	}
	// No chisel held: one comes too.
	a = PlanPearlAsk(stock, pearlplan.Held{}, false)
	if last := a.Lines[len(a.Lines)-1]; last != (Line{"Chisel", 1}) {
		t.Fatalf("%+v", a.Lines)
	}
	// No chisel anywhere: nothing can be cut, so nothing is asked.
	noChisel := stockMap(map[string]int{"Oyster pearl": 100000})
	if a := PlanPearlAsk(noChisel, pearlplan.Held{}, false); len(a.Lines) != 0 || !strings.Contains(a.Why, "chisel") {
		t.Fatalf("%+v", a)
	}
	// Bolts only (the mule's pearls are gone, this account holds tips): no chisel is needed.
	boltsOnly := stockMap(map[string]int{"Bolts": 900})
	if a := PlanPearlAsk(boltsOnly, pearlplan.Held{Tips: 600}, false); !reflect.DeepEqual(a.Lines, []Line{{"Bolts", 900}}) {
		t.Fatalf("%+v", a)
	}
	if a := PlanPearlAsk(nil, pearlplan.Held{}, true); len(a.Lines) != 0 || a.Why == "" {
		t.Fatalf("no published bank: %+v", a)
	}
}

// A round of another kind, or one resuming this account's own bank, follows straight on; one of the
// same kind decided on the mule's (possibly stale) bank waits for the next check.
func TestChain(t *testing.T) {
	for _, c := range []struct {
		last Mode
		next Decision
		want bool
	}{
		{ModeGems, Decision{Mode: ModePearls}, true},
		{ModePearls, Decision{Mode: ModeBows}, true},
		{ModeGems, Decision{Mode: ModeGems}, false},
		{ModeGems, Decision{Mode: ModeGems, Resume: true}, true},
		{ModeBows, Decision{Mode: ModeSwarm}, false},
		{ModeBows, Decision{Mode: ModeDeliver, Resume: true}, true},
	} {
		if got := Chain(c.last, c.next); got != c.want {
			t.Errorf("Chain(%s, %+v) = %v, want %v", c.last, c.next, got, c.want)
		}
	}
}

// Bows and strings in this account's own bank make a bow round on their own (Resume), ahead of a
// pearl round, though the mule holds no strings (live 5 Oct 2026: 32,829 unstrung Yew shortbows and
// 55,418 strings in the main account's bank, the mule's strings all drawn, and a pearl round ran instead).
func TestDecideBowsResumeFromOwnBank(t *testing.T) {
	now := time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC)
	set := orderSettings()
	set.GemCutting = false
	s := readySnap(now)
	s.FletchingLevel, s.FletchingKnown = 96, true
	s.Stock = stockMap(map[string]int{"Yew longbow": 500, "Oyster pearl": 1000, "Bolts": 5000})
	s.OwnPearl = pearlplan.Held{Tips: 3000000}
	s.OwnBows = OwnBows{Unstrung: map[string]int{"Yew longbow": 32829}, Strings: 55418}
	d := Decide(set, s)
	if d.Mode != ModeBows || !d.Resume || d.Pairs != 32829 {
		t.Fatalf("own bows and strings: %+v, want a bow round resumed from the bank", d)
	}
	// Ten bows here, the mule's 500 and this account's strings: 510 pairs over the two banks, a round
	// of both (not a resume) at a threshold of 100, and none at the owner's 20,000.
	s.OwnBows = OwnBows{Unstrung: map[string]int{"Yew longbow": 10}, Strings: 55418}
	if d := Decide(set, s); d.Mode != ModeBows || d.Resume || d.Pairs != 510 {
		t.Fatalf("510 pairs over the two banks: %+v", d)
	}
	set.BowThreshold = 20000
	if d := Decide(set, s); d.Mode == ModeBows {
		t.Fatalf("510 pairs started a bow round at the threshold 20,000: %+v", d)
	}
	set.BowThreshold = 100
	// A bow Fletching cannot string does not count.
	s.FletchingLevel = 60
	s.OwnBows = OwnBows{Unstrung: map[string]int{"Yew shortbow": 32829}, Strings: 55418}
	if d := Decide(set, s); d.Mode == ModeBows {
		t.Fatalf("Fletching 60 counted Yew shortbows: %+v", d)
	}
}
