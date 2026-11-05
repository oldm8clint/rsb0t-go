// Package miscplan is the pure plan for Managing Miscellania, the daily minigame the Throne of Miscellania quest opens: how much to
// put in the coffers, how to split the ten worker points, when to collect, how to keep the people's approval up, and what of the
// haul goes on to the mule. It imports nothing of the client, so every number is unit-tested against the content's own arithmetic.
//
// The model is content 289's minigames/game_managing_miscellania/scripts/managing_miscellania.rs2, integer division and all:
//
//	one daily tick (1440 minutes), run when the account enters Miscellania, at most 30 ticks back:
//	  reduction = min(50000, 5 + coffer/10), never more than the coffer; coffer -= reduction
//	  effectiveness = reduction*100/8333
//	  resourcePts += (effectiveness/100) * approval          (approval 0..127; resourcePts caps at 262143)
//	  approval = approval*975/1000
//
//	collecting (Advisor Ghrim, "collect the resources") turns resourcePts into NOTED items by the worker points that stand AT THAT
//	moment, then zeroes resourcePts:
//	  maple logs = 102*pts*woodPts/2048     coal = 60*pts*minePts/2048
//	  fish = 100*pts*fishPts/2048  (raw or cooked: tuna fish/2, swordfish fish*3/20)
//	  herbs = 5*pts*herbPts/2048, each a random grimy herb
//
// What follows from it, and what the plan is built on:
//   - effectiveness/100 is an integer, so a tick yields NOTHING until the reduction reaches 8333, which takes a coffer of 83280 (the
//     starting 10000 is useless on its own). Above that the yield is a staircase: one step per 8333 of reduction, six at the 50000 cap
//     (a 500k coffer). A tick is worth the most per coin when its reduction sits exactly on a step, so the coffer is funded to the
//     smallest balance that keeps every tick of the visit interval on the step wanted, not to a round number.
//   - the coffer is not spent, only drawn down 10% a day: the coins stay in it. The cost of a plan is the reductions, the deposit is
//     only the float.
//   - the worker points are read at collection, so the split to ask for is the one for the NEXT collection, set after this one.
//   - approval multiplies every tick and decays 2.5% a day; each resource gathered in Miscellania itself adds 1 (cap 127), and a stall
//     stolen from there takes 4, so the account helps and never steals.
package miscplan

import "fmt"

// The content's constants.
const (
	TickMinutes      = 1440
	MaxTicks         = 30
	CofferCap        = 5_000_000
	ReductionCap     = 50_000
	StepReduction    = 8333
	ApprovalMax      = 127
	ApprovalRegent   = 96 // 75%: what King Vargas wants before the quest completes
	ApprovalDecayNum = 975
	ApprovalDecayDen = 1000
	PointsCap        = 262143
	WorkerPoints     = 10
	StartingCoffer   = 10_000 // what the quest's completion puts in
	MaxSteps         = ReductionCap / StepReduction
)

// State is what the account's varps hold.
type State struct {
	Coffer     int
	Approval   int
	ResPoints  int
	LastUpdate int // minutes, the content's date_minutes
}

// Alloc is the worker points, summing to at most WorkerPoints, each 0..10.
type Alloc struct{ Mine, Wood, Herb, Fish int }

// Valid says whether the split is one the interface can hold.
func (a Alloc) Valid() bool {
	for _, v := range []int{a.Mine, a.Wood, a.Herb, a.Fish} {
		if v < 0 || v > WorkerPoints {
			return false
		}
	}
	return a.Mine+a.Wood+a.Herb+a.Fish <= WorkerPoints
}

// Reduction is what one tick draws from a coffer.
func Reduction(coffer int) int {
	if coffer <= 0 {
		return 0
	}
	r := 5 + coffer/10
	if r > ReductionCap {
		r = ReductionCap
	}
	if r > coffer {
		r = coffer
	}
	return r
}

// Steps is the multiplier a reduction earns: effectiveness/100 in the content.
func Steps(reduction int) int { return reduction * 100 / StepReduction / 100 }

// Tick is one daily update.
func Tick(s State) State {
	if s.Coffer > 0 {
		r := Reduction(s.Coffer)
		s.Coffer -= r
		s.ResPoints += Steps(r) * s.Approval
		if s.ResPoints > PointsCap {
			s.ResPoints = PointsCap
		}
	}
	s.Approval = s.Approval * ApprovalDecayNum / ApprovalDecayDen
	s.LastUpdate += TickMinutes
	return s
}

// TicksDue is how many daily updates entering Miscellania at minute now runs: the content pretends the last update was at most 30 days
// ago and runs one per whole day since.
func TicksDue(lastUpdate, now int) int {
	if now <= lastUpdate {
		return 0
	}
	n := (now - lastUpdate) / TickMinutes
	if n > MaxTicks {
		n = MaxTicks
	}
	return n
}

// Advance runs every tick due at minute now, the way entering the zone does.
func Advance(s State, now int) State {
	if s.LastUpdate == 0 {
		return s // the first entry only stamps the time and sets 25% approval
	}
	if now-s.LastUpdate > TickMinutes*MaxTicks {
		s.LastUpdate = now - TickMinutes*MaxTicks
	}
	for now-s.LastUpdate >= TickMinutes {
		s = Tick(s)
	}
	return s
}

// Resources is a collection.
type Resources struct {
	Maple, Coal, Tuna, Swordfish, Herbs int
	Cooked                              bool
}

// Collect is what Ghrim hands over for points at the allocation standing now.
func Collect(points int, a Alloc, cooked bool) Resources {
	fish := 100 * points * a.Fish / 2048
	return Resources{
		Maple: 102 * points * a.Wood / 2048, Coal: 60 * points * a.Mine / 2048,
		Tuna: fish / 2, Swordfish: fish * 3 / 20, Herbs: 5 * points * a.Herb / 2048, Cooked: cooked,
	}
}

// Total is the item count of a collection.
func (r Resources) Total() int { return r.Maple + r.Coal + r.Tuna + r.Swordfish + r.Herbs }

// Lines are the noted items a collection puts in the pack, as the item names the handoff matches, zero lines left out.
func (r Resources) Lines() map[string]int {
	out := map[string]int{}
	add := func(name string, n int) {
		if n > 0 {
			out[name] = n
		}
	}
	add("maple logs", r.Maple)
	add("coal", r.Coal)
	if r.Cooked {
		add("tuna", r.Tuna)
		add("swordfish", r.Swordfish)
	} else {
		add("raw tuna", r.Tuna)
		add("raw swordfish", r.Swordfish)
	}
	return out
}

// MinUsefulCoffer is the smallest coffer whose tick yields anything (reaches one step).
func MinUsefulCoffer() int { return MinCofferFor(1) }

// MinCofferFor is the smallest coffer whose tick earns steps steps (1..MaxSteps), 0 for none.
func MinCofferFor(steps int) int {
	if steps <= 0 {
		return 0
	}
	if steps > MaxSteps {
		steps = MaxSteps
	}
	need := steps * StepReduction
	return (need - 5) * 10
}

// CofferNeeded is the smallest coffer to start an interval of days ticks with so that every one of them earns at least steps steps
// (the last tick is the one the drawdown starves), and false when the cap cannot hold it.
func CofferNeeded(steps, days int) (int, bool) {
	if steps <= 0 || days <= 0 {
		return 0, true
	}
	if steps > MaxSteps {
		steps = MaxSteps
	}
	lo, hi := 0, CofferCap
	ok := func(c int) bool {
		for i := 0; i < days; i++ {
			r := Reduction(c)
			if Steps(r) < steps {
				return false
			}
			c -= r
		}
		return true
	}
	if !ok(hi) {
		return 0, false
	}
	for lo < hi {
		m := (lo + hi) / 2
		if ok(m) {
			hi = m
		} else {
			lo = m + 1
		}
	}
	return lo, true
}

// Drawn is what a coffer spends over days ticks: the cost of the interval.
func Drawn(coffer, days int) int {
	spent := 0
	for i := 0; i < days; i++ {
		r := Reduction(coffer)
		spent += r
		coffer -= r
	}
	return spent
}

// FundParams is what the funding rule needs.
type FundParams struct {
	Wallet     int // coins the account may put in now (above any reserve it keeps)
	VisitDays  int // days to the next visit, 1..30
	MaxSteps   int // the most steps worth wanting (0: all six)
	MinSteps   int // fewer than this and the plan says do not bother (0: one)
	CofferNow  int
	Approval   int
	CofferRoom int // 0: the content's 5M cap
}

// Funding is the answer.
type Funding struct {
	Deposit  int
	Steps    int  // the multiplier every tick of the interval is funded for
	Worth    bool // false: no affordable plan earns anything
	CostDay  int  // average reduction per day, the real spend
	PtsDay   int  // resource points per day at the approval now
	Reason   string
	Needed   int // the coffer wanted at the start
	CofferTo int
}

// PlanFunding is the largest step multiplier the wallet can float for the whole interval, and the deposit that does it. Larger is
// better at no loss of efficiency (the same coin buys the same step), so the top affordable one is taken.
func PlanFunding(p FundParams) Funding {
	if p.VisitDays < 1 {
		p.VisitDays = 1
	}
	if p.VisitDays > MaxTicks {
		p.VisitDays = MaxTicks
	}
	top := p.MaxSteps
	if top <= 0 || top > MaxSteps {
		top = MaxSteps
	}
	low := p.MinSteps
	if low <= 0 {
		low = 1
	}
	room := p.CofferRoom
	if room <= 0 {
		room = CofferCap
	}
	for k := top; k >= low; k-- {
		need, ok := CofferNeeded(k, p.VisitDays)
		if !ok || need > room {
			continue
		}
		dep := need - p.CofferNow
		if dep < 0 {
			dep = 0
		}
		if dep > p.Wallet {
			continue
		}
		drawn := Drawn(p.CofferNow+dep, p.VisitDays)
		return Funding{
			Deposit: dep, Steps: k, Worth: true, CostDay: drawn / p.VisitDays, PtsDay: k * p.Approval, Needed: need,
			CofferTo: p.CofferNow + dep,
			Reason:   fmt.Sprintf("a coffer of %d keeps %d step(s) on every one of %d day(s); %d coins drawn over the interval", need, k, p.VisitDays, drawn),
		}
	}
	need, _ := CofferNeeded(low, p.VisitDays)
	return Funding{Worth: false, Needed: need, CofferTo: p.CofferNow, Reason: fmt.Sprintf(
		"the wallet of %d cannot float %d step(s) for %d day(s): that wants a coffer of %d (below it a tick earns nothing)", p.Wallet, low, p.VisitDays, need)}
}

// Prices are the owner's value of one item of each resource, to rank the split by.
type Prices struct{ Maple, Coal, Fish, Herb int }

// yieldPerPoint is the value one worker point earns per 2048 resource points, per resource: 60 coal, 102 maple, 5 herbs, and
// 65 fish kept of 100 (tuna 50 and swordfish 15).
func yieldPerPoint(p Prices) (mine, wood, herb, fish int) {
	return 60 * p.Coal, 102 * p.Maple, 5 * p.Herb, 65 * p.Fish
}

// Split is the worker points to ask for. Spread keeps at least one point on every resource users want "across the board" and gives
// the rest to the best paying; without it all ten go to the single best.
func Split(p Prices, spread bool) Alloc {
	mine, wood, herb, fish := yieldPerPoint(p)
	vals := []int{mine, wood, herb, fish}
	best := 0
	for i, v := range vals {
		if v > vals[best] {
			best = i
		}
	}
	pts := [4]int{}
	if !spread {
		pts[best] = WorkerPoints
	} else {
		for i := range pts {
			pts[i] = 1
		}
		for left := WorkerPoints - 4; left > 0; left-- {
			// the next point goes where a point adds most relative to the points there already (keeps it spread, favouring value)
			pick, score := 0, -1
			for i, v := range vals {
				if s := v * 1000 / (pts[i] + 1); s > score {
					pick, score = i, s
				}
			}
			pts[pick]++
		}
	}
	return Alloc{Mine: pts[0], Wood: pts[1], Herb: pts[2], Fish: pts[3]}
}

// CollectParams decide when to collect.
type CollectParams struct {
	Points        int
	Alloc         Alloc
	PtsPerDay     int // what the funding earns a day
	MinItems      int // do not walk to Ghrim for fewer items than this (0: 200)
	DaysSinceLast int
	MaxDays       int // collect at least this often (0: 7)
}

// ShouldCollect says whether the visit collects, and why. The points cap at 262143, so a stock that would hit it inside two days always
// goes; below a worthwhile haul it waits, because the points keep (they are linear, so collecting later loses nothing but the risk of the cap).
func ShouldCollect(p CollectParams) (bool, string) {
	min := p.MinItems
	if min <= 0 {
		min = 200
	}
	maxDays := p.MaxDays
	if maxDays <= 0 {
		maxDays = 7
	}
	if p.Points >= PointsCap-2*p.PtsPerDay {
		return true, "the resource points are about to hit their cap and would be lost"
	}
	haul := Collect(p.Points, p.Alloc, false).Total()
	if haul >= min {
		return true, fmt.Sprintf("a haul of %d items is worth the walk", haul)
	}
	if p.DaysSinceLast >= maxDays && haul > 0 {
		return true, fmt.Sprintf("%d days since the last collection", p.DaysSinceLast)
	}
	return false, fmt.Sprintf("a haul of %d items is under %d and the points keep", haul, min)
}

// HelpActions is how many gathering actions in Miscellania lift approval to want (each is +1, cap 127).
func HelpActions(approval, want int) int {
	if want > ApprovalMax {
		want = ApprovalMax
	}
	if approval >= want {
		return 0
	}
	return want - approval
}

// SteadyApproval is where approval settles when it is topped up to ApprovalMax on every visit and visits are every days days.
func SteadyApproval(days int) int {
	a := ApprovalMax
	for i := 0; i < days; i++ {
		a = a * ApprovalDecayNum / ApprovalDecayDen
	}
	return a
}

// Handoff is the lines of a collection that go to the mule, and the ones the account keeps.
type Handoff struct{ Give, Keep map[string]int }

// PlanHandoff splits a collection: the account keeps what its own plan wants (keep, by item name) and the rest goes to the mule.
func PlanHandoff(r Resources, keep map[string]int) Handoff {
	h := Handoff{Give: map[string]int{}, Keep: map[string]int{}}
	for name, n := range r.Lines() {
		k := keep[name]
		if k > n {
			k = n
		}
		if k > 0 {
			h.Keep[name] = k
		}
		if n-k > 0 {
			h.Give[name] = n - k
		}
	}
	return h
}

// Visit is one pass of the loop, all of it a decision and none of it I/O.
type Visit struct {
	Now       int
	Wallet    int
	VisitDays int
	Prices    Prices
	Spread    bool
	Keep      map[string]int
	Cooked    bool
	Current   Alloc // the split standing now (what a collection will read); zero reads as the split planned
}

// VisitPlan is what the runner does, in order.
type VisitPlan struct {
	After     State // the state after the ticks entering the zone runs
	HelpFirst int   // gathering actions to do before leaving, to lift approval
	Collect   bool
	Why       string
	Haul      Resources
	Handoff   Handoff
	Fund      Funding
	Alloc     Alloc // the split to set after collecting, for the next one
}

// PlanVisit is the whole loop for one visit of an account that has the quest complete.
func PlanVisit(s State, v Visit) VisitPlan {
	after := Advance(s, v.Now)
	alloc := Split(v.Prices, v.Spread)
	vp := VisitPlan{After: after, Alloc: alloc}
	days := v.VisitDays
	if days < 1 {
		days = 1
	}
	vp.HelpFirst = HelpActions(after.Approval, SteadyApproval(days))
	cur := v.Current
	if cur == (Alloc{}) {
		cur = alloc
	}
	ok, why := ShouldCollect(CollectParams{Points: after.ResPoints, Alloc: cur, PtsPerDay: Steps(Reduction(after.Coffer)) * after.Approval,
		DaysSinceLast: days})
	vp.Collect, vp.Why = ok, why
	if ok {
		vp.Haul = Collect(after.ResPoints, cur, v.Cooked)
		vp.Handoff = PlanHandoff(vp.Haul, v.Keep)
	}
	vp.Fund = PlanFunding(FundParams{Wallet: v.Wallet, VisitDays: days, CofferNow: after.Coffer, Approval: after.Approval})
	return vp
}
