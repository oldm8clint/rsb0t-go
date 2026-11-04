package wcfletchplan

// The chop manipulation's carrier log, and how ProgressiveWCFletch keeps one.
//
// The knife-on-log batch (internal/scripts/chopmanipplan) needs one log in the pack to put the knife on. The owner, 6 October
// 2026: "it never took a log to tick manip with instead it chopped a yew then started to tick manip ... we should always
// take a log or chop a near by tree for a log so we can always tick manip on the yew tree." A yew pays a log every
// 10 to 20 seconds, so an account that starts every trip without a carrier chops its first yew swings plain, and a fletch
// step that cuts "all the logs" eats the carrier the moment it has one.
//
// So the carrier is kept: the fletch step cuts every log but one (CarrierReserve), the bank gives one back after the
// deposit (CarrierFromBank), and an account with none and a plain tree beside its camp cuts one log from that tree first
// (NearestPlainPatch). Any log will do as a carrier as long as the account's Fletching level lets Make 1 of the first
// product go through (chopmanipplan.PickCarrier), a plain log best of all because the fletch step may then cut every
// log of the rung.

import (
	"strings"

	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/tile"
)

// CarrierReserve is how many of the rung's logs the fletch step must leave in the pack: 1 when the carrier the knife would
// use is one of them, 0 when the account has no usable carrier (nothing to keep back) or its carrier is another log
// (a plain log while the rung chops oaks: every oak log may be cut). held counts the un-noted logs in the pack by name.
// With the manipulation off nothing is reserved.
func CarrierReserve(manip bool, held map[string]int, rungLogs string, fletching int) int {
	if !manip {
		return 0
	}
	car, ok := chopmanipplan.PickCarrier(held, fletching)
	if !ok {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(car.Logs), strings.TrimSpace(rungLogs)) {
		return 1
	}
	return 0
}

// NeedsCarrier reports whether the account has no log the batch could use.
func NeedsCarrier(manip bool, held map[string]int, fletching int) bool {
	if !manip {
		return false
	}
	_, ok := chopmanipplan.PickCarrier(held, fletching)
	return !ok
}

// CarrierFromBank is the log to take out of the bank, one of it, when the pack holds none the batch could use: a plain log if
// the bank has one, else the cheapest log the account's Fletching level allows. ok is false when the pack already has a
// carrier, the manipulation is off, or the bank has nothing usable.
func CarrierFromBank(manip bool, pack, bank map[string]int, fletching int) (name string, ok bool) {
	if !manip || !NeedsCarrier(manip, pack, fletching) {
		return "", false
	}
	car, ok := chopmanipplan.PickCarrier(bank, fletching)
	if !ok {
		return "", false
	}
	return car.Logs, true
}

// CarrierPatchReach is how far from the account a plain-tree patch may be for a chop there to be worth a log: about a dozen
// tiles, the distance between the oak camp and the Seers normal trees, and between the church yews and the dead trees
// beside them (they share an anchor).
const CarrierPatchReach = 12

// NearestPlainPatch is the plain-tree patch (a Tree or Dead tree patch of NormalPatches) whose anchor is nearest to here
// and within CarrierPatchReach of it, on the same level. ok is false when none is that near: the account then waits for its
// own tier's first log, as before.
func NearestPlainPatch(here tile.Tile) (Patch, bool) {
	best, bestDist := -1, 0
	for i, p := range NormalPatches {
		if p.Anchor.Level != here.Level {
			continue
		}
		d := tile.Chebyshev(here, p.Anchor)
		if d > CarrierPatchReach {
			continue
		}
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return Patch{}, false
	}
	return NormalPatches[best], true
}
