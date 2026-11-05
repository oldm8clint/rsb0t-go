package miscplan

import "testing"

func TestReductionAndStaircase(t *testing.T) {
	if Reduction(10_000) != 1005 || Steps(Reduction(10_000)) != 0 {
		t.Fatalf("the starting coffer earns nothing: %d", Reduction(10_000))
	}
	if Reduction(5_000_000) != 50_000 || Steps(50_000) != 6 {
		t.Fatal("the cap is 50000, six steps")
	}
	if Reduction(3) != 3 || Reduction(0) != 0 {
		t.Fatal("never more than the coffer")
	}
	for k := 1; k <= MaxSteps; k++ {
		c := MinCofferFor(k)
		if Steps(Reduction(c)) != k || Steps(Reduction(c-10)) >= k {
			t.Fatalf("MinCofferFor(%d)=%d is not the first coffer of that step", k, c)
		}
	}
	if MinUsefulCoffer() != 83280 {
		t.Fatalf("min useful coffer %d", MinUsefulCoffer())
	}
}

func TestTickMatchesContent(t *testing.T) {
	s := Tick(State{Coffer: 500_000, Approval: 127, LastUpdate: 1})
	// reduction 50000, steps 6, points 762, approval 127*975/1000=123
	if s.Coffer != 450_000 || s.ResPoints != 762 || s.Approval != 123 || s.LastUpdate != 1441 {
		t.Fatalf("%+v", s)
	}
	if got := Tick(State{Coffer: 10_000, Approval: 127}); got.ResPoints != 0 || got.Coffer != 8995 {
		t.Fatalf("a 10k coffer: %+v", got)
	}
	big := Tick(State{Coffer: 5_000_000, Approval: 127, ResPoints: PointsCap - 1})
	if big.ResPoints != PointsCap {
		t.Fatal("points cap")
	}
}

func TestAdvanceTicksAndThirtyDayCap(t *testing.T) {
	s := State{Coffer: 500_000, Approval: 127, LastUpdate: 100}
	if TicksDue(100, 100+3*TickMinutes+5) != 3 || TicksDue(100, 99) != 0 || TicksDue(100, 100+100*TickMinutes) != MaxTicks {
		t.Fatal("ticks due")
	}
	a := Advance(s, 100+3*TickMinutes)
	if a.LastUpdate != 100+3*TickMinutes {
		t.Fatal(a.LastUpdate)
	}
	long := Advance(s, 100+90*TickMinutes)
	if long.LastUpdate != 100+90*TickMinutes {
		t.Fatalf("a long absence is capped at 30 ticks and still ends current: %d", long.LastUpdate)
	}
	if Advance(State{Coffer: 500_000}, 5).LastUpdate != 0 {
		t.Fatal("the first entry only stamps")
	}
}

func TestCollect(t *testing.T) {
	r := Collect(2048, Alloc{Mine: 3, Wood: 3, Herb: 2, Fish: 2}, false)
	// 60*3, 102*3, fish 200 -> tuna 100 sword 30, herbs 5*2
	if r.Coal != 180 || r.Maple != 306 || r.Tuna != 100 || r.Swordfish != 30 || r.Herbs != 10 {
		t.Fatalf("%+v", r)
	}
	l := r.Lines()
	if l["raw tuna"] != 100 || l["maple logs"] != 306 || len(l) != 4 {
		t.Fatalf("%v", l)
	}
	if Collect(2048, Alloc{Fish: 10}, true).Lines()["tuna"] != 500 {
		t.Fatal("cooked tuna")
	}
	if (Alloc{Mine: 6, Wood: 5}).Valid() || !(Alloc{Mine: 5, Wood: 5}).Valid() {
		t.Fatal("alloc valid")
	}
}

func TestCofferNeededAndDrawn(t *testing.T) {
	one, ok := CofferNeeded(1, 1)
	if !ok || one != 83280 {
		t.Fatalf("%d", one)
	}
	// seven days at one step needs more than one day: the 10% drawdown starves the last tick
	seven, _ := CofferNeeded(1, 7)
	if seven <= one {
		t.Fatalf("seven days %d should need more than one day %d", seven, one)
	}
	c := seven
	for i := 0; i < 7; i++ {
		r := Reduction(c)
		if Steps(r) < 1 {
			t.Fatalf("day %d starved at %d", i, c)
		}
		c -= r
	}
	// one less coin and some day starves
	c = seven - 1
	starved := false
	for i := 0; i < 7; i++ {
		r := Reduction(c)
		if Steps(r) < 1 {
			starved = true
		}
		c -= r
	}
	if !starved {
		t.Fatal("not minimal")
	}
	// the cap draws a flat 50000 a day while the coffer is large, so six steps for a month is a coffer of about 500k + 29*50k
	month, ok := CofferNeeded(6, 30)
	if !ok || month < 1_900_000 || month > 2_000_000 {
		t.Fatalf("six steps for a month: %d %v", month, ok)
	}
	if Drawn(month, 30) < 29*ReductionCap {
		t.Fatal("a month at the cap draws nearly the cap every day")
	}
	if Drawn(100_000, 1) != 10_005 {
		t.Fatal(Drawn(100_000, 1))
	}
}

func TestPlanFundingTakesTheLargestAffordableStep(t *testing.T) {
	f := PlanFunding(FundParams{Wallet: 200_000, VisitDays: 1, Approval: 120})
	if !f.Worth || f.Steps != 2 || f.Deposit != MinCofferFor(2) || f.PtsDay != 240 {
		t.Fatalf("%+v", f)
	}
	f = PlanFunding(FundParams{Wallet: 100_000, VisitDays: 1, CofferNow: 10_000, Approval: 120})
	if !f.Worth || f.Steps != 1 || f.Deposit != 83280-10_000 {
		t.Fatalf("an existing coffer counts: %+v", f)
	}
	f = PlanFunding(FundParams{Wallet: 50_000, VisitDays: 1, CofferNow: 10_000})
	if f.Worth || f.Deposit != 0 || f.Reason == "" {
		t.Fatalf("too poor to earn anything must say so: %+v", f)
	}
	if f2 := PlanFunding(FundParams{Wallet: 9_000_000, VisitDays: 1, MaxSteps: 3}); f2.Steps != 3 {
		t.Fatal("max steps respected")
	}
	if f3 := PlanFunding(FundParams{Wallet: 9_000_000, VisitDays: 99}); !f3.Worth {
		t.Fatalf("a visit interval past 30 days is clamped: %+v", f3)
	}
}

func TestSplit(t *testing.T) {
	p := Prices{Maple: 1, Coal: 1, Fish: 1, Herb: 1}
	if (Split(p, false) == Alloc{}) || !Split(p, true).Valid() {
		t.Fatal("split valid")
	}
	a := Split(p, true)
	if a.Mine+a.Wood+a.Herb+a.Fish != WorkerPoints || a.Mine < 1 || a.Wood < 1 || a.Herb < 1 || a.Fish < 1 {
		t.Fatalf("spread keeps one on every resource: %+v", a)
	}
	one := Split(Prices{Maple: 500, Coal: 1, Fish: 1, Herb: 1}, false)
	if one.Wood != 10 {
		t.Fatalf("%+v", one)
	}
	herb := Split(Prices{Maple: 1, Coal: 1, Fish: 1, Herb: 5000}, true)
	if herb.Herb <= herb.Mine {
		t.Fatalf("value tips the spread: %+v", herb)
	}
}

func TestShouldCollect(t *testing.T) {
	a := Alloc{Mine: 3, Wood: 3, Herb: 2, Fish: 2}
	if ok, _ := ShouldCollect(CollectParams{Points: PointsCap - 100, Alloc: a, PtsPerDay: 762}); !ok {
		t.Fatal("near the cap goes")
	}
	if ok, _ := ShouldCollect(CollectParams{Points: 100, Alloc: a}); ok {
		t.Fatal("a tiny haul waits")
	}
	if ok, _ := ShouldCollect(CollectParams{Points: 3000, Alloc: a, DaysSinceLast: 7}); !ok {
		t.Fatal("a week old and non-empty goes")
	}
	if ok, _ := ShouldCollect(CollectParams{Points: 0, Alloc: a, DaysSinceLast: 30}); ok {
		t.Fatal("nothing to collect")
	}
}

func TestApprovalAndHandoff(t *testing.T) {
	if HelpActions(120, 124) != 4 || HelpActions(127, 124) != 0 || HelpActions(0, 999) != ApprovalMax {
		t.Fatal("help actions")
	}
	if SteadyApproval(1) != 123 || SteadyApproval(0) != 127 {
		t.Fatal(SteadyApproval(1))
	}
	h := PlanHandoff(Resources{Coal: 500, Maple: 100}, map[string]int{"coal": 100, "maple logs": 900})
	if h.Give["coal"] != 400 || h.Keep["coal"] != 100 || h.Give["maple logs"] != 0 || h.Keep["maple logs"] != 100 {
		t.Fatalf("%+v", h)
	}
}

func TestPlanVisit(t *testing.T) {
	s := State{Coffer: 600_000, Approval: 100, ResPoints: 20_000, LastUpdate: 1000}
	v := PlanVisit(s, Visit{Now: 1000 + 2*TickMinutes, Wallet: 0, VisitDays: 3, Prices: Prices{1, 1, 1, 1}, Spread: true})
	if v.After.LastUpdate != 1000+2*TickMinutes || v.After.ResPoints <= 20_000 {
		t.Fatalf("%+v", v.After)
	}
	if !v.Collect || v.Haul.Total() == 0 {
		t.Fatalf("20k points is a haul: %+v", v)
	}
	if v.HelpFirst <= 0 {
		t.Fatal("approval below the steady state asks for help")
	}
	if v.Fund.Deposit != 0 {
		t.Fatalf("an empty wallet deposits nothing: %+v", v.Fund)
	}
}
