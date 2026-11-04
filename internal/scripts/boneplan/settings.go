package boneplan

import "strings"

// MaxPrayerLevel is the ceiling on a goal.
const MaxPrayerLevel = 99

// Settings are BoneBurier's settings. BoneBurier.ts itself carries no settingsSchema at all —
// every one of these lives only on its own overlay settings page (see boneSettings.ts's own
// doc comment) — so there is no TS manifest key spelling to match; these are named after
// bonePlan.ts/boneSettings.ts's own field names, the same way every other ScriptContext.Settings
// key in this codebase is.
type Settings struct {
	// Source is "ground" or "bank" (BoneSource's string form).
	Source    string `json:"source"`
	Names     string `json:"names"`
	BankNames string `json:"bankNames"`
	// Disposal is "bury", "bank" or "split".
	Disposal string `json:"disposal"`
	Radius   int    `json:"radius"`
	// BuryWhenFull: false buries as each comes up (BoneBurier.ts's own default).
	BuryWhenFull bool   `json:"buryWhenFull"`
	IdleMins     int    `json:"idleMins"`
	GoalLevel    int    `json:"goalLevel"`
	LogDetail    string `json:"logDetail"`
}

// DefaultSettings mirrors BONE_DEFAULTS.
func DefaultSettings() Settings {
	return Settings{
		Source:       string(SourceGround),
		Names:        strings.Join(DefaultNames, ","),
		BankNames:    strings.Join(DefaultBankNames, ","),
		Disposal:     string(DisposalBury),
		Radius:       DefaultRadius,
		BuryWhenFull: false,
		IdleMins:     0,
		GoalLevel:    0,
		LogDetail:    "Normal",
	}
}

// Normalised clamps radius/idleMins/goalLevel to BONE_BOUNDS' ranges and tidies the name lists,
// mirroring normalisedBone.
func (s Settings) Normalised() Settings {
	if s.Radius < 1 {
		s.Radius = DefaultRadius
	}
	if s.Radius > 50 {
		s.Radius = 50
	}
	if s.IdleMins < 0 {
		s.IdleMins = 0
	}
	if s.IdleMins > 120 {
		s.IdleMins = 120
	}
	if s.GoalLevel < 0 {
		s.GoalLevel = 0
	}
	if s.GoalLevel > MaxPrayerLevel {
		s.GoalLevel = MaxPrayerLevel
	}
	names := ParseNames(strings.Split(s.Names, ","))
	if len(names) == 0 {
		names = DefaultNames
	}
	s.Names = strings.Join(names, ",")
	s.BankNames = strings.Join(ParseNames(strings.Split(s.BankNames, ",")), ",")
	return s
}

func (s Settings) NameList() []string     { return ParseNames(strings.Split(s.Names, ",")) }
func (s Settings) BankNameList() []string { return ParseNames(strings.Split(s.BankNames, ",")) }
func (s Settings) SourceMode() BoneSource { return ParseSource(s.Source) }

// Mode is the effective disposal mode: bones drawn from the bank are always for the ground —
// taking bones out to put them back is a round trip that buries nothing.
func (s Settings) Mode() DisposalMode {
	if s.SourceMode() == SourceBank {
		return DisposalBury
	}
	return ParseDisposal(s.Disposal)
}

func (s Settings) BanksAnything() bool {
	return s.Mode() == DisposalBank || (s.Mode() == DisposalSplit && len(s.BankNameList()) > 0)
}

func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
