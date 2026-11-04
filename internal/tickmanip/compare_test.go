package tickmanip

import (
	"reflect"
	"testing"
)

func TestParseStepRoundTrip(t *testing.T) {
	for _, st := range []Step{KnifeUse, MakeShafts, MoveOwn, Horsey, Click} {
		got, ok := ParseStep(st.String())
		if !ok || got != st {
			t.Errorf("ParseStep(%q) = %v, %v; want %v", st.String(), got, ok, st)
		}
	}
	if _, ok := ParseStep("cheat:give coins 1"); ok {
		t.Error("a cheat name must not parse as a step")
	}
}

// rowsFor builds the rows a perfect lab would have produced for plan against sk, so Compare must call it OK.
func rowsFor(sk Skill, plan Plan, ticks int, skillStat, fletchStat int) *RowSet {
	set := &RowSet{Base: 500, Rows: map[int]*Row{}}
	tr := Replay(sk, plan, ticks)
	for _, res := range tr.Results {
		row := NewRow(set.Base + res.Tick)
		for _, st := range plan(res.Tick) {
			row.Sent = append(row.Sent, st.String())
		}
		if res.Roll {
			row.XP = map[int]int{skillStat: 25}
		}
		if res.Fletched {
			if row.XP == nil {
				row.XP = map[int]int{}
			}
			row.XP[fletchStat] = 5
		}
		if res.AnyAnim() {
			row.Anim = 879
		}
		set.Rows[row.N] = row
	}
	return set
}

func TestComparePerfectLabIsOK(t *testing.T) {
	const skill, fletch = 8, 9
	for _, sk := range Skills {
		plan := Recommended(sk)
		set := rowsFor(sk, plan, 60, skill, fletch)
		// The plan rebuilt from the Sent column must be the plan that ran.
		rebuilt := PlanFromRows(set)
		for tick := 0; tick < 60; tick++ {
			if !reflect.DeepEqual(rebuilt(tick), plan(tick)) {
				t.Fatalf("%v: rebuilt plan differs at tick %d: %v vs %v", sk, tick, rebuilt(tick), plan(tick))
			}
		}
		obs := Observe(set, 59, skill, fletch)
		c := Compare(sk, rebuilt, 59, obs)
		if !c.OK() || !c.ExactRolls() {
			t.Errorf("%v: a perfect lab disagreed with the model: %s", sk, c.Summary())
		}
	}
}

func TestCompareFlagsACatchOffARollTick(t *testing.T) {
	const skill, fletch = 8, 9
	plan := NormalPlan()
	set := rowsFor(Woodcutting, plan, 30, skill, fletch)
	// Move the first catch one tick later than the model's roll (3 -> 4): the lab "rolled" when the model says it cannot.
	delete(set.Rows[set.Base+3].XP, skill)
	set.Rows[set.Base+4].XP = map[int]int{skill: 25}
	c := Compare(Woodcutting, PlanFromRows(set), 29, Observe(set, 29, skill, fletch))
	if c.OK() {
		t.Fatal("a catch on a tick without a modelled roll must fail the comparison")
	}
	if !reflect.DeepEqual(c.SuccessOffRoll, []int{4}) {
		t.Errorf("SuccessOffRoll = %v, want [4]", c.SuccessOffRoll)
	}
}

func TestCompareToleratesFailedRolls(t *testing.T) {
	const skill, fletch = 8, 9
	set := rowsFor(Woodcutting, NormalPlan(), 30, skill, fletch)
	// A roll that failed gives no experience: allowed, but ExactRolls says it was not exact.
	delete(set.Rows[set.Base+7].XP, skill)
	c := Compare(Woodcutting, PlanFromRows(set), 29, Observe(set, 29, skill, fletch))
	if !c.OK() {
		t.Errorf("a failed roll must not fail the comparison: %s", c.Summary())
	}
	if c.ExactRolls() || !reflect.DeepEqual(c.RollNoSuccess, []int{7}) {
		t.Errorf("RollNoSuccess = %v, ExactRolls = %v; want [7] and false", c.RollNoSuccess, c.ExactRolls())
	}
}

func TestCompareSeesALeakedFletch(t *testing.T) {
	const skill, fletch = 8, 9
	// A knife pair alone: the model leaks a fletch at tick 2, and a lab that did not leak disagrees.
	only := func(tick int) []Step {
		if tick == 0 {
			return Knife()
		}
		return nil
	}
	set := rowsFor(Woodcutting, only, 8, skill, fletch)
	delete(set.Rows[set.Base+2].XP, fletch)
	c := Compare(Woodcutting, PlanFromRows(set), 7, Observe(set, 7, skill, fletch))
	if c.OK() || !reflect.DeepEqual(c.FletchMissed, []int{2}) {
		t.Errorf("FletchMissed = %v (ok=%v), want [2]", c.FletchMissed, c.OK())
	}
}

func TestObserveSeparatesCancelsFromAnimations(t *testing.T) {
	set := &RowSet{Base: 0, Rows: map[int]*Row{}}
	for tick, anim := range map[int]int{0: NoAnim, 1: 879, 2: -1, 3: 1248} {
		r := NewRow(tick)
		r.Anim = anim
		set.Rows[tick] = r
	}
	o := Observe(set, 3, 8, 9)
	if !reflect.DeepEqual(o.Anim, []int{1, 3}) || !reflect.DeepEqual(o.Cancel, []int{2}) {
		t.Errorf("anims %v cancels %v, want [1 3] and [2]", o.Anim, o.Cancel)
	}
}

func TestPairBatchesNamesTheTickEachBatchWasRead(t *testing.T) {
	set := &RowSet{Base: 100, Rows: map[int]*Row{}}
	add := func(tick int, sent []string, msgs ...string) {
		r := set.Rows[100+tick]
		if r == nil {
			r = NewRow(100 + tick)
			set.Rows[100+tick] = r
		}
		r.Sent = append(r.Sent, sent...)
		r.Msgs = append(r.Msgs, msgs...)
	}
	// Three writes at ticks 0, 3 and 6, each led by its ack. The first is read on time, the second a tick late, the third on time.
	add(0, []string{AckName, "knife-on-log", "make-1-shafts", "click"}, AckPrefix+": 1")
	add(3, []string{AckName, "knife-on-log", "make-1-shafts", "click"})
	add(4, nil, AckPrefix+": 1")
	add(6, []string{AckName, "knife-on-log", "make-1-shafts", "click"}, AckPrefix+": 1")
	bs := PairBatches(set)
	if len(bs) != 3 {
		t.Fatalf("batches = %d, want 3", len(bs))
	}
	wantLate := []int{0, 1, 0}
	for i, b := range bs {
		if b.Late() != wantLate[i] {
			t.Errorf("batch %d: late %d, want %d (%+v)", i, b.Late(), wantLate[i], b)
		}
	}
	on, l1, lm, un := LateStats(bs)
	if on != 2 || l1 != 1 || lm != 0 || un != 0 {
		t.Errorf("LateStats = %d %d %d %d, want 2 1 0 0", on, l1, lm, un)
	}
	// The plan the server ran puts the second batch at tick 4, ack first.
	plan := PlanFromBatches(bs)
	if len(plan(3)) != 0 || !reflect.DeepEqual(plan(4), []Step{Ack, KnifeUse, MakeShafts, Click}) {
		t.Errorf("server plan: tick 3 = %v, tick 4 = %v; want nothing then ack and the three steps", plan(3), plan(4))
	}
}

func TestPairBatchesTwoAcksInOneTick(t *testing.T) {
	set := &RowSet{Base: 0, Rows: map[int]*Row{}}
	mk := func(tick int, sent []string, acks int) {
		r := NewRow(tick)
		r.Sent = sent
		for i := 0; i < acks; i++ {
			r.Msgs = append(r.Msgs, AckPrefix+": 1")
		}
		set.Rows[tick] = r
	}
	// A batch written at tick 0 and another at tick 1 are both read in tick 1.
	mk(0, []string{AckName, "click"}, 0)
	mk(1, []string{AckName, "knife-on-log"}, 2)
	bs := PairBatches(set)
	if len(bs) != 2 || bs[0].Actual != 1 || bs[1].Actual != 1 {
		t.Fatalf("batches %+v, want both read at tick 1", bs)
	}
	plan := PlanFromBatches(bs)
	if got := plan(1); !reflect.DeepEqual(got, []Step{Ack, Click, Ack, KnifeUse}) {
		t.Errorf("tick 1 steps = %v, want [ack click ack knife-on-log] in write order", got)
	}
}

// TestMergedLateBatchesAreSplitByTheEventBudget is the case the lab found: a lone click written one tick late and the
// next batch (ack, knife, Make 1, click) arrive together, six user events, so the engine reads five and leaves the last
// click for the next tick. The model, fed the ack-led plan, must split them the same way.
func TestMergedLateBatchesAreSplitByTheEventBudget(t *testing.T) {
	sim := NewSim(NetFishing, baseClock)
	merged := []Step{Ack, Click, Ack, KnifeUse, MakeShafts, Click}
	r0 := sim.Tick(0, merged)
	if len(r0.Read) != 5 || r0.Deferred != 1 {
		t.Fatalf("read %d, deferred %d; want 5 and 1", len(r0.Read), r0.Deferred)
	}
	r1 := sim.Tick(1, nil)
	if len(r1.Read) != 1 || r1.Read[0] != Click {
		t.Errorf("tick 1 read %v, want the deferred click", r1.Read)
	}
}
