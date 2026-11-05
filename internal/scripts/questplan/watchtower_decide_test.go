package questplan

import (
	"slices"
	"strings"
	"testing"
)

// The Watch Tower's decision table, stage by stage. Every row is a snapshot an account can be in and the step the module asks for there; the park rows
// carry the words of the reason, which is what the ladder shows the operator.

// wtWeak is an account the ogres' country should refuse: low levels and little health.
func wtWeak(s Snapshot) Snapshot {
	s.Levels = map[string]int{"attack": 30, "strength": 30, "defence": 30, "hitpoints": 40, "magic": 40, "agility": 35, "thieving": 77, "herblore": 50, "mining": 60}
	s.HP, s.MaxHP = 40, 40
	return s
}

// wtShielded is the account the dragons' nest can be asked of: Defence 99, 99 hitpoints, an anti-dragon shield worn and sixteen sharks.
func wtShielded(s Snapshot) Snapshot {
	lv := map[string]int{}
	for k, v := range s.Levels {
		lv[k] = v
	}
	lv["attack"], lv["strength"], lv["defence"], lv["hitpoints"] = 99, 99, 99, 99
	s.Levels = lv
	s.HP, s.MaxHP = 99, 99
	s.WornIDs = map[int]int{WtObjAntiDragonShield: 1}
	inv := map[int]int{}
	for k, v := range s.InvIDs {
		inv[k] = v
	}
	inv[WtObjShark] = 16
	s.InvIDs = inv
	return s
}

// wtCanary is the unproven cohort's typical account on 19 Oct 2026: Attack 52, Strength 43, Defence 50, Hitpoints 53.
func wtCanary(s Snapshot) Snapshot {
	s.Levels = map[string]int{"attack": 52, "strength": 43, "defence": 50, "hitpoints": 53, "magic": 58, "agility": 35, "thieving": 78, "herblore": 1, "mining": 3}
	s.HP, s.MaxHP = 53, 53
	return s
}

func TestWatchtowerTribesAreAskedInTheMapsOrder(t *testing.T) {
	const bones = WtObjDragonBones
	base := wtSnap(WtGivenFingernails, 0, -1)
	// The bones first, before any risk: nobody sells them.
	st := wtProvisionOf(t, base, KitWtBones)
	if len(st.Kit) != 1 || st.Kit[0].ID != WtObjDragonBones || len(st.Kit[0].Sources) != 0 {
		t.Errorf("the bones kit %+v: one line, no shop", st.Kit)
	}
	wtParks(t, base.wtBlocked("provision:"+KitWtBones), WtSupplyPark, "dragon bones")
	wtParks(t, base.wtFailed("provision:"+KitWtBones), WtSupplyPark, "dragon bones", "no shop")
	// A round that succeeded and left a line short is a line a leg used up (the rope of each swing): it is asked again, not parked on.
	wtProvisionOf(t, base.wtRan("provision:"+KitWtBones), KitWtBones)
	// The bank's bones are drawn by the same round (the provisioner reads the bank first); held, Og is next.
	st = wtTalkOf(t, base.wtWith(bones, 1), "Og")
	if !slices.Contains(st.Stop.Prefer, "I seek entrance to the city of ogres.") || slices.Contains(st.Stop.Prefer, "I have come to kill you.") {
		t.Errorf("Og's preferences %v", st.Stop.Prefer)
	}
	// An account the ogres' country refuses is parked with the model's reason before it walks anywhere.
	wtParks(t, wtWeak(base.wtWith(bones, 1)), WtOddsPark, "lose to ogre")
	// The canary (Defence 50, 53 hitpoints) loses 34 hitpoints of its 53 in the twenty ticks of Og's first talk among his four guards, and nothing is eaten in
	// a chat: refused at Og, with the number of ogres in the reason.
	wtParks(t, wtCanary(base.wtWith(bones, 1)), WtOddsPark, "og's stand", "4 ogres")
	// Food first: none in the pack asks the bank and the mule once.
	hungry := base.wtWith(bones, 1)
	hungry.InvIDs = map[int]int{bones: 1}
	wtProvisionOf(t, hungry, KitWtFood)
	// A round that found none (the bank and the mule hold no food) parks the quest with that reason rather than sending the account in with nothing.
	wtParks(t, hungry.wtRan("provision:"+KitWtFood), WtSupplyPark, "no food")
	if st := DecideWatchtower(hungry.wtWith(WtObjShark, 3).wtRan("provision:" + KitWtFood)); st.Kind != StepTalk {
		t.Errorf("after the food round the account goes on: %s", st.Describe())
	}

	// Og spoken, the key in the pack: Grew next. The rope first (a shop has it), the swing, the talk on the island.
	grew := base.wtWith(bones, 1).wtWith(WtObjTobanKey, 1)
	grew.Stage = WtState{Stage: WtGivenFingernails, Flags: WtOgSpoken, Left: -1}.Pack()
	st = wtProvisionOf(t, grew, KitWtIsland)
	if len(st.Kit) != 1 || st.Kit[0].ID != WtObjRope || len(st.Kit[0].Sources) == 0 {
		t.Errorf("the island kit %+v: a rope with a shop", st.Kit)
	}
	withRope := grew.wtWith(WtObjRope, 1)
	wtCustom(t, withRope, CustomWtIslandIn, 0)
	wtParks(t, withRope.wtLevel("agility", 9), WtStatsPark, "agility 10", "swing back")
	wtTalkOf(t, withRope.wtAt(2513, 3084, 0), "Grew")

	// Both spoken, the key in the pack and the gold not: the camp, the chest, Toban, Gorad.
	camp := base.wtWith(bones, 1).wtWith(WtObjTobanKey, 1)
	camp.Stage = WtState{Stage: WtGivenFingernails, Flags: WtOgSpoken | WtGrewSpoken, Left: -1}.Pack()
	wtCustom(t, camp, CustomWtCampIn, 0)
	wtCustom(t, camp.wtAt(2576, 3029, 0), CustomWtChest, 0)
	// Gold in the pack, Toban not yet spoken to and Gorad's tooth wanted: Gorad comes BEFORE Toban, so that Toban's relic piece is not in the pack while
	// the fight is on (a death at Gorad then costs the tooth and the gold, which the quest asks again, and not a piece the wizard has no copy of).
	got := camp.wtWith(WtObjStolenGold, 1).wtWith(WtObjTobanKey, 0).wtAt(2576, 3029, 0)
	wtCustom(t, got, CustomWtGorad, 0)
	// With the tooth in the pack (or Grew helped), Toban's talk is next.
	toothed := got.wtWith(WtObjTooth, 1)
	st = wtTalkOf(t, toothed, "Toban")
	if !slices.Contains(st.Stop.Prefer, "I could do something for you...") {
		t.Errorf("Toban's preferences %v", st.Stop.Prefer)
	}
	// The fight is topped up for first: a hurt account in the camp eats before it swings.
	hurt := got
	hurt.HP = 40
	if st := DecideWatchtower(hurt); st.Kind != StepOpHeld || st.Op != "Eat" {
		t.Errorf("a hurt account in the camp: %s", st.Describe())
	}
	// Toban helped: Gorad alone is left, and is priced from OUTSIDE (the cave): the canary's account is refused with Gorad's name in the reason and walks
	// nowhere; the strong one goes in. Inside the camp nothing is priced again.
	gorad := base.wtWith(WtObjStolenGold, 1).wtAt(2576, 3029, 0)
	gorad.Stage = WtState{Stage: WtGivenFingernails, Flags: WtOgSpoken | WtGrewSpoken | WtTobanHelped | WtTobanSpoken, Left: -1}.Pack()
	wtCustom(t, gorad, CustomWtGorad, 0)
	wtCustom(t, wtCanary(gorad), CustomWtGorad, 0)
	wtCustom(t, gorad.wtAt(2613, 3094, 0), CustomWtCampIn, 0)
	// An account of Defence 85 and 85 hitpoints with Attack and Strength 40 passes the road and the camp's four ogres and cannot finish Gorad (80
	// hitpoints) before the two ogres at his back finish it: it is refused at the cave with Gorad's name in the reason.
	outside := gorad.wtAt(2613, 3094, 0)
	outside.Levels = map[string]int{"attack": 40, "strength": 40, "defence": 85, "hitpoints": 85, "magic": 50, "agility": 35}
	outside.HP, outside.MaxHP = 85, 85
	if o := WtGoradOdds(outside); o.OK {
		t.Fatalf("a Defence 85 account with Attack 40 beats Gorad among the camp's ogres: %+v", o)
	}
	wtParks(t, outside, WtOddsPark, "gorad")
	// The canary's account is refused for the camp's talking before Gorad is looked at.
	wtParks(t, wtCanary(gorad.wtAt(2613, 3094, 0)), WtOddsPark, "toban's camp")
	// A hurt, foodless account in the camp is walked out, not parked or sent on.
	stranded := gorad
	stranded.HP, stranded.InvIDs = 12, map[int]int{WtObjStolenGold: 1}
	if st := DecideWatchtower(stranded); st.Kind != StepWalk {
		t.Errorf("a hurt account with no food in the camp: %s", st.Describe())
	}

	// The tooth in the pack: Grew's island, to give it. The gold in the pack: Og. The parts: the wizard, each.
	tooth := base.wtWith(WtObjTooth, 1).wtWith(WtObjStolenGold, 1).wtWith(WtObjRope, 1)
	tooth.Stage = WtState{Stage: WtGivenFingernails, Flags: WtOgSpoken | WtGrewSpoken | WtTobanHelped | WtTobanSpoken, Left: -1}.Pack()
	wtCustom(t, tooth, CustomWtIslandIn, 0)
	wtTalkOf(t, tooth.wtAt(2513, 3084, 0), "Grew")
	// The island trip is priced from the bank of the branch, with the real number of ogres: the canary's account (Defence 50, 53 hitpoints) is refused
	// before the swing, and not once it is on the island (where a park would leave it among four ogres).
	wtParks(t, wtCanary(tooth), WtOddsPark, "grew")
	wtTalkOf(t, wtCanary(tooth).wtAt(2513, 3084, 0), "Grew")
	goldOnly := base.wtWith(WtObjStolenGold, 1)
	goldOnly.Stage = WtState{Stage: WtGivenFingernails, Flags: WtOgSpoken | WtGrewHelped | WtGrewSpoken | WtTobanHelped | WtTobanSpoken, Left: -1}.Pack()
	wtTalkOf(t, goldOnly, "Og")
	parts := base
	parts.Stage = WtState{Stage: WtGivenFingernails, Flags: WtOgHelped | WtOgSpoken | WtGrewHelped | WtGrewSpoken | WtTobanHelped | WtTobanSpoken, Left: -1}.Pack()
	wtCustom(t, parts.wtWith(WtObjPart2, 1).wtWith(WtObjPart3, 1), WtGivePartName(WtObjPart2), WtObjPart2)
	wtCustom(t, parts.wtWith(WtObjPart3, 1), WtGivePartName(WtObjPart3), WtObjPart3)
	// A piece the wizard refused ("I already have that part") is kept and never given again, and does not stop the next piece: it fails only its own custom.
	wtCustom(t, parts.wtWith(WtObjPart2, 1).wtWith(WtObjPart3, 1).wtFailed(WtGivePartName(WtObjPart2)), WtGivePartName(WtObjPart3), WtObjPart3)
	if st := DecideWatchtower(parts.wtWith(WtObjPart2, 1).wtFailed(WtGivePartName(WtObjPart2))); st.Kind == StepCustom && st.Custom == WtGivePartName(WtObjPart2) {
		t.Errorf("a refused piece was given again: %s", st.Describe())
	}
	// A piece in the bank is drawn first.
	wtProvisionOf(t, parts.wtBank(WtObjPart3, 1), KitWtParts)
	// No piece, no relic: the journal cannot tell stage 3 (the relic made and lost: the wizard's menu offers another) from stage 2 (a piece lost after its tribe
	// was helped: the wizard has no copy). The wizard is asked once; then each giver once, in the order of their cost; then a day's park with the reason.
	wtCustom(t, parts, CustomWtRelicBack, 0)
	asked := parts.wtFailed(CustomWtRelicBack)
	wtCustom(t, asked, CustomWtAskOg, 0)
	wtCustom(t, asked.wtRan(CustomWtAskOg), CustomWtAskToban, 0)
	grewNext := asked.wtRan(CustomWtAskOg, CustomWtAskToban)
	wtProvisionOf(t, grewNext, KitWtIsland)
	wtCustom(t, grewNext.wtWith(WtObjRope, 1), CustomWtAskGrew, 0)
	wtParks(t, grewNext.wtWith(WtObjRope, 1).wtFailed(CustomWtAskGrew), WtRelicPark, "relic", "lost")
	// A giver is not asked for a piece the account holds.
	wtCustom(t, asked.wtWith(WtObjPart1, 1).wtFailed(WtGivePartName(WtObjPart1)), CustomWtAskToban, 0)
	// Nothing in this ends in the old half-hour loop: whatever has been tried, the fall-through is the day.
	for _, ran := range [][]string{{CustomWtAskOg}, {CustomWtAskOg, CustomWtAskToban}, {CustomWtAskOg, CustomWtAskToban, CustomWtAskGrew}} {
		st := DecideWatchtower(asked.wtRan(ran...).wtWith(WtObjRope, 1))
		if st.Kind == StepWait && st.ParkFor == WtLegPark {
			t.Errorf("after asking %v the lost piece parks for the half hour: %s", ran, st.Describe())
		}
	}
	wtParks(t, asked.wtRan(CustomWtAskOg, CustomWtAskToban, CustomWtAskGrew), WtRelicPark, "relic")
	// A lost key, with no gold either: Og gives another.
	lost := base
	lost.Stage = WtState{Stage: WtGivenFingernails, Flags: WtOgSpoken, Left: -1}.Pack()
	lost = lost.wtWith(bones, 1)
	wtTalkOf(t, lost, "Og")
}

func TestWatchtowerRelicGateAndCity(t *testing.T) {
	// The relic made: stage 3 reads as 2, and the relic decides. In the bank it is drawn first.
	relic := wtSnap(WtGivenFingernails, 0, -1)
	wtCustom(t, relic.wtWith(WtObjRelic, 1), CustomWtRelicGate, 0)
	wtProvisionOf(t, relic.wtBank(WtObjRelic, 1), KitWtRelic)
	wtParks(t, wtWeak(relic.wtWith(WtObjRelic, 1)), WtOddsPark, "ogre")

	// Stage 4: the rock cake (Thieving 15), the battlement, the chasm (Agility 25).
	city := wtSnap(WtGivenRelic, WtLookingRelic, -1)
	wtParks(t, city.wtLevel("thieving", 14), WtStatsPark, "thieving 15", "rock cake")
	wtCustom(t, city, CustomWtCake, 0)
	wtParks(t, wtWeak(city), WtOddsPark)
	wtCustom(t, city.wtWith(WtObjRockCake, 1), CustomWtBattlement, 0)
	paid := city
	paid.Stage = WtState{Stage: WtGivenRelic, Flags: WtMarketAsked | WtMarketPaid, Left: -1}.Pack()
	st := wtProvisionOf(t, paid, KitWtCity)
	ids := []int{}
	for _, l := range st.Kit {
		ids = append(ids, l.ID)
	}
	if !slices.Contains(ids, WtObjCoins) || !slices.Contains(ids, WtObjDeathRune) {
		t.Errorf("the city kit %v lacks coins or a death rune", ids)
	}
	kitted := paid.wtWith(WtObjCoins, 200).wtWith(WtObjDeathRune, 1)
	wtCustom(t, kitted, CustomWtJump, 0)
	wtParks(t, kitted.wtLevel("agility", 24), WtStatsPark, "agility 25", "chasm")
	// Inside the wall (the lower city) the gift is not asked again.
	wtCustom(t, kitted.wtAt(2508, 3011, 0), CustomWtJump, 0)
	// Across the chasm: the City guard's riddle.
	st = wtTalkOf(t, kitted.wtAt(2530, 3029, 0), "City guard")
	if !slices.Contains(st.Stop.Prefer, "I seek passage into the skavid caves.") {
		t.Errorf("the guard's preferences %v", st.Stop.Prefer)
	}

	// Stage 5: the rune on the guard, on his side of the chasm; the jump if the account is not there.
	riddle := wtSnap(WtGivenRiddle, 0, -1).wtWith(WtObjDeathRune, 1).wtWith(WtObjCoins, 200)
	wtCustom(t, riddle, CustomWtJump, 0)
	wtCustom(t, riddle.wtAt(2530, 3029, 0), CustomWtRiddleGive, 0)
	norune := wtSnap(WtGivenRiddle, 0, -1).wtAt(2530, 3029, 0)
	wtProvisionOf(t, norune, KitWtCity)
	// Parked OUTSIDE the pocket: on the City guard's side the account would stand among the ogres, so it is walked out first and the park is made at the bank.
	if st := DecideWatchtower(norune.wtBlocked("provision:" + KitWtCity)); st.Kind != StepWalk || st.Anchor != WtBank {
		t.Errorf("a blocked kit on the City guard's side: %s", st.Describe())
	}
	wtParks(t, norune.wtBlocked("provision:"+KitWtCity).wtAt(2613, 3094, 0), WtSupplyPark, "death rune")
}

func TestWatchtowerCavesInOrder(t *testing.T) {
	base := wtSnap(WtSolvedRiddle, 0, -1).wtWith(WtObjMap, 1)
	// The light first: the candle by the tower is a ground source of the kit.
	st := wtProvisionOf(t, base, KitWtCaves)
	hasCandle := false
	for _, l := range st.Kit {
		if l.ID == WtObjLitCandle && len(l.Sources) > 0 && l.Sources[0].Kind == SourceGround {
			hasCandle = true
		}
	}
	if !hasCandle {
		t.Errorf("the caves kit %+v has no candle with a ground source", st.Kit)
	}
	lit := base.wtWith(WtObjLitCandle, 1)
	wtCustom(t, lit, CustomWtCave, 5)
	// A lit torch will do for the light: no candle is asked.
	wtCustom(t, base.wtWith(WtObjLitTorch, 1), CustomWtCave, 5)
	// The words, in order of the flags: the scared skavid teaches, the talkers follow, the mad skavid is last.
	for _, c := range []struct {
		flags uint32
		want  int
	}{
		{WtLearning, 1}, {WtLearning | WtLearnedNod, 2}, {WtLearning | WtLearnedNod | WtLearnedIg, 3},
		{WtLearning | WtLearnedNod | WtLearnedIg | WtLearnedAr, 4},
	} {
		s := lit
		s.Stage = WtState{Stage: WtSolvedRiddle, Flags: c.flags, Left: -1}.Pack()
		wtCustom(t, s, CustomWtCave, c.want)
	}
	all := lit
	all.Stage = WtState{Stage: WtSolvedRiddle, Flags: WtLearning | WtLearnedNod | WtLearnedIg | WtLearnedAr | WtLearnedCur, Left: -1}.Pack()
	st = wtProvisionOf(t, all, KitWtBar)
	hasBar := false
	for _, l := range st.Kit {
		hasBar = hasBar || (l.ID == WtObjGoldBar && len(l.Sources) == 0)
	}
	if !hasBar {
		t.Errorf("the bar kit %+v: a gold bar from the bank or the mule", st.Kit)
	}
	wtCustom(t, all.wtWith(WtObjGoldBar, 1), CustomWtMad, 0)
	// A lost map: drawn from the bank, or asked of the City guard across the chasm.
	nomap := wtSnap(WtSolvedRiddle, 0, -1).wtWith(WtObjLitCandle, 1).wtWith(WtObjCoins, 200).wtWith(WtObjDeathRune, 1)
	wtProvisionOf(t, nomap.wtBank(WtObjMap, 1), KitWtMap)
	wtCustom(t, nomap, CustomWtJump, 0)
	st = wtTalkOf(t, nomap.wtAt(2530, 3029, 0), "City guard")
	if !slices.Contains(st.Stop.Prefer, "I have lost the map you gave me.") {
		t.Errorf("the map talk prefers %v", st.Stop.Prefer)
	}
	// An account the country refuses is parked, with the first leg.
	wtParks(t, wtWeak(lit), WtOddsPark)
}

func TestWatchtowerEnclaveEntryAndTheWizard(t *testing.T) {
	// Stage 7: a nightshade from the cave, then the guard (the chieftains are priced).
	s := wtSnap(WtSkavidCrystal, 0, -1).wtWith(WtObjMap, 1).wtWith(WtObjLitCandle, 1)
	wtCustom(t, s, CustomWtNightshade, 0)
	wtCustom(t, s.wtWith(WtObjNightshade, 1), CustomWtEnclaveIn, 0)
	wtCustom(t, s.wtWith(WtObjNightshade, 1).wtAt(2588, 9410, 0), CustomWtEnclaveOut, 0)
	weak := wtWeak(s.wtWith(WtObjNightshade, 1))
	weak.MaxHP, weak.HP = 25, 25
	weak.InvIDs = map[int]int{WtObjNightshade: 1, WtObjShark: 5}
	wtParks(t, weak, WtOddsPark)
	// The enclave is priced BEFORE the nightshade is fetched: the canary's account is parked at the enclave itself, with the dragons in the reason, and no
	// walk to cave 2 is asked of it. It is priced as the walk it is (58 tiles, 55 dragon-ticks), not as a chieftain's attention.
	st := wtParks(t, wtCanary(s), WtEnclavePark, "blue dragons", "fire")
	if !strings.Contains(st.Reason, "50") {
		t.Errorf("the reason does not name the fire's maximum: %s", st.Reason)
	}
	wtParks(t, wtCanary(s.wtWith(WtObjNightshade, 1)), WtEnclavePark, "blue dragons")
	// A strong account (Defence 75, 70 hitpoints, ten sharks) can walk out of the nest and is let in; an account whose bites cannot stay above the fire is not.
	wtCustom(t, s, CustomWtNightshade, 0)
	thin := s
	thin.Levels = map[string]int{"attack": 75, "strength": 75, "defence": 75, "hitpoints": 60, "magic": 70}
	thin.HP, thin.MaxHP = 60, 60
	wtParks(t, thin, WtEnclavePark, "eat at")
	// With the anti-dragon shield the fire is 5 and the same thin account goes in.
	shielded := thin
	shielded.WornIDs = map[int]int{WtObjAntiDragonShield: 1}
	wtCustom(t, shielded, CustomWtNightshade, 0)
	// No park inside the pocket: an account already in the nest is walked out first, and the park is made outside.
	inside := wtCanary(s).wtWith(WtObjNightshade, 1).wtAt(2588, 9410, 0)
	wtCustom(t, inside, CustomWtEnclaveOut, 0)
	// A top-up first: a strong account that is hurt eats at the bank before it goes towards the dragons.
	hurt := wtShielded(s.wtWith(WtObjNightshade, 1))
	hurt.HP = 35
	if st := DecideWatchtower(hurt); st.Kind != StepOpHeld || st.Op != "Eat" {
		t.Errorf("a hurt account before the enclave: %s", st.Describe())
	}
	// Stage 8: the wizard's floor by the ladders, then the talk.
	e := wtSnap(WtFedNightshade, 0, -1)
	wtCustom(t, e, CustomWtClimb, 0)
	wtCustom(t, e.wtAt(2588, 9410, 0), CustomWtClimb, 0)
	wtTalkOf(t, e.wtAt(2549, 3112, 2), "Watchtower wizard")
}

func TestWatchtowerPotion(t *testing.T) {
	base := wtSnap(WtLearnedPotion, 0, -1)
	// Herblore 14 first: nothing is fetched for a potion the account cannot mix.
	wtParks(t, base.wtLevel("herblore", 13), WtStatsPark, "herblore 14", "ogre potion")
	// The ogre potion in the pack goes to the wizard.
	wtCustom(t, base.wtWith(WtObjOgrePotion, 1), CustomWtInfuse, 0)
	// The scarce lines first and alone: the guam leaf and the bat bones, which only a bank or the mule has.
	st := wtProvisionOf(t, base, KitWtScarce)
	ids := []int{}
	for _, l := range st.Kit {
		ids = append(ids, l.ID)
		if len(l.Sources) != 0 {
			t.Errorf("scarce line %s has sources %v", l.Name, l.Sources)
		}
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int{WtObjGuamLeaf, WtObjBatBones}) {
		t.Errorf("the scarce kit is %v", ids)
	}
	wtParks(t, base.wtBlocked("provision:"+KitWtScarce), WtSupplyPark, "guam leaf", "bat bones")
	// With both, the berries (a trip to the island, a rope), then the rest of the kit, then the brew.
	scarce := base.wtWith(WtObjGuamLeaf, 1).wtWith(WtObjBatBones, 1)
	st = wtProvisionOf(t, scarce, KitWtIsland)
	if len(st.Kit) != 1 || st.Kit[0].ID != WtObjRope {
		t.Errorf("the island kit %+v", st.Kit)
	}
	wtCustom(t, scarce.wtWith(WtObjRope, 1), CustomWtBerries, WtJangerberryQty)
	wtParks(t, scarce.wtWith(WtObjRope, 1).wtLevel("agility", 9), WtStatsPark, "agility 10")
	berried := scarce.wtWith(WtObjJangerberries, 2)
	st = wtProvisionOf(t, berried, KitWtPotion)
	ids = ids[:0]
	for _, l := range st.Kit {
		ids = append(ids, l.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int{WtObjVialWater, WtObjPestle}) {
		t.Errorf("the potion kit is %v", ids)
	}
	ready := berried.wtWith(WtObjVialWater, 1).wtWith(WtObjPestle, 1)
	wtCustom(t, ready, CustomWtBrew, 0)
	// Part-way through the chain, only what is left is asked for: a guam vial needs no leaf and no vial, ground bones no bat bones and no pestle.
	mid := base.wtWith(WtObjGuamJangerVial, 1).wtWith(WtObjGroundBones, 1)
	wtCustom(t, mid, CustomWtBrew, 0)
	mid2 := base.wtWith(WtObjGuamVial, 1).wtWith(WtObjBatBones, 1).wtWith(WtObjPestle, 1)
	wtProvisionOf(t, mid2, KitWtIsland)
	wtCustom(t, mid2.wtWith(WtObjRope, 1), CustomWtBerries, WtJangerberryQty)
	wtCustom(t, mid2.wtWith(WtObjJangerberries, 1), CustomWtBrew, 0)
}

func TestWatchtowerShamansAndCrystals(t *testing.T) {
	base := wtShielded(wtSnap(WtMadePotion, 0, 6)).wtWith(WtObjMagicPotion, 1)
	// No magic potion: back to the chain, or out of the enclave to brew it.
	nopotion := wtShielded(wtSnap(WtMadePotion, 0, 4))
	wtProvisionOf(t, nopotion, KitWtScarce)
	wtCustom(t, nopotion.wtAt(2588, 9410, 0), CustomWtEnclaveOut, 0)
	// Magic 14, a pickaxe in the pack for the rock, a nightshade for the guard.
	wtParks(t, base.wtLevel("magic", 13), WtStatsPark, "magic 14", "shamans")
	wtProvisionOf(t, base, KitWtRock)
	pick := base.wtWith(WtObjPickaxe, 1)
	wtProvisionOf(t, pick, KitWtCaves)
	wtCustom(t, pick.wtWith(WtObjMap, 1).wtWith(WtObjLitCandle, 1), CustomWtNightshade, 0)
	night := pick.wtWith(WtObjNightshade, 1)
	wtCustom(t, night, CustomWtDissolve, 6)
	// Inside, with the potion: on to the shamans, no nightshade wanted; unread shamans count as six.
	wtCustom(t, pick.wtAt(2588, 9410, 0), CustomWtDissolve, 6)
	unread := wtShielded(wtSnap(WtMadePotion, 0, -1)).wtWith(WtObjMagicPotion, 1).wtWith(WtObjPickaxe, 1).wtAt(2588, 9410, 0)
	wtCustom(t, unread, CustomWtDissolve, 6)
	wtCustom(t, wtShielded(wtSnap(WtMadePotion, 0, 2)).wtWith(WtObjMagicPotion, 1).wtWith(WtObjPickaxe, 1).wtAt(2588, 9410, 0), CustomWtDissolve, 2)
	// The nest is priced as a whole before the door: the six shamans, the rock and the way out. An account the model refuses is parked at the door, the strong
	// one without a shield included (it can leave, it cannot work), and nothing is fetched for it.
	weak := wtWeak(night)
	weak.InvIDs = map[int]int{WtObjMagicPotion: 1, WtObjPickaxe: 1, WtObjNightshade: 1, WtObjShark: 5}
	weak.MaxHP, weak.HP = 25, 25
	wtParks(t, weak, WtOddsPark)
	unshielded := night
	unshielded.WornIDs = map[int]int{}
	wtParks(t, unshielded.wtWith(WtObjShark, 10), WtEnclavePark, "six shamans", "dragon")
	wtParks(t, wtCanary(night).wtWith(WtObjPickaxe, 1), WtEnclavePark, "blue dragons")

	// All six gone: the rock (Mining 40, a pickaxe), the robe if the third crystal was lost, then out.
	done := wtShielded(wtSnap(WtMadePotion, 0, 0)).wtWith(WtObjCrystal3, 1).wtWith(WtObjPickaxe, 1).wtWith(WtObjNightshade, 1)
	wtCustom(t, done, CustomWtRock, 0)
	wtParks(t, done.wtLevel("mining", 39), WtStatsPark, "mining 40", "rock of dalgroth")
	wtCustom(t, done.wtAt(2590, 9448, 0), CustomWtRock, 0)
	// A level the rock wants, found with the account INSIDE the nest, walks it out first: the park is made outside, never among the dragons.
	wtCustom(t, done.wtLevel("mining", 39).wtAt(2590, 9448, 0), CustomWtEnclaveOut, 0)
	nopick := wtShielded(wtSnap(WtMadePotion, 0, 0)).wtWith(WtObjCrystal3, 1).wtWith(WtObjNightshade, 1)
	wtProvisionOf(t, nopick, KitWtRock)
	norobe := wtShielded(wtSnap(WtMadePotion, 0, 0)).wtWith(WtObjCrystal4, 1).wtWith(WtObjNightshade, 1)
	wtCustom(t, norobe, CustomWtRobe, 0)
	two := wtShielded(wtSnap(WtMadePotion, WtMinedRock, 0)).wtWith(WtObjCrystal3, 1).wtWith(WtObjCrystal4, 1).wtAt(2590, 9448, 0)
	wtCustom(t, two, CustomWtEnclaveOut, 0)
	// Outside with two: Grew's crystal is asked of Grew (the island), the mad skavid's of the mad skavid (the east gate).
	out := wtShielded(wtSnap(WtMadePotion, WtMinedRock, 0)).wtWith(WtObjCrystal3, 1).wtWith(WtObjCrystal4, 1)
	wtProvisionOf(t, out, KitWtIsland)
	wtCustom(t, out.wtWith(WtObjRope, 1), CustomWtIslandIn, 0)
	st := wtTalkOf(t, out.wtWith(WtObjRope, 1).wtAt(2513, 3084, 0), "Grew")
	if !slices.Contains(st.Stop.Prefer, "I've lost the crystal you gave me.") {
		t.Errorf("the lost-crystal talk prefers %v", st.Stop.Prefer)
	}
	second := out.wtWith(WtObjCrystal1, 1)
	wtProvisionOf(t, second, KitWtCaves)
	wtCustom(t, second.wtWith(WtObjMap, 1).wtWith(WtObjLitCandle, 1), CustomWtMad, 1)
	// A crystal in the bank is drawn.
	banked := out.wtBank(WtObjCrystal1, 1).wtBank(WtObjCrystal2, 1)
	wtProvisionOf(t, banked, KitWtDraw)
	// All four: the wizard.
	all := out.wtWith(WtObjCrystal1, 1).wtWith(WtObjCrystal2, 1)
	wtCustom(t, all, CustomWtCrystals, 0)
	// Stage 11: the lever.
	wtCustom(t, wtSnap(WtFoundAll, 0, -1), CustomWtLever, 0)
}

func TestWatchtowerFinish(t *testing.T) {
	done := wtSnap(WtComplete, 0, -1)
	done.Journal = StatusComplete
	if st := DecideWatchtower(done); st.Kind != StepDone {
		t.Errorf("done: %s", st.Describe())
	}
	scroll := done.wtWith(WtObjSpellScroll, 1)
	if st := DecideWatchtower(scroll); st.Kind != StepOpHeld || st.ItemID != WtObjSpellScroll || st.Op != "Read" || ValidateStep(st) != "" {
		t.Errorf("the scroll: %s", st.Describe())
	}
	wtCustom(t, done.wtAt(2928, 4715, 2), CustomWtDown, 0)
	wtCustom(t, done.wtAt(2549, 3112, 2), CustomWtDown, 0)
	// With the scroll read and the tower left, done.
	if st := DecideWatchtower(done.wtAt(2544, 3112, 0)); st.Kind != StepDone {
		t.Errorf("on the ground: %s", st.Describe())
	}
}

// Whatever the journal, the pack and the position say, every step the decision asks for is well formed and a park carries its reason; a custom that failed
// on its last run parks the quest for half an hour instead of being asked again.
func TestWatchtowerEveryStepIsWellFormed(t *testing.T) {
	positions := [][3]int{{2613, 3094, 0}, {2549, 3112, 2}, {2544, 3112, 1}, {2513, 3084, 0}, {2576, 3029, 0}, {2508, 3011, 0}, {2530, 3029, 0}, {2549, 3027, 0},
		{2498, 9418, 0}, {2588, 9410, 0}, {2928, 4715, 2}}
	packs := []map[int]int{
		{}, {WtObjShark: 10}, {WtObjShark: 10, WtObjDragonBones: 1, WtObjRope: 1, WtObjTobanKey: 1}, {WtObjShark: 10, WtObjMap: 1, WtObjLitCandle: 1, WtObjNightshade: 1},
		{WtObjShark: 10, WtObjMagicPotion: 1, WtObjPickaxe: 1, WtObjCrystal3: 1}, {WtObjShark: 10, WtObjCoins: 500, WtObjDeathRune: 3, WtObjRockCake: 1},
		{WtObjShark: 10, WtObjPart1: 1, WtObjPart2: 1, WtObjPart3: 1, WtObjRelic: 1},
	}
	seen := map[string]bool{}
	for stage := 0; stage <= 14; stage++ {
		for _, flags := range []uint32{0, WtOgSpoken | WtGrewSpoken, WtOgHelped | WtGrewHelped | WtTobanHelped, WtMarketPaid, WtLearning | WtLearnedNod, WtMinedRock} {
			for _, left := range []int{-1, 0, 3, 6} {
				for _, pos := range positions {
					for _, inv := range packs {
						s := wtSnap(stage, flags, left).wtAt(pos[0], pos[1], pos[2])
						s.InvIDs = inv
						for _, j := range []Status{StatusNotStarted, StatusInProgress, StatusComplete, StatusUnknown} {
							s.Journal = j
							st := DecideWatchtower(s)
							seen[st.Describe()] = true
							if why := ValidateStep(st); why != "" {
								t.Fatalf("stage %d flags %#x left %d at %v journal %s: %s: %s", stage, flags, left, pos, j, st.Describe(), why)
							}
							if st.Kind == StepCustom {
								// A failed custom parks for the leg's half hour (or the step is not a leg).
								f := DecideWatchtower(s.wtFailed(st.Custom))
								if f.Kind == StepCustom && f.Custom == st.Custom {
									t.Fatalf("stage %d: %s failed and was asked again", stage, st.Custom)
								}
							}
							if st.Kind == StepWait && st.ParkFor > 0 && st.Reason == "" {
								t.Fatalf("a park without a reason at stage %d", stage)
							}
						}
					}
				}
			}
		}
	}
	if len(seen) < 25 {
		t.Errorf("only %d distinct steps over the whole sweep: the decision is not being exercised", len(seen))
	}
	for d := range seen {
		if strings.Contains(d, "%!") {
			t.Errorf("a badly formatted description %q", d)
		}
	}
}

// The module is the engine's: its tools keep the quest's items out of the deposits, its levels are the steps', it never fights back and one death parks it.
func TestWatchtowerModule(t *testing.T) {
	m := Watchtower
	if m.Record.ID != WatchtowerID || m.Record.Name != WatchtowerName || m.Record.QuestPoints != 4 || len(m.Record.Requirements.Skills) != 0 {
		t.Errorf("record %+v", m.Record)
	}
	if !m.NoRetaliate || m.MaxDeaths != 1 || m.Decide == nil || m.ReadStage == nil || !m.BankSet || m.Bank != WtBank {
		t.Errorf("module %+v", m)
	}
	for _, id := range []int{WtObjRelic, WtObjPart1, WtObjPart2, WtObjPart3, WtObjTobanKey, WtObjStolenGold, WtObjDragonBones, WtObjMap, WtObjNightshade, WtObjCrystal1,
		WtObjCrystal2, WtObjCrystal3, WtObjCrystal4, WtObjMagicPotion, WtObjOgrePotion, WtObjRope, WtObjTooth, WtObjRockCake, WtObjLitCandle, WtObjGuamLeaf} {
		if !slices.Contains(m.ToolIDs, id) {
			t.Errorf("tool %d is not kept in the pack", id)
		}
		if !slices.Contains(WtKeepIDs(), id) {
			t.Errorf("tool %d is deposited by the quest's own bank visits", id)
		}
	}
	for _, id := range WtFoodIDs {
		if !slices.Contains(WtKeepIDs(), id) {
			t.Errorf("food %d is deposited", id)
		}
	}
	// Copies: a caller cannot change the module's lists.
	a := WtKeepIDs()
	a[0] = -1
	if WtKeepIDs()[0] == -1 || WtToolIDs()[0] == -1 {
		t.Error("the keep list is shared")
	}
	// The foods are eaten at the line the chieftain's hit needs.
	if m.Sustain.EatAt < FoeOgreChieftain.PeakHit()+1 || !m.Sustain.IsFood(WtObjShark) {
		t.Errorf("sustain %+v", m.Sustain)
	}
}
