package combatplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The real melee ladder, and the correction the first version of this file needed: at melee 30
// the five slots resolve to FIVE different shops, because the Varrock sword shop and Horvik's
// armour shop are separate ShopSites even though both are in Varrock. So batching saves nothing
// here and the saving has to come from one journey visiting all five, not from clustering.
//
// This test is therefore pinning a fact about the gear table, so that anyone who later assumes
// batching is doing the work finds out here instead of in a live run.
func TestTheMeleeLadderGivesOneStopPerSlotBecauseNoTwoSlotsShareAKeeper(t *testing.T) {
	l := Levels{Attack: 30, Strength: 30, Defence: 30}
	plan := BuyPlan(l, nil, nil, nil)
	if len(plan) < 2 {
		t.Fatalf("BuyPlan at melee 30 returned %d purchases, want the full melee ladder", len(plan))
	}

	stops := ClusterBySite(plan)
	if len(stops) != len(plan) {
		t.Errorf("the melee ladder clustered %d purchases into %d stops; it has always been one keeper per slot, so a change here means the gear table changed", len(plan), len(stops))
	}
	for _, s := range stops {
		if len(s.Buy) == 0 {
			t.Errorf("stop at %s has no purchases", s.Site.Keeper)
		}
		for _, p := range s.Buy {
			if p.Site.Shop != s.Site.Shop {
				t.Errorf("stop at %s carries a purchase for %s", s.Site.Shop, p.Site.Shop)
			}
		}
	}

	// Every purchase survives clustering: a journey that silently dropped a slot would read as a
	// saving.
	got := 0
	for _, s := range stops {
		got += len(s.Buy)
	}
	if got != len(plan) {
		t.Errorf("clustering %d purchases kept %d of them", len(plan), got)
	}
}

// Where clustering genuinely pays: one keeper who can supply several of the things the plan
// wants is asked for all of them while the account is standing there. A rune list is the real
// case.
func TestClusterBySiteBatchesSeveralItemsFromOneKeeper(t *testing.T) {
	aubury := ShopSite{Shop: ShopVarrockRune, Keeper: "Aubury"}
	plan := []Purchase{
		{Slot: SlotWeapon, Item: Item{Name: "Fire rune"}, Site: aubury},
		{Slot: SlotWeapon, Item: Item{Name: "Death rune"}, Site: aubury},
		{Slot: SlotWeapon, Item: Item{Name: "Air rune"}, Site: aubury},
	}
	stops := ClusterBySite(plan)
	if len(stops) != 1 {
		t.Fatalf("three items from one keeper clustered into %d stops, want 1", len(stops))
	}
	if len(stops[0].Buy) != 3 {
		t.Errorf("the stop carries %d purchases, want 3", len(stops[0].Buy))
	}
}

// Stop order follows the plan, so the weapon's shop is visited before the shield's. Coins run out
// partway more often than not, and what got bought should be the front of the plan.
func TestClusterBySiteKeepsPlanPriorityOrder(t *testing.T) {
	plan := []Purchase{
		{Slot: SlotWeapon, Item: Item{Name: "A"}, Site: ShopSite{Shop: "sword", Keeper: "K1"}},
		{Slot: SlotBody, Item: Item{Name: "B"}, Site: ShopSite{Shop: "armour", Keeper: "K2"}},
		{Slot: SlotLegs, Item: Item{Name: "C"}, Site: ShopSite{Shop: "sword", Keeper: "K1"}},
	}
	stops := ClusterBySite(plan)
	if len(stops) != 2 {
		t.Fatalf("got %d stops, want 2", len(stops))
	}
	if stops[0].Site.Shop != "sword" || stops[1].Site.Shop != "armour" {
		t.Errorf("stop order %s, %s — want the weapon's shop first", stops[0].Site.Shop, stops[1].Site.Shop)
	}
	if len(stops[0].Buy) != 2 || stops[0].Buy[0].Slot != SlotWeapon || stops[0].Buy[1].Slot != SlotLegs {
		t.Errorf("the sword shop's stop is %v, want the weapon then the legs", stops[0].Slots())
	}
}

func TestClusterBySiteOfNothingIsNothing(t *testing.T) {
	if stops := ClusterBySite(nil); len(stops) != 0 {
		t.Errorf("clustering an empty plan gave %d stops", len(stops))
	}
}

// Once a run is away from its rung the question is what is cheapest to reach, not what it wants
// most. Falador, Al Kharid and Barbarian Village in plan order crosses the map three times.
func TestOrderStopsFromPutsTheNearestKeeperFirst(t *testing.T) {
	far := ShopStop{Site: ShopSite{Shop: "falador", Stand: tile.Tile{X: 2979, Z: 3383}}}
	near := ShopStop{Site: ShopSite{Shop: "alkharid", Stand: tile.Tile{X: 3321, Z: 3188}}}
	mid := ShopStop{Site: ShopSite{Shop: "varrock", Stand: tile.Tile{X: 3229, Z: 3438}}}

	// The Al Kharid rung's own stand.
	from := tile.Tile{X: 3288, Z: 3172}
	got := OrderStopsFrom([]ShopStop{far, mid, near}, from)
	want := []string{"alkharid", "varrock", "falador"}
	for i, w := range want {
		if got[i].Site.Shop != w {
			t.Errorf("stop %d is %s, want %s (order was %s)", i, got[i].Site.Shop, w, shopNames(got))
		}
	}
}

// Ordering must not lose or duplicate a stop, and it must not disturb the input.
func TestOrderStopsFromKeepsEveryStopAndDoesNotMutateTheInput(t *testing.T) {
	in := []ShopStop{
		{Site: ShopSite{Shop: "c", Stand: tile.Tile{X: 300, Z: 0}}},
		{Site: ShopSite{Shop: "a", Stand: tile.Tile{X: 100, Z: 0}}},
		{Site: ShopSite{Shop: "b", Stand: tile.Tile{X: 200, Z: 0}}},
	}
	first := in[0].Site.Shop
	got := OrderStopsFrom(in, tile.Tile{})
	if len(got) != len(in) {
		t.Fatalf("ordering %d stops returned %d", len(in), len(got))
	}
	if in[0].Site.Shop != first {
		t.Errorf("ordering mutated its input: stop 0 became %s", in[0].Site.Shop)
	}
	seen := map[string]int{}
	for _, s := range got {
		seen[s.Site.Shop]++
	}
	for _, s := range in {
		if seen[s.Site.Shop] != 1 {
			t.Errorf("stop %s appears %d times after ordering", s.Site.Shop, seen[s.Site.Shop])
		}
	}
}

// Ties keep the plan's own order, so a tie-break cannot silently reorder priority.
func TestOrderStopsFromBreaksTiesByPlanOrder(t *testing.T) {
	in := []ShopStop{
		{Site: ShopSite{Shop: "first", Stand: tile.Tile{X: 10, Z: 0}}},
		{Site: ShopSite{Shop: "second", Stand: tile.Tile{X: 10, Z: 0}}},
	}
	got := OrderStopsFrom(in, tile.Tile{})
	if got[0].Site.Shop != "first" {
		t.Errorf("equidistant stops reordered: got %s first", got[0].Site.Shop)
	}
}

func TestMayStartRunRefusesWhenThereIsNothingToBuy(t *testing.T) {
	v := MayStartRun(TripRequest{Budget: DefaultTripBudget, WantToBuy: false, TripsThisRun: 0})
	if v.Allowed {
		t.Errorf("allowed a journey with an empty plan: %s", v.Why)
	}
}

// The one rule carried over unchanged: an account holding nothing worth swinging arms itself
// before it fights.
func TestMayStartRunGivesTheFirstJourneyOfTheRunFree(t *testing.T) {
	v := MayStartRun(TripRequest{Budget: DefaultTripBudget, WantToBuy: true, TripsThisRun: 0, KillsSinceRun: 0})
	if !v.Allowed {
		t.Errorf("refused the first journey of the run: %s", v.Why)
	}
}

// The measured failure: one kill bought a trip, so the run shopped after every kill. Forty is the
// price of a second journey in the same band.
func TestASingleKillNoLongerPaysForAnotherJourney(t *testing.T) {
	r := TripRequest{Budget: TripBudget{RunsPerBand: 2, StopsPerRun: 4, KillsPerRun: 40},
		Band: "alkharid", WantToBuy: true, TripsThisRun: 1, RunsThisBand: 1, KillsSinceRun: 1}
	if v := MayStartRun(r); v.Allowed {
		t.Errorf("one kill paid for another journey, which is the 19-trip behaviour: %s", v.Why)
	}
	r.KillsSinceRun = 40
	if v := MayStartRun(r); !v.Allowed {
		t.Errorf("40 kills did not pay for a journey: %s", v.Why)
	}
}

// The budget itself: one journey per band by default, whatever the killing looks like.
func TestTheBandBudgetCapsJourneysHoweverWellTheRunIsKilling(t *testing.T) {
	r := TripRequest{Budget: DefaultTripBudget, Band: "alkharid", WantToBuy: true,
		TripsThisRun: 1, RunsThisBand: 1, KillsSinceRun: 10000}
	v := MayStartRun(r)
	if v.Allowed {
		t.Errorf("the band budget did not bite with 10,000 kills banked: %s", v.Why)
	}
	// Crossing into a new band is what resets it, which the caller does by zeroing RunsThisBand.
	r.Band, r.RunsThisBand = "giants", 0
	if v := MayStartRun(r); !v.Allowed {
		t.Errorf("a new band did not restore the allowance: %s", v.Why)
	}
}

// A verdict nobody can read is a verdict nobody can audit, which is the trap
// docs/trustworthy_harnesses.md keeps naming.
func TestEveryTripVerdictCarriesItsReason(t *testing.T) {
	for _, r := range []TripRequest{
		{Budget: DefaultTripBudget, WantToBuy: false},
		{Budget: DefaultTripBudget, WantToBuy: true, TripsThisRun: 0},
		{Budget: DefaultTripBudget, Band: "cows", WantToBuy: true, TripsThisRun: 1, RunsThisBand: 1},
		{Budget: DefaultTripBudget, Band: "cows", WantToBuy: true, TripsThisRun: 1, KillsSinceRun: 3},
	} {
		if v := MayStartRun(r); v.Why == "" {
			t.Errorf("verdict %v for %+v carried no reason", v.Allowed, r)
		}
	}
}

// A journey whose every stop fails still has to end, or the cap is not a cap.
func TestMayMakeStopEndsAJourneyWhoseStopsAllFail(t *testing.T) {
	b := TripBudget{RunsPerBand: 1, StopsPerRun: 3, KillsPerRun: 40}
	for i := 0; i < 3; i++ {
		if !MayMakeStop(b, i) {
			t.Errorf("stop %d refused inside a 3-stop budget", i)
		}
	}
	if MayMakeStop(b, 3) {
		t.Error("a fourth stop was allowed inside a 3-stop budget")
	}
}

// A zero budget must read as "use the measured defaults", not as "never shop" — a settings bag
// that names none of these keys is the common case, and silently disabling gear buying would be
// a different feature.
func TestAZeroBudgetNormalisesToTheDefaults(t *testing.T) {
	got := TripBudget{}.Normalise()
	if got != DefaultTripBudget {
		t.Errorf("zero budget normalised to %+v, want %+v", got, DefaultTripBudget)
	}
	if !MayMakeStop(TripBudget{}, 0) {
		t.Error("a zero budget refused the first stop, which reads as never shopping")
	}
	if v := MayStartRun(TripRequest{Budget: TripBudget{}, WantToBuy: true, TripsThisRun: 0}); !v.Allowed {
		t.Errorf("a zero budget refused the free first journey: %s", v.Why)
	}
}

func shopNames(stops []ShopStop) string {
	out := ""
	for i, s := range stops {
		if i > 0 {
			out += ","
		}
		out += s.Site.Shop
	}
	return out
}
