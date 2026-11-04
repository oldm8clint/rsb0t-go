package chopmanipplan

import (
	"sort"
	"strings"
)

// PlainLogs is the plain log's display name, the one species whose first fletching product is arrow shafts.
const PlainLogs = "Logs"

// ShortbowLevel is the Fletching level of the first product (the shortbow) of each log that is not plain, read out of
// content/scripts/skill_fletching/configs/cut_logs/cut_logs.dbrow (data=shortbow,...). The make menu offers the
// shortbow whatever the account's level, and the level test runs AFTER the choice (cut_logs.rs2), answering a product
// the account may not make with a message box and NO write of %action_delay, so a carrier whose shortbow the account
// cannot make would open a box every batch and move nothing.
var ShortbowLevel = map[string]int{
	"Oak logs":    20,
	"Willow logs": 35,
	"Maple logs":  50,
	"Yew logs":    65,
	"Magic logs":  80,
}

// Carrier is the log a batch's knife is used on, and the Make 1 button the batch presses.
type Carrier struct {
	// Logs is the log's display name.
	Logs string
	// MakeComp is the Make 1 component of the make menu that log opens.
	MakeComp int
}

// PickCarrier chooses the log to hold back from the pack for the knife, or false when none will do. held counts the
// un-noted logs in the pack by display name. Plain logs always work: the menu's first product is arrow shafts, which
// need Fletching 1. Any other log works only when the account can make its shortbow, and the cheapest such log (the
// lowest Fletching requirement) is chosen so the most valuable ones are never the ones a split batch could fletch.
func PickCarrier(held map[string]int, fletching int) (Carrier, bool) {
	var plain string
	type cand struct {
		name  string
		level int
	}
	var cands []cand
	for name, n := range held {
		if n <= 0 {
			continue
		}
		trimmed := strings.TrimSpace(name)
		if strings.EqualFold(trimmed, PlainLogs) {
			plain = name
			continue
		}
		for known, level := range ShortbowLevel {
			if strings.EqualFold(trimmed, known) && fletching >= level {
				cands = append(cands, cand{name: known, level: level})
			}
		}
	}
	if plain != "" {
		return Carrier{Logs: PlainLogs, MakeComp: Make1Shafts}, true
	}
	if len(cands) == 0 {
		return Carrier{}, false
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].level != cands[j].level {
			return cands[i].level < cands[j].level
		}
		return cands[i].name < cands[j].name
	})
	return Carrier{Logs: cands[0].name, MakeComp: Make1Bow}, true
}
