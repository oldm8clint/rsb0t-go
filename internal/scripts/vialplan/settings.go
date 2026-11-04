package vialplan

import "strings"

// Settings are VialFiller's settings. The JSON keys are the TS manifest's own settingsSchema.
type Settings struct {
	// Bank picks "Falador West" (default) or "Falador East".
	Bank string `json:"bank"`
	// BuyVials enables the periodic Jatix restock leg; default false.
	BuyVials bool `json:"buyVials"`
	// BuyEveryRuns is how many trips between restocks, 1-50.
	BuyEveryRuns int `json:"buyEveryRuns"`
	// BuyQty is vials bought per restock, 1-28.
	BuyQty int `json:"buyQty"`
	// CoinsPerTrip is the coin withdrawal target on a restock trip, 1-100000.
	CoinsPerTrip int    `json:"coinsPerTrip"`
	LogDetail    string `json:"logDetail"`
}

// Defaults matching VialFiller.ts's own settingsSchema.
const (
	DefaultBuyEveryRuns = 5
	DefaultBuyQty       = 27
	DefaultCoinsPerTrip = 1000
)

// DefaultSettings are the manifest defaults.
func DefaultSettings() Settings {
	return Settings{
		Bank:         "Falador West",
		BuyVials:     false,
		BuyEveryRuns: DefaultBuyEveryRuns,
		BuyQty:       DefaultBuyQty,
		CoinsPerTrip: DefaultCoinsPerTrip,
		LogDetail:    "Normal",
	}
}

// Normalised applies the TS setting readers' own fallbacks and bounds.
func (s Settings) Normalised() Settings {
	s.Bank = strings.TrimSpace(s.Bank)
	if s.Bank == "" {
		s.Bank = "Falador West"
	}
	if s.BuyEveryRuns < 1 {
		s.BuyEveryRuns = 1
	}
	if s.BuyEveryRuns > 50 {
		s.BuyEveryRuns = 50
	}
	if s.BuyQty < 1 {
		s.BuyQty = 1
	}
	if s.BuyQty > PackSlots {
		s.BuyQty = PackSlots
	}
	if s.CoinsPerTrip < 1 {
		s.CoinsPerTrip = 1
	}
	if s.CoinsPerTrip > 100_000 {
		s.CoinsPerTrip = 100_000
	}
	return s
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
