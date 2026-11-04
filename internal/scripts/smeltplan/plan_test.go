package smeltplan

import "testing"

// counts builds a Counts over a map, so a test reads as "the bank holds this".
func counts(m map[int32]int) Counts {
	return func(id int32) int { return m[id] }
}

func recipe(t *testing.T, name string) Recipe {
	t.Helper()
	r, ok := RecipeFor(name)
	if !ok {
		t.Fatalf("RecipeFor(%q) found nothing", name)
	}
	return r
}

// The whole table is grounded in content, so pin the numbers a future edit could drift:
// scripts/skill_smithing/configs/smelting/smelting.struct's levelrequired/productexp and
// ingredient counts, and the object ids read back from the live config archive.
func TestRecipesMatchContent(t *testing.T) {
	want := []struct {
		bar       string
		barID     int32
		level     int
		xp        int
		primary   string
		primaryID int32
		coal      int
	}{
		{"Bronze bar", 2349, 1, 62, "Copper ore", 436, 0},
		{"Iron bar", 2351, 15, 125, "Iron ore", 440, 0},
		{"Silver bar", 2355, 20, 137, "Silver ore", 442, 0},
		{"Steel bar", 2353, 30, 175, "Iron ore", 440, 2},
		{"Gold bar", 2357, 40, 225, "Gold ore", 444, 0},
		{"Mithril bar", 2359, 50, 300, "Mithril ore", 447, 4},
		{"Adamantite bar", 2361, 70, 375, "Adamantite ore", 449, 6},
		{"Runite bar", 2363, 85, 500, "Runite ore", 451, 8},
	}
	if len(Recipes) != len(want) {
		t.Fatalf("Recipes has %d rows, want %d", len(Recipes), len(want))
	}
	for i, w := range want {
		got := Recipes[i]
		if got.Bar != w.bar || got.BarID != w.barID || got.Level != w.level || got.XP != w.xp {
			t.Errorf("row %d = %s/%d lvl %d xp %d, want %s/%d lvl %d xp %d",
				i, got.Bar, got.BarID, got.Level, got.XP, w.bar, w.barID, w.level, w.xp)
		}
		if got.Primary.Name != w.primary || got.Primary.ID != w.primaryID || got.Primary.Per != 1 {
			t.Errorf("row %d primary = %+v, want %s/%d x1", i, got.Primary, w.primary, w.primaryID)
		}
		if w.coal == 0 {
			if got.HasSecondary() && got.Secondary.Name == "Coal" {
				t.Errorf("row %d (%s) should need no coal, got %d", i, got.Bar, got.Secondary.Per)
			}
			continue
		}
		if got.Secondary.Name != "Coal" || got.Secondary.ID != 453 || got.Secondary.Per != w.coal {
			t.Errorf("row %d (%s) secondary = %+v, want Coal/453 x%d", i, got.Bar, got.Secondary, w.coal)
		}
	}
}

// Bronze is the one alloy whose second ingredient is not coal; a table edit that assumed
// "secondary means coal" would break it silently.
func TestBronzeSecondaryIsTinNotCoal(t *testing.T) {
	r := recipe(t, "Bronze bar")
	if !r.HasSecondary() || r.Secondary.Name != "Tin ore" || r.Secondary.ID != 438 || r.Secondary.Per != 1 {
		t.Errorf("bronze secondary = %+v, want Tin ore/438 x1", r.Secondary)
	}
}

// The XP column is in content units (tenths), and the engine floors the fraction. A caller that
// divided a live XP delta by DisplayXP would mis-count bars, which is why nothing does.
func TestDisplayXPFloorsTheContentTenths(t *testing.T) {
	if got := recipe(t, "Bronze bar").DisplayXP(); got != 6 {
		t.Errorf("bronze DisplayXP = %d, want 6 (content 62 tenths, floored)", got)
	}
	if got := recipe(t, "Runite bar").DisplayXP(); got != 50 {
		t.Errorf("runite DisplayXP = %d, want 50", got)
	}
}

// Content gives iron alone a coin flip; nothing else may be marked non-deterministic.
func TestOnlyIronFailsRandomly(t *testing.T) {
	for _, r := range Recipes {
		want := r.Bar == "Iron bar"
		if r.FailsRandomly() != want {
			t.Errorf("%s FailsRandomly = %v, want %v", r.Bar, r.FailsRandomly(), want)
		}
	}
}

func TestRecipeForAcceptsShortAndCasualNames(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Bronze bar", "Bronze bar"},
		{"bronze", "Bronze bar"},
		{"  STEEL  ", "Steel bar"},
		{"Adamant", "Adamantite bar"},
		{"adamantite bar", "Adamantite bar"},
		{"Rune", "Runite bar"},
		{"runite", "Runite bar"},
	} {
		got, ok := RecipeFor(tc.in)
		if !ok || got.Bar != tc.want {
			t.Errorf("RecipeFor(%q) = %q/%v, want %q", tc.in, got.Bar, ok, tc.want)
		}
	}
	if _, ok := RecipeFor("Dragon bar"); ok {
		t.Error("RecipeFor should not invent a row for an unknown bar")
	}
	if _, ok := RecipeFor(""); ok {
		t.Error("RecipeFor(\"\") should find nothing")
	}
}

func TestSlotsPerBarAndSetsPerTrip(t *testing.T) {
	for _, tc := range []struct {
		bar         string
		slots, sets int
	}{
		{"Bronze bar", 2, 14}, // copper + tin
		{"Iron bar", 1, 28},   // iron alone
		{"Steel bar", 3, 9},   // iron + 2 coal
		{"Mithril bar", 5, 5}, // mithril + 4 coal
		{"Runite bar", 9, 3},  // runite + 8 coal
	} {
		r := recipe(t, tc.bar)
		if got := r.SlotsPerBar(); got != tc.slots {
			t.Errorf("%s SlotsPerBar = %d, want %d", tc.bar, got, tc.slots)
		}
		if got := SetsPerTrip(r); got != tc.sets {
			t.Errorf("%s SetsPerTrip = %d, want %d", tc.bar, got, tc.sets)
		}
	}
}

// The TS source's own #117: gating on the primary ore alone lets a pack with iron but no coal
// skip the bank, then spin at the furnace making nothing.
func TestCanSmeltRequiresEveryIngredient(t *testing.T) {
	steel := recipe(t, "Steel bar")
	ironOnly := counts(map[int32]int{440: 10})
	if CanSmelt(ironOnly, steel) {
		t.Error("iron with no coal should not count as able to smelt steel")
	}
	notEnoughCoal := counts(map[int32]int{440: 10, 453: 1})
	if CanSmelt(notEnoughCoal, steel) {
		t.Error("one coal is not the two steel needs")
	}
	enough := counts(map[int32]int{440: 1, 453: 2})
	if !CanSmelt(enough, steel) {
		t.Error("one iron and two coal is exactly one steel bar")
	}
}

func TestBarsFundedByTakesTheScarcestIngredient(t *testing.T) {
	steel := recipe(t, "Steel bar")
	if got := BarsFundedBy(counts(map[int32]int{440: 10, 453: 6}), steel); got != 3 {
		t.Errorf("BarsFundedBy = %d, want 3 (6 coal / 2 per bar is the limit)", got)
	}
	if got := BarsFundedBy(counts(map[int32]int{440: 2, 453: 100}), steel); got != 2 {
		t.Errorf("BarsFundedBy = %d, want 2 (iron is the limit)", got)
	}
	if got := BarsFundedBy(counts(map[int32]int{}), steel); got != 0 {
		t.Errorf("an empty bank funds %d bars, want 0", got)
	}
}

func TestWithdrawForCapsAtAPackAndKeepsTheRatio(t *testing.T) {
	steel := recipe(t, "Steel bar")
	// A deep bank: capped at 9 sets, so 9 iron and 18 coal, which is 27 of 28 slots.
	plan := WithdrawFor(counts(map[int32]int{440: 1000, 453: 1000}), steel)
	if len(plan) != 2 {
		t.Fatalf("plan has %d steps, want 2", len(plan))
	}
	if plan[0].Ore.Name != "Iron ore" || plan[0].Count != 9 {
		t.Errorf("step 0 = %+v, want 9 Iron ore", plan[0])
	}
	if plan[1].Ore.Name != "Coal" || plan[1].Count != 18 {
		t.Errorf("step 1 = %+v, want 18 Coal", plan[1])
	}
	if slots := plan[0].Count + plan[1].Count; slots > PackSlots {
		t.Errorf("plan wants %d slots, more than a pack", slots)
	}
}

func TestWithdrawForShortBankTakesWhatItCan(t *testing.T) {
	steel := recipe(t, "Steel bar")
	plan := WithdrawFor(counts(map[int32]int{440: 5, 453: 4}), steel)
	if len(plan) != 2 || plan[0].Count != 2 || plan[1].Count != 4 {
		t.Fatalf("plan = %+v, want 2 iron + 4 coal (coal funds only 2 bars)", plan)
	}
}

// The stop condition: an empty plan, not a short one, is what ends the run.
func TestWithdrawForEmptyWhenNotEvenOneBar(t *testing.T) {
	steel := recipe(t, "Steel bar")
	if plan := WithdrawFor(counts(map[int32]int{440: 10, 453: 1}), steel); plan != nil {
		t.Errorf("one coal cannot fund a steel bar, want no plan, got %+v", plan)
	}
	if plan := WithdrawFor(counts(map[int32]int{}), steel); plan != nil {
		t.Errorf("an empty bank should produce no plan, got %+v", plan)
	}
}

func TestDecideStage(t *testing.T) {
	if got := DecideStage(Snapshot{CanSmelt: true}); got != StageSmelt {
		t.Errorf("holding ore should smelt, got %q", got)
	}
	// Smelting wins even once the bank is known empty: the pack in hand is still worth bars.
	if got := DecideStage(Snapshot{CanSmelt: true, BankExhausted: true}); got != StageSmelt {
		t.Errorf("a loaded pack should finish before stopping, got %q", got)
	}
	if got := DecideStage(Snapshot{}); got != StageBank {
		t.Errorf("an empty pack should bank, got %q", got)
	}
	if got := DecideStage(Snapshot{BankExhausted: true}); got != StageDone {
		t.Errorf("empty pack and empty bank should stop, got %q", got)
	}
}

func TestSmeltStalled(t *testing.T) {
	if SmeltStalled(5, 8, 20_000) {
		t.Error("ore still falling is progress, however long the batch runs")
	}
	if SmeltStalled(5, 5, 1_000) {
		t.Error("a short pause is not a stall")
	}
	if !SmeltStalled(5, 5, SmeltIdle+1) {
		t.Error("no movement past the idle window is a stall")
	}
	if !SmeltStalled(6, 5, SmeltIdle+1) {
		t.Error("a count that went UP past the idle window is also a stall")
	}
}

func TestSettingsNormalise(t *testing.T) {
	got := Settings{Bar: "adamant", BankBooth: "  ", LogDetail: ""}.Normalised()
	if got.Bar != "Adamantite bar" {
		t.Errorf("Bar = %q, want the canonical Adamantite bar", got.Bar)
	}
	if got.BankBooth != BoothName {
		t.Errorf("a blank booth should fall back to %q, got %q", BoothName, got.BankBooth)
	}
	if got.Verbose() {
		t.Error("a blank LogDetail should not be verbose")
	}
	if r := got.Recipe(); r.BarID != 2361 {
		t.Errorf("Recipe() = %s/%d, want the adamantite row", r.Bar, r.BarID)
	}
}

func TestSettingsUnknownBarFallsBackButIsReportable(t *testing.T) {
	got := Settings{Bar: "Dragon bar"}.Normalised()
	if got.Bar != DefaultSettings().Bar {
		t.Errorf("an unknown bar should fall back to the default, got %q", got.Bar)
	}
	if name, unknown := UnknownBar("Dragon bar"); !unknown || name != "Dragon bar" {
		t.Errorf("UnknownBar should report the typo, got %q/%v", name, unknown)
	}
	if _, unknown := UnknownBar("Rune"); unknown {
		t.Error("a valid short name is not unknown")
	}
}

func TestBarNamesCoversTheWholeTable(t *testing.T) {
	names := BarNames()
	if len(names) != len(Recipes) {
		t.Fatalf("BarNames has %d entries, want %d", len(names), len(Recipes))
	}
	for i, n := range names {
		if n != Recipes[i].Bar {
			t.Errorf("BarNames[%d] = %q, want %q", i, n, Recipes[i].Bar)
		}
		if _, ok := RecipeFor(n); !ok {
			t.Errorf("BarNames offers %q, which RecipeFor does not accept", n)
		}
	}
}
