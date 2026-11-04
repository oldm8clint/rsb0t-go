package flaxplan

import "strings"

// Settings are FlaxPicker's settings. The JSON keys mirror the TS manifest's own settingsSchema
// (flaxName, pickOp, bankBooth) plus the two logging/verbosity conveniences every other ported
// script's own Settings carries.
type Settings struct {
	// FlaxName is the loc/item name picked; blank falls back to Flax.
	FlaxName string `json:"flaxName"`
	// PickOp is the loc option used to pick it; blank falls back to Pick.
	PickOp string `json:"pickOp"`
	// BankBooth is the bank loc's display name; blank falls back to "Bank booth".
	BankBooth string `json:"bankBooth"`
	LogDetail string `json:"logDetail"`
}

// DefaultSettings are the manifest defaults.
func DefaultSettings() Settings {
	return Settings{
		FlaxName:  Flax,
		PickOp:    PickOp,
		BankBooth: BoothName,
		LogDetail: "Normal",
	}
}

// Normalised applies the TS setting readers' own blank-falls-back-to-default rule.
func (s Settings) Normalised() Settings {
	s.FlaxName = strings.TrimSpace(s.FlaxName)
	if s.FlaxName == "" {
		s.FlaxName = Flax
	}
	s.PickOp = strings.TrimSpace(s.PickOp)
	if s.PickOp == "" {
		s.PickOp = PickOp
	}
	s.BankBooth = strings.TrimSpace(s.BankBooth)
	if s.BankBooth == "" {
		s.BankBooth = BoothName
	}
	return s
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
