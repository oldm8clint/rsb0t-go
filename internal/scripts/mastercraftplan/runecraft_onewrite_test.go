package mastercraftplan

import "testing"

// craftRuneOneWrite (CR-2) is ON unless the settings say otherwise, takes true and false, and refuses anything else.
func TestOneWriteIsOnByDefaultAndSwitchesOff(t *testing.T) {
	if !DefaultRuneSettings().OneWrite {
		t.Fatal("the one-write accept is off by default")
	}
	s, err := DecodeSettings(map[string]interface{}{KeyRuneOneWrite: false})
	if err != nil || s.Rune.OneWrite {
		t.Fatalf("false: %v, %+v", err, s.Rune.OneWrite)
	}
	s, err = DecodeSettings(map[string]interface{}{KeyRuneOneWrite: true})
	if err != nil || !s.Rune.OneWrite {
		t.Fatalf("true: %v, %+v", err, s.Rune.OneWrite)
	}
	if s, err = DecodeSettings(map[string]interface{}{}); err != nil || !s.Rune.OneWrite {
		t.Fatalf("absent: %v, %+v", err, s.Rune.OneWrite)
	}
	if _, err = DecodeSettings(map[string]interface{}{KeyRuneOneWrite: "sometimes"}); err == nil {
		t.Fatal("a word was accepted for a switch")
	}
	// It is independent of the direct ops: each is its own switch.
	s, err = DecodeSettings(map[string]interface{}{KeyRuneOneWrite: false})
	if err != nil || !s.Rune.DirectOps {
		t.Fatalf("switching the one-write accept off touched the direct ops: %v, %+v", err, s.Rune.DirectOps)
	}
}
