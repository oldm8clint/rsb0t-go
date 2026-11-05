package questplan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"rsb0t-go/internal/scripts/levels"
)

// Training rung ids, and what the trainer catalogue makes of them (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 C,
// lane N2).
//
// A training rung is a ladder stop that trains one skill to one level. Until N2 only the twelve LadderFacts rungs could run:
// GateRungID named a rung only for a gate whose level was EXACTLY a rung's (Crafting 18 and 40, never 19), any other level
// got the generic id train:<skill>:<level>, and that id had no factory, so the quest behind the gate was held for want of a
// level nothing trained. Now an id names its skill and level, and the catalogue says which row works it:
//
//	train:<skill>:<level>   the generic stop (TrainRungID), what a gate between two named rungs and a planned stint ask for
//	<skill><level>          the named style (agility25, smithing34), also any level of a skill the catalogue has a rung row for
//
// Either form resolves to the row of the skill whose range reaches the level (From < level <= To) and whose Via is ViaRung,
// best first. The row's Family is the rung family that is built (questmods.RungFamilies) and the stint stops at the level:
// a rung's Done is the base level, so Crafting 19 trains to 19 and no further, where the named crafting40 is the whole
// flax stretch. The named rungs stay what they were; they are only the exact-level case of this.
//
// A level no row reaches has no rung. The reason is the named gap (SkillGaps) the needs planner already reports as
// "unreachable: no trainer for <skill> <level>"; it is never a rung id with a missing factory.

// rungSkillNames are the skills a rung id may name, by their lower-case names, longest first so that a prefix match on
// "runecrafting" is not taken for "runecraft".
var rungSkillNames = func() []string {
	names := []string{"attack", "defence", "strength", "hitpoints", "ranged", "prayer", "magic", "cooking", "woodcutting", "fletching",
		"fishing", "firemaking", "crafting", "smithing", "mining", "herblore", "agility", "thieving", "slayer", "farming", "runecraft", "runecrafting"}
	sort.SliceStable(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	return names
}()

// RungSkillKnown reports whether skill is a skill name a rung id may carry.
func RungSkillKnown(skill string) bool {
	skill = strings.ToLower(strings.TrimSpace(skill))
	for _, n := range rungSkillNames {
		if n == skill {
			return true
		}
	}
	return false
}

// canonicalLevel reads s as a level written the one way the ids are (2 to 99, no sign, no leading zero).
func canonicalLevel(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || strconv.Itoa(n) != s || n < 2 || n > levels.MaxLevel {
		return 0, false
	}
	return n, true
}

// ParseRungID reads a training rung id as the skill and the level it trains to: "train:<skill>:<level>" (TrainRungID) or
// "<skill><level>" ("agility25"). The case and the ends are forgiven; ok is false for anything else, a quest id or a rung
// whose level is a setting (prayerkq) or whose name is not a skill's (cows20) among them. A level is 2 to 99: level 1 is
// where every account starts and there is nothing to train to it.
func ParseRungID(id string) (skill string, level int, ok bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if rest, found := strings.CutPrefix(id, "train:"); found {
		name, lv, found := strings.Cut(rest, ":")
		if !found || !RungSkillKnown(name) {
			return "", 0, false
		}
		n, good := canonicalLevel(lv)
		if !good {
			return "", 0, false
		}
		return name, n, true
	}
	for _, name := range rungSkillNames {
		rest, found := strings.CutPrefix(id, name)
		if !found || rest == "" {
			continue
		}
		if n, good := canonicalLevel(rest); good {
			return name, n, true
		}
	}
	return "", 0, false
}

// RungRow is the rung row that works an account up to `level` in skill: the best-first row whose Via is ViaRung, which
// names a rung family, and whose range covers the level just below it (From < level <= To). A loop row never is: the
// needs engine does not stint a loop. When two rows reach the level the first in the catalogue's order is the pick,
// which is what the needs planner's own ranking comes to when no site is full.
func (c Catalogue) RungRow(skill string, level int) (TrainerSpec, bool) {
	for _, t := range c.For(skill, level-1) {
		if t.Via == ViaRung && t.Family != "" {
			return t, true
		}
	}
	return TrainerSpec{}, false
}

// RungResolution is what the catalogue makes of a rung id.
type RungResolution struct {
	// ID is the rung's id as it is built under, trimmed and lower case.
	ID string
	// Skill and Level are what it trains and the level it stops at.
	Skill string
	Level int
	// Trainer is the row that works it; Trainer.Family is the rung family to build.
	Trainer TrainerSpec
	// Named is true when the id is one of the row's named rungs (agility25), false for a generic stop (train:agility:30).
	Named bool
}

// ResolveRung says which row works rung id, and to what level. why is the reason when none does: an id that is not a rung
// id, a rung whose level is a setting, a level a loop trains, or a level no row reaches (with the gap that names it).
func (c Catalogue) ResolveRung(id string) (res RungResolution, why string) {
	id = strings.ToLower(strings.TrimSpace(id))
	if t, ok := c.RungOwner(id); ok {
		if t.Via == ViaRung && t.Family != "" {
			for level, named := range t.Rungs {
				if named == id {
					return RungResolution{ID: id, Skill: t.Skill, Level: level, Trainer: t, Named: true}, ""
				}
			}
		}
		return RungResolution{}, fmt.Sprintf("%s trains to a level a setting decides, so no id carries it", id)
	}
	skill, level, ok := ParseRungID(id)
	if !ok {
		return RungResolution{}, fmt.Sprintf("%q is not a training rung id (train:<skill>:<level> or <skill><level>)", id)
	}
	if t, ok := c.RungRow(skill, level); ok {
		return RungResolution{ID: id, Skill: skill, Level: level, Trainer: t}, ""
	}
	return RungResolution{}, c.noRungReason(skill, level)
}

// noRungReason says why no rung row reaches `level` in skill.
func (c Catalogue) noRungReason(skill string, level int) string {
	if u := c.FirstUncovered(skill, 1, level); u != 0 {
		why := fmt.Sprintf("no trainer for %s %d (no row covers level %d)", skill, level, u)
		if g, ok := GapFor(skill, u); ok {
			why += "; lane " + g.Lane + ": " + g.Reason
		}
		return why
	}
	for _, t := range c.For(skill, level-1) {
		if t.Via == ViaLoop {
			return fmt.Sprintf("%s %d is trained by the %s loop, which the needs engine does not stint", skill, level, t.Loop)
		}
	}
	return fmt.Sprintf("no rung row reaches %s %d", skill, level)
}

// GateRung is the rung that trains the level a gate asks of an account: the named rung of exactly that level when the
// ladder has one (Crafting 18), else the generic stop, which the catalogue can build when a row reaches the level. ok is
// false, with the named gap in why, when none does.
func (c Catalogue) GateRung(g Gate) (id string, ok bool, why string) {
	id = GateRungID(g)
	res, why := c.ResolveRung(id)
	return id, res.ID != "", why
}

var (
	sharedMu        sync.Mutex
	sharedCatalogue *Catalogue
)

// SharedCatalogue is the default catalogue, built once on first use. It is for the hot paths that ask what a rung id
// means on every ladder pass; the rows register from init, so by the time anything calls it every row is there. A test
// that wants rows of its own builds a Catalogue with NewCatalogue.
func SharedCatalogue() Catalogue {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedCatalogue == nil {
		c := DefaultCatalogue()
		sharedCatalogue = &c
	}
	return *sharedCatalogue
}

// ModuleFilesFor is the source files that ARE quest or rung id, for the one question cmd/questpromote asks of git. A quest
// and a named rung are ModuleFiles' entries; a generic rung (train:crafting:19, mining20) is the row that works it, so the
// files of the row's own Files, else of its first named rung: a change to the Cows rung resets every level of it, named or
// not. Nil for an id that is neither.
func ModuleFilesFor(id string) []string {
	id = strings.ToLower(strings.TrimSpace(id))
	if files := ModuleFiles[id]; len(files) > 0 {
		return files
	}
	res, _ := SharedCatalogue().ResolveRung(id)
	if res.ID == "" {
		return nil
	}
	return res.Trainer.SourceFiles()
}

// SourceFiles are the row's source files: its Files when it names any, else those of its first named rung (ModuleFiles,
// by the rung's id), so a row that already has a rung in the table needs no second entry.
func (t TrainerSpec) SourceFiles() []string {
	if len(t.Files) > 0 {
		return t.Files
	}
	for _, id := range t.RungIDs() {
		if files := ModuleFiles[id]; len(files) > 0 {
			return files
		}
	}
	return nil
}
