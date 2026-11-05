package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/wcfletchplan"
)

// twoTickOaks: absent is the level-driven rule, a bool keeps its old meaning, a string names the mode.
func TestOakModeReadsTheSetting(t *testing.T) {
	cases := []struct {
		m    map[string]interface{}
		want wcfletchplan.OakMode
	}{
		{nil, wcfletchplan.OakAuto},
		{map[string]interface{}{}, wcfletchplan.OakAuto},
		{map[string]interface{}{"twoTickOaks": true}, wcfletchplan.OakWindow},
		{map[string]interface{}{"twoTickOaks": false}, wcfletchplan.OakOff},
		{map[string]interface{}{"twoTickOaks": "auto"}, wcfletchplan.OakAuto},
		{map[string]interface{}{"twoTickOaks": "Window"}, wcfletchplan.OakWindow},
		{map[string]interface{}{"twoTickOaks": "off"}, wcfletchplan.OakOff},
		{map[string]interface{}{"twoTickOaks": "nonsense"}, wcfletchplan.OakAuto},
	}
	for _, c := range cases {
		if got := oakMode(c.m); got != c.want {
			t.Errorf("%v: %s, want %s", c.m, got, c.want)
		}
	}
}

func TestOakSettingsDefaultsAndBounds(t *testing.T) {
	var o wcfletchOaks
	o.configure(map[string]interface{}{})
	d := wcfletchplan.DefaultOakSettings()
	if o.mode != wcfletchplan.OakAuto || o.set.Stint != 120*time.Minute || o.set.Rest != 360*time.Minute || o.set.Slots != 1 ||
		o.set.MaxLevel != 0 || o.set.Margin != d.Margin || o.set.Realised != d.Realised {
		t.Fatalf("defaults: %+v", o.set)
	}
	o.configure(map[string]interface{}{
		"twoTickOakStintMinutes": 60.0, "twoTickOakRestMinutes": 30.0, "twoTickOakSlots": 2.0, "twoTickOakMaxLevel": 70.0,
		"twoTickOakMargin": 1.5, "twoTickOakRealised": 0.5,
	})
	if o.set.Stint != time.Hour || o.set.Rest != 30*time.Minute || o.set.Slots != 2 || o.set.MaxLevel != 70 || o.set.Margin != 1.5 || o.set.Realised != 0.5 {
		t.Fatalf("overrides: %+v", o.set)
	}
	o.configure(map[string]interface{}{"twoTickOakSlots": 0.0, "twoTickOakRealised": 3.0, "twoTickOakStintMinutes": 0.0})
	if o.set.Slots != 1 || o.set.Realised != d.Realised || o.set.Stint != d.Stint {
		t.Fatalf("bad values fall back: %+v", o.set)
	}
	// The window mode needs a sane window; a bad one is off.
	o.configure(map[string]interface{}{"twoTickOaks": true, "twoTickOakMinutes": 2000.0})
	if o.mode != wcfletchplan.OakOff {
		t.Errorf("a window longer than its day: %s", o.mode)
	}
}
