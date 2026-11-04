package toadcollectplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Settings are the ToadCollector's settings. Loading is strict (DecodeSettings): an unknown key is an error.
type Settings struct {
	// Mule is the MuleReceiver account the legs go to; blank never hands over (a full pack is banked).
	Mule string `json:"mule"`
	// HandOffAt is the legs held that make a hand-off due (1..PackSlots).
	HandOffAt int `json:"handOffAt"`
	// HandOffMinutes hands over whatever is held once this long has passed since the last hand-off.
	HandOffMinutes int `json:"handOffMinutes"`
	// LogDetail is "Normal" or "Verbose".
	LogDetail string `json:"logDetail"`
	// PinWorld is read by the fleet (internal/fleet/worldhome.go), not by this script.
	PinWorld json.Number `json:"pinWorld,omitempty"`
}

var knownKeys = map[string]bool{"mule": true, "handOffAt": true, "handOffMinutes": true, "logDetail": true, "pinWorld": true}

// DefaultSettings are the defaults.
func DefaultSettings() Settings {
	return Settings{Mule: DefaultMule, HandOffAt: DefaultHandOffAt, HandOffMinutes: DefaultHandOffMinutes, LogDetail: "Normal"}
}

// Verbose reports whether every step is logged.
func (s Settings) Verbose() bool { return strings.EqualFold(s.LogDetail, "Verbose") }

// DecodeSettings strictly decodes raw over the defaults. Numbers may arrive as JSON numbers or numeric strings.
func DecodeSettings(raw map[string]interface{}) (Settings, error) {
	s := DefaultSettings()
	fixed := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		if !knownKeys[k] {
			return Settings{}, fmt.Errorf("ToadCollector settings: unknown key %q", k)
		}
		switch k {
		case "handOffAt", "handOffMinutes":
			if str, ok := v.(string); ok {
				n, err := strconv.Atoi(strings.TrimSpace(str))
				if err != nil {
					return Settings{}, fmt.Errorf("ToadCollector settings: %s: %q is not a whole number", k, str)
				}
				v = n
			}
		case "pinWorld":
			v = fmt.Sprint(v)
		}
		fixed[k] = v
	}
	data, err := json.Marshal(fixed)
	if err != nil {
		return Settings{}, fmt.Errorf("ToadCollector settings: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("ToadCollector settings: %w", err)
	}
	return s, s.Validate()
}

// Validate reports every problem with s.
func (s Settings) Validate() error {
	var problems []string
	if s.HandOffAt < 1 || s.HandOffAt > PackSlots {
		problems = append(problems, fmt.Sprintf("handOffAt %d is outside 1..%d", s.HandOffAt, PackSlots))
	}
	if s.HandOffMinutes < 1 || s.HandOffMinutes > 24*60 {
		problems = append(problems, fmt.Sprintf("handOffMinutes %d is outside 1..%d", s.HandOffMinutes, 24*60))
	}
	if !strings.EqualFold(s.LogDetail, "Normal") && !strings.EqualFold(s.LogDetail, "Verbose") {
		problems = append(problems, fmt.Sprintf("logDetail %q is not Normal or Verbose", s.LogDetail))
	}
	if len(problems) > 0 {
		return fmt.Errorf("ToadCollector settings: %s", strings.Join(problems, "; "))
	}
	return nil
}
