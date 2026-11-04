package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fleetplan"
)

// buyAllCost is a shelf bought to nothing with no cap: the price walk summed.
func buyAllCost(sh fleetplan.RuneShop, rune string, stock int) int {
	return sh.CostToBuy(rune, stock, stock)
}

// Zaff's table is content's: varrock.inv staff_of_air,2,1000; zaff.rs2 openshop(..., 550, 1000,
// 20, ...); staves.obj cost 1500; npc 546 at 3203,3433.
func TestZaffMatchesContent(t *testing.T) {
	l, ok := Zaff.Line(fleetplan.StaffOfAir)
	if !ok || l.Base != 2 || l.RateTicks != 1000 || l.Cost != 1500 || len(Zaff.Lines) != 3 {
		t.Fatalf("Zaff line %+v %v", l, ok)
	}
	// varrock.inv stock1=battlestaff,5,100; staves.obj [battlestaff] cost 7000.
	if b, ok := Zaff.Line(Battlestaff); !ok || b.Base != 5 || b.RateTicks != 100 || b.Cost != 7000 {
		t.Fatalf("Zaff Battlestaff line %+v %v", b, ok)
	}
	// varrock.inv [staffshop] stock6 / [memberstaffshop] stock7 staff_of_fire,2,1000; staves.obj [staff_of_fire] cost 1500.
	if f, ok := Zaff.Line(StaffOfFire); !ok || f.Base != 2 || f.RateTicks != 1000 || f.Cost != 1500 {
		t.Fatalf("Zaff Staff of fire line %+v %v", f, ok)
	}
	if got := Zaff.PriceAt(StaffOfFire, 1); got != 1530 {
		t.Errorf("the second Staff of fire costs %d, want 1530", got)
	}
	if Zaff.Keeper != "Zaff" || Zaff.NPCID != 546 || Zaff.SellMultiplier != 1000 || Zaff.Delta != 20 {
		t.Errorf("Zaff %+v", Zaff)
	}
	if ZaffStand.X != 3203 || ZaffStand.Z != 3433 || ZaffStand.Level != 0 {
		t.Errorf("ZaffStand %+v", ZaffStand)
	}
	if got := Zaff.PriceAt(fleetplan.StaffOfAir, 2); got != 1500 {
		t.Errorf("a staff at a full shelf costs %d, want 1500", got)
	}
	if got := Zaff.PriceAt(fleetplan.StaffOfAir, 1); got != 1530 {
		t.Errorf("the second staff costs %d, want 1530", got)
	}
	if _, ok := Site(SiteZaff); ok {
		t.Error("Zaff must not be a rune site")
	}
}

func TestPlanStaffs(t *testing.T) {
	for _, c := range []struct {
		name                          string
		stock, want, capGP, spendable int
		wantN, wantCost               int
	}{
		{"both at the default cap", 2, 2, DefaultZaffStaffCap, 1 << 30, 2, 3030},
		{"want one", 2, 1, DefaultZaffStaffCap, 1 << 30, 1, 1500},
		{"cap at base buys the first only", 2, 2, 1500, 1 << 30, 1, 1500},
		{"cap under base buys none", 2, 2, 1499, 1 << 30, 0, 0},
		{"coins for one", 2, 2, DefaultZaffStaffCap, 2000, 1, 1500},
		{"coins for none", 2, 2, DefaultZaffStaffCap, 1499, 0, 0},
		{"negative spendable", 2, 2, DefaultZaffStaffCap, -300, 0, 0},
		{"empty shelf", 0, 2, DefaultZaffStaffCap, 1 << 30, 0, 0},
		{"one left, priced at the stock it leaves", 1, 2, DefaultZaffStaffCap, 1 << 30, 1, 1530},
		{"off", 2, 0, DefaultZaffStaffCap, 1 << 30, 0, 0},
		{"a sold-up shelf is cheaper", 4, 3, DefaultZaffStaffCap, 1 << 30, 3, 1440 + 1470 + 1500},
	} {
		n, cost := PlanStaffs(c.stock, c.want, c.capGP, c.spendable)
		if n != c.wantN || cost != c.wantCost {
			t.Errorf("%s: (%d, %d), want (%d, %d)", c.name, n, cost, c.wantN, c.wantCost)
		}
	}
	if got := StaffEstCost(2, DefaultZaffStaffCap); got != 3030 {
		t.Errorf("StaffEstCost %d, want 3030", got)
	}
	p := StaffPlan(2, 2, DefaultZaffStaffCap, 1<<30)
	if p.Coins != 3030 || len(p.Lines) != 1 || p.Lines[0].Count != 2 || p.Lines[0].AssumedStock != 2 {
		t.Errorf("StaffPlan %+v", p)
	}
	if got := PredictedSpend(Zaff, p, map[string]int{fleetplan.StaffOfAir: 1}); got != 1500 {
		t.Errorf("the price check predicts %d for one staff, want 1500", got)
	}
	if p := StaffPlan(0, 2, DefaultZaffStaffCap, 1<<30); len(p.Lines) != 0 || p.Coins != 0 {
		t.Errorf("an empty shelf plans %+v", p)
	}
}

// The staff stations sit after each Varrock visit and never change the hop count.
func TestRingWithStaffs(t *testing.T) {
	p := PlanRound(RoundInput{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 2, HopMinGain: 200, HopsAvailable: true, Staffs: true, Worth: allWorth(500)})
	want := "deliver@1 draw@1 Betty@1 Aubury@1 Zaff@1 hop2 Aubury@2 Zaff@2 Betty@2 Lundail@2 hop1 Lundail@1"
	if got := keys(p.Ring); got != want || Hops(p.Ring) != 2 {
		t.Fatalf("ring %q, want %q", got, want)
	}
	if open, close, ok := OBlock(p.Ring); !ok || p.Ring[open+2].Key() != "Zaff@2" || close != 10 {
		t.Errorf("O block %d..%d", open, close)
	}
	p = PlanRound(RoundInput{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 2, HopMinGain: 200, HopsAvailable: true, Staffs: true, Worth: allWorth(10)})
	if got := keys(p.Ring); got != "deliver@1 draw@1 Betty@1 Aubury@1 Zaff@1 Lundail@1" {
		t.Errorf("without the O block: %q", got)
	}
	// Staffs off (the default): no Zaff station anywhere.
	p = PlanRound(RoundInput{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 2, HopMinGain: 200, HopsAvailable: true, Worth: allWorth(500)})
	if strings.Contains(keys(p.Ring), "Zaff") {
		t.Errorf("staffs off still visits Zaff: %q", keys(p.Ring))
	}
}

// Staffs cross to the mule only when the ring buys them; coins never do.
func TestMuleRulesWithStaffs(t *testing.T) {
	classic := DefaultSettings()
	classic.Rotate = RotateOff
	set, err := classic.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	set.ZaffStaffs = 2
	items := set.DeliverItems()
	if got := strings.Join(items, ","); got != "Mind rune,Air rune,Chaos rune,Staff of air" {
		t.Fatalf("items %s", got)
	}
	r := MuleRules(items)
	pack := []donorplan.ItemRow{
		{Name: "Coins", ID: 995, Count: 90000},
		{Name: "Staff of air", ID: 1381, Count: 1},
		{Name: "Staff of air", ID: 1381, Count: 1},
		{Name: "Air rune", ID: 556, Count: 50},
		{Name: "Magic staff", ID: 1389, Count: 1},
		{Name: "Mind rune", ID: 558, Count: 900},
	}
	load := donorplan.PlanLoad(pack, donorplan.DefaultMuleFreeSlots, r)
	var names []string
	for _, g := range load {
		names = append(names, g.Name)
	}
	if len(load) == 0 || load[0].Name != "Mind rune" {
		t.Fatalf("load %v, want minds first", names)
	}
	joined := strings.Join(names, ",")
	if strings.Contains(joined, "Coins") || strings.Contains(joined, "Magic staff") ||
		!strings.Contains(joined, "Staff of air") || !strings.Contains(joined, "Air rune") {
		t.Errorf("load %v", names)
	}
	if n := donorplan.HaulOf(pack, r); n != 4 {
		t.Errorf("haul %d slots, want 4 (minds, air, two staffs)", n)
	}
	// Staffs off: a staff in the pack stays.
	if !MuleRules(DefaultSettings().Runes).Stays(donorplan.ItemRow{Name: "Staff of air", ID: 1381, Count: 1}) {
		t.Error("a staff crossed with staffs off")
	}
}

func TestRoundIdle(t *testing.T) {
	max := 20 * time.Minute
	for _, c := range []struct {
		name     string
		wait     time.Duration
		max      time.Duration
		failures int
		want     time.Duration
	}{
		{"no failures keeps the restock wait", 2 * time.Minute, max, 0, 2 * time.Minute},
		{"no failures keeps the one-minute floor", MinIdle, max, 0, MinIdle},
		{"a failure raises a full-shelf estimate to FailureIdle", MinIdle, max, 1, FailureIdle},
		{"a longer restock wait stands", 12 * time.Minute, max, 3, 12 * time.Minute},
		{"never above idleMaxMinutes", 30 * time.Minute, max, 0, max},
		{"FailureIdle bounded by a small idleMaxMinutes", MinIdle, 3 * time.Minute, 2, 3 * time.Minute},
	} {
		if got := RoundIdle(c.wait, c.max, c.failures); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	for _, o := range []string{OutcomeWalkFailed, OutcomeAborted} {
		if !Failed(o) {
			t.Errorf("%s is a failure", o)
		}
	}
	for _, o := range []string{OutcomeSkipped, OutcomeEmpty, OutcomeCapped, OutcomeDeferred, OutcomeHopped, OutcomeBought} {
		if Failed(o) {
			t.Errorf("%s is not a failure", o)
		}
	}
}

// One delivery hop per round: a hop that did not land, or a mule whose world flapped, defers the
// delivery rather than hopping again.
func TestDeliverHopOncePerRound(t *testing.T) {
	st := NewState()
	st.StartRound(BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites()}), 0, time.Unix(1, 0))
	if ok, _ := st.DeliverHopAllowed(); !ok {
		t.Fatal("the first delivery hop of a round is allowed")
	}
	st.DeliverHopRound = st.Round
	if ok, why := st.DeliverHopAllowed(); ok || !strings.Contains(why, "already hopped") {
		t.Errorf("a second delivery hop in round %d: %v %q", st.Round, ok, why)
	}
	st.StartRound(st.Ring, 0, time.Unix(2, 0))
	if ok, _ := st.DeliverHopAllowed(); !ok {
		t.Error("the next round may hop again")
	}
}
