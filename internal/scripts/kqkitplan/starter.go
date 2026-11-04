package kqkitplan

// THE STARTER KIT (a user request), which replaces the tiered KQ kit as the hand-out: ONE mule
// (the mule); each account receives it exactly ONCE -- 1,000 Chaos runes to train with, 500 food of any
// kind (the most plentiful first) and one staff of any sort -- and afterwards tops up only from its
// own bank and earnings, not the mule (the existing ask paths stay for genuinely missing essentials).
//
// Stage 6 stays off (owner: until accounts have the protection prayers); this is training supply only.
//
// What makes "exactly once" hold is the persisted mark (scarabplan.ProdProgress.StarterKitAt, the
// account's own ask store): StarterDue reads it, and the runner writes it the moment anything of the
// kit arrives. An account that already holds the kit (a hand-over of its own, a consolidated bank) gets
// no lines and is marked without a trip.
//
// THROUGHPUT, from the mule's own numbers (docs and internal/bot/mule_kit.go): one bundled kit trade is
// about kitServeGuess (45 s) of the mule's time, so 80 an hour if it did nothing else; the open-stock
// window caps what it may give of one item to everyone combined (anyPerWindow 30,000 per 30-minute
// window on the mule), and a kit carries 1,000 chaos, so the window admits 30 kits a half hour -- 60 an
// hour. KitsPerHour says so, and the fleet of ~329 accounts is about five and a half hours of mule.

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// The starter kit's numbers.
const (
	StarterChaos = 1000
	StarterFood  = 500
)

// StarterInput is one account's starter-kit decision.
type StarterInput struct {
	// Delivered is the persisted mark: the kit has been handed over once.
	Delivered bool
	// Spell is what the account casts; its staff ladder orders the staff ask.
	Spell fleetplan.Spell
	// Staff is the autocast staff the account holds anywhere ("" for none); ChaosHeld the chaos
	// carried and banked; FoodHeld the food items carried and banked, of any kind.
	Staff     string
	ChaosHeld int
	FoodHeld  int
	// MaxHP sizes the food ladder (a heal above it is asked last).
	MaxHP int
}

// StarterLines is the mule ask: only what the account is short of, as scarabplan.AskLines the entry
// point filters by the mule's published bank (a staff the mule has none of is dropped there, and the
// account's own Zaff path buys one). Nil when delivered or already holding the whole kit.
func StarterLines(in StarterInput) []fleetplan.AskLine {
	if in.Delivered {
		return nil
	}
	var lines []fleetplan.AskLine
	if in.Staff == "" {
		lines = append(lines, fleetplan.AskLine{Choices: fleetplan.StaffPreference(in.Spell), Count: 1, TryDown: true})
	}
	if short := StarterChaos - max(0, in.ChaosHeld); short > 0 {
		lines = append(lines, fleetplan.AskLine{Choices: []string{fleetplan.ChaosRune}, Count: short})
	}
	if short := StarterFood - max(0, in.FoodHeld); short > 0 {
		// Bulk takes whichever food the mule holds the MOST of and asks the whole count, as notes: the
		// cheapest, most plentiful food, not the biggest heal (owner: "cheapest/most plentiful first").
		lines = append(lines, fleetplan.AskLine{Choices: fleetplan.FoodLadderFor(max(1, in.MaxHP)), Count: short, Bulk: true})
	}
	return lines
}

// StarterDue reports whether the account should go for its kit now, and why not. stageOK: Stage 3 or
// later and not in a Stage 6 life; safe: standing somewhere the walk to the mule is a plain land walk
// (not the Wilderness, the lair, a trip); tries and maxTries bound the attempts that brought nothing.
func StarterDue(in StarterInput, stageOK, safe bool, tries, maxTries int) (bool, string) {
	switch {
	case in.Delivered:
		return false, "the starter kit was delivered"
	case !stageOK:
		return false, "not in a stage that trains"
	case !safe:
		return false, "not somewhere safe to walk from"
	case maxTries > 0 && tries >= maxTries:
		return false, "the starter kit asks brought nothing " + itoa(tries) + " times; left to the ordinary asks"
	}
	if len(StarterLines(in)) == 0 {
		return false, "the account already holds the whole kit"
	}
	return true, ""
}

// KitsPerHour is how many starter kits the mule can hand out in an hour: the lesser of its serving
// time and its open-stock window for the kit's biggest item (chaos). trade is the time one bundled
// kit costs the mule; windowItem the most of one item it gives everyone per window; window that
// window's length.
func KitsPerHour(trade time.Duration, windowItem int, window time.Duration) int {
	if trade <= 0 || window <= 0 {
		return 0
	}
	byTime := int(time.Hour / trade)
	perWindow := windowItem / StarterChaos
	byWindow := perWindow * int(time.Hour/window)
	if byWindow < byTime {
		return byWindow
	}
	return byTime
}

// StarterMarked reports whether got (what arrived) is enough to mark the kit delivered: anything of
// it. The remainder (a staff the mule lacked, food it could not cook) is the ordinary asks' to supply.
func StarterMarked(got map[string]int) bool {
	for name, n := range got {
		if n > 0 && strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}
