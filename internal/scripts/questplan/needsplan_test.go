package questplan

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// A small world the planner's rules can be read in.
//
//	q1  Mining 10                                  (a rung trains it)
//	q2  after q1, Agility 20                        (a rung trains it)
//	q3  nothing, advised combat 40                  (ready; defers below its advice)
//	q4  Fishing 5                                   (no trainer: a gap)
//	q5  100 quest points                            (more than the world pays)
//	q6  Attack 20                                   (a loop trains it)
//	q7  nothing                                     (ready)
func miniFacts() map[string]RungFacts {
	return map[string]RungFacts{
		"q1": {ID: "q1", Name: "Quest one", QP: 2, EstMinutes: 30, Gates: []Gate{{Skill: "mining", Level: 10, Kind: GateServer}}},
		"q2": {ID: "q2", Name: "Quest two", QP: 5, EstMinutes: 30, Prereqs: []string{"q1"}, Gates: []Gate{{Skill: "agility", Level: 20, Kind: GateServer}}},
		"q3": {ID: "q3", Name: "Quest three", QP: 1, EstMinutes: 20, MinCombat: 40},
		"q4": {ID: "q4", Name: "Quest four", QP: 1, EstMinutes: 20, Gates: []Gate{{Skill: "fishing", Level: 5, Kind: GateServer}}},
		"q5": {ID: "q5", Name: "Quest five", QP: 1, EstMinutes: 20, QPNeeded: 100},
		"q6": {ID: "q6", Name: "Quest six", QP: 1, EstMinutes: 20, Gates: []Gate{{Skill: "attack", Level: 20, Kind: GateServer}}},
		"q7": {ID: "q7", Name: "Quest seven", QP: 1, EstMinutes: 20},
		"r1": {ID: "r1", Name: "A rung", TrainSkill: "mining", TrainLevel: 10},
	}
}

func miniCatalogue() Catalogue {
	mine := row("mine-a", "mining", 1, 20)
	mine.EstXPPerHour = 2000
	mine.Sites = []TrainerSite{{Key: "rocks", Capacity: 2}}
	mine.Rungs = map[int]string{10: "r1"}
	agi := row("agi", "agility", 1, 35)
	agi.EstXPPerHour = 6000
	agi.Sites = []TrainerSite{{Key: "course", Capacity: 5}}
	return NewCatalogue(mine, agi, loopRow("atk-loop", "attack", 1, 40))
}

func miniTable() NeedTable {
	return BuildNeedTable(NeedSources{Facts: miniFacts()})
}

var miniOrder = []string{"q1", "q2", "q3", "q4", "q5", "q6", "q7"}

func plan(sub Subject, set NeedsSettings, fleet FleetState) NeedsPlan {
	return PlanNeeds(sub, miniOrder, miniTable(), miniCatalogue(), fleet, set, t0)
}

func fresh() Subject {
	s := accountAt(map[string]int{"hitpoints": 30})
	s.Needs, s.Parked, s.Deferrals = map[string]NeedHistory{}, map[string]time.Time{}, map[string]int{}
	return s
}

func cand(pl NeedsPlan, key string) *Candidate {
	for i := range pl.Candidates {
		if pl.Candidates[i].Key == key {
			return &pl.Candidates[i]
		}
	}
	return nil
}

func blockedNeed(pl NeedsPlan, quest, key string) (BlockedNeed, bool) {
	for _, b := range pl.Blocked {
		if b.Quest != quest {
			continue
		}
		for _, n := range b.Needs {
			if n.Need.Key() == key {
				return n, true
			}
		}
	}
	return BlockedNeed{}, false
}

func TestThePlanSchedulesPrerequisitesAndNamesWhatBlocksEachQuest(t *testing.T) {
	pl := plan(fresh(), DefaultNeedsSettings(), FleetState{})
	// q2 waits for q1 (a quest), and for Agility 20 (a stint); q1 for Mining 10.
	if n, ok := blockedNeed(pl, "q2", "quest:q1"); !ok || n.Status != NeedWaitQuest {
		t.Errorf("q2 waits for q1: %+v %v", n, ok)
	}
	if n, ok := blockedNeed(pl, "q2", "skill:agility:20"); !ok || n.Status != NeedStint || !strings.Contains(n.Detail, "agi to 20") {
		t.Errorf("q2's Agility 20 is a stint of agi: %+v", n)
	}
	if n, ok := blockedNeed(pl, "q1", "skill:mining:10"); !ok || n.Status != NeedStint {
		t.Errorf("q1's Mining 10 is a stint: %+v", n)
	}
	// The ready quests are candidates (q3 is below its advice and deferred, q7 is clean), the blocked ones are not.
	if c := cand(pl, "quest:q7"); c == nil || !c.Eligible {
		t.Errorf("quest:q7 should be an eligible candidate: %+v", c)
	}
	if c := cand(pl, "quest:q3"); c == nil || c.Eligible || !strings.Contains(c.Why, "below advice") {
		t.Errorf("quest:q3 is a candidate, deferred for its advice: %+v", c)
	}
	for _, key := range []string{"quest:q1", "quest:q2", "quest:q4", "quest:q5", "quest:q6"} {
		if cand(pl, key) != nil {
			t.Errorf("%s is blocked and must not be a candidate", key)
		}
	}
	// q1 comes before q2 in the schedule even when the order lists q2 first.
	rev := PlanNeeds(fresh(), []string{"q2", "q1"}, miniTable(), miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if len(rev.Blocked) != 2 || rev.Blocked[0].Quest != "q1" {
		t.Errorf("a prerequisite is scheduled before the quest that wants it: %+v", rev.Blocked)
	}
}

func TestAGapAndAnImpossibleQuestPointTotalAreCountedNotPlanned(t *testing.T) {
	pl := plan(fresh(), DefaultNeedsSettings(), FleetState{})
	if n, ok := blockedNeed(pl, "q4", "skill:fishing:5"); !ok || n.Status != NeedUnreachable || !strings.Contains(n.Detail, "no trainer for fishing 5") {
		t.Errorf("q4: %+v", n)
	}
	if n, ok := blockedNeed(pl, "q5", "qp:100"); !ok || n.Status != NeedUnreachable {
		t.Errorf("q5's 100 quest points are more than the world pays: %+v", n)
	}
	for _, c := range pl.Candidates {
		if strings.Contains(c.Key, "fishing") {
			t.Errorf("a gap is never planned for: %s", c.Key)
		}
	}
	lines := strings.Join(pl.LogLines(), "\n")
	if !strings.Contains(lines, "[needs] unreachable: no trainer for fishing 5") {
		t.Errorf("the log line for the gap: %s", lines)
	}
	if pl.UnreachableCount() != 2 {
		t.Errorf("unreachable count %d", pl.UnreachableCount())
	}
	// A quest-point total the world can pay is a wait, not a gap.
	f := miniFacts()
	q5 := f["q5"]
	q5.QPNeeded = 6
	f["q5"] = q5
	pl2 := PlanNeeds(fresh(), miniOrder, BuildNeedTable(NeedSources{Facts: f}), miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if n, ok := blockedNeed(pl2, "q5", "qp:6"); !ok || n.Status != NeedWaitQuest {
		t.Errorf("6 quest points are reachable: %+v", n)
	}
}

func TestALoopTrainsItAndNothingIsStinted(t *testing.T) {
	pl := plan(fresh(), DefaultNeedsSettings(), FleetState{})
	if n, ok := blockedNeed(pl, "q6", "skill:attack:20"); !ok || n.Status != NeedLoop || !strings.Contains(n.Detail, "atk-loop") {
		t.Errorf("q6 is covered by the loop: %+v", n)
	}
	for _, c := range pl.Candidates {
		if c.Stint != nil && c.Stint.Skill == "attack" {
			t.Errorf("a loop is never stinted: %s", c.Key)
		}
	}
}

// The ranking by value over cost, read off two quests that differ in what they pay.
func TestRankingIsValueOverCostThenOwnersOrder(t *testing.T) {
	f := map[string]RungFacts{
		"cheap": {ID: "cheap", Name: "cheap", QP: 1, EstMinutes: 10},
		"rich":  {ID: "rich", Name: "rich", QP: 8, EstMinutes: 40},
		"twin":  {ID: "twin", Name: "twin", QP: 1, EstMinutes: 10},
	}
	tab := BuildNeedTable(NeedSources{Facts: f})
	pl := PlanNeeds(fresh(), []string{"twin", "cheap", "rich"}, tab, NewCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	var order []string
	for _, c := range pl.Candidates {
		order = append(order, c.Quest)
	}
	// rich: 80 over (40*5/4=50 min); cheap and twin: 10 over 20 minutes (the base slot): the equal ones keep the owner's order.
	if got := strings.Join(order, ","); got != "rich,twin,cheap" {
		t.Errorf("order = %s", got)
	}
	if pl.Next == nil || pl.Next.Quest != "rich" {
		t.Errorf("next = %+v", pl.Next)
	}
	// More quest points never rank a quest lower.
	prev := -1.0
	for qp := 1; qp <= 6; qp++ {
		ff := map[string]RungFacts{"x": {ID: "x", QP: qp, EstMinutes: 30}}
		p := PlanNeeds(fresh(), []string{"x"}, BuildNeedTable(NeedSources{Facts: ff}), NewCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
		if s := p.Candidates[0].Score; s < prev {
			t.Errorf("quest points %d scored %.2f, below %.2f", qp, s, prev)
		} else {
			prev = s
		}
	}
}

// A reward counts toward value only as far as it fills a need that is still open.
func TestARewardIsWorthItsXPOnlyWhereANeedIsOpen(t *testing.T) {
	f := map[string]RungFacts{
		"pays":  {ID: "pays", QP: 0, EstMinutes: 30, XP: map[string]int{"prayer": 5000}},
		"asks":  {ID: "asks", QP: 1, EstMinutes: 30, Gates: []Gate{{Skill: "prayer", Level: 31, Kind: GateServer}}},
		"alone": {ID: "alone", QP: 0, EstMinutes: 30, XP: map[string]int{"prayer": 5000}},
	}
	// With "asks" in the order Prayer 31 is wanted, so "pays" is worth its XP up to what is missing; without it, nothing.
	with := PlanNeeds(fresh(), []string{"pays", "asks"}, BuildNeedTable(NeedSources{Facts: f}), NewCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	without := PlanNeeds(fresh(), []string{"alone"}, BuildNeedTable(NeedSources{Facts: f}), NewCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if cand(with, "quest:pays").Value != 5 || cand(without, "quest:alone").Value != 0 {
		t.Errorf("values: pays %.1f (want 5), alone %.1f (want 0)", cand(with, "quest:pays").Value, cand(without, "quest:alone").Value)
	}
}

func TestTheRiskCeilingAndTheAllowListDecideWhichRungsMayRun(t *testing.T) {
	mineA := row("mine-a", "mining", 1, 20)
	mineB := row("mine-b", "mining", 1, 40)
	mineB.Risk, mineB.EstXPPerHour = R1, 9000
	cat := NewCatalogue(mineA, mineB)
	tab := BuildNeedTable(NeedSources{Facts: miniFacts()})
	sub := fresh()
	sub.HasFood = true
	run := func(set NeedsSettings) BlockedNeed {
		pl := PlanNeeds(sub, []string{"q1"}, tab, cat, FleetState{}, set, t0)
		n, _ := blockedNeed(pl, "q1", "skill:mining:10")
		return n
	}
	// Default: the R0 trainers only.
	if n := run(DefaultNeedsSettings()); n.Status != NeedStint || !strings.Contains(n.Detail, "mine-a") {
		t.Errorf("default: %+v", n)
	}
	// An allow list of the R1 row alone, inside the ceiling: it runs.
	set := DefaultNeedsSettings()
	set.Allow = map[string]bool{"mine-b": true}
	if n := run(set); n.Status != NeedStint || !strings.Contains(n.Detail, "mine-b") {
		t.Errorf("allow mine-b: %+v", n)
	}
	// Matching on the skill allows both, and the lower risk still wins.
	set.Allow = map[string]bool{"mining": true}
	if n := run(set); !strings.Contains(n.Detail, "mine-a") {
		t.Errorf("allow by skill: %+v", n)
	}
	// The ceiling beats the allow list.
	set.Allow, set.MaxRisk = map[string]bool{"mine-b": true}, R0
	if n := run(set); n.Status != NeedSettings || !strings.Contains(n.Detail, "ceiling") {
		t.Errorf("ceiling: %+v", n)
	}
	// An allow list that names nothing that covers the skill leaves the need to the settings, never a gap.
	set = DefaultNeedsSettings()
	set.Allow = map[string]bool{"agility": true}
	if n := run(set); n.Status != NeedSettings || !strings.Contains(n.Detail, "allow list") {
		t.Errorf("not in the allow list: %+v", n)
	}
}

func TestEligibilityGatesEachStint(t *testing.T) {
	stint := func(pl NeedsPlan) *Candidate { return cand(pl, "stint:mine-a:mining:10") }
	base := func() (Subject, NeedsSettings, FleetState) { return fresh(), DefaultNeedsSettings(), FleetState{} }

	sub, set, fleet := base()
	pl := plan(sub, set, fleet)
	c := stint(pl)
	if c == nil || !c.Eligible || c.Stint.Site != "rocks" || c.Stint.Rung != "r1" || c.Stint.FromLevel != 1 || c.Stint.ToLevel != 10 {
		t.Fatalf("the plain stint: %+v", c)
	}
	if len(c.Helps) != 1 || c.Helps[0] != "q1" || c.Quest != "q1" {
		t.Errorf("helps %v", c.Helps)
	}

	// The fleet is full there.
	sub, set, fleet = base()
	fleet.SiteLoad = map[string]int{"rocks": 2}
	if c = stint(plan(sub, set, fleet)); c == nil || c.Eligible || !strings.Contains(c.Why, "capacity") {
		t.Errorf("full site: %+v", c)
	}
	// One place left.
	fleet.SiteLoad = map[string]int{"rocks": 1}
	if c = stint(plan(sub, set, fleet)); !c.Eligible {
		t.Errorf("a site with room: %+v", c)
	}

	// A parked skill.
	sub, set, fleet = base()
	sub.Needs[StintKey("mining")] = NeedHistory{Fails: 1, ParkedUntil: t0.Add(10 * time.Minute)}
	if c = stint(plan(sub, set, fleet)); c.Eligible || !strings.Contains(c.Why, "parked until 12:10") {
		t.Errorf("parked: %+v", c)
	}
	sub.Needs[StintKey("mining")] = NeedHistory{ParkedUntil: t0.Add(-time.Minute)}
	if c = stint(plan(sub, set, fleet)); !c.Eligible {
		t.Errorf("a park that has run out: %+v", c)
	}

	// The share: spent, short, and a clipped stint.
	sub, set, fleet = base()
	set.ShareLeft = 0
	if c = stint(plan(sub, set, fleet)); c.Eligible || !strings.Contains(c.Why, "share is spent") {
		t.Errorf("share spent: %+v", c)
	}
	set.ShareLeft = 10 * time.Minute
	if c = stint(plan(sub, set, fleet)); c.Eligible {
		t.Errorf("less share than a minimum stint: %+v", c)
	}
	set.ShareLeft = 25 * time.Minute
	if c = stint(plan(sub, set, fleet)); !c.Eligible || c.Stint.Minutes != 25 {
		t.Errorf("a stint clipped to the share: %+v", c.Stint)
	}

	// Safety: a risk class of R1 or more wants food and no recent death.
	risky := row("risky", "mining", 1, 20)
	risky.Risk, risky.Sites = R1, []TrainerSite{{Key: "pit", Capacity: 3}}
	riskyCat := NewCatalogue(risky)
	set = DefaultNeedsSettings()
	set.Allow = map[string]bool{"risky": true}
	run := func(s Subject) *Candidate {
		return cand(PlanNeeds(s, []string{"q1"}, miniTable(), riskyCat, FleetState{}, set, t0), "stint:risky:mining:10")
	}
	sub = fresh()
	if c = run(sub); c.Eligible || !strings.Contains(c.Why, "no food") {
		t.Errorf("no food: %+v", c)
	}
	sub.HasFood = true
	if c = run(sub); !c.Eligible {
		t.Errorf("food in the pack: %+v", c)
	}
	sub.DeathsToday = 1
	if c = run(sub); c.Eligible || !strings.Contains(c.Why, "death today") {
		t.Errorf("died today: %+v", c)
	}
	sub.DeathsToday, sub.Deaths7d = 0, 2
	if c = run(sub); c.Eligible || !strings.Contains(c.Why, "deaths in 7 days") {
		t.Errorf("two deaths in a week: %+v", c)
	}
	sub.Deaths7d = 1
	if c = run(sub); !c.Eligible {
		t.Errorf("one death in a week is the most: %+v", c)
	}
	// An R0 stint needs neither food nor a clean week.
	sub, set, fleet = base()
	sub.DeathsToday, sub.Deaths7d = 3, 5
	if c = stint(plan(sub, set, fleet)); !c.Eligible {
		t.Errorf("R0 after deaths: %+v", c)
	}
}

func TestAQuestIsHeldBackBySlotAndPark(t *testing.T) {
	sub, set := fresh(), DefaultNeedsSettings()
	pl := plan(sub, set, FleetState{SlotBusy: map[string]bool{"q7": true}})
	if c := cand(pl, "quest:q7"); c.Eligible || !strings.Contains(c.Why, "slot") {
		t.Errorf("slot full: %+v", c)
	}
	if pl.Next != nil && pl.Next.Key == "quest:q7" {
		t.Error("a quest in a full slot is never the answer")
	}
	sub.Parked["q7"] = t0.Add(time.Hour)
	if c := cand(plan(sub, set, FleetState{}), "quest:q7"); c.Eligible || !strings.Contains(c.Why, "parked until 13:00") {
		t.Errorf("parked: %+v", c)
	}
	sub.Parked["q7"] = t0.Add(-time.Hour)
	if c := cand(plan(sub, set, FleetState{}), "quest:q7"); !c.Eligible {
		t.Errorf("a park that has run out: %+v", c)
	}
}

func TestAStintNeedsItsConsumablesInHandOrGatherable(t *testing.T) {
	smith := row("smith", "mining", 1, 20)
	smith.Inputs = []TrainerInput{
		{Name: "Hammer", IDs: []int{1}, Qty: 1, Tool: true},
		{Name: "Bars", IDs: []int{2, 3}, Qty: 10},
		{Name: "Flax", IDs: []int{4}, Qty: 10, Gathers: true},
	}
	cat := NewCatalogue(smith)
	run := func(s Subject) *Candidate {
		return cand(PlanNeeds(s, []string{"q1"}, miniTable(), cat, FleetState{}, DefaultNeedsSettings(), t0), "stint:smith:mining:10")
	}
	sub := fresh()
	if c := run(sub); !c.Eligible {
		t.Errorf("holdings not read: the stock is not judged: %+v", c)
	}
	sub.OwnKnown = true
	if c := run(sub); c.Eligible || !strings.Contains(c.Why, "wants 10 bars") {
		t.Errorf("no bars: %+v", c)
	}
	sub.Own[2], sub.Own[3] = 4, 6
	if c := run(sub); !c.Eligible {
		t.Errorf("ten bars between two kinds: %+v", c)
	}
	// A mule on another world is no stock; a reachable one is.
	sub.Own = map[int]int{2: 4}
	sub.MuleKnown, sub.MuleStock, sub.World, sub.MuleWorld = true, map[int]int{3: 50}, 2, 1
	if c := run(sub); c.Eligible {
		t.Errorf("the mule is on another world: %+v", c)
	}
	sub.MuleWorld = 2
	if c := run(sub); !c.Eligible {
		t.Errorf("the mule is on the account's world: %+v", c)
	}
}

func TestStintSizingAndRungNames(t *testing.T) {
	pl := plan(fresh(), DefaultNeedsSettings(), FleetState{})
	c := cand(pl, "stint:agi:agility:20")
	if c == nil {
		t.Fatal("no agility stint")
	}
	// 1 to 20 is 4,470 XP at 6,000 an hour: 45 minutes (to the next five), inside the row's 20 to 60.
	if c.Stint.Minutes != 45 || c.Stint.Rung != TrainRungID("agility", 20) {
		t.Errorf("agility stint %+v: no named rung trains exactly to 20, so the generic id", c.Stint)
	}
	if want := float64(levels.XPAtLevel(20)) / 6000; c.Stint.Hours != want {
		t.Errorf("hours %v want %v", c.Stint.Hours, want)
	}
	// A short need still takes the row's minimum, a long one its maximum.
	near := fresh()
	near.XP["agility"] = levels.XPAtLevel(19)
	if c = cand(plan(near, DefaultNeedsSettings(), FleetState{}), "stint:agi:agility:20"); c.Stint.Minutes != 20 {
		t.Errorf("a short stint: %+v", c.Stint)
	}
	long := fresh()
	f := miniFacts()
	q2 := f["q2"]
	q2.Gates = []Gate{{Skill: "agility", Level: 35, Kind: GateServer}}
	f["q2"] = q2
	pl = PlanNeeds(long, miniOrder, BuildNeedTable(NeedSources{Facts: f}), miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if c = cand(pl, "stint:agi:agility:35"); c == nil || c.Stint.Minutes != 60 {
		t.Errorf("a long stint is capped at the row's maximum: %+v", c)
	}
	// A named rung is used when it trains exactly to the stint's end.
	if c = cand(pl, "stint:mine-a:mining:10"); c.Stint.Rung != "r1" {
		t.Errorf("mining 10 has the named rung r1: %+v", c.Stint)
	}
}

// A quest below the study's advice is passed over a bounded number of times, and then competes as it is.
func TestAQuestBelowItsAdviceIsDeferredAtMostThreeTimes(t *testing.T) {
	sub, set := fresh(), DefaultNeedsSettings()
	pl := plan(sub, set, FleetState{})
	c := cand(pl, "quest:q3")
	if c.Eligible || !strings.Contains(c.Why, "deferred, below advice") || pl.Deferrals["q3"] != 1 {
		t.Fatalf("first pass: %+v deferrals %v", c, pl.Deferrals)
	}
	sub.Deferrals = pl.Deferrals
	pl = plan(sub, set, FleetState{})
	if pl.Deferrals["q3"] != 2 {
		t.Errorf("second pass: %v", pl.Deferrals)
	}
	sub.Deferrals = map[string]int{"q3": 3}
	pl = plan(sub, set, FleetState{})
	if c = cand(pl, "quest:q3"); !c.Eligible || pl.Deferrals != nil {
		t.Errorf("at the bound it runs below its advice: %+v %v", c, pl.Deferrals)
	}
	if !strings.Contains(c.Reason, "below advice") {
		t.Errorf("and says so: %q", c.Reason)
	}
	// With nothing else ready it is not deferred at all.
	only := PlanNeeds(fresh(), []string{"q3"}, miniTable(), miniCatalogue(), FleetState{}, set, t0)
	if c = cand(only, "quest:q3"); !c.Eligible || only.Deferrals != nil {
		t.Errorf("a lone quest is not deferred: %+v", c)
	}
}

func TestNoteStintParksAfterThreeFailuresAndADeathParksForADay(t *testing.T) {
	var h NeedHistory
	h = NoteStint(h, false, false, t0)
	h = NoteStint(h, false, false, t0)
	if h.Fails != 2 || !h.ParkedUntil.IsZero() {
		t.Fatalf("two failures do not park: %+v", h)
	}
	h = NoteStint(h, false, false, t0)
	if h.Parks != 1 || !h.ParkedUntil.Equal(t0.Add(5*time.Minute)) || h.Fails != 0 {
		t.Fatalf("the third failure parks for the first backoff, 5 minutes: %+v", h)
	}
	for _, want := range []time.Duration{10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 2 * time.Hour, 2 * time.Hour} {
		for i := 0; i < MaxStintFails; i++ {
			h = NoteStint(h, false, false, t0)
		}
		if got := h.ParkedUntil.Sub(t0); got != want {
			t.Errorf("park %d: %v, want %v", h.Parks, got, want)
		}
	}
	// A success clears the failures and the park.
	h = NoteStint(h, true, false, t0)
	if h.Fails != 0 || !h.ParkedUntil.IsZero() {
		t.Errorf("success: %+v", h)
	}
	// A death parks for a day at once and is remembered.
	h = NoteStint(NeedHistory{}, false, true, t0)
	if !h.ParkedUntil.Equal(t0.Add(24*time.Hour)) || !h.DiedAt.Equal(t0) {
		t.Errorf("death: %+v", h)
	}
}

func TestLogLinesSayWhatIsNextAndWhatWasLeftOut(t *testing.T) {
	sub := fresh()
	sub.Deferrals = map[string]int{"q3": 3}
	pl := plan(sub, DefaultNeedsSettings(), FleetState{})
	lines := pl.LogLines()
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "[needs] next: ") {
		t.Fatalf("lines: %v", lines)
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{"[needs] unreachable: no trainer for fishing 5", "deferred: "} {
		if !strings.Contains(all, want) {
			t.Errorf("no %q in\n%s", want, all)
		}
	}
	// A stint reads as one.
	set := DefaultNeedsSettings()
	f := map[string]RungFacts{"only": {ID: "only", Name: "Only", QP: 3, Gates: []Gate{{Skill: "agility", Level: 25, Kind: GateServer}}}}
	cat := NewCatalogue(func() TrainerSpec {
		r := row("agi", "agility", 1, 35)
		r.Rungs = map[int]string{25: "agility25"}
		return r
	}())
	one := PlanNeeds(fresh(), []string{"only"}, BuildNeedTable(NeedSources{Facts: f}), cat, FleetState{}, set, t0)
	got := one.LogLines()[0]
	want := "[needs] next: stint agility 1->25 with agi (rung agility25, 60 min) for only"
	if !strings.HasPrefix(got, want) {
		t.Errorf("\n got %s\nwant %s...", got, want)
	}
	// Nothing left.
	done := fresh()
	for id := range miniFacts() {
		done.Done[id] = true
	}
	d := plan(done, set, FleetState{})
	if !d.Done || d.Next != nil || !strings.Contains(d.LogLines()[0], "every quest of the order is done") {
		t.Errorf("done: %+v %v", d.Done, d.LogLines())
	}
}

func TestPlanNeedsToleratesAMessyOrder(t *testing.T) {
	f := miniFacts()
	f["a"] = RungFacts{ID: "a", Name: "a", QP: 1, Prereqs: []string{"b"}}
	f["b"] = RungFacts{ID: "b", Name: "b", QP: 1, Prereqs: []string{"a"}}
	f["c"] = RungFacts{ID: "c", Name: "c", QP: 1, Prereqs: []string{"missing"}}
	tab := BuildNeedTable(NeedSources{Facts: f})
	pl := PlanNeeds(fresh(), []string{"a", "r1", "ghost", "c", "q7", "q7"}, tab, miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if cand(pl, "quest:q7") == nil || cand(pl, "quest:q7") != nil && !cand(pl, "quest:q7").Eligible {
		t.Error("the ready quest is still planned")
	}
	if len(pl.Warnings) == 0 || !strings.Contains(strings.Join(pl.Warnings, ";"), "ghost") {
		t.Errorf("an unknown id is a warning: %v", pl.Warnings)
	}
	if n, ok := blockedNeed(pl, "c", "quest:missing"); !ok || n.Status != NeedUnreachable {
		t.Errorf("a prerequisite with no facts is unreachable: %+v", n)
	}
	// The cycle neither hangs nor is run.
	if cand(pl, "quest:a") != nil || cand(pl, "quest:b") != nil {
		t.Error("a quest in a prerequisite cycle is never ready")
	}
	// Training rung ids in the order are acquirers, not quests.
	for _, c := range pl.Candidates {
		if c.Quest == "r1" {
			t.Errorf("a rung was planned as a quest: %+v", c)
		}
	}
}

func TestPlanNeedsIsDeterministic(t *testing.T) {
	sub := fresh()
	a := plan(sub, DefaultNeedsSettings(), FleetState{})
	for i := 0; i < 30; i++ {
		if b := plan(sub, DefaultNeedsSettings(), FleetState{}); !reflect.DeepEqual(a, b) {
			t.Fatalf("run %d differs", i)
		}
	}
}

// Contention breaks a tie between rows of the same rank, risk and speed, and nothing else.
func TestContentionBreaksTiesBetweenEqualRows(t *testing.T) {
	a, b := row("a", "mining", 1, 20), row("b", "mining", 1, 20)
	a.Sites, b.Sites = []TrainerSite{{Key: "sa", Capacity: 4}}, []TrainerSite{{Key: "sb", Capacity: 4}}
	cat := NewCatalogue(a, b)
	run := func(load map[string]int) string {
		pl := PlanNeeds(fresh(), []string{"q1"}, miniTable(), cat, FleetState{SiteLoad: load}, DefaultNeedsSettings(), t0)
		n, _ := blockedNeed(pl, "q1", "skill:mining:10")
		return n.Detail
	}
	if got := run(nil); !strings.HasPrefix(got, "a ") {
		t.Errorf("no load: the id breaks the tie: %s", got)
	}
	if got := run(map[string]int{"sa": 3}); !strings.HasPrefix(got, "b ") {
		t.Errorf("a is busier: %s", got)
	}
	if got := run(map[string]int{"sa": 4}); !strings.HasPrefix(got, "b ") {
		t.Errorf("a is full: %s", got)
	}
	// Both full: the best row stays the pick and its candidate says why it cannot run.
	pl := PlanNeeds(fresh(), []string{"q1"}, miniTable(), cat, FleetState{SiteLoad: map[string]int{"sa": 4, "sb": 4}}, DefaultNeedsSettings(), t0)
	if c := cand(pl, "stint:a:mining:10"); c == nil || c.Eligible || !strings.Contains(c.Why, "capacity") {
		t.Errorf("both full: %+v", c)
	}
}

// An item that only a skill gets becomes the skill's stint, and an item with no way is a named gap.
func TestItemNeedsBecomeStintsOrGaps(t *testing.T) {
	f := map[string]RungFacts{
		"smith": {ID: "smith", Name: "Smith", QP: 2, EstMinutes: 20},
		"stuck": {ID: "stuck", Name: "Stuck", QP: 2, EstMinutes: 20},
	}
	items := []ItemNeed{
		{Item: 900, Name: "Widget", Qty: 1, Quests: []string{"smith"}, Source: "test",
			Chain: []ChainLink{{Kind: SourceBank}, {Kind: SourceSkill, Skill: "mining", Level: 15, Method: "make-widget", Built: true}}},
		{Item: 901, Name: "Gizmo", Qty: 1, Quests: []string{"stuck"}, Source: "test", Gap: "no gizmo source (lane Z)", Lane: "Z",
			Chain: []ChainLink{{Kind: SourceBank}}},
	}
	tab := BuildNeedTable(NeedSources{Facts: f, Items: items})
	sub := fresh()
	sub.OwnKnown = true
	pl := PlanNeeds(sub, []string{"smith", "stuck"}, tab, miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if n, ok := blockedNeed(pl, "smith", "skill:mining:15"); !ok || n.Status != NeedStint {
		t.Errorf("the widget needs Mining 15, a stint: %+v", n)
	}
	if c := cand(pl, "stint:mine-a:mining:15"); c == nil || c.Quest != "smith" {
		t.Errorf("stint: %+v", c)
	}
	if n, ok := blockedNeed(pl, "stuck", "item:901:1"); !ok || n.Status != NeedUnreachable || !strings.Contains(n.Detail, "lane Z") {
		t.Errorf("the gizmo has no source: %+v", n)
	}
	if !strings.Contains(strings.Join(pl.LogLines(), "\n"), "[needs] unreachable: Gizmo: no gizmo source (lane Z)") {
		t.Errorf("log: %v", pl.LogLines())
	}
	// With the holdings unread the items are not judged: both quests are ready, and the plan says what it left out.
	sub.OwnKnown = false
	pl = PlanNeeds(sub, []string{"smith", "stuck"}, tab, miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if cand(pl, "quest:smith") == nil || cand(pl, "quest:stuck") == nil || len(pl.Unjudged) != 2 {
		t.Errorf("unjudged: candidates %d unjudged %d", len(pl.Candidates), len(pl.Unjudged))
	}
	// A mule on another world, with nothing else open, is the far verdict, and the hop setting opens it.
	farItems := []ItemNeed{{Item: 902, Name: "Bar", Qty: 1, Quests: []string{"smith"}, Source: "test", Chain: []ChainLink{{Kind: SourceBank}, {Kind: SourceMule}}}}
	farTab := BuildNeedTable(NeedSources{Facts: f, Items: farItems})
	far := fresh()
	far.OwnKnown, far.MuleKnown, far.MuleStock, far.World, far.MuleWorld = true, true, map[int]int{902: 9}, 3, 1
	pl = PlanNeeds(far, []string{"smith"}, farTab, miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if n, ok := blockedNeed(pl, "smith", "item:902:1"); !ok || n.Status != NeedFar {
		t.Errorf("far mule: %+v", n)
	}
	set := DefaultNeedsSettings()
	set.MuleHop = true
	pl = PlanNeeds(far, []string{"smith"}, farTab, miniCatalogue(), FleetState{}, set, t0)
	if cand(pl, "quest:smith") == nil {
		t.Error("with the hop the mule answers and the quest is ready")
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// THE PROPERTY: THE PLANNER NEVER LOOPS.

var allSkills = []string{"agility", "attack", "cooking", "crafting", "defence", "firemaking", "fishing", "fletching", "herblore", "hitpoints",
	"magic", "mining", "prayer", "ranged", "runecraft", "smithing", "strength", "thieving", "woodcutting"}

// randomSubject is an account drawn to cover the states the live fleet has: fresh, mid-ladder and maxed, holdings read and not,
// a mule near and far, deaths and parks.
func randomSubject(rng *rand.Rand, quests []string, facts map[string]RungFacts) (Subject, NeedsSettings, FleetState) {
	sub := accountAt(nil)
	maxLevel := []int{1, 5, 20, 40, 60, 80}[rng.Intn(6)]
	for _, s := range allSkills {
		l := 1
		if rng.Intn(3) > 0 {
			l = 1 + rng.Intn(maxLevel)
		}
		sub.XP[s] = levels.XPAtLevel(l)
		if l < 99 {
			sub.XP[s] += rng.Intn(levels.XPAtLevel(l+1) - levels.XPAtLevel(l) + 1)
		}
	}
	for _, id := range quests {
		if rng.Intn(100) < 25 {
			sub.Done[id] = true
			sub.QP += facts[id].QP
		}
	}
	sub.QP += rng.Intn(6)
	sub.OwnKnown = rng.Intn(2) == 0
	for _, id := range []int{ObjNails, ObjSilverSickle, ObjSilverBar, ObjRuneEssence, ObjShark, ObjBones, 2351, 2349} {
		if rng.Intn(4) == 0 {
			sub.Own[id] = 1 + rng.Intn(60)
		}
	}
	sub.MuleKnown = rng.Intn(4) > 0
	for _, id := range []int{ObjSilverBar, ObjShark, ObjBones, ObjNails, DoricIronOre, 2351, 2349, 2353} {
		if rng.Intn(2) == 0 {
			sub.MuleStock[id] = rng.Intn(100)
		}
	}
	sub.World, sub.MuleWorld = 1+rng.Intn(3), 1
	sub.IsProducer = rng.Intn(25) == 0
	sub.DeathsToday, sub.Deaths7d, sub.HasFood = rng.Intn(2), rng.Intn(4), rng.Intn(2) == 0
	sub.Needs, sub.Parked, sub.Deferrals = map[string]NeedHistory{}, map[string]time.Time{}, map[string]int{}
	for _, s := range allSkills {
		if rng.Intn(12) == 0 {
			sub.Needs[StintKey(s)] = NeedHistory{Fails: 1, ParkedUntil: t0.Add(time.Duration(rng.Intn(48*60)-60) * time.Minute)}
		}
	}
	for _, id := range quests {
		if rng.Intn(15) == 0 {
			sub.Parked[id] = t0.Add(time.Duration(rng.Intn(600)-60) * time.Minute)
		}
		if rng.Intn(10) == 0 {
			sub.Deferrals[id] = rng.Intn(5)
		}
	}
	set := DefaultNeedsSettings()
	set.MaxRisk = Risk(rng.Intn(4))
	set.MuleHop = rng.Intn(2) == 0
	switch rng.Intn(4) {
	case 0:
		set.Allow = map[string]bool{}
	case 1:
		set.Allow = map[string]bool{"prayer": true, "cows-hitpoints": true, "anvil-daggers": true}
	}
	if rng.Intn(3) == 0 {
		set.ShareLeft = time.Duration(rng.Intn(600)) * time.Minute
	}
	fleet := FleetState{SlotBusy: map[string]bool{}, SiteLoad: map[string]int{}, SlotLoad: map[string]int{}}
	for _, id := range quests {
		if rng.Intn(10) == 0 {
			fleet.SlotBusy[id] = true
		}
	}
	for _, tr := range DefaultCatalogue().Trainers() {
		for _, s := range tr.Sites {
			if rng.Intn(5) == 0 {
				fleet.SiteLoad[s.Key] = rng.Intn(s.Capacity + 1)
			}
		}
	}
	return sub, set, fleet
}

// apply is the world's answer to the planner's choice: a quest completes and pays its rewards, a stint raises the skill by
// what its trainer makes in the time (never past the stint's end level) and spends the share.
func apply(sub *Subject, set *NeedsSettings, c *Candidate, tab NeedTable, cat Catalogue) {
	switch c.Kind {
	case CandQuest:
		f, _ := tab.Facts(c.Quest)
		sub.Done[c.Quest] = true
		sub.QP += f.QP
		for s, xp := range f.XP {
			sub.XP[s] += xp
		}
	case CandStint:
		st := c.Stint
		tr, _ := cat.ByID(st.Trainer)
		gain := tr.Rate() * st.Minutes / 60
		next := min(sub.XP[st.Skill]+max(gain, 1), levels.XPAtLevel(st.ToLevel))
		sub.XP[st.Skill] = max(next, sub.XP[st.Skill]+1)
		if set.ShareLeft >= 0 {
			set.ShareLeft -= time.Duration(st.Minutes) * time.Minute
		}
	}
}

func signature(sub Subject, set NeedsSettings) string {
	var b strings.Builder
	var done []string
	for id, d := range sub.Done {
		if d {
			done = append(done, id)
		}
	}
	sort.Strings(done)
	fmt.Fprintf(&b, "%v|%d|%d|", done, sub.QP, set.ShareLeft)
	for _, s := range allSkills {
		fmt.Fprintf(&b, "%d,", sub.XP[s])
	}
	return b.String()
}

// Random accounts are each planned once, and the answer holds the invariants a caller relies on (checkPlan). The first simSize
// of them are then followed to the end: the planner's answers are applied one after another (a quest completes and pays, a stint
// raises its XP by what the trainer makes in the time and spends the share) until it has none. Each answer changes the state it
// was a function of, so no state is answered twice, the run ends, and what is left has no eligible candidate.
//
// Each followed account costs about 20 answers and each answer a full plan, so the default plans 3,000 accounts and follows 250
// of them to the end (500 and 60 under -short). RSB0T_NEEDS_ACCOUNTS=10000 plans and follows ten thousand (the plan's figure;
// about twenty minutes on the loaded laptop, run it before changing the planner's ranking or eligibility).
func TestThePlannerNeverLoops(t *testing.T) {
	accounts, simSize := 3000, 250
	if testing.Short() {
		accounts, simSize = 500, 60
	}
	if v := os.Getenv("RSB0T_NEEDS_ACCOUNTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			accounts, simSize = n, n
		}
	}
	tab, cat := DefaultNeedTable(), DefaultCatalogue()
	quests := tab.Quests()
	facts := map[string]RungFacts{}
	for _, id := range quests {
		facts[id], _ = tab.Facts(id)
	}
	order := append([]string(nil), OwnerLadder...)
	rng := rand.New(rand.NewSource(20261020))
	steps, maxSteps, stints, ranQuests, idle := 0, 0, 0, 0, 0
	for n := 0; n < accounts; n++ {
		sub, set, fleet := randomSubject(rng, quests, facts)
		pl := PlanNeeds(sub, order, tab, cat, fleet, set, t0)
		checkPlan(t, n, sub, set, pl)
		if n >= simSize {
			continue
		}
		seen := map[string]bool{signature(sub, set): true}
		var i int
		for i = 0; i < 400; i++ {
			if i > 0 {
				pl = PlanNeeds(sub, order, tab, cat, fleet, set, t0)
				checkPlan(t, n, sub, set, pl)
			}
			if pl.Next == nil {
				break
			}
			c := pl.Next
			apply(&sub, &set, c, tab, cat)
			steps++
			if c.Kind == CandStint {
				stints++
			} else {
				ranQuests++
			}
			sig := signature(sub, set)
			if seen[sig] {
				t.Fatalf("account %d step %d: %s changed nothing (state seen before)", n, i, c.Key)
			}
			seen[sig] = true
			sub.Deferrals = pl.Deferrals // the caller keeps them (nil when none changed)
			if sub.Deferrals == nil {
				sub.Deferrals = map[string]int{}
			}
		}
		if i >= 400 {
			t.Fatalf("account %d: the plan was still answering after 400 steps", n)
		}
		if i == 0 {
			idle++
		}
		maxSteps = max(maxSteps, i)
	}
	if stints == 0 || ranQuests == 0 {
		t.Errorf("the simulation exercised %d stints and %d quests: the generator is not covering the planner", stints, ranQuests)
	}
	t.Logf("%d accounts planned, %d followed to the end: %d answers applied (%d stints, %d quests), the longest run %d steps, %d with nothing to do at once",
		accounts, min(simSize, accounts), steps, stints, ranQuests, maxSteps, idle)
}

// checkPlan holds one answer to the invariants that do not depend on what the world does next.
func checkPlan(t *testing.T, n int, sub Subject, set NeedsSettings, pl NeedsPlan) {
	t.Helper()
	seen := map[string]bool{}
	for _, c := range pl.Candidates {
		if seen[c.Key] {
			t.Fatalf("account %d: candidate %s twice", n, c.Key)
		}
		seen[c.Key] = true
		if c.Kind == CandQuest && sub.Done[c.Quest] {
			t.Fatalf("account %d: a done quest %s is a candidate", n, c.Quest)
		}
		if c.Cost <= 0 || c.Value < 0 || c.Score != c.Score {
			t.Fatalf("account %d: %s has value %v cost %v score %v", n, c.Key, c.Value, c.Cost, c.Score)
		}
		if c.Kind == CandStint {
			st := c.Stint
			if st == nil || st.FromLevel >= st.ToLevel || st.Rung == "" || st.Trainer == "" || len(c.Helps) == 0 {
				t.Fatalf("account %d: malformed stint %s: %+v helps %v", n, c.Key, st, c.Helps)
			}
			if st.Minutes < 0 || st.Minutes > int(set.MaxStint/time.Minute) {
				t.Fatalf("account %d: stint of %d minutes", n, st.Minutes)
			}
			if c.Eligible && set.ShareLeft >= 0 && time.Duration(st.Minutes)*time.Minute > set.ShareLeft {
				t.Fatalf("account %d: an eligible stint of %d minutes with %s of share", n, st.Minutes, set.ShareLeft)
			}
			if c.Eligible && sub.Level(st.Skill) != st.FromLevel {
				t.Fatalf("account %d: stint from level %d, account at %d", n, st.FromLevel, sub.Level(st.Skill))
			}
		}
	}
	first := ""
	for _, c := range pl.Candidates {
		if c.Eligible {
			first = c.Key
			break
		}
	}
	switch {
	case pl.Next == nil && first != "":
		t.Fatalf("account %d: no Next with an eligible candidate %s", n, first)
	case pl.Next != nil && (!pl.Next.Eligible || pl.Next.Key != first):
		t.Fatalf("account %d: Next %s is not the best eligible candidate (%s)", n, pl.Next.Key, first)
	}
	if pl.NextStint != nil && (pl.NextStint.Stint == nil || !pl.NextStint.Eligible) {
		t.Fatalf("account %d: NextStint %+v", n, pl.NextStint)
	}
	if pl.Done && pl.Next != nil {
		t.Fatalf("account %d: done and a next", n)
	}
	// A level that only makes an item the producer alone makes is never an ordinary account's to train (plan 3.5): it is not even
	// among the needs the planner resolves for it.
	if !sub.IsProducer {
		exclusive := map[int]bool{}
		for _, in := range ItemNeeds {
			if in.Producer == ProducerExclusive {
				exclusive[in.Item] = true
			}
		}
		for _, b := range pl.Blocked {
			for _, bn := range b.Needs {
				if bn.Need.Kind == NeedSkill && exclusive[bn.Need.WaiverItem] {
					t.Fatalf("account %d: %s is blocked on %s for an account that is not the producer: %+v", n, b.Quest, bn.Need.Describe(), bn)
				}
			}
		}
	}
}

// A quest that waits on an item only the producer makes holds no stint for an ordinary account, says so in the log, and is the
// producer's own stint; the mini world has the same shape as the nails, with a Mining level the catalogue can train.
func TestAWaitForTheProducerIsLoggedAndTrainsNothing(t *testing.T) {
	f := map[string]RungFacts{
		"nail": {ID: "nail", Name: "Nail quest", QP: 1, EstMinutes: 20,
			Gates: []Gate{{Skill: "mining", Level: 10, Kind: GateItem, WaiverItem: 900, WaiverQty: 4}}},
	}
	items := []ItemNeed{{Item: 900, Name: "Widget", Qty: 4, Quests: []string{"nail"}, Producer: ProducerExclusive, Source: "test", Lane: "Z",
		Chain: []ChainLink{{Kind: SourceBank}, {Kind: SourceMule}, {Kind: SourceSkill, Skill: "mining", Level: 10, Method: "make-widget", Built: true}}}}
	tab := BuildNeedTable(NeedSources{Facts: f, Items: items})
	order := []string{"nail"}

	sub := fresh()
	sub.OwnKnown, sub.MuleKnown = true, true
	pl := PlanNeeds(sub, order, tab, miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if pl.Next != nil || len(pl.Candidates) != 0 || len(pl.Unreachable) != 0 {
		t.Fatalf("the ordinary account waits and trains nothing: next %+v candidates %d unreachable %d", pl.Next, len(pl.Candidates), len(pl.Unreachable))
	}
	n, ok := blockedNeed(pl, "nail", "item:900:4")
	if !ok || n.Status != NeedProvision {
		t.Fatalf("the widgets are a provision: %+v %v", n, ok)
	}
	lines := strings.Join(pl.LogLines(), "\n")
	if !strings.Contains(lines, "[needs] next: none; deferred: nail (4 Widget: ask the mule; the designated producer makes widget (the mule holds 0 of the 4 wanted))") {
		t.Errorf("the log says what the quest waits for: %s", lines)
	}

	sub.IsProducer = true
	pl = PlanNeeds(sub, order, tab, miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if c := cand(pl, "stint:mine-a:mining:10"); c == nil || !c.Eligible || pl.Next == nil || pl.Next.Key != c.Key {
		t.Errorf("the producer trains the level: %+v next %+v", c, pl.Next)
	}

	// The mule's stock ends the wait.
	sub.IsProducer = false
	sub.MuleStock[900] = 4
	pl = PlanNeeds(sub, order, tab, miniCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if cand(pl, "quest:nail") == nil || pl.Next == nil || pl.Next.Key != "quest:nail" {
		t.Errorf("with the widgets on the mule the quest is ready: next %+v", pl.Next)
	}
}

// Dragon Slayer is held by the ladder (HeldRungs); a plan that answered with it would be an answer the ladder refuses for ever.
func TestAHeldRungIsNeverTheAnswer(t *testing.T) {
	f := map[string]RungFacts{DragonSlayerID: {ID: DragonSlayerID, Name: "Dragon Slayer", QP: 2, EstMinutes: 40}}
	pl := PlanNeeds(fresh(), []string{DragonSlayerID}, BuildNeedTable(NeedSources{Facts: f}), NewCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	c := cand(pl, "quest:"+DragonSlayerID)
	if c == nil || c.Eligible || !strings.Contains(c.Why, "held back") || pl.Next != nil {
		t.Errorf("held: %+v next %+v", c, pl.Next)
	}
}

// The zero Subject (no maps at all) is a fresh account and must not panic anything.
func TestPlanNeedsAcceptsTheZeroSubject(t *testing.T) {
	pl := PlanNeeds(Subject{}, OwnerLadder, DefaultNeedTable(), DefaultCatalogue(), FleetState{}, DefaultNeedsSettings(), t0)
	if pl.Next == nil || len(pl.Candidates) == 0 || len(pl.LogLines()) == 0 {
		t.Errorf("a fresh account has something to do: %+v", pl.Next)
	}
	if got := FleetNeeds([]Subject{{}}, OwnerLadder, DefaultNeedTable(), DefaultCatalogue(), DefaultNeedsSettings(), false); got.Accounts != 1 {
		t.Errorf("report: %+v", got)
	}
}
