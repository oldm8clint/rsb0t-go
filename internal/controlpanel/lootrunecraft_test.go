package controlpanel

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// The Runecrafting section of the Loot panel (a user request: "runecrafting loot panel is needed") as /api/loot delivers
// it. internal/loot and runecraftrunner prove the Drops; these prove the aggregator keeps them (lifetime, persisted, since the
// restart), nets the essence off the runes, and reads the supply side, per account.

var rcBase = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// rcBook prices what the section values: the runes the altars make and the Rune essence they bind. Every price is confident
// (nine trades, coherent), so the essence's cost is known.
func rcBook() *PriceBook {
	return auditBook(
		MarketPrice{Name: "Air rune", Coins: 5},
		MarketPrice{Name: "Body rune", Coins: 4},
		MarketPrice{Name: "Nature rune", Coins: 200},
		MarketPrice{Name: "Rune essence", Coins: 20},
	)
}

// crafted is one verified craft built by the real constructor, the way runecraftrunner.Craft reports it.
func crafted(altar string, runeID int, runeName string, essence, runes, xp, level int) loot.Drop {
	var d loot.Drop
	loot.RuneCrafted(func(x loot.Drop) { d = x }, nil, altar, runeID, runeName, essence, runes, xp, level)
	return d
}

// totalsOf folds drops through a real Tally, exactly as one account's fleet tally does, and returns what the aggregator polls.
func totalsOf(drops ...loot.Drop) []loot.Totals {
	tally := loot.NewTally(func() time.Time { return rcBase })
	for _, d := range drops {
		tally.Add(d)
	}
	return tally.Snapshot()
}

func essenceMined(n int64) loot.Totals {
	return loot.Totals{Source: loot.Mining(nil, 2491, "Rune Essence"), Events: n, Items: []loot.Item{{ID: 1436, Name: "Rune essence", Count: n, Value: 25}}}
}

func handedOver(n int) []loot.Totals {
	var d loot.Drop
	loot.EssenceHandedOver(func(x loot.Drop) { d = x }, n)
	return totalsOf(d)
}

// seedRunecraft is the fleet the section tests read: the main account crafted four times (Air twice, Body, Nature), one miner mined 5,000
// essence and handed 2,021 to the mule, another mined 3,000 and has handed none.
func seedRunecraft(a *statsAggregator) {
	mainAcct := totalsOf(
		crafted("Air", 556, "Air rune", 27, 54, 135, 18),
		crafted("Air", 556, "Air rune", 27, 54, 135, 19),
		crafted("Body", 559, "Body rune", 27, 54, 202, 20),
		crafted("Nature", 561, "Nature rune", 27, 27, 243, 44),
	)
	a.loot.record(lootReading("main", 1, mainAcct...), rcBase)
	miner1 := append([]loot.Totals{essenceMined(5000)}, handedOver(2021)...)
	a.loot.record(lootReading("miner1", 1, miner1...), rcBase)
	a.loot.record(lootReading("miner2", 1, essenceMined(3000)), rcBase)
}

// TestTheRunecraftSectionReadsTheCraftsTheEssenceAndTheSupply: every figure of the owner's list, from records built by the
// real constructors.
func TestTheRunecraftSectionReadsTheCraftsTheEssenceAndTheSupply(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(rcBase))
	a.loot.prices = rcBook()
	seedRunecraft(a)

	rc := a.lootSnapshot().Lifetime.Runecraft
	if rc == nil {
		t.Fatal("no Runecraft block")
	}
	// 4 crafts of 27 essence; 54+54+54+27 runes; 135+135+202+243 experience; the highest level read.
	if rc.Crafts != 4 || rc.Essence != 108 || rc.Runes != 189 || rc.XP != 715 || rc.Level != 44 || rc.Accounts != 1 {
		t.Errorf("crafts %d essence %d runes %d xp %d level %d accounts %d", rc.Crafts, rc.Essence, rc.Runes, rc.XP, rc.Level, rc.Accounts)
	}
	// The runes at the market: 108 Air at 5, 54 Body at 4, 27 Nature at 200. The essence at 20 each.
	if rc.Gross != 540+216+5400 || rc.Cost != 108*20 || !rc.CostKnown || rc.Net != 540+216+5400-108*20 {
		t.Errorf("gross %d cost %d known %v net %d", rc.Gross, rc.Cost, rc.CostKnown, rc.Net)
	}
	// By rune, the most valuable first, with the unit price the panel uses everywhere.
	if len(rc.ByRune) != 3 {
		t.Fatalf("by rune: %+v", rc.ByRune)
	}
	for i, want := range []RunecraftRune{
		{ID: 561, Name: "Nature rune", Count: 27, Unit: 200, Value: 5400},
		{ID: 556, Name: "Air rune", Count: 108, Unit: 5, Value: 540},
		{ID: 559, Name: "Body rune", Count: 54, Unit: 4, Value: 216},
	} {
		if rc.ByRune[i] != want {
			t.Errorf("rune %d = %+v, want %+v", i, rc.ByRune[i], want)
		}
	}
	// By altar, the busiest first.
	if len(rc.Altars) != 3 || rc.Altars[0].Name != "Air altar" || rc.Altars[0].Crafts != 2 || rc.Altars[0].Essence != 54 || rc.Altars[0].Runes != 108 ||
		rc.Altars[0].XP != 270 || rc.Altars[0].Level != 19 || rc.Altars[1].Name != "Body altar" || rc.Altars[2].Name != "Nature altar" || rc.Altars[2].Level != 44 {
		t.Errorf("altars: %+v", rc.Altars)
	}
	// The supply: what was mined by both miners, what one handed to the mule.
	if s := rc.Supply; s.Mined != 8000 || s.MinedAccounts != 2 || s.Handed != 2021 || s.HandedAccounts != 1 || s.MinedFirst == 0 || s.HandedFirst == 0 {
		t.Errorf("supply: %+v", s)
	}
	if rc.First == 0 || rc.Last < rc.First {
		t.Errorf("span %d..%d", rc.First, rc.Last)
	}
	// Per account: the crafter first (it has value), then the miner who handed over, then the other.
	if len(rc.PerAcct) != 3 {
		t.Fatalf("per account: %+v", rc.PerAcct)
	}
	c, m1, m2 := rc.PerAcct[0], rc.PerAcct[1], rc.PerAcct[2]
	if c.Account != "main" || c.Crafts != 4 || c.Essence != 108 || c.Runes != 189 || c.XP != 715 || c.Level != 44 || c.Value != 6156 || c.Cost != 2160 || c.Mined != 0 || c.Handed != 0 {
		t.Errorf("main: %+v", c)
	}
	if m1.Account != "miner1" || m1.Mined != 5000 || m1.Handed != 2021 || m1.Crafts != 0 || m2.Account != "miner2" || m2.Mined != 3000 || m2.Handed != 0 {
		t.Errorf("miners: %+v %+v", m1, m2)
	}
}

// TestTheRunecraftCardIsNetOfTheEssenceItBoundAndTheHandoverIsNotLoot: the generic card is there with no code of its own, its
// rows are the altars, its essence is netted as a spend, its experience counts toward the skill, and a hand-over adds no item
// and no gp anywhere.
func TestTheRunecraftCardIsNetOfTheEssenceItBoundAndTheHandoverIsNotLoot(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(rcBase))
	a.loot.prices = rcBook()
	seedRunecraft(a)
	p := a.lootSnapshot().Lifetime

	rows := cards(t, p)[loot.Runecrafting("Air", 556).GroupKey()]
	if len(rows) != 3 {
		t.Fatalf("the Runecraft card has %d rows, want one a altar: %+v", len(rows), rows)
	}
	air, ok := sourceView(p, loot.Runecrafting("Air", 556))
	if !ok {
		t.Fatal("no Air altar row")
	}
	// Two crafts, 54 essence bound: priced as a cost, the way the runes cast at an npc are.
	if air.Events != 2 || air.XP != 270 || itemCount(air, 556) != 108 || !air.CostKnown || air.CostUnits != 54 || air.Cost != 54*20 || air.CostItem != "Rune essence" {
		t.Errorf("air row: events %d xp %d runes %d cost %d x %d known %v item %q", air.Events, air.XP, itemCount(air, 556), air.Cost, air.CostUnits, air.CostKnown, air.CostItem)
	}
	if air.Source.Level != 19 {
		t.Errorf("air row level %d, want the last read, 19", air.Source.Level)
	}
	if p.Events[loot.ActionRunecraft] != 4 || p.XP[loot.SkillRunecraft] != 715 {
		t.Errorf("events %v xp %v", p.Events, p.XP)
	}

	// the main account's account row: runes at the market, less the essence.
	for _, av := range p.Accounts {
		if av.Account != "main" {
			continue
		}
		if av.Gross != 6156 || av.Cost != 2160 || av.Value != 6156-2160 || av.Items != 189 {
			t.Errorf("main: gross %d cost %d value %d items %d", av.Gross, av.Cost, av.Value, av.Items)
		}
	}
	// The hand-over moves essence that was counted when it was mined: its card is a count, with no items and no value, so
	// the miner's account is worth exactly what it mined.
	sup, ok := sourceView(p, loot.EssenceHandover())
	if !ok || sup.Events != 2021 || len(sup.Items) != 0 || sup.Cost != 0 || sup.CostItem != "" {
		t.Errorf("essence supply row: %+v (found %v)", sup, ok)
	}
	for _, av := range p.Accounts {
		if av.Account == "miner1" && (av.Items != 5000 || av.Cost != 0 || av.Events != 5000) {
			t.Errorf("miner1: items %d cost %d events %d, want 5,000 mined, nothing netted, and the 2,021 essence handed over not counted as 2,021 more events", av.Items, av.Cost, av.Events)
		}
	}
	if p.Events[loot.ActionHandover] != 2021 {
		t.Errorf("events[handover] = %d", p.Events[loot.ActionHandover])
	}
}

// TestTheRunecraftSectionSurvivesARestartAndStartsAgainSinceTheRestart: the lifetime block is rebuilt from the persisted
// records; the since-restart block holds only this process.
func TestTheRunecraftSectionSurvivesARestartAndStartsAgainSinceTheRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	a1 := newStatsAggregator(path, nil, fixedNow(rcBase))
	a1.loot.prices = rcBook()
	seedRunecraft(a1)
	before := a1.lootSnapshot().Lifetime.Runecraft
	a1.close()

	a2 := newStatsAggregator(path, nil, fixedNow(rcBase.Add(time.Hour)))
	a2.loot.prices = rcBook()
	after := a2.lootSnapshot()
	if after.SinceRestart.Runecraft != nil {
		t.Errorf("the since-restart block is not empty after a restart: %+v", after.SinceRestart.Runecraft)
	}
	rc := after.Lifetime.Runecraft
	if rc == nil || rc.Crafts != before.Crafts || rc.Essence != before.Essence || rc.Runes != before.Runes || rc.XP != before.XP || rc.Level != before.Level ||
		rc.Gross != before.Gross || rc.Cost != before.Cost || rc.Supply != before.Supply || len(rc.PerAcct) != len(before.PerAcct) {
		t.Fatalf("lifetime after the restart %+v, before %+v", rc, before)
	}
	// One more craft after the restart: both windows see it, lifetime on top of what was there.
	a2.loot.record(lootReading("main", 1, totalsOf(crafted("Nature", 561, "Nature rune", 27, 27, 243, 45))...), rcBase.Add(time.Hour))
	snap := a2.lootSnapshot()
	if snap.SinceRestart.Runecraft == nil || snap.SinceRestart.Runecraft.Crafts != 1 || snap.SinceRestart.Runecraft.Level != 45 || snap.SinceRestart.Runecraft.Supply.Mined != 0 {
		t.Errorf("since restart: %+v", snap.SinceRestart.Runecraft)
	}
	if snap.Lifetime.Runecraft.Crafts != before.Crafts+1 || snap.Lifetime.Runecraft.Level != 45 {
		t.Errorf("lifetime: crafts %d level %d", snap.Lifetime.Runecraft.Crafts, snap.Lifetime.Runecraft.Level)
	}
}

// TestNoRunecraftBlockUntilSomethingIsRecorded: a fleet that never crafted, mined essence or handed it over sends no block,
// and the section's JSON keys are absent.
func TestNoRunecraftBlockUntilSomethingIsRecorded(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(rcBase))
	a.loot.record(lootReading("fisher1", 1, casketTotals(4, 80, 2)), rcBase)
	// Ore from an ordinary rock is mining, not essence.
	a.loot.record(lootReading("miner9", 1, loot.Totals{Source: loot.Mining(nil, 450, "Rocks"), Events: 3, Items: []loot.Item{{ID: 436, Name: "Copper ore", Count: 3, Value: 12}}}), rcBase)
	body := a.lootSnapshot()
	if body.Lifetime.Runecraft != nil || body.SinceRestart.Runecraft != nil || body.EssenceStock != nil {
		t.Errorf("a block for a fleet that never runecrafted: %+v %+v %+v", body.Lifetime.Runecraft, body.SinceRestart.Runecraft, body.EssenceStock)
	}
}

// TestAnEssenceNobodyPricesLeavesTheSectionGross: no trustworthy price for the essence (and none recorded arriving) is
// unknown, not free: nothing is subtracted and the block says so.
func TestAnEssenceNobodyPricesLeavesTheSectionGross(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(rcBase))
	a.loot.prices = auditBook(MarketPrice{Name: "Air rune", Coins: 5})
	a.loot.record(lootReading("main", 1, totalsOf(crafted("Air", 556, "Air rune", 27, 54, 135, 3))...), rcBase)
	rc := a.lootSnapshot().Lifetime.Runecraft
	if rc == nil || rc.CostKnown || rc.Cost != 0 || rc.Gross != 270 || rc.Net != 270 || rc.Essence != 27 {
		t.Fatalf("section %+v, want 27 essence of unknown price and the runes gross", rc)
	}
	air, _ := sourceView(a.lootSnapshot().Lifetime, loot.Runecrafting("Air", 556))
	if air.CostKnown || air.Cost != 0 || air.CostItem != "Rune essence" {
		t.Errorf("the row says %+v", air)
	}
}

// A craft whose runes were not seen still records its spend and its experience, and puts no rune on the section.
func TestACraftWithNoRunesSeenStillCountsItsEssenceAndExperience(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(rcBase))
	a.loot.prices = rcBook()
	a.loot.record(lootReading("main", 1, totalsOf(crafted("Air", 556, "Air rune", 27, 0, 135, 7))...), rcBase)
	rc := a.lootSnapshot().Lifetime.Runecraft
	if rc == nil || rc.Crafts != 1 || rc.Essence != 27 || rc.Runes != 0 || rc.XP != 135 || len(rc.ByRune) != 0 || rc.Gross != 0 || rc.Net != -27*20 {
		t.Fatalf("section %+v", rc)
	}
}

// ---- GET /api/loot ----

// runecraftSource is a fleet whose mule's cached bank is readable: what the section's "mule stock" line reads.
type runecraftSource struct {
	*staticSource
	mule    string
	slots   []Slot
	updated int64
}

func (s runecraftSource) MuleStatuses() []MuleStatus { return []MuleStatus{{Account: s.mule}} }
func (s runecraftSource) Bank(account string) ([]Slot, int64, bool) {
	if account != s.mule {
		return nil, 0, false
	}
	return s.slots, s.updated, true
}

func TestTheLootEndpointCarriesTheRunecraftSectionAndTheMulesEssence(t *testing.T) {
	src := runecraftSource{staticSource: &staticSource{}, mule: "mule", updated: rcBase.UnixMilli(),
		// The unnoted essence and a note of it add up; coins are not essence.
		slots: []Slot{{Slot: 0, ID: 995, Name: "Coins", Count: 900000}, {Slot: 1, ID: 1436, Name: "Rune essence", Count: 4087}, {Slot: 2, ID: 1437, Name: "Rune essence", Count: 913}}}
	h := newHarness(t, Config{}, testOptions(), src)
	h.srv.stats.loot.prices = rcBook()
	seedRunecraft(h.srv.stats)

	res := h.get("/api/loot")
	text := bodyText(t, res)
	for _, want := range []string{`"runecraft":{`, `"perAccount":[`, `"byRune":[`, `"altars":[`, `"supply":{`, `"essenceStock":[{"account":"mule","essence":5000,`} {
		if !strings.Contains(text, want) {
			t.Errorf("/api/loot lacks %s", want)
		}
	}
	var body LootBody
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatal(err)
	}
	rc := body.Lifetime.Runecraft
	if rc == nil || rc.Crafts != 4 || rc.Supply.Handed != 2021 || len(body.EssenceStock) != 1 || body.EssenceStock[0].Essence != 5000 || body.EssenceStock[0].Updated != rcBase.UnixMilli() {
		t.Fatalf("decoded: %+v stock %+v", rc, body.EssenceStock)
	}
}

// A fleet with no mule to read (or no section to put its stock in) sends no essenceStock key; one that never runecrafted sends
// no runecraft key.
func TestTheLootEndpointOmitsWhatItCannotSay(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	h.srv.stats.loot.prices = rcBook()
	h.srv.stats.loot.record(lootReading("fisher1", 1, casketTotals(4, 80, 2)), rcBase)
	text := bodyText(t, h.get("/api/loot"))
	if strings.Contains(text, `"runecraft"`) || strings.Contains(text, `"essenceStock"`) {
		t.Errorf("a fleet that never runecrafted carries the section: %.300s", text)
	}
	seedRunecraft(h.srv.stats)
	text = bodyText(t, h.get("/api/loot"))
	if !strings.Contains(text, `"runecraft":{`) || strings.Contains(text, `"essenceStock"`) {
		t.Errorf("a section with no mule to read: %.300s", text)
	}
}

// ---- the simulated fleet ----

// The fake fleet's crafts and hand-overs go through the real constructors, and its mule's bank shows what was handed over.
func TestTheFakeFleetFillsTheRunecraftSectionAndTheMulesStock(t *testing.T) {
	f := NewFakeSource(6, 1)
	f.mu.Lock()
	for i := 0; i < 40; i++ {
		f.addFakeRunecraft("fisher01")
		f.addFakeRunecraft("miner02")
		if i%4 == 0 {
			f.addFakeHandover("miner02")
		}
	}
	handed := f.essenceHanded
	f.mu.Unlock()
	a := newStatsAggregator("", f, fixedNow(rcBase))
	a.loot.record(f.LootTotals(), rcBase)
	body := a.lootSnapshot()
	rc := body.Lifetime.Runecraft
	if rc == nil || rc.Crafts != 80 || rc.Essence != 80*27 || rc.Accounts != 2 || len(rc.ByRune) == 0 || rc.Supply.Handed != handed || rc.Supply.HandedAccounts != 1 || handed == 0 {
		t.Fatalf("section %+v (handed %d)", rc, handed)
	}
	if len(body.EssenceStock) != 1 || body.EssenceStock[0].Account != fakeMuleName || body.EssenceStock[0].Essence != 4087+handed {
		t.Errorf("stock %+v, want %s at %d", body.EssenceStock, fakeMuleName, 4087+handed)
	}
}
