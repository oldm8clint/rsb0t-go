package runerunnerplan

// The rules that keep the buyer out of trouble: when it may hop, when Lundail's Wilderness
// crossing is allowed, when the lever may be pulled, and when a shop and world pair is taken out
// of the ring for a while (a keeper that will not open) or for good (prices the model does not
// predict).

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// QuietFor is how long the account must be out of combat and unhurt before a hop.
const QuietFor = 15 * time.Second

// HopCheck is what MayHop reads.
type HopCheck struct {
	Dead bool
	// HP and MaxHP are hitpoints now and the level.
	HP, MaxHP int
	InCombat  bool
	// LastHurt is the last time the account was in combat or lost hitpoints; zero for never.
	LastHurt time.Time
	Now      time.Time
	// RandomEvent is randomevents.Busy.
	RandomEvent bool
	Here        *tile.Tile
	TripActive  bool
	// LoginBusy is the fleet's login path busy (a restart ramp); HopInProgress a hop already
	// running for this account.
	LoginBusy     bool
	HopInProgress bool
}

// MayHop reports whether a hop may be asked for now, and the first reason it may not.
func MayHop(c HopCheck) (bool, string) {
	switch {
	case c.Dead:
		return false, "dead"
	case c.MaxHP <= 0 || c.HP*2 < c.MaxHP:
		return false, fmt.Sprintf("hitpoints %d/%d are under half", c.HP, c.MaxHP)
	case c.InCombat:
		return false, "in combat"
	case !c.LastHurt.IsZero() && c.Now.Sub(c.LastHurt) < QuietFor:
		return false, fmt.Sprintf("hurt %s ago (wait %s)", c.Now.Sub(c.LastHurt).Round(time.Second), QuietFor)
	case c.RandomEvent:
		return false, "a random event is present"
	case c.Here == nil:
		return false, "position unknown"
	case fleetplan.WildernessLevel(*c.Here) > 0:
		return false, "in the Wilderness"
	case fleetplan.InMageBank(*c.Here):
		return false, "in the Mage Bank"
	case c.TripActive:
		return false, "a Mage Bank trip is active"
	case c.LoginBusy:
		return false, "the fleet's login path is busy"
	case c.HopInProgress:
		return false, "a hop is already in progress"
	}
	return true, ""
}

// ---- Lundail ----

// DeathWindow is the rolling window LundailMaxDeaths counts over.
const DeathWindow = 24 * time.Hour

// PruneDeaths keeps the deaths inside DeathWindow.
func PruneDeaths(deaths []time.Time, now time.Time) []time.Time {
	var out []time.Time
	for _, d := range deaths {
		if now.Sub(d) < DeathWindow {
			out = append(out, d)
		}
	}
	return out
}

// LundailAllowed applies the death policy: fewer than max Lundail deaths inside DeathWindow. At
// the limit, Lundail waits until the oldest death ages out.
func LundailAllowed(deaths []time.Time, now time.Time, max int) (bool, string) {
	recent := PruneDeaths(deaths, now)
	if len(recent) < max {
		return true, ""
	}
	oldest := recent[0]
	for _, d := range recent {
		if d.Before(oldest) {
			oldest = d
		}
	}
	return false, fmt.Sprintf("%d Lundail deaths in 24h (lundailMaxDeaths %d); off until %s", len(recent), max, oldest.Add(DeathWindow).Format(time.RFC3339))
}

// LeverCheck is what LeverGo reads, straight after scarabrunner.PrepareKit.
type LeverCheck struct {
	KitOK  bool
	KitWhy string
	// PlanCoins and PlanRunes are the kit's purchase plan (Coins, Runes()).
	PlanCoins, PlanRunes int
	MinVisitRunes        int
	DeathsOK             bool
	DeathsWhy            string
}

// LeverGo is the empty-crossing rule (runs b5g115r7t and b51x1x5s3 crossed the Wilderness with
// an empty purse): the lever is pulled only with the blade-only kit verified, coins planned for
// Gundai, a purchase worth the crossing, and the death policy allowing Lundail.
func LeverGo(c LeverCheck) (bool, string) {
	switch {
	case !c.KitOK:
		why := "the Wilderness kit is not the blade alone"
		if c.KitWhy != "" {
			why += ": " + c.KitWhy
		}
		return false, why
	case c.PlanCoins <= 0:
		return false, "no coins planned for Gundai"
	case c.PlanRunes < c.MinVisitRunes:
		return false, fmt.Sprintf("the plan buys %d runes, under minVisitRunes %d", c.PlanRunes, c.MinVisitRunes)
	case !c.DeathsOK:
		return false, c.DeathsWhy
	}
	return true, ""
}

// ---- pairs off the ring ----

// StrikeLimit is how many failed opens in a row take a pair off the ring for PairOffFor.
const (
	StrikeLimit = 3
	PairOffFor  = 60 * time.Minute
)

// Strike records a keeper that would not open for this pair ("Betty@1"). At StrikeLimit in a row
// the pair is off for PairOffFor and the strikes start again; off reports that.
func (st *State) Strike(pair string, now time.Time) (off bool) {
	st.normalise()
	st.PairStrikes[pair]++
	if st.PairStrikes[pair] >= StrikeLimit {
		st.PairOffUntil[pair] = now.Add(PairOffFor)
		delete(st.PairStrikes, pair)
		return true
	}
	return false
}

// ClearStrikes forgets a pair's strikes after a visit that opened the shop.
func (st *State) ClearStrikes(pair string) {
	if st.PairStrikes != nil {
		delete(st.PairStrikes, pair)
	}
}

// PairOff reports whether a pair is off the ring now, and why.
func (st State) PairOff(pair string, now time.Time) (bool, string) {
	if until, ok := st.PairOffUntil[pair]; ok && now.Before(until) {
		return true, fmt.Sprintf("%s would not open %d times in a row; off until %s", pair, StrikeLimit, until.Format(time.RFC3339))
	}
	if st.PriceMismatch[pair] >= PriceMismatchLimit {
		return true, fmt.Sprintf("%s: %d price-model mismatches; off until the script is restarted", pair, st.PriceMismatch[pair])
	}
	return false, ""
}

// NotePrice records one visit's price check for a pair. mismatch is PriceMismatch's verdict and
// disabled is true once the pair has reached PriceMismatchLimit.
func (st *State) NotePrice(pair string, predicted, spent int) (mismatch, disabled bool) {
	st.normalise()
	if !PriceMismatch(predicted, spent) {
		return false, st.PriceMismatch[pair] >= PriceMismatchLimit
	}
	st.PriceMismatch[pair]++
	return true, st.PriceMismatch[pair] >= PriceMismatchLimit
}

// ClearPriceChecks forgets every price mismatch: what an operator's restart of the script does
// (a restart the ring itself caused by hopping does not; see State.HopPending).
func (st *State) ClearPriceChecks() (cleared []string) {
	for pair, n := range st.PriceMismatch {
		if n > 0 {
			cleared = append(cleared, pair)
		}
	}
	st.PriceMismatch = map[string]int{}
	return cleared
}

// FareFloat is the coins the buyer keeps in its pack for the ship chain (Port Sarim, Karamja,
// Brimhaven, Ardougne): both fares each way. The White Wolf Mountain dens are avoided at every
// combat level, so the only land-and-sea route between the Taverley side and Catherby is that
// chain, and the Walker leaves fare edges out of a plan the pack cannot pay for.
const FareFloat = 2 * fleetplan.ShipFares

// FareShort reports how many coins to draw so the pack holds FareFloat; 0 when it already does.
func FareShort(packCoins int) int {
	if packCoins >= FareFloat {
		return 0
	}
	return FareFloat - packCoins
}
