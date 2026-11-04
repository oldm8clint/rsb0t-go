package antipoisonmakeplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Settings are AntipoisonMaker's settings. Loading is strict: an unknown key is an error.
type Settings struct {
	// Mule is the MuleReceiver that holds the Marrentill and takes the potions; it must list this
	// account in its allowedPartners.
	Mule string `json:"mule"`
	// HandOffAt is how many Antipoison(3) to bank before handing them to the mule.
	HandOffAt int `json:"handOffAt"`
	// KitSize is how many Marrentill one mule ask requests (1..1000, the mule's per-order cap).
	KitSize int `json:"kitSize"`
	// ShopVials is how many vials of water a shop run buys (1..27).
	ShopVials int `json:"shopVials"`
	// HuntHPFloor is the hitpoints at or below which a unicorn hunt breaks off and returns to the bank.
	HuntHPFloor int `json:"huntHPFloor"`
	// MaxFruitlessAsks is how many mule asks in a row that bring no Marrentill retire the mule as
	// a source (0 never).
	MaxFruitlessAsks int `json:"maxFruitlessAsks"`
	// BackoffMinutes is the wait after a failed ask, shop run or hand-off before trying it again.
	BackoffMinutes int `json:"backoffMinutes"`
	// LogDetail is "Normal" or "Verbose".
	LogDetail string `json:"logDetail"`
	// PinWorld is read by the fleet (internal/fleet/worldhome.go), not by this script.
	PinWorld json.Number `json:"pinWorld,omitempty"`
}

// Defaults.
const (
	DefaultMule             = "mule1"
	DefaultHandOffAt        = 27
	DefaultKitSize          = 300
	DefaultShopVials        = 27
	DefaultHuntHPFloor      = 8
	DefaultMaxFruitlessAsks = 3
	DefaultBackoffMinutes   = 5
)

// DefaultSettings are the defaults.
func DefaultSettings() Settings {
	return Settings{
		Mule:             DefaultMule,
		HandOffAt:        DefaultHandOffAt,
		KitSize:          DefaultKitSize,
		ShopVials:        DefaultShopVials,
		HuntHPFloor:      DefaultHuntHPFloor,
		MaxFruitlessAsks: DefaultMaxFruitlessAsks,
		BackoffMinutes:   DefaultBackoffMinutes,
		LogDetail:        "Normal",
	}
}

// Verbose reports whether every step is logged.
func (s Settings) Verbose() bool { return strings.EqualFold(s.LogDetail, "Verbose") }

// Validate checks the ranges.
func (s Settings) Validate() error {
	switch {
	case strings.TrimSpace(s.Mule) == "":
		return fmt.Errorf("AntipoisonMaker settings: mule is empty (the potions have nowhere to go)")
	case s.HandOffAt < 1 || s.HandOffAt > PackSlots:
		return fmt.Errorf("AntipoisonMaker settings: handOffAt %d must be 1..%d", s.HandOffAt, PackSlots)
	case s.KitSize < 1 || s.KitSize > 1000:
		return fmt.Errorf("AntipoisonMaker settings: kitSize %d must be 1..1000", s.KitSize)
	case s.ShopVials < 1 || s.ShopVials > PackSlots-1:
		return fmt.Errorf("AntipoisonMaker settings: shopVials %d must be 1..%d", s.ShopVials, PackSlots-1)
	case s.HuntHPFloor < 0:
		return fmt.Errorf("AntipoisonMaker settings: huntHPFloor %d must be 0 or more", s.HuntHPFloor)
	case s.MaxFruitlessAsks < 0:
		return fmt.Errorf("AntipoisonMaker settings: maxFruitlessAsks %d must be 0 or more", s.MaxFruitlessAsks)
	case s.BackoffMinutes < 1 || s.BackoffMinutes > 120:
		return fmt.Errorf("AntipoisonMaker settings: backoffMinutes %d must be 1..120", s.BackoffMinutes)
	}
	if !strings.EqualFold(s.LogDetail, "Normal") && !strings.EqualFold(s.LogDetail, "Verbose") {
		return fmt.Errorf("AntipoisonMaker settings: logDetail %q must be Normal or Verbose", s.LogDetail)
	}
	return nil
}

// DecodeSettings strictly decodes raw over the defaults. Numbers may arrive as JSON numbers or as
// numeric strings (the control panel's form sends strings).
func DecodeSettings(raw map[string]interface{}) (Settings, error) {
	s := DefaultSettings()
	fixed := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		switch k {
		case "handOffAt", "kitSize", "shopVials", "huntHPFloor", "maxFruitlessAsks", "backoffMinutes":
			if str, ok := v.(string); ok {
				n, err := strconv.Atoi(strings.TrimSpace(str))
				if err != nil {
					return Settings{}, fmt.Errorf("AntipoisonMaker settings: %s: %q is not a whole number", k, str)
				}
				v = n
			}
		}
		fixed[k] = v
	}
	body, err := json.Marshal(fixed)
	if err != nil {
		return Settings{}, fmt.Errorf("AntipoisonMaker settings: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("AntipoisonMaker settings: %w", err)
	}
	return s, s.Validate()
}
