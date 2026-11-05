package questplan

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
)

// The trainer catalogue (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 B): every way the fleet knows to
// raise a skill, as data the needs planner (needsplan.go) can rank. It holds no behaviour. A row says which
// base levels the trainer is worth working at, how dangerous it is, where it is worked and how many accounts
// that site takes, what it eats, what it makes on the way, and how fast it goes; the live half (the
// questmods executor and the bot's rung factory) is named, not linked, so this package stays pure.
//
// ROWS LIVE IN PER-SKILL FILES (trainers_agility.go, trainers_mining.go ...), each registering its rows from
// its own init. A trainer lane (T1 to T9) adds ONE file and edits no other, which is what stops nine lanes
// colliding in one table (the completeness critic's item 17). Registration order is the order Go initialises
// the files in, which is the order of their names; the catalogue sorts rows itself, so nothing depends on it.
//
// WHAT A ROW CLAIMS, AND HOW IT IS HELD TO IT. Every row is validated (TrainerSpec.Validate) when a catalogue
// is built; an invalid row is dropped and listed in Catalogue.Problems, never panicked on at start-up (a bad
// row must not stop a fleet binary), and TestTheDefaultCatalogueHasNoProblems fails the build instead. The
// twelve rungs the ladder already runs each belong to exactly one row (TestEveryTrainingRungBelongsToARow), and
// a rung's module files are the row's (TestEveryTrainerRungHasModuleFiles), so a trainer's evidence window
// (cmd/questpromote, lane N6) is the rung's. The estimates are labelled: a rate with no measurement behind it
// says so in Source, and the stint ledger (lane N6) replaces it with the one that was measured.

// Risk is how dangerous a trainer is for the account working it (plan 3.4).
type Risk int

const (
	// R0 has no hostile npc in range and no Wilderness: mines, anvils, ranges, shores, banks.
	R0 Risk = iota
	// R1 is passive or low-level monsters the account's fight model beats with food: cows, chickens, the Gnome course.
	R1
	// R2 is aggressive or higher-level monsters (giants, ice warriors), only through the existing rung gating.
	R2
	// R3 is the Wilderness or a boss, only the listed owner-ruled legs.
	R3
)

// String is "R0" to "R3".
func (r Risk) String() string { return fmt.Sprintf("R%d", int(r)) }

// Via is who runs a trainer.
type Via string

const (
	// ViaRung is a ladder rung: the needs engine runs a stint of it (a questrunner.Quest built by a bot factory).
	ViaRung Via = "rung"
	// ViaLoop is a loop the fleet already runs for the account (the Stage 5 combat trainer, the magic slices, the
	// self-cook session): the needs engine does not stint it, it only knows the loop will raise the level, and says
	// so ("covered by the loop") instead of treating the need as a gap.
	ViaLoop Via = "loop"
)

// TrainerSite is one place a trainer is worked.
type TrainerSite struct {
	// Key is the site's stable id ("seers-flax-wheel"); the fleet's site load is counted under it.
	Key  string
	Name string
	Tile tile.Tile
	// Capacity is how many accounts may work the site at once. A provisional cap until the site board
	// (lane N3) has measured the real one: 280 accounts must not crowd one rock, one pen or one wheel.
	Capacity int
}

// TrainerInput is something a trainer needs in the pack.
type TrainerInput struct {
	Name string
	// IDs are the object ids any of which serves (bronze, iron or steel bar).
	IDs []int
	Qty int
	// Tool is an item the provisioner's tool lines supply (hammer, axe, pickaxe): it is never counted against a
	// stint's stock, only a consumable is.
	Tool bool
	// Gathers marks a consumable the rung can also fetch from the world when the bank and the mule have none (flax from the
	// Seers field, bones from the cows): it is not counted against a stint's stock either.
	Gathers bool
}

// TrainerByproduct is what a trainer makes on the way and the fleet can use.
type TrainerByproduct struct {
	Name    string
	ID      int
	PerHour int
}

// TrainerSpec is one row of the catalogue.
type TrainerSpec struct {
	// ID is the row's stable id ("gnome-course"), lower case, no spaces.
	ID string
	// Skill is the lower-case skill the row raises.
	Skill string
	// From and To are the base levels the row covers: it can take an account from From up to To. An account at
	// level ℓ is trained by the row when From <= ℓ < To, and a need for level L is covered when every level from
	// the account's own up to L-1 is.
	From, To int
	Risk     Risk
	Via      Via
	// Loop (ViaLoop) names the loop that trains the skill ("stage5-combat", "stage5-magic", "scarab-selfcook").
	Loop string
	// Family (ViaRung) is the rung family a generic rung id is built with (lane N2: gnome, crafting, cows ...).
	Family string
	// Rungs (ViaRung) are the named rungs the ladder already runs, by the exact level each trains to
	// (agility25 at 25). RungsAtSetting are named rungs whose level is a setting, not a number of this table
	// (prayerkq trains to whatever the Queen's protection prayer needs).
	Rungs          map[int]string
	RungsAtSetting []string
	Sites          []TrainerSite
	Inputs         []TrainerInput
	Byproducts     []TrainerByproduct
	// EstXPPerHour is the estimate and MeasuredXPPerHour the measurement (0 when there is none); Source says
	// which of them to believe and where either came from.
	EstXPPerHour      int
	MeasuredXPPerHour int
	// MinStint and MaxStint bound one stint (ViaRung only; a loop is not stinted).
	MinStint, MaxStint time.Duration
	// Slot and SlotCap are an optional fleet-wide slot: at most SlotCap accounts in the trainer at once.
	Slot    string
	SlotCap int
	// Rank orders rows that cover the same level, lowest first, ahead of risk and speed: a loop that already
	// trains the skill outranks a rung that would duplicate it.
	Rank   int
	Source string
	Note   string
	// Files (ViaRung) are the row's source files, repo-relative, for the promotion window of the generic rungs it works
	// (ModuleFilesFor). Empty when a named rung of the row has a ModuleFiles entry, which then stands for the row.
	Files []string
}

// Covers reports whether the row trains an account of base level ℓ.
func (t TrainerSpec) Covers(level int) bool { return level >= t.From && level < t.To }

// Rate is the XP an hour the planner plans with: the measurement when there is one, else the estimate.
func (t TrainerSpec) Rate() int {
	if t.MeasuredXPPerHour > 0 {
		return t.MeasuredXPPerHour
	}
	return t.EstXPPerHour
}

// Measured reports whether Rate is a measurement.
func (t TrainerSpec) Measured() bool { return t.MeasuredXPPerHour > 0 }

// HoursBetween is the time the row takes to raise an account from xp to the start of level to, in hours;
// 0 when the account is there already.
func (t TrainerSpec) HoursBetween(xp, to int) float64 {
	need := levels.XPAtLevel(to) - xp
	if need <= 0 || t.Rate() <= 0 {
		return 0
	}
	return float64(need) / float64(t.Rate())
}

// RungFor is the named rung that trains exactly to level, if the row has one.
func (t TrainerSpec) RungFor(level int) (string, bool) {
	id, ok := t.Rungs[level]
	return id, ok
}

// RungIDs are every named rung the row claims, sorted.
func (t TrainerSpec) RungIDs() []string {
	out := append([]string(nil), t.RungsAtSetting...)
	for _, id := range t.Rungs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// GenericRungID is the id of the stint the row would run to reach level when no named rung trains exactly
// there: the same id the ladder's planner already gives a generic stop (TrainRungID). The catalogue resolves it back to
// a row (Catalogue.ResolveRung) and the bot builds it from the row's Family (bot.QuestFactoryFor).
func (t TrainerSpec) GenericRungID(level int) string { return TrainRungID(t.Skill, level) }

// Validate reports why the row is malformed, or "".
func (t TrainerSpec) Validate() string {
	if t.ID == "" || t.ID != strings.ToLower(strings.TrimSpace(t.ID)) || strings.ContainsAny(t.ID, " \t") {
		return fmt.Sprintf("trainer id %q must be non-empty, lower case and without spaces", t.ID)
	}
	if t.Skill == "" || t.Skill != strings.ToLower(strings.TrimSpace(t.Skill)) {
		return t.ID + ": the skill must be non-empty and lower case"
	}
	if t.From < 1 || t.To <= t.From || t.To > levels.MaxLevel {
		return fmt.Sprintf("%s: levels %d to %d are not a range inside 1 to %d", t.ID, t.From, t.To, levels.MaxLevel)
	}
	if t.Risk < R0 || t.Risk > R3 {
		return fmt.Sprintf("%s: risk %d is not R0 to R3", t.ID, int(t.Risk))
	}
	if t.Rate() <= 0 {
		return t.ID + ": no XP an hour, estimated or measured"
	}
	if strings.TrimSpace(t.Source) == "" {
		return t.ID + ": no source for its rate"
	}
	switch t.Via {
	case ViaRung:
		if t.Family == "" {
			return t.ID + ": a rung row names the rung family generic ids are built with"
		}
		if t.MinStint <= 0 || t.MaxStint < t.MinStint {
			return t.ID + ": a rung row needs 0 < MinStint <= MaxStint"
		}
		for level, id := range t.Rungs {
			if level < t.From || level > t.To || strings.TrimSpace(id) == "" {
				return fmt.Sprintf("%s: named rung %q at level %d is outside %d to %d", t.ID, id, level, t.From, t.To)
			}
		}
		if t.Loop != "" {
			return t.ID + ": a rung row names no loop"
		}
	case ViaLoop:
		if t.Loop == "" {
			return t.ID + ": a loop row names its loop"
		}
		if len(t.Rungs) > 0 || len(t.RungsAtSetting) > 0 || t.Family != "" || t.MinStint != 0 || t.MaxStint != 0 {
			return t.ID + ": a loop row has no rungs, family or stint length: the loop is not stinted"
		}
	default:
		return fmt.Sprintf("%s: unknown way to run it %q", t.ID, t.Via)
	}
	seen := map[string]bool{}
	for _, s := range t.Sites {
		if s.Key == "" || s.Capacity <= 0 || seen[s.Key] {
			return fmt.Sprintf("%s: site %q needs a unique key and a positive capacity", t.ID, s.Key)
		}
		seen[s.Key] = true
	}
	for _, f := range t.Files {
		if strings.TrimSpace(f) == "" || strings.Contains(f, "\\") {
			return fmt.Sprintf("%s: source file %q is not a repo-relative path with forward slashes", t.ID, f)
		}
	}
	if (t.Slot == "") != (t.SlotCap == 0) || t.SlotCap < 0 {
		return t.ID + ": a fleet-wide slot needs both a name and a positive cap"
	}
	for _, in := range t.Inputs {
		if in.Name == "" || in.Qty <= 0 || len(in.IDs) == 0 {
			return fmt.Sprintf("%s: input %q needs a name, ids and a positive quantity", t.ID, in.Name)
		}
	}
	return ""
}

var (
	trainerMu   sync.Mutex
	trainerRows []TrainerSpec
)

// RegisterTrainer adds a row to the default catalogue, from a trainers_<skill>.go init. It checks nothing:
// NewCatalogue does, so a bad row is reported where it is read and never stops the process at start-up.
func RegisterTrainer(t TrainerSpec) {
	trainerMu.Lock()
	defer trainerMu.Unlock()
	trainerRows = append(trainerRows, t)
}

// Catalogue is a validated set of trainer rows, ordered best first per skill.
type Catalogue struct {
	rows     []TrainerSpec
	problems []string
	// bySkill are the rows of each skill, best first, for For.
	bySkill map[string][]TrainerSpec
}

// DefaultCatalogue is every registered row.
func DefaultCatalogue() Catalogue {
	trainerMu.Lock()
	rows := append([]TrainerSpec(nil), trainerRows...)
	trainerMu.Unlock()
	return NewCatalogue(rows...)
}

// NewCatalogue validates rows and orders them: by skill, then the order For answers in. A row that does not
// validate, and a second row under an id already taken, is dropped and listed in Problems.
func NewCatalogue(rows ...TrainerSpec) Catalogue {
	var c Catalogue
	seen := map[string]bool{}
	claimed := map[string]string{}
	for _, t := range rows {
		if why := t.Validate(); why != "" {
			c.problems = append(c.problems, why)
			continue
		}
		if seen[t.ID] {
			c.problems = append(c.problems, "trainer "+t.ID+" is registered twice")
			continue
		}
		dup := ""
		for _, id := range t.RungIDs() {
			if owner, ok := claimed[id]; ok {
				dup = fmt.Sprintf("%s claims rung %s, which %s already does", t.ID, id, owner)
				break
			}
		}
		if dup != "" {
			c.problems = append(c.problems, dup)
			continue
		}
		for _, id := range t.RungIDs() {
			claimed[id] = t.ID
		}
		seen[t.ID] = true
		c.rows = append(c.rows, t)
	}
	sort.SliceStable(c.rows, func(i, j int) bool {
		a, b := c.rows[i], c.rows[j]
		if a.Skill != b.Skill {
			return a.Skill < b.Skill
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return trainerBetter(a, b)
	})
	c.bySkill = map[string][]TrainerSpec{}
	for _, t := range c.rows {
		c.bySkill[t.Skill] = append(c.bySkill[t.Skill], t)
	}
	for _, rows := range c.bySkill {
		sort.SliceStable(rows, func(i, j int) bool { return trainerBetter(rows[i], rows[j]) })
	}
	return c
}

// trainerBetter is the static "best first" order of two rows that cover a level: the lower Rank, then the lower
// risk, then the faster, then the id (plan 3.2 D.3 ranks by risk, then hours: at one level hours fall as the rate rises).
func trainerBetter(a, b TrainerSpec) bool {
	if a.Rank != b.Rank {
		return a.Rank < b.Rank
	}
	if a.Risk != b.Risk {
		return a.Risk < b.Risk
	}
	if a.Rate() != b.Rate() {
		return a.Rate() > b.Rate()
	}
	return a.ID < b.ID
}

// Problems are the rows the catalogue refused, and why.
func (c Catalogue) Problems() []string { return append([]string(nil), c.problems...) }

// Trainers are the rows, sorted by skill and level.
func (c Catalogue) Trainers() []TrainerSpec { return append([]TrainerSpec(nil), c.rows...) }

// ByID is the row with that id.
func (c Catalogue) ByID(id string) (TrainerSpec, bool) {
	for _, t := range c.rows {
		if t.ID == id {
			return t, true
		}
	}
	return TrainerSpec{}, false
}

// RungOwner is the row that claims the named rung id.
func (c Catalogue) RungOwner(rung string) (TrainerSpec, bool) {
	for _, t := range c.rows {
		for _, id := range t.RungIDs() {
			if id == rung {
				return t, true
			}
		}
	}
	return TrainerSpec{}, false
}

// For is the rows that train an account of base level `level` in skill, best first.
func (c Catalogue) For(skill string, level int) []TrainerSpec {
	var out []TrainerSpec
	for _, t := range c.bySkill[strings.ToLower(skill)] {
		if t.Covers(level) {
			out = append(out, t)
		}
	}
	return out
}

// Skills are the skills with at least one row.
func (c Catalogue) Skills() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range c.rows {
		if !seen[t.Skill] {
			seen[t.Skill] = true
			out = append(out, t.Skill)
		}
	}
	return out
}

// FirstUncovered is the lowest level in [from, to) no row trains, or 0 when the whole stretch is covered: the
// first level an account asked to reach `to` from `from` cannot be trained through. A need for level L of an
// account at level c is FirstUncovered(skill, c, L).
func (c Catalogue) FirstUncovered(skill string, from, to int) int {
	for l := max(from, 1); l < to; l++ {
		if len(c.For(skill, l)) == 0 {
			return l
		}
	}
	return 0
}

// Covered reports whether a need for level `to` is reachable from level 1.
func (c Catalogue) Covered(skill string, to int) bool { return c.FirstUncovered(skill, 1, to) == 0 }

// Leg is one stretch of a training path: a trainer taking the account from level From to level To.
type Leg struct {
	Trainer TrainerSpec
	From    int
	To      int
}

// Path is the legs that raise skill from level `from` to level `to`, taking at each level the first row
// `pick` accepts out of those that cover it (best first), and extending a leg while the same row stays the pick.
// When some level has no acceptable row the path stops there: uncovered is that level, and legs hold the stretch
// before it. uncovered is 0 for a complete path.
func (c Catalogue) Path(skill string, from, to int, pick func([]TrainerSpec) (TrainerSpec, bool)) (legs []Leg, uncovered int) {
	if pick == nil {
		pick = func(rows []TrainerSpec) (TrainerSpec, bool) {
			if len(rows) == 0 {
				return TrainerSpec{}, false
			}
			return rows[0], true
		}
	}
	l := max(from, 1)
	for l < to {
		t, ok := pick(c.For(skill, l))
		if !ok || !t.Covers(l) { // a pick that does not cover l would never advance
			return legs, l
		}
		end := min(t.To, to)
		legs = append(legs, Leg{Trainer: t, From: l, To: end})
		l = end
	}
	return legs, 0
}
