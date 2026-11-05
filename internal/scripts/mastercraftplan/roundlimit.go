package mastercraftplan

import (
	"fmt"
	"strconv"
	"time"
)

// THE ROUND TIME LIMIT (a user request: "this round should last for an hour regardless if all the bars get used or not"
// for the platebody round, and "superheating mithril should also be done only in 1hour batches here and there instead of
// spending the whole time smithing them so we can move onto other tasks").
//
// A mode may have its own budget, shorter than craftMaxRoundMinutes. A round that reaches it ends at the next safe point (the
// loads' hold check, which runs before every bank trip and every burst, so the pack is banked and the bank in order) and
// pauses its mode through the ledger's pause fields for craftRoundLimitPauseMinutes, so another round gets a turn. The pause
// is not a failure: the round did its work, and the ledger's backoff and hand-over counters are left alone.
//
// THE OWNER HAS NOT CHOSEN THE FINAL ROTATION YET: the budgets (60 minutes each) and the pause (60 minutes) are placeholders
// until he says how long each round runs and what brings it back. Every duration is a setting, and RoundBudget is the one
// place a new rule for a mode goes.
//
// SUPERHEAT PHASES (changed 5 Nov 2026 evening, schedule.go): the Mithril (and Gold) batch has craftSuperheatRoundMinutes, the
// Adamantite and Runite batch craftAdamantRoundMinutes, each paused by its own craftPause*Minutes after; the Steel phase has no clock
// and runs to craftSteelStop. The pause of a phase is Ledger.Sched.HeatPause (PauseHeat), not the mode-wide pause below.

// The settings of the limit, and their defaults.
const (
	KeySuperheatRoundMinutes  = "craftSuperheatRoundMinutes"
	KeyPlatebodyRoundMinutes  = "craftPlatebodyRoundMinutes"
	KeyRoundLimitPauseMinutes = "craftRoundLimitPauseMinutes"

	DefaultSuperheatRound = 60 * time.Minute
	DefaultPlatebodyRound = 60 * time.Minute
	// DefaultRoundLimitPause is how long a mode that reached its limit is left alone. A placeholder (see above).
	DefaultRoundLimitPause = 60 * time.Minute
)

func init() {
	knownKeys[KeySuperheatRoundMinutes] = true
	knownKeys[KeyPlatebodyRoundMinutes] = true
	knownKeys[KeyRoundLimitPauseMinutes] = true
}

// validateRoundLimits reports every problem with the limit's settings (0 is no limit / no pause).
func (s Settings) validateRoundLimits() []string {
	var p []string
	for _, c := range []struct {
		key string
		v   time.Duration
	}{{KeySuperheatRoundMinutes, s.SuperheatRound}, {KeyPlatebodyRoundMinutes, s.PlatebodyRound}, {KeyRoundLimitPauseMinutes, s.RoundLimitPause}} {
		if c.v < 0 || c.v > 24*time.Hour {
			p = append(p, fmt.Sprintf("%s %s is outside 0..1440 minutes (0 is none)", c.key, c.v))
		}
	}
	return p
}

// RoundBudget is the time one round of mode may last, 0 for none beyond craftMaxRoundMinutes. bar is the bar the round is
// casting or smithing now (the superheat limit applies to "Mithril bar" only).
func RoundBudget(set Settings, mode Mode, bar string) time.Duration {
	switch mode {
	case ModeSuperheat:
		return heatBudget(set, bar)
	case ModePlatebody:
		return set.PlatebodyRound
	case ModeBones:
		return set.Sched.BonesRound
	}
	return 0
}

// LimitReached is whether a round of mode that began at start has used its budget while working on bar.
func LimitReached(set Settings, mode Mode, bar string, start, now time.Time) bool {
	return RoundExpired(start, now, RoundBudget(set, mode, bar))
}

// PauseForLimit holds mode off until now plus craftRoundLimitPauseMinutes. It is not a fruitless pause: it is set even though
// the round produced something.
func (l *Ledger) PauseForLimit(mode Mode, now time.Time, set Settings) {
	pause := set.RoundLimitPause
	if mode == ModePlatebody {
		pause = set.Sched.PausePlatebody
	}
	if mode == ModeBones {
		pause = set.Sched.BonesPause
	}
	until := now.Add(pause)
	if pause <= 0 {
		until = time.Time{}
	}
	switch mode {
	case ModeSuperheat:
		l.SuperheatPauseUntil = until
	case ModePlatebody:
		l.Plate.PauseUntil = until
	case ModeBones:
		l.Bones.PauseUntil = until
	}
}

// LimitWhy is the log line of a round that reached its limit.
func LimitWhy(mode Mode, budget time.Duration, pause time.Duration) string {
	return string(mode) + " round reached its " + strconv.Itoa(int(budget.Minutes())) + " minute limit: banked, and the mode is paused " + strconv.Itoa(int(pause.Minutes())) + " minutes so another round gets a turn"
}
