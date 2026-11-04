package mulereceiverplan

// Translated from rsb0t-scripts test/muleSettings.test.ts (this port's own edge cases, following
// the crafterplan/fisherplan/barbarianplan test style).

import (
	"reflect"
	"testing"
)

func TestNudgedRuneStepSizes(t *testing.T) {
	cases := []struct {
		value, dir, want int
	}{
		{0, 1, 1},     // below 20: step 1
		{19, 1, 20},   // still below 20 going up
		{20, 1, 25},   // 20..99: step 5
		{99, 1, 104},  // still below 100 going up: step 5
		{100, 1, 125}, // 100+: step 25
		{20, -1, 19},  // going down from 20: at = value-1 = 19, still step 1
		{100, -1, 95}, // going down from 100: at = value-1 = 99, step 5
	}
	for _, c := range cases {
		if got := Nudged(NumKitLaw, c.value, c.dir); got != c.want {
			t.Errorf("Nudged(kitLaw, %d, %d) = %d, want %d", c.value, c.dir, got, c.want)
		}
	}
}

func TestNudgedClampsToBounds(t *testing.T) {
	if got := Nudged(NumKitLaw, 999, 1); got != 1000 {
		t.Errorf("Nudged should clamp at the max, got %d", got)
	}
	if got := Nudged(NumKitLaw, 0, -1); got != 0 {
		t.Errorf("Nudged should clamp at the min, got %d", got)
	}
	if got := Nudged(NumWalkRadius, 5, -1); got != 5 {
		t.Errorf("Nudged(walkRadius) should not go below its min 5, got %d", got)
	}
	if got := Nudged(NumWalkRadius, 300, 1); got != 300 {
		t.Errorf("Nudged(walkRadius) should not go above its max 300, got %d", got)
	}
}

func TestNudgedAlchCastsStep(t *testing.T) {
	if got := Nudged(NumAlchCasts, 5, 1); got != 6 {
		t.Errorf("Nudged(alchCasts, 5, 1) = %d, want 6 (below 10: step 1)", got)
	}
	if got := Nudged(NumAlchCasts, 10, 1); got != 20 {
		t.Errorf("Nudged(alchCasts, 10, 1) = %d, want 20 (at/above 10: step 10)", got)
	}
	if got := Nudged(NumAlchCasts, 1, -1); got != 1 {
		t.Errorf("Nudged(alchCasts) should clamp at its min 1, got %d", got)
	}
}

func TestWithNamesFiltersAndDedupes(t *testing.T) {
	// "Alice" is kept, "ALICE" is a duplicate (case-insensitive), the blank part is skipped, the
	// over-length name is dropped, and "Carol!" fails the name pattern (no "!" allowed).
	got := WithNames(nil, "Alice, bob , ALICE, , this-name-is-far-too-long-for-a-partner-name, Carol!", ListPartners)
	want := []string{"Alice", "bob"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WithNames = %v, want %v", got, want)
	}
}

func TestWithNamesCollapsesInternalWhitespace(t *testing.T) {
	got := WithNames(nil, "Sir   Gawain", ListPartners)
	want := []string{"Sir Gawain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WithNames = %v, want %v", got, want)
	}
}

func TestWithNamesRespectsPerListMax(t *testing.T) {
	// 31 characters: under the item list's max of 40, over the partner list's max of 12.
	longButOKForItems := "an-item-name-over-partner-limit"
	if len(longButOKForItems) > nameMax[ListAlchItems] || len(longButOKForItems) <= nameMax[ListPartners] {
		t.Fatalf("test fixture length %d does not sit between the partner (%d) and item (%d) maxes", len(longButOKForItems), nameMax[ListPartners], nameMax[ListAlchItems])
	}
	got := WithNames(nil, longButOKForItems, ListAlchItems)
	if len(got) != 1 {
		t.Fatalf("WithNames(alchItems) should accept a name under its own longer max, got %v", got)
	}
	// The same string is too long for a partner name (max 12).
	if got := WithNames(nil, longButOKForItems, ListPartners); len(got) != 0 {
		t.Fatalf("WithNames(partners) should reject a name over its shorter max, got %v", got)
	}
}

func TestWithoutName(t *testing.T) {
	got := WithoutName([]string{"Alice", "Bob"}, "alice")
	want := []string{"Bob"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WithoutName = %v, want %v (case-insensitive)", got, want)
	}
}

func TestAdoptedAddsNewName(t *testing.T) {
	next, ok := Adopted([]string{"Alice"}, "Bob", "MyMule")
	if !ok || !reflect.DeepEqual(next, []string{"Alice", "Bob"}) {
		t.Fatalf("Adopted = %v ok=%v, want [Alice Bob] true", next, ok)
	}
}

func TestAdoptedRejectsBlankSelfAndDuplicate(t *testing.T) {
	if _, ok := Adopted([]string{"Alice"}, "", "MyMule"); ok {
		t.Error("Adopted should reject a blank name")
	}
	if _, ok := Adopted([]string{"Alice"}, "MyMule", "MyMule"); ok {
		t.Error("Adopted should reject the mule's own name")
	}
	if _, ok := Adopted([]string{"Alice"}, "alice", "MyMule"); ok {
		t.Error("Adopted should reject a name already on the list (case-insensitive)")
	}
	if _, ok := Adopted([]string{"Alice"}, "!!!", "MyMule"); ok {
		t.Error("Adopted should reject a name WithNames would not take")
	}
}

func TestNormalisedClampsAndDedupes(t *testing.T) {
	cfg := MuleConfig{
		Partners:    []string{"Alice", "alice", "Bob"},
		Post:        "   ",
		PostTile:    "  100,200  ",
		WalkRadius:  9999,
		MaxLegTiles: -5,
		KitLaw:      -1,
		KitAir:      50000,
		AlchCasts:   0,
		AlchItems:   []string{"Yew logs", "yew logs"},
	}
	got := Normalised(cfg)
	if !reflect.DeepEqual(got.Partners, []string{"Alice", "Bob"}) {
		t.Errorf("Normalised should dedupe partners case-insensitively, got %v", got.Partners)
	}
	if got.Post != MuleDefaults().Post {
		t.Errorf("Normalised should fall back to the default post when blank, got %q", got.Post)
	}
	if got.PostTile != "100,200" {
		t.Errorf("Normalised should trim postTile, got %q", got.PostTile)
	}
	if got.WalkRadius != 300 {
		t.Errorf("Normalised should clamp walkRadius to its max, got %d", got.WalkRadius)
	}
	if got.MaxLegTiles != 10 {
		t.Errorf("Normalised should clamp maxLegTiles to its min, got %d", got.MaxLegTiles)
	}
	if got.KitLaw != 0 {
		t.Errorf("Normalised should clamp a negative kitLaw to 0, got %d", got.KitLaw)
	}
	if got.KitAir != 1000 {
		t.Errorf("Normalised should clamp kitAir to its max, got %d", got.KitAir)
	}
	if got.AlchCasts != 1 {
		t.Errorf("Normalised should clamp alchCasts to its min 1, got %d", got.AlchCasts)
	}
	if !reflect.DeepEqual(got.AlchItems, []string{"Yew logs"}) {
		t.Errorf("Normalised should dedupe alchItems, got %v", got.AlchItems)
	}
}

func TestConfigFromFormCarriesOverDifferingValues(t *testing.T) {
	raw := map[string]interface{}{
		"partners":   "Alice, Bob",
		"pmOrders":   false, // differs from default true
		"walkRadius": float64(75),
		"post":       "Varrock West", // differs from default "Seers"
		"alchCasts":  float64(50),    // matches default, should not be carried
		"unknownKey": "ignored",
	}
	out := ConfigFromForm(raw)
	if !reflect.DeepEqual(out.Config.Partners, []string{"Alice", "Bob"}) {
		t.Errorf("Config.Partners = %v", out.Config.Partners)
	}
	if out.Config.PmOrders != false {
		t.Errorf("Config.PmOrders = %v, want false", out.Config.PmOrders)
	}
	if out.Config.WalkRadius != 75 {
		t.Errorf("Config.WalkRadius = %d, want 75", out.Config.WalkRadius)
	}
	if out.Config.Post != "Varrock West" {
		t.Errorf("Config.Post = %q, want Varrock West", out.Config.Post)
	}
	// Everything else should be default.
	if out.Config.DeliverOrders != MuleDefaults().DeliverOrders {
		t.Errorf("Config.DeliverOrders should default, got %v", out.Config.DeliverOrders)
	}

	wantCarried := map[string]bool{"partners": true, "pmOrders": true, "walkRadius": true, "post": true}
	for _, k := range out.Carried {
		if !wantCarried[k] {
			t.Errorf("unexpected carried key %q", k)
		}
		delete(wantCarried, k)
	}
	if len(wantCarried) != 0 {
		t.Errorf("expected keys were not marked carried: %v", wantCarried)
	}
}

func TestConfigFromFormWrongTypeFallsBackToDefault(t *testing.T) {
	raw := map[string]interface{}{
		"pmOrders":   "not a bool",
		"walkRadius": "not a number",
	}
	out := ConfigFromForm(raw)
	def := MuleDefaults()
	if out.Config.PmOrders != def.PmOrders {
		t.Errorf("a wrong-typed pmOrders should fall back to the default")
	}
	if out.Config.WalkRadius != def.WalkRadius {
		t.Errorf("a wrong-typed walkRadius should fall back to the default")
	}
	if len(out.Carried) != 0 {
		t.Errorf("nothing should be marked carried when every value is wrong-typed, got %v", out.Carried)
	}
}

func TestConfigFromFormEmptyListStringNotCarried(t *testing.T) {
	raw := map[string]interface{}{"partners": "   "}
	out := ConfigFromForm(raw)
	if out.Config.Partners != nil {
		t.Errorf("a blank partners string should leave Partners empty, got %v", out.Config.Partners)
	}
	if len(out.Carried) != 0 {
		t.Errorf("a blank list string should not be marked carried, got %v", out.Carried)
	}
}

func TestDescribeSetting(t *testing.T) {
	cases := []struct {
		value interface{}
		want  string
	}{
		{[]string{}, "none"},
		{[]string{"Alice", "Bob"}, "Alice, Bob"},
		{true, "on"},
		{false, "off"},
		{"", "blank"},
		{"Seers", "Seers"},
		{5, "5"},
	}
	for _, c := range cases {
		if got := DescribeSetting(c.value); got != c.want {
			t.Errorf("DescribeSetting(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestMuleDefaultsMatchSource(t *testing.T) {
	d := MuleDefaults()
	if d.Post != "Seers" || d.WalkRadius != 50 || d.MaxLegTiles != 150 {
		t.Fatalf("MuleDefaults() = %+v, does not match muleSettings.ts's MULE_DEFAULTS", d)
	}
	if d.AlchSpell != AutoSpellLabel {
		t.Fatalf("MuleDefaults().AlchSpell = %q, want the AUTO_SPELL label %q", d.AlchSpell, AutoSpellLabel)
	}
	if got := d.RuneCounts(); got != (RuneCounts{Law: 10, Air: 40, Fire: 5, Water: 5, Earth: 5}) {
		t.Fatalf("MuleDefaults().RuneCounts() = %+v", got)
	}
}
