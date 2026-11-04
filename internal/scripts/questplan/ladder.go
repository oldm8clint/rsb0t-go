package questplan

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/levels"
)

// The quest ladder's order optimiser: a pure planner over each rung's requirements and rewards
// (ladderfacts.go, one row per study in private evidence notes and the earlier
// quest-chain studies). It starts from the owner's order and changes it only where the facts
// force it:
//
//   - a HARD blocker on the next rung (a prerequisite quest, a server level gate, a quest-point
//     minimum, a level an item needs when no copy of the item is owned) is resolved in place:
//     the missing prerequisite quest is pulled forward, a training stop is inserted for the
//     level, or a quest-point filler (Goblin Diplomacy, Romeo & Juliet) is inserted;
//   - a PRACTICAL shortfall (the study's advised combat level or an advised level such as
//     Protect from Missiles) defers the rung behind later rungs that are ready, a bounded number
//     of times, because later quests raise combat through their own XP; if nothing later is
//     ready it runs anyway, with a warning.
//
// Rewards are applied as the plan goes, so a quest that pays Agility XP (The Grand Tree) makes
// the training stop after it shorter, and the Prayer quests before Scorpion Catcher are counted
// before a Prayer stop is inserted. XP is 1x (content's stat_advance / 10); a lab run at 10x
// reaches every level sooner, which only ever removes stops, never adds one.

// GateKind is how binding a level gate is.
type GateKind string

const (
	// GateServer is enforced by content: the quest cannot progress below it.
	GateServer GateKind = "server"
	// GateItem is a level an item needs (nails need Smithing 34), waived when the account or
	// the mule already owns WaiverQty of WaiverItem.
	GateItem GateKind = "item"
	// GateAdvised is the study's advice (a fight, a prayer): it defers, never inserts training.
	GateAdvised GateKind = "advised"
	// GateOwner is the owner's own rule (Waterfall at 20 Hitpoints).
	GateOwner GateKind = "owner"
	// GateModule is a level the ported module itself waits for (a fight it will not start
	// below it, such as Horror's Protect from Missiles). It inserts training like a server
	// gate: an advised gate there would livelock the ladder, the module waiting for a level
	// no rung produces.
	GateModule GateKind = "module"
)

// Gate is one level requirement.
type Gate struct {
	Skill     string
	Level     int
	Kind      GateKind
	Boostable bool
	// WaiverItem/WaiverQty waive a GateItem when owned.
	WaiverItem, WaiverQty int
	Note                  string
}

// RungFacts is one rung's requirements and rewards.
type RungFacts struct {
	ID, Name string
	// Train, for a training rung, is the skill and level it trains to; a quest leaves it empty.
	TrainSkill string
	TrainLevel int
	// Prereqs are rung ids that must be complete first (hard).
	Prereqs []string
	// SoftPrereqs are prerequisites an owned item waives (Priest in Peril needs Rune Mysteries
	// only to mine the 50 essence the account does not already own).
	SoftPrereqs []SoftPrereq
	Gates       []Gate
	// QPNeeded is the quest-point minimum (hard); QP is the reward.
	QPNeeded, QP int
	// XP is the 1x reward by lower-case skill.
	XP map[string]int
	// MinCombat is the study's advised combat level (practical).
	MinCombat int
	// Filler marks a quest run only to make up quest points.
	Filler bool
	// Finale is never pulled ahead of another rung by a deferral (the owner's Dragon Slayer
	// comes last whatever else is waiting).
	Finale bool
	// Region and EstMinutes are for the report.
	Region     string
	EstMinutes int
	// Source is the study the row comes from.
	Source string
	Notes  string
}

// SoftPrereq is a prerequisite rung that owning WaiverQty of WaiverItem makes unnecessary.
type SoftPrereq struct {
	ID                    string
	WaiverItem, WaiverQty int
	Note                  string
}

// Account is the planner's starting state.
type Account struct {
	// XP is by lower-case skill (1x experience).
	XP   map[string]int
	QP   int
	Done map[string]bool
	// Owned counts items the account or its mule already holds, by object id (item waivers).
	Owned map[int]int
}

// Level is a skill's level from XP (Hitpoints defaults to 10).
func (a Account) Level(skill string) int {
	xp, ok := a.XP[strings.ToLower(skill)]
	if !ok && strings.EqualFold(skill, "hitpoints") {
		return 10
	}
	return levels.LevelFromXP(xp)
}

// Combat is the 2004 combat level formula.
func (a Account) Combat() int {
	def, hp, pray := float64(a.Level("defence")), float64(a.Level("hitpoints")), float64(a.Level("prayer"))
	att, str := float64(a.Level("attack")), float64(a.Level("strength"))
	rng, mag := float64(a.Level("ranged")), float64(a.Level("magic"))
	base := 0.25 * (def + hp + math.Floor(pray/2))
	melee := 0.325 * (att + str)
	ranged := 0.325 * math.Floor(rng*1.5)
	magic := 0.325 * math.Floor(mag*1.5)
	return int(math.Floor(base + math.Max(melee, math.Max(ranged, magic))))
}

func (a Account) clone() Account {
	b := Account{XP: map[string]int{}, QP: a.QP, Done: map[string]bool{}, Owned: a.Owned}
	for k, v := range a.XP {
		b.XP[k] = v
	}
	for k, v := range a.Done {
		b.Done[k] = v
	}
	return b
}

// PlannedRung is one line of the plan.
type PlannedRung struct {
	ID     string
	Reason string
	// Inserted is true for a rung the owner's order did not name (a training stop, a pulled
	// prerequisite, a quest-point filler).
	Inserted bool
	// Combat, QP and Levels are the account after the rung.
	Combat int
	QP     int
	Levels map[string]int
}

// LadderPlan is the optimiser's answer.
type LadderPlan struct {
	Rungs    []PlannedRung
	Warnings []string
	Final    Account
}

// GateRungID is the rung the planner inserts for a hard gate: the named training rung that
// trains its skill to exactly its level, else the generic stop.
func GateRungID(g Gate) string {
	if id := trainingRungFor(LadderFacts, g.Skill, g.Level); id != "" {
		return id
	}
	return TrainRungID(g.Skill, g.Level)
}

// TrainRungID is the id of a generic training stop.
func TrainRungID(skill string, level int) string {
	return fmt.Sprintf("train:%s:%d", strings.ToLower(skill), level)
}

// maxDeferrals bounds how many times a rung may be deferred for a practical shortfall.
const maxDeferrals = 3

// PlanLadder orders the rungs.
func PlanLadder(order []string, facts map[string]RungFacts, start Account) LadderPlan {
	facts = copyFacts(facts)
	acct := start.clone()
	pulled := map[string]bool{}
	var plan LadderPlan
	pending := append([]string(nil), order...)
	deferred := map[string]int{}
	inserted := map[string]bool{}

	schedule := func(id, reason string, ins bool) {
		f := facts[id]
		if f.TrainSkill != "" {
			if need := levels.XPAtLevel(f.TrainLevel); acct.XP[f.TrainSkill] < need {
				acct.XP[f.TrainSkill] = need
			}
		}
		for skill, xp := range f.XP {
			acct.XP[skill] += xp
		}
		acct.QP += f.QP
		acct.Done[id] = true
		lv := map[string]int{}
		for skill := range acct.XP {
			lv[skill] = acct.Level(skill)
		}
		plan.Rungs = append(plan.Rungs, PlannedRung{ID: id, Reason: reason, Inserted: ins,
			Combat: acct.Combat(), QP: acct.QP, Levels: lv})
	}
	remove := func(i int) { pending = append(pending[:i], pending[i+1:]...) }

	for guard := 0; len(pending) > 0 && guard < 500; guard++ {
		// Drop anything already done.
		for i := 0; i < len(pending); i++ {
			if acct.Done[pending[i]] {
				remove(i)
				i--
			}
		}
		if len(pending) == 0 {
			break
		}
		head := pending[0]
		f, known := facts[head]
		if !known {
			plan.Warnings = append(plan.Warnings, "no facts for "+head+"; dropped")
			remove(0)
			continue
		}
		// Hard blockers first, in a fixed order: prerequisites, quest points, levels.
		if pre := firstMissingPrereq(f, acct); pre != "" {
			if _, ok := facts[pre]; !ok {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s needs %s, which has no facts", head, pre))
				remove(0)
				continue
			}
			pending = moveToFront(pending, pre)
			inserted[pre] = inserted[pre] || !inOrder(order, pre)
			pulled[pre] = true
			continue
		}
		if f.QPNeeded > acct.QP {
			filler := nextFiller(facts, acct, pending)
			if filler == "" {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s needs %d quest points and the plan reaches only %d", head, f.QPNeeded, acct.QP))
				remove(0)
				continue
			}
			schedule(filler, fmt.Sprintf("quest-point filler: %s needs %d QP, the account has %d", f.Name, f.QPNeeded, acct.QP), true)
			continue
		}
		if g, ok := firstHardGate(f, acct); ok {
			id := trainingRungFor(facts, g.Skill, g.Level)
			reason := fmt.Sprintf("%s needs %s %d (%s gate%s) and the account has %d",
				f.Name, g.Skill, g.Level, g.Kind, boostNote(g), acct.Level(g.Skill))
			if g.Note != "" {
				reason += ": " + g.Note
			}
			if id == "" {
				id = TrainRungID(g.Skill, g.Level)
				facts[id] = RungFacts{ID: id, Name: fmt.Sprintf("Train %s to %d", g.Skill, g.Level), TrainSkill: strings.ToLower(g.Skill), TrainLevel: g.Level}
			}
			schedule(id, reason, !inOrder(order, id))
			continue
		}
		// Practical: advised combat and advised levels defer, bounded.
		if why := practicalShortfall(f, acct); why != "" && deferred[head] < maxDeferrals {
			if j := firstReady(pending[1:], facts, acct); j >= 0 {
				deferred[head]++
				next := pending[1+j]
				remove(1 + j)
				schedule(next, fmt.Sprintf("pulled ahead of %s, which is deferred (%s)", f.Name, why), inserted[next])
				continue
			}
		}
		reason := "owner's order"
		if pulled[head] {
			reason = "prerequisite pulled forward"
		}
		if why := practicalShortfall(f, acct); why != "" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s runs below its advice: %s", f.Name, why))
			reason += "; below advice: " + why
		}
		schedule(head, reason, inserted[head])
		remove(0)
	}
	plan.Final = acct
	return plan
}

func inOrder(order []string, id string) bool {
	for _, o := range order {
		if o == id {
			return true
		}
	}
	return false
}

func moveToFront(p []string, id string) []string {
	out := []string{id}
	for _, x := range p {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func firstMissingPrereq(f RungFacts, a Account) string {
	for _, p := range f.Prereqs {
		if !a.Done[p] {
			return p
		}
	}
	for _, sp := range f.SoftPrereqs {
		if a.Done[sp.ID] || (sp.WaiverItem > 0 && a.Owned[sp.WaiverItem] >= sp.WaiverQty) {
			continue
		}
		return sp.ID
	}
	return ""
}

func copyFacts(in map[string]RungFacts) map[string]RungFacts {
	out := make(map[string]RungFacts, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstHardGate(f RungFacts, a Account) (Gate, bool) {
	for _, g := range f.Gates {
		switch g.Kind {
		case GateServer, GateOwner, GateModule:
		case GateItem:
			if g.WaiverItem > 0 && a.Owned[g.WaiverItem] >= g.WaiverQty {
				continue
			}
		default:
			continue
		}
		if a.Level(g.Skill) < g.Level {
			return g, true
		}
	}
	return Gate{}, false
}

func practicalShortfall(f RungFacts, a Account) string {
	var why []string
	if f.MinCombat > 0 && a.Combat() < f.MinCombat {
		why = append(why, fmt.Sprintf("combat %d, advised %d", a.Combat(), f.MinCombat))
	}
	for _, g := range f.Gates {
		if g.Kind == GateAdvised && a.Level(g.Skill) < g.Level {
			why = append(why, fmt.Sprintf("%s %d, advised %d", g.Skill, a.Level(g.Skill), g.Level))
		}
	}
	return strings.Join(why, "; ")
}

// firstReady is the index (into rest) of the first QUEST with no blocker of any kind. A
// training stop is never pulled ahead by a deferral: training is placed only where a gate
// needs it, so that the quests before it have paid their XP first (The Grand Tree's Agility
// before the Agility 35 stretch), and a Finale stays last. Nor is a quest-point filler: it has
// no blocker of any kind, so it would always be the first one ready, and the owner's ladder
// (OwnerLadder names Goblin Diplomacy and Romeo & Juliet since W13) would run its fillers
// ahead of every quest it defers for want of combat levels.
func firstReady(rest []string, facts map[string]RungFacts, a Account) int {
	for i, id := range rest {
		f, ok := facts[id]
		if !ok || a.Done[id] || f.TrainSkill != "" || f.Finale || f.Filler {
			continue
		}
		if firstMissingPrereq(f, a) != "" || f.QPNeeded > a.QP {
			continue
		}
		if _, hard := firstHardGate(f, a); hard {
			continue
		}
		if practicalShortfall(f, a) != "" {
			continue
		}
		return i
	}
	return -1
}

// trainingRungFor is the named training rung that trains skill to exactly level, if the facts
// have one. Exactly, not "at least": Nature Spirit's Crafting 18 must not pull the whole
// Crafting 40 flax stretch forward, it gets a generic stop to 18 and Crafting 40 stays where the
// Fremennik Trials need it.
func trainingRungFor(facts map[string]RungFacts, skill string, level int) string {
	for id, f := range facts {
		if strings.EqualFold(f.TrainSkill, skill) && f.TrainLevel == level {
			return id
		}
	}
	return ""
}

// nextFiller is the first not-done filler quest whose own blockers are clear, by QP reward.
func nextFiller(facts map[string]RungFacts, a Account, pending []string) string {
	var ids []string
	for id, f := range facts {
		if f.Filler && !a.Done[id] {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if facts[ids[i]].QP != facts[ids[j]].QP {
			return facts[ids[i]].QP > facts[ids[j]].QP
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		f := facts[id]
		// A filler with a quest-point minimum of its own (Black Knights' Fortress wants 12) is not one the account can take yet
		// when it has fewer: it is the filler for a plan that has already made up the points it wants.
		if f.QPNeeded > a.QP {
			continue
		}
		if firstMissingPrereq(f, a) == "" {
			if _, hard := firstHardGate(f, a); !hard {
				return id
			}
		}
	}
	_ = pending
	return ""
}

func boostNote(g Gate) string {
	if g.Boostable {
		return ", boostable"
	}
	return ""
}

// Describe renders a plan as numbered lines.
func (p LadderPlan) Describe(facts map[string]RungFacts) string {
	var b strings.Builder
	for i, r := range p.Rungs {
		name := facts[r.ID].Name
		if name == "" {
			name = r.ID
		}
		mark := ""
		if r.Inserted {
			mark = " [inserted]"
		}
		fmt.Fprintf(&b, "%2d. %s%s — %s (combat %d, QP %d)\n", i+1, name, mark, r.Reason, r.Combat, r.QP)
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "WARNING: %s\n", w)
	}
	return b.String()
}
