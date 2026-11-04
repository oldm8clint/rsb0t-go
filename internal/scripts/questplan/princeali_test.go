package questplan

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// ---- the journal ----

// The text prince_journal.rs2 renders for each stage, joined in order (the journal is `~quest_journal(title, $text)` over exactly
// that text, with `|` the client's line break). The stage 20 branch lists five items, each struck through when the pack holds it.
const (
	paJ1       = "@str@Prince Ali has been kidnapped but luckily the spy Leela|@str@has found he is being held near draynor village. I will|"
	paJ2       = "@str@need to disguise the Prince and tie up his captor to free|@str@him from their clutches.|"
	paJGuard   = "@str@I also had to prevent the Guard from seeing what I was up|@str@to, by getting him drunk.|"
	paJTied    = "@str@With the guard disposed of, I used my rope to tie up Lady|@str@Keli in a cupboard, so I could disguise the Prince.|"
	paJSaved   = "@str@I then used a wig, a skirt and some skin paste to make the|@str@prince look like Lady Keli so he could escape to his|@str@freedom with Leela after unlocking his cell door.|"
	paJStage20 = paJ1 + "@str@need to disguise the Prince and tie up his captor to free|@str@him from their clutches. To do this I should:||" +
		"@dbl@Talk to @dre@Leela@dbl@ near @dre@Draynor Village@dbl@ for advice|"
)

var princeJournalFixtures = map[int]string{
	0: "@dbl@I can start this quest by speaking to @dre@Hassan@dbl@ in @dre@Al-Kharid Palace@dbl@.",
	10: "@str@I started this quest by speaking to Hassan in Al-Kharid|@str@Palace. He told me I should speak to Osman the spy master.|" +
		"@dbl@I should go and speak to @dre@Osman@dbl@ for details on the quest.",
	20: paJStage20 + "@dbl@Get a duplicate of the key that is imprisoning the prince|@dbl@Get some rope to tie up the Princes' kidnapper|" +
		"@dbl@Get something to colour the Princes' skin as disguise|@dbl@Get a skirt similar to his kidnapper as disguise|@dbl@Get a Wig to help disguise the prince",
	30:  paJ1 + paJ2 + "@dbl@Before I can free @dre@Prince Ali@dbl@, I need to deal with the @dre@Guard@dbl@.|@dre@Leela@dbl@ suggested I speak with the @dre@Guard@dbl@ to try and determine any weaknesses he might have.|",
	40:  paJ1 + paJ2 + paJGuard + "@dbl@The last thing I need to do is deal with @dre@Lady Keli@dbl@.|@dre@Leela@dbl@ suggested I use some @dre@Rope@dbl@ to tie her up.",
	50:  paJ1 + paJ2 + paJGuard + paJTied + "@dbl@I can now free @dre@Prince Ali@dbl@. I'll need to make sure I give him his disguise when I do.",
	100: paJ1 + paJ2 + paJGuard + paJTied + paJSaved + "@dbl@I should return to @dre@Hassan@dbl@ to claim my reward.",
	110: "@str@I started this quest by speaking to Hassan in Al-Kharid|@str@Palace. He told me I should speak to Osman the spy master.|@str@I should go and speak to Osman for details on the quest.|" +
		paJ1 + paJ2 + paJGuard + paJTied + paJSaved + "@str@I returned to Al-Kharid where Hassan rewarded me for|@str@my work.||@red@QUEST COMPLETE!",
}

// Every stage the content renders is read back as that stage, from the text the server really renders. The later stages contain the
// earlier stages' struck-through phrases, so only the order of the needles keeps stage 100 from reading as stage 50 or 40. The stage
// 20 branch is also read with each of its five items struck through.
func TestParsePrinceAliJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, text := range princeJournalFixtures {
		for _, lines := range [][]string{{text}, strings.Split(text, "|")} {
			got, ok := ParsePrinceAliJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d: read as %d (ok %v) from %q", stage, got, ok, lines)
			}
		}
	}
	if len(princeJournalFixtures) != 8 {
		t.Fatalf("%d rendered stages, the content has eight (0, 10, 20, 30, 40, 50, 100, 110)", len(princeJournalFixtures))
	}
	// Stage 20 with each item in the pack (struck through) is still stage 20.
	text := paJStage20 + "@str@I have a duplicate of the key with me|@str@I have some rope with me|@str@I have some skin paste with me|" +
		"@str@I have a skirt suitable for a disguise with me|@str@I have a wig suitable for a disguise with me"
	if got, ok := ParsePrinceAliJournal([]string{text}); !ok || got != PrinceSpokeOsman {
		t.Errorf("stage 20 with every item held read as %d (ok %v)", got, ok)
	}
	for _, lines := range [][]string{nil, {""}, {"I can start this quest by speaking to the Banker"}, {"@dbl@Something else entirely."}} {
		if got, ok := ParsePrinceAliJournal(lines); ok {
			t.Errorf("%q read as stage %d: an unrecognised journal is unknown, never a stage", lines, got)
		}
	}
}

// The journal fixtures are what prince_journal.rs2 says: every line of every fixture appears in the script, with its colour
// tags and line breaks (RSB0T_CONTENT, skipped without a Content checkout).
func TestPrinceAliJournalFixturesAreTheScriptsText(t *testing.T) {
	src := csContent(t, "scripts/quests/quest_prince/scripts/prince_journal.rs2")
	squash := func(s string) string {
		return regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(s), "")
	}
	script := squash(regexp.MustCompile(`@[a-z0-9]{3}@`).ReplaceAllString(src, ""))
	for stage, text := range princeJournalFixtures {
		for _, seg := range strings.Split(text, "|") {
			seg = regexp.MustCompile(`@[a-z0-9]{3}@`).ReplaceAllString(seg, "")
			if q := squash(seg); q != "" && !strings.Contains(script, q) {
				t.Errorf("stage %d: %q is not in prince_journal.rs2", stage, seg)
			}
		}
	}
}

// ---- the stages and the ids ----

func TestPrinceAliStagesAreWhatTheContentCallsThem(t *testing.T) {
	consts := csContent(t, "scripts/quests/quest_prince/configs/quest_prince.constant")
	general := csContent(t, "scripts/general/configs/quest.constant")
	for name, want := range map[string]int{"prince_not_started": PrinceNotStarted, "prince_started": PrinceStarted, "prince_spoken_osman": PrinceSpokeOsman,
		"prince_prep_finished": PrinceHasEquipment, "prince_guard_drunk": PrinceGuardDrunk, "prince_tied_keli": PrinceTiedKeli, "prince_saved": PrinceSaved} {
		if !strings.Contains(consts, fmt.Sprintf("^%s = %d", name, want)) {
			t.Errorf("^%s = %d is not in quest_prince.constant", name, want)
		}
	}
	if !strings.Contains(general, fmt.Sprintf("^prince_complete = %d", PrinceComplete)) || !strings.Contains(general, fmt.Sprintf("^prince_questpoints = %d", PrinceAliRecord.QuestPoints)) {
		t.Errorf("^prince_complete or ^prince_questpoints is not quest.constant's (%d, %d)", PrinceComplete, PrinceAliRecord.QuestPoints)
	}
}

// ---- a pack to decide over ----

// paSnap is a snapshot of an account at a stage holding inv, standing at the Draynor bank with 50 hitpoints and Defence 40, the
// pantry already read (so the legs speak) unless pantry is false.
func paSnap(stage int, inv map[int]int, pantry bool) Snapshot {
	w := newPAWorld(&testing.T{}, stage, inv, nil, nil)
	if pantry {
		w.ran["provision:"+KitPrincePantry] = true
	}
	return w.snapshot()
}

func TestDecidePrinceAliTheNonItemStates(t *testing.T) {
	coins := map[int]int{CoinsID: 300}
	// The tab is not coloured: wait. Complete: done. Not started: Hassan, once the purse holds the toll.
	if st := DecidePrinceAli(Snapshot{Journal: StatusUnknown}); st.Kind != StepWait {
		t.Errorf("an uncoloured tab: %s", st.Describe())
	}
	if st := DecidePrinceAli(Snapshot{Journal: StatusComplete}); st.Kind != StepDone {
		t.Errorf("a green tab: %s", st.Describe())
	}
	if st := DecidePrinceAli(paSnap(PrinceNotStarted, coins, false)); st.Kind != StepTalk || st.Stop.ID != NpcHassan || st.Stop.Prefer[0] != "Can I help you? You must need some help here in the desert." {
		t.Errorf("not started: %s", st.Describe())
	}
	// In progress with the stage unread is a wait, never stage 0.
	in := paSnap(PrinceStarted, coins, false)
	in.StageKnown = false
	if st := DecidePrinceAli(in); st.Kind != StepWait {
		t.Errorf("an unread journal stage: %s", st.Describe())
	}
	// Stage 10: Osman, with the exit first.
	if st := DecidePrinceAli(paSnap(PrinceStarted, coins, false)); st.Kind != StepTalk || st.Stop.ID != NpcOsman || st.Stop.Prefer[0] != "Okay, I better go find some things." {
		t.Errorf("stage 10: %s", st.Describe())
	}
	// Stage 100 pays at Hassan; stage 110 (the journal text) is done even before the tab turns green.
	if st := DecidePrinceAli(paSnap(PrinceSaved, coins, false)); st.Kind != StepTalk || st.Stop.ID != NpcHassan {
		t.Errorf("stage 100: %s", st.Describe())
	}
	green := paSnap(PrinceComplete, coins, false)
	green.Journal, green.Stage, green.StageKnown = StatusInProgress, PrinceComplete, true
	if st := DecidePrinceAli(green); st.Kind != StepDone {
		t.Errorf("stage 110: %s", st.Describe())
	}
	// Still in the cell at stage 100: out by its door first.
	cell := paSnap(PrinceSaved, coins, false)
	cell.Tile = tile.Tile{X: 3123, Z: 3243}
	if st := DecidePrinceAli(cell); st.Kind != StepCustom || st.Custom != CustomPrinceLeave {
		t.Errorf("stage 100 in the cell: %s", st.Describe())
	}
	// The step that opens the door failed: the walk to Hassan takes the graph's own Prison Door hop.
	cell.Failed = map[string]bool{CustomPrinceLeave: true}
	if st := DecidePrinceAli(cell); st.Kind != StepTalk || st.Stop.ID != NpcHassan {
		t.Errorf("stage 100 in the cell after the door step failed: %s", st.Describe())
	}
}

// The purse comes before any step that walks: the Al-Kharid toll gate is ten coins each way and the walker plans no crossing it
// cannot pay. An empty purse with nothing in the bank or from the mule is a wait with the reason, not a walk.
func TestAPurseBelowTheFloorIsDrawnBeforeAnyWalkAndWaitsWhenNothingGivesIt(t *testing.T) {
	for _, stage := range []int{PrinceNotStarted, PrinceStarted, PrinceSpokeOsman, PrinceHasEquipment, PrinceTiedKeli} {
		s := paSnap(stage, map[int]int{CoinsID: PrinceCoinFloor - 1}, true)
		st := DecidePrinceAli(s)
		if st.Kind != StepProvision || st.KitName != KitPrinceCoins || len(st.Kit) != 1 || st.Kit[0].ID != CoinsID || st.Kit[0].Qty != PrinceCoinFloat {
			t.Errorf("stage %d with %d coins: %s", stage, PrinceCoinFloor-1, st.Describe())
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("stage %d: %s", stage, why)
		}
		s.Failed = map[string]bool{"provision:" + KitPrinceCoins: true}
		if st := DecidePrinceAli(s); st.Kind != StepWait || !strings.Contains(st.Reason, "coins") {
			t.Errorf("stage %d after the coin kit failed: %s", stage, st.Describe())
		}
	}
	// The rescued quest needs no purse: the gate is free from stage 100.
	if st := DecidePrinceAli(paSnap(PrinceSaved, nil, true)); st.Kind != StepTalk {
		t.Errorf("stage 100 with an empty purse: %s", st.Describe())
	}
}

// ---- the whole quest, from several starts ----

// An account that owns nothing but coins does the whole quest. The order the geography wants is in the log: the water and the
// goods of the paste, the beers, the skirt, the wool, the clay and the onions, the ashes, then Aggie's and Ned's work, the
// print, the rope, the key, Leela, Joe, the break-out and Hassan.
func TestAnEmptyAccountDoesTheWholeQuest(t *testing.T) {
	w := newPAWorld(t, PrinceNotStarted, map[int]int{CoinsID: 600}, nil, nil)
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.stage != PrinceComplete {
		t.Fatalf("stage %d at the end (log %v)", w.stage, w.log)
	}
	order := []string{"hassan -> 10", "osman -> 20", "provision:prince-pantry", "provision:prince-water", "provision:prince-flour",
		"provision:prince-berries", "beers 3", "provision:prince-skirt", "provision:prince-shears", "wool", "provision:prince-pickaxe", "clay",
		"onion", "provision:prince-fire", "ashes", "aggie dye", "ned wig", "blond wig", "aggie paste", "soft clay", "keli print", "ropes 2",
		"provision:prince-bar", "osman forges", "leela gives the key", "stage 30", "joe drunk", "keli tied", "prince saved", "hassan -> 110"}
	prev := -1
	for _, want := range order {
		i := w.logged(want)
		if i < 0 {
			t.Fatalf("the log has no %q:\n%s", want, strings.Join(w.log, "\n"))
		}
		if i < prev {
			t.Errorf("%q comes before the step that must precede it:\n%s", want, strings.Join(w.log, "\n"))
		}
		prev = i
	}
	// The shops were used for what only they sell: water, the bar, flour, the skirt and the tinderbox; the ground for the logs and
	// the pickaxe and the berries.
	for _, name := range []string{"Jug of water", "Bronze bar", "Pot of flour", "Pink skirt"} {
		if w.shops[name] == 0 {
			t.Errorf("no %s bought: %v", name, w.shops)
		}
	}
	if w.grounds["Logs"] != 1 {
		t.Errorf("logs picked up: %v", w.grounds)
	}
	// 15 coins a rope, two ropes, then 700 coins at the end: the purse never went under the floor between the draws.
	if w.inv[CoinsID] < 700 {
		t.Errorf("%d coins left of 600 plus 700", w.inv[CoinsID])
	}
	// Nothing is left behind that the quest consumed.
	for _, id := range []int{ObjPrinceKey, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste, ObjKeyPrint, ObjSoftClay, ObjPlainWig, ObjBeer} {
		if w.inv[id] != 0 {
			t.Errorf("obj %d left in the pack: %d", id, w.inv[id])
		}
	}
}

// With the mule's published bank the quest asks it once, for what it holds (the bar, the skirt, the balls of wool, the redberries,
// the beers, the tinderbox and the logs), and does not buy them or farm them.
func TestAMuleFedAccountAsksOnceAndBuysOnlyWhatTheMuleLacks(t *testing.T) {
	w := newPAWorld(t, PrinceNotStarted, map[int]int{CoinsID: 600}, nil, paMule())
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.stage != PrinceComplete {
		t.Fatalf("stage %d at the end", w.stage)
	}
	asks := 0
	for _, l := range w.log {
		if strings.HasPrefix(l, "mule ask") {
			asks++
		}
	}
	if asks != 1 {
		t.Errorf("%d mule asks: %v", asks, w.log)
	}
	for _, name := range []string{"Bronze bar", "Pink skirt"} {
		if w.shops[name] != 0 {
			t.Errorf("%s bought although the mule keeps it: %v", name, w.shops)
		}
	}
	for _, l := range w.log {
		if l == "wool" || l == "provision:prince-shears" || l == "provision:prince-berries" || l == "beers 3" || l == "provision:prince-fire" {
			t.Errorf("%q: the mule's goods were gathered the hard way", l)
		}
	}
	// What the mule does not keep still comes from the world: the water (3 buckets cover the paste and the clay is one more),
	// the flour, the clay, the onions, the rope.
	for _, l := range []string{"clay", "onion", "ropes 2", "aggie dye", "aggie paste"} {
		if w.logged(l) < 0 {
			t.Errorf("the log has no %q: %v", l, w.log)
		}
	}
}

// An account whose trainer banked the disguise and the key between two passes draws it all back and goes straight to the next
// talk; nothing is made twice.
func TestABankThatHoldsTheDisguiseAndTheKeySkipsEverySource(t *testing.T) {
	bank := map[int]int{ObjPrinceKey: 1, ObjBlondWig: 1, ObjPinkSkirt: 1, ObjSkinPaste: 1, ObjRope: 2, ObjBeer: 3, CoinsID: 400}
	w := newPAWorld(t, PrinceSpokeOsman, map[int]int{CoinsID: 150}, bank, nil)
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.stage != PrinceComplete {
		t.Fatalf("stage %d at the end (log %v)", w.stage, w.log)
	}
	for _, absent := range []string{"clay", "onion", "ashes", "wool", "aggie", "ned wig", "keli print", "osman forges", "ropes", "beers"} {
		if w.logged(absent) >= 0 {
			t.Errorf("the log has %q, a step the bank made unnecessary: %v", absent, w.log)
		}
	}
	if len(w.shops) != 0 || len(w.grounds) != 0 {
		t.Errorf("sources used: shops %v grounds %v", w.shops, w.grounds)
	}
}

// Every stage resumes from its own pack: an account that stands at stage s with the pieces s needs finishes the quest, and does
// not redo what an earlier stage did.
func TestEveryStageResumesAndFinishes(t *testing.T) {
	coins := map[int]int{CoinsID: 400}
	for _, c := range []struct {
		name  string
		stage int
		inv   map[int]int
		bank  map[int]int
		not   []string // log lines that must not appear
	}{
		{"10 (Osman next)", PrinceStarted, coins, nil, []string{"hassan -> 10"}},
		{"20 with the disguise and the key", PrinceSpokeOsman, paDisguise(), nil, []string{"clay", "ashes", "aggie", "ned wig", "osman forges"}},
		{"30 with the disguise, the key and the beers", PrinceHasEquipment, paDisguise(ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope), nil, []string{"beers", "ropes", "aggie"}},
		{"30 with the key lost: Leela re-issues it", PrinceHasEquipment, paIDs(coins, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste, ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope), nil, []string{"osman forges", "clay"}},
		{"40 with the rope", PrinceGuardDrunk, paDisguise(ObjRope, ObjRope), nil, []string{"beers", "joe drunk"}},
		{"50 with a rope", PrinceTiedKeli, paDisguise(ObjRope), nil, []string{"keli tied", "joe drunk"}},
		{"100 outside", PrinceSaved, nil, nil, []string{"prince saved"}},
	} {
		w := newPAWorld(t, c.stage, c.inv, c.bank, nil)
		if c.name == "30 with the key lost: Leela re-issues it" {
			w.keystatus = 2
		}
		if why := w.run(); why != "" {
			t.Errorf("%s: waited: %s (log %v)", c.name, why, w.log)
			continue
		}
		if w.stage != PrinceComplete {
			t.Errorf("%s: stage %d at the end (log %v)", c.name, w.stage, w.log)
		}
		for _, not := range c.not {
			if w.logged(not) >= 0 {
				t.Errorf("%s: the log has %q: %v", c.name, not, w.log)
			}
		}
	}
}

// A lost key from stage 30 on is Leela's, 15 coins, and a banked one is withdrawn first: she issues none while the bank holds one.
func TestALostKeyIsLeelasAndABankedOneIsDrawnFirst(t *testing.T) {
	w := newPAWorld(t, PrinceHasEquipment, paIDs(map[int]int{CoinsID: 300}, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste, ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope), nil, nil)
	w.keystatus = 2
	if why := w.run(); why != "" || w.logged("leela reissues the key") < 0 {
		t.Errorf("lost key: waited %q, log %v", why, w.log)
	}
	w = newPAWorld(t, PrinceHasEquipment, paIDs(map[int]int{CoinsID: 300}, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste, ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope), map[int]int{ObjPrinceKey: 1}, nil)
	w.keystatus = 2
	if why := w.run(); why != "" || w.logged("leela reissues the key") >= 0 || w.logged("provision:prince-pantry") < 0 {
		t.Errorf("banked key: waited %q, log %v (the pantry draws it; Leela re-issues none)", why, w.log)
	}
}

// ---- what each stage asks for, and nothing without what it needs ----

func TestPrinceAliNeverTalksOrActsWithoutWhatTheStepNeeds(t *testing.T) {
	// A fixed pseudo-random walk over packs: each item is absent half the time and held in a small count otherwise.
	items := []struct{ id, max int }{{ObjPrinceKey, 1}, {ObjBlondWig, 1}, {ObjPinkSkirt, 1}, {ObjSkinPaste, 1}, {ObjBronzeBar, 1}, {ObjKeyPrint, 1},
		{ObjSoftClay, 1}, {ObjPlainWig, 1}, {ObjYellowDye, 1}, {ObjClay, 1}, {ObjPotOfFlour, 1}, {ObjRedberries, 1}, {ObjAshes, 1}, {ObjLogs, 1},
		{ObjTinderbox, 1}, {ObjOnion, 3}, {ObjBallOfWool, 4}, {ObjJugOfWater, 3}, {ObjBucketOfWater, 2}, {ObjRope, 3}, {ObjBeer, 4},
		{ObjWool, 3}, {ObjShears, 1}, {1267, 1}}
	keep := map[int]bool{}
	for _, id := range princeKeep() {
		keep[id] = true
	}
	rng := uint64(0x9E3779B97F4A7C15)
	next := func(n int) int {
		rng ^= rng << 13
		rng ^= rng >> 7
		rng ^= rng << 17
		return int(rng % uint64(n))
	}
	failable := []string{"provision:" + KitPrinceCoins, "provision:" + KitPrinceWater, "provision:" + KitPrinceBar, "provision:" + KitPrinceFlour,
		"provision:" + KitPrinceBerries, "provision:" + KitPrinceSkirt, "provision:" + KitPrinceShears, "provision:" + KitPrincePickaxe,
		"provision:" + KitPrinceFire, "provision:" + KitPrinceFood, "provision:" + KitPrinceAemad, CustomSheepWool, CustomPrinceKey, CustomPrinceAshes, CustomPrinceClay,
		CustomPrinceRope, CustomPrinceBeerBuy, CustomPrinceBeer, CustomPrinceBreakout, CustomPrincePrint}
	kinds := map[string]int{}
	visits, parks := 0, 0
	for _, stage := range []int{PrinceSpokeOsman, PrinceHasEquipment, PrinceGuardDrunk, PrinceTiedKeli} {
		for i := 0; i < 60000; i++ {
			inv := map[int]int{CoinsID: []int{0, 4, 14, 100, 300, 300, 300}[next(7)]}
			for _, it := range items {
				if next(2) == 0 {
					inv[it.id] = 1 + next(it.max)
				}
			}
			s := paSnap(stage, inv, next(8) != 0)
			// Hitpoints, Defence and food are drawn too: a third of the accounts are weak (Defence 1 to 10, 20 to 40 hitpoints) and a
			// third have other maxima, all of them hurt by up to half; every visit to the compound must be one the odds model accepts.
			switch next(3) {
			case 0:
				s.MaxHP = []int{20, 30, 40}[next(3)]
				s.HP = s.MaxHP - next(s.MaxHP/2)
				s.Levels["defence"] = []int{1, 5, 10}[next(3)]
			case 1:
				s.MaxHP = []int{30, 50, 80}[next(3)]
				s.HP = s.MaxHP - next(s.MaxHP/2)
			}
			for i := next(3); i > 0; i-- {
				s.InvIDs[385]++
			}
			s.Tile = []tile.Tile{DraynorBank, DraynorBank, {X: 2655, Z: 3286}, {X: 3210, Z: 3220}, OsmanAnchor}[next(5)]
			// A kit or a custom step that failed on its last try is a wait, whichever it was.
			if next(5) == 0 {
				for _, name := range failable {
					if next(3) == 0 {
						s.Failed[name] = true
					}
				}
			}
			st := DecidePrinceAli(s)
			if why := ValidateStep(st); why != "" {
				t.Fatalf("stage %d %v: malformed %s: %s", stage, inv, st.Describe(), why)
			}
			h := s.HeldID
			kinds[string(st.Kind)]++
			switch st.Kind {
			case StepDone:
				t.Fatalf("stage %d: done before the quest tab says so", stage)
			case StepTalk:
				switch st.Stop.ID {
				case NpcLeela:
					if stage != PrinceSpokeOsman || h(ObjPrinceKey) == 0 || h(ObjBlondWig) == 0 || h(ObjPinkSkirt) == 0 || h(ObjSkinPaste) == 0 {
						t.Fatalf("stage %d %v: Leela asked without the whole disguise", stage, inv)
					}
				case NpcLadyKeli:
					t.Fatalf("stage %d %v: a plain talk to Lady Keli; the print is priced and topped up as %s", stage, inv, CustomPrincePrint)
				case NpcAggie:
					if st.UntilItemID == ObjYellowDye && (h(ObjOnion) < PrinceOnions || h(ObjYellowDye) > 0 || h(ObjBlondWig) > 0 || h(CoinsID) < PrinceDyeFee) {
						t.Fatalf("stage %d %v: Aggie's yellow dye without two onions and 5 coins, or with a wig or a dye made", stage, inv)
					}
					if st.UntilItemID == ObjSkinPaste && (h(ObjPotOfFlour) == 0 || h(ObjRedberries) == 0 || h(ObjAshes) == 0 || h(ObjJugOfWater)+h(ObjBucketOfWater) == 0 || h(ObjSkinPaste) > 0) {
						t.Fatalf("stage %d %v: Aggie's paste without the four goods, or with paste made", stage, inv)
					}
					if st.UntilItemID != ObjYellowDye && st.UntilItemID != ObjSkinPaste {
						t.Fatalf("stage %d %v: Aggie driven until %d", stage, inv, st.UntilItemID)
					}
				case NpcNed:
					if st.UntilItemID != ObjPlainWig || h(ObjBallOfWool) < PrinceBalls || h(ObjPlainWig) > 0 || h(ObjBlondWig) > 0 {
						t.Fatalf("stage %d %v: Ned's wig without three balls, or with a wig made", stage, inv)
					}
				default:
					t.Fatalf("stage %d %v: talk to %d", stage, inv, st.Stop.ID)
				}
			case StepUseOn:
				switch st.TargetID {
				case ObjPlainWig:
					if st.ItemID != ObjYellowDye || h(ObjYellowDye) == 0 || h(ObjPlainWig) == 0 || h(ObjBlondWig) > 0 {
						t.Fatalf("stage %d %v: the dye on a wig it has not", stage, inv)
					}
				case ObjClay:
					if h(st.ItemID) == 0 || (st.ItemID != ObjJugOfWater && st.ItemID != ObjBucketOfWater) || h(ObjClay) == 0 || h(ObjSoftClay) > 0 || h(ObjKeyPrint) > 0 || h(ObjPrinceKey) > 0 || stage != PrinceSpokeOsman {
						t.Fatalf("stage %d %v: water on clay that is not wanted or not held", stage, inv)
					}
				default:
					t.Fatalf("stage %d %v: use-on %s", stage, inv, st.Describe())
				}
			case StepPickLoc:
				if st.LocID != LocOnionPlant || h(ObjOnion) >= PrinceOnions || h(ObjYellowDye) > 0 || h(ObjBlondWig) > 0 {
					t.Fatalf("stage %d %v: onions picked that are not wanted", stage, inv)
				}
			case StepCustom:
				// Every visit to the compound is one the model accepts: a refused account was sent for food or parked instead.
				if ticks, visit := map[string]int{CustomPrincePrint: PrinceKeliPrintVisitTicks, CustomPrinceBeer: PrinceBeerVisitTicks,
					CustomPrinceBreakout: PrinceBreakoutVisitTicks}[st.Custom]; visit {
					visits++
					if ok, why := PrinceVisitOdds(s, ticks); !ok {
						t.Fatalf("stage %d %v: %s for an account the model refuses: %s", stage, inv, st.Custom, why)
					}
				}
				switch st.Custom {
				case CustomPrincePrint:
					if stage != PrinceSpokeOsman || h(ObjSoftClay) == 0 || h(ObjKeyPrint) > 0 || h(ObjPrinceKey) > 0 {
						t.Fatalf("stage %d %v: Keli asked for the print without soft clay, or with a print or a key made", stage, inv)
					}
				case CustomPrinceBreakout:
					if (stage != PrinceGuardDrunk && stage != PrinceTiedKeli) || h(ObjRope) == 0 || h(ObjPrinceKey) == 0 || h(ObjBlondWig) == 0 || h(ObjPinkSkirt) == 0 || h(ObjSkinPaste) == 0 {
						t.Fatalf("stage %d %v: the break-out without rope, key or disguise", stage, inv)
					}
					if st.Qty != b2i(stage == PrinceTiedKeli) {
						t.Fatalf("stage %d: break-out flag %d", stage, st.Qty)
					}
				case CustomPrinceBeer:
					if stage != PrinceHasEquipment || h(ObjBeer) < PrinceBeers {
						t.Fatalf("stage %d %v: Joe without three beers", stage, inv)
					}
				case CustomPrinceKey:
					forging := st.Qty > 0
					if forging && (h(ObjKeyPrint) == 0 || h(ObjBronzeBar) == 0 || stage != PrinceSpokeOsman) {
						t.Fatalf("stage %d %v: Osman asked to forge without a print and a bar", stage, inv)
					}
					if !forging && stage < PrinceHasEquipment {
						t.Fatalf("stage %d %v: Leela asked to re-issue a key that was never issued", stage, inv)
					}
					if !forging && h(CoinsID) < PrinceKeyFee {
						t.Fatalf("stage %d %v: a new key without its 15 coins", stage, inv)
					}
					if h(ObjPrinceKey) > 0 {
						t.Fatalf("stage %d %v: a key step with the key in the pack", stage, inv)
					}
				case CustomPrinceClay:
					if h(ObjClay) > 0 || h(ObjSoftClay) > 0 || h(ObjKeyPrint) > 0 || h(ObjPrinceKey) > 0 || stage != PrinceSpokeOsman {
						t.Fatalf("stage %d %v: clay mined that is not needed", stage, inv)
					}
					if h(ObjBronzePickaxe)+h(1267) == 0 {
						t.Fatalf("stage %d %v: clay mined without a pickaxe", stage, inv)
					}
				case CustomPrinceAshes:
					if h(ObjLogs) == 0 || h(ObjTinderbox) == 0 || h(ObjAshes) > 0 || h(ObjSkinPaste) > 0 {
						t.Fatalf("stage %d %v: a fire without logs and a tinderbox, or for ashes not wanted", stage, inv)
					}
				case CustomPrinceRope:
					if h(ObjRope) >= princeRopesWanted(stage) || h(CoinsID) < PrinceRopeFee {
						t.Fatalf("stage %d %v: rope bought with enough, or without 15 coins", stage, inv)
					}
				case CustomPrinceBeerBuy:
					if stage >= PrinceGuardDrunk || h(ObjBeer) >= PrinceBeers || h(CoinsID) < 2*(PrinceBeers-h(ObjBeer)) {
						t.Fatalf("stage %d %v: beer bought that is not wanted or not affordable", stage, inv)
					}
				case CustomSheepWool:
					if h(ObjBallOfWool) >= PrinceBalls || st.Qty != PrinceBalls-h(ObjBallOfWool) || h(ObjPlainWig) > 0 || h(ObjBlondWig) > 0 {
						t.Fatalf("stage %d %v: wool for a wig that is not wanted (qty %d)", stage, inv, st.Qty)
					}
					if h(ObjShears) == 0 && h(ObjWool) < st.Qty {
						t.Fatalf("stage %d %v: shearing without shears", stage, inv)
					}
				default:
					t.Fatalf("stage %d %v: custom %s", stage, inv, st.Custom)
				}
			case StepProvision:
				// A kit of the quest names only items the quest keeps.
				for _, l := range st.Kit {
					if !keep[l.ID] {
						t.Fatalf("stage %d: kit %s names obj %d, which a bank visit of the quest would deposit", stage, st.KitName, l.ID)
					}
				}
			case StepWait:
				if st.Reason == "" {
					t.Fatalf("stage %d %v: a wait with no reason", stage, inv)
				}
				if st.ParkFor == PrinceOddsPark {
					parks++
				}
				// The module's own dead end: every leg satisfied and a piece still short. No pack may reach it.
				if strings.Contains(st.Reason, "every leg is satisfied") {
					t.Fatalf("stage %d %v: the legs deadlock: %s", stage, inv, st.Reason)
				}
			case StepDeposit:
				// Room for the pack's own work: under three free slots, or fewer than the wool and the shears take.
				if s.FreeSlots >= 3 && !(st.Reason == "room for the wool" && s.FreeSlots < PrinceBalls+1) {
					t.Fatalf("stage %d %v: a deposit with %d free slots (%s)", stage, inv, s.FreeSlots, st.Reason)
				}
			}
		}
	}
	for _, k := range []string{"talk", "useOn", "pickLoc", "custom", "provision", "wait", "deposit"} {
		if kinds[k] == 0 {
			t.Errorf("the sweep never reached a %s step: %v", k, kinds)
		}
	}
	// It reached the compound for accounts the model accepts and the park for those it refuses.
	if visits == 0 || parks == 0 {
		t.Errorf("the sweep made %d visits to the compound and %d odds parks", visits, parks)
	}
}

// ---- East Ardougne's shelf ----

// The mule waits in East Ardougne, so the pantry's ask leaves the account there, and Aemad's Adventurers' Store is a few steps away:
// one pass over his shelf buys the three balls of wool (instead of the Lumbridge pen and wheel), the rope, a tinderbox and a pickaxe
// the tour would otherwise cross the map for. It runs once, only for an account nearer his shop than Ned's.
func TestAnAccountInEastArdougneShopsAtAemadsFirst(t *testing.T) {
	ardougne := tile.Tile{X: 2655, Z: 3286}
	mk := func(at tile.Tile, stage int, inv map[int]int) Snapshot {
		s := paSnap(stage, inv, true)
		s.Tile = at
		return s
	}
	st := DecidePrinceAli(mk(ardougne, PrinceSpokeOsman, map[int]int{CoinsID: 300}))
	if st.Kind != StepProvision || st.KitName != KitPrinceAemad {
		t.Fatalf("an empty pack in East Ardougne: %s", st.Describe())
	}
	if why := ValidateStep(st); why != "" {
		t.Fatal(why)
	}
	got := map[int]KitLine{}
	for _, l := range st.Kit {
		got[l.ID] = l
		if !l.Optional || len(l.Sources) != 1 || l.Sources[0].Kind != SourceShop || l.Sources[0].Shop.NpcID != AemadStore.NpcID {
			t.Errorf("line %+v: optional, one source, Aemad's shelf", l)
		}
	}
	for id, qty := range map[int]int{ObjBallOfWool: PrinceBalls, ObjRope: PrinceRopesBeforeTie, ObjTinderbox: 1, ObjBronzePickaxe: 1} {
		if got[id].Qty != qty {
			t.Errorf("line for obj %d: %+v, want %d", id, got[id], qty)
		}
	}
	if len(got) != 4 {
		t.Errorf("%d lines, want the four goods the shelf holds", len(got))
	}
	// Only what the account still lacks: a wig made, the paste made, ropes held and a pickaxe worn leave nothing to buy.
	full := map[int]int{CoinsID: 300, ObjBlondWig: 1, ObjSkinPaste: 1, ObjRope: 2, ObjPrinceKey: 1, ObjPinkSkirt: 1, ObjBeer: 3}
	if st := DecidePrinceAli(mk(ardougne, PrinceSpokeOsman, full)); st.KitName == KitPrinceAemad {
		t.Errorf("a full pack still shops at Aemad's: %v", st.Kit)
	}
	// Two balls short, a tinderbox and a pickaxe held: balls and rope.
	part := mk(ardougne, PrinceSpokeOsman, map[int]int{CoinsID: 300, ObjBallOfWool: 1, ObjTinderbox: 1})
	part.WornIDs[1265] = 1
	st = DecidePrinceAli(part)
	got = map[int]KitLine{}
	for _, l := range st.Kit {
		got[l.ID] = l
	}
	if st.KitName != KitPrinceAemad || got[ObjBallOfWool].Qty != PrinceBalls-1 || got[ObjRope].Qty != PrinceRopesBeforeTie || len(got) != 2 {
		t.Errorf("a partly stocked pack: %s %+v", st.Describe(), st.Kit)
	}
	// Not for an account that is not nearer his shop: Draynor, Lumbridge, Al-Kharid.
	for _, at := range []tile.Tile{DraynorBank, {X: 3210, Z: 3220}, OsmanAnchor} {
		if st := DecidePrinceAli(mk(at, PrinceSpokeOsman, map[int]int{CoinsID: 300})); st.KitName == KitPrinceAemad {
			t.Errorf("an account at %v shops at Aemad's", at)
		}
	}
	// Once: after the pass has run (or failed) the legs below speak.
	ran := mk(ardougne, PrinceSpokeOsman, map[int]int{CoinsID: 300})
	ran.Ran["provision:"+KitPrinceAemad] = true
	if st := DecidePrinceAli(ran); st.KitName == KitPrinceAemad {
		t.Error("the Aemad pass repeats")
	}
	failed := mk(ardougne, PrinceSpokeOsman, map[int]int{CoinsID: 300})
	failed.Failed["provision:"+KitPrinceAemad] = true
	if st := DecidePrinceAli(failed); st.KitName == KitPrinceAemad {
		t.Error("the Aemad pass repeats after failing")
	}
	// The shop is a real shop: Aemad of the Adventurers' Store, East Ardougne.
	if AemadStore.NpcID != 590 || AemadStore.Npc != "Aemad" {
		t.Errorf("Aemad is %+v", AemadStore)
	}
}

// The whole quest from East Ardougne: Aemad's shelf gives the wool, the rope, the tinderbox and the pickaxe, so there is no shearing,
// no rope bought from Ned and no pickaxe fetched, and the rest of the tour is the same.
func TestAnAccountThatStartsInEastArdougneBuysItsWoolRopeTinderboxAndPickaxeThere(t *testing.T) {
	w := newPAWorld(t, PrinceStarted, map[int]int{CoinsID: 600}, nil, nil)
	w.tile = tile.Tile{X: 2655, Z: 3286}
	if why := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.stage != PrinceComplete {
		t.Fatalf("stage %d at the end (log %v)", w.stage, w.log)
	}
	for name, n := range map[string]int{"Ball of wool": PrinceBalls, "Rope": PrinceRopesBeforeTie, "Tinderbox": 1, "Bronze pickaxe": 1} {
		if w.shops[name] != n {
			t.Errorf("%d %s bought at the shop, want %d: %v", w.shops[name], name, n, w.shops)
		}
	}
	for _, absent := range []string{"wool", "ropes", "provision:prince-shears", "provision:prince-pickaxe"} {
		if w.logged(absent) >= 0 {
			t.Errorf("the log has %q, a step Aemad's shelf made unnecessary: %v", absent, w.log)
		}
	}
	if a, p := w.logged("provision:prince-aemad"), w.logged("provision:prince-water"); a < 0 || p < 0 || a > p {
		t.Errorf("Aemad's shelf (%d) is not ahead of the Shantay water (%d): %v", a, p, w.log)
	}
}

// ---- the pantry ----

// The pantry reads the bank and the mule and never walks to a source: every line is optional and has none, and the quest items
// are untradeable (the mule is never asked for them).
func TestThePrinceAliPantryReadsTheBankAndNeverWalksToASource(t *testing.T) {
	st := DecidePrinceAli(paSnap(PrinceSpokeOsman, map[int]int{CoinsID: 300}, false))
	if st.Kind != StepProvision || st.KitName != KitPrincePantry {
		t.Fatalf("the first step of stage 20: %s", st.Describe())
	}
	if why := ValidateStep(st); why != "" {
		t.Fatal(why)
	}
	got := map[int]KitLine{}
	for _, l := range st.Kit {
		got[l.ID] = l
		if !l.Optional || len(l.Sources) != 0 {
			t.Errorf("pantry line %+v: every line is optional and sourceless", l)
		}
	}
	for _, id := range []int{ObjPrinceKey, ObjKeyPrint, ObjBlondWig, ObjPlainWig, ObjSkinPaste} {
		if l, ok := got[id]; !ok || !l.Untradeable {
			t.Errorf("pantry line for the quest item %d: %+v (want present and untradeable)", id, l)
		}
	}
	for _, id := range []int{ObjBronzeBar, ObjPinkSkirt, ObjRope, ObjBeer, ObjBallOfWool, ObjPotOfFlour, ObjRedberries, ObjAshes, ObjLogs,
		ObjTinderbox, ObjOnion, ObjClay, ObjSoftClay, ObjBucketOfWater, ObjJugOfWater, ObjYellowDye} {
		if l, ok := got[id]; !ok || l.Untradeable {
			t.Errorf("pantry line for %d: %+v (want present and tradeable)", id, l)
		}
	}
	if l := got[ObjRope]; l.Qty != PrinceRopesBeforeTie {
		t.Errorf("rope line %+v: two before Keli is tied", l)
	}
	if l := got[ObjBeer]; l.Qty != PrinceBeers {
		t.Errorf("beer line %+v", l)
	}
	if l := got[ObjBallOfWool]; l.Qty != PrinceBalls {
		t.Errorf("ball line %+v", l)
	}
	if l := got[ObjOnion]; l.Qty != PrinceOnions {
		t.Errorf("onion line %+v", l)
	}
	if l := got[ObjBucketOfWater]; l.Qty != 2 {
		t.Errorf("water wanted for the clay and the paste: %+v", l)
	}
	// An account at stage 50 with its disguise needs a rope, and no beer: Joe is behind it.
	s := paSnap(PrinceTiedKeli, paDisguise(), false)
	st = DecidePrinceAli(s)
	if st.Kind != StepProvision || st.KitName != KitPrincePantry {
		t.Fatalf("stage 50: %s", st.Describe())
	}
	for _, l := range st.Kit {
		if l.ID == ObjBeer {
			t.Errorf("a beer line at stage 50: %+v", l)
		}
		if l.ID == ObjRope && l.Qty != PrinceRopesAfterTie {
			t.Errorf("rope line at stage 50: %+v", l)
		}
	}
	// Holding everything, there is nothing to ask: the pantry is skipped.
	full := paDisguise(ObjRope, ObjRope, ObjBeer, ObjBeer, ObjBeer)
	if st := DecidePrinceAli(paSnap(PrinceHasEquipment, full, false)); st.Kind == StepProvision && st.KitName == KitPrincePantry {
		t.Errorf("a full pack still reads the pantry: %v", st.Kit)
	}
}

// ---- the compound ----

func TestPrinceVisitOddsAndTheParkInsteadOfTheWalk(t *testing.T) {
	mk := func(maxHP, hp, def int, food ...int) Snapshot {
		inv := paDisguise(ObjRope, ObjRope, ObjBeer, ObjBeer, ObjBeer)
		for _, f := range food {
			inv[f]++
		}
		s := paSnap(PrinceHasEquipment, inv, true)
		s.MaxHP, s.HP = maxHP, hp
		s.Levels["defence"] = def
		return s
	}
	// A level-40 account at full hitpoints walks in for the beers and for the break-out with no food.
	if ok, why := PrinceVisitOdds(mk(50, 50, 40), PrinceBreakoutVisitTicks); !ok {
		t.Errorf("a Defence 40 account with 50 hitpoints is refused: %s", why)
	}
	// An account at Defence 1 and 30 hitpoints with nothing to eat is not: two guards cost it more than it has.
	if ok, why := PrinceVisitOdds(mk(30, 30, 1), PrinceBreakoutVisitTicks); ok || !strings.Contains(why, "jail guards") {
		t.Errorf("a Defence 1 account with 30 hitpoints and no food is accepted (%v %q)", ok, why)
	}
	// Food it cannot eat inside a chat does not save it: the account is full already, so four sharks add nothing to the hitpoints it
	// goes in with, and it stays refused after the food kit, whatever it carries.
	if ok, why := PrinceVisitOdds(mk(30, 30, 1, 385, 385, 385, 385), PrinceBreakoutVisitTicks); ok || !strings.Contains(why, "inside a chat") {
		t.Errorf("a full Defence 1 account with four sharks and 30 hitpoints is accepted (%v %q)", ok, why)
	}
	// The food does its work before the visit, as the top-up to the fight's starting hitpoints (80 percent of the maximum): an
	// account that is hurt is refused without it and goes in with it, and no further.
	if ok, _ := PrinceVisitOdds(mk(50, 20, 40), PrinceBreakoutVisitTicks); ok {
		t.Error("a Defence 40 account at 20 of 50 hitpoints with nothing to eat is accepted")
	}
	if ok, why := PrinceVisitOdds(mk(50, 20, 40, 385, 385), PrinceBreakoutVisitTicks); !ok {
		t.Errorf("topped up to 40 of 50 from two sharks it is still refused: %s", why)
	}
	// Food beyond the top-up is not credited: a hurt Defence 1 account with a whole pack of sharks starts at 80 percent and no more.
	if ok, _ := PrinceVisitOdds(mk(30, 20, 1, 385, 385, 385, 385, 385, 385), PrinceBeerVisitTicks); ok {
		t.Error("a Defence 1 account with 30 hitpoints is accepted with a pack of sharks: the food was credited during the chat")
	}
	// A short visit costs less than a long one.
	short, _ := PrinceVisitOdds(mk(60, 60, 10), PrinceBeerVisitTicks)
	long, _ := PrinceVisitOdds(mk(60, 60, 10), PrinceBreakoutVisitTicks*3)
	if !short || long {
		t.Errorf("a Defence 10 account: the short visit %v, the very long %v", short, long)
	}
	// An unread hitpoints stat is nothing to judge.
	if ok, _ := PrinceVisitOdds(mk(0, 0, 1), PrinceBreakoutVisitTicks); !ok {
		t.Error("unread hitpoints are judged")
	}
	// The decision: a refused visit asks for food once, then parks for hours instead of walking in.
	weak := mk(30, 30, 1)
	st := DecidePrinceAli(weak)
	if st.Kind != StepProvision || st.KitName != KitPrinceFood {
		t.Fatalf("a refused beer visit: %s", st.Describe())
	}
	if why := ValidateStep(st); why != "" {
		t.Fatal(why)
	}
	weak.Ran["provision:"+KitPrinceFood] = true
	st = DecidePrinceAli(weak)
	if st.Kind != StepWait || st.ParkFor != PrinceOddsPark || !strings.Contains(st.Reason, "jail compound") {
		t.Fatalf("a refused beer visit after the food: %s (park %s)", st.Describe(), st.ParkFor)
	}
	// Sharks do not change the verdict for an account that is full: the food kit was fetched and it still parks.
	fed := mk(30, 30, 1, 385, 385, 385, 385)
	fed.Ran["provision:"+KitPrinceFood] = true
	if st := DecidePrinceAli(fed); st.Kind != StepWait || st.ParkFor != PrinceOddsPark {
		t.Fatalf("the fed weak account at stage 30: %s (park %s)", st.Describe(), st.ParkFor)
	}
	// A sound account goes in, and the break-out stages are priced the same way.
	strong := mk(50, 50, 40)
	if st := DecidePrinceAli(strong); st.Kind != StepCustom || st.Custom != CustomPrinceBeer {
		t.Fatalf("the sound account at stage 30: %s", st.Describe())
	}
	b := mk(30, 30, 1)
	b.Stage = PrinceGuardDrunk
	if st := DecidePrinceAli(b); st.Kind != StepProvision || st.KitName != KitPrinceFood {
		t.Fatalf("a refused break-out visit: %s", st.Describe())
	}
	// A failed custom step is not repeated at once.
	ok := mk(50, 50, 40)
	ok.Failed = map[string]bool{CustomPrinceBeer: true}
	if st := DecidePrinceAli(ok); st.Kind != StepWait {
		t.Errorf("a failed Joe visit: %s", st.Describe())
	}
	// Prince Ali's foe is the jail guard of content: attack 19, strength 23, defence 21, 32 hitpoints, attackrate 5, hits up to 3.
	if FoeJailGuard.MaxHit() != 3 || FoeJailGuard.PeakHit() != 3 || FoeJailGuard.AttackTicks != 5 {
		t.Errorf("the jail guard hits up to %d every %d ticks", FoeJailGuard.MaxHit(), FoeJailGuard.AttackTicks)
	}
}

// The print at Lady Keli is the first visit to the compound of every run, eighteen pages and five menus under the guard that stands
// four tiles from her: it is priced and topped up like the beers and the break-out, never walked into as a plain talk.
func TestTheKeliPrintIsPricedAndToppedUpLikeTheOtherCompoundVisits(t *testing.T) {
	base := paIDs(map[int]int{CoinsID: 300}, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste, ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope, ObjSoftClay)
	mk := func(maxHP, hp, def int, food ...int) Snapshot {
		inv := paIDs(base)
		for _, f := range food {
			inv[f]++
		}
		s := paSnap(PrinceSpokeOsman, inv, true)
		s.MaxHP, s.HP = maxHP, hp
		s.Levels["defence"] = def
		return s
	}
	// A sound account takes the print through the custom step, which tops it up first.
	st := DecidePrinceAli(mk(50, 50, 40))
	if st.Kind != StepCustom || st.Custom != CustomPrincePrint {
		t.Fatalf("a Defence 40 account with soft clay: %s", st.Describe())
	}
	if why := ValidateStep(st); why != "" {
		t.Fatal(why)
	}
	// A weak one is refused: food once, then the park, and never a talk to Keli.
	weak := mk(30, 30, 1)
	st = DecidePrinceAli(weak)
	if st.Kind != StepProvision || st.KitName != KitPrinceFood {
		t.Fatalf("a Defence 1 account with 30 hitpoints at the print: %s", st.Describe())
	}
	weak.Ran["provision:"+KitPrinceFood] = true
	if st = DecidePrinceAli(weak); st.Kind != StepWait || st.ParkFor != PrinceOddsPark || !strings.Contains(st.Reason, "jail compound") {
		t.Fatalf("a refused print after the food kit: %s (park %s)", st.Describe(), st.ParkFor)
	}
	// Food it carries into the chat does not buy a walk in: it is full, so the top-up has nothing to do.
	full := mk(30, 30, 1, 385, 385, 385, 385)
	full.Ran["provision:"+KitPrinceFood] = true
	if st = DecidePrinceAli(full); st.Kind != StepWait || st.ParkFor != PrinceOddsPark {
		t.Fatalf("a full weak account with sharks at the print: %s", st.Describe())
	}
	// A hurt account that food tops up to the starting level goes in.
	if st = DecidePrinceAli(mk(50, 20, 40, 385, 385)); st.Kind != StepCustom || st.Custom != CustomPrincePrint {
		t.Fatalf("a hurt Defence 40 account with two sharks: %s", st.Describe())
	}
	if st = DecidePrinceAli(mk(50, 20, 40)); st.Kind != StepProvision || st.KitName != KitPrinceFood {
		t.Fatalf("a hurt Defence 40 account with nothing to eat: %s", st.Describe())
	}
	// A print step that failed on its last try is a wait with the reason, not a repeat.
	failed := mk(50, 50, 40)
	failed.Failed[CustomPrincePrint] = true
	if st = DecidePrinceAli(failed); st.Kind != StepWait || !strings.Contains(st.Reason, CustomPrincePrint) {
		t.Fatalf("a failed print step: %s", st.Describe())
	}
	// The three visits are priced from the length of their chats: the print's is the longest.
	if PrinceKeliPrintVisitTicks < PrinceBeerVisitTicks || PrinceKeliPrintVisitTicks < PrinceBreakoutVisitTicks {
		t.Errorf("the print (%d ticks) is priced under the beers (%d) or the break-out (%d)", PrinceKeliPrintVisitTicks, PrinceBeerVisitTicks, PrinceBreakoutVisitTicks)
	}
}

// ---- the legs ----

func TestTheLegsNameTheirSourcesAndFailToWaitsNotLoops(t *testing.T) {
	coins := map[int]int{CoinsID: 300}
	step := func(inv map[int]int, mut func(*Snapshot)) Step {
		s := paSnap(PrinceSpokeOsman, inv, true)
		if mut != nil {
			mut(&s)
		}
		return DecidePrinceAli(s)
	}
	// Water first: two for the clay and the paste, from Shantay.
	st := step(coins, nil)
	if st.Kind != StepProvision || st.KitName != KitPrinceWater || st.Kit[0].Qty != 2 || st.Kit[0].Sources[0].Shop.NpcID != NpcShantay {
		t.Fatalf("an empty pack at stage 20: %s %+v", st.Describe(), st.Kit)
	}
	// A bucket of water counts for one: one jug more.
	st = step(paIDs(coins, ObjBucketOfWater), nil)
	if st.KitName != KitPrinceWater || st.Kit[0].Qty != 1 {
		t.Errorf("one bucket held: %s %+v", st.Describe(), st.Kit)
	}
	// Water held: the flour from Wydin, then the berries.
	water := paIDs(coins, ObjJugOfWater, ObjJugOfWater)
	st = step(water, nil)
	if st.KitName != KitPrinceFlour || st.Kit[0].Sources[0].Shop.NpcID != NpcWydin {
		t.Errorf("flour: %s", st.Describe())
	}
	// Berries: from the Draynor bank Wydin's shelf is nearer than the patch by Varrock, so it is the only source listed; from
	// Varrock the ground spawns come first (the item rule's third rung) and the shelf is the last resort.
	st = step(paIDs(water, ObjPotOfFlour), nil)
	if st.KitName != KitPrinceBerries || len(st.Kit[0].Sources) != 1 || st.Kit[0].Sources[0].Kind != SourceShop || st.Kit[0].Sources[0].Shop.NpcID != NpcWydin {
		t.Fatalf("berries from Draynor: %s %+v", st.Describe(), st.Kit)
	}
	far := step(paIDs(water, ObjPotOfFlour), func(s *Snapshot) { s.Tile = VarrockEastBank })
	if far.KitName != KitPrinceBerries || len(far.Kit[0].Sources) != len(RedberrySpawns)+1 ||
		far.Kit[0].Sources[0].Kind != SourceGround || far.Kit[0].Sources[len(far.Kit[0].Sources)-1].Kind != SourceShop {
		t.Fatalf("berries from Varrock: %s %+v", far.Describe(), far.Kit)
	}
	// A kit that failed is a wait that names what is missing.
	st = step(coins, func(s *Snapshot) { s.Failed = map[string]bool{"provision:" + KitPrinceWater: true} })
	if st.Kind != StepWait || !strings.Contains(st.Reason, "water") {
		t.Errorf("water failed: %s", st.Describe())
	}
	// Beers: without the bank or the mule's, the bartender sells one a talk at 2 coins; too poor is a wait.
	base := paIDs(paIDs(water, ObjPotOfFlour, ObjRedberries))
	st = step(base, nil)
	if st.Kind != StepCustom || st.Custom != CustomPrinceBeerBuy || st.Stop.ID != NpcRustyAnchorBartender || st.Qty != PrinceBeers || st.ItemID != ObjBeer {
		t.Fatalf("beers: %s", st.Describe())
	}
	poor := map[int]int{ObjJugOfWater: 2, ObjPotOfFlour: 1, ObjRedberries: 1, CoinsID: PrinceCoinFloor}
	if st := step(poor, func(s *Snapshot) { s.InvIDs[CoinsID] = PrinceCoinFloor }); st.Kind != StepCustom {
		t.Errorf("the floor purse buys the beers: %s", st.Describe())
	}
	// The wool needs shears first, then the Sheep Shearer module's executor with the balls it lacks.
	beers := paIDs(base, ObjBeer, ObjBeer, ObjBeer, ObjPinkSkirt)
	st = step(beers, nil)
	if st.KitName != KitPrinceShears {
		t.Fatalf("wool without shears: %s", st.Describe())
	}
	st = step(paIDs(beers, ObjShears), nil)
	if st.Kind != StepCustom || st.Custom != CustomSheepWool || st.Qty != PrinceBalls {
		t.Fatalf("wool: %s", st.Describe())
	}
	st = step(paIDs(beers, ObjShears, ObjBallOfWool), nil)
	if st.Custom != CustomSheepWool || st.Qty != PrinceBalls-1 {
		t.Errorf("one ball held: %s qty %d", st.Describe(), st.Qty)
	}
	// Then the clay: a pickaxe first, from the ground or Bob's.
	wool := paIDs(beers, ObjShears, ObjBallOfWool, ObjBallOfWool, ObjBallOfWool)
	st = step(wool, nil)
	if st.KitName != KitPrincePickaxe || st.Kit[0].Sources[0].Kind != SourceGround || st.Kit[0].Sources[1].Shop.NpcID != BobsAxes.NpcID {
		t.Fatalf("clay without a pickaxe: %s %+v", st.Describe(), st.Kit)
	}
	pick := paIDs(wool, 1267) // an iron pickaxe is as good as the bronze one
	st = step(pick, nil)
	if st.Custom != CustomPrinceClay {
		t.Fatalf("clay with a pickaxe: %s", st.Describe())
	}
	// A worn pickaxe counts.
	st = step(wool, func(s *Snapshot) { s.WornIDs[1265] = 1 })
	if st.Custom != CustomPrinceClay {
		t.Errorf("clay with a worn pickaxe: %s", st.Describe())
	}
}

// The ashes leg asks for logs and a tinderbox first (the logs from the ground by Draynor's market, the tinderbox from a shop),
// then burns them; and the Draynor bank is never the Al-Kharid one from the west.
func TestTheAshesLegAndTheBank(t *testing.T) {
	// Everything but the paste's ashes, wig and key is in hand.
	base := map[int]int{CoinsID: 300, ObjJugOfWater: 1, ObjPotOfFlour: 1, ObjRedberries: 1, ObjBeer: 3, ObjPinkSkirt: 1, ObjBallOfWool: 3, ObjSoftClay: 1, ObjOnion: 2}
	st := DecidePrinceAli(paSnap(PrinceSpokeOsman, base, true))
	if st.KitName != KitPrinceFire || len(st.Kit) != 2 {
		t.Fatalf("ashes without logs: %s %+v", st.Describe(), st.Kit)
	}
	logs := st.Kit[0]
	if logs.ID != ObjLogs || len(logs.Sources) != 2 || logs.Sources[0].Kind != SourceGround || logs.Sources[0].Tile != PrinceLogSpawns[0] {
		t.Errorf("logs line %+v", logs)
	}
	if tb := st.Kit[1]; tb.ID != ObjTinderbox || tb.Sources[0].Kind != SourceShop {
		t.Errorf("tinderbox line %+v", tb)
	}
	st = DecidePrinceAli(paSnap(PrinceSpokeOsman, paIDs(base, ObjLogs), true))
	if st.KitName != KitPrinceFire || len(st.Kit) != 1 || st.Kit[0].ID != ObjTinderbox {
		t.Errorf("ashes with logs and no tinderbox: %s %+v", st.Describe(), st.Kit)
	}
	st = DecidePrinceAli(paSnap(PrinceSpokeOsman, paIDs(base, ObjLogs, ObjTinderbox), true))
	if st.Kind != StepCustom || st.Custom != CustomPrinceAshes {
		t.Errorf("ashes with both: %s", st.Describe())
	}
	// Banks: from the west of the toll gate Al-Kharid's bank is not used; inside it is.
	if b := princeBank(Snapshot{TileKnown: true, Tile: tile.Tile{X: 3210, Z: 3220}}); b != DraynorBank {
		t.Errorf("a Lumbridge account banks at %v", b)
	}
	if b := princeBank(Snapshot{TileKnown: true, Tile: OsmanAnchor}); b != (tile.Tile{X: 3269, Z: 3167}) {
		t.Errorf("an account at Osman banks at %v", b)
	}
	if b := princeBank(Snapshot{}); b != DraynorBank {
		t.Errorf("an unplaced account banks at %v", b)
	}
}

// Every kit the quest builds is well formed, and every item it names is one the quest keeps through a bank visit.
func TestEveryPrinceAliKitIsWellFormedAndKept(t *testing.T) {
	keep := map[int]bool{}
	for _, id := range princeKeep() {
		keep[id] = true
	}
	f := princeFacts{stage: PrinceSpokeOsman, needPrint: true, needSoft: true, needClay: true, waterWanted: 2, balls: 0, onions: 0}
	s := Snapshot{TileKnown: true, Tile: DraynorBank}
	kits := map[string][]KitLine{
		KitPrinceCoins: princeCoinsKit(), KitPrincePantry: princePantryKit(f), KitPrinceWater: princeWaterKit(2), KitPrinceBar: princeBarKit(),
		KitPrinceFlour: princeFlourKit(), KitPrinceBerries: princeBerriesKit(s), KitPrinceSkirt: princeSkirtKit(), KitPrinceShears: princeShearsKit(),
		KitPrinceAemad:   princeAemadKit(s, f),
		KitPrincePickaxe: princePickaxeKit(), KitPrinceFire: princeFireKit(f), KitPrinceFood: princeFoodKit(),
	}
	for name, kit := range kits {
		if len(kit) == 0 {
			t.Errorf("kit %s is empty", name)
		}
		for _, l := range kit {
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("kit %s: %s", name, why)
			}
			if !keep[l.ID] && name != KitPrinceFood {
				t.Errorf("kit %s names obj %d, which a bank visit would deposit", name, l.ID)
			}
		}
	}
	for _, id := range princeFoodIDs {
		if !keep[id] {
			t.Errorf("food %d is not kept", id)
		}
	}
	for _, l := range princeFoodKit() {
		found := false
		for _, id := range princeFoodIDs {
			found = found || id == l.ID
		}
		if !found {
			t.Errorf("the food kit asks for %d, which the sustain does not eat", l.ID)
		}
	}
	// Every module tool is in the keep list; the module's tools include the pickaxes mining clay accepts.
	for _, id := range PrinceAli.ToolIDs {
		if !keep[id] {
			t.Errorf("tool %d is not kept", id)
		}
	}
}

// The module's record, its slot and its stage reader are the quest's.
func TestPrinceAliModule(t *testing.T) {
	m := PrinceAli
	if m.Record.ID != "prince" || m.Record.Name != "Prince Ali Rescue" || m.Record.QuestPoints != 3 || m.MaxDeaths != 1 || !m.NoRetaliate {
		t.Errorf("module %+v", m.Record)
	}
	if m.ReadStage == nil || m.Decide == nil || m.Sustain.EatAt <= 0 || len(m.Sustain.FoodIDs) == 0 {
		t.Error("the module lacks its stage reader, decision or sustain")
	}
	f := LadderFacts[PrinceAliID]
	if !f.Filler || f.QP != 3 || len(f.Gates) != 0 || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.EstMinutes <= 0 || f.Region == "" || f.Source == "" {
		t.Errorf("ladder facts %+v: a filler gives its quest points and gates on nothing", f)
	}
	if QuestTabName(PrinceAliID) != PrinceAliName {
		t.Errorf("tab name %q", QuestTabName(PrinceAliID))
	}
	if len(ModuleFiles[PrinceAliID]) == 0 {
		t.Error("no ModuleFiles entry")
	}
	// 60 minutes and the quarter.
	if got := QuestSlot(PrinceAliID, 20*time.Minute); got != 75*time.Minute {
		t.Errorf("slot %s", got)
	}
	if holds := LadderHolds(PrinceAliRecord, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
		t.Errorf("held by %+v for an account with no levels", holds)
	}
}
