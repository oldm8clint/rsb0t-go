package smeltplan

import "strings"

// Settings is SmelterBot's declared settings, resolved by internal/bot from the manifest schema.
type Settings struct {
	// Bar is the bar to smelt, by display name. Blank falls back to the table's first row.
	Bar string `json:"bar"`
	// BankBooth is the bank loc's display name; blank falls back to "Bank booth".
	BankBooth string `json:"bankBooth"`
	LogDetail string `json:"logDetail"`
}

// DefaultSettings are the manifest defaults — bronze, the one row every account can smelt at
// Smithing 1, which is SmelterBot.ts's own default too.
func DefaultSettings() Settings {
	return Settings{
		Bar:       Recipes[0].Bar,
		BankBooth: BoothName,
		LogDetail: "Normal",
	}
}

// Normalised canonicalises the bar name against the real table (so "adamant", "Rune" and
// "bronze bar" all land on content's own spelling) and applies the blank-falls-back-to-default
// rule every sibling script's Settings uses.
//
// An unrecognised bar falls back to the default rather than leaving the run with nothing to do;
// callers should report UnknownBar before starting so a typo is visible rather than silent.
func (s Settings) Normalised() Settings {
	out := s
	if r, ok := RecipeFor(s.Bar); ok {
		out.Bar = r.Bar
	} else {
		out.Bar = DefaultSettings().Bar
	}
	if strings.TrimSpace(out.BankBooth) == "" {
		out.BankBooth = BoothName
	}
	if strings.TrimSpace(out.LogDetail) == "" {
		out.LogDetail = "Normal"
	}
	return out
}

// Recipe is the resolved recipe these settings select.
func (s Settings) Recipe() Recipe {
	r, ok := RecipeFor(s.Bar)
	if !ok {
		r, _ = RecipeFor(DefaultSettings().Bar)
	}
	return r
}

// Verbose reports whether every stage change should be logged.
func (s Settings) Verbose() bool { return strings.EqualFold(strings.TrimSpace(s.LogDetail), "Verbose") }

// UnknownBar returns the caller's bar name when it matches no row at all, so a typo can be
// reported rather than silently becoming the default.
func UnknownBar(name string) (string, bool) {
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	if _, ok := RecipeFor(name); ok {
		return "", false
	}
	return name, true
}
