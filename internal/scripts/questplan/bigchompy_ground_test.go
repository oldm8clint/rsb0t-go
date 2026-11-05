package questplan

// Big Chompy Bird Hunting's hostile ground: the ogres of the Feldip hills priced by the shared model, the enclave in the east that no hostile reaches, the price of a wolf and an
// ogre on the pack in hand NOW, and the steps that price parks (the pool's legs, the west patches, the way from the bank). The routes themselves are checked on the real pack in
// questmods (TestChompyEnclaveLegsKeepClearOfEveryHostile and its neighbours).

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestChompyOgreIsPricedByTheSharedModel(t *testing.T) {
	if got := FoeOgre.MaxHit(); got != 7 {
		t.Errorf("the ogre hits up to %d, the content's Strength 43 and strengthbonus 20 say 7", got)
	}
	if FoeOgre.AttackTicks != 6 || FoeOgre.Hitpoints != 60 {
		t.Errorf("the ogre %+v: attackrate 6 and 60 hitpoints", FoeOgre)
	}
	if foe, ok := FoeByNPC(NpcOgre); !ok || foe != FoeOgre {
		t.Errorf("FoeByNPC(ogre) = %+v %v", foe, ok)
	}
	if got := ChGroundPeakHit(); got != 7 {
		t.Errorf("the top-up is made for a hit of %d, the ogre's 7 is the hardest of the hills", got)
	}
	// The ogre is judged on its own: a pack that beats it passes, and food is the first thing asked for.
	if v := MeleeChain(chSnap(ChStarted), []MeleeFoe{FoeOgre}, chSustain); !v.OK {
		t.Errorf("a 60/60/50 account with ten sharks loses to an ogre: %s", v.Why)
	}
}

// Every spawn's reach stops short of the enclave, whose places and walks are therefore free of hostiles; everything else is hostile ground or the way to it.
func TestChompyEnclaveKeepsClearOfEveryHostile(t *testing.T) {
	for _, w := range ChWolfSpawns {
		if w.X+ChWolfReach >= ChEnclaveWest {
			t.Errorf("the wolf at %v reaches column %d, the enclave starts at %d", w, w.X+ChWolfReach, ChEnclaveWest)
		}
	}
	for _, o := range ChOgreSpawns {
		if o.X+ChOgreReach >= ChEnclaveWest {
			t.Errorf("the ogre at %v reaches column %d, the enclave starts at %d", o, o.X+ChOgreReach, ChEnclaveWest)
		}
	}
	if len(ChWolfSpawns) != 6 || len(ChOgreSpawns) != 11 {
		t.Errorf("%d wolves and %d ogres on the maps' spawn lists, want 6 and 11", len(ChWolfSpawns), len(ChOgreSpawns))
	}
	cave := tile.Tile{X: 2630, Z: 2997}
	inside := map[string]tile.Tile{
		"Rantz": ChRantzAnchor, "the achey grove": ChGrove, "the bait clearing": ChBaitSpot, "the spit": ChSpitNear, "the spit's loc": ChSpitAt, "the potato patch": ChPotatoPatch,
		"the cabbage patch": ChCabbagePatch, "the equa leaves": ChEquaSpawn, "the cave mouth": cave, "the chest": ChChestNear, "Bugs": ChBugsAnchor, "Fycie": ChFycieAnchor,
	}
	for name, at := range inside {
		if !ChInEnclave(at) {
			t.Errorf("%s at %v is outside the enclave", name, at)
		}
		if ChHostileNear(at) {
			t.Errorf("%s at %v is within a hostile's reach", name, at)
		}
	}
	outside := map[string]tile.Tile{
		"the pool's lip": ChBubbleStand, "the wolves' field": ChWolfField, "the onion patch": ChOnionPatch, "the tomato spawn": ChTomatoSpawn, "the doogle leaves": ChDoogleSpawn,
		"Yanille's bank": YanilleBank,
	}
	for name, at := range outside {
		if ChInEnclave(at) {
			t.Errorf("%s at %v is inside the enclave", name, at)
		}
	}
	// The places the pack test pins as hostile ground really are within reach: the lip, the field and the three west patches.
	for name, at := range map[string]tile.Tile{"the pool's lip": ChBubbleStand, "the wolves' field": ChWolfField, "the onion patch": ChOnionPatch, "the tomato spawn": ChTomatoSpawn} {
		if !ChHostileNear(at) {
			t.Errorf("%s at %v is not within a hostile's reach", name, at)
		}
	}
	if ChHostileNear(tile.Tile{X: 2598, Z: 2962, Level: 1}) {
		t.Error("an upstairs tile is within a hostile's reach")
	}
	// The legs: inside the enclave is free, any other end makes it hostile.
	for _, c := range []struct {
		name     string
		from, to tile.Tile
		want     bool
	}{
		{"Rantz to the grove", ChRantzAnchor, ChGrove, false}, {"Rantz to the cave", ChRantzAnchor, cave, false}, {"the cave to Bugs", cave, ChBugsAnchor, false},
		{"Rantz to the pool's lip", ChRantzAnchor, ChBubbleStand, true}, {"the lip to Rantz", ChBubbleStand, ChRantzAnchor, true}, {"the bank to Rantz", YanilleBank, ChRantzAnchor, true},
		{"the doogle leaves to the spit", ChDoogleSpawn, ChSpitNear, true},
	} {
		if got := ChLegExposed(c.from, c.to); got != c.want {
			t.Errorf("%s: exposed %v, want %v", c.name, got, c.want)
		}
	}
	// Exactly the west seasonings are outside the enclave.
	var west []string
	for _, c := range ChSeasoningList() {
		if !ChInEnclave(c.At) {
			west = append(west, c.Name)
		}
	}
	if strings.Join(west, ",") != "Onion,Tomato,Doogle leaves" {
		t.Errorf("the seasonings outside the enclave: %v", west)
	}
}

// The price is made on the pack in hand: the same account passes with its food and fails without it, and the reason names what it was priced against.
func TestChompyGroundIsPricedOnThePackInHand(t *testing.T) {
	if v := ChGroundReady(chSnap(ChStarted)); !v.OK {
		t.Errorf("a 60/60/50 account with ten sharks: %s", v.Why)
	}
	v := ChGroundReady(chSnap(ChStarted, func(s *Snapshot) { delete(s.InvIDs, ObjShark) }))
	if v.OK || !strings.Contains(v.Why, "no food") || !strings.Contains(v.Why, "ogre") || !strings.Contains(v.Why, "wolf") {
		t.Errorf("no food: %+v", v)
	}
	// Eaten down to one shark by the wolves, the same account is not sent on.
	v = ChGroundReady(chSnap(ChStarted, chHold(ObjShark, 1)))
	if v.OK || !strings.Contains(v.Why, "Feldip hills") {
		t.Errorf("one shark: %+v", v)
	}
	// Hitpoints are the pack's too: the account tops up from its bites before it is judged, so low hitpoints with food are no reason, and no food with low hitpoints is.
	if v := ChGroundReady(chSnap(ChStarted, func(s *Snapshot) { s.HP = 20 })); !v.OK {
		t.Errorf("20 of 60 hitpoints with ten sharks: %s", v.Why)
	}
	if v := ChGroundReady(chSnap(ChStarted, chLevel("attack", 20), chLevel("strength", 20), chLevel("defence", 15))); v.OK {
		t.Error("a 20/20/15 account is sent among the wolves and the ogres")
	}
	// An unread stat is no reason to refuse (as the wolf's price).
	if v := ChGroundReady(chSnap(ChStarted, func(s *Snapshot) { s.MaxHP = 0 })); !v.OK {
		t.Errorf("an account whose hitpoints are unread: %+v", v)
	}
	// It agrees with the wolf's price wherever the wolf is the harder foe (the ogre hits slower and has fewer hitpoints).
	for sharks := 0; sharks <= 8; sharks++ {
		s := chSnap(ChStarted, chLevel("attack", 50), chLevel("strength", 50), chLevel("defence", 40), chHold(ObjShark, sharks))
		if w, g := ChWolfReady(s), ChGroundReady(s); g.OK && !w.OK {
			t.Errorf("%d sharks: the ground is priced fit and the wolf is not", sharks)
		}
	}
}

// Every custom the module can ask for has a destination the gate reads, so none is forgotten when one is added.
func TestChompyEveryCustomHasADestination(t *testing.T) {
	for _, name := range []string{CustomChBugs, CustomChChop, CustomChWolf, CustomChGive, CustomChToadies, CustomChShowToad, CustomChBow, CustomChChest, CustomChToad, CustomChBait,
		CustomChShoot, CustomChShowChompy, CustomChKids, CustomChCook, CustomChHandOver} {
		if _, ok := chStepDest(chSnap(ChStarted), Step{Kind: StepCustom, Custom: name}); !ok {
			t.Errorf("custom %q has no destination", name)
		}
	}
	for _, c := range ChSeasoningList() {
		dest, ok := chStepDest(chSnap(ChStarted), Step{Kind: StepCustom, Custom: CustomChSeason, ItemID: c.ID})
		if !ok || dest != c.At {
			t.Errorf("the %s seasoning: destination %v %v, want %v", c.Name, dest, ok, c.At)
		}
	}
	for _, st := range []Step{{Kind: StepProvision}, {Kind: StepScanBank}, {Kind: StepWithdraw}, {Kind: StepWait}, {Kind: StepUseOn}, {Kind: StepDone}} {
		if dest, ok := chStepDest(chSnap(ChStarted), st); ok {
			t.Errorf("a %s step has the destination %v", st.Kind, dest)
		}
	}
	// A bait or a shot that holds no toad is bound for the pool, where one is caught; with a toad in the pack, for the clearing.
	for _, custom := range []string{CustomChBait, CustomChShoot} {
		if dest, _ := chStepDest(chSnap(ChStarted), Step{Kind: StepCustom, Custom: custom}); dest != ChBubbleStand {
			t.Errorf("%s with no toad is bound for %v", custom, dest)
		}
		if dest, _ := chStepDest(chSnap(ChStarted, chHold(ObjChToad, 1)), Step{Kind: StepCustom, Custom: custom}); dest != ChBaitSpot {
			t.Errorf("%s with a toad is bound for %v", custom, dest)
		}
	}
}

// The pool's legs, the west patches and the wolves' field are priced against what stands there with the pack in hand: an unfit pack parks the quest for six hours with the reason,
// a fit one goes on, and the walks inside the enclave are not priced at all.
func TestChompyDecideParksTheHostileLegsOnAnUnfitPack(t *testing.T) {
	bellows := chHold(ObjChBellows3, 1)
	armed := []func(*Snapshot){chHold(ObjChBow, 1), chHold(ObjChArrow, 6), bellows}
	seasonings := func(missing int) func(*Snapshot) {
		return func(s *Snapshot) {
			for _, c := range ChSeasoningList() {
				if c.ID != missing {
					s.InvIDs[c.ID] = 1
				}
			}
		}
	}
	cook := func(missing int) []func(*Snapshot) {
		return []func(*Snapshot){chHold(ObjChRawChompy, 1), chRan(CustomChKids), seasonings(missing)}
	}
	unfit := chHold(ObjShark, 1)
	hostile := []struct {
		name   string
		stage  int
		mods   []func(*Snapshot)
		custom string
	}{
		{"15: the toads at the pool", ChToadies, []func(*Snapshot){bellows}, CustomChToad},
		{"25: bait, catching its own toad at the pool", ChShownToad, []func(*Snapshot){bellows}, CustomChBait},
		{"45: a shot, catching its own toad at the pool", ChGotBow, armed, CustomChShoot},
		{"55: the onion", ChToldToCook, cook(ObjChOnion), CustomChSeason},
		{"55: the tomato", ChToldToCook, cook(ObjChTomato), CustomChSeason},
		{"55: the doogle leaves", ChToldToCook, cook(ObjChDoogle), CustomChSeason},
	}
	for _, x := range hostile {
		fit := DecideChompy(chSnap(x.stage, x.mods...))
		wantStep(t, x.name+", a fit pack", fit, StepCustom, x.custom)
		got := DecideChompy(chSnap(x.stage, append(append([]func(*Snapshot){}, x.mods...), unfit)...))
		if got.Kind != StepWait || got.ParkFor != ChFightParkFor || !strings.Contains(got.Reason, "hostile ground") || !strings.Contains(got.Reason, "Feldip") {
			t.Errorf("%s, an unfit pack: %+v", x.name, got)
		}
		if why := ValidateStep(got); why != "" {
			t.Errorf("%s: malformed park %+v: %s", x.name, got, why)
		}
	}
	// With the toad already in the pack the bait and the shot stay in the clearing, inside the enclave: not priced.
	for _, x := range []struct {
		name   string
		stage  int
		mods   []func(*Snapshot)
		custom string
	}{
		{"25: bait with a toad", ChShownToad, []func(*Snapshot){chHold(ObjChToad, 1)}, CustomChBait},
		{"45: a shot with a toad", ChGotBow, append(append([]func(*Snapshot){}, armed...), chHold(ObjChToad, 1)), CustomChShoot},
		{"10: the toadies menu with Rantz", ChGivenArrows, nil, CustomChToadies},
		{"55: the first seasoning, a patch beside Rantz", ChToldToCook, []func(*Snapshot){chHold(ObjChRawChompy, 1), chRan(CustomChKids)}, CustomChSeason},
		{"55: the spit", ChToldToCook, []func(*Snapshot){chHold(ObjChRawChompy, 1), chRan(CustomChKids), seasonings(0)}, CustomChCook},
		{"60: the seasoned chompy to Rantz", ChCooked, []func(*Snapshot){chHold(ObjChSeasonedChomp, 1)}, CustomChHandOver},
	} {
		got := DecideChompy(chSnap(x.stage, append(append([]func(*Snapshot){}, x.mods...), unfit)...))
		wantStep(t, x.name+", an unfit pack inside the enclave", got, StepCustom, x.custom)
	}
	// The same talk from the bank's end of the road is a hostile leg (two ogres stand beside it): priced; a bank step is not (an account with no food must be able to fetch some).
	atBank := func(s *Snapshot) { s.Tile = YanilleBank }
	wantStep(t, "10: Rantz from the bank, a fit pack", DecideChompy(chSnap(ChGivenArrows, atBank)), StepCustom, CustomChToadies)
	if got := DecideChompy(chSnap(ChGivenArrows, atBank, unfit)); got.Kind != StepWait || got.ParkFor != ChFightParkFor || !strings.Contains(got.Reason, "hostile ground") {
		t.Errorf("Rantz from the bank with an unfit pack: %+v", got)
	}
	noFood := func(s *Snapshot) { delete(s.InvIDs, ObjShark); delete(s.Ran, "provision:"+KitChompy) }
	if got := DecideChompy(chSnap(ChGivenArrows, atBank, noFood)); got.Kind != StepProvision {
		t.Errorf("a foodless account at the bank is not sent for food: %+v", got)
	}
	// The wolves' field is priced too (by the wolf's own gate first, then by the ground's).
	flighted := []func(*Snapshot){chHold(ObjChFlighted, 6), chHold(ObjChShaft, 6), chKitOK}
	wantStep(t, "5: a wolf, a fit pack", DecideChompy(chSnap(ChStarted, flighted...)), StepCustom, CustomChWolf)
	if got := DecideChompy(chSnap(ChStarted, append(append([]func(*Snapshot){}, flighted...), unfit)...)); got.Kind != StepWait || got.ParkFor != ChFightParkFor {
		t.Errorf("a wolf on an unfit pack: %+v", got)
	}
}
