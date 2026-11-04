package mastercraftplan

import (
	"fmt"
	"strconv"
	"strings"
)

// THE ANVIL ROUND (a user request: "add anvil smithing at varrock west, making arrow tips with the bars
// starting with best bar first, must have over 8000 steel bars to trigger it, iron bars can be turned into
// iron platebodies, must use jewellery stall running back and forth, iron platebodies to add to alch list";
// and: "it should get all bars out when there are 8000 not just small amounts").
//
// THE TRIGGER IS NOT THE STOP. More than set.SmithMinSteelBars Steel bars (this account's bank and the mule's
// together) STARTS a round. The round then smiths EVERY bar it can, whatever the stock does meanwhile: Runite,
// Adamantite, Mithril and Steel bars into arrowtips, best bar first, and every Iron bar into Iron platebodies,
// until the bank holds none of a kind it can smith. It does not stop when Steel drops under the threshold and
// it never smiths a small batch per trigger. Ledger.SmithActive carries a round that was cut short (the
// time budget, a restart) to the next check, which resumes it from this account's own bank with no
// threshold.
//
// THE CONTENT (content/scripts, branch 289; read, not run):
//
//   - The anvil (skill_smithing/scripts/smithing/smithing.rs2 [oplocu,_anvil]): a bar used on the anvil ->
//     [label,smithing_anvil_interface]: the Smithing level for the METAL (smithing.struct levelrequired), a
//     hammer in the pack, then it transmits five column inventories and opens interface 994 (smithing). The
//     anvil loc is 2783 "Anvil", with no option of its own.
//   - The make (attempt_smithing): inv_button1/2/3 on a column = 1/5/10 of the product; "Make X" is not an
//     option. The product row (smithing.dbrow) names the bar, the bars a make takes, the units it gives and
//     the Smithing level: arrowtips are 15 for 1 bar (Steel 35, Mithril 55, Adamant 75, Rune 90; Iron 20 and
//     Bronze 5 are not made), an Iron platebody is 1 for 5 Iron bars at Smithing 33. The arrowtips are
//     members-only objs (arrows.obj members=yes) and attempt_smithing refuses them on a free world; the
//     platebody is not.
//   - Where they are (smithing.inv): the arrowtips are column 5 (component 1123) slot 1, the platebody column
//     3 (component 1121) slot 3, for every metal.
//   - The pace (smithing_anvil): p_delay(1), the bars out and the product in, then p_delay(2): FIVE ticks a
//     make, whatever it gives, and the next make of a count follows at once. So an Iron platebody costs one
//     tick a bar and 125 Smithing XP, an arrowtip make 5 ticks for a single bar (Steel 37.5 XP).
//   - The XP (smithing.struct xpperbar, in tenths): Iron 250, Steel 375, Mithril 500, Adamant 625, Rune 750 a
//     bar.
//
// THE PACK: a hammer, the rub jewellery of the run staller if the account has one, and the bars; the products
// stack (arrowtips) or take fewer slots than the bars they used (a platebody 1 for 5).

// Anvil facts.
const (
	// SmithInterface is the smithing interface (pack/interface.pack 994=smithing).
	SmithInterface = 994
	// SmithColumn3 and SmithColumn5 are the column components the platebody (slot 3) and the arrowtips
	// (slot 1) sit in.
	SmithColumn3 = 1121
	SmithColumn5 = 1123
	// Hammer is the tool an anvil needs; HammerID its obj.
	Hammer         = "Hammer"
	HammerID int32 = 2347
	// IronBarsNeeded is the Iron bars one platebody takes.
	IronBarsNeeded = 5
	// SmithTicks is the ticks one make takes (see THE PACE).
	SmithTicks = 5
	// ArrowtipsPerBar is the arrowtips one bar makes.
	ArrowtipsPerBar = 15
	// SmithBatch is the largest count one button makes (Make 10).
	SmithBatch = 10
	// IronBar is the bar the platebody takes.
	IronBar = "Iron bar"
	// SteelBar is the bar whose stock starts a round.
	SteelBar = "Steel bar"
	// IronPlatebodyName is the product the alch list gains.
	IronPlatebodyName = "Iron platebody"
)

// SmithKind is what a product is.
type SmithKind string

// The kinds.
const (
	SmithArrowtipsKind SmithKind = "arrowtips"
	SmithPlatebodyKind SmithKind = "platebody"
)

// SmithProduct is one thing an anvil makes from one metal.
type SmithProduct struct {
	Kind SmithKind
	// Name and ID are the product; Bar and BarID the bar it takes, BarsPer how many a make takes and Per how
	// many units a make gives.
	Name           string
	ID             int32
	Bar            string
	BarID          int32
	BarsPer, Per   int
	Level          int
	Com, Slot      int
	XPTenthsPerBar int
}

// Members says the product needs a members world.
func (p SmithProduct) Members() bool { return p.Kind == SmithArrowtipsKind }

// Makes is how many makes bars bars pay for.
func (p SmithProduct) Makes(bars int) int {
	if bars <= 0 || p.BarsPer <= 0 {
		return 0
	}
	return bars / p.BarsPer
}

// SmithArrowtips are the arrowtips, the best bar first: the order a round makes them in.
var SmithArrowtips = []SmithProduct{
	{Kind: SmithArrowtipsKind, Name: "Rune arrowtips", ID: 44, Bar: "Runite bar", BarID: 2363, BarsPer: 1, Per: ArrowtipsPerBar, Level: 90, Com: SmithColumn5, Slot: 1, XPTenthsPerBar: 750},
	{Kind: SmithArrowtipsKind, Name: "Adamant arrowtips", ID: 43, Bar: "Adamantite bar", BarID: 2361, BarsPer: 1, Per: ArrowtipsPerBar, Level: 75, Com: SmithColumn5, Slot: 1, XPTenthsPerBar: 625},
	{Kind: SmithArrowtipsKind, Name: "Mithril arrowtips", ID: 42, Bar: "Mithril bar", BarID: 2359, BarsPer: 1, Per: ArrowtipsPerBar, Level: 55, Com: SmithColumn5, Slot: 1, XPTenthsPerBar: 500},
	{Kind: SmithArrowtipsKind, Name: "Steel arrowtips", ID: 41, Bar: SteelBar, BarID: 2353, BarsPer: 1, Per: ArrowtipsPerBar, Level: 35, Com: SmithColumn5, Slot: 1, XPTenthsPerBar: 375},
}

// SmithIronPlatebody is the Iron platebody: 5 Iron bars, Smithing 33.
var SmithIronPlatebody = SmithProduct{Kind: SmithPlatebodyKind, Name: IronPlatebodyName, ID: 1115, Bar: IronBar, BarID: 2351,
	BarsPer: IronBarsNeeded, Per: 1, Level: 33, Com: SmithColumn3, Slot: 3, XPTenthsPerBar: 250}

// SmithAllProducts is every product, the arrowtips (best first) and then the platebody: the order a round
// works through them.
func SmithAllProducts() []SmithProduct {
	return append(append([]SmithProduct(nil), SmithArrowtips...), SmithIronPlatebody)
}

// SmithProductsFor is the products this account may make now, in working order: the arrowtips of every bar
// the Smithing level allows (set.SmithArrowtips, a members world) and the Iron platebody
// (set.SmithIronPlatebodies).
func SmithProductsFor(set Settings, smithing int) []SmithProduct {
	var out []SmithProduct
	for _, p := range SmithAllProducts() {
		switch {
		case p.Level > smithing:
		case p.Kind == SmithArrowtipsKind && !set.SmithArrowtips:
		case p.Kind == SmithPlatebodyKind && !set.SmithIronPlatebodies:
		case p.Members() && !set.MembersWorld:
		default:
			out = append(out, p)
		}
	}
	return out
}

// SmithBarNames is the distinct bars of products, in order.
func SmithBarNames(products []SmithProduct) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range products {
		if !seen[p.Bar] {
			seen[p.Bar] = true
			out = append(out, p.Bar)
		}
	}
	return out
}

// SmithStock is this account's holdings by item name (bars).
type SmithStock func(item string) int

// SmithMakes is the makes of products the bars in have pay for, in total.
func SmithMakes(products []SmithProduct, have SmithStock) int {
	n := 0
	for _, p := range products {
		n += p.Makes(have(p.Bar))
	}
	return n
}

// SmithPlan is a decided anvil round.
type SmithPlan struct {
	// Products are what the round may make, in working order.
	Products []SmithProduct
	// Steel, Mithril, Adamant, Rune and Iron are the bars the two banks held when the round was decided: the
	// starting stock the log reports.
	Steel, Mithril, Adamant, Rune, Iron int
	// Resume is a round carried on from this account's own bank (Ledger.SmithActive), with no trigger.
	Resume bool
	Why    string
}

// Describe is the starting stock in words, for the round's first log line.
func (p SmithPlan) Describe() string {
	return fmt.Sprintf("bank holds %d steel, %d mithril, %d adamant, %d rune, %d iron bars", p.Steel, p.Mithril, p.Adamant, p.Rune, p.Iron)
}

// SmithTotals reads the stock off have.
func SmithTotals(have SmithStock) (steel, mithril, adamant, rune, iron int) {
	return have(SteelBar), have("Mithril bar"), have("Adamantite bar"), have("Runite bar"), have(IronBar)
}

// SmithReady decides an anvil round. A round already under way (active, the ledger's SmithActive) with bars in
// this account's own bank to smith is resumed with NO threshold: it carries on until the bank holds none. A new
// one starts only when Steel bars, this account's bank and the mule's together, exceed set.SmithMinSteelBars
// (strictly more), and the Smithing level and the world allow Steel arrowtips. mule is nil when the mule's bank
// has not been published, own nil when this account's bank has not been read this session.
func SmithReady(set Settings, smithing int, active bool, mule Stock, own SmithStock) (SmithPlan, bool) {
	var p SmithPlan
	p.Products = SmithProductsFor(set, smithing)
	// A round under way whose bank has not been read since the script started carries on: the round reads
	// the bank itself, and finds the bars gone (and ends) or smiths them.
	if active && own == nil {
		p.Resume = true
		p.Why = "a round under way (this account's bank has not been read since the restart): carrying on"
		return p, true
	}
	if own == nil {
		own = func(string) int { return 0 }
	}
	total := func(item string) int { return own(item) + stockOf(mule, item) }
	p.Steel, p.Mithril, p.Adamant, p.Rune, p.Iron = SmithTotals(total)
	if len(p.Products) == 0 {
		p.Why = "Smithing " + strconv.Itoa(smithing) + " (or the world, or the products switched off) allows no product"
		return p, false
	}
	if active {
		if n := SmithMakes(p.Products, own); n > 0 {
			p.Resume = true
			p.Why = "a round under way: this account's bank holds bars for " + strconv.Itoa(n) + " more make(s), smithing them all"
			return p, true
		}
	}
	steelOK := false
	for _, pr := range p.Products {
		if pr.Bar == SteelBar {
			steelOK = true
		}
	}
	switch {
	case !steelOK:
		p.Why = "Smithing " + strconv.Itoa(smithing) + " is under the 35 Steel arrowtips need (or arrowtips are off, or the world is free)"
	case mule == nil && own(SteelBar) <= set.SmithMinSteelBars:
		p.Why = "the mule has not published its bank"
	case p.Steel > set.SmithMinSteelBars:
		p.Why = fmt.Sprintf("%d Steel bars in the two banks, over the %d that start a round: smithing every bar", p.Steel, set.SmithMinSteelBars)
		return p, true
	default:
		p.Why = fmt.Sprintf("%d Steel bars in the two banks, not over %d", p.Steel, set.SmithMinSteelBars)
	}
	return p, false
}

// PlanSmithAsk is the kit a new anvil round asks the mule for: EVERY bar of the products' kinds it holds (at
// most set.SmithKit of one), and a hammer when none is held in the bank or the pack.
func PlanSmithAsk(set Settings, products []SmithProduct, mule Stock, own SmithStock, haveHammer bool) []Line {
	var lines []Line
	for _, bar := range SmithBarNames(products) {
		if n := min(stockOf(mule, bar), set.SmithKit); n > 0 {
			lines = append(lines, Line{Item: bar, Qty: n})
		}
	}
	if !haveHammer && own(Hammer) <= 0 && stockOf(mule, Hammer) > 0 {
		lines = append(lines, Line{Item: Hammer, Qty: 1})
	}
	// The rub jewellery for the walks (owner: "must use jewellery stall running back and forth"). Live 8 Oct
	// 2026 the first round started with none ("walks run-stalled: false (none (no rub item))").
	if rubOwned(own) == 0 {
		if name, ok := MuleRubItem(mule); ok {
			lines = append(lines, Line{Item: name, Qty: 1})
		}
	}
	return lines
}

// rubCharges are the charged forms of the rub jewellery, fullest first: content names each charge its own obj.
var rubCharges = func() []string {
	var out []string
	for _, base := range []string{"Ring of dueling", "Games necklace"} {
		for c := 8; c >= 1; c-- {
			out = append(out, fmt.Sprintf("%s(%d)", base, c))
		}
	}
	return out
}()

// rubOwned is how many rub items of any charge the account holds.
func rubOwned(own SmithStock) int {
	n := 0
	for _, name := range rubCharges {
		n += own(name)
	}
	return n
}

// MuleRubItem is the first charged rub item the mule's published bank holds.
func MuleRubItem(mule Stock) (string, bool) {
	for _, name := range rubCharges {
		if stockOf(mule, name) > 0 {
			return name, true
		}
	}
	return "", false
}

// SmithLoad is the bars one bank trip draws for a product: as many as fit in free pack slots, the bank's
// stock bounding it, in whole makes (an Iron platebody takes five).
func SmithLoad(p SmithProduct, free, inBank int) int {
	n := min(free, inBank)
	if p.BarsPer > 1 {
		n -= n % p.BarsPer
	}
	return max(n, 0)
}

// SmithDraw is one bar draw of a bank trip: n bars for product.
type SmithDraw struct {
	Product SmithProduct
	N       int
}

// PlanSmithDraws is a bank trip's full pack: the products in working order (best bar first), each taking as
// many bars as the free slots hold and the bank has, the next product filling what is left. A trip is never
// a small batch while the bank holds more: it draws until the pack is full (a user request: "it should
// get all bars out ... not just small amounts"). bank counts bars by name; free is the pack slots free
// after the hammer and the rub jewellery.
func PlanSmithDraws(products []SmithProduct, bank SmithStock, free int) []SmithDraw {
	var out []SmithDraw
	seen := map[string]int{}
	for _, p := range products {
		if free <= 0 {
			break
		}
		n := SmithLoad(p, free, bank(p.Bar)-seen[p.Bar])
		if n <= 0 {
			continue
		}
		seen[p.Bar] += n
		free -= n
		out = append(out, SmithDraw{Product: p, N: n})
	}
	return out
}

// SmithButton is the button and count of the next batch of makes: Make 10 for ten or more makes, Make 5 for
// five to nine, else Make 1.
func SmithButton(makes int) (option, count int) {
	switch {
	case makes >= SmithBatch:
		return 3, SmithBatch
	case makes >= 5:
		return 2, 5
	}
	return 1, 1
}

// SmithRefusal reads the anvil's chat for its refusals: "level", "hammer", "bars" or "members" ("" none).
func SmithRefusal(said string) string {
	switch {
	case strings.Contains(said, "You need a Smithing level of"):
		return "level"
	case strings.Contains(said, "You need a hammer"):
		return "hammer"
	case strings.Contains(said, "You don't have enough"):
		return "bars"
	case strings.Contains(said, "members' server"):
		return "members"
	}
	return ""
}
