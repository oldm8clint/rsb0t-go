package questplan

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// ---------------------------------------------------------------------------------------------------------------------
// Fixtures: the fleet's usual caster (Attack 53, Strength 41, Defence 50, Hitpoints 55, Magic 66, Crafting and Woodcutting 40) at Draynor.
// ---------------------------------------------------------------------------------------------------------------------

func lcLevels(att, str, def, hp, mag int) map[string]int {
	return map[string]int{"attack": att, "strength": str, "defence": def, "hitpoints": hp, "magic": mag, "crafting": 40, "woodcutting": 40}
}

// lcSnap is a snapshot of the median caster at Draynor's bank in the middle of the quest at stage, with mod applied.
func lcSnap(stage int, mod func(*Snapshot)) Snapshot {
	s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, Tile: DraynorBank, TileKnown: true, HP: 55, MaxHP: 55,
		FreeSlots: 20, Levels: lcLevels(53, 41, 50, 55, 66), InvIDs: map[int]int{}, WornIDs: map[int]int{}, Inv: map[string]int{}}
	if stage == LcNotStarted {
		s.Journal, s.StageKnown, s.Stage = StatusNotStarted, false, 0
	}
	if mod != nil {
		mod(&s)
	}
	return s
}

func lcGive(s *Snapshot, items map[int]int) {
	for id, n := range items {
		s.InvIDs[id] += n
	}
}

// lcFullKit is the pack the trip's kit makes for the median caster: a knife, ten sharks, the runes of 21 Fire Blasts (death 21, fire 105, air 84)
// and the Varrock Teleport out (a fire, three air and a law rune).
func lcFullKit() map[int]int {
	return map[int]int{ObjKnife: 1, ObjShark: LcSharks, ObjDeathRune: 21, ObjFireRune: 106, ObjAirRune: 87, ObjLcLawRune: 1}
}

func lcInDungeon(s *Snapshot) { s.Tile = LcZombieStand }

func wantCustom(t *testing.T, name string, st Step, custom string) {
	t.Helper()
	if st.Kind != StepCustom || st.Custom != custom {
		t.Errorf("%s: step %s (%s), want the custom %q", name, st.Describe(), st.Kind, custom)
	}
}

func wantWait(t *testing.T, name string, st Step, park time.Duration, reason string) {
	t.Helper()
	if st.Kind != StepWait || st.ParkFor != park || !strings.Contains(st.Reason, reason) {
		t.Errorf("%s: step %s (%s, park %s), want a wait of %s mentioning %q", name, st.Describe(), st.Kind, st.ParkFor, park, reason)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The journal.
// ---------------------------------------------------------------------------------------------------------------------

// The seven pages as zanaris_journal.rs2 renders them (colour codes and the client's own | line breaks), the newest paragraph deciding.
func TestLostCityJournalPagesReadAsTheirStage(t *testing.T) {
	str := "@str@According to one of the adventurers in Lumbridge Swamp|@str@the entrance to Zanaris is somewhere around there.|"
	leprechaun := "@str@I found a Leprechaun hiding in a nearby tree.|"
	shed := "@str@He told me that the entrance to Zanaris is in the shed in|@str@Lumbridge swamp but only if I am carrying a Dramen Staff.|"
	spirit := "@str@The Dramen Tree was guarded by a powerful Tree Spirit.|"
	for _, c := range []struct {
		stage int
		page  string
	}{
		{LcNotStarted, "@dbl@I can start this quest by speaking to the Adventurers in the Swamp just south of Lumbridge.|@dbl@To complete this quest I need:|@str@Level 31 Crafting|@dre@Level 36 Woodcutting|@dbl@and be able to defeat a @dre@Level 101 Spirit without weapons"},
		{LcStarted, str + "@dbl@Apparently there is a @dre@leprechaun@dbl@ hiding in a @dre@tree@dbl@ nearby who can tell me how to enter the @dre@Lost City of Zanaris"},
		{LcSpokenShamus, str + leprechaun + "@dbl@He told me that the entrance to @dre@Zanaris@dbl@ is in the @dre@shed@dbl@ in @dre@Lumbridge swamp@dbl@ but only if I am carrying a @dre@Dramen Staff|@dbl@I can find a @dre@Dramen Tree@dbl@ in a cave on @dre@Entrana|@dbl@somewhere"},
		{LcSpiritDefeated, str + leprechaun + shed + spirit + "@dbl@With the @dre@Spirit@dbl@ defeated I can cut a @dre@branch@dbl@ from the tree"},
		{LcTreeChopped, str + leprechaun + shed + spirit + "@str@I cut a branch from the tree.|@dbl@I should @dre@craft@dbl@ the @dre@branch@dbl@ from the tree into a @dre@staff"},
		{LcStaffMade, str + leprechaun + shed + spirit + "@str@I cut a branch from the tree and crafted a Dramen Staff.|@dbl@I should enter @dre@Zanaris@dbl@ by going to the @dre@shed@dbl@ in @dre@Lumbridge Swamp@dbl@ while keeping the @dre@Dramen staff@dbl@ with me"},
		{LcComplete, str + leprechaun + shed + spirit + "@str@I cut a branch from the tree and crafted a Dramen Staff.|@str@With the mystical Dramen Staff in my possession I was|@str@able to enter Zanaris through the shed in Lumbridge|@str@swamp||@red@QUEST COMPLETE!"},
	} {
		got, ok := ParseLostCityJournal(strings.Split(c.page, "|"))
		if !ok || got != c.stage {
			t.Errorf("stage %d page read as %d (ok %v)", c.stage, got, ok)
		}
	}
	for _, empty := range [][]string{nil, {""}, {"@dbl@something else entirely"}} {
		if got, ok := ParseLostCityJournal(empty); ok {
			t.Errorf("%q read as stage %d: an unrecognised page is unknown, never stage 0", empty, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The decision.
// ---------------------------------------------------------------------------------------------------------------------

func TestLostCityDecideTheTabAndTheGates(t *testing.T) {
	if st := DecideLostCity(lcSnap(5, func(s *Snapshot) { s.Journal = StatusComplete })); st.Kind != StepDone {
		t.Errorf("a complete tab: %s", st.Describe())
	}
	if st := DecideLostCity(lcSnap(1, func(s *Snapshot) { s.Journal = StatusUnknown })); st.Kind != StepWait || st.ParkFor != 0 {
		t.Errorf("an uncoloured tab: %s", st.Describe())
	}
	if st := DecideLostCity(lcSnap(1, func(s *Snapshot) { s.TileKnown = false })); st.Kind != StepWait || st.ParkFor != 0 {
		t.Errorf("no position: %s", st.Describe())
	}
	if st := DecideLostCity(lcSnap(1, func(s *Snapshot) { s.StageKnown = false })); st.Kind != StepWait || st.ParkFor != 0 {
		t.Errorf("an unread journal: %s", st.Describe())
	}
	if st := DecideLostCity(lcSnap(6, nil)); st.Kind != StepWait || !strings.Contains(st.Reason, "disagree") {
		t.Errorf("stage 6 on an unfinished tab: %s", st.Describe())
	}
	// Crafting 31 and Woodcutting 36: the record holds the ladder, the decide is the net.
	short := func(s *Snapshot) { s.Levels["crafting"], s.Levels["woodcutting"] = 30, 35 }
	st := DecideLostCity(lcSnap(2, short))
	wantWait(t, "levels short", st, LcFightParkFor, "Crafting 30/31, Woodcutting 35/36")
	wantCustom(t, "levels short in the dungeon", DecideLostCity(lcSnap(2, func(s *Snapshot) { short(s); lcInDungeon(s) })), CustomLcPortal)
	// A level the snapshot has not read is not judged.
	if st := DecideLostCity(lcSnap(0, func(s *Snapshot) {
		delete(s.Levels, "crafting")
		delete(s.Levels, "woodcutting")
		delete(s.Levels, "magic")
	})); st.Kind != StepTalk {
		t.Errorf("unread levels: %s", st.Describe())
	}
	// Magic 25 is the module's own level: the Varrock Teleport is the way out of the dungeon and the Wilderness is never walked. An account under
	// it is parked at every stage before the dungeon, and taken out of the dungeon (by the last resort) when it is somehow in it.
	for _, stage := range []int{LcNotStarted, LcStarted, LcSpokenShamus, LcSpiritDefeated, LcStaffMade} {
		st := DecideLostCity(lcSnap(stage, func(s *Snapshot) { s.Levels["magic"] = LcMinMagic - 1 }))
		wantWait(t, "magic short", st, LcFightParkFor, "Magic 24/25")
		if !strings.Contains(st.Reason, "Wilderness is never walked") {
			t.Errorf("stage %d: the reason %q does not say why Magic is asked", stage, st.Reason)
		}
	}
	wantCustom(t, "magic short in the dungeon", DecideLostCity(lcSnap(2, func(s *Snapshot) { s.Levels["magic"] = 10; lcInDungeon(s) })), CustomLcPortal)
	if st := DecideLostCity(lcSnap(0, func(s *Snapshot) { s.Levels["magic"] = LcMinMagic })); st.Kind == StepWait && strings.Contains(st.Reason, "Magic") {
		t.Errorf("Magic %d is the level asked: %s", LcMinMagic, st.Describe())
	}
}

func TestLostCityDecideStageZeroAndOne(t *testing.T) {
	st := DecideLostCity(lcSnap(0, nil))
	if st.Kind != StepTalk || st.Stop.ID != NpcLcWarrior || len(st.Stop.Prefer) != 4 || st.Stop.Leash <= 0 || ValidateStep(st) != "" {
		t.Errorf("stage 0: %+v", st)
	}
	// The melee fleet's strongest account (75s, no Magic) has no way out of the dungeon (the Varrock Teleport is Magic 25) and no spell for the spirit:
	// never begun, and the reason is the level.
	st = DecideLostCity(lcSnap(0, func(s *Snapshot) { s.Levels, s.HP, s.MaxHP = lcLevels(75, 75, 70, 74, 1), 74, 74 }))
	wantWait(t, "melee only", st, LcFightParkFor, "Magic 1/25")
	// A caster with the Magic of the exit and the hitpoints of the corridor but no swing to finish with and a Water Bolt for the spirit: the model says
	// the ten sharks are eaten before it falls, and it is never begun.
	st = DecideLostCity(lcSnap(0, func(s *Snapshot) { s.Levels, s.HP, s.MaxHP = lcLevels(1, 1, 1, 60, 25), 60, 60 }))
	wantWait(t, "a weak caster", st, LcFightParkFor, "cannot beat the Tree spirit")
	// A caster with the hitpoints of the early fleet cannot take the corridor's two greater demons and keep three of their hits.
	st = DecideLostCity(lcSnap(0, func(s *Snapshot) { s.Levels, s.HP, s.MaxHP = lcLevels(40, 40, 30, 40, 66), 40, 40 }))
	wantWait(t, "a hitpoint-poor caster", st, LcFightParkFor, "greater demons")
	// The same price at stage 1, before the axe is looked for.
	st = DecideLostCity(lcSnap(1, func(s *Snapshot) { s.Levels, s.HP, s.MaxHP = lcLevels(1, 1, 1, 60, 25), 60, 60 }))
	wantWait(t, "a weak caster at stage 1", st, LcFightParkFor, "cannot beat the Tree spirit")

	// Stage 1: the bank is read once for an axe it may hold, then an axe (Bob's bronze axe when the bank has none), then Shamus.
	st = DecideLostCity(lcSnap(1, nil))
	if st.Kind != StepScanBank || st.Bank != DraynorBank || !st.BankSet {
		t.Fatalf("stage 1 with the bank unread: %s", st.Describe())
	}
	st = DecideLostCity(lcSnap(1, func(s *Snapshot) { s.BankKnown, s.BankIDs = true, map[int]int{} }))
	if st.Kind != StepProvision || st.KitName != KitLcAxe || len(st.Kit) != 1 || st.Kit[0].ID != ObjBronzeAxe || !st.Kit[0].Worn || ValidateStep(st) != "" {
		t.Fatalf("stage 1 without an axe: %+v", st)
	}
	if len(st.Kit[0].Sources) != 1 || st.Kit[0].Sources[0].Kind != SourceShop || st.Kit[0].Sources[0].Shop.NpcID != BobsAxes.NpcID {
		t.Errorf("the axe's sources %+v: Bob's store is the shop", st.Kit[0].Sources)
	}
	if !slices.Contains(st.KeepIDs, ObjKnife) || st.Bank != DraynorBank || !st.BankSet {
		t.Errorf("the axe round's bank %v / keep %v", st.Bank, st.KeepIDs)
	}
	// An axe the bank holds is drawn in preference to a purchase, the best one the level can use (Woodcutting 40: a mithril axe wants 21).
	st = DecideLostCity(lcSnap(1, func(s *Snapshot) {
		s.BankKnown, s.BankIDs = true, map[int]int{ObjSteelAxe: 1, ObjMithrilAxe: 1, ObjRuneAxe: 1}
	}))
	if st.Kind != StepProvision || st.Kit[0].ID != ObjMithrilAxe || len(st.Kit[0].Sources) != 0 {
		t.Errorf("a bank with a steel, a mithril and a rune axe at Woodcutting 40: %+v", st.Kit)
	}
	if st = DecideLostCity(lcSnap(1, func(s *Snapshot) { s.BankKnown, s.BankIDs, s.FreeSlots = true, map[int]int{}, 1 })); st.Kind != StepDeposit || st.Bank != DraynorBank {
		t.Errorf("a full pack: %s", st.Describe())
	}
	st = DecideLostCity(lcSnap(1, func(s *Snapshot) {
		s.BankKnown, s.BankIDs = true, map[int]int{}
		s.Blocked = map[string]bool{"provision:" + KitLcAxe: true}
	}))
	wantWait(t, "no axe anywhere", st, LcItemParkFor, "no axe")
	st = DecideLostCity(lcSnap(1, func(s *Snapshot) { lcGive(s, map[int]int{ObjBronzeAxe: 1}) }))
	wantCustom(t, "an axe held", st, CustomLcShamus)
	st = DecideLostCity(lcSnap(1, func(s *Snapshot) { s.WornIDs[ObjRuneAxe] = 1 }))
	wantCustom(t, "an axe worn", st, CustomLcShamus)
	st = DecideLostCity(lcSnap(1, func(s *Snapshot) {
		lcGive(s, map[int]int{ObjIronAxe: 1})
		s.Failed = map[string]bool{CustomLcShamus: true}
	}))
	wantWait(t, "Shamus failed", st, LcLegPark, "Shamus")
}

func TestLostCityDecideTheTripAndTheKit(t *testing.T) {
	// Stage 2 on the mainland: the kit first (a knife, ten sharks, the runes of 21 Fire Blasts and of the Varrock Teleport out), required lines all.
	st := DecideLostCity(lcSnap(2, nil))
	if st.Kind != StepProvision || st.KitName != KitLcTrip || ValidateStep(st) != "" {
		t.Fatalf("stage 2 with an empty pack: %+v", st)
	}
	want := map[int]int{ObjKnife: 1, ObjShark: LcSharks, ObjDeathRune: 21, ObjFireRune: 106, ObjAirRune: 87, ObjLcLawRune: 1}
	got := map[int]int{}
	for _, l := range st.Kit {
		got[l.ID] = l.Qty
		if l.Optional || l.Untradeable {
			t.Errorf("kit line %+v: the trip's lines are required and tradeable", l)
		}
		if l.ID == ObjKnife && (len(l.Sources) != 1 || l.Sources[0].Kind != SourceGround || l.Sources[0].Tile != LcKnifeSpawn) {
			t.Errorf("the knife's sources %+v", l.Sources)
		}
		if (l.ID == ObjFireRune || l.ID == ObjAirRune || l.ID == ObjDeathRune || l.ID == ObjLcLawRune) && !l.Stackable {
			t.Errorf("rune line %+v is not stackable", l)
		}
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("the kit asks %d of %d, want %d (kit %v)", got[id], id, n, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the kit is %v, want exactly %v", got, want)
	}
	// What a kit round banks first is everything the quest does not carry: the keep list has the tools and the food and no armour.
	for _, id := range []int{ObjKnife, ObjLcBranch, ObjLcStaff, CoinsID, ObjShark, ObjFireRune, ObjDeathRune, ObjLcLawRune} {
		if !slices.Contains(st.KeepIDs, id) {
			t.Errorf("the keep list %v lacks %d", st.KeepIDs, id)
		}
	}
	// The pack that holds the kit sails.
	st = DecideLostCity(lcSnap(2, func(s *Snapshot) { lcGive(s, lcFullKit()) }))
	wantCustom(t, "kit held", st, CustomLcSail)
	// A part of the kit is topped up, not asked for again whole.
	st = DecideLostCity(lcSnap(2, func(s *Snapshot) {
		k := lcFullKit()
		k[ObjFireRune] = 100
		lcGive(s, k)
	}))
	if st.Kind != StepProvision {
		t.Errorf("five fire runes short: %s", st.Describe())
	}
	st = DecideLostCity(lcSnap(2, func(s *Snapshot) { s.Blocked = map[string]bool{"provision:" + KitLcTrip: true} }))
	wantWait(t, "the kit blocked", st, LcItemParkFor, "death rune")
	// The law rune of the cast out is as much the kit's as the food: a pack short of it is not sailed, and a kit blocked on it parks the quest
	// with its name (the way out is never the Wilderness).
	noLaw := func(s *Snapshot) {
		k := lcFullKit()
		delete(k, ObjLcLawRune)
		lcGive(s, k)
	}
	st = DecideLostCity(lcSnap(2, noLaw))
	if st.Kind != StepProvision || !strings.Contains(st.Reason, "law rune") || strings.Contains(st.Reason, "death rune") {
		t.Errorf("no law rune: %s", st.Describe())
	}
	st = DecideLostCity(lcSnap(2, func(s *Snapshot) { noLaw(s); s.Blocked = map[string]bool{"provision:" + KitLcTrip: true} }))
	wantWait(t, "no law rune anywhere", st, LcItemParkFor, "law rune")
	st = DecideLostCity(lcSnap(2, func(s *Snapshot) {
		lcGive(s, lcFullKit())
		s.Failed = map[string]bool{CustomLcSail: true}
	}))
	wantWait(t, "the monks did not sail", st, LcLegPark, "monks did not sail")
	// The pack that holds the kit but whose food is gone does not sail: the kit says food again.
	st = DecideLostCity(lcSnap(2, func(s *Snapshot) {
		k := lcFullKit()
		delete(k, ObjShark)
		lcGive(s, k)
	}))
	if st.Kind != StepProvision || !strings.Contains(st.Reason, "shark") {
		t.Errorf("no food: %s", st.Describe())
	}
	// Stage 3 to 5 trips ask for the food, the knife and the runes of the cast out, and not the spell's runes: there is no spirit left to fight.
	exitRunes := map[int]int{ObjFireRune: 1, ObjAirRune: 3, ObjLcLawRune: 1}
	for _, stage := range []int{LcSpiritDefeated, LcTreeChopped, LcStaffMade} {
		st = DecideLostCity(lcSnap(stage, func(s *Snapshot) { s.BankKnown, s.BankIDs = true, map[int]int{} }))
		if st.Kind != StepProvision || st.KitName != KitLcTrip || len(st.Kit) != 2+len(exitRunes) || st.Kit[0].ID != ObjKnife || st.Kit[1].ID != ObjShark || st.Kit[1].Qty != LcTripSharks {
			t.Errorf("stage %d trip: %+v", stage, st)
		}
		for _, l := range st.Kit[2:] {
			if exitRunes[l.ID] != l.Qty || !l.Stackable || l.Optional {
				t.Errorf("stage %d trip: the rune line %+v is not the exit's %v", stage, l, exitRunes)
			}
		}
		st = DecideLostCity(lcSnap(stage, func(s *Snapshot) {
			s.BankKnown, s.BankIDs = true, map[int]int{}
			lcGive(s, map[int]int{ObjKnife: 1, ObjShark: LcTripSharks, ObjFireRune: 1, ObjAirRune: 3, ObjLcLawRune: 1})
		}))
		wantCustom(t, "stage trip kit held", st, CustomLcSail)
	}
}

func TestLostCityDecideTheIslandAndTheDungeon(t *testing.T) {
	deck := tile.Tile{X: 2834, Z: 3331, Level: 1}
	st := DecideLostCity(lcSnap(2, func(s *Snapshot) { s.Tile = deck; lcGive(s, lcFullKit()) }))
	wantCustom(t, "on the deck", st, CustomLcDescend)
	st = DecideLostCity(lcSnap(2, func(s *Snapshot) {
		s.Tile = tile.Tile{X: 2830, Z: 3350}
		s.Failed = map[string]bool{CustomLcDescend: true}
	}))
	wantWait(t, "the ladder failed", st, LcLegPark, "cave monk")
	// The ladder is one way and the dungeon is left by a cast: an account that cannot cast one is not sent down (the Wilderness is never walked),
	// and one that holds Lumbridge's runes instead of Varrock's is.
	island := func(pack map[int]int) Step {
		return DecideLostCity(lcSnap(2, func(s *Snapshot) { s.Tile = deck; lcGive(s, pack) }))
	}
	noLaw := lcFullKit()
	delete(noLaw, ObjLcLawRune)
	wantWait(t, "on the deck without a law rune", island(noLaw), LcItemParkFor, "law rune")
	noAir := lcFullKit()
	noAir[ObjAirRune] = 2
	wantWait(t, "on the deck with two air runes", island(noAir), LcItemParkFor, "air rune")
	wantCustom(t, "on the deck with the kit", island(lcFullKit()), CustomLcDescend)
	lumbridge := map[int]int{ObjKnife: 1, ObjShark: LcSharks, ObjDeathRune: 21, ObjEarthRune: 1, ObjAirRune: 3, ObjLcLawRune: 1}
	wantCustom(t, "on the deck with Lumbridge's runes", island(lumbridge), CustomLcDescend)

	dungeon := func(stage int, mod func(*Snapshot)) Step {
		return DecideLostCity(lcSnap(stage, func(s *Snapshot) {
			lcInDungeon(s)
			lcGive(s, lcFullKit())
			if mod != nil {
				mod(s)
			}
		}))
	}
	// Stage 2: no axe is the zombies' to give; with it the spirit.
	wantCustom(t, "no axe", dungeon(2, nil), CustomLcAxe)
	wantCustom(t, "an axe", dungeon(2, func(s *Snapshot) { lcGive(s, map[int]int{ObjBronzeAxe: 1}) }), CustomLcSpirit)
	wantWait(t, "the zombies gave none", dungeon(2, func(s *Snapshot) { s.Failed = map[string]bool{CustomLcAxe: true} }), LcLegPark, "zombies")
	wantWait(t, "the spirit was not beaten", dungeon(2, func(s *Snapshot) {
		lcGive(s, map[int]int{ObjIronAxe: 1})
		s.Failed = map[string]bool{CustomLcSpirit: true}
	}), LcFightParkFor, "Tree spirit")
	// The food went on the zombies and the corridor: an account whose pack cannot take the spirit goes out by the door and is not sent in.
	noFood := func(s *Snapshot) {
		delete(s.InvIDs, ObjShark)
		s.HP = 30
	}
	wantCustom(t, "no food at the tree", dungeon(2, noFood), CustomLcPortal)
	wantCustom(t, "no food, an axe", dungeon(2, func(s *Snapshot) { noFood(s); lcGive(s, map[int]int{ObjBronzeAxe: 1}) }), CustomLcPortal)
	// Stage 3: an axe, then the branch.
	wantCustom(t, "stage 3 no axe", dungeon(3, nil), CustomLcAxe)
	wantCustom(t, "stage 3 axe", dungeon(3, func(s *Snapshot) { lcGive(s, map[int]int{ObjBronzeAxe: 1}) }), CustomLcBranch)
	wantWait(t, "no branch", dungeon(3, func(s *Snapshot) {
		lcGive(s, map[int]int{ObjBronzeAxe: 1})
		s.Failed = map[string]bool{CustomLcBranch: true}
	}), LcLegPark, "branch")
	// Stage 4 and after: the branch is carved in place with the knife, and with no knife the account goes out to fetch one.
	carve := dungeon(4, func(s *Snapshot) { lcGive(s, map[int]int{ObjLcBranch: 1}) })
	if carve.Kind != StepUseOn || carve.ItemID != ObjKnife || carve.TargetID != ObjLcBranch || carve.TargetKind != TargetItem || ValidateStep(carve) != "" {
		t.Errorf("a branch and a knife in the dungeon: %+v", carve)
	}
	wantCustom(t, "a branch and no knife", dungeon(4, func(s *Snapshot) { delete(s.InvIDs, ObjKnife); lcGive(s, map[int]int{ObjLcBranch: 1}) }), CustomLcPortal)
	// Stage 5: with the staff, out; without it and without a branch, the branch again (the tree gives one at every stage after 2).
	wantCustom(t, "the staff", dungeon(5, func(s *Snapshot) { lcGive(s, map[int]int{ObjLcStaff: 1}) }), CustomLcPortal)
	wantCustom(t, "the staff worn", dungeon(5, func(s *Snapshot) { s.WornIDs[ObjLcStaff] = 1 }), CustomLcPortal)
	wantCustom(t, "stage 5 and nothing", dungeon(5, func(s *Snapshot) { lcGive(s, map[int]int{ObjBronzeAxe: 1}) }), CustomLcBranch)
	// Zanaris on an unfinished tab (the quest's closing queue a few ticks away): the account waits, it does not walk out and in again.
	st = DecideLostCity(lcSnap(5, func(s *Snapshot) { s.Tile = LcZanarisArrival; s.WornIDs[ObjLcStaff] = 1 }))
	if st.Kind != StepWait || st.ParkFor != 0 || !strings.Contains(st.Reason, "Zanaris") {
		t.Errorf("in Zanaris on an unfinished tab: %s", st.Describe())
	}
}

func TestLostCityDecideAfterTheDungeon(t *testing.T) {
	// The staff in the pack or worn: the shed.
	for _, wear := range []bool{false, true} {
		st := DecideLostCity(lcSnap(5, func(s *Snapshot) {
			s.Tile = LcWildernessLanding
			if wear {
				s.WornIDs[ObjLcStaff] = 1
			} else {
				lcGive(s, map[int]int{ObjLcStaff: 1})
			}
		}))
		wantCustom(t, "the staff on the mainland", st, CustomLcZanaris)
	}
	st := DecideLostCity(lcSnap(5, func(s *Snapshot) {
		lcGive(s, map[int]int{ObjLcStaff: 1})
		s.Failed = map[string]bool{CustomLcZanaris: true}
	}))
	wantWait(t, "the door failed", st, LcLegPark, "shed's door")
	// A branch and a knife: carved wherever the account stands (the Wilderness included).
	st = DecideLostCity(lcSnap(4, func(s *Snapshot) { s.Tile = LcWildernessLanding; lcGive(s, map[int]int{ObjLcBranch: 1, ObjKnife: 1}) }))
	if st.Kind != StepUseOn || st.TargetID != ObjLcBranch {
		t.Errorf("a branch and a knife: %s", st.Describe())
	}
	// A branch and no knife: a knife only (no sharks, no runes), then the carve.
	st = DecideLostCity(lcSnap(4, func(s *Snapshot) { lcGive(s, map[int]int{ObjLcBranch: 1}) }))
	if st.Kind != StepProvision || len(st.Kit) != 1 || st.Kit[0].ID != ObjKnife {
		t.Errorf("a branch and no knife: %+v", st)
	}
	wantWait(t, "no knife anywhere", DecideLostCity(lcSnap(4, func(s *Snapshot) {
		lcGive(s, map[int]int{ObjLcBranch: 1})
		s.Blocked = map[string]bool{"provision:" + KitLcTrip: true}
	})), LcItemParkFor, "knife")
	// Nothing in hand at stage 4 or 5: the bank is read once, the staff or the branch it holds drawn, and only then the trip.
	for _, stage := range []int{LcTreeChopped, LcStaffMade} {
		st = DecideLostCity(lcSnap(stage, nil))
		if st.Kind != StepScanBank || st.Bank != DraynorBank {
			t.Errorf("stage %d with nothing: %s", stage, st.Describe())
		}
	}
	st = DecideLostCity(lcSnap(5, func(s *Snapshot) { s.BankKnown, s.BankIDs = true, map[int]int{ObjLcStaff: 1} }))
	if st.Kind != StepWithdraw || len(st.Items) != 1 || st.Items[0].ID != ObjLcStaff || ValidateStep(st) != "" {
		t.Errorf("a staff in the bank: %+v", st)
	}
	st = DecideLostCity(lcSnap(4, func(s *Snapshot) { s.BankKnown, s.BankIDs = true, map[int]int{ObjLcStaff: 1, ObjLcBranch: 2} }))
	if st.Kind != StepWithdraw || st.Items[0].ID != ObjLcBranch {
		t.Errorf("a branch in the bank at stage 4 (the staff is not made yet): %+v", st)
	}
	// Stage 3 has neither: it does not read the bank for them.
	st = DecideLostCity(lcSnap(3, nil))
	if st.Kind != StepProvision || st.KitName != KitLcTrip {
		t.Errorf("stage 3 on the mainland: %s", st.Describe())
	}
}

// Every step the decision asks for, over every stage, region and pack the sweep names, is well formed, and no step ever carries a forbidden
// thing onto the ship: no kit line, keep list or strip list holds an axe or the staff.
func TestLostCityEveryStepIsWellFormed(t *testing.T) {
	tiles := []tile.Tile{DraynorBank, LcShamusTreeAt, tile.Tile{X: 2834, Z: 3331, Level: 1}, LcZombieStand, LcTreeStand, LcWildernessLanding, LcZanarisArrival, LcShedOutside}
	packs := []map[int]int{{}, lcFullKit(), {ObjLcBranch: 1}, {ObjLcBranch: 1, ObjKnife: 1}, {ObjLcStaff: 1}, {ObjBronzeAxe: 1}, {ObjBronzeAxe: 1, ObjKnife: 1, ObjShark: 3},
		{ObjIronAxe: 1, ObjLcBranch: 1, ObjKnife: 1}, {ObjKnife: 1, ObjShark: LcTripSharks, ObjFireRune: 1, ObjAirRune: 3, ObjLcLawRune: 1},
		{ObjKnife: 1, ObjShark: LcSharks, ObjDeathRune: 21, ObjFireRune: 105, ObjAirRune: 84}}
	for _, journal := range []Status{StatusNotStarted, StatusInProgress} {
		for stage := 0; stage <= 5; stage++ {
			for _, at := range tiles {
				for _, pack := range packs {
					for _, flag := range []string{"", CustomLcSail, CustomLcAxe, CustomLcSpirit, CustomLcBranch, CustomLcDescend, CustomLcShamus, CustomLcZanaris} {
						s := lcSnap(stage, func(s *Snapshot) {
							s.Journal, s.Tile = journal, at
							lcGive(s, pack)
							if flag != "" {
								s.Failed = map[string]bool{flag: true}
							}
						})
						st := DecideLostCity(s)
						if why := ValidateStep(st); why != "" {
							t.Fatalf("journal %s stage %d at %v pack %v failed %q: %s (%s)", journal, stage, at, pack, flag, why, st.Describe())
						}
						for _, l := range st.Kit {
							if slices.Contains(LcForbiddenOnEntrana(), l.ID) && st.KitName == KitLcTrip {
								t.Fatalf("the trip kit carries %v, which the monks refuse", l)
							}
						}
					}
				}
			}
		}
	}
	keep := LcEntranaKeep()
	for _, id := range LcForbiddenOnEntrana() {
		if slices.Contains(keep, id) {
			t.Errorf("the strip's keep list holds %d, which the monks refuse", id)
		}
	}
	for _, id := range LcForbiddenOnEntrana() {
		if !slices.Contains(lcTools, id) {
			t.Errorf("%d is not one of the quest's tools: a floor bite or a top-up could take it", id)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The prices.
// ---------------------------------------------------------------------------------------------------------------------

func TestLostCitySpiritOdds(t *testing.T) {
	type prof struct {
		name              string
		att, str, def, hp int
		mag               int
		ok, corridorOK    bool
		packFor           func(magic int) map[int]int
	}
	kit := func(sharks int) func(int) map[int]int {
		return func(magic int) map[int]int {
			pack := map[int]int{ObjShark: sharks}
			if _, _, mh, runes, ok := LcKitSpell(magic); ok {
				for id, per := range runes {
					pack[id] = per * LcKitCasts(mh)
				}
			}
			// The runes of the cast out ride the real kit, and are not the fight's.
			for id, n := range LcKitExit().Runes {
				pack[id] += n
			}
			return pack
		}
	}
	for _, p := range []prof{
		{"the median caster", 53, 41, 50, 55, 66, true, true, kit(LcSharks)},
		{"a Magic 59 caster with 74 hitpoints", 60, 60, 60, 74, 59, true, true, kit(LcSharks)},
		{"Magic 41 (Wind Blast)", 53, 41, 50, 55, 41, true, true, kit(LcSharks)},
		{"Magic 35 (Fire Bolt)", 53, 41, 50, 55, 35, true, true, kit(LcSharks)},
		{"melee only (75s, no Magic)", 75, 75, 70, 74, 1, false, true, kit(LcSharks)},
		{"melee 99s", 99, 99, 99, 99, 1, false, true, kit(LcSharks)},
		{"40 hitpoints and Defence 30 take the fight with eight sharks but not the corridor", 30, 30, 30, 40, 66, true, false, kit(8)},
		{"no food at all", 53, 41, 50, 55, 66, false, false, kit(0)},
	} {
		s := Snapshot{MaxHP: p.hp, HP: p.hp, Levels: lcLevels(p.att, p.str, p.def, p.hp, p.mag), InvIDs: map[int]int{}}
		pack := p.packFor(p.mag)
		odds := LcSpiritOdds(s, pack)
		if odds.OK != p.ok {
			t.Errorf("%s: the spirit's odds are ok=%v (%s), want %v", p.name, odds.OK, odds.Why, p.ok)
		}
		if ok, why := LcCorridorFit(s, pack); ok != p.corridorOK {
			t.Errorf("%s: the corridor is ok=%v (%s), want %v", p.name, ok, why, p.corridorOK)
		}
		if !odds.OK && odds.Why == "" {
			t.Errorf("%s: a refusal with no reason", p.name)
		}
	}
	// An account whose levels are unread is not judged (as every other gate here treats an unread stat).
	if odds := LcSpiritOdds(Snapshot{InvIDs: map[int]int{}}, map[int]int{}); !odds.OK {
		t.Errorf("an unread account is refused: %s", odds.Why)
	}
	if ok, _ := LcCorridorFit(Snapshot{}, nil); !ok {
		t.Error("an account with unread hitpoints is refused the corridor")
	}
}

// The fight is won by casting and lost by meleeing at the fleet's levels: the reason the runes are in the kit.
func TestLostCityTheSpiritIsACastersFight(t *testing.T) {
	s := lcSnap(2, nil)
	melee := LcSpiritOdds(s, map[int]int{ObjShark: 10})
	if melee.OK || melee.Sims[0].Won {
		t.Errorf("the median caster meleeing the spirit with ten sharks: %+v", melee)
	}
	cast := LcSpiritOdds(s, lcFullKit())
	r := cast.Sims[0]
	if !cast.OK || !r.Won || r.Ticks > 120 {
		t.Errorf("the median caster casting: %+v (%s)", r, cast.Why)
	}
	// Fire Blast's runes carried: casts, never a worn staff (a staff is a weapon the monks take).
	if name, level, maxHit, runes, ok := LcKitSpell(66); !ok || name != "Fire Blast" || level != 59 || maxHit != 16 || runes[ObjDeathRune] != 1 || runes[ObjFireRune] != 5 || runes[ObjAirRune] != 4 {
		t.Errorf("the kit spell at Magic 66: %s %d %d %v", name, level, maxHit, runes)
	}
	if n := LcKitCasts(16); n != 21 {
		t.Errorf("casts for Fire Blast: %d", n)
	}
	if LcKitCasts(2) != LcMaxCasts || LcKitCasts(1000) != LcMinCasts || LcKitCasts(0) != LcMinCasts {
		t.Errorf("the cast bounds: %d %d %d", LcKitCasts(2), LcKitCasts(1000), LcKitCasts(0))
	}
	// The potential price of an account is the price of the account with the kit; a refusal there is a refusal before the first step.
	if odds := LcPotentialOdds(lcSnap(0, nil)); !odds.OK {
		t.Errorf("the median caster's potential odds: %s", odds.Why)
	}
	// An account at 5 hitpoints is priced as the account it is once mended (the potential is at full hitpoints).
	if odds := LcPotentialOdds(lcSnap(0, func(s *Snapshot) { s.HP = 5 })); !odds.OK {
		t.Errorf("a hurt account's potential odds: %s", odds.Why)
	}
}

// The way out of the dungeon is a teleport, and the Wilderness is never walked but as the last resort: the two exits, what each wants, which one is
// cast, and that the kit always holds what the cast out needs on top of what the fight spends.
func TestLostCityTheWayOutIsATeleport(t *testing.T) {
	exits := LcExits()
	if len(exits) != 2 || exits[0].Name != "Lumbridge Teleport" || exits[1].Name != "Varrock Teleport" {
		t.Fatalf("exits %+v: Lumbridge then Varrock", exits)
	}
	if exits[0].Level != 31 || exits[1].Level != 25 || LcLumbridgeLevel != 31 || LcVarrockLevel != 25 {
		t.Errorf("levels %d and %d", exits[0].Level, exits[1].Level)
	}
	if kit := LcKitExit(); kit.Name != "Varrock Teleport" || kit.Level != LcMinMagic || kit.Runes[ObjFireRune] != 1 || kit.Runes[ObjAirRune] != 3 || kit.Runes[ObjLcLawRune] != 1 || len(kit.Runes) != 3 {
		t.Errorf("the kit exit %+v", kit)
	}
	if e := exits[0]; e.Runes[ObjEarthRune] != 1 || e.Runes[ObjAirRune] != 3 || e.Runes[ObjLcLawRune] != 1 || len(e.Runes) != 3 {
		t.Errorf("Lumbridge's runes %v", e.Runes)
	}
	// Both land in a city on the mainland, far from the Wilderness and the dungeon, and every rune an exit takes is one the kit and the strip may carry.
	for _, e := range exits {
		if LcRegionOf(e.Landing) != LcMainland || LcInWilderness(e.Landing) || InLcDungeon(e.Landing) || e.Landing.Level != 0 {
			t.Errorf("%s lands at %v", e.Name, e.Landing)
		}
		for id := range e.Runes {
			if !slices.Contains(LcRuneIDs, id) || !slices.Contains(lcTools, id) || !slices.Contains(LcEntranaKeep(), id) {
				t.Errorf("%s wants rune %d, which the kit, the tools or the strip's keep list lack", e.Name, id)
			}
		}
	}
	if tile.Chebyshev(exits[0].Landing, LcShedOutside) > 60 {
		t.Errorf("Lumbridge's landing %v is not near the shed", exits[0].Landing)
	}
	// The copies are the caller's own.
	exits[0].Runes[ObjEarthRune] = 99
	LcKitExit().Runes[ObjFireRune] = 99
	if LcExits()[0].Runes[ObjEarthRune] != 1 || LcKitExit().Runes[ObjFireRune] != 1 {
		t.Error("LcExits or LcKitExit hands out its own maps")
	}

	varrock := map[int]int{ObjFireRune: 1, ObjAirRune: 3, ObjLcLawRune: 1}
	lumbridge := map[int]int{ObjEarthRune: 1, ObjAirRune: 3, ObjLcLawRune: 1}
	both := map[int]int{ObjFireRune: 1, ObjEarthRune: 1, ObjAirRune: 3, ObjLcLawRune: 1}
	with := func(base map[int]int, mod func(map[int]int)) map[int]int {
		out := map[int]int{}
		for id, n := range base {
			out[id] = n
		}
		mod(out)
		return out
	}
	for _, c := range []struct {
		name  string
		pack  map[int]int
		magic int
		want  string
	}{
		{"Varrock's runes", varrock, 66, "Varrock Teleport"},
		{"Lumbridge's runes", lumbridge, 66, "Lumbridge Teleport"},
		{"both sets: the nearer", both, 66, "Lumbridge Teleport"},
		{"both sets at Magic 28: Lumbridge wants 31", both, 28, "Varrock Teleport"},
		{"Lumbridge's runes at Magic 30", lumbridge, 30, ""},
		{"Varrock's runes at Magic 25", varrock, 25, "Varrock Teleport"},
		{"Varrock's runes at Magic 24", varrock, 24, ""},
		{"Magic not read", varrock, 0, "Varrock Teleport"},
		{"no law rune", with(varrock, func(m map[int]int) { delete(m, ObjLcLawRune) }), 66, ""},
		{"two air runes", with(varrock, func(m map[int]int) { m[ObjAirRune] = 2 }), 66, ""},
		{"no fire rune", with(varrock, func(m map[int]int) { delete(m, ObjFireRune) }), 66, ""},
		{"a hundred of each", map[int]int{ObjFireRune: 100, ObjAirRune: 100, ObjLcLawRune: 100}, 99, "Varrock Teleport"},
		{"an empty pack", map[int]int{}, 66, ""},
		{"a nil pack", nil, 66, ""},
	} {
		got, ok := LcExitFor(c.pack, c.magic)
		if (c.want == "") == ok || got.Name != c.want {
			t.Errorf("%s: exit %q (castable %v), want %q", c.name, got.Name, ok, c.want)
		}
		why := LcExitRefusal(c.pack, c.magic)
		if (why == "") != (c.want != "") {
			t.Errorf("%s: refusal %q for an exit that is %q", c.name, why, c.want)
		}
	}
	if why := LcExitRefusal(with(varrock, func(m map[int]int) { delete(m, ObjLcLawRune) }), 66); !strings.Contains(why, "0 of 1 law rune") || !strings.Contains(why, "Varrock Teleport") {
		t.Errorf("the refusal for a missing law rune: %q", why)
	}
	if why := LcExitRefusal(varrock, 20); !strings.Contains(why, "Magic 20 is under the 25") {
		t.Errorf("the refusal for Magic 20: %q", why)
	}

	// The kit always holds the cast out on top of what the fight spends: whatever the Magic level, a pack of exactly the kit, with the fight's casts
	// spent, still casts an exit; and a trip with no fight carries the exit's runes alone.
	for magic := LcMinMagic; magic <= 99; magic++ {
		for _, fight := range []bool{true, false} {
			s := Snapshot{Levels: map[string]int{"magic": magic}}
			pack := map[int]int{}
			for _, l := range LcTripKit(s, fight) {
				if l.ID != ObjShark && l.ID != ObjKnife {
					pack[l.ID] = l.Qty
				}
			}
			if _, _, maxHit, runes, ok := LcKitSpell(magic); ok && fight {
				for id, per := range runes {
					pack[id] -= per * LcKitCasts(maxHit)
				}
			}
			if !fight && len(pack) != 3 {
				t.Errorf("Magic %d, no fight: the rune lines are %v, want the exit's three", magic, pack)
			}
			if _, ok := LcExitFor(pack, magic); !ok {
				t.Fatalf("Magic %d, fight %v: after the casts the pack %v cannot cast an exit: %s", magic, fight, pack, LcExitRefusal(pack, magic))
			}
		}
	}
	// The kit's numbers at Magic 66: 21 Fire Blasts and the Varrock Teleport (the mule's runes, summed per rune).
	kitOf := func(fight bool) map[int]int {
		out := map[int]int{}
		for _, l := range LcTripKit(lcSnap(2, nil), fight) {
			out[l.ID] = l.Qty
		}
		return out
	}
	want := map[int]int{ObjKnife: 1, ObjShark: LcSharks, ObjDeathRune: 21, ObjFireRune: 106, ObjAirRune: 87, ObjLcLawRune: 1}
	if got := kitOf(true); !mapsEqual(got, want) {
		t.Errorf("the fight trip's kit %v, want %v", got, want)
	}
	if got := kitOf(false); !mapsEqual(got, map[int]int{ObjKnife: 1, ObjShark: LcTripSharks, ObjFireRune: 1, ObjAirRune: 3, ObjLcLawRune: 1}) {
		t.Errorf("the trip's kit without a fight: %v", got)
	}
}

func mapsEqual(a, b map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// The fight is priced on the stock above the exit's runes: the cast out is never spent on a spell. A pack that holds a death rune and exactly the
// Varrock Teleport's three air could cast one Wind Blast with them; the model does not count it.
func TestLostCityTheFightIsPricedAboveTheExitRunes(t *testing.T) {
	stock := lcFightStock(lcFullKit())
	if stock[ObjFireRune] != 105 || stock[ObjAirRune] != 84 || stock[ObjDeathRune] != 21 || stock[ObjLcLawRune] != 0 {
		t.Errorf("the fight's stock of the full kit is %v: 21 Fire Blasts' worth and no law rune", stock)
	}
	if lcFullKit()[ObjFireRune] != 106 {
		t.Error("lcFightStock changed the pack it was given")
	}
	pack := map[int]int{ObjDeathRune: 1, ObjAirRune: 3, ObjFireRune: 1, ObjLcLawRune: 1}
	if plan := CastPlan(pack, nil, 66, 400); len(plan) == 0 {
		t.Fatal("the unreserved pack casts nothing: the check below proves nothing")
	}
	if plan := CastPlan(lcFightStock(pack), nil, 66, 400); len(plan) != 0 {
		t.Errorf("the fight is priced on the exit's runes: %+v", plan)
	}
	// The full kit wins inside the fight's tick budget with the exit's runes held back; a kit that holds the exit's runes and nothing for the
	// fight does not (the median caster meleeing the spirit loses it).
	s := lcSnap(2, nil)
	if full := LcSpiritOdds(s, lcFullKit()); !full.OK || full.Sims[0].Ticks >= 120 {
		t.Errorf("the full kit's odds %+v (%s)", full.Sims[0], full.Why)
	}
	exitOnly := map[int]int{ObjShark: LcSharks, ObjFireRune: 1, ObjAirRune: 3, ObjLcLawRune: 1}
	if odds := LcSpiritOdds(s, exitOnly); odds.OK {
		t.Errorf("a pack of the exit's runes alone beats the spirit: %+v", odds.Sims[0])
	}
}

// The corridor's cost is the greater demons' swings at the rate they hit this Defence, and an account at Defence 50 takes about twenty hitpoints a pass.
func TestLostCityCorridorDamage(t *testing.T) {
	s := lcSnap(2, nil)
	if d := LcCorridorDamage(s); d < 18 || d > 26 {
		t.Errorf("the corridor costs the median caster %.1f hitpoints a pass", d)
	}
	weak := lcSnap(2, func(s *Snapshot) { s.Levels["defence"] = 1 })
	strong := lcSnap(2, func(s *Snapshot) { s.Levels["defence"] = 99 })
	if !(LcCorridorDamage(weak) > LcCorridorDamage(s) && LcCorridorDamage(s) > LcCorridorDamage(strong)) {
		t.Errorf("damage by Defence: %.1f %.1f %.1f", LcCorridorDamage(weak), LcCorridorDamage(s), LcCorridorDamage(strong))
	}
	if FoeGreaterDemon.MaxHit() != 9 || FoeTreeSpirit.MaxHit() != 10 || FoeEntranaZombie.MaxHit() != 3 {
		t.Errorf("the foes' hardest hits: %d %d %d", FoeGreaterDemon.MaxHit(), FoeTreeSpirit.MaxHit(), FoeEntranaZombie.MaxHit())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The geography.
// ---------------------------------------------------------------------------------------------------------------------

func TestLostCityRegions(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want LcRegion
	}{
		{LcDungeonArrival, LcDungeon}, {LcZombieStand, LcDungeon}, {LcTreeStand, LcDungeon}, {LcSpiritSpawn, LcDungeon}, {LcDoorStand, LcDungeon}, {LcDoorAt, LcDungeon},
		{LcZanarisArrival, LcZanaris}, {tile.Tile{X: 3222, Z: 9589}, LcZanaris},
		{tile.Tile{X: 2834, Z: 3331, Level: 1}, LcIsland}, {LcCaveMonkAnchor, LcIsland}, {PortSarimMonks, LcMainland}, {LcShedOutside, LcMainland},
		{LcWildernessLanding, LcMainland}, {DraynorBank, LcMainland},
		// The dungeon's north (the dragons and the poison spiders) is beyond the pocket's box and no part of this quest.
		{tile.Tile{X: 2880, Z: 9800}, LcMainland},
	} {
		if got := LcRegionOf(c.at); got != c.want {
			t.Errorf("%v is in region %d, want %d", c.at, got, c.want)
		}
	}
	if !LcInWilderness(LcWildernessLanding) || LcInWilderness(LcShedOutside) || LcInWilderness(DraynorBank) {
		t.Error("the Wilderness rectangle")
	}
	// The Wilderness level of the landing (3772 - 3520) / 8 + 1: 32, where teleports are refused (above 20).
	if lvl := (LcWildernessLanding.Z-3520)/8 + 1; lvl != 32 || lvl <= 20 {
		t.Errorf("the landing's Wilderness level %d", lvl)
	}
	// The black demons' ground keeps the zombies' stand and the tree's stand clear and covers the demons' own spawns.
	if InLcBlackDemonGround(LcZombieStand) || InLcBlackDemonGround(LcTreeStand) || InLcBlackDemonGround(LcDoorStand) || InLcBlackDemonGround(LcDungeonArrival) {
		t.Error("a stand of the quest is in a black demon's ground")
	}
	for _, h := range LcBlackDemonSpawns() {
		if !InLcBlackDemonGround(h) {
			t.Errorf("the black demon at %v is not in its own ground", h)
		}
	}
	if len(LcBlackDemonSpawns()) != 4 || len(LcGreaterDemonSpawns()) != 2 {
		t.Error("the demons' spawns")
	}
	// The shed's door is opened from the tile west of it: a tile whose x is not the door's (check_axis).
	if LcShedOutside.X == LcShedDoorAt.X || LcShedOutside.Z != LcShedDoorAt.Z || LcDoorStand.X == LcDoorAt.X {
		t.Errorf("the shed's outside %v, the door %v; the Magic door's stand %v, the door %v", LcShedOutside, LcShedDoorAt, LcDoorStand, LcDoorAt)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The module and the ladder.
// ---------------------------------------------------------------------------------------------------------------------

func TestLostCityModuleWiring(t *testing.T) {
	m := LostCity
	r := m.Record
	if r.ID != "zanaris" || r.Name != "Lost City" || r.QuestPoints != 3 || LostCityID != "zanaris" || LostCityName != "Lost City" {
		t.Errorf("record %+v", r)
	}
	// Crafting and Woodcutting are the content's; Magic 25 is the module's (the Varrock Teleport out of the dungeon).
	wantSkills := map[string]int{"crafting": 31, "woodcutting": 36, "magic": 25}
	if len(r.Requirements.Skills) != 3 || len(r.Requirements.Quests) != 0 || r.Requirements.MinQuestPoints != 0 || LcMinMagic != 25 || LcMinMagic != LcKitExit().Level {
		t.Errorf("requirements %+v", r.Requirements)
	}
	for _, sk := range r.Requirements.Skills {
		if wantSkills[sk.Skill] != sk.Level {
			t.Errorf("requirement %+v", sk)
		}
	}
	if m.Decide == nil || m.ReadStage == nil || !m.NoRetaliate || m.MaxDeaths != 1 || m.MaxNearDeaths != 1 || m.ExclusiveDone == nil {
		t.Errorf("module %+v", m)
	}
	if c, ok := m.StepDeathCaps[CustomLcSpirit]; !ok || c.Max != 1 || c.Skip != 24*time.Hour {
		t.Errorf("step death caps %+v", m.StepDeathCaps)
	}
	if m.Bank != DraynorBank || !m.BankSet {
		t.Errorf("bank %v", m.Bank)
	}
	// Food is no tool (the top-up would never eat it); the knife, the branch, the staff, the coins, every axe and the runes are.
	for _, id := range lcFoodIDs {
		if slices.Contains(m.ToolIDs, id) {
			t.Errorf("food %d is a tool: the top-up and the floor bite would leave it", id)
		}
	}
	for _, id := range append([]int{ObjKnife, ObjLcBranch, ObjLcStaff, CoinsID, ObjFireRune, ObjAirRune, ObjDeathRune, ObjLcLawRune}, LcAxes...) {
		if !slices.Contains(m.ToolIDs, id) {
			t.Errorf("%d is not a tool of the quest", id)
		}
	}
	if !slices.Contains(m.Sustain.FoodIDs, ObjShark) || m.Sustain.EatAt < FoeTreeSpirit.PeakHit()*2 {
		t.Errorf("sustain %+v: the eat line is twice the spirit's hardest hit", m.Sustain)
	}
	// The slot is released once the journal says the spirit is defeated, and not before.
	if LcPastSpirit(lcSnap(2, nil)) || !LcPastSpirit(lcSnap(3, nil)) || !LcPastSpirit(lcSnap(5, nil)) || LcPastSpirit(lcSnap(3, func(s *Snapshot) { s.StageKnown = false })) {
		t.Error("LcPastSpirit")
	}
}

func TestLostCitySitsBetweenDwarfCannonAndHazeelCult(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	z := at(LostCityID)
	if z < 0 || z <= at(DwarfCannonID) || z >= at(HazeelCultID) || at("dragon") != len(OwnerLadder)-1 || at(BlackKnightsFortressID) != len(OwnerLadder)-2 {
		t.Fatalf("OwnerLadder %v: want ... sheepherder, mcannon, itwatchtower, zanaris, chompybird, mortton, hazeelcult, elemental_workshop, desertrescue, blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[LostCityID]
	if !ok {
		t.Fatal("no ladder row")
	}
	if f.ID != LostCityID || f.Name != LostCity.Record.Name || f.QP != 3 || !f.Filler || f.QP != LostCity.Record.QuestPoints {
		t.Errorf("row %+v does not match the record %+v", f, LostCity.Record)
	}
	if f.Finale || f.TrainSkill != "" || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 || len(f.XP) != 0 {
		t.Errorf("row %+v: a filler with no prerequisite, no combat advice and no experience", f)
	}
	if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
		t.Errorf("row lacks its estimate, region, source or notes: %+v", f)
	}
	// The levels are advised, never hard: a hard gate would insert a training stop for one filler (and must name a rung that produces it).
	if len(f.Gates) != 3 {
		t.Fatalf("gates %+v", f.Gates)
	}
	for _, g := range f.Gates {
		if g.Kind != GateAdvised || g.Hard() || (g.Skill != "crafting" && g.Skill != "woodcutting" && g.Skill != "magic") {
			t.Errorf("gate %+v", g)
		}
	}
	if QuestTabName(LostCityID) != "Lost City" {
		t.Errorf("tab name %q", QuestTabName(LostCityID))
	}
	// The slot is the estimate and a quarter, and well under the longest a holder of a fleet-wide slot keeps (90 minutes).
	if got := QuestSlot(LostCityID, 20*time.Minute); got != 75*time.Minute || got >= 90*time.Minute {
		t.Errorf("slot %s", got)
	}
	if why := RungHeld(LostCityID); why != "" {
		t.Errorf("held back by the ladder: %s", why)
	}
	files := ModuleFiles[LostCityID]
	if len(files) != 2 {
		t.Fatalf("ModuleFiles %v: want its plan and its customs", files)
	}
	for _, file := range files {
		if _, err := os.Stat("../../../" + file); err != nil {
			t.Errorf("ModuleFiles names %s: %v", file, err)
		}
	}
	// Neither a prerequisite quest nor a gate rung in front of it: the planner inserts nothing for an account with no Crafting and Woodcutting.
	if got := planIDs(PlanLadder([]string{LostCityID}, LadderFacts, stage5Graduate())); !slices.Equal(got, []string{LostCityID}) {
		t.Errorf("the plan for the quest alone is %v", got)
	}
	if holds := LadderHolds(LostCity.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
		t.Errorf("the ladder's own holds are %+v: the record is the one", holds)
	}
	below := CheckRequirements(LostCity.Record, PlayerState{SkillLevels: map[string]int{"crafting": 30, "woodcutting": 36, "magic": 25}})
	failed := 0
	for _, r := range below {
		if !r.OK {
			failed++
			if !strings.Contains(r.Reason, "Crafting 31 (have 30)") {
				t.Errorf("the record says %q", r.Reason)
			}
		}
	}
	if failed != 1 {
		t.Errorf("at Crafting 30 the record fails %d of its three skills: %+v", failed, below)
	}
	// Under Magic 25 the record holds the ladder off the quest (the way out is a Varrock Teleport).
	lowMagic := CheckRequirements(LostCity.Record, PlayerState{SkillLevels: map[string]int{"crafting": 31, "woodcutting": 36, "magic": 24}})
	failed = 0
	for _, r := range lowMagic {
		if !r.OK {
			failed++
			if !strings.Contains(r.Reason, "Magic 25 (have 24)") {
				t.Errorf("the record says %q", r.Reason)
			}
		}
	}
	if failed != 1 {
		t.Errorf("at Magic 24 the record fails %d skills: %+v", failed, lowMagic)
	}
	// A filler of three points: one short of Dragon Slayer's 32 with every other filler done, Lost City is what the planner adds.
	short := stage5Graduate()
	short.QP = 29
	short.Owned[ObjNails] = 12
	short.Done = map[string]bool{}
	for id, fr := range LadderFacts {
		if fr.Filler && id != LostCityID {
			short.Done[id] = true
		}
	}
	if got := planIDs(PlanLadder([]string{"dragon"}, LadderFacts, short)); len(got) != 2 || got[0] != LostCityID {
		t.Errorf("three points short with only Lost City left: plan %v", got)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The fleet's own levels.
// ---------------------------------------------------------------------------------------------------------------------

// lcCensus reads testdata/lostcity_census_20261019.csv: the levels of the 211 live accounts that reported them on 19 Oct 2026 (no names).
func lcCensus(t *testing.T) [][7]int {
	t.Helper()
	b, err := os.ReadFile("testdata/lostcity_census_20261019.csv")
	if err != nil {
		t.Fatal(err)
	}
	var out [][7]int
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var row [7]int
		parts := strings.Split(line, ",")
		if len(parts) != 7 {
			t.Fatalf("census row %q", line)
		}
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				t.Fatalf("census row %q: %v", line, err)
			}
			row[i] = n
		}
		out = append(out, row)
	}
	return out
}

// lcCensusSnap is a snapshot of an account of the census at stage 0 at Draynor's bank, with the quest's own levels given (the ladder's rungs
// give an account at the tail Crafting and Woodcutting 40; the census holds what they were that day).
func lcCensusSnap(row [7]int, crafting, woodcutting int) Snapshot {
	s := lcSnap(0, func(s *Snapshot) {
		s.Levels = map[string]int{"attack": row[0], "strength": row[1], "defence": max(row[2], 1), "hitpoints": row[3], "magic": row[4], "crafting": crafting, "woodcutting": woodcutting}
		s.HP, s.MaxHP = row[3], row[3]
	})
	return s
}

// What the decision says of the live fleet's levels (the census of 19 Oct 2026): who the quest would begin for today, who it refuses for the levels it
// asks and who for the fight, and who it would begin for once the ladder's own rungs had given every account Crafting and Woodcutting 40. The rule
// the numbers rest on: a caster (Magic 41 and over holds Wind Blast and better) with the hitpoints and Defence for the corridor begins, and a melee
// account never does.
func TestLostCityTheFleetsOwnLevels(t *testing.T) {
	census := lcCensus(t)
	if len(census) != 211 {
		t.Fatalf("the census holds %d accounts", len(census))
	}
	var short, refused, begin, beginAtForty, castersRefused, magicOnly int
	for _, row := range census {
		now := DecideLostCity(lcCensusSnap(row, row[5], row[6]))
		at40 := DecideLostCity(lcCensusSnap(row, 40, 40))
		switch {
		case now.Kind == StepTalk:
			begin++
		case strings.Contains(now.Reason, "Crafting") || strings.Contains(now.Reason, "Woodcutting"):
			short++
			// Magic 25 (the Varrock Teleport out) is the third level the quest asks: how many of the accounts it stops have the other two.
			if row[5] >= LcCrafting && row[6] >= LcWoodcutting {
				magicOnly++
			}
		case now.Kind == StepWait && now.ParkFor == LcFightParkFor:
			refused++
		default:
			t.Errorf("row %v: %s", row, now.Describe())
		}
		if at40.Kind == StepTalk {
			beginAtForty++
			if row[4] < 35 || row[3] < 45 {
				t.Errorf("row %v begins: Magic %d, Hitpoints %d", row, row[4], row[3])
			}
		} else if at40.Kind != StepWait || at40.ParkFor != LcFightParkFor {
			t.Errorf("row %v at 40/40: %s", row, at40.Describe())
		} else if row[4] >= 59 && row[3] >= 60 {
			castersRefused++
			t.Errorf("row %v: a Magic %d caster with %d hitpoints is refused: %s", row, row[4], row[3], at40.Reason)
		}
		// A melee account (no Magic worth casting) is refused whatever its melee levels: the fight is a caster's.
		if row[4] < 35 && at40.Kind == StepTalk {
			t.Errorf("row %v: a non-caster begins", row)
		}
	}
	t.Logf("of %d accounts: %d would begin today, %d are short of Crafting 31 or Woodcutting 36, %d are refused for the fight or the corridor; with Crafting and Woodcutting 40, %d would begin",
		len(census), begin, short, refused, beginAtForty)
	if castersRefused != 0 {
		t.Errorf("%d strong casters were refused", castersRefused)
	}
	// The census is a fixture, so the counts are exact: 21 begin today, 188 are short of the quest's two levels (the Crafting and Woodcutting rungs of
	// the ladder give them), 2 are refused by the model, and 174 of 211 could do it with the two levels (the other 37 are melee accounts and idle
	// ones: three under Magic 35, twenty-five under 45 hitpoints and nine casters of 46 to 49 hitpoints whom the corridor's margin refuses).
	if begin != 21 || short != 188 || refused != 2 || beginAtForty != 174 {
		t.Errorf("the census says %d begin, %d short, %d refused, %d at 40/40; the fixture said 21, 188, 2, 174", begin, short, refused, beginAtForty)
	}
	// The Magic 25 the exit asks costs the fleet nothing: no account that has Crafting 31 and Woodcutting 36 is stopped by its Magic alone (every one of
	// them that does not begin is refused by the fight), and none of the 174 that could begin with the two levels has less than Magic 35.
	if magicOnly != 0 {
		t.Errorf("%d accounts with Crafting 31 and Woodcutting 36 are stopped by Magic 25 alone", magicOnly)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The provisioning.
// ---------------------------------------------------------------------------------------------------------------------

// The trip's kit, planned by the provisioner (PlanProvision) over what the live mule publishes (20 Oct 2026: 78,332 sharks, 183 knives, 19,913 death,
// 486,598 fire, 961,379 air runes and 935 law runes, and NO earth rune, which is why the cast out is Varrock's and not Lumbridge's) and an own bank
// that holds none of it: one bundled ask for every line, inside the mule's cap of 28 unstacked items; with the own bank holding it all the bank gives
// it; with no sharks in either the plan is blocked on them and on nothing else.
func TestLostCityTheKitIsProvisionedFromTheBankAndTheMule(t *testing.T) {
	s := lcSnap(2, nil)
	kit := LcTripKit(s, true)
	mule := map[int]int{ObjShark: 78332, ObjKnife: 183, ObjDeathRune: 19913, ObjFireRune: 486598, ObjAirRune: 961379, ObjLcLawRune: 935}
	st := ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, Bank: map[int]int{}, BankKnown: true, Mule: mule, MuleKnown: true, FreeSlots: 28, Levels: map[string]int{}}
	plan := PlanProvision(kit, st)
	if plan.Phase != PhaseMule || len(plan.Blocked) != 0 || len(plan.Withdraw) != 0 {
		t.Fatalf("an empty bank and the live mule: %+v", plan)
	}
	ask := map[int]int{}
	slots := 0
	for _, c := range plan.MuleAsk {
		ask[c.ID] = c.Count
		if c.ID == ObjShark || c.ID == ObjKnife {
			slots += c.Count
		} else {
			slots++
		}
	}
	for id, n := range map[int]int{ObjKnife: 1, ObjShark: LcSharks, ObjDeathRune: 21, ObjFireRune: 106, ObjAirRune: 87, ObjLcLawRune: 1} {
		if ask[id] != n {
			t.Errorf("the mule is asked for %d of %d, want %d (ask %v)", ask[id], id, n, plan.MuleAsk)
		}
	}
	if len(ask) != 6 || ask[ObjEarthRune] != 0 {
		t.Errorf("the ask %v: six lines and no earth rune (the mule holds none)", ask)
	}
	if slots > MaxBundleSlots {
		t.Errorf("the ask is %d slots, over the bundle's %d", slots, MaxBundleSlots)
	}
	// The own bank holds it all: drawn from the bank, nothing asked of the mule.
	st.Bank = map[int]int{ObjShark: 40, ObjKnife: 3, ObjDeathRune: 500, ObjFireRune: 2000, ObjAirRune: 2000, ObjLcLawRune: 50}
	plan = PlanProvision(kit, st)
	if plan.Phase != PhaseBank || len(plan.MuleAsk) != 0 || len(plan.Withdraw) != 6 {
		t.Errorf("a bank that holds the kit: %+v", plan)
	}
	// No law rune in the bank or on the mule: the plan is blocked on it and on nothing else (the trip is not made without the cast out).
	st.Bank = map[int]int{}
	lawless := map[int]int{ObjShark: 78332, ObjKnife: 183, ObjDeathRune: 19913, ObjFireRune: 486598, ObjAirRune: 961379}
	st.Mule = lawless
	plan = PlanProvision(kit, st)
	if plan.Phase == PhaseSatisfied || len(plan.Blocked) != 1 || !strings.Contains(plan.Blocked[0], "Law rune") {
		t.Errorf("no law rune anywhere: %+v", plan)
	}
	st.Mule = mule
	// Nowhere any sharks: blocked on them only (the mule's published bank is the filter, so no ask is placed for what it lacks).
	st.Bank = map[int]int{}
	delete(mule, ObjShark)
	plan = PlanProvision(kit, st)
	if plan.Phase == PhaseSatisfied || len(plan.Blocked) != 1 || !strings.Contains(plan.Blocked[0], "Shark") {
		t.Errorf("no sharks anywhere: %+v", plan)
	}
	// A pack that holds the kit is satisfied, and a kit line is never a worn one (Entrana takes no armour, and the strip has stripped).
	st.Pack = lcFullKit()
	if plan := PlanProvision(kit, st); plan.Phase != PhaseSatisfied {
		t.Errorf("a full pack: %+v", plan)
	}
	for _, l := range kit {
		if l.Worn || l.Untradeable || l.Optional {
			t.Errorf("kit line %+v", l)
		}
	}
	// The axe line (stage 1): a worn axe of the line's own kind counts as held; an empty world with coins in the pack is sent to Bob's store, the one
	// source, and with none the plan says the shops cannot be paid (the bank's coins are drawn first when it holds some).
	axe := lcAxeLine(lcSnap(1, nil))
	if plan := PlanProvision([]KitLine{axe}, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{ObjBronzeAxe: 1}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20}); plan.Phase != PhaseSatisfied {
		t.Errorf("a worn bronze axe: %+v", plan)
	}
	plan = PlanProvision([]KitLine{axe}, ProvisionState{Pack: map[int]int{CoinsID: 100}, Worn: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20,
		Levels: map[string]int{"woodcutting": 40}})
	if plan.Phase != PhaseSource || len(plan.Tasks) != 1 || plan.Tasks[0].Source.Kind != SourceShop || plan.Tasks[0].Source.Shop.NpcID != BobsAxes.NpcID {
		t.Errorf("an axe from nowhere: %+v", plan)
	}
	plan = PlanProvision([]KitLine{axe}, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, Bank: map[int]int{CoinsID: 5000}, BankKnown: true, FreeSlots: 20,
		Levels: map[string]int{"woodcutting": 40}})
	if plan.Phase != PhaseBank || len(plan.Withdraw) == 0 || plan.Withdraw[0].ID != CoinsID {
		t.Errorf("an axe from nowhere, coins in the bank: %+v", plan)
	}
}
