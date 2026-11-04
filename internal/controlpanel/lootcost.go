package controlpanel

import (
	"strings"

	"rsb0t-go/internal/loot"
)

// Pricing what a loot source CONSUMED, so the Loot panel reports profit rather than turnover
// (owner, 2026-09-21: "loot panel should consider the cost of a log or any other fletching
// material when its putting a profit price or gp earned through fletching", and "crystal chest
// loot total should consider it costs 1 crystal key to open the chest").
//
// internal/loot/cost.go says WHAT each source consumes and how many per event, and argues the
// omissions. This file does the one thing that package must not: ask the market what it is
// worth.
//
// THE RULE THAT MATTERS HERE IS "UNKNOWN IS NOT ZERO", which is marketprice.go's own first rule
// and applies with more force to a cost than to a value. A drop with no price shows as worth
// nothing, which understates it; a COST with no price, treated as zero, would claim the input
// was free and inflate the profit it is supposed to correct. A Crystal key is exactly the item
// that can hit this: its trades can fail MarketPrice.Coherent (a p75 more than ten times its
// p25), and the PriceBook then declines to answer for it. So the cost is reported as unknown,
// nothing is subtracted, and the panel says the figure is gross for that card rather than
// showing a net number that is really a gross one.

// costUnit resolves what one unit of a consumed input is worth, and whether the answer can be
// trusted enough to subtract.
//
// It asks with a cache cost of ZERO on purpose. A loot.Cost names an item the account spent, not
// one it received, so there is no loot.Item carrying the cache's own unit cost to pass in — and
// inventing one would smuggle a shop price in under the market's name. With no cache cost,
// PriceBook.Value falls through market, listing and (uncorroborated) trade, and then has nothing
// left: HighAlchOf(0) is 0 and the cost fallback is 0 too. A resolved unit of 0 is therefore
// exactly "the book has nothing for this name", which is the case to report rather than round.
func costUnit(prices *PriceBook, name string) (int64, bool) {
	if name == "" {
		return 0, false
	}
	unit, from := prices.Value(name, 0)
	if unit <= 0 {
		return 0, false
	}
	// AND THEN UNDO ONE THING Value DOES ON PURPOSE FOR VALUES AND MUST NOT DO FOR COSTS.
	//
	// Value has a last-resort branch: "a single uncorroborated trade is still better than
	// nothing when the item has a cache cost of zero", because an item that would otherwise read
	// as worthless is worse than one priced from thin evidence. Passing a cache cost of zero —
	// which a cost has no choice but to do, see above — arms that branch for every input, and it
	// fires precisely for the prices the book had just REFUSED: the first test of this written
	// got 5,000 gp for a Crystal key whose own trades spanned 200 to 90,000 and had already
	// failed MarketPrice.Coherent.
	//
	// For a value that trade-off is defensible. For a cost it is not: an over-confident input
	// price is subtracted from real earnings and makes a profitable source look like a loss. So
	// a market-sourced cost is re-checked against the same two gates Value applies before its
	// escape hatch, and an input that fails them is unknown — which is what the panel then says.
	// Only ValueFromMarket. ValueFromParts is deliberately trusted as it stands: it is the one
	// answer a Crystal key gets, since the key itself has never traded on this market while its
	// two halves trade constantly, and madeOf already applies these same two gates to the
	// COMPONENT price before multiplying it up. Re-checking the key's own (absent) record here
	// would throw that away and make every chest open unpriceable again.
	if from == ValueFromMarket {
		m, ok := prices.Lookup(name)
		if !ok || m.Trades < minConfidentTrades || !m.Coherent() {
			return 0, false
		}
	}
	return unit, true
}

// sourceCost is what one account's record of one source consumed, in coins — 0 both when the
// source consumes nothing and when its input has no trustworthy price. The two are told apart on
// the per-source view (LootSourceView.CostKnown); for the account and fleet totals they are the
// same thing, because in both cases there is nothing defensible to subtract.
func sourceCost(prices *PriceBook, src loot.Source, events int64, credited creditBook) int64 {
	if events <= 0 {
		return 0
	}
	total, _, known := inputsCost(prices, src, events, credited)
	if !known {
		return 0
	}
	return total
}

// spentCost prices measured spends (loot.Drop.Spent), known only when every one has a trustworthy
// price, for the same reason inputsCost's is.
func spentCost(prices *PriceBook, src loot.Source, spent []loot.Item, credited creditBook) (int64, bool) {
	if len(spent) == 0 {
		return 0, false
	}
	var total int64
	for _, it := range spent {
		unit, ok := spentUnit(prices, src, it, credited)
		if !ok {
			return 0, false
		}
		total += it.Count * unit
	}
	return total, true
}

// spentUnit is what one unit of a measured spend is worth. A rune is priced as every cost is
// (costUnit: the market's coherent price or nothing). A food or a drink (loot.KindConsumable) is
// priced as loot is, market then listing then alchemy then its cache cost, because eating a Cake the
// market has never traded still cost the fleet a cake and "unknown" would leave the Consumables
// card's whole total off; and a potion's form is priced at one dose (loot.Doses), because a drink
// uses a quarter of a Strength potion(4), not the four doses.
func spentUnit(prices *PriceBook, src loot.Source, it loot.Item, credited creditBook) (int64, bool) {
	// The coins a shop purchase paid (loot.KindShop) are worth exactly what they say.
	if it.ID == loot.CoinsID {
		return 1, true
	}
	if src.Kind != loot.KindConsumable {
		return costUnitCredited(prices, it.Name, credited)
	}
	unit, _ := prices.Value(it.Name, it.Value)
	if unit <= 0 {
		return 0, false
	}
	if doses := int64(loot.Doses(it.Name)); doses > 1 {
		unit /= doses
	}
	// A food eaten a bite at a time (a cake is three bites) costs the whole food at its first bite and
	// nothing after (loot.LaterBite; a user request: only the Cake's price counts).
	if loot.LaterBite(it.Name) {
		return 0, true
	}
	return unit, true
}

// spentLabel names the spends for the card's "net of" line ("Fire rune + Air rune").
func spentLabel(spent []loot.Item) string {
	names := make([]string, 0, len(spent))
	for _, it := range spent {
		names = append(names, it.Name)
	}
	return strings.Join(names, " + ")
}

// inputsCost prices every input one record of src consumed (loot.Source.Inputs: a tip put on a
// bolt uses the tip AND the Bolts). known is false when the source consumes nothing or when ANY
// input has no trustworthy price: a half-priced cost is the same lie as an unpriced one, a net
// figure that is really partly gross, so nothing is subtracted and the panel says gross. label
// names the inputs ("Pearl bolttips + Bolts") for the card's "net of" line.
func inputsCost(prices *PriceBook, src loot.Source, events int64, credited creditBook) (total int64, label string, known bool) {
	ins := src.Inputs()
	if len(ins) == 0 {
		return 0, "", false
	}
	known = true
	names := make([]string, 0, len(ins))
	for _, c := range ins {
		names = append(names, c.Name)
		unit, ok := costUnitCredited(prices, c.Name, credited)
		if !ok {
			known = false
			continue
		}
		total += events * c.Per * unit
	}
	if !known {
		total = 0
	}
	return total, strings.Join(names, " + "), known
}

// lootUnit is what one unit of a recorded item is worth to the Loot panel: the market's own price
// (PriceBook.Value), except for an unopened container, which is worth nothing because what it pays is
// counted when it is opened (loot.IsUnvaluedContainer).
func lootUnit(prices *PriceBook, it loot.Item) int64 {
	if loot.IsUnvaluedContainer(it.ID) {
		return 0
	}
	unit, _ := prices.Value(it.Name, it.Value)
	return unit
}

// creditBook is, for every item the Loot panel recorded ARRIVING (lowercased name), the cache cost it
// arrived with: the one thing PriceBook.Value needs to price it exactly as its arrival was priced.
type creditBook map[string]int64

// creditedItems collects the creditBook of one period's records.
func creditedItems(m map[string]map[string]*lootRec) creditBook {
	out := creditBook{}
	for _, sources := range m {
		for _, rec := range sources {
			// A strung bow made by stringing is not an arrival of a bow: it is the unstrung bow changing,
			// and counting it would make every stringing row match its own product.
			if strings.HasSuffix(rec.Source.Name, loot.UnstrungSuffix) {
				continue
			}
			for _, it := range rec.Items {
				if it.Name == "" || it.Count <= 0 {
					continue
				}
				k := strings.ToLower(strings.TrimSpace(it.Name))
				if cur, ok := out[k]; !ok || it.Value > cur {
					out[k] = it.Value
				}
			}
		}
	}
	return out
}

// costUnitCredited prices one consumed input.
//
// costUnit is strict on purpose: only a market price that is confident and coherent is subtracted, and
// anything else is unknown, because a cost guessed too high makes a profitable source look like a loss.
// But an input the panel ITSELF recorded arriving (a pearl a Big net opened, a gem a casket paid, a bow
// the fleet cut) was added to the fleet's value at a price, and leaving it out of the cost when it is
// used up counts that value twice: once as the pearl, again as the bolt tips cut from it. So when the
// strict answer is no, an input that has a recorded arrival (in the lifetime, whichever period is drawn) is priced exactly as its arrival
// was (PriceBook.Value with the cache cost it arrived with), which makes the pass-through net to what
// the step added, not to its whole output. An input with no arrival keeps the strict answer.
//
// An unstrung bow (loot.UnstrungSuffix) is priced as the strung bow, and only when a bow of that name was
// recorded arriving: the cut that made it was credited at the strung bow's price, since the two share a
// name (loot.Stringing), so that is the price to take back. With no such arrival it stays unknown.
func costUnitCredited(prices *PriceBook, name string, credited creditBook) (int64, bool) {
	name = strings.TrimSpace(name)
	if base := strings.TrimSpace(strings.TrimSuffix(name, loot.UnstrungSuffix)); base != name {
		if _, arrived := credited[strings.ToLower(base)]; !arrived {
			return 0, false
		}
		name = base
	}
	if unit, ok := costUnit(prices, name); ok {
		return unit, true
	}
	cacheCost, arrived := credited[strings.ToLower(name)]
	if !arrived {
		return 0, false
	}
	unit, _ := prices.Value(name, cacheCost)
	if unit <= 0 {
		return 0, false
	}
	return unit, true
}
