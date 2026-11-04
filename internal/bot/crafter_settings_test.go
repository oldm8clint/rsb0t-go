package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestCrafterSettingsDefaultsMatchTheTSManifest(t *testing.T) {
	s := crafterSettings(map[string]interface{}{})
	if s.Mule != donorplan.DefaultMule || s.MuleAt != 5000 || s.MuleKeep != "Coins, Shears" {
		t.Errorf("mule defaults = %q/%d/%q", s.Mule, s.MuleAt, s.MuleKeep)
	}
	if s.FlaxSource != crafterplan.PickFlax || s.Fare != 60 || s.DropAt != 27 || s.RestBelowHP != 40 || s.SkirtCost != 3 {
		t.Errorf("defaults = %+v", s)
	}
	if !s.Donor().Enabled() {
		t.Error("the default mule hands over")
	}
}

func TestCrafterSettingsKeepAnExplicitBlankMule(t *testing.T) {
	s := crafterSettings(map[string]interface{}{"mule": " ", "muleKeep": ""})
	if s.Mule != "" || s.Donor().Enabled() {
		t.Errorf("blank mule = %q, enabled %v; want never to hand over", s.Mule, s.Donor().Enabled())
	}
	if s.MuleKeep != "" {
		t.Errorf("explicit blank muleKeep = %q", s.MuleKeep)
	}
}

func TestCrafterSettingsReadTheRoster(t *testing.T) {
	s := crafterSettings(map[string]interface{}{
		"flaxSource": crafterplan.BankFlax, "mule": "Mulecon", "muleAt": 250.0,
		"penTile": "3200,3265", "wheelTile": []interface{}{3209.0, 3213.0, 1.0}, "dropAt": 0.0,
	})
	if s.FlaxSource != crafterplan.BankFlax || s.Mule != "Mulecon" || s.MuleAt != 250 {
		t.Errorf("read = %+v", s)
	}
	if s.PenTile != (tile.Tile{X: 3200, Z: 3265}) || s.WheelTile != (tile.Tile{X: 3209, Z: 3213, Level: 1}) {
		t.Errorf("tiles = %+v, %+v", s.PenTile, s.WheelTile)
	}
	if s.DropAt != 1 {
		t.Errorf("dropAt 0 should clamp to 1, got %d", s.DropAt)
	}
	if !crafterplan.KeepsBankFlax(s.FlaxSource) {
		t.Error("bank-first flax is held back from the mule")
	}
}

func TestCrafterSettingsReadTheMuleFlaxKeys(t *testing.T) {
	d := crafterSettings(map[string]interface{}{})
	if d.FlaxKitSize != crafterplan.DefaultFlaxKitSize || d.FlaxKitBackoffMinutes != crafterplan.DefaultFlaxKitBackoffMinutes {
		t.Errorf("defaults = %d/%d", d.FlaxKitSize, d.FlaxKitBackoffMinutes)
	}
	b := &crafterBot{settings: d}
	if b.mulePostMatters() {
		t.Error("the default source never involves the mule's post")
	}
	s := crafterSettings(map[string]interface{}{
		"flaxSource": crafterplan.MuleFlax, "mule": "mule", "muleAt": 500.0,
		"flaxKitSize": 2000.0, "flaxKitBackoffMinutes": 20.0,
	})
	if s.FlaxSource != crafterplan.MuleFlax || s.Mule != "mule" || s.MuleAt != 500 || s.FlaxKitSize != 2000 || s.FlaxKitBackoffMinutes != 20 {
		t.Errorf("read = %+v", s)
	}
	b.settings = s
	if !b.mulePostMatters() {
		t.Error("flax from a named mule involves its post")
	}
	b.settings.Mule = ""
	if b.mulePostMatters() {
		t.Error("no mule named: no post")
	}
	if got := crafterSettings(map[string]interface{}{"flaxKitSize": 3.0}).FlaxKitSize; got != crafterplan.MinFlaxKitSize {
		t.Errorf("a tiny kit clamps to %d, got %d", crafterplan.MinFlaxKitSize, got)
	}
}
