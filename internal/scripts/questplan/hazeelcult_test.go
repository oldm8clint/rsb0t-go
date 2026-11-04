package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Hazeel Cult (W13, 19 Oct 2026): the journal parser over the pages the content renders (the cult's side and the Carnillean side), every
// state of Decide on the cult's side, the keep list, the pockets' rectangles, and whole simulated quests (a world that answers every step
// the way the content's scripts do).

// ---------------------------------------------------------------------------------------------------------------------
// The journal
// ---------------------------------------------------------------------------------------------------------------------

// hzJournalFixtures are the pages hazeelcult_journal.rs2 renders, every string of a branch joined in order (the journal is
// `~quest_journal(title, $x)` over exactly that text, `|` the client's line break).
const (
	hzHead = "@str@I spoke to Sir Ceril Carnillean at his house, and|@str@agreed to help him investigate the theft of a|@str@family heirloom."
	hzMeet = "|@str@I found a member of the cult called Clivet at the entrance|@str@to the cult's hideout, south of Ardougne.|"
	// The cult's side, struck line by line as the stages pass.
	hzPoison = "@str@Having decided to assist the cult in their mission|@str@to revive Hazeel, I have poisoned some food to|@str@try and poison the Carnilleans.|"
	hzAmulet = "@str@I spoke to Clivet and he told me that I had failed, but he|@str@gave me an amulet anyway.|"
	hzRafts  = "@str@Then I managed to take the sewer rafts to the cult hideout.|"
	hzFound  = "@str@I found the scroll needed to revive Hazeel in a secret|@str@compartment upstairs at the Carnillean Mansion.|"
	// The Carnillean side's lines.
	hzLies   = "@str@He told me a pack of lies about the Carnilleans, then asked|@str@me to join the cult. Obviously, I refused.|@str@I was still no closer to recovering the missing armour|"
	hzKilled = "@str@I managed to enter the hideout, kill the cult leader and|@str@retrieve the armour. I discovered that Jones the Butler|@str@was secretly a member of the cult and a traitor.|"
)

var hzJournalFixtures = map[int]string{
	HzNotStarted: "@dbl@I can start this quest by talking to @dre@Sir Ceril Carnillean@dbl@ at the house due @dre@West@dbl@ of @dre@Ardougne Zoo",
	HzStarted:    hzHead + "|@dbl@The @dre@Cult@dbl@ who were responsible live in a cave @dre@South@dbl@ of the|@dre@Carnillean Mansion near the start of the forest|",
	HzSpokenClivet: hzHead + hzMeet + "@dbl@He told me a pack of lies about the Carnilleans, then asked me to join the cult. Obviously, I refused.|" +
		"I was still no closer to recovering the @dre@missing armour",
	HzClivetDecision: hzHead + hzMeet + "@dre@Clivet@dbl@ told me the truth about the @dre@Carnilleans@dbl@, and I decided I would help them in their revenge. He gave me some @dre@poison@dbl@ to put into the @dre@family's food",
	HzPoured:         hzHead + hzMeet + hzPoison + "@dbl@I should report back to @dre@Clivet@dbl@.",
	HzFinishedSide: hzHead + hzMeet + hzPoison + hzAmulet + hzRafts +
		"@dre@Alomone@dbl@ told me to find a @dre@spell scroll@dbl@ that is hidden somewhere within the @dre@Carnillean household|",
	HzScroll: hzHead + hzMeet + hzPoison + hzAmulet + hzRafts + hzFound +
		"@dbl@I should take the scroll to @dre@Alomone@dbl@ so they can perform the ceremony to revive @dre@Hazeel@dbl@ and get my @dre@reward.",
	HzComplete: hzHead + hzMeet + hzPoison + hzAmulet + hzRafts + hzFound + "@str@I gave Alomone the scroll from the Carnillean Mansion|" +
		"@str@and helped perform the revivification of Hazeel.|@str@I was rewarded for all of my help in returning Hazeel|@str@to his followers.|@red@QUEST COMPLETE!",
	// The Carnillean side: the same head, then the other branch.
	HzGoodSide + HzClivetDecision: hzHead + hzMeet + hzLies + "@dbl@After speaking to him, he jumped onto a @dre@raft@dbl@ and headed into the @dre@sewer system@dbl@. I need to find a way to follow.",
	HzGoodSide + HzFinishedSide:   hzHead + hzMeet + hzLies + hzKilled + "@dbl@I should return @dre@Ceril's armour@dbl@ and tell him about @dre@Jones",
	HzGoodSide + HzScroll: hzHead + hzMeet + hzLies + hzKilled + "@str@I returned the armour, but Ceril didn't believe Jones was|@str@involved with the cult and was responsible for the theft.|" +
		"@dbl@I have to find some @dre@evidence@dbl@ linking @dre@Butler Jones@dbl@ to the @dre@Cult@dbl@ so that I can @dre@clear my name@dbl@ and claim my @dre@reward",
}

func TestParseHazeelJournalReadsEveryRenderedStageOfBothSides(t *testing.T) {
	for want, page := range hzJournalFixtures {
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseHazeelJournal(lines)
			if !ok || got != want {
				t.Errorf("stage %d page %.70q: parsed %d %v", want, page, got, ok)
			}
		}
	}
	// The cult's amulet changes the page at stage 5 and nothing else about the stage.
	withAmulet := hzHead + hzMeet + hzPoison + hzAmulet +
		"@dbl@By turning the @dre@sewer valves@dbl@ in the directions shown by the @dre@amulet@dbl@, I can take the @dre@sewer raft@dbl@ to the hideout"
	if got, ok := ParseHazeelJournal([]string{withAmulet}); !ok || got != HzPoured {
		t.Errorf("stage 5 with the amulet parsed %d %v", got, ok)
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseHazeelJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
	// Every page of the Carnillean side reads as the side, never as a cult stage (it would be run on the cult's road).
	for stage, page := range hzJournalFixtures {
		if stage < HzGoodSide {
			continue
		}
		got, _ := ParseHazeelJournal([]string{page})
		if got < HzGoodSide {
			t.Errorf("the Carnillean page of stage %d parsed %d", stage-HzGoodSide, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated world
// ---------------------------------------------------------------------------------------------------------------------

// hzWorld is Hazeel Cult as the content runs it: %hazeelcultquest, the talks, the poison, the crate's key and the chest's scroll, and where
// the account stands (the surface, the landing, the hideout, the kitchen, the attic).
type hzWorld struct {
	*kcWorld
	stage int
	good  bool
	// chestFinds counts the scrolls the chest has given.
	chestFinds int
}

func newHzWorld(t *testing.T, stage int, inv, bank map[int]int) *hzWorld {
	w := &hzWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage}
	w.tile = HzCerilAnchor
	return w
}

func (w *hzWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case HzNotStarted:
		j = StatusNotStarted
	case HzComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.stage, true
	if w.good {
		s.Stage += HzGoodSide
	}
	return s
}

func (w *hzWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepTalk:
		w.talk(st.Stop)
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *hzWorld) talk(stop NpcStop) {
	switch stop.ID {
	case NpcHzCeril:
		if w.stage != HzNotStarted {
			w.t.Fatalf("a talk with Ceril at stage %d", w.stage)
		}
		w.stage = HzStarted
		w.log = append(w.log, "ceril")
	case NpcHzClivet:
		if !InHzLanding(w.tile) {
			w.t.Fatalf("a talk with Clivet away from the landing, at %v", w.tile)
		}
		switch w.stage {
		case HzStarted, HzSpokenClivet:
			if w.inv[ObjPoison] != 0 {
				w.t.Fatalf("Clivet's menus with a vial already held")
			}
			w.inv[ObjPoison]++ // "Ok, count me in."
			w.stage = HzClivetDecision
			w.log = append(w.log, "clivet-join")
		case HzClivetDecision:
			if w.inv[ObjPoison] == 0 {
				w.inv[ObjPoison]++ // "I lost the poison you gave me..."
			}
			w.log = append(w.log, "clivet-vial")
		default:
			w.t.Fatalf("a talk with Clivet at stage %d", w.stage)
		}
	default:
		w.t.Fatalf("a talk with npc %d", stop.ID)
	}
}

func (w *hzWorld) custom(st Step) {
	switch st.Custom {
	case CustomHzDown:
		w.tile = HzLanding
	case CustomHzPour:
		if w.stage != HzClivetDecision || w.inv[ObjPoison] == 0 {
			w.t.Fatalf("the poison poured at stage %d holding %d", w.stage, w.inv[ObjPoison])
		}
		w.inv[ObjPoison]--
		w.stage = HzPoured
		w.tile = HzRangeStand
	case CustomHzAlomone:
		if w.stage != HzPoured {
			w.t.Fatalf("Alomone's first talk at stage %d", w.stage)
		}
		w.stage = HzFinishedSide
		w.tile = HzHideoutStand
	case CustomHzKey:
		if w.inv[ObjHzKey] == 0 {
			w.inv[ObjHzKey]++
		}
		w.tile = HzCrateStand
	case CustomHzChest:
		if w.inv[ObjHzKey] == 0 {
			w.t.Fatalf("the chest without a key")
		}
		// [oploc1,carnilleanopenchest]: a pack that holds the scroll gets "The chest is empty."; otherwise the scroll, and stage 6 becomes 7.
		if w.inv[ObjHzScroll] == 0 {
			w.inv[ObjHzScroll]++
			w.chestFinds++
			if w.stage == HzFinishedSide {
				w.stage = HzScroll
			}
		}
		w.tile = tile.Tile{X: 2572, Z: 3270, Level: 1}
	case CustomHzRitual:
		if w.stage != HzScroll || w.inv[ObjHzScroll] == 0 {
			w.t.Fatalf("the ritual at stage %d holding %d scrolls", w.stage, w.inv[ObjHzScroll])
		}
		w.inv[ObjHzScroll]--
		w.stage = HzComplete
		w.tile = HzHideoutStand
	default:
		w.t.Fatalf("unexpected custom %q", st.Custom)
	}
	w.log = append(w.log, st.Custom)
}

func (w *hzWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 60; i++ {
		st := DecideHazeelCult(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.tile, w.chestFinds)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.tile, w.chestFinds) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("60 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// ---------------------------------------------------------------------------------------------------------------------
// Whole quests
// ---------------------------------------------------------------------------------------------------------------------

// A fresh account does the whole cult road in the content's order: Ceril, down the cave to Clivet, the poison on the range, the valves and
// the raft and Alomone, the crate's key and the chest's scroll, the ritual.
func TestAFreshAccountDoesHazeelCultOnTheCultsSide(t *testing.T) {
	w := newHzWorld(t, HzNotStarted, nil, nil)
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"ceril", CustomHzDown, "clivet-join", CustomHzPour, CustomHzAlomone, CustomHzKey, CustomHzChest, CustomHzRitual}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.chestFinds != 1 || w.inv[ObjHzScroll] != 0 || w.inv[ObjPoison] != 0 {
		t.Errorf("%d scrolls from the chest, %d in the pack, %d poison", w.chestFinds, w.inv[ObjHzScroll], w.inv[ObjPoison])
	}
}

// A quest begun elsewhere is finished from wherever it stands: each stage's own first step, with the right items.
func TestHazeelCultResumesFromEveryStage(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		inv   map[int]int
		at    tile.Tile
		want  []string
	}{
		{"started, at the mansion", HzStarted, nil, HzCerilAnchor, []string{CustomHzDown, "clivet-join", CustomHzPour, CustomHzAlomone, CustomHzKey, CustomHzChest, CustomHzRitual}},
		{"started, already at the landing", HzStarted, nil, HzLanding, []string{"clivet-join", CustomHzPour, CustomHzAlomone, CustomHzKey, CustomHzChest, CustomHzRitual}},
		{"spoken to Clivet, on the stairs", HzSpokenClivet, nil, HzStairsTop, []string{CustomHzDown, "clivet-join", CustomHzPour, CustomHzAlomone, CustomHzKey, CustomHzChest, CustomHzRitual}},
		{"the poison in hand", HzClivetDecision, map[int]int{ObjPoison: 1}, HzCerilAnchor, []string{CustomHzPour, CustomHzAlomone, CustomHzKey, CustomHzChest, CustomHzRitual}},
		{"the poison lost", HzClivetDecision, nil, HzCerilAnchor, []string{CustomHzDown, "clivet-vial", CustomHzPour, CustomHzAlomone, CustomHzKey, CustomHzChest, CustomHzRitual}},
		{"the soup poisoned", HzPoured, nil, HzCerilAnchor, []string{CustomHzAlomone, CustomHzKey, CustomHzChest, CustomHzRitual}},
		{"Alomone has spoken", HzFinishedSide, nil, HzHideoutStand, []string{CustomHzKey, CustomHzChest, CustomHzRitual}},
		{"Alomone has spoken, the key held", HzFinishedSide, map[int]int{ObjHzKey: 1}, HzCerilAnchor, []string{CustomHzChest, CustomHzRitual}},
		{"the scroll found", HzScroll, map[int]int{ObjHzScroll: 1}, HzCerilAnchor, []string{CustomHzRitual}},
		{"the scroll lost", HzScroll, map[int]int{ObjHzKey: 1}, HzCerilAnchor, []string{CustomHzChest, CustomHzRitual}},
		{"the scroll and the key lost", HzScroll, nil, HzCerilAnchor, []string{CustomHzKey, CustomHzChest, CustomHzRitual}},
	} {
		w := newHzWorld(t, c.stage, c.inv, nil)
		w.tile = c.at
		if why, _ := w.run(); why != "" {
			t.Errorf("%s: waited: %s (log %v)", c.name, why, w.log)
			continue
		}
		if !slices.Equal(w.log, c.want) {
			t.Errorf("%s: the quest ran\n  %v\nwant\n  %v", c.name, w.log, c.want)
		}
	}
}

// The chest moves the quest on only for an account that holds no scroll: one found early (at stage 6, by hand) is banked first, the
// chest then gives another and the quest goes to 7.
func TestAScrollHeldAtStageSixIsBankedBeforeTheChest(t *testing.T) {
	w := newHzWorld(t, HzFinishedSide, map[int]int{ObjHzScroll: 1, ObjHzKey: 1, CoinsID: 50}, nil)
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"deposit", CustomHzChest, CustomHzRitual}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.bank[ObjHzScroll] != 1 || w.inv[ObjHzKey] != 1 || w.chestFinds != 1 {
		t.Errorf("the bank holds %d scrolls, the pack %d keys, the chest gave %d", w.bank[ObjHzScroll], w.inv[ObjHzKey], w.chestFinds)
	}
	// The deposit step keeps the key and the coins, and not the scroll.
	st := DecideHazeelCult(newHzWorld(t, HzFinishedSide, map[int]int{ObjHzScroll: 1, ObjHzKey: 1}, nil).snapshot())
	if st.Kind != StepDeposit || slices.Contains(st.KeepIDs, ObjHzScroll) || !slices.Contains(st.KeepIDs, ObjHzKey) || !slices.Contains(st.KeepIDs, ObjPoison) {
		t.Errorf("the deposit is %+v", st)
	}
}

// A pack with no room banks first, keeping the quest's items: three scripts add an item without asking for room.
func TestAFullPackBanksBeforeAnItemIsHandedOver(t *testing.T) {
	full := map[int]int{}
	for i := 0; i < 28; i++ {
		full[10000+i] = 1
	}
	for _, c := range []struct {
		name  string
		stage int
		inv   func() map[int]int
	}{
		{"Clivet's vial", HzStarted, func() map[int]int { return full }},
		{"Clivet's new vial", HzClivetDecision, func() map[int]int { return full }},
		{"the crate's key", HzFinishedSide, func() map[int]int { return full }},
		{"the chest's scroll", HzScroll, func() map[int]int {
			m := map[int]int{ObjHzKey: 1}
			for i := 0; i < 27; i++ {
				m[10000+i] = 1
			}
			return m
		}},
	} {
		w := newHzWorld(t, c.stage, c.inv(), nil)
		w.tile = HzLanding
		st := DecideHazeelCult(w.snapshot())
		if st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, ObjPoison) || !slices.Contains(st.KeepIDs, ObjHzKey) || !slices.Contains(st.KeepIDs, ObjHzScroll) {
			t.Errorf("%s: a full pack is met with %s (%+v)", c.name, st.Describe(), st)
		}
	}
	// A pack with room is not sent to the bank.
	w := newHzWorld(t, HzStarted, map[int]int{CoinsID: 10}, nil)
	w.tile = HzLanding
	if st := DecideHazeelCult(w.snapshot()); st.Kind != StepTalk {
		t.Errorf("a pack with room is met with %s", st.Describe())
	}
}

// An account on the Carnillean side is parked for a day with the reason said, from any stage; it is never walked down the cult's road.
func TestAnAccountOnTheCarnilleanSideIsParkedForADay(t *testing.T) {
	for _, stage := range []int{HzClivetDecision, HzFinishedSide, HzScroll} {
		w := newHzWorld(t, stage, map[int]int{ObjPoison: 1}, nil)
		w.good = true
		why, park := w.run()
		if park != HzParkFor || !strings.Contains(why, "Carnillean side") || len(w.log) != 0 {
			t.Errorf("stage %d: parked %s with %q after %v", stage, park, why, w.log)
		}
	}
}

// The tab and the journal disagreeing, an unread stage or position, an uncoloured tab: each waits and does nothing.
func TestHazeelCultWaitsOnWhatItHasNotRead(t *testing.T) {
	w := newHzWorld(t, HzStarted, nil, nil)
	s := w.snapshot()
	s.StageKnown = false
	if st := DecideHazeelCult(s); st.Kind != StepWait {
		t.Errorf("an unread stage: %s", st.Describe())
	}
	s = w.snapshot()
	s.TileKnown = false
	if st := DecideHazeelCult(s); st.Kind != StepWait {
		t.Errorf("an unread position: %s", st.Describe())
	}
	s = w.snapshot()
	s.Journal = StatusUnknown
	if st := DecideHazeelCult(s); st.Kind != StepWait {
		t.Errorf("an uncoloured tab: %s", st.Describe())
	}
	s = w.snapshot()
	s.Stage = HzNotStarted
	if st := DecideHazeelCult(s); st.Kind != StepWait || !strings.Contains(st.Reason, "disagree") {
		t.Errorf("a yellow tab on a stage-0 journal: %s", st.Describe())
	}
	s = w.snapshot()
	s.Stage = 8
	if st := DecideHazeelCult(s); st.Kind != StepWait || !strings.Contains(st.Reason, "unrecognised") {
		t.Errorf("a stage the module does not know: %s", st.Describe())
	}
	s = w.snapshot()
	s.Journal = StatusComplete
	if st := DecideHazeelCult(s); st.Kind != StepDone {
		t.Errorf("a green tab: %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The module and its places
// ---------------------------------------------------------------------------------------------------------------------

func TestTheHazeelCultModuleIsWhatTheEngineNeeds(t *testing.T) {
	m := HazeelCult
	if m.Record.ID != "hazeelcult" || m.Record.Name != "Hazeel Cult" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if r := m.Record.Requirements; len(r.Skills) != 0 || len(r.Quests) != 0 || r.MinQuestPoints != 0 {
		t.Errorf("the content asks for nothing, the record asks %+v", r)
	}
	if m.Decide == nil || m.ReadStage == nil || !m.NoRetaliate || m.MaxDeaths != 1 || !m.BankSet {
		t.Errorf("module %+v: it fights nothing, parks after one death and reads its stage from the journal", m)
	}
	for _, id := range []int{ObjPoison, ObjHzKey, ObjHzScroll, CoinsID} {
		if !slices.Contains(m.ToolIDs, id) || !slices.Contains(HzKeepIDs(), id) {
			t.Errorf("obj %d is not among the module's tools and the keep list", id)
		}
	}
	// Both hops are in the module and land on the far side of the underground boundary the walk graph has no edge for.
	if len(m.Hops) != 2 || NeedsHop(HzCerilAnchor, HzLanding) != true || NeedsHop(HzLanding, HzStairsTop) != true {
		t.Errorf("hops %+v", m.Hops)
	}
	if h, ok := NearestHopFrom(HzCerilAnchor, m.Hops); !ok || h.LocName != "Cave entrance" {
		t.Errorf("from the mansion the hop is %+v", h)
	}
	if h, ok := NearestHopFrom(HzLanding, m.Hops); !ok || h.LocName != "Stairs" {
		t.Errorf("from the landing the hop is %+v", h)
	}
	// Every step the module can ask for is well formed: talks name a preference and a leash, customs a name.
	for _, st := range []Step{hzTalk(HzCerilStop, "x"), hzTalk(HzClivetStop, "x"), hzDeposit("x"), hzCustom(CustomHzDown, "x")} {
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", st.Describe(), why)
		}
	}
	// The menus: Ceril's two, Clivet's three, in the order they come.
	if !slices.Equal(HzCerilStop.Prefer, []string{"What's wrong?", "Yes, of course"}) {
		t.Errorf("Ceril's preferences %v", HzCerilStop.Prefer)
	}
	if !slices.Equal(HzClivetStop.Prefer, []string{"What do you mean?", "So what would I have to do?", "Ok, count me in."}) {
		t.Errorf("Clivet's preferences %v", HzClivetStop.Prefer)
	}
	// None of the cult's answers is one of the Carnillean side's three refusals, which a substring preference could match.
	for _, refusal := range []string{"I've heard enough of your rubbish.", "You're crazy, I'd never help you.", "No. I won't do it."} {
		for _, p := range HzClivetStop.Prefer {
			if strings.Contains(strings.ToLower(refusal), strings.ToLower(p)) {
				t.Errorf("Clivet's preference %q is part of the refusal %q", p, refusal)
			}
		}
	}
}

func TestTheSewerAndTheAtticAreWhatThePackMakesThem(t *testing.T) {
	for _, c := range []struct {
		what   string
		at     tile.Tile
		sewer  bool
		attic  bool
		island int
	}{
		{"the landing", HzLanding, true, false, 0}, {"Clivet", HzClivetAnchor, true, false, 0}, {"the raft's stand", HzRaftMouthStand, true, false, 0},
		{"the stairs' foot", tile.Tile{X: 2569, Z: 9684}, true, false, 0},
		{"the hideout's raft", HzHideoutStand, true, false, 0}, {"Alomone", HzAlomoneAnchor, true, false, 0},
		{"island 1", tile.Tile{X: 2578, Z: 9687}, true, false, 1}, {"island 2", tile.Tile{X: 2593, Z: 9694}, true, false, 2},
		{"island 3", tile.Tile{X: 2599, Z: 9712}, true, false, 3}, {"island 4", tile.Tile{X: 2616, Z: 9725}, true, false, 4},
		// The kitchen cellar is NOT in the sewer: its ladder joins the mansion's ground floor.
		{"the kitchen's range", HzRangeStand, false, false, 0}, {"the crate's stand", HzCrateStand, false, false, 0},
		{"the forest by the cave", HzCaveStand, false, false, 0}, {"the stairs' top", HzStairsTop, false, false, 0}, {"Ceril", HzCerilAnchor, false, false, 0},
		{"the bedroom's side of the wall", HzWallSouth, false, false, 0}, {"the ladder room", HzWallNorth, false, true, 0},
		{"the attic's chest stand", HzChestStand, false, true, 0}, {"the attic ladder's stand", tile.Tile{X: 2572, Z: 3271, Level: 2}, false, true, 0},
		{"the ground floor under the ladder room", tile.Tile{X: 2572, Z: 3271}, false, false, 0},
	} {
		if got := InHzSewer(c.at); got != c.sewer {
			t.Errorf("%s %v: InHzSewer %v, want %v", c.what, c.at, got, c.sewer)
		}
		if got := InHzAttic(c.at); got != c.attic {
			t.Errorf("%s %v: InHzAttic %v, want %v", c.what, c.at, got, c.attic)
		}
		if got := HzIslandOf(c.at); got != c.island {
			t.Errorf("%s %v: HzIslandOf %d, want %d", c.what, c.at, got, c.island)
		}
	}
	if !InHzLanding(HzLanding) || InHzLanding(HzHideoutStand) || !InHzHideout(HzHideoutStand) || InHzHideout(HzLanding) || !InHzKitchen(HzRangeStand) || InHzKitchen(HzLanding) {
		t.Error("the landing, the hideout and the kitchen are not told apart")
	}
	// The valves: five, in the content's bit order, the third turned the other way.
	if len(HzValves) != 5 {
		t.Fatalf("%d valves", len(HzValves))
	}
	for i, v := range HzValves {
		want := "Turn-right"
		if i == 2 {
			want = "Turn-left"
		}
		if v.Op != want || v.Loc != LocHzValve1+i {
			t.Errorf("valve %d is loc %d turned %q, want loc %d %q", i+1, v.Loc, v.Op, LocHzValve1+i, want)
		}
	}
}
