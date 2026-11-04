package consolidateplan

// THE HAND-OVER THAT RIDES A KIT ASK (a user request: "hand the bank to the mule on every kit ask").
// An account that asks the mule for a kit walks to its post and stands at the bank beside it anyway; the
// 24-hour deposit trip (consolidate.go) is a second walk of the same 400 tiles for the same bank. So a
// kit ask (every stage from 2 up) also hands over what the trip would: the surplus coins, runes and food over the
// account's keep, and the loot, drawn as bank notes, in one more trade before the kit order.
//
// WHAT STAYS, besides everything the deposit trip already keeps (KeepFor, Classify: the training
// runes, food keep, staffs, worn gear and its upgrades, tools, quest items, the alch stock the account
// casts on): every item the kit ask itself names, since handing a rune over and asking it back in the
// same visit is a pointless pair of trades; and the rub jewellery (Games necklace, Ring of dueling),
// which the run staller needs in the pack on every walk (scarabplan.IsRubItem; Classify keeps it too).
//
// WHY THE MINIMUMS ARE LOWER than the trip's. The deposit trip must justify a 400-tile walk, so it wants
// 100,000 coins or 15 rows of loot. The kit ask is walking anyway, and the cost of the hand-over is one
// trade of about 40 seconds at the mule, so a smaller surplus is worth it. The minimums here are the
// trip's, capped at these.
//
// Pure.

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// Kit-ask hand-over minimums: a surplus smaller than these is not worth a trade at the mule.
const (
	KitAskMinCoins     = 20_000
	KitAskMinRunes     = 100
	KitAskMinFood      = 10
	KitAskMinLootValue = 5_000
	KitAskMinLootRows  = 5
	// KitAskEarlyReserve is the least coins a Stage 2 or 3 account keeps on a kit ask: it pays ship fares
	// and shop prices out of its purse. Stage 5 keeps consolidateCoinsReserve as it always did.
	KitAskEarlyReserve = 5_000
)

// KitAskInput is the kit ask the hand-over would ride.
type KitAskInput struct {
	// Stage is the ScarabSwarm stage. Every stage from 2 up hands over on a kit ask (scarabqueue2-1007; the
	// a user request: "all accounts"), where the deposit trip (Due) stays Stage 5 only. Stages 2 and 3
	// are still buying (fares, shops, a staff, market chaos), so they keep at least KitAskEarlyReserve coins
	// whatever consolidateCoinsReserve says; the rest of the keep (food, the casting runes, the chaos
	// earmark, gear, the alch stock, the rub jewellery) is the same for every stage.
	Stage int
	// Purpose is the ask's purpose (scarabplan.MuleAsk*).
	Purpose string
	// Kit is every item name the ask names, rungs included.
	Kit []string
	// Magic is the account's Magic level, 0 when unknown. Under NatureCutoff the account hands over ALL its
	// Nature runes (Settings.AllNatures): it cannot cast High Level Alchemy, so they are the fleet's to
	// spend (a user request: Natures are the bottleneck of every alch loop).
	Magic int
}

// NatureCutoff is the Magic level that opens High Level Alchemy (scarabplan.HighLevelAlchemy).
const NatureCutoff = 55

// HandsNatures reports whether an account at this Magic level hands every Nature rune over.
func HandsNatures(magic int) bool { return magic > 0 && magic < NatureCutoff }

// DropOff is the Settings an endgame account's periodic drop-off to the mule plans with: the account's own,
// with the least time between two trips set to every (endgameDropOffHours) and the minimums the kit ask's
// hand-over uses, since a trip every few hours carries less than one a day. The keep rules (food, the casting
// and walk-alch runes, gear, the alch stock, the rub jewellery) are the account's own, as in the daily trip.
func DropOff(s Settings, every time.Duration) Settings {
	s = s.Normalised()
	if every >= time.Hour {
		s.Every = every
	}
	s.MinCoins = min(s.MinCoins, KitAskMinCoins)
	s.MinRunes = min(s.MinRunes, KitAskMinRunes)
	s.MinFood = min(s.MinFood, KitAskMinFood)
	s.MinLootValue = min(s.MinLootValue, KitAskMinLootValue)
	s.MinLootRows = min(s.MinLootRows, KitAskMinLootRows)
	return s
}

// KitAskHandOver is the Settings the hand-over that rides this kit ask plans with, and whether there is
// one: the account's own Settings with the kit's names kept and the minimums lowered. The reason is
// given when it is not made.
func KitAskHandOver(s Settings, in KitAskInput) (Settings, bool, string) {
	s = s.Normalised()
	switch {
	case !s.Enabled:
		return s, false, "consolidateToMule is off"
	case !s.OnKitAsk:
		return s, false, "consolidateOnKitAsk is off"
	case in.Stage < fleetplan.StageFoodAndFunds:
		return s, false, "a Stage 1 account has nothing to hand over"
	case in.Purpose == fleetplan.MuleAskFoodHungry:
		return s, false, "a hungry account's ask is not delayed"
	case in.Purpose == fleetplan.MuleAskStarter:
		return s, false, "the once-only starter kit is asked on its own"
	case len(in.Kit) == 0:
		return s, false, "nothing is asked for"
	}
	if in.Stage < fleetplan.StageSurvivalCombat {
		s.CoinsReserve = max(s.CoinsReserve, KitAskEarlyReserve)
	}
	s.MinCoins = min(s.MinCoins, KitAskMinCoins)
	s.MinRunes = min(s.MinRunes, KitAskMinRunes)
	s.MinFood = min(s.MinFood, KitAskMinFood)
	s.MinLootValue = min(s.MinLootValue, KitAskMinLootValue)
	s.MinLootRows = min(s.MinLootRows, KitAskMinLootRows)
	s.AllNatures = HandsNatures(in.Magic)
	keep := make([]string, 0, len(s.KeepItems)+len(in.Kit))
	for _, n := range s.KeepItems {
		if s.AllNatures && strings.EqualFold(strings.TrimSpace(n), NatureRune) {
			continue // a Nature rune on the keep list does not hold it back
		}
		keep = append(keep, n)
	}
	for _, n := range in.Kit {
		if s.AllNatures && strings.EqualFold(strings.TrimSpace(n), NatureRune) {
			continue // asked back too? The rule is all of them; the order that follows is the mule's to refuse
		}
		if n = strings.TrimSpace(n); n != "" && !kept(keep, n) {
			keep = append(keep, n)
		}
	}
	s.KeepItems = keep
	return s, true, ""
}
