package herbplan

import "strings"

// Settings are HerbCleaner's settings. The JSON keys mirror the TS manifest's own settingsSchema
// (herbs) plus the bank-booth and verbosity conveniences every other ported script carries.
type Settings struct {
	// Herbs restricts the run to these clean herb names ("Guam leaf", "Ranarr weed", ...).
	// Empty means every herb the account's Herblore level allows, which is the TS default too.
	Herbs []string `json:"herbs"`
	// BankBooth is the bank loc's display name; blank falls back to "Bank booth".
	BankBooth string `json:"bankBooth"`
	LogDetail string `json:"logDetail"`
}

// DefaultSettings are the manifest defaults.
func DefaultSettings() Settings {
	return Settings{
		Herbs:     nil,
		BankBooth: BoothName,
		LogDetail: "Normal",
	}
}

// Normalised canonicalises the herb list against the real table (matching ignoring case,
// de-duplicated, stored in the table's own spelling) and applies the blank-falls-back-to-default
// rule every sibling script's Settings uses.
//
// An unrecognised herb name is dropped rather than kept, since nothing downstream could act on
// it — but silently dropping every name would turn a typo into "clean everything", so callers
// should report UnknownHerbs before starting a run.
func (s Settings) Normalised() Settings {
	out := make([]string, 0, len(s.Herbs))
	seen := map[string]bool{}
	for _, name := range s.Herbs {
		h, ok := HerbByName(name)
		if !ok || seen[h.Key] {
			continue
		}
		seen[h.Key] = true
		out = append(out, h.Name)
	}
	s.Herbs = out
	s.BankBooth = strings.TrimSpace(s.BankBooth)
	if s.BankBooth == "" {
		s.BankBooth = BoothName
	}
	return s
}

// UnknownHerbs lists the entries of raw that name no herb in the table, so a run can warn about
// a typo instead of quietly widening itself to every herb.
func UnknownHerbs(raw []string) []string {
	var unknown []string
	for _, name := range raw {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, ok := HerbByName(name); !ok {
			unknown = append(unknown, strings.TrimSpace(name))
		}
	}
	return unknown
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
