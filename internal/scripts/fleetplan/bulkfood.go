package fleetplan

import (
	"sort"
)

// Bulk food ask defaults (settings foodAskBulk and foodAskFloor).
const (
	// DefaultFoodAskBulk is how much food one ask is for (owner: "100+"), noted.
	DefaultFoodAskBulk = 150
	// MaxFoodAskBulk bounds the setting: a noted line is one pack slot, but the mule's own stock is finite.
	MaxFoodAskBulk = 2000
	// DefaultFoodAskFloor is the food items (or raw fish) the account's own bank may hold before the ask is
	// held back altogether.
	DefaultFoodAskFloor = 40
	// BulkFoodKindsMax bounds how many kinds one ask is made of when no single kind covers the bulk.
	BulkFoodKindsMax = 3
)

// BulkFoodKind is what a bulk food ask is made of.
type BulkFoodKind int

const (
	// BulkFoodNone: the mule's published bank holds neither cooked food nor raw fish this account can use.
	BulkFoodNone BulkFoodKind = iota
	// BulkFoodCooked: cooked food, off the line's Choices ladder.
	BulkFoodCooked
	// BulkFoodRaw: raw fish to cook, off the line's RawChoices ladder.
	BulkFoodRaw
)

// BulkFoodChoice is ChooseBulkFood's answer.
type BulkFoodChoice struct {
	Kind BulkFoodKind
	// Rungs is what to ask for, one entry per kind (at most BulkFoodKindsMax), their counts summing to Total.
	Rungs []ItemCount
	Total int
	// CookedHeld is the cooked food the mule could give (usable stock of the best cover of the ladder, up to
	// the bulk) -- what a raw choice was made in spite of.
	CookedHeld int
}

// ChooseBulkFood is the sizing decision of a BulkFood line: cooked food first, then the best raw fish the
// account can cook, then nothing. The bulk is l.Count. stock is the mule's published bank (the plan's, less
// what other accounts have claimed), nil when it is unpublished: the line is then asked blind, the first
// cooked rung (the first raw one when the account has no cooked ladder) for the whole bulk, which the mule
// caps at what it holds.
func ChooseBulkFood(l AskLine, stock func(item string) (int, bool)) BulkFoodChoice {
	want := max(l.Count, 1)
	if stock == nil {
		switch {
		case len(l.Choices) > 0:
			return BulkFoodChoice{Kind: BulkFoodCooked, Rungs: []ItemCount{{Item: l.Choices[0], Count: want}}, Total: want}
		case len(l.RawPlan) > 0:
			rungs, total := rawPlanRungs(l.RawPlan, nil)
			return BulkFoodChoice{Kind: BulkFoodRaw, Rungs: rungs, Total: total}
		case len(l.RawChoices) > 0:
			return BulkFoodChoice{Kind: BulkFoodRaw, Rungs: []ItemCount{{Item: l.RawChoices[0], Count: want}}, Total: want}
		}
		return BulkFoodChoice{}
	}
	cooked, ct := bulkPool(l.Choices, want, stock)
	raw, rt := bulkPool(l.RawChoices, want, stock)
	if len(l.RawPlan) > 0 {
		raw, rt = rawPlanRungs(l.RawPlan, stock)
	}
	switch {
	case ct >= want:
		return BulkFoodChoice{Kind: BulkFoodCooked, Rungs: cooked, Total: ct, CookedHeld: ct}
	case rt >= want:
		return BulkFoodChoice{Kind: BulkFoodRaw, Rungs: raw, Total: rt, CookedHeld: ct}
	case ct <= 0 && rt <= 0:
		return BulkFoodChoice{}
	case rt > ct:
		return BulkFoodChoice{Kind: BulkFoodRaw, Rungs: raw, Total: rt, CookedHeld: ct}
	}
	return BulkFoodChoice{Kind: BulkFoodCooked, Rungs: cooked, Total: ct, CookedHeld: ct}
}

// rawPlanRungs is a BulkFood line's RawPlan as rungs: every line of the plan in its order (biggest first),
// each cut to what the mule's usable stock holds, and dropped when it holds none. stock nil: the plan as it is.
func rawPlanRungs(plan []ItemCount, stock func(item string) (int, bool)) (rungs []ItemCount, total int) {
	for _, r := range plan {
		n := r.Count
		if stock != nil {
			have, ok := stock(r.Item)
			if !ok {
				continue
			}
			n = min(n, UsableStock(have))
		}
		if n <= 0 {
			continue
		}
		rungs = append(rungs, ItemCount{Item: r.Item, Count: n})
		total += n
	}
	return rungs, total
}

// UsableStock is what an ask may take of a stock: the stock less its fleet reserve.
func UsableStock(have int) int { return have - have/FleetReserveFraction }

// bulkPool is the cover of want from one ladder: the first rung (ladder order, which is the quality
// order) whose usable stock holds all of want, alone; failing that the fewest kinds that together hold it
// (the deepest stocks first, at most BulkFoodKindsMax, ties in ladder order), each taken in full up to what
// is still wanted. total is what the rungs add up to; under want when the whole ladder holds less.
func bulkPool(ladder []string, want int, stock func(item string) (int, bool)) (rungs []ItemCount, total int) {
	type held struct {
		item string
		n    int
	}
	var hs []held
	for _, c := range ladder {
		have, ok := stock(c)
		if !ok {
			continue
		}
		if n := UsableStock(have); n > 0 {
			hs = append(hs, held{c, n})
		}
	}
	for _, h := range hs {
		if h.n >= want {
			return []ItemCount{{Item: h.item, Count: want}}, want
		}
	}
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].n > hs[j].n })
	left := want
	for _, h := range hs {
		if left <= 0 || len(rungs) >= BulkFoodKindsMax {
			break
		}
		n := min(h.n, left)
		rungs = append(rungs, ItemCount{Item: h.item, Count: n})
		total += n
		left -= n
	}
	return rungs, total
}
