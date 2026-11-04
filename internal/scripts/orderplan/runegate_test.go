package orderplan

import (
	"strings"
	"testing"
	"time"
)

func runeStock(m map[string]int) func(string) int {
	return func(item string) int { return m[strings.ToLower(strings.TrimSpace(item))] }
}

func TestDefaultRuneGateIsTheOwnersRule(t *testing.T) {
	g := DefaultRuneGate()
	if g.Floor != 20_000 || g.Max != 1_000 {
		t.Fatalf("the owner's numbers: stack 20k, hand out 1k of each: %+v", g)
	}
	for _, r := range []string{"Chaos rune", "death RUNE", " Blood rune "} {
		if !g.Covers(r) {
			t.Errorf("%q is a pickpocket's rune and gated by default", r)
		}
	}
	for _, r := range []string{"Water rune", "Air rune", "Mind rune", "Nature rune", "Law rune", "Shark", ""} {
		if g.Covers(r) {
			t.Errorf("%q must not be gated: the strike fallback and the alch and teleport kits run on them", r)
		}
	}
	if (RuneGate{}).Active() || (RuneGate{Floor: 20000}).Active() || (RuneGate{Runes: []string{"Chaos rune"}}).Active() {
		t.Error("a gate needs a floor and a rune")
	}
	if !DefaultPolicy([]string{"acct7"}).Runes.Active() {
		t.Error("a default policy carries the gate")
	}
}

func TestTheMuleHandsAGatedRuneOutOnlyFromTwentyThousand(t *testing.T) {
	pol := DefaultPolicy([]string{"acct7"})
	now := time.Now()
	for _, held := range []int{1, 1_000, 19_000, 19_999} {
		d := Decide(now, "acct7", Format("Chaos rune", 1000), pol, NewLedger(DefaultWindow), runeStock(map[string]int{"chaos rune": held}))
		if d.Verdict != Decline || !strings.Contains(d.Reason, "holding until 20000") {
			t.Fatalf("%d chaos banked: want a decline that says what it is waiting for, got %+v", held, d)
		}
	}
	// At the floor the gate OPENS (an inclusive gate: "from 20,000 held").
	d := Decide(now, "acct7", Format("Chaos rune", 1000), pol, NewLedger(DefaultWindow), runeStock(map[string]int{"chaos rune": 20_000}))
	if d.Verdict != Fulfil || d.Qty != 1000 {
		t.Fatalf("20,000 held hands a thousand: %+v", d)
	}
	// A big order is clamped to a thousand of each rune per hand-out, however deep the pool.
	d = Decide(now, "acct7", Format("Death rune", 5000), pol, NewLedger(DefaultWindow), runeStock(map[string]int{"death rune": 250_000}))
	if d.Verdict != Fulfil || d.Qty != 1000 || !d.Clamped {
		t.Fatalf("an order for 5,000 is cut to 1,000: %+v", d)
	}
	// A small one is met in full.
	d = Decide(now, "acct7", Format("Blood rune", 300), pol, NewLedger(DefaultWindow), runeStock(map[string]int{"blood rune": 20_001}))
	if d.Verdict != Fulfil || d.Qty != 300 || d.Clamped {
		t.Fatalf("an order for 300 is 300: %+v", d)
	}
}

func TestOtherRunesAreNotGated(t *testing.T) {
	pol := DefaultPolicy([]string{"acct7"})
	for _, item := range []string{"Water rune", "Nature rune", "Law rune", "Mind rune"} {
		d := Decide(time.Now(), "acct7", Format(item, 400), pol, NewLedger(DefaultWindow), runeStock(map[string]int{strings.ToLower(item): 50}))
		if d.Verdict != Fulfil || d.Qty != 50 {
			t.Errorf("%s with 50 banked is served as ever (the mule's stock is the only limit): %+v", item, d)
		}
	}
}

func TestACatalogueEntryOrAnOffGateOverridesTheGate(t *testing.T) {
	stock := runeStock(map[string]int{"chaos rune": 120})

	// A rune an operator put in the catalogue is on the terms it was given.
	pol := DefaultPolicy([]string{"acct7"})
	pol.Catalog = append(pol.Catalog, Handout{Item: "Chaos rune", Max: 50, WindowMax: 1000})
	if d := Decide(time.Now(), "acct7", Format("Chaos rune", 400), pol, NewLedger(DefaultWindow), stock); d.Verdict != Fulfil || d.Qty != 50 {
		t.Fatalf("a catalogue entry always wins: %+v", d)
	}

	// The zero gate is off: a hand-built policy behaves as before.
	off := DefaultPolicy([]string{"acct7"})
	off.Runes = RuneGate{}
	if d := Decide(time.Now(), "acct7", Format("Chaos rune", 400), off, NewLedger(DefaultWindow), stock); d.Verdict != Fulfil || d.Qty != 120 {
		t.Fatalf("no gate, no wait: %+v", d)
	}
	off.Runes = RuneGate{Floor: 0, Max: 1000, Runes: []string{"Chaos rune"}}
	if d := Decide(time.Now(), "acct7", Format("Chaos rune", 400), off, NewLedger(DefaultWindow), stock); d.Verdict != Fulfil {
		t.Fatalf("a zero floor is off: %+v", d)
	}

	// With the open path off, an uncatalogued rune is refused on sight, gate or no gate.
	closed := DefaultPolicy([]string{"acct7"})
	closed.Open = OpenStock{}
	if d := Decide(time.Now(), "acct7", Format("Chaos rune", 400), closed, NewLedger(DefaultWindow), runeStock(map[string]int{"chaos rune": 90_000})); d.Verdict != Decline {
		t.Fatalf("handoutAnything off refuses it: %+v", d)
	}
}

// anyKeepBack is a reserve on every uncatalogued item; for a gated rune the gate replaces it, so the two do not
// contradict (a reserve of 500 would have handed 19,500 of 20,000).
func TestTheGateReplacesTheOpenPathsReserveForAGatedRune(t *testing.T) {
	pol := DefaultPolicy([]string{"acct7"})
	pol.Open.KeepBack = 500
	d := Decide(time.Now(), "acct7", Format("Chaos rune", 2000), pol, NewLedger(DefaultWindow), runeStock(map[string]int{"chaos rune": 20_000, "shark": 1_000}))
	if d.Verdict != Fulfil || d.Qty != 1000 {
		t.Fatalf("a gated rune ignores the reserve: %+v", d)
	}
	// And the reserve still holds for an ungated item.
	d = Decide(time.Now(), "acct7", Format("Shark", 2000), pol, NewLedger(DefaultWindow), runeStock(map[string]int{"shark": 1_000}))
	if d.Verdict != Fulfil || d.Qty != 500 {
		t.Fatalf("a reserve of 500 of 1,000 sharks leaves 500: %+v", d)
	}
}

func TestAKitLineForAGatedRuneIsGatedToo(t *testing.T) {
	pol := DefaultPolicy([]string{"acct7"})
	stock := runeStock(map[string]int{"chaos rune": 25_000, "death rune": 3_000, "water rune": 4_000})
	kit := DecideKit(time.Now(), "acct7", []KitLine{
		{Item: "Chaos rune", Qty: 1500}, {Item: "Death rune", Qty: 1000}, {Item: "Water rune", Qty: 700},
	}, pol, NewLedger(DefaultWindow), stock, func(string) (KitFacts, bool) { return KitFacts{Stackable: true}, true }, 0)
	got := map[string]KitLineDecision{}
	for _, l := range kit.Lines {
		got[l.Item] = l
	}
	if got["Chaos rune"].Qty != 1000 {
		t.Errorf("the chaos line is cut to a thousand: %+v", got["Chaos rune"])
	}
	if got["Death rune"].Qty != 0 || !strings.Contains(got["Death rune"].Reason, "holding until 20000") {
		t.Errorf("3,000 death is under the gate: %+v", got["Death rune"])
	}
	if got["Water rune"].Qty != 700 {
		t.Errorf("water is not gated: %+v", got["Water rune"])
	}
}

func TestTheAskingSideReadsTheSameGate(t *testing.T) {
	g := DefaultRuneGate()
	pub := map[string]int{"Chaos rune": 19_999, "Death rune": 20_000, "Water rune": 3}
	stock := g.Stock(func(item string) (int, bool) {
		n, ok := pub[item]
		return n, ok
	})
	if n, ok := stock("Chaos rune"); !ok || n != 0 {
		t.Errorf("a stack under the floor reads as none the mule will hand out: %d %v", n, ok)
	}
	if n, ok := stock("Death rune"); !ok || n != 20_000 {
		t.Errorf("a stack at the floor reads as published: %d %v", n, ok)
	}
	if n, ok := stock("Water rune"); !ok || n != 3 {
		t.Errorf("an ungated rune is as published: %d %v", n, ok)
	}
	if _, ok := stock("Blood rune"); ok {
		t.Error("an item the bank does not list stays unknown")
	}
	if g.Stock(nil) != nil {
		t.Error("no reading stays no reading")
	}
	if (RuneGate{}).Stock(func(string) (int, bool) { return 5, true }) == nil {
		t.Error("an inactive gate passes the reading through")
	}
	if g.Cap("Chaos rune", 5000) != 1000 || g.Cap("chaos rune", 300) != 300 || g.Cap("Water rune", 5000) != 5000 {
		t.Error("the cap is a thousand of a gated rune and nothing else")
	}
}
