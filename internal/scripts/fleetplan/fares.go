package fleetplan

import (
	"fmt"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// FareAction is what an account about to walk across the water does about its fares first.
type FareAction int

const (
	// FareNone: the walk stays on one landmass, or the pack already holds the fares.
	FareNone FareAction = iota
	// FareDraw: take the fares out of the account's own bank (nearest bank; one try per FareDrawRetry).
	FareDraw
	// FareEarn: the bank came up short, pickpocket the fares before planning the walk.
	FareEarn
	// FareCannot: the fares cannot be earned where the account stands (Kandarin below Thieving 25):
	// the caller skips the walk or parks rather than sending it to fail.
	FareCannot
)

func (a FareAction) String() string {
	switch a {
	case FareNone:
		return "none"
	case FareDraw:
		return "draw"
	case FareEarn:
		return "earn"
	case FareCannot:
		return "cannot"
	}
	return "?"
}

// FareInput is what DecideFare reads.
type FareInput struct {
	// Here and Dest are the walk's endpoints; nil means unknown and decides nothing.
	Here, Dest *tile.Tile
	// Coins is what the pack holds.
	Coins int
	// RoundTrip asks for the fares of the walk back as well: a quest leg ends where the account
	// cannot earn the way home (Imp Catcher's imps are in Kandarin, Mizgog is at Draynor).
	RoundTrip bool
	// BankTried is true once the account's own bank has been tried for this need and did not
	// cover it (empty, unreachable, or inside the retry window).
	BankTried bool
	// Thieving is the account's Thieving level, for where the fares can be earned.
	Thieving int
	// TeleportCovers says a teleport the account can pay for and may cast now lands it on the destination's landmass, so the
	// crossing there costs no fare; TeleportCoversBoth that the way back is covered too (a second payable cast, to the landmass
	// the account stands on now). The caller computes them with TeleportCoverage, from a cheap estimate and no planner run, and
	// leaves both false when the last cast for the crossing failed or the spell is paused, so a refused cast cannot strand an
	// account on the wrong coast with no coins (teleport-walks, 13 Oct 2026).
	TeleportCovers, TeleportCoversBoth bool
}

// FareDecision is DecideFare's answer.
type FareDecision struct {
	Action FareAction
	// Need is the coins the pack should hold for the walk; Short is what it lacks. Draw is what a
	// FareDraw takes out of the bank: Need, or the FareFloat when that is more.
	Need, Short, Draw int
	// Why is the log line's reason, "" for FareNone.
	Why string
}

// FareFloat is the least a fare draw takes out of the bank: ten one-way crossings. A draw of the bare
// fare left the pack empty on the far side, so the next crossing needed another bank or a pickpocket
// detour (live, 12 October 2026: 2,609 "short of the fare" waits and 99 STRANDED walks to Lumbridge in
// 6.4 hours, one of them from an account whose bank held 89,627 coins).
const FareFloat = 600

// DecideFare is the one rule for a walk between two landmasses (freshstart.LandmassOf): hold the
// ships' fares before the walk is planned, drawing them from the bank and, when the bank has none,
// earning them. The walker plans a route across the water only while the pack can pay it, so a
// walk started without them is "unreachable" at best and a trip over White Wolf Mountain at worst.
func DecideFare(in FareInput) FareDecision {
	leg := freshstart.FareToCross(in.Here, in.Dest)
	if leg == 0 {
		return FareDecision{}
	}
	// A cast that lands on the destination's side pays the way there, a second the way back.
	need := 0
	if !in.TeleportCovers && !in.TeleportCoversBoth {
		need += leg
	}
	if in.RoundTrip && !in.TeleportCoversBoth {
		need += leg
	}
	if need == 0 {
		return FareDecision{}
	}
	if in.Coins >= need {
		return FareDecision{Need: need}
	}
	d := FareDecision{Need: need, Short: need - in.Coins, Draw: max(need, FareFloat)}
	d.Why = fmt.Sprintf("%d of %d coins for the ships from %s to %s", in.Coins, need,
		freshstart.LandmassOf(*in.Here), freshstart.LandmassOf(*in.Dest))
	switch {
	case !in.BankTried:
		d.Action = FareDraw
	case !CanEarnFaresAt(in.Here, in.Thieving):
		d.Action = FareCannot
	default:
		d.Action = FareEarn
	}
	return d
}

// CanEarnFaresAt is whether coins can be pickpocketed where the account stands. East of the
// ships and on Karamja the courtyard / banana mechanism of freshstartrunner.PickpocketStep works
// from anywhere; in Kandarin only the Ardougne market rungs pay (WestFundRung), which opens at
// Thieving 25.
func CanEarnFaresAt(here *tile.Tile, thieving int) bool {
	if here == nil {
		return false
	}
	if freshstart.LandmassOf(*here) != freshstart.LandKandarin {
		return true
	}
	_, ok := WestFundRung(thieving)
	return ok
}
