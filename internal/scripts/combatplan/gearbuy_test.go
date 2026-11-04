package combatplan

import "testing"

func TestShopSiteForEveryStockedItem(t *testing.T) {
	// Every item gear.go says a shop stocks must have a keeper and a tile, or the buying loop
	// would plan a trip it cannot make.
	for _, it := range Items() {
		if it.Shop == "" {
			continue
		}
		site, ok := ShopSiteFor(it)
		if !ok {
			t.Fatalf("%s is stocked by %q but that shop has no keeper", it.Name, it.Shop)
		}
		if site.Keeper == "" {
			t.Fatalf("%s: shop %q has no keeper name", it.Name, it.Shop)
		}
		if site.Stand.X == 0 && site.Stand.Z == 0 {
			t.Fatalf("%s: shop %q has no stand tile", it.Name, it.Shop)
		}
	}
}

func TestChampionsGuildShopsAreUnreachable(t *testing.T) {
	// Both guild shops sit behind 32 quest points, which a combat-only run never earns.
	for _, shop := range []string{ShopChampionsGuild, ShopRuniteGuild} {
		site, ok := shopSites[shop]
		if !ok {
			t.Fatalf("%q missing from the shop table", shop)
		}
		if site.Reachable() {
			t.Fatalf("%q must not be reachable: it is behind the guild door", shop)
		}
	}
	if !shopSites[ShopVarrockSword].Reachable() {
		t.Fatal("the Varrock sword shop has no gate and must be reachable")
	}
}

func TestWornTier(t *testing.T) {
	worn := []string{"Bronze scimitar", "Steel platebody", "Not a real item"}
	if it, ok := WornTier(worn, SlotBody); !ok || it.Name != "Steel platebody" {
		t.Fatalf("got %q %v, want Steel platebody", it.Name, ok)
	}
	if _, ok := WornTier(worn, SlotLegs); ok {
		t.Fatal("nothing is worn on the legs")
	}
	// The better of two in one slot wins.
	if it, _ := WornTier([]string{"Bronze med helm", "Mithril full helm"}, SlotHelm); it.Name != "Mithril full helm" {
		t.Fatalf("got %q, want the mithril helm", it.Name)
	}
}

func TestBuyPlanPicksBestReachableTier(t *testing.T) {
	// At 30/30 the shops' best reachable weapon is the adamant longsword (Varrock sword shop);
	// adamant scimitars are not sold, Al Kharid stops at mithril.
	plan := BuyPlan(Levels{Attack: 30, Strength: 30, Defence: 30}, nil, nil, nil)
	bySlot := map[Slot]Purchase{}
	for _, p := range plan {
		bySlot[p.Slot] = p
	}
	weapon, ok := bySlot[SlotWeapon]
	if !ok {
		t.Fatal("expected a weapon purchase at attack 30")
	}
	if weapon.Item.Tier != "Adamant" {
		t.Fatalf("got weapon %q (tier %s), want an adamant one", weapon.Item.Name, weapon.Item.Tier)
	}
	if weapon.Site.Keeper == "" {
		t.Fatal("the weapon purchase has no keeper to talk to")
	}
	// Nothing in the plan may be gated behind the guild door.
	for _, p := range plan {
		if !p.Site.Reachable() {
			t.Fatalf("plan includes %s from the unreachable %q", p.Item.Name, p.Site.Shop)
		}
		if p.Item.Level > 30 {
			t.Fatalf("plan includes %s, which needs level %d", p.Item.Name, p.Item.Level)
		}
	}
}

func TestBuyPlanSkipsWhatIsAlreadyGoodEnough(t *testing.T) {
	l := Levels{Attack: 20, Strength: 20, Defence: 20}
	worn := []string{"Mithril scimitar", "Mithril platebody", "Mithril platelegs", "Mithril full helm", "Mithril sq shield"}
	if plan := BuyPlan(l, nil, worn, nil); len(plan) != 0 {
		t.Fatalf("expected nothing to buy at mithril/20, got %d purchases (first %s)", len(plan), plan[0].Item.Name)
	}
	// One slot emptied brings exactly that slot back.
	plan := BuyPlan(l, nil, worn[1:], nil)
	if len(plan) != 1 || plan[0].Slot != SlotWeapon {
		t.Fatalf("expected one weapon purchase, got %+v", plan)
	}
}

func TestBuyPlanDoesNotRebuyWhatIsAlreadyHeld(t *testing.T) {
	l := Levels{Attack: 20, Strength: 20, Defence: 20}
	first := BuyPlan(l, nil, nil, nil)
	if len(first) == 0 {
		t.Fatal("expected a plan for a naked account")
	}
	held := []string{first[0].Item.Name}
	second := BuyPlan(l, nil, nil, held)
	for _, p := range second {
		if p.Item.Name == first[0].Item.Name {
			t.Fatalf("%s is already in the pack and must not be bought again", p.Item.Name)
		}
	}
}

func TestEquipPlanWearsTheBestHeld(t *testing.T) {
	l := Levels{Attack: 30, Strength: 30, Defence: 30}
	held := []string{"Bronze scimitar", "Adamant longsword", "Steel platebody"}
	plan := EquipPlan(l, nil, nil, held)
	names := map[string]bool{}
	for _, it := range plan {
		names[it.Name] = true
	}
	if !names["Adamant longsword"] {
		t.Fatalf("expected the adamant longsword to be worn, got %v", names)
	}
	if names["Bronze scimitar"] {
		t.Fatal("the bronze scimitar should be skipped: a better weapon is held")
	}
	if !names["Steel platebody"] {
		t.Fatalf("expected the steel platebody to be worn, got %v", names)
	}
}

func TestEquipPlanRefusesWhatTheLevelForbids(t *testing.T) {
	plan := EquipPlan(Levels{Attack: 1, Strength: 1, Defence: 1}, nil, nil, []string{"Rune longsword", "Bronze dagger"})
	for _, it := range plan {
		if it.Name == "Rune longsword" {
			t.Fatal("a level 1 account must not be told to wear a rune longsword")
		}
	}
	if len(plan) != 1 || plan[0].Name != "Bronze dagger" {
		t.Fatalf("expected only the bronze dagger, got %+v", plan)
	}
}

// TestGearCeilingIsTheNoMuleStall is the honest statement of this build's limit: past 40 the
// shops cannot dress the account, and the plan says so rather than looping.
func TestGearCeilingIsTheNoMuleStall(t *testing.T) {
	below := CheckGearCeiling(Levels{Attack: 39, Defence: 39}, nil)
	if below.Stalled {
		t.Fatal("below rune the shops still have something to sell")
	}
	at := CheckGearCeiling(Levels{Attack: 40, Defence: 40}, nil)
	if !at.Stalled {
		t.Fatal("at Attack 40 the account has outgrown every reachable shop")
	}
	if at.Reason == "" {
		t.Fatal("a stall with no stated reason is exactly the silent spin this must avoid")
	}
	if at.BestWeaponTier != "Adamant" {
		t.Fatalf("best buyable weapon tier %q, want Adamant", at.BestWeaponTier)
	}
}
