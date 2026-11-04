package bowstringplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Settings are BowStringer's settings. Loading is strict (DecodeSettings): an unknown key is
// an error, as account.DecodeSettings would make it.
type Settings struct {
	// Mule is the MuleReceiver that holds the bows and strings; it must list this account in
	// its allowedPartners.
	Mule string `json:"mule"`
	// Bows is the bows to string, by name; the highest-level one Fletching allows goes first.
	Bows []string `json:"bows"`
	// KitSize is how many bows one kit order asks for (1..KitMax).
	KitSize int `json:"kitSize"`
	// MaxFruitlessKits is how many kits in a row that bring no unstrung bow retire the bow
	// (0 never).
	MaxFruitlessKits int `json:"maxFruitlessKits"`
	// OrderBackoffMinutes is the wait after a deferred or fruitless kit before asking again.
	OrderBackoffMinutes int `json:"orderBackoffMinutes"`
	// LogDetail is "Normal" or "Verbose".
	LogDetail string `json:"logDetail"`
	// PinWorld is read by the fleet (internal/fleet/worldhome.go), not by this script; it is
	// accepted so a pinned roster entry still loads.
	PinWorld json.Number `json:"pinWorld,omitempty"`
}

// Defaults.
const (
	DefaultMule                = "mule"
	DefaultKitSize             = 1000
	DefaultMaxFruitlessKits    = 3
	DefaultOrderBackoffMinutes = 5
)

// DefaultSettings are the defaults.
func DefaultSettings() Settings {
	return Settings{
		Mule:                DefaultMule,
		Bows:                DefaultBowNames(),
		KitSize:             DefaultKitSize,
		MaxFruitlessKits:    DefaultMaxFruitlessKits,
		OrderBackoffMinutes: DefaultOrderBackoffMinutes,
		LogDetail:           "Normal",
	}
}

// Verbose reports whether every step is logged.
func (s Settings) Verbose() bool { return strings.EqualFold(s.LogDetail, "Verbose") }

// DecodeSettings strictly decodes raw over the defaults. Numbers may arrive as JSON numbers or
// as numeric strings (the control panel's form sends strings); anything else of the wrong type,
// an unknown key, an unknown bow or a value out of range is an error.
func DecodeSettings(raw map[string]interface{}) (Settings, error) {
	s := DefaultSettings()
	fixed := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		switch k {
		case "kitSize", "maxFruitlessKits", "orderBackoffMinutes":
			if str, ok := v.(string); ok {
				n, err := strconv.Atoi(strings.TrimSpace(str))
				if err != nil {
					return Settings{}, fmt.Errorf("BowStringer settings: %s: %q is not a whole number", k, str)
				}
				v = n
			}
		case "bows":
			if str, ok := v.(string); ok {
				var list []string
				for _, p := range strings.Split(str, ",") {
					if p = strings.TrimSpace(p); p != "" {
						list = append(list, p)
					}
				}
				v = list
			}
		case "pinWorld":
			v = fmt.Sprint(v)
		}
		fixed[k] = v
	}
	data, err := json.Marshal(fixed)
	if err != nil {
		return Settings{}, fmt.Errorf("BowStringer settings: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("BowStringer settings: %w", err)
	}
	return s, s.Validate()
}

// Validate reports every problem with s.
func (s Settings) Validate() error {
	var problems []string
	if strings.TrimSpace(s.Mule) == "" {
		problems = append(problems, "mule is empty")
	}
	if len(s.Bows) == 0 {
		problems = append(problems, "bows is empty")
	}
	for _, b := range s.Bows {
		if _, ok := LevelFor(b); !ok {
			problems = append(problems, fmt.Sprintf("bows: %q is not a stringable bow", b))
		}
	}
	if s.KitSize < 1 || s.KitSize > KitMax {
		problems = append(problems, fmt.Sprintf("kitSize %d is outside 1..%d", s.KitSize, KitMax))
	}
	if s.MaxFruitlessKits < 0 || s.MaxFruitlessKits > 100 {
		problems = append(problems, fmt.Sprintf("maxFruitlessKits %d is outside 0..100", s.MaxFruitlessKits))
	}
	if s.OrderBackoffMinutes < 1 || s.OrderBackoffMinutes > 120 {
		problems = append(problems, fmt.Sprintf("orderBackoffMinutes %d is outside 1..120", s.OrderBackoffMinutes))
	}
	if !strings.EqualFold(s.LogDetail, "Normal") && !strings.EqualFold(s.LogDetail, "Verbose") {
		problems = append(problems, fmt.Sprintf("logDetail %q is not Normal or Verbose", s.LogDetail))
	}
	if len(problems) > 0 {
		return fmt.Errorf("BowStringer settings: %s", strings.Join(problems, "; "))
	}
	return nil
}
