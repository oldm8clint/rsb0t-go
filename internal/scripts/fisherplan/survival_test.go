package fisherplan

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
)

func TestDefaultSurvivalIsOffAndInRange(t *testing.T) {
	s := DefaultSurvival()
	if s.Enabled {
		t.Fatal("survival must default off (account owner's call, 2026-09-16) -- opt in via eatFood")
	}
	if got := s.Normalise(); !reflect.DeepEqual(got, s) {
		t.Fatalf("defaults change under Normalise: %+v -> %+v", s, got)
	}
}

func TestNormaliseClampsAndFiltersFoods(t *testing.T) {
	s := Survival{Enabled: true, Foods: []string{"lobster", "Raw shrimps", "LOBSTER", "bread"}, FoodCount: 40, EatBelow: 0.95, FleeBelow: 0.99, MaxHit: 99}.Normalise()
	if s.FoodCount != MaxFoodCount || s.EatBelow != 0.9 || s.FleeBelow != 0.8 || s.MaxHit != 30 {
		t.Fatalf("clamps: %+v", s)
	}
	if !reflect.DeepEqual(s.Foods, []string{"Lobster", "Bread"}) {
		t.Fatalf("foods = %v, want known foods once each in FoodOptions spelling", s.Foods)
	}
	low := Survival{EatBelow: 0.2, FleeBelow: 0.5, FoodCount: -1, MaxHit: -3}.Normalise()
	if low.FleeBelow != low.EatBelow || low.FoodCount != 0 || low.MaxHit != 0 {
		t.Fatalf("flee share must not pass the eat share, negatives clamp to zero: %+v", low)
	}
}

func TestParseFoodList(t *testing.T) {
	if got := ParseFoodList(""); got != nil {
		t.Errorf("blank = %v, want nil (any food)", got)
	}
	if got := ParseFoodList("any"); got != nil {
		t.Errorf("any = %v, want nil", got)
	}
	if got := ParseFoodList(" Lobster, ,Tuna "); !reflect.DeepEqual(got, []string{"Lobster", "Tuna"}) {
		t.Errorf("list = %v", got)
	}
}

func TestAllows(t *testing.T) {
	any := DefaultSurvival()
	if !any.Allows("Shrimps") || !any.Allows("2/3 cake") || any.Allows("Raw shrimps") || any.Allows("Coins") {
		t.Fatal("any-food survival must allow every cooked food form and nothing else")
	}
	only := Survival{Enabled: true, Foods: []string{"Tuna"}}
	if !only.Allows("tuna") || only.Allows("Shrimps") {
		t.Fatal("a food list must restrict what counts")
	}
}

func TestThresholdsKeepAHitOfRoom(t *testing.T) {
	s := DefaultSurvival()
	cases := []struct{ maxHP, eat, flee int }{
		{10, 5, 4},   // a fresh account: 50% is 5, the margin is also 5; 30% is 3, raised to the 4 max hit
		{20, 10, 6},  // 50% and 30% of 20
		{99, 50, 30}, // shares dominate once HP is high
		{5, 4, 4},    // never eats at full HP, flee never above eat
	}
	for _, c := range cases {
		if got := s.EatAt(c.maxHP); got != c.eat {
			t.Errorf("EatAt(%d) = %d, want %d", c.maxHP, got, c.eat)
		}
		if got := s.FleeAt(c.maxHP); got != c.flee {
			t.Errorf("FleeAt(%d) = %d, want %d", c.maxHP, got, c.flee)
		}
	}
	if got := ResumeAt(10); got != 9 {
		t.Errorf("ResumeAt(10) = %d, want 9", got)
	}
}

func TestDecideSurvival(t *testing.T) {
	s := DefaultSurvival()
	s.Enabled = true // exercising the enabled decision logic regardless of the app-level default
	base := SurvivalInput{MaxHP: 10, Known: true, InCatherby: true}
	with := func(f func(*SurvivalInput)) SurvivalInput {
		in := base
		f(&in)
		return in
	}
	cases := []struct {
		name string
		in   SurvivalInput
		want SurvivalAction
	}{
		{"healthy does nothing", with(func(i *SurvivalInput) { i.HP = 9; i.Food = 3 }), SurvivalAction{}},
		{"unknown HP does nothing", with(func(i *SurvivalInput) { i.Known = false; i.HP = 1 }), SurvivalAction{}},
		{"dead does nothing", with(func(i *SurvivalInput) { i.HP = 0 }), SurvivalAction{}},
		{"eats at the threshold", with(func(i *SurvivalInput) { i.HP = 5; i.Food = 1 }), SurvivalAction{Eat: true}},
		{"critical with food and no attacker only eats", with(func(i *SurvivalInput) { i.HP = 3; i.Food = 1 }), SurvivalAction{Eat: true}},
		{"critical with food under attack eats and flees", with(func(i *SurvivalInput) { i.HP = 3; i.Food = 1; i.InCombat = true }), SurvivalAction{Eat: true, Flee: true}},
		{"critical with no food flees and rests", with(func(i *SurvivalInput) { i.HP = 4 }), SurvivalAction{Flee: true, Rest: true}},
		{"hurt but above flee with no food keeps going", with(func(i *SurvivalInput) { i.HP = 5 }), SurvivalAction{}},
		{"no food under attack flees at any HP", with(func(i *SurvivalInput) { i.HP = 7; i.InCombat = true }), SurvivalAction{Flee: true}},
		{"food under attack above critical fights on", with(func(i *SurvivalInput) { i.HP = 7; i.Food = 2; i.InCombat = true }), SurvivalAction{}},
		{"at the bank with no food rests, no flee", with(func(i *SurvivalInput) { i.HP = 2; i.AtBank = true }), SurvivalAction{Rest: true}},
		{"resting holds until resume", with(func(i *SurvivalInput) { i.HP = 8; i.AtBank = true; i.Resting = true }), SurvivalAction{Rest: true}},
		{"resting ends at resume", with(func(i *SurvivalInput) { i.HP = 9; i.AtBank = true; i.Resting = true }), SurvivalAction{}},
		{"food ends resting", with(func(i *SurvivalInput) { i.HP = 8; i.Food = 2; i.Resting = true }), SurvivalAction{}},
		{"outside Catherby only eats", with(func(i *SurvivalInput) { i.HP = 2; i.InCatherby = false; i.InCombat = true; i.Food = 1 }), SurvivalAction{Eat: true}},
		{"outside Catherby with no food does nothing", with(func(i *SurvivalInput) { i.HP = 2; i.InCatherby = false }), SurvivalAction{}},
	}
	for _, c := range cases {
		if got := DecideSurvival(s, c.in); got != c.want {
			t.Errorf("%s: DecideSurvival = %+v, want %+v", c.name, got, c.want)
		}
	}
	off := s
	off.Enabled = false
	if got := DecideSurvival(off, with(func(i *SurvivalInput) { i.HP = 1 })); got != (SurvivalAction{}) {
		t.Errorf("disabled survival acted: %+v", got)
	}
	if (SurvivalAction{Rest: true}).Interrupts() || !(SurvivalAction{Eat: true}).Interrupts() || !(SurvivalAction{Flee: true}).Interrupts() {
		t.Error("only eating and fleeing interrupt the loop")
	}
}

func TestFoodHeldNamesAndKeep(t *testing.T) {
	s := DefaultSurvival()
	s.Enabled = true // exercising the enabled decision logic regardless of the app-level default
	pack := []freshstart.PackItem{{Name: "Small fishing net", Count: 1}, {Name: "Shrimps", Count: 2}, {Name: "Raw shrimps", Count: 5}, {Name: "Bread", Count: 1}}
	if got := s.FoodHeld(pack); got != 3 {
		t.Errorf("FoodHeld = %d, want 3", got)
	}
	if got := s.FoodNames(pack); !reflect.DeepEqual(got, []string{"Shrimps", "Bread"}) {
		t.Errorf("FoodNames = %v", got)
	}
	keep := s.KeepNames()
	for _, want := range []string{"shrimps", "lobster", "2/3 cake"} {
		if !hasName(keep, want) {
			t.Errorf("KeepNames missing %q", want)
		}
	}
	if hasName(keep, "raw shrimps") {
		t.Error("KeepNames must not keep raw fish from the mule")
	}
	off := s
	off.Enabled = false
	if off.KeepNames() != nil {
		t.Error("disabled survival keeps nothing extra")
	}
}

func TestPickFood(t *testing.T) {
	s := DefaultSurvival()
	pack := []freshstart.PackItem{{Name: "Lobster", Count: 1}, {Name: "Shrimps", Count: 1}, {Name: "Bread", Count: 1}}
	// 40/99, not critical, 59 missing: every bite fits, so the biggest.
	if got, _ := s.PickFood(pack, 40, 99); got != "Lobster" {
		t.Errorf("room for all = %q, want Lobster", got)
	}
	// 5/10, 5 missing: bread (4) is the biggest that fits.
	if got, _ := s.PickFood(pack, 5, 10); got != "Bread" {
		t.Errorf("5 missing = %q, want Bread", got)
	}
	// 98/99 would never be asked, but with 1 missing nothing fits: the smallest bite.
	if got, _ := s.PickFood(pack, 98, 99); got != "Shrimps" {
		t.Errorf("nothing fits = %q, want Shrimps", got)
	}
	// Critical takes the biggest whatever it wastes.
	if got, _ := s.PickFood(pack, 3, 10); got != "Lobster" {
		t.Errorf("critical = %q, want Lobster", got)
	}
	if _, ok := s.PickFood([]freshstart.PackItem{{Name: "Raw tuna", Count: 1}}, 3, 10); ok {
		t.Error("raw fish is not food")
	}
}

func TestFoodToWithdraw(t *testing.T) {
	s := DefaultSurvival()
	s.Enabled = true // exercising the enabled decision logic regardless of the app-level default
	bank := []freshstart.PackItem{{Name: "Lobster", Count: 10}, {Name: "Tuna", Count: 1}, {Name: "Shrimps", Count: 20}, {Name: "Raw lobster", Count: 50}}
	// A 10 HP account: lobster and tuna both heal the whole 10, tuna is the smaller bite.
	got := s.FoodToWithdraw(bank, 0, 10)
	want := []freshstart.SellLine{{Name: "Tuna", Count: 1}, {Name: "Lobster", Count: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("10 HP withdraw = %+v, want %+v", got, want)
	}
	// A 60 HP account uses the lobster's whole 12.
	if got := s.FoodToWithdraw(bank, 1, 60); !reflect.DeepEqual(got, []freshstart.SellLine{{Name: "Lobster", Count: 2}}) {
		t.Errorf("60 HP withdraw = %+v", got)
	}
	if got := s.FoodToWithdraw(bank, 3, 10); got != nil {
		t.Errorf("a full food count withdraws nothing, got %+v", got)
	}
	only := Survival{Enabled: true, Foods: []string{"Shrimps"}, FoodCount: 3}
	if got := only.FoodToWithdraw(bank, 0, 10); !reflect.DeepEqual(got, []freshstart.SellLine{{Name: "Shrimps", Count: 3}}) {
		t.Errorf("restricted withdraw = %+v", got)
	}
	off := s
	off.Enabled = false
	if off.FoodToWithdraw(bank, 0, 10) != nil {
		t.Error("disabled survival withdraws nothing")
	}
}

func TestStarterKeepKeepsTheStarterFood(t *testing.T) {
	keep := StarterKeep()
	for _, want := range []string{"Shrimps", "Bread"} {
		if !hasName(keep, want) {
			t.Errorf("StarterKeep sells the starter kit's %s (content tutorial.rs2 gives one of each): %v", want, keep)
		}
	}
	if len(freshstart.SellPlan([]freshstart.PackItem{{Name: "Shrimps", Count: 1}, {Name: "Bread", Count: 1}}, keep)) != 0 {
		t.Error("the store trip must not sell the starter food")
	}
}
