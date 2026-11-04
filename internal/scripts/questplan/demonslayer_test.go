package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// ---- snapshots ----

var (
	dsVarrock = t3(3185, 3436, 0) // the Varrock west bank
	dsPalace  = t3(3204, 3473, 0) // beside Sir Prysin
	dsSewer   = t3(3237, 9859, 0) // the manhole's landing
	dsTower   = t3(3112, 3162, 1) // Traiborn's room
)

// dsSnap is a Demon Slayer snapshot: the stage, where the account stands, what it holds and wears, a mid-level account (combat 49:
// the circle's wizards skip it, and its swing fells Delrith in under twenty ticks).
func dsSnap(stage int, at tile.Tile, held map[int]int, worn ...int) Snapshot {
	s := Snapshot{
		Journal: StatusInProgress, Stage: stage, StageKnown: true, Tile: at, TileKnown: true,
		InvIDs: map[int]int{}, WornIDs: map[int]int{}, Inv: map[string]int{}, Worn: map[string]int{}, FreeSlots: 20,
		HP: 45, MaxHP: 45, QuestPoints: 20, Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{},
		Levels: map[string]int{"attack": 40, "strength": 40, "defence": 30, "hitpoints": 45, "prayer": 31, "magic": 55, "ranged": 1},
	}
	for id, n := range held {
		s.InvIDs[id] = n
	}
	for _, id := range worn {
		s.WornIDs[id] = 1
	}
	if stage == DemonNotStarted {
		s.Journal, s.StageKnown = StatusNotStarted, false
	}
	return s
}

func dsWith(s Snapshot, f func(*Snapshot)) Snapshot { f(&s); return s }

// dsKeysRan marks the run's one read of the bank for banked keys and the sword as done.
func dsKeysRan(s Snapshot) Snapshot { s.Ran["provision:"+KitDemonKeys] = true; return s }

// dsDecide is DecideDemonSlayer through the module.
func dsDecide(s Snapshot) Step { return DemonSlayer.Decide(s) }

var (
	dsAllKeys = map[int]int{ObjKeyTraiborn: 1, ObjKeyRovin: 1, ObjKeyDrain: 1}
	dsFood    = map[int]int{ObjShark: 4}
)

func dsHeld(parts ...map[int]int) map[int]int {
	out := map[int]int{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

// ---- the module's surface ----

func TestDemonSlayerRecordAndModule(t *testing.T) {
	r := DemonSlayerRecord
	if r.ID != "demon" || r.Name != "Demon Slayer" || r.QuestPoints != 3 || r.Requirements.MinQuestPoints != 0 ||
		len(r.Requirements.Skills) != 1 || r.Requirements.Skills[0].Skill != "hitpoints" || r.Requirements.Skills[0].Level != DemonMinHitpoints {
		t.Errorf("record %+v: want demon, the tab's spelling, 3 quest points, the hitpoints floor and nothing else", r)
	}
	if DemonSlayerID != DemonSlayer.Record.ID || QuestTabName(DemonSlayerID) != DemonSlayerName {
		t.Errorf("the ladder facts name it %q, the tab %q", QuestTabName(DemonSlayerID), DemonSlayerName)
	}
	m := DemonSlayer
	if m.MaxDeaths != 2 || !m.NoRetaliate || m.ReadStage == nil || m.Decide == nil || m.ExclusiveDone != nil {
		t.Errorf("module %+v: two deaths park it, it never fights back, it reads the stage and holds its slot for the run", m)
	}
	for _, step := range []string{CustomDemonDelrith, CustomDemonDrain, CustomDemonSewer} {
		c, ok := m.StepDeathCaps[step]
		if !ok || c.Max != 1 || c.Window != 24*time.Hour || c.Skip != 24*time.Hour {
			t.Errorf("the death cap on %s is %+v: one death parks the quest for a day", step, c)
		}
	}
	if !slices.Equal(m.Sustain.FoodIDs, DemonFoods) || m.Sustain.EatAt != 15 {
		t.Errorf("sustain %+v", m.Sustain)
	}
	// Every bite the sustain eats has a heal the fight model knows.
	for _, f := range DemonFoods {
		if FoodBiteHeal(f) == 0 {
			t.Errorf("food %d is not in the food table", f)
		}
	}
	// The tools are the quest's items and the coins, and never food (the floor bite must be free to eat it) or bones (an account
	// that gathered 25 and lost its place must not carry them through every errand after).
	for _, id := range []int{CoinsID, ObjKeyTraiborn, ObjKeyRovin, ObjKeyDrain, ObjSilverlight} {
		if !slices.Contains(m.ToolIDs, id) {
			t.Errorf("tool %d is not kept by the module", id)
		}
		if !slices.Contains(DemonKeepIDs(), id) || !slices.Contains(DemonBonesKeepIDs(), id) {
			t.Errorf("tool %d is not on the keep lists", id)
		}
	}
	// The bucket and its water are kept by the steps that use them and let go of by the bones trip's bare pack, which has room for 25
	// bones, the coins and two keys and no more.
	for _, id := range []int{ObjBucket, ObjBucketOfWater} {
		if slices.Contains(m.ToolIDs, id) || !slices.Contains(DemonKeepIDs(), id) || slices.Contains(DemonBonesKeepIDs(), id) {
			t.Errorf("the bucket item %d: a module tool %v, ordinary keep %v, bones-trip keep %v", id, slices.Contains(m.ToolIDs, id),
				slices.Contains(DemonKeepIDs(), id), slices.Contains(DemonBonesKeepIDs(), id))
		}
	}
	for _, id := range append([]int{ObjBones}, DemonFoods...) {
		if slices.Contains(m.ToolIDs, id) {
			t.Errorf("%d is a module tool", id)
		}
		if id != ObjBones && !slices.Contains(DemonKeepIDs(), id) {
			t.Errorf("food %d is not on the keep list of a bank visit", id)
		}
		if slices.Contains(DemonBonesKeepIDs(), id) && id != ObjBones {
			t.Errorf("food %d is on the bones trip's keep list", id)
		}
	}
	if slices.Contains(DemonKeepIDs(), ObjBones) {
		t.Error("bones are on the ordinary keep list")
	}
	if n := len(DemonBonesKeepIDs()); n != 6 {
		t.Errorf("the bones trip keeps %d item types", n)
	}
}

func TestDemonSlayerFactsRow(t *testing.T) {
	f, ok := LadderFacts[DemonSlayerID]
	if !ok {
		t.Fatal("no ladder row")
	}
	if f.ID != DemonSlayerID || f.Name != DemonSlayerName || f.QP != 3 || !f.Filler || f.QPNeeded != 0 || f.Finale || len(f.Gates) != 0 ||
		len(f.Prereqs) != 0 || f.TrainSkill != "" || f.MinCombat != DemonMinCombat || f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
		t.Errorf("row %+v", f)
	}
	// Its slot: 45 minutes and a quarter, which is under the 90 minutes a slot holder is dropped after (questrunner.slotStale).
	if got := QuestSlot(DemonSlayerID, 20*time.Minute); got != 56*time.Minute+15*time.Second {
		t.Errorf("slot %s", got)
	}
	if _, ok := ModuleFiles[DemonSlayerID]; !ok {
		t.Error("no ModuleFiles entry")
	}
	// It sits after the Clock Tower and ahead of Black Knights' Fortress, which wants the points it makes, and the finale. The
	// fillers merged in the same wave sit between it and Black Knights' Fortress (the one place that spells the whole tail is
	// ownerFillerTail), so only its place among its elders and the finale is pinned here.
	n := len(OwnerLadder)
	demon, cog, bk := slices.Index(OwnerLadder, DemonSlayerID), slices.Index(OwnerLadder, ClockTowerID), slices.Index(OwnerLadder, BlackKnightsFortressID)
	if n < 6 || OwnerLadder[n-1] != "dragon" || OwnerLadder[n-2] != BlackKnightsFortressID || cog < 0 || demon <= cog || demon >= bk ||
		!slices.Contains(OwnerLadder[:demon], RomeoJulietID) {
		t.Fatalf("the ladder ends %v, want ... cog, demon, ..., blackknight, dragon", OwnerLadder[max(0, n-8):])
	}
	// The unproven tail's quest points are what the finale wants: a filler adds to them.
	if el := Evaluate(DemonSlayerRecord, PlayerState{SkillLevels: map[string]int{"hitpoints": 19}}, BankSnapshot{}, StatusNotStarted); el.Status != EligBlocked {
		t.Errorf("19 hitpoints: %+v", el)
	}
	if el := Evaluate(DemonSlayerRecord, PlayerState{SkillLevels: map[string]int{"hitpoints": 20}}, BankSnapshot{}, StatusNotStarted); el.Status != EligReady {
		t.Errorf("20 hitpoints: %+v", el)
	}
	if got := QuestTabName(DemonSlayerID); got != "Demon Slayer" {
		t.Errorf("tab name %q", got)
	}
}

// ---- journal ----

// The text demon_journal.rs2 renders for each page, joined in order (the journal is `~quest_journal(title, $text)` over exactly that
// text, with `|` the client's line break). The key-hunt page names the keys the pack holds, one line each.
const (
	dsJGypsy = "@str@I spoke to the Gypsy in Varrock Square who saw my future.|@str@Unfortunately it involved killing a demon who nearly|@str@destroyed Varrock over 150 years ago.||"
	dsJNeed  = "@dbl@To defeat the @dre@demon@dbl@ I need the magical sword of @dre@Silverlight@dbl@.|"
)

var demonJournalFixtures = map[string]struct {
	stage int
	text  string
}{
	"not started":    {DemonNotStarted, "@dbl@I can start this quest by speaking to the @dre@Gypsy@dbl@ in the @dre@tent|@dbl@in @dre@Varrock's main square||I must be able to defeat an apocalyptic demon!"},
	"talked to Aris": {DemonTalkedAris, dsJGypsy + dsJNeed + "I should ask @dre@Sir Prysin@dbl@ in @dre@Varrock Palace@dbl@ where it is."},
	"key hunt, no key": {DemonKeyHunt, dsJGypsy + dsJNeed + "@dre@Sir Prysin@dbl@ needs @dre@3 keys@dbl@ before he can give me @dre@Silverlight@dbl@.||" +
		"@dbl@The @dre@1st Key@dbl@ was dropped down the @dre@palace kitchen drains@dbl@.|Maybe some water can dislodge it.|" +
		"@dbl@The @dre@2nd Key@dbl@ is with Captain Rovin in Varrock Palace.|@dbl@The @dre@3rd Key@dbl@ is with Wizard Traiborn at the Wizards' Tower,|south of Draynor Village.|"},
	"key hunt, one key": {DemonKeyHunt, dsJGypsy + dsJNeed + "@dre@Sir Prysin@dbl@ needs @dre@3 keys@dbl@ before he can give me @dre@Silverlight@dbl@.||" +
		"@str@I have the 1st Key with me.|@dbl@The @dre@2nd Key@dbl@ is with Captain Rovin in Varrock Palace.|@dbl@The @dre@3rd Key@dbl@ is with Wizard Traiborn at the Wizards' Tower,|south of Draynor Village.|"},
	"key hunt, all keys": {DemonKeyHunt, dsJGypsy + dsJNeed + "Now I have @dre@all 3 keys@dbl@ I should go and speak to @dre@Sir|Prysin@dbl@ and @dbl@collect the magical sword @dre@Silverlight@dbl@ from him.||"},
	"silverlight": {DemonSilverlight, dsJGypsy + "@str@I reclaimed the magical sword Silverlight from Sir Prysin.||" +
		"@dbl@Now I should go to the stone circle south of the city and|destroy @dre@Delrith@dbl@ using @dre@Silverlight@dbl@!"},
	"complete": {DemonComplete, "@str@I spoke to the Gypsy in Varrock Square who saw my future.|@str@Unfortunately it involved killing a demon who nearly|@str@destroyed Varrock over 150 years ago.||@str@I reclaimed the magical sword Silverlight from Sir Prysin.|" +
		"@str@Using its power I managed to destroy the demon Delrith|@str@like the great hero Wally did many years before.||@red@QUEST COMPLETE!"},
}

// Every page the content renders is read back as its stage, from the text the server really renders, whole and split at its line
// breaks. The later pages repeat the earlier pages' struck-through lines, so only the order of the needles keeps the sword's page from
// reading as the key hunt.
func TestParseDemonSlayerJournalReadsEveryRenderedPage(t *testing.T) {
	for name, f := range demonJournalFixtures {
		for _, lines := range [][]string{{f.text}, strings.Split(f.text, "|")} {
			got, ok := ParseDemonSlayerJournal(lines)
			if !ok || got != f.stage {
				t.Errorf("%s: read as %d (ok %v) from %q", name, got, ok, lines)
			}
		}
	}
	for _, lines := range [][]string{nil, {""}, {"I can start this quest by speaking to the Banker"}, {"@dbl@Something else entirely."}} {
		if got, ok := ParseDemonSlayerJournal(lines); ok {
			t.Errorf("%q read as stage %d: an unrecognised journal is unknown, never a stage", lines, got)
		}
	}
}

// ---- the conversations ----

// dsMenus plays the menus of a conversation through a stop's preferences, in order, and returns what was chosen; it fails the test
// on a menu with no preferred option, as the dialogue driver would stop.
func dsMenus(t *testing.T, stop NpcStop, menus ...[]string) []string {
	t.Helper()
	var picked []string
	for i, m := range menus {
		p, ok := PickPreferred(m, stop.Prefer)
		if !ok {
			t.Errorf("%s: menu %d %q has no preferred option in %q", stop.Npc, i+1, m, stop.Prefer)
		}
		picked = append(picked, p)
	}
	return picked
}

// The menus of each conversation, in the order the scripts reach them (the content test pins every one to a p_choice call).
var (
	dsGypsyMenus = [][]string{
		{"Ok, here you go.", "Who are you calling young one?!", "No, I don't believe in that stuff."},
		{"Very interesting. What does that Aaargh bit mean?", "Are you alright?", "Aaargh?"},
		{"Who's Delrith?", "Get a grip!"},
		{"How am I meant to fight a demon who can destroy cities?", "Okay, where is he? I'll kill him for you!", "Wally doesn't sound like a very heroic name."},
		{"What is the magical incantation?", "Where can I find Silverlight?"},
		{"Okay, thanks. I'll do my best to stop the demon.", "What is the magical incantation?"},
	}
	dsPrysinMenus = [][]string{
		{"I am a mighty adventurer. Who are you?", "I'm not sure, I was hoping you could tell me.", "Gypsy Aris said I should come and talk to you."},
		{"I need to find Silverlight.", "Yes, she is still alive."},
		{"Well, the gypsy's crystal ball seems to think otherwise.", "He's back and unfortunately I've got to deal with him."},
		{"So give me the keys!", "And why is this a problem?"},
		{"Can you give me your key?", "Where can I find Captain Rovin?", "Where does the wizard live?"},
		{"Can you give me your key?", "Where can I find Captain Rovin?", "Well I'd better go key hunting."},
	}
	dsLostSwordMenu = []string{"No, not yet.", "No, I'm afraid I've lost Silverlight."}
	dsRovinMenus    = [][]string{
		{"I am one of the palace guards.", "What about the King?", "Yes I know, but this is important."},
		{"There's a demon who wants to invade this city.", "Erm I forgot.", "The castle has just received its ale delivery."},
	}
	dsTraibornMenus = [][]string{
		{"What's a thingummywut?", "Teach me to be a mighty and powerful wizard.", "I need to get a key given to you by Sir Prysin."},
		{"He told me you were looking after it for him.", "He's one of the King's knights.", "Well, have you got any keys knocking around?"},
		{" Hmm, that's too bad. I really need that key.", "I'll get the bones for you."},
	}
	dsChantMenu = []string{"Carlem Gabindo Purchai Zaree Camerinthum", "Purchai Zaree Gabindo Carlem Camerinthum", "Purchai Camerinthum Aber Gabindo Carlem", "Carlem Aber Camerinthum Purchai Gabindo"}
)

func TestTheStopsAnswerEveryMenuOfTheirConversations(t *testing.T) {
	// Gypsy Aris at stage 0 (demon_slayer_not_started_aris to the closing pair).
	got := dsMenus(t, GypsyArisStop, dsGypsyMenus...)
	want := []string{"Ok, here you go.", "Very interesting. What does that Aaargh bit mean?", "Who's Delrith?", "Okay, where is he? I'll kill him for you!",
		"Where can I find Silverlight?", "Okay, thanks. I'll do my best to stop the demon."}
	if !slices.Equal(got, want) {
		t.Errorf("Gypsy: %q, want %q", got, want)
	}

	// Sir Prysin at stage 1: the options only that stage offers, then the keys; every later menu ends in "key hunting".
	got = dsMenus(t, PrysinIntroStop, dsPrysinMenus...)
	want = []string{"Gypsy Aris said I should come and talk to you.", "I need to find Silverlight.", "He's back and unfortunately I've got to deal with him.",
		"So give me the keys!", "Where does the wizard live?", "Well I'd better go key hunting."}
	if !slices.Equal(got, want) {
		t.Errorf("Prysin: %q, want %q", got, want)
	}
	// At stage 0 the first menu lacks Aris's line, and the stop must not guess among the two it offers.
	if p, ok := PickPreferred(dsPrysinMenus[0][:2], PrysinIntroStop.Prefer); ok {
		t.Errorf("stage 0's menu was answered with %q: Sir Prysin gives the keys only to an account Aris has sent", p)
	}

	// The lost sword (stage 29) is asked for by the one line that says so; "not yet" is the answer of an account that still has it.
	got = dsMenus(t, PrysinLostSwordStop, dsLostSwordMenu)
	if got[0] != "No, I'm afraid I've lost Silverlight." {
		t.Errorf("lost sword: %q", got)
	}

	// Captain Rovin: the third option of the first menu (offered at stages 2 to 28 only), the demon in the second.
	got = dsMenus(t, RovinStop, dsRovinMenus...)
	if got[0] != "Yes I know, but this is important." || got[1] != "There's a demon who wants to invade this city." {
		t.Errorf("Rovin: %q", got)
	}
	if p, ok := PickPreferred(dsRovinMenus[0][:2], RovinStop.Prefer); ok {
		t.Errorf("Rovin's menu without the third option (stage 1 or 29) was answered with %q", p)
	}

	// Traiborn: the key through the two menus to the bones; his bones talk has no menu, and the menu of an account past the key (stage 28
	// and on) offers no key line, which the stop must refuse rather than start the bones again.
	got = dsMenus(t, TraibornStop, dsTraibornMenus...)
	want = []string{"I need to get a key given to you by Sir Prysin.", "Well, have you got any keys knocking around?", "I'll get the bones for you."}
	if !slices.Equal(got, want) {
		t.Errorf("Traiborn: %q, want %q", got, want)
	}
	if p, ok := PickPreferred(dsTraibornMenus[0][:2], TraibornStop.Prefer); ok {
		t.Errorf("Traiborn's menu past the key was answered with %q", p)
	}
	// The refusals are never preferred: "Hmm, that's too bad" gives the quest up.
	for _, s := range []NpcStop{GypsyArisStop, PrysinIntroStop, RovinStop, TraibornStop} {
		for _, p := range s.Prefer {
			if strings.Contains(strings.ToLower(p), "too bad") || strings.Contains(strings.ToLower(p), "i don't believe") {
				t.Errorf("%s prefers a refusal: %q", s.Npc, p)
			}
		}
	}
}

// Delrith's chant: only the fourth line is chosen, whole, and not one of the three wrong ones contains it or is contained by it.
func TestTheIncantationPicksTheOneRightLine(t *testing.T) {
	menu := dsChantMenu
	p, ok := PickPreferred(menu, []string{DemonIncantation})
	if !ok || p != menu[3] {
		t.Errorf("picked %q (ok %v), want the fourth line", p, ok)
	}
	for i, wrong := range menu[:3] {
		if strings.Contains(strings.ToLower(wrong), strings.ToLower(DemonIncantation)) {
			t.Errorf("wrong line %d contains the incantation", i+1)
		}
	}
	// The TS def's shorter text is a substring of the right line and of none of the wrong ones.
	const ts = "Aber Camerinthum Purchai Gabindo"
	for i, line := range menu {
		if has := strings.Contains(line, ts); has != (i == 3) {
			t.Errorf("line %d contains the TS def's text: %v", i+1, has)
		}
	}
}

// ---- decide ----

func TestDecideDemonSlayerNonItemStates(t *testing.T) {
	coins := map[int]int{CoinsID: 40}
	cases := []struct {
		name string
		s    Snapshot
		kind StepKind
		want func(Step) bool
	}{
		{"complete", dsWith(dsSnap(0, dsVarrock, nil), func(s *Snapshot) { s.Journal = StatusComplete }), StepDone, nil},
		{"tab not coloured", dsWith(dsSnap(0, dsVarrock, nil), func(s *Snapshot) { s.Journal = StatusUnknown }), StepWait,
			func(st Step) bool { return st.ParkFor == 0 }},
		{"not started, a coin: the Gypsy", dsSnap(0, dsVarrock, coins), StepTalk,
			func(st Step) bool { return st.Stop.ID == NpcGypsyAris && st.Stop.Anchor == GypsyAnchor }},
		{"not started, no coin: the purse first", dsSnap(0, dsVarrock, nil), StepProvision,
			func(st Step) bool {
				return st.KitName == KitDemonCoins && st.BankSet && len(st.Kit) == 1 && st.Kit[0].ID == CoinsID && st.Kit[0].Qty == DemonCoinFloat && !st.Bare
			}},
		{"not started, the purse failed: parks for hours", dsWith(dsSnap(0, dsVarrock, nil), func(s *Snapshot) { s.Failed["provision:"+KitDemonCoins] = true }), StepWait,
			func(st Step) bool { return st.ParkFor == DemonCoinRetry && strings.Contains(st.Reason, "coin") }},
		{"not started, 19 hitpoints: parks for the hitpoints before a coin is spent",
			dsWith(dsSnap(0, dsVarrock, coins), func(s *Snapshot) { s.MaxHP, s.Levels["hitpoints"] = 19, 19 }), StepWait,
			func(st Step) bool { return st.ParkFor == DemonFitRetry && strings.Contains(st.Reason, "19 hitpoints") }},
		{"not started, hitpoints unread: goes on", dsWith(dsSnap(0, dsVarrock, coins), func(s *Snapshot) { s.MaxHP = 0 }), StepTalk, nil},
		{"stage read as 0 on a yellow tab: the same", dsWith(dsSnap(0, dsVarrock, coins), func(s *Snapshot) { s.Journal, s.Stage, s.StageKnown = StatusInProgress, 0, true }), StepTalk,
			func(st Step) bool { return st.Stop.ID == NpcGypsyAris }},
		{"stage unread: waits", dsWith(dsSnap(2, dsVarrock, nil), func(s *Snapshot) { s.StageKnown = false }), StepWait,
			func(st Step) bool { return st.ParkFor == 0 && strings.Contains(st.Reason, "journal stage") }},
		{"stage 1: Sir Prysin's first talk", dsSnap(1, dsPalace, nil), StepTalk,
			func(st Step) bool {
				return st.Stop.ID == NpcSirPrysin && st.Stop.Anchor == PrysinAnchor && st.UntilItemID == 0
			}},
		{"an unrecognised stage waits", dsSnap(7, dsVarrock, nil), StepWait, func(st Step) bool { return st.ParkFor == 0 }},
	}
	for _, c := range cases {
		st := dsDecide(c.s)
		if st.Kind != c.kind || (c.want != nil && !c.want(st)) {
			t.Errorf("%s: %s %+v", c.name, st.Describe(), st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
	}
}

func TestDecideDemonSlayerKeyHunt(t *testing.T) {
	bones := map[int]int{ObjBones: DemonBonesNeeded}
	cases := []struct {
		name string
		s    Snapshot
		kind StepKind
		want func(Step) bool
	}{
		// The first thing of the run is the bank: a key or the sword the trainer banked is drawn before any is asked for twice.
		{"the bank first", dsSnap(2, dsPalace, nil), StepProvision, func(st Step) bool {
			return st.KitName == KitDemonKeys && st.BankSet && !st.Bare && len(st.Kit) == 4 && slices.Equal(st.KeepIDs, DemonKeepIDs())
		}},
		{"the bank read failed: goes on to the keys", dsWith(dsSnap(2, dsPalace, nil), func(s *Snapshot) { s.Failed["provision:"+KitDemonKeys] = true }), StepProvision,
			func(st Step) bool { return st.KitName == KitDemonBones }},

		// Traiborn's key first: the bones, then the tower.
		{"no key, no bones: the bones, in a bare pack", dsKeysRan(dsSnap(2, dsPalace, nil)), StepProvision, func(st Step) bool {
			return st.KitName == KitDemonBones && st.Bare && len(st.Kit) == 1 && st.Kit[0].ID == ObjBones && st.Kit[0].Qty == DemonBonesNeeded &&
				slices.Equal(st.KeepIDs, DemonBonesKeepIDs()) && !slices.Contains(st.KeepIDs, ObjShark)
		}},
		{"24 bones: one more set to find", dsKeysRan(dsSnap(2, dsPalace, map[int]int{ObjBones: 24})), StepProvision,
			func(st Step) bool { return st.KitName == KitDemonBones }},
		{"the bones kit failed: parks for the bones", dsWith(dsKeysRan(dsSnap(2, dsPalace, nil)), func(s *Snapshot) { s.Failed["provision:"+KitDemonBones] = true }), StepWait,
			func(st Step) bool { return st.ParkFor == DemonBonesRetry && strings.Contains(st.Reason, "25 bones") }},
		{"25 bones: the tower", dsKeysRan(dsSnap(2, dsPalace, bones)), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonTraiborn }},
		{"in the tower with the bones: the same", dsKeysRan(dsSnap(2, dsTower, bones)), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonTraiborn }},
		{"the tower failed: parks for hours", dsWith(dsKeysRan(dsSnap(2, dsTower, bones)), func(s *Snapshot) { s.Failed[CustomDemonTraiborn] = true }), StepWait,
			func(st Step) bool { return st.ParkFor == DemonTraibornRetry && strings.Contains(st.Reason, "Traiborn") }},
		{"the tower failed beats the bones kit", dsWith(dsKeysRan(dsSnap(2, dsTower, nil)), func(s *Snapshot) { s.Failed[CustomDemonTraiborn] = true }), StepWait,
			func(st Step) bool { return st.ParkFor == DemonTraibornRetry }},

		// Captain Rovin's, with the bones key in hand.
		{"Traiborn's key held: Rovin", dsKeysRan(dsSnap(2, dsTower, map[int]int{ObjKeyTraiborn: 1})), StepTalk, func(st Step) bool {
			return st.Stop.ID == NpcCaptainRovin && st.Stop.Anchor == RovinAnchor && st.UntilItemID == ObjKeyRovin
		}},
		{"Traiborn's key held, a full pack: room first", dsWith(dsKeysRan(dsSnap(2, dsPalace, map[int]int{ObjKeyTraiborn: 1})), func(s *Snapshot) { s.FreeSlots = 1 }), StepDeposit,
			func(st Step) bool { return slices.Equal(st.KeepIDs, DemonKeepIDs()) && st.BankSet && !st.Bare }},

		// The drain key.
		{"two keys, nothing for the drain: the food, once", dsKeysRan(dsSnap(2, dsPalace, map[int]int{ObjKeyTraiborn: 1, ObjKeyRovin: 1})), StepProvision,
			func(st Step) bool {
				return st.KitName == KitDemonFoodSewer && len(st.Kit) == 2 && st.Kit[0].Optional && st.Kit[1].Optional
			}},
	}
	keysTwo := map[int]int{ObjKeyTraiborn: 1, ObjKeyRovin: 1}
	drained := func(s Snapshot) Snapshot { s.Ran["provision:"+KitDemonFoodSewer] = true; return dsKeysRan(s) }
	cases = append(cases, []struct {
		name string
		s    Snapshot
		kind StepKind
		want func(Step) bool
	}{
		{"two keys: a bucket for the drain", drained(dsSnap(2, dsPalace, keysTwo)), StepProvision, func(st Step) bool {
			return st.KitName == KitDemonBucket && st.BankSet && len(st.Kit) == 2 && st.Kit[0].ID == ObjBucketOfWater && st.Kit[0].Optional &&
				st.Kit[1].ID == ObjBucket && st.Kit[1].OnlyIfShort == ObjBucketOfWater && slices.Equal(st.KeepIDs, DemonKeepIDs())
		}},
		{"two keys, the bucket kit failed: parks for the bucket", dsWith(drained(dsSnap(2, dsPalace, keysTwo)), func(s *Snapshot) { s.Failed["provision:"+KitDemonBucket] = true }),
			StepWait, func(st Step) bool { return st.ParkFor == DemonBucketRetry && strings.Contains(st.Reason, "bucket") }},
		{"an empty bucket: the sink", drained(dsSnap(2, dsPalace, dsHeld(keysTwo, map[int]int{ObjBucket: 1}))), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonWater }},
		{"an empty bucket, the sink failed: parks", dsWith(drained(dsSnap(2, dsPalace, dsHeld(keysTwo, map[int]int{ObjBucket: 1}))), func(s *Snapshot) { s.Failed[CustomDemonWater] = true }),
			StepWait, func(st Step) bool { return st.ParkFor == DemonBucketRetry && strings.Contains(st.Reason, "sink") }},
		{"a bucket of water: the pour and the sewer", drained(dsSnap(2, dsPalace, dsHeld(keysTwo, map[int]int{ObjBucketOfWater: 1}))), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonDrain && strings.Contains(st.Reason, "drain") }},
		{"a bucket of water, one slot: room for the key first", dsWith(drained(dsSnap(2, dsPalace, dsHeld(keysTwo, map[int]int{ObjBucketOfWater: 1}))), func(s *Snapshot) { s.FreeSlots = 1 }),
			StepDeposit, func(st Step) bool {
				return slices.Contains(st.KeepIDs, ObjBucketOfWater) && slices.Contains(st.KeepIDs, ObjKeyTraiborn)
			}},
		{"the pour failed: parks for hours", dsWith(drained(dsSnap(2, dsPalace, dsHeld(keysTwo, map[int]int{ObjBucket: 1}))), func(s *Snapshot) { s.Failed[CustomDemonDrain] = true }),
			StepWait, func(st Step) bool { return st.ParkFor == DemonDrainRetry && strings.Contains(st.Reason, "drain key") }},
		{"all three keys: Sir Prysin, until the sword is in the pack", dsKeysRan(dsSnap(2, dsPalace, dsAllKeys)), StepTalk, func(st Step) bool {
			return st.Stop.ID == NpcSirPrysin && st.UntilItemID == ObjSilverlight && st.Stop.Prefer[0] == "I'm still looking."
		}},
		{"all three keys, the bank not read: the bank first", dsSnap(2, dsPalace, dsAllKeys), StepProvision,
			func(st Step) bool { return st.KitName == KitDemonKeys }},
	}...)
	for _, c := range cases {
		st := dsDecide(c.s)
		if st.Kind != c.kind || (c.want != nil && !c.want(st)) {
			t.Errorf("%s: %s %+v", c.name, st.Describe(), st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
	}
}

// The sewer gate: a hunted account with no hitpoints to spare is not sent down, and parks for the hitpoints; the same account with
// food goes; a strong one needs none.
func TestDecideDemonSlayerSewerGate(t *testing.T) {
	keys := map[int]int{ObjKeyTraiborn: 1, ObjKeyRovin: 1, ObjBucketOfWater: 1}
	weak := func(s *Snapshot) {
		// Combat 43: past the circle's wizards (so the early gate lets it by) and still hunted in the sewer (ceiling 54).
		s.Levels = map[string]int{"attack": 55, "strength": 55, "defence": 8, "hitpoints": 21, "prayer": 1, "magic": 1, "ranged": 1}
		s.MaxHP, s.HP = 21, 21
	}
	drained := func(s Snapshot) Snapshot { s.Ran["provision:"+KitDemonFoodSewer] = true; return dsKeysRan(s) }
	st := dsDecide(drained(dsWith(dsSnap(2, dsPalace, keys), weak)))
	if st.Kind != StepWait || st.ParkFor != DemonFitRetry || !strings.Contains(st.Reason, "manhole") {
		t.Errorf("a weak account at the manhole: %s %+v", st.Describe(), st)
	}
	st = dsDecide(drained(dsWith(dsSnap(2, dsPalace, dsHeld(keys, dsFood)), weak)))
	if st.Kind != StepCustom || st.Custom != CustomDemonDrain {
		t.Errorf("the same account with four sharks: %s", st.Describe())
	}
	st = dsDecide(drained(dsSnap(2, dsPalace, keys)))
	if st.Kind != StepCustom || st.Custom != CustomDemonDrain {
		t.Errorf("a combat-49 account without food: %s", st.Describe())
	}
}

// An account the circle's wizards hunt is parked at every stage, before a coin or an hour of keys is spent on a fight it cannot start
// (the circle is single-way combat: see DemonHuntFit), for the hours a combat level takes; one they leave alone goes on.
func TestDecideDemonSlayerParksAHuntedAccountAtEveryStage(t *testing.T) {
	hunted := func(s Snapshot) Snapshot {
		a := dsAtCombat(30, 45)
		s.Levels, s.MaxHP, s.HP = a.Levels, a.MaxHP, a.HP
		return s
	}
	coins := map[int]int{CoinsID: 40}
	for name, s := range map[string]Snapshot{
		"not started, with a coin":    dsSnap(0, dsVarrock, coins),
		"stage 0 on a yellow tab":     dsWith(dsSnap(0, dsVarrock, coins), func(s *Snapshot) { s.Journal, s.Stage, s.StageKnown = StatusInProgress, 0, true }),
		"stage 1":                     dsSnap(1, dsPalace, nil),
		"stage 2, no keys":            dsSnap(2, dsPalace, coins),
		"stage 2, all keys":           dsSnap(2, dsPalace, dsAllKeys),
		"stage 29, the sword in hand": dsKeysRan(dsSnap(29, dsPalace, map[int]int{ObjSilverlight: 1})),
		"stage 29, the sword worn":    dsKeysRan(dsSnap(29, dsPalace, nil, ObjSilverlight)),
		"stage 29, at the circle":     dsKeysRan(dsSnap(29, DelrithSpawn, nil, ObjSilverlight)),
	} {
		st := dsDecide(hunted(s))
		if st.Kind != StepWait || st.ParkFor != DemonFitRetry || !strings.Contains(st.Reason, "single-way") || !strings.Contains(st.Reason, "combat 41 and under") {
			t.Errorf("%s: %s %+v", name, st.Describe(), st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed step: %s", name, why)
		}
		// The same snapshot at combat 42 does not park for it.
		a := dsAtCombat(DemonMinCombat, 45)
		fit := s
		fit.Levels, fit.MaxHP, fit.HP = a.Levels, a.MaxHP, a.HP
		if st := dsDecide(fit); st.Kind == StepWait && st.ParkFor == DemonFitRetry {
			t.Errorf("%s at combat %d: parked: %s", name, DemonMinCombat, st.Reason)
		}
	}
	// An account found in the sewer is still taken out first: the gate parks it only once it is up.
	st := dsDecide(hunted(dsSnap(2, dsSewer, dsAllKeys)))
	if st.Kind != StepCustom || st.Custom != CustomDemonSewer {
		t.Errorf("a hunted account in the sewer: %s %+v", st.Describe(), st)
	}
}

// An account found in the sewer is taken out of it first, whatever else it has to do and whatever the journal says.
func TestDecideDemonSlayerInTheSewer(t *testing.T) {
	for name, s := range map[string]Snapshot{
		"stage 2 with the bucket spent": dsSnap(2, dsSewer, map[int]int{ObjKeyTraiborn: 1}),
		"stage 2 holding the key":       dsSnap(2, dsSewer, dsAllKeys),
		"stage unread":                  dsWith(dsSnap(2, dsSewer, nil), func(s *Snapshot) { s.StageKnown = false }),
		"stage 29":                      dsSnap(29, dsSewer, map[int]int{ObjSilverlight: 1}),
	} {
		if st := dsDecide(s); st.Kind != StepCustom || st.Custom != CustomDemonSewer {
			t.Errorf("%s: %s", name, st.Describe())
		}
	}
	// A ladder that would not climb parks the quest for the hour, not for ever, and the next walk takes the account out.
	st := dsDecide(dsWith(dsSnap(2, dsSewer, nil), func(s *Snapshot) { s.Failed[CustomDemonSewer] = true }))
	if st.Kind != StepWait || st.ParkFor != DemonSewerRetry {
		t.Errorf("the failed climb: %s %+v", st.Describe(), st)
	}
	// The far edge of the sewer, and the corridor of the way to the key, are in it; the street above and the Wilderness dungeon are not.
	for _, at := range []tile.Tile{DemonSewerLanding, DemonSewerKey, t3(3130, 9856, 0), t3(3270, 9922, 0)} {
		if !InVarrockSewer(at) {
			t.Errorf("%v is not in the sewer", at)
		}
	}
	for _, at := range []tile.Tile{DemonManholeStand, t3(3237, 9923, 0), t3(3237, 9859, 1), t3(3129, 9858, 0), t3(2530, 9703, 0), TraibornAnchor} {
		if InVarrockSewer(at) {
			t.Errorf("%v is in the sewer", at)
		}
	}
}

func TestDecideDemonSlayerStoneCircle(t *testing.T) {
	sword := map[int]int{ObjSilverlight: 1}
	circleFood := func(s Snapshot) Snapshot { s.Ran["provision:"+KitDemonFoodCircle] = true; return dsKeysRan(s) }
	cases := []struct {
		name string
		s    Snapshot
		kind StepKind
		want func(Step) bool
	}{
		{"no sword anywhere, the bank not read: the bank", dsSnap(29, dsPalace, nil), StepProvision,
			func(st Step) bool { return st.KitName == KitDemonKeys && strings.Contains(st.Reason, "Silverlight") }},
		{"no sword, the bank read: Sir Prysin gives it again", dsKeysRan(dsSnap(29, dsPalace, nil)), StepTalk, func(st Step) bool {
			return st.Stop.ID == NpcSirPrysin && st.UntilItemID == ObjSilverlight && st.Stop.Prefer[0] == "No, I'm afraid I've lost Silverlight."
		}},
		{"the sword in the pack: the food, once", dsKeysRan(dsSnap(29, dsPalace, sword)), StepProvision,
			func(st Step) bool { return st.KitName == KitDemonFoodCircle && len(st.Kit) == 2 }},
		{"the sword in the pack: wield it", circleFood(dsSnap(29, dsPalace, sword)), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonWield && st.ItemID == ObjSilverlight }},
		{"the wield failed: parks for the hour", dsWith(circleFood(dsSnap(29, dsPalace, sword)), func(s *Snapshot) { s.Failed[CustomDemonWield] = true }), StepWait,
			func(st Step) bool { return st.ParkFor == DemonWieldRetry }},
		{"the sword worn: Delrith", circleFood(dsSnap(29, dsPalace, nil, ObjSilverlight)), StepCustom,
			func(st Step) bool {
				return st.Custom == CustomDemonDelrith && strings.Contains(st.Reason, "incantation")
			}},
		{"the sword worn at the circle: Delrith", circleFood(dsSnap(29, DelrithSpawn, nil, ObjSilverlight)), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonDelrith }},
		{"the visit failed: parks for hours", dsWith(circleFood(dsSnap(29, dsPalace, nil, ObjSilverlight)), func(s *Snapshot) { s.Failed[CustomDemonDelrith] = true }), StepWait,
			func(st Step) bool {
				return st.ParkFor == DemonDelrithRetry && strings.Contains(st.Reason, "stone circle")
			}},
		{"low hitpoints and no food: waits half an hour", dsWith(circleFood(dsSnap(29, dsPalace, nil, ObjSilverlight)), func(s *Snapshot) { s.HP = 20 }), StepWait,
			func(st Step) bool { return st.ParkFor == DemonLowHPRetry && strings.Contains(st.Reason, "20 of 45") }},
		{"low hitpoints with food: goes", dsWith(circleFood(dsSnap(29, dsPalace, dsFood, ObjSilverlight)), func(s *Snapshot) { s.HP = 20 }), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonDelrith }},
		{"a sword held and worn at once is worn", circleFood(dsSnap(29, dsPalace, sword, ObjSilverlight)), StepCustom,
			func(st Step) bool { return st.Custom == CustomDemonDelrith }},
	}
	for _, c := range cases {
		st := dsDecide(c.s)
		if st.Kind != c.kind || (c.want != nil && !c.want(st)) {
			t.Errorf("%s: %s %+v", c.name, st.Describe(), st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
	}
}

// ---- the gate ----

// dsLevels is an account's base levels.
func dsLevels(att, str, def, hp, mag, pray int) map[string]int {
	return map[string]int{"attack": att, "strength": str, "defence": def, "hitpoints": hp, "magic": mag, "prayer": pray, "ranged": 1}
}

func dsAcct(l map[string]int, held map[int]int) Snapshot {
	s := dsSnap(29, dsPalace, held, ObjSilverlight)
	s.Levels, s.MaxHP, s.HP = l, l["hitpoints"], l["hitpoints"]
	return s
}

// dsAtCombat is an account of exactly the combat level (Attack and Strength 40, so that Silverlight fells Delrith well inside the limit),
// with the hitpoints given, by raising Defence.
func dsAtCombat(want, hp int) Snapshot {
	l := dsLevels(40, 40, 1, hp, 1, 1)
	for def := 1; def < 400; def++ {
		l["defence"] = def
		if demonCombat(l) >= want {
			break
		}
	}
	return dsAcct(l, nil)
}

func TestDemonFitByAccount(t *testing.T) {
	cases := []struct {
		name  string
		s     Snapshot
		ok    bool
		reasn string
	}{
		{"the fleet's usual account", dsAcct(dsLevels(40, 40, 30, 45, 55, 31), nil), true, ""},
		// The wizards hunt every account of combat 41 and under, and a hunted account cannot start the fight whatever it carries.
		{"a Waterfall graduate", dsAcct(dsLevels(32, 32, 19, 20, 17, 1), nil), false, "single-way"},
		{"a Waterfall graduate with four sharks", dsAcct(dsLevels(32, 32, 19, 30, 17, 1), dsFood), false, "single-way"},
		{"a Waterfall graduate with eight sharks", dsAcct(dsLevels(32, 32, 19, 30, 17, 1), map[int]int{ObjShark: 8}), false, "single-way"},
		{"an account at combat 38 with four sharks", dsAcct(dsLevels(32, 32, 30, 40, 40, 1), dsFood), false, "single-way"},
		{"a combat-25 account with eight sharks", dsAcct(dsLevels(20, 20, 15, 35, 20, 10), map[int]int{ObjShark: 8}), false, "single-way"},
		{"combat 41, the last the bearded wizards hunt", dsAtCombat(41, 45), false, "stone circle"},
		{"combat 42, the first they leave alone, with no food", dsAtCombat(42, 45), true, ""},
		{"a fresh account", dsAcct(dsLevels(1, 1, 1, 10, 1, 1), nil), false, "hitpoints"},
		{"19 hitpoints", dsAcct(dsLevels(60, 60, 60, 19, 60, 60), dsFood), false, "19 hitpoints"},
		{"20 hitpoints at combat 80 needs nothing", dsAcct(dsLevels(70, 70, 70, 20, 70, 60), nil), true, ""},
		{"a mage whose sword cannot fell him", dsAcct(dsLevels(5, 5, 30, 50, 66, 20), dsFood), false, "swing"},
		{"hitpoints unread", dsWith(dsAcct(dsLevels(40, 40, 30, 45, 55, 31), nil), func(s *Snapshot) { s.MaxHP = 0 }), true, ""},
		{"attack unread", dsWith(dsAcct(dsLevels(0, 0, 30, 45, 55, 31), nil), func(s *Snapshot) {}), true, ""},
		{"levels unread: the visit's own watch is the net", dsWith(dsAcct(dsLevels(40, 40, 30, 45, 55, 31), nil), func(s *Snapshot) { s.Levels = nil }), true, ""},
	}
	for _, c := range cases {
		ok, why := DemonFit(c.s)
		if ok != c.ok || (c.reasn != "" && !strings.Contains(why, c.reasn)) {
			t.Errorf("%s: fit %v (%s), want %v containing %q", c.name, ok, why, c.ok, c.reasn)
		}
		if ok && why != "" {
			t.Errorf("%s: a pass with a reason: %s", c.name, why)
		}
	}
	// The hitpoints gate alone is the same floor.
	if ok, _ := DemonFitHitpoints(dsAcct(dsLevels(1, 1, 1, 19, 1, 1), nil)); ok {
		t.Error("19 hitpoints pass the hitpoints gate")
	}
	if ok, _ := DemonFitHitpoints(dsAcct(dsLevels(1, 1, 1, 20, 1, 1), nil)); !ok {
		t.Error("20 hitpoints fail the hitpoints gate")
	}
}

// Food does not buy a hunted account into the fight, and the reason says why: the circle is single-way combat, so the server refuses the
// attack on Delrith ("I'm already under attack!") while a wizard's spell has landed in the last eight ticks.
func TestAHuntedAccountIsParkedWhateverItCarries(t *testing.T) {
	for _, hp := range []int{25, 45, 60} {
		for _, food := range []map[int]int{nil, dsFood, {ObjShark: 28}} {
			s := dsAtCombat(30, hp)
			if DemonCombat(s) >= DemonMinCombat {
				t.Fatalf("the test's account at %d hitpoints is combat %d, not hunted", hp, DemonCombat(s))
			}
			s.InvIDs = dsHeld(food)
			if ok, why := DemonFit(s); ok || !strings.Contains(why, "single-way") || !strings.Contains(why, "already under attack") {
				t.Errorf("combat %d, %d hitpoints, food %v: fit %v (%s)", DemonCombat(s), hp, food, ok, why)
			}
			if ok, why := DemonHuntFit(s); ok || !strings.Contains(why, fmt.Sprintf("combat %d and under", DemonMinCombat-1)) {
				t.Errorf("combat %d: hunt fit %v (%s)", DemonCombat(s), ok, why)
			}
		}
	}
	// Combat 42 is the line: DemonMinCombat is the bearded wizards' ceiling (twice their level 20) and the margin and one.
	if DemonMinCombat != 42 || DemonMinCombat != demonCircleHunters[len(demonCircleHunters)-1].Ceiling+demonCombatMargin+1 {
		t.Errorf("DemonMinCombat %d", DemonMinCombat)
	}
	for combat := 3; combat < DemonMinCombat; combat++ {
		if ok, _ := DemonHuntFit(dsAtCombat(combat, 45)); ok {
			t.Errorf("combat %d passes the hunt gate", combat)
		}
	}
	for _, combat := range []int{DemonMinCombat, DemonMinCombat + 1, 60, 99, 126} {
		if ok, why := DemonHuntFit(dsAtCombat(combat, 45)); !ok {
			t.Errorf("combat %d fails the hunt gate: %s", combat, why)
		}
	}
	// An unread combat level passes: the gate decides on what it has read, and the visit's watch is the net.
	if ok, _ := DemonHuntFit(Snapshot{}); !ok {
		t.Error("an unread combat level fails the hunt gate")
	}
}

// The refusal the visit watches for is the server's, word for word as the chat carries it.
func TestTheUnderAttackRefusalIsRecognised(t *testing.T) {
	for chat, want := range map[string]bool{
		"I'm already under attack!":                 true,
		"You hit it.\nI'm already under attack!\n":  true,
		"Someone else is fighting that.":            false,
		"Maybe I'd better wield silverlight first.": false,
		"": false,
		"Delrith is already under attack from someone else, I fear": false,
	} {
		if got := DemonRefusedUnderAttack(chat); got != want {
			t.Errorf("%q: %v, want %v", chat, got, want)
		}
	}
	if DemonUnderAttackLimit < 2 {
		t.Errorf("the visit leaves at the first refusal (limit %d)", DemonUnderAttackLimit)
	}
}

// The wizards hunt an account only up to twice their level, outside the Wilderness (Npc.ts HuntCheckNotTooStrong): the young ones
// (7) up to combat 14, the bearded ones (20) up to 40, and the gate parks exactly those accounts, with a margin of one level.
func TestTheCircleWizardsHuntByCombatLevel(t *testing.T) {
	for _, c := range []struct {
		combat int
		young  bool
		beard  bool
	}{{3, true, true}, {13, true, true}, {14, true, true}, {15, false, true}, {40, false, true}, {41, false, true}, {42, false, false}, {80, false, false}} {
		s := dsAtCombat(c.combat, 45)
		got := DemonCombat(s)
		youngHunts := demonHunted(s, 14)
		beardHunts := demonHunted(s, 40)
		// The margin of one level above a ceiling is the model's, for an account whose own reading is a point off.
		wantYoung, wantBeard := got <= 14+demonCombatMargin, got <= 40+demonCombatMargin
		if youngHunts != wantYoung || beardHunts != wantBeard {
			t.Errorf("combat %d (built %d): young hunts %v beard hunts %v, want %v %v", c.combat, got, youngHunts, beardHunts, wantYoung, wantBeard)
		}
		// The gate parks the account when either hunts it, which is when the bearded ones do (their ceiling holds the young ones').
		if ok, _ := DemonHuntFit(s); ok == (youngHunts || beardHunts) {
			t.Errorf("combat %d: hunt fit %v with young %v beard %v", got, ok, youngHunts, beardHunts)
		}
	}
	// An unread combat level is hunted (the model's reading); the gate lets it through to the visit's own watch.
	if !demonHunted(Snapshot{}, 14) {
		t.Error("an unread combat level is not hunted")
	}
}

func TestDemonSewerFitByAccount(t *testing.T) {
	// A strong account is skipped by every monster on the route (the highest is the giant spider, 27 times two).
	strong := dsAcct(dsLevels(60, 60, 55, 60, 60, 40), nil)
	if DemonCombat(strong) <= DemonSewerCeiling+demonCombatMargin || DemonSewerDamage(strong) != 0 {
		t.Errorf("combat %d is charged %.1f for the sewer", DemonCombat(strong), DemonSewerDamage(strong))
	}
	if ok, why := DemonSewerFit(strong); !ok {
		t.Errorf("the strong account: %s", why)
	}
	mid := dsAcct(dsLevels(40, 40, 30, 45, 55, 31), nil)
	if DemonSewerDamage(mid) <= 0 {
		t.Errorf("combat %d is not charged for the sewer", DemonCombat(mid))
	}
	if ok, why := DemonSewerFit(mid); !ok {
		t.Errorf("the usual account has hitpoints to spare: %s", why)
	}
	thin := dsAcct(dsLevels(20, 20, 5, 20, 1, 1), nil)
	if ok, _ := DemonSewerFit(thin); ok {
		t.Error("an account of 20 hitpoints and Defence 5 passes the sewer with no food")
	}
	if ok, why := DemonSewerFit(dsAcct(dsLevels(20, 20, 5, 20, 1, 1), dsFood)); !ok {
		t.Errorf("the same with sharks: %s", why)
	}
	if ok, _ := DemonSewerFit(dsAcct(dsLevels(1, 1, 1, 19, 1, 1), dsFood)); ok {
		t.Error("19 hitpoints pass the sewer")
	}
}

// The foes' numbers are the content's (the content test pins them to the npc files).
func TestTheDemonFoes(t *testing.T) {
	if FoeDelrith.PeakHit() != 3 || FoeDelrith.AttackTicks != 4 || FoeDelrith.Hitpoints != 7 {
		t.Errorf("Delrith %+v hits up to %d", FoeDelrith, FoeDelrith.PeakHit())
	}
	if FoeYoungDarkWizard.PeakHit() != 4 || FoeBeardedDarkWizard.PeakHit() != 6 {
		t.Errorf("the wizards hit up to %d and %d", FoeYoungDarkWizard.PeakHit(), FoeBeardedDarkWizard.PeakHit())
	}
	if FoeSewerZombie.PeakHit() != 3 || FoeSewerSpider.PeakHit() != 3 {
		t.Errorf("the sewer's hitters hit up to %d and %d", FoeSewerZombie.PeakHit(), FoeSewerSpider.PeakHit())
	}
	// A caster is charged its spell and nothing else: a Defence-1 account is hit often and at the spell's rate.
	p := MeleeFighter{Attack: 1, Strength: 1, Defence: 1, Magic: 1}
	if got := FoePerAttack(FoeBeardedDarkWizard, p); got < 2 || got > 3 {
		t.Errorf("a bearded wizard's cast costs a Defence-1 account %.2f hitpoints, want 2 to 3 (spell up to 6, mostly hits)", got)
	}
	// Silverlight in hand: five-tick swings, +14 attack, +12 strength.
	f := demonSwing(dsAcct(dsLevels(40, 40, 30, 45, 55, 31), nil))
	if f.AttackTicks != 5 || f.AttackBonus != 14 || f.StrengthBonus != 12 || f.Attack != 40 || f.Defence != 30 {
		t.Errorf("swing %+v", f)
	}
}

// ---- the steps and the kits ----

// Every step the module can return from every state the tests above walk through is well formed, and every provisioning kit is
// valid and keeps what it provisions.
func TestEveryDemonSlayerKitIsWellFormedAndKept(t *testing.T) {
	kits := map[string][]KitLine{
		KitDemonCoins: demonCoinsKit(), KitDemonKeys: demonKeysKit(), KitDemonBones: demonBonesKit(), KitDemonBucket: demonBucketKit(),
		KitDemonFoodSewer: demonFoodKit(), KitDemonFoodCircle: demonFoodKit(),
	}
	keep := map[string][]int{
		KitDemonCoins: demonKeep(), KitDemonKeys: demonKeep(), KitDemonBones: demonBonesKeep(), KitDemonBucket: demonKeep(),
		KitDemonFoodSewer: demonKeep(), KitDemonFoodCircle: demonKeep(),
	}
	for name, kit := range kits {
		for _, l := range kit {
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("%s: %s", name, why)
			}
			if !slices.Contains(keep[name], l.ID) {
				t.Errorf("%s: %s (%d) is provisioned and not kept by the step's own bank visit", name, l.Name, l.ID)
			}
		}
	}
	// The keys and the sword are quest items: never asked of the mule, never bought, never waited for.
	for _, l := range demonKeysKit() {
		if !l.Untradeable || !l.Optional || len(l.Sources) != 0 {
			t.Errorf("%s: %+v", l.Name, l)
		}
	}
	// Bones: 25 sets, one skill source only (the bank and the mule are implied), the cows' pasture.
	b := demonBonesKit()[0]
	if b.ID != ObjBones || b.Qty != 25 || len(b.Sources) != 1 || b.Sources[0].Kind != SourceSkill || b.Sources[0].Method != MethodDemonBones ||
		b.Sources[0].Tile != DemonCowField {
		t.Errorf("the bones line %+v", b)
	}
	// The bucket: bank and mule for either the water or the empty one; the empty one's ground spawn comes before the shop in the
	// item rule, and the shop is Varrock's general store.
	bk := demonBucketKit()
	if bk[1].Sources[0].Kind != SourceGround || bk[1].Sources[1].Kind != SourceShop || bk[1].Sources[1].Shop != VarrockGenStore {
		t.Errorf("the bucket's sources %+v", bk[1].Sources)
	}
	// The coins are a stackable purse the bank and the mule alone fill.
	c := demonCoinsKit()[0]
	if c.ID != CoinsID || !c.Stackable || c.Qty != DemonCoinFloat || len(c.Sources) != 0 {
		t.Errorf("the purse %+v", c)
	}
	// The bones kit plans: the pack with 25 free slots, an empty bank and no mule goes to the cows for all of them; one holding 12
	// banked takes those and farms 13.
	plan := PlanProvision(demonBonesKit(), ProvisionState{Pack: map[int]int{CoinsID: 30}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 26,
		Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseSource || len(plan.Tasks) != 1 || plan.Tasks[0].Qty != 25 || plan.Tasks[0].Source.Method != MethodDemonBones {
		t.Errorf("an empty bank: %s", plan.Summary())
	}
	plan = PlanProvision(demonBonesKit(), ProvisionState{Pack: map[int]int{CoinsID: 30}, Bank: map[int]int{ObjBones: 12}, BankKnown: true, FreeSlots: 26,
		Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseBank || len(plan.Withdraw) != 1 || plan.Withdraw[0].Count != 12 || len(plan.Tasks) != 1 || plan.Tasks[0].Qty != 13 {
		t.Errorf("12 banked: %s", plan.Summary())
	}
	// The mule's bones, when it publishes enough, fill the line in one ask (a bank with none).
	plan = PlanProvision(demonBonesKit(), ProvisionState{Pack: map[int]int{CoinsID: 30}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 26,
		Mule: map[int]int{ObjBones: 5000}, MuleKnown: true, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseMule || len(plan.MuleAsk) != 1 || plan.MuleAsk[0].Count != 25 {
		t.Errorf("a mule with 5000 bones: %s", plan.Summary())
	}
	// A bank with a banked key hands it back; one with nothing is satisfied with the bank read.
	plan = PlanProvision(demonKeysKit(), ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjKeyRovin: 1, ObjKeyDrain: 1}, BankKnown: true, FreeSlots: 20,
		Mule: map[int]int{ObjKeyRovin: 99}, MuleKnown: true, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseBank || len(plan.Withdraw) != 2 || len(plan.MuleAsk) != 0 {
		t.Errorf("two banked keys: %s", plan.Summary())
	}
	plan = PlanProvision(demonKeysKit(), ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, Levels: map[string]int{},
		Exhausted: map[string]bool{}})
	if plan.Phase != PhaseSatisfied {
		t.Errorf("an empty bank: %s", plan.Summary())
	}
	plan = PlanProvision(demonKeysKit(), ProvisionState{Pack: map[int]int{}, BankKnown: false, FreeSlots: 20, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseBank {
		t.Errorf("an unread bank is read first: %s", plan.Summary())
	}
	// A sword already worn is held.
	plan = PlanProvision(demonKeysKit(), ProvisionState{Pack: map[int]int{}, Worn: map[int]int{ObjSilverlight: 1}, Bank: map[int]int{ObjSilverlight: 1}, BankKnown: true,
		FreeSlots: 20, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if len(plan.Withdraw) != 0 {
		t.Errorf("a worn sword drawn again: %s", plan.Summary())
	}
}

func TestDemonSlayerDescribesItsStepsForTheLog(t *testing.T) {
	for want, s := range map[string]Snapshot{
		"talk to Gypsy Aris":    dsSnap(0, dsVarrock, map[int]int{CoinsID: 5}),
		"talk to Sir Prysin":    dsSnap(1, dsPalace, nil),
		"provision demon-keys":  dsSnap(2, dsPalace, nil),
		"demon-traiborn":        dsKeysRan(dsSnap(2, dsTower, map[int]int{ObjBones: 25})),
		"talk to Captain Rovin": dsKeysRan(dsSnap(2, dsPalace, map[int]int{ObjKeyTraiborn: 1})),
		"demon-sewer":           dsSnap(2, dsSewer, nil),
		"demon-wield":           dsKeysRan(dsWith(dsSnap(29, dsPalace, map[int]int{ObjSilverlight: 1}), func(s *Snapshot) { s.Ran["provision:"+KitDemonFoodCircle] = true })),
		"demon-delrith":         dsKeysRan(dsWith(dsSnap(29, dsPalace, nil, ObjSilverlight), func(s *Snapshot) { s.Ran["provision:"+KitDemonFoodCircle] = true })),
	} {
		if got := dsDecide(s).Describe(); !strings.Contains(got, want) {
			t.Errorf("described %q, want it to contain %q", got, want)
		}
	}
}

// Whatever the state, a step is never a loop: a Wait always has a reason, and a Wait the account cannot mend by waiting (a fit gate, a
// kit with no source, a failed step) parks for hours rather than a few minutes.
func TestDemonSlayerNeverReturnsAnUnexplainedWait(t *testing.T) {
	stages := []int{0, 1, 2, 29, 30}
	flags := []string{"", "provision:" + KitDemonKeys, "provision:" + KitDemonBones, "provision:" + KitDemonBucket, "provision:" + KitDemonCoins,
		"provision:" + KitDemonFoodSewer, "provision:" + KitDemonFoodCircle, CustomDemonTraiborn, CustomDemonDrain, CustomDemonWater, CustomDemonSewer,
		CustomDemonWield, CustomDemonDelrith}
	packs := []map[int]int{nil, {CoinsID: 5}, dsAllKeys, {ObjBones: 25}, {ObjBucket: 1}, {ObjBucketOfWater: 1}, {ObjSilverlight: 1}, {ObjKeyTraiborn: 1, ObjKeyRovin: 1},
		{ObjKeyTraiborn: 1, ObjKeyRovin: 1, ObjBucket: 1}}
	spots := []tile.Tile{dsVarrock, dsPalace, dsSewer, dsTower, DelrithSpawn}
	n := 0
	for _, stage := range stages {
		for _, flag := range flags {
			for _, pack := range packs {
				for _, spot := range spots {
					for _, worn := range [][]int{nil, {ObjSilverlight}} {
						for _, ran := range []bool{false, true} {
							s := dsSnap(stage, spot, pack, worn...)
							if stage == 30 {
								s.Journal = StatusComplete
							}
							if flag != "" {
								s.Failed[flag] = true
							}
							if ran {
								for _, k := range []string{KitDemonKeys, KitDemonFoodSewer, KitDemonFoodCircle} {
									if "provision:"+k != flag {
										s.Ran["provision:"+k] = true
									}
								}
							}
							st := dsDecide(s)
							n++
							if why := ValidateStep(st); why != "" {
								t.Fatalf("stage %d failed %q pack %v at %v worn %v: malformed %s: %+v", stage, flag, pack, spot, worn, why, st)
							}
							if st.Kind == StepWait && strings.TrimSpace(st.Reason) == "" {
								t.Fatalf("an unexplained wait: stage %d failed %q pack %v", stage, flag, pack)
							}
							// A step that failed is not decided again: what the failure ruled out is never the answer, and the engine's
							// watchdog never has to count a repeat to stop it.
							if st.Kind == StepCustom && s.Failed[st.Custom] {
								t.Fatalf("stage %d: %s was returned after it failed (pack %v at %v)", stage, st.Custom, pack, spot)
							}
							if st.Kind == StepProvision && s.Failed["provision:"+st.KitName] {
								t.Fatalf("stage %d: kit %s was returned after it failed", stage, st.KitName)
							}
						}
					}
				}
			}
		}
	}
	if n < 1000 {
		t.Fatalf("only %d states walked", n)
	}
}
