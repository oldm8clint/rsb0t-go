package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Monk's Friend and the Clock Tower (W13, 17 Oct 2026): the journal parsers over the pages the content renders, the decisions
// over whole simulated quests (a world that answers every step the way the content's scripts do), every state of both Decide
// functions, the kits through the real planner, and the pure helpers of the ogre guard.

// ---------------------------------------------------------------------------------------------------------------------
// The simulated world
// ---------------------------------------------------------------------------------------------------------------------

// kcWorld is the state both quests' simulations share: the pack, the bank, the engine's Ran/Failed memory.
type kcWorld struct {
	t         *testing.T
	inv       map[int]int
	bank      map[int]int
	bankKnown bool
	ran       map[string]bool
	failed    map[string]bool
	tile      tile.Tile
	log       []string
	maxHP     int
	// ground are the items a ground source can supply, by object id; a source for anything else fails.
	ground map[int]bool
	// shop are the items a shop source can supply (coins are not charged).
	shop map[int]bool
	// skill are the items a skill source can supply.
	skill map[int]bool
}

func newKCWorld(t *testing.T, inv, bank map[int]int) *kcWorld {
	w := &kcWorld{t: t, inv: map[int]int{}, bank: map[int]int{}, ran: map[string]bool{}, failed: map[string]bool{}, maxHP: 40,
		ground: map[int]bool{}, shop: map[int]bool{}, skill: map[int]bool{}}
	for id, n := range inv {
		w.inv[id] = n
	}
	for id, n := range bank {
		w.bank[id] = n
	}
	return w
}

func (w *kcWorld) used() int {
	n := 0
	for _, c := range w.inv {
		if c > 0 {
			n++
		}
	}
	return n
}

func (w *kcWorld) base(j Status) Snapshot {
	invIDs := map[int]int{}
	for id, n := range w.inv {
		if n > 0 {
			invIDs[id] = n
		}
	}
	s := Snapshot{Journal: j, Inv: map[string]int{}, InvIDs: invIDs, Worn: map[string]int{}, WornIDs: map[int]int{},
		FreeSlots: 28 - w.used(), Tile: w.tile, TileKnown: true, HP: w.maxHP, MaxHP: w.maxHP,
		Levels: map[string]int{"hitpoints": w.maxHP, "woodcutting": 5}, Failed: map[string]bool{}, Blocked: map[string]bool{}, Ran: map[string]bool{},
		QuestsDone: map[string]bool{}}
	for k, v := range w.ran {
		s.Ran[k] = v
	}
	for k, v := range w.failed {
		s.Failed[k] = v
	}
	if w.bankKnown {
		s.BankKnown = true
		s.BankIDs = map[int]int{}
		for id, n := range w.bank {
			s.BankIDs[id] = n
		}
	}
	return s
}

// provision plays the real planner over the world: the bank's lines are drawn, a ground, skill or shop task's quantity
// arrives if the world has that source; an unsourced line is blocked.
func (w *kcWorld) provision(st Step) bool {
	for pass := 0; pass < 16; pass++ {
		pack := map[int]int{}
		for id, n := range w.inv {
			if n > 0 {
				pack[id] = n
			}
		}
		bank := map[int]int{}
		for id, n := range w.bank {
			bank[id] = n
		}
		plan := PlanProvision(st.Kit, ProvisionState{Pack: pack, Worn: map[int]int{}, Bank: bank, BankKnown: w.bankKnown,
			FreeSlots: 28 - w.used(), Levels: map[string]int{"woodcutting": 5}, Exhausted: map[string]bool{}})
		switch plan.Phase {
		case PhaseSatisfied:
			return true
		case PhaseBank:
			w.bankKnown = true
			moved := false
			for _, c := range plan.Withdraw {
				n := minInt(c.Count, w.bank[c.ID])
				if c.ID == CoinsID {
					n = c.Count
				}
				if n > 0 {
					w.bank[c.ID] -= n
					w.inv[c.ID] += n
					moved = true
				}
			}
			if !moved && len(plan.Short) > 0 && plan.Withdraw == nil && !w.bankKnown {
				return false
			}
			if !moved {
				// A read-only visit: the plan asks again with the bank known.
				continue
			}
		case PhaseSource:
			task := plan.Tasks[0]
			ok := false
			switch task.Source.Kind {
			case SourceGround:
				ok = w.ground[task.Line.ID]
			case SourceShop:
				ok = w.shop[task.Line.ID]
			case SourceSkill:
				ok = w.skill[task.Line.ID]
			}
			if !ok {
				return false
			}
			w.inv[task.Line.ID] += task.Qty
		case PhaseMule:
			return false
		default:
			return false
		}
	}
	return false
}

func (w *kcWorld) deposit(st Step) {
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
	w.bankKnown = true
}

// ---------------------------------------------------------------------------------------------------------------------
// Monk's Friend
// ---------------------------------------------------------------------------------------------------------------------

// mfJournalFixtures are the pages drunkmonk_journal.rs2 renders, every string of a branch joined in order (the journal is
// `~quest_journal(title, $text)` over exactly that text, `|` the client's line break). 50 and 60 are one page.
var mfBase = "@str@Brother Omad asked me to recover a child's blanket.|@str@I found the secret cave and gave back the blanket.||"

var mfJournalFixtures = map[int]string{
	MonkNotStarted: "@dbl@I can start this quest by speaking to @dre@Brother Omad@dbl@ in the|@dre@Monastery@dbl@ south of @dre@Ardougne",
	MonkSpokeOmad: "@dre@Brother Omad@dbl@ asked me to recover a @dre@child's blanket@dbl@.|I need to find a @dre@secret cave@dbl@ that is hidden under a @dre@ring of" +
		"|@dre@stones@dbl@ in the @dre@forest@dbl@ south of @dre@Ardougne",
	MonkRetrievedBlanket: "@str@Brother Omad asked me to recover a child's blanket.|@str@I found the secret cave and gave back the blanket.",
	MonkLookingCedric:    mfBase + "@dbl@I agreed to find @dre@Brother Cedric@dbl@. He is somewhere in the|@dre@forest@dbl@ south of @dre@Ardougne",
	MonkFindingWater: mfBase + "@str@I found Brother Cedric in the forest south of Ardougne.|" +
		"@dbl@I need to take him a @dre@jug of water",
	MonkMendingCart: mfBase + "@str@I found Brother Cedric in the forest south of Ardougne. I|@str@sobered him up.|" +
		"@dbl@He needs some @dre@wood@dbl@ for his cart",
	MonkFixedCart: mfBase + "@str@I found Brother Cedric in the forest south of Ardougne. I|@str@sobered him up and I helped him fix his cart.|" +
		"@dre@Brother Cedric@dbl@ said that I should tell @dre@Brother Omad@dbl@ that|he is on the way.",
	MonkComplete: mfBase + "@str@I found Brother Cedric in the forest south of Ardougne. I|@str@sobered him up and I helped him fix his cart.||" +
		"@str@I had a party with the Monks. There were party balloons|@str@and we danced the night away!||" + "@red@QUEST COMPLETE!",
}

func TestParseMonksFriendJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, page := range mfJournalFixtures {
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseMonksFriendJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d page %q: parsed %d %v", stage, page[:40], got, ok)
			}
		}
	}
	// The header and the history carry no marker of a later stage: every page from 20 on repeats the blanket line, which is
	// why the newest marker is tried first.
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseMonksFriendJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

// mfWorld is Monk's Friend as the content runs it: %drunkmonkquest and the scripts' answers to each talk.
type mfWorld struct {
	*kcWorld
	stage int
}

func newMFWorld(t *testing.T, stage int, inv, bank map[int]int) *mfWorld {
	w := &mfWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage}
	w.tile = ArdougneSouthBank
	return w
}

func (w *mfWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case MonkNotStarted:
		j = StatusNotStarted
	case MonkComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.stage, true
	if w.stage == 60 { // the journal renders 50 and 60 alike
		s.Stage = MonkMendingCart
	}
	return s
}

func (w *mfWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		key := "provision:" + st.KitName
		if w.provision(st) {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
		}
		w.log = append(w.log, key)
	case StepScanBank:
		w.bankKnown = true
		w.log = append(w.log, "scanbank")
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepTalk:
		w.talk(st)
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *mfWorld) talk(st Step) {
	switch st.Stop.ID {
	case NpcBrotherOmad:
		switch w.stage {
		case MonkNotStarted:
			w.stage = MonkSpokeOmad // omad_help
		case MonkSpokeOmad:
			if w.inv[ObjChildsBlanket] > 0 { // omad_have_blanket
				w.inv[ObjChildsBlanket]--
				w.stage = MonkRetrievedBlanket
			}
		case MonkRetrievedBlanket:
			w.stage = MonkLookingCedric // omad_where_look
		default:
			w.t.Fatalf("a talk with Omad at stage %d", w.stage)
		}
		w.log = append(w.log, fmt.Sprintf("omad%d", w.stage))
	case NpcBrotherCedric:
		switch w.stage {
		case MonkLookingCedric:
			w.stage = MonkFindingWater // cedric_okay
		case MonkFindingWater:
			if w.inv[ObjJugOfWater] > 0 { // cedric_need_water, then cedric_fix_cart with "Yes, I'd be happy to!"
				w.inv[ObjJugOfWater]--
				w.stage = 60
			}
		case 50, 60:
			if w.stage == 50 {
				w.stage = 60 // cedric_fix_cart
			} else if w.inv[ObjLogs] > 0 { // cedric_get_wood
				w.inv[ObjLogs]--
				w.stage = MonkFixedCart
			}
		default:
			w.t.Fatalf("a talk with Cedric at stage %d", w.stage)
		}
		w.log = append(w.log, fmt.Sprintf("cedric%d", w.stage))
	default:
		w.t.Fatalf("a talk with npc %d", st.Stop.ID)
	}
}

func (w *mfWorld) custom(st Step) {
	switch st.Custom {
	case CustomMonkBlanket:
		w.inv[ObjChildsBlanket]++
	case CustomMonkJug:
		if w.inv[ObjJug] > 0 {
			w.inv[ObjJug]--
			w.inv[ObjJugOfWater]++
		}
	case CustomMonkParty:
		w.stage = MonkComplete
	default:
		w.t.Fatalf("unexpected custom %q", st.Custom)
	}
	w.log = append(w.log, st.Custom)
}

func (w *mfWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 60; i++ {
		st := DecideMonksFriend(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("60 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// A fresh account whose bank holds a jug of water and an axe does the whole quest in the content's order.
func TestAFreshAccountDoesMonksFriendInOrder(t *testing.T) {
	w := newMFWorld(t, MonkNotStarted, map[int]int{CoinsID: 100}, map[int]int{ObjJugOfWater: 1, ObjIronAxe: 1, ObjLogs: 1})
	w.ground[ObjLogs] = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	// The bank is read by the first kit (the jug of water is in it), so the log's kit needs no separate visit.
	want := []string{"omad10", CustomMonkBlanket, "omad20", "omad30", "cedric40", "provision:" + KitMonkJug, "cedric60",
		"provision:" + KitMonkLogs, "cedric70", CustomMonkParty}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	for _, id := range []int{ObjChildsBlanket, ObjJug, ObjJugOfWater, ObjLogs} {
		if w.inv[id] != 0 {
			t.Errorf("%d of obj %d left in the pack", w.inv[id], id)
		}
	}
}

// An account with an empty bank buys the jug (the shop is the last rung) and fills it; the log is chopped with the shop's axe.
func TestAnEmptyAccountBuysAJugAndChopsALog(t *testing.T) {
	w := newMFWorld(t, MonkFindingWater, map[int]int{CoinsID: 200}, nil)
	w.shop[ObjJug] = true
	w.shop[ObjIronAxe] = true
	w.skill[ObjLogs] = true
	w.bankKnown = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"provision:" + KitMonkJug, CustomMonkJug, "cedric60", "provision:" + KitMonkLogs, "cedric70", CustomMonkParty}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
}

// A jug that cannot be had parks the quest with its reason, for half an hour, not a retry.
func TestNoJugParksTheQuestWithAReason(t *testing.T) {
	w := newMFWorld(t, MonkFindingWater, map[int]int{CoinsID: 200}, nil)
	w.bankKnown = true
	why, park := w.run()
	if !strings.Contains(why, "no jug of water") || park != 30*time.Minute {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
	if want := []string{"provision:" + KitMonkJug}; !slices.Equal(w.log, want) {
		t.Errorf("log %v, want one failed kit", w.log)
	}
	// The same with the logs.
	w = newMFWorld(t, MonkMendingCart, map[int]int{CoinsID: 200}, nil)
	w.bankKnown = true
	why, park = w.run()
	if !strings.Contains(why, "no log or axe") || park != 30*time.Minute {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
}

// The party is a custom step, never a talk: %drunkmonkquest moves only in the closing queue, so a talk that returned with the
// dialogue would find stage 70 and start the party twice.
func TestStage70IsNeverATalkWithOmad(t *testing.T) {
	w := newMFWorld(t, MonkFixedCart, nil, nil)
	st := DecideMonksFriend(w.snapshot())
	if st.Kind != StepCustom || st.Custom != CustomMonkParty {
		t.Fatalf("stage 70 decided %s", st.Describe())
	}
}

// The blanket is handed in at stage 10 only when it is held (the journal reads the same with or without it).
func TestTheBlanketIsHandedInOnlyWhenHeld(t *testing.T) {
	w := newMFWorld(t, MonkSpokeOmad, nil, nil)
	st := DecideMonksFriend(w.snapshot())
	if st.Kind != StepCustom || st.Custom != CustomMonkBlanket {
		t.Fatalf("stage 10 without the blanket decided %s", st.Describe())
	}
	w.inv[ObjChildsBlanket] = 1
	st = DecideMonksFriend(w.snapshot())
	if st.Kind != StepTalk || st.Stop.ID != NpcBrotherOmad {
		t.Fatalf("stage 10 with the blanket decided %s", st.Describe())
	}
	// An account that lost its stage reading (stage 0 with a blanket) hands it in too.
	w = newMFWorld(t, MonkNotStarted, map[int]int{ObjChildsBlanket: 1}, nil)
	s := w.snapshot()
	s.Journal = StatusInProgress
	if st = DecideMonksFriend(s); st.Kind != StepTalk {
		t.Fatalf("stage 0 in progress with the blanket decided %s", st.Describe())
	}
}

func TestDecideMonksFriendTheNonItemStates(t *testing.T) {
	check := func(name string, s Snapshot, kind StepKind, detail func(Step) bool) {
		t.Helper()
		st := DecideMonksFriend(s)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", name, why)
		}
		if st.Kind != kind || (detail != nil && !detail(st)) {
			t.Errorf("%s: got %s", name, st.Describe())
		}
	}
	at := func(stage int) Snapshot { return newMFWorld(t, stage, nil, nil).snapshot() }
	check("complete", at(MonkComplete), StepDone, nil)
	check("not started", at(MonkNotStarted), StepTalk, func(s Step) bool { return s.Stop.ID == NpcBrotherOmad })
	unknown := at(MonkSpokeOmad)
	unknown.Journal, unknown.StageKnown = StatusUnknown, false
	check("tab not coloured", unknown, StepWait, nil)
	unread := at(MonkSpokeOmad)
	unread.StageKnown = false
	check("stage unread", unread, StepWait, nil)
	odd := at(MonkSpokeOmad)
	odd.Stage = 77
	check("a stage the content does not have", odd, StepWait, nil)
	check("stage 20", at(MonkRetrievedBlanket), StepTalk, func(s Step) bool {
		return s.Stop.ID == NpcBrotherOmad && slices.Equal(s.Stop.Prefer, []string{"Who's Brother Cedric?", "Where should I look?"})
	})
	check("stage 30", at(MonkLookingCedric), StepTalk, func(s Step) bool { return s.Stop.ID == NpcBrotherCedric })
	// A full pack is banked before the blanket, the jug or the log is taken.
	full := newMFWorld(t, MonkSpokeOmad, nil, nil)
	for i := 0; i < 27; i++ {
		full.inv[1000+i] = 1
	}
	check("a full pack", full.snapshot(), StepDeposit, func(s Step) bool {
		return s.BankSet && s.Bank == ArdougneSouthBank && slices.Contains(s.KeepIDs, ObjJug)
	})
	// The stops are the content's npcs, and the talks that carry a menu name the options the scripts offer.
	if OmadStartStop.ID != NpcBrotherOmad || CedricStop.ID != NpcBrotherCedric || !slices.Equal(CedricStop.Prefer, []string{"Yes, I'd be happy to!"}) ||
		!slices.Equal(OmadStartStop.Prefer, []string{"Why can't you sleep, what's wrong?", "Can I help at all?"}) {
		t.Errorf("stops: %+v %+v", OmadStartStop, CedricStop)
	}
}

// For every stage and every combination of the quest's items in the pack and the bank, and every memory of the engine's, the
// module returns a well-formed step, and it never sends the account somewhere with nothing to do there.
func TestDecideMonksFriendEveryStateYieldsAWellFormedStep(t *testing.T) {
	items := []int{ObjChildsBlanket, ObjJug, ObjJugOfWater, ObjLogs, ObjIronAxe}
	for _, stage := range []int{0, 10, 20, 30, 40, 50, 70, 80} {
		for packMask := 0; packMask < 1<<len(items); packMask++ {
			for bankMask := 0; bankMask < 1<<len(items); bankMask += 7 {
				for mem := 0; mem < 8; mem++ {
					inv, bank := map[int]int{}, map[int]int{}
					for i, id := range items {
						if packMask&(1<<i) != 0 {
							inv[id] = 1
						}
						if bankMask&(1<<i) != 0 {
							bank[id] = 1
						}
					}
					w := newMFWorld(t, stage, inv, bank)
					w.bankKnown = mem&1 != 0
					if mem&2 != 0 {
						w.failed["provision:"+KitMonkJug] = true
					}
					if mem&4 != 0 {
						w.failed["provision:"+KitMonkLogs] = true
					}
					st := DecideMonksFriend(w.snapshot())
					if why := ValidateStep(st); why != "" {
						t.Fatalf("stage %d pack %v bank %v mem %d: malformed %s: %s", stage, inv, bank, mem, st.Describe(), why)
					}
					if st.Kind != StepTalk {
						continue
					}
					switch {
					case stage == MonkSpokeOmad && st.Stop.ID == NpcBrotherOmad && inv[ObjChildsBlanket] == 0:
						t.Fatalf("stage 10 pack %v: to Omad without the blanket", inv)
					case stage == MonkFindingWater && st.Stop.ID == NpcBrotherCedric && inv[ObjJugOfWater] == 0:
						t.Fatalf("stage 40 pack %v: to Cedric without the water", inv)
					case stage == MonkMendingCart && st.Stop.ID == NpcBrotherCedric && inv[ObjLogs] == 0:
						t.Fatalf("stage 50 pack %v: to Cedric without a log", inv)
					}
				}
			}
		}
	}
}

func TestMonksFriendKitsThroughThePlanner(t *testing.T) {
	// The jug of water is drawn from the bank first; the empty jug is wanted only while there is none.
	kit := monkJugKit()
	for _, l := range kit {
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjJugOfWater: 1, ObjJug: 5}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseBank ||
		len(p.Withdraw) == 0 || p.Withdraw[0].ID != ObjJugOfWater {
		t.Errorf("a jug of water in the bank: %s", p.Summary())
	}
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjJug: 5}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseBank ||
		len(p.Withdraw) != 1 || p.Withdraw[0].ID != ObjJug {
		t.Errorf("only jugs in the bank: %s", p.Summary())
	}
	// The mule publishes 206 jugs: one bundled ask, no walk.
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, MuleKnown: true,
		Mule: map[int]int{ObjJug: 206}}); p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != ObjJug || p.MuleAsk[0].Count != 1 {
		t.Errorf("the mule's jugs: %s", p.Summary())
	}
	// Nothing anywhere: Port Khazard's store, two coins.
	p := PlanProvision(kit, ProvisionState{Pack: map[int]int{CoinsID: 50}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20})
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceShop || p.Tasks[0].Source.Shop.NpcID != NpcKhazardShopkeeper || p.ShopCoins != 2 {
		t.Errorf("the store: %s", p.Summary())
	}
	// A jug of water in the pack serves the line: the empty jug is not wanted.
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{ObjJugOfWater: 1}, Bank: map[int]int{ObjJug: 5}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseSatisfied {
		t.Errorf("with the water in hand: %s", p.Summary())
	}

	// The log: the best banked axe, else the shop's iron axe; a banked log first, else a tree beside the cart.
	s := Snapshot{BankKnown: true, BankIDs: map[int]int{ObjSteelAxe: 1}, Levels: map[string]int{"woodcutting": 10}}
	lk := monkLogsKit(s)
	if lk[0].ID != ObjSteelAxe || lk[1].ID != ObjLogs || lk[1].Sources[0].Kind != SourceSkill || lk[1].Sources[0].Method != MethodChopLogs || lk[1].Sources[0].Tile != MonkTree {
		t.Errorf("the logs kit %+v", lk)
	}
	s.BankIDs = map[int]int{}
	if lk = monkLogsKit(s); lk[0].ID != ObjIronAxe || lk[0].Sources[0].Shop.NpcID != AemadShop.NpcID {
		t.Errorf("the logs kit with an empty bank %+v", lk)
	}
	for _, l := range lk {
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	// An unread bank is read before an axe is shopped for.
	w := newMFWorld(t, MonkMendingCart, nil, nil)
	if st := DecideMonksFriend(w.snapshot()); st.Kind != StepScanBank || !st.BankSet {
		t.Errorf("an unread bank at stage 50: %s", st.Describe())
	}
	w.inv[ObjSteelAxe] = 1
	if st := DecideMonksFriend(w.snapshot()); st.Kind != StepProvision || st.KitName != KitMonkLogs {
		t.Errorf("an axe in the pack: %s", st.Describe())
	}
}

func TestMonksFriendModuleAndRecord(t *testing.T) {
	m := MonksFriend
	if m.Record.ID != MonksFriendID || m.Record.Name != "Monk's Friend" || m.Record.QuestPoints != 1 || !m.NoRetaliate || m.MaxDeaths != 2 ||
		m.ReadStage == nil || m.ExclusiveDone == nil || !m.BankSet || m.Bank != ArdougneSouthBank {
		t.Errorf("module %+v", m)
	}
	if len(m.Record.Requirements.Skills) != 1 || m.Record.Requirements.Skills[0] != (SkillReq{Skill: "hitpoints", Level: 20}) {
		t.Errorf("requirements %+v", m.Record.Requirements)
	}
	for _, id := range []int{ObjChildsBlanket, ObjJug, ObjJugOfWater, ObjLogs, CoinsID, ObjIronAxe, ObjRuneAxe} {
		if !slices.Contains(m.ToolIDs, id) {
			t.Errorf("object %d is not a tool of the quest, so a bank visit would bank it", id)
		}
	}
	if keep := MonkKeepIDs(); !slices.Equal(keep, m.ToolIDs) {
		t.Errorf("the keep list %v is not the tools %v", keep, m.ToolIDs)
	}
	// The slot is held until the blanket is in hand and not after.
	w := newMFWorld(t, MonkSpokeOmad, nil, nil)
	if MonkPastBlanket(w.snapshot()) {
		t.Error("past the blanket with no blanket")
	}
	w.inv[ObjChildsBlanket] = 1
	if !MonkPastBlanket(w.snapshot()) {
		t.Error("not past the blanket holding it")
	}
	for _, stage := range []int{MonkRetrievedBlanket, MonkLookingCedric, MonkFindingWater, MonkMendingCart, MonkFixedCart, MonkComplete} {
		if !MonkPastBlanket(newMFWorld(t, stage, nil, nil).snapshot()) {
			t.Errorf("stage %d is not past the blanket", stage)
		}
	}
	if MonkPastBlanket(newMFWorld(t, MonkNotStarted, nil, nil).snapshot()) {
		t.Error("stage 0 is past the blanket")
	}
}

func TestTheThievesCaveIsAPocketOfExactlyItsOwnRoom(t *testing.T) {
	in := []tile.Tile{MonkCaveFoot, MonkBlanketSpawn, {X: 2561, Z: 9621}, {X: 2579, Z: 9608}, {X: 2565, Z: 9613}}
	for _, p := range in {
		if !InMonkCave(p) {
			t.Errorf("%v is not in the cave", p)
		}
	}
	// The Clock Tower's cellar, its red cog, its ogres and the surface are not.
	out := []tile.Tile{{X: 2566, Z: 9643}, {X: 2583, Z: 9613}, {X: 2586, Z: 9614}, MonkRingLadder, {X: 2566, Z: 9627}, {X: 2561, Z: 9623}, {X: 2561, Z: 9621, Level: 1}}
	for _, p := range out {
		if InMonkCave(p) {
			t.Errorf("%v is in the cave", p)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The Clock Tower
// ---------------------------------------------------------------------------------------------------------------------

// ctJournal is the in-progress page cog_journal.rs2 renders for the colours of mask placed.
func ctJournal(mask int) string {
	page := "@str@I spoke to Brother Kojo at the Clock Tower South of|@str@Argougne and agreed to help him repair the clock.|" +
		"@dbl@To repair the clock I need to find the four coloured cogs|and place them on the four correctly coloured spindles.|"
	for i, c := range []struct {
		name string
		bit  int
	}{{"Blue", CogBitBlue}, {"Black", CogBitBlack}, {"White", CogBitWhite}, {"Red", CogBitRed}} {
		tail := "|"
		if i == 3 {
			tail = ""
		}
		if mask&c.bit == 0 {
			page += "@dbl@I haven't placed the @dre@" + c.name + " Cog@dbl@ on its @dre@spindle@dbl@ yet" + tail
		} else {
			page += "@str@I have successfully placed the " + c.name + " Cog on its spindle" + tail
		}
	}
	return page
}

const ctJournalStart = "@str@I spoke to Brother Kojo at the Clock Tower South of|@str@Argougne and agreed to help him repair the clock.|"

var ctJournalNotStarted = "@dbl@I can start this quest by talking to @dre@Brother Kojo@dbl@ at the|@dre@Clock Tower@dbl@ which is located @dre@South@dbl@ of @dre@Ardougne|"
var ctJournalAllPlaced = ctJournalStart + "@str@I have placed all four cogs successfully on the spindles.|" + "@dbl@I should speak to @dre@Brother Kojo@dbl@ and claim my @dre@reward"
var ctJournalComplete = ctJournalStart + "@str@I have placed all four cogs successfully on the spindles.|" + "@str@Brother Kojo was grateful for all my help and rewarded me.||" + "@red@QUEST COMPLETE!"

func TestParseClockTowerJournalReadsEveryRenderedPage(t *testing.T) {
	for mask := 0; mask < 16; mask++ {
		page := ctJournal(mask)
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseClockTowerJournal(lines)
			if !ok || got != mask {
				t.Errorf("mask %04b: parsed %d %v from %q", mask, got, ok, page)
			}
		}
	}
	for page, want := range map[string]int{ctJournalNotStarted: CogStageNotStarted, ctJournalAllPlaced: CogAllPlaced, ctJournalComplete: CogStageComplete} {
		if got, ok := ParseClockTowerJournal([]string{page}); !ok || got != want {
			t.Errorf("page %q: parsed %d %v, want %d", page, got, ok, want)
		}
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else"}} {
		if got, ok := ParseClockTowerJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

func TestCogColourTableAgreesWithItself(t *testing.T) {
	bits, objs, spindles := 0, map[int]bool{}, map[int]bool{}
	for _, c := range CogColours {
		bits |= c.Bit
		objs[c.Obj], spindles[c.Spindle] = true, true
		if got, ok := CogByName(c.Name); !ok || got != c {
			t.Errorf("CogByName(%s) = %+v", c.Name, got)
		}
		if got, ok := CogByObj(c.Obj); !ok || got != c {
			t.Errorf("CogByObj(%d) = %+v", c.Obj, got)
		}
		if c.Stand == (tile.Tile{}) || c.Spawn == (tile.Tile{}) || c.Spawn.Level != 0 {
			t.Errorf("%s: stand %v spawn %v", c.Name, c.Stand, c.Spawn)
		}
	}
	if bits != 15 || len(objs) != 4 || len(spindles) != 4 {
		t.Errorf("bits %b, %d objects, %d spindles", bits, len(objs), len(spindles))
	}
	// Red is last: the cave of ogres is entered only when the three others are done.
	if CogColours[len(CogColours)-1].Name != "red" {
		t.Errorf("the fetch order is %v", CogColours)
	}
	if c, ok := NextCog(0); !ok || c.Name != "blue" {
		t.Errorf("NextCog(0) = %v %v", c, ok)
	}
	if c, ok := NextCog(CogBitBlue | CogBitBlack | CogBitWhite); !ok || c.Name != "red" {
		t.Errorf("NextCog(three) = %v %v", c, ok)
	}
	if _, ok := NextCog(15); ok {
		t.Error("NextCog(15) found a cog")
	}
	if CogPlaced(CogAllPlaced, CogColours[0]) || CogPlaced(CogStageNotStarted, CogColours[0]) {
		t.Error("the endpoints are not masks")
	}
}

// ctWorld is the Clock Tower as the content runs it.
type ctWorld struct {
	*kcWorld
	mask      int
	started   bool
	complete  bool
	poisoned  bool
	cooled    bool
	poured    int // poison used on the trough
	cogsTaken []string
}

func newCTWorld(t *testing.T, inv, bank map[int]int) *ctWorld {
	w := &ctWorld{kcWorld: newKCWorld(t, inv, bank), started: true}
	w.tile = KojoAnchor
	return w
}

func (w *ctWorld) allPlaced() bool { return w.mask == 15 }

func (w *ctWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch {
	case w.complete:
		j = StatusComplete
	case !w.started:
		j = StatusNotStarted
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.mask, true
	if w.allPlaced() {
		s.Stage = CogAllPlaced
	}
	return s
}

func (w *ctWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		key := "provision:" + st.KitName
		if w.provision(st) {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
		}
		w.log = append(w.log, key)
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepTalk:
		if st.Stop.ID != NpcBrotherKojo {
			t.Fatalf("a talk with npc %d", st.Stop.ID)
		}
		switch {
		case !w.started:
			w.started = true
		case w.allPlaced():
			w.complete = true
			w.inv[CoinsID] += 500
		}
		w.log = append(w.log, "kojo")
	case StepOpHeld:
		if st.Op != "Drop" {
			t.Fatalf("op %q", st.Op)
		}
		w.inv[st.ItemID]--
		w.log = append(w.log, "drop")
	case StepGrabGround:
		if st.ItemID != ObjRatPoison {
			t.Fatalf("grab of %d", st.ItemID)
		}
		w.inv[ObjRatPoison]++
		w.log = append(w.log, "grab:poison")
	case StepUseOn:
		// A bucket on the well: [oplocu,_well].
		if st.ItemID != ObjBucket || st.TargetID != LocWell || w.inv[ObjBucket] == 0 {
			t.Fatalf("use %s on %s", st.Item, st.Target)
		}
		w.inv[ObjBucket]--
		w.inv[ObjBucketOfWater]++
		w.log = append(w.log, "well")
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *ctWorld) custom(st Step) {
	t := w.t
	switch st.Custom {
	case CustomCogBlue, CustomCogRed:
		name := strings.TrimPrefix(st.Custom, "cog-")
		c, _ := CogByName(name)
		w.inv[c.Obj]++
		w.cogsTaken = append(w.cogsTaken, name)
	case CustomCogBlack:
		if w.inv[ObjBucketOfWater] == 0 {
			t.Fatalf("the black cog fetched with no bucket of water")
		}
		w.inv[ObjBucketOfWater]--
		w.inv[ObjBucket]++
		w.cooled = true
		w.inv[ObjCogBlack]++
		w.cogsTaken = append(w.cogsTaken, "black")
	case CustomCogWhite:
		if !w.poisoned {
			if w.inv[ObjRatPoison] == 0 {
				t.Fatalf("the white cog fetched with no poison and the rats alive")
			}
			w.inv[ObjRatPoison]--
			w.poisoned = true
			w.poured++
		}
		w.inv[ObjCogWhite]++
		w.cogsTaken = append(w.cogsTaken, "white")
	case CustomCogPlace:
		c, ok := CogByObj(st.ItemID)
		if !ok || w.inv[c.Obj] == 0 {
			t.Fatalf("a place step for object %d that is not held", st.ItemID)
		}
		if w.mask&c.Bit != 0 {
			t.Fatalf("a place step for the %s cog whose spindle is full", c.Name)
		}
		w.inv[c.Obj]--
		w.mask |= c.Bit
	default:
		t.Fatalf("unexpected custom %q", st.Custom)
	}
	w.log = append(w.log, st.Custom)
}

func (w *ctWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 80; i++ {
		st := DecideClockTower(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.mask, w.started, w.complete, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.poisoned)
		w.apply(st)
		if fmt.Sprint(w.mask, w.started, w.complete, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.poisoned) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("80 steps and the quest is not done (mask %04b, log %v)", w.mask, w.log)
	return "", 0
}

// A fresh account with sharks in the bank does the whole quest: the food once, every cog fetched and fitted in turn, the bucket
// and the poison where the black and white legs want them, and Kojo at the end.
func TestAFreshAccountDoesTheClockTowerInOrder(t *testing.T) {
	w := newCTWorld(t, map[int]int{CoinsID: 100}, map[int]int{ObjShark: 6})
	w.started = false
	w.ground[ObjBucket] = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"kojo", "provision:" + KitCogFood,
		CustomCogBlue, CustomCogPlace,
		"provision:" + KitCogBucket, "well", CustomCogBlack, CustomCogPlace,
		"grab:poison", CustomCogWhite, CustomCogPlace,
		CustomCogRed, CustomCogPlace, "kojo"}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if !slices.Equal(w.cogsTaken, []string{"blue", "black", "white", "red"}) {
		t.Errorf("the cogs were fetched in the order %v", w.cogsTaken)
	}
	if w.inv[CoinsID] != 600 || w.inv[ObjShark] != 3 {
		t.Errorf("pack %v: the reward is 500 coins and the sharks are carried", w.inv)
	}
}

// A cog for a filled spindle is a spare the spindle refuses ("You have already placed a cog here."): it is dropped, and a cog for
// an empty one is fitted before another is fetched.
func TestACogForAFilledSpindleIsDropped(t *testing.T) {
	w := newCTWorld(t, map[int]int{ObjCogRed: 1}, nil)
	w.mask = CogBitRed | CogBitBlue
	w.ran["provision:"+KitCogFood] = true
	st := DecideClockTower(w.snapshot())
	if st.Kind != StepOpHeld || st.Op != "Drop" || st.ItemID != ObjCogRed {
		t.Fatalf("a red cog for a full spindle decided %s", st.Describe())
	}
	// A cog whose spindle is empty is fitted before another is fetched.
	w = newCTWorld(t, map[int]int{ObjCogWhite: 1}, nil)
	w.mask = CogBitBlue
	st = DecideClockTower(w.snapshot())
	if st.Kind != StepCustom || st.Custom != CustomCogPlace || st.ItemID != ObjCogWhite {
		t.Fatalf("a white cog in the pack decided %s", st.Describe())
	}
}

// Each colour's leg asks for what it needs first: the bucket of water for the black cog, the poison for the white, the food for
// the red, never in the wrong leg.
func TestEachLegAsksForItsOwnPrerequisites(t *testing.T) {
	ran := func(w *ctWorld) *ctWorld { w.ran["provision:"+KitCogFood] = true; return w }
	// Black: no water, no bucket: the kit; a bucket: the well; the water: the leg.
	w := ran(newCTWorld(t, nil, nil))
	w.mask = CogBitBlue
	if st := DecideClockTower(w.snapshot()); st.Kind != StepProvision || st.KitName != KitCogBucket {
		t.Errorf("black with nothing: %s", st.Describe())
	}
	w.inv[ObjBucket] = 1
	if st := DecideClockTower(w.snapshot()); st.Kind != StepUseOn || st.TargetID != LocWell || st.Anchor != WellStand {
		t.Errorf("black with a bucket: %s", st.Describe())
	}
	w.inv[ObjBucketOfWater] = 1
	if st := DecideClockTower(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomCogBlack {
		t.Errorf("black with the water: %s", st.Describe())
	}
	w.failed["provision:"+KitCogBucket] = true
	delete(w.inv, ObjBucketOfWater)
	delete(w.inv, ObjBucket)
	if st := DecideClockTower(w.snapshot()); st.Kind != StepWait || st.ParkFor != 30*time.Minute || !strings.Contains(st.Reason, "bucket") {
		t.Errorf("black with the kit failed: %s", st.Describe())
	}
	// White: the poison first, then the leg.
	w = ran(newCTWorld(t, nil, nil))
	w.mask = CogBitBlue | CogBitBlack
	if st := DecideClockTower(w.snapshot()); st.Kind != StepGrabGround || st.ItemID != ObjRatPoison || st.Anchor != PoisonSpawn {
		t.Errorf("white with no poison: %s", st.Describe())
	}
	w.inv[ObjRatPoison] = 1
	if st := DecideClockTower(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomCogWhite {
		t.Errorf("white with poison: %s", st.Describe())
	}
	// Red: two sharks' worth of food, or the kit once more, or a long wait.
	w = ran(newCTWorld(t, nil, nil))
	w.mask = CogBitBlue | CogBitBlack | CogBitWhite
	if st := DecideClockTower(w.snapshot()); st.Kind != StepProvision || st.KitName != KitCogFoodRed {
		t.Errorf("red with no food: %s", st.Describe())
	}
	w.ran["provision:"+KitCogFoodRed] = true
	if st := DecideClockTower(w.snapshot()); st.Kind != StepWait || st.ParkFor != 2*time.Hour || !strings.Contains(st.Reason, "ogres") {
		t.Errorf("red with the food kit run and nothing to eat: %s", st.Describe())
	}
	w.inv[ObjShark] = 1
	if st := DecideClockTower(w.snapshot()); st.Kind != StepWait {
		t.Errorf("red with one shark (%d hitpoints of food, %d wanted): %s", FoodItemHeal(ObjShark), CogRedFoodHeal, st.Describe())
	}
	w.inv[ObjShark] = 2
	if st := DecideClockTower(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomCogRed {
		t.Errorf("red with two sharks: %s", st.Describe())
	}
	delete(w.inv, ObjShark)
	w.inv[365] = 4 // four bass heal 52
	if st := DecideClockTower(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomCogRed {
		t.Errorf("red with four bass: %s", st.Describe())
	}
}

// A bank without food does not stop the three safe cogs: the quest does them and waits, parked, at the ogres.
func TestAnAccountWithNoFoodDoesTheSafeCogsAndParksAtTheOgres(t *testing.T) {
	w := newCTWorld(t, nil, nil)
	w.ground[ObjBucket] = true
	why, park := w.run()
	if !strings.Contains(why, "ogres") || park != 2*time.Hour {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
	if w.mask != CogBitBlue|CogBitBlack|CogBitWhite {
		t.Errorf("the three safe cogs are %04b placed", w.mask)
	}
	if slices.Contains(w.cogsTaken, "red") {
		t.Errorf("the red cog was fetched without food: %v", w.cogsTaken)
	}
}

func TestDecideClockTowerTheNonItemStates(t *testing.T) {
	check := func(name string, s Snapshot, kind StepKind, detail func(Step) bool) {
		t.Helper()
		st := DecideClockTower(s)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", name, why)
		}
		if st.Kind != kind || (detail != nil && !detail(st)) {
			t.Errorf("%s: got %s", name, st.Describe())
		}
	}
	w := newCTWorld(t, nil, nil)
	w.complete = true
	check("complete", w.snapshot(), StepDone, nil)
	w = newCTWorld(t, nil, nil)
	w.started = false
	check("not started", w.snapshot(), StepTalk, func(s Step) bool { return s.Stop.ID == NpcBrotherKojo })
	unknown := newCTWorld(t, nil, nil).snapshot()
	unknown.Journal, unknown.StageKnown = StatusUnknown, false
	check("tab not coloured", unknown, StepWait, nil)
	unread := newCTWorld(t, nil, nil).snapshot()
	unread.StageKnown = false
	check("stage unread", unread, StepWait, nil)
	for _, odd := range []int{CogStageNotStarted, CogStageComplete} {
		s := newCTWorld(t, nil, nil).snapshot()
		s.Stage = odd
		check(fmt.Sprintf("a stage %d on a yellow tab", odd), s, StepWait, nil)
	}
	w = newCTWorld(t, nil, nil)
	w.mask = 15
	check("all four placed", w.snapshot(), StepTalk, func(s Step) bool { return s.Stop.ID == NpcBrotherKojo })
	if !slices.Equal(KojoStop.Prefer, []string{"OK old monk, what can I do?"}) {
		t.Errorf("Kojo's preference %v", KojoStop.Prefer)
	}
}

// For every colour mask and every combination of the quest's items in the pack, and every memory of the engine's, the module
// returns a well-formed step, never fetches the colour whose spindle is full, and never walks to the ogres without food.
func TestDecideClockTowerEveryStateYieldsAWellFormedStep(t *testing.T) {
	items := []int{ObjCogWhite, ObjCogBlack, ObjCogBlue, ObjCogRed, ObjRatPoison, ObjBucket, ObjBucketOfWater, ObjShark}
	for mask := 0; mask < 16; mask++ {
		for packMask := 0; packMask < 1<<len(items); packMask++ {
			for mem := 0; mem < 16; mem++ {
				inv := map[int]int{}
				for i, id := range items {
					if packMask&(1<<i) != 0 {
						inv[id] = 1
					}
				}
				// Two cogs are never carried together.
				cogs := 0
				for _, c := range CogColours {
					cogs += inv[c.Obj]
				}
				if cogs > 1 {
					continue
				}
				w := newCTWorld(t, inv, nil)
				w.mask = mask
				if mem&1 != 0 {
					w.ran["provision:"+KitCogFood] = true
				}
				if mem&2 != 0 {
					w.failed["provision:"+KitCogBucket] = true
				}
				if mem&4 != 0 {
					w.ran["provision:"+KitCogFoodRed] = true
				}
				if mem&8 != 0 {
					w.failed["provision:"+KitCogFood] = true
				}
				st := DecideClockTower(w.snapshot())
				if why := ValidateStep(st); why != "" {
					t.Fatalf("mask %04b pack %v mem %d: malformed %s: %s", mask, inv, mem, st.Describe(), why)
				}
				if st.Kind != StepCustom {
					continue
				}
				c, ok := CogByName(strings.TrimPrefix(st.Custom, "cog-"))
				switch st.Custom {
				case CustomCogBlue, CustomCogBlack, CustomCogWhite, CustomCogRed:
					if !ok || mask&c.Bit != 0 {
						t.Fatalf("mask %04b: fetches the %s cog whose spindle is full", mask, st.Custom)
					}
					if cogs > 0 {
						t.Fatalf("mask %04b pack %v: fetches a cog while carrying one", mask, inv)
					}
					if st.Custom == CustomCogBlack && inv[ObjBucketOfWater] == 0 {
						t.Fatalf("mask %04b pack %v: the black leg with no bucket of water", mask, inv)
					}
					if st.Custom == CustomCogWhite && inv[ObjRatPoison] == 0 {
						t.Fatalf("mask %04b pack %v: the white leg with no poison", mask, inv)
					}
					if st.Custom == CustomCogRed && FoodPoolHeal(inv, cogFoodIDs) < CogRedFoodHeal {
						t.Fatalf("mask %04b pack %v: the ogres' cave with no food", mask, inv)
					}
				case CustomCogPlace:
					pc, pok := CogByObj(st.ItemID)
					if !pok || inv[pc.Obj] == 0 || mask&pc.Bit != 0 {
						t.Fatalf("mask %04b pack %v: fits object %d", mask, inv, st.ItemID)
					}
				}
			}
		}
	}
}

// A pack the trainer left full is banked before a cog is fetched (a take into a full pack is refused), with the quest's own items kept.
func TestAFullPackIsBankedBeforeACogIsFetched(t *testing.T) {
	w := newCTWorld(t, nil, nil)
	w.ran["provision:"+KitCogFood] = true
	for i := 0; i < 27; i++ {
		w.inv[1000+i] = 1
	}
	st := DecideClockTower(w.snapshot())
	if st.Kind != StepDeposit || !st.BankSet || st.Bank != ArdougneSouthBank || !slices.Contains(st.KeepIDs, ObjCogRed) || !slices.Contains(st.KeepIDs, ObjRatPoison) {
		t.Fatalf("a full pack decided %s", st.Describe())
	}
	// A cog already in the pack is fitted without a bank trip: the fitting frees the slot.
	w.inv[ObjCogBlue] = 1
	if st = DecideClockTower(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomCogPlace {
		t.Fatalf("a cog in a full pack decided %s", st.Describe())
	}
}

func TestClockTowerKitsThroughThePlanner(t *testing.T) {
	kit := cogBucketKit()
	for _, l := range append(kit, cogFoodKit()...) {
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	// The mule publishes three buckets of water; the bank's is drawn first.
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjBucketOfWater: 1}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseBank ||
		len(p.Withdraw) != 1 || p.Withdraw[0].ID != ObjBucketOfWater {
		t.Errorf("a bucket of water in the bank: %s", p.Summary())
	}
	if p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, MuleKnown: true,
		Mule: map[int]int{ObjBucketOfWater: 3}}); p.Phase != PhaseMule || p.MuleAsk[0].ID != ObjBucketOfWater {
		t.Errorf("the mule's buckets of water: %s", p.Summary())
	}
	// No bucket anywhere: the ground by the cart first, the store second.
	p := PlanProvision(kit, ProvisionState{Pack: map[int]int{CoinsID: 10}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20})
	if p.Phase != PhaseSource || p.Tasks[0].Source.Kind != SourceGround || p.Tasks[0].Source.Tile != CogBucketSpawn {
		t.Errorf("the ground spawn: %s", p.Summary())
	}
	p = PlanProvision(kit, ProvisionState{Pack: map[int]int{CoinsID: 10}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20,
		Exhausted: map[string]bool{SourceKey(kit[1], kit[1].Sources[0]): true}})
	if p.Phase != PhaseSource || p.Tasks[0].Source.Kind != SourceShop || p.Tasks[0].Source.Shop.NpcID != NpcKhazardShopkeeper {
		t.Errorf("the store after the ground spawn failed: %s", p.Summary())
	}
	// The food: three sharks, from the bank or the mule, and an empty bank is not a block.
	food := cogFoodKit()
	if p := PlanProvision(food, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjShark: 9}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseBank ||
		len(p.Withdraw) != 1 || p.Withdraw[0].ID != ObjShark || p.Withdraw[0].Count != 3 {
		t.Errorf("sharks in the bank: %s", p.Summary())
	}
	if p := PlanProvision(food, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, MuleKnown: true,
		Mule: map[int]int{ObjShark: 33165}}); p.Phase != PhaseMule || p.MuleAsk[0].ID != ObjShark || p.MuleAsk[0].Count != 3 {
		t.Errorf("sharks from the mule: %s", p.Summary())
	}
	if p := PlanProvision(food, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20}); p.Phase != PhaseSatisfied || len(p.Blocked) != 0 {
		t.Errorf("an empty bank blocks an optional kit: %s", p.Summary())
	}
}

func TestClockTowerModuleAndRecord(t *testing.T) {
	m := ClockTower
	if m.Record.ID != ClockTowerID || m.Record.Name != "Clock Tower" || m.Record.QuestPoints != 1 || !m.NoRetaliate || m.MaxDeaths != 2 || m.ReadStage == nil ||
		!m.BankSet || m.Bank != ArdougneSouthBank || m.ExclusiveDone != nil {
		t.Errorf("module %+v", m)
	}
	if len(m.Record.Requirements.Skills) != 1 || m.Record.Requirements.Skills[0] != (SkillReq{Skill: "hitpoints", Level: 30}) {
		t.Errorf("requirements %+v", m.Record.Requirements)
	}
	// One death at the red cog parks the quest for a day.
	if c := m.StepDeathCaps[CustomCogRed]; c.Max != 1 || c.Window != 24*time.Hour || c.Skip != 24*time.Hour || len(m.StepDeathCaps) != 1 {
		t.Errorf("death caps %+v", m.StepDeathCaps)
	}
	if m.Sustain.EatAt <= 0 || !m.Sustain.IsFood(ObjShark) {
		t.Errorf("sustain %+v", m.Sustain)
	}
	// Every cog, the poison, the bucket and its water, the coins and the food survive a bank visit.
	for _, id := range []int{ObjCogWhite, ObjCogBlack, ObjCogBlue, ObjCogRed, ObjRatPoison, ObjBucket, ObjBucketOfWater, CoinsID, ObjShark} {
		if !slices.Contains(m.ToolIDs, id) {
			t.Errorf("object %d is not a tool of the quest", id)
		}
	}
	if keep := CogKeepIDs(); !slices.Equal(keep, m.ToolIDs) {
		t.Errorf("the keep list %v is not the tools %v", keep, m.ToolIDs)
	}
}

func TestOgreGuardHelpers(t *testing.T) {
	cog := CogColours[3].Spawn // red, 2583,9613
	for _, c := range []struct {
		ogre tile.Tile
		want int
	}{
		{tile.Tile{X: 2586, Z: 9614}, 3}, // the nook's ogre at its spawn: footprint 2586-2587 x 9614-9615, three columns east
		{tile.Tile{X: 2585, Z: 9608}, 4}, // the second ogre's spawn: footprint 9608-9609, four rows south
		{tile.Tile{X: 2588, Z: 9606}, 6}, // the third's
		{tile.Tile{X: 2585, Z: 9613}, 2}, // two columns east, on the cog's row
		{tile.Tile{X: 2584, Z: 9614}, 1}, // diagonal, touching
		{tile.Tile{X: 2582, Z: 9612}, 0}, // its footprint covers the cog's tile
		{tile.Tile{X: 2583, Z: 9618}, 5}, // straight north: the footprint's low row is 5 up
		{tile.Tile{X: 2580, Z: 9616}, 3}, // north-west: 2580-2581 x 9616-9617
		{tile.Tile{X: 2583, Z: 9609}, 3}, // south: 2583-2584 x 9609-9610, the cog three rows above its top row
		{tile.Tile{X: 2590, Z: 9622}, 9}, // far
	} {
		if got := OgreDistance(c.ogre, cog); got != c.want {
			t.Errorf("ogre at %v is %d from the red cog, want %d", c.ogre, got, c.want)
		}
	}
	spawns := []tile.Tile{{X: 2585, Z: 9608}, {X: 2586, Z: 9614}, {X: 2588, Z: 9606}}
	// At their spawns the nearest ogre is 3 from the cog: not within the calm distance (3), so the account waits.
	if OgreCalm(spawns, cog, OgreCalmNear) {
		t.Error("calm with the ogre of the nook at its spawn, 3 from the cog")
	}
	// The same ogres, the nook's one wandered 2 east: 5 away, calm.
	if !OgreCalm([]tile.Tile{{X: 2585, Z: 9608}, {X: 2589, Z: 9615}, {X: 2588, Z: 9606}}, cog, OgreCalmNear) {
		t.Error("not calm with every ogre 4 or more away")
	}
	if !OgreCalm(nil, cog, OgreCalmNear) {
		t.Error("an empty cave is not calm")
	}
}

func TestTheClockTowerPocketsAndRooms(t *testing.T) {
	in := map[string][]tile.Tile{
		"pen":        {PenGateTile, {X: 2579, Z: 9656}, TroughStand, {X: 2587, Z: 9654}, {X: 2590, Z: 9660}, {X: 2592, Z: 9653}, {X: 2593, Z: 9652}, {X: 2595, Z: 9652}},
		"white room": {{X: 2577, Z: 9655}, {X: 2575, Z: 9656}},
		"blue room":  {{X: 2574, Z: 9633}, {X: 2572, Z: 9632}},
	}
	check := func(name string, f func(tile.Tile) bool) {
		for _, p := range in[name] {
			if !f(p) {
				t.Errorf("%v is not in the %s", p, name)
			}
		}
	}
	check("pen", InCogPen)
	check("white room", InCogWhiteRoom)
	check("blue room", InCogBlueRoom)
	// The lever's corridor, the outside of the gate, the poison room, the cellar's corridor beside the pen's south-west corner
	// (2579-2580, 9652) and the surface are none of them.
	for _, p := range []tile.Tile{CogLeverTile, PenGateOutside, PoisonSpawn, {X: 2566, Z: 3243}, {X: 2577, Z: 9655, Level: 1},
		{X: 2579, Z: 9652}, {X: 2580, Z: 9652}, {X: 2583, Z: 9654}, {X: 2591, Z: 9653}, {X: 2592, Z: 9652}, {X: 2596, Z: 9657}} {
		if InCogPen(p) || InCogWhiteRoom(p) || InCogBlueRoom(p) {
			t.Errorf("%v is in a room it should not be", p)
		}
	}
	// The red cog's stage is 6 or more tiles from every ogre's spawn.
	for _, o := range []tile.Tile{{X: 2585, Z: 9608}, {X: 2586, Z: 9614}, {X: 2588, Z: 9606}} {
		if d := OgreDistance(o, RedCogStage); d < 6 {
			t.Errorf("the staging tile is %d from the ogre at %v", d, o)
		}
	}
	// The pen, the rooms and the cave do not overlap.
	for x := 2556; x <= 2600; x++ {
		for z := 9598; z <= 9662; z++ {
			p := tile.Tile{X: x, Z: z}
			n := 0
			for _, f := range []func(tile.Tile) bool{InCogPen, InCogWhiteRoom, InCogBlueRoom, InMonkCave} {
				if f(p) {
					n++
				}
			}
			if n > 1 {
				t.Fatalf("%v is in %d rooms", p, n)
			}
		}
	}
}
