package mastercraftplan

import "testing"

// craftRuneDirectOps (CR-1) is ON unless the settings say otherwise, takes true and false, and refuses anything else.
func TestDirectOpsAreOnByDefaultAndSwitchOff(t *testing.T) {
	if !DefaultRuneSettings().DirectOps {
		t.Fatal("the direct ops are off by default")
	}
	s, err := DecodeSettings(map[string]interface{}{KeyRuneDirectOps: false})
	if err != nil || s.Rune.DirectOps {
		t.Fatalf("false: %v, %+v", err, s.Rune.DirectOps)
	}
	s, err = DecodeSettings(map[string]interface{}{KeyRuneDirectOps: true})
	if err != nil || !s.Rune.DirectOps {
		t.Fatalf("true: %v, %+v", err, s.Rune.DirectOps)
	}
	if s, err = DecodeSettings(map[string]interface{}{}); err != nil || !s.Rune.DirectOps {
		t.Fatalf("absent: %v, %+v", err, s.Rune.DirectOps)
	}
	if _, err = DecodeSettings(map[string]interface{}{KeyRuneDirectOps: "sometimes"}); err == nil {
		t.Fatal("a word was accepted for a switch")
	}
}
