package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/mastercraftplan"
)

// A round that reached its time limit pauses its mode for the rotation pause whatever it produced, and is no failure.
func TestARoundAtItsLimitPausesItsModeAndIsNoFailure(t *testing.T) {
	b := &masterCrafterBot{set: mastercraftplan.DefaultSettings()}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for _, mode := range []mastercraftplan.Mode{mastercraftplan.ModePlatebody, mastercraftplan.ModeSuperheat} {
		b.ledger = mastercraftplan.Ledger{}
		b.recordWaits(nil, masterRoundOutcome{Mode: mode, Produced: 400, LimitReached: true}, now)
		until := b.ledger.Plate.PauseUntil
		if mode == mastercraftplan.ModeSuperheat {
			until = b.ledger.SuperheatPauseUntil
		}
		if !until.Equal(now.Add(b.set.RoundLimitPause)) {
			t.Errorf("%s paused until %v, want %v", mode, until, now.Add(b.set.RoundLimitPause))
		}
		if b.ledger.Fails(mode) != 0 {
			t.Errorf("%s: a limit is no failure", mode)
		}
	}
}

// An unfinished platebody round keeps the bars in the bank from the deliver round.
func TestPlateKeepsBarsWhileActive(t *testing.T) {
	b := &masterCrafterBot{}
	if b.plateKeepsBars() {
		t.Error("no round, no keep")
	}
	b.ledger.Plate.Active = true
	if !b.plateKeepsBars() {
		t.Error("an active round keeps its bars")
	}
}
