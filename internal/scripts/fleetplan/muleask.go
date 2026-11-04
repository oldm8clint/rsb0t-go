package fleetplan

import (
	"sort"
	"strings"
)

// Mule ask purposes, the keys of ProdProgress.MuleAsks.
const (
	MuleAskFund   = "fund"
	MuleAskFood   = "food"
	MuleAskStage2 = "stage2"
	MuleAskStage3 = "stage3"
	MuleAskStage5 = "stage5"
	MuleAskStaff  = "staff"
	// MuleAskStarter is the once-only starter kit (kqkitplan/starter.go, setting kitFromMule).
	MuleAskStarter = "starterkit"
	// MuleAskKQ is Stage 6's ask for the life spell's runes when Stage 3's supply loop has none of them.
	MuleAskKQ = "kqrunes"
)

// AskLine is one line of a mule ask, before it is checked against the mule's published bank.
type AskLine struct {
	// Choices is the item ladder, best first: a food ladder, a staff ladder, or one item.
	Choices []string
	// Count is how many are wanted.
	Count int
	// FairShare asks for FairShare(Count, stock) of whichever rung the mule holds (the owner's
	// "leave some for the fleet"); otherwise the ask is Count, capped at what the bank holds.
	FairShare bool
	// TryDown keeps every held rung, so that at the post a rung that does not arrive is followed
	// by the next one (the staff ladder). Otherwise only the first held rung is asked for.
	TryDown bool
	// Bulk (the food bulk ask) takes the rung the mule holds the MOST of and asks the full Count,
	// capped at that stock less its FleetReserveFraction reserve, instead of a FairShare of the
	// first rung held. Owner, 2026-09-26: "mule holds plenty of food for each account to get 1k
	// of something". Live 27 Sep 04:11 a FairShare of the first rung (Cake, 25,816 held) gave 244,
	// while the mule held 77,138 Bread, 47,960 Shark, 36,037 Bass (about 250,000 food in all).
	Bulk bool
	// Own says what the account's own bank may supply of this line before the mule is asked
	// (ownbank.go, PlanFromOwnBank). The zero value, OwnNone, consults nothing: a line already sized
	// against the bank (the runes, the coins) must keep it, or the bank would be subtracted twice.
	Own OwnCover
	// Rider marks a line that rides an ask that is going anyway and never starts a walk (the Strange
	// fruit). When the account's own bank answers every other line, the riders are dropped with them.
	Rider bool

	// Fill (the alch kit, a user request: "the best-paying items the mule's published bank holds")
	// takes the ladder's rungs in order, each one the mule holds in full up to what is still wanted,
	// until Count is covered: one kit line per rung (at most FillLinesMax), instead of one rung with all
	// of Count. With the mule's bank unpublished it asks the first rung for Count, as any line does.
	Fill bool
	// FewestFirst (the alchables) asks for the choices the mule holds the FEWEST of first, each up
	// to its whole stock, one plan entry each until Count is met (at most FewestFirstItems): owner,
	// 6 Oct 2026, "when pulling alch items from the mule we should take the items less in stock
	// first so the mule's bank doesnt fill up as much". A stack the fleet empties frees a bank slot.
	FewestFirst bool

	// BulkFood (bulkfood.go, a user request: "they should be requesting 100+") makes the line a bulk food
	// ask: Count is the bulk, Choices the cooked ladder best first, RawChoices the raw fish the account can
	// cook, best first (empty: none). ChooseBulkFood picks cooked food first, else raw fish, one kind where
	// it can; OwnFloor is the food items the account's own bank may hold before the ask is held back
	// (Own: OwnFoodBulk).
	BulkFood   bool
	RawChoices []string
	OwnFloor   int
	// RawPlan (the cook session, cooksession.go) is the raw-fish mix a BulkFood line asks for when it falls to
	// raw fish: PlanRawSession's lines, best tier first, so the ask is the same mix the account would cook
	// and not Count of one fish. nil keeps the old pool over RawChoices.
	RawPlan []ItemCount
}

// FillLinesMax is the most kit lines one Fill line makes: a noted line takes one pack slot and one
// offer on the trade screen, and the lowest-paying rungs are the ones a short pack drops anyway.
const FillLinesMax = 12

// FewestFirstItems bounds the items one FewestFirst line asks for: each is one pack slot (noted).
const FewestFirstItems = 4

// AskPlan is one line after the filter: the rungs to ask for, in order, each with its count.
type AskPlan struct {
	Line  AskLine
	Rungs []ItemCount
	// Raw marks a bulk food entry (AskLine.BulkFood) that asks for RAW fish to be cooked, and CookedHeld the
	// cooked food the mule could have given instead (ChooseBulkFood), for the log.
	Raw        bool
	CookedHeld int
}

// FilterAskByBank is step 1 of the one mule-ask entry point: what is still worth asking the mule
// for, judged from its PUBLISHED bank before anybody walks anywhere. stock is the mule's bank
// (internal/bot's muleBankCount), nil when it has not been published or is stale; then every line
// is asked blind, as before -- the first rung only (all rungs for TryDown), a fair-share line
// capped at BlindShare. With a published bank a rung the mule holds none of is dropped, and a line
// with no rung left is reported in none. An empty plan means: do not walk.
func FilterAskByBank(lines []AskLine, stock func(item string) (int, bool)) (plan []AskPlan, none []string) {
	for _, l := range lines {
		if l.Count <= 0 || len(l.Choices) == 0 {
			continue
		}
		p := AskPlan{Line: l}

		if l.BulkFood {
			ch := ChooseBulkFood(l, stock)
			if ch.Kind == BulkFoodNone {
				none = append(none, strings.Join(append(append([]string(nil), l.Choices...), l.RawChoices...), "/"))
				continue
			}
			for _, r := range ch.Rungs {
				plan = append(plan, AskPlan{Line: l, Rungs: []ItemCount{r}, Raw: ch.Kind == BulkFoodRaw, CookedHeld: ch.CookedHeld})
			}
			continue
		}

		if l.Fill && stock != nil {
			// Each rung the mule holds, best first, until Count is covered: a plan of its own apiece, so
			// the kit has a line for each (AskPlanKit takes the first rung of every plan).
			left, lines := l.Count, 0
			for _, c := range l.Choices {
				if left <= 0 || lines >= FillLinesMax {
					break
				}
				have, known := stock(c)
				if !known || have <= 0 {
					continue
				}
				n := min(have, left)
				plan = append(plan, AskPlan{Line: l, Rungs: []ItemCount{{Item: c, Count: n}}})
				left -= n
				lines++
			}
			if lines == 0 {
				none = append(none, strings.Join(l.Choices, "/"))
			}
			continue
		}

		if l.FewestFirst && stock != nil {
			if ps := fewestFirst(l, stock); len(ps) > 0 {
				plan = append(plan, ps...)
			} else {
				none = append(none, strings.Join(l.Choices, "/"))
			}
			continue
		}
		if l.Bulk && stock != nil {
			if c, n := bulkRung(l, stock); n > 0 {
				p.Rungs = append(p.Rungs, ItemCount{Item: c, Count: n})
				plan = append(plan, p)
			} else {
				none = append(none, strings.Join(l.Choices, "/"))
			}
			continue
		}
		for _, c := range l.Choices {
			n := l.Count
			if stock == nil {
				if l.FairShare {
					n = BlindShare(l.Count)
				}
			} else {
				have, known := stock(c)
				if !known || have <= 0 {
					continue
				}
				if l.FairShare {
					n = FairShare(l.Count, have, DefaultCohortShare)
				} else if n > have {
					n = have
				}
			}
			if n <= 0 {
				continue
			}
			p.Rungs = append(p.Rungs, ItemCount{Item: c, Count: n})
			if !l.TryDown {
				break
			}
		}
		if len(p.Rungs) == 0 {
			none = append(none, strings.Join(l.Choices, "/"))
			continue
		}
		plan = append(plan, p)
	}
	return plan, none
}

// fewestFirst is a FewestFirst line's plan: the choices the mule holds, fewest first (a tie keeps
// the line's order), each asked up to its whole stock until Count is met.
func fewestFirst(l AskLine, stock func(item string) (int, bool)) []AskPlan {
	type held struct {
		item string
		n    int
	}
	var hs []held
	for _, c := range l.Choices {
		if n, ok := stock(c); ok && n > 0 {
			hs = append(hs, held{c, n})
		}
	}
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].n < hs[j].n })
	var out []AskPlan
	left := l.Count
	for _, h := range hs {
		if left <= 0 || len(out) >= FewestFirstItems {
			break
		}
		n := min(left, h.n)
		out = append(out, AskPlan{Line: l, Rungs: []ItemCount{{Item: h.item, Count: n}}})
		left -= n
	}
	return out
}

// bulkRung is a Bulk line's rung: the choice the mule holds the most of (the first on a tie), and
// Count capped at that stock less its reserve.
func bulkRung(l AskLine, stock func(item string) (int, bool)) (string, int) {
	best, have := "", 0
	for _, c := range l.Choices {
		if n, ok := stock(c); ok && n > have {
			best, have = c, n
		}
	}
	n := min(l.Count, have-have/FleetReserveFraction)
	if best == "" || n <= 0 {
		return "", 0
	}
	return best, n
}

// AskPlanKit is the plan's first rung per line, the one-line log form of what will be asked for.
func AskPlanKit(plan []AskPlan) []ItemCount {
	out := make([]ItemCount, 0, len(plan))
	for _, p := range plan {
		out = append(out, p.Rungs[0])
	}
	return out
}
