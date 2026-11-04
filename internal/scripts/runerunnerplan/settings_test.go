package runerunnerplan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// decode is the strict decode the bot does: the defaults, then the operator's object on top with
// unknown keys refused.
func decode(t *testing.T, raw string) (Settings, error) {
	t.Helper()
	s := DefaultSettings()
	// These tests pin the derived caps (three times each rune's cost); buyAll's own tests use
	// DefaultSettings directly (buyall_test.go).
	s.BuyAll = false
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if err := CheckRawKeys(m); err != nil {
		return s, err
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, err
	}
	return s, s.Validate()
}

// Test 17: defaults from an empty object, and every refusal the design names.
func TestSettingsDefaults(t *testing.T) {
	s, err := decode(t, `{"rotate": "off"}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Mule != "mule" || r.TripCoins != DefaultTripCoins || r.CoinReserve != 10000 || r.FareCoins != 300 ||
		r.MinVisitRunes != 50 || r.HopMinGain != 200 || r.MaxHopsPerRound != 2 || r.LundailMaxDeaths != 2 ||
		r.DeliverMinRunes != 300 || r.DeliverMaxAgeMinutes != 180 || r.MuleWaitMinutes != 10 ||
		r.IdleMaxMinutes != 20 || r.TickMs != 600 || r.CheckpointDir != "data/runerunner-checkpoints" ||
		r.StopAfterRounds != 0 || r.LogDetail != "Normal" || r.MuleStand != "" || len(r.Worlds) != 0 {
		t.Errorf("defaults %+v", r.Settings)
	}
	if strings.Join(r.ShopList, ",") != "Aubury,Betty,Lundail,WizardsGuild" || strings.Join(r.RuneList, ",") != "Mind rune,Air rune,Chaos rune" {
		t.Errorf("shops %v runes %v", r.ShopList, r.RuneList)
	}
	if r.CapTable["Mind rune"] != 18 || r.CapTable["Air rune"] != 5 || r.CapTable["Chaos rune"] != 90 || r.HaveStand {
		t.Errorf("caps %v stand %v", r.CapTable, r.HaveStand)
	}
	if r.ZaffStaffs != 0 || r.ZaffStaffCap != 1600 || r.Staffs() {
		t.Errorf("staffs %d cap %d on %v", r.ZaffStaffs, r.ZaffStaffCap, r.Staffs())
	}
	if got := strings.Join(r.DeliverItems(), ","); got != "Mind rune,Air rune,Chaos rune" {
		t.Errorf("deliver items %s", got)
	}
}

func TestSettingKeysAreTheJSONTags(t *testing.T) {
	keys := SettingKeys()
	if len(keys) != reflect.TypeOf(Settings{}).NumField() {
		t.Errorf("%d keys for %d fields", len(keys), reflect.TypeOf(Settings{}).NumField())
	}
	data, _ := json.Marshal(DefaultSettings())
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("key %s is not in the encoded defaults", k)
		}
	}
}

func TestSettingsRefusals(t *testing.T) {
	for _, c := range []struct {
		name, raw, want string
	}{
		{"unknown key", `{"mules": "x"}`, "unknown RuneRunner setting"},
		{"pinWorld", `{"pinWorld": 1}`, "RuneRunner hops worlds; remove pinWorld"},
		{"empty mule", `{"mule": " "}`, "mule:"},
		{"unknown shop", `{"shops": ["Zaff"]}`, "not a rune shop"},
		{"duplicate shop", `{"shops": ["Betty", "betty"]}`, "listed twice"},
		{"no shops", `{"shops": []}`, "no shops"},
		{"unsold rune", `{"shops": ["Aubury", "Betty"], "runes": ["Mind rune", "Law rune"], "priceCaps": ["Mind rune=18", "Law rune=100"]}`, "Law rune is not sold by any enabled shop"},
		{"unknown rune", `{"runes": ["Soul rune"], "priceCaps": ["Soul rune=300"]}`, "not sold by any rune shop"},
		{"blood without the guild", `{"shops": ["Aubury", "Lundail"], "runes": ["Blood rune"], "priceCaps": ["Blood rune=150"]}`, "Blood rune is not sold by any enabled shop"},
		{"missing cap", `{"rotate": "off", "priceCaps": ["Mind rune=18", "Air rune=5"]}`, "Chaos rune has no cap"},
		{"missing air cap", `{"rotate": "off", "priceCaps": ["Mind rune=18", "Chaos rune=90"]}`, "Air rune has no cap"},
		{"zaff staffs negative", `{"zaffStaffs": -1}`, "zaffStaffs"},
		{"zaff staffs too many", `{"zaffStaffs": 11}`, "zaffStaffs: 11 is outside 0-10"},
		{"zaff cap zero", `{"zaffStaffCap": 0}`, "zaffStaffCap"},
		{"zaff as a rune shop", `{"shops": ["Aubury", "Zaff"]}`, "not a rune shop"},
		{"staff as a rune", `{"runes": ["Staff of air"], "priceCaps": ["Staff of air=1600"]}`, "not sold by any rune shop"},
		{"extra cap", `{"rotate": "off", "priceCaps": ["Mind rune=18", "Chaos rune=90", "Death rune=100"]}`, "not in runes"},
		{"cap too high", `{"rotate": "off", "priceCaps": ["Mind rune=18", "Chaos rune=1001"]}`, "1-1000"},
		{"cap zero", `{"rotate": "off", "priceCaps": ["Mind rune=0", "Chaos rune=90"]}`, "1-1000"},
		{"cap not a number", `{"rotate": "off", "priceCaps": ["Mind rune=cheap", "Chaos rune=90"]}`, "whole number"},
		{"duplicate cap", `{"rotate": "off", "priceCaps": ["Mind rune=18", "mind rune=10", "Chaos rune=90"]}`, "capped twice"},
		{"world 0", `{"worlds": [0]}`, "not a world"},
		{"duplicate world", `{"worlds": [1, 1]}`, "listed twice"},
		{"three worlds", `{"rotate": "off", "worlds": [1, 2, 3]}`, "plus one other"},
		{"trip coins low", `{"tripCoins": 999}`, "tripCoins: 999 is outside 1000-10000000"},
		{"fare low", `{"fareCoins": 10}`, "fareCoins"},
		{"hops 1", `{"maxHopsPerRound": 1}`, "maxHopsPerRound"},
		{"deaths 0", `{"lundailMaxDeaths": 0}`, "lundailMaxDeaths"},
		{"tick", `{"tickMs": 50}`, "tickMs"},
		{"deliver age", `{"deliverMaxAgeMinutes": 5}`, "deliverMaxAgeMinutes"},
		{"idle", `{"idleMaxMinutes": 0}`, "idleMaxMinutes"},
		{"stand level", `{"muleStand": "2809,3441,1"}`, "level 0"},
		{"stand garbage", `{"muleStand": "catherby"}`, "not x,z"},
		{"log detail", `{"logDetail": "Loud"}`, "logDetail"},
		{"checkpoint dir", `{"checkpointDir": ""}`, "checkpointDir"},
		{"fractional", `{"tripCoins": 1000.5}`, "cannot unmarshal"},
		{"null worlds", `{"worlds": null}`, "null"},
	} {
		if _, err := decode(t, c.raw); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error containing %q", c.name, err, c.want)
		}
	}
}

// The Wizards' Guild is a shop the setting may name (wizguild, a user request): its Magic 66 door
// is the account's current level, checked each round (EligibleShops), not a settings error.
func TestSettingsAcceptTheWizardsGuild(t *testing.T) {
	s, err := decode(t, `{"shops": ["Lundail", "wizardsguild"]}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Rotation || !r.Enabled(SiteWizardsGuild) || strings.Join(r.ShopList, ",") != "Lundail,WizardsGuild" {
		t.Fatalf("shops %v rotation %v", r.ShopList, r.Rotation)
	}
	// The default runes are every rune the two stock, blood (the guild's alone) last, each with a cap.
	if got := strings.Join(r.RuneList, ","); !strings.HasPrefix(got, "Blood rune,") || !strings.HasSuffix(got, ",Cosmic rune") {
		t.Errorf("runes %s", got)
	}
	if r.CapTable["Blood rune"] != 150 || r.CapTable["Mind rune"] != 9 {
		t.Errorf("caps %v", r.CapTable)
	}
	// The default shops include it, and the default runes then include blood with its derived cap.
	d, err := decode(t, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	dr, err := d.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !dr.Enabled(SiteWizardsGuild) || dr.CapTable["Blood rune"] != 150 {
		t.Errorf("defaults: shops %v caps %v", dr.ShopList, dr.CapTable)
	}
}

func TestSettingsAccepted(t *testing.T) {
	s, err := decode(t, `{"worlds": ["1", "2"], "muleStand": "2809,3441", "runes": ["chaos rune", "MIND RUNE"], "priceCaps": ["Chaos rune=45", "Mind rune=18"], "shops": ["lundail"], "maxHopsPerRound": 0}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Worlds) != 2 || r.Worlds[1] != 2 || r.Stand != (tile.Tile{X: 2809, Z: 3441}) || !r.HaveStand {
		t.Errorf("worlds %v stand %v", r.Worlds, r.Stand)
	}
	if strings.Join(r.RuneList, ",") != "Chaos rune,Mind rune" || r.CapTable.For("chaos RUNE") != 45 {
		t.Errorf("runes %v caps %v", r.RuneList, r.CapTable)
	}
	if !r.Enabled("Lundail") || r.Enabled("Aubury") {
		t.Errorf("shops %v", r.ShopList)
	}
	var w WorldList
	if err := json.Unmarshal([]byte(`"1, 2"`), &w); err != nil || len(w) != 2 {
		t.Errorf("a comma string decodes: %v %v", w, err)
	}
}
