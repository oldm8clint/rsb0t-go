package questmods

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

func TestTheRungFamiliesRegisterWithoutProblems(t *testing.T) {
	if p := RungFamilyProblems(); len(p) != 0 {
		t.Errorf("rung family problems: %v", p)
	}
	want := []string{"cows", "crafting", "fletching", "gnome", "smithing", "woodcutting"}
	if got := RungFamilyNames(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("families %v, want %v", got, want)
	}
}

// A rung of any level in any family is a quest under its own id with a Done of its own (a rung has no quest tab), and it stops at its
// level: at the level the module says Done, one level below it does not. That is the whole difference between the generic stop to
// Crafting 19 and the named crafting40, which is the same code with a bigger number.
func TestEveryFamilyBuildsARungThatStopsAtItsLevel(t *testing.T) {
	for _, family := range RungFamilyNames() {
		skills, _ := RungFamilySkills(family)
		for _, skill := range skills {
			for _, level := range []int{2, 19, 25, 40, 99} {
				id := fmt.Sprintf("train:%s:%d", skill, level)
				q, err := BuildRung(family, id, skill, level)
				if err != nil {
					t.Errorf("%s %s %d: %v", family, skill, level, err)
					continue
				}
				if q.ID() != id || q.Module.Record.ID != id {
					t.Errorf("%s %s %d: the quest is %q, want %q", family, skill, level, q.ID(), id)
				}
				if q.Done == nil {
					t.Errorf("%s: a rung with no Done would read its quest tab, which it has not got", id)
				}
				if q.Module.Decide == nil {
					t.Errorf("%s: no decide", id)
					continue
				}
				at := questplan.Snapshot{Levels: map[string]int{skill: level}}
				if step := q.Module.Decide(at); step.Kind != questplan.StepDone {
					t.Errorf("%s at %s %d decides %v %q, want done", id, skill, level, step.Kind, step.Reason)
				}
				below := questplan.Snapshot{Levels: map[string]int{skill: level - 1}}
				if step := q.Module.Decide(below); step.Kind == questplan.StepDone {
					t.Errorf("%s at %s %d (a level short) decides done", id, skill, level-1)
				}
			}
		}
	}
}

// The named rungs are the generic ones at the level they have always had: the builders agree with the constructors the registry replaces.
func TestTheFamiliesBuildTheNamedRungsAsTheyAlwaysWere(t *testing.T) {
	for _, c := range []struct {
		family, id, skill string
		level             int
		custom            string
	}{
		{"gnome", "agility25", "agility", 25, "gnome-laps"},
		{"crafting", "crafting40", "crafting", 40, "craft-spin"},
		{"cows", "cows20", "hitpoints", 20, "cows-hitpoints"},
		{"cows", "attack20", "attack", 20, "cows-attack"},
		{"smithing", "smithing34", "smithing", 34, ""},
		{"woodcutting", "woodcutting40", "woodcutting", 40, ""},
		{"fletching", "fletching25", "fletching", 25, ""},
	} {
		q, err := BuildRung(c.family, c.id, c.skill, c.level)
		if err != nil {
			t.Errorf("%s: %v", c.id, err)
			continue
		}
		below := questplan.Snapshot{Levels: map[string]int{c.skill: c.level - 1}}
		if c.custom != "" {
			if step := q.Module.Decide(below); step.Kind != questplan.StepCustom || step.Custom != c.custom {
				t.Errorf("%s decides %v %q, want custom %s", c.id, step.Kind, step.Custom, c.custom)
			}
			if _, ok := q.Customs[c.custom]; !ok {
				t.Errorf("%s: no custom step %s", c.id, c.custom)
			}
		}
	}
	// Prayer is the cows family's other half: the bones loop first, the cows when the bones are out.
	q, err := BuildRung("cows", "train:prayer:33", "prayer", 33)
	if err != nil {
		t.Fatal(err)
	}
	if step := q.Module.Decide(questplan.Snapshot{Levels: map[string]int{"prayer": 32}}); step.Custom != questplan.CustomPrayerBones {
		t.Errorf("a prayer rung begins with %q, want the bones loop", step.Custom)
	}
	for _, name := range []string{questplan.CustomPrayerBones, questplan.CustomPrayerCows, "cows-prayer"} {
		if _, ok := q.Customs[name]; !ok {
			t.Errorf("a prayer rung has no %s step", name)
		}
	}
	// A generic Prayer rung never asks the mule for bones: only prayer31 and prayerkq do (the mule's bones are one finite stock).
	if questplan.PrayerRungAsksMule("train:prayer:33") || questplan.PrayerRungAsksMule("train:prayer:31") {
		t.Error("a generic prayer rung may ask the mule for bones")
	}
}

func TestBuildRungRefusesWhatItCannotBuild(t *testing.T) {
	for _, c := range []struct {
		family, id, skill string
		level             int
		want              string
	}{
		{"mining", "train:mining:20", "mining", 20, "no rung family"},
		{"gnome", "train:mining:20", "mining", 20, "trains [agility]"},
		{"cows", "x", "smithing", 20, "trains"},
		{"gnome", "train:agility:1", "agility", 1, "not 2 to 99"},
		{"gnome", "train:agility:100", "agility", 100, "not 2 to 99"},
		{"gnome", "  ", "agility", 20, "needs an id"},
	} {
		if _, err := BuildRung(c.family, c.id, c.skill, c.level); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("BuildRung(%s, %q, %s, %d) = %v, want an error mentioning %q", c.family, c.id, c.skill, c.level, err, c.want)
		}
	}
	// The skill is read lower case: a gate's own spelling ("Crafting") builds.
	if _, err := BuildRung("crafting", "train:crafting:19", " Crafting ", 19); err != nil {
		t.Errorf("a capitalised skill: %v", err)
	}
}

func TestARungFamilyThatIsMalformedOrTwiceRegisteredIsListedNotPanicked(t *testing.T) {
	build := func(id, _ string, level int) questrunner.Quest { return GnomeAgility(id, level) }
	r := newRungRegistry()
	r.register(RungFamily{Name: "ok", Skills: []string{"agility"}, Build: build})
	for _, bad := range []RungFamily{
		{Name: "", Skills: []string{"agility"}, Build: build},
		{Name: "Upper", Skills: []string{"agility"}, Build: build},
		{Name: "a b", Skills: []string{"agility"}, Build: build},
		{Name: "a:b", Skills: []string{"agility"}, Build: build},
		{Name: "nobuilder", Skills: []string{"agility"}},
		{Name: "noskills", Build: build},
		{Name: "ok", Skills: []string{"agility"}, Build: build},
	} {
		r.register(bad)
	}
	if got := r.names(); len(got) != 1 || got[0] != "ok" {
		t.Errorf("families %v, want only ok", got)
	}
	if got := r.problemList(); len(got) != 7 {
		t.Errorf("%d problems, want 7: %v", len(got), got)
	}
	if _, err := r.build("ok", "train:agility:20", "agility", 20); err != nil {
		t.Errorf("the good family: %v", err)
	}
}
