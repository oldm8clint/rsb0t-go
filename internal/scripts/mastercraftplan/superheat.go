package mastercraftplan

import (
	"strconv"
	"strings"
)

// THE SUPERHEAT ROUND (a user request: "superheating, request ores from the mule and nature runes to
// go with the amount of ores and start superheating them into bars. start with the highest available
// bar we can make, leave gold ore out for now until we have reached all the requirements to do and
// complete family crest").
//
// THE CONTENT (content/scripts, branch 289):
//
//   - The spell (skill_magic/configs/magic_spells.dbrow [magic_spell_superheat]): Magic 43, not
//     members, 1 Nature rune and 4 Fire runes, 53 Magic XP (530 tenths). A worn fire staff stands in
//     for the fire runes (magic.rs2 ~staff_runes). Spellbook component magic:superheat_item, id 1173
//     (pack/interface.pack), cast on a held ore with OPHELDT.
//   - The cast (skill_magic/scripts/spells/superheat.rs2 [label,magic_spell_superheat]): the ore's
//     smeltsto param names the bar; iron ore becomes STEEL when the pack holds more than one coal and
//     Smithing is 30+ (casting on coal itself does nothing); the bar's smelting struct names the
//     primary ore, the secondary (tin or coal) and its count, the Smithing level and the XP. No
//     failure chance (unlike a furnace's iron), then p_delay(1).
//   - The bars (skill_smithing/configs/smelting/smelting.struct): bronze 1 (copper + 1 tin, 6.2 XP),
//     iron 15 (12.5), silver 20 (13.7), steel 30 (iron + 2 coal, 17.5), gold 40 (22.5; 56.25 with
//     goldsmith gauntlets worn: superheat.rs2 scales it 5/2), mithril 50 (+4 coal, 30), adamantite 70
//     (+6 coal, 37.5), runite 85 (+8 coal, 50).
//   - THE PACE: OpHeldTHandler drops a cast while the player is delayed ("cannot interact while
//     delayed"). p_delay(1) sets delayedUntil = tick + 2 (PlayerOps P_DELAY: currentTick + 1 + n),
//     and the delay clears in processPlayers, AFTER processClientsIn has read that tick's packets
//     (World.cycle). So a cast read at tick T blocks T+1 and T+2 and the next is read at T+3: one bar
//     every three ticks, 1.8 s, 2,000 casts an hour. Sending the next cast when the client has seen
//     tick T+2 lands it on T+3; that is the fastest pace that waits for each cast to show.

// Bar is one bar a superheat can make.
type Bar struct {
	Name string
	// BarID is the bar; OreID/Ore the primary ore the spell is cast on; SecondID/Second/SecondPer the
	// other ingredient a bar takes (tin for bronze, coal for steel and up; 0 for none).
	BarID     int32
	Ore       string
	OreID     int32
	Second    string
	SecondID  int32
	SecondPer int
	// Level is the Smithing level; XPTenths the Smithing XP in content's tenths.
	Level, XPTenths int
	// Gold marks the gold bar, which the round leaves alone unless SuperheatGold.
	Gold bool
	// Low marks the bars under Steel (Bronze, Iron, Silver), which the round leaves alone unless
	// SuperheatLowBars (a user request: "mastercrafter should make steel bars and up only").
	Low bool
}

// Ore, rune and staff names and ids (content pack/obj.pack).
const (
	Coal         = "Coal"
	CoalID int32 = 453
	// NatureRune and FireRune are the spell's runes.
	NatureRune         = "Nature rune"
	NatureRuneID int32 = 561
	FireRune           = "Fire rune"
	FireRuneID   int32 = 554
	// StaffOfFire is the staff the mule may hold (obj 1387 staff_of_fire).
	StaffOfFire = "Staff of fire"
	// SuperheatLevel, SuperheatFire and SuperheatTicks are the spell's Magic level, its fire runes a
	// cast without a staff, and the ticks one cast takes (see THE PACE).
	SuperheatLevel = 43
	SuperheatFire  = 4
	SuperheatTicks = 3
	// SuperheatXPTenths is the Magic XP of a cast.
	SuperheatXPTenths = 530
)

// Bars are the bars, the highest Smithing level first: the order a round makes them in.
var Bars = []Bar{
	{Name: "Runite bar", BarID: 2363, Ore: "Runite ore", OreID: 451, Second: Coal, SecondID: CoalID, SecondPer: 8, Level: 85, XPTenths: 500},
	{Name: "Adamantite bar", BarID: 2361, Ore: "Adamantite ore", OreID: 449, Second: Coal, SecondID: CoalID, SecondPer: 6, Level: 70, XPTenths: 375},
	{Name: "Mithril bar", BarID: 2359, Ore: "Mithril ore", OreID: 447, Second: Coal, SecondID: CoalID, SecondPer: 4, Level: 50, XPTenths: 300},
	{Name: "Gold bar", BarID: 2357, Ore: "Gold ore", OreID: 444, Level: 40, XPTenths: 225, Gold: true},
	{Name: "Steel bar", BarID: 2353, Ore: "Iron ore", OreID: 440, Second: Coal, SecondID: CoalID, SecondPer: 2, Level: 30, XPTenths: 175},
	{Name: "Silver bar", BarID: 2355, Ore: "Silver ore", OreID: 442, Level: 20, XPTenths: 137, Low: true},
	{Name: "Iron bar", BarID: 2351, Ore: "Iron ore", OreID: 440, Level: 15, XPTenths: 125, Low: true},
	{Name: "Bronze bar", BarID: 2349, Ore: "Copper ore", OreID: 436, Second: "Tin ore", SecondID: 438, SecondPer: 1, Level: 1, XPTenths: 62, Low: true},
}

// BarByName finds a bar by its name ("Steel bar", or "steel"), ignoring case.
func BarByName(name string) (Bar, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, b := range Bars {
		n := strings.ToLower(b.Name)
		if key == n || key+" bar" == n {
			return b, true
		}
	}
	return Bar{}, false
}

// BarNames lists every bar's name.
func BarNames() []string {
	out := make([]string, 0, len(Bars))
	for _, b := range Bars {
		out = append(out, b.Name)
	}
	return out
}

// Slots is the pack slots one bar's ingredients take (none of the ores stack).
func (b Bar) Slots() int { return 1 + b.SecondPer }

// PerLoad is how many bars one load makes with free pack slots for ingredients.
func (b Bar) PerLoad(free int) int {
	if free <= 0 {
		return 0
	}
	return free / b.Slots()
}

// Casts is how many of b a holding of ores can make (the primary ore, and the second ingredient at
// its count a bar).
func (b Bar) Casts(have func(item string) int) int {
	n := have(b.Ore)
	if b.SecondPer > 0 {
		n = min(n, have(b.Second)/b.SecondPer)
	}
	return max(n, 0)
}

// SuperheatBars is the bars this account may make, highest first: Smithing allows it, the setting
// lists it (empty: all), gold only when set.SuperheatGold, and Bronze, Iron and Silver only when
// set.SuperheatLowBars (off by default: Steel and up only, a user request; the Iron bars the fleet
// holds go to the anvil round's Iron platebodies instead).
func SuperheatBars(set Settings, smithing int) []Bar {
	allowed := map[string]bool{}
	for _, n := range set.SuperheatBars {
		if b, ok := BarByName(n); ok {
			allowed[b.Name] = true
		}
	}
	var out []Bar
	for _, b := range Bars {
		if b.Level > smithing || (b.Gold && !set.SuperheatGold) || (b.Low && !set.SuperheatLowBars) {
			continue
		}
		if len(allowed) > 0 && !allowed[b.Name] {
			continue
		}
		out = append(out, b)
	}
	return out
}

// SuperheatStock is the ore, coal and runes in one place (the mule's bank, this account's bank).
type SuperheatStock func(item string) int

// stockFn turns a Stock into a SuperheatStock (0 for unknown).
func stockFn(s Stock) SuperheatStock {
	return func(item string) int { return stockOf(s, item) }
}

// sumStock is two holdings together.
func sumStock(a, b SuperheatStock) SuperheatStock {
	return func(item string) int {
		n := 0
		if a != nil {
			n += a(item)
		}
		if b != nil {
			n += b(item)
		}
		return n
	}
}

// SuperheatPlan is what a superheat round can make.
type SuperheatPlan struct {
	// Bar is the highest bar the ores allow; Casts how many of it the ore, the natures and the fire
	// allow (at most set.KitSize, the most one kit line asks for).
	Bar   Bar
	Casts int
	// Total is the casts every allowed bar's ore would make together, for the threshold.
	Total int
	// Fire is the fire runes the casts need (0 with a staff).
	Fire int
	Why  string
}

// PlanSuperheat picks the bar a round makes: the highest allowed bar that the ores (the mule's and
// this account's together) make at least one of, bounded by the natures (one a cast) and, without a
// fire staff, the fire runes (four a cast). Total adds every allowed bar's casts the same way, so the
// threshold reads "how many bars are waiting".
func PlanSuperheat(set Settings, smithing int, have SuperheatStock, staff bool) SuperheatPlan {
	var p SuperheatPlan
	bars := SuperheatBars(set, smithing)
	if len(bars) == 0 {
		p.Why = "Smithing " + strconv.Itoa(smithing) + " allows none of the chosen bars"
		return p
	}
	natures := have(NatureRune)
	runeCap := natures
	if !staff {
		runeCap = min(runeCap, have(FireRune)/SuperheatFire)
	}
	// Ores are shared between bars (iron makes steel or iron), so a later bar's count is what the
	// earlier ones left.
	left := map[string]int{}
	use := func(item string) int {
		if n, ok := left[item]; ok {
			return n
		}
		return have(item)
	}
	for _, b := range bars {
		n := b.Casts(use)
		if n <= 0 {
			continue
		}
		if p.Casts == 0 {
			p.Bar, p.Casts = b, min(n, runeCap, set.KitSize)
		}
		p.Total += n
		left[b.Ore] = use(b.Ore) - n
		if b.SecondPer > 0 {
			left[b.Second] = use(b.Second) - n*b.SecondPer
		}
	}
	p.Total = min(p.Total, runeCap)
	switch {
	case p.Bar.Name == "":
		p.Why = "no ore for any bar Smithing " + strconv.Itoa(smithing) + " allows"
	case p.Casts <= 0:
		if staff {
			p.Why = "no Nature runes"
		} else {
			p.Why = "not enough Nature or Fire runes (no fire staff)"
		}
	}
	if !staff {
		p.Fire = p.Casts * SuperheatFire
	}
	return p
}

// SuperheatReady decides a superheat round: Magic 43, and at least set.SuperheatThreshold bars'
// worth of ore and runes between the mule and this account's own bank, or any of the chosen bar's
// ore already in this account's bank (a round that ended early resumes).
func SuperheatReady(set Settings, magic, smithing int, mule Stock, own SuperheatStock, staff bool) (SuperheatPlan, bool, bool, string) {
	if magic < SuperheatLevel {
		return SuperheatPlan{}, false, false, "Magic " + strconv.Itoa(magic) + " is under the " + strconv.Itoa(SuperheatLevel) + " Superheat Item needs"
	}
	if own != nil {
		ownPlan := PlanSuperheat(set, smithing, sumStock(own, stockFn(mule)), staff)
		if ownPlan.Casts > 0 && own(ownPlan.Bar.Ore) > 0 {
			return ownPlan, true, true, "this account's bank holds " + strconv.Itoa(own(ownPlan.Bar.Ore)) + " " + ownPlan.Bar.Ore + " to superheat"
		}
	}
	if mule == nil {
		return SuperheatPlan{}, false, false, "the mule has not published its bank"
	}
	p := PlanSuperheat(set, smithing, sumStock(own, stockFn(mule)), staff)
	if p.Casts <= 0 {
		return p, false, false, p.Why
	}
	if p.Total < set.SuperheatThreshold {
		return p, false, false, "the ore and runes make " + strconv.Itoa(p.Total) + " bars, under the threshold " + strconv.Itoa(set.SuperheatThreshold)
	}
	return p, true, false, strconv.Itoa(p.Total) + " bars' worth of ore and runes, " + p.Bar.Name + " first"
}

// PlanSuperheatAsk is the kit a superheat round asks the mule for, for the casts of p.Bar this
// account's own bank cannot already make: the ores, the natures and (without a staff) the fire runes,
// or the mule's Staff of fire when it publishes one and no staff is held.
func PlanSuperheatAsk(p SuperheatPlan, mule Stock, own SuperheatStock, staff bool) []Line {
	if p.Casts <= 0 {
		return nil
	}
	if own == nil {
		own = func(string) int { return 0 }
	}
	var lines []Line
	add := func(item string, want int) {
		need := min(want-own(item), stockOf(mule, item))
		if need > 0 {
			lines = append(lines, Line{Item: item, Qty: need})
		}
	}
	b := p.Bar
	add(b.Ore, p.Casts)
	if b.SecondPer > 0 {
		add(b.Second, p.Casts*b.SecondPer)
	}
	add(NatureRune, p.Casts)
	if !staff {
		if stockOf(mule, StaffOfFire) > 0 {
			lines = append(lines, Line{Item: StaffOfFire, Qty: 1})
		} else {
			add(FireRune, p.Casts*SuperheatFire)
		}
	}
	return lines
}

// SuperheatLoad is one load's draw: the bars a load makes with free slots, bounded by what the bank
// holds.
func SuperheatLoad(b Bar, free int, ore, second int) int {
	n := min(b.PerLoad(free), ore)
	if b.SecondPer > 0 {
		n = min(n, second/b.SecondPer)
	}
	return max(n, 0)
}

// SuperheatRefusal reads the cast's chat for content's refusals: the Magic level, the runes, the
// Smithing level and the ore count (smelting.struct's processfailure_superheat lines: "You need four
// heaps of coal to smelt mithril.", "You need one copper ore and one tin ore to make bronze.").
// reason is "level", "runes", "smithing" or "ore".
func SuperheatRefusal(said string) string {
	switch {
	case strings.Contains(said, "Your Magic level is not high enough"):
		return "level"
	case strings.Contains(said, "You do not have enough") && strings.Contains(said, "Runes to cast this spell"):
		return "runes"
	case strings.Contains(said, "You need a smithing level of at least"):
		return "smithing"
	case strings.Contains(said, "You need to cast superheat item on ore"),
		strings.Contains(said, "heaps of coal to smelt"),
		strings.Contains(said, "one copper ore and one tin ore"):
		return "ore"
	}
	return ""
}
