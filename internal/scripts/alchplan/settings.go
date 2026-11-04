package alchplan

import "strings"

// Settings are Alcher's settings, keyed exactly like Alcher.ts's own settingsSchema so a
// fleet.config.json roster entry decodes straight in.
type Settings struct {
	Spell        string `json:"spell"`
	Items        string `json:"items"`
	CastsPerTrip int    `json:"castsPerTrip"`
	UseFireStaff bool   `json:"useFireStaff"`
	LogDetail    string `json:"logDetail"`
}

func DefaultSettings() Settings {
	return Settings{
		Spell:        AutoSpell,
		Items:        DefaultItems,
		CastsPerTrip: DefaultCastsPerTrip,
		UseFireStaff: true,
		LogDetail:    "Normal",
	}
}

// Normalised clamps CastsPerTrip to the manifest's 1-5000 range, matching Alcher.ts's own
// castsPerTrip() reader.
func (s Settings) Normalised() Settings {
	if s.CastsPerTrip < 1 {
		s.CastsPerTrip = 1
	}
	if s.CastsPerTrip > 5000 {
		s.CastsPerTrip = 5000
	}
	if strings.TrimSpace(s.Spell) == "" {
		s.Spell = AutoSpell
	}
	return s
}

func (s Settings) ItemList() []string { return ParseItems(s.Items) }

func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
