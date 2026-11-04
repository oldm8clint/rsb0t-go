package combatplan

import "testing"

// contentSpells mirrors the rows of skill_combat/configs/magic/magic_combat_spells.dbrow this
// package's decisions actually turn on: the bottom of the ladder, the best spell whose runes a
// reachable shop stocks, and the two Waves that need a blood rune. Kept local to the test so
// the pure package still imports nothing, which is the whole point of SpellOption.
func contentSpells() []SpellOption {
	return []SpellOption{
		{Name: "Wind Strike", Level: 1, Runes: []RuneCost{{"Mind rune", 1}, {"Air rune", 1}}},
		{Name: "Fire Strike", Level: 13, Runes: []RuneCost{{"Mind rune", 1}, {"Fire rune", 3}, {"Air rune", 2}}},
		{Name: "Fire Bolt", Level: 35, Runes: []RuneCost{{"Chaos rune", 1}, {"Fire rune", 4}, {"Air rune", 3}}},
		{Name: "Wind Blast", Level: 41, Runes: []RuneCost{{"Death rune", 1}, {"Air rune", 3}}},
		{Name: "Fire Blast", Level: 59, Runes: []RuneCost{{"Death rune", 1}, {"Fire rune", 5}, {"Air rune", 4}}},
		{Name: "Wind Wave", Level: 62, Members: true, Runes: []RuneCost{{"Blood rune", 1}, {"Air rune", 5}}},
		{Name: "Fire Wave", Level: 75, Members: true, Runes: []RuneCost{{"Blood rune", 1}, {"Fire rune", 7}, {"Air rune", 5}}},
	}
}

func TestStyleToTrainFollowsMeleeThenRangedThenMagic(t *testing.T) {
	g := DefaultGoals()
	cases := []struct {
		name string
		in   Levels
		want Style
		ok   bool
	}{
		{"melee first while any melee stat is short", Levels{Attack: 75, Strength: 74, Defence: 75, Ranged: 1, Magic: 1}, StyleMelee, true},
		{"ranged once melee is done", Levels{Attack: 75, Strength: 75, Defence: 75, Ranged: 40, Magic: 1}, StyleRanged, true},
		{"magic last", Levels{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 60}, StyleMagic, true},
		{"nothing left", Levels{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 75}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := StyleToTrain(c.in, g)
			if got != c.want || ok != c.ok {
				t.Fatalf("got %q %v, want %q %v", got, ok, c.want, c.ok)
			}
		})
	}
}

// TestStyleToTrainIgnoresDisciplinesNobodyAskedFor is the backward-compatibility guard: a run
// configured the way every existing proof configures one, with no Ranged or Magic goal, must
// still be a melee run and must still finish.
func TestStyleToTrainIgnoresDisciplinesNobodyAskedFor(t *testing.T) {
	meleeOnly := Goals{Attack: 40, Strength: 40, Defence: 40, Prayer: 1}
	if _, ok := StyleToTrain(Levels{Attack: 40, Strength: 40, Defence: 40}, meleeOnly); ok {
		t.Fatal("a melee-only goal set should be finished once melee is done, not fall through to Ranged")
	}
	if !AllGoalsMet(Levels{Attack: 40, Strength: 40, Defence: 40}, meleeOnly, 1) {
		t.Fatal("AllGoalsMet should agree with StyleToTrain for a melee-only goal set")
	}
}

func TestAllGoalsMetIncludesPrayer(t *testing.T) {
	g := DefaultGoals()
	maxed := Levels{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 75}
	if AllGoalsMet(maxed, g, 42) {
		t.Fatal("prayer 42 against a goal of 43 is not finished")
	}
	if !AllGoalsMet(maxed, g, 43) {
		t.Fatal("expected finished at prayer 43")
	}
}

// TestPlanRangedKitStopsAtLowesShelf is the Ranged supply ceiling. Lowe stocks bows to oak and
// arrows to steel; everything above is the Ranging Guild, which this trainer never enters.
func TestPlanRangedKitStopsAtLowesShelf(t *testing.T) {
	kit, ok := PlanRangedKit(Levels{Ranged: 75}, nil, nil)
	if !ok {
		t.Fatal("expected a kit: Lowe always has something")
	}
	if kit.Bow.Tier != "Oak" {
		t.Fatalf("bow = %q (tier %q), want an oak bow: willow and better are Ranging Guild only", kit.Bow.Name, kit.Bow.Tier)
	}
	if kit.Arrow.Tier != "Steel" {
		t.Fatalf("arrow = %q (tier %q), want steel: mithril and better are Ranging Guild only", kit.Arrow.Name, kit.Arrow.Tier)
	}
	// The pairing must satisfy the server's own check, not just look sensible.
	if !ArrowFitsBow(kit.Arrow, kit.Bow) {
		t.Fatalf("%s cannot be fired from a %s", kit.Arrow.Name, kit.Bow.Name)
	}
}

func TestPlanRangedKitAtLevelOne(t *testing.T) {
	kit, ok := PlanRangedKit(Levels{Ranged: 1}, nil, nil)
	if !ok {
		t.Fatal("expected a kit at Ranged 1")
	}
	if kit.Bow.Level > 1 {
		t.Fatalf("bow %q needs Ranged %d, which a level 1 account cannot wear", kit.Bow.Name, kit.Bow.Level)
	}
	if !ArrowFitsBow(kit.Arrow, kit.Bow) {
		t.Fatalf("%s cannot be fired from a %s", kit.Arrow.Name, kit.Bow.Name)
	}
}

// TestRangingGuildIsNeverPlanned pins the gate itself. The guild is the only source of the
// better bows and arrows, so if Reachable ever stops excluding it every Ranged plan silently
// starts walking at a door it has never opened.
func TestRangingGuildIsNeverPlanned(t *testing.T) {
	site, ok := shopSites[ShopRangingGuild]
	if !ok {
		t.Fatal("the Ranging Guild should still be listed, for honesty")
	}
	if site.Reachable() {
		t.Fatal("the Ranging Guild must not read as reachable: Ranged 40 door, and no route walked with this client")
	}
	for _, l := range []Levels{{Ranged: 20}, {Ranged: 40}, {Ranged: 75}} {
		kit, ok := PlanRangedKit(l, nil, nil)
		if !ok {
			t.Fatalf("Ranged %d: expected a kit", l.Ranged)
		}
		if kit.Bow.Shop == ShopRangingGuild || kit.Arrow.Shop == ShopRangingGuild {
			t.Fatalf("Ranged %d planned a Ranging Guild trip: bow %q from %q, arrow %q from %q",
				l.Ranged, kit.Bow.Name, kit.Bow.Shop, kit.Arrow.Name, kit.Arrow.Shop)
		}
	}
}

func TestCheckRangedCeiling(t *testing.T) {
	// At Ranged 5 an oak bow is both the best wearable and the best buyable, so nothing is
	// stalled and the run has nothing to announce.
	if c := CheckRangedCeiling(Levels{Ranged: 5}, nil); c.Stalled {
		t.Fatalf("Ranged 5 should not be stalled: %s", c.Reason)
	}
	// At Ranged 20 a willow bow is wearable and unbuyable, which is the ceiling.
	c := CheckRangedCeiling(Levels{Ranged: 20}, nil)
	if !c.Stalled {
		t.Fatal("Ranged 20 allows a willow bow that no reachable shop stocks, so the ceiling should be announced")
	}
	if c.Reason == "" {
		t.Fatal("a stalled ceiling must carry the reason, since that line is the whole output")
	}
}

// TestPlanMagicSpellRefusesTheWavesForSupplyNotLevel is the blood-rune wall. At Magic 62 on a
// members world Wind Wave is castable and unsupplied, and choosing it would leave the run
// autocasting a spell it can never restock.
func TestPlanMagicSpellRefusesTheWavesForSupplyNotLevel(t *testing.T) {
	spells := contentSpells()
	buyable := ShopRunes()
	got, ok := PlanMagicSpell(62, true, spells, "Fire rune", buyable)
	if !ok {
		t.Fatal("expected a spell at Magic 62")
	}
	if got.Name != "Fire Blast" {
		t.Fatalf("got %q, want Fire Blast: Wind Wave is castable at 62 but its blood rune is sold only behind a Magic 66 door", got.Name)
	}
	// And still at 75, where every Wave is castable.
	if got, _ := PlanMagicSpell(75, true, spells, "Fire rune", buyable); got.Name != "Fire Blast" {
		t.Fatalf("got %q at Magic 75, want Fire Blast", got.Name)
	}
}

func TestPlanMagicSpellClimbsWithLevel(t *testing.T) {
	spells := contentSpells()
	buyable := ShopRunes()
	cases := []struct {
		level int
		want  string
	}{
		{1, "Wind Strike"},
		{13, "Fire Strike"},
		{35, "Fire Bolt"},
		{41, "Wind Blast"},
		{59, "Fire Blast"},
	}
	for _, c := range cases {
		got, ok := PlanMagicSpell(c.level, false, spells, "Fire rune", buyable)
		if !ok || got.Name != c.want {
			t.Fatalf("Magic %d: got %q %v, want %q", c.level, got.Name, ok, c.want)
		}
	}
}

func TestPlanMagicSpellHonoursMembers(t *testing.T) {
	// Nothing changes on a free world here, because the only members spells are the Waves and
	// those are already refused for supply. The check exists so that stays true by test rather
	// than by luck if a buyable members spell is ever added.
	free, _ := PlanMagicSpell(75, false, contentSpells(), "Fire rune", ShopRunes())
	if free.Members {
		t.Fatalf("a free world chose the members spell %q", free.Name)
	}
}

func TestSpellSupplied(t *testing.T) {
	buyable := ShopRunes()
	fireBlast := SpellOption{Name: "Fire Blast", Level: 59, Runes: []RuneCost{{"Death rune", 1}, {"Fire rune", 5}, {"Air rune", 4}}}
	windWave := SpellOption{Name: "Wind Wave", Level: 62, Members: true, Runes: []RuneCost{{"Blood rune", 1}, {"Air rune", 5}}}
	if !SpellSupplied(fireBlast, "Fire rune", buyable) {
		t.Fatal("Fire Blast's runes are all on Aubury's shelf")
	}
	if SpellSupplied(windWave, "Air rune", buyable) {
		t.Fatal("Wind Wave needs a blood rune, which no reachable shop stocks")
	}
	// The staff only excuses its OWN rune. A staff of air does not conjure a death rune.
	noDeath := map[string]bool{"air rune": true, "fire rune": true}
	if SpellSupplied(fireBlast, "Fire rune", noDeath) {
		t.Fatal("a missing death rune should make Fire Blast unsupplied")
	}
}

func TestRuneShoppingListDropsTheStaffsOwnRune(t *testing.T) {
	fireBlast := SpellOption{Name: "Fire Blast", Runes: []RuneCost{{"Death rune", 1}, {"Fire rune", 5}, {"Air rune", 4}}}
	list := RuneShoppingList(fireBlast, "Fire rune", 100)
	for _, r := range list {
		if r.Name == "Fire rune" {
			t.Fatal("a staff of fire supplies the fire rune, so it must not be on the shopping list")
		}
	}
	want := map[string]int{"Death rune": 100, "Air rune": 400}
	if len(list) != len(want) {
		t.Fatalf("got %d rune kinds, want %d: %+v", len(list), len(want), list)
	}
	for _, r := range list {
		if want[r.Name] != r.Count {
			t.Fatalf("%s = %d, want %d", r.Name, r.Count, want[r.Name])
		}
	}
	// With no staff every rune is bought, including the element's own.
	if n := len(RuneShoppingList(fireBlast, "", 1)); n != 3 {
		t.Fatalf("with no staff the list should carry all three runes, got %d", n)
	}
}

func TestCheckMagicCeiling(t *testing.T) {
	spells := contentSpells()
	// At 59 Fire Blast is both the best castable and the best suppliable: no ceiling.
	if c := CheckMagicCeiling(59, true, spells, "Fire rune"); c.Stalled {
		t.Fatalf("Magic 59 should not be stalled: %s", c.Reason)
	}
	// At 62 Wind Wave is castable and unsupplied.
	c := CheckMagicCeiling(62, true, spells, "Fire rune")
	if !c.Stalled {
		t.Fatal("Magic 62 allows Wind Wave, whose blood rune cannot be bought, so the ceiling should be announced")
	}
	if c.Reason == "" {
		t.Fatal("a stalled ceiling must carry its reason")
	}
}

func TestElementOf(t *testing.T) {
	element, staff, runeName, ok := ElementOf("Fire Blast")
	if !ok || element != "Fire" || staff != "Staff of fire" || runeName != "Fire rune" {
		t.Fatalf("got %q %q %q %v", element, staff, runeName, ok)
	}
	if _, _, _, ok := ElementOf("Wind Strike"); !ok {
		t.Fatal("Wind Strike is an air spell and should resolve")
	}
	// The empty string must answer false rather than panic, which is what indexing the first
	// field of an empty name would have done.
	if _, _, _, ok := ElementOf(""); ok {
		t.Fatal("an empty spell name has no element")
	}
	if _, _, _, ok := ElementOf("Crumble Undead"); ok {
		t.Fatal("a non-elemental spell has no elemental staff")
	}
}

// TestWearTheBowYouActuallyHave is the regression guard for the defect that stopped the first
// live Ranged run dead, and for the one just before it.
//
// Both came from the same confusion: asking the BUY plan what to WEAR. The account held a plain
// Shortbow, Lowe sells an Oak shortbow, so the weapon step demanded a bow that had never been in
// the pack and warned once a minute instead of shooting anything. Melee never had this bug
// because EquipPlan and BuyPlan were always separate functions, and these two lookups are what
// give Ranged the same separation.
func TestWearTheBowYouActuallyHave(t *testing.T) {
	l := Levels{Ranged: 20}
	held := []string{"Shortbow", "Bronze arrow"}

	bow, ok := BestHeldInSlot(SlotBow, l, nil, held)
	if !ok || bow.Name != "Shortbow" {
		t.Fatalf("best held bow = %q %v, want the Shortbow actually in the pack", bow.Name, ok)
	}

	// The case is only meaningful while the shop genuinely sells something better, which is what
	// made the live run refuse to fight.
	kit, kitOK := PlanRangedKit(l, nil, nil)
	if !kitOK || kit.Bow.Name == bow.Name {
		t.Fatalf("the buy plan offers %q, which makes this test vacuous", kit.Bow.Name)
	}

	arrow, ok := BestHeldArrowForBow(bow, held)
	if !ok || arrow.Name != "Bronze arrow" {
		t.Fatalf("best held arrow = %q %v, want the Bronze arrow in the pack", arrow.Name, ok)
	}

	// And the pairing rule still binds: a steel arrow's levelrequire param is 5 against a plain
	// shortbow's 1, so the server would answer "Your bow isn't powerful enough for those arrows."
	if got, ok := BestHeldArrowForBow(bow, []string{"Steel arrow"}); ok {
		t.Fatalf("a %s should not be fireable from a %s", got.Name, bow.Name)
	}
}

// TestBestHeldRespectsTheWearRequirement stops the wear lookup handing back something the server
// would refuse, which would leave a run insisting on a bow it cannot equip.
func TestBestHeldRespectsTheWearRequirement(t *testing.T) {
	held := []string{"Willow shortbow", "Shortbow"}
	if got, ok := BestHeldInSlot(SlotBow, Levels{Ranged: 1}, nil, held); !ok || got.Name != "Shortbow" {
		t.Fatalf("at Ranged 1 got %q %v, want the Shortbow: a willow bow needs Ranged 20", got.Name, ok)
	}
	if got, ok := BestHeldInSlot(SlotBow, Levels{Ranged: 20}, nil, held); !ok || got.Name != "Willow shortbow" {
		t.Fatalf("at Ranged 20 got %q %v, want the willow bow", got.Name, ok)
	}
	// Nothing held in the slot is an honest "no", not a zero value that reads as an item.
	if got, ok := BestHeldInSlot(SlotBow, Levels{Ranged: 75}, nil, []string{"Trout", "Coins"}); ok {
		t.Fatalf("got %q from a pack with no bow in it", got.Name)
	}
}

func TestStaffRune(t *testing.T) {
	if got := StaffRune("Staff of fire"); got != "Fire rune" {
		t.Fatalf("Staff of fire supplies %q, want Fire rune", got)
	}
	if got := StaffRune("staff of air"); got != "Air rune" {
		t.Fatalf("matching should be case-insensitive, got %q", got)
	}
	// Anything that is not one of the four elemental staffs supplies nothing, which is what
	// stops SpellSupplied excusing a rune the account has no source for.
	for _, name := range []string{"Rune scimitar", "Plainstaff", "", "Magic staff"} {
		if got := StaffRune(name); got != "" {
			t.Fatalf("%q should supply no rune, got %q", name, got)
		}
	}
}

func TestStaffsAreBuyableFromZaff(t *testing.T) {
	it, ok := ItemByName("Staff of fire")
	if !ok {
		t.Fatal("the gear table should know the staff a Fire Blast run wields")
	}
	if it.Slot != SlotStaff {
		t.Fatalf("staff slot = %q, want %q", it.Slot, SlotStaff)
	}
	if it.Shop != ShopVarrockStaff {
		t.Fatalf("staff shop = %q, want Zaff's", it.Shop)
	}
	site, ok := ShopSiteFor(it)
	if !ok || !site.Reachable() {
		t.Fatalf("Zaff should be reachable: site %+v, ok %v", site, ok)
	}
	if site.Keeper != "Zaff" {
		t.Fatalf("keeper = %q, want Zaff", site.Keeper)
	}
}

func TestRuneShopIsReachable(t *testing.T) {
	site, ok := shopSites[ShopVarrockRune]
	if !ok {
		t.Fatal("a Magic run needs a rune shop in the table")
	}
	if site.Keeper != "Aubury" || !site.Reachable() {
		t.Fatalf("rune shop = %+v, want a reachable Aubury", site)
	}
}

// TestStaffsNeverLeakIntoAMeleePlan is the regression guard for putting staffs into the shared
// item table. BuyPlan and EquipPlan walk MeleeSlots, so a staff must never appear in either,
// however high the account's Magic gets. A staff bought as a "weapon" would be worn in place of
// the sword and the melee rung would stop killing anything.
func TestStaffsNeverLeakIntoAMeleePlan(t *testing.T) {
	l := Levels{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 75}
	for _, p := range BuyPlan(l, nil, nil, nil) {
		if p.Slot == SlotStaff || p.Item.Slot == SlotStaff {
			t.Fatalf("a melee buy plan asked for the staff %q", p.Item.Name)
		}
	}
	held := []string{"Staff of fire", "Adamant longsword"}
	for _, it := range EquipPlan(l, nil, nil, held) {
		if it.Slot == SlotStaff {
			t.Fatalf("a melee equip plan asked to wear %q", it.Name)
		}
	}
	// The melee weapon slot still resolves to a real weapon with the staff in the table.
	if w, ok := bestReachableBuyable(SlotWeapon, l, nil, nil); !ok || w.Slot != SlotWeapon {
		t.Fatalf("melee weapon plan = %+v, ok %v", w, ok)
	}
}

// TestArrowRecoveryOddsMatchContent pins the number the arrow budget rests on. It is read out
// of content rather than estimated: ^dropammo_chance = 5 and ranged_dropammo_npc drops the
// arrow when random(5) is not zero, which is 4 times in 5.
func TestArrowRecoveryOddsMatchContent(t *testing.T) {
	if ArrowDropNumerator != 4 || ArrowDropDenominator != 5 {
		t.Fatalf("arrow recovery = %d/%d, want 4/5 from ^dropammo_chance = 5",
			ArrowDropNumerator, ArrowDropDenominator)
	}
	if ArrowDropNumerator >= ArrowDropDenominator {
		t.Fatal("every arrow cannot be recovered: the fifth is destroyed outright")
	}
}
