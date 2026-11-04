package essenceplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
)

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()
	if s.Pickaxe != BestAvailable {
		t.Errorf("DefaultSettings().Pickaxe = %q, want %q", s.Pickaxe, BestAvailable)
	}
	if s.Mule != "" {
		t.Errorf("DefaultSettings().Mule = %q, want \"\"", s.Mule)
	}
	if s.MuleAt != MuleEssenceAt {
		t.Errorf("DefaultSettings().MuleAt = %d, want %d", s.MuleAt, MuleEssenceAt)
	}
	if s.LogDetail != "Normal" {
		t.Errorf("DefaultSettings().LogDetail = %q, want Normal", s.LogDetail)
	}
}

func TestSettingsNormalised(t *testing.T) {
	if got := (Settings{Pickaxe: "  "}).Normalised(); got.Pickaxe != BestAvailable {
		t.Errorf("Normalised blank pickaxe = %q, want %q", got.Pickaxe, BestAvailable)
	}
	if got := (Settings{Pickaxe: " Rune "}).Normalised(); got.Pickaxe != "Rune" {
		t.Errorf("Normalised trims pickaxe = %q, want Rune", got.Pickaxe)
	}
	if got := (Settings{MuleAt: -5}).Normalised(); got.MuleAt != 0 {
		t.Errorf("Normalised negative MuleAt = %d, want 0", got.MuleAt)
	}
	if got := (Settings{MuleAt: donorplan.MaxMuleAt + 1}).Normalised(); got.MuleAt != donorplan.MaxMuleAt {
		t.Errorf("Normalised over-max MuleAt = %d, want %d", got.MuleAt, donorplan.MaxMuleAt)
	}
	if got := (Settings{MuleAt: 20_000}).Normalised(); got.MuleAt != 20_000 {
		t.Errorf("Normalised in-range MuleAt = %d, want unchanged 20000", got.MuleAt)
	}
}

func TestSettingsVerbose(t *testing.T) {
	if (Settings{LogDetail: "Normal"}).Verbose() {
		t.Error("Verbose(Normal) = true, want false")
	}
	if !(Settings{LogDetail: "Verbose"}).Verbose() {
		t.Error("Verbose(Verbose) = false, want true")
	}
	if !(Settings{LogDetail: " verbose "}).Verbose() {
		t.Error("Verbose(padded/lowercase) = false, want true")
	}
	if (Settings{}).Verbose() {
		t.Error("Verbose(blank) = true, want false")
	}
}

func TestSettingsDonor(t *testing.T) {
	s := Settings{Mule: "mulecon, other", MuleAt: 12_000, MuleKeep: "Coins, Ashes"}
	d := s.Donor()
	if d.Mule != "mulecon" {
		t.Errorf("Donor().Mule = %q, want mulecon (first name only)", d.Mule)
	}
	if d.MuleAt != 12_000 {
		t.Errorf("Donor().MuleAt = %d, want 12000", d.MuleAt)
	}
	if len(d.Keep) != 2 || d.Keep[0] != "Coins" || d.Keep[1] != "Ashes" {
		t.Errorf("Donor().Keep = %v, want [Coins Ashes]", d.Keep)
	}
	if !d.Enabled() {
		t.Error("Donor().Enabled() = false, want true")
	}
	if (Settings{}).Donor().Enabled() {
		t.Error("Donor().Enabled() for blank settings = true, want false")
	}
}

// ---- the supply settings ----

func TestSupplyDefaults(t *testing.T) {
	s := DefaultSettings()
	if s.Gateway != GatewayAubury || s.MuleStopAt != MuleEssenceCap || s.MuleItems != MuleItemsAll || s.Batch != DefaultBatch || s.MuleAtTile != "" || s.BuyPickaxe || s.MineTickManip {
		t.Errorf("defaults %+v", s)
	}
	if s.SupplyLoop() {
		t.Error("the default gateway keeps the original loop")
	}
	// DefaultSettings is a fixed point of Normalised: the bot compares the two.
	if got := s.Normalised(); got != s {
		t.Errorf("Normalised(defaults) = %+v, want %+v", got, s)
	}
}

func TestDecodeSettingsDefaultsAndGuildDefaults(t *testing.T) {
	for _, raw := range []map[string]interface{}{{}, nil} {
		got, err := DecodeSettings(raw)
		if err != nil || got != DefaultSettings() {
			t.Errorf("DecodeSettings(%v) = %+v, %v; want the defaults", raw, got, err)
		}
	}
	// A Guild miner with nothing else set: 5,000, one note stack of essence.
	got, err := DecodeSettings(map[string]interface{}{"gateway": "Guild", "mule": "m"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Gateway != GatewayGuild || got.MuleAt != GuildMuleAt || got.MuleItems != MuleItemsEssence || got.MuleStopAt != 150000 || !got.SupplyLoop() {
		t.Errorf("guild defaults %+v", got)
	}
	// muleAt given stays.
	got, err = DecodeSettings(map[string]interface{}{"gateway": "guild", "muleAt": 2000.0, "muleItems": "essence", "muleStopAt": "80000", "batch": 540.0})
	if err != nil || got.MuleAt != 2000 || got.MuleItems != MuleItemsEssence || got.MuleStopAt != 80000 || got.Batch != 540 {
		t.Errorf("%+v %v", got, err)
	}
	// The whole bank is the Aubury script's hand-over: a Guild miner asking for it is refused, loudly.
	if _, err = DecodeSettings(map[string]interface{}{"gateway": "guild", "muleItems": "all"}); err == nil || !strings.Contains(err.Error(), "note stack") {
		t.Errorf("guild with muleItems all: %v", err)
	}
	if got, err = DecodeSettings(map[string]interface{}{"gateway": "aubury", "muleItems": "essence"}); err != nil || got.MuleItems != MuleItemsEssence || got.SupplyLoop() {
		t.Errorf("aubury may hand over just the essence: %+v %v", got, err)
	}
	// Aubury keeps the original defaults.
	got, err = DecodeSettings(map[string]interface{}{"gateway": "aubury", "mule": "m"})
	if err != nil || got.MuleAt != MuleEssenceAt || got.MuleItems != MuleItemsAll {
		t.Errorf("%+v %v", got, err)
	}
	// The meet tile.
	got, err = DecodeSettings(map[string]interface{}{"muleAtTile": "2809,3441"})
	if tl, ok := got.Meet(); err != nil || !ok || tl.X != 2809 || tl.Z != 3441 || tl.Level != 0 {
		t.Errorf("%+v %v", got, err)
	}
}

// Loading is strict: a typo in a setting that is a stop must not leave the miner running without it.
func TestDecodeSettingsIsStrict(t *testing.T) {
	for name, raw := range map[string]map[string]interface{}{
		"unknown key":           {"muleStop": 5},
		"wrong case":            {"MuleAt": 5},
		"gateway":               {"gateway": "sedridor"},
		"muleItems":             {"muleItems": "everything"},
		"muleStopAt negative":   {"muleStopAt": -1},
		"muleStopAt too big":    {"muleStopAt": 5_000_000},
		"muleStopAt text":       {"muleStopAt": "lots"},
		"muleStopAt fraction":   {"muleStopAt": 12.5},
		"muleStopAt null":       {"muleStopAt": nil},
		"batch under a load":    {"batch": 5},
		"batch over":            {"batch": 1_000_000},
		"mule not text":         {"mule": 7},
		"pickaxe not text":      {"pickaxe": true},
		"muleAtTile unreadable": {"muleAtTile": "catherby"},
		"muleAtTile one number": {"muleAtTile": "2809"},
		"muleAtTile zero":       {"muleAtTile": "0,0"},
		"muleAt text":           {"muleAt": "soon"},
		"mineTickManip maybe":   {"mineTickManip": "maybe"},
		"buyPickaxe maybe":      {"buyPickaxe": "maybe"},
		"buyPickaxe a number":   {"buyPickaxe": 1},
	} {
		if got, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s: accepted %v as %+v", name, raw, got)
		}
	}
	// One error names every problem.
	_, err := DecodeSettings(map[string]interface{}{"muleStop": 1, "gateway": "x"})
	if err == nil || !strings.Contains(err.Error(), "muleStop is not a setting") || !strings.Contains(err.Error(), "gateway") {
		t.Errorf("%v", err)
	}
	// The fleet's own keys pass.
	if _, err := DecodeSettings(map[string]interface{}{"homeWorld": 1.0, "pinWorld": 2.0, "logDetail": "Verbose"}); err != nil {
		t.Errorf("fleet keys: %v", err)
	}
	// muleAt out of range is clamped as it always was.
	got, err := DecodeSettings(map[string]interface{}{"muleAt": 99_999_999.0})
	if err != nil || got.MuleAt != donorplan.MaxMuleAt {
		t.Errorf("%+v %v", got, err)
	}
	got, err = DecodeSettings(map[string]interface{}{"muleAt": -4.0})
	if err != nil || got.MuleAt != 0 {
		t.Errorf("%+v %v", got, err)
	}
}

// Every key the script reads is known and sorted, and each decodes.
func TestKnownKeysDecode(t *testing.T) {
	keys := KnownKeys()
	if len(keys) != 12 {
		t.Fatalf("keys %v", keys)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Errorf("not sorted: %v", keys)
		}
	}
	sample := map[string]interface{}{
		KeyPickaxe: "Rune", KeyMule: "m", KeyMuleAt: 1000, KeyMuleKeep: "Coins", KeyLogDetail: "Verbose", KeyGateway: "cromperty",
		KeyMuleStopAt: 1, KeyMuleItems: "essence", KeyMuleAtTile: "2725,3491,0", KeyBatch: 54, KeyBuyPickaxe: "true", KeyMineTickManip: "true",
	}
	if len(sample) != len(keys) {
		t.Fatal("the sample misses a key")
	}
	got, err := DecodeSettings(sample)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pickaxe != "Rune" || got.Mule != "m" || got.MuleAt != 1000 || got.MuleKeep != "Coins" || !got.Verbose() || got.Gateway != "cromperty" ||
		got.MuleStopAt != 1 || got.MuleItems != "essence" || got.MuleAtTile != "2725,3491,0" || got.Batch != 54 || !got.BuyPickaxe || !got.MineTickManip {
		t.Errorf("%+v", got)
	}
}

func TestParseTile(t *testing.T) {
	for raw, want := range map[string]bool{"2809,3441": true, " 2809 , 3441 , 1 ": true, "2809,3441,4": false, "2809": false, "": false, "a,b": false, "-1,5": false, "1,2,3,4": false} {
		if _, ok := ParseTile(raw); ok != want {
			t.Errorf("ParseTile(%q) ok=%v, want %v", raw, ok, want)
		}
	}
}

func TestNormalisedSupplyBounds(t *testing.T) {
	got := Settings{Gateway: "  GUILD ", MuleItems: " ", Batch: 3, MuleStopAt: -2}.Normalised()
	if got.Gateway != GatewayGuild || got.MuleItems != MuleItemsEssence || got.Batch != MinBatch || got.MuleStopAt != 0 {
		t.Errorf("%+v", got)
	}
	got = Settings{Batch: 1 << 30, MuleStopAt: 1 << 30}.Normalised()
	if got.Gateway != GatewayAubury || got.MuleItems != MuleItemsAll || got.Batch != MaxBatch || got.MuleStopAt != MaxMuleStopAt {
		t.Errorf("%+v", got)
	}
}
