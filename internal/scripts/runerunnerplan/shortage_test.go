package runerunnerplan

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func muleHolding(m map[string]int) func(string) (int, bool) {
	return func(item string) (int, bool) { return m[item], true }
}

func shopOf(key string) ShopSite { s, _ := Site(key); return s }

// The mule's bank on 7 October 2026 07:15: coins and junk runes in the hundreds of thousands, 109
// Nature, no Fire, Chaos or Death.
var liveMule = map[string]int{
	"Water rune": 395773, "Body rune": 193811, "Air rune": 83544, "Mind rune": 44074,
	"Earth rune": 39455, "Law rune": 40362, "Cosmic rune": 4832, "Nature rune": 109,
}

func TestCeilingCapIsSixTimesTheBasePrice(t *testing.T) {
	for rune, want := range map[string]int{"Chaos rune": 90, "Death rune": 180, "Nature rune": 120, "Fire rune": 24, "Mind rune": 18} {
		if got := CeilingCap(rune); got != want {
			t.Errorf("CeilingCap(%s) = %d, want %d", rune, got, want)
		}
	}
	if CeilingCap("Bananas") != 0 {
		t.Error("a rune no shop sells has no ceiling")
	}
	// And it is the price the shop's own walk stops rising at: Aubury's Chaos on an empty shelf.
	if p := shopOf("Aubury").Shop.PriceAt("Chaos rune", 1); p != CeilingCap("Chaos rune") {
		t.Errorf("Aubury charges %d for the last Chaos rune; the ceiling is %d", p, CeilingCap("Chaos rune"))
	}
}

func TestPlanShortageMovesTheRunesTheMuleIsOutOfToTheFrontAtTheCeiling(t *testing.T) {
	runes := FullStockRunes(DefaultSites())
	caps, err := ParseCaps(DerivedCaps(runes), runes)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]string{}, runes...)
	p := PlanShortage(ShortageInput{Runes: runes, Caps: caps, Mule: muleHolding(liveMule)})

	if want := []string{"Nature rune", "Chaos rune", "Death rune", "Fire rune"}; !reflect.DeepEqual(p.Runes[:4], want) {
		t.Fatalf("priority starts %v, want %v", p.Runes[:4], want)
	}
	if len(p.Runes) != len(runes) {
		t.Fatalf("%d runes in, %d out", len(runes), len(p.Runes))
	}
	rest := p.Runes[4:]
	for i, r := range []string{"Blood rune", "Mind rune", "Air rune", "Water rune", "Earth rune", "Body rune"} {
		if rest[i] != r {
			t.Fatalf("the rest keeps its order: got %v", rest)
		}
	}
	for rune, want := range map[string]int{"Nature rune": 120, "Chaos rune": 90, "Death rune": 180, "Fire rune": 24} {
		if got := p.Caps.For(rune); got != want {
			t.Errorf("cap %s = %d, want %d", rune, got, want)
		}
	}
	if got := p.Caps.For("Water rune"); got != caps.For("Water rune") {
		t.Errorf("a rune that is not short keeps its cap: %d", got)
	}
	if !reflect.DeepEqual(runes, before) {
		t.Errorf("the input list was changed: %v", runes)
	}
	if caps.For("Chaos rune") != 45 {
		t.Errorf("the input caps were changed: %v", caps)
	}
	line := p.String()
	for _, want := range []string{"Nature rune held=109 cap=120", "Chaos rune held=0 cap=90", "Fire rune held=0 cap=24"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q lacks %q", line, want)
		}
	}
}

func TestPlanShortageLeavesEverythingWhenTheMuleHoldsEnoughOrPublishedNothing(t *testing.T) {
	runes := FullStockRunes(DefaultSites())
	caps, _ := ParseCaps(DerivedCaps(runes), runes)

	stocked := map[string]int{"Nature rune": 2000, "Chaos rune": 3000, "Death rune": 2000, "Fire rune": 5000}
	for name, mule := range map[string]func(string) (int, bool){"stocked": muleHolding(stocked), "unpublished": nil} {
		p := PlanShortage(ShortageInput{Runes: runes, Caps: caps, Mule: mule})
		if !reflect.DeepEqual(p.Runes, runes) || !reflect.DeepEqual(p.Caps, caps) || len(p.Short) != 0 {
			t.Errorf("%s: changed the plan: %v %v", name, p.Runes, p.Short)
		}
		if !strings.Contains(p.String(), "not short") {
			t.Errorf("%s: %q", name, p.String())
		}
	}
	// One scarce rune just under its level: only that one moves.
	stocked["Death rune"] = 1999
	p := PlanShortage(ShortageInput{Runes: runes, Caps: caps, Mule: muleHolding(stocked)})
	if p.Runes[0] != "Death rune" || len(p.Short) != 1 {
		t.Fatalf("only Death is short: %v %v", p.Runes[:3], p.Short)
	}
}

func TestPlanShortageNeverLowersACapAndIgnoresAnUnlistedRune(t *testing.T) {
	runes := []string{"Mind rune", "Chaos rune"} // no Nature, Death or Fire in this runner's list
	caps := Caps{"Mind rune": 9, "Chaos rune": 400}
	p := PlanShortage(ShortageInput{Runes: runes, Caps: caps, Mule: muleHolding(map[string]int{})})
	if !reflect.DeepEqual(p.Runes, []string{"Chaos rune", "Mind rune"}) {
		t.Fatalf("got %v", p.Runes)
	}
	if p.Caps.For("Chaos rune") != 400 {
		t.Errorf("a cap above the ceiling stays: %d", p.Caps.For("Chaos rune"))
	}
	if _, ok := p.Caps["Nature rune"]; ok {
		t.Error("an unlisted rune gained a cap")
	}
}

// Under the shortage caps the plan buys the deep end of the shelf the default cap left alone: at
// the live Aubury Chaos stock of 89 the default cap (45) buys nothing and the ceiling buys it all.
func TestShortageCapsBuyTheDeepEndOfTheShelf(t *testing.T) {
	aubury := shopOf("Aubury").Shop
	if n, _ := BuyableUnderCap(aubury, "Chaos rune", 89, 45); n != 0 {
		t.Fatalf("the default cap buys %d at stock 89", n)
	}
	n, cost := BuyableUnderCap(aubury, "Chaos rune", 89, CeilingCap("Chaos rune"))
	if n != 89 || cost != 89*90 {
		t.Fatalf("the ceiling buys %d for %d", n, cost)
	}
	// 800 of Betty's Death runes (stock 200 of 1000: the price is at the ceiling).
	n, cost = BuyableUnderCap(Betty, "Death rune", 800, CeilingCap("Death rune"))
	if n != 800 || cost > 800*180 {
		t.Fatalf("Betty's Death: %d for %d", n, cost)
	}
}

func TestShortageGivesTheCoinsOfATripToTheScarceRunesFirst(t *testing.T) {
	runes := FullStockRunes(DefaultSites())
	caps, _ := ParseCaps(DerivedCaps(runes), runes)
	seen := map[string]int{"Mind rune": 1000, "Air rune": 1000, "Water rune": 1000, "Earth rune": 1000, "Fire rune": 1000,
		"Chaos rune": 700, "Body rune": 1000, "Death rune": 700}
	// 5,000 coins: in the default order the cheap runes eat them and Chaos gets little or none.
	base := PlanVisit(Betty, seen, 5000, runes, caps, nil)
	p := PlanShortage(ShortageInput{Runes: runes, Caps: caps, Mule: muleHolding(liveMule)})
	short := PlanVisit(Betty, seen, 5000, p.Runes, p.Caps, nil)
	var baseChaos, shortChaos int
	for _, l := range base.Lines {
		if l.Rune == "Chaos rune" {
			baseChaos = l.Count
		}
	}
	for _, l := range short.Lines {
		if l.Rune == "Chaos rune" {
			shortChaos = l.Count
		}
	}
	if shortChaos <= baseChaos || short.Lines[0].Rune != "Chaos rune" {
		t.Fatalf("5,000 coins bought %d Chaos in the default order and %d short-first (%v)", baseChaos, shortChaos, short.Lines)
	}
}

func TestPurseNeeded(t *testing.T) {
	now := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	cases := []struct {
		name          string
		below, amount int
		bank, pack    int
		readAt        time.Time
		want          bool
	}{
		{"never read: walk", 150000, 300000, 0, 0, time.Time{}, true},
		{"fresh and funded: skip", 150000, 300000, 140000, 20000, now.Add(-5 * time.Minute), false},
		{"fresh and broke: walk", 150000, 300000, 1072, 0, now.Add(-5 * time.Minute), true},
		{"stale and funded: walk", 150000, 300000, 400000, 0, now.Add(-time.Hour), true},
		{"top-up off (below 0)", 0, 300000, 0, 0, time.Time{}, false},
		{"top-up off (amount 0)", 150000, 0, 0, 0, time.Time{}, false},
	}
	for _, c := range cases {
		got, why := PurseNeeded(c.below, c.amount, c.bank, c.readAt, c.pack, now)
		if got != c.want {
			t.Errorf("%s: %v (%s)", c.name, got, why)
		}
		if !got && why == "" {
			t.Errorf("%s: no reason", c.name)
		}
	}
}

func TestTopUpAskTimeoutOutlastsTheMulesKitQueue(t *testing.T) {
	// Live 7 October 2026: kit asks were quoted "about 4m45s" at the mule, and an order given up
	// on after 150 seconds was never handed over.
	if TopUpAskTimeout < 5*time.Minute {
		t.Fatalf("TopUpAskTimeout %s is shorter than the mule's queue", TopUpAskTimeout)
	}
}

func TestFleetHopBudgetFollowsTheRingsOwn(t *testing.T) {
	for perHour, want := range map[int]int{1: 4, 6: 4, 8: 4, 12: 6, 30: 15} {
		n, w := FleetHopBudget(perHour)
		if n != want || w != 30*time.Minute {
			t.Errorf("FleetHopBudget(%d) = %d in %s, want %d in 30m", perHour, n, w, want)
		}
	}
}

// The live phase-3 and phase-4 circuits start at Lundail and had no top-up at all.
func TestALundailFirstRotationRoundHasThePurseStationNotTheDraw(t *testing.T) {
	shops := []string{SiteAubury, SiteBetty, SiteLundail}
	for _, c := range []struct {
		phase                 int
		first                 string
		wantPurse, wantDrawAt bool
	}{
		{0, SiteBetty, false, true},   // forward: Betty first
		{3, SiteLundail, true, false}, // reverse: Lundail first
		{4, SiteLundail, true, false},
	} {
		circuit := RotationCircuit(shops, []int{1, 2, 3}, c.phase)
		if circuit[0].Site != c.first {
			t.Fatalf("phase %d starts at %s, not %s", c.phase, circuit[0].Site, c.first)
		}
		ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: shops, Phase: c.phase})
		if ring[0].Kind != KindDeliver {
			t.Fatalf("phase %d: %s", c.phase, PlanString(ring, 0))
		}
		wantKind := KindDraw
		if c.wantPurse {
			wantKind = KindPurse
		}
		if ring[1].Kind != wantKind || ring[1].World != 1 {
			t.Fatalf("phase %d: the station after the delivery is %s, want %s: %s", c.phase, ring[1].Key(), wantKind, PlanString(ring, 0))
		}
		n := 0
		for _, s := range ring {
			if s.Kind == KindDraw || s.Kind == KindPurse {
				n++
			}
		}
		if n != 1 {
			t.Errorf("phase %d: %d top-up stations in %s", c.phase, n, PlanString(ring, 0))
		}
	}
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: shops, Phase: 3})
	if got := ring[1].Key(); got != "purse@1" {
		t.Errorf("key %q", got)
	}
}
