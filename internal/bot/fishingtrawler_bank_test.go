package bot

import (
	"testing"
	"time"
)

func TestTrawlerBankVisitOutlivesTheCrewLoopRestart(t *testing.T) {
	at := time.Date(2026, 11, 4, 9, 37, 55, 0, time.UTC)
	(&fishingTrawlerBot{}).banked(&Context{Username: "CrewMemberA"}, at)
	// The end-of-game hop restarts the script: a new crew loop, the same account.
	if got := (&fishingTrawlerBot{}).lastBank(&Context{Username: "crewmembera"}); !got.Equal(at) {
		t.Fatalf("a restarted crew loop forgot the visit at %s: %s", at, got)
	}
	if got := (&fishingTrawlerBot{}).lastBank(&Context{Username: "someone else"}); !got.IsZero() {
		t.Fatalf("another account's visit leaked: %s", got)
	}
}
