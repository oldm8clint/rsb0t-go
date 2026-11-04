package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestPickPreferredTakesTheFirstPreferenceNotTheFirstOption(t *testing.T) {
	opts := []string{"Yes please!", "Oh, it's a rune shop. No thank you, then.", "I have been sent here with a package for you."}
	got, ok := PickPreferred(opts, []string{"I have been sent here with a package for you."})
	if !ok || got != opts[2] {
		t.Fatalf("PickPreferred = %q, %v; want the package line", got, ok)
	}
}

func TestPickPreferredRespectsPreferenceOrderNotOptionOrder(t *testing.T) {
	// Both preferences are present; the earlier preference must win even though the option
	// matching the later one comes first on screen.
	opts := []string{"Not right now.", "Sure, no problem."}
	got, ok := PickPreferred(opts, []string{"Sure, no problem.", "Not right now."})
	if !ok || got != "Sure, no problem." {
		t.Fatalf("PickPreferred = %q, %v; want the earlier preference", got, ok)
	}
}

func TestPickPreferredIsCaseInsensitiveAndSubstring(t *testing.T) {
	got, ok := PickPreferred([]string{"I'M LOOKING FOR THE HEAD WIZARD."}, []string{"looking for the head wizard"})
	if !ok || got != "I'M LOOKING FOR THE HEAD WIZARD." {
		t.Fatalf("PickPreferred = %q, %v", got, ok)
	}
}

func TestPickPreferredReportsNoMatchRatherThanGuessing(t *testing.T) {
	// The whole point: falling through to an unmatched option is worse than stopping,
	// because a refusal is routinely offered first.
	_, ok := PickPreferred([]string{"I don't think so, it might animate and attack me!", "Search it"}, []string{"Open"})
	if ok {
		t.Fatal("PickPreferred matched nothing but reported ok; it must refuse rather than guess")
	}
}

func TestSedridorPreferListDoesNotCrossMatchItsOwnMenus(t *testing.T) {
	// A real hazard in this quest: seridor_3's p_choice2 offers "No, I'll only give it to
	// the head wizard.", which shares words with the first preference "I'm looking for the
	// head wizard.". If the first preference matched that refusal, the quest would loop.
	opts := []string{"Ok, here you are.", "No, I'll only give it to the head wizard."}
	got, ok := PickPreferred(opts, SedridorStop.Prefer)
	if !ok || got != "Ok, here you are." {
		t.Fatalf("Sedridor menu 2 picked %q (%v); want \"Ok, here you are.\"", got, ok)
	}
}

func TestSedridorPreferListPicksEachOfItsThreeMenus(t *testing.T) {
	menus := []struct {
		name string
		opts []string
		want string
	}{
		{"rune_mysteries p_choice3",
			[]string{"Nothing thanks, I'm just looking around.", "What are you doing down here?", "I'm looking for the head wizard."},
			"I'm looking for the head wizard."},
		{"seridor_3 p_choice2",
			[]string{"Ok, here you are.", "No, I'll only give it to the head wizard."},
			"Ok, here you are."},
		{"head_wizard_incredible p_choice2",
			[]string{"Yes, certainly.", "No, I'm busy."},
			"Yes, certainly."},
	}
	for _, m := range menus {
		got, ok := PickPreferred(m.opts, SedridorStop.Prefer)
		if !ok || got != m.want {
			t.Errorf("%s: picked %q (%v); want %q", m.name, got, ok, m.want)
		}
	}
}

func TestDukePreferListPicksBothOfItsMenus(t *testing.T) {
	multi, ok := PickPreferred([]string{"Have you any quests for me?", "Where can I find money?"}, DukeStop.Prefer)
	if !ok || multi != "Have you any quests for me?" {
		t.Fatalf("duke multi2 picked %q (%v)", multi, ok)
	}
	accept, ok := PickPreferred([]string{"Sure, no problem.", "Not right now."}, DukeStop.Prefer)
	if !ok || accept != "Sure, no problem." {
		t.Fatalf("duke_rune_mysteries_start p_choice2 picked %q (%v)", accept, ok)
	}
}

func TestNormaliseLinesReplacesColourTagsWithSpaceAndFoldsPipes(t *testing.T) {
	in := []string{"@str@I spoke to Duke Horacio|@str@and he showed me", "@dbl@a strange talisman"}
	got := NormaliseLines(in)
	want := "i spoke to duke horacio and he showed me a strange talisman"
	if got != want {
		t.Fatalf("NormaliseLines = %q, want %q", got, want)
	}
}

func TestNormaliseLinesDoesNotJoinWordsAcrossATag(t *testing.T) {
	// Removing the tag outright would produce "redberrypie"; replacing with a space is what
	// keeps a two-word needle matchable.
	if got := NormaliseLines([]string{"redberry@dbl@pie"}); got != "redberry pie" {
		t.Fatalf("NormaliseLines = %q, want %q", got, "redberry pie")
	}
}

func TestPickByLineTakesTheLongestMatchingRule(t *testing.T) {
	spoken := []string{"Speak the words cur tanath now."}
	opts := []string{"ar cur", "cur tanath"}
	rules := []LineRule{{WhenLine: "ar cur", Choose: "ar cur"}, {WhenLine: "cur tanath", Choose: "cur tanath"}}
	got, ok := PickByLine(spoken, opts, rules)
	if !ok || got != "cur tanath" {
		t.Fatalf("PickByLine = %q, %v; the longer rule must win over the overlapping shorter one", got, ok)
	}
}

func TestPickByLineRefusesWithNothingSpoken(t *testing.T) {
	if _, ok := PickByLine(nil, []string{"a"}, []LineRule{{WhenLine: "x", Choose: "a"}}); ok {
		t.Fatal("PickByLine matched on an empty line")
	}
}

func TestIsUndergroundAndNeedsHop(t *testing.T) {
	surface := tile.Tile{X: 3105, Z: 3162}
	basement := tile.Tile{X: 3103, Z: 9572}
	if IsUnderground(surface) {
		t.Error("the Wizards' Tower ground floor read as underground")
	}
	if !IsUnderground(basement) {
		t.Error("the Wizards' Tower basement did not read as underground")
	}
	if !NeedsHop(surface, basement) || !NeedsHop(basement, surface) {
		t.Error("crossing the underground boundary must need a hop, both ways")
	}
	if NeedsHop(surface, tile.Tile{X: 3253, Z: 3402}) {
		t.Error("two surface tiles must not need a hop")
	}
}

func TestNearestHopFromPicksTheSameSideHop(t *testing.T) {
	down, ok := NearestHopFrom(tile.Tile{X: 3110, Z: 3162}, WizardHops)
	if !ok || down.Op != "Climb-down" {
		t.Fatalf("from the surface got %q (%v); want the Climb-down hop", down.Op, ok)
	}
	up, ok := NearestHopFrom(tile.Tile{X: 3103, Z: 9572}, WizardHops)
	if !ok || up.Op != "Climb-up" {
		t.Fatalf("from the basement got %q (%v); want the Climb-up hop", up.Op, ok)
	}
}

func TestLadderHopWalkTargetFallsBackToStand(t *testing.T) {
	// Neither Wizards' Tower hop needs a separate walk target any more (see WizardHops'
	// own comment on why the TS def's did and why this build's do not), so both fall back.
	for i, h := range WizardHops {
		if got := h.WalkTarget(); got != h.Stand {
			t.Errorf("hop %d walk target = %v; want its Stand %v", i, got, h.Stand)
		}
	}
	// And an explicit Walk still wins when one is set.
	explicit := LadderHop{Stand: tile.Tile{X: 1, Z: 1}, Walk: tile.Tile{X: 9, Z: 9}, WalkSet: true}
	if got := explicit.WalkTarget(); got != (tile.Tile{X: 9, Z: 9}) {
		t.Errorf("an explicit Walk was ignored: %v", got)
	}
}

func TestWizardHopsMatchTheBakedTransportGraph(t *testing.T) {
	// These four tiles are the live-found correction to defs/runemysteries.ts, and the
	// reason the first end-to-end run parked. internal/webwalk/graph.json carries:
	//   Climb-down loc 2147 @ 3104,3162  stand 3104,3163 -> lands 3104,9576
	//   Climb-up   loc 2148 @ 3103,9576  stand 3103,9577 -> lands 3105,3162
	// If a future edit "restores" the TS tiles, this fails rather than costing another run.
	down, up := WizardHops[0], WizardHops[1]
	if down.Op != "Climb-down" || down.Stand != (tile.Tile{X: 3104, Z: 3163}) || down.Arrive != (tile.Tile{X: 3104, Z: 9576}) {
		t.Errorf("down hop = %+v; the graph says stand 3104,3163 -> 3104,9576", down)
	}
	if up.Op != "Climb-up" || up.Stand != (tile.Tile{X: 3103, Z: 9577}) || up.Arrive != (tile.Tile{X: 3105, Z: 3162}) {
		t.Errorf("up hop = %+v; the graph says stand 3103,9577 -> 3105,3162", up)
	}
	// 3105,3162 is where the UP ladder lands, never where the DOWN one is stood on.
	if down.Stand == up.Arrive {
		t.Error("the down hop stands on the up ladder's landing tile — that is the TS def's bug")
	}
}

// snapshot builds a Snapshot with the named items held.
func snapshot(journal Status, held ...string) Snapshot {
	inv := map[string]int{}
	for _, h := range held {
		inv[strings.ToLower(h)]++
	}
	return Snapshot{Journal: journal, Inv: inv, Worn: map[string]int{}}
}

func TestDecideRuneMysteriesWholeStateMachine(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		want StepKind
		npc  string
	}{
		{"complete is done", snapshot(StatusComplete), StepDone, ""},
		{"unknown waits, it is not notStarted", snapshot(StatusUnknown), StepWait, ""},
		{"not started goes to the Duke", snapshot(StatusNotStarted), StepTalk, "Duke Horacio"},
		{"holding the talisman goes to Sedridor", snapshot(StatusInProgress, AirTalisman), StepTalk, "Sedridor"},
		{"holding the package goes to Aubury", snapshot(StatusInProgress, ResearchPackage), StepTalk, "Aubury"},
		{"holding the notes goes to Sedridor", snapshot(StatusInProgress, ResearchNotes), StepTalk, "Sedridor"},
	}
	for _, c := range cases {
		got := DecideRuneMysteries(c.snap)
		if got.Kind != c.want {
			t.Errorf("%s: kind = %q, want %q", c.name, got.Kind, c.want)
			continue
		}
		if c.npc != "" && got.Stop.Npc != c.npc {
			t.Errorf("%s: npc = %q, want %q", c.name, got.Stop.Npc, c.npc)
		}
	}
}

func TestDecideRuneMysteriesNotesOutrankTheTalisman(t *testing.T) {
	// The final handover gives the air talisman BACK in the same script that deletes the
	// notes (sedridor.rs2 head_wizard_notes), so the pack can briefly hold both. If the
	// talisman branch won, the account would be sent to Sedridor for a talisman he no
	// longer wants, which is the same destination but the wrong reason — so assert the
	// ordering directly rather than through the NPC.
	both := snapshot(StatusInProgress, AirTalisman, ResearchNotes)
	if got := DecideRuneMysteries(both); got.Stop.Npc != "Sedridor" {
		t.Fatalf("holding both went to %q", got.Stop.Npc)
	}
	// And a package plus a talisman must go to Aubury, not back to Sedridor.
	pkg := snapshot(StatusInProgress, AirTalisman, ResearchPackage)
	if got := DecideRuneMysteries(pkg); got.Stop.Npc != "Aubury" {
		t.Fatalf("holding the package and a spare talisman went to %q, want Aubury", got.Stop.Npc)
	}
}

func TestDecideRuneMysteriesRotatesRecoveryProbes(t *testing.T) {
	// An empty pack mid-quest is a real state (Aubury is "looking the package over"), and
	// the only way on is to talk to somebody again. Rotating means a wrong first guess
	// costs one tick rather than wedging.
	seen := map[string]bool{}
	for i := 0; i < len(runeMysteriesProbes); i++ {
		s := snapshot(StatusInProgress)
		s.NoProgress = i
		seen[DecideRuneMysteries(s).Stop.Npc] = true
	}
	if len(seen) != len(runeMysteriesProbes) {
		t.Fatalf("the probe rotation visited %d distinct NPCs, want %d: %v", len(seen), len(runeMysteriesProbes), seen)
	}
	// First probe is Aubury, because `given_package` is the empty-pack stage a run lands in.
	first := snapshot(StatusInProgress)
	if got := DecideRuneMysteries(first); got.Stop.Npc != "Aubury" {
		t.Fatalf("the first probe is %q, want Aubury", got.Stop.Npc)
	}
}

func TestDecideRuneMysteriesSurvivesANegativeNoProgress(t *testing.T) {
	s := snapshot(StatusInProgress)
	s.NoProgress = -1
	// Go's % keeps the sign of the dividend, so a bare index would panic here.
	if got := DecideRuneMysteries(s); got.Kind != StepTalk {
		t.Fatalf("kind = %q", got.Kind)
	}
}

func TestEveryReachableRuneMysteriesStepIsWellFormed(t *testing.T) {
	// The Kind-plus-fields encoding cannot make an unset field a compile error the way the
	// TS discriminated union does, so the check lives here instead.
	snaps := []Snapshot{
		snapshot(StatusComplete), snapshot(StatusUnknown), snapshot(StatusNotStarted),
		snapshot(StatusInProgress), snapshot(StatusInProgress, AirTalisman),
		snapshot(StatusInProgress, ResearchPackage), snapshot(StatusInProgress, ResearchNotes),
	}
	for i := range runeMysteriesProbes {
		s := snapshot(StatusInProgress)
		s.NoProgress = i
		snaps = append(snaps, s)
	}
	for _, s := range snaps {
		step := DecideRuneMysteries(s)
		if why := ValidateStep(step); why != "" {
			t.Errorf("snapshot %+v produced a malformed step: %s", s, why)
		}
	}
}

func TestSnapshotLookupsAreCaseInsensitiveAndBankAwareness(t *testing.T) {
	s := Snapshot{
		Inv:     map[string]int{"air talisman": 1},
		InvIDs:  map[int]int{1438: 1},
		Bank:    map[string]int{"coins": 400},
		BankIDs: map[int]int{995: 400},
	}
	if !s.Has("Air Talisman") || s.Held("AIR TALISMAN") != 1 {
		t.Error("name lookup is not case-insensitive")
	}
	if s.HeldID(1438) != 1 {
		t.Error("id lookup failed")
	}
	// An unread bank is not an empty one: the second return is what says so.
	if n, known := s.Banked("coins"); known || n != 0 {
		t.Errorf("an unread bank reported %d, known=%v; want 0, false", n, known)
	}
	s.BankKnown = true
	if n, known := s.Banked("Coins"); !known || n != 400 {
		t.Errorf("a read bank reported %d, known=%v; want 400, true", n, known)
	}
	if n, known := s.BankedID(995); !known || n != 400 {
		t.Errorf("BankedID reported %d, known=%v", n, known)
	}
}

func TestCheckRequirements(t *testing.T) {
	rec := Record{Requirements: Requirements{
		MinQuestPoints: 3,
		Skills:         []SkillReq{{Skill: "mining", Level: 20}},
		Quests:         []string{"runemysteries"},
	}}
	player := PlayerState{QuestPoints: 1, SkillLevels: map[string]int{"mining": 10}, CompletedQuests: map[string]bool{}}
	res := CheckRequirements(rec, player)
	if len(res) != 3 {
		t.Fatalf("got %d results, want 3", len(res))
	}
	for _, r := range res {
		if r.OK {
			t.Errorf("unexpectedly satisfied: %+v", r)
		}
		if r.Reason == "" {
			t.Error("a failed requirement carried no reason")
		}
	}
	if !strings.Contains(res[1].Reason, "Mining 20") {
		t.Errorf("skill reason = %q; want it to name the skill with a capital", res[1].Reason)
	}

	ok := CheckRequirements(rec, PlayerState{
		QuestPoints:     5,
		SkillLevels:     map[string]int{"mining": 25},
		CompletedQuests: map[string]bool{"runemysteries": true},
	})
	for _, r := range ok {
		if !r.OK {
			t.Errorf("a satisfied requirement reported failure: %+v", r)
		}
	}
}

func TestCheckItemsTreatsAnUnreadBankAsUnknownNotEmpty(t *testing.T) {
	rec := Record{Items: []Item{{Name: "Bucket", Qty: 1, Kind: MustHave}}}
	unread := CheckItems(rec, BankSnapshot{Counts: map[string]int{}, Known: false})
	if !unread[0].OK {
		t.Error("a mustHave item blocked on a bank nothing had opened yet; that stops the queue before it can ever open a booth")
	}
	read := CheckItems(rec, BankSnapshot{Counts: map[string]int{}, Known: true})
	if read[0].OK {
		t.Error("a mustHave item passed against a bank that was read and did not hold it")
	}
}

func TestCheckItemsMarksAcquirablesForGathering(t *testing.T) {
	rec := Record{Items: []Item{{Name: "Clay", Qty: 6, Kind: Acquirable}}}
	res := CheckItems(rec, BankSnapshot{Counts: map[string]int{"clay": 2}, Known: true})
	if !res[0].OK || !res[0].WillGather {
		t.Errorf("acquirable = %+v; want ok with WillGather", res[0])
	}
	full := CheckItems(rec, BankSnapshot{Counts: map[string]int{"clay": 6}, Known: true})
	if full[0].WillGather {
		t.Error("a satisfied acquirable still asked to be gathered")
	}
}

func TestEvaluateDoneBeatsEverything(t *testing.T) {
	rec := Record{ID: "x", Name: "X", Requirements: Requirements{MinQuestPoints: 99}}
	got := Evaluate(rec, PlayerState{}, BankSnapshot{Known: true}, StatusComplete)
	if got.Status != EligDone || len(got.Reasons) != 0 {
		t.Fatalf("a complete quest evaluated as %+v", got)
	}
}

func TestEvaluateDropsItemGatesOnceAQuestIsUnderway(t *testing.T) {
	// Re-imposing item requirements mid-quest blocks resuming, since by then the items may
	// legitimately be gone — handed over, ground up, drunk.
	rec := Record{ID: "x", Name: "X", Items: []Item{{Name: "Bucket", Qty: 1, Kind: MustHave}}}
	bank := BankSnapshot{Counts: map[string]int{}, Known: true}

	notStarted := Evaluate(rec, PlayerState{}, bank, StatusNotStarted)
	if notStarted.Status != EligBlocked {
		t.Fatalf("starting without the item evaluated as %v", notStarted.Status)
	}
	inProgress := Evaluate(rec, PlayerState{}, bank, StatusInProgress)
	if inProgress.Status != EligReady {
		t.Fatalf("resuming without the item evaluated as %v (%v)", inProgress.Status, inProgress.Reasons)
	}
}

func TestEvaluateKeepsSkillGatesMidQuest(t *testing.T) {
	// Nothing consumes a level or a quest point, so those gates apply either way.
	rec := Record{ID: "x", Name: "X", Requirements: Requirements{Skills: []SkillReq{{Skill: "mining", Level: 20}}}}
	got := Evaluate(rec, PlayerState{SkillLevels: map[string]int{"mining": 1}}, BankSnapshot{Known: true}, StatusInProgress)
	if got.Status != EligBlocked {
		t.Fatalf("a skill gate was dropped mid-quest: %+v", got)
	}
}

func TestRuneMysteriesIsReadyForAFreshAccount(t *testing.T) {
	got := Evaluate(RuneMysteries.Record, PlayerState{}, BankSnapshot{Known: true}, StatusNotStarted)
	if got.Status != EligReady {
		t.Fatalf("Rune Mysteries blocked a fresh account: %+v", got)
	}
}

func TestNextQuestSkipsParkedButStillReturnsOneWhenAllAreParked(t *testing.T) {
	order := []string{"a", "b"}
	picked := map[string]bool{"a": true, "b": true}
	elig := map[string]Eligibility{
		"a": {ID: "a", Status: EligReady},
		"b": {ID: "b", Status: EligReady},
	}
	if got := NextQuest(order, picked, elig, map[string]bool{"a": true}); got != "b" {
		t.Errorf("with a parked, NextQuest = %q, want b", got)
	}
	if got := NextQuest(order, picked, elig, map[string]bool{"a": true, "b": true}); got != "a" {
		t.Errorf("with both parked, NextQuest = %q, want the first ready one", got)
	}
	blocked := map[string]Eligibility{"a": {ID: "a", Status: EligBlocked}, "b": {ID: "b", Status: EligDone}}
	if got := NextQuest(order, picked, blocked, nil); got != "" {
		t.Errorf("with nothing ready, NextQuest = %q, want empty", got)
	}
}

func TestQueueRowsLabelsEachState(t *testing.T) {
	order := []string{"run", "park", "block", "done", "unrated", "skipped"}
	picked := map[string]bool{"run": true, "park": true, "block": true, "done": true, "unrated": true}
	elig := map[string]Eligibility{
		"run":   {ID: "run", Name: "Running", Status: EligReady},
		"park":  {ID: "park", Name: "Parked", Status: EligReady},
		"block": {ID: "block", Name: "Blocked", Status: EligBlocked, Reasons: []string{"needs Mining 20 (have 1)"}},
		"done":  {ID: "done", Name: "Done", Status: EligDone},
	}
	rows := QueueRows(order, picked, elig, map[string]bool{"park": true}, "run")
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5 (the unpicked quest must not appear)", len(rows))
	}
	want := []QueueStatus{QueueRunning, QueueParked, QueueBlocked, QueueDone, QueueUnknown}
	for i, w := range want {
		if rows[i].Status != w {
			t.Errorf("row %d (%s) = %v, want %v", i, rows[i].ID, rows[i].Status, w)
		}
	}
	if len(rows[2].Reasons) != 1 {
		t.Error("a blocked row lost its reasons")
	}
}

func TestProgressSignatureIsStableAcrossMapIterationOrder(t *testing.T) {
	// Go randomises map iteration, so an unsorted signature would differ from itself on the
	// next tick — which would silently disable parking altogether.
	s := Snapshot{
		Journal:   StatusInProgress,
		Inv:       map[string]int{"coins": 400, "air talisman": 1, "notes": 1, "bucket": 2, "pot": 3},
		InvIDs:    map[int]int{995: 400, 1438: 1, 292: 1, 1925: 2, 1931: 3},
		Worn:      map[string]int{},
		WornIDs:   map[int]int{},
		Tile:      tile.Tile{X: 3253, Z: 3402},
		TileKnown: true,
	}
	first := ProgressSignature(s)
	for i := 0; i < 50; i++ {
		if got := ProgressSignature(s); got != first {
			t.Fatalf("signature changed between calls on identical state:\n%s\n%s", first, got)
		}
	}
}

func TestProgressSignatureChangesWithEveryThingThatCounts(t *testing.T) {
	base := Snapshot{Journal: StatusInProgress, Inv: map[string]int{}, Worn: map[string]int{}, TileKnown: true}
	sig := ProgressSignature(base)

	moved := base
	moved.Tile = tile.Tile{X: 1, Z: 1}
	if ProgressSignature(moved) == sig {
		t.Error("walking did not count as progress")
	}
	got := base
	got.Inv = map[string]int{"notes": 1}
	if ProgressSignature(got) == sig {
		t.Error("gaining an item did not count as progress")
	}
	advanced := base
	advanced.Journal = StatusComplete
	if ProgressSignature(advanced) == sig {
		t.Error("the quest completing did not count as progress")
	}
	// NoProgress itself must NOT be in the signature: it is derived from the signature, and
	// including it would make every signature unique and parking impossible.
	counted := base
	counted.NoProgress = 7
	if ProgressSignature(counted) != sig {
		t.Error("NoProgress leaked into the signature, which would stop the watchdog ever parking")
	}
}

func TestProgressSignatureDistinguishesUnknownPositionFromTileZero(t *testing.T) {
	unknown := Snapshot{Inv: map[string]int{}, Worn: map[string]int{}}
	zero := Snapshot{Inv: map[string]int{}, Worn: map[string]int{}, TileKnown: true}
	if ProgressSignature(unknown) == ProgressSignature(zero) {
		t.Error("an unknown position read the same as standing on 0,0,0")
	}
}

func TestWatchdogCountsConsecutiveIdenticalSignatures(t *testing.T) {
	var w Watchdog
	if got := w.Note("a"); got != 0 {
		t.Fatalf("first note = %d, want 0", got)
	}
	for i := 1; i <= NoProgressPark; i++ {
		if got := w.Note("a"); got != i {
			t.Fatalf("note %d = %d", i, got)
		}
	}
	if got := w.Note("b"); got != 0 {
		t.Fatalf("a changed signature = %d, want 0", got)
	}
	w.Note("b")
	w.Reset()
	if got := w.Note("b"); got != 0 {
		t.Fatalf("after Reset = %d, want 0", got)
	}
}

func TestStepAdvancesWorld(t *testing.T) {
	if (Step{Kind: StepWait, Reason: "x"}).AdvancesWorld() || (Step{Kind: StepDone}).AdvancesWorld() {
		t.Error("wait and done must not burn the no-progress budget; they change nothing by design")
	}
	if !(Step{Kind: StepTalk}).AdvancesWorld() {
		t.Error("a talk step must be held to account")
	}
}

func TestValidateStepCatchesEachMalformedKind(t *testing.T) {
	bad := []Step{
		{Kind: StepTalk},
		{Kind: StepTalk, Stop: NpcStop{Npc: "Aubury", Leash: 4}},
		{Kind: StepTalk, Stop: NpcStop{Npc: "Aubury", Prefer: []string{"x"}}},
		{Kind: StepInteractLoc, Loc: "Ladder"},
		{Kind: StepUseOn, Item: "Barcrawl card", Target: "Bartender"},
		{Kind: StepUseOn, Item: "Barcrawl card", Target: "Bartender", TargetKind: "nonsense"},
		{Kind: StepWithdraw},
		{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Coins"}}},
		{Kind: StepWait},
		{Kind: "invented"},
	}
	for i, s := range bad {
		if why := ValidateStep(s); why == "" {
			t.Errorf("case %d (%+v) validated clean", i, s)
		}
	}
	good := []Step{
		{Kind: StepTalk, Stop: DukeStop},
		{Kind: StepInteractLoc, Loc: "Ladder", Op: "Climb-down"},
		{Kind: StepUseOn, Item: "Barcrawl card", Target: "Bartender", TargetKind: TargetNpc},
		{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Coins", Qty: 400}}},
		{Kind: StepDeposit}, {Kind: StepScanBank}, {Kind: StepDone},
		{Kind: StepWait, Reason: "quest journal not loaded"},
	}
	for i, s := range good {
		if why := ValidateStep(s); why != "" {
			t.Errorf("good case %d (%+v) rejected: %s", i, s, why)
		}
	}
}

func TestStepDescribe(t *testing.T) {
	cases := []struct {
		step Step
		want string
	}{
		{Step{Kind: StepTalk, Stop: DukeStop}, "talk to Duke Horacio"},
		{Step{Kind: StepDone}, "done"},
		{Step{Kind: StepWait, Reason: "quest journal not loaded"}, "quest journal not loaded"},
		{Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Coins", Qty: 408}}}, "withdraw Coins x408"},
	}
	for _, c := range cases {
		if got := c.step.Describe(); got != c.want {
			t.Errorf("Describe() = %q, want %q", got, c.want)
		}
	}
}

func TestRuneMysteriesModuleNamesTheQuestTabSpelling(t *testing.T) {
	// "Rune Mysteries" (without "Quest") returns StatusUnknown from internal/quests for
	// ever, and the module would then wait for a journal that never loads.
	if RuneMysteries.Record.Name != "Rune Mysteries Quest" {
		t.Fatalf("record name = %q; internal/quests/table.go spells it \"Rune Mysteries Quest\"", RuneMysteries.Record.Name)
	}
	if RuneMysteries.Record.QuestPoints != 1 {
		t.Errorf("quest points = %d; ^runemysteries_questpoints is 1", RuneMysteries.Record.QuestPoints)
	}
	if len(RuneMysteries.Hops) != 2 {
		t.Errorf("the Wizards' Tower basement needs a hop each way, got %d", len(RuneMysteries.Hops))
	}
}
