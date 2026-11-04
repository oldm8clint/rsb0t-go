package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// ---- the journal ----

// Every stage the content renders is read back as that stage, from the text the server really renders
// (fillers_journals_test.go: each branch of gobdip_journal.rs2 joined in order). The later stages contain the earlier
// stages' struck-through phrases, so only the order of the needles keeps stage 3 from reading as stage 2 or 1.
func TestParseGoblinDiplomacyJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, text := range gobdipJournalFixtures {
		// As one string and as the lines the client splits it into at each `|`.
		for _, lines := range [][]string{{text}, strings.Split(text, "|")} {
			got, ok := ParseGoblinDiplomacyJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d: read as %d (ok %v) from %q", stage, got, ok, lines)
			}
		}
	}
	if len(gobdipJournalFixtures) != 7 {
		t.Fatalf("%d rendered stages, the content has seven (0-6)", len(gobdipJournalFixtures))
	}
	for _, lines := range [][]string{nil, {""}, {"I can start this quest by talking to the Banker"}, {"@dbl@Something else entirely."}} {
		if got, ok := ParseGoblinDiplomacyJournal(lines); ok {
			t.Errorf("%q read as stage %d: an unrecognised journal is unknown, never a stage", lines, got)
		}
	}
}

// ---- a world to run the module in ----

// gdWorld is the account and the little of the world the quest touches: enough to run DecideGoblinDiplomacy step by step
// to the end and see what it does on the way. It applies each step as the content does (the scripts named in the
// comments), and provisioning runs the real PlanProvision over the world's pack and bank.
type gdWorld struct {
	t       *testing.T
	stage   int
	inv     map[int]int
	bank    map[int]int
	ran     map[string]bool
	failed  map[string]bool
	log     []string // one word per step: what happened
	farmed  int      // plain mail taken from the farm source
	berries int      // redberries taken from a ground or shop source
	shops   int      // shop sources used
	tile    tile.Tile
}

func newGDWorld(t *testing.T, stage int, inv, bank map[int]int) *gdWorld {
	if inv == nil {
		inv = map[int]int{}
	}
	if bank == nil {
		bank = map[int]int{}
	}
	return &gdWorld{t: t, stage: stage, inv: inv, bank: bank, ran: map[string]bool{}, failed: map[string]bool{},
		tile: tile.Tile{X: 3093, Z: 3243}}
}

func (w *gdWorld) snapshot() Snapshot {
	s := Snapshot{InvIDs: map[int]int{}, Inv: map[string]int{}, WornIDs: map[int]int{}, Worn: map[string]int{},
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, Tile: w.tile, TileKnown: true,
		BankKnown: true, BankIDs: map[int]int{}}
	used := 0
	for id, n := range w.inv {
		if n > 0 {
			s.InvIDs[id] = n
			if id == CoinsID {
				used++
			} else {
				used += n
			}
		}
	}
	for id, n := range w.bank {
		s.BankIDs[id] = n
	}
	s.FreeSlots = 28 - used
	for k, v := range w.ran {
		s.Ran[k] = v
	}
	for k, v := range w.failed {
		s.Failed[k] = v
	}
	switch {
	case w.stage == GobdipNotStarted:
		s.Journal = StatusNotStarted
	case w.stage >= GobdipComplete:
		s.Journal = StatusComplete
	default:
		s.Journal, s.Stage, s.StageKnown = StatusInProgress, w.stage, true
	}
	return s
}

func (w *gdWorld) take(id, n int) bool {
	if w.inv[id] < n {
		return false
	}
	w.inv[id] -= n
	return true
}

// provision runs the real planner over the world: the bank's lines are drawn, a source task's quantity arrives from it.
func (w *gdWorld) provision(st Step) bool {
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
			w.inv[task.Line.ID] += task.Qty
			switch task.Source.Kind {
			case SourceSkill:
				if task.Source.Method == MethodGoblinMail {
					w.farmed += task.Qty
				}
			case SourceGround:
				w.berries += task.Qty
			case SourceShop:
				w.shops++
				w.berries += task.Qty
				w.inv[CoinsID] -= task.Source.Price * task.Qty
			}
		default:
			return false
		}
	}
	return false
}

// apply plays one step and says what it was in one word for the log.
func (w *gdWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepTalk:
		w.talk(st)
	case StepPickLoc:
		if st.LocID != LocOnionPlant || st.ExpectItemID != ObjOnion {
			t.Fatalf("pickLoc %s: not the onion patch", st.Describe())
		}
		w.inv[ObjOnion]++
		w.log = append(w.log, "onion")
	case StepUseOn:
		w.useOn(st)
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
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *gdWorld) talk(st Step) {
	t := w.t
	switch st.Stop.ID {
	case NpcRustyAnchorBartender:
		// goblin_diplomacy_start.
		if w.stage == GobdipNotStarted {
			w.stage = GobdipStarted
		}
		w.log = append(w.log, "bartender")
	case NpcWyson:
		// wyson_the_gardener.rs2: "How about 20 coins?" is two leaves.
		if st.UntilItemID != ObjWoadLeaf {
			t.Fatalf("Wyson is not driven until the woad arrives: %s", st.Describe())
		}
		if !w.take(CoinsID, GobdipWoadFee) {
			t.Fatalf("talked to Wyson with %d coins", w.inv[CoinsID])
		}
		w.inv[ObjWoadLeaf] += 2
		w.log = append(w.log, "wyson")
	case NpcAggie:
		// aggie.rs2: the dye is chosen by the second prefer line.
		var ingredient, n, dye int
		switch {
		case strings.Contains(st.Stop.Prefer[1], "red dye"):
			ingredient, n, dye = ObjRedberries, 3, ObjRedDye
		case strings.Contains(st.Stop.Prefer[1], "yellow dye"):
			ingredient, n, dye = ObjOnion, 2, ObjYellowDye
		case strings.Contains(st.Stop.Prefer[1], "blue dye"):
			ingredient, n, dye = ObjWoadLeaf, 2, ObjBlueDye
		default:
			t.Fatalf("Aggie's dye is unreadable from %v", st.Stop.Prefer)
		}
		if st.UntilItemID != dye {
			t.Fatalf("Aggie is driven until item %d, want the dye %d", st.UntilItemID, dye)
		}
		if w.inv[ingredient] < n || w.inv[CoinsID] < GobdipDyeFee {
			t.Fatalf("talked to Aggie for obj %d with %d of obj %d and %d coins", dye, w.inv[ingredient], ingredient, w.inv[CoinsID])
		}
		w.inv[ingredient] -= n
		w.inv[CoinsID] -= GobdipDyeFee
		w.inv[dye]++
		w.log = append(w.log, "aggie")
	case NpcGeneralWartface:
		// goblin_diplomacy_greet_player_reply, by %goblinquest.
		switch w.stage {
		case GobdipStarted:
			w.stage = GobdipWillBringArmour
		case GobdipWillBringArmour:
			if w.take(ObjGoblinMailOrange, 1) {
				w.stage = GobdipGaveOrange
			}
		case GobdipGaveOrange:
			if w.take(ObjGoblinMailBlue, 1) {
				w.stage = GobdipGaveBlue
			}
		case GobdipGaveBlue:
			if w.take(ObjGoblinMail, 1) {
				w.stage = GobdipComplete // gave_brown, then the queue completes the quest at once
			}
		case GobdipGaveBrown:
			w.stage = GobdipComplete
		}
		w.log = append(w.log, fmt.Sprintf("generals%d", w.stage))
	default:
		t.Fatalf("talk to an unexpected npc %d", st.Stop.ID)
	}
}

func (w *gdWorld) useOn(st Step) {
	t := w.t
	switch {
	case st.ItemID == ObjRedDye && st.TargetID == ObjYellowDye:
		// craft_dyes: red and yellow make orange.
		if w.inv[ObjRedDye] < 1 || w.inv[ObjYellowDye] < 1 {
			t.Fatalf("mixing red %d and yellow %d", w.inv[ObjRedDye], w.inv[ObjYellowDye])
		}
		w.inv[ObjRedDye]--
		w.inv[ObjYellowDye]--
		w.inv[ObjOrangeDye]++
		w.log = append(w.log, "mix")
	case st.TargetID == ObjGoblinMail && (st.ItemID == ObjOrangeDye || st.ItemID == ObjBlueDye):
		// dye_goblin_mail_armour: a dye and a plain mail make the coloured one.
		if w.inv[st.ItemID] < 1 || w.inv[ObjGoblinMail] < 1 {
			t.Fatalf("dyeing mail with dye %d: dye %d, plain mail %d", st.ItemID, w.inv[st.ItemID], w.inv[ObjGoblinMail])
		}
		w.inv[st.ItemID]--
		w.inv[ObjGoblinMail]--
		if st.ItemID == ObjOrangeDye {
			w.inv[ObjGoblinMailOrange]++
			w.log = append(w.log, "orangemail")
		} else {
			w.inv[ObjGoblinMailBlue]++
			w.log = append(w.log, "bluemail")
		}
	default:
		t.Fatalf("unexpected use-on %s", st.Describe())
	}
}

// run plays DecideGoblinDiplomacy to the end (or until it waits, which the caller expects or not).
func (w *gdWorld) run() (waited string) {
	w.t.Helper()
	for i := 0; i < 80; i++ {
		st := DecideGoblinDiplomacy(w.snapshot())
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
	w.t.Fatalf("80 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return ""
}

func gdIDs(m map[int]int, ids ...int) map[int]int {
	out := map[int]int{}
	for k, v := range m {
		out[k] = v
	}
	for _, id := range ids {
		out[id]++
	}
	return out
}

// ---- the whole quest, from several starts ----

// An account that owns nothing but coins does the whole quest in the order the geography wants: the bartender; the
// pantry (the bank is empty, so it only reads it); the berries, the onions, Wyson's woad; one visit to Aggie for the three
// dyes and the mix; and only then the mail farm, which is in the village the generals stand in; the dyes on the mail; the
// four visits to the generals.
func TestAnEmptyAccountDoesTheWholeQuestInTheGeographicOrder(t *testing.T) {
	w := newGDWorld(t, GobdipNotStarted, map[int]int{CoinsID: 100}, nil)
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"bartender", "provision:gobdip-pantry", "provision:gobdip-berries", "onion", "onion", "wyson",
		"aggie", "aggie", "aggie", "mix", "provision:gobdip-mail", "orangemail", "bluemail",
		"generals2", "generals3", "generals4", "generals6"}
	if fmt.Sprint(w.log) != fmt.Sprint(want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.stage != GobdipComplete {
		t.Errorf("stage %d at the end", w.stage)
	}
	if w.farmed != 3 || w.berries != 3 || w.shops != 0 {
		t.Errorf("farmed %d mail (want 3), %d berries (want 3, from the ground), %d shop sources (want 0)", w.farmed, w.berries, w.shops)
	}
	// 20 for the woad and 5 for each of three dyes, and nothing else spent.
	if got := w.inv[CoinsID]; got != 100-GobdipWoadFee-3*GobdipDyeFee {
		t.Errorf("%d coins left of 100", got)
	}
	// Nothing the quest made is left over.
	for _, id := range []int{ObjGoblinMail, ObjGoblinMailOrange, ObjGoblinMailBlue, ObjRedDye, ObjYellowDye, ObjBlueDye, ObjOrangeDye,
		ObjWoadLeaf, ObjRedberries, ObjOnion} {
		if w.inv[id] != 0 {
			t.Errorf("%d of obj %d left in the pack", w.inv[id], id)
		}
	}
}

// What the bank or the mule holds is taken first, at every rung: dyes in the bank mean no Aggie, no Wyson, no berries and
// no onions, and plain mail there means no farm.
func TestABankThatHoldsTheDyesAndTheMailSkipsEverySource(t *testing.T) {
	w := newGDWorld(t, GobdipStarted, map[int]int{}, map[int]int{ObjOrangeDye: 1, ObjBlueDye: 1, ObjGoblinMail: 3, CoinsID: 5000})
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"provision:gobdip-pantry", "orangemail", "bluemail", "generals2", "generals3", "generals4", "generals6"}
	if fmt.Sprint(w.log) != fmt.Sprint(want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.farmed != 0 || w.berries != 0 {
		t.Errorf("sources used with the bank full: %d mail, %d berries", w.farmed, w.berries)
	}
	if w.inv[CoinsID] != GobdipCoinFloat {
		t.Errorf("the pantry drew %d coins, want its float of %d", w.inv[CoinsID], GobdipCoinFloat)
	}
}

// The trainer banks everything between two ladder passes. The coloured mail is untradeable and the content never
// re-issues what is lost, so a stage 3 account whose blue mail went to the bank takes it back out and goes on, without
// making a second blue dye.
func TestAnAccountThatBankedItsColouredMailTakesItBack(t *testing.T) {
	w := newGDWorld(t, GobdipGaveOrange, map[int]int{CoinsID: 40}, map[int]int{ObjGoblinMailBlue: 1, ObjGoblinMail: 1})
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"provision:gobdip-pantry", "generals4", "generals6"}
	if fmt.Sprint(w.log) != fmt.Sprint(want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
}

// Only the hand-overs still to come are provisioned: a stage 3 account makes no orange dye, a stage 4 account needs one
// plain mail and nothing else, and a stage 5 account goes straight to the generals.
func TestEachStageProvisionsOnlyWhatIsLeft(t *testing.T) {
	cases := []struct {
		name  string
		stage int
		inv   map[int]int
		want  []string
	}{
		{"stage 3, nothing held", GobdipGaveOrange, map[int]int{CoinsID: 60},
			[]string{"provision:gobdip-pantry", "wyson", "aggie", "provision:gobdip-mail", "bluemail", "generals4", "generals6"}},
		{"stage 4, nothing held", GobdipGaveBlue, map[int]int{CoinsID: 5},
			[]string{"provision:gobdip-pantry", "provision:gobdip-mail", "generals6"}},
		{"stage 5", GobdipGaveBrown, nil, []string{"generals6"}},
		{"stage 1 holding the dyes and mail", GobdipStarted,
			map[int]int{ObjOrangeDye: 1, ObjBlueDye: 1, ObjGoblinMail: 3},
			[]string{"provision:gobdip-pantry", "orangemail", "bluemail", "generals2", "generals3", "generals4", "generals6"}},
		{"stage 2 holding the orange mail, the blue dye and two plain", GobdipWillBringArmour,
			map[int]int{ObjGoblinMailOrange: 1, ObjBlueDye: 1, ObjGoblinMail: 2},
			[]string{"provision:gobdip-pantry", "bluemail", "generals3", "generals4", "generals6"}},
	}
	for _, c := range cases {
		w := newGDWorld(t, c.stage, c.inv, nil)
		if why := w.run(); why != "" {
			t.Errorf("%s: waited: %s (log %v)", c.name, why, w.log)
			continue
		}
		if fmt.Sprint(w.log) != fmt.Sprint(c.want) {
			t.Errorf("%s ran\n  %v\nwant\n  %v", c.name, w.log, c.want)
		}
	}
}

// An account too poor for Wyson's woad (20 coins) or Aggie's fee (5) waits and says so; it does not walk to a seller it
// cannot pay.
func TestAnAccountWithoutCoinsWaitsForTheFeeNotTheWalk(t *testing.T) {
	w := newGDWorld(t, GobdipGaveOrange, map[int]int{CoinsID: 12}, nil)
	why := w.run()
	if !strings.Contains(why, "Wyson") || !strings.Contains(why, "20 coins") {
		t.Fatalf("wait %q, want Wyson's 20 coins", why)
	}
	if strings.Join(w.log, ",") != "provision:gobdip-pantry" {
		t.Fatalf("log %v: only the pantry may have run before the wait", w.log)
	}
	w = newGDWorld(t, GobdipGaveOrange, map[int]int{CoinsID: 4, ObjWoadLeaf: 2}, nil)
	if why := w.run(); !strings.Contains(why, "Aggie") || !strings.Contains(why, "5 coins") {
		t.Fatalf("wait %q, want Aggie's 5 coins", why)
	}
}

// A kit the bank, the mule and every source have failed is a wait, not a loop, and it names what is missing.
func TestAFailedKitIsAWaitNotARetry(t *testing.T) {
	s := (&gdWorld{stage: GobdipStarted, inv: map[int]int{CoinsID: 60}, ran: map[string]bool{"provision:" + KitGobdipPantry: true},
		failed: map[string]bool{"provision:" + KitGobdipBerries: true}, tile: DraynorBank}).snapshot()
	if st := DecideGoblinDiplomacy(s); st.Kind != StepWait || !strings.Contains(st.Reason, "redberries") {
		t.Errorf("berries failed: %s", st.Describe())
	}
	s = (&gdWorld{stage: GobdipGaveBlue, inv: map[int]int{CoinsID: 60}, ran: map[string]bool{"provision:" + KitGobdipPantry: true},
		failed: map[string]bool{"provision:" + KitGobdipMail: true}, tile: DraynorBank}).snapshot()
	if st := DecideGoblinDiplomacy(s); st.Kind != StepWait || !strings.Contains(st.Reason, "goblin mail") {
		t.Errorf("mail failed: %s", st.Describe())
	}
	// A pantry that failed on the way (the bank would not open) does not stop the quest: it goes on with what it holds.
	s = (&gdWorld{stage: GobdipGaveBlue, inv: map[int]int{CoinsID: 60}, ran: map[string]bool{},
		failed: map[string]bool{"provision:" + KitGobdipPantry: true}, tile: DraynorBank}).snapshot()
	if st := DecideGoblinDiplomacy(s); st.Kind != StepProvision || st.KitName != KitGobdipMail {
		t.Errorf("pantry failed: %s", st.Describe())
	}
}

// ---- the decide table ----

func TestDecideGoblinDiplomacyTheNonItemStates(t *testing.T) {
	check := func(name string, s Snapshot, kind StepKind, detail func(Step) bool) {
		t.Helper()
		st := DecideGoblinDiplomacy(s)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", name, why)
		}
		if st.Kind != kind || (detail != nil && !detail(st)) {
			t.Errorf("%s: got %s", name, st.Describe())
		}
	}
	base := func(stage int) Snapshot {
		return (&gdWorld{stage: stage, inv: map[int]int{CoinsID: 60}, ran: map[string]bool{"provision:" + KitGobdipPantry: true},
			failed: map[string]bool{}, tile: DraynorBank}).snapshot()
	}
	check("complete", base(GobdipComplete), StepDone, nil)
	check("not started", base(GobdipNotStarted), StepTalk, func(s Step) bool { return s.Stop.ID == NpcRustyAnchorBartender })
	unknown := base(GobdipStarted)
	unknown.Journal, unknown.StageKnown = StatusUnknown, false
	check("tab not coloured", unknown, StepWait, nil)
	unread := base(GobdipStarted)
	unread.StageKnown = false
	check("stage unread", unread, StepWait, nil)
	check("stage 5 is one more greeting", base(GobdipGaveBrown), StepTalk, func(s Step) bool { return s.Stop.ID == NpcGeneralWartface })

	// The pantry is the first thing a started quest does, and is the same kit for what is left.
	fresh := base(GobdipStarted)
	fresh.Ran = map[string]bool{}
	check("pantry first", fresh, StepProvision, func(s Step) bool {
		return s.KitName == KitGobdipPantry && s.BankSet && containsInt(s.KeepIDs, ObjGoblinMailOrange) && containsInt(s.KeepIDs, 385)
	})

	// Berries, then onions, then woad, then Aggie: each only once the one before it is in hand.
	s := base(GobdipStarted)
	check("berries", s, StepProvision, func(st Step) bool { return st.KitName == KitGobdipBerries })
	s.InvIDs[ObjRedberries] = 3
	check("onions", s, StepPickLoc, func(st Step) bool { return st.LocID == LocOnionPlant })
	s.InvIDs[ObjOnion] = 2
	check("woad", s, StepTalk, func(st Step) bool { return st.Stop.ID == NpcWyson && st.UntilItemID == ObjWoadLeaf })
	s.InvIDs[ObjWoadLeaf] = 2
	check("red dye", s, StepTalk, func(st Step) bool { return st.Stop.ID == NpcAggie && st.UntilItemID == ObjRedDye })
	delete(s.InvIDs, ObjRedberries)
	s.InvIDs[ObjRedDye] = 1
	check("yellow dye", s, StepTalk, func(st Step) bool { return st.Stop.ID == NpcAggie && st.UntilItemID == ObjYellowDye })
	delete(s.InvIDs, ObjOnion)
	s.InvIDs[ObjYellowDye] = 1
	check("blue dye", s, StepTalk, func(st Step) bool { return st.Stop.ID == NpcAggie && st.UntilItemID == ObjBlueDye })
	delete(s.InvIDs, ObjWoadLeaf)
	s.InvIDs[ObjBlueDye] = 1
	check("mix", s, StepUseOn, func(st Step) bool {
		return st.ItemID == ObjRedDye && st.TargetKind == TargetItem && st.TargetID == ObjYellowDye
	})
	delete(s.InvIDs, ObjRedDye)
	delete(s.InvIDs, ObjYellowDye)
	s.InvIDs[ObjOrangeDye] = 1
	check("mail", s, StepProvision, func(st Step) bool {
		return st.KitName == KitGobdipMail && len(st.Kit) == 1 && st.Kit[0].Qty == 3 && st.Kit[0].Sources[0].Method == MethodGoblinMail
	})
	s.InvIDs[ObjGoblinMail] = 3
	check("orange on a mail", s, StepUseOn, func(st Step) bool { return st.ItemID == ObjOrangeDye && st.TargetID == ObjGoblinMail })
	delete(s.InvIDs, ObjOrangeDye)
	s.InvIDs[ObjGoblinMail] = 2
	s.InvIDs[ObjGoblinMailOrange] = 1
	check("blue on a mail", s, StepUseOn, func(st Step) bool { return st.ItemID == ObjBlueDye && st.TargetID == ObjGoblinMail })
	delete(s.InvIDs, ObjBlueDye)
	s.InvIDs[ObjGoblinMail] = 1
	s.InvIDs[ObjGoblinMailBlue] = 1
	check("generals", s, StepTalk, func(st Step) bool { return st.Stop.ID == NpcGeneralWartface })

	// A full pack banks first, keeping the quest's items and nothing else: bare, so the run staller's kit goes too, and with no
	// food in the keep list (live 17 Oct 2026: a keep list of every food id banked nothing from a pack of food, kit and quest
	// items, and the engine parked the step after eight ticks).
	full := base(GobdipStarted)
	full.FreeSlots = 1
	check("full pack", full, StepDeposit, func(st Step) bool {
		if !st.Bare || !containsInt(st.KeepIDs, CoinsID) || !containsInt(st.KeepIDs, ObjGoblinMail) || !containsInt(st.KeepIDs, ObjOrangeDye) {
			return false
		}
		for _, f := range GoblinDiplomacy.Sustain.FoodIDs {
			if containsInt(st.KeepIDs, f) {
				return false
			}
		}
		return true
	})
	// ... before the pantry, not after it: the pantry draws up to ten lines and cannot into a pack with no room.
	freshFull := full
	freshFull.Ran = map[string]bool{}
	check("full pack before the pantry", freshFull, StepDeposit, func(st Step) bool { return st.Bare })
	// Three free slots is room (the mail kit is three).
	roomy := base(GobdipStarted)
	roomy.FreeSlots = 3
	check("three free slots", roomy, StepProvision, nil)
}

func containsInt(ids []int, id int) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// For every combination of what the pack can hold at every stage the module returns a well-formed step and never one
// that wastes the plain mail the brown hand-over needs, nor sends the account to the generals empty-handed.
func TestDecideGoblinDiplomacyNeverDyesTheBrownMailOrVisitsTheGeneralsEmptyHanded(t *testing.T) {
	items := []int{ObjGoblinMail, ObjGoblinMailOrange, ObjGoblinMailBlue, ObjOrangeDye, ObjBlueDye, ObjRedDye, ObjYellowDye,
		ObjRedberries, ObjOnion, ObjWoadLeaf, CoinsID}
	for stage := GobdipStarted; stage <= GobdipGaveBrown; stage++ {
		for mask := 0; mask < 1<<len(items); mask++ {
			for plain := 1; plain <= 3; plain++ {
				if mask&1 == 0 && plain > 1 {
					continue
				}
				inv := map[int]int{}
				for i, id := range items {
					if mask&(1<<i) != 0 {
						inv[id] = 1
					}
				}
				if inv[ObjGoblinMail] > 0 {
					inv[ObjGoblinMail] = plain
				}
				if inv[ObjRedberries] > 0 {
					inv[ObjRedberries] = 3
				}
				if inv[ObjOnion] > 0 {
					inv[ObjOnion] = 2
				}
				if inv[ObjWoadLeaf] > 0 {
					inv[ObjWoadLeaf] = 2
				}
				if inv[CoinsID] > 0 {
					inv[CoinsID] = 60
				}
				w := &gdWorld{stage: stage, inv: inv, ran: map[string]bool{"provision:" + KitGobdipPantry: true}, failed: map[string]bool{}, tile: DraynorBank}
				s := w.snapshot()
				st := DecideGoblinDiplomacy(s)
				if why := ValidateStep(st); why != "" {
					t.Fatalf("stage %d inv %v: malformed %s: %s", stage, inv, st.Describe(), why)
				}
				need := gobdipNeeds(stage)
				switch st.Kind {
				case StepTalk:
					if st.Stop.ID != NpcGeneralWartface {
						continue
					}
					want := map[int]int{GobdipWillBringArmour: ObjGoblinMailOrange, GobdipGaveOrange: ObjGoblinMailBlue, GobdipGaveBlue: ObjGoblinMail}[stage]
					if want != 0 && inv[want] == 0 {
						t.Fatalf("stage %d inv %v: to the generals without obj %d", stage, inv, want)
					}
					// And with everything for the later hand-overs too: one visit does one hand-over, but the account is
					// not sent back out for the next.
					if (need.Orange && inv[ObjGoblinMailOrange] == 0) || (need.Blue && inv[ObjGoblinMailBlue] == 0) || (need.Brown && inv[ObjGoblinMail] == 0) {
						t.Fatalf("stage %d inv %v: to the generals short of mail for a later hand-over (%+v)", stage, inv, need)
					}
				case StepUseOn:
					if st.TargetID != ObjGoblinMail {
						continue
					}
					pendingOrange := need.Orange && inv[ObjGoblinMailOrange] == 0
					pendingBlue := need.Blue && inv[ObjGoblinMailBlue] == 0
					others := 0
					if st.ItemID == ObjOrangeDye {
						others = b2i(pendingBlue)
					} else {
						others = b2i(pendingOrange)
					}
					if inv[st.ItemID] == 0 || inv[ObjGoblinMail] < 1+others+b2i(need.Brown) {
						t.Fatalf("stage %d inv %v: dye %d on the mail leaves too little for the rest", stage, inv, st.ItemID)
					}
				}
			}
		}
	}
}

// ---- the kits through the real planner ----

func TestThePantryReadsTheBankAndNeverWalksToASource(t *testing.T) {
	kit := gobdipPantryKit(gobdipNeeds(GobdipStarted))
	for _, l := range kit {
		if !l.Optional || len(l.Sources) != 0 {
			t.Errorf("pantry line %s (obj %d) must be optional and sourceless, is optional %v with %d sources", l.Name, l.ID, l.Optional, len(l.Sources))
		}
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("%s", why)
		}
	}
	// Empty bank: read it, then nothing to do - never a task.
	read := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, FreeSlots: 28})
	if read.Phase != PhaseBank || len(read.Withdraw) != 0 {
		t.Fatalf("an unread bank: %s", read.Summary())
	}
	empty := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 28})
	if empty.Phase != PhaseSatisfied || len(empty.Tasks) != 0 {
		t.Fatalf("an empty bank leaves the pantry %s", empty.Summary())
	}
	// A full bank: everything the bank holds is drawn, the coins up to the float.
	full := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, FreeSlots: 28, BankKnown: true, Bank: map[int]int{
		ObjOrangeDye: 1, ObjBlueDye: 2, ObjGoblinMail: 9, ObjGoblinMailOrange: 1, ObjRedberries: 20, CoinsID: 100000}})
	drew := map[int]int{}
	for _, c := range full.Withdraw {
		drew[c.ID] = c.Count
	}
	want := map[int]int{ObjOrangeDye: 1, ObjBlueDye: 1, ObjGoblinMail: 3, ObjGoblinMailOrange: 1, ObjRedberries: 3, CoinsID: GobdipCoinFloat}
	if full.Phase != PhaseBank || fmt.Sprint(drew) != fmt.Sprint(want) {
		t.Fatalf("a full bank: drew %v, want %v (%s)", drew, want, full.Summary())
	}
	// The mule is asked for the tradeable lines only: never the coloured mail, which is an untradeable quest item.
	mule := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 28, MuleKnown: true,
		Mule: map[int]int{ObjGoblinMailOrange: 5, ObjGoblinMailBlue: 5, ObjOrangeDye: 5, ObjGoblinMail: 5}})
	asked := map[int]int{}
	for _, c := range mule.MuleAsk {
		asked[c.ID] = c.Count
	}
	if mule.Phase != PhaseMule || asked[ObjGoblinMailOrange] != 0 || asked[ObjGoblinMailBlue] != 0 || asked[ObjOrangeDye] != 1 || asked[ObjGoblinMail] != 3 {
		t.Fatalf("the mule ask %v (%s)", asked, mule.Summary())
	}
	// At the last stage the kit is one plain mail and the coins.
	last := gobdipPantryKit(gobdipNeeds(GobdipGaveBlue))
	if len(last) != 2 || last[0].ID != ObjGoblinMail || last[0].Qty != 1 {
		t.Errorf("stage 4 pantry %+v", last)
	}
}

// The redberries come from the ground patch before Wydin's shelf (the item rule's order), through each of the three
// spawns, and the shelf is only the last resort.
func TestTheBerriesComeFromThePatchBeforeTheShelf(t *testing.T) {
	kit := gobdipBerriesKit()
	for _, l := range kit {
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	st := ProvisionState{Pack: map[int]int{CoinsID: 60}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, Exhausted: map[string]bool{}}
	var order []string
	for i := 0; i < 6; i++ {
		p := PlanProvision(kit, st)
		if p.Phase != PhaseSource {
			t.Fatalf("round %d: %s", i, p.Summary())
		}
		task := p.Tasks[0]
		order = append(order, fmt.Sprintf("%s %d,%d", task.Source.Kind, task.Source.Tile.X+task.Source.Shop.Anchor.X, task.Source.Tile.Z+task.Source.Shop.Anchor.Z))
		st.Exhausted[SourceKey(task.Line, task.Source)] = true
		if task.Source.Kind == SourceShop {
			break
		}
	}
	want := []string{"ground 3267,3371", "ground 3271,3366", "ground 3278,3374", "shop 3014,3204"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("source order %v, want %v", order, want)
	}
	// Every shop task needs its coins budgeted; with none the shop is not planned.
	broke := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20,
		Exhausted: map[string]bool{SourceKey(kit[0], kit[0].Sources[0]): true, SourceKey(kit[0], kit[0].Sources[1]): true, SourceKey(kit[0], kit[0].Sources[2]): true}})
	if broke.Phase != PhaseBlocked {
		t.Errorf("no ground and no coins: %s", broke.Summary())
	}
}

// The mail: the bank, then the mule, then the farm, which is a skill source with no level gate.
func TestTheMailComesFromTheBankThenTheMuleThenTheFarm(t *testing.T) {
	kit := gobdipMailKit(3)
	if why := ValidateKitLine(kit[0]); why != "" {
		t.Fatal(why)
	}
	base := ProvisionState{Pack: map[int]int{}, FreeSlots: 20, BankKnown: true, Bank: map[int]int{}}
	if p := PlanProvision(kit, base); p.Phase != PhaseSource || p.Tasks[0].Source.Method != MethodGoblinMail || p.Tasks[0].Qty != 3 {
		t.Errorf("empty bank, no mule: %s", p.Summary())
	}
	base.Bank = map[int]int{ObjGoblinMail: 2}
	if p := PlanProvision(kit, base); p.Phase != PhaseBank || p.Withdraw[0].Count != 2 {
		t.Errorf("two in the bank: %s", p.Summary())
	}
	base.Bank = map[int]int{}
	base.MuleKnown, base.Mule = true, map[int]int{ObjGoblinMail: 50}
	if p := PlanProvision(kit, base); p.Phase != PhaseMule || p.MuleAsk[0].Count != 3 {
		t.Errorf("the mule holds fifty: %s", p.Summary())
	}
	// With one already in hand the farm is asked for two.
	base.MuleKnown, base.Pack = false, map[int]int{ObjGoblinMail: 1}
	if p := PlanProvision(kit, base); p.Phase != PhaseSource || p.Tasks[0].Qty != 2 {
		t.Errorf("one held: %s", p.Summary())
	}
}

// ---- the module ----

// Every item the quest makes or carries is a tool, so no engine deposit banks it mid-quest (the Murder Mystery lesson: the
// trainer banks the rest between passes, and the pantry draws it back).
func TestEveryItemOfTheQuestIsAToolAndTheKeepListAddsFood(t *testing.T) {
	tools := map[int]bool{}
	for _, id := range GoblinDiplomacy.ToolIDs {
		tools[id] = true
	}
	for _, id := range []int{CoinsID, ObjGoblinMail, ObjGoblinMailOrange, ObjGoblinMailBlue, ObjRedDye, ObjYellowDye, ObjBlueDye, ObjOrangeDye,
		ObjWoadLeaf, ObjRedberries, ObjOnion} {
		if !tools[id] {
			t.Errorf("obj %d is not in Module.ToolIDs", id)
		}
	}
	keep := gobdipKeep()
	for id := range tools {
		if !containsInt(keep, id) {
			t.Errorf("the keep list lacks tool %d", id)
		}
	}
	for _, f := range GoblinDiplomacy.Sustain.FoodIDs {
		if !containsInt(keep, f) {
			t.Errorf("the keep list lacks food %d", f)
		}
	}
	// Every kit line any step can ask for is one of the tools: a line the module does not keep would be banked the next time
	// the pack is cleared.
	for _, kit := range [][]KitLine{gobdipPantryKit(gobdipNeeds(GobdipStarted)), gobdipBerriesKit(), gobdipMailKit(3)} {
		for _, l := range kit {
			if !tools[l.ID] {
				t.Errorf("kit line %s (obj %d) is not a tool", l.Name, l.ID)
			}
		}
	}
	if GoblinDiplomacy.MaxDeaths != 2 || GoblinDiplomacy.ReadStage == nil || GoblinDiplomacy.Decide == nil || !GoblinDiplomacy.BankSet {
		t.Errorf("module %+v", GoblinDiplomacy)
	}
	if GoblinDiplomacyRecord.QuestPoints != LadderFacts[GoblinDiplomacyID].QP || GoblinDiplomacyRecord.Name != LadderFacts[GoblinDiplomacyID].Name {
		t.Errorf("the record %+v disagrees with the ladder facts %+v", GoblinDiplomacyRecord, LadderFacts[GoblinDiplomacyID])
	}
}

// The farm stays out of the Wilderness: the last village goblin spawn is nine tiles short of the line, and the farm's
// northern edge sits between them.
func TestTheMailFarmStaysSouthOfTheWilderness(t *testing.T) {
	// Content/maps m46_61, the Goblin Village's red and green armour goblins (npc 298 and 299).
	spawns := []tile.Tile{{X: 2951, Z: 3481}, {X: 2951, Z: 3483}, {X: 2951, Z: 3501}, {X: 2952, Z: 3507}, {X: 2953, Z: 3487}, {X: 2954, Z: 3500},
		{X: 2955, Z: 3507}, {X: 2957, Z: 3492}, {X: 2959, Z: 3499}, {X: 2962, Z: 3503}, {X: 2964, Z: 3511},
		{X: 2951, Z: 3497}, {X: 2952, Z: 3510}, {X: 2954, Z: 3491}, {X: 2954, Z: 3503}, {X: 2956, Z: 3500}, {X: 2957, Z: 3482},
		{X: 2957, Z: 3484}, {X: 2957, Z: 3508}, {X: 2958, Z: 3507}, {X: 2963, Z: 3514}}
	const wildernessZ = 3520
	if len(spawns) != 21 {
		t.Fatalf("%d spawns, the village has 21", len(spawns))
	}
	for _, sp := range spawns {
		if sp.Z > GoblinVillageNorth {
			t.Errorf("goblin spawn %v lies north of the farm's edge %d: its goblin could never be fought", sp, GoblinVillageNorth)
		}
		if tile.Chebyshev(sp, GoblinVillageField) > 24 {
			t.Errorf("goblin spawn %v is out of the farm's search range from %v", sp, GoblinVillageField)
		}
	}
	if GoblinVillageNorth >= wildernessZ || GoblinGenerals.Z >= GoblinVillageNorth {
		t.Errorf("the farm edge %d must lie south of the Wilderness line %d and north of the generals %v", GoblinVillageNorth, wildernessZ, GoblinGenerals)
	}
}

// ---- the slot: past the crowded steps ----

// The fleet-wide slot is for the berry patch and the village's goblins. An account is past them when it needs no berries and
// holds all the plain mail it needs; the onions, the woad, Aggie and the dyes on the mail are nobody else's wait.
func TestGobdipPastCrowd(t *testing.T) {
	cases := []struct {
		name  string
		stage int
		inv   map[int]int
		want  bool
	}{
		{"quest done", GobdipComplete, nil, true},
		{"not started", GobdipNotStarted, map[int]int{ObjGoblinMail: 3}, false},
		{"started, nothing in hand", GobdipStarted, map[int]int{CoinsID: 60}, false},
		{"started, berries but no mail", GobdipStarted, map[int]int{ObjRedberries: 3}, false},
		{"started, mail but no berries", GobdipStarted, map[int]int{ObjGoblinMail: 3}, false},
		{"started, mail and berries", GobdipStarted, map[int]int{ObjGoblinMail: 3, ObjRedberries: 3}, true},
		{"started, two berries are not three", GobdipStarted, map[int]int{ObjGoblinMail: 3, ObjRedberries: 2}, false},
		{"started, mail and the red dye (no berries needed)", GobdipStarted, map[int]int{ObjGoblinMail: 3, ObjRedDye: 1}, true},
		{"started, mail and the orange dye", GobdipStarted, map[int]int{ObjGoblinMail: 3, ObjOrangeDye: 1}, true},
		{"started, two mail of the three", GobdipStarted, map[int]int{ObjGoblinMail: 2, ObjOrangeDye: 1}, false},
		{"started, the orange mail in hand needs two plain", GobdipStarted, map[int]int{ObjGoblinMailOrange: 1, ObjGoblinMail: 2}, true},
		{"started, the orange mail in hand and one plain", GobdipStarted, map[int]int{ObjGoblinMailOrange: 1, ObjGoblinMail: 1}, false},
		{"orange wanted, no berries, no mail", GobdipWillBringArmour, nil, false},
		{"orange given: two plain needed", GobdipGaveOrange, map[int]int{ObjGoblinMail: 2}, true},
		{"orange given: one plain is short", GobdipGaveOrange, map[int]int{ObjGoblinMail: 1}, false},
		{"orange given, the blue mail in hand: one plain", GobdipGaveOrange, map[int]int{ObjGoblinMailBlue: 1, ObjGoblinMail: 1}, true},
		{"blue given: one plain", GobdipGaveBlue, map[int]int{ObjGoblinMail: 1}, true},
		{"blue given: none", GobdipGaveBlue, nil, false},
		{"brown given", GobdipGaveBrown, nil, true},
	}
	for _, c := range cases {
		s := newGDWorld(t, c.stage, c.inv, nil).snapshot()
		if got := GobdipPastCrowd(s); got != c.want {
			t.Errorf("%s: past the crowd = %v, want %v", c.name, got, c.want)
		}
	}
	// A reading it cannot make is not "past".
	unread := newGDWorld(t, GobdipStarted, map[int]int{ObjGoblinMail: 3, ObjRedberries: 3}, nil).snapshot()
	unread.StageKnown = false
	if GobdipPastCrowd(unread) {
		t.Error("an unread journal counted as past the crowd")
	}
	unread.StageKnown, unread.Journal = true, StatusUnknown
	if GobdipPastCrowd(unread) {
		t.Error("an uncoloured tab counted as past the crowd")
	}
	if GoblinDiplomacy.ExclusiveDone == nil {
		t.Error("the module gives its slot up nowhere before the end of its run")
	}
}

// Whatever the pack holds, the module is never past the crowd while it still decides to provision berries or to farm mail:
// the slot is held through exactly the steps that need it.
func TestGobdipIsNeverPastTheCrowdWhileItNeedsTheCrowd(t *testing.T) {
	for stage := GobdipStarted; stage <= GobdipGaveBlue; stage++ {
		for _, mail := range []int{0, 1, 2, 3} {
			for _, berries := range []int{0, 3} {
				for _, dye := range []int{0, ObjRedDye, ObjOrangeDye} {
					inv := map[int]int{CoinsID: 60, ObjGoblinMail: mail, ObjRedberries: berries}
					if dye != 0 {
						inv[dye] = 1
					}
					if mail == 0 {
						delete(inv, ObjGoblinMail)
					}
					if berries == 0 {
						delete(inv, ObjRedberries)
					}
					w := newGDWorld(t, stage, inv, nil)
					w.ran = map[string]bool{"provision:" + KitGobdipPantry: true}
					s := w.snapshot()
					step := DecideGoblinDiplomacy(s)
					crowded := step.Kind == StepProvision && (step.KitName == KitGobdipBerries || step.KitName == KitGobdipMail)
					if crowded && GobdipPastCrowd(s) {
						t.Errorf("stage %d with %v: the module is about to %s but reads past the crowd", stage, inv, step.Describe())
					}
				}
			}
		}
	}
}
