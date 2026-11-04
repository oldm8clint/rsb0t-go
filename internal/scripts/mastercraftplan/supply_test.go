package mastercraftplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/shopplan"
)

var supplyNow = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

// readySnapshot is a snapshot with the mule here, levels known, and nothing else to do.
func readySnapshot(stock map[string]int) Snapshot {
	return Snapshot{
		Now: supplyNow, MuleHeard: true, MuleSameWorld: true, Stock: stockMap(stock),
		CraftingLevel: 1, FletchingLevel: 60, FletchingKnown: true,
		MagicLevel: 60, SmithingLevel: 60, HerbloreLevel: 60, HerbloreKnown: true,
		Ledger: &Ledger{},
	}
}

func TestBoltsWantedOnlyWhenTipsWaitAndBoltsAreShort(t *testing.T) {
	set := orderSettings()
	if ok, _ := BoltsWanted(set, pearlplan.Held{Tips: 50000}, pearlplan.Held{Bolts: 20000}); ok {
		t.Error("wanted with 20,000 Bolts at the mule")
	}
	if ok, _ := BoltsWanted(set, pearlplan.Held{Tips: 10}, pearlplan.Held{}); ok {
		t.Error("wanted for 10 tips")
	}
	if ok, why := BoltsWanted(set, pearlplan.Held{Tips: 50000, Bolts: 20}, pearlplan.Held{Bolts: 30}); !ok {
		t.Errorf("not wanted: %s", why)
	}
}

func TestPlanShopTripSkipsAShelfStillRestocking(t *testing.T) {
	set := orderSettings()
	want := ShopWants{Bolts: true}
	stops := PlanShopTrip(set, want, nil, supplyNow)
	if len(stops) != 2 || stops[0].Shop.Keeper != "Hickton" || stops[1].Shop.Keeper != "Lowe" {
		t.Fatalf("never-visited shops: %+v", stops)
	}
	if stops[1].Units() != 1500 || stops[0].Units() != 200 {
		t.Errorf("units %d and %d, want both shelves whole (cap 18)", stops[0].Units(), stops[1].Units())
	}
	// Hickton bought out five minutes ago: 50 back, under half of 200. Lowe never seen.
	l := &Ledger{}
	hick, _ := shopplan.SupplyShopByName("Hickton")
	bolt, _ := hick.ItemByID(pearlplan.BoltID)
	l.NoteShelf(hick, bolt, 0, supplyNow.Add(-5*time.Minute))
	stops = PlanShopTrip(set, want, l.Shelves, supplyNow)
	if len(stops) != 1 || stops[0].Shop.Keeper != "Lowe" {
		t.Errorf("after Hickton's buyout: %+v", stops)
	}
	// Twelve minutes on: 120 back, over half.
	stops = PlanShopTrip(set, want, l.Shelves, supplyNow.Add(7*time.Minute))
	if len(stops) != 2 || stops[0].Items[0].Shelf != 120 {
		t.Errorf("twelve minutes on: %+v", stops)
	}
	if c := TripCoins(stops); c%1000 != 0 || c < int(stops[0].Coins()+stops[1].Coins()) {
		t.Errorf("trip coins %d", c)
	}
	// Steel arrowtips are only at Hickton.
	tips := PlanShopTrip(set, ShopWants{ArrowTips: true}, nil, supplyNow)
	if len(tips) != 1 || tips[0].Shop.Keeper != "Hickton" || tips[0].Items[0].Item.ID != ArrowTipsID {
		t.Errorf("arrowtip trip: %+v", tips)
	}
}

func TestDecideOrdersTheLaterRoundsAfterBows(t *testing.T) {
	set := orderSettings()
	// Bows ready AND a shop trip due: bows first.
	s := readySnapshot(map[string]int{"Maple longbow": 500, "Bow string": 500})
	s.OwnPearl = pearlplan.Held{Tips: 5000}
	if d := Decide(set, s); d.Mode != ModeBows {
		t.Fatalf("got %s (%s), want bows before the shop trip", d.Mode, d.Why)
	}
	s = readySnapshot(map[string]int{})
	s.OwnPearl = pearlplan.Held{Tips: 5000}
	d := Decide(set, s)
	if d.Mode != ModeShop || len(d.Shops) == 0 || !d.Wants.Bolts {
		t.Fatalf("got %s (%s), want a shop trip for Bolts", d.Mode, d.Why)
	}
	// Bolts bought into this account's bank: the pearl round resumes, and may chain on.
	s.OwnPearl = pearlplan.Held{Tips: 5000, Bolts: 1700}
	if d := Decide(set, s); d.Mode != ModePearls || !d.Resume || !Chain(ModeShop, d) {
		t.Fatalf("got %s resume %v (%s), want the pearl round to resume", d.Mode, d.Resume, d.Why)
	}
	// Arrow parts, ore for 600 steel bars and natures: arrows first (a user request), then superheat, then herbs.
	s = readySnapshot(map[string]int{"Iron ore": 600, "Coal": 1200, "Nature rune": 600, "Fire rune": 2400,
		"Arrow shaft": 5000, "Feather": 5000, HerbName(herbplan.Herbs[0]): 2000})
	if d := Decide(set, s); d.Mode != ModeArrows {
		t.Fatalf("got %s (%s), want arrows", d.Mode, d.Why)
	}
	set.Arrows = false
	if d := Decide(set, s); d.Mode != ModeSuperheat || d.Superheat.Bar.Name != "Steel bar" || d.Superheat.Casts != 600 {
		t.Fatalf("got %s %+v (%s), want 600 steel bars", d.Mode, d.Superheat, d.Why)
	}
	set.Superheat = false
	if d := Decide(set, s); d.Mode != ModeHerbs {
		t.Fatalf("got %s (%s), want herbs", d.Mode, d.Why)
	}
}

func TestPlanSuperheatHighestBarFirstAndNoGold(t *testing.T) {
	set := orderSettings()
	have := func(m map[string]int) SuperheatStock { return func(k string) int { return m[k] } }
	p := PlanSuperheat(set, 99, have(map[string]int{"Gold ore": 5000, "Mithril ore": 10, "Coal": 100, "Iron ore": 50, "Nature rune": 1000}), true)
	if p.Bar.Name != "Mithril bar" || p.Casts != 10 || p.Fire != 0 {
		t.Fatalf("plan %+v, want 10 mithril first", p)
	}
	// 10 mithril use 40 coal; 60 coal left make 30 steel; the 20 iron ore left make no Iron bars (Steel and up
	// only, 8 Oct 2026). No gold.
	if p.Total != 10+30 {
		t.Errorf("total %d, want 40 (gold and Iron bars left out)", p.Total)
	}
	set.SuperheatGold = true
	if q := PlanSuperheat(set, 99, have(map[string]int{"Gold ore": 5000, "Nature rune": 1000}), true); q.Bar.Name != "Gold bar" {
		t.Errorf("with gold allowed: %+v", q)
	}
	// Smithing 40: no mithril; without a staff the fire runes bound the casts.
	set.SuperheatGold = false
	q := PlanSuperheat(set, 40, have(map[string]int{"Mithril ore": 10, "Coal": 100, "Iron ore": 50, "Nature rune": 1000, "Fire rune": 40}), false)
	if q.Bar.Name != "Steel bar" || q.Casts != 10 || q.Fire != 40 {
		t.Errorf("smithing 40, 40 fire runes: %+v", q)
	}
	lines := PlanSuperheatAsk(q, stockMap(map[string]int{"Iron ore": 50, "Coal": 100, "Nature rune": 1000, "Staff of fire": 1}), nil, false)
	if len(lines) != 4 || lines[3] != (Line{StaffOfFire, 1}) {
		t.Errorf("ask %v, want the ores, natures and the mule's staff", lines)
	}
	if _, ok, _, why := SuperheatReady(set, 40, 99, stockMap(map[string]int{"Iron ore": 9000}), nil, true); ok || why == "" {
		t.Error("ready at Magic 40")
	}
}

func TestSuperheatRefusalReadsContentsLines(t *testing.T) {
	for said, want := range map[string]string{
		"You do not have enough Nature Runes to cast this spell.":    "runes",
		"Your Magic level is not high enough for this spell.":        "level",
		"You need a smithing level of at least 50 to smelt mithril.": "smithing",
		"You need four heaps of coal to smelt mithril.":              "ore",
		"You retrieve a bar of steel.":                               "",
	} {
		if got := SuperheatRefusal(said); got != want {
			t.Errorf("%q: %q, want %q", said, got, want)
		}
	}
}

func TestArrowsReadyAndAsk(t *testing.T) {
	set := orderSettings()
	mule := ArrowHeld{Shafts: 3000, Feathers: 2000, Tips: 500}
	ok, resume, why := ArrowsReady(set, 60, true, mule, ArrowHeld{}, true)
	if !ok || resume {
		t.Fatalf("ready %v resume %v: %s", ok, resume, why)
	}
	lines := PlanArrowAsk(set, mule, ArrowHeld{})
	want := []Line{{ArrowShaft, 2000}, {Feather, 2000}, {ArrowTips, 500}}
	if len(lines) != len(want) {
		t.Fatalf("ask %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("ask %v, want %v", lines, want)
		}
	}
	if ok, _, _ := ArrowsReady(set, 60, true, ArrowHeld{Shafts: 100, Feathers: 100}, ArrowHeld{}, true); ok {
		t.Error("ready for 100 arrows")
	}
	// This account's own parts count with the mule's (a user request: a few headless arrows and one
	// tip are no round), and parts of its own that make the threshold resume without the mule's bank.
	if ok, _, _ := ArrowsReady(set, 60, true, ArrowHeld{Tips: 1}, ArrowHeld{Headless: 40}, true); ok {
		t.Error("40 headless arrows and one tip started a round")
	}
	if ok, _, _ := ArrowsReady(set, 60, true, ArrowHeld{Tips: 1000}, ArrowHeld{Headless: 400, Shafts: 600, Feathers: 600}, true); !ok {
		t.Error("1,000 arrows' worth over the two banks is no round")
	}
	if ok, resume, _ := ArrowsReady(set, 60, true, ArrowHeld{}, ArrowHeld{Headless: 1000, Tips: 1000}, false); !ok || !resume {
		t.Error("1,000 steel arrows' parts in this account's bank do not resume")
	}
	if !ArrowTipsWanted(set, ArrowHeld{Headless: 5000}, ArrowHeld{Tips: 10}) {
		t.Error("tips not wanted for 5000 headless arrows")
	}
}

func TestHerbsReadyOnGrimyGuamsByID(t *testing.T) {
	set := orderSettings()
	guam := HerbName(herbplan.Herbs[0])
	if guam != "Herb id199" {
		t.Fatalf("guam's name %q", guam)
	}
	if ok, _, _ := HerbsReady(set, 60, true, stockMap(map[string]int{guam: 999, "Herb": 5000}), nil); ok {
		t.Error("ready on 999 guams (the merged Herb count must not count)")
	}
	if ok, _, why := HerbsReady(set, 60, true, stockMap(map[string]int{guam: 1000}), nil); !ok {
		t.Errorf("not ready on 1000 guams: %s", why)
	}
	lines := PlanHerbAsk(set, 30, stockMap(map[string]int{guam: 1000, "Herb id207": 50, "Herb id209": 70}))
	// Herblore 30 cleans guam and ranarr (25), not irit (40).
	if len(lines) != 2 || lines[0] != (Line{guam, 1000}) || lines[1] != (Line{"Herb id207", 50}) {
		t.Errorf("ask %v", lines)
	}
}
