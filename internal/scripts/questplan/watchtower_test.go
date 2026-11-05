package questplan

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Watch Tower's pure half: the journal reading, the areas, the prices and the decision table.

// ---------------------------------------------------------------------------------------------------------------------
// The journal
// ---------------------------------------------------------------------------------------------------------------------

// wtPages are the journal pages of itwatchtower_journal.rs2, as the client renders them (the `|` breaks a line, the @xxx@ tags colour it). Each
// line is checked against the script's source in watchtower_content_test.go, so the fixtures are the content's.
var wtPages = struct {
	started, tribes, tribesAllGiven, riddle, cave, crystal, nightshade, potionToMake, potionMade, shamans, shamansDone, rock, crystals, lever string
}{
	started: "@dbl@I accepted the challenge of finding the lost @dre@crystals.|@dbl@I need to @dre@find evidence@dbl@ of what's happened. It has been suggested I try searching the @dre@bushes@dbl@ near the Watchtower|",
	tribes: "@str@I accepted the challenge of finding the lost crystals.|@str@I found some fingernails as evidence. I should take them to|@str@the Watchtower wizard.|" +
		"|@dbl@Now I need to @dre@deal with the tribal ogres.||@dbl@I should search the area to see if I can find any ogre and try and communicate with the leaders.|" +
		"@dbl@Og wants me to @dre@retrieve his stolen gold@dbl@ from Toban.|@dbl@Grew wants me to give him @dre@one of Gorad's teeth.|@dbl@Toban wants the @dre@bones of an adult dragon.|" +
		"@dbl@The north west guard wants a @dre@sign of friendship.|",
	tribesAllGiven: "@str@I accepted the challenge of finding the lost crystals.|@str@I found some fingernails as evidence. I should take them to|@str@the Watchtower wizard.|" +
		"|@dbl@Now I need to @dre@deal with the tribal ogres.||@dbl@I should search the area to see if I can find any ogre and try and communicate with the leaders.|" +
		"@str@I returned Og's stolen gold.|@str@I knocked out one of Gorad's teeth and gave it to Grew.|@str@I gave the bones to Toban.|" +
		"@str@I gave the ogre relic to the north west guard.|@dbl@The north-east guard wants @dre@a surprise gift.|@dbl@I have a @dre@rock cake@dbl@ that is very surprising.|@dbl@I wonder if it will do...|" +
		"@dbl@Some guards gave me a @dre@puzzle to solve.|",
	riddle: "@str@I accepted the challenge of finding the lost crystals.|@str@I found some fingernails as evidence. I should take them to|@str@the Watchtower wizard.|" +
		"|@str@I have solved the guard's puzzle.|@dbl@I was given @dre@a map@dbl@ by the guard.|@dbl@I have it with me now, so I can navigate the skavid caves.|@dbl@I need to @dre@search the skavid caves.|" +
		"|@dbl@I have been taught a few words of the skavid language:||@dbl@ar, nod, gor, ig, cur||@dre@'Cur bidith' - 'Ig'|" +
		"|@dre@'Bidith tanath' - 'Cur'|",
	cave: "@str@I accepted the challenge of finding the lost crystals.|" +
		"|@str@I was given a map by the guard.|@str@I need to search the skavid caves.||@str@I found my way into the skavid caves.|" +
		"@dbl@I have been told that the other crystals are in the @dre@shamans' enclave@dbl@. I should speak to the @dre@Watchtower wizard@dbl@ to find out how to get in.",
	crystal: "@str@I accepted the challenge of finding the lost crystals.|" +
		"|@str@I was given a map by the guard.|@str@I need to search the skavid caves.||@str@I found my way into the skavid caves.|" +
		"@str@I used some cave nightshade to distract the guard.|@dbl@I need to @dre@defeat the ogre shamans@dbl@ and @dre@find the other crystals.|",
	nightshade: "",
	potionToMake: "@str@I used some cave nightshade to distract the guard.||@str@I need to defeat the ogre shamans and find the other|@str@crystals.|" +
		"@str@I tried to defeat the shamans, but they are protected|@str@by powerful magics! I should speak with the Watchtower|@str@wizard to see if he has any advice for me.|" +
		"@dbl@I need to make the @dre@potion@dbl@ that will @dre@defeat the ogre @dre@shamans@dbl@.",
	potionMade: "@str@I used some cave nightshade to distract the guard.||@str@I need to defeat the ogre shamans and find the other|@str@crystals.|" +
		"@str@I tried to defeat the shamans, but they are protected|@str@by powerful magics! I should speak with the Watchtower|@str@wizard to see if he has any advice for me.|" +
		"|@dbl@I have made the @dre@ogre potion@dbl@. I need to get it enchanted by the Watchtower wizard.|",
	shamans: "@str@I used some cave nightshade to distract the guard.||@str@I need to defeat the ogre shamans and find the other|@str@crystals.|" +
		"@str@I tried to defeat the shamans, but they are protected|@str@by powerful magics! I should speak with the Watchtower|@str@wizard to see if he has any advice for me.|" +
		"|@str@I have made the ogre potion.|@str@I gave the potion to the wizard.|@str@He infused it into a magic ogre potion.|@str@I need to defeat the ogre shamans.||" +
		"@dbl@Now I need to @dre@kill 4 ogre shaman(s).|",
	shamansDone: "@str@I used some cave nightshade to distract the guard.||@str@I need to defeat the ogre shamans and find the other|@str@crystals.|" +
		"@str@I tried to defeat the shamans, but they are protected|@str@by powerful magics! I should speak with the Watchtower|@str@wizard to see if he has any advice for me.|" +
		"|@str@I have made the ogre potion.|@str@I gave the potion to the wizard.|@str@He infused it into a magic ogre potion.|@str@I need to defeat the ogre shamans.||" +
		"|@str@I killed all the ogre shamans.||@dbl@Now the @dre@shamans@dbl@ are dead, I should be able to @dre@mine the Rock of Dalgroth@dbl@.|",
	rock: "@str@I have made the ogre potion.|@str@I gave the potion to the wizard.|@str@He infused it into a magic ogre potion.|@str@I need to defeat the ogre shamans.||" +
		"|@str@I killed all the ogre shamans.||@dbl@I have @dre@mined the sacred rock@dbl@ and have taken the last @dre@crystal.|@dbl@I need to @dre@recover some of the crystals@dbl@ and take them to the Watchtower wizard.|",
	crystals: "@str@I used some cave nightshade to distract the guard.||@str@I need to defeat the ogre shamans and find the other|@str@crystals.|" +
		"|@str@I need to return all the crystals to the Watchtower wizard.||@dbl@I have taken the crystals to the Watchtower wizard. Now I need to throw the @dre@lever@dbl@ to @dre@activate the shield@dbl@.|",
	lever: "|@str@I helped the Wizards in Yanille, and rescued the|@str@lost crystals.|@str@I defeated the ogres and their minions.|@str@My task here is done.||@dre@QUEST COMPLETE!|",
}

func wtParse(t *testing.T, page string) WtState {
	t.Helper()
	// The client hands the lines over as the interface holds them: once as a whole string, once split at the breaks.
	n, ok := ParseWatchtowerJournal([]string{page})
	if !ok {
		t.Fatalf("page not recognised: %.80s", page)
	}
	m, ok2 := ParseWatchtowerJournal(strings.Split(page, "|"))
	if !ok2 || m != n {
		t.Fatalf("the split page reads %d (%v), the whole page %d", m, ok2, n)
	}
	return WtStateOf(n)
}

func TestWatchtowerJournalStages(t *testing.T) {
	for _, c := range []struct {
		name  string
		page  string
		stage int
		has   uint32
		not   uint32
		left  int
	}{
		{"started", wtPages.started, WtStarted, 0, WtOgSpoken, -1},
		{"tribes", wtPages.tribes, WtGivenFingernails, WtOgSpoken | WtGrewSpoken | WtTobanSpoken | WtLookingRelic, WtOgHelped | WtGrewHelped | WtTobanHelped, -1},
		{"tribes all given and the relic shown", wtPages.tribesAllGiven, WtGivenRiddle, WtOgHelped | WtGrewHelped | WtTobanHelped | WtOgSpoken | WtGrewSpoken | WtTobanSpoken | WtMarketAsked, WtMarketPaid, -1},
		{"map and words", wtPages.riddle, WtSolvedRiddle, WtLearning | WtLearnedIg | WtLearnedCur, WtLearnedAr | WtLearnedNod, -1},
		{"told of the enclave", wtPages.cave, WtSkavidCrystal, 0, WtLearning, -1},
		{"fed the guard nightshade", wtPages.crystal, WtFedNightshade, 0, 0, -1},
		{"to make the potion", wtPages.potionToMake, WtLearnedPotion, 0, 0, -1},
		{"potion made, to infuse", wtPages.potionMade, WtLearnedPotion, 0, 0, -1},
		{"infused, four shamans left", wtPages.shamans, WtMadePotion, 0, WtMinedRock, 4},
		{"shamans done", wtPages.shamansDone, WtMadePotion, 0, WtMinedRock, 0},
		{"rock mined", wtPages.rock, WtMadePotion, WtMinedRock, 0, 0},
		{"crystals taken to the wizard", wtPages.crystals, WtFoundAll, 0, 0, -1},
		{"done", wtPages.lever, WtComplete, 0, 0, -1},
	} {
		if c.page == "" {
			continue
		}
		st := wtParse(t, c.page)
		if st.Stage != c.stage || st.Has(c.has) != true || (c.not != 0 && st.Flags&c.not != 0) || st.Left != c.left {
			t.Errorf("%s: read %v, want stage %d with %#x and without %#x, shamans left %d", c.name, st, c.stage, c.has, c.not, c.left)
		}
	}
	// The pack and the unpack agree on every stage and flag.
	for stage := 0; stage <= 14; stage++ {
		for _, flags := range []uint32{0, WtOgSpoken, WtMinedRock, WtOgSpoken | WtGrewHelped | WtLearnedNod | WtMarketPaid} {
			for left := -1; left <= 6; left++ {
				in := WtState{Stage: stage, Flags: flags, Left: left}
				if out := WtStateOf(in.Pack()); out != in {
					t.Fatalf("%v packs to %d and back to %v", in, in.Pack(), out)
				}
			}
		}
	}
	// Nothing recognised is unknown, not stage 0.
	if _, ok := ParseWatchtowerJournal([]string{"Rune Mysteries||@dbl@I can start this quest by talking to Duke Horacio"}); ok {
		t.Error("another quest's page was read as the Watch Tower's")
	}
	if _, ok := ParseWatchtowerJournal(nil); ok {
		t.Error("an empty page was read")
	}
}

// A tribe's wants line is dropped once it is satisfied, so a helped tribe also reads as spoken to; the market is the same.
func TestWatchtowerHelpedImpliesSpoken(t *testing.T) {
	st := wtParse(t, wtPages.tribesAllGiven)
	if !st.Has(WtOgSpoken|WtGrewSpoken|WtTobanSpoken|WtMarketAsked) || st.Has(WtMarketPaid) {
		t.Errorf("flags %#x", st.Flags)
	}
	paid := strings.Replace(wtPages.tribesAllGiven, "@dbl@The north-east guard wants @dre@a surprise gift.|@dbl@I have a @dre@rock cake@dbl@ that is very surprising.|@dbl@I wonder if it will do...|",
		"@str@I gave the north-east guard a rock cake.|", 1)
	if st := wtParse(t, paid); !st.Has(WtMarketPaid | WtMarketAsked) {
		t.Errorf("a paid market reads %#x", st.Flags)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The areas
// ---------------------------------------------------------------------------------------------------------------------

func TestWatchtowerAreas(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want WtArea
	}{
		{tile.Tile{X: 2613, Z: 3094}, WtAreaMain},
		{tile.Tile{X: 2544, Z: 3112}, WtAreaMain},
		{tile.Tile{X: 2544, Z: 3112, Level: 1}, WtAreaTower},
		{tile.Tile{X: 2549, Z: 3112, Level: 2}, WtAreaTower},
		{tile.Tile{X: 2928, Z: 4715, Level: 2}, WtAreaMirror},
		{tile.Tile{X: 2513, Z: 3084}, WtAreaIsland},
		{tile.Tile{X: 2505, Z: 3087}, WtAreaIsland},
		{tile.Tile{X: 2501, Z: 3087}, WtAreaMain},
		{tile.Tile{X: 2576, Z: 3029}, WtAreaCamp},
		{tile.Tile{X: 2500, Z: 2988}, WtAreaMain},
		{tile.Tile{X: 2508, Z: 3011}, WtAreaLowerCity},
		{tile.Tile{X: 2531, Z: 3026}, WtAreaLowerCity},
		{tile.Tile{X: 2506, Z: 3011}, WtAreaMain},
		{tile.Tile{X: 2530, Z: 3029}, WtAreaCityGuard},
		{tile.Tile{X: 2541, Z: 3029}, WtAreaCityGuard},
		{tile.Tile{X: 2549, Z: 3027}, WtAreaEastInside},
		{tile.Tile{X: 2529, Z: 3013}, WtAreaEastInside},
		{tile.Tile{X: 2550, Z: 3030}, WtAreaMain},
		{tile.Tile{X: 2498, Z: 9418}, WtAreaCaves},
		{tile.Tile{X: 2522, Z: 9411}, WtAreaCaves},
		{tile.Tile{X: 2588, Z: 9410}, WtAreaEnclave},
		{tile.Tile{X: 2598, Z: 9467}, WtAreaEnclave},
	} {
		if got := WtAreaOf(c.at); got != c.want {
			t.Errorf("%v is in %s, want %s", c.at, got, c.want)
		}
	}
	// The six landings are each their own cave, and nowhere else is one.
	for i, l := range WtCaveLandings {
		if got := WtCaveAt(l); got != i+1 {
			t.Errorf("landing %v is cave %d, want %d", l, got, i+1)
		}
	}
	if WtCaveAt(WtDarkLanding) != 0 || WtCaveAt(tile.Tile{X: 2540, Z: 3054}) != 0 {
		t.Error("the dark cave or the surface counts as a cave")
	}
	// Every area names itself.
	for a := WtAreaMain; a <= WtAreaEnclave; a++ {
		if a.String() == "" {
			t.Errorf("area %d has no name", a)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The prices
// ---------------------------------------------------------------------------------------------------------------------

// wtSnap is a stage-and-flags snapshot of an account standing at the Yanille bank with a strong fighter's levels and ten sharks, the default every row varies.
func wtSnap(stage int, flags uint32, left int) Snapshot {
	st := WtState{Stage: stage, Flags: flags, Left: left}
	return Snapshot{
		Journal: StatusInProgress, Stage: st.Pack(), StageKnown: true, Tile: tile.Tile{X: 2613, Z: 3094}, TileKnown: true,
		InvIDs: map[int]int{WtObjShark: 10}, WornIDs: map[int]int{}, Inv: map[string]int{}, BankIDs: map[int]int{}, BankKnown: true,
		FreeSlots: 18, HP: 70, MaxHP: 70, Failed: map[string]bool{}, Ran: map[string]bool{}, Blocked: map[string]bool{},
		Levels: map[string]int{"attack": 75, "strength": 75, "defence": 75, "hitpoints": 70, "magic": 70, "agility": 35, "thieving": 77, "herblore": 50, "mining": 60},
	}
}

func (s Snapshot) wtWith(id, n int) Snapshot {
	inv := map[int]int{}
	for k, v := range s.InvIDs {
		inv[k] = v
	}
	inv[id] = n
	s.InvIDs = inv
	return s
}

func (s Snapshot) wtAt(x, z, level int) Snapshot {
	s.Tile = tile.Tile{X: x, Z: z, Level: level}
	return s
}

func (s Snapshot) wtLevel(skill string, n int) Snapshot {
	lv := map[string]int{}
	for k, v := range s.Levels {
		lv[k] = v
	}
	lv[skill] = n
	s.Levels = lv
	return s
}

func (s Snapshot) wtFailed(keys ...string) Snapshot {
	m := map[string]bool{}
	for k, v := range s.Failed {
		m[k] = v
	}
	for _, k := range keys {
		m[k] = true
	}
	s.Failed = m
	return s
}

func (s Snapshot) wtRan(keys ...string) Snapshot {
	m := map[string]bool{}
	for k, v := range s.Ran {
		m[k] = v
	}
	for _, k := range keys {
		m[k] = true
	}
	s.Ran = m
	return s
}

func (s Snapshot) wtBlocked(keys ...string) Snapshot {
	m := map[string]bool{}
	for k, v := range s.Blocked {
		m[k] = v
	}
	for _, k := range keys {
		m[k] = true
	}
	s.Blocked = m
	s.Failed = s.wtFailed(keys...).Failed
	return s
}

func (s Snapshot) wtBank(id, n int) Snapshot {
	b := map[int]int{}
	for k, v := range s.BankIDs {
		b[k] = v
	}
	b[id] = n
	s.BankIDs = b
	return s
}

// The foes' numbers are the content's (the questFoes table pins them against ogre.npc), and the models say what they should of the fleet's fighters.
func TestWatchtowerFoesAndPrices(t *testing.T) {
	if FoeOgre.MaxHit() != 7 || FoeGorad.MaxHit() != 7 || FoeOgreChieftain.MaxHit() != 9 {
		t.Errorf("max hits: ogre %d, Gorad %d, chieftain %d", FoeOgre.MaxHit(), FoeGorad.MaxHit(), FoeOgreChieftain.MaxHit())
	}
	for id, want := range map[int]MeleeFoe{WtNpcOgre: FoeOgre, WtNpcOgre2: FoeOgre, WtNpcGorad: FoeGorad, WtNpcChieftain: FoeOgreChieftain} {
		if got, ok := FoeByNPC(id); !ok || got != want {
			t.Errorf("FoeByNPC(%d) = %+v (%v), want %+v", id, got, ok, want)
		}
	}
	strong := wtSnap(WtGivenFingernails, 0, -1)
	if o := WtOgreOdds(strong); !o.OK {
		t.Errorf("a strong fighter with ten sharks loses to two ogres: %s", o.Why)
	}
	if o := WtGoradOdds(strong); !o.OK {
		t.Errorf("a strong fighter with ten sharks loses to Gorad: %s", o.Why)
	}
	if ok, why := WtChieftainOdds(strong, WtChieftainTicks); !ok {
		t.Errorf("a strong fighter with ten sharks cannot take the enclave: %s", why)
	}
	// The canary's typical account (Attack 52, Strength 43, Defence 50, Hitpoints 53, 19 Oct 2026) with ten sharks: the price on Gorad is no.
	canary := wtSnap(WtGivenFingernails, 0, -1)
	canary.Levels = map[string]int{"attack": 52, "strength": 43, "defence": 50, "hitpoints": 53, "magic": 58, "agility": 35, "thieving": 78}
	canary.HP, canary.MaxHP = 53, 53
	if o := WtGoradOdds(canary); o.OK {
		t.Errorf("the canary's typical account should not be sent after Gorad (level 68, 80 hitpoints): %+v", o)
	} else if !strings.Contains(o.Why, "Gorad") && !strings.Contains(o.Why, "Ogre") {
		t.Errorf("the reason names no foe: %s", o.Why)
	}
	// Without food nobody passes: the model counts the bites in the pack.
	hungry := strong
	hungry.InvIDs = map[int]int{}
	if o := WtOgreOdds(hungry); o.OK && hungry.Levels["defence"] < 99 {
		t.Log("a strong fighter with no food beats two ogres: the model allows it")
	}
	if ok, _ := WtChieftainOdds(Snapshot{}, WtChieftainTicks); !ok {
		t.Error("an unread account was refused")
	}
	// A weak account with little food cannot be sent through the chieftains.
	weak := wtSnap(WtMadePotion, 0, 6)
	weak.MaxHP, weak.HP = 30, 30
	weak.InvIDs = map[int]int{}
	weak.Levels = map[string]int{"defence": 1, "attack": 1, "strength": 1}
	if ok, _ := WtChieftainOdds(weak, WtChieftainTicks); ok {
		t.Error("a level 3 account with no food was cleared for the enclave")
	}
}

// The fences are squares round the surface chieftains and nothing else; the enclave helper reads its own list.
func TestWatchtowerFencesAndShamans(t *testing.T) {
	rects := WtAvoid()
	if len(rects) != len(WtChieftainSpawns) {
		t.Fatalf("%d rects for %d chieftains", len(rects), len(WtChieftainSpawns))
	}
	for i, r := range rects {
		c := WtChieftainSpawns[i]
		if !r.Contains(c) || !r.Contains(tile.Tile{X: c.X + WtChieftainPad, Z: c.Z - WtChieftainPad}) || r.Contains(tile.Tile{X: c.X + WtChieftainPad + 1, Z: c.Z}) {
			t.Errorf("rect %d %+v is not the chieftain's reach of %d", i, r, WtChieftainPad)
		}
	}
	for _, s := range WtShamanClear {
		if !WtClearOfChieftains(s, 7) {
			t.Errorf("the clear shaman spot %v is within 7 of a chieftain", s)
		}
	}
	for _, s := range WtShamanNear {
		if WtClearOfChieftains(s, 7) {
			t.Errorf("the near shaman spot %v is clear of the chieftains", s)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The decision
// ---------------------------------------------------------------------------------------------------------------------

func wtCustom(t *testing.T, s Snapshot, want string, qty int) Step {
	t.Helper()
	st := DecideWatchtower(s)
	if why := ValidateStep(st); why != "" {
		t.Fatalf("a malformed step %s: %s", st.Describe(), why)
	}
	if st.Kind != StepCustom || st.Custom != want || (qty >= 0 && st.Qty != qty) {
		t.Fatalf("got %s (%s), want custom %s (qty %d)", st.Describe(), st.Kind, want, qty)
	}
	return st
}

func wtTalkOf(t *testing.T, s Snapshot, npc string) Step {
	t.Helper()
	st := DecideWatchtower(s)
	if why := ValidateStep(st); why != "" {
		t.Fatalf("a malformed step %s: %s", st.Describe(), why)
	}
	if st.Kind != StepTalk || st.Stop.Npc != npc {
		t.Fatalf("got %s (%s), want a talk with %s", st.Describe(), st.Kind, npc)
	}
	return st
}

func wtProvisionOf(t *testing.T, s Snapshot, kit string) Step {
	t.Helper()
	st := DecideWatchtower(s)
	if why := ValidateStep(st); why != "" {
		t.Fatalf("a malformed step %s: %s", st.Describe(), why)
	}
	if st.Kind != StepProvision || st.KitName != kit {
		t.Fatalf("got %s (%s %s), want provision %s", st.Describe(), st.Kind, st.KitName, kit)
	}
	return st
}

func wtParks(t *testing.T, s Snapshot, dur time.Duration, mention ...string) Step {
	t.Helper()
	st := DecideWatchtower(s)
	if st.Kind != StepWait || st.ParkFor != dur {
		t.Fatalf("got %s (%s, park %s), want a park of %s mentioning %v", st.Describe(), st.Kind, st.ParkFor, dur, mention)
	}
	for _, m := range mention {
		if !strings.Contains(strings.ToLower(st.Reason), strings.ToLower(m)) {
			t.Errorf("the park's reason %q does not mention %q", st.Reason, m)
		}
	}
	return st
}

func TestWatchtowerStartAndEvidence(t *testing.T) {
	s := wtSnap(WtNotStarted, 0, -1)
	s.Journal = StatusNotStarted
	s.StageKnown = false
	// Stage 0: by the wall, at Agility 18; the park names the level.
	wtCustom(t, s, CustomWtClimb, 1)
	wtParks(t, s.wtLevel("agility", 17), WtStatsPark, "agility 18", "climbing rocks")
	// An unread level is not judged.
	un := s
	un.Levels = map[string]int{}
	wtCustom(t, un, CustomWtClimb, 1)
	// Up the wall to the first floor: the ladder is next; on the wizard's floor, the talk with his three menus.
	wtCustom(t, s.wtAt(2548, 3117, 1), CustomWtClimb, 1)
	st := wtTalkOf(t, s.wtAt(2549, 3112, 2), "Watchtower wizard")
	if !slices.Equal(st.Stop.Prefer, WtStartPrefer) {
		t.Errorf("start prefers %v", st.Stop.Prefer)
	}
	// On the floor the level is not judged again (the account is there).
	wtTalkOf(t, s.wtAt(2549, 3112, 2).wtLevel("agility", 1), "Watchtower wizard")
	// A climb that failed parks.
	wtParks(t, s.wtFailed(CustomWtClimb), WtLegPark, "climb")
	// The tab not coloured yet is a wait; a tab in progress with no stage read is a wait.
	if st := DecideWatchtower(Snapshot{Journal: StatusUnknown}); st.Kind != StepWait || st.ParkFor != 0 {
		t.Errorf("unknown tab: %s", st.Describe())
	}
	if st := DecideWatchtower(Snapshot{Journal: StatusInProgress}); st.Kind != StepWait {
		t.Errorf("unread stage: %s", st.Describe())
	}

	// Stage 1: the bush, then the wizard by the ladders (the quest is started).
	e := wtSnap(WtStarted, 0, -1)
	wtCustom(t, e, CustomWtBush, -1)
	if st := DecideWatchtower(e.wtWith(WtObjCoins, 5)); st.Kind != StepCustom {
		t.Errorf("a pack with coins: %s", st.Describe())
	}
	dep := e
	dep.FreeSlots = 0
	dep.InvIDs = map[int]int{WtObjShark: 10, 1511: 5}
	if st := DecideWatchtower(dep); st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, WtObjFingernails) {
		t.Errorf("a full pack: %s", st.Describe())
	}
	wtCustom(t, e.wtWith(WtObjFingernails, 1), CustomWtClimb, 0)
	st = wtTalkOf(t, e.wtWith(WtObjFingernails, 1).wtAt(2549, 3112, 2), "Watchtower wizard")
	if !slices.Equal(st.Stop.Prefer, WtNailsPrefer) {
		t.Errorf("fingernails prefer %v", st.Stop.Prefer)
	}
	wtParks(t, e.wtFailed(CustomWtBush), WtLegPark)
}

// ---------------------------------------------------------------------------------------------------------------------
// The hunters' prices
// ---------------------------------------------------------------------------------------------------------------------

// The dragons' numbers are the content's, and the expected hit of one is what dragon.rs2 says: three attacks in four a claw or a bite of up to 10, one in four
// dragonfire of up to 30, or up to 50 when the dragon's magic roll beats the account's defence roll, or 5 with a shield.
func TestWatchtowerDragonModel(t *testing.T) {
	if FoeBlueDragon.MaxHit() != 10 || FoeGreaterDemon.MaxHit() != 9 {
		t.Errorf("max hits: dragon %d, demon %d", FoeBlueDragon.MaxHit(), FoeGreaterDemon.MaxHit())
	}
	if WtDragonPeak(false) != 50 || WtDragonPeak(true) != 10 || FoeBlueDragon.PeakHit() != 50 {
		t.Errorf("peaks: unshielded %d, shielded %d, foe %d", WtDragonPeak(false), WtDragonPeak(true), FoeBlueDragon.PeakHit())
	}
	// FoePerAttack must not add a zap of its own for the fire (ZapOneIn is 0): it prices the claw alone.
	low, high := MeleeFighter{Defence: 1}, MeleeFighter{Defence: 99}
	if got, want := FoePerAttack(FoeBlueDragon, low), 0.9566*5; got < want-0.05 || got > want+0.05 {
		t.Errorf("a dragon's claw at Defence 1 is %.2f a swing, want about %.2f", got, want)
	}
	// At Defence 1 an attack is 3/4 of 4.78 and 1/4 of 20.5 (15 and half of 20 at the 55% the magic roll beats a Defence 1 roll): about 8.7.
	if got := WtDragonPerAttack(low, false); got < 8.5 || got > 8.9 {
		t.Errorf("a dragon's attack at Defence 1 is %.2f, want about 8.7", got)
	}
	// More defence, less damage; a shield takes the fire to 5 and the attack to well under half at low defence.
	if !(WtDragonPerAttack(high, false) < WtDragonPerAttack(low, false)) {
		t.Error("Defence 99 takes as much from a dragon as Defence 1")
	}
	if got := WtDragonPerAttack(low, true); got > 0.5*WtDragonPerAttack(low, false) {
		t.Errorf("a shield at Defence 1 takes %.2f of %.2f", got, WtDragonPerAttack(low, false))
	}
	// A tick of one dragon is its attack over four ticks, scaled like every other fight price.
	if got, want := WtDragonPerTick(low, false), WtDragonPerAttack(low, false)*FightIncomingScale/4; got != want {
		t.Errorf("per tick %.3f, want %.3f", got, want)
	}
	// The eat line is above the fire for an account of 67 hitpoints and over, and under it for less (it eats at three quarters).
	if got := WtEnclaveEatLine(70, false); got <= WtDragonPeak(false) {
		t.Errorf("an account of 70 hitpoints eats at %d, under the fire's %d", got, WtDragonPeak(false))
	}
	if got := WtEnclaveEatLine(60, false); got > WtDragonPeak(false) {
		t.Errorf("an account of 60 hitpoints eats at %d, over the fire's maximum", got)
	}
	if got := WtEnclaveEatLine(53, true); got != wtSustain.EatAt {
		t.Errorf("a shielded account of 53 hitpoints eats at %d, want the module's %d", got, wtSustain.EatAt)
	}
}

// The visits are the walks they name: each is a stay, its walk no longer than the stay, and the work contains the exit.
func TestWatchtowerVisitsAreConsistent(t *testing.T) {
	for _, v := range []WtVisit{WtVisitExit, WtVisitWork, WtVisitRock, WtVisitRobe} {
		if v.Name == "" || v.Walk <= 0 || v.Ticks < v.Walk || v.Dragon <= 0 || v.Dragon > 2*v.Ticks || v.Demon < 0 || v.Chief < 0 {
			t.Errorf("visit %+v", v)
		}
	}
	if WtVisitWork.Ticks <= WtVisitRock.Ticks || WtVisitWork.Dragon <= WtVisitExit.Dragon || WtVisitWork.Ticks <= WtVisitExit.Ticks {
		t.Error("the whole work is not more than the rock or the exit")
	}
}

// What the enclave's price says of the fleet's accounts: the typical canary account cannot walk out of the nest at all (its bites would not stay above the
// fire), a strong account with ten sharks can walk out but not work there, and only a maxed account with the shield and food can do the whole of it.
func TestWatchtowerEnclaveOdds(t *testing.T) {
	strong := wtSnap(WtSkavidCrystal, 0, -1)
	canary := wtCanary(strong)
	maxed := wtShielded(strong)
	if ok, why := WtEnclaveOdds(canary, WtVisitExit); ok || !strings.Contains(why, "blue dragons") || !strings.Contains(why, "50") {
		t.Errorf("the canary walks out of the nest: %v %q", ok, why)
	}
	if ok, why := WtEnclaveOdds(strong, WtVisitExit); !ok {
		t.Errorf("a strong account with ten sharks cannot walk out of the nest: %s", why)
	}
	if ok, why := WtEnclaveOdds(strong, WtVisitWork); ok || !strings.Contains(why, "six shamans") || !strings.Contains(why, "dragon-ticks") {
		t.Errorf("a strong account without a shield works in the nest: %v %q", ok, why)
	}
	for _, v := range []WtVisit{WtVisitExit, WtVisitWork, WtVisitRock, WtVisitRobe} {
		if ok, why := WtEnclaveOdds(maxed, v); !ok {
			t.Errorf("the maxed shielded account is refused %s: %s", v.Name, why)
		}
	}
	// The shield is the difference; the same account without it is refused the work.
	bare := maxed
	bare.WornIDs = map[int]int{}
	if ok, _ := WtEnclaveOdds(bare, WtVisitWork); ok {
		t.Error("a maxed account without the shield does the whole of the nest")
	}
	// Food is the other: the same shielded account with two sharks cannot pay for the work.
	thin := maxed
	thin.InvIDs = map[int]int{WtObjShark: 2}
	if ok, _ := WtEnclaveOdds(thin, WtVisitWork); ok {
		t.Error("a shielded account with two sharks does the whole of the nest")
	}
	// An unread account is not judged, as everywhere here.
	if ok, _ := WtEnclaveOdds(Snapshot{}, WtVisitWork); !ok {
		t.Error("an unread account was refused")
	}
	// Hitpoints that the bites cannot keep above the fire are refused whatever the food (60 hitpoints eat at 45), and the reason says so.
	frail := maxed
	frail.WornIDs = map[int]int{}
	frail.HP, frail.MaxHP = 60, 60
	if ok, why := WtEnclaveOdds(frail, WtVisitExit); ok || !strings.Contains(why, "would eat at 45") {
		t.Errorf("60 hitpoints under the fire: %v %q", ok, why)
	}
}

// The sites' prices: the typical canary account is refused at Og (and at the camp and the island, where the same four ogres stand) and the berries are the
// one trip it can take (no chat); a strong account takes them all; the unfed stretch of the chat is checked apart from the whole.
func TestWatchtowerSitePrices(t *testing.T) {
	strong := wtSnap(WtGivenFingernails, 0, -1)
	canary := wtCanary(strong)
	if WtOgreVolley(4) != 28 {
		t.Errorf("a volley of four ogres is %d", WtOgreVolley(4))
	}
	for _, sites := range [][]WtSite{{WtSiteOg}, {WtSiteBranch, WtSiteGrew}, {WtSiteBranch, WtSiteBerries}, wtCampTrip()} {
		if ok, why := WtTripOdds(strong, sites...); !ok {
			t.Errorf("a strong account with ten sharks is refused %s: %s", wtSiteNames(sites), why)
		}
	}
	if ok, why := WtTripOdds(canary, WtSiteOg); ok || !strings.Contains(why, "Og's stand holds 4 ogres") || !strings.Contains(why, "chat") {
		t.Errorf("the canary at Og: %v %q", ok, why)
	}
	if ok, _ := WtTripOdds(canary, wtCampTrip()...); ok {
		t.Error("the canary spends the camp's chats among four ogres")
	}
	if ok, why := WtTripOdds(canary, WtSiteBranch, WtSiteBerries); !ok {
		t.Errorf("the canary's berry trip (no chat) is refused: %s", why)
	}
	// Without food nobody has a budget.
	hungry := strong
	hungry.InvIDs = map[int]int{}
	hungry.HP, hungry.MaxHP = 30, 30
	if ok, _ := WtTripOdds(hungry, WtSiteBranch, WtSiteGrew); ok {
		t.Error("an account of 30 hitpoints with no food crosses to the island")
	}
	if ok, _ := WtTripOdds(Snapshot{}, WtSiteOg); !ok {
		t.Error("an unread account was refused")
	}
	if ok, _ := WtTripOdds(strong); !ok {
		t.Error("an empty trip was refused")
	}
	// A site's names read in a sentence.
	if got := wtSiteNames([]WtSite{WtSiteCaveMouth, WtSiteCamp, WtSiteCaveMouth}); got != "the camp's cave mouth, Toban's camp and the camp's cave mouth" {
		t.Errorf("site names %q", got)
	}
}

// Gorad is fought with the ogres of his corner at the account's back, and that is in the price: the same account fares worse than against Gorad alone.
func TestWatchtowerGoradPriceCountsTheCampOgres(t *testing.T) {
	s := wtSnap(WtGivenFingernails, 0, -1)
	s.Levels = map[string]int{"attack": 70, "strength": 70, "defence": 70, "hitpoints": 60, "magic": 50}
	s.HP, s.MaxHP = 60, 60
	alone := MeleeChain(s, []MeleeFoe{FoeGorad}, wtSustain)
	amid := WtGoradOdds(s)
	if len(amid.Sims) == 0 || len(alone.Sims) == 0 {
		t.Fatalf("no fights played: %+v %+v", alone, amid)
	}
	if !(amid.Sims[0].MinHP < alone.Sims[0].MinHP) && !(amid.Sims[0].BitesUsed > alone.Sims[0].BitesUsed) {
		t.Errorf("the ogres at his back cost nothing: alone %+v, amid %+v", alone.Sims[0], amid.Sims[0])
	}
	if WtSiteGorad.Ogres != 2 {
		t.Errorf("Gorad's corner holds %d ogres", WtSiteGorad.Ogres)
	}
}

// SimulateFight's Extra is hitpoints a tick from hunters that are not fought: zero is the old fight, and more is lower and shorter.
func TestSimulateFightExtraAttackers(t *testing.T) {
	in := FightSimInput{Foe: FoeGorad, Fighter: MeleeFighter{Attack: 70, Strength: 70, Defence: 70}, HP: 60, MaxHP: 60,
		Phases: []FightPhase{{Name: "melee", PerAction: 2, ActionTicks: 4}}, Bites: []int{20, 20, 20}, EatLine: 28}
	base := SimulateFight(in)
	again := SimulateFight(in)
	if base.Ticks != again.Ticks || base.MinHP != again.MinHP {
		t.Fatal("the simulation is not deterministic")
	}
	in.Extra = 0.5
	amid := SimulateFight(in)
	if !(amid.MinHP < base.MinHP) && !(amid.BitesUsed > base.BitesUsed) {
		t.Errorf("an extra half a hitpoint a tick changed nothing: base %+v, amid %+v", base, amid)
	}
	in.Extra = 50
	if res := SimulateFight(in); res.Won || res.How != "dies" {
		t.Errorf("fifty hitpoints a tick: %+v", res)
	}
}
