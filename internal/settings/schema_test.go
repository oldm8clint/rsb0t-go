package settings

import (
	"encoding/json"
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

var testSchema = Schema{
	"runAuto":      {Type: TypeBoolean, Default: true},
	"runEnergyMin": {Type: TypeNumber, Default: 20.0, Min: Float(0), Max: Float(100)},
	"lampSkill":    {Type: TypeString, Default: "strength", Options: []string{"attack", "strength", "Magic"}},
	"note":         {Type: TypeString, Default: ""},
	"foods":        {Type: TypeList, Default: []string{"Trout"}, Options: []string{"Trout", "Salmon", "Lobster"}},
	"skip":         {Type: TypeList, Default: []string{}},
	"anchor":       {Type: TypeTile, Default: tile.Tile{X: 3222, Z: 3218}},
}

func TestSchemaResolveCoercesAndDefaults(t *testing.T) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(`{
		"runAuto": "yes",
		"runEnergyMin": 250,
		"lampSkill": "MAGIC",
		"note": "  hi  ",
		"foods": ["salmon", "cake", "LOBSTER"],
		"skip": "a, ,b",
		"anchor": "3200, 3201, 1",
		"extra": 5
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	b := testSchema.Resolve(raw)
	if !b.Bool("runAuto", false) {
		t.Error("boolean string not parsed")
	}
	if got := b.Num("runEnergyMin", -1); got != 100 {
		t.Errorf("number not clamped: %v", got)
	}
	if got := b.Str("lampSkill", ""); got != "Magic" {
		t.Errorf("option not canonicalised: %q", got)
	}
	if got := b.Str("note", ""); got != "hi" {
		t.Errorf("string not trimmed: %q", got)
	}
	if got := b.Raw()["foods"]; !reflect.DeepEqual(got, []string{"Salmon", "Lobster"}) {
		t.Errorf("list options = %v", got)
	}
	if got := b.Raw()["skip"]; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("comma list = %v", got)
	}
	if got := b.Tile("anchor", tile.Tile{}); got != (tile.Tile{X: 3200, Z: 3201, Level: 1}) {
		t.Errorf("tile = %+v", got)
	}
	if got := b.Num("extra", 0); got != 5 {
		t.Errorf("undeclared key not kept: %v", got)
	}

	d := testSchema.Resolve(map[string]interface{}{"lampSkill": "fishing", "runEnergyMin": "abc", "anchor": "nope"})
	if d.Str("lampSkill", "") != "strength" || d.Num("runEnergyMin", -1) != 20 || d.Tile("anchor", tile.Tile{}) != (tile.Tile{X: 3222, Z: 3218}) {
		t.Errorf("unusable values did not fall back: %v", d.Raw())
	}
	if !d.Bool("runAuto", false) {
		t.Error("missing boolean did not take its default")
	}
}

func TestBagTileForms(t *testing.T) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(`{"obj": {"x": 1, "z": 2, "level": 3}, "arr": [4, 5], "bad": [1], "str": "6,7"}`), &raw); err != nil {
		t.Fatal(err)
	}
	b := NewBagFrom(raw)
	fb := tile.Tile{X: -1}
	cases := map[string]tile.Tile{
		"obj":     {X: 1, Z: 2, Level: 3},
		"arr":     {X: 4, Z: 5},
		"str":     {X: 6, Z: 7},
		"bad":     fb,
		"missing": fb,
	}
	for key, want := range cases {
		if got := b.Tile(key, fb); got != want {
			t.Errorf("Tile(%q) = %+v, want %+v", key, got, want)
		}
	}
	raw["obj"] = "changed"
	if b.Tile("obj", fb) != (tile.Tile{X: 1, Z: 2, Level: 3}) {
		t.Error("NewBagFrom must copy the map")
	}
	if _, ok := ParseTile("1,2,3,4"); ok {
		t.Error("ParseTile accepted four parts")
	}
}
