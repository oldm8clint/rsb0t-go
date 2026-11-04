package questplan

import (
	"slices"
	"strconv"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// sheepSnap is an account on its way through Sheep Shearer: the quest tab, the pack by object id (a repeated id is a
// repeated item), a bank that holds nothing, and a position beside Fred.
func sheepSnap(journal Status, held ...int) Snapshot {
	s := Snapshot{Journal: journal, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}, Tile: tile.Tile{X: 3190, Z: 3275}, TileKnown: true,
		Failed: map[string]bool{}, Ran: map[string]bool{}}
	for _, id := range held {
		s.InvIDs[id]++
	}
	return s
}

func sheepWithStage(s Snapshot, stage int) Snapshot { s.Stage, s.StageKnown = stage, true; return s }
func sheepWithKit(s Snapshot) Snapshot              { s.Ran["provision:"+KitSheepBalls] = true; return s }
func sheepRepeat(id, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = id
	}
	return out
}

func TestSheepShearerEveryStage(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		kind StepKind
		want string // npc, custom name or kit name, "" for any
	}{
		{"complete", sheepSnap(StatusComplete), StepDone, ""},
		{"the tab is not coloured yet", sheepSnap(StatusUnknown), StepWait, ""},
		{"not started, empty pack: the cheap rungs first, even before the start", sheepSnap(StatusNotStarted), StepProvision, KitSheepBalls},
		{"not started, the pass ran: Fred", sheepWithKit(sheepSnap(StatusNotStarted)), StepTalk, "Fred the Farmer"},
		{"not started, the pass failed on the way: Fred, not asked again", func() Snapshot {
			s := sheepSnap(StatusNotStarted)
			s.Failed["provision:"+KitSheepBalls] = true
			return s
		}(), StepTalk, "Fred the Farmer"},
		{"not started with the 20 in the pack: Fred, no pass", sheepSnap(StatusNotStarted, sheepRepeat(ObjBallOfWool, 20)...), StepTalk, "Fred the Farmer"},
		{"started, 20 owed, 20 held: the hand-in", sheepWithStage(sheepSnap(StatusInProgress, sheepRepeat(ObjBallOfWool, 20)...), 1), StepCustom, CustomSheepHandIn},
		{"started, 7 owed, 7 held: the hand-in", sheepWithStage(sheepSnap(StatusInProgress, sheepRepeat(ObjBallOfWool, 7)...), 14), StepCustom, CustomSheepHandIn},
		{"started, 1 owed, 1 held: the last ball", sheepWithStage(sheepSnap(StatusInProgress, ObjBallOfWool), 20), StepCustom, CustomSheepHandIn},
		{"started, 7 owed, 6 held, no shears: the shears", sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress, sheepRepeat(ObjBallOfWool, 6)...), 14)), StepProvision, KitSheepShears},
		{"started, the pass not yet run: the pass", sheepWithStage(sheepSnap(StatusInProgress), 1), StepProvision, KitSheepBalls},
		{"started, shears held: shear and spin", sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress, ObjShears), 1)), StepCustom, CustomSheepWool},
		{"started, 20 wool held, no shears: only the wheel is needed", sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress, sheepRepeat(ObjWool, 20)...), 1)), StepCustom, CustomSheepWool},
		{"stage unread: all 20 owed", sheepWithKit(sheepSnap(StatusInProgress, ObjShears)), StepCustom, CustomSheepWool},
		{"stage unread, 20 held: the hand-in", sheepWithKit(sheepSnap(StatusInProgress, sheepRepeat(ObjBallOfWool, 20)...)), StepCustom, CustomSheepHandIn},
		{"a pack with no room for the wool banks first", func() Snapshot {
			s := sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress, ObjShears), 1))
			s.FreeSlots = 5
			return s
		}(), StepDeposit, ""},
	}
	for _, c := range cases {
		step := DecideSheepShearer(c.snap)
		if why := ValidateStep(step); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
		if step.Kind != c.kind {
			t.Errorf("%s: got %s (%s), want %s", c.name, step.Kind, step.Describe(), c.kind)
			continue
		}
		var got string
		switch step.Kind {
		case StepTalk:
			got = step.Stop.Npc
		case StepCustom:
			got = step.Custom
		case StepProvision:
			got = step.KitName
		}
		if c.want != "" && got != c.want {
			t.Errorf("%s: got %q, want %q (%s)", c.name, got, c.want, step.Describe())
		}
	}
}

// A shears kit that failed leaves the quest waiting rather than walking to a pen it cannot shear.
func TestSheepShearerWaitsWhenTheShearsKitFailed(t *testing.T) {
	s := sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress), 1))
	s.Failed["provision:"+KitSheepShears] = true
	if step := DecideSheepShearer(s); step.Kind != StepWait || ValidateStep(step) != "" {
		t.Errorf("got %s (%s), want a wait", step.Kind, step.Describe())
	}
}

// The wool the pack already holds is spun, not bought again: the pass asks for the balls the wool will not cover, the
// shearing for the wool still lacking, and the custom step for every ball still to come.
func TestSheepShearerCountsWoolAlreadyHeld(t *testing.T) {
	s := sheepWithStage(sheepSnap(StatusInProgress, append(sheepRepeat(ObjWool, 5), sheepRepeat(ObjBallOfWool, 3)...)...), 1)
	step := DecideSheepShearer(s)
	if step.Kind != StepProvision || step.KitName != KitSheepBalls || len(step.Kit) != 1 || step.Kit[0].Qty != 15 {
		t.Fatalf("pass %s %+v, want one line of 15 balls (20 owed less the 5 wool in the pack)", step.Describe(), step.Kit)
	}
	// The line's Qty is a total against the balls held: PlanProvision then asks for 15 - 3 = 12.
	p := PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{ObjBallOfWool: 3}, Worn: map[int]int{}, FreeSlots: 10,
		Bank: map[int]int{ObjBallOfWool: 50}, BankKnown: true})
	if len(p.Withdraw) != 1 || p.Withdraw[0].Count != 12 {
		t.Errorf("plan %s, want 12 from the bank", p.Summary())
	}
	step = DecideSheepShearer(sheepWithKit(s))
	if step.Kind != StepProvision || step.KitName != KitSheepShears {
		t.Fatalf("%s, want the shears next", step.Describe())
	}
	s.InvIDs[ObjShears] = 1
	step = DecideSheepShearer(sheepWithKit(s))
	if step.Kind != StepCustom || step.Custom != CustomSheepWool || step.Qty != 17 {
		t.Fatalf("%s qty %d, want the shearing for the 17 balls still to come (20 owed less 3 held)", step.Describe(), step.Qty)
	}
}

// Fred takes a partial delivery: a journal that says 7 are owed makes 7 the target, so a run that ended part way does not
// gather 20 again.
func TestSheepShearerGathersOnlyWhatFredStillOwes(t *testing.T) {
	s := sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress, ObjShears), 14)) // 7 owed
	step := DecideSheepShearer(s)
	if step.Kind != StepCustom || step.Custom != CustomSheepWool || step.Qty != 7 {
		t.Fatalf("%s qty %d, want the shearing for 7", step.Describe(), step.Qty)
	}
	// Full pack minus shears is room enough for 7 wool, not for 8.
	s.FreeSlots = 7
	if st := DecideSheepShearer(s); st.Kind != StepCustom {
		t.Errorf("7 free slots for 7 wool: %s", st.Describe())
	}
	s.FreeSlots = 6
	if st := DecideSheepShearer(s); st.Kind != StepDeposit {
		t.Errorf("6 free slots for 7 wool: %s", st.Describe())
	}
}

// The bank and mule rungs are implied; Aemad's shelf is listed for an account that stands nearer to it than to Fred, at the
// bank it stands by, and never for one in Lumbridge.
func TestSheepShearerBallsKitUsesAemadOnlyWhereItIsOnTheWay(t *testing.T) {
	inArdougne := sheepSnap(StatusNotStarted)
	inArdougne.Tile = tile.Tile{X: 2640, Z: 3290}
	step := DecideSheepShearer(inArdougne)
	if step.Kind != StepProvision || step.KitName != KitSheepBalls || len(step.Kit) != 1 {
		t.Fatalf("Ardougne: %s", step.Describe())
	}
	line := step.Kit[0]
	if why := ValidateKitLine(line); why != "" {
		t.Error(why)
	}
	if line.ID != ObjBallOfWool || line.Qty != 20 || !line.Optional || len(line.Sources) != 1 ||
		line.Sources[0].Kind != SourceShop || line.Sources[0].Shop != AemadStore || line.Sources[0].Price != 2 {
		t.Errorf("Ardougne kit line %+v, want 20 optional balls from Aemad at 2 gp", line)
	}
	if want := NearestBank(inArdougne.Tile).Tile; step.Bank != want || !step.BankSet {
		t.Errorf("Ardougne bank %v (set %v), want %v", step.Bank, step.BankSet, want)
	}
	// The same account in Lumbridge buys nothing from the other side of the map, and banks at Draynor (Al Kharid's is
	// nearer, behind a toll gate).
	atFred := sheepSnap(StatusNotStarted)
	step = DecideSheepShearer(atFred)
	if len(step.Kit) != 1 || len(step.Kit[0].Sources) != 0 {
		t.Errorf("Lumbridge kit %+v, want no shop", step.Kit)
	}
	if step.Bank != DraynorBank {
		t.Errorf("Lumbridge bank %v, want Draynor's", step.Bank)
	}
	atFred.Tile = tile.Tile{X: 3269, Z: 3170} // beside Al Kharid's bank: still not its booth
	if step = DecideSheepShearer(atFred); step.Bank != DraynorBank {
		t.Errorf("Al Kharid bank %v, want Draynor's (the toll gate)", step.Bank)
	}
	// Before a position packet has arrived the shop is not listed and the bank is Draynor's.
	blind := sheepSnap(StatusNotStarted)
	blind.TileKnown = false
	if step = DecideSheepShearer(blind); step.Bank != DraynorBank || len(step.Kit[0].Sources) != 0 {
		t.Errorf("no position: bank %v kit %+v", step.Bank, step.Kit)
	}
	// With a bank that holds 20 balls the pass is one withdrawal, then nothing else.
	p := PlanProvision(step.Kit, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, FreeSlots: 28,
		Bank: map[int]int{ObjBallOfWool: 20}, BankKnown: true})
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].Count != 20 {
		t.Errorf("plan %s, want 20 from the bank", p.Summary())
	}
}

func TestSheepShearerShearsKit(t *testing.T) {
	kit := sheepShearsKit()
	if len(kit) != 1 || kit[0].ID != ObjShears || kit[0].Qty != 1 || kit[0].Optional {
		t.Fatalf("kit %+v", kit)
	}
	if why := ValidateKitLine(kit[0]); why != "" {
		t.Error(why)
	}
	src := kit[0].Sources
	if len(src) != 2 || src[0].Kind != SourceGround || src[0].Tile != ShearsFarm || src[1].Kind != SourceShop || src[1].Shop != LumbridgeStore {
		t.Errorf("sources %+v, want Fred's farm floor then the Lumbridge store", src)
	}
	p := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, FreeSlots: 20, Bank: map[int]int{}, BankKnown: true})
	if p.Phase != PhaseSource || len(p.Tasks) != 1 || p.Tasks[0].Source.Kind != SourceGround {
		t.Errorf("plan %s, want the free pair first", p.Summary())
	}
}

// The deposit-first of every provisioning round and deposit step leaves the shears, the wool, the balls and the coins in the
// pack: a bank visit that banked the balls would send the quest back to the pen for them.
func TestSheepShearerKeepListsHoldEveryItem(t *testing.T) {
	want := []int{ObjShears, ObjWool, ObjBallOfWool, CoinsID}
	for _, id := range want {
		if !slices.Contains(SheepShearer.ToolIDs, id) {
			t.Errorf("Module.ToolIDs lacks %d: the engine's own deposits would bank it", id)
		}
	}
	steps := map[string]Step{
		"balls":  DecideSheepShearer(sheepSnap(StatusNotStarted)),
		"shears": DecideSheepShearer(sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress), 1))),
		"deposit": DecideSheepShearer(func() Snapshot {
			s := sheepWithKit(sheepWithStage(sheepSnap(StatusInProgress, ObjShears), 1))
			s.FreeSlots = 2
			return s
		}()),
	}
	for name, st := range steps {
		if st.Kind != StepProvision && st.Kind != StepDeposit {
			t.Errorf("%s: got %s", name, st.Describe())
			continue
		}
		for _, id := range want {
			if !slices.Contains(st.KeepIDs, id) {
				t.Errorf("%s step's keep list lacks %d (%v)", name, id, st.KeepIDs)
			}
		}
	}
	if k := SheepKeepIDs(); !slices.Equal(k, want) {
		t.Errorf("SheepKeepIDs %v", k)
	}
}

// sheep_journal.rs2's strings, as the client renders them.
func TestParseSheepShearerJournalAgainstTheContentText(t *testing.T) {
	notStarted := []string{"@dbl@I can start this quest by speaking to @dre@Farmer Fred @dbl@at his|", "@dre@farm @dbl@just a little way @dre@North West of Lumbridge"}
	inProgress := func(owed string) []string {
		return []string{"@str@I asked Farmer Fred, near Lumbridge, for a quest. Fred|", "@str@said he'd pay me for shearing his sheep for him!||",
			"@dbl@I need to collect " + owed + " more @dre@balls of wool."}
	}
	complete := []string{"@str@I asked Farmer Fred, near Lumbridge, for a quest. Fred|", "@str@said he'd pay me for shearing his sheep for him!||",
		"@str@I brought Farmer Fred 20 balls of wool, and he paid me for|", "@str@it!||", "@red@QUEST COMPLETE!"}
	for _, c := range []struct {
		name  string
		lines []string
		stage int
		ok    bool
	}{
		{"not started", notStarted, 0, true},
		{"20 owed is stage 1", inProgress("20"), 1, true},
		{"19 owed is stage 2", inProgress("19"), 2, true},
		{"7 owed is stage 14", inProgress("7"), 14, true},
		{"1 owed is stage 20", inProgress("1"), 20, true},
		{"complete", complete, SheepComplete, true},
		{"the unreachable all-collected line", []string{"@dbl@I have collected all 20 @dre@balls of wool.|"}, SheepLastWool + 1, true},
		{"nothing rendered", nil, 0, false},
		{"21 owed is not a number the journal prints", inProgress("21"), 0, false},
		{"an unrelated page", []string{"@dbl@Something else entirely."}, 0, false},
	} {
		stage, ok := ParseSheepShearerJournal(c.lines)
		if stage != c.stage || ok != c.ok {
			t.Errorf("%s: stage %d ok %v, want %d %v", c.name, stage, ok, c.stage, c.ok)
		}
	}
}

func TestSheepBallsOwedFollowsTheStage(t *testing.T) {
	for _, c := range []struct{ stage, owed int }{{0, 20}, {-1, 20}, {1, 20}, {2, 19}, {14, 7}, {20, 1}, {21, 1}, {22, 1}} {
		if got := SheepBallsOwed(c.stage); got != c.owed {
			t.Errorf("stage %d owes %d, want %d", c.stage, got, c.owed)
		}
	}
	// The journal's N and the varp agree both ways: parsing a journal that says N owed and asking what is owed gives N back.
	for n := 1; n <= SheepBalls; n++ {
		stage, ok := ParseSheepShearerJournal([]string{"@dbl@I need to collect " + strconv.Itoa(n) + " more @dre@balls of wool."})
		if !ok || SheepBallsOwed(stage) != n {
			t.Errorf("journal %d owed: stage %d ok %v owes %d", n, stage, ok, SheepBallsOwed(stage))
		}
	}
}

// Content's menus, as fred_the_farmer.rs2 writes them, against the stop's preference list.
func TestFredStopPicksTheRightOptionsAtEveryMenu(t *testing.T) {
	menus := []struct {
		options []string
		want    string
	}{
		{[]string{"I'm looking for a quest.", "I'm looking for something to kill.", "I'm lost."}, "I'm looking for a quest."},
		{[]string{"Yes okay. I can do that.", "That doesn't sound a very exciting quest.", "What do you mean, The Thing?"}, "Yes okay. I can do that."},
		// The two shorter menus the other branches reach.
		{[]string{"Yes okay. I can do that.", "No, I'll give it a miss."}, "Yes okay. I can do that."},
		{[]string{"Yes okay. I can do that.", "Erm, I'm a bit worried about this thing."}, "Yes okay. I can do that."},
	}
	for _, m := range menus {
		if got, ok := PickPreferred(m.options, FredStop.Prefer); !ok || got != m.want {
			t.Errorf("menu %v: picked %q (%v), want %q", m.options, got, ok, m.want)
		}
	}
}

func TestSheepShearerModuleIdentity(t *testing.T) {
	m := SheepShearer
	if m.Record.ID != "sheep" || m.Record.Name != "Sheep Shearer" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if QuestTabName("sheep") != m.Record.Name {
		t.Errorf("QuestTabName %q", QuestTabName("sheep"))
	}
	if m.ReadStage == nil {
		t.Error("Sheep Shearer reads its stage: how many balls Fred has taken is only in the journal")
	}
	if m.MaxDeaths != 1 || m.Sustain.EatAt != 0 || m.NoRetaliate {
		t.Errorf("a quest with no combat: MaxDeaths %d, sustain %+v, no-retaliate %v", m.MaxDeaths, m.Sustain, m.NoRetaliate)
	}
	if !m.BankSet || m.Bank != DraynorBank {
		t.Errorf("bank %v set %v", m.Bank, m.BankSet)
	}
}
