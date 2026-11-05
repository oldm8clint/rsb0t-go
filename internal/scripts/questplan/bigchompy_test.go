package questplan

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Big Chompy Bird Hunting's plan: the journal's markers against pages rendered the way chompybird_journal.rs2 renders them, the decision at every stage
// and every pack that changes it, the kit and its keep list, and the price of a Feldip wolf.

// chJournalPage renders the quest journal as chompybird_journal.rs2 builds it for a stage (%chompybird, 0 to 65): bow says the pack holds an ogre bow
// (the journal's line about it shows only while it does) and raw says it holds a raw chompy. Lines break at the script's "|", and the colour tags are the
// script's own.
func chJournalPage(stage int, bow, raw bool) []string {
	var text string
	add := func(s string) { text += s }
	switch {
	case stage == 0:
		add("@dbl@I can start this quest by speaking to the ogre @dre@Rantz @dbl@who|lives @dre@East @dbl@of the @dre@Ogre City@dbl@, @dre@North @dbl@of the @dre@Swamp Pool.|")
		add("@dbl@To complete this quest I need:|@dre@Level 5 Fletching|@dre@Level 30 Cooking|@dre@Level 30 Ranging|")
	case stage == 65:
		add("@str@Once I had Rantz's bow, it was easy to hunt chompy birds|@str@using the nearby swamp toads. I cooked and seasoned a|@str@Chompy Bird for Rantz and his family.")
		add("|@str@In return for this I got some experience in :|@str@Fletching, cooking and ranged. Plus I kept an ogre bow.|")
		add("|@red@QUEST COMPLETE!")
	default:
		add("@str@I found an ogre named Rantz near a cave just East of|@str@the Ogre City. When I spoke to him he seemed obsessed|@str@with using things called 'stabbers' to hunt 'Chompy Birds'.|")
		if stage == 5 {
			add("|@dbl@I agreed to get @dre@Rants @dbl@some @dre@'stabbers'")
		}
		if stage >= 10 {
			add("|@str@It turned out that 'stabbers' are a kind of specially made|@str@ogre arrow that are quite sturdily constructed. I brought|@str@Rantz some so that he could catch 'chompy'.|")
			if stage == 10 {
				add("|@dbl@Rantz says I need to get some '@dre@fatsy toadies@dbl@' to make the|@dre@chompy @dbl@come.")
			}
		}
		if stage >= 15 {
			add("|@str@Rantz told me 'fatsy toadies' grow fat on swamp gas...|")
			if stage < 25 {
				add("|@dbl@I need to bring @dre@Rantz @dbl@a '@dre@fatsy toady@dbl@' to catch @dre@chompy@dbl@.")
			}
		}
		if stage >= 25 {
			add("|@str@I managed to trap some 'fatsy toadies' by using some gas|@str@filled ogre bellows. I took the 'fatsy toady' to Rantz.|")
			if stage == 25 {
				add("|@dbl@I should @dre@drop a toad @dbl@in the spot @dre@Rantz @dbl@showed to me")
			}
		}
		if stage >= 30 {
			add("|@str@I dropped a fatsy toady on the spot Rantz showed me.|")
			if stage < 40 {
				add("|@dbl@I should see how @dre@Rantz @dbl@catches a @dre@Chompy Bird.")
			}
		}
		if stage > 45 {
			add("|@str@Rantz doesn't seem to be a very good shot... maybe I|@str@should try to shoot a chompy for him?|")
		} else if stage == 40 || stage == 45 {
			if bow {
				add("|@str@Rantz doesn't seem to be a very good shot... maybe I|@str@should try to shoot a chompy for him?|")
				add("|@dre@Rantz @dbl@has lent me an @dre@Ogre bow @dbl@so I can kill a @dre@chompy|bird.")
			} else {
				add("|@dre@Rantz @dbl@doesn't seem to be a very good shot... maybe I|should try to shoot a @dre@chompy @dbl@for him?")
			}
		}
		if stage >= 50 {
			add("|@str@Rantz taught me how to use bloated swamp toads to lure|@str@Chompy Birds and hunt them. I've killed a chompy bird!|")
		}
		if stage == 50 {
			if raw {
				add("|@str@Rantz stands no chance of shooting a chompy bird - he'll|@str@starve to death if I don't help him.|")
				add("|@dbl@I should take this @dre@raw chompy @dbl@to @dre@Rantz.")
			} else {
				add("|@dre@Rantz @dbl@stands no chance of shooting a @dre@chompy bird @dbl@- he'll|starve to death if I don't @dre@help him.")
			}
		}
		if stage >= 55 {
			add("|@str@I showed Rantz the chompy. He seemed quite impressed.|")
			if stage == 55 {
				add("|@dbl@That cheeky ogre @dre@Rantz @dbl@has asked me to @dre@cook @dbl@the @dre@chompy|@dbl@for him and his kids, and they want it @dre@seasoning @dbl@as well!")
			}
		}
		if stage >= 60 {
			add("|@str@That cheeky ogre Rantz has asked me to cook the chompy|@str@for him and his kids, and they want it seasoning as well!|")
			if stage == 60 {
				add("|@dbl@I've @dre@cooked @dbl@the @dre@chompy @dbl@for that cheeky @dre@ogre @dbl@and his @dre@kids@dbl@,|I'd better take it to them before they @dre@starve@dbl@!")
			}
		}
	}
	return strings.Split(text, "|")
}

// chWrapLines re-breaks a journal page the way the interface's own word wrap would, at width characters, so that a marker that crosses a break is exercised.
func chWrapLines(lines []string, width int) []string {
	var out []string
	for _, l := range lines {
		words := strings.Fields(l)
		cur := ""
		for _, w := range words {
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

// Every stage's page reads as the stage the module means by it: the journal cannot tell 15 from 20, 30 from 35, or 40 from 45 (without the bow in the pack),
// and a pair reads as the lower, as the plan says.
func TestChompyJournalReadsEveryStage(t *testing.T) {
	for _, c := range []struct {
		stage     int
		bow, raw  bool
		want      int
		readsLike string
	}{
		{0, false, false, ChNotStarted, ""}, {5, false, false, ChStarted, ""}, {10, false, false, ChGivenArrows, ""},
		{15, false, false, ChToadies, ""}, {20, false, false, ChToadies, "15"}, {25, false, false, ChShownToad, ""},
		{30, false, false, ChDroppedToad, ""}, {35, false, false, ChDroppedToad, "30"}, {40, false, false, ChRantzMissed, ""},
		{40, true, false, ChGotBow, "lent"}, {45, false, false, ChRantzMissed, "the bow was dropped"}, {45, true, false, ChGotBow, ""},
		{50, false, false, ChKilled, ""}, {50, false, true, ChKilled, "with the raw chompy"}, {55, false, false, ChToldToCook, ""},
		{60, false, false, ChCooked, ""}, {65, false, false, ChComplete, ""},
	} {
		page := chJournalPage(c.stage, c.bow, c.raw)
		for _, width := range []int{0, 24, 38, 60} {
			lines := page
			if width > 0 {
				lines = chWrapLines(page, width)
			}
			got, ok := ParseChompyJournal(lines)
			if !ok || got != c.want {
				t.Errorf("stage %d (bow %v raw %v, width %d, %s): parsed %d ok %v, want %d", c.stage, c.bow, c.raw, width, c.readsLike, got, ok, c.want)
			}
		}
	}
	for _, empty := range [][]string{nil, {""}, {"Loading..."}, {"@dbl@Some other quest's page"}} {
		if got, ok := ParseChompyJournal(empty); ok {
			t.Errorf("%v parsed as stage %d: an unread page is unknown, never stage 0", empty, got)
		}
	}
}

// chSnap is a snapshot of an account in the middle of the quest: a strong account (it prices a wolf) at Rantz, with coins and food, and the mods applied.
func chSnap(stage int, mods ...func(*Snapshot)) Snapshot {
	s := Snapshot{
		Journal: StatusInProgress, Stage: stage, StageKnown: true, Tile: ChRantzAnchor, TileKnown: true,
		InvIDs: map[int]int{CoinsID: 800, ObjShark: 10, ObjKnife: 1, ObjChChisel: 1}, WornIDs: map[int]int{},
		Bank: map[string]int{}, BankIDs: map[int]int{}, BankKnown: true,
		Levels: map[string]int{"attack": 60, "strength": 60, "defence": 50, "hitpoints": 60, "magic": 40, "ranged": 40, "cooking": 40, "fletching": 20, "crafting": 20,
			"woodcutting": 15},
		HP: 60, MaxHP: 60, FreeSlots: 10,
		Failed: map[string]bool{}, Ran: map[string]bool{"provision:" + KitChompy: true}, Blocked: map[string]bool{}, QuestsDone: map[string]bool{},
	}
	if stage == 0 {
		s.Journal, s.StageKnown = StatusNotStarted, false
	}
	for _, m := range mods {
		m(&s)
	}
	return s
}

func chHold(id, n int) func(*Snapshot) { return func(s *Snapshot) { s.InvIDs[id] = n } }
func chWear(id int) func(*Snapshot)    { return func(s *Snapshot) { s.WornIDs[id] = 1 } }
func chFail(name string) func(*Snapshot) {
	return func(s *Snapshot) { s.Failed[name] = true }
}
func chRan(name string) func(*Snapshot) { return func(s *Snapshot) { s.Ran[name] = true } }
func chLevel(skill string, n int) func(*Snapshot) {
	return func(s *Snapshot) { s.Levels[skill] = n }
}
func chBank(id, n int) func(*Snapshot) { return func(s *Snapshot) { s.BankIDs[id] = n } }

// chKitOK is a pack that satisfies the whole kit of the arrows' stage: an axe, the feathers and the food, with the tools already in it.
func chKitOK(s *Snapshot) {
	s.InvIDs[ObjBronzeAxe] = 1
	s.InvIDs[ObjChFeather] = 48
}

func wantStep(t *testing.T, name string, got Step, kind StepKind, custom string) {
	t.Helper()
	if why := ValidateStep(got); why != "" {
		t.Errorf("%s: malformed step %+v: %s", name, got, why)
	}
	if got.Kind != kind || got.Custom != custom {
		t.Errorf("%s: step %s %q (%s), want %s %q", name, got.Kind, got.Custom, got.Reason, kind, custom)
	}
}

// Before the quest: the kit (coins, an axe, the feathers) is fetched first, then the wolf is priced, then Rantz is talked to.
func TestChompyDecideBeforeTheQuest(t *testing.T) {
	// A bare pack asks for the required lines in one round.
	bare := chSnap(0, func(s *Snapshot) { s.InvIDs = map[int]int{ObjShark: 10, ObjKnife: 1, ObjChChisel: 1} })
	st := DecideChompy(bare)
	wantStep(t, "a pack with no coins, axe or feathers", st, StepProvision, "")
	if st.KitName != KitChompy {
		t.Errorf("kit %q", st.KitName)
	}
	have := map[int]bool{}
	for _, l := range st.Kit {
		have[l.ID] = true
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("kit line %+v: %s", l, why)
		}
	}
	for _, id := range []int{CoinsID, ObjChFeather} {
		if !have[id] {
			t.Errorf("the kit lacks %d: %+v", id, st.Kit)
		}
	}
	axe := false
	for _, a := range Axes {
		axe = axe || have[a]
	}
	if !axe {
		t.Errorf("the kit lacks an axe: %+v", st.Kit)
	}
	for _, l := range st.Kit {
		if l.ID == ObjChFeather && l.Qty != ChFeathersPerArrow*ChArrowTarget {
			t.Errorf("feathers %d, want %d (four for each of the twelve arrows)", l.Qty, ChFeathersPerArrow*ChArrowTarget)
		}
	}
	// The kit satisfied, a strong account starts the quest with Rantz.
	ready := chSnap(0, chKitOK)
	wantStep(t, "a kit in hand", DecideChompy(ready), StepTalk, "")
	if got := DecideChompy(ready); got.Stop.ID != NpcChRantz || !slices.Contains(got.Stop.Prefer, "Ok, I'll make you some 'stabbers'") {
		t.Errorf("the start talk %+v", got.Stop)
	}
	// A kit whose every source is dead parks the quest, naming what is missing.
	blocked := chSnap(0, func(s *Snapshot) { s.InvIDs = map[int]int{ObjShark: 10}; s.Blocked["provision:"+KitChompy] = true })
	if got := DecideChompy(blocked); got.Kind != StepWait || got.ParkFor != ChItemParkFor || !strings.Contains(got.Reason, "feather") {
		t.Errorf("a blocked kit: %+v", got)
	}
	// Below a level the quest is not started.
	for _, c := range []struct{ skill, want string }{{"ranged", "Ranged 29/30"}, {"cooking", "Cooking 29/30"}, {"fletching", "Fletching 4/5"}, {"crafting", "Crafting 4/5"}} {
		lvl := map[string]int{"ranged": 29, "cooking": 29, "fletching": 4, "crafting": 4}[c.skill]
		got := DecideChompy(chSnap(0, chKitOK, chLevel(c.skill, lvl)))
		if got.Kind != StepWait || got.ParkFor != ChSkillParkFor || !strings.Contains(got.Reason, c.want) {
			t.Errorf("%s: %+v", c.skill, got)
		}
	}
	// A weak account is not sent among the wolves: it parks for the hours its levels will take to change.
	weak := chSnap(0, chKitOK, chLevel("attack", 20), chLevel("strength", 20), chLevel("defence", 15), chLevel("hitpoints", 30), func(s *Snapshot) { s.HP, s.MaxHP = 30, 30 })
	if got := DecideChompy(weak); got.Kind != StepWait || got.ParkFor != ChFightParkFor || !strings.Contains(got.Reason, "Wolf") && !strings.Contains(got.Reason, "wolf") {
		t.Errorf("a weak account: %+v", got)
	}
	// So is an account with nothing to eat, once the optional food has been looked for.
	hungry := chSnap(0, chKitOK, func(s *Snapshot) { delete(s.InvIDs, ObjShark) })
	if got := DecideChompy(hungry); got.Kind != StepWait || got.ParkFor != ChFightParkFor || !strings.Contains(got.Reason, "no food") {
		t.Errorf("no food: %+v", got)
	}
	// The optional lines (food, the tools) are looked for once a run, not waited for.
	noFood := chSnap(0, chKitOK, func(s *Snapshot) { delete(s.InvIDs, ObjShark); delete(s.Ran, "provision:"+KitChompy) })
	if got := DecideChompy(noFood); got.Kind != StepProvision || got.KitName != KitChompy {
		t.Errorf("optional food: %+v", got)
	}
	// An unread tab, an unread position and an unread stage wait.
	for name, s := range map[string]Snapshot{
		"unknown tab": chSnap(5, func(s *Snapshot) { s.Journal = StatusUnknown }),
		"no tile":     chSnap(5, func(s *Snapshot) { s.TileKnown = false }),
		"no stage":    chSnap(5, func(s *Snapshot) { s.StageKnown = false }),
	} {
		if got := DecideChompy(s); got.Kind != StepWait || got.ParkFor != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := DecideChompy(chSnap(5, func(s *Snapshot) { s.Journal = StatusComplete })); got.Kind != StepDone {
		t.Errorf("a complete tab: %+v", got)
	}
}

// Stage 5, the arrows: every stage of the fletching pipeline, in the order the content makes it.
func TestChompyDecideTheArrows(t *testing.T) {
	type c struct {
		name   string
		mods   []func(*Snapshot)
		kind   StepKind
		custom string
		item   int
		target int
	}
	base := []func(*Snapshot){chKitOK}
	with := func(extra ...func(*Snapshot)) []func(*Snapshot) {
		return append(append([]func(*Snapshot){}, base...), extra...)
	}
	for _, x := range []c{
		{"nothing made: a tree", with(), StepCustom, CustomChChop, 0, 0},
		{"a log: shafts", with(chHold(ObjChLogs, 1)), StepUseOn, "", ObjKnife, ObjChLogs},
		{"six shafts: flighted", with(chHold(ObjChShaft, 6)), StepUseOn, "", ObjChFeather, ObjChShaft},
		{"flighted and no bones: a wolf", with(chHold(ObjChFlighted, 6)), StepCustom, CustomChWolf, 0, 0},
		{"flighted and a bone: tips", with(chHold(ObjChFlighted, 6), chHold(ObjChWolfBones, 1)), StepUseOn, "", ObjChChisel, ObjChWolfBones},
		{"flighted and tips: arrows", with(chHold(ObjChFlighted, 6), chHold(ObjChTips, 4)), StepUseOn, "", ObjChTips, ObjChFlighted},
		{"twelve arrows: Rantz", with(chHold(ObjChArrow, 12)), StepCustom, CustomChGive, 0, 0},
		{"six arrows and six to make: shafts first", with(chHold(ObjChArrow, 6)), StepCustom, CustomChChop, 0, 0},
		{"six arrows, six flighted, no tips: a wolf", with(chHold(ObjChArrow, 6), chHold(ObjChFlighted, 6)), StepCustom, CustomChWolf, 0, 0},
		{"tools missing: Bugs", with(func(s *Snapshot) { delete(s.InvIDs, ObjKnife); delete(s.InvIDs, ObjChChisel) }), StepCustom, CustomChBugs, 0, 0},
	} {
		got := DecideChompy(chSnap(ChStarted, x.mods...))
		wantStep(t, x.name, got, x.kind, x.custom)
		if x.kind == StepUseOn && (got.ItemID != x.item || got.TargetID != x.target || got.TargetKind != TargetItem) {
			t.Errorf("%s: use %d on %d (%s), want %d on %d", x.name, got.ItemID, got.TargetID, got.TargetKind, x.item, x.target)
		}
	}
	// Short of feathers the kit is asked for them (required): the bank, the mule, then Gerrant's shop.
	short := DecideChompy(chSnap(ChStarted, with(chHold(ObjChFeather, 3), chHold(ObjChShaft, 6))...))
	wantStep(t, "feathers short", short, StepProvision, "")
	foundShop := false
	for _, l := range short.Kit {
		if l.ID == ObjChFeather {
			for _, src := range l.Sources {
				foundShop = foundShop || (src.Kind == SourceShop && src.Shop.NpcID == 558)
			}
		}
	}
	if !foundShop {
		t.Errorf("no shop among the feathers' sources: %+v", short.Kit)
	}
	// Half the feathers are needed once the flighted shafts exist: six flighted shafts and six more to make need 24.
	half := chSnap(ChStarted, with(chHold(ObjChFlighted, 6), chHold(ObjChTips, 6))...)
	if got := ChFeathersNeeded(half); got != 24 {
		t.Errorf("feathers needed with six flighted: %d, want 24", got)
	}
	// A wolf is priced every time: the same pack with a weak account parks, with the reason.
	weak := chSnap(ChStarted, with(chHold(ObjChFlighted, 6), chLevel("attack", 15), chLevel("strength", 15), chLevel("defence", 10))...)
	if got := DecideChompy(weak); got.Kind != StepWait || got.ParkFor != ChFightParkFor || !strings.Contains(got.Reason, "wolf") {
		t.Errorf("a weak account at the wolves: %+v", got)
	}
	// A hunt, a chop and a Bugs that failed park instead of repeating.
	for name, c := range map[string]struct {
		mods []func(*Snapshot)
		fail string
		park time.Duration
	}{
		"wolf": {with(chHold(ObjChFlighted, 6), chFail(CustomChWolf)), CustomChWolf, ChStuckParkFor},
		"chop": {with(chFail(CustomChChop)), CustomChChop, ChStuckParkFor},
		"bugs": {with(func(s *Snapshot) { delete(s.InvIDs, ObjKnife); delete(s.InvIDs, ObjChChisel) }, chFail(CustomChBugs)), CustomChBugs, ChItemParkFor},
	} {
		if got := DecideChompy(chSnap(ChStarted, c.mods...)); got.Kind != StepWait || got.ParkFor != c.park {
			t.Errorf("%s failed: %+v", name, got)
		}
	}
	// Short of any tool with the arrows needing one parks (Bugs sells them at stage 5 only).
	if got := DecideChompy(chSnap(ChStarted, with(chHold(ObjChLogs, 1), func(s *Snapshot) { delete(s.InvIDs, ObjKnife); s.Failed[CustomChBugs] = true })...)); got.Kind != StepWait || got.ParkFor == 0 {
		t.Errorf("no knife: %+v", got)
	}
}

// Stages 10 to 35: the toadies, the chest, the bellows, the toads, the bait.
func TestChompyDecideTheToadsAndTheBait(t *testing.T) {
	bellows := chHold(ObjChBellows3, 1)
	for _, x := range []struct {
		name   string
		stage  int
		mods   []func(*Snapshot)
		kind   StepKind
		custom string
	}{
		{"10 with no toad: the toadies menu", ChGivenArrows, nil, StepCustom, CustomChToadies},
		{"10 with a toad: show it", ChGivenArrows, []func(*Snapshot){chHold(ObjChToad, 1)}, StepCustom, CustomChShowToad},
		{"15, no bellows: the chest", ChToadies, nil, StepCustom, CustomChChest},
		{"15, bellows: a toad", ChToadies, []func(*Snapshot){bellows}, StepCustom, CustomChToad},
		{"15, a toad: show it", ChToadies, []func(*Snapshot){bellows, chHold(ObjChToad, 2)}, StepCustom, CustomChShowToad},
		{"25, a toad in the pack: bait", ChShownToad, []func(*Snapshot){chHold(ObjChToad, 1)}, StepCustom, CustomChBait},
		{"25, bellows and no toad: bait (it catches its own)", ChShownToad, []func(*Snapshot){bellows}, StepCustom, CustomChBait},
		{"25, no bellows and no toad: the chest", ChShownToad, nil, StepCustom, CustomChChest},
		{"30, bellows: bait", ChDroppedToad, []func(*Snapshot){bellows}, StepCustom, CustomChBait},
	} {
		wantStep(t, x.name, DecideChompy(chSnap(x.stage, x.mods...)), x.kind, x.custom)
	}
	// The chest said the account owns a pair: the bank is read, the pair drawn, and with none anywhere the quest parks.
	refused := chFail(CustomChChest)
	got := DecideChompy(chSnap(ChToadies, refused, func(s *Snapshot) { s.BankKnown = false }))
	wantStep(t, "an unread bank", got, StepScanBank, "")
	got = DecideChompy(chSnap(ChToadies, refused, chBank(ObjChBellows2, 1)))
	if got.Kind != StepWithdraw || len(got.Items) != 1 || got.Items[0].ID != ObjChBellows2 || got.Bank != YanilleBank {
		t.Errorf("a banked pair: %+v", got)
	}
	if got = DecideChompy(chSnap(ChToadies, refused)); got.Kind != StepWait || got.ParkFor != ChStuckParkFor {
		t.Errorf("no bellows anywhere: %+v", got)
	}
	for name, s := range map[string]Snapshot{
		"toad round":  chSnap(ChToadies, bellows, chFail(CustomChToad)),
		"bait":        chSnap(ChDroppedToad, bellows, chFail(CustomChBait)),
		"toadies":     chSnap(ChGivenArrows, chFail(CustomChToadies)),
		"show a toad": chSnap(ChGivenArrows, chHold(ObjChToad, 1), chFail(CustomChShowToad)),
	} {
		if got := DecideChompy(s); got.Kind != StepWait || got.ParkFor != ChStuckParkFor {
			t.Errorf("%s failed: %+v", name, got)
		}
	}
}

// Stages 40 to 60: the bow, the arrows, the shot, the roast, the hand-over.
func TestChompyDecideTheHuntAndTheRoast(t *testing.T) {
	armed := []func(*Snapshot){chHold(ObjChBow, 1), chHold(ObjChArrow, 6), chHold(ObjChBellows3, 1)}
	seasonings := func(s *Snapshot) {
		for _, c := range ChSeasoningList() {
			s.InvIDs[c.ID] = 1
		}
	}
	for _, x := range []struct {
		name   string
		stage  int
		mods   []func(*Snapshot)
		kind   StepKind
		custom string
		item   int
	}{
		{"40, no bow: Rantz lends one", ChRantzMissed, []func(*Snapshot){chHold(ObjChArrow, 6), chHold(ObjChBellows3, 1)}, StepCustom, CustomChBow, 0},
		{"45 armed: shoot", ChGotBow, armed, StepCustom, CustomChShoot, 0},
		{"45 with the bow worn and the arrows quivered: shoot", ChGotBow, []func(*Snapshot){chWear(ObjChBow), chWear(ObjChArrow), chHold(ObjChBellows3, 1)}, StepCustom, CustomChShoot, 0},
		{"45, no bellows: the chest", ChGotBow, []func(*Snapshot){chHold(ObjChBow, 1), chHold(ObjChArrow, 6)}, StepCustom, CustomChChest, 0},
		{"45, no arrows and no chisel to make more with: park", ChGotBow, []func(*Snapshot){chHold(ObjChBow, 1), chHold(ObjChBellows3, 1), chKitOK, func(s *Snapshot) { delete(s.InvIDs, ObjChChisel) }}, StepWait, "", 0},
		{"45, no arrows: the arrows again", ChGotBow, []func(*Snapshot){chHold(ObjChBow, 1), chHold(ObjChBellows3, 1), chKitOK}, StepCustom, CustomChChop, 0},
		{"50, a raw chompy: show it", ChKilled, []func(*Snapshot){chHold(ObjChRawChompy, 1)}, StepCustom, CustomChShowChompy, 0},
		{"50, none: another shot", ChKilled, armed, StepCustom, CustomChShoot, 0},
		{"55: ask the children first", ChToldToCook, []func(*Snapshot){chHold(ObjChRawChompy, 1)}, StepCustom, CustomChKids, 0},
		{"55, asked: the first seasoning", ChToldToCook, []func(*Snapshot){chHold(ObjChRawChompy, 1), chRan(CustomChKids)}, StepCustom, CustomChSeason, ObjChPotato},
		{"55, a seasoning missing: it", ChToldToCook, []func(*Snapshot){chHold(ObjChRawChompy, 1), chRan(CustomChKids), seasonings, func(s *Snapshot) { delete(s.InvIDs, ObjChTomato) }}, StepCustom, CustomChSeason, ObjChTomato},
		{"55, seasoned and no chompy: a shot", ChToldToCook, append([]func(*Snapshot){chRan(CustomChKids), seasonings}, armed...), StepCustom, CustomChShoot, 0},
		{"55, all there: the spit", ChToldToCook, []func(*Snapshot){chHold(ObjChRawChompy, 1), chRan(CustomChKids), seasonings}, StepCustom, CustomChCook, 0},
		{"60, the seasoned chompy: Rantz", ChCooked, []func(*Snapshot){chHold(ObjChSeasonedChomp, 1)}, StepCustom, CustomChHandOver, 0},
		{"60, lost: roast again (the children are not asked again)", ChCooked, []func(*Snapshot){chHold(ObjChRawChompy, 1), seasonings}, StepCustom, CustomChCook, 0},
	} {
		got := DecideChompy(chSnap(x.stage, x.mods...))
		wantStep(t, x.name, got, x.kind, x.custom)
		if x.item != 0 && got.ItemID != x.item {
			t.Errorf("%s: item %d, want %d", x.name, got.ItemID, x.item)
		}
	}
	// The failures park, each with the reason.
	for name, s := range map[string]Snapshot{
		"bow":    chSnap(ChRantzMissed, chHold(ObjChArrow, 6), chHold(ObjChBellows3, 1), chFail(CustomChBow)),
		"shot":   chSnap(ChGotBow, append(armed, chFail(CustomChShoot))...),
		"kids":   chSnap(ChToldToCook, chHold(ObjChRawChompy, 1), chFail(CustomChKids)),
		"season": chSnap(ChToldToCook, chHold(ObjChRawChompy, 1), chRan(CustomChKids), chFail(CustomChSeason)),
		"spit":   chSnap(ChToldToCook, chHold(ObjChRawChompy, 1), chRan(CustomChKids), seasonings, chFail(CustomChCook)),
		"hand":   chSnap(ChCooked, chHold(ObjChSeasonedChomp, 1), chFail(CustomChHandOver)),
		"show":   chSnap(ChKilled, chHold(ObjChRawChompy, 1), chFail(CustomChShowChompy)),
	} {
		if got := DecideChompy(s); got.Kind != StepWait || got.ParkFor != ChStuckParkFor {
			t.Errorf("%s failed: %+v", name, got)
		}
	}
	// The stage the journal and the tab disagree on waits.
	if got := DecideChompy(chSnap(ChComplete)); got.Kind != StepWait || got.ParkFor != 0 {
		t.Errorf("stage 65 with the tab in progress: %+v", got)
	}
	if got := DecideChompy(chSnap(ChNotStarted, func(s *Snapshot) { s.Journal, s.StageKnown, s.Stage = StatusInProgress, true, 0 })); got.Kind != StepWait {
		t.Errorf("stage 0 with the tab in progress: %+v", got)
	}
}

// Every step the decision can return is well formed, and every bank step keeps what the quest carries.
func TestChompyStepsAreWellFormedAndKeepTheQuestsItems(t *testing.T) {
	keep := map[int]bool{}
	for _, id := range ChKeepIDs() {
		keep[id] = true
	}
	for _, id := range []int{CoinsID, ObjKnife, ObjChChisel, ObjChFeather, ObjChLogs, ObjChShaft, ObjChFlighted, ObjChWolfBones, ObjChTips, ObjChArrow, ObjChBow, ObjChToad,
		ObjChRawChompy, ObjChRuinedChompy, ObjChSeasonedChomp, ObjChPotato, ObjChOnion, ObjChCabbage, ObjChTomato, ObjChDoogle, ObjChEqua, ObjBronzeAxe, ObjShark} {
		if !keep[id] {
			t.Errorf("the keep list lacks %d", id)
		}
	}
	for _, id := range ChBellowsIDs() {
		if !keep[id] {
			t.Errorf("the keep list lacks the bellows %d", id)
		}
	}
	tools := map[int]bool{}
	for _, id := range ChToolIDs() {
		tools[id] = true
	}
	// The seasonings can be eaten (a cabbage, a tomato) and the walk's floor bite takes any food the pack carries: they are tools, and the real foods are not.
	for _, c := range ChSeasoningList() {
		if !tools[c.ID] {
			t.Errorf("the seasoning %s is not a tool: the floor bite could eat it", c.Name)
		}
	}
	if tools[ObjShark] {
		t.Error("a shark is a tool: the floor bite would leave it")
	}
	// Walk every stage with a spread of packs and validate what comes out.
	stages := []int{0, 5, 10, 15, 25, 30, 40, 45, 50, 55, 60}
	packs := []func(*Snapshot){nil, chKitOK, chHold(ObjChArrow, 12), chHold(ObjChToad, 1), chHold(ObjChBellows3, 1), chHold(ObjChBow, 1), chHold(ObjChRawChompy, 1),
		chHold(ObjChSeasonedChomp, 1), chFail(CustomChChest), chRan(CustomChKids)}
	for _, stage := range stages {
		for i, p := range packs {
			var mods []func(*Snapshot)
			if p != nil {
				mods = append(mods, p)
			}
			st := DecideChompy(chSnap(stage, mods...))
			if why := ValidateStep(st); why != "" {
				t.Errorf("stage %d pack %d: %+v: %s", stage, i, st, why)
			}
			for _, id := range st.KeepIDs {
				if !keep[id] && !slices.Contains(ChKeepIDs(), id) {
					t.Errorf("stage %d: keep id %d is outside the quest's list", stage, id)
				}
			}
		}
	}
}

// The wolf's price: levels that win, levels that do not, and food.
func TestChompyWolfIsPricedByTheSharedModel(t *testing.T) {
	if got := FoeFeldipWolf.MaxHit(); got != 6 {
		t.Errorf("the wolf hits up to %d, the content's Strength 55 says 6", got)
	}
	if foe, ok := FoeByNPC(NpcFeldipWolf); !ok || foe.Hitpoints != 69 || foe.Name != "Wolf" {
		t.Errorf("FoeByNPC(wolf) = %+v %v", foe, ok)
	}
	strong := chSnap(ChStarted)
	if v := ChWolfReady(strong); !v.OK {
		t.Errorf("a 60/60/50 account with ten sharks: %s", v.Why)
	}
	for name, s := range map[string]Snapshot{
		"20/20/15":  chSnap(ChStarted, chLevel("attack", 20), chLevel("strength", 20), chLevel("defence", 15)),
		"30/30/30":  chSnap(ChStarted, chLevel("attack", 30), chLevel("strength", 30), chLevel("defence", 30)),
		"no food":   chSnap(ChStarted, func(s *Snapshot) { delete(s.InvIDs, ObjShark) }),
		"one shark": chSnap(ChStarted, chLevel("attack", 40), chLevel("strength", 40), chLevel("defence", 35), chHold(ObjShark, 1)),
	} {
		if v := ChWolfReady(s); v.OK || v.Why == "" {
			t.Errorf("%s: the model says the account wins: %+v", name, v)
		}
	}
	// A foe the account cannot read is not judged (an unread stat is no reason to refuse).
	if v := ChWolfReady(chSnap(ChStarted, func(s *Snapshot) { s.MaxHP = 0 })); !v.OK {
		t.Errorf("an account whose hitpoints are unread: %+v", v)
	}
}

// The module's record, its stage reader and its sustain.
func TestChompyModuleShape(t *testing.T) {
	m := BigChompy
	if m.Record.ID != "chompybird" || m.Record.Name != "Big Chompy Bird Hunting" || m.Record.QuestPoints != 2 {
		t.Errorf("record %+v", m.Record)
	}
	skills := map[string]int{}
	for _, r := range m.Record.Requirements.Skills {
		skills[r.Skill] = r.Level
	}
	for skill, level := range map[string]int{"ranged": 30, "cooking": 30, "fletching": 5, "crafting": 5} {
		if skills[skill] != level {
			t.Errorf("the record asks %s %d, want %d", skill, skills[skill], level)
		}
	}
	if m.Decide == nil || m.ReadStage == nil || m.MaxDeaths != 2 || !m.BankSet || m.Bank != YanilleBank || len(m.ToolIDs) == 0 {
		t.Errorf("module %+v", m)
	}
	if m.Sustain.EatAt != 14 || !m.Sustain.IsFood(ObjShark) {
		t.Errorf("sustain %+v", m.Sustain)
	}
	if ChInBaitZone(ChRantzAnchor) || !ChInBaitZone(ChBaitSpot) || !ChInBaitZone(tile.Tile{X: 2631, Z: 2962}) || ChInBaitZone(tile.Tile{X: 2630, Z: 2962}) || ChInBaitZone(tile.Tile{X: 2631, Z: 2972}) {
		t.Error("the bait clearing is not 2631..2639, 2962..2971")
	}
	if !ChInCave(ChBugsAnchor) || ChInCave(ChRantzAnchor) {
		t.Error("the cave's box is wrong")
	}
}
