package essenceplan

import (
	"math"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/questplan"
)

// ---- the mining model ----

// The design's per-hour figures (27 essence + 45 ticks of teleport, portal and bank), within 10%.
func TestMiningRateByPickaxe(t *testing.T) {
	cases := []struct {
		item   string
		period int
		perHr  float64
	}{
		{"Bronze pickaxe", 8, 620},
		{"Iron pickaxe", 7, 690},
		{"Steel pickaxe", 6, 780},
		{"Mithril pickaxe", 5, 900},
		{"Adamant pickaxe", 4, 1060},
		{"Rune pickaxe", 3, 1290},
	}
	for _, c := range cases {
		p, ok := PickPeriod(c.item)
		if !ok || p != c.period {
			t.Errorf("%s: period %d ok %v, want %d", c.item, p, ok, c.period)
		}
		got := EssencePerHour(c.item)
		if math.Abs(got-c.perHr)/c.perHr > 0.10 {
			t.Errorf("%s: %.0f essence/h, want %.0f within 10%%", c.item, got, c.perHr)
		}
	}
	// A faster pickaxe is always worth more, and every tier the miner knows has a period.
	prev := 0.0
	for i := len(PickTiers) - 1; i >= 0; i-- { // bronze first
		r := EssencePerHour(PickTiers[i].Item)
		if r <= prev {
			t.Errorf("%s: %.0f/h is not above the tier below it (%.0f)", PickTiers[i].Item, r, prev)
		}
		prev = r
	}
	if _, ok := PickPeriod("Rune axe"); ok || EssencePerHour("Rune axe") != 0 || HoursToMine(100, "Rune axe") != 0 {
		t.Error("an axe is not a pickaxe")
	}
	if p, ok := PickPeriod("  RUNE PICKAXE "); !ok || p != 3 {
		t.Errorf("PickPeriod ignores case and padding: %d %v", p, ok)
	}
}

func TestLoadTicksAndHoursToMine(t *testing.T) {
	if n, ok := LoadTicks("Bronze pickaxe"); !ok || n != 27*8+45 {
		t.Errorf("a bronze load takes %d ticks, want 261", n)
	}
	if _, ok := LoadTicks("Spade"); ok {
		t.Error("a spade has no load time")
	}
	// 5,000 essence at bronze is about eight hours: the muleAt default is a shift.
	if h := HoursToMine(GuildMuleAt, "Bronze pickaxe"); h < 7.5 || h > 8.7 {
		t.Errorf("5,000 essence at bronze = %.1f h, want about 8", h)
	}
	if HoursToMine(0, "Rune pickaxe") != 0 || HoursToMine(-3, "Rune pickaxe") != 0 {
		t.Error("nothing to mine takes no time")
	}
	for _, c := range []struct{ in, want int }{{0, 27}, {1, 27}, {27, 27}, {28, 54}, {270, 270}, {271, 297}, {-5, 27}} {
		if got := RoundUpToLoads(c.in); got != c.want {
			t.Errorf("RoundUpToLoads(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	if b, l, m, c := DefaultBatch, LoadSize, GuildMuleAt, MuleEssenceCap; b != 270 || l != 27 || m != 5000 || c != 150000 {
		t.Errorf("the design's numbers: batch %d load %d muleAt %d cap %d", b, l, m, c)
	}
}

// ---- the Guild's stage machine ----

// The miner's stage machine is the same through every gateway (the mine is one instance); at the Guild it is
// what the stretch loop runs: bank when anything is carried out of the mine, a pickaxe when none is held,
// the teleport when it is, mine until full inside and then the portal.
func TestDecideStageGuild(t *testing.T) {
	g, ok := GatewayByKey(GatewayGuild)
	if !ok {
		t.Fatal("no Guild gateway")
	}
	if ok, _ := g.Allows(65); ok {
		t.Fatal("the Guild must refuse Magic 65")
	}
	if ok, _ := g.Allows(66); !ok {
		t.Fatal("the Guild must allow Magic 66")
	}
	for _, c := range []struct {
		name string
		s    MinerSnapshot
		want MinerStage
	}{
		{"full pack outside the mine goes to the bank", MinerSnapshot{PackFull: true, Haul: 27, PickHeld: true}, StageBank},
		{"any haul outside the mine goes to the bank", MinerSnapshot{Haul: 1, PickHeld: true}, StageBank},
		{"no pickaxe is fetched", MinerSnapshot{}, StagePickaxe},
		{"an empty pack and a pickaxe teleports in", MinerSnapshot{PickHeld: true}, StageTeleport},
		{"inside and not full mines", MinerSnapshot{InMine: true, PickHeld: true, Haul: 5}, StageMine},
		{"inside and full takes the portal", MinerSnapshot{InMine: true, PackFull: true, PickHeld: true, Haul: 27}, StagePortal},
	} {
		if got := DecideStage(c.s); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// The Guild is a gateway the supply loop runs; Aubury keeps the original loop.
	if !(Settings{Gateway: GatewayGuild}).SupplyLoop() || !(Settings{Gateway: GatewayCromperty}).SupplyLoop() || (Settings{Gateway: GatewayAubury}).SupplyLoop() {
		t.Error("SupplyLoop: Guild and Cromperty yes, Aubury no")
	}
	if got := (Settings{Gateway: GatewayGuild}).GatewayInfo(); got.Key != GatewayGuild || got.BankName != "Yanille" {
		t.Errorf("GatewayInfo %+v", got)
	}
}

// ---- the decision ----

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func view() SupplyView {
	return SupplyView{Now: t0, BankKnown: true, MuleAt: 5000, MuleStopAt: 150000, Batch: DefaultBatch, MuleKnown: true}
}

func TestSupplyReadsTheBankFirst(t *testing.T) {
	v := view()
	v.BankKnown = false
	if d := DecideSupply(v); d.Action != SupplyRead {
		t.Errorf("%+v", d)
	}
}

func TestSupplyMinesTowardTheThreshold(t *testing.T) {
	v := view()
	for _, c := range []struct{ bank, goal int }{
		{0, 270},    // a batch
		{4000, 270}, // a batch still short of the threshold
		{4800, 216}, // 200 short: eight loads (216), not a full batch
		{4999, 27},  // one short: a load
	} {
		v.BankEssence = c.bank
		d := DecideSupply(v)
		if d.Action != SupplyMine || d.Goal != c.goal || d.Goal%LoadSize != 0 {
			t.Errorf("bank %d: %+v, want a %d stretch", c.bank, d, c.goal)
		}
	}
}

// muleAt 5,000 starts exactly one note-stack trade, of everything banked; below it nothing is handed over.
func TestMuleHandoffThreshold(t *testing.T) {
	v := view()
	v.BankEssence = 4999
	if d := DecideSupply(v); d.Action == SupplyHandoff {
		t.Fatalf("4,999 banked must not hand over: %+v", d)
	}
	v.BankEssence = 5000
	d := DecideSupply(v)
	if d.Action != SupplyHandoff || d.Send != 5000 {
		t.Fatalf("5,000 banked hands over all 5,000: %+v", d)
	}
	v.BankEssence = 5400
	if d := DecideSupply(v); d.Action != SupplyHandoff || d.Send != 5400 {
		t.Fatalf("the whole stack goes: %+v", d)
	}
	// muleAt 0 never hands over, whatever is banked.
	v.MuleAt, v.BankEssence = 0, 90_000
	if d := DecideSupply(v); d.Action == SupplyHandoff {
		t.Fatalf("muleAt 0: %+v", d)
	}
	// A failed hand-over waits out its retry, and mining goes on meanwhile.
	v = view()
	v.BankEssence = 5200
	v.NextHandoff = t0.Add(5 * time.Minute)
	if d := DecideSupply(v); d.Action != SupplyMine || d.Goal != 270 {
		t.Fatalf("waiting out a retry mines a batch: %+v", d)
	}
	v.Now = t0.Add(5 * time.Minute)
	if d := DecideSupply(v); d.Action != SupplyHandoff {
		t.Fatalf("the retry is due: %+v", d)
	}
}

// muleStopAt 150,000 stops the miner: the mule's own published stock is the measure, and what is banked counts.
func TestMuleStopAtStopsTheMiner(t *testing.T) {
	v := view()
	v.MuleEssence = 149_000
	v.BankEssence = 800
	d := DecideSupply(v)
	if d.Action != SupplyMine || d.Goal != 8*LoadSize { // 200 short of the cap: eight loads (216), a load never short
		t.Fatalf("200 short of the cap: %+v", d)
	}
	// The pipeline (the mule's stock plus the bank) reaches the cap: the bank goes over, up to the room the cap leaves.
	v.BankEssence = 1000
	d = DecideSupply(v)
	if d.Action != SupplyHandoff || d.Send != 1000 || d.Headroom != 1000 {
		t.Fatalf("the last 1,000: %+v", d)
	}
	v.BankEssence = 3000
	d = DecideSupply(v)
	if d.Action != SupplyHandoff || d.Send != 1000 {
		t.Fatalf("only the room the cap leaves goes: %+v", d)
	}
	// A full mule: hold, mine nothing, hand nothing over.
	v.MuleEssence = 150_000
	d = DecideSupply(v)
	if d.Action != SupplyHold || !strings.Contains(d.Why, "cap") {
		t.Fatalf("a full mule holds: %+v", d)
	}
	v.MuleEssence, v.BankEssence = 151_000, 0
	if d := DecideSupply(v); d.Action != SupplyHold {
		t.Fatalf("over the cap holds: %+v", d)
	}
	// The mule uses some: mining resumes at once.
	v.MuleEssence = 120_000
	if d := DecideSupply(v); d.Action != SupplyMine {
		t.Fatalf("the mule used some: %+v", d)
	}
	// muleStopAt 0 never stops.
	v.MuleStopAt, v.MuleEssence = 0, 9_000_000
	if d := DecideSupply(v); d.Action != SupplyMine || d.Headroom != -1 {
		t.Fatalf("no cap: %+v", d)
	}
}

// With the mule's bank not published, the miner's own deliveries are the measure.
func TestSupplyMeasuresOwnDeliveriesWithoutAPublishedBank(t *testing.T) {
	v := view()
	v.MuleKnown = false
	v.Delivered = 149_500
	v.BankEssence = 100
	d := DecideSupply(v)
	if d.Action != SupplyMine || d.Source == "" || !strings.Contains(d.Source, "deliveries") {
		t.Fatalf("%+v", d)
	}
	v.Delivered = 150_000
	if d := DecideSupply(v); d.Action != SupplyHold {
		t.Fatalf("own deliveries at the cap hold: %+v", d)
	}
	// A published bank beats the deliveries.
	v.MuleKnown, v.MuleEssence = true, 3
	if d := DecideSupply(v); d.Action != SupplyMine || d.Stock != 3 {
		t.Fatalf("%+v", d)
	}
}

func TestSupplyManualHandoff(t *testing.T) {
	v := view()
	v.BankEssence = 400
	v.Manual = true
	d := DecideSupply(v)
	if d.Action != SupplyHandoff || d.Send != 400 {
		t.Fatalf("a hand-over asked for goes with what is banked: %+v", d)
	}
	v.BankEssence = 10 // less than a load: not worth the trip
	if d := DecideSupply(v); d.Action != SupplyMine {
		t.Fatalf("%+v", d)
	}
}

// ---- the stack-only hand-over ----

func TestStackRulesCarryOnlyEssence(t *testing.T) {
	bank := []donorplan.ItemRow{
		{Name: "Coins", ID: 995, Count: 20000}, {Name: "Rune essence", ID: EssenceID, Count: 6000},
		{Name: "Bronze pickaxe", ID: 1265, Count: 1}, {Name: "Lobster", ID: 379, Count: 40},
	}
	rules := RulesFor(MuleItemsEssence, []string{"nothing"})
	rows := donorplan.HandoffRows(bank, rules)
	if len(rows) != 1 || rows[0].Name != "Rune essence" {
		t.Fatalf("rows %v", rows)
	}
	// The original loop still hands over the whole bank but the pickaxe.
	all := donorplan.HandoffRows(bank, RulesFor(MuleItemsAll, nil))
	if len(all) != 3 || all[0].Name != "Rune essence" {
		t.Fatalf("all %v", all)
	}
	// The pack: the pickaxe, a food and the note stack. One slot is for the mule, noted or not.
	pack := []donorplan.ItemRow{
		{Name: "Bronze pickaxe", ID: 1265, Count: 1}, {Name: "Lobster", ID: 379, Count: 1},
		{Name: "Rune essence", ID: NotedEssenceID, Count: 5000},
	}
	if n := donorplan.HaulOf(pack, rules); n != 1 {
		t.Errorf("haul %d, want the one stack", n)
	}
	load := donorplan.PlanLoad(pack, donorplan.DefaultMuleFreeSlots, rules)
	if len(load) != 1 || load[0].ID != NotedEssenceID || load[0].Slots != 1 {
		t.Errorf("load %v", load)
	}
	// Unnoted essence left in the pack goes too (a stretch cut short).
	pack = append(pack, donorplan.ItemRow{Name: "Rune essence", ID: EssenceID, Count: 1}, donorplan.ItemRow{Name: "Rune essence", ID: EssenceID, Count: 1})
	if n := donorplan.HaulOf(pack, rules); n != 3 {
		t.Errorf("haul %d, want 3 slots", n)
	}
	// Nothing but the pickaxe and food: nothing to hand over.
	if n := donorplan.HaulOf(pack[:2], rules); n != 0 {
		t.Errorf("haul %d with no essence", n)
	}
}

// ---- the bounded market lot ----

func lot() LotTerms {
	return LotTerms{Budget: 300_000, Want: 5000, MuleKnown: true, MuleCoins: 2_000_000, OnMarketWorld: true}
}

func TestMarketLotBudget(t *testing.T) {
	// The budget caps it: 300,000 gp at the 150 ceiling is 2,000 essence, however much is wanted.
	p, why := PlanMarketLot(lot())
	if why != "" || p.Qty != 2000 || p.MaxCoins != 300_000 || p.Ask != 300_000 {
		t.Fatalf("%+v %q", p, why)
	}
	if p.MaxCoins > lot().Budget {
		t.Error("a lot never exceeds the budget")
	}
	// What is wanted caps it when it is less.
	tt := lot()
	tt.Want = 100
	if p, _ := PlanMarketLot(tt); p.Qty != 100 || p.MaxCoins != 15_000 {
		t.Errorf("%+v", p)
	}
	// Money already spent comes off the budget; a spent budget refuses.
	tt = lot()
	tt.Spent = 240_000
	if p, why := PlanMarketLot(tt); why != "" || p.Qty != 400 || p.MaxCoins != 60_000 {
		t.Errorf("%+v %q", p, why)
	}
	tt.Spent = 300_000
	if _, why := PlanMarketLot(tt); !strings.Contains(why, "spent") {
		t.Errorf("a spent budget: %q", why)
	}
	// The mule's coins: only what is above one million funds a lot.
	tt = lot()
	tt.MuleCoins = 1_050_000
	if p, why := PlanMarketLot(tt); why != "" || p.Qty != 333 || p.MaxCoins != 49_950 {
		t.Errorf("1.05M on the mule funds 50,000: %+v %q", p, why)
	}
	for _, coins := range []int{0, 773_226, 999_999, 1_000_000} {
		tt.MuleCoins = coins
		if p, why := PlanMarketLot(tt); why == "" || p.Qty != 0 || !strings.Contains(why, "floor") {
			t.Errorf("the mule holds %d: %+v %q, want a refusal", coins, p, why)
		}
	}
	// Unpublished mule coins, a world without the stalls, the lot off and nothing wanted are refused.
	for name, mod := range map[string]func(*LotTerms){
		"unknown coins":  func(l *LotTerms) { l.MuleKnown = false },
		"another world":  func(l *LotTerms) { l.OnMarketWorld = false },
		"budget 0":       func(l *LotTerms) { l.Budget = 0 },
		"negative":       func(l *LotTerms) { l.Budget = -5 },
		"nothing wanted": func(l *LotTerms) { l.Want = 0 },
	} {
		tt = lot()
		mod(&tt)
		if p, why := PlanMarketLot(tt); why == "" || p.Qty != 0 {
			t.Errorf("%s: %+v %q", name, p, why)
		}
	}
	// A few coins above the floor buy nothing at 150 each.
	tt = lot()
	tt.MuleCoins = 1_000_100
	if _, why := PlanMarketLot(tt); why == "" {
		t.Error("100 coins do not buy an essence at 150")
	}
}

// The ceiling is the quest provisioner's: one number for the fleet.
func TestMarketCeilingIsTheQuestProvisioners(t *testing.T) {
	ours, theirs := MarketUnitCeiling, questplan.PipEssenceUnitCeiling
	if ours != theirs || ours != 150 {
		t.Errorf("ceiling %d vs questplan %d", ours, theirs)
	}
}

// ---- the refusals ----

// A Rune Mysteries refusal and the Guild door's turning-away are both recognised; a line that is neither is not.
func TestRefusalLines(t *testing.T) {
	if !IsQuestRefusalMessage("You need to have completed the Rune Mysteries Quest to use this feature.") {
		t.Error("the Rune Mysteries refusal")
	}
	if got := RefusalReason("You need to have completed the Rune Mysteries Quest to use this feature."); got == "" {
		t.Error("RefusalReason of the quest line")
	}
	for _, line := range []string{
		"You need a magic level of 66. The magical energy in here is unsafe for those below that level.",
		"you need a magic level of 66",
	} {
		if !IsGuildDoorRefusal(line) {
			t.Errorf("the Guild door line %q", line)
		}
	}
	for _, line := range []string{"You need a Mining level of 66 to mine this.", "Welcome to the Magicians' Guild!", ""} {
		if IsGuildDoorRefusal(line) || RefusalReason(line) != "" {
			t.Errorf("%q is no refusal", line)
		}
	}
}

// HandoverOff (no mule set, or muleAt 0) makes every hand-over branch unreachable: the threshold, a manual request and the
// cap that used to hand over the last of the bank. The same view without it hands over, so the test is the flag's alone.
func TestSupplyHandoverOffNeverHandsOver(t *testing.T) {
	v := view() // muleAt 5000, a published mule, 150,000 cap
	v.BankEssence = 6000
	if d := DecideSupply(v); d.Action != SupplyHandoff {
		t.Fatalf("control: 6,000 banked at muleAt 5,000 hands over: %+v", d)
	}
	v.HandoverOff = true
	if d := DecideSupply(v); d.Action != SupplyMine || d.Goal != DefaultBatch || d.Send != 0 {
		t.Fatalf("muleAt is not a threshold without a mule, the miner mines a batch: %+v", d)
	}
	v.Manual = true
	if d := DecideSupply(v); d.Action == SupplyHandoff {
		t.Fatalf("a manual request is ignored: %+v", d)
	}

	// The cap: the same view without the flag hands over the room it leaves.
	v = view()
	v.MuleEssence, v.BankEssence = 149_000, 3000
	if d := DecideSupply(v); d.Action != SupplyHandoff || d.Send != 1000 {
		t.Fatalf("control: %+v", d)
	}
	v.HandoverOff = true
	d := DecideSupply(v)
	if d.Action != SupplyHold || !strings.Contains(d.Why, "cap") || !strings.Contains(d.Why, "stays in the bank") {
		t.Fatalf("at the cap with no one to hand to it holds: %+v", d)
	}
	// Even a MuleAt left in the view and a long-gone retry wait change nothing.
	v.MuleAt, v.NextHandoff, v.Manual = 100, t0.Add(-time.Hour), true
	if d := DecideSupply(v); d.Action != SupplyHold {
		t.Fatalf("%+v", d)
	}
	// Below the cap it keeps mining, to the cap and no further.
	v.MuleEssence, v.BankEssence = 100_000, 49_900
	if d := DecideSupply(v); d.Action != SupplyMine || d.Goal != 4*LoadSize {
		t.Fatalf("100 short of the cap: %+v", d)
	}
}
