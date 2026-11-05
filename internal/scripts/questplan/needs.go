package questplan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

// THE REQUIREMENT MODEL (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 A).
//
// The owner's ask of 19 Oct 2026 was that every account "get stats and item requirements on their own". Before any
// account can be told what to train, something has to know what each quest wants that this account does not have.
// That knowledge lived in five places that did not agree:
//
//   - LadderFacts: the gates (server, owner, module, item, advised), the prerequisite quests, the quest-point
//     minimum and the study's advised combat level;
//   - Record.Requirements: what CheckRequirements holds a quest back on, which the 14 October 2026 Fremennik
//     failure showed can omit a level LadderFacts states;
//   - SoftPrereqs: a prerequisite that owning an item waives (Priest in Peril's Rune Mysteries);
//   - the skill levels of the kit lines' skill sources (Nature Spirit's silver ore wants Mining 20), which only
//     bind when no bank or mule has the item: they are chain links of an ItemNeed (needsitems.go), not gates;
//   - the module's own floors (Fight Arena will not start below 50 Hitpoints), which a Record cannot state.
//
// BuildNeedTable merges the five into one typed list per quest (Need) and Unmet judges it against one account
// (Subject). It reads BASE levels: the account's XP through levels.LevelFromXP, never a level a client reported.
// A drained level read as a base one is the exact error that made the panel and the checkpoint disagree about
// Prayer 37 on 19 Oct 2026 (259 accounts below it against 3), and 344 of 6,688 panel skill readings that day
// disagreed with their own XP.
//
// WHAT IS NOT JUDGED. An item is judged only from holdings someone read: an unread bank is not an empty one (the
// provisioner's rule, provision.go), so with Subject.OwnKnown false an item, and a need a held item would waive, is
// returned in the unjudged list and never counted unmet.

// NeedKind is what a need is for.
type NeedKind string

const (
	// NeedSkill is a base level in a skill.
	NeedSkill NeedKind = "skill"
	// NeedQuestPoints is a quest-point total.
	NeedQuestPoints NeedKind = "qp"
	// NeedQuest is a prerequisite quest complete.
	NeedQuest NeedKind = "quest"
	// NeedCombat is a combat level.
	NeedCombat NeedKind = "combat"
	// NeedItem is something in the pack (needsitems.go carries the chain that gets it).
	NeedItem NeedKind = "item"
)

// CombatSkill is the pseudo-skill a combat-level need and the trainer rows that raise it are filed under.
const CombatSkill = "combat"

// Strength is how binding a need is.
type Strength string

const (
	// StrengthHard holds the quest back: the server refuses the step, the owner or the module will not start it.
	StrengthHard Strength = "hard"
	// StrengthAdvised is the study's advice (a fight, a prayer): it defers a quest, never inserts training.
	StrengthAdvised Strength = "advised"
)

// Origin is which of the five sources named a need.
type Origin string

const (
	OriginFacts  Origin = "facts"
	OriginRecord Origin = "record"
	OriginSoft   Origin = "soft-prereq"
	OriginKit    Origin = "kit"
	OriginModule Origin = "module"
)

// Need is one requirement of one quest.
type Need struct {
	Quest    string
	Kind     NeedKind
	Strength Strength
	// Skill and Level are a skill need's (Level alone is a quest-point total or a combat level).
	Skill string
	Level int
	// Prereq is the quest a NeedQuest waits for.
	Prereq string
	// ItemID, ItemName and Qty are a NeedItem's.
	ItemID   int
	ItemName string
	Qty      int
	// WaiverItem and WaiverQty: owning that many of that object waives the need (Nature Spirit's Crafting 18 when the
	// silver sickle is already made; Priest in Peril's Rune Mysteries when the 50 essence is already held).
	WaiverItem, WaiverQty int
	// ServesItem is set on a skill need that only exists to produce that object through an item's chain.
	ServesItem int
	Origins    []Origin
	Note       string
}

// Key identifies the need without the quest that wants it: the fleet counts, and an account parks, a need once however
// many quests ask for it.
func (n Need) Key() string {
	switch n.Kind {
	case NeedSkill:
		return fmt.Sprintf("skill:%s:%d", n.Skill, n.Level)
	case NeedCombat:
		return fmt.Sprintf("combat:%d", n.Level)
	case NeedQuestPoints:
		return fmt.Sprintf("qp:%d", n.Level)
	case NeedQuest:
		return "quest:" + n.Prereq
	case NeedItem:
		return fmt.Sprintf("item:%d:%d", n.ItemID, n.Qty)
	}
	return "unknown:" + string(n.Kind)
}

// Describe is the need as a log line reads it.
func (n Need) Describe() string {
	switch n.Kind {
	case NeedSkill:
		return fmt.Sprintf("%s %d", skillLabel(n.Skill), n.Level)
	case NeedCombat:
		return fmt.Sprintf("combat level %d", n.Level)
	case NeedQuestPoints:
		return fmt.Sprintf("%d quest points", n.Level)
	case NeedQuest:
		return n.Prereq + " complete"
	case NeedItem:
		if n.Qty > 1 {
			return fmt.Sprintf("%d %s", n.Qty, n.ItemName)
		}
		return n.ItemName
	}
	return string(n.Kind)
}

func (n Need) hard() bool { return n.Strength == StrengthHard }

// NeedHistory is what one account's side store remembers about one need (lane N3 persists it in
// ScarabSwarmNeeds.json): the failed stints since the last success, the parks they cost, and when it may be tried again.
type NeedHistory struct {
	Fails       int       `json:"fails,omitempty"`
	Parks       int       `json:"parks,omitempty"`
	ParkedUntil time.Time `json:"parkedUntil"`
	DiedAt      time.Time `json:"diedAt"`
}

// Subject is the account a plan is made for. Everything in it is a reading someone took; none of it is current-level data.
type Subject struct {
	// Account holds the XP by lower-case skill (1x), the quest points and the completed quests. Its Owned map is
	// PlanLadder's waiver view and is not read here: Own and MuleStock are.
	Account
	// Own is what the account holds (pack, worn and bank) by object id, OwnKnown false when it has not been read.
	Own      map[int]int
	OwnKnown bool
	// MuleStock is the mule's published bank, MuleKnown false when there is no mule or it has not been heard.
	MuleStock map[int]int
	MuleKnown bool
	// World is the world the account is on and MuleWorld the mule's (0 for unknown, which reads as reachable: the
	// ask itself will say otherwise).
	World, MuleWorld int
	// Needs are the account's stint histories by StintKey (the skill a stint trains), Parked its quest parks by quest id,
	// Deferrals how many times each quest has been deferred for an advised shortfall.
	Needs     map[string]NeedHistory
	Parked    map[string]time.Time
	Deferrals map[string]int
	// ParkedNeeds are the quests the ladder parked on a train need (the provisioner's PhaseTrain: "needs:mining:20 ..."), by quest id,
	// with the park's reason. Each is a hard skill need of that quest the item chains may not have named (planner.assess adds it), so a
	// quest waiting for a stint is always one the planner knows to give one.
	ParkedNeeds map[string]string
	// DeathsToday and Deaths7d are the account's deaths in the last day and week; HasFood is food in the pack. They
	// are the safety preconditions of plan 3.4 for a stint of risk R1 or more.
	DeathsToday, Deaths7d int
	HasFood               bool
	// IsProducer is whether the account is a designated producer (plan decisions D4 and D13: master makes the nails). A producer plans
	// the training of a ProducerExclusive item's skill link; every other account asks the mule and waits for the producer's stock.
	// Lane N3 sets it from the settings' producer list; the zero value is an ordinary account.
	IsProducer bool
}

// Level is the base level of skill, from XP. "combat" is the combat level.
func (s Subject) Level(skill string) int {
	if strings.EqualFold(skill, CombatSkill) {
		return s.Account.Combat()
	}
	lvl := s.Account.Level(skill)
	if strings.EqualFold(skill, "hitpoints") {
		lvl = max(lvl, 10) // no account is below 10 Hitpoints; an XP reading of 0 is a missing one
	}
	return lvl
}

// holds is how many of an object the account has and can have: its own, plus the mule's when the mule can be reached.
func (s Subject) holds(id int, muleReach bool) (own, mule int) {
	own = s.Own[id]
	if muleReach {
		mule = s.MuleStock[id]
	}
	return own, mule
}

// muleReachable reports whether an ask of the mule can be answered: the mule is heard and on the account's world, or the
// account may hop to it (lane N5), or a world is not known.
func (s Subject) muleReachable(hop bool) bool {
	if !s.MuleKnown {
		return false
	}
	return hop || s.World == 0 || s.MuleWorld == 0 || s.World == s.MuleWorld
}

// ModuleFloor is a level a module will not start below that neither its Record nor LadderFacts states.
type ModuleFloor struct {
	Quest    string
	Skill    string // CombatSkill for a combat level
	Level    int
	Strength Strength
	Note     string
}

// ModuleFloors are the module-private gates (plan 3.2 A, the fifth source). Each level is the module's own constant, so
// a change of the constant moves the need with it.
var ModuleFloors = []ModuleFloor{
	{Quest: FightArenaID, Skill: "hitpoints", Level: ArenaMinHitpoints, Strength: StrengthHard,
		Note: "the module refuses below it: Bouncer killed every low-hitpoint account that went in (fightarena.go)"},
	{Quest: DemonSlayerID, Skill: CombatSkill, Level: DemonMinCombat, Strength: StrengthHard,
		Note: "the dark wizards hunt combat 41 and under, and a hunted account cannot start the fight (DemonHuntFit)"},
	{Quest: BlackKnightsFortressID, Skill: "hitpoints", Level: BKMinHitpoints, Strength: StrengthAdvised,
		Note: "the knights hunt this account; BKMinHitpointsImmune (20) for one they do not"},
	{Quest: FishingContestID, Skill: "hitpoints", Level: FcWoodMinHitpoints, Strength: StrengthAdvised,
		Note: "the wood's guard dog; FcWoodMinHitpointsImm (15) for an account it does not hunt"},
}

// NeedSources is what BuildNeedTable merges.
type NeedSources struct {
	Facts  map[string]RungFacts
	Record func(id string) (Record, bool)
	Floors []ModuleFloor
	Items  []ItemNeed
}

// DefaultNeedSources are the package's own tables.
func DefaultNeedSources() NeedSources {
	return NeedSources{Facts: LadderFacts, Record: RecordOf, Floors: ModuleFloors, Items: ItemNeeds}
}

// NeedTable is every quest's needs, merged once.
type NeedTable struct {
	facts map[string]RungFacts
	// factPtr are the same rows by pointer, so the planner holds a quest's facts without copying them.
	factPtr map[string]*RungFacts
	needs   map[string][]Need
	items   map[string][]ItemNeed
	// rungs are the training rung ids of the facts (acquirers, not quests).
	rungs map[string]bool
}

// DefaultNeedTable merges the package's own tables.
func DefaultNeedTable() NeedTable { return BuildNeedTable(DefaultNeedSources()) }

// RequirementsOf is the merged needs of quest id from the package's own tables (plan 3.2 A's RequirementsOf).
func RequirementsOf(id string) []Need { return DefaultNeedTable().Requirements(id) }

// BuildNeedTable merges the five sources. A training rung (a row with TrainSkill) is not a quest and has no needs.
func BuildNeedTable(src NeedSources) NeedTable {
	t := NeedTable{facts: map[string]RungFacts{}, factPtr: map[string]*RungFacts{}, needs: map[string][]Need{}, items: map[string][]ItemNeed{}, rungs: map[string]bool{}}
	for id, f := range src.Facts {
		if f.TrainSkill != "" {
			t.rungs[id] = true
			continue
		}
		t.facts[id] = f
		fp := f
		t.factPtr[id] = &fp
		var rec *Record
		if src.Record != nil {
			if r, ok := src.Record(id); ok {
				rec = &r
			}
		}
		t.needs[id] = mergeNeeds(id, f, rec, src.Floors)
	}
	for _, in := range src.Items {
		for _, q := range in.Quests {
			if _, ok := t.facts[q]; ok {
				t.items[q] = append(t.items[q], in)
			}
		}
	}
	return t
}

// skillAcc accumulates one skill's needs of one strength.
type skillAcc struct {
	skill string
	st    Strength
}

// mergeNeeds is the merge of one quest's sources, in a fixed order: quest points, prerequisite quests, skills by name,
// the combat level.
func mergeNeeds(id string, f RungFacts, rec *Record, floors []ModuleFloor) []Need {
	skills := map[skillAcc]*Need{}
	add := func(skill string, level int, st Strength, wItem, wQty int, o Origin, note string) {
		skill = strings.ToLower(skill)
		if level <= 0 {
			return
		}
		k := skillAcc{skill, st}
		n := skills[k]
		if n == nil {
			kind := NeedSkill
			if skill == CombatSkill {
				kind = NeedCombat
			}
			skills[k] = &Need{Quest: id, Kind: kind, Strength: st, Skill: skill, Level: level,
				WaiverItem: wItem, WaiverQty: wQty, Origins: []Origin{o}, Note: note}
			if kind == NeedCombat {
				skills[k].Skill = ""
			}
			return
		}
		switch {
		case level > n.Level:
			n.Level, n.WaiverItem, n.WaiverQty, n.Note = level, wItem, wQty, note
		case level == n.Level && (n.WaiverItem != wItem || n.WaiverQty != wQty):
			n.WaiverItem, n.WaiverQty = 0, 0 // a source that states the level with no waiver binds it
		}
		n.Origins = addOrigin(n.Origins, o)
	}

	for _, g := range f.Gates {
		switch g.Kind {
		case GateServer, GateOwner, GateModule:
			add(g.Skill, g.Level, StrengthHard, 0, 0, OriginFacts, g.Note)
		case GateItem:
			add(g.Skill, g.Level, StrengthHard, g.WaiverItem, g.WaiverQty, OriginFacts, g.Note)
		default:
			add(g.Skill, g.Level, StrengthAdvised, 0, 0, OriginFacts, g.Note)
		}
	}
	if rec != nil {
		for _, s := range rec.Requirements.Skills {
			add(s.Skill, s.Level, StrengthHard, 0, 0, OriginRecord, "held by the record's requirement")
		}
	}
	for _, fl := range floors {
		if fl.Quest == id {
			add(fl.Skill, fl.Level, fl.Strength, 0, 0, OriginModule, fl.Note)
		}
	}
	add(CombatSkill, f.MinCombat, StrengthAdvised, 0, 0, OriginFacts, "the study's advised combat level")

	var out []Need
	qp := f.QPNeeded
	qpOrigins := []Origin{}
	if f.QPNeeded > 0 {
		qpOrigins = append(qpOrigins, OriginFacts)
	}
	if rec != nil && rec.Requirements.MinQuestPoints > 0 {
		qp = max(qp, rec.Requirements.MinQuestPoints)
		qpOrigins = addOrigin(qpOrigins, OriginRecord)
	}
	if qp > 0 {
		out = append(out, Need{Quest: id, Kind: NeedQuestPoints, Strength: StrengthHard, Level: qp, Origins: qpOrigins})
	}

	quests := map[string]*Need{}
	var order []string
	addQuest := func(pre string, wItem, wQty int, o Origin, note string) {
		if pre == "" {
			return
		}
		n := quests[pre]
		if n == nil {
			quests[pre] = &Need{Quest: id, Kind: NeedQuest, Strength: StrengthHard, Prereq: pre,
				WaiverItem: wItem, WaiverQty: wQty, Origins: []Origin{o}, Note: note}
			order = append(order, pre)
			return
		}
		if wItem == 0 { // a hard prerequisite beats a waivable one
			n.WaiverItem, n.WaiverQty = 0, 0
		}
		n.Origins = addOrigin(n.Origins, o)
	}
	for _, p := range f.Prereqs {
		addQuest(p, 0, 0, OriginFacts, "")
	}
	if rec != nil {
		for _, p := range rec.Requirements.Quests {
			addQuest(p, 0, 0, OriginRecord, "")
		}
	}
	for _, sp := range f.SoftPrereqs {
		addQuest(sp.ID, sp.WaiverItem, sp.WaiverQty, OriginSoft, sp.Note)
	}
	sort.Strings(order)
	for _, p := range order {
		out = append(out, *quests[p])
	}

	// An advised level the hard one of the same skill already covers adds nothing but its origin: Elemental Workshop's Mining 20 is
	// stated by LadderFacts (advised) and by its record (hard), and the merged need says both.
	for k, n := range skills {
		if k.st == StrengthAdvised {
			if h := skills[skillAcc{k.skill, StrengthHard}]; h != nil && h.Level >= n.Level {
				for _, o := range n.Origins {
					h.Origins = addOrigin(h.Origins, o)
				}
			}
		}
	}
	var ss []Need
	var combat []Need
	for k, n := range skills {
		if k.st == StrengthAdvised {
			if h := skills[skillAcc{k.skill, StrengthHard}]; h != nil && h.Level >= n.Level {
				continue
			}
		}
		if n.Kind == NeedCombat {
			combat = append(combat, *n)
			continue
		}
		ss = append(ss, *n)
	}
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].Skill != ss[j].Skill {
			return ss[i].Skill < ss[j].Skill
		}
		return ss[i].Strength < ss[j].Strength
	})
	sort.Slice(combat, func(i, j int) bool { return combat[i].Strength < combat[j].Strength })
	out = append(out, ss...)
	out = append(out, combat...)
	for i := range out {
		out[i].Origins = sortOrigins(out[i].Origins)
	}
	return out
}

// originRank is the fixed order origins are listed in, so a merged need reads the same on every run.
var originRank = map[Origin]int{OriginFacts: 0, OriginRecord: 1, OriginSoft: 2, OriginKit: 3, OriginModule: 4}

func sortOrigins(os []Origin) []Origin {
	out := append([]Origin(nil), os...)
	sort.Slice(out, func(i, j int) bool { return originRank[out[i]] < originRank[out[j]] })
	return out
}

func addOrigin(os []Origin, o Origin) []Origin {
	for _, x := range os {
		if x == o {
			return os
		}
	}
	return append(os, o)
}

// Facts is the quest's facts row.
func (t NeedTable) Facts(id string) (RungFacts, bool) {
	f, ok := t.facts[id]
	return f, ok
}

// factsOf is the quest's facts row by pointer (read only).
func (t NeedTable) factsOf(id string) *RungFacts { return t.factPtr[id] }

// Quests are the ids the table has needs for, sorted.
func (t NeedTable) Quests() []string {
	out := make([]string, 0, len(t.facts))
	for id := range t.facts {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Items are the item needs of quest id.
func (t NeedTable) Items(id string) []ItemNeed { return t.items[id] }

// Requirements are quest id's needs: the merged gates, quest points, prerequisites and combat level, then one
// NeedItem for each item the quest wants whose chain the table knows.
func (t NeedTable) Requirements(id string) []Need {
	out := append([]Need(nil), t.needs[id]...)
	for _, in := range t.items[id] {
		out = append(out, in.Need(id))
	}
	return out
}

// Unmet is the needs of quest id the subject does not meet, in the order Requirements gives them, and those it could
// not judge. A skill need is judged on base levels, with the XP of the quest's unmet prerequisites counted in (Jungle
// Potion wants Herblore 3, which Druidic Ritual pays: the need is not a gap for an account that has not done it yet). A
// skill need an item waives, and an item, are judged only from holdings that were read.
//
// An item need that is unmet but has a way to be got that needs no training (a mule that can be reached, a shop, a
// ground spawn) is not returned: the provisioner will fetch it when the quest runs. What is returned is an item the
// provisioner cannot (ItemJudgement says why), and the skill needs its chain's training link adds (ServesItem). An item only the
// designated producer makes (ProducerExclusive) is returned as the item for every other account, with no skill need: the level
// that makes it is the producer's (Subject.IsProducer), so an ordinary account waits for the mule's stock and trains nothing.
func (t NeedTable) Unmet(sub Subject, set NeedsSettings, id string) (unmet, unjudged []Need) {
	t.unmetInto(sub, set, id, func(n Need) { unmet = append(unmet, n) }, func(n Need) { unjudged = append(unjudged, n) })
	return unmet, unjudged
}

// unmetInto is Unmet handing each need to a callback as it is found, so the planner sorts them into its own lists without a
// slice in between.
func (t NeedTable) unmetInto(sub Subject, set NeedsSettings, id string, addUnmet, addUnjudged func(Need)) {
	if sub.Done[id] {
		return
	}
	proj := t.projected(sub, id)
	var seen []Need // the unmet skill needs so far, for the chain skill needs that repeat one
	for _, n := range t.needs[id] {
		if t.metDirect(sub, proj, n) {
			continue
		}
		// Below the level (or without the prerequisite): an owned item may still waive the need, which only a read of the
		// holdings can say.
		if n.WaiverItem > 0 && !sub.OwnKnown {
			addUnjudged(n)
			continue
		}
		if t.waivedBy(sub, set, n) {
			continue
		}
		if _, ok := t.producerGate(id, n); ok && !sub.IsProducer {
			// A level that exists only to make an item the producer alone makes (Smithing 34 for the nails of Horror and Dragon
			// Slayer, plan 3.5) is not this account's to train: the item loop below asks the mule for the item and, when the
			// mule has too little, waits for the producer (ItemProducer). Training it here would send 283 accounts to the anvil
			// for about 810 iron bars each.
			continue
		}
		addUnmet(n)
		if n.Kind == NeedSkill {
			seen = append(seen, n)
		}
	}
	for _, in := range t.items[id] {
		if !sub.OwnKnown {
			addUnjudged(in.Need(id))
			continue
		}
		j := JudgeItem(sub, set, in)
		switch j.State {
		case ItemStuck, ItemMuleFar, ItemProducer:
			n := in.Need(id)
			n.Note = j.Reason
			addUnmet(n)
		case ItemTrain:
			for _, sk := range j.Skills {
				sk.Quest = id
				if !t.alreadyUnmet(seen, sk) && !subjectMeetsSkill(proj, sk) {
					addUnmet(sk)
					seen = append(seen, sk)
				}
			}
		}
	}
}

// producerGate returns the exclusive-producer item need that skill need n of quest id exists only to serve: n is an item gate, waived
// by an object, and the quest's chain for that object is the producer's alone (ProducerExclusive). The nails are the case: Horror's
// and Dragon Slayer's Smithing 34 is waived by the nails it would make. A level some other source also states carries no waiver
// (mergeNeeds), so it is not one.
func (t NeedTable) producerGate(id string, n Need) (ItemNeed, bool) {
	if n.Kind != NeedSkill || n.WaiverItem <= 0 {
		return ItemNeed{}, false
	}
	for _, in := range t.items[id] {
		if in.Item == n.WaiverItem && in.Producer == ProducerExclusive {
			return in, true
		}
	}
	return ItemNeed{}, false
}

// alreadyUnmet reports whether an unmet skill need of at least sk's level is in the list.
func (t NeedTable) alreadyUnmet(list []Need, sk Need) bool {
	for _, n := range list {
		if n.Kind == NeedSkill && n.Skill == sk.Skill && n.Level >= sk.Level {
			return true
		}
	}
	return false
}

func subjectMeetsSkill(a Account, n Need) bool { return a.Level(n.Skill) >= n.Level }

// metDirect judges one need against the subject without its waiver, with proj (its XP plus its prerequisites' rewards) for the
// skill needs.
func (t NeedTable) metDirect(sub Subject, proj Account, n Need) bool {
	switch n.Kind {
	case NeedSkill:
		return proj.Level(n.Skill) >= n.Level
	case NeedCombat:
		return proj.Combat() >= n.Level
	case NeedQuestPoints:
		return sub.QP >= n.Level
	case NeedQuest:
		return sub.Done[n.Prereq]
	}
	return true
}

// waivedBy reports whether an owned item waives the need, from holdings that were read. The mule counts when it can be reached,
// by the same rule the item chains and the planner's schedule use (the same world, or a hop the settings allow).
func (t NeedTable) waivedBy(sub Subject, set NeedsSettings, n Need) bool {
	if n.WaiverItem <= 0 || !sub.OwnKnown {
		return false
	}
	own, mule := sub.holds(n.WaiverItem, sub.muleReachable(set.MuleHop))
	return own+mule >= n.WaiverQty
}

// projected is the subject's account with the XP rewards of quest id's unmet prerequisites (transitively) added: what it
// will hold when the quest's turn comes. A quest with no unmet prerequisite is judged on the subject's own account, uncopied.
func (t NeedTable) projected(sub Subject, id string) Account {
	if !t.hasUnmetPrereq(sub, id) {
		return sub.Account
	}
	acct := Account{XP: make(map[string]int, len(sub.XP)), QP: sub.QP, Done: sub.Done}
	for k, v := range sub.XP {
		acct.XP[k] = v
	}
	seen := map[string]bool{id: true}
	var walk func(q string, depth int)
	walk = func(q string, depth int) {
		if depth > 8 {
			return
		}
		for _, n := range t.needs[q] {
			if n.Kind != NeedQuest || seen[n.Prereq] || sub.Done[n.Prereq] {
				continue
			}
			seen[n.Prereq] = true
			if f, ok := t.facts[n.Prereq]; ok {
				for skill, xp := range f.XP {
					acct.XP[skill] += xp
				}
				acct.QP += f.QP
			}
			walk(n.Prereq, depth+1)
		}
	}
	walk(id, 0)
	return acct
}

// hasUnmetPrereq reports whether quest id has a prerequisite the subject has not done.
func (t NeedTable) hasUnmetPrereq(sub Subject, id string) bool {
	for _, n := range t.needs[id] {
		if n.Kind == NeedQuest && !sub.Done[n.Prereq] {
			return true
		}
	}
	return false
}

// UnmetOf is Unmet over the package's own tables with default settings.
func UnmetOf(sub Subject, id string) (unmet, unjudged []Need) {
	return DefaultNeedTable().Unmet(sub, DefaultNeedsSettings(), id)
}

// HardUnmet is the hard needs among unmet.
func HardUnmet(unmet []Need) []Need {
	var out []Need
	for _, n := range unmet {
		if n.hard() {
			out = append(out, n)
		}
	}
	return out
}

// XPFor is the XP a subject lacks to reach level in skill, 0 when it is there.
func (s Subject) XPFor(skill string, level int) int {
	return max(levels.XPAtLevel(level)-s.XP[strings.ToLower(skill)], 0)
}
