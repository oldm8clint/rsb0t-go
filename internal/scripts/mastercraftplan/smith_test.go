package mastercraftplan

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func smithSet() Settings { return DefaultSettings() }

func ownBars(m map[string]int) SmithStock { return func(k string) int { return m[k] } }

// THE OWNER'S TRIGGER (8 Oct 2026): "must have over 8000 steel bars to trigger it" and "it should get all bars
// out when there are 8000 not just small amounts". 8000 exactly is not over; 8001 starts a round; the round
// then keeps going below 8000 until the bank holds none.
func TestSmithStartsOverEightThousandSteelBars(t *testing.T) {
	set := smithSet()
	if set.SmithMinSteelBars != 8000 {
		t.Fatalf("default threshold %d, want 8000", set.SmithMinSteelBars)
	}
	_, ok := SmithReady(set, 60, false, stockMap(map[string]int{"Steel bar": 8000}), nil)
	if ok {
		t.Error("8000 Steel bars started a round: it must be over 8000")
	}
	p, ok := SmithReady(set, 60, false, stockMap(map[string]int{"Steel bar": 8001, "Iron bar": 20}), nil)
	if !ok || p.Resume || p.Steel != 8001 || p.Iron != 20 {
		t.Fatalf("8001: %+v %v", p, ok)
	}
	// The two banks together count: 5000 here and 3001 at the mule.
	if _, ok := SmithReady(set, 60, false, stockMap(map[string]int{"Steel bar": 3001}), ownBars(map[string]int{"Steel bar": 5000})); !ok {
		t.Error("5000 own and 3001 at the mule did not start a round")
	}
	// Mithril, Adamantite or Runite bars alone never start one.
	if _, ok := SmithReady(set, 99, false, stockMap(map[string]int{"Mithril bar": 90000, "Runite bar": 9000, "Iron bar": 90000}), nil); ok {
		t.Error("a round started without Steel bars over the threshold")
	}
}

// Started at 8001, the round is "active" and keeps going below 8000 until the bank has none of a bar it
// can smith: the mule's stock and the threshold no longer matter.
func TestSmithKeepsGoingBelowEightThousandUntilTheBankIsEmpty(t *testing.T) {
	set := smithSet()
	mule := stockMap(map[string]int{"Steel bar": 8001})
	p, ok := SmithReady(set, 60, false, mule, nil)
	if !ok || p.Resume {
		t.Fatalf("start: %+v %v", p, ok)
	}
	// The round has drawn everything: the bank holds 8001, then smiths down.
	for _, steel := range []int{8001, 7999, 4000, 100, 1} {
		p, ok := SmithReady(set, 60, true, nil, ownBars(map[string]int{"Steel bar": steel}))
		if !ok || !p.Resume {
			t.Fatalf("with %d Steel bars left in the bank and the round active: ready %v resume %v", steel, ok, p.Resume)
		}
	}
	// None left: not ready (the caller clears the active flag), and a fresh start needs the threshold again.
	if p, ok := SmithReady(set, 60, true, stockMap(map[string]int{"Steel bar": 500}), ownBars(map[string]int{"Steel bar": 0})); ok {
		t.Errorf("an empty bank still resumes: %+v", p)
	}
	// Iron bars for platebodies count too: 5 make one, 4 do not.
	if _, ok := SmithReady(set, 60, true, nil, ownBars(map[string]int{"Iron bar": 5})); !ok {
		t.Error("5 Iron bars left in an active round did not resume")
	}
	if _, ok := SmithReady(set, 60, true, nil, ownBars(map[string]int{"Iron bar": 4})); ok {
		t.Error("4 Iron bars made a platebody round")
	}
	// A restart that has not read the bank carries an active round on; an inactive one does not.
	if p, ok := SmithReady(set, 60, true, nil, nil); !ok || !p.Resume {
		t.Errorf("active, bank unread: %+v %v", p, ok)
	}
	if _, ok := SmithReady(set, 60, false, nil, nil); ok {
		t.Error("an inactive round started from an unread bank and no mule")
	}
	// An inactive ledger never resumes on leftovers.
	if _, ok := SmithReady(set, 60, false, nil, ownBars(map[string]int{"Steel bar": 500})); ok {
		t.Error("leftovers started a round when none was active")
	}
}

func TestSmithProductsBestBarFirstByLevel(t *testing.T) {
	set := smithSet()
	names := func(ps []SmithProduct) string {
		var n []string
		for _, p := range ps {
			n = append(n, p.Name)
		}
		return strings.Join(n, ",")
	}
	if got := names(SmithProductsFor(set, 99)); got != "Rune arrowtips,Adamant arrowtips,Mithril arrowtips,Steel arrowtips,Iron platebody" {
		t.Errorf("at 99: %s", got)
	}
	if got := names(SmithProductsFor(set, 60)); got != "Mithril arrowtips,Steel arrowtips,Iron platebody" {
		t.Errorf("at 60: %s", got)
	}
	if got := names(SmithProductsFor(set, 34)); got != "Iron platebody" {
		t.Errorf("at 34: %s", got)
	}
	set.MembersWorld = false
	if got := names(SmithProductsFor(set, 99)); got != "Iron platebody" {
		t.Errorf("on a free world: %s", got)
	}
	set = smithSet()
	set.SmithIronPlatebodies = false
	if got := names(SmithProductsFor(set, 40)); got != "Steel arrowtips" {
		t.Errorf("platebodies off: %s", got)
	}
	// Under Smithing 35 the Steel trigger cannot be met.
	if p, ok := SmithReady(smithSet(), 34, false, stockMap(map[string]int{"Steel bar": 20000}), nil); ok || !strings.Contains(p.Why, "35") {
		t.Errorf("Smithing 34: %v %q", ok, p.Why)
	}
}

// Content 289's numbers (smithing.dbrow, smithing.inv, smithing.struct).
func TestSmithProductsAreContents(t *testing.T) {
	want := map[string][5]int{ // level, bars per make, units, component, slot
		"Rune arrowtips": {90, 1, 15, 1123, 1}, "Adamant arrowtips": {75, 1, 15, 1123, 1},
		"Mithril arrowtips": {55, 1, 15, 1123, 1}, "Steel arrowtips": {35, 1, 15, 1123, 1},
		"Iron platebody": {33, 5, 1, 1121, 3},
	}
	for _, p := range SmithAllProducts() {
		w, ok := want[p.Name]
		if !ok {
			t.Errorf("unexpected product %s", p.Name)
			continue
		}
		if got := [5]int{p.Level, p.BarsPer, p.Per, p.Com, p.Slot}; got != w {
			t.Errorf("%s: %v, content says %v", p.Name, got, w)
		}
		delete(want, p.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing %v", want)
	}
	if SmithArrowtips[3].ID != ArrowTipsID {
		t.Errorf("Steel arrowtips id %d, the arrow round reads %d", SmithArrowtips[3].ID, ArrowTipsID)
	}
	for _, p := range SmithAllProducts()[:4] {
		if p.Bar == IronBar {
			t.Errorf("%s uses Iron", p.Name)
		}
	}
}

// The kit asks for every bar the products use that the mule holds, up to the kit cap, and a hammer.
func TestPlanSmithAskTakesEveryBar(t *testing.T) {
	set := smithSet()
	products := SmithProductsFor(set, 99)
	mule := stockMap(map[string]int{"Steel bar": 8471, "Mithril bar": 2554, "Runite bar": 546, "Iron bar": 10281, "Hammer": 5, "Silver bar": 187, "Gold bar": 12})
	lines := PlanSmithAsk(set, products, mule, ownBars(nil), false)
	var got []string
	for _, l := range lines {
		got = append(got, l.Item+"="+strconv.Itoa(l.Qty))
	}
	if g := strings.Join(got, ","); g != "Runite bar=546,Mithril bar=2554,Steel bar=8471,Iron bar=10281,Hammer=1" {
		t.Errorf("ask %s", g)
	}
	set.SmithKit = 1000
	if l := PlanSmithAsk(set, products, mule, ownBars(map[string]int{"Hammer": 1}), false); len(l) != 4 || l[2].Qty != 1000 {
		t.Errorf("capped, hammer owned: %v", l)
	}
	if l := PlanSmithAsk(set, products, mule, ownBars(nil), true); len(l) != 4 {
		t.Errorf("hammer in the pack: %v", l)
	}
}

// A trip's pack is full: the best bar first, the next filling what is left, whole makes of five Iron bars.
func TestPlanSmithDrawsFillsThePackBestBarFirst(t *testing.T) {
	products := SmithProductsFor(smithSet(), 99)
	bank := ownBars(map[string]int{"Runite bar": 3, "Mithril bar": 100, "Steel bar": 8000, "Iron bar": 900})
	say := func(ds []SmithDraw) string {
		var s []string
		for _, d := range ds {
			s = append(s, d.Product.Bar+"="+strconv.Itoa(d.N))
		}
		return strings.Join(s, ",")
	}
	if got := say(PlanSmithDraws(products, bank, 26)); got != "Runite bar=3,Mithril bar=23" {
		t.Errorf("26 free: %s", got)
	}
	if got := say(PlanSmithDraws(products, ownBars(map[string]int{"Steel bar": 8000, "Iron bar": 900}), 26)); got != "Steel bar=26" {
		t.Errorf("steel only: %s", got)
	}
	if got := say(PlanSmithDraws(products, ownBars(map[string]int{"Steel bar": 4, "Iron bar": 900}), 26)); got != "Steel bar=4,Iron bar=20" {
		t.Errorf("4 steel then iron in whole makes: %s", got)
	}
	if got := say(PlanSmithDraws(products, ownBars(map[string]int{"Iron bar": 3}), 26)); got != "" {
		t.Errorf("3 iron bars: %s", got)
	}
}

func TestSmithLoadAndButtons(t *testing.T) {
	steel, iron := SmithArrowtips[3], SmithIronPlatebody
	if n := SmithLoad(steel, 26, 8000); n != 26 {
		t.Errorf("steel load %d", n)
	}
	if n := SmithLoad(steel, 26, 9); n != 9 {
		t.Errorf("short bank %d", n)
	}
	if n := SmithLoad(iron, 26, 8000); n != 25 {
		t.Errorf("iron load %d, want whole makes of five", n)
	}
	if n := SmithLoad(iron, 26, 4); n != 0 {
		t.Errorf("4 iron bars %d", n)
	}
	for makes, want := range map[int][2]int{26: {3, 10}, 10: {3, 10}, 9: {2, 5}, 5: {2, 5}, 4: {1, 1}, 1: {1, 1}} {
		if o, c := SmithButton(makes); [2]int{o, c} != want {
			t.Errorf("%d makes: button %d count %d, want %v", makes, o, c, want)
		}
	}
}

// Superheating makes Steel and up only: no Bronze, Iron or Silver, whatever the ore (a user request), and
// gold stays as it was.
func TestSuperheatMakesSteelAndUpOnly(t *testing.T) {
	set := smithSet()
	var names []string
	for _, b := range SuperheatBars(set, 99) {
		names = append(names, b.Name)
	}
	if got := strings.Join(names, ","); got != "Runite bar,Adamantite bar,Mithril bar,Steel bar" {
		t.Errorf("bars %s", got)
	}
	set.SuperheatGold = true
	for _, b := range SuperheatBars(set, 99) {
		if b.Gold {
			t.Error("gold with no gauntlets held")
		}
	}
	set.GauntletsHeld = true
	names = nil
	for _, b := range SuperheatBars(set, 99) {
		names = append(names, b.Name)
	}
	if got := strings.Join(names, ","); got != "Runite bar,Adamantite bar,Mithril bar,Gold bar,Steel bar" {
		t.Errorf("with gold %s", got)
	}
	set.SuperheatLowBars = true
	if got := len(SuperheatBars(set, 99)); got != 8 {
		t.Errorf("with low bars %d bars", got)
	}
	// Iron ore with no coal makes nothing; with coal it is Steel.
	set = smithSet()
	have := func(m map[string]int) SuperheatStock { return func(k string) int { return m[k] } }
	if p := PlanSuperheat(set, 99, have(map[string]int{"Iron ore": 500, "Copper ore": 500, "Tin ore": 500, "Silver ore": 500, "Nature rune": 900}), true); p.Casts != 0 {
		t.Errorf("planned %+v from ores that only make Bronze, Iron and Silver", p)
	}
	if p := PlanSuperheat(set, 99, have(map[string]int{"Iron ore": 500, "Coal": 200, "Nature rune": 900}), true); p.Bar.Name != "Steel bar" || p.Casts != 100 || p.Total != 100 {
		t.Errorf("steel: %+v", p)
	}
}

func TestDecideStartsAnAnvilRoundAndCarriesItOn(t *testing.T) {
	set := smithSet()
	set.Arrows, set.Herbs, set.Superheat = false, false, false
	set.CannonballTarget = 0 // the cannonball round would take the Steel bars first (cannonballs_test.go)
	s := readySnapshot(map[string]int{"Steel bar": 8001})
	d := Decide(set, s)
	if d.Mode != ModeSmith || d.Resume || d.Smith.Steel != 8001 {
		t.Fatalf("got %+v", d)
	}
	// Active in the ledger with the bars in the account's own bank: resumes, chains straight on.
	s = readySnapshot(map[string]int{})
	s.Ledger.SmithActive = true
	s.OwnSmith = ownBars(map[string]int{"Steel bar": 40})
	d = Decide(set, s)
	if d.Mode != ModeSmith || !d.Resume || !Chain(ModeSmith, d) {
		t.Fatalf("resume: %+v", d)
	}
	// A pause or a backoff holds it off.
	s.Ledger.SmithPauseUntil = supplyNow.Add(time.Hour)
	if d = Decide(set, s); d.Mode == ModeSmith {
		t.Errorf("paused round decided: %+v", d)
	}
	// Switched off.
	set.Smith = false
	s = readySnapshot(map[string]int{"Steel bar": 20000})
	if d = Decide(set, s); d.Mode == ModeSmith {
		t.Errorf("craftSmith off: %+v", d)
	}
}

func TestSmithSettingsDecode(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{"craftSmith": "true", "craftSmithMinSteelBars": 9000.0, "craftSmithIronPlatebodies": false, "craftSmithKit": "5000", "craftSuperheatLowBars": true})
	if err != nil || !s.Smith || s.SmithMinSteelBars != 9000 || s.SmithIronPlatebodies || s.SmithKit != 5000 || !s.SuperheatLowBars {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := DecodeSettings(map[string]interface{}{"craftSmithKit": 0}); err == nil {
		t.Error("kit 0 accepted")
	}
	if _, err := DecodeSettings(map[string]interface{}{"craftSmithArrowtips": false}); err == nil {
		t.Error("smith on with arrowtips off accepted")
	}
	if d := DefaultSettings(); !d.Smith || d.SmithMinSteelBars != 8000 || !d.SmithArrowtips || !d.SmithIronPlatebodies || d.SuperheatLowBars {
		t.Errorf("defaults %+v", d)
	}
}

// A new round asks for one rub item when the account holds none and the mule does (owner: "must use
// jewellery stall running back and forth"; live 8 Oct the first round had none).
func TestPlanSmithAskAddsARubItemWhenNoneIsHeld(t *testing.T) {
	mule := func(item string) (int, bool) {
		switch item {
		case "Steel bar":
			return 9000, true
		case "Ring of dueling(8)":
			return 40, true
		}
		return 0, false
	}
	own := func(string) int { return 0 }
	set := DefaultSettings()
	lines := PlanSmithAsk(set, SmithProductsFor(set, 64), mule, own, true)
	found := false
	for _, l := range lines {
		if l.Item == "Ring of dueling(8)" && l.Qty == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("lines %+v: want one Ring of dueling(8)", lines)
	}
	ownRing := func(item string) int {
		if item == "Ring of dueling(3)" {
			return 1
		}
		return 0
	}
	for _, l := range PlanSmithAsk(set, SmithProductsFor(set, 64), mule, ownRing, true) {
		if l.Item == "Ring of dueling(8)" {
			t.Fatal("asked for a ring while one is held")
		}
	}
}
