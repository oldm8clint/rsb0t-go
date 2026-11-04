// Package antipoisonmakeplan is AntipoisonMaker's own decisions: what to do next given what the
// account's bank and pack hold. It is pure: no client, actions, fleet or bot imports and no I/O,
// so its tests run with no server.
//
// The script makes Antipoison(3) by Herblore and hands it to the fleet's mule, which supplies the
// Kalphite Queen fighters (scarabswarm_stage6.go asks the mule for antipoison once the mule's
// published bank shows some). Every fact below was read from the 289 content checkout:
//
//   - skill_herblore/configs/brewing: Marrentill (obj "marentill", 251) + Vial of water (227) makes
//     the Unfinished potion (marrentillvial, 93) at Herblore 5; that + Unicorn horn dust (235) makes
//     Antipoison(3) (175) for 37.5 XP, also Herblore 5. Both are one opheldu with no delay
//     (brew_potion.rs2), so a burst of single uses is a burst of potions.
//   - skill_herblore/scripts/grinding: Unicorn horn (237) used with a Pestle and mortar (233)
//     becomes one Unicorn horn dust, also with no delay.
//   - drop tables/scripts/unicorn.rs2: every Unicorn (npc 89) drops exactly one Unicorn horn.
//   - No Druidic Ritual check exists in the brewing script: any Herblore >= 5 account may brew.
//
// None of these items stacks, so a 28-slot pack makes 14 potions a batch (14 + 14).
package antipoisonmakeplan

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Object ids, from content/pack/obj.pack.
const (
	MarrentillID  int32 = 251
	VialWaterID   int32 = 227
	VialEmptyID   int32 = 229
	PestleID      int32 = 233
	HornDustID    int32 = 235
	HornID        int32 = 237
	UnfID         int32 = 93
	AntipoisonID  int32 = 175
	CoinsID       int32 = 995
	MarrentillAsk       = "Marrentill" // the name the mule is asked for
	AntipoisonAsk       = "Antipoison(3)"
)

// Levels and sizes.
const (
	MinLevel  = 5  // Herblore, for both brews
	PackSlots = 28 // a full pack
	Batch     = 14 // potions a batch: two items per potion fill the pack
)

// Places, all level 0.
var (
	// UnicornCentre is the middle of the unicorn field north of Catherby. The map spawns are
	// content/maps/m43_54.jm2 NPC section lines "0 31 10: 89" and "0 39 4: 89", i.e. (2783,3466)
	// and (2791,3460): open ground with no walls within four tiles of either, 25 tiles from the
	// Catherby bank where the mule posts. (The unicorns have wanderrange 17.)
	UnicornCentre = tile.Tile{X: 2787, Z: 3463}
	// UnicornLeash is how far from UnicornCentre a unicorn is still hunted.
	UnicornLeash = 16
	// JatixStand is Jatix's Herblore shop in Taverley (content taverly.inv: Pestle and mortar 3; Jatix
	// spawns at 2899,3427 in m45_53.jm2). Only the one-off pestle is bought there: from Catherby the walker
	// avoids White Wolf Mountain and sails Ardougne-Brimhaven-Port Sarim, about 8 minutes each way.
	JatixStand = tile.Tile{X: 2899, Z: 3430}
	// AemadStand is Aemad's Adventuring Supplies in East Ardougne (content ardougne_east.inv [adventurershop]:
	// Vial of water 500, restocking; Aemad spawns at 2613,3294 and Kortan at 2615,3292 in m40_51.jm2). Its vials
	// come filled, so no sink is needed, and Ardougne is on the walker's way out of Catherby.
	AemadStand = tile.Tile{X: 2614, Z: 3293}
)

// Shop names and walk limits.
const (
	Jatix       = "Jatix"
	Aemad       = "Aemad"
	Kortan      = "Kortan"
	CoinsPerRun = 300 // coins drawn for a shop run: 27 vials of water and a pestle cost well under this
	// JatixWalk bounds the walk to Taverley: on 1 Nov the 6-minute limit ran out 50 tiles short.
	JatixWalk = 15 * time.Minute
	AemadWalk = 8 * time.Minute
)

// Step is what the next Loop pass does.
type Step string

const (
	StepStop       Step = "stop"
	StepHandOff    Step = "hand-off"
	StepAntipoison Step = "antipoison"
	StepUnf        Step = "unf"
	StepGrind      Step = "grind"
	StepHunt       Step = "hunt"
	StepShop       Step = "shop"
	StepAskMule    Step = "ask-mule"
	StepWait       Step = "wait"
)

// Snapshot is what the bank and pack hold together, read at a bank with the pack deposited.
type Snapshot struct {
	Level      int
	LevelKnown bool

	Marrentill int
	VialWater  int
	VialEmpty  int
	Dust       int
	Horn       int
	Unf        int
	Antipoison int
	HasPestle  bool
	Coins      int
	PackFree   int
	HandOffAt  int
	// Back-offs and verdicts the bot keeps between passes.
	HandOffBackoff bool // a hand-off failed lately
	AskBackoff     bool // a mule ask failed lately
	ShopBackoff    bool // a shop run failed lately (no coins, shop unreachable)
	AskExhausted   bool // the mule has no Marrentill left after repeated asks
}

// Decision is Decide's answer.
type Decision struct {
	Step Step
	// N is the batch size for StepUnf/StepAntipoison/StepGrind, the horns to collect for StepHunt.
	N int
	// Why is a short reason for the log.
	Why string
}

// Decide picks the next step. Priorities, in order:
//
//  1. Herblore below 5: stop.
//  2. Antipoison held at or above handOffAt: hand it to the mule.
//  3. Unfinished potions and dust both on hand: finish potions (they are the XP and the product).
//  4. Spare dust, Marrentill and water vials: make unfinished potions, no more than the dust can
//     finish (an unfinished potion without dust is a slot spent on nothing).
//  5. Otherwise the bottleneck is dust: grind horns held (a pestle first), else hunt unicorns.
//  6. Spare dust but no Marrentill or no water: ask the mule / run to the shop.
//
// When every route is backed off, a partial load of Antipoison is handed over rather than idled on.
func Decide(s Snapshot) Decision {
	if !s.LevelKnown {
		return Decision{Step: StepWait, Why: "Herblore level not yet reported"}
	}
	if s.Level < MinLevel {
		return Decision{Step: StepStop, Why: fmt.Sprintf("Herblore %d is below %d: Antipoison cannot be brewed", s.Level, MinLevel)}
	}
	if s.HandOffAt > 0 && s.Antipoison >= s.HandOffAt && !s.HandOffBackoff {
		return Decision{Step: StepHandOff, N: min(s.Antipoison, PackSlots), Why: fmt.Sprintf("%d Antipoison on hand (hand off at %d)", s.Antipoison, s.HandOffAt)}
	}
	if s.Unf > 0 && s.Dust > 0 {
		return Decision{Step: StepAntipoison, N: min(Batch, s.Unf, s.Dust), Why: fmt.Sprintf("%d unfinished potions and %d dust", s.Unf, s.Dust)}
	}
	spareDust := s.Dust - s.Unf
	if spareDust > 0 {
		switch {
		case s.Marrentill > 0 && s.VialWater > 0:
			return Decision{Step: StepUnf, N: UnfBatch(s.Marrentill, s.VialWater, spareDust), Why: fmt.Sprintf("%d Marrentill, %d vials of water, %d spare dust", s.Marrentill, s.VialWater, spareDust)}
		case s.Marrentill == 0 && !s.AskBackoff && !s.AskExhausted:
			return Decision{Step: StepAskMule, Why: "no Marrentill in the bank"}
		case s.VialWater == 0 && !s.ShopBackoff:
			return Decision{Step: StepShop, Why: "no vials of water"}
		}
	} else {
		switch {
		case s.Horn > 0 && s.HasPestle:
			return Decision{Step: StepGrind, N: min(s.Horn, PackSlots-1), Why: fmt.Sprintf("%d horns to grind", s.Horn)}
		case s.Horn > 0 && !s.HasPestle && !s.ShopBackoff:
			return Decision{Step: StepShop, Why: "no pestle and mortar"}
		case s.Horn == 0 && !(s.Unf == 0 && s.Marrentill == 0 && s.AskExhausted):
			// Dust is what is missing. Hunting needs no other supplies, but it is pointless for ever
			// once the mule has no Marrentill and nothing is unfinished: that falls through below.
			return Decision{Step: StepHunt, N: HuntHorns(s.PackFree), Why: "no dust and no horns"}
		}
	}
	if s.Antipoison > 0 && !s.HandOffBackoff {
		return Decision{Step: StepHandOff, N: min(s.Antipoison, PackSlots), Why: fmt.Sprintf("nothing else to do: handing over the %d Antipoison made", s.Antipoison)}
	}
	if s.AskExhausted && s.Marrentill == 0 && s.Unf == 0 {
		return Decision{Step: StepStop, Why: "the mule has no Marrentill left and none is on hand"}
	}
	return Decision{Step: StepWait, Why: "every route is backing off"}
}

// UnfBatch is how many unfinished potions to make: bounded by the herbs, the water, the dust they
// will be finished with and a pack's worth of pairs.
func UnfBatch(marrentill, water, spareDust int) int {
	return max(0, min(Batch, marrentill, water, spareDust))
}

// HuntHorns is how many horns one hunt trip collects before it goes back to the bank: a pack's
// worth less the slot the pestle takes at the grinding, bounded by the free slots now.
func HuntHorns(packFree int) int {
	if packFree <= 0 || packFree > PackSlots {
		packFree = PackSlots
	}
	return min(packFree, PackSlots-1)
}

// AfterAsk updates the count of consecutive fruitless mule asks and says whether the mule is
// considered out of Marrentill (max <= 0 never retires it).
func AfterAsk(arrived bool, fruitless, max int) (newFruitless int, exhausted bool) {
	if arrived {
		return 0, false
	}
	fruitless++
	return fruitless, max > 0 && fruitless >= max
}

// ShopList is what a shop run buys: vials of water (Aemad's) up to the free slots less what is needed
// for the pestle, and the pestle (Jatix's) if missing.
type ShopList struct {
	Vials  int
	Pestle bool
}

// ShopFor sizes a shop run. freeSlots is the pack's free slots when it leaves the bank (coins take
// one); wantVials is the settings' vial count.
func ShopFor(freeSlots, wantVials int, hasPestle bool) ShopList {
	l := ShopList{Pestle: !hasPestle}
	room := freeSlots
	if l.Pestle {
		room--
	}
	l.Vials = max(0, min(wantVials, room))
	return l
}
