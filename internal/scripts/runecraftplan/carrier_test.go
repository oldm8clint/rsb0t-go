package runecraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The prices are the content's formula: buy 6, 5, 4, 3, 2 at stock 0, 1, 13, 26, 38 and sell 2, 1, 0 from 13.
func TestShelfPrice(t *testing.T) {
	for stock, want := range map[int]int{0: 6, 1: 5, 12: 5, 13: 4, 25: 4, 26: 3, 37: 3, 38: 2, 50: 2, 65: 2, 500: 2} {
		if got := ShelfBuyPrice(stock); got != want {
			t.Errorf("buy at stock %d = %d, want %d", stock, got, want)
		}
	}
	for stock, want := range map[int]int{0: 2, 1: 1, 12: 1, 13: 0, 26: 0, 38: 0, 500: 0} {
		if got := ShelfSellPrice(stock); got != want {
			t.Errorf("sell at stock %d = %d, want %d", stock, got, want)
		}
	}
	// The whole trip. The old rule (sell the 25 needed, buy 25 back) at an empty shelf: 14 gp in, 112 out, 98 net; the floor
	// policy at 38 or more: nothing in, 50 out.
	in := ShelfSellValue(0, 25)
	out, got := ShelfBuyCost(25, 25)
	if in != 14 || out != 112 || got != 25 || out-in != 98 {
		t.Fatalf("old rule at an empty shelf: in %d out %d bought %d net %d, want 14 112 25 98", in, out, got, out-in)
	}
	if cost, _ := ShelfBuyCost(65, 25); cost != 50 {
		t.Fatalf("25 units from a shelf of 65 cost %d, want 50", cost)
	}
	if cost, bought := ShelfBuyCost(10, 25); bought != 10 || cost != 50 {
		t.Fatalf("a shelf of 10 sells 10 units, sold %d for %d", bought, cost)
	}
	if v := ShelfSellValue(40, 25); v != 0 {
		t.Fatalf("selling over stock 13 pays %d, want 0", v)
	}
}

func shelfBase(mod func(s *ShelfState)) ShelfState {
	s := ShelfState{Stock: 0, FreeRow: true, Notes: 3000, Unnoted: 0, Coins: 10000, PackFree: 25, Want: CarrierOffer, Floor: ShelfFloor}
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestShelfFloorStep(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mod       func(s *ShelfState)
		sell, buy int
		shelf     int
		net       int
		ok        bool
		in        string
	}{
		{"a shelf at the floor: sell 25, buy 25, shelf unchanged", func(s *ShelfState) { s.Stock = 40 }, 25, 25, 40, 50, true, ""},
		{"a shelf over the floor", func(s *ShelfState) { s.Stock = 100 }, 25, 25, 100, 50, true, ""},
		{"a shelf of 31 is lifted to the floor", func(s *ShelfState) { s.Stock = 31 }, 34, 25, 40, 50, true, ""},
		{"an empty shelf is built up to the floor", func(s *ShelfState) { s.Stock = 0 }, 65, 25, 40, 36, true, ""},
		{"a shelf of 38 is lifted by 27", func(s *ShelfState) { s.Stock = 38 }, 27, 25, 40, 50, true, ""},
		{"a custom floor", func(s *ShelfState) { s.Stock, s.Floor = 0, 60 }, 85, 25, 60, 0, true, ""},
		{"notes for only part of it: sell what there is, buy what the shelf holds", func(s *ShelfState) { s.Stock, s.Notes = 5, 10 }, 10, 15, 0, 0, true, "holds only 15"},
		{"no notes and a shelf: buy only what is there", func(s *ShelfState) { s.Stock, s.Notes = 12, 0 }, 0, 12, 0, 0, true, "holds only 12"},
		{"no notes and an empty shelf", func(s *ShelfState) { s.Stock, s.Notes = 0, 0 }, 0, 0, 0, 0, false, "empty"},
		{"a full shop with no essence row: buy only", func(s *ShelfState) { s.Stock, s.FreeRow = 0, false }, 0, 0, 0, 0, false, "empty"},
		{"a full shop with an essence row still takes the sale", func(s *ShelfState) { s.Stock, s.FreeRow = 30, false }, 35, 25, 40, 50, true, ""},
		{"the pack holds 10 already: buy 15", func(s *ShelfState) { s.Stock, s.Unnoted, s.PackFree = 40, 10, 15 }, 15, 15, 40, 30, true, ""},
		{"the pack holds the load: nothing", func(s *ShelfState) { s.Unnoted = 25 }, 0, 0, 0, 0, true, "already holds"},
		{"less pack room than the trip wants", func(s *ShelfState) { s.Stock, s.PackFree = 40, 20 }, 20, 20, 40, 40, true, ""},
		{"coins under 200: refuse", func(s *ShelfState) { s.Coins = 199 }, 0, 0, 0, 0, false, "199 coins"},
	} {
		p := PlanShelf(shelfBase(tc.mod))
		if p.OK != tc.ok || p.Sell != tc.sell || p.Buy != tc.buy || (tc.ok && tc.buy > 0 && p.ShelfAfter != tc.shelf) || !strings.Contains(p.Why, tc.in) {
			t.Errorf("%s: %+v, want sell %d buy %d shelf %d ok %v containing %q", tc.name, p, tc.sell, tc.buy, tc.shelf, tc.ok, tc.in)
			continue
		}
		if tc.net != 0 && p.Net() != tc.net {
			t.Errorf("%s: net %d, want %d", tc.name, p.Net(), tc.net)
		}
	}
}

// A shop with no free row and no essence row takes no sale at all.
func TestShelfBuyOnlyWhenTheShopIsFull(t *testing.T) {
	p := PlanShelf(shelfBase(func(s *ShelfState) { s.Stock, s.FreeRow = 0, false }))
	if !p.BuyOnly || p.Sell != 0 {
		t.Fatalf("%+v", p)
	}
	// A shelf that holds some essence in a full shop: the row exists, so the sale is possible.
	if q := PlanShelf(shelfBase(func(s *ShelfState) { s.Stock, s.FreeRow = 3, false })); q.BuyOnly || q.Sell == 0 {
		t.Fatalf("%+v", q)
	}
}

// The fare home is never spent on essence.
func TestShelfLeavesTheFare(t *testing.T) {
	p := PlanShelf(shelfBase(func(s *ShelfState) { s.Stock, s.Coins, s.Notes = 40, 250, 0 }))
	if p.Buy == 0 || 250-p.Cost < 30 {
		t.Fatalf("%+v", p)
	}
	// 220 gp buys 25 units at 2 (50) with 170 left; fine. A shelf of 3 sells its units at stock 3, 2 and 1: 5 each.
	q := PlanShelf(shelfBase(func(s *ShelfState) { s.Stock, s.Coins, s.Notes = 3, 220, 0 }))
	if q.Cost != 15 || q.Buy != 3 {
		t.Fatalf("%+v", q)
	}
}

func carrierSnap(mod func(s *CarrierSnapshot)) CarrierSnapshot {
	s := CarrierSnapshot{Place: PlaceStore, Notes: 3000, Coins: 10000, Gate: GateInfo{Heard: true, State: "open", Surplus: 0}}
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestDecideCarrierStageTable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mod   func(s *CarrierSnapshot)
		stage CarrierStage
		in    string
	}{
		{"dead", func(s *CarrierSnapshot) { s.Dead = true }, CarrierRecover, "died"},
		{"mainland, no kit: ask the mule", func(s *CarrierSnapshot) { s.Place, s.Notes, s.Coins = PlaceMainland, 0, 0 }, CarrierAskKit, "asking the mule"},
		{"mainland, no kit, already asked: done", func(s *CarrierSnapshot) {
			s.Place, s.Notes, s.Coins, s.KitAsked = PlaceMainland, 0, 0, true
		}, CarrierDone, "kit is spent"},
		{"mainland, the kit asked and spent, leftovers at the post: deliver", func(s *CarrierSnapshot) {
			s.Place, s.Notes, s.Natures, s.KitAsked, s.AtPost = PlaceMainland, 10, 2300, true, true
		}, CarrierDeliver, "leftovers"},
		{"mainland, leftovers away from the post: walk home", func(s *CarrierSnapshot) {
			s.Place, s.Notes, s.Natures, s.KitAsked = PlaceMainland, 10, 2300, true
		}, CarrierGoHome, "mule's post"},
		{"mainland with the kit and an open gate: cross", func(s *CarrierSnapshot) { s.Place = PlaceMainland }, CarrierCross, "3000 notes"},
		// Loose unnoted essence (a load a shift ended with) is a leftover, not a kit: it is not crossed with, it is handed over.
		{"mainland with a loose load and no notes: ask the mule for a kit", func(s *CarrierSnapshot) {
			s.Place, s.Notes, s.Unnoted = PlaceMainland, 0, 25
		}, CarrierAskKit, "asking the mule"},
		{"mainland with a loose load, the kit asked and nothing came: deliver, not cross", func(s *CarrierSnapshot) {
			s.Place, s.Notes, s.Unnoted, s.KitAsked, s.AtPost = PlaceMainland, 0, 25, true, true
		}, CarrierDeliver, "kit is spent"},
		{"mainland with the kit and the gate closed: the shift is not started; deliver the kit back", func(s *CarrierSnapshot) {
			s.Place, s.AtPost, s.Gate = PlaceMainland, true, GateInfo{Heard: true, State: "closed"}
		}, CarrierDeliver, "closed"},
		{"mainland, the shift is over", func(s *CarrierSnapshot) { s.Place, s.Over, s.Notes = PlaceMainland, true, 0 }, CarrierDone, "over"},
		{"at the store short of essence: shelf", nil, CarrierShelf, "3000 notes"},
		{"on the island with notes and no load: walk to the store", func(s *CarrierSnapshot) { s.Place = PlaceIsland }, CarrierWalkToStore, ""},
		{"at the gate with notes and no load: walk to the store", func(s *CarrierSnapshot) { s.Place = PlaceGate }, CarrierWalkToStore, ""},
		{"a full load at the store: walk to the gate", func(s *CarrierSnapshot) { s.Unnoted = 25 }, CarrierWalkToGate, "25 essence"},
		{"a short load of 20 still goes", func(s *CarrierSnapshot) { s.Unnoted = 20 }, CarrierWalkToGate, ""},
		{"a load of 19 does not: back to the shelf", func(s *CarrierSnapshot) { s.Unnoted = 19 }, CarrierShelf, ""},
		{"a full load at the gate: trade", func(s *CarrierSnapshot) { s.Place, s.Unnoted = PlaceGate, 25 }, CarrierTrade, "at the gate"},
		{"a full load at the gate, the crafter away: wait", func(s *CarrierSnapshot) {
			s.Place, s.Unnoted, s.Gate = PlaceGate, 25, GateInfo{Heard: true, State: "away"}
		}, CarrierWait, "away"},
		{"a full load, waited too long: give up", func(s *CarrierSnapshot) { s.Place, s.Unnoted, s.GateWaited = PlaceGate, 25, GateWaitMax }, CarrierGoHome, "waited"},
		{"hitpoints low on the island: rest", func(s *CarrierSnapshot) { s.Place, s.Unnoted, s.Resting = PlaceIsland, 25, true }, CarrierRest, "low"},
		{"hitpoints low on the mainland: not a rest", func(s *CarrierSnapshot) { s.Place, s.Resting = PlaceMainland, true }, CarrierCross, ""},
		{"the kit spent, no courier: go home", func(s *CarrierSnapshot) { s.Place, s.Notes = PlaceGate, 10 }, CarrierGoHome, "kit is spent"},
		{"the kit spent, the crafter holds Natures: courier", func(s *CarrierSnapshot) {
			s.Place, s.Notes, s.Gate = PlaceGate, 10, GateInfo{Heard: true, State: "open", Surplus: 2300}
		}, CarrierCourier, "2300 Natures"},
		{"the kit spent, a surplus under the courier minimum: go home", func(s *CarrierSnapshot) {
			s.Place, s.Notes, s.Gate = PlaceGate, 10, GateInfo{Heard: true, State: "open", Surplus: CourierMin - 1}
		}, CarrierGoHome, ""},
		{"the gate closed while on the island: go home", func(s *CarrierSnapshot) {
			s.Place, s.Unnoted, s.Gate = PlaceStore, 25, GateInfo{Heard: true, State: "closed"}
		}, CarrierGoHome, "closed"},
		{"the crafter not heard: go home", func(s *CarrierSnapshot) { s.Place, s.Gate = PlaceStore, GateInfo{} }, CarrierGoHome, "not heard"},
		{"the shift is over on the island", func(s *CarrierSnapshot) { s.Place, s.Over = PlaceIsland, true }, CarrierGoHome, "over"},
	} {
		got := DecideCarrierStage(carrierSnap(tc.mod))
		if got.Stage != tc.stage || !strings.Contains(strings.ToLower(got.Why), strings.ToLower(tc.in)) {
			t.Errorf("%s: %s (%q), want %s containing %q", tc.name, got.Stage, got.Why, tc.stage, tc.in)
		}
	}
}

func TestPlaceOf(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   tile.Tile
		want Place
	}{
		{"Catherby", tile.Tile{X: 2809, Z: 3441}, PlaceMainland},
		{"Ardougne's pier", ArdougnePier, PlaceMainland},
		{"Brimhaven's landing", Landing, PlaceIsland},
		{"Jiminua's store", JiminuaAt, PlaceStore},
		{"beside the store", tile.Tile{X: JiminuaAt.X + StoreNear, Z: JiminuaAt.Z}, PlaceStore},
		{"the jungle road", tile.Tile{X: 2800, Z: 3080}, PlaceIsland},
		{"the gate", Nature.Exit, PlaceGate},
		{"the ruins' doorstep", tile.Tile{X: 2867, Z: 3021}, PlaceGate},
		{"upstairs in Brimhaven", tile.Tile{X: 2772, Z: 3234, Level: 1}, PlaceMainland},
	} {
		if got := PlaceOf(tc.at); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

func shiftBase(mod func(v *ShiftView)) ShiftView {
	v := ShiftView{
		Now: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), CrafterSet: true, Members: true,
		Gate: GateInfo{Heard: true, State: "open"}, MaxHP: 51, MinHP: 25,
		MuleHeard: true, MuleSameWorld: true, MuleKnown: true, MuleEssence: 20000, KitWant: 3000,
	}
	if mod != nil {
		mod(&v)
	}
	return v
}

func TestDecideCarrierShift(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mod   func(v *ShiftView)
		start bool
		in    string
	}{
		{"everything in place", nil, true, "mule holds 20000"},
		{"the gate opening starts a shift", func(v *ShiftView) { v.Gate.State = "opening" }, true, ""},
		{"no crafter set", func(v *ShiftView) { v.CrafterSet = false }, false, "runecraftCrafter"},
		{"a free world", func(v *ShiftView) { v.Members = false }, false, "members"},
		{"backing off", func(v *ShiftView) { v.BackedOffUntil = v.Now.Add(time.Hour) }, false, "backing off"},
		{"the gate is not heard", func(v *ShiftView) { v.Gate = GateInfo{} }, false, "not heard"},
		{"the crafter is away", func(v *ShiftView) { v.Gate.State = "away" }, false, "away"},
		{"the gate is closed", func(v *ShiftView) { v.Gate.State = "closed" }, false, "closed"},
		{"the gate is on another world", func(v *ShiftView) { v.GateWorld, v.MyWorld = 1, 2 }, false, "world 1"},
		{"the gate's world is not carried", func(v *ShiftView) { v.GateWorld, v.MyWorld = 0, 2 }, true, ""},
		{"too few hitpoints", func(v *ShiftView) { v.MaxHP = 24 }, false, "carrierMinHP"},
		{"just enough hitpoints", func(v *ShiftView) { v.MaxHP = 25 }, true, ""},
		{"the gate is saturated", func(v *ShiftView) { v.Carriers = MaxCarriers }, false, "saturated"},
		{"one under the cap", func(v *ShiftView) { v.Carriers = MaxCarriers - 1 }, true, ""},
		{"the mule is not heard", func(v *ShiftView) { v.MuleHeard = false }, false, "not heard"},
		{"the mule is on another world", func(v *ShiftView) { v.MuleSameWorld = false }, false, "another world"},
		{"the mule has not published", func(v *ShiftView) { v.MuleKnown = false }, false, "not published"},
		{"the mule has no essence", func(v *ShiftView) { v.MuleEssence = 0 }, false, "under the 100"},
		{"the mule has a few trips of it: not worth the crossing", func(v *ShiftView) { v.MuleEssence = 130 }, false, "worth the crossing"},
		{"one load under the shift minimum", func(v *ShiftView) { v.MuleEssence = ShiftMinEssence - 1 }, false, "a shift is worth"},
		{"the shift minimum", func(v *ShiftView) { v.MuleEssence = ShiftMinEssence }, true, ""},
		{"a small kit setting is its own minimum", func(v *ShiftView) { v.KitWant, v.MuleEssence = 300, 310 }, true, ""},
		{"a small kit setting still wants its kit", func(v *ShiftView) { v.KitWant, v.MuleEssence = 300, 250 }, false, "worth"},
	} {
		start, why := DecideCarrierShift(shiftBase(tc.mod))
		if start != tc.start || !strings.Contains(why, tc.in) {
			t.Errorf("%s: %v (%q), want %v containing %q", tc.name, start, why, tc.start, tc.in)
		}
	}
}

func TestKitEssence(t *testing.T) {
	for _, tc := range []struct{ want, mule, got int }{
		{3000, 20000, 3000},
		{3000, 2999, 2975},
		{3000, 130, 125},
		{3000, 99, 0},
		{3000, 0, 0},
		{50, 20000, 0},
		{100, 20000, 100},
	} {
		if got := KitEssence(tc.want, tc.mule); got != tc.got {
			t.Errorf("KitEssence(%d, %d) = %d, want %d", tc.want, tc.mule, got, tc.got)
		}
	}
	if ShiftTrips(3000) != 120 || ShiftTrips(24) != 0 {
		t.Fatal("a kit of 3,000 notes is 120 trips")
	}
}

func TestHPLowHysteresis(t *testing.T) {
	if HPLow(50, 100, false) || !HPLow(49, 100, false) {
		t.Fatal("rest begins under 50%")
	}
	if !HPLow(84, 100, true) || HPLow(85, 100, true) {
		t.Fatal("rest ends at 85%")
	}
	if HPLow(0, 100, false) || HPLow(10, 0, false) {
		t.Fatal("an unread or dead account is not resting")
	}
}

// A floor of 0 (the default since 16 Oct 2026: the live shop's surplus is gone by the next visit) sells only the trip's
// 25 notes into an empty shelf and buys them back, leaving nothing behind; it is not read as "unset, use 40".
func TestAZeroFloorSellsOnlyTheTripsNotes(t *testing.T) {
	// ... unless the shelf already holds the load: then it is bought as it stands, with nothing sold into it (the sale is the window
	// another shopper takes the stack in).
	if q := PlanShelf(shelfBase(func(s *ShelfState) { s.Floor, s.Stock = 0, 25 })); !q.OK || q.Sell != 0 || q.Buy != CarrierOffer || q.ShelfAfter != 0 || q.Income != 0 {
		t.Fatalf("a shelf of 25: %+v", q)
	}
	if q := PlanShelf(shelfBase(func(s *ShelfState) { s.Floor, s.Stock = 0, 24 })); q.Sell != CarrierOffer || q.Buy != CarrierOffer {
		t.Fatalf("a shelf of 24 is one short: %+v", q)
	}
	if q := PlanShelf(shelfBase(func(s *ShelfState) { s.Floor, s.Stock = ShelfFloor, 30 })); q.Sell != CarrierOffer+10 {
		t.Fatalf("a floor above the shelf still sells to lift it: %+v", q)
	}
	p := PlanShelf(shelfBase(func(s *ShelfState) { s.Floor = 0 }))
	if !p.OK || p.Sell != CarrierOffer || p.Buy != CarrierOffer || p.ShelfAfter != 0 {
		t.Fatalf("plan %+v, want sell %d, buy %d, shelf 0", p, CarrierOffer, CarrierOffer)
	}
	if DefaultShelfFloor != 0 {
		t.Errorf("DefaultShelfFloor %d, want 0", DefaultShelfFloor)
	}
}
