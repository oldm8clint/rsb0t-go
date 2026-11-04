package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/settings"
)

func TestContextSettingsBag(t *testing.T) {
	ctx := NewContext(nil, "", nil, nil, nil, nil, nil, nil, nil, map[string]interface{}{
		"muleName": "mulecon",
		"muleAt":   float64(5000),
		"anchor":   "2726,3485,0",
	}, nil)
	bag := ctx.SettingsBag()
	if bag.Str("muleName", "") != "mulecon" || bag.Int("muleAt", 0) != 5000 {
		t.Fatalf("SettingsBag = %v", bag.Raw())
	}
	if got := bag.Tile("anchor", tile.Tile{}); got != (tile.Tile{X: 2726, Z: 3485}) {
		t.Fatalf("Tile = %+v", got)
	}
	bag.Set("muleName", "other")
	if ctx.Settings["muleName"] != "mulecon" {
		t.Fatal("the bag must be a copy of Context.Settings")
	}

	resolved := ctx.ResolveSettings(settings.Schema{
		"muleAt":  {Type: settings.TypeNumber, Default: 1000.0, Max: settings.Float(4000)},
		"bankRun": {Type: settings.TypeBoolean, Default: true},
	})
	if resolved.Int("muleAt", 0) != 4000 || !resolved.Bool("bankRun", false) {
		t.Fatalf("ResolveSettings = %v", resolved.Raw())
	}
}
