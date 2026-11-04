package fighterfood

import "testing"

// The index must answer exactly what the old scan over FoodOptions answered.
func TestBaseFoodIndexMatchesTheScan(t *testing.T) {
	scan := func(name string) (string, bool) {
		target := key(name)
		if target == "" {
			return "", false
		}
		for _, food := range FoodOptions {
			if IsFoodItem(target, food) {
				return food, true
			}
		}
		return "", false
	}
	names := append(AllFoodForms(), "Raw shrimps", "", "  CAKE ", "2/3 Cake", "Slice of cake", "Bones", "Chocolate slice", "Half a meat pie")
	for _, n := range names {
		gb, gok := BaseFood(n)
		wb, wok := scan(n)
		if gb != wb || gok != wok {
			t.Errorf("BaseFood(%q) = %q,%v; scan = %q,%v", n, gb, gok, wb, wok)
		}
	}
}
