package retaliateplan

import (
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Choices the settings take. Every key has a default, so a script assigned with an empty settings object runs the proven
// layout (docs/bot_script_settings_additional.md, "RetaliateChopper").
const (
	// ClearDrop drops one log as the interaction clear each hit (a power-chop: one log in, one out, the pack never
	// fills). It needs a log in the pack; with none held the controls-tab button clears instead.
	ClearDrop = "drop"
	// ClearButton presses "Auto retaliate: On" (a no-op that clears the interaction, content player_controls.rs2
	// controls:com_2: if_close, then p_stopaction). It needs no item, and keeps the logs, so the pack fills.
	ClearButton = "button"

	// FullDrop empties the logs with a burst of drops when the pack is full.
	FullDrop = "drop"
	// FullBank walks to the Falador east bank, deposits the logs, and walks back to set the rats up again.
	FullBank = "bank"

	// FallbackKnife chops plain with the knife-on-log manipulation (3 ticks a roll) when the rats cannot be phased.
	FallbackKnife = "knife"
	// FallbackPlain chops with a plain click (4 ticks a roll).
	FallbackPlain = "plain"

	// SiteFalador is the only site this build knows: the Falador south oaks and the two farm rats.
	SiteFalador = "falador-oaks"
)

// DefaultAggro is the preference list of aggro spells: the three stat-lowering spells, none of which deals damage (a rat has
// 2 hitpoints, and the script never fights one to death). Wind Strike, which can kill a rat, is only used when the settings
// list it.
const DefaultAggro = "confuse,weaken,curse"

// Settings are RetaliateChopper's, read by internal/bot/retaliatechopper.go with the StringSetting/IntSetting helpers.
type Settings struct {
	// Site is the site table to use; only SiteFalador exists.
	Site string
	// Stand overrides the stand tile as "x,z"; empty picks the best tile the collision pack and the loc reach model allow.
	Stand string
	// Clear is ClearDrop or ClearButton.
	Clear string
	// Full is FullDrop or FullBank: what to do when the pack is full.
	Full string
	// Aggro is the comma-separated spell preference used to wake the two rats, first affordable one wins.
	Aggro string
	// PhaseTries is how many times the rats are re-phased (the account steps away and back and the hit spacing is measured
	// again) before the account falls back to ordinary chopping.
	PhaseTries int
	// MinGain is the share of roll gaps that must be 2 ticks for the controller to keep going.
	MinGain float64
	// Lead is the starting number of ticks a write is aimed ahead of the tick it should be read in (0 to 2). The controller
	// moves it itself from what it sees.
	Lead int
	// StopHPPercent stops the script, with auto-retaliate off, when hitpoints fall below this share of the maximum. The rats
	// hit for 0, so any real damage is another monster.
	StopHPPercent int
	// Fallback is FallbackKnife or FallbackPlain.
	Fallback string
	// FallbackMins is how long the account chops the ordinary way after the rats could not be phased, before it tries again.
	FallbackMins int
	// ReportEvery is how many ticks of looping pass between two rolling log lines.
	ReportEvery int
	// LogDetail is "Verbose" to log every decision of the loop's setup.
	LogDetail string
}

// DefaultSettings are what an account with no settings runs.
func DefaultSettings() Settings {
	return Settings{
		Site: SiteFalador, Clear: ClearDrop, Full: FullDrop, Aggro: DefaultAggro,
		PhaseTries: 6, MinGain: 0.7, Lead: 1, StopHPPercent: 70,
		Fallback: FallbackKnife, FallbackMins: 10, ReportEvery: 300, LogDetail: "Normal",
	}
}

// Normalised fills zero and out-of-range values with the defaults and clamps the rest.
func (s Settings) Normalised() Settings {
	d := DefaultSettings()
	s.Site = strings.ToLower(strings.TrimSpace(s.Site))
	if s.Site != SiteFalador {
		s.Site = d.Site
	}
	s.Stand = strings.TrimSpace(s.Stand)
	s.Clear = strings.ToLower(strings.TrimSpace(s.Clear))
	if s.Clear != ClearDrop && s.Clear != ClearButton {
		s.Clear = d.Clear
	}
	s.Full = strings.ToLower(strings.TrimSpace(s.Full))
	if s.Full != FullDrop && s.Full != FullBank {
		s.Full = d.Full
	}
	if strings.TrimSpace(s.Aggro) == "" {
		s.Aggro = d.Aggro
	}
	if s.PhaseTries < 1 {
		s.PhaseTries = d.PhaseTries
	}
	if s.PhaseTries > 20 {
		s.PhaseTries = 20
	}
	if s.MinGain <= 0 || s.MinGain > 1 {
		s.MinGain = d.MinGain
	}
	if s.Lead < 0 {
		s.Lead = 0
	}
	if s.Lead > MaxLead {
		s.Lead = MaxLead
	}
	if s.StopHPPercent < 1 || s.StopHPPercent > 99 {
		s.StopHPPercent = d.StopHPPercent
	}
	s.Fallback = strings.ToLower(strings.TrimSpace(s.Fallback))
	if s.Fallback != FallbackKnife && s.Fallback != FallbackPlain {
		s.Fallback = d.Fallback
	}
	if s.FallbackMins < 1 {
		s.FallbackMins = d.FallbackMins
	}
	if s.FallbackMins > 240 {
		s.FallbackMins = 240
	}
	if s.ReportEvery < 50 {
		s.ReportEvery = d.ReportEvery
	}
	if strings.TrimSpace(s.LogDetail) == "" {
		s.LogDetail = d.LogDetail
	}
	return s
}

// Verbose reports whether LogDetail asks for every setup decision.
func (s Settings) Verbose() bool { return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose") }

// AggroList is the aggro spells in preference order, lower-cased.
func (s Settings) AggroList() []string {
	var out []string
	for _, p := range strings.Split(s.Aggro, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// StandTile parses the Stand override ("3003,3312"); ok is false when it is empty or malformed.
func (s Settings) StandTile() (tile.Tile, bool) {
	parts := strings.Split(s.Stand, ",")
	if len(parts) != 2 {
		return tile.Tile{}, false
	}
	x, ex := strconv.Atoi(strings.TrimSpace(parts[0]))
	z, ez := strconv.Atoi(strings.TrimSpace(parts[1]))
	if ex != nil || ez != nil || x <= 0 || z <= 0 {
		return tile.Tile{}, false
	}
	return tile.Tile{X: x, Z: z}, true
}
