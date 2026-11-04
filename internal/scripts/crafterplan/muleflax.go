package crafterplan

// Flax from the mule (a user request: "have a couple accounts get flax from the mule and string
// it into bow strings"). the mule's bank holds about 200,000 flax and no bow strings; with flaxSource
// set to MuleFlax the Seers loop spins that flax instead of picking its own:
//
//   - The account's own bank is drawn first, exactly as BankFlax draws it.
//   - Once a bank visit has counted no flax and the pack holds none, the next step is a mule flax
//     trip (SeersMuleFlax): walk to the mule's post, ask for a kit of flaxKitSize flax (it crosses
//     as bank notes), bank the notes beside the mule (the bank is one bank everywhere, so they are
//     drawn back as flax at Seers), and walk back.
//   - A kit that brings nothing backs the asks off (FlaxKitBackoff) and the loop picks flax in the
//     meantime, so a mule that refuses, holds none or cannot be reached is never hammered.
//
// The bow string hand-over meets the mule at its post too (HandoffStand), not at the Seers stand.

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// MuleFlax is the flax source that draws the bank first, then asks the mule for a kit of flax, and
// picks only while the mule is backing off.
const MuleFlax = "Mule, then pick"

// The flax kit's bounds.
const (
	// DefaultFlaxKitSize is the flax one kit asks for: orderplan.OpenMaxPerOrder, the most of an
	// uncatalogued item a mule serves in one order by default (anyMaxPerOrder).
	DefaultFlaxKitSize = 1000
	// MinFlaxKitSize is one pack of flax: a smaller kit is not worth the walk.
	MinFlaxKitSize = 28
	// MaxFlaxKitSize is orderplan.OpenPerWindow, the most of one uncatalogued item a default mule
	// hands everyone combined per window; a mule clamps a larger ask to its own per-order cap anyway.
	MaxFlaxKitSize = 10000
	// DefaultFlaxKitBackoffMinutes is the wait after a kit that brought nothing, doubled for each
	// fruitless kit in a row up to MaxFlaxKitBackoff.
	DefaultFlaxKitBackoffMinutes = 15
	// MaxFlaxKitBackoff caps the doubling.
	MaxFlaxKitBackoff = 2 * time.Hour
	// MulePostRadius is how close to the mule's post still counts as the Seers loop: the account
	// stands there for a flax kit or a hand-over, outside the Seers road's own radius (the
	// Catherby post is 84 tiles from the Seers bank, the road's radius 80). 60 also takes in
	// Catherby's fishing spots (2846-2854,3424-3430, about 45 tiles from the mule's post), where the
	// fishers switched to this script on 3 Oct start.
	MulePostRadius = 60
)

// AsksMuleForFlax reports whether this flax source fetches flax from the mule.
func AsksMuleForFlax(source string) bool {
	return source == MuleFlax
}

// MuleFlaxDue reports whether a mule flax trip may start now: the source asks the mule and no
// back-off is running.
func MuleFlaxDue(source string, now, nextAsk time.Time) bool {
	return AsksMuleForFlax(source) && !now.Before(nextAsk)
}

// ClampFlaxKitSize bounds the flaxKitSize setting.
func ClampFlaxKitSize(n int) int {
	return max(MinFlaxKitSize, min(n, MaxFlaxKitSize))
}

// FlaxKitOutcome is what one flax kit came to.
type FlaxKitOutcome string

// The outcomes.
const (
	// FlaxKitGained: the bank holds flax again.
	FlaxKitGained FlaxKitOutcome = "gained"
	// FlaxKitEmpty: the mule said it holds none.
	FlaxKitEmpty FlaxKitOutcome = "empty"
	// FlaxKitDeferred: nothing arrived for a passing reason (its per-donor window, a queue, a
	// timeout, the mule away or on another world, the walk failed).
	FlaxKitDeferred FlaxKitOutcome = "deferred"
)

// DeclineMeansNoFlax reports whether the mule's reason says its bank has no flax at all (rather
// than a window, a queue or the allow list). The mule's words: orderplan's "none in the bank" and
// mulerunner's "cannot identify".
func DeclineMeansNoFlax(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "none in the bank") || strings.Contains(r, "cannot identify")
}

// ClassifyFlaxKit names a kit's outcome from the flax the bank held after it (the kit is only asked
// for once the bank held none, so any is the gain) and the mule's reason for the flax line.
func ClassifyFlaxKit(bankFlaxAfter int, reason string) FlaxKitOutcome {
	switch {
	case bankFlaxAfter > 0:
		return FlaxKitGained
	case DeclineMeansNoFlax(reason):
		return FlaxKitEmpty
	}
	return FlaxKitDeferred
}

// AfterFlaxKit is the back-off after a kit: none after a gain (and the fruitless count resets);
// otherwise base doubled for each fruitless kit in a row, capped at MaxFlaxKitBackoff, and a mule
// that holds no flax waits the cap at once. fruitless is the count before this kit.
func AfterFlaxKit(out FlaxKitOutcome, fruitless int, base time.Duration) (wait time.Duration, fruitlessAfter int) {
	if out == FlaxKitGained {
		return 0, 0
	}
	fruitlessAfter = fruitless + 1
	if out == FlaxKitEmpty {
		return MaxFlaxKitBackoff, fruitlessAfter
	}
	if base <= 0 {
		base = DefaultFlaxKitBackoffMinutes * time.Minute
	}
	wait = base
	for i := 1; i < fruitlessAfter && wait < MaxFlaxKitBackoff; i++ {
		wait *= 2
	}
	return min(wait, MaxFlaxKitBackoff), fruitlessAfter
}

// AtMulePost reports whether t is within MulePostRadius of the mule's post, on its level.
func AtMulePost(t, post tile.Tile) bool {
	return post.X > 0 && t.Level == post.Level && tile.Chebyshev(t, post) <= MulePostRadius
}

// DecidePhaseWithMule is DecidePhase for an account fed by the mule: past crafting 10, standing at
// the mule's post is the Seers loop too, so a stop or a relog during a flax trip or a hand-over at
// the post walks back to Seers rather than starting the boat road or pickpocketing for a fare.
// Every other source decides exactly as DecidePhase does.
func DecidePhaseWithMule(s freshstart.Snapshot, source string, post tile.Tile) freshstart.Phase {
	p := DecidePhase(s)
	if !AsksMuleForFlax(source) || s.Tile == nil || s.Crafting < BowStringLevel {
		return p
	}
	switch p {
	case freshstart.PhaseFund, freshstart.PhaseSkirt, freshstart.PhaseTravel:
		if AtMulePost(*s.Tile, post) {
			return freshstart.PhaseArrived
		}
	}
	return p
}

// HandoffStand is where the bow string hand-over meets the mule: at its post when the flax comes
// from the mule (it stands at its own post, Catherby for the mule), else at the Seers stand as before.
func HandoffStand(source string, post, seers tile.Tile) tile.Tile {
	if AsksMuleForFlax(source) && post.X > 0 {
		return post
	}
	return seers
}
