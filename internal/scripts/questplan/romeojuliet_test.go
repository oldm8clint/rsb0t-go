package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// ---- the journal ----

// Every stage the content renders reads back as that stage from the text the server really renders (the later stages
// contain the earlier ones' struck-through phrases, so only the order of the needles keeps stage 40 from reading as 30).
// Stage 50 renders three ways, by what the pack holds.
func TestParseRomeoJulietJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, text := range romeoJournalFixtures {
		for _, lines := range [][]string{{text}, strings.Split(text, "|")} {
			got, ok := ParseRomeoJulietJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d: read as %d (ok %v) from %q", stage, got, ok, lines)
			}
		}
	}
	if len(romeoJournalFixtures) != 7 || len(romeoJournalStage50) != 3 {
		t.Fatalf("%d rendered stages and %d stage 50 renderings, the content has 7 (0 10 20 30 40 60 100) and 3", len(romeoJournalFixtures), len(romeoJournalStage50))
	}
	for name, text := range romeoJournalStage50 {
		if got, ok := ParseRomeoJulietJournal([]string{text}); !ok || got != RomeoSpokeApothecary {
			t.Errorf("stage 50 with the %s line: read as %d (ok %v)", name, got, ok)
		}
	}
	// Content branch 274 spelt the item "cadaver": the stage-50 needle names neither spelling.
	old := strings.ReplaceAll(romeoJournalStage50["berries"], "cadava", "cadaver")
	if got, ok := ParseRomeoJulietJournal([]string{old}); !ok || got != RomeoSpokeApothecary {
		t.Errorf("the 274 spelling: read as %d (ok %v)", got, ok)
	}
	for _, lines := range [][]string{nil, {""}, {"I can start this quest by talking to Juliet"}, {"@dbl@Something else entirely."}} {
		if got, ok := ParseRomeoJulietJournal(lines); ok {
			t.Errorf("%q read as stage %d: an unrecognised journal is unknown, never a stage", lines, got)
		}
	}
}

// ---- a world to run the module in ----

// rjWorld is the account and the part of Varrock the quest touches, with the content's own rules for each talk (the
// scripts named in the comments).
type rjWorld struct {
	t      *testing.T
	stage  int
	inv    map[int]int
	bank   map[int]int
	ran    map[string]bool
	failed map[string]bool
	log    []string
	picked int // cadava berries taken from the ground
	tile   tile.Tile
}

func newRJWorld(t *testing.T, stage int, inv, bank map[int]int) *rjWorld {
	if inv == nil {
		inv = map[int]int{}
	}
	if bank == nil {
		bank = map[int]int{}
	}
	return &rjWorld{t: t, stage: stage, inv: inv, bank: bank, ran: map[string]bool{}, failed: map[string]bool{}, tile: VarrockWestBank}
}

func (w *rjWorld) used() int {
	n := 0
	for id, c := range w.inv {
		if c > 0 {
			if id == CoinsID {
				n++
			} else {
				n += c
			}
		}
	}
	return n
}

func (w *rjWorld) snapshot() Snapshot {
	s := Snapshot{InvIDs: map[int]int{}, Inv: map[string]int{}, WornIDs: map[int]int{}, Worn: map[string]int{},
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, Tile: w.tile, TileKnown: true,
		BankKnown: true, BankIDs: map[int]int{}}
	for id, n := range w.inv {
		if n > 0 {
			s.InvIDs[id] = n
		}
	}
	for id, n := range w.bank {
		s.BankIDs[id] = n
	}
	s.FreeSlots = 28 - w.used()
	for k, v := range w.ran {
		s.Ran[k] = v
	}
	for k, v := range w.failed {
		s.Failed[k] = v
	}
	switch {
	case w.stage == RomeoNotStarted:
		s.Journal = StatusNotStarted
	case w.stage >= RomeoComplete:
		s.Journal = StatusComplete
	default:
		s.Journal, s.Stage, s.StageKnown = StatusInProgress, w.stage, true
	}
	return s
}

func (w *rjWorld) provision(st Step) bool {
	for pass := 0; pass < 16; pass++ {
		s := w.snapshot()
		plan := PlanProvision(st.Kit, ProvisionState{Pack: s.InvIDs, Bank: w.bank, BankKnown: true, FreeSlots: s.FreeSlots,
			Levels: map[string]int{}, Exhausted: map[string]bool{}})
		switch plan.Phase {
		case PhaseSatisfied:
			return true
		case PhaseBank:
			moved := false
			for _, c := range plan.Withdraw {
				n := minInt(c.Count, w.bank[c.ID])
				if n > 0 {
					w.bank[c.ID] -= n
					w.inv[c.ID] += n
					moved = true
				}
			}
			if !moved {
				return false
			}
		case PhaseSource:
			task := plan.Tasks[0]
			if task.Source.Kind != SourceGround || task.Line.ID != ObjCadavaBerries {
				w.t.Fatalf("an unexpected source %s", task.Describe())
			}
			w.inv[task.Line.ID] += task.Qty
			w.picked += task.Qty
		default:
			return false
		}
	}
	return false
}

func (w *rjWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		ok := w.provision(st)
		key := "provision:" + st.KitName
		if ok {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
		}
		w.log = append(w.log, key)
	case StepDeposit:
		keep := map[int]bool{}
		for _, id := range st.KeepIDs {
			keep[id] = true
		}
		for id, n := range w.inv {
			if !keep[id] && n > 0 {
				w.bank[id] += n
				delete(w.inv, id)
			}
		}
		w.log = append(w.log, "deposit")
	case StepTalk:
		w.talk(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *rjWorld) talk(st Step) {
	t := w.t
	switch st.Stop.ID {
	case NpcRomeo:
		switch w.stage {
		case RomeoNotStarted:
			w.stage = RomeoSpokeRomeo // romeo_tell_her
		case RomeoSpokeJuliet:
			if w.inv[ObjJulietMessage] > 0 {
				w.inv[ObjJulietMessage]-- // romeo_messagefrom
				w.stage = RomeoPassedMessage
			}
		case RomeoJulietCrypt:
			w.stage = RomeoComplete // romeo_allset
		}
		w.log = append(w.log, fmt.Sprintf("romeo%d", w.stage))
	case NpcJuliet:
		switch w.stage {
		case RomeoSpokeRomeo:
			// juliet_agree_message: inv_add, which drops the message when the pack is full.
			if w.used() < 28 {
				w.inv[ObjJulietMessage]++
			}
			w.stage = RomeoSpokeJuliet
		case RomeoSpokeJuliet:
			// juliet_deliver_letter: a new message only when neither the pack nor the bank holds one.
			if w.inv[ObjJulietMessage] == 0 && w.bank[ObjJulietMessage] == 0 && w.used() < 28 {
				w.inv[ObjJulietMessage]++
			}
		case RomeoSpokeApothecary:
			// juliet_potion_made.
			if w.inv[ObjCadavaPotion] > 0 {
				w.inv[ObjCadavaPotion]--
				w.stage = RomeoJulietCrypt
			}
		}
		w.log = append(w.log, fmt.Sprintf("juliet%d", w.stage))
	case NpcFatherLawrence:
		if w.stage == RomeoPassedMessage {
			w.stage = RomeoSpokeFather // father_lawrence_help
		}
		w.log = append(w.log, fmt.Sprintf("lawrence%d", w.stage))
	case NpcApothecary:
		switch w.stage {
		case RomeoSpokeFather:
			w.stage = RomeoSpokeApothecary // apothecary_lawrence_sent
		case RomeoSpokeApothecary:
			// apothecary_make_cadava: nothing while the pack holds a potion; berries for a potion otherwise.
			if w.inv[ObjCadavaPotion] == 0 && w.inv[ObjCadavaBerries] > 0 {
				w.inv[ObjCadavaBerries]--
				w.inv[ObjCadavaPotion]++
			}
		}
		w.log = append(w.log, fmt.Sprintf("apothecary%d", w.stage))
	default:
		t.Fatalf("talk to an unexpected npc %d", st.Stop.ID)
	}
}

func (w *rjWorld) run() (waited string) {
	w.t.Helper()
	for i := 0; i < 60; i++ {
		st := DecideRomeoJuliet(w.snapshot())
		switch st.Kind {
		case StepDone:
			return ""
		case StepWait:
			return st.Reason
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.ran, w.failed)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.ran, w.failed) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("60 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return ""
}

// ---- the whole quest, from several starts ----

func TestAFreshAccountDoesRomeoAndJulietInOrder(t *testing.T) {
	w := newRJWorld(t, RomeoNotStarted, map[int]int{CoinsID: 10}, nil)
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"romeo10", "juliet20", "romeo30", "lawrence40", "apothecary50", "provision:romeo-potion", "apothecary50",
		"juliet60", "romeo100"}
	if fmt.Sprint(w.log) != fmt.Sprint(want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.picked != 1 {
		t.Errorf("%d cadava berries taken from the ground, want 1", w.picked)
	}
	for _, id := range []int{ObjJulietMessage, ObjCadavaBerries, ObjCadavaPotion} {
		if w.inv[id] != 0 {
			t.Errorf("%d of obj %d left in the pack", w.inv[id], id)
		}
	}
}

// Juliet issues a second message only while neither the pack nor the bank holds one. An account whose trainer banked the
// message is told to hurry and given nothing, for ever: the module draws it from the bank first (the Murder Mystery
// lesson), and asks Juliet for a replacement only when the bank has none.
func TestABankedMessageIsDrawnBeforeJulietIsAsked(t *testing.T) {
	w := newRJWorld(t, RomeoSpokeJuliet, nil, map[int]int{ObjJulietMessage: 1})
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if got := w.log[0]; got != "provision:romeo-message" {
		t.Fatalf("log %v: the bank is read before Juliet", w.log)
	}
	if strings.Contains(strings.Join(w.log[:2], ","), "juliet") {
		t.Errorf("log %v: Juliet was asked while the message sat in the bank", w.log)
	}

	// No message anywhere: the same kit reads the bank, finds none, and Juliet issues one.
	w = newRJWorld(t, RomeoSpokeJuliet, nil, nil)
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if fmt.Sprint(w.log[:3]) != "[provision:romeo-message juliet20 romeo30]" {
		t.Errorf("log %v", w.log)
	}

	// The message held needs no bank at all.
	w = newRJWorld(t, RomeoSpokeJuliet, map[int]int{ObjJulietMessage: 1}, nil)
	if why := w.run(); why != "" || w.log[0] != "romeo30" {
		t.Errorf("held: %q, log %v", why, w.log)
	}
}

// The potion is drawn from the bank before another berry is looked for, and a banked berry before a ground one.
func TestTheBankIsTriedForThePotionAndTheBerryBeforeTheGround(t *testing.T) {
	w := newRJWorld(t, RomeoSpokeApothecary, nil, map[int]int{ObjCadavaPotion: 1})
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.picked != 0 || fmt.Sprint(w.log) != "[provision:romeo-potion juliet60 romeo100]" {
		t.Errorf("a banked potion: log %v, picked %d", w.log, w.picked)
	}
	w = newRJWorld(t, RomeoSpokeApothecary, nil, map[int]int{ObjCadavaBerries: 4})
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.picked != 0 || fmt.Sprint(w.log) != "[provision:romeo-potion apothecary50 juliet60 romeo100]" {
		t.Errorf("a banked berry: log %v, picked %d", w.log, w.picked)
	}
	// Both the potion and berries held: the potion goes to Juliet first, so the Apothecary's refusal to brew a second never
	// comes up.
	w = newRJWorld(t, RomeoSpokeApothecary, map[int]int{ObjCadavaPotion: 1, ObjCadavaBerries: 1}, nil)
	if why := w.run(); why != "" || w.log[0] != "juliet60" {
		t.Errorf("both held: %q, log %v", why, w.log)
	}
}

// A pack with no room banks first (Juliet's message and the Apothecary's potion go into a free slot, and a full pack
// loses them), keeping the quest's own items.
func TestAFullPackIsBankedBeforeJulietGivesItsMessage(t *testing.T) {
	inv := map[int]int{CoinsID: 100, ObjCadavaBerries: 1}
	for i := 0; i < 26; i++ { // 26 distinct items, the coins and the berry: 28 slots
		inv[1265+i] = 1
	}
	w := newRJWorld(t, RomeoSpokeRomeo, inv, nil)
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.log[0] != "deposit" {
		t.Fatalf("log %v: the pack was full", w.log)
	}
	if w.bank[1265] != 1 || w.inv[CoinsID] != 100 {
		t.Errorf("the deposit took the wrong things: bank %v, coins %d", w.bank, w.inv[CoinsID])
	}
}

func TestDecideRomeoJulietTheNonItemStates(t *testing.T) {
	check := func(name string, s Snapshot, kind StepKind, detail func(Step) bool) {
		t.Helper()
		st := DecideRomeoJuliet(s)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", name, why)
		}
		if st.Kind != kind || (detail != nil && !detail(st)) {
			t.Errorf("%s: got %s", name, st.Describe())
		}
	}
	at := func(stage int) Snapshot {
		return (&rjWorld{stage: stage, inv: map[int]int{}, tile: VarrockWestBank}).snapshot()
	}
	check("complete", at(RomeoComplete), StepDone, nil)
	check("not started", at(RomeoNotStarted), StepTalk, func(s Step) bool { return s.Stop.ID == NpcRomeo })
	unknown := at(RomeoSpokeRomeo)
	unknown.Journal, unknown.StageKnown = StatusUnknown, false
	check("tab not coloured", unknown, StepWait, nil)
	unread := at(RomeoSpokeRomeo)
	unread.StageKnown = false
	check("stage unread", unread, StepWait, nil)
	odd := at(RomeoSpokeRomeo)
	odd.Stage = 77
	check("a stage the content does not have", odd, StepWait, nil)
	for stage, npc := range map[int]int{RomeoSpokeRomeo: NpcJuliet, RomeoPassedMessage: NpcFatherLawrence, RomeoSpokeFather: NpcApothecary, RomeoJulietCrypt: NpcRomeo} {
		check(fmt.Sprintf("stage %d", stage), at(stage), StepTalk, func(s Step) bool { return s.Stop.ID == npc })
	}
	// The stops are where the content puts the NPCs; Juliet is upstairs.
	if JulietAnchor.Level != 1 || RomeoStartStop.ID != NpcRomeo || ApothecaryStop.ID != NpcApothecary || LawrenceStop.ID != NpcFatherLawrence {
		t.Errorf("stops: %+v %+v", JulietAnchor, RomeoStartStop)
	}
}

// For every stage and every combination of the three quest items in the pack and the bank the module returns a well-formed
// step; it never sends the account to Juliet at stage 20 while the message is in the bank unread, nor to the Apothecary
// at stage 50 without berries, nor to Romeo at stage 20 without the message.
func TestDecideRomeoJulietNeverTalksWithoutWhatTheTalkNeeds(t *testing.T) {
	items := []int{ObjJulietMessage, ObjCadavaBerries, ObjCadavaPotion}
	for _, stage := range []int{0, 10, 20, 30, 40, 50, 60} {
		for packMask := 0; packMask < 8; packMask++ {
			for bankMask := 0; bankMask < 8; bankMask++ {
				for ran := 0; ran < 4; ran++ {
					inv, bank := map[int]int{}, map[int]int{}
					for i, id := range items {
						if packMask&(1<<i) != 0 {
							inv[id] = 1
						}
						if bankMask&(1<<i) != 0 {
							bank[id] = 1
						}
					}
					w := &rjWorld{stage: stage, inv: inv, bank: bank, ran: map[string]bool{}, failed: map[string]bool{}, tile: VarrockWestBank}
					if ran&1 != 0 {
						w.ran["provision:"+KitRomeoMessage] = true
					}
					if ran&2 != 0 {
						w.failed["provision:"+KitRomeoPotion] = true
					}
					st := DecideRomeoJuliet(w.snapshot())
					if why := ValidateStep(st); why != "" {
						t.Fatalf("stage %d pack %v bank %v: malformed %s: %s", stage, inv, bank, st.Describe(), why)
					}
					if st.Kind != StepTalk {
						continue
					}
					switch {
					case stage == RomeoSpokeJuliet && st.Stop.ID == NpcRomeo && inv[ObjJulietMessage] == 0:
						t.Fatalf("stage 20 pack %v: to Romeo without the message", inv)
					case stage == RomeoSpokeJuliet && st.Stop.ID == NpcJuliet && inv[ObjJulietMessage] == 0 && bank[ObjJulietMessage] > 0 && ran&1 == 0:
						t.Fatalf("stage 20 pack %v bank %v: to Juliet with the message banked and the bank unread", inv, bank)
					case stage == RomeoSpokeApothecary && st.Stop.ID == NpcApothecary && (inv[ObjCadavaBerries] == 0 || inv[ObjCadavaPotion] > 0):
						t.Fatalf("stage 50 pack %v: to the Apothecary without berries, or with a potion already", inv)
					case stage == RomeoSpokeApothecary && st.Stop.ID == NpcJuliet && inv[ObjCadavaPotion] == 0:
						t.Fatalf("stage 50 pack %v: to Juliet without the potion", inv)
					}
				}
			}
		}
	}
}

// ---- the kits through the real planner ----

func TestTheMessageKitReadsTheBankAndIsSatisfiedWhenItIsEmpty(t *testing.T) {
	kit := romeoMessageKit()
	if len(kit) != 1 || !kit[0].Optional || !kit[0].Untradeable || len(kit[0].Sources) != 0 || kit[0].ID != ObjJulietMessage {
		t.Fatalf("kit %+v", kit)
	}
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, FreeSlots: 20}); p.Phase != PhaseBank || len(p.Withdraw) != 0 {
		t.Errorf("unread bank: %s", p.Summary())
	}
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, MuleKnown: true,
		Mule: map[int]int{ObjJulietMessage: 5}}); p.Phase != PhaseSatisfied || len(p.MuleAsk) != 0 {
		t.Errorf("empty bank: %s (an untradeable item is never asked of the mule)", p.Summary())
	}
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjJulietMessage: 1}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseBank {
		t.Errorf("banked message: %s", p.Summary())
	}
}

// The berry is wanted only while the potion is short, and then comes from the bank, the mule, and the three ground spawns
// in turn - never the Apothecary's own shelf, which has none.
func TestThePotionKitWantsABerryOnlyWhileThePotionIsShort(t *testing.T) {
	kit := romeoPotionKit()
	for _, l := range kit {
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{ObjCadavaPotion: 1}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseSatisfied || len(p.Tasks) != 0 {
		t.Errorf("potion held: %s", p.Summary())
	}
	st := ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, Exhausted: map[string]bool{}}
	var order []tile.Tile
	for i := 0; i < 4; i++ {
		p := PlanProvision(kit, st)
		if i == 3 {
			if p.Phase != PhaseBlocked || len(p.Blocked) != 1 {
				t.Fatalf("every spawn gone: %s", p.Summary())
			}
			break
		}
		if p.Phase != PhaseSource || p.Tasks[0].Line.ID != ObjCadavaBerries || p.Tasks[0].Qty != 1 {
			t.Fatalf("round %d: %s", i, p.Summary())
		}
		order = append(order, p.Tasks[0].Source.Tile)
		st.Exhausted[SourceKey(p.Tasks[0].Line, p.Tasks[0].Source)] = true
	}
	if fmt.Sprint(order) != fmt.Sprint(CadavaSpawns) {
		t.Errorf("spawn order %v, want %v", order, CadavaSpawns)
	}
	// The mule's berry comes before the ground; the potion is never asked of it.
	p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, MuleKnown: true,
		Mule: map[int]int{ObjCadavaBerries: 9, ObjCadavaPotion: 9}})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != ObjCadavaBerries {
		t.Errorf("the mule ask %s", p.Summary())
	}
}

func TestTheQuestKeepsItsItemsAndTheRecordAgreesWithTheFacts(t *testing.T) {
	tools := map[int]bool{}
	for _, id := range RomeoJuliet.ToolIDs {
		tools[id] = true
	}
	for _, id := range []int{ObjJulietMessage, ObjCadavaBerries, ObjCadavaPotion, CoinsID} {
		if !tools[id] {
			t.Errorf("obj %d is not in Module.ToolIDs: an engine deposit would bank it", id)
		}
	}
	if RomeoJuliet.MaxDeaths != 2 || RomeoJuliet.ReadStage == nil || RomeoJuliet.Decide == nil || !RomeoJuliet.BankSet {
		t.Errorf("module %+v", RomeoJuliet)
	}
	if RomeoJulietRecord.QuestPoints != LadderFacts[RomeoJulietID].QP || RomeoJulietRecord.Name != LadderFacts[RomeoJulietID].Name ||
		RomeoJulietRecord.Name != QuestTabName(RomeoJulietID) {
		t.Errorf("the record %+v disagrees with the ladder facts %+v", RomeoJulietRecord, LadderFacts[RomeoJulietID])
	}
	// The berry patch is one place: the cadava berries and the redberries of Goblin Diplomacy lie within a dozen tiles.
	for _, c := range CadavaSpawns {
		for _, r := range RedberrySpawns {
			if tile.Chebyshev(c, r) > 14 {
				t.Errorf("cadava %v and redberry %v are not in one patch", c, r)
			}
		}
	}
}
