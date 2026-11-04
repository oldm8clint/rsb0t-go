package fisherplan

import (
	"math"
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/wcfletchplan"
)

// FLETCHING TO 41 FOR THE PEARL TIPS (a user request): a pearl is only cut into bolt tips at Fletching
// 41 (pearlplan.Level), and every BassOysterCaskets account is at Fletching 1, so
//
//	"they could also include bringing a chisel with them and getting 41 fletching from provided logs from
//	the mule ... should use logs, oak logs, willow logs, making arrow shafts with logs, and unstrung bows
//	with the rest"
//
// This file is the pure half: what to cut at each level, and how many logs of each kind the whole climb
// takes. The cutting is the bot's (internal/bot/bassoyster_fletch.go), which runs wcfletchrunner.Fletch
// over a bank load of logs round after round.
//
// EVERY NUMBER IS DERIVED, not typed: the products, their levels and their experience are
// wcfletchplan.Ladder's, which is read from content 289's cut_logs.dbrow through internal/bankxp (plain
// Logs: arrow shafts at 1 for 7.5 xp a log; oak shortbow 20 for 16.5 and longbow 25 for 25; willow
// shortbow 35 for 33.3 and longbow 40 for 41.5). Plain logs only ever make arrow shafts here, as the owner
// said: wcfletchplan.NormalShafts, which also stacks fifteen to a slot where a bow takes a whole one.
//
// THE CLIMB, from nothing: about 600 plain logs (arrow shafts) to level 20, about 800 oak logs to 35 (the
// shortbow to 25, the longbow after) and about 540 willow logs to 41: 1,945 logs, a bit over an hour of
// cutting at two ticks a log (content's [queue,fletch_logs]), once per account.

// FletchGoal is the Fletching level the training takes an account to: the level a pearl is cut at.
const FletchGoal = pearlplan.Level

// KnifeName is the fletching tool: carried only while the training runs (the mule hands one out).
const KnifeName = "Knife"

// FletchLogNames is the display names of the logs the training uses, plain logs first.
func FletchLogNames() []string {
	out := make([]string, 0, fletchRungs)
	for idx := 0; idx < fletchRungs; idx++ {
		out = append(out, wcfletchplan.Ladder[idx].LogsName)
	}
	return out
}

// IsFletchLog reports whether name is one of FletchLogNames, ignoring case and spaces at the ends.
func IsFletchLog(name string) bool { return hasName(FletchLogNames(), strings.TrimSpace(name)) }

// fletchRungs is how many ladder rungs the training uses: plain logs (0), oak (1) and willow (2). Maple
// and above are not part of the owner's list and not the mule's to spare.
const fletchRungs = 3

// FletchChoice is one thing a kind of log can be cut into at some level.
type FletchChoice struct {
	// LogsName is the logs' display name ("Logs", "Oak logs", "Willow logs"), LogsID their obj id.
	LogsName string
	LogsID   int
	// Product is what the make menu is asked for.
	Product wcfletchplan.Product
}

// XPPerLog is the experience one log of this choice gives.
func (c FletchChoice) XPPerLog() float64 { return c.Product.XP() }

// FletchChoices is what each of the three logs can be cut into at this Fletching level (nothing, for a
// log whose first bow is above it), the most experience a log first and, on a tie, the cheaper log.
func FletchChoices(level int) []FletchChoice {
	var out []FletchChoice
	for idx := 0; idx < fletchRungs; idx++ {
		p, ok := wcfletchplan.Choose(idx, level, wcfletchplan.NormalShafts)
		if !ok {
			continue
		}
		r := wcfletchplan.Ladder[idx]
		out = append(out, FletchChoice{LogsName: r.LogsName, LogsID: r.LogsID, Product: p})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].XPPerLog() > out[j].XPPerLog() })
	return out
}

// ChooseFletch is the best choice the stock can feed: the first of FletchChoices whose logs the stock
// holds some of. stock answers how many of a kind (by display name) the account holds, banked and carried.
func ChooseFletch(level int, stock func(logs string) int) (FletchChoice, bool) {
	for _, c := range FletchChoices(level) {
		if stock(c.LogsName) > 0 {
			return c, true
		}
	}
	return FletchChoice{}, false
}

// FletchDone reports whether the training is over at this level.
func FletchDone(level int) bool { return level >= FletchGoal }

// FletchNeeds is the logs of each kind (by display name) an account at this much Fletching experience
// needs to reach FletchGoal, cutting the best choice at every level with the stock unlimited. It steps a
// level at a time, so it overshoots each level boundary by less than one log.
func FletchNeeds(xp int) map[string]int {
	needs := map[string]int{}
	goal := levels.XPAtLevel(FletchGoal)
	for xp < goal {
		level := levels.LevelFromXP(xp)
		choices := FletchChoices(level)
		if len(choices) == 0 {
			break
		}
		best := choices[0]
		next := min(levels.XPAtLevel(level+1), goal)
		logs := int(math.Ceil(float64(next-xp) / best.XPPerLog()))
		needs[best.LogsName] += max(logs, 1)
		xp = next
	}
	return needs
}

// fletchAskMargin and fletchAskSlack size an ask a little over the need: a batch interrupted by a level-up
// dialogue or a random event leaves a log or two uncut, and a cut that lands a point short must not leave
// the account under 41 with an empty bank.
const (
	fletchAskMarginPercent = 3
	fletchAskSlack         = 10
)

// FletchAsk is how many logs of each kind to ask the mule for: what FletchNeeds says less what the
// account already holds (banked and carried, by display name), with a margin, and nothing for a kind it
// holds enough of.
func FletchAsk(xp int, held func(logs string) int) map[string]int {
	out := map[string]int{}
	for name, need := range FletchNeeds(xp) {
		want := need + need*fletchAskMarginPercent/100 + fletchAskSlack - max(held(name), 0)
		if want > 0 {
			out[name] = want
		}
	}
	return out
}
