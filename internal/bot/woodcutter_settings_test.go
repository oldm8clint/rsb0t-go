package bot

import (
	"reflect"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

func TestWoodcutterSettingsDefaults(t *testing.T) {
	for _, values := range []map[string]interface{}{{}, nil} {
		got := woodcutterSettings(values)
		want := woodcutplan.DefaultSettings()
		// reflect.DeepEqual, not !=: Settings now carries AxePrices, a map, so the struct is
		// no longer comparable. The assertion is unchanged.
		if !reflect.DeepEqual(got, want) {
			t.Errorf("woodcutterSettings(%v) = %+v, want the manifest defaults %+v", values, got, want)
		}
		// Unlike thiever's blank-by-default mule, the woodcutter manifest ships a real
		// default partner (Mulecon) — a fresh account with no axe anywhere is otherwise
		// permanently PhaseStuck. See woodcutplan.DefaultSettings' own doc comment.
		if !got.Donor().Enabled() {
			t.Errorf("woodcutterSettings(%v): the manifest default mule should enable hand-overs", values)
		}
	}
}

func TestWoodcutterSettingsOverrides(t *testing.T) {
	got := woodcutterSettings(map[string]interface{}{
		"target": "Magic", "fare": 100.0, "pickpocketTarget": "Woman",
		"restBelowHp": 55.0, "guideNpc": "Guide", "skipOption": "Yep",
		"mule": "otheracct", "muleAt": 2000.0, "muleKeep": "Coins, Shears", "logDetail": "Verbose",
	})
	if got.Target != "Magic" || got.Fare != 100 || got.PickpocketTarget != "Woman" {
		t.Errorf("woodcutterSettings(overrides) = %+v", got)
	}
	if got.RestBelowHP != 55 || got.GuideNPC != "Guide" || got.SkipOption != "Yep" {
		t.Errorf("woodcutterSettings(overrides) = %+v", got)
	}
	if got.Mule != "otheracct" || got.MuleAt != 2000 || got.MuleKeep != "Coins, Shears" || got.LogDetail != "Verbose" {
		t.Errorf("woodcutterSettings(overrides) = %+v", got)
	}
}

func TestWoodcutterSettingsBlankMuleDisablesHandoff(t *testing.T) {
	got := woodcutterSettings(map[string]interface{}{"mule": ""})
	if got.Donor().Enabled() {
		t.Error("an explicit blank mule must disable hand-overs even though the manifest default is non-blank")
	}
}

func TestWoodcutterSettingsMuleAtClampedToDonorMax(t *testing.T) {
	got := woodcutterSettings(map[string]interface{}{"muleAt": 99_999_999.0})
	if got.MuleAt <= 0 || got.MuleAt > 1_000_000 {
		t.Errorf("muleAt = %d, want clamped within donorplan.MaxMuleAt", got.MuleAt)
	}
}

// TestWoodcutterAxeBuyingIsOffUnlessFullyConfigured pins the default that matters most here.
// Buying at the Seers market means trading coins with a PERSON, so the failure mode of a
// half-configured run is handing coins to the wrong account — and the safe default is to do
// nothing at all. Both a price and a budget are required, and a run that sets neither behaves
// exactly as it did before this was added.
func TestWoodcutterAxeBuyingIsOffUnlessFullyConfigured(t *testing.T) {
	if woodcutterSettings(nil).BuysAxes() {
		t.Error("the defaults must never buy an axe")
	}
	cases := []struct {
		name string
		m    map[string]interface{}
		want bool
	}{
		{"price only", map[string]interface{}{"axePrice": float64(600)}, false},
		{"budget only", map[string]interface{}{"axeBudget": float64(1000)}, false},
		{"both set", map[string]interface{}{"axePrice": float64(600), "axeBudget": float64(1000)}, true},
		{"price exactly the budget", map[string]interface{}{"axePrice": float64(600), "axeBudget": float64(600)}, true},
		// A price OVER the budget can never buy anything. It must fail closed (buy nothing)
		// rather than open (quietly raise the ceiling), which is why Normalised zeroes the
		// price rather than the budget.
		{"price over the budget", map[string]interface{}{"axePrice": float64(2000), "axeBudget": float64(1000)}, false},
		{"negative price", map[string]interface{}{"axePrice": float64(-5), "axeBudget": float64(1000)}, false},
	}
	for _, c := range cases {
		if got := woodcutterSettings(c.m).BuysAxes(); got != c.want {
			t.Errorf("%s: BuysAxes = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWoodcutterAxeSettingsRead(t *testing.T) {
	s := woodcutterSettings(map[string]interface{}{
		"axeWanted":    "Mithril axe",
		"axePrice":     float64(600),
		"axeBudget":    float64(5000),
		"marketSeller": "  seersstall  ",
	})
	if s.AxeWanted != "Mithril axe" || s.AxePrice != 600 || s.AxeBudget != 5000 {
		t.Errorf("axe settings = %+v", s)
	}
	if s.MarketSeller != "seersstall" {
		t.Errorf("MarketSeller = %q, want it trimmed", s.MarketSeller)
	}
	// An axe name that is not an axe falls back to the default rather than being asked for.
	if got := woodcutterSettings(map[string]interface{}{"axeWanted": "Rune scimitar"}).AxeWanted; got != "Rune axe" {
		t.Errorf("a non-axe axeWanted should fall back, got %q", got)
	}
	// The stands default to somewhere real rather than 0,0, which would path into the sea.
	d := woodcutterSettings(nil)
	if d.MarketStand == (tile.Tile{}) || d.BobStand == (tile.Tile{}) {
		t.Errorf("stands must default to real tiles, got market %+v bob %+v", d.MarketStand, d.BobStand)
	}
}

// TestWoodcutterStartCampSetting pins the Draynor start camp's own switch, both ways.
//
// It defaults ON, which is a real behaviour change for anyone who was running this script, so
// the default is asserted here rather than left implicit: the reason is the measured one — the
// Seers normal-tree camp holds 12 trees and feeds 2.0 accounts, so a cohort started together at
// Woodcutting 1 never levels out of it.
func TestWoodcutterStartCampSetting(t *testing.T) {
	if !woodcutterSettings(nil).StartCamp {
		t.Error("the Draynor start camp should default ON")
	}
	if woodcutterSettings(map[string]interface{}{"startCamp": false}).StartCamp {
		t.Error(`"startCamp": false must turn the camp off`)
	}
	if !woodcutterSettings(map[string]interface{}{"startCamp": true}).StartCamp {
		t.Error(`"startCamp": true must turn the camp on`)
	}
}

// TestWoodcutterStoreKeepFollowsTheStartCamp pins the keep-list switch on the BOT, not just in
// the plan package, because the bot is where the two callers are (the snapshot's own Sellable
// count and StoreTrip itself) and they must not disagree: a Sellable computed from one list and
// a sale made against the other would sell the axe while the phase machine believed it had not.
func TestWoodcutterStoreKeepFollowsTheStartCamp(t *testing.T) {
	b := &woodcutterBot{settings: woodcutplan.Settings{StartCamp: true}}
	if !hasFold(b.storeKeep(), "Bronze axe") {
		t.Error("with the camp on, the store keep list must hold every axe back")
	}
	if !b.startCampOn() {
		t.Error("startCampOn should be true with the setting on and nothing latched off")
	}
	// The latch: once the camp has been given up for this run, BOTH answers flip together.
	b.startCampOff = true
	if b.startCampOn() {
		t.Error("startCampOn must be false once the camp has been given up")
	}
	if hasFold(b.storeKeep(), "Bronze axe") {
		t.Error("with the camp given up, the bronze axe is sellable again")
	}

	off := &woodcutterBot{settings: woodcutplan.Settings{StartCamp: false}}
	if hasFold(off.storeKeep(), "Bronze axe") {
		t.Error("with the camp off, the store keep list is the ordinary one")
	}
}

func hasFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

// TestWoodcutterDefaultMuleStandMatchesTheMuleReceiver pins the duplication woodcutplan's own
// DefaultMuleStand doc comment promises is checked. woodcutplan follows this project's
// "duplicated, not shared" convention for small script-specific constants, which is fine only as
// long as something notices when the original moves.
func TestWoodcutterDefaultMuleStandMatchesTheMuleReceiver(t *testing.T) {
	if woodcutplan.DefaultMuleStand != mulereceiverplan.SeersStand {
		t.Errorf("woodcutplan.DefaultMuleStand = %+v, mulereceiverplan.SeersStand = %+v — the duplicate has drifted",
			woodcutplan.DefaultMuleStand, mulereceiverplan.SeersStand)
	}
}

// TestWoodcutterReadsMuleAtTile pins the setting that was MISSING, and its absence was a
// deployment blocker rather than a rough edge: internal/bot/woodcutter.go hardcoded the Seers
// stand in the one place it mattered, while fisher.go, lootchestfisher.go and advancedfighter.go
// all already read `muleAtTile`. An operator could therefore name the fleet's real mule with
// "mule" and still have this script walk to a tile that mule has never stood on, with nothing in
// the log to say so.
func TestWoodcutterReadsMuleAtTile(t *testing.T) {
	if got := woodcutterSettings(nil).MuleStand; got != woodcutplan.DefaultMuleStand {
		t.Errorf("default muleAtTile = %+v, want %+v", got, woodcutplan.DefaultMuleStand)
	}
	// The Catherby bank stand, which is where the fleet's own mule actually posts.
	got := woodcutterSettings(map[string]interface{}{
		"muleAtTile": map[string]interface{}{"x": 2809.0, "z": 3441.0},
	}).MuleStand
	want := tile.Tile{X: 2809, Z: 3441}
	if got != want {
		t.Errorf("muleAtTile override = %+v, want %+v", got, want)
	}
}
