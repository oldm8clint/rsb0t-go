package bot

import (
	"reflect"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestFisherTileSettingsKeepTheRunningDefaults(t *testing.T) {
	// A roster without muleAtTile keeps trading at the Seers stand, the Go port's behaviour the
	// running fleet relies on (ProgressiveFisher.ts itself defaults to the Catherby bank).
	if got := fisherTileSetting(map[string]interface{}{}, "muleAtTile", mulereceiverplan.SeersStand); got != mulereceiverplan.SeersStand {
		t.Errorf("missing muleAtTile = %+v, want the Seers stand %+v", got, mulereceiverplan.SeersStand)
	}
	if got := fisherTileSetting(nil, "pickpocketTile", freshstart.PickpocketSpot); got != freshstart.PickpocketSpot {
		t.Errorf("nil settings pickpocketTile = %+v, want %+v", got, freshstart.PickpocketSpot)
	}
	want := tile.Tile{X: 2809, Z: 3441}
	for _, v := range []interface{}{"2809,3441", map[string]interface{}{"x": 2809.0, "z": 3441.0}, []interface{}{2809.0, 3441.0}} {
		if got := fisherTileSetting(map[string]interface{}{"muleAtTile": v}, "muleAtTile", mulereceiverplan.SeersStand); got != want {
			t.Errorf("muleAtTile %v = %+v, want %+v", v, got, want)
		}
	}
	if got := fisherTileSetting(map[string]interface{}{"muleAtTile": "nonsense"}, "muleAtTile", mulereceiverplan.SeersStand); got != mulereceiverplan.SeersStand {
		t.Errorf("an unreadable muleAtTile = %+v, want the default", got)
	}
}

func TestHaulAfterReserveLeavesOutReservedCoins(t *testing.T) {
	cases := []struct{ haul, coins, want int }{
		{1, 1, 0},   // only the reserved coins are left: done
		{3, 500, 2}, // exactly the reserve stays
		{3, 501, 3}, // 1 coin above the reserve still crosses
		{2, 0, 2},   // no coins in the pack
		{0, 10, 0},  // nothing counted: never below zero
	}
	for _, c := range cases {
		if got := haulAfterReserve(c.haul, c.coins, 500); got != c.want {
			t.Errorf("haulAfterReserve(%d, %d, 500) = %d, want %d", c.haul, c.coins, got, c.want)
		}
	}
}

func intPtr(v int) *int { return &v }
func TestFisherMuleSettingKeepsAnExplicitBlank(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]interface{}
		want     string
		enabled  bool
	}{
		{"missing key uses the default mule", map[string]interface{}{}, defaultFisherMule, true},
		{"nil settings use the default mule", nil, defaultFisherMule, true},
		{"explicit blank never hands over", map[string]interface{}{"mule": ""}, "", false},
		{"whitespace is blank too", map[string]interface{}{"mule": "  "}, "", false},
		{"a named mule is kept", map[string]interface{}{"mule": "mulecon"}, "mulecon", true},
		{"a non-string value uses the default mule", map[string]interface{}{"mule": 3.0}, defaultFisherMule, true},
	}
	for _, c := range cases {
		got := fisherMuleSetting(c.settings)
		if got != c.want {
			t.Errorf("%s: fisherMuleSetting = %q, want %q", c.name, got, c.want)
		}
		if on := donorplan.NewSettings(got, fisherplan.MuleFishAt, fisherplan.DefaultMuleKeep).Enabled(); on != c.enabled {
			t.Errorf("%s: hand-overs enabled = %v, want %v", c.name, on, c.enabled)
		}
	}
}

func TestFisherSurvivalSettingsDefaultOff(t *testing.T) {
	got := readFisherSurvival(map[string]interface{}{})
	if want := fisherplan.DefaultSurvival(); !reflect.DeepEqual(got, want) {
		t.Fatalf("missing keys = %+v, want the safe defaults %+v", got, want)
	}
	if readFisherSurvival(nil).Enabled {
		t.Fatal("nil settings must not eat unless eatFood is explicitly turned on")
	}
}

func TestFisherSurvivalSettingsOverridesAndClamps(t *testing.T) {
	got := readFisherSurvival(map[string]interface{}{
		"eatFood": false, "food": "Lobster, raw tuna, tuna", "foodCount": 99.0, "eatBelowHp": 70.0, "fleeBelowHp": 90.0, "maxHit": 6.0,
	})
	if got.Enabled {
		t.Error("eatFood false must turn survival off")
	}
	if !reflect.DeepEqual(got.Foods, []string{"Lobster", "Tuna"}) {
		t.Errorf("foods = %v, want the known ones", got.Foods)
	}
	if got.FoodCount != fisherplan.MaxFoodCount || got.EatBelow != 0.7 || got.FleeBelow != 0.7 || got.MaxHit != 6 {
		t.Errorf("clamps = %+v", got)
	}
}

func TestFisherBankKeepKeepsCarriedFood(t *testing.T) {
	surv := fisherplan.DefaultSurvival()
	surv.Enabled = true // exercising the enabled decision logic regardless of the app-level default
	pack := []freshstart.PackItem{{Name: "Shrimps", Count: 2}, {Name: "Raw shrimps", Count: 10}}
	keep := fisherBankKeep([]string{"Small fishing net"}, surv, pack)
	if !reflect.DeepEqual(keep, []string{"Small fishing net", "Shrimps"}) {
		t.Errorf("keep = %v, want the tool and the food, never the raw fish", keep)
	}
	surv.Enabled = false
	if got := fisherBankKeep([]string{"Small fishing net"}, surv, pack); !reflect.DeepEqual(got, []string{"Small fishing net"}) {
		t.Errorf("survival off keeps = %v", got)
	}
}

func TestFisherHandoffKeepHoldsFoodBack(t *testing.T) {
	surv := fisherplan.DefaultSurvival()
	surv.Enabled = true // exercising the enabled decision logic regardless of the app-level default
	// Above the death float: the surplus coins go to the mule and the trade screen holds the
	// donor's travel money back.
	keep := fisherHandoffKeep([]string{"Coins", "Feather"}, surv, fisherplan.DeathFloatCoins+1)
	has := func(names []string, name string) bool {
		for _, k := range names {
			if strings.EqualFold(k, name) {
				return true
			}
		}
		return false
	}
	if has(keep, "Coins") {
		t.Error("above the death float, surplus coins are split at the trade screen, not kept whole")
	}
	if !has(keep, "Feather") || !has(keep, "Lobster") || !has(keep, "Shrimps") {
		t.Errorf("keep = %v, want muleKeep plus the food", keep)
	}
	rows := fisherplan.MuleRows([]donorplan.ItemRow{{Name: "Raw tuna", Count: 500}, {Name: "Lobster", Count: 20}, {Name: "Shrimps", Count: 5}}, keep)
	if len(rows) != 1 || rows[0].Name != "Raw tuna" {
		t.Errorf("mule rows = %+v, want only the fish", rows)
	}
}

// The case the death float exists for: an account whose banked coins are at or below the
// float hands over NO coins, so the money that funds its recovery after a death stays in the
// bank rather than going to the mule. Measured on the live fleet before this rule existed,
// 189 of 269 accounts held zero banked coins and 46 more held under 500 -- 87% could not have
// funded their own recovery.
func TestFisherHandoffKeepsTheDeathFloatBanked(t *testing.T) {
	surv := fisherplan.DefaultSurvival()
	has := func(names []string, name string) bool {
		for _, k := range names {
			if strings.EqualFold(k, name) {
				return true
			}
		}
		return false
	}
	for _, banked := range []int{0, 1, fisherplan.DeathFloatCoins - 1, fisherplan.DeathFloatCoins} {
		keep := fisherHandoffKeep([]string{"Coins"}, surv, banked)
		if !has(keep, "Coins") {
			t.Errorf("banked %d coins (float %d): coins were handed over; they must stay banked as death-recovery money", banked, fisherplan.DeathFloatCoins)
		}
		// And prove it through the rule the hand-off actually uses, not just the list: a coin
		// row must not appear among the rows drawn for the mule.
		rows := fisherplan.MuleRows([]donorplan.ItemRow{{Name: "Coins", Count: banked}, {Name: "Raw tuna", Count: 500}}, keep)
		for _, r := range rows {
			if strings.EqualFold(r.Name, "Coins") {
				t.Errorf("banked %d coins: MuleRows still drew the coin row %+v", banked, r)
			}
		}
	}
	// One coin above the float and the surplus is handed over again.
	keep := fisherHandoffKeep([]string{"Coins"}, surv, fisherplan.DeathFloatCoins+1)
	if has(keep, "Coins") {
		t.Errorf("above the float, coins must be handed over; keep = %v", keep)
	}
}
