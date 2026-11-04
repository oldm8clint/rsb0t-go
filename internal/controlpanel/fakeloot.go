package controlpanel

import (
	"math/rand"
	"sort"

	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/muleledger"
)

// The fake fleet's loot: each fake script stands in for one real loot source, with the real
// content 289 npc type ids, obj ids, names, combat levels and cache costs (read from the lab
// config archive), so the loot panel's models and icons resolve exactly as they would live.
// The drop rates are plausible, not the game's.

type fakeDrop struct {
	id     int
	name   string
	value  int64
	min    int64
	max    int64
	chance int // 1 in chance
}

type fakeLootSource struct {
	src   loot.Source
	drops []fakeDrop
}

var (
	fakeCoins     = fakeDrop{id: 995, name: "Coins", value: 1}
	fakeBones     = fakeDrop{id: 526, name: "Bones", value: 1, min: 1, max: 1, chance: 1}
	fakeSapphire  = fakeDrop{id: 1623, name: "Uncut sapphire", value: 25, min: 1, max: 1}
	fakeEmerald   = fakeDrop{id: 1621, name: "Uncut emerald", value: 50, min: 1, max: 1}
	fakeRuby      = fakeDrop{id: 1619, name: "Uncut ruby", value: 100, min: 1, max: 1}
	fakeDiamond   = fakeDrop{id: 1617, name: "Uncut diamond", value: 200, min: 1, max: 1}
	fakeCosmic    = fakeDrop{id: 1454, name: "Cosmic talisman", value: 4, min: 1, max: 1}
	fakeLoopHalf  = fakeDrop{id: 987, name: "Half of a key", value: 1, min: 1, max: 1}
	fakeToothHalf = fakeDrop{id: 985, name: "Half of a key", value: 1, min: 1, max: 1}
)

func withCoins(lo, hi int64) fakeDrop { d := fakeCoins; d.min, d.max, d.chance = lo, hi, 1; return d }
func withLevel(s loot.Source, level int) loot.Source {
	s.Level = level
	return s
}
func rare(d fakeDrop, chance int) fakeDrop { d.chance = chance; return d }

// fakeLootSources is indexed like fakeBots.
var fakeLootSources = [][]fakeLootSource{
	// fisher: caskets and oysters, opened in bulk. A casket gives exactly one reward.
	{
		{loot.Openable("Casket", 405), []fakeDrop{
			rare(withCoins(20, 640), 2), rare(fakeSapphire, 4), rare(fakeEmerald, 8), rare(fakeRuby, 16),
			rare(fakeDiamond, 32), rare(fakeCosmic, 20), rare(fakeLoopHalf, 64), rare(fakeToothHalf, 64),
		}},
		{loot.Openable("Oyster", 407), []fakeDrop{
			{id: 411, name: "Oyster pearl", value: 112, min: 1, max: 1, chance: 1},
			{id: 409, name: "Empty oyster", value: 5, min: 1, max: 1, chance: 4},
		}},
	},
	// miner stands in for a goblin killer.
	{{loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Goblin", ID: 100, Level: 2}, []fakeDrop{
		fakeBones, rare(withCoins(1, 25), 3), {id: 562, name: "Chaos rune", value: 15, min: 1, max: 4, chance: 20},
	}}},
	// thief: pickpockets and the Baker's stall, all on the one Thieving card (internal/loot's
	// thieving.go). Built with the real constructors, so the fake fleet has exactly the shape the
	// live one records.
	{
		{withLevel(loot.NPC(nil, loot.ActionPickpocket, 1, "Man"), 2), []fakeDrop{withCoins(3, 3)}},
		{withLevel(loot.NPC(nil, loot.ActionPickpocket, 9, "Guard"), 21), []fakeDrop{withCoins(30, 30)}},
		{withLevel(loot.NPC(nil, loot.ActionPickpocket, 23, "Knight of Ardougne"), 46), []fakeDrop{withCoins(50, 50)}},
		{loot.Stall(nil, 2561, "Baker's stall"), []fakeDrop{
			{id: 1891, name: "Cake", value: 50, min: 1, max: 1, chance: 2}, {id: 2309, name: "Bread", value: 12, min: 1, max: 1, chance: 3},
			{id: 1901, name: "Chocolate slice", value: 30, min: 1, max: 1, chance: 8},
		}},
	},
	// cutter stands in for a chicken killer.
	{{loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Chicken", ID: 41, Level: 1}, []fakeDrop{
		fakeBones, {id: 314, name: "Feather", value: 2, min: 5, max: 15, chance: 2}, {id: 2138, name: "Raw chicken", value: 1, min: 1, max: 1, chance: 1},
	}}},
	// crafter stands in for a cow killer. Bones only, because that is all a fleet cow kill ever
	// yields: ScarabSwarm picks up bones alone (boneplan.DefaultNames) and CombatTrainer's cow rung
	// picks up nothing. This set used to add Cowhide and Raw beef, which the live fleet never took —
	// and that is how the -fake panel showed a Cow card with drops while the live one had none.
	{{loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Cow", ID: 81, Level: 2}, []fakeDrop{fakeBones}}},
	// agility stands in for a moss giant killer.
	{{loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Moss giant", ID: 112, Level: 42}, []fakeDrop{
		{id: 532, name: "Big bones", value: 1, min: 1, max: 1, chance: 1}, rare(withCoins(5, 300), 3),
		{id: 561, name: "Nature rune", value: 20, min: 1, max: 6, chance: 12}, rare(fakeSapphire, 40), rare(fakeLoopHalf, 128),
	}}},
}

// fakeRandomEvents are the random events whose rewards the fleet records (see
// internal/loot/randomevent.go), with content 289's own reward items; the rates are plausible,
// not the game's. Every account can get any of them, whatever its script.
var fakeRandomEvents = []fakeLootSource{
	{randomEventSource("Strange box"), []fakeDrop{
		rare(withCoins(80, 640), 2), rare(withCoins(240, 240), 25), rare(fakeSapphire, 5), rare(fakeEmerald, 9),
		rare(fakeRuby, 19), rare(fakeDiamond, 75), rare(fakeLoopHalf, 150), rare(fakeToothHalf, 150),
	}},
	{randomEventSource("Mysterious Old Man"), []fakeDrop{
		{id: 405, name: "Casket", value: 50, min: 1, max: 1, chance: 1}, {id: 1971, name: "Kebab", value: 3, min: 1, max: 1, chance: 9},
		{id: 1969, name: "Spinach roll", value: 1, min: 1, max: 1, chance: 10}, rare(withCoins(240, 240), 25),
	}},
	{randomEventSource("Drunken Dwarf"), []fakeDrop{
		{id: 1971, name: "Kebab", value: 3, min: 1, max: 1, chance: 1}, {id: 1917, name: "Beer", value: 2, min: 1, max: 1, chance: 1},
	}},
	{randomEventSource("Strange plant"), []fakeDrop{{id: 464, name: "Strange fruit", min: 1, max: 1, chance: 1}}},
	// The Genie's one item per event is the Lamp it hands over (loot.EventReward.GivesOwn).
	{randomEventSource("Genie"), []fakeDrop{{id: 2528, name: "Lamp", value: 1, min: 1, max: 1, chance: 1}}},
	{randomEventSource("mime"), []fakeDrop{
		rare(withCoins(500, 500), 1), {id: 3057, name: "Mime mask", min: 1, max: 1, chance: 5}, {id: 3058, name: "Mime top", min: 1, max: 1, chance: 5},
		{id: 3059, name: "Mime legs", min: 1, max: 1, chance: 5}, {id: 3061, name: "Mime boots", min: 1, max: 1, chance: 5},
	}},
	{randomEventSource("maze"), []fakeDrop{
		rare(withCoins(40, 400), 1), {id: 314, name: "Feather", value: 2, min: 20, max: 200, chance: 3},
		{id: 562, name: "Chaos rune", value: 15, min: 5, max: 40, chance: 4}, {id: 560, name: "Death rune", value: 30, min: 2, max: 20, chance: 6},
		{id: 561, name: "Nature rune", value: 20, min: 1, max: 8, chance: 8}, {id: 556, name: "Air rune", value: 4, min: 15, max: 15, chance: 3},
	}},
}

// The fishing spots, which on a real fleet are the overwhelming majority of the loot: 267 of
// the 269 live accounts fish. Npc type ids, obj ids, names and cache costs are content 289's
// own (read from the lab config archive); the rates are plausible, not the game's.
//
// Every one of these npcs is named "Fishing spot" in content 289 — which is the point. The
// panel merges sources by kind, action and name, so these four arrive as ONE card carrying all
// four spot models and every fish, casket, oyster, seaweed and scrap of leather between them,
// while staying four separate sources underneath (see internal/loot/fishing.go).
var fakeFishingSpots = []fakeLootSource{
	// 316, Net/Bait: the shrimp spot every fresh account starts on.
	{loot.Fishing(nil, 316), []fakeDrop{
		{id: 317, name: "Raw shrimps", value: 5, min: 1, max: 1, chance: 1},
		{id: 321, name: "Raw anchovies", value: 15, min: 1, max: 1, chance: 4},
	}},
	// 309, Lure/Bait: trout and salmon.
	{loot.Fishing(nil, 309), []fakeDrop{
		{id: 335, name: "Raw trout", value: 20, min: 1, max: 1, chance: 1},
		{id: 331, name: "Raw salmon", value: 50, min: 1, max: 1, chance: 3},
		{id: 349, name: "Raw pike", value: 25, min: 1, max: 1, chance: 6},
	}},
	// 312, Cage/Harpoon: lobster, tuna and swordfish.
	{loot.Fishing(nil, 312), []fakeDrop{
		{id: 377, name: "Raw lobster", value: 150, min: 1, max: 1, chance: 1},
		{id: 359, name: "Raw tuna", value: 100, min: 1, max: 1, chance: 3},
		{id: 371, name: "Raw swordfish", value: 200, min: 1, max: 1, chance: 7},
	}},
	// 313, Net/Harpoon: the Big net spot, and the only one that lands anything but fish.
	{loot.Fishing(nil, 313), []fakeDrop{
		{id: 353, name: "Raw mackerel", value: 17, min: 1, max: 1, chance: 1},
		{id: 341, name: "Raw cod", value: 25, min: 1, max: 1, chance: 3},
		{id: 363, name: "Raw bass", value: 120, min: 1, max: 1, chance: 9},
		{id: 401, name: "Seaweed", value: 2, min: 1, max: 1, chance: 5},
		{id: 405, name: "Casket", value: 50, min: 1, max: 1, chance: 14},
		{id: 407, name: "Oyster", value: 200, min: 1, max: 1, chance: 18},
		{id: 1059, name: "Leather gloves", value: 6, min: 1, max: 1, chance: 30},
		{id: 1061, name: "Leather boots", value: 6, min: 1, max: 1, chance: 34},
	}},
}

// The Kalphite Queen, whose card users asked to show both of her forms in sequence
// (2026-09-20). She is here ONLY under -fake: nothing on the real fleet has killed her, so on a
// production panel this card simply does not exist until something does — internal/bot's kill
// loot is generic (lootcombat.go reports loot.NPC for whatever died), so no further work is
// needed the day one falls.
//
// Both of her fighting forms are level 333 and both are named "Kalphite Queen", so they merge
// into one card exactly as the three "Man" ids do; FORMS in web/panels/loot.js is what turns
// that merge into a dissolve between forms instead of two models side by side. Npc ids, obj
// ids, names and costs are content 289's own; the drop rates are plausible, not the game's.
var fakeKalphiteQueen = []fakeLootSource{
	// 1158: the walking first form.
	{loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Kalphite Queen", ID: 1158, Level: 333}, []fakeDrop{
		{id: 532, name: "Big bones", value: 1, min: 1, max: 1, chance: 1}, rare(withCoins(1000, 9000), 2),
		{id: 561, name: "Nature rune", value: 20, min: 20, max: 70, chance: 3},
		rare(fakeRuby, 5), rare(fakeDiamond, 9),
	}},
	// 1160: the flying second form.
	{loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Kalphite Queen", ID: 1160, Level: 333}, []fakeDrop{
		{id: 532, name: "Big bones", value: 1, min: 1, max: 1, chance: 1}, rare(withCoins(2000, 12000), 2),
		{id: 560, name: "Death rune", value: 30, min: 20, max: 60, chance: 3},
		rare(fakeDiamond, 4), {id: 1149, name: "Dragon med helm", value: 60000, min: 1, max: 1, chance: 40},
	}},
}

// Woodcutting and Mining, the other two gathering skills (internal/loot/gather.go says which
// they are and why). Loc type ids, obj ids, names and costs are content 289's own; the rates are
// plausible, not the game's. Each skill's sources merge into ONE card because they group on the
// skill, while the per-tree and per-rock rows stay separate underneath.
var fakeWoodcutting = []fakeLootSource{
	{loot.Woodcutting(nil, 1276, "Tree"), []fakeDrop{{id: 1511, name: "Logs", value: 4, min: 1, max: 1, chance: 1}}},
	{loot.Woodcutting(nil, 1281, "Oak"), []fakeDrop{{id: 1521, name: "Oak logs", value: 20, min: 1, max: 1, chance: 1}}},
	{loot.Woodcutting(nil, 1308, "Willow"), []fakeDrop{{id: 1519, name: "Willow logs", value: 40, min: 1, max: 1, chance: 1}}},
	{loot.Woodcutting(nil, 1307, "Maple tree"), []fakeDrop{{id: 1517, name: "Maple logs", value: 75, min: 1, max: 1, chance: 1}}},
}

var fakeMining = []fakeLootSource{
	{loot.Mining(nil, 2491, "Rune Essence"), []fakeDrop{{id: 1436, name: "Rune essence", value: 25, min: 1, max: 1, chance: 1}}},
	{loot.Mining(nil, 450, "Rocks"), []fakeDrop{
		{id: 436, name: "Copper ore", value: 12, min: 1, max: 1, chance: 1},
		{id: 438, name: "Tin ore", value: 12, min: 1, max: 1, chance: 2},
	}},
	{loot.Mining(nil, 451, "Rocks"), []fakeDrop{
		{id: 440, name: "Iron ore", value: 45, min: 1, max: 1, chance: 1},
		{id: 453, name: "Coal", value: 120, min: 1, max: 1, chance: 3},
	}},
}

// Fletching, the one PRODUCTION skill the fleet runs (internal/loot/production.go says why it is
// not a gathering one, and what the fleet actually makes: arrow shafts from plain Logs, and
// nothing else, until string or attach mode is ported).
//
// One source, because plain Logs are the only material that has a shafts column at all. It is
// still written as a list, so a second material lands on the same Fletching card here exactly as
// it will on the live panel.
//
// One fake EVENT is one LOG CUT, because loot.ActionFletch counts materials consumed — which is
// also what the Fletching card's cost is priced from (internal/loot/cost.go). Each event yields
// fifteen shafts: `data=shafts,15` on content/scripts/skill_fletching/configs/cut_logs/
// cut_logs.dbrow's [fletching_normal] row, the real ratio rather than a plausible one. Obj 52 is
// arrow_shaft and 1511 is logs, both read out of content/pack/obj.pack rather than recalled.
var fakeFletching = []fakeLootSource{
	{loot.Fletching(nil, 1511, "Logs"), []fakeDrop{{id: 52, name: "Arrow shaft", value: 1, min: 15, max: 15, chance: 1}}},
}

func randomEventSource(name string) loot.Source {
	r, ok := loot.RandomEventReward(name)
	if !ok {
		panic("fake random event " + name + " is not a recorded reward event")
	}
	return r.Source()
}

// stepLoot rolls one fake account's loot for one Step. Caller holds f.mu.
func (f *FakeSource) stepLoot(account string, bot int) {
	if f.rng.Intn(90) == 0 {
		f.addFakeLoot(account, fakeRandomEvents[f.rng.Intn(len(fakeRandomEvents))], 1)
	}
	// Fishing, on every account whatever its script: the real fleet fishes on 267 of 269, and
	// a panel whose headline feature is the Fishing spot card wants it populated under -fake.
	// Drawn from f.fishRNG, not f.rng — see that field's comment for what sharing the stream
	// with every other simulated behaviour broke.
	if f.fishRNG.Intn(3) == 0 {
		f.addFakeLootWith(f.fishRNG, account, fakeFishingSpots[f.fishRNG.Intn(len(fakeFishingSpots))], 1+f.fishRNG.Intn(5), false)
	}
	// The other two gathering skills, so all three cards have something behind them under -fake.
	// Rarer than fishing, as they are on the real fleet.
	if f.fishRNG.Intn(9) == 0 {
		f.addFakeLootWith(f.fishRNG, account, fakeWoodcutting[f.fishRNG.Intn(len(fakeWoodcutting))], 1+f.fishRNG.Intn(4), false)
	}
	if f.fishRNG.Intn(11) == 0 {
		f.addFakeLootWith(f.fishRNG, account, fakeMining[f.fishRNG.Intn(len(fakeMining))], 1+f.fishRNG.Intn(4), false)
	}
	// Fletching, so the Production card has something behind it under -fake. Rarer than chopping
	// because one batch cuts a whole pack of logs at once rather than one per swing, and larger
	// when it lands, because every log is fifteen shafts.
	if f.fishRNG.Intn(23) == 0 {
		f.addFakeLootWith(f.fishRNG, account, fakeFletching[f.fishRNG.Intn(len(fakeFletching))], 1+f.fishRNG.Intn(20), false)
	}
	// A rare Kalphite Queen kill, on the same separate stream and with the mule left alone, so
	// the two-form card has something behind it under -fake without disturbing anything else.
	if f.fishRNG.Intn(240) == 0 {
		f.addFakeLootWith(f.fishRNG, account, fakeKalphiteQueen[f.fishRNG.Intn(len(fakeKalphiteQueen))], 1, false)
	}
	// A fake crafter at the altars and fake miners handing essence to the mule, so the Runecrafting section and the Essence
	// supply card have something behind them under -fake. On their own stream (runeRNG) for the reason fishRNG is separate.
	if f.runeRNG.Intn(14) == 0 {
		f.addFakeRunecraft(account)
	}
	if f.runeRNG.Intn(40) == 0 {
		f.addFakeHandover(account)
	}
	if f.rng.Intn(6) != 0 {
		return
	}
	sources := fakeLootSources[bot%len(fakeLootSources)]
	s := sources[f.rng.Intn(len(sources))]
	events := 1
	if s.src.Kind == loot.KindOpenable {
		events = 1 + f.rng.Intn(6) // a burst of opens
	}
	f.addFakeLoot(account, s, events)
}

// fakeAltars are the three altars the fleet crafts at, with content 289's own rune ids and names, the experience a
// essence pays there in tenths (runecraftplan: Air 5.0, Body 7.5, Nature 9.0) and a plausible market value; the rates are
// plausible, not the game's.
var fakeAltars = []struct {
	altar string
	rune  int
	name  string
	xp10  int
	value int64
}{
	{"Air", 556, "Air rune", 50, 4},
	{"Body", 559, "Body rune", 75, 1},
	{"Nature", 561, "Nature rune", 90, 220},
}

// tallyLocked is account's loot Tally, made when it has none. Caller holds f.mu.
func (f *FakeSource) tallyLocked(account string) *loot.Tally {
	if f.loot == nil {
		f.loot = map[string]*loot.Tally{}
	}
	t := f.loot[account]
	if t == nil {
		t = loot.NewTally(f.now)
		f.loot[account] = t
	}
	return t
}

// addFakeRunecraft is one fake craft of 27 essence, through the real constructor (loot.RuneCrafted) so the fake fleet has
// exactly the shape the live one records. Caller holds f.mu.
func (f *FakeSource) addFakeRunecraft(account string) {
	rng := f.runeRNG
	a := fakeAltars[rng.Intn(len(fakeAltars))]
	const essence = 27
	runes := essence * (1 + rng.Intn(3)) // 1 to 3 runes an essence, by level
	level := 20 + rng.Intn(25)
	t := f.tallyLocked(account)
	loot.RuneCrafted(func(d loot.Drop) {
		// The cache's unit cost, which loot.RuneCrafted takes from the config the fake fleet does not have.
		for i := range d.Items {
			d.Items[i].Value = a.value
		}
		for i := range d.Spent {
			d.Spent[i].Value = 25
		}
		t.Add(d)
	}, nil, a.altar, a.rune, a.name, essence, runes, essence*a.xp10/10, level)
}

// addFakeHandover is one fake hand-over of a note stack to the mule, through the real constructor. Caller holds f.mu.
func (f *FakeSource) addFakeHandover(account string) {
	n := 1000 + f.runeRNG.Intn(2000)
	f.essenceHanded += int64(n)
	loot.EssenceHandedOver(f.tallyLocked(account).Add, n)
}

// addFakeLoot rolls events of s for account and tallies them. Caller holds f.mu.
func (f *FakeSource) addFakeLoot(account string, s fakeLootSource, events int) {
	f.addFakeLootWith(f.rng, account, s, events, true)
}

// addFakeLootWith is addFakeLoot drawing from rng, and driving the simulated mule hand-off only
// when mule is set.
//
// Both knobs exist for the fishing sources and both matter. The rng because every simulated
// behaviour here shares one seeded stream, so extra draws shift all the others (see
// FakeSource.fishRNG). The mule flag because the simulated hand-off cadence was calibrated
// against the sources that existed before fishing did; feeding it a third, much busier source
// as well would quietly change what the Mule dashboard shows under -fake. Fishing is additive
// to the Loot panel and changes nothing else.
//
// Caller holds f.mu.
func (f *FakeSource) addFakeLootWith(rng *rand.Rand, account string, s fakeLootSource, events int, mule bool) {
	gained := map[int]*loot.Item{}
	for e := 0; e < events; e++ {
		rollDrops(rng, s, gained)
	}
	items := make([]loot.Item, 0, len(gained))
	for _, it := range gained {
		items = append(items, *it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if f.loot == nil {
		f.loot = map[string]*loot.Tally{}
	}
	t := f.loot[account]
	if t == nil {
		t = loot.NewTally(f.now)
		f.loot[account] = t
	}
	t.Add(loot.Drop{Source: s.src, Events: events, Items: items})
	// Every bone a fake kill drops is buried, as ScarabSwarm and CombatTrainer bury theirs, so the
	// Prayer card has something behind it under -fake. Derived from the kill's own roll, so it
	// draws nothing from any random stream and shifts no other simulated behaviour. The XP is
	// content's own per bone (bankxp: 4.5 for Bones, 15 for Big bones), in whole points.
	for _, it := range items {
		if xp10, ok := fakeBuryXP10[it.ID]; ok && s.src.Action == loot.ActionKill {
			t.Add(loot.Drop{Source: loot.Bury(nil, it.ID, it.Name), Events: int(it.Count), XP: int(it.Count * xp10 / 10)})
		}
	}
	if mule {
		f.stepMuleTrade(account, items)
	}
}

// fakeBuryXP10 is the Prayer experience of the bones fake kills drop, in tenths of a point.
var fakeBuryXP10 = map[int]int64{526: 45, 532: 150}

// fakeMule is the simulated mule every fake donor hands off to.
const fakeMule = "mule"

// fakeTradeFailures are plausible reasons a simulated hand-off fails.
var fakeTradeFailures = []string{"confirm screen timeout", "no stable offer", "random event before confirm"}

// stepMuleTrade occasionally records a simulated hand-off of what account just looted to the
// fake mule, so the mule ledger has data under -fake. Caller holds f.mu.
func (f *FakeSource) stepMuleTrade(account string, items []loot.Item) {
	if f.ledger == nil {
		f.ledger = muleledger.Open("", 0)
		f.ledger.SetClock(f.now)
	}
	if f.rng.Intn(8) != 0 {
		return
	}
	e := muleledger.Entry{Mule: fakeMule, Partner: account, Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted}
	switch {
	case f.rng.Intn(6) == 0:
		e.Outcome, e.Reason = muleledger.OutcomeFailed, fakeTradeFailures[f.rng.Intn(len(fakeTradeFailures))]
	case f.rng.Intn(15) == 0:
		e.Kind, e.Partner = muleledger.KindHandout, account
		e.Out = []muleledger.Item{{ID: 301, Name: "Lobster pot", Count: 1}}
	default:
		for _, it := range items {
			e.In = append(e.In, muleledger.Item{ID: it.ID, Name: it.Name, Count: it.Count})
		}
		e.In = append(e.In, muleledger.Item{ID: 360, Name: "Raw tuna", Count: int64(20 + f.rng.Intn(200))})
	}
	f.ledger.Record(e)
}

// MuleTrades answers MuleLedgerSource for the fake fleet.
func (f *FakeSource) MuleTrades(q muleledger.Query) []muleledger.Entry {
	f.mu.Lock()
	l := f.ledger
	f.mu.Unlock()
	return l.Entries(q)
}

func rollDrops(rng *rand.Rand, s fakeLootSource, into map[int]*loot.Item) {
	add := func(d fakeDrop) {
		n := d.min
		if d.max > d.min {
			n += rng.Int63n(d.max - d.min + 1)
		}
		if n <= 0 {
			return
		}
		it := into[d.id]
		if it == nil {
			it = &loot.Item{ID: d.id, Name: d.name, Value: d.value}
			into[d.id] = it
		}
		it.Count += n
	}
	if s.src.Kind == loot.KindOpenable || s.src.Kind == loot.KindRandomEvent && !allCertain(s.drops) {
		// Exactly one reward per open: walk the table rarest-first so rare rolls get their chance.
		order := append([]fakeDrop(nil), s.drops...)
		sort.SliceStable(order, func(i, j int) bool { return order[i].chance > order[j].chance })
		for _, d := range order {
			if d.chance <= 1 || rng.Intn(d.chance) == 0 {
				add(d)
				return
			}
		}
		add(order[len(order)-1]) // nothing rarer came up: the commonest reward
		return
	}
	for _, d := range s.drops {
		if d.chance <= 1 || rng.Intn(d.chance) == 0 {
			add(d)
		}
	}
}

// allCertain reports whether every drop always comes (the Drunken Dwarf's kebab and beer).
func allCertain(drops []fakeDrop) bool {
	for _, d := range drops {
		if d.chance > 1 {
			return false
		}
	}
	return true
}

// LootTotals answers LootSource for the fake fleet.
func (f *FakeSource) LootTotals() []fleet.AccountLoot {
	f.mu.Lock()
	names := make([]string, 0, len(f.loot))
	tallies := make(map[string]*loot.Tally, len(f.loot))
	for name, t := range f.loot {
		names = append(names, name)
		tallies[name] = t
	}
	f.mu.Unlock()
	sort.Strings(names)
	out := make([]fleet.AccountLoot, 0, len(names))
	for _, name := range names {
		t := tallies[name]
		out = append(out, fleet.AccountLoot{Account: name, Epoch: t.Epoch(), Sources: t.Snapshot()})
	}
	return out
}
