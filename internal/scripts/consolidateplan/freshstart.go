package consolidateplan

import (
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// THE FRESH START (a user request: "all accs should hand off to the mule to start fresh", answered as
// "bank surplus + loot"): one deposit trip for every Stage 3 and Stage 5 account whose last hand-off is
// older than FreshStartAt, whatever its timer, with the minimums at one so anything over the keep goes. The
// keep itself (the working kit, the coin reserve, consolidateKeepItems) is unchanged, so no account
// stalls for want of what it was using. After the trip the account is back on its ordinary timer.

// FreshStartAt is the moment users asked for the fresh start; a hand-off before it is owed one more.
var FreshStartAt = time.Date(2026, time.October, 11, 23, 0, 0, 0, time.FixedZone("+0800", 8*3600))

// FreshStartOwed reports whether an account whose last hand-off was at last (zero when never) and which
// is at stage owes the fresh-start trip.
func FreshStartOwed(last time.Time, stage int) bool {
	if stage != fleetplan.StageSurvivalCombat && stage != freshStartThieving {
		return false
	}
	return last.Before(FreshStartAt)
}

// freshStartThieving is Stage 3 (thieving and rune supply), the other stage with a bank worth emptying.
const freshStartThieving = 3

// FreshStart is s for the fresh-start trip: every minimum at one.
func FreshStart(s Settings) Settings {
	s = s.Normalised()
	s.MinCoins, s.MinRunes, s.MinFood, s.MinLootValue, s.MinLootRows = 1, 1, 1, 1, 1
	return s
}
