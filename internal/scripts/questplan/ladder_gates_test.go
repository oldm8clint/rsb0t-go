package questplan

import (
	"reflect"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/levels"
)

// The account the live fleet put in front of the Fremennik Trials on 14 October 2026: Woodcutting
// 2, Fletching 1, Crafting 40, Agility 35, a melee graduate otherwise.
func fremennikAcct() Account {
	return Account{
		XP: map[string]int{
			"woodcutting": levels.XPAtLevel(2),
			"fletching":   levels.XPAtLevel(1),
			"crafting":    levels.XPAtLevel(40),
			"agility":     levels.XPAtLevel(35),
			"attack":      levels.XPAtLevel(45),
			"strength":    levels.XPAtLevel(45),
			"defence":     levels.XPAtLevel(40),
			"hitpoints":   levels.XPAtLevel(49),
			"prayer":      levels.XPAtLevel(37),
		},
		Done: map[string]bool{}, Owned: map[int]int{},
	}
}

func planIDs(p LadderPlan) []string {
	var out []string
	for _, r := range p.Rungs {
		out = append(out, r.ID)
	}
	return out
}

// The planner was never the leak: given the live levels it puts Woodcutting 40 and Fletching 25
// ahead of the Trials, and Crafting 40, which the account has, is left alone.
func TestPlanLadderWithTheLiveLevelsTrainsWoodcuttingAndFletchingBeforeFremennik(t *testing.T) {
	p := PlanLadder([]string{"fremennik"}, LadderFacts, fremennikAcct())
	if want := []string{"woodcutting40", "fletching25", "fremennik"}; !reflect.DeepEqual(planIDs(p), want) {
		t.Fatalf("plan %v, want %v:\n%s", planIDs(p), want, p.Describe(LadderFacts))
	}
	if !p.Rungs[0].Inserted || !strings.Contains(p.Rungs[0].Reason, "woodcutting 40") || !strings.Contains(p.Rungs[0].Reason, "server gate") {
		t.Errorf("the Woodcutting stop's reason: %q", p.Rungs[0].Reason)
	}
	if got := p.Rungs[1].Levels["woodcutting"]; got < 40 {
		t.Errorf("Woodcutting is %d after its stop", got)
	}
}

// With no named rung for a skill the planner inserts a generic stop (the generic rung is the
// bot's to run or refuse, which is why RunLadder must hold the quest until the level is real).
func TestPlanLadderWithNoNamedRungInsertsAGenericStop(t *testing.T) {
	facts := copyFacts(LadderFacts)
	delete(facts, "woodcutting40")
	delete(facts, "fletching25")
	p := PlanLadder([]string{"fremennik"}, facts, fremennikAcct())
	if want := []string{"train:woodcutting:40", "train:fletching:25", "fremennik"}; !reflect.DeepEqual(planIDs(p), want) {
		t.Fatalf("plan %v, want %v", planIDs(p), want)
	}
	if got := GateRungID(Gate{Skill: "woodcutting", Level: 40}); got != "woodcutting40" {
		t.Errorf("GateRungID names %q for Woodcutting 40", got)
	}
	if got := GateRungID(Gate{Skill: "woodcutting", Level: 41}); got != "train:woodcutting:41" {
		t.Errorf("GateRungID names %q for Woodcutting 41", got)
	}
}

// LadderHolds is the planner's own firstHardGate, read at run time: the account the fleet sent
// to Rellekka is held by exactly the two levels it lacks.
func TestLadderHoldsNamesTheGatesTheLiveAccountLacks(t *testing.T) {
	st := PlayerState{SkillLevels: map[string]int{"woodcutting": 2, "fletching": 1, "crafting": 40, "agility": 35}}
	holds := LadderHolds(FremennikRecord, st)
	if len(holds) != 2 {
		t.Fatalf("holds %+v, want Woodcutting and Fletching", holds)
	}
	if holds[0].Skill != "woodcutting" || holds[0].Need != 40 || holds[0].Have != 2 || holds[0].Rung != "woodcutting40" {
		t.Errorf("first hold %+v", holds[0])
	}
	if holds[1].Skill != "fletching" || holds[1].Need != 25 || holds[1].Have != 1 || holds[1].Rung != "fletching25" {
		t.Errorf("second hold %+v", holds[1])
	}
	if got := holds[0].Reason(); got != "needs Woodcutting 40 (have 2; server gate: Olaf's lyre)" {
		t.Errorf("reason %q", got)
	}
	// A boostable gate is judged on the base level: nothing in the ladder drinks a boost.
	if !holds[0].Gate.Boostable {
		t.Error("the Woodcutting gate should be the boostable one this test is about")
	}
	// At the levels, nothing; a level the state does not mention is level 0, not "fine".
	st.SkillLevels["woodcutting"], st.SkillLevels["fletching"] = 40, 25
	if holds := LadderHolds(FremennikRecord, st); len(holds) != 0 {
		t.Errorf("holds at the levels: %+v", holds)
	}
	if holds := LadderHolds(FremennikRecord, PlayerState{}); len(holds) != 4 {
		t.Errorf("a state that mentions no skill holds %d gates, want all 4", len(holds))
	}
}

// The walk to Rellekka crosses the basalt rocks, whose northbound dice rock keeps the graph's Agility 35
// (webwalk/basalt.go): below it every walk to the council and to the swaying tree ends "unreachable" (17 parks
// on 14 Oct 2026), so the Trials are held for the level and the ladder trains it first.
func TestFremennikIsHeldBelowAgility35AndTheRungTrainsIt(t *testing.T) {
	st := PlayerState{SkillLevels: map[string]int{"woodcutting": 40, "fletching": 25, "crafting": 40, "agility": 34}}
	holds := LadderHolds(FremennikRecord, st)
	if len(holds) != 1 || holds[0].Skill != "agility" || holds[0].Need != 35 || holds[0].Have != 34 || holds[0].Rung != "agility35" {
		t.Fatalf("holds %+v, want Agility 35 trained by agility35", holds)
	}
	if got := holds[0].Reason(); got != "needs Agility 35 (have 34; module gate: the basalt rocks to Rellekka)" {
		t.Errorf("reason %q", got)
	}
	st.SkillLevels["agility"] = 35
	if holds := LadderHolds(FremennikRecord, st); len(holds) != 0 {
		t.Errorf("held at Agility 35: %+v", holds)
	}
	// The planner puts the rung ahead of the Trials for an account that lacks it.
	acct := fremennikAcct()
	acct.XP["agility"] = levels.XPAtLevel(20)
	p := PlanLadder([]string{"fremennik"}, LadderFacts, acct)
	want := []string{"woodcutting40", "fletching25", "agility35", "fremennik"}
	if !reflect.DeepEqual(planIDs(p), want) {
		t.Fatalf("plan %v, want %v", planIDs(p), want)
	}
}

// Item and advised gates never hold a quest back by themselves, and a record that already states a
// level (Attack 20 for The Holy Grail) reports it itself.
func TestLadderHoldsLeavesItemAndAdvisedGatesAndRecordedLevels(t *testing.T) {
	// Horror's Smithing 34 is an item gate (nails) and waived by owned nails: not a hold.
	st := PlayerState{SkillLevels: map[string]int{"agility": 35, "prayer": 43, "smithing": 1}}
	if holds := LadderHolds(HorrorRecord, st); len(holds) != 0 {
		t.Errorf("Horror at Agility 35 and Prayer 43 is held by %+v", holds)
	}
	// Its module gate (Prayer 43) and server gate (Agility 35) are.
	holds := LadderHolds(HorrorRecord, PlayerState{SkillLevels: map[string]int{"smithing": 1}})
	var skills []string
	for _, h := range holds {
		skills = append(skills, h.Skill)
	}
	if !reflect.DeepEqual(skills, []string{"agility", "prayer"}) {
		t.Errorf("Horror holds %v, want agility and prayer", skills)
	}
	// The Grail's record states Attack 20: CheckRequirements says it, the table does not repeat it.
	if holds := LadderHolds(HolyGrailRecord, PlayerState{SkillLevels: map[string]int{"attack": 1}}); len(holds) != 0 {
		t.Errorf("The Holy Grail's Attack 20 is told twice: %+v", holds)
	}
	// An id the table does not know has nothing to hold it.
	if holds := LadderHolds(Record{ID: "nope"}, PlayerState{}); holds != nil {
		t.Errorf("unknown quest held by %+v", holds)
	}
	// Quest points: Dragon Slayer's 32 are in its record, a bare record gets the table's.
	bare := Record{ID: "dragon", Name: "Dragon Slayer"}
	h := LadderHolds(bare, PlayerState{QuestPoints: 20, SkillLevels: map[string]int{}})
	if len(h) != 1 || h[0].Skill != "" || h[0].Need != 32 || h[0].Have != 20 || !strings.Contains(h[0].Reason(), "needs 32 quest points") {
		t.Errorf("a bare Dragon Slayer record at 20 quest points: %+v", h)
	}
	if h := LadderHolds(DragonSlayerRecord, PlayerState{QuestPoints: 20}); len(h) != 0 {
		t.Errorf("Dragon Slayer's own record states the 32: %+v", h)
	}
}

// Every hard gate in the table has a named training rung, so that a ladder built from the table
// (the bot's "owner" expansion) can train it; a gate without one would be held back for ever.
// And every named rung trains the skill it is named for.
func TestEveryHardGateHasANamedTrainingRung(t *testing.T) {
	for id, f := range LadderFacts {
		for _, g := range f.Gates {
			if !g.Hard() {
				continue
			}
			rung := GateRungID(g)
			rf, ok := LadderFacts[rung]
			if !ok || rf.TrainSkill != strings.ToLower(g.Skill) || rf.TrainLevel != g.Level {
				t.Errorf("%s needs %s %d (%s) and has no named training rung (GateRungID says %q)", id, g.Skill, g.Level, g.Label(), rung)
			}
		}
	}
}
