package fighterfood

import "testing"

func TestFoodHealAmount(t *testing.T) {
	cases := map[string]int{
		"Shark":            20,
		"lobster":          12,
		"2/3 cake":         4,
		"chocolate slice":  5,
		"half a meat pie":  4,
		"Anchovies":        3,
		"Bread":            4,
		"Mackerel":         6,
		"Cod":              7,
		"":                 DefaultFoodHeal,
		"totally made up":  DefaultFoodHeal,
		"raw lobster meat": 12, // substring fallback
	}
	for food, want := range cases {
		if got := FoodHealAmount(food); got != want {
			t.Errorf("FoodHealAmount(%q) = %d, want %d", food, got, want)
		}
	}
}

func TestIsFoodItem(t *testing.T) {
	if !IsFoodItem("2/3 Cake", "Cake") {
		t.Fatal("a partial cake form should count as the configured cake")
	}
	if IsFoodItem("Trout", "Cake") {
		t.Fatal("an unrelated item should not count")
	}
	if !IsFoodItem("lobster", "Lobster") {
		t.Fatal("an exact-name single-bite food should count as itself")
	}
}

func TestShouldEat(t *testing.T) {
	if ShouldEat(EatInput{HP: 10, MaxHP: 10, Heal: 5, FoodCount: 0, Mode: EatSmart}) {
		t.Fatal("no food should never eat")
	}
	if !ShouldEat(EatInput{HP: 4, MaxHP: 99, Heal: 5, FoodCount: 1, Mode: EatSmart}) {
		t.Fatal("at or below the safety floor should always eat")
	}
	if !ShouldEat(EatInput{HP: 80, MaxHP: 99, Heal: 12, FoodCount: 1, Mode: EatSmart}) {
		t.Fatal("smart mode should eat when a full heal fits without overhealing")
	}
	if ShouldEat(EatInput{HP: 95, MaxHP: 99, Heal: 12, FoodCount: 1, Mode: EatSmart}) {
		t.Fatal("smart mode should hold when a full heal would overheal past max")
	}
	if !ShouldEat(EatInput{HP: 30, MaxHP: 100, Heal: 12, FoodCount: 1, Mode: EatThreshold, ThresholdFrac: 0.5}) {
		t.Fatal("threshold mode should eat below the configured fraction")
	}
	if ShouldEat(EatInput{HP: 60, MaxHP: 100, Heal: 12, FoodCount: 1, Mode: EatThreshold, ThresholdFrac: 0.5}) {
		t.Fatal("threshold mode should hold above the configured fraction")
	}
}

func TestParseEatMode(t *testing.T) {
	if ParseEatMode("Threshold") != EatThreshold {
		t.Fatal("Threshold should parse as threshold mode")
	}
	if ParseEatMode("Smart") != EatSmart || ParseEatMode("") != EatSmart || ParseEatMode("garbage") != EatSmart {
		t.Fatal("anything but threshold should parse as smart mode")
	}
}

func TestIsDeathLine(t *testing.T) {
	if !IsDeathLine("Oh dear, you are dead!") {
		t.Fatal("expected the real death line to match")
	}
	if IsDeathLine("You feel a bit dead tired.") {
		t.Fatal("unrelated chat should not match")
	}
}

func TestBaseFood(t *testing.T) {
	cases := []struct {
		name, want string
		ok         bool
	}{
		{"Shrimps", "Shrimps", true},
		{"bread", "Bread", true},
		{"2/3 cake", "Cake", true},
		{"Raw shrimps", "", false},
		{"Burnt fish", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := BaseFood(c.name)
		if got != c.want || ok != c.ok {
			t.Errorf("BaseFood(%q) = %q, %v, want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
	forms := AllFoodForms()
	if len(forms) < len(FoodOptions) {
		t.Fatalf("AllFoodForms has %d names, fewer than the %d foods", len(forms), len(FoodOptions))
	}
}
