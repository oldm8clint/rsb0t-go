package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/retaliateplan"
)

func TestRetaliateChopperIsRegistered(t *testing.T) {
	if _, ok := New("RetaliateChopper"); !ok {
		t.Fatal("RetaliateChopper is not registered")
	}
}

func TestRetaliateChopperSettingsDefaultsAreThePlanDefaults(t *testing.T) {
	got := retaliateChopperSettings(map[string]interface{}{})
	if got != retaliateplan.DefaultSettings() {
		t.Fatalf("empty settings should give the defaults: %+v vs %+v", got, retaliateplan.DefaultSettings())
	}
	if got.Site != "falador-oaks" || got.Clear != "drop" || got.Full != "drop" || got.Fallback != "knife" ||
		got.PhaseTries != 6 || got.MinGain != 0.7 || got.Lead != 1 || got.FallbackMins != 10 || got.ReportEvery != 300 || got.StopHPPercent != 70 {
		t.Fatalf("documented defaults drifted: %+v", got)
	}
	if !strings.Contains(got.Aggro, "confuse") || strings.Contains(got.Aggro, "wind strike") {
		t.Fatalf("the default aggro list must hold the no-damage spells only: %q", got.Aggro)
	}
}

func TestRetaliateChopperSettingsOverrides(t *testing.T) {
	got := retaliateChopperSettings(map[string]interface{}{
		"stand": "3004,3312", "clear": "button", "full": "bank", "aggro": "curse,wind strike", "phaseTries": 3.0,
		"minGain": 0.5, "lead": 2.0, "stopHPPercent": 90.0, "fallback": "plain", "fallbackMins": 30.0,
		"reportEvery": 120.0, "logDetail": "Verbose",
	})
	if tl, ok := got.StandTile(); !ok || tl.X != 3004 || tl.Z != 3312 {
		t.Fatalf("stand %+v", got)
	}
	if got.Clear != "button" || got.Full != "bank" || got.PhaseTries != 3 || got.MinGain != 0.5 || got.Lead != 2 ||
		got.StopHPPercent != 90 || got.Fallback != "plain" || got.FallbackMins != 30 || got.ReportEvery != 120 || !got.Verbose() {
		t.Fatalf("overrides not applied: %+v", got)
	}
	if l := got.AggroList(); len(l) != 2 || l[0] != "curse" || l[1] != "wind strike" {
		t.Fatalf("aggro %v", l)
	}
}

func TestRetaliateChopperRefusesToRunBesideChopManip(t *testing.T) {
	b := &retaliateChopperBot{}
	ctx := &Context{Settings: map[string]interface{}{"chopManip": true}, stop: make(chan struct{})}
	var logged []string
	ctx.Log = func(s string) { logged = append(logged, s) }
	if err := b.OnStart(ctx); err != nil {
		t.Fatal(err)
	}
	if b.stopped == "" || len(logged) == 0 || !strings.Contains(logged[0], "chopManip") {
		t.Fatalf("stopped %q logged %v", b.stopped, logged)
	}
	if b.run != nil {
		t.Fatal("a runner was built beside chopManip")
	}
}
