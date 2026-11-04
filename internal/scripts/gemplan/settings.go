package gemplan

import "strings"

// Settings are GemCutter's settings. The JSON keys mirror the TS manifest's own settingsSchema
// (gems) plus the bank-booth and verbosity conveniences every other ported script carries.
type Settings struct {
	// Gems restricts the run to these cut-gem names ("Sapphire", "Ruby", ...). Empty means
	// every gem the account's Crafting level allows, which is the TS default too.
	Gems []string `json:"gems"`
	// BankBooth is the bank loc's display name; blank falls back to "Bank booth".
	BankBooth string `json:"bankBooth"`
	LogDetail string `json:"logDetail"`
}

// DefaultSettings are the manifest defaults.
func DefaultSettings() Settings {
	return Settings{
		Gems:      nil,
		BankBooth: BoothName,
		LogDetail: "Normal",
	}
}

// Normalised canonicalises the gem list against the real table (matching ignoring case,
// de-duplicated, stored in the table's own spelling) and applies the blank-falls-back-to-default
// rule every sibling script's Settings uses.
//
// An unrecognised gem name is dropped rather than kept, since nothing downstream could act on
// it — but silently dropping every name would turn a typo into "cut everything", so callers
// should report UnknownGems before starting a run.
func (s Settings) Normalised() Settings {
	out := make([]string, 0, len(s.Gems))
	seen := map[string]bool{}
	for _, name := range s.Gems {
		g, ok := GemByName(name)
		if !ok || seen[g.Key] {
			continue
		}
		seen[g.Key] = true
		out = append(out, g.Name)
	}
	s.Gems = out
	s.BankBooth = strings.TrimSpace(s.BankBooth)
	if s.BankBooth == "" {
		s.BankBooth = BoothName
	}
	return s
}

// UnknownGems lists the entries of raw that name no gem in the table, so a run can warn about a
// typo instead of quietly widening itself to every gem.
func UnknownGems(raw []string) []string {
	var unknown []string
	for _, name := range raw {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, ok := GemByName(name); !ok {
			unknown = append(unknown, strings.TrimSpace(name))
		}
	}
	return unknown
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
