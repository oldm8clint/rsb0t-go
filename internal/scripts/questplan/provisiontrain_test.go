package questplan

import (
	"strings"
	"testing"
	"time"
)

// Lane N4 (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 F): the provisioner answers PhaseTrain for a kit whose only
// way left is a skill the account can be trained in, instead of PhaseBlocked, and never anywhere the plan would be wrong to.

const (
	trainLogs  = 1511
	trainOre   = 442
	trainShark = 385
)

// A Woodcutting 30 source the account is below, which the catalogue trains (seers-chop covers 1 to 39).
func chopLine(train bool) KitLine {
	return KitLine{ID: trainLogs, Name: "Logs", Qty: 5, Sources: []Source{
		{Kind: SourceSkill, Skill: "woodcutting", Level: 30, Method: MethodChopLogs, Train: train}}}
}

func emptyBankState(levels map[string]int) ProvisionState {
	return ProvisionState{BankKnown: true, Bank: map[int]int{}, FreeSlots: 28, Levels: levels}
}

func TestAKitWhoseOnlySourceIsATrainableSkillYieldsATrainPhaseNotBlocked(t *testing.T) {
	st := emptyBankState(map[string]int{"woodcutting": 1})

	p := PlanProvision([]KitLine{chopLine(true)}, st)
	if p.Phase != PhaseTrain || len(p.Blocked) != 0 || len(p.Tasks) != 0 {
		t.Fatalf("a trainable skill source is a train phase: %+v", p)
	}
	if len(p.Train) != 1 {
		t.Fatalf("train needs %+v", p.Train)
	}
	n := p.Train[0]
	if n.Skill != "woodcutting" || n.Level != 30 || n.From != 1 || n.Qty != 5 || n.Line.ID != trainLogs || n.Trainer != "seers-chop" {
		t.Errorf("need %+v: want woodcutting 1 to 30 for 5 logs via seers-chop", n)
	}
	if !strings.Contains(p.Summary(), "TRAIN: woodcutting 30 for 5 Logs") {
		t.Errorf("the summary does not say it is waiting for a stint: %s", p.Summary())
	}

	// The same kit with the source not marked is what it always was.
	if b := PlanProvision([]KitLine{chopLine(false)}, st); b.Phase != PhaseBlocked || len(b.Train) != 0 {
		t.Errorf("an unmarked source below its level must stay blocked: %+v", b)
	}
}

func TestTheTrainPhaseIsTheLastResort(t *testing.T) {
	st := emptyBankState(map[string]int{"woodcutting": 1})
	st.Pack = map[int]int{CoinsID: 100}
	shop := Source{Kind: SourceShop, Shop: LumbridgeStore, Price: 2}

	// A shop that sells the item is not bypassed by hours of training: the item rule's skill rung comes before the shop in
	// the order, but a skill the account is below the level of is a stint, and a shop is a walk.
	line := chopLine(true)
	line.Sources = append(line.Sources, shop)
	if p := PlanProvision([]KitLine{line}, st); p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceShop || len(p.Train) != 0 {
		t.Errorf("a line with a shop must buy, not train: %+v", p)
	}

	// Once the shop failed this round, nothing is left but the level.
	st.Exhausted = map[string]bool{SourceKey(line, shop): true}
	if p := PlanProvision([]KitLine{line}, st); p.Phase != PhaseTrain {
		t.Errorf("with the shop failed the kit waits for the level: %+v", p)
	}

	// The bank and the mule answer first and train nothing.
	bank := emptyBankState(map[string]int{"woodcutting": 1})
	bank.Bank = map[int]int{trainLogs: 5}
	if p := PlanProvision([]KitLine{chopLine(true)}, bank); p.Phase != PhaseBank || len(p.Train) != 0 {
		t.Errorf("a banked line is withdrawn, not trained for: %+v", p)
	}
	mule := emptyBankState(map[string]int{"woodcutting": 1})
	mule.MuleKnown, mule.Mule = true, map[int]int{trainLogs: 100}
	if p := PlanProvision([]KitLine{chopLine(true)}, mule); p.Phase != PhaseMule || len(p.Train) != 0 {
		t.Errorf("a mule that holds the line is asked, not trained for: %+v", p)
	}
	// An unread bank is read before anything is judged, training included.
	if p := PlanProvision([]KitLine{chopLine(true)}, ProvisionState{FreeSlots: 28, Levels: map[string]int{"woodcutting": 1}}); p.Phase != PhaseBank || len(p.Train) != 0 {
		t.Errorf("an unread bank is read first: %+v", p)
	}
}

func TestOtherLinesAreWorkedBeforeTheKitWaitsForALevel(t *testing.T) {
	st := emptyBankState(map[string]int{"woodcutting": 1})
	st.Pack = map[int]int{CoinsID: 100}
	hammer := ToolLine(ToolHammer, Snapshot{})
	kit := []KitLine{chopLine(true), hammer}

	p := PlanProvision(kit, st)
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Line.ID != ObjHammer {
		t.Fatalf("the hammer is fetched first: %+v", p)
	}
	if len(p.Train) != 1 {
		t.Errorf("the plan should already know the logs wait for a level: %+v", p.Train)
	}
	st.Pack[ObjHammer] = 1
	if p := PlanProvision(kit, st); p.Phase != PhaseTrain {
		t.Errorf("with the hammer held only the level is left: %+v", p)
	}
}

func TestAKitWithAnotherBlockedLineDoesNotTrainForNothing(t *testing.T) {
	st := emptyBankState(map[string]int{"woodcutting": 1})
	dead := KitLine{ID: trainShark, Name: "Shark", Qty: 3} // a line no source covers
	p := PlanProvision([]KitLine{chopLine(true), dead}, st)
	if p.Phase != PhaseBlocked || len(p.Blocked) != 1 || !strings.Contains(p.Blocked[0], "Shark") {
		t.Fatalf("a kit that cannot be completed must not send the account to train: %+v", p)
	}
	// The coins a shop task is short of are the same: money is not a level.
	poor := emptyBankState(map[string]int{"woodcutting": 1})
	hammer := ToolLine(ToolHammer, Snapshot{})
	if p := PlanProvision([]KitLine{chopLine(true), hammer}, poor); p.Phase == PhaseTrain {
		t.Errorf("a shop the account cannot pay for is blocked, not a reason to train: %+v", p)
	}
}

func TestAnOptionalLineNeverAsksForTraining(t *testing.T) {
	st := emptyBankState(map[string]int{"woodcutting": 1})
	l := chopLine(true)
	l.Optional = true
	p := PlanProvision([]KitLine{l}, st)
	if p.Phase != PhaseSatisfied || len(p.Train) != 0 {
		t.Fatalf("an optional line is taken when a source has it and never waited for: %+v", p)
	}
}

func TestAMetLevelIsASourceNotATrainNeed(t *testing.T) {
	st := emptyBankState(map[string]int{"woodcutting": 30})
	p := PlanProvision([]KitLine{chopLine(true)}, st)
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Method != MethodChopLogs || len(p.Train) != 0 {
		t.Fatalf("at the level the skill source is worked: %+v", p)
	}
}

func TestAnUnreadLevelTrainsFromOne(t *testing.T) {
	p := PlanProvision([]KitLine{chopLine(true)}, ProvisionState{BankKnown: true, Bank: map[int]int{}, FreeSlots: 28})
	if p.Phase != PhaseTrain || p.Train[0].From != 1 {
		t.Fatalf("an unread level is taken as level 1: %+v", p)
	}
}

// A skill no trainer covers is a named gap, never a training that cannot happen (plan 3.1, rule 6).
func TestASourceNoTrainerCoversIsBlockedWithTheGapNamed(t *testing.T) {
	line := KitLine{ID: trainOre, Name: "Ore", Qty: 1, Sources: []Source{
		{Kind: SourceSkill, Skill: "runecraft", Level: 20, Method: "craft-runes", Train: true}}}
	p := PlanProvision([]KitLine{line}, emptyBankState(map[string]int{"runecraft": 1}))
	if p.Phase != PhaseBlocked || len(p.Train) != 0 || len(p.Blocked) != 1 {
		t.Fatalf("no trainer, no train phase: %+v", p)
	}
	for _, want := range []string{"craft-runes needs runecraft 20 (have 1)", "no trainer for runecraft 20", "no quest asks for Runecrafting"} {
		if !strings.Contains(p.Blocked[0], want) {
			t.Errorf("the blocked reason lacks %q: %s", want, p.Blocked[0])
		}
	}
}

// A catalogue that stops short of the level blocks at the first uncovered level, whatever the default one covers.
func TestACatalogueThatStopsShortBlocksAtThePlaceItStops(t *testing.T) {
	short := NewCatalogue(TrainerSpec{
		ID: "test-chop", Skill: "woodcutting", From: 1, To: 12, Risk: R0, Via: ViaRung, Family: "woodcutting",
		EstXPPerHour: 10000, MinStint: 20 * time.Minute, MaxStint: time.Hour, Source: "test",
	})
	st := emptyBankState(map[string]int{"woodcutting": 1})
	st.Trainers = &short
	p := PlanProvision([]KitLine{chopLine(true)}, st)
	if p.Phase != PhaseBlocked || !strings.Contains(p.Blocked[0], "no trainer for woodcutting 30 (no row trains it from level 12)") {
		t.Fatalf("the hole at 12 should block: %+v", p)
	}
	// The same catalogue at an account past the hole's start is still short of 30.
	st.Levels = map[string]int{"woodcutting": 11}
	if p := PlanProvision([]KitLine{chopLine(true)}, st); p.Phase != PhaseBlocked {
		t.Errorf("level 11 still meets level 12 uncovered: %+v", p)
	}
	// A catalogue that covers the whole stretch trains.
	full := NewCatalogue(TrainerSpec{
		ID: "test-chop-all", Skill: "woodcutting", From: 1, To: 60, Risk: R0, Via: ViaRung, Family: "woodcutting",
		EstXPPerHour: 10000, MinStint: 20 * time.Minute, MaxStint: time.Hour, Source: "test",
	})
	st.Trainers = &full
	st.Levels = map[string]int{"woodcutting": 11}
	if p := PlanProvision([]KitLine{chopLine(true)}, st); p.Phase != PhaseTrain || p.Train[0].Trainer != "test-chop-all" || p.Train[0].From != 11 {
		t.Errorf("a covering catalogue should train from 11: %+v", p)
	}
}

// A loop row (the Stage 5 combat trainer) covers a need without a stint; the quest parks and the loop raises the level.
func TestALoopRowCoversAKitsNeedToo(t *testing.T) {
	line := KitLine{ID: trainOre, Name: "Bones", Qty: 1, Sources: []Source{
		{Kind: SourceSkill, Skill: "attack", Level: 20, Method: MethodDemonBones, Train: true}}}
	p := PlanProvision([]KitLine{line}, emptyBankState(map[string]int{"attack": 1}))
	if p.Phase != PhaseTrain || p.Train[0].Trainer == "" {
		t.Fatalf("attack is trained by a row (the combat loop or the cows rung): %+v", p)
	}
}

func TestValidateKitLineRefusesAMisplacedTrainFlag(t *testing.T) {
	for name, src := range map[string]Source{
		"a shop":            {Kind: SourceShop, Shop: LumbridgeStore, Price: 1, Train: true},
		"a ground spawn":    {Kind: SourceGround, Tile: RimmingtonPickaxe, Train: true},
		"a skill, no level": {Kind: SourceSkill, Skill: "mining", Method: "mine-silver", Train: true},
	} {
		if why := ValidateKitLine(KitLine{ID: 1, Name: "x", Qty: 1, Sources: []Source{src}}); why == "" {
			t.Errorf("%s marked Train was accepted", name)
		}
	}
	if why := ValidateKitLine(chopLine(true)); why != "" {
		t.Errorf("a well-formed Train source: %s", why)
	}
}

func TestTheNatureSpiritSilverOreIsTrainableForMining(t *testing.T) {
	var found bool
	step := sickleStep(Snapshot{InvIDs: map[int]int{ObjSickleMould: 1}, Levels: map[string]int{"mining": 1}}, nil)
	for _, l := range step.Kit {
		if l.ID != ObjSilverOre {
			continue
		}
		found = true
		if len(l.Sources) != 1 || !l.Sources[0].Train || l.Sources[0].Skill != "mining" || l.Sources[0].Level != 20 {
			t.Errorf("silver ore source %+v: want Mining 20 marked Train", l.Sources)
		}
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("the line is malformed: %s", why)
		}
	}
	if !found {
		t.Fatalf("no silver ore line in %+v", step.Kit)
	}
}

func TestTrainReasonAndParseNeedsParkRoundTrip(t *testing.T) {
	need := TrainNeed{Line: KitLine{Name: "Silver ore"}, Source: Source{Method: MethodMineSilver}, Skill: "mining", From: 3, Level: 20, Qty: 1}
	reason := TrainReason([]TrainNeed{need})
	if !strings.HasPrefix(reason, "needs:mining:20 ") {
		t.Fatalf("the reason must start with the stable code: %q", reason)
	}
	if skill, level, ok := ParseNeedsPark(reason); !ok || skill != "mining" || level != 20 {
		t.Errorf("parsed %q %d %v", skill, level, ok)
	}
	if !strings.Contains(TrainReason([]TrainNeed{need, need}), "1 more line(s)") {
		t.Error("a second waiting line is not mentioned")
	}
	if TrainReason(nil) != "" {
		t.Error("no needs, no reason")
	}
	if got := NeedsParkCode("Mining", 20); got != "needs:mining:20" {
		t.Errorf("code %q", got)
	}
	for _, bad := range []string{"", "needs:", "needs:mining", "needs:mining:", "needs:mining:x", "needs:mining:0", "needs:Mining:20",
		"needs:mining:100", "parked: needs:mining:20", "needs: mining:20", "needs:mining 20", "prerequisite quest not complete: barcrawl"} {
		if skill, level, ok := ParseNeedsPark(bad); ok {
			t.Errorf("%q parsed as %q %d", bad, skill, level)
		}
	}
	if skill, level, ok := ParseNeedsPark("needs:woodcutting:40"); !ok || skill != "woodcutting" || level != 40 {
		t.Errorf("a bare code: %q %d %v", skill, level, ok)
	}
	if TrainParkFor < time.Hour {
		t.Errorf("a train park of %s would re-run the provisioner every few minutes for the same answer", TrainParkFor)
	}
}
