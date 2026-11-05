package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// Lane N4 (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 F, with the completeness critic's item 19): a kit that waits for a
// level ends the quest's run and parks it on the need, so the stint runs outside the quest's fleet-wide slot.

// trainKit is Nature Spirit's silver ore in miniature: one line, one skill source the account is below, marked Train.
func trainKit() []questplan.KitLine {
	return []questplan.KitLine{{ID: 442, Name: "Silver ore", Qty: 1, Sources: []questplan.Source{{
		Kind: questplan.SourceSkill, Skill: "woodcutting", Level: 30, Method: "chop-logs", Tile: tile.Tile{X: 2724, Z: 3500}, Train: true}}}}
}

// worked counts how often the skill source was worked by any test provisioner: a train phase must never work it.
var worked int

// emptyBankProvisioner reads an empty bank; its skill source counts the times it is worked and fails.
func emptyBankProvisioner() *Provisioner {
	worked = 0
	p := &Provisioner{Methods: map[string]SkillMethod{"chop-logs": func(*Deps, questplan.SourceTask) bool {
		worked++
		return false
	}}}
	p.bankVisitFn = func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
		p.Bank.Note(map[int]int{})
		return true
	}
	return p
}

func TestAProvisionRoundThatWaitsForALevelFailsWithTheNeedAndNeverWorksIt(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, map[string]int{"woodcutting": 1})
	p := emptyBankProvisioner()

	ok, why := p.Provision(e.D, "silver", trainKit(), nil, tile.Tile{X: 3092, Z: 3245})
	if ok || !strings.HasPrefix(why, "needs:woodcutting:30 ") {
		t.Fatalf("ok=%v why=%q: want a failure under the stable code", ok, why)
	}
	if !p.LastBlocked {
		t.Error("a caller that does not read the train need must see the round as blocked, as it always was")
	}
	if worked != 0 {
		t.Errorf("the skill source was worked %d times below its level", worked)
	}
	need := p.TakeTrain()
	if len(need) != 1 || need[0].Skill != "woodcutting" || need[0].Level != 30 || need[0].From != 1 {
		t.Fatalf("train needs %+v", need)
	}
	if again := p.TakeTrain(); again != nil {
		t.Errorf("the need was read twice: %+v", again)
	}
	if !sink.has("TRAIN: woodcutting 30 for 1 Silver ore") {
		t.Errorf("the plan line does not say it waits for a stint: %v", sink.lines)
	}

	// A round that ends any other way leaves no need behind.
	p2 := emptyBankProvisioner()
	e2 := fakeEngineAt(&logSink{}, map[string]int{"woodcutting": 30})
	p2.Provision(e2.D, "silver", trainKit(), nil, tile.Tile{})
	if p2.LastTrain != nil || worked == 0 {
		t.Errorf("at the level the source is worked and a failure leaves no need: train %+v, worked %d", p2.LastTrain, worked)
	}
}

func TestACatalogueGivenToTheProvisionerDecidesWhatIsTrainable(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, map[string]int{"woodcutting": 1})
	p := emptyBankProvisioner()
	none := questplan.NewCatalogue()
	p.Trainers = &none
	ok, why := p.Provision(e.D, "silver", trainKit(), nil, tile.Tile{})
	if ok || p.LastTrain != nil || !p.LastBlocked || !strings.Contains(why, "no trainer for woodcutting 30") {
		t.Fatalf("an empty catalogue must block with the gap named: ok=%v train=%v blocked=%v why=%q", ok, p.LastTrain, p.LastBlocked, why)
	}
}

// trainQuest provisions the train kit on every decide and counts the decides.
func trainQuest(id string, decides *int) Quest {
	return Quest{Module: questplan.Module{
		Record: questplan.Record{ID: id, Name: id},
		Decide: func(questplan.Snapshot) questplan.Step {
			*decides++
			return questplan.Step{Kind: questplan.StepProvision, KitName: "silver", Kit: trainKit(), BankSet: true}
		},
	}}
}

func TestAQuestWhoseKitWaitsForALevelParksOnTheNeedAtOnce(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, map[string]int{"woodcutting": 1})
	e.Prov = emptyBankProvisioner()
	decides := 0

	res := e.Run(trainQuest("spirit", &decides), time.Minute)
	if res.Outcome != OutcomeParked || !strings.HasPrefix(res.Reason, "needs:woodcutting:30 ") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, sink.lines)
	}
	if decides != 1 || worked != 0 {
		t.Errorf("decide ran %d times, the source %d: the quest kept provisioning for a level instead of parking", decides, worked)
	}
	if !sink.has("parking for 2h0m0s: needs:woodcutting:30") {
		t.Errorf("the log does not say what it waits for: %v", sink.lines)
	}
	if parked, why := e.Store.Parked("spirit", time.Now().Add(questplan.TrainParkFor-time.Minute)); !parked || !strings.HasPrefix(why, "needs:woodcutting:30") {
		t.Errorf("not parked for the stint: %v %q", parked, why)
	}
	if parked, _ := e.Store.Parked("spirit", time.Now().Add(questplan.TrainParkFor+time.Minute)); parked {
		t.Error("parked past the train park")
	}
}

// A quest at the level provisions and works the source as it always did.
func TestAQuestAtTheLevelWorksTheSourceAndParksOnNothing(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, map[string]int{"woodcutting": 30})
	e.Prov = emptyBankProvisioner()
	decides := 0
	res := e.Run(trainQuest("spirit", &decides), time.Minute)
	if worked == 0 {
		t.Errorf("the source was never worked at the level: %v", sink.lines)
	}
	if res.Outcome == OutcomeParked && strings.HasPrefix(res.Reason, questplan.NeedsParkPrefix) {
		t.Errorf("parked on a need the account does not have: %q", res.Reason)
	}
}

// A train need left over from before a run is not that run's: the first step that fails for another reason does not park on it.
func TestAStaleTrainNeedDoesNotParkALaterFailure(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, map[string]int{"woodcutting": 1})
	p := emptyBankProvisioner()
	p.LastTrain = []questplan.TrainNeed{{Skill: "mining", Level: 20, Line: questplan.KitLine{Name: "Ore"}}}
	e.Prov = p
	decides := 0
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "other", Name: "other"},
		Decide: func(questplan.Snapshot) questplan.Step {
			decides++
			return questplan.Step{Kind: questplan.StepCustom, Custom: "fails"}
		},
	}, Customs: map[string]CustomFunc{"fails": func(*Engine, questplan.Step) bool { return false }}}
	res := e.Run(q, 3*time.Second)
	if res.Outcome == OutcomeParked && strings.HasPrefix(res.Reason, questplan.NeedsParkPrefix) {
		t.Fatalf("parked on a stale need: %q", res.Reason)
	}
}

// THE POINT OF THE DESIGN (the critic's item 19): a crowded quest holds its fleet-wide slot while it runs. A kit that waits for a
// level must end the run, so the slot is free for the next account while the stint is worked, and the park ends the moment the level is there.
func TestATrainParkReleasesTheSlotAndEndsWhenTheLevelIsReached(t *testing.T) {
	id := questplan.PriestInPerilID
	other := &Deps{}
	defer releaseQuest(id, other)

	sink := &logSink{}
	e := fakeEngineAt(sink, map[string]int{"woodcutting": 1})
	e.Prov = emptyBankProvisioner()
	decides, otherHeldDuringRun := 0, true
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: id, Name: id},
		Decide: func(questplan.Snapshot) questplan.Step {
			decides++
			if decides == 1 {
				otherHeldDuringRun = tryHoldQuest(id, other, time.Now()) // the slot is the running account's
			}
			return questplan.Step{Kind: questplan.StepProvision, KitName: "silver", Kit: trainKit(), BankSet: true}
		},
	}}

	res := e.RunLadder([]Quest{q}, 30*time.Minute, time.Hour)
	if otherHeldDuringRun {
		t.Fatal("another account took the slot while the quest held it: the test proves nothing")
	}
	if res.Outcome != LadderStuck || len(res.Blocked[id]) == 0 || !strings.Contains(res.Blocked[id][0], "needs:woodcutting:30") {
		t.Fatalf("outcome %s blocked %v (%v)", res.Outcome, res.Blocked, sink.lines)
	}
	if !tryHoldQuest(id, other, time.Now()) {
		t.Fatal("the quest parked on a level but kept its fleet-wide slot: the stint would run inside it")
	}
	releaseQuest(id, other)
	if decides != 1 {
		t.Fatalf("decides %d after the first ladder call", decides)
	}

	// Below the level the park stands.
	st := e.D.Session.State()
	seedSkill(st, skillStat(t, "woodcutting"), 29)
	if res := e.RunLadder([]Quest{q}, 30*time.Minute, time.Hour); res.Outcome != LadderStuck || decides != 1 {
		t.Fatalf("at 29 the park should stand: %s, %d decides (%v)", res.Outcome, decides, sink.lines)
	}
	// At the level it ends at once, long before its two hours do.
	seedSkill(st, skillStat(t, "woodcutting"), 30)
	e.RunLadder([]Quest{q}, 30*time.Minute, time.Hour)
	if decides < 2 || !sink.has("woodcutting 30 is reached, so the park waiting on it ends") {
		t.Errorf("the park did not end at the level: %d decides (%v)", decides, sink.lines)
	}
	releaseQuest(id, e.D)
}
