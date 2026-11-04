package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Sea Slug Quest (W13, 18 Oct 2026): the journal parser over the pages the content renders, every state of Decide, and whole
// simulated quests (a world that answers every step the way the content's scripts do, region by region).

// ---------------------------------------------------------------------------------------------------------------------
// The journal
// ---------------------------------------------------------------------------------------------------------------------

// slugJournalFixtures are the pages seaslug_journal.rs2 renders, joined in order (`|` the client's line break). Two pairs read alike
// (stages 4 and 5, 8 and 9), and the parser returns the older of each.
var slugJournalFixtures = func() map[int]string {
	spoke := "@str@I have spoken to Caroline and agreed to help.||"
	gave := "@str@I gave Holgart the Swamp Paste and his boat is now|@str@ready to take me to the Fishing Platform.||"
	foundKennith := "@str@I've found Kennith, he's hiding behind some boxes.||"
	foundKent := "@str@I've found Kent on a small island.||"
	torchTalk := "@str@Kent has asked me to help Kennith escape.|@str@After speaking to Bailey, I found out that Sea Slugs are|@str@afraid of heat.|"
	damp := "@str@I should find a way of lighting this damp torch.|"
	opening := "@str@I've created an opening to let Kenneth escape.||"
	path := "@str@Kennith can't get downstairs without some help.||"
	crane := "@str@I've used the Crane to lower Kennith into the boat.||"
	wontGo := "@dre@Kennith@dbl@ won't go near the @dre@Sea Slugs@dbl@.|I need to find another way to get him out."
	return map[int]string{
		SlugNotStarted: "@dbl@I can start this quest by speaking to @dre@Caroline@dbl@ who is @dre@East of Ardougne||@dbl@Requirements:|@dbl@You'll need level 30 @dre@Firemaking",
		SlugStarted: spoke + "@dbl@I need to talk to @dre@Holgart@dbl@ He'll take me to the @dre@Fishing Platform@dbl@.||" +
			"@dbl@I need to get to the @dre@Fishing Platform@dbl@ and find out what's happened to @dre@Kent@dbl@ and @dre@Kennith@dbl@.",
		SlugSpokenHolgart: spoke + "@dbl@I've spoken to @dre@Holgart@dbl@, but his boat is broken.|He needs me to bring him some @dre@Swamp Paste@dbl@.||" +
			"@dbl@I can make @dre@Swamp Paste@dbl@ by mixing @dre@Swamp Tar@dbl@ with @dre@Flour@dbl@ and then heating the mixture on a @dre@Fire@dbl@.|" +
			"@dbl@I can find @dre@Swamp Tar@dbl@ in the @dre@Swamp South of Lumbridge@dbl@.||" +
			"@dbl@I need to get to the @dre@Fishing Platform@dbl@ and find out what's happened to @dre@Kent@dbl@ and @dre@Kennith@dbl@.",
		SlugBoatRepaired:  spoke + gave + "@dbl@I need to find @dre@Kent@dbl@ and @dre@Kennith@dbl@.",
		SlugSpokenKennith: spoke + gave + foundKennith + "@dbl@I need to find @dre@Kent.",
		SlugSpokenKent: spoke + gave + foundKennith + foundKent + "@dre@Kent@dbl@ has asked me to help @dre@Kennith@dbl@ escape.|" +
			"@dbl@After speaking to @dre@Bailey@dbl@, I found out that Sea Slugs are afraid of heat.|@dbl@I should find a way of lighting this damp torch.",
		SlugLitTorch:          spoke + gave + foundKennith + foundKent + torchTalk + damp + wontGo,
		SlugKennithNeedEscape: spoke + gave + foundKennith + foundKent + torchTalk + damp + opening + wontGo,
		SlugNeedKennithPath:   spoke + gave + foundKennith + foundKent + torchTalk + damp + opening + path + wontGo,
		SlugSavedKennith: spoke + gave + foundKennith + foundKent + torchTalk + damp + opening + path + crane +
			"@dbl@I need to take the boat back to shore and talk to @dre@Caroline@dbl@.",
		SlugComplete: spoke + gave + foundKennith + foundKent + torchTalk + damp + opening + path + crane +
			"@str@I've spoken to Caroline and she thanked me for rescuing|@str@her family from the Sea Slugs.|@red@QUEST COMPLETE!",
	}
}()

func TestParseSeaSlugJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, page := range slugJournalFixtures {
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseSeaSlugJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d page %.60q: parsed %d %v", stage, page, got, ok)
			}
		}
	}
	// Stage 2 with the paste in the pack prints another second paragraph, and still reads as stage 2.
	held := "@str@I have spoken to Caroline and agreed to help.||@dbl@I need to take the @dre@Swamp Paste@dbl@ to @dre@Holgart@dbl@ He's near @dre@Caroline@dbl@, they're @dre@East of Ardougne@dbl@.||" +
		"@dbl@I need to get to the @dre@Fishing Platform@dbl@ and find out what's happened to @dre@Kent@dbl@ and @dre@Kennith@dbl@."
	if got, ok := ParseSeaSlugJournal([]string{held}); !ok || got != SlugSpokenHolgart {
		t.Errorf("stage 2 with the paste parsed %d %v", got, ok)
	}
	// The page for 4 is the page for 5, and the page for 8 the page for 9: the older stage of each pair is what the parser can say.
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseSeaSlugJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated world
// ---------------------------------------------------------------------------------------------------------------------

// slugWorld is the Sea Slug Quest as the content runs it: %seaslugquest, where the account stands (a region and a floor), and what each
// talk, custom and use-on does.
type slugWorld struct {
	*kcWorld
	stage  int
	region SlugRegion
	level  int
	fm     int
	// rubs counts the rub steps the quest asked for.
	rubs    int
	blocked map[string]bool
}

func newSlugWorld(t *testing.T, stage int, region SlugRegion, inv, bank map[int]int) *slugWorld {
	w := &slugWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage, region: region, fm: 40, blocked: map[string]bool{}}
	w.setRegion(region)
	return w
}

func (w *slugWorld) setRegion(r SlugRegion) {
	w.region, w.level = r, 0
	switch r {
	case SlugMainland:
		w.tile = ArdougneSouthBank
	case SlugPlatform:
		w.tile = SlugPlatformArrive
	case SlugIsland:
		w.tile = SlugIslandArrive
	}
}

func (w *slugWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case SlugNotStarted:
		j = StatusNotStarted
	case SlugComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.stage, true
	s.Levels["firemaking"] = w.fm
	for k, v := range w.blocked {
		s.Blocked[k] = v
	}
	s.Tile.Level = w.level
	return s
}

func (w *slugWorld) apply(st Step) {
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
			delete(w.blocked, key)
		} else {
			w.failed[key] = true
			w.blocked[key] = true
		}
		w.log = append(w.log, key)
	case StepTalk:
		w.talk(st)
	case StepGrabGround:
		w.inv[st.ItemID]++
		w.log = append(w.log, "grab:"+st.Item)
	case StepUseOn:
		if st.ItemID != ObjBrokenGlass || st.TargetID != ObjDampSticks || w.inv[ObjBrokenGlass] == 0 || w.inv[ObjDampSticks] == 0 {
			t.Fatalf("unexpected use-on %s", st.Describe())
		}
		w.inv[ObjDampSticks]-- // the glass is not used up
		w.inv[ObjDrySticks]++
		w.log = append(w.log, "dry")
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *slugWorld) talk(st Step) {
	if SlugRegionOf(w.tile) != w.region && w.region != SlugMainland {
		w.t.Fatalf("a talk at a region the account is not in")
	}
	switch st.Stop.ID {
	case NpcCaroline:
		if w.region != SlugMainland {
			w.t.Fatalf("a talk with Caroline from the %s", w.region)
		}
		switch w.stage {
		case SlugNotStarted:
			w.stage = SlugStarted
		case SlugSavedKennith:
			w.stage = SlugComplete
		default:
			w.t.Fatalf("a talk with Caroline at stage %d", w.stage)
		}
		w.log = append(w.log, fmt.Sprintf("caroline%d", w.stage))
	case NpcHolgartLand:
		if w.region != SlugMainland {
			w.t.Fatalf("a talk with the shore's Holgart from the %s", w.region)
		}
		switch w.stage {
		case SlugStarted:
			w.stage = SlugSpokenHolgart
		case SlugSpokenHolgart:
			if w.inv[ObjSwampPaste] == 0 {
				break
			}
			w.inv[ObjSwampPaste]--
			w.stage = SlugBoatRepaired
			w.board(SlugPlatform)
		case SlugBoatRepaired, SlugSpokenKennith:
			w.board(SlugPlatform)
		case SlugSailedKent:
			w.board(SlugIsland)
		default:
			w.board(SlugPlatform)
		}
		w.log = append(w.log, fmt.Sprintf("holgart%d", w.stage))
	case NpcBailey:
		if w.region != SlugPlatform || w.level != 0 {
			w.t.Fatalf("a talk with Bailey from the %s floor %d", w.region, w.level)
		}
		if w.stage >= SlugSpokenKent && w.stage <= SlugKennithNeedEscape+1 && w.inv[ObjTorchLit]+w.inv[ObjTorchUnlit] == 0 {
			w.inv[ObjTorchUnlit]++
		}
		w.log = append(w.log, "bailey")
	default:
		w.t.Fatalf("a talk with npc %d", st.Stop.ID)
	}
}

// board is the boat: the torch goes out on the crossing from the shore (board_ardougne_to_fishing_platform).
func (w *slugWorld) board(to SlugRegion) {
	if w.region == SlugMainland && w.inv[ObjTorchLit] > 0 {
		w.inv[ObjTorchUnlit] += w.inv[ObjTorchLit]
		delete(w.inv, ObjTorchLit)
	}
	w.setRegion(to)
}

func (w *slugWorld) custom(st Step) {
	switch st.Custom {
	case CustomSlugToPlatform:
		if w.region == SlugIsland && w.stage == SlugSailedKent {
			w.t.Fatalf("the island's boat does not sail before Kent is spoken to")
		}
		w.board(SlugPlatform)
	case CustomSlugToIsland:
		w.board(SlugIsland)
		if w.stage == SlugSpokenKennith {
			w.stage = SlugSailedKent
		}
	case CustomSlugToMainland:
		w.board(SlugMainland)
	case CustomSlugDown:
		w.level = 0
	case CustomSlugKennith:
		w.level = 1
		switch w.stage {
		case SlugBoatRepaired:
			w.stage = SlugSpokenKennith
		case SlugLitTorch:
			if w.inv[ObjTorchLit] == 0 {
				w.t.Fatalf("shouting across with no lit torch")
			}
			w.stage = SlugKennithNeedEscape
		}
	case CustomSlugKent:
		if w.region != SlugIsland {
			w.t.Fatalf("Kent from the %s", w.region)
		}
		w.stage = SlugSpokenKent
	case CustomSlugRub:
		w.rubs++
		if w.inv[ObjDrySticks] == 0 || w.fm < SlugFiremaking {
			w.t.Fatalf("a rub with no dry sticks or too little Firemaking")
		}
		if w.inv[ObjTorchUnlit] > 0 {
			w.inv[ObjTorchUnlit]--
			w.inv[ObjTorchLit]++
		}
		if w.stage == SlugSpokenKent {
			w.stage = SlugLitTorch
		}
	case CustomSlugPanel:
		w.level = 1
		if w.inv[ObjTorchLit] == 0 {
			w.t.Fatalf("up the ladder with no lit torch")
		}
		if w.stage == SlugKennithNeedEscape {
			w.stage = SlugPanelOpened
		}
		if w.stage == SlugPanelOpened {
			w.stage = SlugNeedKennithPath
		}
	case CustomSlugCrane:
		w.level = 1
		if w.stage == SlugNeedKennithPath {
			w.stage = SlugSavedKennith
		}
	default:
		w.t.Fatalf("unexpected custom %q", st.Custom)
	}
	w.log = append(w.log, st.Custom)
}

func (w *slugWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 80; i++ {
		st := DecideSeaSlug(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.region, w.level)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.region, w.level) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("80 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// ---------------------------------------------------------------------------------------------------------------------
// Whole quests
// ---------------------------------------------------------------------------------------------------------------------

// A fresh account on the shore does the whole quest in the content's order: the paste from the shop, the platform, Kennith, the island,
// Kent, the torch (Bailey's, a stick and a piece of glass off the deck), Kennith again, the panel, the crane, home and Caroline.
func TestAFreshAccountDoesTheSeaSlugQuestInOrder(t *testing.T) {
	w := newSlugWorld(t, SlugNotStarted, SlugMainland, map[int]int{CoinsID: 200}, nil)
	w.shop[ObjSwampPaste] = true
	w.bankKnown = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"caroline1", "holgart2", "provision:" + KitSlugPaste, "holgart3", CustomSlugKennith, CustomSlugToIsland, CustomSlugKent, CustomSlugToPlatform,
		"bailey", "grab:Damp sticks", "grab:Broken glass", "dry", CustomSlugRub, CustomSlugKennith, CustomSlugPanel, CustomSlugCrane,
		CustomSlugToMainland, "caroline12"}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.rubs != 1 {
		t.Errorf("%d rubs", w.rubs)
	}
}

// An account that holds the paste already hands it over and is taken out to the platform in one talk.
func TestAnAccountWithThePasteIsBoardedByHandingItOver(t *testing.T) {
	w := newSlugWorld(t, SlugSpokenHolgart, SlugMainland, map[int]int{ObjSwampPaste: 1}, nil)
	st := DecideSeaSlug(w.snapshot())
	if st.Kind != StepTalk || st.Stop.ID != NpcHolgartLand {
		t.Fatalf("with the paste the quest decided %s", st.Describe())
	}
	if !slices.Contains(st.Stop.Prefer, "Okay, lets do it.") || !slices.Contains(st.Stop.Prefer, "Will you take me there?") {
		t.Errorf("the shore stop prefers %v: it must pick the boarding line of both menus", st.Stop.Prefer)
	}
}

// No paste to be had parks the quest for the item park, with the reason said.
func TestNoPasteParksTheQuest(t *testing.T) {
	w := newSlugWorld(t, SlugSpokenHolgart, SlugMainland, map[int]int{CoinsID: 200}, nil)
	w.bankKnown = true
	why, park := w.run()
	if !strings.Contains(why, "no swamp paste") || park != SlugParkFor {
		t.Fatalf("waited %q for %s (log %v)", why, park, w.log)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Every state of Decide
// ---------------------------------------------------------------------------------------------------------------------

func TestSeaSlugDecideOutsideTheJournalStages(t *testing.T) {
	w := newSlugWorld(t, SlugNotStarted, SlugMainland, nil, nil)
	if st := DecideSeaSlug(w.snapshot()); st.Kind != StepTalk || st.Stop.ID != NpcCaroline {
		t.Errorf("not started decided %s", st.Describe())
	}
	// Not started, standing on the platform: out first (a talk there is a walk to Witchaven that never arrives).
	w = newSlugWorld(t, SlugNotStarted, SlugPlatform, nil, nil)
	if st := DecideSeaSlug(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomSlugToMainland {
		t.Errorf("not started on the platform decided %s", st.Describe())
	}
	s := newSlugWorld(t, SlugNotStarted, SlugMainland, nil, nil).snapshot()
	s.Journal = StatusUnknown
	if st := DecideSeaSlug(s); st.Kind != StepWait || strings.Contains(st.Reason, "start") {
		t.Errorf("an uncoloured tab decided %s", st.Describe())
	}
	if st := DecideSeaSlug(newSlugWorld(t, SlugComplete, SlugMainland, nil, nil).snapshot()); st.Kind != StepDone {
		t.Errorf("complete decided %s", st.Describe())
	}
	s = newSlugWorld(t, SlugStarted, SlugMainland, nil, nil).snapshot()
	s.StageKnown = false
	if st := DecideSeaSlug(s); st.Kind != StepWait {
		t.Errorf("an unread stage decided %s", st.Describe())
	}
	s = newSlugWorld(t, SlugStarted, SlugMainland, nil, nil).snapshot()
	s.TileKnown = false
	if st := DecideSeaSlug(s); st.Kind != StepWait {
		t.Errorf("an unread position decided %s: the region is the whole decision", st.Describe())
	}
	s = newSlugWorld(t, SlugNotStarted, SlugMainland, nil, nil).snapshot()
	s.Journal = StatusInProgress
	if st := DecideSeaSlug(s); st.Kind != StepWait || !strings.Contains(st.Reason, "disagree") {
		t.Errorf("a started tab over stage 0 decided %s", st.Describe())
	}
}

// Each stage, in each region, and what the quest asks for there.
func TestSeaSlugDecideEveryStageInEveryRegion(t *testing.T) {
	const (
		talkCaroline = "talk Caroline"
		talkHolgart  = "talk Holgart"
	)
	lit := map[int]int{ObjTorchLit: 1}
	unlit := map[int]int{ObjTorchUnlit: 1}
	cases := []struct {
		name   string
		stage  int
		region SlugRegion
		level  int
		inv    map[int]int
		want   string
	}{
		{"1 on the shore", SlugStarted, SlugMainland, 0, nil, talkHolgart},
		{"1 on the platform", SlugStarted, SlugPlatform, 0, nil, CustomSlugToMainland},
		{"2 with the paste on the shore", SlugSpokenHolgart, SlugMainland, 0, map[int]int{ObjSwampPaste: 1}, talkHolgart},
		{"2 without it on the shore", SlugSpokenHolgart, SlugMainland, 0, nil, "provision"},
		{"3 on the shore", SlugBoatRepaired, SlugMainland, 0, nil, talkHolgart},
		{"3 on the platform", SlugBoatRepaired, SlugPlatform, 0, nil, CustomSlugKennith},
		{"3 on the upper deck", SlugBoatRepaired, SlugPlatform, 1, nil, CustomSlugKennith},
		{"3 on the island", SlugBoatRepaired, SlugIsland, 0, nil, CustomSlugToPlatform},
		{"4 on the shore", SlugSpokenKennith, SlugMainland, 0, nil, talkHolgart},
		{"4 on the platform", SlugSpokenKennith, SlugPlatform, 0, nil, CustomSlugToIsland},
		{"4 on the island", SlugSpokenKennith, SlugIsland, 0, nil, CustomSlugKent},
		{"6 on the island", SlugSpokenKent, SlugIsland, 0, nil, CustomSlugToPlatform},
		{"6 on the shore", SlugSpokenKent, SlugMainland, 0, nil, CustomSlugToPlatform},
		{"6 on the upper deck", SlugSpokenKent, SlugPlatform, 1, nil, CustomSlugDown},
		{"6 with no torch", SlugSpokenKent, SlugPlatform, 0, nil, talkBailey},
		{"6 with an unlit torch and no sticks", SlugSpokenKent, SlugPlatform, 0, unlit, "grab Damp sticks"},
		{"6 with sticks and no glass", SlugSpokenKent, SlugPlatform, 0, map[int]int{ObjTorchUnlit: 1, ObjDampSticks: 1}, "grab Broken glass"},
		{"6 with sticks and glass", SlugSpokenKent, SlugPlatform, 0, map[int]int{ObjTorchUnlit: 1, ObjDampSticks: 1, ObjBrokenGlass: 1}, "use Broken glass"},
		{"6 with dry sticks", SlugSpokenKent, SlugPlatform, 0, map[int]int{ObjTorchUnlit: 1, ObjDrySticks: 1}, CustomSlugRub},
		{"7 lit, on the platform", SlugLitTorch, SlugPlatform, 0, lit, CustomSlugKennith},
		{"7 lit, on the shore", SlugLitTorch, SlugMainland, 0, lit, CustomSlugToPlatform},
		{"7 with the torch gone", SlugLitTorch, SlugPlatform, 0, nil, talkBailey},
		{"7 with the torch out", SlugLitTorch, SlugPlatform, 0, unlit, "grab Damp sticks"},
		{"8 lit", SlugKennithNeedEscape, SlugPlatform, 0, lit, CustomSlugPanel},
		{"9 lit", SlugPanelOpened, SlugPlatform, 1, lit, CustomSlugPanel},
		{"10 lit", SlugNeedKennithPath, SlugPlatform, 1, lit, CustomSlugCrane},
		{"10 lit on the shore", SlugNeedKennithPath, SlugMainland, 0, lit, CustomSlugToPlatform},
		{"11 on the platform", SlugSavedKennith, SlugPlatform, 1, lit, CustomSlugToMainland},
		{"11 on the island", SlugSavedKennith, SlugIsland, 0, nil, CustomSlugToMainland},
		{"11 on the shore", SlugSavedKennith, SlugMainland, 0, nil, talkCaroline},
	}
	for _, c := range cases {
		w := newSlugWorld(t, c.stage, c.region, c.inv, nil)
		w.level = c.level
		w.bankKnown = true
		st := DecideSeaSlug(w.snapshot())
		got := st.Custom
		switch st.Kind {
		case StepTalk:
			got = "talk " + strings.TrimSuffix(st.Stop.Npc, " ")
			if st.Stop.ID == NpcHolgartLand {
				got = talkHolgart
			}
		case StepProvision:
			got = "provision"
		case StepGrabGround:
			got = "grab " + st.Item
		case StepUseOn:
			got = "use " + st.Item
		case StepWait:
			got = "wait: " + st.Reason
		}
		if got != c.want {
			t.Errorf("%s: decided %q (%s), want %q", c.name, got, st.Describe(), c.want)
		}
	}
}

const talkBailey = "talk Bailey"

// A torch lost past the panel is not Bailey's to hand back; and below Firemaking 30 the sticks never catch, so nothing is fetched.
func TestTheTorchStepParksWhereNothingCanMendIt(t *testing.T) {
	w := newSlugWorld(t, SlugNeedKennithPath, SlugPlatform, nil, nil)
	st := DecideSeaSlug(w.snapshot())
	if st.Kind != StepWait || st.ParkFor != SlugParkFor || !strings.Contains(st.Reason, "no torch") {
		t.Fatalf("a lost torch at stage 10 decided %s (park %s)", st.Describe(), st.ParkFor)
	}
	w = newSlugWorld(t, SlugSpokenKent, SlugPlatform, nil, nil)
	w.fm = 29
	st = DecideSeaSlug(w.snapshot())
	if st.Kind != StepWait || st.ParkFor != SlugParkFor || !strings.Contains(st.Reason, "Firemaking 29") {
		t.Fatalf("Firemaking 29 decided %s (park %s)", st.Describe(), st.ParkFor)
	}
	// The record asks for the level too, so the ladder never starts the quest below it.
	for _, c := range []struct {
		fm int
		ok bool
	}{{29, false}, {30, true}, {99, true}} {
		ok := true
		for _, r := range CheckRequirements(SeaSlugRecord, PlayerState{SkillLevels: map[string]int{"firemaking": c.fm}}) {
			ok = ok && r.OK
		}
		if ok != c.ok {
			t.Errorf("Firemaking %d: the record's requirements hold %v, want %v", c.fm, ok, c.ok)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Regions, the slot and the keep list
// ---------------------------------------------------------------------------------------------------------------------

func TestTheSeaSlugRegions(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want SlugRegion
	}{
		{SlugPlatformArrive, SlugPlatform}, {SlugHolgartPlatform, SlugPlatform}, {SlugBaileyAnchor, SlugPlatform}, {SlugLadderTop, SlugPlatform},
		{SlugCratesStand, SlugPlatform}, {SlugCraneStand, SlugPlatform}, {SlugDampSticksSpawn, SlugPlatform},
		{SlugIslandArrive, SlugIsland}, {SlugKentAnchor, SlugIsland}, {SlugHolgartIsland, SlugIsland},
		{SlugCarolineAnchor, SlugMainland}, {SlugHolgartLandAnchor, SlugMainland}, {SlugShore, SlugMainland}, {ArdougneSouthBank, SlugMainland},
		{KhazardGeneralStore.Anchor, SlugMainland},
	} {
		if got := SlugRegionOf(c.at); got != c.want {
			t.Errorf("%v is in the %s, want the %s", c.at, got, c.want)
		}
	}
}

// The fleet slot is given up once the torch is lit: the damp sticks, the broken glass and Bailey's torch are spawns of one.
func TestTheSeaSlugSlotIsGivenUpFromTheLitTorchOn(t *testing.T) {
	for stage := SlugStarted; stage <= SlugSavedKennith; stage++ {
		s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true}
		if got, want := SeaSlug.ExclusiveDone(s), stage >= SlugLitTorch; got != want {
			t.Errorf("stage %d: past the crowded steps %v, want %v", stage, got, want)
		}
	}
	if !SeaSlug.ExclusiveDone(Snapshot{Journal: StatusComplete}) {
		t.Error("a complete quest holds its slot")
	}
}

func TestTheSeaSlugKeepsItsOwnItems(t *testing.T) {
	keep := SlugKeepIDs()
	for _, id := range []int{ObjSwampPaste, ObjTorchLit, ObjTorchUnlit, ObjDampSticks, ObjDrySticks, ObjBrokenGlass, CoinsID} {
		if !slices.Contains(keep, id) {
			t.Errorf("obj %d is not in the keep list", id)
		}
	}
	if slices.Contains(keep, ObjSeaSlug) {
		t.Error("the sea slug is a thing to keep: it is a thing never to pick up")
	}
	if !slices.Equal(SeaSlug.ToolIDs, keep) {
		t.Error("the module's tools are not the keep list")
	}
	keep[0] = -1
	if SlugKeepIDs()[0] == -1 {
		t.Error("SlugKeepIDs returned the live slice")
	}
}

// The module never fights, and one death anywhere parks it for the day: nothing in it can kill an account.
func TestTheSeaSlugNeverFights(t *testing.T) {
	if !SeaSlug.NoRetaliate || SeaSlug.MaxDeaths != 1 {
		t.Errorf("no-retaliate %v, max deaths %d", SeaSlug.NoRetaliate, SeaSlug.MaxDeaths)
	}
	for stage := SlugNotStarted; stage <= SlugComplete; stage++ {
		for _, region := range []SlugRegion{SlugMainland, SlugPlatform, SlugIsland} {
			for _, inv := range []map[int]int{nil, {ObjTorchLit: 1}, {ObjSwampPaste: 1}} {
				w := newSlugWorld(t, stage, region, inv, nil)
				if st := DecideSeaSlug(w.snapshot()); st.Kind == StepFight {
					t.Errorf("stage %d in the %s decided a fight", stage, region)
				}
			}
		}
	}
}
