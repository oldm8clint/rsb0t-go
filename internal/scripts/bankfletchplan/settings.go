package bankfletchplan

import "strings"

// Settings are BankFletcher's settings for this first, cut-mode-only port. The JSON keys mirror
// the TS manifest's own material/product keys, narrowed to what this port actually drives.
type Settings struct {
	// Material is the log type fletched; blank falls back to Logs. Only plain Logs is proven
	// live by this port (see plan.go's own doc comment on scope), but any log name is accepted
	// so a future session can extend the make-menu match without touching Settings again.
	Material  string `json:"material"`
	LogDetail string `json:"logDetail"`
}

// DefaultSettings are this port's own defaults.
func DefaultSettings() Settings {
	return Settings{
		Material:  Logs,
		LogDetail: "Normal",
	}
}

// Normalised applies the blank-falls-back-to-default rule every sibling Settings uses.
func (s Settings) Normalised() Settings {
	s.Material = strings.TrimSpace(s.Material)
	if s.Material == "" {
		s.Material = Logs
	}
	return s
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
