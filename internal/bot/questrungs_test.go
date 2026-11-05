package bot

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questmods"
	"rsb0t-go/internal/scripts/questplan"
)

// Generic training rungs (lane N2): any id a gate or a planned stint can carry builds, to its own level, wherever a trainer row reaches the
// level; a level no row reaches is a named gap and never an id with a missing factory.

// Every level a rung row reaches has a factory under the id the planner writes for it (the row's named rung, else the generic stop), and the
// quest it builds carries that id and a Done of its own: the "no factory" a hard gate used to log is not a state the catalogue can reach.
func TestEveryLevelATrainerRowReachesHasAFactory(t *testing.T) {
	built := 0
	for _, tr := range questplan.DefaultCatalogue().Trainers() {
		if tr.Via != questplan.ViaRung {
			continue
		}
		for level := tr.From + 1; level <= tr.To; level++ {
			ids := []string{tr.GenericRungID(level), strings.ToLower(tr.Skill) + strconv.Itoa(level)}
			if named, ok := tr.RungFor(level); ok {
				ids = append(ids, named)
			}
			for _, id := range ids {
				f, why := QuestFactoryWhy(id)
				if f == nil {
					t.Errorf("trainer %s: %s has no factory: %s", tr.ID, id, why)
					continue
				}
				q := f(nil)
				if q.ID() != id || q.Done == nil || q.Module.Decide == nil {
					t.Errorf("trainer %s: the factory for %s builds %q (done %v)", tr.ID, id, q.ID(), q.Done != nil)
				}
				built++
			}
		}
	}
	if built < 400 {
		t.Errorf("only %d rung ids were built; the catalogue's rung rows cover far more levels than that", built)
	}
}

// The planner's stints name a rung (Stint.Rung) and a trainer: whatever the planner can write is built with the row it was planned for, and a
// registered rung is its registered factory.
func TestAPlannedStintBuildsWithItsOwnTrainer(t *testing.T) {
	for _, tr := range questplan.DefaultCatalogue().Trainers() {
		if tr.Via != questplan.ViaRung {
			continue
		}
		for level := tr.From + 1; level <= tr.To; level++ {
			id := tr.GenericRungID(level)
			if named, ok := tr.RungFor(level); ok {
				id = named
			}
			q, err := StintQuest(tr, id, level, nil)
			if err != nil || q.ID() != id {
				t.Errorf("trainer %s: StintQuest(%s) = %q, %v", tr.ID, id, q.ID(), err)
			}
		}
	}
	// A loop row is not stinted.
	loop, _ := questplan.DefaultCatalogue().ByID("stage5-magic")
	if _, err := StintQuest(loop, "train:magic:50", 50, nil); err == nil || !strings.Contains(err.Error(), "not a rung") {
		t.Errorf("a stint of a loop: %v", err)
	}
	// The row decides, not the catalogue's first: a generic id built for another family's row is that family's rung.
	cows, _ := questplan.DefaultCatalogue().ByID("cows-attack")
	q, err := StintQuest(cows, "train:attack:25", 25, nil)
	if err != nil || q.ID() != "train:attack:25" {
		t.Fatalf("StintQuest = %q %v", q.ID(), err)
	}
	if _, ok := q.Customs["cows-attack"]; !ok {
		t.Errorf("the cows-attack row builds a quest without its cows step: %v", reflect.ValueOf(q.Customs).MapKeys())
	}
}

// Every row names a family the bot can build for its skill: a row that does not promises a stint nothing can run.
func TestEveryRungRowNamesAFamilyThatTrainsItsSkill(t *testing.T) {
	for _, tr := range questplan.DefaultCatalogue().Trainers() {
		if tr.Via != questplan.ViaRung {
			continue
		}
		skills, ok := questmods.RungFamilySkills(tr.Family)
		if !ok {
			t.Errorf("trainer %s names rung family %q, which questmods does not register (have %v)", tr.ID, tr.Family, questmods.RungFamilyNames())
			continue
		}
		found := false
		for _, s := range skills {
			found = found || s == tr.Skill
		}
		if !found {
			t.Errorf("trainer %s trains %s with family %s, which trains %v", tr.ID, tr.Skill, tr.Family, skills)
		}
	}
	if p := questmods.RungFamilyProblems(); len(p) != 0 {
		t.Errorf("rung family problems: %v", p)
	}
}

// A registered rung is its registered factory, whatever the catalogue would build: prayerkq's level is a setting, and the named rungs run as
// they always did (the mule-asking Prayer rungs among them).
func TestQuestFactoryForPrefersTheRegisteredRung(t *testing.T) {
	for id := range QuestFactories {
		f, ok := QuestFactoryFor(strings.ToUpper(" " + id + " "))
		if !ok || f(nil).ID() != id {
			t.Errorf("QuestFactoryFor(%q) = %v", id, ok)
		}
	}
	f, ok := QuestFactoryFor(questplan.KQPrayerRungID)
	if !ok {
		t.Fatal("prayerkq has no factory")
	}
	if q := f(nil); q.ID() != questplan.KQPrayerRungID {
		t.Errorf("prayerkq builds %q", q.ID())
	}
}
