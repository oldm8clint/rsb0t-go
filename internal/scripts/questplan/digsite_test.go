package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// digIn builds a snapshot for DecideDigsite: a comfortable account (combat above the skeletons' ceiling, every level the content asks for) at the
// Examiner, in a quest that is in progress at the given stage and flags. Fields are overridden by the caller.
type digIn struct {
	stage, flags int
	at           tile.Tile
	inv          map[int]int
	levels       map[string]int
	hp, maxHP    int
	free         int
	ran, failed  map[string]bool
	blocked      map[string]bool
	bank         map[int]int
	bundled      bool // the stage's bundled bank/mule pass has run
}

func (in digIn) snapshot() Snapshot {
	lv := map[string]int{"attack": 60, "strength": 60, "defence": 60, "hitpoints": 70, "magic": 50, "prayer": 40, "thieving": 40, "agility": 40, "herblore": 30, "crafting": 20}
	for k, v := range in.levels {
		lv[k] = v
	}
	at := in.at
	if at == (tile.Tile{}) {
		at = DigExaminerAnchor
	}
	hp, maxHP := in.hp, in.maxHP
	if maxHP == 0 {
		hp, maxHP = 70, 70
	}
	free := in.free
	if free == 0 {
		free = 14
	}
	s := Snapshot{
		Journal: StatusInProgress, Stage: DigRaw(in.stage, in.flags), StageKnown: true, Tile: at, TileKnown: true, Levels: lv, HP: hp, MaxHP: maxHP, FreeSlots: free,
		InvIDs: map[int]int{CoinsID: 5000}, Inv: map[string]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{},
	}
	for id, n := range in.inv {
		s.InvIDs[id] = n
	}
	for k := range in.ran {
		s.Ran[k] = in.ran[k]
	}
	for k := range in.failed {
		s.Failed[k] = in.failed[k]
	}
	for k := range in.blocked {
		s.Blocked[k] = in.blocked[k]
	}
	if in.bank != nil {
		s.BankKnown, s.BankIDs = true, in.bank
	}
	if !in.bundled {
		s.Ran[fmt.Sprintf("provision:%s-%d", KitDigBundle, in.stage)] = true
	}
	return s
}

func (in digIn) decide() Step { return DecideDigsite(in.snapshot()) }

// stepIs checks a step against what the plan intends, and that it is well formed.
type dgWant struct {
	kind   StepKind
	custom string // a custom step's name
	npc    int    // a talk step's npc id
	item   int    // a custom step's ItemID
	target int    // a custom step's TargetID
	kit    string // a provision step's kit
	park   bool   // a wait that parks
}

func (w dgWant) check(t *testing.T, name string, got Step) {
	t.Helper()
	if why := ValidateStep(got); why != "" {
		t.Errorf("%s: malformed step %q: %s", name, got.Describe(), why)
	}
	bad := got.Kind != w.kind
	switch w.kind {
	case StepCustom:
		bad = bad || got.Custom != w.custom || got.ItemID != w.item || got.TargetID != w.target
	case StepTalk:
		bad = bad || got.Stop.ID != w.npc
	case StepProvision:
		bad = bad || got.KitName != w.kit
	case StepWait:
		bad = bad || (got.ParkFor > 0) != w.park
	}
	if bad {
		t.Errorf("%s: got %s (kind %s custom %q npc %d item %d target %d kit %q park %s), want %+v", name, got.Describe(), got.Kind, got.Custom, got.Stop.ID,
			got.ItemID, got.TargetID, got.KitName, got.ParkFor, w)
	}
}

func dgCustom(name string) dgWant { return dgWant{kind: StepCustom, custom: name} }
func dgCustomItem(name string, item int) dgWant {
	return dgWant{kind: StepCustom, custom: name, item: item}
}
func dgTalk(npc int) dgWant    { return dgWant{kind: StepTalk, npc: npc} }
func dgKit(name string) dgWant { return dgWant{kind: StepProvision, kit: name} }
func dgWait() dgWant           { return dgWant{kind: StepWait} }
func dgParks() dgWant          { return dgWant{kind: StepWait, park: true} }

var (
	dgSurface = DigExaminerAnchor
	dgWest    = DigWestLanding
	dgEast    = DigEastLanding
	dgAltar   = DigAltarLanding
)

// The start, the tab's states and the places the plan will not work from.
func TestDecideDigsiteStart(t *testing.T) {
	base := digIn{stage: DigStamping}.snapshot()
	unknown, notStarted := base, base
	unknown.Journal = StatusUnknown
	notStarted.Journal = StatusNotStarted
	dgWait().check(t, "an uncoloured tab", DecideDigsite(unknown))
	dgTalk(DigNpcExaminer).check(t, "a red tab", DecideDigsite(notStarted))
	if st := DecideDigsite(notStarted); st.Stop.Prefer[0] != "Can I take an exam?" {
		t.Errorf("the start talk prefers %v", st.Stop.Prefer)
	}
	notStarted.Tile = DigEastLanding
	dgCustom(CustomDigLeave).check(t, "a red tab underground", DecideDigsite(notStarted))

	noStage := base
	noStage.StageKnown = false
	dgWait().check(t, "an unread journal", DecideDigsite(noStage))
	noTile := base
	noTile.TileKnown = false
	dgWait().check(t, "an unread position", DecideDigsite(noTile))
	zero := digIn{stage: DigNotStarted}.snapshot()
	dgWait().check(t, "stage 0 with a yellow tab", DecideDigsite(zero))
	done := base
	done.Journal = StatusComplete
	dgWant{kind: StepDone}.check(t, "a green tab", DecideDigsite(done))
}

// Stage 1: the letter.
func TestDecideDigsiteLetter(t *testing.T) {
	st := digIn{stage: DigStamping}
	// The bundled bank pass comes first, with the letters as optional untradeable lines.
	first := digIn{stage: DigStamping}
	first.bundled = true
	got := first.decide()
	dgKit(fmt.Sprintf("%s-%d", KitDigBundle, DigStamping)).check(t, "the first pass of a run", got)
	ids := map[int]KitLine{}
	for _, l := range got.Kit {
		ids[l.ID] = l
		if !l.Optional || len(l.Sources) != 0 || ValidateKitLine(l) != "" {
			t.Errorf("bundle line %+v must be optional, sourceless and valid", l)
		}
	}
	if _, ok := ids[DigObjStampedLetter]; !ok || !ids[DigObjPlainLetter].Untradeable {
		t.Errorf("the stage 1 bundle is %+v, want both letters, untradeable", ids)
	}
	if !slices.Equal(got.KeepIDs, DigKeepIDs()) {
		t.Errorf("the bundle keeps %v", got.KeepIDs)
	}

	dgTalk(DigNpcExaminer).check(t, "no letter", st.decide())
	if p := st.decide().Stop.Prefer; p[0] != "I have lost the letter you gave me." {
		t.Errorf("the lost-letter talk prefers %v", p)
	}
	st.inv = map[int]int{DigObjPlainLetter: 1}
	dgTalk(DigNpcCurator).check(t, "the plain letter", st.decide())
	st.inv = map[int]int{DigObjStampedLetter: 1}
	got = st.decide()
	dgTalk(DigNpcExaminer).check(t, "the stamped letter", got)
	// The stamped letter starts the first exam at once with every errand open: the stop answers its wrong-only menus, and the right ones first.
	for _, a := range DigExamAnswers {
		if !slices.Contains(got.Stop.Prefer, a) {
			t.Errorf("the letter stop does not know the answer %q", a)
		}
	}
	for _, f := range digExamFallbacks {
		if !slices.Contains(got.Stop.Prefer, f) {
			t.Errorf("the letter stop has no fallback %q", f)
		}
	}
	// Underground (a death does not put the account there, but a parked shaft run might have): up first.
	st.at = dgWest
	dgCustom(CustomDigLeave).check(t, "stage 1 in a shaft", st.decide())
}

// Stage 2: the three samples are collected before any is handed over, each by its own way, and the exam is sat once the journal says so.
func TestDecideDigsiteFirstExam(t *testing.T) {
	st := digIn{stage: DigFirstExam}
	dgCustomItem(CustomDigPickpocket, DigObjSampleGreen).check(t, "nothing yet", st.decide())
	if got := st.decide(); got.Qty != 1 {
		t.Errorf("the pickpocket asks for %d samples", got.Qty)
	}
	st.levels = map[string]int{"thieving": 24}
	dgParks().check(t, "Thieving 24", st.decide())
	st.levels = map[string]int{"thieving": 25}
	dgCustomItem(CustomDigPickpocket, DigObjSampleGreen).check(t, "Thieving 25", st.decide())

	// The green sample in hand; the purple one is next, then the orange one.
	st.inv = map[int]int{DigObjSampleGreen: 1}
	dgCustom(CustomDigBush).check(t, "the green sample held", st.decide())
	st.inv = map[int]int{DigObjSampleGreen: 1, DigObjSamplePurple: 1}
	dgCustom(CustomDigTray).check(t, "two samples held, no tray", st.decide())
	st.inv[DigObjTrayEmpty] = 1
	dgCustomItem(CustomDigPan, DigObjSampleOrange).check(t, "two samples held, a tray", st.decide())
	// The guide refused a pan: tea (once a run), then the pan again.
	st.failed = map[string]bool{CustomDigPan: true}
	dgKit(KitDigTea).check(t, "the guide refused", st.decide())
	if got := st.decide(); len(got.Kit) != 1 || got.Kit[0].ID != DigObjTea || len(got.Kit[0].Sources) != 1 || got.Kit[0].Sources[0].Shop.NpcID != DigNpcTeaSeller {
		t.Errorf("the tea kit is %+v", got.Kit)
	}
	st.blocked = map[string]bool{"provision:" + KitDigTea: true}
	dgParks().check(t, "no tea anywhere", st.decide())
	st.blocked = nil
	st.inv[DigObjTea] = 1
	dgCustomItem(CustomDigPan, DigObjSampleOrange).check(t, "the tea in the pack", st.decide())

	// All three in hand, none answered: handed over green, purple, orange.
	st.failed = nil
	st.inv = map[int]int{DigObjSampleGreen: 1, DigObjSamplePurple: 1, DigObjSampleOrange: 1}
	for i, step := range []struct{ item, npc int }{{DigObjSampleGreen, DigNpcGreen}, {DigObjSamplePurple, DigNpcPurple}, {DigObjSampleOrange, DigNpcOrange}} {
		got := st.decide()
		dgWant{kind: StepCustom, custom: CustomDigGiveSample, item: step.item, target: step.npc}.check(t, fmt.Sprintf("hand-over %d", i), got)
		st.flags |= []int{DigFlagGreen, DigFlagPurple, DigFlagOrange}[i]
		delete(st.inv, step.item)
	}
	// An answered errand needs no sample, whatever the pack holds, and a held sample of an answered errand is not handed over again.
	st = digIn{stage: DigFirstExam, flags: DigFlagGreen | DigFlagOrange, inv: map[int]int{DigObjSampleGreen: 1}}
	dgCustom(CustomDigBush).check(t, "green and orange answered", st.decide())
	st.inv = map[int]int{DigObjSamplePurple: 1}
	dgWant{kind: StepCustom, custom: CustomDigGiveSample, item: DigObjSamplePurple, target: DigNpcPurple}.check(t, "only the purple one left", st.decide())
	// The journal's ready line sits the exam, and the exam stop knows its menus.
	st = digIn{stage: DigFirstExam, flags: DigFlagGreen | DigFlagPurple | DigFlagOrange | DigFlagReady}
	got := st.decide()
	dgTalk(DigNpcExaminer).check(t, "ready", got)
	for _, p := range []string{"Yes, I certainly am.", "I am ready for the next exam.", "I am ready for the last exam"} {
		if !slices.Contains(got.Stop.Prefer, p) {
			t.Errorf("the exam stop lacks %q", p)
		}
	}
	// Everything answered but no ready line: wait, never sit the exam blind.
	st = digIn{stage: DigFirstExam, flags: DigFlagGreen | DigFlagPurple | DigFlagOrange}
	dgWait().check(t, "answered but not ready", st.decide())
}

// Stages 3 and 4: every student once, the opal between the purple student's two talks, then the exam.
func TestDecideDigsiteSecondAndThirdExam(t *testing.T) {
	st := digIn{stage: DigSecondExam}
	dgCustom(CustomDigStudentGreen).check(t, "second: nothing yet", st.decide())
	st.ran = map[string]bool{CustomDigStudentGreen: true}
	dgCustom(CustomDigStudentOrange).check(t, "second: green asked", st.decide())
	st.ran[CustomDigStudentOrange] = true
	dgCustom(CustomDigStudentPurple).check(t, "second: green and orange asked", st.decide())
	st.ran[CustomDigStudentPurple] = true
	dgWait().check(t, "second: all asked and not ready", st.decide())
	st.flags = DigFlagReady
	dgTalk(DigNpcExaminer).check(t, "second: ready", st.decide())

	// The third exam: the purple student asks for an opal on her first talk.
	st = digIn{stage: DigThirdExam, ran: map[string]bool{CustomDigStudentGreen3: true, CustomDigStudentOrange3: true}}
	dgCustom(CustomDigStudentPurple3).check(t, "third: green and orange asked", st.decide())
	st.ran[CustomDigStudentPurple3] = true
	st.flags = DigFlagOpal
	dgCustom(CustomDigTray).check(t, "third: she wants an opal, no tray", st.decide())
	st.inv = map[int]int{DigObjTrayEmpty: 1}
	dgCustomItem(CustomDigPan, DigObjUncutOpal).check(t, "third: panning for it", st.decide())
	st.inv = map[int]int{DigObjUncutOpal: 1}
	dgKit(KitDigChisel).check(t, "third: an uncut opal and no chisel", st.decide())
	if got := st.decide(); len(got.Kit) != 1 || got.Kit[0].ID != DigObjChisel || got.Kit[0].Sources[0].Shop.NpcID != NpcVarrockGenKpr {
		t.Errorf("the chisel kit is %+v", got.Kit)
	}
	st.inv = map[int]int{DigObjUncutOpal: 1, DigObjChisel: 1}
	dgCustom(CustomDigCutOpal).check(t, "third: an uncut opal and a chisel", st.decide())
	st.inv = map[int]int{DigObjOpal: 1}
	got := st.decide()
	dgCustom(CustomDigStudentPurple3).check(t, "third: an opal", got)
	if !strings.Contains(got.Reason, "opal") {
		t.Errorf("the hand-over is described %q", got.Reason)
	}
	// An opal in the pack before she has been asked: the first talk asks, the second gives.
	st = digIn{stage: DigThirdExam, ran: map[string]bool{CustomDigStudentGreen3: true, CustomDigStudentOrange3: true}, inv: map[int]int{DigObjOpal: 1}}
	got = st.decide()
	dgCustom(CustomDigStudentPurple3).check(t, "third: opal held, not asked", got)
	if !strings.Contains(got.Reason, "ask") {
		t.Errorf("the first talk is described %q", got.Reason)
	}
	st.ran[CustomDigStudentPurple3] = true
	got = st.decide()
	dgCustom(CustomDigStudentPurple3).check(t, "third: opal held, asked", got)
	if !strings.Contains(got.Reason, "give") {
		t.Errorf("the second talk is described %q", got.Reason)
	}
	st.flags = DigFlagReady
	dgTalk(DigNpcExaminer).check(t, "third: ready", st.decide())
}

// Stage 5: the find for the expert, and his letter.
func TestDecideDigsiteImpress(t *testing.T) {
	st := digIn{stage: DigImpressExpert}
	st.inv = map[int]int{DigObjExpertScroll: 1}
	dgCustom(CustomDigGiveScroll).check(t, "the letter", st.decide())
	st.inv = map[int]int{DigObjTalisman: 1}
	dgCustomItem(CustomDigUseOnExpert, DigObjTalisman).check(t, "the talisman", st.decide())
	st.inv = nil
	dgTalk(DigNpcExaminer).check(t, "no trowel, none banked", st.decide())
	if p := st.decide().Stop.Prefer; p[0] != "I have lost my trowel." {
		t.Errorf("the trowel talk prefers %v", p)
	}
	st.bank = map[int]int{DigObjTrowel: 1}
	dgWant{kind: StepWithdraw}.check(t, "no trowel, one banked", st.decide())
	st.bank = nil
	st.inv = map[int]int{DigObjTrowel: 1}
	dgCustom(CustomDigSacks).check(t, "no jar", st.decide())
	st.inv[DigObjSpecimenJar] = 1
	dgCustomItem(CustomDigPickpocket, DigObjSpecimenBrush).check(t, "no brush", st.decide())
	st.levels = map[string]int{"thieving": 20}
	dgParks().check(t, "no brush at Thieving 20", st.decide())
	st.levels = nil
	st.inv[DigObjSpecimenBrush] = 1
	dgCustom(CustomDigTalisman).check(t, "the kit", st.decide())
	// A long dig that found nothing: the expert's lost letter, once, and then back to digging.
	st.failed = map[string]bool{CustomDigTalisman: true}
	dgCustom(CustomDigLostLetter).check(t, "a dig that found nothing", st.decide())
	st.ran = map[string]bool{CustomDigLostLetter: true}
	dgCustom(CustomDigTalisman).check(t, "the expert had nothing to give", st.decide())
	// Down a shaft: up first.
	st.at = dgEast
	dgCustom(CustomDigLeave).check(t, "stage 5 in a shaft", st.decide())
}

// kitIDs is the object ids of a provision step's lines.
func dgKitIDs(st Step) []int {
	var out []int
	for _, l := range st.Kit {
		out = append(out, l.ID)
	}
	slices.Sort(out)
	return out
}

// Stage 6: the order of the chemistry, from nothing to the compound, with every dig before the first explosive item.
func TestDecideDigsiteChemistryOrder(t *testing.T) {
	held := func(ids ...int) map[int]int {
		m := map[int]int{DigObjTrowel: 1}
		for _, id := range ids {
			m[id] = 1
		}
		return m
	}
	st := digIn{stage: DigMineshaftPermit, inv: held()}
	// The shopping first: a vial and a pestle at Jatix's, a tinderbox at Varrock's.
	got := st.decide()
	dgKit(KitDigChem).check(t, "nothing", got)
	if ids := dgKitIDs(got); !slices.Equal(ids, []int{DigObjVial, DigObjPestle, DigObjTinderbox}) {
		t.Errorf("the chemistry kit is %v, want the vial, the pestle and the tinderbox", ids)
	}
	for _, l := range got.Kit {
		if len(l.Sources) != 1 || l.Sources[0].Kind != SourceShop || l.Untradeable || l.Optional {
			t.Errorf("kit line %+v: want a plain shop line", l)
		}
		switch l.ID {
		case DigObjVial, DigObjPestle:
			if l.Sources[0].Shop.NpcID != PlagueNpcJatix {
				t.Errorf("%s is bought at npc %d, want Jatix", l.Name, l.Sources[0].Shop.NpcID)
			}
		case DigObjTinderbox:
			if l.Sources[0].Shop.NpcID != NpcVarrockGenKpr {
				t.Errorf("the tinderbox is bought at npc %d, want Varrock's shop keeper", l.Sources[0].Shop.NpcID)
			}
		}
	}
	st.blocked = map[string]bool{"provision:" + KitDigChem: true}
	dgParks().check(t, "no shop has them", st.decide())
	st.blocked = nil

	// The pack has the tools: the west shaft is next (the key and the root), then the charcoal and the pestle, the chest, the barrel, the mixes.
	tools := []int{DigObjVial, DigObjPestle, DigObjTinderbox}
	steps := []struct {
		name string
		inv  []int
		want dgWant
	}{
		{"the west shaft", tools, dgCustom(CustomDigWest)},
		{"the key but no root", append([]int{DigObjChestKey}, tools...), dgCustom(CustomDigWest)},
		{"the root but no key", append([]int{DigObjArceniaRoot}, tools...), dgCustom(CustomDigWest)},
		{"the charcoal dig", append([]int{DigObjChestKey, DigObjArceniaRoot}, tools...), dgCustom(CustomDigCharcoal)},
		{"the grinding", append([]int{DigObjChestKey, DigObjArceniaRoot, DigObjCharcoal}, tools...),
			dgWant{kind: StepCustom, custom: CustomDigMix, item: DigObjCharcoal, target: DigObjPestle}},
		{"the chest", append([]int{DigObjChestKey, DigObjArceniaRoot, DigObjGroundCharcoal}, tools...), dgCustom(CustomDigChest)},
		{"the powder named", append([]int{DigObjArceniaRoot, DigObjGroundCharcoal, DigObjPowder}, tools...), dgCustomItem(CustomDigUseOnExpert, DigObjPowder)},
		{"the barrel", append([]int{DigObjArceniaRoot, DigObjGroundCharcoal, DigObjNitrate}, tools...), dgCustom(CustomDigBarrel)},
		{"the liquid named", append([]int{DigObjArceniaRoot, DigObjGroundCharcoal, DigObjNitrate, DigObjLiquid}, tools...), dgCustomItem(CustomDigUseOnExpert, DigObjLiquid)},
		{"the first mix", append([]int{DigObjArceniaRoot, DigObjGroundCharcoal, DigObjNitrate, DigObjNitro}, tools...),
			dgWant{kind: StepCustom, custom: CustomDigMix, item: DigObjNitrate, target: DigObjNitro}},
		{"the second mix", append([]int{DigObjArceniaRoot, DigObjGroundCharcoal, DigObjPreMix}, tools...),
			dgWant{kind: StepCustom, custom: CustomDigMix, item: DigObjPreMix, target: DigObjGroundCharcoal}},
		{"the third mix", append([]int{DigObjArceniaRoot, DigObjPostMix}, tools...),
			dgWant{kind: StepCustom, custom: CustomDigMix, item: DigObjPostMix, target: DigObjArceniaRoot}},
		{"the blast", append([]int{DigObjCompound}, tools...), dgCustom(CustomDigBlast)},
	}
	for _, c := range steps {
		s := digIn{stage: DigMineshaftPermit, inv: held(c.inv...)}
		c.want.check(t, c.name, s.decide())
	}
	// No trowel at the charcoal or the barrel: the Examiner's spare (or the bank's).
	s := digIn{stage: DigMineshaftPermit, inv: map[int]int{DigObjChestKey: 1, DigObjArceniaRoot: 1, DigObjVial: 1, DigObjPestle: 1, DigObjTinderbox: 1}}
	dgTalk(DigNpcExaminer).check(t, "no trowel", s.decide())
	// Nothing is made of the pack's volatile items before the dig is over: with a charcoal still to find, the barrel waits even when a vial and
	// the nitrate are in hand.
	s = digIn{stage: DigMineshaftPermit, inv: held(DigObjChestKey, DigObjArceniaRoot, DigObjNitrate, DigObjVial, DigObjPestle, DigObjTinderbox)}
	dgCustom(CustomDigCharcoal).check(t, "the charcoal before the barrel", s.decide())

	// Levels: the second mix wants Herblore 10 and the winch Agility 10; the account is not started on the explosives below them.
	s = digIn{stage: DigMineshaftPermit, levels: map[string]int{"herblore": 9}, inv: held()}
	dgParks().check(t, "Herblore 9", s.decide())
	s = digIn{stage: DigMineshaftPermit, levels: map[string]int{"agility": 9}, inv: held(tools...)}
	dgParks().check(t, "Agility 9 at the west winch", s.decide())
	s = digIn{stage: DigMineshaftPermit, levels: map[string]int{"herblore": 9}, inv: held(DigObjPostMix, DigObjArceniaRoot, DigObjTinderbox)}
	dgWant{kind: StepCustom, custom: CustomDigMix, item: DigObjPostMix, target: DigObjArceniaRoot}.check(t, "Herblore 9 with the second mix already made", s.decide())

	// Where the account stands: a shaft is left unless the next step belongs there.
	west := digIn{stage: DigMineshaftPermit, at: dgWest, inv: held(tools...)}
	dgCustom(CustomDigWest).check(t, "in the west shaft, the key and the root to find", west.decide())
	west.inv = held(append([]int{DigObjChestKey, DigObjArceniaRoot}, tools...)...)
	dgCustom(CustomDigLeave).check(t, "in the west shaft with both", west.decide())
	east := digIn{stage: DigMineshaftPermit, at: dgEast, inv: held(append([]int{DigObjCompound}, tools...)...)}
	dgCustom(CustomDigBlast).check(t, "in the east shaft with the compound", east.decide())
	east.inv = held(append([]int{DigObjChestKey, DigObjArceniaRoot}, tools...)...)
	dgCustom(CustomDigLeave).check(t, "in the east shaft with the charcoal to dig", east.decide())
}

// The bundle of a stage names the stage's items, and the explosive ones are only ever asked of the bank.
func TestDecideDigsiteBundles(t *testing.T) {
	for stage := DigStamping; stage <= DigRemovedBlockage; stage++ {
		s := digIn{stage: stage, bundled: true}.snapshot()
		for _, l := range digBundle(s, stage) {
			if !l.Optional || len(l.Sources) != 0 {
				t.Errorf("stage %d: bundle line %+v must be optional and sourceless", stage, l)
			}
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("stage %d: %s", stage, why)
			}
			if slices.Contains(digVolatile, l.ID) && !l.Untradeable {
				t.Errorf("stage %d: %s could be asked of the mule", stage, l.Name)
			}
			if !slices.Contains(digKeep, l.ID) {
				t.Errorf("stage %d: bundle line %s is not in the keep list, so the next bank step would deposit it again", stage, l.Name)
			}
		}
	}
	// The stage 6 bundle asks for what is missing, and not for what is held.
	s := digIn{stage: DigMineshaftPermit, inv: map[int]int{DigObjRope: 2, DigObjTrowel: 1}}.snapshot()
	for _, l := range digBundle(s, DigMineshaftPermit) {
		if l.ID == DigObjRope || l.ID == DigObjTrowel {
			t.Errorf("the bundle asks for %s, which the pack holds", l.Name)
		}
	}
}

// Stage 7 and 8: the light, the cave and the tablet, and the altar cave's price.
func TestDecideDigsiteBlastAndTablet(t *testing.T) {
	st := digIn{stage: DigPouredCompound, inv: map[int]int{DigObjTinderbox: 1}}
	dgCustom(CustomDigBlast).check(t, "poured, a tinderbox", st.decide())
	st.inv = nil
	dgKit(KitDigBox).check(t, "poured, no tinderbox", st.decide())
	st.blocked = map[string]bool{"provision:" + KitDigBox: true}
	dgParks().check(t, "no tinderbox anywhere", st.decide())
	st.blocked = nil
	st.at = dgEast
	dgCustom(CustomDigLeave).check(t, "in the east shaft without a tinderbox", st.decide())
	st.inv = map[int]int{DigObjTinderbox: 1}
	dgCustom(CustomDigBlast).check(t, "in the east shaft with one", st.decide())

	tab := digIn{stage: DigRemovedBlockage}
	dgCustom(CustomDigTablet).check(t, "past the blast", tab.decide())
	tab.at = dgAltar
	dgCustom(CustomDigTablet).check(t, "in the altar cave", tab.decide())
	tab.inv = map[int]int{DigObjStoneTablet: 1}
	tab.at = dgAltar
	got := tab.decide()
	if got.Kind == StepCustom && got.Custom == CustomDigTablet {
		t.Errorf("holding the tablet in the cave: %s", got.Describe())
	}
	tab.at = dgSurface
	dgCustomItem(CustomDigUseOnExpert, DigObjStoneTablet).check(t, "the tablet in hand", tab.decide())
	// A full pack: bank first (the tablet needs a slot).
	full := digIn{stage: DigRemovedBlockage}.snapshot()
	full.FreeSlots = 0
	dgWant{kind: StepDeposit}.check(t, "a full pack", DecideDigsite(full))
	// The pack's levels: the winch.
	low := digIn{stage: DigRemovedBlockage, levels: map[string]int{"agility": 9}}
	dgParks().check(t, "Agility 9", low.decide())
}

// The altar cave is priced for an account the skeletons hunt, and only then.
func TestDigAltarFit(t *testing.T) {
	strong := digIn{stage: DigRemovedBlockage}.snapshot()
	if ok, why := DigAltarFit(strong); !ok {
		t.Errorf("a combat %d account is refused: %s", DemonCombat(strong), why)
	}
	// An account the skeletons skip is not priced at all, however little it holds.
	skipped := digIn{stage: DigRemovedBlockage, levels: map[string]int{"attack": 40, "strength": 40, "defence": 40, "hitpoints": 45, "magic": 1}, hp: 5, maxHP: 45}.snapshot()
	if c := DemonCombat(skipped); c <= DigSkeletonCeiling {
		t.Fatalf("the test account is combat %d, not above the ceiling %d", c, DigSkeletonCeiling)
	}
	if ok, why := DigAltarFit(skipped); !ok {
		t.Errorf("an account above the ceiling is refused: %s", why)
	}
	// A hunted account with nothing to eat is refused, with the numbers; with food it goes.
	weak := digIn{stage: DigRemovedBlockage, levels: map[string]int{"attack": 20, "strength": 20, "defence": 20, "hitpoints": 25, "magic": 1, "prayer": 1, "ranged": 1}, maxHP: 25, hp: 25}
	s := weak.snapshot()
	c := DemonCombat(s)
	if c == 0 || c > DigSkeletonCeiling {
		t.Fatalf("the weak account is combat %d", c)
	}
	ok, why := DigAltarFit(s)
	if ok {
		t.Fatalf("a combat %d account with 25 hitpoints and no food is let into the skeletons' cave", c)
	}
	for _, want := range []string{"skeletons", "combat 50", fmt.Sprintf("combat %d", c), "25 hitpoints"} {
		if !strings.Contains(why, want) {
			t.Errorf("the refusal %q lacks %q", why, want)
		}
	}
	weak.inv = map[int]int{385: 6}
	if ok, why := DigAltarFit(weak.snapshot()); !ok {
		t.Errorf("six sharks do not cover the cave: %s", why)
	}
	weak.inv = map[int]int{385: 1}
	if ok, _ := DigAltarFit(weak.snapshot()); ok {
		t.Error("one shark covers the cave")
	}
	// The refusal on the surface: one food kit a run, then the park; underground it is the way out.
	weak.inv = nil
	weak.stage = DigRemovedBlockage
	dgKit(KitDigFood).check(t, "unfit, nothing tried", weak.decide())
	if got := weak.decide(); got.Kind != StepProvision || len(got.Kit) == 0 || !got.Kit[0].Optional {
		t.Errorf("the food kit %+v", got.Kit)
	}
	weak.ran = map[string]bool{"provision:" + KitDigFood: true}
	dgParks().check(t, "unfit after the food kit", weak.decide())
	weak.at = dgAltar
	dgCustom(CustomDigLeave).check(t, "unfit in the cave", weak.decide())
	// The same refusal gates the pour: the blast ends in the cave.
	pour := digIn{stage: DigMineshaftPermit, levels: weak.levels, maxHP: 25, hp: 25, inv: map[int]int{DigObjCompound: 1, DigObjTinderbox: 1}}
	dgKit(KitDigFood).check(t, "unfit at the pour", pour.decide())
	pour.ran = map[string]bool{"provision:" + KitDigFood: true}
	dgParks().check(t, "unfit at the pour after the food kit", pour.decide())
	// An unread maximum judges nothing.
	unread := strong
	unread.MaxHP = 0
	if ok, _ := DigAltarFit(unread); !ok {
		t.Error("an unread maximum hitpoints was refused")
	}
}

// Every step any scenario above asks for is well formed (check does it) and the module is wired the way the engine reads it.
func TestDigsiteModule(t *testing.T) {
	m := Digsite
	if m.Record.ID != "itexam" || m.Record.Name != "Digsite Quest" || m.Record.QuestPoints != 2 {
		t.Errorf("record %+v", m.Record)
	}
	skills := map[string]int{}
	for _, r := range m.Record.Requirements.Skills {
		skills[r.Skill] = r.Level
	}
	if len(skills) != 3 || skills["thieving"] != 25 || skills["agility"] != 10 || skills["herblore"] != 10 {
		t.Errorf("requirements %v", skills)
	}
	if !m.NoRetaliate || m.MaxDeaths != 2 || m.ReadStage == nil || m.Decide == nil || !m.Sustain.IsFood(385) {
		t.Errorf("module %+v", m)
	}
	for _, c := range []string{CustomDigBlast, CustomDigTablet} {
		cap, ok := m.StepDeathCaps[c]
		if !ok || cap.Max != 1 || cap.Window != 24*time.Hour || cap.Skip != 24*time.Hour {
			t.Errorf("death cap of %s: %+v %v", c, cap, ok)
		}
	}
	if m.Bank != VarrockEastBank || !m.BankSet {
		t.Errorf("bank %v", m.Bank)
	}
	// No object is both a tool and loot to drop, and nothing explosive can be dropped (the lists are what keeps a chemical out of Drop).
	for _, id := range digJunk {
		if slices.Contains(digTools, id) || slices.Contains(digVolatile, id) || slices.Contains(digKeep, id) {
			t.Errorf("object %d is junk to drop and a tool of the quest", id)
		}
	}
	for _, id := range digVolatile {
		if !slices.Contains(digTools, id) {
			t.Errorf("explosive object %d is not a tool, so a bank step would deposit it", id)
		}
	}
	for id := range DigKeepLimit {
		if !slices.Contains(digTools, id) {
			t.Errorf("object %d has a keep limit and is not a tool", id)
		}
	}
	// The pickpocket's spoil: the three things a workman gives that this quest has no use for are all on the list.
	for _, id := range []int{952, 1059, 1925} {
		if !slices.Contains(digJunk, id) {
			t.Errorf("object %d (a pocket's spoil) is not droppable", id)
		}
	}
	if len(DigToolIDs()) != len(digTools) || len(DigKeepIDs()) != len(digKeep) || len(DigJunkIDs()) != len(digJunk) || len(DigVolatileIDs()) != 5 {
		t.Error("the id accessors are not copies of the lists")
	}
	if QuestTabName(DigsiteID) != "Digsite Quest" {
		t.Errorf("tab %q", QuestTabName(DigsiteID))
	}
}

// The regions are disjoint, the landings are inside their own, and the surface is nowhere in any.
func TestDigRegions(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want DigRegion
	}{
		{DigEastLanding, DigShaftEast}, {DigBrickStand, DigShaftEast}, {DigWestLanding, DigShaftWest}, {DigCaveWorkmanAnchor, DigShaftWest},
		{DigRootSpawns[0], DigShaftWest}, {DigRootSpawns[1], DigShaftWest}, {DigAltarLanding, DigAltarEast}, {DigAltarEastLanding, DigAltarEast},
		{DigTabletSpawn, DigAltarEast}, {DigAltarRope, DigAltarEast}, {DigAltarWestLanding, DigAltarWest},
		{DigExaminerAnchor, DigSurface}, {DigStudentsAnchor, DigSurface}, {DigMainWinch, DigSurface}, {VarrockEastBank, DigSurface},
		{tile.Tile{X: 3370, Z: 9828, Level: 1}, DigSurface},
	} {
		if got := DigRegionOf(c.at); got != c.want {
			t.Errorf("%v is in %q, want %q", c.at, got, c.want)
		}
		if (c.want != DigSurface) != InDigCave(c.at) {
			t.Errorf("InDigCave(%v) disagrees with %q", c.at, c.want)
		}
	}
	// Disjoint rectangles: no tile of any is in two.
	for x := 3340; x <= 3400; x++ {
		for z := 9730; z <= 9860; z++ {
			n := 0
			for _, in := range []func(tile.Tile) bool{InDigShaftEast, InDigShaftWest, InDigAltarEast, InDigAltarWest} {
				if in(tile.Tile{X: x, Z: z}) {
					n++
				}
			}
			if n > 1 {
				t.Fatalf("%d,%d is in %d of the digsite's cave rectangles", x, z, n)
			}
		}
	}
}

// The zones are the content's inzone() rectangles, and each holds its own stand.
func TestDigZones(t *testing.T) {
	for _, z := range []DigZone{DigLevel3Zone, DigTrainingZone} {
		if !z.Contains(z.Stand) {
			t.Errorf("zone %+v does not hold its stand %v", z, z.Stand)
		}
	}
	if DigLevel3Zone.Contains(DigTrainingZone.Stand) || DigTrainingZone.Contains(DigLevel3Zone.Stand) {
		t.Error("the level 3 site and the training site overlap")
	}
	if DigLevel3Zone.Contains(tile.Tile{X: 3353, Z: 3408, Level: 1}) {
		t.Error("a zone contains an upper floor tile")
	}
}
