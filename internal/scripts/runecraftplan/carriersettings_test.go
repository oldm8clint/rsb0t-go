package runecraftplan

import (
	"strings"
	"testing"
)

func TestCarrierDefaults(t *testing.T) {
	s, err := DecodeCarrierSettings(nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Crafter != "" || s.Mule != "mule" || s.Source != SourceShelf || s.KitEssence != 3000 || s.KitCoins != 10000 || s.MinHP != 25 || s.ShelfFloor != DefaultShelfFloor || s.KeepNatures != 200 || !s.WalkRub || s.WalkAlch {
		t.Fatalf("defaults: %+v", s)
	}
	if len(s.SwarmSettings) != 0 {
		t.Fatalf("swarm settings: %v", s.SwarmSettings)
	}
}

func TestCarrierSettingsDecode(t *testing.T) {
	s, err := DecodeCarrierSettings(map[string]interface{}{
		"runecraftCrafter": " Main Crafter ", "mule": "mule12", "carrierSource": "Shelf", "carrierKitEssence": "1500", "carrierKitCoins": 5000.0,
		"carrierMinHP": 30, "carrierShelfFloor": "50", "walkRub": "false", "walkAlch": false, "homeWorld": 1.0, "pinWorld": 1.0,
		"swarmSettings": `{"mule": "", "walkAlch": true}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Crafter != "Main Crafter" || s.Mule != "mule12" || s.KitEssence != 1500 || s.KitCoins != 5000 || s.MinHP != 30 || s.ShelfFloor != 50 || s.WalkRub || s.WalkAlch {
		t.Fatalf("decoded %+v", s)
	}
	// The carrier's mule is the embedded swarm's unless it names one.
	if m := s.SwarmSettingsMap(); m["mule"] != "mule12" || m["walkAlch"] != true {
		t.Fatalf("swarm settings %v", m)
	}
	s.SwarmSettings["mule"] = "other"
	if m := s.SwarmSettingsMap(); m["mule"] != "other" {
		t.Fatalf("swarm settings %v", m)
	}
	// A map works too, and is copied.
	in := map[string]interface{}{"k": 1.0}
	s2, err := DecodeCarrierSettings(map[string]interface{}{"swarmSettings": in})
	if err != nil {
		t.Fatal(err)
	}
	in["k"] = 2.0
	if s2.SwarmSettings["k"] != 1.0 {
		t.Fatal("swarmSettings must be copied")
	}
}

// TestCarrierSettingsStrict: every mistake is refused with the whole list, and walkAlch true is refused with the reason.
func TestCarrierSettingsStrict(t *testing.T) {
	for name, tc := range map[string]struct {
		raw map[string]interface{}
		in  string
	}{
		"an unknown key":               {map[string]interface{}{"carrierMinHp": 25}, "carrierMinHp is not a setting"},
		"a number as text":             {map[string]interface{}{"runecraftCrafter": 5}, "not text"},
		"hitpoints under the floor":    {map[string]interface{}{"carrierMinHP": 9}, "outside 10..99"},
		"hitpoints not whole":          {map[string]interface{}{"carrierMinHP": 25.5}, "not a whole number"},
		"a kit under four trips":       {map[string]interface{}{"carrierKitEssence": 99}, "outside"},
		"a kit over the cap":           {map[string]interface{}{"carrierKitEssence": 20001}, "outside"},
		"coins under the fares":        {map[string]interface{}{"carrierKitCoins": 100}, "outside"},
		"a floor over the cap":         {map[string]interface{}{"carrierShelfFloor": 100}, "outside"},
		"a negative floor":             {map[string]interface{}{"carrierShelfFloor": -1}, "outside"},
		"walk-alch on":                 {map[string]interface{}{"walkAlch": true}, "walk-alch is on; refusing to start"},
		"walk-alch on as text":         {map[string]interface{}{"walkAlch": "true"}, "refusing to start"},
		"a flag that is not one":       {map[string]interface{}{"walkRub": "maybe"}, "not true or false"},
		"the bank source is unbuilt":   {map[string]interface{}{"carrierSource": "bank"}, "not built yet"},
		"a source that is none":        {map[string]interface{}{"carrierSource": "cart"}, "not \"shelf\" or \"bank\""},
		"swarm settings not an object": {map[string]interface{}{"swarmSettings": 5.0}, "not an object"},
		"swarm settings bad json":      {map[string]interface{}{"swarmSettings": "{nope"}, "not a JSON object"},
	} {
		_, err := DecodeCarrierSettings(tc.raw)
		if err == nil || !strings.Contains(err.Error(), tc.in) {
			t.Errorf("%s: %v, want an error containing %q", name, err, tc.in)
		}
	}
	// Every problem is listed, not just the first.
	_, err := DecodeCarrierSettings(map[string]interface{}{"carrierMinHP": 1, "bogus": 1, "walkAlch": true})
	if err == nil || !strings.Contains(err.Error(), "carrierMinHP") || !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "walk-alch") {
		t.Fatalf("%v", err)
	}
}

func TestCarrierKnownKeysAreSorted(t *testing.T) {
	keys := CarrierKnownKeys()
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("%v", keys)
		}
	}
	if len(keys) != 11 {
		t.Fatalf("%d keys: %v", len(keys), keys)
	}
}

// carrierKeepNatures (P4): the share of a courier haul a carrier keeps in its own bank, 0 to hand everything to the mule, strictly bounded.
func TestCarrierKeepNaturesSetting(t *testing.T) {
	if s, err := DecodeCarrierSettings(map[string]interface{}{"carrierKeepNatures": "350"}); err != nil || s.KeepNatures != 350 {
		t.Fatalf("%+v %v", s, err)
	}
	if s, err := DecodeCarrierSettings(map[string]interface{}{"carrierKeepNatures": 0}); err != nil || s.KeepNatures != 0 {
		t.Fatalf("0 hands everything over: %+v %v", s, err)
	}
	for _, v := range []interface{}{-1, MaxKeepNatures + 1, "lots", 1.5} {
		if _, err := DecodeCarrierSettings(map[string]interface{}{"carrierKeepNatures": v}); err == nil {
			t.Errorf("accepted %v", v)
		}
	}
}
