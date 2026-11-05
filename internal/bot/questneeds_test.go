package bot

import (
	"reflect"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questmods"
	"rsb0t-go/internal/scripts/questplan"
)

// The requirement model (questplan/needs.go) reads quest records from questplan.RecordOf, which lists the records questplan owns
// and takes the one it cannot import (Waterfall's) from that package's init. This holds the registry to the quests the bot runs:
// a record added, renamed or changed in a quest's file with no line in questplan/records.go fails here, not in the fleet.
func TestTheRecordRegistryIsTheFactoriesRecords(t *testing.T) {
	for id, f := range QuestFactories {
		if questplan.LadderFacts[id].TrainSkill != "" || id == questplan.KQPrayerRungID {
			continue // a training rung is not a quest
		}
		q := f(nil)
		rec, ok := questplan.RecordOf(id)
		if !ok {
			if id == questmods.BarcrawlID && len(q.Module.Record.Requirements.Skills) == 0 && len(q.Module.Record.Requirements.Quests) == 0 {
				continue // a miniquest whose record declares no requirement at all
			}
			t.Errorf("quest %q has a factory and no record in questplan.RecordOf", id)
			continue
		}
		if !reflect.DeepEqual(rec, q.Module.Record) {
			t.Errorf("quest %q: the registry's record is not the factory's\n registry: %+v\n factory:  %+v", id, rec, q.Module.Record)
		}
	}
	for _, id := range questplan.RecordIDs() {
		if _, ok := QuestFactories[id]; !ok {
			t.Errorf("questplan.RecordOf knows %q, which has no quest factory", id)
		}
	}
}

// Record and Facts agree for every quest the bot runs, Waterfall's record included (questplan cannot see it).
func TestEveryFactoryQuestsRecordAgreesWithItsFacts(t *testing.T) {
	checked := 0
	for id, f := range QuestFactories {
		facts, ok := questplan.LadderFacts[id]
		if !ok || facts.TrainSkill != "" || id == questplan.KQPrayerRungID {
			continue
		}
		checked++
		for _, why := range questplan.RecordFactsDisagreements(id, facts, f(nil).Module.Record) {
			t.Error(why)
		}
	}
	if checked < 40 {
		t.Errorf("only %d quests were checked", checked)
	}
	if rec, ok := questplan.RecordOf("waterfall"); !ok || len(rec.Requirements.Skills) == 0 {
		t.Errorf("Waterfall's record did not register itself with questplan: %+v", rec)
	}
}

// The three exception lists are exact over every record, Waterfall's included (questplan's own test cannot see it).
func TestTheRecordFactsExceptionListsAreExact(t *testing.T) {
	for _, p := range questplan.ExceptionProblems(questplan.LadderFacts, questplan.RecordOf) {
		t.Error(p)
	}
	if _, ok := questplan.RecordOf("waterfall"); !ok {
		t.Error("Waterfall's record is not registered, so its exceptions went unchecked")
	}
}

// A trainer row's rungs are rungs the bot can build: a row that names a rung with no factory would promise a stint nothing can run.
func TestEveryTrainerRungHasAFactory(t *testing.T) {
	for _, tr := range questplan.DefaultCatalogue().Trainers() {
		for _, id := range tr.RungIDs() {
			f, ok := QuestFactories[id]
			if !ok {
				t.Errorf("trainer %s names rung %s, which has no quest factory", tr.ID, id)
				continue
			}
			if got := f(nil).ID(); got != id {
				t.Errorf("trainer %s: the factory for %s builds %q", tr.ID, id, got)
			}
		}
	}
}

// The planner's table covers every quest the bot can run: a registered quest the facts do not know is invisible to the needs
// engine, which is how a quest ends up with requirements nobody checks.
func TestTheNeedsTableKnowsEveryRegisteredQuest(t *testing.T) {
	tab := questplan.DefaultNeedTable()
	known := map[string]bool{}
	for _, id := range tab.Quests() {
		known[id] = true
	}
	for id := range QuestFactories {
		if questplan.LadderFacts[id].TrainSkill != "" || id == questplan.KQPrayerRungID {
			continue
		}
		if !known[id] {
			t.Errorf("quest %q is registered and the needs table has no row for it", id)
		}
	}
}

// Every hard gate's rung exists as a trainer row or as a named gap: the superset the plan asks of
// TestEveryHardGateHasARungThatProducesIt (which still holds the factories of the gates' named rungs).
func TestEveryHardGateIsCoveredByARowOrANamedGap(t *testing.T) {
	cat := questplan.DefaultCatalogue()
	for id, f := range questplan.LadderFacts {
		for _, g := range f.Gates {
			if g.Kind == questplan.GateAdvised {
				continue
			}
			if u := cat.FirstUncovered(strings.ToLower(g.Skill), 1, g.Level); u != 0 {
				if _, ok := questplan.GapFor(g.Skill, u); !ok {
					t.Errorf("%s: gate %s %d (%s) runs past the catalogue at level %d and no gap names it", id, g.Skill, g.Level, g.Kind, u)
				}
			}
		}
	}
}
