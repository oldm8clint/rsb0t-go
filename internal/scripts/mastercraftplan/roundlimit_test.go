package mastercraftplan

import (
	"testing"
	"time"
)

func TestRoundBudgetIsMithrilOnlyForSuperheat(t *testing.T) {
	set := DefaultSettings()
	if set.SuperheatRound != time.Hour || set.PlatebodyRound != time.Hour {
		t.Fatalf("defaults %s %s, want an hour each", set.SuperheatRound, set.PlatebodyRound)
	}
	if RoundBudget(set, ModeSuperheat, "Mithril bar") != time.Hour {
		t.Error("Mithril has the hour")
	}
	// The Adamantite batch is an hour too (a user request); Steel runs to its stock.
	for _, bar := range []string{"Adamantite bar", "Runite bar"} {
		if RoundBudget(set, ModeSuperheat, bar) != time.Hour {
			t.Errorf("%q has the Adamantite hour", bar)
		}
	}
	for _, bar := range []string{"Steel bar", ""} {
		if RoundBudget(set, ModeSuperheat, bar) != 0 {
			t.Errorf("%q must run until its stock is reached or the ore is gone", bar)
		}
	}
	if RoundBudget(set, ModePlatebody, "") != time.Hour || RoundBudget(set, ModeSmith, "") != 0 {
		t.Error("platebody has the hour, the anvil round none")
	}
}

func TestLimitReachedAndPause(t *testing.T) {
	set := DefaultSettings()
	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if LimitReached(set, ModeSuperheat, "Mithril bar", t0, t0.Add(59*time.Minute)) {
		t.Error("59 minutes is inside the hour")
	}
	if !LimitReached(set, ModeSuperheat, "Mithril bar", t0, t0.Add(time.Hour)) {
		t.Error("an hour is the limit")
	}
	if LimitReached(set, ModeSuperheat, "Steel bar", t0, t0.Add(5*time.Hour)) {
		t.Error("Steel has no time limit")
	}
	var l Ledger
	l.PauseForLimit(ModeSuperheat, t0, set)
	if !l.SuperheatPauseUntil.Equal(t0.Add(set.RoundLimitPause)) {
		t.Errorf("pause %v", l.SuperheatPauseUntil)
	}
	l.PauseForLimit(ModePlatebody, t0, set)
	if !l.Plate.PauseUntil.Equal(t0.Add(set.Sched.PausePlatebody)) {
		t.Errorf("plate pause %v", l.Plate.PauseUntil)
	}
	set.RoundLimitPause = 0
	l.PauseForLimit(ModeSuperheat, t0, set)
	if !l.SuperheatPauseUntil.IsZero() {
		t.Error("a zero pause clears it")
	}
}

func TestRoundLimitSettings(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{KeySuperheatRoundMinutes: 30, KeyPlatebodyRoundMinutes: 90, KeyRoundLimitPauseMinutes: 15})
	if err != nil || s.SuperheatRound != 30*time.Minute || s.PlatebodyRound != 90*time.Minute || s.RoundLimitPause != 15*time.Minute {
		t.Fatalf("%v %+v", err, s)
	}
	s.SuperheatRound = -time.Minute
	if len(s.validateRoundLimits()) != 1 {
		t.Error("a negative budget is a problem")
	}
}
