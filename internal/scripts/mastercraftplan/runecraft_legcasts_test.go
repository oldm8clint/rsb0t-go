package mastercraftplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/runecraftplan"
)

// craftRuneLegCasts (CX-1) is "low2" unless the settings say otherwise, takes the four modes (any case), and refuses anything else.
func TestLegCastsAreLow2ByDefaultAndTakeTheFourModes(t *testing.T) {
	if got := DefaultRuneSettings().LegCasts; got != runecraftplan.LegCastsLow2 {
		t.Fatalf("the default is %q", got)
	}
	for in, want := range map[string]runecraftplan.LegCastMode{
		"off": runecraftplan.LegCastsOff, "low2": runecraftplan.LegCastsLow2, "high1": runecraftplan.LegCastsHigh1,
		"HIGH2": runecraftplan.LegCastsHigh2, " Low2 ": runecraftplan.LegCastsLow2,
	} {
		s, err := DecodeSettings(map[string]interface{}{KeyRuneLegCasts: in})
		if err != nil || s.Rune.LegCasts != want {
			t.Errorf("%q: %v, %q", in, err, s.Rune.LegCasts)
		}
	}
	if s, err := DecodeSettings(map[string]interface{}{}); err != nil || s.Rune.LegCasts != runecraftplan.LegCastsLow2 {
		t.Fatalf("absent: %v, %q", err, s.Rune.LegCasts)
	}
	for _, bad := range []interface{}{"high", "both", true, 2, ""} {
		_, err := DecodeSettings(map[string]interface{}{KeyRuneLegCasts: bad})
		if err == nil || !strings.Contains(err.Error(), KeyRuneLegCasts) {
			t.Errorf("%v was accepted (%v)", bad, err)
		}
	}
	// It is its own switch: the bows held through the trades and the direct ops are not touched by it.
	s, err := DecodeSettings(map[string]interface{}{KeyRuneLegCasts: "off"})
	if err != nil || !s.Rune.DirectOps || s.Rune.AlchHold {
		t.Fatalf("switching the leg casts off touched another setting: %v, %+v", err, s.Rune)
	}
}
