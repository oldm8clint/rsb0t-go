package questplan

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

// THE NEEDS PLANNER (plan 3.2 D). Pure: it reads one account's subject, the owner's order, the merged needs, the trainer
// catalogue and a snapshot of what the fleet is doing now, and answers with the next thing the account should do about
// what it lacks, and why not the others. It runs nothing; lane N3 gives it a tier to run in and lane N7 the hold.
//
// WHAT IT DECIDES, in the plan's order.
//
//  1. It walks the owner's order from the account's first incomplete quest, scheduling a prerequisite quest before the
//     quest that wants it.
//  2. For each quest it computes the unmet needs (NeedTable.Unmet).
//  3. For each unmet skill need it takes the trainer that covers the account's level: a LOOP the fleet already runs
//     (Stage 5's combat trainer, the magic slices) is reported as covering the need and never stinted; otherwise the
//     best PERMITTED rung by rank, risk, hours, then fleet contention. A stretch no row covers is "unreachable: no trainer for
//     <skill> <level>": named and counted, never planned for.
//  4. It ranks the candidates (a quest that is ready, a stint that unblocks quests) by value over cost.
//  5. It picks the best that is ELIGIBLE now: the quest's slot is free, the need is not parked, the trainer is inside the
//     risk ceiling and the allow list, a site has room, the share has time left, the account is safe for the risk class, and
//     the trainer's stock is in hand.
//
// IT CANNOT LOOP. A stint it returns raises an XP total the need is measured against, and a quest it returns completes: so
// every action it names changes the very state that made it the answer. The livelock guards are the rest: an advised
// shortfall defers a quest at most MaxDeferrals times, a failed stint parks its need on the side store's backoff
// (NoteStint), and a need with no trainer is a counted gap and is skipped. TestThePlannerNeverLoops applies the plan's
// answers to 10,000 random accounts until nothing is left and fails on a step that changes nothing.

// NeedsWeights price a candidate. They are the planner's own and only order candidates; nothing is bought with them.
type NeedsWeights struct {
	// PerQP is the value of one quest point.
	PerQP float64
	// PerKiloXP is the value of 1,000 XP of a quest's reward that fills a skill need still open somewhere in the ladder.
	PerKiloXP float64
	// PerUnlock is the value of each quest a candidate completely unblocks.
	PerUnlock float64
}

// NeedsSettings are the engine's knobs (plan 3.9), minus the ones that only make sense in a running fleet.
type NeedsSettings struct {
	// MaxRisk is the highest risk class a stint may have (default R1: stage5NeedsMaxRisk).
	MaxRisk Risk
	// Allow is the allow list, matched against a trainer's id and its skill. nil allows every R0 trainer
	// (stage5NeedsSkills); a list allows exactly its entries, inside MaxRisk.
	Allow map[string]bool
	// MinStint and MaxStint bound a stint; the planner sizes it to the need's ETA between them.
	MinStint, MaxStint time.Duration
	// ShareLeft is the stint time the account's duty cycle still allows (the rolling 24-hour ledger, lane N3). Negative is
	// unlimited, zero is spent.
	ShareLeft time.Duration
	// MuleHop lets an account on another world ask the mule by hopping to it (lane N5, decision D8).
	MuleHop bool
	// QuestSlot is the base time a quest costs when LadderFacts gives it no estimate.
	QuestSlot time.Duration
	Weights   NeedsWeights
	// MaxDeferrals bounds how often a quest below its advice is passed over for one that is ready (ladder.go's maxDeferrals).
	MaxDeferrals int
}

// DefaultNeedsSettings are the defaults of plan 3.9: R1 at most, the R0 trainers allowed, stints of 20 to 120 minutes,
// no share limit (the caller sets one), no mule hop.
func DefaultNeedsSettings() NeedsSettings {
	return NeedsSettings{MaxRisk: R1, MinStint: 20 * time.Minute, MaxStint: 120 * time.Minute, ShareLeft: -1,
		QuestSlot: 20 * time.Minute, Weights: NeedsWeights{PerQP: 10, PerKiloXP: 1, PerUnlock: 5}, MaxDeferrals: maxDeferrals}
}

// allows reports whether the settings let a stint of rung trainer t run, and why not.
func (s NeedsSettings) allows(t TrainerSpec) (bool, string) {
	if t.Risk > s.MaxRisk {
		return false, fmt.Sprintf("trainer %s is %s and the ceiling is %s", t.ID, t.Risk, s.MaxRisk)
	}
	if s.Allow == nil {
		if t.Risk != R0 {
			return false, fmt.Sprintf("trainer %s is %s and the default allow list is the R0 trainers", t.ID, t.Risk)
		}
		return true, ""
	}
	if s.Allow[t.ID] || s.Allow[t.Skill] {
		return true, ""
	}
	return false, fmt.Sprintf("trainer %s is not in the allow list", t.ID)
}

// FleetState is what the rest of the fleet is doing now: the planner's view of contention, and nothing else. SiteBoard.State
// fills the site and slot counts; the quests' slots (questrunner's) are the caller's to add.
type FleetState struct {
	// SlotBusy are the quests whose fleet-wide slot is full (questrunner's exclusive quests).
	SlotBusy map[string]bool
	// SiteLoad is the accounts at each trainer site now (by TrainerSite.Key), SlotLoad the accounts in each trainer slot.
	SiteLoad map[string]int
	SlotLoad map[string]int
}

// CandKind is what a candidate is.
type CandKind string

const (
	CandQuest CandKind = "quest"
	CandStint CandKind = "stint"
)

// Stint is one run of a trainer the planner wants: a rung factory to run for Minutes, ending at ToLevel at most.
type Stint struct {
	Trainer string
	Skill   string
	// FromLevel is the account's level now and ToLevel the level the stint ends at.
	FromLevel, ToLevel int
	// Hours is the ETA of the whole leg at the trainer's rate; Minutes is the stint, sized to it between MinStint and
	// MaxStint (and the share left).
	Hours   float64
	Minutes int
	// Rung is the named ladder rung that trains exactly to ToLevel, else the generic id lane N2 gives a factory.
	Rung string
	Risk Risk
	// Site is the least loaded site with room ("" when the trainer has none).
	Site string
}

// Candidate is something the planner might return.
type Candidate struct {
	Kind CandKind
	// Key is "quest:<id>" or "stint:<trainer>:<skill>:<to>".
	Key string
	// Quest is the quest a quest candidate runs, or the first quest (in the owner's order) a stint serves.
	Quest string
	// Helps are the quests a stint serves, in the owner's order.
	Helps []string
	Stint *Stint
	// Value, Cost and Score rank it: Score is value over cost.
	Value, Cost, Score float64
	// Order is the owner's-order index of the first quest it serves (the tie-break).
	Order  int
	Reason string
	// Eligible is whether it can run now, and Why when it cannot.
	Eligible bool
	Why      string
}

// NeedStatus is what became of one unmet need.
type NeedStatus string

const (
	// NeedStint: a stint of a rung will raise it (a candidate exists).
	NeedStint NeedStatus = "stint"
	// NeedLoop: a loop the fleet already runs raises it; the planner waits.
	NeedLoop NeedStatus = "loop"
	// NeedWaitQuest: a prerequisite quest (scheduled before) or the quests that pay quest points come first.
	NeedWaitQuest NeedStatus = "quest"
	// NeedSettings: a trainer covers it but the settings do not allow it.
	NeedSettings NeedStatus = "settings"
	// NeedUnreachable: nothing covers it: a named gap.
	NeedUnreachable NeedStatus = "unreachable"
	// NeedFar: the item is on a mule on another world and the account may not hop (lane N5).
	NeedFar NeedStatus = "far"
	// NeedProvision: an item the provisioner gets (the bank, the mule, a spawn, a shop or a skill source) with no training needed. In a
	// plan it is also an item only the designated producer makes (ProducerExclusive): the account asks the mule and waits.
	NeedProvision NeedStatus = "provision"
)

// Blocked is a quest the account cannot run yet and each reason why.
type Blocked struct {
	Quest string
	Needs []BlockedNeed
}

// BlockedNeed is one unmet hard need of a blocked quest and what the planner made of it.
type BlockedNeed struct {
	Need   Need
	Status NeedStatus
	// Detail names the trainer or loop, or says why not.
	Detail string
}

// Unreachable is a need with no trainer: counted, named, not planned for.
type Unreachable struct {
	Quest string
	Need  Need
	// Level is the first level that cannot be trained through (the account's own when no row covers it).
	Level  int
	Gap    NeedGap
	HasGap bool
	Reason string
}

// NeedsPlan is the planner's answer.
type NeedsPlan struct {
	// Next is the best eligible candidate of either kind, NextStint the best eligible stint (the engine hook's question
	// when the ladder has no quest ready). Nil when there is none.
	Next, NextStint *Candidate
	// Candidates are all of them, ranked.
	Candidates  []Candidate
	Blocked     []Blocked
	Unreachable []Unreachable
	// Unjudged are needs the holdings were not read for.
	Unjudged []Need
	// Deferrals is the subject's deferral counts after this answer, for the caller to keep. It is nil when nothing was deferred this
	// time: the counts the caller already has stand.
	Deferrals map[string]int
	Warnings  []string
	// Done is true when no quest of the order is left to do.
	Done bool
	// Now is the time the plan was made for.
	Now time.Time
}

// qinfo is one scheduled quest as the planner holds it.
type qinfo struct {
	id      string
	idx     int
	facts   *RungFacts
	hard    []Need
	advised []Need
	// value is questValue once the totals are known.
	value float64
}

type planner struct {
	sub   Subject
	table NeedTable
	cat   Catalogue
	fleet FleetState
	set   NeedsSettings
	now   time.Time

	plan  NeedsPlan
	qs    []*qinfo
	byID  map[string]*qinfo
	cands map[string]*Candidate
	// remaining is the XP still wanted per skill by the highest hard skill need among the quests left.
	remaining map[string]int
	// unlockedBy counts, per quest, the quests whose only hard unmet need is that quest.
	unlockedBy map[string]int
}

// PlanNeeds is the planner. order is the owner's order of quest ids (training rung ids in it are ignored: they are
// acquirers, not quests); now is the time the plan is made for.
func PlanNeeds(sub Subject, order []string, table NeedTable, cat Catalogue, fleet FleetState, set NeedsSettings, now time.Time) NeedsPlan {
	if set.MaxDeferrals <= 0 {
		set.MaxDeferrals = maxDeferrals
	}
	p := &planner{sub: sub, table: table, cat: cat, fleet: fleet, set: set, now: now,
		byID: map[string]*qinfo{}, cands: map[string]*Candidate{}, remaining: map[string]int{}, unlockedBy: map[string]int{}}
	p.plan.Now = now
	p.schedule(order)
	p.assess()
	p.buildCandidates()
	p.rank()
	return p.plan
}

// schedule is step 1: the owner's order, prerequisites first, each quest once.
func (p *planner) schedule(order []string) {
	seen := map[string]bool{}
	onPath := map[string]bool{}
	var visit func(id string, depth int)
	visit = func(id string, depth int) {
		if seen[id] || p.sub.Done[id] || p.table.rungs[id] {
			return
		}
		if _, ok := p.table.facts[id]; !ok {
			seen[id] = true
			p.plan.Warnings = append(p.plan.Warnings, "no facts for "+id+"; skipped")
			return
		}
		if depth > 8 || onPath[id] {
			return
		}
		onPath[id] = true
		for _, n := range p.table.needs[id] {
			if n.Kind == NeedQuest && !p.sub.Done[n.Prereq] && !p.waived(n) {
				visit(n.Prereq, depth+1)
			}
		}
		onPath[id] = false
		if seen[id] {
			return
		}
		seen[id] = true
		q := &qinfo{id: id, idx: len(p.qs), facts: p.table.factPtr[id]}
		p.qs = append(p.qs, q)
		p.byID[id] = q
	}
	for _, id := range order {
		visit(id, 0)
	}
	p.plan.Done = len(p.qs) == 0
}

// waived reports whether an owned item waives a need that the holdings were read for.
func (p *planner) waived(n Need) bool {
	if n.WaiverItem <= 0 || !p.sub.OwnKnown {
		return false
	}
	own, mule := p.sub.holds(n.WaiverItem, p.sub.muleReachable(p.set.MuleHop))
	return own+mule >= n.WaiverQty
}

// assess is step 2: each scheduled quest's unmet needs, and the totals the valuation reads.
func (p *planner) assess() {
	maxLevel := map[string]int{}
	for _, q := range p.qs {
		q := q
		p.table.unmetInto(p.sub, p.set, q.id, func(n Need) {
			if n.hard() {
				q.hard = append(q.hard, n)
				if n.Kind == NeedSkill {
					maxLevel[n.Skill] = max(maxLevel[n.Skill], n.Level)
				}
			} else {
				q.advised = append(q.advised, n)
			}
		}, func(n Need) { p.plan.Unjudged = append(p.plan.Unjudged, n) })
		if n, ok := p.parkedNeed(q); ok {
			q.hard = append(q.hard, n)
			maxLevel[n.Skill] = max(maxLevel[n.Skill], n.Level)
		}
	}
	for skill, level := range maxLevel {
		p.remaining[skill] = p.sub.XPFor(skill, level)
	}
	for _, q := range p.qs {
		if len(q.hard) == 1 && q.hard[0].Kind == NeedQuest {
			p.unlockedBy[q.hard[0].Prereq]++
		}
		q.value = p.questValue(q)
	}
}

// parkedNeed is the skill need a quest parked on a train need waits for (ParseNeedsPark), when the account is still below the level and
// no hard need of the quest already names it: the provisioner found a kit line whose only source left is a skill source the account is
// below, and the stint that raises it is this planner's to plan.
func (p *planner) parkedNeed(q *qinfo) (Need, bool) {
	reason, ok := p.sub.ParkedNeeds[q.id]
	if !ok {
		return Need{}, false
	}
	skill, level, ok := ParseNeedsPark(reason)
	if !ok || p.sub.Level(skill) >= level {
		return Need{}, false
	}
	for _, n := range q.hard {
		if n.Kind == NeedSkill && n.Skill == skill && n.Level >= level {
			return Need{}, false
		}
	}
	return Need{Quest: q.id, Kind: NeedSkill, Strength: StrengthHard, Skill: skill, Level: level, Origins: []Origin{OriginKit},
		Note: "the quest is parked waiting for a stint (" + NeedsParkCode(skill, level) + ")"}, true
}

// questValue is what finishing the quest is worth: its quest points, and the part of its XP reward that fills a skill need
// still open somewhere in the ladder.
func (p *planner) questValue(q *qinfo) float64 {
	w := p.set.Weights
	v := w.PerQP * float64(q.facts.QP)
	skills := make([]string, 0, len(q.facts.XP))
	for skill := range q.facts.XP {
		skills = append(skills, skill)
	}
	sort.Strings(skills)
	for _, skill := range skills {
		v += w.PerKiloXP * float64(min(q.facts.XP[skill], p.remaining[skill])) / 1000
	}
	return v
}

// slotHours is what the quest costs in time: its slot (LadderFacts' estimate plus a quarter, at least the base slot).
func (p *planner) slotHours(id string) float64 {
	base := p.set.QuestSlot
	if base <= 0 {
		base = 20 * time.Minute
	}
	return QuestSlot(id, base).Hours()
}

// buildCandidates is steps 3 and 4: what to do about each quest.
func (p *planner) buildCandidates() {
	var keys []string
	for _, q := range p.qs {
		if len(q.hard) == 0 {
			p.addQuestCandidate(q)
			continue
		}
		b := Blocked{Quest: q.id}
		for _, n := range q.hard {
			b.Needs = append(b.Needs, p.resolve(q, n, &keys))
		}
		p.plan.Blocked = append(p.plan.Blocked, b)
	}
	// A stint helps every quest that still has a hard skill need above the account's level on its skill: all of it when the
	// stint reaches the need, the fraction of the XP it covers when it is one leg of several.
	for _, key := range keys {
		c := p.cands[key]
		st := c.Stint
		for _, q := range p.qs {
			if p.stintShare(q, st) > 0 {
				c.Helps = append(c.Helps, q.id)
			}
		}
		if len(c.Helps) > 0 {
			c.Quest = c.Helps[0]
			c.Order = p.byID[c.Helps[0]].idx
		}
		p.valueStint(c)
	}
}

// stintShare is the fraction of quest q's hard need on the stint's skill that the stint covers: 1 when it reaches the
// level, the XP share when it is one leg, 0 when the quest has no such need.
func (p *planner) stintShare(q *qinfo, st *Stint) float64 {
	best := 0.0
	for _, n := range q.hard {
		if n.Kind != NeedSkill || n.Skill != st.Skill || n.Level <= st.FromLevel {
			continue
		}
		xp := p.sub.XP[st.Skill]
		want := levels.XPAtLevel(n.Level) - xp
		got := levels.XPAtLevel(st.ToLevel) - xp
		if want <= 0 || got <= 0 {
			continue
		}
		best = math.Max(best, math.Min(1, float64(got)/float64(want)))
	}
	return best
}

func (p *planner) addQuestCandidate(q *qinfo) {
	c := &Candidate{Kind: CandQuest, Key: "quest:" + q.id, Quest: q.id, Order: q.idx, Helps: []string{q.id}}
	c.Value = q.value + p.set.Weights.PerUnlock*float64(p.unlockedBy[q.id])
	c.Cost = p.slotHours(q.id)
	c.Reason = "ready: " + q.facts.Name
	c.Eligible = true
	if why := RungHeld(q.id); why != "" {
		// The ladder will not start it (Dragon Slayer): answering with it would be an answer the ladder refuses, for ever.
		c.Eligible, c.Why = false, "held back: "+why
	} else if p.fleet.SlotBusy[q.id] {
		c.Eligible, c.Why = false, "its fleet slot is full"
	} else if until, ok := p.sub.Parked[q.id]; ok && until.After(p.now) {
		c.Eligible, c.Why = false, "parked until "+until.Format("15:04")
	}
	if len(q.advised) > 0 {
		c.Reason += "; below advice: " + describeNeeds(q.advised)
	}
	p.cands[c.Key] = c
}

// resolve decides what to do about one hard unmet need of a blocked quest.
func (p *planner) resolve(q *qinfo, n Need, keys *[]string) BlockedNeed {
	switch n.Kind {
	case NeedQuest:
		if _, ok := p.table.facts[n.Prereq]; !ok {
			p.noteUnreachable(Unreachable{Quest: q.id, Need: n, Reason: "prerequisite quest " + n.Prereq + " has no facts"})
			return BlockedNeed{Need: n, Status: NeedUnreachable, Detail: "no facts for " + n.Prereq}
		}
		return BlockedNeed{Need: n, Status: NeedWaitQuest, Detail: "runs after " + n.Prereq}
	case NeedQuestPoints:
		reach := p.sub.QP
		for _, other := range p.qs {
			if other.id != q.id {
				reach += other.facts.QP
			}
		}
		if reach < n.Level {
			p.noteUnreachable(Unreachable{Quest: q.id, Need: n, Reason: fmt.Sprintf("needs %d quest points and the ladder's quests reach only %d", n.Level, reach)})
			return BlockedNeed{Need: n, Status: NeedUnreachable, Detail: fmt.Sprintf("the ladder reaches only %d", reach)}
		}
		return BlockedNeed{Need: n, Status: NeedWaitQuest, Detail: "runs after the quests that pay quest points"}
	case NeedCombat:
		return BlockedNeed{Need: n, Status: NeedLoop, Detail: "stage5-combat raises combat levels"}
	case NeedItem:
		j := JudgeItem(p.sub, p.set, p.itemFor(q.id, n))
		switch j.State {
		case ItemMuleFar:
			return BlockedNeed{Need: n, Status: NeedFar, Detail: j.Reason + " (lane N5)"}
		case ItemProducer:
			// Not a gap: the producer is the trainer. The account asks the mule, trains nothing, and the quest waits for the stock.
			return BlockedNeed{Need: n, Status: NeedProvision, Detail: "ask the mule; " + j.Reason}
		}
		p.noteUnreachable(Unreachable{Quest: q.id, Need: n, Reason: j.Reason})
		return BlockedNeed{Need: n, Status: NeedUnreachable, Detail: j.Reason}
	}
	return p.resolveSkill(q, n, keys)
}

// itemFor is the table's item need for a NeedItem of quest id.
func (p *planner) itemFor(id string, n Need) ItemNeed {
	for _, in := range p.table.items[id] {
		if in.Item == n.ItemID && in.Qty == n.Qty {
			return in
		}
	}
	return ItemNeed{Item: n.ItemID, Name: n.ItemName, Qty: n.Qty, Gap: "no chain is known for it"}
}

func (p *planner) resolveSkill(q *qinfo, n Need, keys *[]string) BlockedNeed {
	cur := p.sub.Level(n.Skill)
	legs, uncovered := p.cat.Path(n.Skill, cur, n.Level, nil)
	if uncovered != 0 {
		u := Unreachable{Quest: q.id, Need: n, Level: uncovered,
			Reason: fmt.Sprintf("no trainer for %s %d", n.Skill, n.Level)}
		u.Gap, u.HasGap = GapFor(n.Skill, uncovered)
		p.noteUnreachable(u)
		detail := fmt.Sprintf("no trainer for %s %d (no row covers level %d)", n.Skill, n.Level, uncovered)
		if u.HasGap {
			detail += "; lane " + u.Gap.Lane
		}
		return BlockedNeed{Need: n, Status: NeedUnreachable, Detail: detail}
	}
	if len(legs) == 0 {
		return BlockedNeed{Need: n, Status: NeedLoop, Detail: "the level is already reached"}
	}
	if legs[0].Trainer.Via == ViaLoop {
		return BlockedNeed{Need: n, Status: NeedLoop, Detail: legs[0].Trainer.ID + " (" + legs[0].Trainer.Loop + ") trains it"}
	}
	t, why, ok := p.pickRung(n.Skill, cur, n.Level)
	if !ok {
		return BlockedNeed{Need: n, Status: NeedSettings, Detail: why}
	}
	st := p.stintFor(t, cur, n.Level)
	key := fmt.Sprintf("stint:%s:%s:%d", t.ID, st.Skill, st.ToLevel)
	if _, dup := p.cands[key]; !dup {
		c := &Candidate{Kind: CandStint, Key: key, Stint: &st,
			Reason: fmt.Sprintf("%s %d to %d with %s", skillLabel(st.Skill), st.FromLevel, st.ToLevel, t.ID)}
		p.cands[key] = c
		*keys = append(*keys, key)
	}
	return BlockedNeed{Need: n, Status: NeedStint, Detail: fmt.Sprintf("%s to %d (%d min)", t.ID, st.ToLevel, st.Minutes)}
}

// pickRung is step 3 for a rung: the permitted row for an account at level `cur` heading for `want`, by rank, risk, hours
// and then contention. why says what blocked the rows that cover the level when none is permitted.
func (p *planner) pickRung(skill string, cur, want int) (TrainerSpec, string, bool) {
	var rows []TrainerSpec
	var refused []string
	for _, t := range p.cat.For(skill, cur) {
		if t.Via != ViaRung {
			continue
		}
		if ok, why := p.set.allows(t); !ok {
			refused = append(refused, why)
			continue
		}
		rows = append(rows, t)
	}
	if len(rows) == 0 {
		why := "no rung is permitted for " + skill
		if len(refused) > 0 {
			why = strings.Join(refused, "; ")
		}
		return TrainerSpec{}, why, false
	}
	xp := p.sub.XP[skill]
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		if a.Risk != b.Risk {
			return a.Risk < b.Risk
		}
		if ha, hb := a.HoursBetween(xp, min(a.To, want)), b.HoursBetween(xp, min(b.To, want)); ha != hb {
			return ha < hb
		}
		if la, lb := p.load(a), p.load(b); la != lb {
			return la < lb
		}
		return a.ID < b.ID
	})
	// The first row with room; when every row is full the best one stays the pick and its candidate says so.
	for _, t := range rows {
		if _, ok := p.freeSite(t); ok {
			return t, "", true
		}
	}
	return rows[0], "", true
}

// load is a trainer's contention: the load over the capacity of its sites and its fleet slot.
func (p *planner) load(t TrainerSpec) float64 {
	used, cap := 0, 0
	for _, s := range t.Sites {
		used += p.fleet.SiteLoad[s.Key]
		cap += s.Capacity
	}
	l := 0.0
	if cap > 0 {
		l = float64(used) / float64(cap)
	}
	if t.SlotCap > 0 {
		l = math.Max(l, float64(p.fleet.SlotLoad[t.Slot])/float64(t.SlotCap))
	}
	return l
}

// freeSite is the least loaded site with room, and whether the trainer has room at all (a trainer with no site has).
func (p *planner) freeSite(t TrainerSpec) (string, bool) {
	if t.SlotCap > 0 && p.fleet.SlotLoad[t.Slot] >= t.SlotCap {
		return "", false
	}
	if len(t.Sites) == 0 {
		return "", true
	}
	best, bestLoad := "", 2.0
	for _, s := range t.Sites {
		used := p.fleet.SiteLoad[s.Key]
		if used >= s.Capacity {
			continue
		}
		if l := float64(used) / float64(s.Capacity); l < bestLoad {
			best, bestLoad = s.Key, l
		}
	}
	return best, best != ""
}

// stintFor sizes a stint of rung trainer t for an account at level cur wanting `want`.
func (p *planner) stintFor(t TrainerSpec, cur, want int) Stint {
	to := min(t.To, want)
	xp := p.sub.XP[t.Skill]
	hours := t.HoursBetween(xp, to)
	site, _ := p.freeSite(t)
	minutes := int(math.Ceil(hours*60/5) * 5)
	minutes = max(minutes, int(p.set.MinStint/time.Minute))
	if t.MinStint > 0 {
		minutes = max(minutes, int(t.MinStint/time.Minute))
	}
	cap := int(p.set.MaxStint / time.Minute)
	if t.MaxStint > 0 {
		cap = min(cap, int(t.MaxStint/time.Minute))
	}
	minutes = min(minutes, cap)
	if p.set.ShareLeft >= 0 {
		minutes = min(minutes, int(p.set.ShareLeft/time.Minute))
	}
	rung, ok := t.RungFor(to)
	if !ok {
		rung = t.GenericRungID(to)
	}
	return Stint{Trainer: t.ID, Skill: t.Skill, FromLevel: cur, ToLevel: to, Hours: hours, Minutes: minutes, Rung: rung, Risk: t.Risk, Site: site}
}

// valueStint is step 4 for a stint: each quest's share of its value (split over its hard needs) times how much of its need the
// stint covers, plus a bonus for each quest it frees completely; over the stint's hours and the first quest's slot. Then its
// eligibility.
func (p *planner) valueStint(c *Candidate) {
	st := c.Stint
	var v float64
	for _, id := range c.Helps {
		q := p.byID[id]
		v += q.value / float64(max(len(q.hard), 1)) * p.stintShare(q, st)
		free := true
		for _, n := range q.hard {
			if !(n.Kind == NeedSkill && n.Skill == st.Skill && n.Level <= st.ToLevel) {
				free = false
				break
			}
		}
		if free {
			v += p.set.Weights.PerUnlock
		}
	}
	c.Value = v
	c.Cost = float64(st.Minutes)/60 + p.slotHours(c.Quest)
	t, _ := p.cat.ByID(st.Trainer)
	c.Eligible, c.Why = p.stintEligible(t, st)
}

// stintEligible is step 5 for a stint.
func (p *planner) stintEligible(t TrainerSpec, st *Stint) (bool, string) {
	if h, ok := p.sub.Needs[StintKey(st.Skill)]; ok && h.ParkedUntil.After(p.now) {
		return false, fmt.Sprintf("%s stints are parked until %s after %d failures", st.Skill, h.ParkedUntil.Format("15:04"), h.Fails)
	}
	if t.Risk >= R1 {
		switch {
		case p.sub.DeathsToday > 0:
			return false, "an " + t.Risk.String() + " stint after a death today"
		case p.sub.Deaths7d > 1:
			return false, fmt.Sprintf("an %s stint after %d deaths in 7 days (one is the most)", t.Risk, p.sub.Deaths7d)
		case !p.sub.HasFood:
			return false, "an " + t.Risk.String() + " stint with no food in the pack"
		}
	}
	if _, ok := p.freeSite(t); !ok {
		return false, "every site of " + t.ID + " is at capacity"
	}
	if least := max(p.set.MinStint, t.MinStint); p.set.ShareLeft >= 0 && p.set.ShareLeft < least {
		return false, fmt.Sprintf("the share is spent (%s left, a stint is at least %s)", p.set.ShareLeft.Round(time.Minute), least)
	}
	if why := p.inputsShort(t); why != "" {
		return false, why
	}
	return true, ""
}

// inputsShort says which consumable input of the trainer the account lacks, or "". An item the account may fetch itself
// (Gathers) and one whose holdings were not read are not judged.
func (p *planner) inputsShort(t TrainerSpec) string {
	if !p.sub.OwnKnown {
		return ""
	}
	reach := p.sub.muleReachable(p.set.MuleHop)
	for _, in := range t.Inputs {
		if in.Tool || in.Gathers {
			continue
		}
		have := 0
		for _, id := range in.IDs {
			own, mule := p.sub.holds(id, reach)
			have += own + mule
		}
		if have < in.Qty {
			return fmt.Sprintf("%s wants %d %s and the account and its mule hold %d", t.ID, in.Qty, strings.ToLower(in.Name), have)
		}
	}
	return ""
}

func (p *planner) noteUnreachable(u Unreachable) {
	p.plan.Unreachable = append(p.plan.Unreachable, u)
}

// rank is steps 4 and 5: order the candidates and pick.
func (p *planner) rank() {
	keys := make([]string, 0, len(p.cands))
	for k := range p.cands {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var cs []Candidate
	for _, k := range keys {
		c := *p.cands[k]
		c.Score = c.Value / math.Max(c.Cost, 0.05)
		cs = append(cs, c)
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Score != cs[j].Score {
			return cs[i].Score > cs[j].Score
		}
		if cs[i].Order != cs[j].Order {
			return cs[i].Order < cs[j].Order
		}
		return cs[i].Key < cs[j].Key
	})
	p.plan.Candidates = cs
	p.deferBelowAdvice()
	for i := range p.plan.Candidates {
		c := &p.plan.Candidates[i]
		if !c.Eligible {
			continue
		}
		if p.plan.Next == nil {
			p.plan.Next = c
		}
		if c.Kind == CandStint && p.plan.NextStint == nil {
			p.plan.NextStint = c
		}
	}
}

// deferBelowAdvice is PlanLadder's practical deferral: a ready quest below the study's advice (an advised level or combat) is
// passed over, at most MaxDeferrals times, while another quest that is ready and not below its advice is eligible.
// Each pass-over is counted in the plan's Deferrals, which the caller keeps; at the bound the quest competes as it is.
func (p *planner) deferBelowAdvice() {
	var clean bool
	for _, c := range p.plan.Candidates {
		if c.Kind == CandQuest && c.Eligible && len(p.byID[c.Quest].advised) == 0 {
			clean = true
			break
		}
	}
	if !clean {
		return
	}
	for i := range p.plan.Candidates {
		c := &p.plan.Candidates[i]
		if c.Kind != CandQuest || !c.Eligible {
			continue
		}
		q := p.byID[c.Quest]
		if len(q.advised) == 0 || p.sub.Deferrals[q.id] >= p.set.MaxDeferrals {
			continue
		}
		if p.plan.Deferrals == nil {
			p.plan.Deferrals = map[string]int{}
			for k, v := range p.sub.Deferrals {
				p.plan.Deferrals[k] = v
			}
		}
		p.plan.Deferrals[q.id]++
		c.Eligible = false
		c.Why = fmt.Sprintf("deferred, below advice (%s), %d of %d", describeNeeds(q.advised), p.plan.Deferrals[q.id], p.set.MaxDeferrals)
	}
}

func describeNeeds(ns []Need) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, n.Describe())
	}
	return strings.Join(parts, ", ")
}

// StintKey is the key a stint's history is kept under in Subject.Needs: the skill it trains, so a failing Mining stint
// parks Mining and not each level separately.
func StintKey(skill string) string { return "train:" + strings.ToLower(skill) }

// MaxStintFails is how many failed stints in a row park their skill.
const MaxStintFails = 3

// NoteStint is the new history of a skill's stints after one ended. A success clears it. A failure counts; MaxStintFails of
// them in a row park the skill on the side store's backoff (Backoff: 5, 10, 20, 40 minutes, then 2 hours). A death parks it
// for a day at once. The caller (lane N3) persists the result in ScarabSwarmNeeds.json.
func NoteStint(h NeedHistory, ok, died bool, now time.Time) NeedHistory {
	switch {
	case died:
		h.Fails = 0
		h.Parks++
		h.DiedAt = now
		h.ParkedUntil = now.Add(24 * time.Hour)
	case ok:
		h = NeedHistory{DiedAt: h.DiedAt}
	default:
		h.Fails++
		if h.Fails >= MaxStintFails {
			h.Parks++
			h.Fails = 0
			h.ParkedUntil = now.Add(Backoff(h.Parks))
		}
	}
	return h
}

// LogLines are the plan's decisions as the engine logs them (plan 3.9): one line for the next action with what was deferred and why,
// and one for each need with no trainer.
func (pl NeedsPlan) LogLines() []string {
	var lines []string
	switch {
	case pl.Done:
		lines = append(lines, "[needs] next: none; every quest of the order is done")
	case pl.Next == nil:
		lines = append(lines, "[needs] next: none; "+pl.deferredSummary())
	default:
		next := "quest " + pl.Next.Quest
		if st := pl.Next.Stint; st != nil {
			next = fmt.Sprintf("stint %s %d->%d with %s (rung %s, %d min)", st.Skill, st.FromLevel, st.ToLevel, st.Trainer, st.Rung, st.Minutes)
		}
		line := fmt.Sprintf("[needs] next: %s for %s (%s)", next, strings.Join(pl.Next.Helps, ", "), pl.Next.Reason)
		if d := pl.deferredSummary(); d != "" {
			line += "; " + d
		}
		lines = append(lines, line)
	}
	seen := map[string]bool{}
	for _, u := range pl.Unreachable {
		var line string
		switch u.Need.Kind {
		case NeedSkill:
			line = fmt.Sprintf("[needs] unreachable: no trainer for %s %d", u.Need.Skill, u.Need.Level)
			if u.HasGap {
				line += " (lane " + u.Gap.Lane + ")"
			}
		default:
			line = "[needs] unreachable: " + u.Need.Describe() + ": " + u.Reason
		}
		if !seen[line] {
			seen[line] = true
			lines = append(lines, line)
		}
	}
	return lines
}

// deferredSummary is "deferred: ..." for the blocked quests and the candidates that could not run, at most six.
func (pl NeedsPlan) deferredSummary() string {
	var parts []string
	for _, c := range pl.Candidates {
		if !c.Eligible && c.Why != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", c.Reason, c.Why))
		}
	}
	for _, b := range pl.Blocked {
		var why []string
		for _, n := range b.Needs {
			if n.Status == NeedSettings || n.Status == NeedFar || n.Status == NeedUnreachable || n.Status == NeedProvision {
				why = append(why, n.Need.Describe()+": "+n.Detail)
			}
		}
		if len(why) > 0 {
			parts = append(parts, b.Quest+" ("+strings.Join(why, "; ")+")")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 6 {
		parts = append(parts[:6], fmt.Sprintf("and %d more", len(parts)-6))
	}
	return "deferred: " + strings.Join(parts, ", ")
}

// UnreachableCount is the number of distinct (skill, level) needs with no trainer, for a gauge.
func (pl NeedsPlan) UnreachableCount() int {
	seen := map[string]bool{}
	for _, u := range pl.Unreachable {
		seen[u.Need.Key()] = true
	}
	return len(seen)
}
