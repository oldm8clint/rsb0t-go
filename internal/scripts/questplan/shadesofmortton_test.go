package questplan

import (
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Shades of Mort'ton's plan: the journal's markers against pages rendered the way mortton_journal.rs2 renders them, the decision at every stage and every pack that
// changes it, the kit and its keep list, the dose arithmetic and the price of the shades and of the swamp.

// moJournalPage renders the quest journal as mortton_journal.rs2 builds it for a stage (%morttonquest, 0 to 85): used says the serum has been used on a villager (it changes
// stage 10's last paragraph), diary that the pack holds Herbi Flax's diary (stage 5's), oil that it holds olive oil (stage 55's) and remains and pyre that it holds a set of remains
// and a pyre log (stage 70's). Lines break at the script's "|", and the colour tags are the script's own.
func moJournalPage(stage int, used, diary, oil, remains, pyre bool) []string {
	var text string
	add := func(s string) { text += s }
	switch {
	case stage == 0:
		return strings.Split("@dbl@I can start this quest by exploring the town of @dre@Mort'ton@dbl@ which is accessible from the southern edge of @dre@Mort Myre@dbl@.", "|")
	case stage == MtComplete:
		add("@str@I travelled to the town of Mort'ton where the people had an|@str@affliction. I found the diary of 'Herbi Flax' and managed to|@str@work out the ingredients for a special serum. The serum|@str@worked on the afflicted and I was able to speak to several|@str@villagers.|@str@After slaying five shades and helping to rebuild a sacred|@str@temple, I was able to put the shades spirits to rest. The|@str@town has pretty much returned to normal, except for all the|@str@shade activity, but now I have a way to put the shades|@str@spirits to rest and gain access to their treasure filled lairs!|")
		add("@red@QUEST COMPLETE!")
		return strings.Split(text, "|")
	}
	if stage >= MtReadDiary {
		add("@str@I can start this quest by exploring the town of Mort'ton|@str@which is accessible from the southern edge of Mort Myre.|")
	}
	if stage >= MtMadeSerum {
		add("@str@I made my way down through Mort Myre and found the town|@str@of Mort'ton. I discovered a diary which had some notes on|@str@a special serum in it. I made the serum.|")
	}
	if stage >= MtKillShades {
		add("@str@I made serum 207 from tarromin, water and ashes by using|@str@Herbi Flax's diary. I wonder what the serum is used for?|")
		add("@str@I used the serum on a local NPC and I've been able to ask|@str@a few questions about the place.|")
		if stage == MtKillShades {
			add("@dbl@I talked to @dre@Razmire Keelgan@dbl@ and he asked me to kill @dre@five shades@dbl@ for him. He seems to hate them!|")
		} else {
			add("@str@I talked to Razmire Keelgan and he asked me to kill five|@str@shades for him. He seems to hate them!|")
		}
	}
	if stage >= MtShadesToRazmire {
		add("@str@I've killed five shades and showed the remains to Razmire.|@str@Razmire told me to show the remains to Ulsquire.|")
	}
	if stage >= MtShadesToUlsquire {
		add("@str@I showed Ulsquire the shade remains and he gave me some|@str@information which may be useful.|")
	}
	if stage >= MtRebuildTemple {
		add("@str@Ulsquire seems to know a lot about this area, he's quite|@str@interested in 'Flaemtaer temple' and wishes he could|@str@rebuild it.|")
	}
	if stage >= MtCanLightAltar {
		add("@str@I helped rebuild the temple using limestone bricks, wooden|@str@planks and nails.|")
	}
	if stage >= MtSacredOil {
		add("@str@When all of the temple walls were rebuilt, a fire altar|@str@appeared. The flame from the altar was sacred.|@str@I placed a flask of olive oil in the flame and it became|@str@sacred oil.|")
	}
	if stage >= MtPyreLogs {
		add("@str@I used sacred oil on logs turning them into pyre logs.|")
	}
	if stage >= MtLogsOnPyre {
		add("@str@I placed some pyre logs onto a funeral pyre.|")
	}
	if stage >= MtLitPyre {
		add("@str@I placed the shade remains on the pyre and set it on fire in|@str@order to put the Shades to rest.|")
	}
	if stage == MtReadDiary {
		add("@dbl@I made my way down through @dre@Mort Myre@dbl@ and I found the town of @dre@Mort'ton@dbl@. ")
		if diary {
			add("I've found a @dre@diary@dbl@ which has some interesting @dre@notes@dbl@ in it. Perhaps this can help me?")
		} else {
			add("I discovered a @dre@diary@dbl@ which had some interesting @dre@notes@dbl@ in it. @dbl@Maybe it was important?")
		}
	}
	if stage == MtMadeSerum {
		if used {
			add("@str@I made serum 207 from tarromin, water and ashes by|@str@using Herbi Flax's diary. I wonder what the serum is used|@str@for?|")
			add("@dbl@I used the @dre@serum@dbl@ on a local NPC and I've been able to ask a few questions about the place.")
		} else {
			add("@dbl@I made @dre@serum 207@dbl@ from tarromin, water and ashes by using @dre@Herbi Flax's@dbl@ diary. I wonder what the @dre@serum@dbl@ is used for?")
		}
	}
	switch stage {
	case MtKillShades:
		add("@dbl@I have to kill @dre@five@dbl@ shades.")
	case MtKilled1:
		add("@dbl@I've killed @dre@one@dbl@ shade, I need to kill another @dre@four@dbl@.")
	case MtKilled2:
		add("@dbl@I've killed @dre@two@dbl@ shades, I need to kill another @dre@three@dbl@.")
	case MtKilled3:
		add("@dbl@I've killed @dre@three@dbl@ shades, I need to kill another @dre@two@dbl@.")
	case MtKilled4:
		add("@dbl@I've killed @dre@four@dbl@ shades, I need to kill another @dre@one@dbl@.")
	case MtKilled5:
		add("@dbl@I've killed all @dre@five shades@dbl@, I should return to @dre@Razmire@dbl@ now.")
	case MtShadesToRazmire:
		add("@dbl@I should show the @dre@shade@dbl@ remains to @dre@Ulsquire@dbl@.")
	case MtShadesToUlsquire:
		add("@dre@Ulsquire@dbl@ seems to know a lot about this area, maybe if I @dre@investigate@dbl@ around here a little more I can help the locals.")
	case MtUlsquireTemple:
		add("@dre@Ulsquire@dbl@ seems to know a lot about this area, he's quite interested in @dre@'Flaemtaer temple'@dbl@ and wishes he could rebuild it.")
	case MtRebuildTemple:
		add("@dbl@I've started @dre@rebuilding@dbl@ the @dre@temple@dbl@ using @dre@limestone bricks@dbl@, @dre@wooden planks@dbl@ and @dre@nails@dbl@. I wonder what @dre@power@dbl@ the @dre@temple@dbl@ has, if any?|")
		if oil {
			add("@dbl@I have some @dre@olive oil@dbl@ which @dre@Ulsquire@dbl@ said may be useful when the @dre@temple@dbl@ is constructed.")
		}
	case MtCanLightAltar:
		add("@dbl@There must be some way to make use of the @dre@altar@dbl@ that appears in the @dre@middle@dbl@ of the @dre@temple@dbl@.")
	case MtSacredOil:
		add("@dre@Ulsquire@dbl@ said something about putting the @dre@Shades@dbl@ to rest by giving them a @dre@holy cremation@dbl@, maybe this @dre@oil@dbl@ can help?")
	case MtPyreLogs:
		switch {
		case !remains && !pyre:
			add("@dbl@Maybe the @dre@pyre logs@dbl@ are connected to the @dre@Shade@dbl@ remains?|")
		case remains && !pyre:
			add("@dbl@I have some @dre@Shade@dbl@ remains, I need to find a way to put its @dre@spirit@dbl@ to rest.")
		case !remains && pyre:
			add("@dbl@I have some @dre@pyre logs@dbl@, perhaps I can use these somehow to put a @dre@Shades spirit@dbl@ to rest.")
		default:
			add("@str@Maybe the pyre logs are connected to the Shade remains?|")
			add("@dbl@I have some @dre@Shade@dbl@ remains and I have some @dre@pyre logs@dbl@, I can now try to put a @dre@shades@dbl@ spirit to rest.")
		}
	case MtLogsOnPyre:
		add("@dbl@I need to put the @dre@shade remains@dbl@ on the @dre@funeral pyre@dbl@ and then @dre@light@dbl@ it in order to put the@dre@ shade spirit@dbl@ to rest.")
	case MtLitPyre:
		add("@dbl@The @dre@shades spirit@dbl@ was released, I should tell @dre@Ulsquire@dbl@.")
	}
	return strings.Split(text, "|")
}

// moWrapLines re-breaks a journal page the way the interface's own word wrap would, at width characters, so that a marker that crosses a break is exercised.
func moWrapLines(lines []string, width int) []string {
	var out []string
	for _, l := range lines {
		cur := ""
		for _, w := range strings.Fields(l) {
			if cur != "" && len(cur)+1+len(w) > width {
				out = append(out, cur)
				cur = w
				continue
			}
			if cur != "" {
				cur += " "
			}
			cur += w
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

// Every stage's page reads as the stage the module means by it, at any wrap width and in every variant of the pack the page shows.
func TestMorttonJournalReadsEveryStage(t *testing.T) {
	stages := []int{MtNotStarted, MtReadDiary, MtMadeSerum, MtKillShades, MtKilled1, MtKilled2, MtKilled3, MtKilled4, MtKilled5, MtShadesToRazmire, MtShadesToUlsquire,
		MtUlsquireTemple, MtRebuildTemple, MtCanLightAltar, MtSacredOil, MtPyreLogs, MtLogsOnPyre, MtLitPyre, MtComplete}
	for _, stage := range stages {
		for _, v := range []struct{ used, diary, oil, remains, pyre bool }{{}, {true, true, true, true, true}, {false, true, false, true, false}, {true, false, true, false, true}} {
			page := moJournalPage(stage, v.used, v.diary, v.oil, v.remains, v.pyre)
			for _, width := range []int{0, 24, 38, 60} {
				lines := page
				if width > 0 {
					lines = moWrapLines(page, width)
				}
				got, ok := ParseMorttonJournal(lines)
				if !ok || got != stage {
					t.Errorf("stage %d (%+v, width %d): parsed %d ok %v", stage, v, width, got, ok)
				}
			}
		}
	}
	for _, empty := range [][]string{nil, {""}, {"Loading..."}, {"@dbl@Some other quest's page"}} {
		if got, ok := ParseMorttonJournal(empty); ok {
			t.Errorf("%v parsed as stage %d: an unread page is unknown, never stage 0", empty, got)
		}
	}
}

// The journal's stages are the content's own (quest_mortton.constant), the quest tab's name is the tab's spelling and the record's levels and prerequisite are the content's.
func TestMorttonConstantsAreTheContents(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"read diary", MtReadDiary, 5}, {"made serum", MtMadeSerum, 10}, {"kill shades", MtKillShades, 15}, {"killed 5", MtKilled5, 40}, {"to Razmire", MtShadesToRazmire, 45},
		{"to Ulsquire", MtShadesToUlsquire, 47}, {"temple", MtUlsquireTemple, 50}, {"rebuild", MtRebuildTemple, 55}, {"altar", MtCanLightAltar, 60}, {"sacred oil", MtSacredOil, 65},
		{"pyre logs", MtPyreLogs, 70}, {"logs on pyre", MtLogsOnPyre, 75}, {"lit", MtLitPyre, 80}, {"complete", MtComplete, 85},
	} {
		if c.got != c.want {
			t.Errorf("%s: %d, want %d", c.name, c.got, c.want)
		}
	}
	if MorttonRecord.QuestPoints != 3 || MorttonShades.Record.ID != MorttonID || QuestTabName(MorttonID) != MorttonName {
		t.Errorf("record %+v tab %q", MorttonRecord, QuestTabName(MorttonID))
	}
	if !slices.Equal(MorttonRecord.Requirements.Quests, []string{NatureSpiritID}) {
		t.Errorf("prerequisites %v: the swamp's gate is Nature Spirit", MorttonRecord.Requirements.Quests)
	}
	want := map[string]int{"herblore": 15, "crafting": 20}
	for _, r := range MorttonRecord.Requirements.Skills {
		if want[r.Skill] != r.Level {
			t.Errorf("skill requirement %+v", r)
		}
		delete(want, r.Skill)
	}
	if len(want) != 0 {
		t.Errorf("missing skill requirements %v", want)
	}
}

const (
	moStrongLevel = 60
)

var (
	moVarrock = tile.Tile{X: 3253, Z: 3420}
	moSwamp   = tile.Tile{X: 3440, Z: 3340}
)

// moSnap is a snapshot of a strong account (it prices two shades and two ghasts) in the middle of the quest, the pack of a prepared account, the kit's optional round already
// made, and the mods applied.
func moSnap(stage int, at tile.Tile, mods ...func(*Snapshot)) Snapshot {
	s := Snapshot{
		Journal: StatusInProgress, Stage: stage, StageKnown: true, Tile: at, TileKnown: true,
		InvIDs: map[int]int{CoinsID: 5000, ObjShark: 12, ObjTinderbox: 1, ObjMtLogs: 3, ObjAshes: 2}, WornIDs: map[int]int{},
		Bank: map[string]int{}, BankIDs: map[int]int{}, BankKnown: true,
		Levels: map[string]int{"attack": moStrongLevel, "strength": moStrongLevel, "defence": 50, "hitpoints": moStrongLevel, "herblore": 20, "crafting": 25, "firemaking": 5},
		HP:     moStrongLevel, MaxHP: moStrongLevel, FreeSlots: 14,
		Failed: map[string]bool{}, Ran: map[string]bool{"provision:" + KitMorttonOut: true}, Blocked: map[string]bool{}, QuestsDone: map[string]bool{},
	}
	if stage == 0 {
		s.Journal, s.StageKnown = StatusNotStarted, false
	}
	for _, m := range mods {
		m(&s)
	}
	return s
}

func moHold(id, n int) func(*Snapshot) {
	return func(s *Snapshot) {
		if n <= 0 {
			delete(s.InvIDs, id)
			return
		}
		s.InvIDs[id] = n
	}
}
func moFail(name string) func(*Snapshot) { return func(s *Snapshot) { s.Failed[name] = true } }
func moLevel(skill string, n int) func(*Snapshot) {
	return func(s *Snapshot) { s.Levels[skill] = n }
}
func moFree(n int) func(*Snapshot) { return func(s *Snapshot) { s.FreeSlots = n } }

// moSerum is a pack with n doses of serum 207 in vials of three.
func moSerum(doses int) func(*Snapshot) {
	return func(s *Snapshot) {
		if doses/3 > 0 {
			s.InvIDs[ObjMtSerum3] = doses / 3
		}
		switch doses % 3 {
		case 1:
			s.InvIDs[ObjMtSerum1] = 1
		case 2:
			s.InvIDs[ObjMtSerum2] = 1
		}
	}
}

// moBuild is the building material of one run: the sets the pack takes.
func moBuild(s *Snapshot) {
	s.InvIDs[ObjMtPlank], s.InvIDs[ObjMtBrick], s.InvIDs[ObjMtPaste] = 5, 5, 30
}

func moWant(t *testing.T, name string, got Step, kind StepKind, custom string) {
	t.Helper()
	if why := ValidateStep(got); why != "" {
		t.Errorf("%s: malformed step %+v: %s", name, got, why)
	}
	if got.Kind != kind || got.Custom != custom {
		t.Errorf("%s: step %s %q (%s), want %s %q", name, got.Kind, got.Custom, got.Reason, kind, custom)
	}
}

func moPark(t *testing.T, name string, got Step, parkFor interface{ Hours() float64 }, reason string) {
	t.Helper()
	if got.Kind != StepWait || got.ParkFor == 0 || got.ParkFor.Hours() != parkFor.Hours() || !strings.Contains(strings.ToLower(got.Reason), strings.ToLower(reason)) {
		t.Errorf("%s: %s %q park %v, want a park of %vh naming %q", name, got.Kind, got.Reason, got.ParkFor, parkFor.Hours(), reason)
	}
}

// Before the quest and before the crossing: the pack is made fit (kit, ashes burnt) in Varrock, and the account walks to the town only when it is.
func TestMorttonDecideBeforeTheCrossing(t *testing.T) {
	// Unread tab, position and stage wait; a complete tab is done.
	for name, s := range map[string]Snapshot{
		"unknown tab":              moSnap(5, moVarrock, func(s *Snapshot) { s.Journal = StatusUnknown }),
		"no tile":                  moSnap(5, moVarrock, func(s *Snapshot) { s.TileKnown = false }),
		"no stage":                 moSnap(5, moVarrock, func(s *Snapshot) { s.StageKnown = false }),
		"stage 0 on a started tab": moSnap(5, moVarrock, func(s *Snapshot) { s.Stage = 0 }),
	} {
		if got := DecideMortton(s); got.Kind != StepWait || got.ParkFor != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := DecideMortton(moSnap(5, moVarrock, func(s *Snapshot) { s.Journal = StatusComplete })); got.Kind != StepDone {
		t.Errorf("a complete tab: %+v", got)
	}
	// Below a level the quest is left alone for hours.
	for _, c := range []struct {
		skill string
		level int
		want  string
	}{{"herblore", 14, "Herblore 14/15"}, {"crafting", 19, "Crafting 19/20"}} {
		moPark(t, c.skill, DecideMortton(moSnap(0, moVarrock, moLevel(c.skill, c.level))), MtSkillParkFor, c.want)
	}
	// A prepared account at Varrock with ashes made walks to the town.
	ready := moSnap(0, moVarrock)
	got := DecideMortton(ready)
	moWant(t, "a prepared account", got, StepWalk, "")
	if got.Anchor != MtTown {
		t.Errorf("the walk's anchor %v, want %v", got.Anchor, MtTown)
	}
	// Logs and no ashes: the logs are burnt for them first (two of them, two burns).
	burn := DecideMortton(moSnap(0, moVarrock, moHold(ObjAshes, 0)))
	moWant(t, "logs and no ashes", burn, StepCustom, CustomMtBurn)
	if burn.Qty != 2 || burn.Radius != 2 {
		t.Errorf("burn %d logs for %d ashes, want 2 and 2", burn.Qty, burn.Radius)
	}
	moPark(t, "a burn that failed", DecideMortton(moSnap(0, moVarrock, moHold(ObjAshes, 0), moFail(CustomMtBurn))), MtStuckParkFor, "ashes")
	// One ashes in hand: one more is burnt.
	one := DecideMortton(moSnap(0, moVarrock, moHold(ObjAshes, 1)))
	moWant(t, "one ashes", one, StepCustom, CustomMtBurn)
	if one.Qty != 1 || one.Radius != 1 {
		t.Errorf("burn %d for %d, want 1 and 1", one.Qty, one.Radius)
	}
	// A bare pack asks for the required lines in one round, and a kit whose every source is dead parks the quest.
	bare := moSnap(0, moVarrock, func(s *Snapshot) { s.InvIDs = map[int]int{ObjShark: 12} })
	st := DecideMortton(bare)
	moWant(t, "a bare pack", st, StepProvision, "")
	if st.KitName != KitMorttonOut || st.Bank != VarrockEastBank || !st.BankSet {
		t.Errorf("kit %q bank %v", st.KitName, st.Bank)
	}
	have := map[int]KitLine{}
	for _, l := range st.Kit {
		have[l.ID] = l
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("kit line %+v: %s", l, why)
		}
	}
	for _, id := range []int{CoinsID, ObjTinderbox, ObjMtLogs} {
		if _, ok := have[id]; !ok {
			t.Errorf("the kit lacks %d: %+v", id, st.Kit)
		}
	}
	if l := have[ObjMtLogs]; l.Min != 3 || l.Qty < 3 || len(l.Sources) == 0 || l.Sources[0].Kind != SourceGround || l.Sources[0].Tile != MtVarrockLogs {
		t.Errorf("logs line %+v: two for the ashes and one for the pyre, picked up at the spawns by the east bank", l)
	}
	if l := have[CoinsID]; l.Min != MtCoinsMin || l.Qty != MtCoins || !l.Stackable {
		t.Errorf("coins line %+v", l)
	}
	blocked := moSnap(0, moVarrock, func(s *Snapshot) { s.InvIDs = map[int]int{ObjShark: 12}; s.Blocked["provision:"+KitMorttonOut] = true })
	moPark(t, "a blocked kit", DecideMortton(blocked), MtItemParkFor, "coins")
	// The optional lines (ashes and food) are looked for once a run, not waited for.
	noFood := moSnap(0, moVarrock, func(s *Snapshot) { s.InvIDs[ObjShark] = 6; delete(s.Ran, "provision:"+KitMorttonOut) })
	if got := DecideMortton(noFood); got.Kind != StepProvision || got.KitName != KitMorttonOut {
		t.Errorf("optional food: %+v", got)
	}
	noFoodRan := moSnap(0, moVarrock, func(s *Snapshot) { s.InvIDs[ObjShark] = 6 })
	moWant(t, "short of the optional food, once looked for", DecideMortton(noFoodRan), StepWalk, "")
	// An account the swamp's ghasts would beat is not walked into it: under the floor of hitpoints, of bites, or by the model.
	weakHP := moSnap(0, moVarrock, func(s *Snapshot) { s.MaxHP, s.HP = 20, 20; s.Levels["hitpoints"] = 20 })
	moPark(t, "20 hitpoints", DecideMortton(weakHP), MtFightParkFor, "hitpoints")
	noBites := moSnap(0, moVarrock, func(s *Snapshot) { s.InvIDs[ObjShark] = 2 })
	moPark(t, "two bites", DecideMortton(noBites), MtFightParkFor, "bites of food")
	weak := moSnap(0, moVarrock, moLevel("attack", 5), moLevel("strength", 5), moLevel("defence", 1), func(s *Snapshot) { s.MaxHP, s.HP = 26, 26; s.Levels["hitpoints"] = 26 })
	moPark(t, "level 5s", DecideMortton(weak), MtFightParkFor, "ghast")
	// In the swamp the account is committed: the walk goes on whatever it carries.
	for _, s := range []Snapshot{moSnap(0, moSwamp), moSnap(10, moSwamp, func(s *Snapshot) { s.InvIDs = map[int]int{} })} {
		st := DecideMortton(s)
		moWant(t, "in the swamp", st, StepWalk, "")
		if st.Anchor != MtTown {
			t.Errorf("the swamp's walk anchors at %v", st.Anchor)
		}
	}
}

// Stages 0 to 10: the diary, the table's herbs and the serum, in the order the content makes them.
func TestMorttonDecideTheDiaryAndTheSerum(t *testing.T) {
	// Stage 0 in the town: the diary off the shelf, read to its last page.
	moWant(t, "the shelf", DecideMortton(moSnap(0, MtTown)), StepCustom, CustomMtTakeDiary)
	moWant(t, "the diary read", DecideMortton(moSnap(0, MtTown, moHold(ObjMtDiary, 1))), StepCustom, CustomMtReadDiary)
	moPark(t, "a shelf that gave no diary", DecideMortton(moSnap(0, MtTown, moFail(CustomMtTakeDiary))), MtStuckParkFor, "diary")
	moPark(t, "a diary that would not read", DecideMortton(moSnap(0, MtTown, moHold(ObjMtDiary, 1), moFail(CustomMtReadDiary))), MtStuckParkFor, "diary")
	moWant(t, "a full pack", DecideMortton(moSnap(0, MtTown, moFree(0))), StepCustom, CustomMtDropJunk)
	moPark(t, "a full pack that cannot be emptied", DecideMortton(moSnap(0, MtTown, moFree(0), moFail(CustomMtDropJunk))), MtStuckParkFor, "room")
	// A tinderbox gone, or the ashes and the logs for them, is fetched from Varrock while the crossing is still behind the account: the town sends it back.
	if got := DecideMortton(moSnap(0, MtTown, moHold(ObjTinderbox, 0))); got.Kind != StepProvision && got.Kind != StepWalk {
		t.Errorf("no tinderbox in the town: %+v", got)
	}
	// Stage 5: the table's herbs, one tarromin identified, the vial, the water, the unfinished potion, the ashes: serum 207, twice.
	type c struct {
		name   string
		mods   []func(*Snapshot)
		custom string
	}
	for _, x := range []c{
		{"nothing made: the table", nil, CustomMtHerbs},
		{"an unidentified tarromin: identified", []func(*Snapshot){moHold(ObjMtUnidTarromin, 2)}, CustomMtIdentify},
		{"a tarromin: a vial from the floor", []func(*Snapshot){moHold(ObjMtTarromin, 1), moHold(ObjMtUnidTarromin, 1)}, CustomMtVial},
		{"a tarromin and a vial: the sink", []func(*Snapshot){moHold(ObjMtTarromin, 1), moHold(ObjMtVial, 1)}, CustomMtWater},
		{"a tarromin and a vial of water: mixed", []func(*Snapshot){moHold(ObjMtTarromin, 1), moHold(ObjMtVialWater, 1)}, CustomMtMixUnf},
		{"an unfinished potion and ashes: serum", []func(*Snapshot){moHold(ObjMtUnfTarromin, 1)}, CustomMtMixSerum},
		{"an unfinished potion and no ashes: burnt", []func(*Snapshot){moHold(ObjMtUnfTarromin, 1), moHold(ObjAshes, 0)}, CustomMtBurn},
	} {
		moWant(t, x.name, DecideMortton(moSnap(MtReadDiary, MtTown, x.mods...)), StepCustom, x.custom)
	}
	moPark(t, "no ashes, no logs", DecideMortton(moSnap(MtReadDiary, MtTown, moHold(ObjMtUnfTarromin, 1), moHold(ObjAshes, 0), moHold(ObjMtLogs, 0))), MtItemParkFor, "ashes")
	// A step that failed once is not repeated: the quest parks naming it.
	for _, f := range []struct {
		name   string
		mods   []func(*Snapshot)
		custom string
		want   string
	}{
		{"herbs", nil, CustomMtHerbs, "table"},
		{"identify", []func(*Snapshot){moHold(ObjMtUnidTarromin, 1)}, CustomMtIdentify, "identified"},
		{"vial", []func(*Snapshot){moHold(ObjMtTarromin, 1)}, CustomMtVial, "vial"},
		{"water", []func(*Snapshot){moHold(ObjMtTarromin, 1), moHold(ObjMtVial, 1)}, CustomMtWater, "sink"},
		{"mixunf", []func(*Snapshot){moHold(ObjMtTarromin, 1), moHold(ObjMtVialWater, 1)}, CustomMtMixUnf, "vial of water"},
		{"mixserum", []func(*Snapshot){moHold(ObjMtUnfTarromin, 1)}, CustomMtMixSerum, "ashes"},
	} {
		moPark(t, "a failed "+f.name, DecideMortton(moSnap(MtReadDiary, MtTown, append(append([]func(*Snapshot){}, f.mods...), moFail(f.custom))...)), MtStuckParkFor, f.want)
	}
	// Two serums are brewed (six doses cover the four a run spends and the spare for the oil or the material), and none after: serum in hand, the junk is dropped.
	one := moSnap(MtMadeSerum, MtTown, moSerum(3), moHold(ObjMtUnidTarromin, 1))
	moWant(t, "one serum, one tarromin left", DecideMortton(one), StepCustom, CustomMtIdentify)
	two := moSnap(MtMadeSerum, MtTown, moSerum(6), moHold(ObjMtUnidTarromin, 1))
	moWant(t, "two serums: the spare herb is kept, Razmire is next", DecideMortton(two), StepCustom, CustomMtSend)
	moWant(t, "a junk drop that did nothing is not asked for again", DecideMortton(moSnap(MtMadeSerum, MtTown, moSerum(6), moHold(ObjMtDiary, 1), moFail(CustomMtDropJunk))), StepCustom, CustomMtSend)
	moWant(t, "two serums and the diary", DecideMortton(moSnap(MtMadeSerum, MtTown, moSerum(6), moHold(ObjMtDiary, 1))), StepCustom, CustomMtDropJunk)
	moWant(t, "two serums and rotten food", DecideMortton(moSnap(MtMadeSerum, MtTown, moSerum(6), moHold(ObjMtRottenFood, 1))), StepCustom, CustomMtDropJunk)
	moWant(t, "serum in hand: Razmire", DecideMortton(moSnap(MtMadeSerum, MtTown, moSerum(6))), StepCustom, CustomMtSend)
	moWant(t, "stage 5 with serum in hand: Razmire", DecideMortton(moSnap(MtReadDiary, MtTown, moSerum(6))), StepCustom, CustomMtSend)
	moPark(t, "no serum at all", DecideMortton(moSnap(MtMadeSerum, MtTown, moSerum(0), moHold(ObjMtUnidTarromin, 0), func(s *Snapshot) { s.Failed[CustomMtHerbs] = true })), MtStuckParkFor, "table")
	moPark(t, "Razmire's send-off failed", DecideMortton(moSnap(MtMadeSerum, MtTown, moSerum(6), moFail(CustomMtSend))), MtStuckParkFor, "did not go through")
}

// Stages 15 to 40: five shades are killed on a model that prices two at once, the hunt is given up on when it fails, and Razmire takes the remains.
func TestMorttonDecideTheShades(t *testing.T) {
	for _, stage := range []int{MtKillShades, MtKilled1, MtKilled2, MtKilled3, MtKilled4} {
		moWant(t, "stage hunt", DecideMortton(moSnap(stage, MtTown, moSerum(6))), StepCustom, CustomMtHunt)
	}
	// Five killed and fewer than five sets of remains in the pack (one is dropped by a kill that was not picked up): the hunt goes on.
	moWant(t, "four sets of remains at 40", DecideMortton(moSnap(MtKilled5, MtTown, moSerum(6), moHold(ObjMtRemains, 4))), StepCustom, CustomMtHunt)
	moWant(t, "five sets of remains at 40", DecideMortton(moSnap(MtKilled5, MtTown, moSerum(6), moHold(ObjMtRemains, 5))), StepCustom, CustomMtHandOver)
	moPark(t, "a failed hunt", DecideMortton(moSnap(MtKilled2, MtTown, moSerum(6), moFail(CustomMtHunt))), MtStuckParkFor, "shade")
	moPark(t, "a failed hand-over", DecideMortton(moSnap(MtKilled5, MtTown, moSerum(6), moHold(ObjMtRemains, 5), moFail(CustomMtHandOver))), MtStuckParkFor, "did not go through")
	// Short of the doses the rest of the run spends, the account goes back to the table for the herbs (once per account), and the quest parks when it gave none.
	moWant(t, "short of serum for the hand-over", DecideMortton(moSnap(MtKilled5, MtTown, moHold(ObjMtRemains, 5))), StepCustom, CustomMtHerbs)
	moPark(t, "no serum, no herbs", DecideMortton(moSnap(MtKilled5, MtTown, moHold(ObjMtRemains, 5), moFail(CustomMtHerbs))), MtStuckParkFor, "table")
	// An account the model says loses to two shades is parked, naming the shades; so is one with no food.
	weak := moSnap(MtKillShades, MtTown, moSerum(6), moLevel("attack", 15), moLevel("strength", 15), moLevel("defence", 10), func(s *Snapshot) { s.MaxHP, s.HP = 30, 30; s.Levels["hitpoints"] = 30 })
	moPark(t, "a weak account", DecideMortton(weak), MtFightParkFor, "shade")
	moPark(t, "no food", DecideMortton(moSnap(MtKilled1, MtTown, moSerum(6), moHold(ObjShark, 0))), MtFightParkFor, "no food")
}

// Stages 45 to 50: the shop, Ulsquire's two talks and the hammer; every purchase is asked for while the pack is short of it.
func TestMorttonDecideTheShopsAndUlsquire(t *testing.T) {
	// 45: the hammer first, then the material, then Ulsquire is shown the remains.
	hammerShort := DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), moHold(ObjMtRemains, 3)))
	moWant(t, "no hammer at 45", hammerShort, StepCustom, CustomMtGeneral)
	if hammerShort.ItemID != ObjHammer {
		t.Errorf("the general store's item %d, want the hammer %d", hammerShort.ItemID, ObjHammer)
	}
	material := DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), moHold(ObjHammer, 1), moHold(ObjMtRemains, 3)))
	moWant(t, "no material at 45", material, StepCustom, CustomMtBuilders)
	if material.Qty != MtSetsThatFit(14) || material.Qty != 5 {
		t.Errorf("the material's sets %d, want %d (what 14 free slots take)", material.Qty, MtSetsThatFit(14))
	}
	if q := DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), moHold(ObjHammer, 1), moHold(ObjMtRemains, 3), moFree(4))); q.Qty != 1 {
		t.Errorf("four free slots take %d sets, want 1", q.Qty)
	}
	if got := DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), moHold(ObjHammer, 1), moHold(ObjMtRemains, 3), moFree(2))); got.Kind != StepWait || got.ParkFor != 0 {
		t.Errorf("two free slots: %+v", got)
	}
	stocked := func(s *Snapshot) {
		moBuild(s)
		s.InvIDs[ObjHammer] = 1
		s.InvIDs[ObjMtOil3] = 1
		s.InvIDs[ObjMtRemains] = 3
	}
	moWant(t, "stocked: Ulsquire", DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), stocked)), StepCustom, CustomMtShow)
	moWant(t, "stage 47: the temple talk", DecideMortton(moSnap(MtShadesToUlsquire, MtTown, moSerum(6), stocked)), StepCustom, CustomMtTempleTlk)
	moPark(t, "no builders' store", DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), moHold(ObjHammer, 1), moFail(CustomMtBuilders))), MtStuckParkFor, "building material")
	moPark(t, "no hammer", DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), moFail(CustomMtGeneral))), MtStuckParkFor, "hammer")
	moPark(t, "a show that failed", DecideMortton(moSnap(MtShadesToRazmire, MtTown, moSerum(6), stocked, moFail(CustomMtShow))), MtStuckParkFor, "did not go through")
	moPark(t, "no serum, no herbs, at Ulsquire", DecideMortton(moSnap(MtShadesToUlsquire, MtTown, stocked, moFail(CustomMtHerbs))), MtStuckParkFor, "table")
}

// Stages 50 to 60: the temple is built by one executor that restocks for itself, so no purchase is asked for once the first wall has gone up; the altar follows.
func TestMorttonDecideTheTempleAndTheAltar(t *testing.T) {
	ready := func(s *Snapshot) { s.InvIDs[ObjHammer] = 1; s.InvIDs[ObjMtOil3] = 1; moBuild(s) }
	moWant(t, "temple at 50", DecideMortton(moSnap(MtUlsquireTemple, MtTown, moSerum(6), ready)), StepCustom, CustomMtTemple)
	// Stage 55 with an empty pack (the pool holds the material): the temple again, never a purchase.
	empty := func(s *Snapshot) { s.InvIDs[ObjHammer] = 1; s.InvIDs[ObjMtOil3] = 1 }
	moWant(t, "temple at 55, pack empty", DecideMortton(moSnap(MtRebuildTemple, MtTown, moSerum(6), empty)), StepCustom, CustomMtTemple)
	moWant(t, "temple at 50, pack empty: the material first", DecideMortton(moSnap(MtUlsquireTemple, MtTown, moSerum(6), empty)), StepCustom, CustomMtBuilders)
	moPark(t, "a temple that failed", DecideMortton(moSnap(MtRebuildTemple, MtTown, moSerum(6), empty, moFail(CustomMtTemple))), MtStuckParkFor, "temple")
	// The hammer is the temple's tool and is bought at any stage up to the altar.
	moWant(t, "no hammer at 55", DecideMortton(moSnap(MtRebuildTemple, MtTown, moSerum(6))), StepCustom, CustomMtGeneral)
	// The shades are priced for the temple as for the hunt.
	weak := moSnap(MtRebuildTemple, MtTown, moSerum(6), empty, moLevel("attack", 15), moLevel("strength", 15), moLevel("defence", 10), func(s *Snapshot) { s.MaxHP, s.HP = 30, 30 })
	moPark(t, "a weak account at the temple", DecideMortton(weak), MtFightParkFor, "temple")
	// Stage 60: the altar; oil is bought when the pack has none.
	moWant(t, "altar at 60", DecideMortton(moSnap(MtCanLightAltar, MtTown, moSerum(6), empty)), StepCustom, CustomMtAltar)
	noOil := DecideMortton(moSnap(MtCanLightAltar, MtTown, moSerum(6), func(s *Snapshot) { s.InvIDs[ObjHammer] = 1 }))
	moWant(t, "no oil at 60", noOil, StepCustom, CustomMtGeneral)
	if noOil.ItemID != ObjMtOil3 {
		t.Errorf("the oil bought is %d, want the three-dose flask %d", noOil.ItemID, ObjMtOil3)
	}
	moPark(t, "an altar that failed", DecideMortton(moSnap(MtCanLightAltar, MtTown, moSerum(6), empty, moFail(CustomMtAltar))), MtStuckParkFor, "altar")
}

// Stages 65 to 80: the sacred oil, the pyre logs, and the cremation with its Firemaking.
func TestMorttonDecideThePyre(t *testing.T) {
	sacred := func(s *Snapshot) { s.InvIDs[ObjMtSacred3] = 1 }
	logs := func(s *Snapshot) { s.InvIDs[ObjMtLogs] = 2 }
	remains := func(s *Snapshot) { s.InvIDs[ObjMtRemains] = 3 }
	moWant(t, "pyre logs at 65", DecideMortton(moSnap(MtSacredOil, MtTown, moSerum(6), sacred, logs, remains)), StepCustom, CustomMtPyreLogs)
	moPark(t, "no log at 65", DecideMortton(moSnap(MtSacredOil, MtTown, moSerum(6), sacred, moHold(ObjMtLogs, 0), remains)), MtItemParkFor, "log")
	moWant(t, "olive oil, no sacred oil, at 65: the altar", DecideMortton(moSnap(MtSacredOil, MtTown, moSerum(6), func(s *Snapshot) { s.InvIDs[ObjMtOil3] = 1 }, logs, remains, moHold(ObjHammer, 1))), StepCustom, CustomMtAltar)
	moPark(t, "pyre logs that failed", DecideMortton(moSnap(MtSacredOil, MtTown, moSerum(6), sacred, logs, remains, moFail(CustomMtPyreLogs))), MtStuckParkFor, "pyre log")
	pyreLog := func(s *Snapshot) { s.InvIDs[ObjMtPyreLogs] = 1 }
	moWant(t, "the cremation at 70", DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog, remains)), StepCustom, CustomMtCremate)
	moWant(t, "the cremation at 75", DecideMortton(moSnap(MtLogsOnPyre, MtTown, moSerum(6), pyreLog, remains)), StepCustom, CustomMtCremate)
	// A pyre that went out takes the pyre log with it: another is made while the oil lasts, and the quest parks when it does not.
	moWant(t, "a lost pyre log with oil", DecideMortton(moSnap(MtLogsOnPyre, MtTown, moSerum(6), sacred, logs, remains)), StepCustom, CustomMtPyreLogs)
	moPark(t, "a lost pyre log and no oil", DecideMortton(moSnap(MtLogsOnPyre, MtTown, moSerum(6), logs, remains)), MtPyreParkFor, "went out")
	moPark(t, "no remains", DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog)), MtItemParkFor, "remains")
	moPark(t, "no tinderbox", DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog, remains, moHold(ObjTinderbox, 0))), MtItemParkFor, "tinderbox")
	moPark(t, "a cremation that failed", DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog, remains, moFail(CustomMtCremate))), MtStuckParkFor, "cremated")
	// The pyre wants Firemaking 5: an account under it burns its spare logs first, and parks when it has none.
	low := DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog, remains, moLevel("firemaking", 3), moHold(ObjMtLogs, 5)))
	moWant(t, "Firemaking 3 with logs", low, StepCustom, CustomMtBurn)
	if low.Qty != 5 || low.Radius != 0 {
		t.Errorf("burn %d logs for %d ashes, want 5 and none", low.Qty, low.Radius)
	}
	moPark(t, "Firemaking 3, no logs", DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog, remains, moLevel("firemaking", 3), moHold(ObjMtLogs, 0))), MtSkillParkFor, "Firemaking 5")
	moPark(t, "a burn that failed", DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog, remains, moLevel("firemaking", 3), moHold(ObjMtLogs, 5), moFail(CustomMtBurn))), MtStuckParkFor, "burnt")
	// An unread Firemaking level is not judged.
	moWant(t, "an unread Firemaking level", DecideMortton(moSnap(MtPyreLogs, MtTown, moSerum(6), pyreLog, remains, func(s *Snapshot) { delete(s.Levels, "firemaking") })), StepCustom, CustomMtCremate)
	// 80: Ulsquire is told, on a dose of serum.
	moWant(t, "the finish", DecideMortton(moSnap(MtLitPyre, MtTown, moSerum(3))), StepCustom, CustomMtFinish)
	moPark(t, "no serum for the finish", DecideMortton(moSnap(MtLitPyre, MtTown, moFail(CustomMtHerbs))), MtStuckParkFor, "table")
	moPark(t, "a finish that failed", DecideMortton(moSnap(MtLitPyre, MtTown, moSerum(3), moFail(CustomMtFinish))), MtStuckParkFor, "did not go through")
}

// Every step of the quest the plan can return is well formed, and every custom name is in the module's death-cap table only if the module names it.
func TestMorttonStepsAreWellFormed(t *testing.T) {
	names := []string{CustomMtBurn, CustomMtTakeDiary, CustomMtReadDiary, CustomMtHerbs, CustomMtIdentify, CustomMtVial, CustomMtWater, CustomMtMixUnf, CustomMtMixSerum, CustomMtDropJunk,
		CustomMtSend, CustomMtHunt, CustomMtHandOver, CustomMtGeneral, CustomMtBuilders, CustomMtShow, CustomMtTempleTlk, CustomMtTemple, CustomMtAltar, CustomMtPyreLogs,
		CustomMtCremate, CustomMtFinish}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] || !strings.HasPrefix(n, "mt-") {
			t.Errorf("custom name %q is repeated or not prefixed", n)
		}
		seen[n] = true
	}
	for name := range MorttonShades.StepDeathCaps {
		if !seen[name] {
			t.Errorf("death cap for %q, which is no custom step of the quest", name)
		}
	}
	if MorttonShades.MaxDeaths != 2 || MorttonShades.Decide == nil || MorttonShades.ReadStage == nil {
		t.Errorf("module %+v", MorttonShades)
	}
	// A walk through every tile class: outside, swamp, town.
	for _, at := range []tile.Tile{moVarrock, moSwamp, MtTown} {
		for stage := 0; stage < MtComplete; stage += 5 {
			if st := DecideMortton(moSnap(stage, at, moSerum(6))); st.Kind == "" {
				t.Errorf("stage %d at %v: a step with no kind", stage, at)
			} else if why := ValidateStep(st); why != "" {
				t.Errorf("stage %d at %v: %+v: %s", stage, at, st, why)
			}
		}
	}
}

// The serum arithmetic: the doses a run spends, the vials still to brew (never more than the table's two tarromin allow), the ashes short, the logs the crossing is stocked with.
func TestMorttonDoseArithmetic(t *testing.T) {
	for _, c := range []struct{ stage, want int }{{0, 4}, {5, 4}, {10, 4}, {15, 3}, {40, 3}, {45, 2}, {47, 2}, {50, 1}, {60, 1}, {80, 1}} {
		if got := MtDosesNeeded(c.stage); got != c.want {
			t.Errorf("stage %d: %d doses, want %d", c.stage, got, c.want)
		}
	}
	for _, c := range []struct {
		stage, doses, short, ashes int
		ashesHeld                  int
	}{{0, 0, 2, 2, 0}, {0, 3, 1, 1, 0}, {0, 6, 0, 0, 0}, {10, 3, 1, 0, 1}, {15, 3, 0, 0, 0}, {15, 0, 1, 1, 0}, {50, 0, 1, 1, 0}} {
		s := moSnap(c.stage, MtTown, moSerum(c.doses), moHold(ObjAshes, c.ashesHeld))
		if got := MtSerumsShort(s, c.stage); got != c.short {
			t.Errorf("stage %d with %d doses: %d serums short, want %d", c.stage, c.doses, got, c.short)
		}
		if got := MtAshesShort(s, c.stage); got != c.ashes {
			t.Errorf("stage %d with %d doses and %d ashes: %d short, want %d", c.stage, c.doses, c.ashesHeld, got, c.ashes)
		}
		if got := MtDoses(s); got != c.doses {
			t.Errorf("the pack's doses %d, want %d", got, c.doses)
		}
	}
	// Permanent serum and sanctified oil count as doses of their kind.
	perm := moSnap(0, MtTown, func(s *Snapshot) {
		s.InvIDs[ObjMtPerm4] = 1
		s.InvIDs[ObjMtSerum1] = 1
		s.InvIDs[ObjMtSacred2] = 1
		s.InvIDs[ObjMtOil4] = 1
	})
	if MtDoses(perm) != 5 || MtSacredDoses(perm) != 2 || MtOliveDoses(perm) != 4 {
		t.Errorf("doses %d serum, %d sacred, %d olive", MtDoses(perm), MtSacredDoses(perm), MtOliveDoses(perm))
	}
	// Firemaking: the burns before the pyre can be laid, from a level's first experience (levels 2, 3 and 4: 83, 174, 276; 5 is 388; the altar and the pyre log give 180).
	for _, c := range []struct{ level, burns int }{{1, 6}, {2, 4}, {3, 1}, {4, 0}, {5, 0}, {20, 0}} {
		if got := MtFiremakingBurns(c.level); got != c.burns {
			t.Errorf("Firemaking %d: %d burns, want %d", c.level, got, c.burns)
		}
	}
	// The logs the crossing is stocked with: the ashes' burns, the pyre's own log and the experience's burns.
	for _, c := range []struct{ level, doses, lo, hi int }{{5, 0, 3, 3}, {5, 6, 1, 1}, {1, 0, 3, 8}, {3, 0, 3, 3}, {2, 6, 1, 6}} {
		s := moSnap(0, moVarrock, moSerum(c.doses), moLevel("firemaking", c.level))
		if lo, hi := MtLogsWanted(s, 0); lo != c.lo || hi != c.hi {
			t.Errorf("Firemaking %d with %d doses: logs %d..%d, want %d..%d", c.level, c.doses, lo, hi, c.lo, c.hi)
		}
	}
	// Sets of material that fit: a plank and a brick each and a slot for the paste.
	for _, c := range []struct{ free, want int }{{0, 0}, {2, 0}, {3, 1}, {4, 1}, {5, 2}, {11, 5}, {28, 5}} {
		if got := MtSetsThatFit(c.free); got != c.want {
			t.Errorf("%d free slots take %d sets, want %d", c.free, got, c.want)
		}
	}
}

// The kit and its keep list: coins to the stage where the shopping is over, a tinderbox, logs, ashes and food; every line is well formed and the keep list carries the tools.
func TestMorttonKitAndKeepList(t *testing.T) {
	for _, stage := range []int{0, 10, 45, 60, 80} {
		for _, at := range []tile.Tile{moVarrock, MtTown} {
			s := moSnap(stage, at, func(s *Snapshot) { s.InvIDs = map[int]int{} })
			for _, l := range MtKitLines(s, stage) {
				if why := ValidateKitLine(l); why != "" {
					t.Errorf("stage %d: kit line %+v: %s", stage, l, why)
				}
			}
		}
	}
	kit := func(stage int, mods ...func(*Snapshot)) map[int]KitLine {
		out := map[int]KitLine{}
		for _, l := range MtKitLines(moSnap(stage, moVarrock, mods...), stage) {
			out[l.ID] = l
		}
		return out
	}
	bare := func(s *Snapshot) { s.InvIDs = map[int]int{} }
	if l, ok := kit(0, bare)[CoinsID]; !ok || l.Qty != MtCoins || l.Min != MtCoinsMin {
		t.Errorf("coins at the start: %+v", l)
	}
	if l, ok := kit(MtCanLightAltar, bare)[CoinsID]; !ok || l.Min != MtCoinsLow {
		t.Errorf("coins at the altar: %+v (the shopping is almost over)", l)
	}
	if _, ok := kit(MtLitPyre, bare)[CoinsID]; ok {
		t.Error("coins asked for once the pyre is lit")
	}
	if _, ok := kit(0)[CoinsID]; ok {
		t.Error("coins asked for with 5,000 in the pack")
	}
	for _, id := range []int{ObjMtLogs, ObjAshes, ObjShark} {
		if l, ok := kit(0, bare)[id]; ok && id != ObjMtLogs && !l.Optional {
			t.Errorf("line %+v should be optional", l)
		}
	}
	keep := MtKeepIDs()
	for _, id := range []int{ObjMtDiary, ObjMtTarromin, ObjMtUnfTarromin, ObjMtVialWater, ObjMtVial, ObjMtRemains, ObjMtBrick, ObjMtPlank, ObjMtPaste, ObjMtPyreLogs, ObjMtLogs, ObjTinderbox,
		ObjHammer, ObjAshes, CoinsID, ObjMtSerum3, ObjMtPerm3, ObjMtOil3, ObjMtSacred3, ObjShark, ObjMtUnidTarromin} {
		if !slices.Contains(keep, id) {
			t.Errorf("the keep list lacks %d", id)
		}
	}
	for _, id := range MtToolIDs() {
		if !slices.Contains(MorttonShades.ToolIDs, id) || !slices.Contains(keep, id) {
			t.Errorf("tool %d is not in the module's tools or the keep list", id)
		}
	}
	// A copy is returned, not the module's own slice.
	keep[0] = -1
	if MtKeepIDs()[0] == -1 {
		t.Error("MtKeepIDs returned the module's own slice")
	}
}

// The price of the shades (two at once) and of the swamp's ghasts: a strong account is ready for both, a weak one for neither, and food decides.
func TestMorttonFightPrices(t *testing.T) {
	strong := moSnap(MtKillShades, MtTown)
	if v := MtShadesReady(strong); !v.OK {
		t.Errorf("a strong account against two shades: %s", v.Why)
	}
	if v := MtCrossingReady(moSnap(0, moVarrock)); !v.OK {
		t.Errorf("a strong account against the swamp: %s", v.Why)
	}
	weak := moSnap(MtKillShades, MtTown, moLevel("attack", 10), moLevel("strength", 10), moLevel("defence", 5), func(s *Snapshot) { s.MaxHP, s.HP = 30, 30 })
	if v := MtShadesReady(weak); v.OK || !strings.Contains(v.Why, "two Loar shades") {
		t.Errorf("a weak account against two shades: %+v", v)
	}
	nofood := moSnap(MtKillShades, MtTown, moHold(ObjShark, 0))
	if v := MtShadesReady(nofood); v.OK || !strings.Contains(v.Why, "no food") {
		t.Errorf("no food: %+v", v)
	}
	if v := MtCrossingReady(moSnap(0, moVarrock, moHold(ObjShark, 3))); v.OK || !strings.Contains(v.Why, "bites of food") {
		t.Errorf("three sharks for the crossing: %+v", v)
	}
	// The foes are the content's: level 40, 38 hitpoints, a hit of up to 4.
	if FoeLoarShade.Hitpoints != 38 || FoeLoarShade.MaxHit() != 4 || FoeLoarShadePair.Hitpoints != 76 {
		t.Errorf("shade %+v max hit %d, pair %+v", FoeLoarShade, FoeLoarShade.MaxHit(), FoeLoarShadePair)
	}
	for _, id := range []int{NpcMtShade, NpcMtShadow} {
		if f, ok := FoeByNPC(id); !ok || f != FoeLoarShade {
			t.Errorf("npc %d is not priced as the Loar shade: %+v %v", id, f, ok)
		}
	}
}

// The town's boxes: the town is a square of the map, the swamp is the zone round it, and the two never overlap.
func TestMorttonZones(t *testing.T) {
	for _, c := range []struct {
		at          tile.Tile
		town, swamp bool
	}{{MtTown, true, false}, {MtTempleInside, true, false}, {MtPyre, true, false}, {MtRazmire, true, false}, {moSwamp, false, true}, {moVarrock, false, false},
		{tile.Tile{X: 3420, Z: 3300}, false, true}, {tile.Tile{X: 3500, Z: 3400}, false, true}, {tile.Tile{X: 3500, Z: 3200}, false, false}} {
		if InMortton(c.at) != c.town || InMorttonSwamp(c.at) != c.swamp {
			t.Errorf("%v: town %v swamp %v, want %v and %v", c.at, InMortton(c.at), InMorttonSwamp(c.at), c.town, c.swamp)
		}
	}
	for _, at := range []tile.Tile{MtShelf, MtTable, MtVials, MtSink, MtSinkNear, MtRazmire, MtUlsquire, MtTown, MtTempleInside, MtTempleDoor, MtAltar, MtPyre} {
		if !InMortton(at) {
			t.Errorf("%v is outside the town's square", at)
		}
	}
	if InMortton(MtVarrockLogs) || InMorttonSwamp(MtVarrockLogs) {
		t.Errorf("the log spawns by Varrock's bank are in Mort'ton")
	}
	if !MtInTempleZone(MtAltar) || MtInTempleZone(MtTown) {
		t.Errorf("the temple zone: altar %v, town %v", MtInTempleZone(MtAltar), MtInTempleZone(MtTown))
	}
	for _, id := range []int{LocMtWallBase, LocMtWallDone, LocMtCornerBase, LocMtCornerDone} {
		if !MtIsTempleWall(id) {
			t.Errorf("loc %d is not a temple wall", id)
		}
	}
	if !MtWallRepairable(LocMtWallBase) || MtWallRepairable(LocMtWallDone) || !MtWallRepairable(LocMtCornerBase) || MtWallRepairable(LocMtCornerDone) || MtIsTempleWall(LocMtAltarLit) {
		t.Error("the wall classes are wrong")
	}
}
