package questmods

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questrunner"
)

// RUNG FAMILIES (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 C, lane N2).
//
// A training rung is the same code at a different skill and target: GnomeAgility("agility25", 25) and ("agility35", 35) are one
// family, as are the four Cows rungs. A family registers ONE builder here, under the name a trainer-catalogue row gives as its
// Family (questplan.TrainerSpec.Family), and the bot builds any rung of the row from it: the twelve named rungs the ladder has
// always run, and the generic stop to any level in between (train:crafting:19), which stops at its level, not at the next named
// one. A trainer lane (T1 to T9) adds its family from its own file with RegisterRungFamily, and edits no other.

// RungBuilder builds the training rung of one family: the quest that trains skill to level under the ladder id id. Its Done is the
// server's own base level, since a training rung has no quest tab.
type RungBuilder func(id, skill string, level int) questrunner.Quest

// RungFamily is a family of training rungs.
type RungFamily struct {
	// Name is the catalogue row's Family ("gnome", "cows").
	Name string
	// Skills are the lower-case skills the family trains; BuildRung refuses any other, so a row that names the wrong family
	// fails here and not as a ladder that trains nothing.
	Skills []string
	Build  RungBuilder
}

// rungRegistry is a set of families. The package keeps one (rungs); a test builds its own, so that trying a bad family leaves the
// real list alone.
type rungRegistry struct {
	mu       sync.RWMutex
	families map[string]RungFamily
	problems []string
}

func newRungRegistry() *rungRegistry { return &rungRegistry{families: map[string]RungFamily{}} }

var rungs = newRungRegistry()

// RegisterRungFamily adds a family, from an init. A malformed or duplicate family is dropped and listed in RungFamilyProblems,
// never panicked on, as the trainer catalogue treats a bad row: a bad family must not stop a fleet binary, and the test over
// the problems list fails the build instead.
func RegisterRungFamily(f RungFamily) { rungs.register(f) }

func (r *rungRegistry) register(f RungFamily) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case f.Name == "" || f.Name != strings.ToLower(strings.TrimSpace(f.Name)) || strings.ContainsAny(f.Name, " 	:"):
		r.problems = append(r.problems, fmt.Sprintf("rung family %q must be non-empty, lower case and without spaces or colons", f.Name))
	case f.Build == nil || len(f.Skills) == 0:
		r.problems = append(r.problems, fmt.Sprintf("rung family %s needs a builder and at least one skill", f.Name))
	default:
		if _, dup := r.families[f.Name]; dup {
			r.problems = append(r.problems, fmt.Sprintf("rung family %s is registered twice", f.Name))
			return
		}
		f.Skills = append([]string(nil), f.Skills...)
		r.families[f.Name] = f
	}
}

// RungFamilyProblems are the families RegisterRungFamily refused, and why.
func RungFamilyProblems() []string { return rungs.problemList() }

func (r *rungRegistry) problemList() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.problems...)
}

// RungFamilyNames are the registered families, sorted.
func RungFamilyNames() []string { return rungs.names() }

func (r *rungRegistry) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.families))
	for name := range r.families {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// RungFamilySkills are the skills family trains, and whether it is registered.
func RungFamilySkills(family string) ([]string, bool) {
	rungs.mu.RLock()
	defer rungs.mu.RUnlock()
	f, ok := rungs.families[family]
	return append([]string(nil), f.Skills...), ok
}

// BuildRung builds the rung of family that trains skill to level under id. It is an error for a family nobody registered, for a
// skill the family does not train, and for a level that is not 2 to 99 (level 1 is where every account starts).
func BuildRung(family, id, skill string, level int) (questrunner.Quest, error) {
	return rungs.build(family, id, skill, level)
}

func (r *rungRegistry) build(family, id, skill string, level int) (questrunner.Quest, error) {
	r.mu.RLock()
	f, ok := r.families[family]
	r.mu.RUnlock()
	if !ok {
		return questrunner.Quest{}, fmt.Errorf("no rung family %q (have %v)", family, r.names())
	}
	skill = strings.ToLower(strings.TrimSpace(skill))
	served := false
	for _, s := range f.Skills {
		served = served || s == skill
	}
	if !served {
		return questrunner.Quest{}, fmt.Errorf("rung family %s trains %v, not %q", family, f.Skills, skill)
	}
	if level < 2 || level > levels.MaxLevel {
		return questrunner.Quest{}, fmt.Errorf("rung %s: level %d is not 2 to %d", id, level, levels.MaxLevel)
	}
	if strings.TrimSpace(id) == "" {
		return questrunner.Quest{}, fmt.Errorf("rung family %s: a rung needs an id", family)
	}
	return f.Build(id, skill, level), nil
}

func init() {
	RegisterRungFamily(RungFamily{Name: "gnome", Skills: []string{"agility"},
		Build: func(id, _ string, level int) questrunner.Quest { return GnomeAgility(id, level) }})
	RegisterRungFamily(RungFamily{Name: "crafting", Skills: []string{"crafting"},
		Build: func(id, _ string, level int) questrunner.Quest { return CraftingSpin(id, level) }})
	// The cows kill for the melee styles and Hitpoints and bury for Prayer. Strength and Defence are trained the way Attack is
	// (cowsTo sets the style); no catalogue row asks for them yet.
	RegisterRungFamily(RungFamily{Name: "cows", Skills: []string{"hitpoints", "attack", "strength", "defence", "prayer"}, Build: CowsRung})
	RegisterRungFamily(RungFamily{Name: "smithing", Skills: []string{"smithing"},
		Build: func(id, _ string, level int) questrunner.Quest { return SmithingRung(id, level) }})
	RegisterRungFamily(RungFamily{Name: "woodcutting", Skills: []string{"woodcutting"},
		Build: func(id, _ string, level int) questrunner.Quest { return WoodcuttingRung(id, level) }})
	RegisterRungFamily(RungFamily{Name: "fletching", Skills: []string{"fletching"},
		Build: func(id, _ string, level int) questrunner.Quest { return FletchingRung(id, level) }})
}
