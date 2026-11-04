package questplan

import (
	"fmt"
	"strings"
	"time"
)

// Tai Bwo Wannai Trio's decision: the order of the legs, what each needs in the pack, and when the quest parks (taibwotrio.go has the
// story and the stages, taibwohazards.go the ground).
//
// The legs, in the only order that closes (the TS def's, checked against the content): Lubufu (the Karambwanji, the apprenticeship, a
// vessel); Tiadeche's catch (the vessel baited and handed over, his first Karambwan); Tamayu (the vow, a first hunt watched, four doses
// of agility potion, a Karambwan-poisoned spear, the killing hunt); Tinsay (the rum, the sandwich, the marinade); Tiadeche's manual (a
// second vessel to Tinsay, the manual to Tiadeche); and Timfraku. Tamayu comes before Tinsay's sandwich because only he skins a monkey and
// only once his hunt is over; Tiadeche's catch comes before Tamayu because the Karambwan the spear is made of is his first catch; the
// manual comes last because Tinsay writes it only when his three deliveries are done.

// The custom steps (internal/scripts/questmods) and kit names.
const (
	CustomTbStart         = "tbwt-start"
	CustomTbReport        = "tbwt-report"
	CustomTbFish          = "tbwt-fish"
	CustomTbLubufuMeet    = "tbwt-lubufu-meet"
	CustomTbLubufuGive    = "tbwt-lubufu-give"
	CustomTbLubufuAsk     = "tbwt-lubufu-ask"
	CustomTbVessel        = "tbwt-vessel"
	CustomTbBait          = "tbwt-bait"
	CustomTbTiadecheMeet  = "tbwt-tiadeche-meet"
	CustomTbTiadecheCatch = "tbwt-tiadeche-catch"
	CustomTbTiadecheGift  = "tbwt-tiadeche-gift"
	CustomTbKarambwan     = "tbwt-karambwan"
	CustomTbTamayuMeet    = "tbwt-tamayu-meet"
	CustomTbTamayuHunt    = "tbwt-tamayu-hunt"
	CustomTbTamayuGive    = "tbwt-tamayu-give"
	CustomTbCook          = "tbwt-cook"
	CustomTbGrind         = "tbwt-grind"
	CustomTbPoison        = "tbwt-poison"
	CustomTbRum           = "tbwt-rum"
	CustomTbBanana        = "tbwt-banana"
	CustomTbSlice         = "tbwt-slice"
	CustomTbMix           = "tbwt-mix"
	CustomTbMonkey        = "tbwt-monkey"
	CustomTbSkin          = "tbwt-skin"
	CustomTbSandwich      = "tbwt-sandwich"
	CustomTbJogre         = "tbwt-jogre"
	CustomTbBurn          = "tbwt-burn"
	CustomTbPasteBones    = "tbwt-paste-bones"
	CustomTbTinsayMeet    = "tbwt-tinsay-meet"
	CustomTbTinsayGive    = "tbwt-tinsay-give"
	CustomTbManual        = "tbwt-manual"

	KitTbScarce = "tbwt-scarce"
	KitTbTools  = "tbwt-tools"
)

// The quest's gates and numbers.
const (
	// TbMinMagic is Superheat Item's level (magic_spells.dbrow [magic_spell_superheat]): the only way the fleet burns Jogre bones.
	TbMinMagic = 43
	// TbMinHitpoints is the module's own floor.
	TbMinHitpoints = 35
	// TbCoinsQty and TbCoinsMin: the ships are 30 a crossing, the rum about 30 and BuyFromShop keeps 100 for the fare home.
	TbCoinsQty = 300
	TbCoinsMin = 150
	// TbRoom is the pack room the crossing wants before it starts: the Karambwanji, the vessel, the bananas and the bones.
	TbRoom = 8
	// TbSharks is the food asked of the mule.
	TbSharks = 6
	// TbKeepKarambwanji is how many raw Karambwanji the pack keeps once Lubufu has his twenty: a bait for Tiadeche's vessel, a paste for
	// Tinsay's marinade, and one to spare (the vessel's fishing, a lost bait).
	TbKeepKarambwanji = 3
	// TbFishReserve is the free slots a fishing round leaves: the shrimps that come with the Karambwanji, and the vessel.
	TbFishReserve = 2
	// TbCastCasts is the Wind Bolt casts the monkey's runes pay for (2 air and a chaos rune each).
	TbCastCasts = 6
	// TbSupplyPark, TbStatsPark, TbOddsPark and TbLegPark are the parks of a supply the bank and the mule do not hold, of a level the
	// account lacks, of a price the model refuses and of a leg that would not be done.
	TbSupplyPark = 6 * time.Hour
	TbStatsPark  = 12 * time.Hour
	TbOddsPark   = 3 * time.Hour
	TbLegPark    = 30 * time.Minute
)

// TaiBwoRecord is the quest's identity. The levels are the content's own (Cooking 30 for the marinade, Fishing 5 for the net, the Agility
// 15 the journal lists) and the module's (Magic 43 for Superheat Item, Hitpoints 35); Jungle Potion is Timfraku's own gate ("Trufitus
// sent me" is offered at %junglepotion 12 and above).
var TaiBwoRecord = Record{ID: TaiBwoID, Name: TaiBwoName, QuestPoints: 2,
	Requirements: Requirements{
		Quests: []string{JunglePotionID},
		Skills: []SkillReq{{Skill: "cooking", Level: 30}, {Skill: "agility", Level: 15}, {Skill: "fishing", Level: 5},
			{Skill: "magic", Level: TbMinMagic}, {Skill: "hitpoints", Level: TbMinHitpoints}}}}

// tbTools are the quest's own items, which every bank visit of the quest leaves in the pack and the eat loop never touches: the coins,
// the tools, the runes, the bottles and the spears, and every item the legs make.
var tbTools = func() []int {
	out := []int{
		CoinsID, TbObjNet, TbObjPestle, ObjKnife, ObjSeaweed, ObjFireRune, ObjAirRune, ObjChaosRune, TbObjNatureRune,
		ObjBanana, ObjKaramjaRum, TbObjSlicedBanana, TbObjRumSlicedBanana,
		TbObjVessel, TbObjVesselLoaded, TbObjManual, TbObjRawKarambwanji, TbObjKarambwanjiPaste,
		TbObjRawKarambwan, TbObjPoorKarambwan, TbObjBurntKarambwan, TbObjPoisonPaste,
		TbObjMonkeyCorpse, TbObjMonkeySkin, TbObjSandwich,
		TbObjJogreBones, TbObjBurntJogreBones, TbObjBurntBonesInPaste, TbObjMarinatedBones,
	}
	for _, p := range TbPotions {
		out = append(out, p.ID)
	}
	for _, sp := range TbSpears {
		out = append(out, sp.ID, sp.PoisonID)
	}
	return out
}()

// TbKeepIDs is a bank visit's keep list: the tools and the food (a copy).
func TbKeepIDs() []int { return append(append([]int(nil), tbTools...), tbwtFoods...) }

// TbToolIDs is Module.ToolIDs (a copy).
func TbToolIDs() []int { return append([]int(nil), tbTools...) }

func tbwtWait(d time.Duration, reason string) Step {
	return Step{Kind: StepWait, Reason: reason, ParkFor: d}
}

// held is how many of the ids the pack holds in all.
func tbHeld(s Snapshot, ids ...int) int {
	n := 0
	for _, id := range ids {
		n += s.HeldID(id)
	}
	return n
}

// tbAway reports an account that is off the island (or whose position is not read): its next errand starts with the supplies.
func tbAway(s Snapshot) bool {
	return s.TileKnown && !OnKaramja(s.Tile) && !InTbwtCutscene(s.Tile) && !InJungleCave(s.Tile)
}

// tbNeed is one item a leg ahead consumes or uses.
type tbNeed struct {
	Line KitLine
	// Scarce says the bank and the mule may well hold none (the potions, the spear, the nature rune): the first pass asks for those alone,
	// so that an account which cannot be supplied does not carry a pack of tools around for nothing.
	Scarce bool
	// Why is the leg that wants it, for the park's reason.
	Why string
}

// tbPasteOrBeyond reports the Karambwanji paste made, or the marinade past it.
func tbPasteOrBeyond(s Snapshot, st TbwtState) bool {
	return st.Tinsay >= TbTinsayComplete || tbHeld(s, TbObjKarambwanjiPaste, TbObjBurntBonesInPaste, TbObjMarinatedBones) > 0
}

// tbBonesOrBeyond reports a Jogre's bones held in any of the forms the marinade makes of them.
func tbBonesOrBeyond(s Snapshot, st TbwtState) bool {
	return st.Tinsay >= TbTinsayComplete || tbHeld(s, TbObjJogreBones, TbObjBurntJogreBones, TbObjBurntBonesInPaste, TbObjMarinatedBones) > 0
}

// tbSpearReady reports the spear question settled: Tamayu has his, or the pack holds one of the forms it goes through.
func tbSpearReady(s Snapshot, st TbwtState) bool {
	return st.Tamayu >= TbTamayuComplete || st.Spear || TbPoisonedSpearHeld(s) > 0
}

// tbNeedsKarambwanji reports a leg ahead that wants raw Karambwanji from the lake.
func tbNeedsKarambwanji(s Snapshot, st TbwtState) bool {
	return st.Lubufu < TbLubufuDone ||
		(st.Tiadeche < TbTiadecheManual && s.HeldID(TbObjVesselLoaded) == 0) ||
		(!tbPasteOrBeyond(s, st) && st.Tinsay < TbTinsayComplete)
}

// tbwtNeeds is everything the legs still ahead use and the pack must hold: the tools, the runes, the bottles and the spear. A need
// drops off the list once its leg is behind the account, so a quest in its last leg never crosses back to a bank for a knife it used.
func tbwtNeeds(s Snapshot, st TbwtState) []tbNeed {
	var out []tbNeed
	add := func(line KitLine, scarce bool, why string) {
		out = append(out, tbNeed{Line: line, Scarce: scarce, Why: why})
	}
	if tbNeedsKarambwanji(s, st) {
		add(KitLine{ID: TbObjNet, Name: "Small fishing net", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: FcHarry, Price: 5, Note: "Harry's fishing shop in Catherby"}}}, false, "the Karambwanji of the Holy Lake")
	}
	if st.Tinsay < TbTinsayGivenRum && tbHeld(s, TbObjSlicedBanana, TbObjRumSlicedBanana) == 0 {
		add(KitLine{ID: ObjKnife, Name: "Knife", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: TbJiminuaShop, Price: 6, Note: "Jiminua's Jungle Store keeps 2, restocking in a hundred ticks"}}}, false, "slicing the banana")
	}
	if (!tbSpearReady(s, st) && s.HeldID(TbObjPoisonPaste) == 0) || !tbPasteOrBeyond(s, st) {
		add(KitLine{ID: TbObjPestle, Name: "Pestle and mortar", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: TbJiminuaShop, Price: 10, Note: "Jiminua's Jungle Store keeps 3, restocking in five hundred ticks"},
			{Kind: SourceShop, Shop: PlagueJatixShop, Price: 4, Note: "Jatix's Herblore Store in Taverley keeps 3"}}}, false, "grinding the Karambwan and the Karambwanji")
	}
	if st.Tinsay < TbTinsayGivenSandwich && tbHeld(s, TbObjSandwich) == 0 && tbHeld(s, TbObjMonkeySkin) == 0 {
		add(KitLine{ID: ObjSeaweed, Name: "Seaweed", Qty: 1}, false, "the sandwich")
	}
	if st.Tinsay < TbTinsayGivenSandwich && tbHeld(s, TbObjMonkeyCorpse, TbObjMonkeySkin, TbObjSandwich) == 0 {
		add(KitLine{ID: ObjAirRune, Name: "Air rune", Qty: 2 * TbCastCasts, Stackable: true,
			Sources: []Source{{Kind: SourceShop, Shop: BettyShop, Price: 4}}}, false, "the monkey (Wind Bolt)")
		add(KitLine{ID: ObjChaosRune, Name: "Chaos rune", Qty: TbCastCasts, Stackable: true,
			Sources: []Source{{Kind: SourceShop, Shop: BettyShop, Price: 15}}}, false, "the monkey (Wind Bolt)")
	}
	if !tbBonesBurnt(s, st) {
		add(KitLine{ID: ObjFireRune, Name: "Fire rune", Qty: 4, Stackable: true,
			Sources: []Source{{Kind: SourceShop, Shop: BettyShop, Price: 4}}}, false, "burning the Jogre bones (Superheat Item)")
		add(KitLine{ID: TbObjNatureRune, Name: "Nature rune", Qty: 1, Stackable: true}, true, "burning the Jogre bones (Superheat Item)")
	}
	if st.Tamayu < TbTamayuComplete && !st.Agility && TbDosesHeld(s) < TbAgilityDoses {
		add(KitLine{ID: 3032, Name: "Agility potion(4)", Qty: 1, Optional: true}, true, "Tamayu's four doses of agility potion")
		add(KitLine{ID: 3034, Name: "Agility potion(3)", Qty: 2, Optional: true, OnlyIfShort: 3032}, true, "Tamayu's four doses of agility potion")
	}
	if !tbSpearReady(s, st) && TbSpearHeld(s) == 0 && s.HeldID(TbObjPoisonPaste) == 0 {
		for _, sp := range TbSpears[:3] {
			add(KitLine{ID: sp.ID, Name: sp.Name, Qty: 1, Optional: true}, true, "a spear above bronze for Tamayu")
		}
	}
	return out
}

// tbBonesBurnt reports the bones burnt already (or the marinade past that), so the Superheat runes are not wanted.
func tbBonesBurnt(s Snapshot, st TbwtState) bool {
	return st.Tinsay >= TbTinsayComplete || tbHeld(s, TbObjBurntJogreBones, TbObjBurntBonesInPaste, TbObjMarinatedBones) > 0
}

// tbShort reports whether the pack holds less than the need asks, counting the alternatives of a potion or a spear as one need.
func tbShort(s Snapshot, n tbNeed) bool {
	switch n.Line.ID {
	case 3032, 3034:
		return TbDosesHeld(s) < TbAgilityDoses
	case 1239, 1241, 1243:
		return TbSpearHeld(s) == 0
	}
	return s.HeldID(n.Line.ID) < n.Line.Qty
}

// tbMissing is the names of the needs the pack does not meet, once each.
func tbMissing(s Snapshot, needs []tbNeed) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range needs {
		name := n.Line.Name
		switch n.Line.ID {
		case 3032, 3034:
			name = "4 doses of agility potion"
		case 1239, 1241, 1243:
			name = "a spear above bronze"
		}
		if tbShort(s, n) && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// tbSuppliesKit is a pass's lines: the scarce needs or the rest, with the coins and the food on the second.
func tbSuppliesKit(needs []tbNeed, scarce bool) []KitLine {
	var kit []KitLine
	for _, n := range needs {
		if n.Scarce == scarce {
			kit = append(kit, n.Line)
		}
	}
	if !scarce {
		kit = append(kit,
			KitLine{ID: CoinsID, Name: "Coins", Qty: TbCoinsQty, Min: TbCoinsMin, Stackable: true},
			KitLine{ID: ObjShark, Name: "Shark", Qty: TbSharks, Optional: true})
	}
	return kit
}

// tbwtPrepare is the pass before an errand: pack room, the scarce supplies, the rest, and the prices. It reports false when the account
// is ready. It runs for every errand, so a death (which empties the pack) or a lost tool sends the account to the bank again, at most
// once a run for each pass.
func tbwtPrepare(s Snapshot, st TbwtState) (Step, bool) {
	needs := tbwtNeeds(s, st)
	missing := tbMissing(s, needs)
	away := tbAway(s)
	food := FoodPoolHeal(s.InvIDs, tbwtFoods) > 0
	coinsLow := away && s.HeldID(CoinsID) < TbCoinsMin
	bank := cookSheepBank(s)
	// Room first, from the mainland: the lake's Karambwanji want a dozen slots of the pack, and the island has no bank to make them in.
	if away && s.FreeSlots < TbRoom && tbJunk(s) > 0 {
		return Step{Kind: StepDeposit, KeepIDs: TbKeepIDs(), Bank: bank, BankSet: true, Reason: "room for the Karambwanji and the quest's tools"}, true
	}
	if len(missing) == 0 && !coinsLow && (food || !away) {
		return tbwtPriced(s, st)
	}
	scarceMissing := false
	for _, n := range needs {
		if n.Scarce && tbShort(s, n) {
			scarceMissing = true
		}
	}
	ran := func(kit string) bool { return s.Ran["provision:"+kit] || s.Failed["provision:"+kit] }
	if scarceMissing && !ran(KitTbScarce) {
		return Step{Kind: StepProvision, KitName: KitTbScarce, Kit: tbSuppliesKit(needs, true), KeepIDs: TbKeepIDs(), Bank: bank, BankSet: true,
			Reason: "the agility potions, a spear and a nature rune, which the bank and the mule may not hold"}, true
	}
	if scarceMissing && ran(KitTbScarce) {
		return tbwtWait(TbSupplyPark, "not starting Tai Bwo Wannai Trio: the bank and the mule hold no "+strings.Join(tbMissing(s, scarceNeeds(needs)), ", ")+" (the fleet's Herblore is 1 and nobody holds a bottle)"), true
	}
	if !ran(KitTbTools) {
		return Step{Kind: StepProvision, KitName: KitTbTools, Kit: tbSuppliesKit(needs, false), KeepIDs: TbKeepIDs(), Bank: bank, BankSet: true,
			Reason: "the tools, the runes, coins for the ships and the rum, and food"}, true
	}
	if rest := tbMissing(s, needs); len(rest) > 0 {
		return tbwtWait(TbSupplyPark, "not going on with Tai Bwo Wannai Trio: the bank and the mule hold no "+strings.Join(rest, ", ")), true
	}
	if away && s.HeldID(CoinsID) < TbCoinsMin {
		return tbwtWait(TbSupplyPark, fmt.Sprintf("no %d coins for the ships and the rum from the bank or the mule (the pack holds %d): Karamja has no bank", TbCoinsMin, s.HeldID(CoinsID))), true
	}
	return tbwtPriced(s, st)
}

// tbJunk is how many kinds of item the pack holds that a bank visit would deposit (it keeps TbKeepIDs): zero means a deposit frees
// nothing, and asking for one again and again would only wear the watchdog down.
func tbJunk(s Snapshot) int {
	keep := map[int]bool{}
	for _, id := range TbKeepIDs() {
		keep[id] = true
	}
	n := 0
	for id, c := range s.InvIDs {
		if c > 0 && !keep[id] {
			n++
		}
	}
	return n
}

func scarceNeeds(needs []tbNeed) []tbNeed {
	var out []tbNeed
	for _, n := range needs {
		if n.Scarce {
			out = append(out, n)
		}
	}
	return out
}

// tbwtPriced is the models' half of the pass: the Jogre while the bones are still to be had, and the Shaikahan's ground while Tiadeche
// has a visit still to come. It reports false when both are fine.
func tbwtPriced(s Snapshot, st TbwtState) (Step, bool) {
	if !tbBonesOrBeyond(s, st) {
		if odds := TbwtJogreOdds(s); !odds.OK {
			return tbwtWait(TbOddsPark, "not going on with Tai Bwo Wannai Trio: its one fight is a Jogre for the bones, and "+odds.Why), true
		}
	}
	if st.Tiadeche < TbTiadecheDone {
		if ok, why := TbwtShaikahanOdds(s); !ok {
			return tbwtWait(TbOddsPark, "not going on with Tai Bwo Wannai Trio: "+why), true
		}
	}
	return Step{}, false
}

// tbwtGates are the account's levels: nothing here can be mended by a bank. An unread level is not judged.
func tbwtGates(s Snapshot) (Step, bool) {
	for _, g := range TaiBwoRecord.Requirements.Skills {
		if lvl, read := s.Levels[g.Skill]; read && lvl < g.Level {
			return tbwtWait(TbStatsPark, fmt.Sprintf("Tai Bwo Wannai Trio wants %s %d (the account has %d)", g.Skill, g.Level, lvl)), true
		}
	}
	return Step{}, false
}

// tbwtErrand is a custom step after the pass; one that failed on its last run parks the quest instead of being asked again at once.
func tbwtErrand(s Snapshot, st TbwtState, custom string, qty int, reason string) Step {
	if s.Failed[custom] {
		return tbwtWait(TbLegPark, fmt.Sprintf("the %s errand could not be done (a walk round the hunters, or a talk that would not take): %s", custom, reason))
	}
	if step, ok := tbwtPrepare(s, st); ok {
		return step
	}
	return Step{Kind: StepCustom, Custom: custom, Qty: qty, Reason: reason}
}

// DecideTaiBwoTrio is the whole quest, read from the journal's state, the pack and where the account stands.
func DecideTaiBwoTrio(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if step, ok := tbwtGates(s); ok {
		return step
	}
	if s.Journal == StatusNotStarted {
		return tbwtErrand(s, TbwtState{Phase: TbPhasePre}, CustomTbStart, 0, "ask Timfraku for the job")
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	st := TbwtStateOf(s.Stage)
	if s.TileKnown && InTbwtCutscene(s.Tile) {
		return Step{Kind: StepWait, Reason: "watching Tamayu hunt the Shaikahan"}
	}
	switch st.Phase {
	case TbPhaseComplete:
		return Step{Kind: StepWait, Reason: "the quest tab has not turned green yet"}
	case TbPhasePre:
		return tbwtErrand(s, st, CustomTbStart, 0, "ask Timfraku for the job")
	case TbPhaseBrothers:
		return tbwtErrand(s, st, CustomTbReport, 0, "tell Timfraku his sons are home")
	case TbPhaseStarted:
		return tbwtLegs(s, st)
	}
	return Step{Kind: StepWait, Reason: "unrecognised Tai Bwo Wannai Trio stage"}
}

func tbwtLegs(s Snapshot, st TbwtState) Step {
	switch {
	case st.Lubufu < TbLubufuDone:
		return tbwtLubufu(s, st)
	case st.Tiadeche < TbTiadecheManual:
		return tbwtCatch(s, st)
	case st.Tamayu < TbTamayuComplete:
		return tbwtTamayu(s, st)
	case st.Tinsay < TbTinsayComplete:
		return tbwtTinsay(s, st)
	}
	return tbwtManual(s, st)
}

// tbDropExcess is the step that drops a Karambwanji beyond the ones the pack keeps, once Lubufu has his twenty.
func tbDropExcess(s Snapshot) (Step, bool) {
	if s.HeldID(TbObjRawKarambwanji) > TbKeepKarambwanji {
		return Step{Kind: StepOpHeld, ItemID: TbObjRawKarambwanji, Item: "Raw karambwanji", Op: "Drop", Reason: "more Karambwanji than the quest has a use for"}, true
	}
	return Step{}, false
}

func tbwtLubufu(s Snapshot, st TbwtState) Step {
	switch st.Lubufu {
	case TbLubufuNone, TbLubufuMet, TbLubufuOffered:
		return tbwtErrand(s, st, CustomTbLubufuMeet, 0, "introduce the account to Lubufu and offer to collect his bait")
	case TbLubufuBait:
		held := s.HeldID(TbObjRawKarambwanji)
		if held >= TbKarambwanjiWanted || (held > 0 && s.FreeSlots <= TbFishReserve) {
			return tbwtErrand(s, st, CustomTbLubufuGive, 0, fmt.Sprintf("hand Lubufu %d of the %d Karambwanji he wants", held, TbKarambwanjiWanted))
		}
		room := s.FreeSlots - TbFishReserve
		if room < 1 {
			return tbwtWait(TbLegPark, "no room in the pack for the Karambwanji Lubufu wants")
		}
		want := min(held+room, TbKarambwanjiWanted+TbKeepKarambwanji)
		return tbwtErrand(s, st, CustomTbFish, want, fmt.Sprintf("net Karambwanji at the Holy Lake (%d of %d)", held, want))
	}
	if step, ok := tbDropExcess(s); ok {
		return step
	}
	return tbwtErrand(s, st, CustomTbLubufuAsk, 0, "answer Lubufu's questions until he offers the apprenticeship and the vessel")
}

// tbVessel is the vessel the pack holds, loaded or not (0 for none).
func tbVessel(s Snapshot) int {
	if s.HeldID(TbObjVesselLoaded) > 0 {
		return TbObjVesselLoaded
	}
	if s.HeldID(TbObjVessel) > 0 {
		return TbObjVessel
	}
	return 0
}

// tbwtBeast is the Shaikahan's gate for an errand at Tiadeche.
func tbwtBeast(s Snapshot, st TbwtState, custom string, qty int, reason string) Step {
	if ok, why := TbwtShaikahanOdds(s); !ok {
		return tbwtWait(TbOddsPark, "not going to Tiadeche: "+why)
	}
	return tbwtErrand(s, st, custom, qty, reason)
}

func tbwtCatch(s Snapshot, st TbwtState) Step {
	switch st.Tiadeche {
	case TbTiadecheNone, TbTiadecheIntro, TbTiadecheCaught:
		return tbwtBeast(s, st, CustomTbTiadecheMeet, 0, "talk to Tiadeche")
	}
	if s.HeldID(TbObjVesselLoaded) > 0 {
		return tbwtBeast(s, st, CustomTbTiadecheCatch, 0, "hand Tiadeche the baited vessel and take his first catch")
	}
	if s.HeldID(TbObjVessel) > 0 {
		if s.HeldID(TbObjRawKarambwanji) > 0 {
			return tbwtErrand(s, st, CustomTbBait, 0, "bait the vessel with a Karambwanji")
		}
		return tbwtErrand(s, st, CustomTbFish, 1, "net a Karambwanji for the vessel's bait")
	}
	return tbwtErrand(s, st, CustomTbVessel, 0, "ask Lubufu for a Karambwan vessel")
}

func tbwtTamayu(s Snapshot, st TbwtState) Step {
	switch st.Tamayu {
	case TbTamayuNone, TbTamayuIntro:
		return tbwtErrand(s, st, CustomTbTamayuMeet, 0, "bring Tamayu the news")
	case TbTamayuSlay:
		return tbwtErrand(s, st, CustomTbTamayuHunt, 0, "watch Tamayu's hunt")
	}
	if !st.Agility {
		p, ok := TbPotionHeld(s)
		if !ok {
			return tbwtWait(TbSupplyPark, fmt.Sprintf("Tamayu is short of his four doses of agility potion and the pack holds %d", TbDosesHeld(s)))
		}
		return tbwtErrand(s, st, CustomTbTamayuGive, p.ID, "give Tamayu the "+p.Name)
	}
	if !st.Spear {
		return tbwtSpear(s, st)
	}
	return tbwtErrand(s, st, CustomTbTamayuHunt, 1, "follow Tamayu on the killing hunt")
}

// tbwtSpear makes the spear Tamayu wants: a Karambwan, cooked on the jungle fire, ground into the poison, smeared on the spear.
func tbwtSpear(s Snapshot, st TbwtState) Step {
	if kp := TbPoisonedSpearHeld(s); kp > 0 {
		return tbwtErrand(s, st, CustomTbTamayuGive, kp, "give Tamayu the Karambwan-poisoned spear")
	}
	if s.HeldID(TbObjBurntKarambwan) > 0 {
		return Step{Kind: StepOpHeld, ItemID: TbObjBurntKarambwan, Item: "Burnt karambwan", Op: "Drop", Reason: "the Karambwan burnt"}
	}
	if s.HeldID(TbObjPoisonPaste) > 0 {
		sp := TbSpearHeld(s)
		if sp == 0 {
			return tbwtWait(TbSupplyPark, "the Karambwan paste is made and the pack holds no spear above bronze to smear it on")
		}
		return tbwtErrand(s, st, CustomTbPoison, sp, "smear the poisonous Karambwan paste over the spear")
	}
	if s.HeldID(TbObjPoorKarambwan) > 0 {
		return tbwtErrand(s, st, CustomTbGrind, TbObjPoorKarambwan, "grind the poorly cooked Karambwan into poison")
	}
	if s.HeldID(TbObjRawKarambwan) > 0 {
		return tbwtErrand(s, st, CustomTbCook, TbObjRawKarambwan, "cook the Karambwan on the jungle fire")
	}
	// No Karambwan in the chain: Tiadeche's second gift (the bit his first "Yes" sets is the only thing between the pack and it), then
	// the vessel at Fishing 65, and nothing else.
	if !s.Failed[CustomTbTiadecheGift] && !s.Ran[CustomTbTiadecheGift] {
		return tbwtBeast(s, st, CustomTbTiadecheGift, 0, "ask Tiadeche for another raw Karambwan")
	}
	if s.Level("fishing") >= TbMinFishingForKarambwan {
		switch {
		case s.HeldID(TbObjVesselLoaded) == 0 && s.HeldID(TbObjVessel) == 0:
			return tbwtErrand(s, st, CustomTbVessel, 0, "ask Lubufu for a Karambwan vessel to fish a Karambwan with")
		case s.HeldID(TbObjRawKarambwanji) == 0 && s.HeldID(TbObjVesselLoaded) == 0:
			return tbwtErrand(s, st, CustomTbFish, 2, "net a Karambwanji for the vessel's bait")
		}
		return tbwtErrand(s, st, CustomTbKarambwan, 0, "lower the vessel for a Karambwan")
	}
	return tbwtWait(TbSupplyPark, fmt.Sprintf("the Karambwan the poison is made of burnt, Tiadeche has no second one for the account and Fishing %d is under the %d the vessel wants",
		s.Level("fishing"), TbMinFishingForKarambwan))
}

func tbwtTinsay(s Snapshot, st TbwtState) Step {
	switch st.Tinsay {
	case TbTinsayNone, TbTinsayIntro:
		return tbwtErrand(s, st, CustomTbTinsayMeet, 0, "bring Tinsay the news")
	case TbTinsayGivenRum, TbTinsayGivenSandwich:
		return tbwtErrand(s, st, CustomTbTinsayMeet, 0, "ask Tinsay what he needs next")
	case TbTinsayRum:
		return tbwtRum(s, st)
	case TbTinsaySandwich:
		return tbwtSandwich(s, st)
	}
	return tbwtBones(s, st)
}

func tbwtRum(s Snapshot, st TbwtState) Step {
	switch {
	case s.HeldID(TbObjRumSlicedBanana) > 0:
		return tbwtErrand(s, st, CustomTbTinsayGive, TbObjRumSlicedBanana, "give Tinsay the sliced banana in Karamjan rum")
	case s.HeldID(TbObjSlicedBanana) > 0:
		if s.HeldID(ObjKaramjaRum) > 0 {
			return tbwtErrand(s, st, CustomTbMix, 0, "drop the banana slices into the rum")
		}
		return tbwtErrand(s, st, CustomTbRum, 0, "buy Karamjan rum from Zambo")
	case s.HeldID(ObjBanana) > 0:
		return tbwtErrand(s, st, CustomTbSlice, 0, "slice the banana")
	}
	return tbwtErrand(s, st, CustomTbBanana, 0, "pick a banana in the plantation")
}

func tbwtSandwich(s Snapshot, st TbwtState) Step {
	switch {
	case s.HeldID(TbObjSandwich) > 0:
		return tbwtErrand(s, st, CustomTbTinsayGive, TbObjSandwich, "give Tinsay the seaweed sandwich")
	case s.HeldID(TbObjMonkeySkin) > 0:
		return tbwtErrand(s, st, CustomTbSandwich, 0, "sandwich the seaweed into the monkey skin")
	case s.HeldID(TbObjMonkeyCorpse) > 0:
		return tbwtErrand(s, st, CustomTbSkin, 0, "ask Tamayu to skin the monkey")
	}
	return tbwtErrand(s, st, CustomTbMonkey, 0, "cast a Wind Bolt at a monkey for its corpse")
}

func tbwtBones(s Snapshot, st TbwtState) Step {
	switch {
	case s.HeldID(TbObjMarinatedBones) > 0:
		return tbwtErrand(s, st, CustomTbTinsayGive, TbObjMarinatedBones, "give Tinsay the marinated Jogre bones")
	case s.HeldID(TbObjBurntBonesInPaste) > 0:
		return tbwtErrand(s, st, CustomTbCook, TbObjBurntBonesInPaste, "marinate the bones on the jungle fire")
	case s.HeldID(TbObjBurntJogreBones) > 0:
		switch {
		case s.HeldID(TbObjKarambwanjiPaste) > 0:
			return tbwtErrand(s, st, CustomTbPasteBones, 0, "smother the bones in Karambwanji paste")
		case s.HeldID(TbObjRawKarambwanji) > 0:
			return tbwtErrand(s, st, CustomTbGrind, TbObjRawKarambwanji, "grind a Karambwanji into paste")
		}
		return tbwtErrand(s, st, CustomTbFish, 1, "net a Karambwanji for the marinade")
	case s.HeldID(TbObjJogreBones) > 0:
		return tbwtErrand(s, st, CustomTbBurn, 0, "burn the Jogre bones with Superheat Item")
	}
	return tbwtErrand(s, st, CustomTbJogre, 0, "kill a Jogre for its bones")
}

// tbwtManual is the end of the brothers: the second vessel to Tinsay for the manual, the manual to Tiadeche.
func tbwtManual(s Snapshot, st TbwtState) Step {
	if s.HeldID(TbObjManual) > 0 {
		return tbwtBeast(s, st, CustomTbManual, 0, "hand Tiadeche the crafting manual")
	}
	if st.Tiadeche >= TbTiadecheDone && !s.Ran[CustomTbTiadecheMeet] && !s.Failed[CustomTbTiadecheMeet] {
		// Stage 5 and the stages past it read the same page; talking to Tiadeche moves the quest on at 6 and says nothing at 5.
		return tbwtBeast(s, st, CustomTbTiadecheMeet, 0, "talk to Tiadeche: his manual may be handed over already")
	}
	if v := tbVessel(s); v != 0 {
		return tbwtErrand(s, st, CustomTbTinsayGive, v, "take a vessel to Tinsay for Tiadeche's crafting instructions")
	}
	return tbwtErrand(s, st, CustomTbVessel, 0, "ask Lubufu for a second Karambwan vessel")
}

// TbStageName names a stage for the report.
func TbStageName(n int) string { return TbwtStateOf(n).String() }

// TaiBwoTrio is the engine module. It never fights back (NoRetaliate): the hunters are walked round, the Shaikahan is priced and the
// Jogre is fought by an explicit attack. One death anywhere parks it for a day: the pack is gone and the price was wrong for the account.
var TaiBwoTrio = Module{
	Record: TaiBwoRecord, Bank: DraynorBank, BankSet: true, Anchor: TbTamayuAnchor,
	ToolIDs:     tbTools,
	ReadStage:   ParseTaiBwoJournal,
	Sustain:     tbwtSustain,
	NoRetaliate: true,
	MaxDeaths:   1,
	Decide:      DecideTaiBwoTrio,
}
