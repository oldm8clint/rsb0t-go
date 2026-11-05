package questplan

import (
	"fmt"
	"strings"
	"time"
)

// The Watch Tower's decision: the order of the legs, what each needs in the pack, which level and which price gate it, and when the quest parks
// (watchtower.go has the story, the stages and the ground).
//
// The shape is Tai Bwo Wannai Trio's: every errand is a custom step behind a preparation (room in the pack, the leg's kit from the bank, the mule or a
// shop, food, the models' prices), a custom that failed on its last run parks the quest for half an hour, and a level the account lacks parks it for
// twelve hours with the reason. The pockets (the tower's upper floors, the island, the camp, the lower city, the City guard's side of the chasm, the
// ground behind the east gate, the caves, the enclave, the mirror tower) are registered by questmods with their own ways out, and every walk of the
// engine leaves the pocket it starts in before it goes anywhere outside it, so the decision names no escape; it reads the area only where the next step
// begins inside one (the tower's floors, the island, the camp, the City guard's side, the enclave).
//
// The owner's rule is "build up to the first step an account cannot do safely and park there with the reason". What decides each step's gate is
// therefore the step: Decide never looks ahead.

// WtWizardStop is a conversation with the wizard on his floor (any of the four: he wanders), the options his menus offer for the stage.
func WtWizardStop(prefer ...string) NpcStop {
	return NpcStop{Npc: "Watchtower wizard", ID: WtNpcWizard, Anchor: WtWizardAnchor, Leash: 12, Prefer: prefer}
}

// The stops. The talks with no menu carry a placeholder preference (a talk step must name one).
var (
	// WtStartPrefer is the three menus of the first conversation, in order of appearance.
	WtStartPrefer = []string{"What's the matter?", "So how come the spell doesn't work?", "Can I be of help?"}
	// WtNailsPrefer is the fingernails' two menus.
	WtNailsPrefer = []string{"What do you suggest I do?", "So what do I do?"}
	// WtOgStop is Og's talk: the first asks "I seek entrance to the city of ogres." (the other line, "I have come to kill you.", calls his guards), the later
	// ones have no menu except "I have lost the key!".
	WtOgStop = NpcStop{Npc: "Og", ID: WtNpcOg, Anchor: WtOgAnchor, Leash: 10, Prefer: []string{"I seek entrance to the city of ogres.", "I have lost the key!"}}
	// WtGrewStop is Grew's first talk: "Don't eat me; I can help you." ("You will have to kill me first." sets his guards on the account).
	WtGrewStop = NpcStop{Npc: "Grew", ID: WtNpcGrew, Anchor: WtGrewAnchor, Leash: 10, Prefer: []string{"Don't eat me; I can help you."}}
	// WtGrewLostStop is a helped Grew's menu, for the crystal an account lost.
	WtGrewLostStop = NpcStop{Npc: "Grew", ID: WtNpcGrew, Anchor: WtGrewAnchor, Leash: 10, Prefer: []string{"I've lost the crystal you gave me."}}
	// WtTobanStop is Toban's talk: "I seek entrance to the city of ogres." then "I could do something for you..." ("Die, creature!" costs five hitpoints).
	WtTobanStop = NpcStop{Npc: "Toban", ID: WtNpcToban, Anchor: WtTobanAnchor, Leash: 10,
		Prefer: []string{"I seek entrance to the city of ogres.", "I could do something for you..."}}
	// The lost-piece menus of the three tribes once they are helped (og.rs2, grew.rs2, toban.rs2): the same words in three spellings.
	WtOgLostPartStop    = NpcStop{Npc: "Og", ID: WtNpcOg, Anchor: WtOgAnchor, Leash: 10, Prefer: []string{"I have lost the relic part you gave me."}}
	WtGrewLostPartStop  = NpcStop{Npc: "Grew", ID: WtNpcGrew, Anchor: WtGrewAnchor, Leash: 10, Prefer: []string{"I've lost the relic part you gave me."}}
	WtTobanLostPartStop = NpcStop{Npc: "Toban", ID: WtNpcToban, Anchor: WtTobanAnchor, Leash: 10, Prefer: []string{"I can't find the relic part you gave me."}}
	// WtCityGuardRiddleStop is the City guard's first conversation: "I seek passage into the skavid caves." ("I am an ogre killer..." sets him on the account).
	WtCityGuardRiddleStop = NpcStop{Npc: "City guard", ID: WtNpcCityGuard, Anchor: WtCityGuardAnchor, Leash: 10,
		Prefer: []string{"I seek passage into the skavid caves."}}
	// WtCityGuardMapStop is his menu once the riddle is solved, for the map an account lost.
	WtCityGuardMapStop = NpcStop{Npc: "City guard", ID: WtNpcCityGuard, Anchor: WtCityGuardAnchor, Leash: 10,
		Prefer: []string{"I have lost the map you gave me."}}
)

// wtTools are the quest's own items, which every bank visit of the quest leaves in the pack and the eat loop never touches: the pieces of the relic,
// the key, the gold, the bones, the crystals, the map, the light, the berries, the bottles and everything the legs make.
var wtTools = []int{
	WtObjCoins, WtObjRope, WtObjLitCandle, WtObjLitBlackCandle, WtObjLitTorch, WtObjVialWater, WtObjJangerberries, WtObjGuamLeaf, WtObjGuamVial,
	WtObjPestle, WtObjPickaxe, WtObjBatBones, WtObjDragonBones, WtObjDeathRune, WtObjGoldBar, WtObjRelic, WtObjPart1, WtObjPart2, WtObjPart3, WtObjMap,
	WtObjTooth, WtObjTobanKey, WtObjRockCake, WtObjCrystal1, WtObjCrystal2, WtObjCrystal3, WtObjCrystal4, WtObjFingernails, WtObjJangerVial,
	WtObjGuamJangerVial, WtObjGroundBones, WtObjStolenGold, WtObjOgrePotion, WtObjMagicPotion, WtObjSpellScroll, WtObjShamanRobe, WtObjNightshade,
}

// WtKeepIDs is a bank visit's keep list: the tools and the food (a copy).
func WtKeepIDs() []int { return append(append([]int(nil), wtTools...), WtFoodIDs...) }

// WtToolIDs is Module.ToolIDs (a copy).
func WtToolIDs() []int { return append([]int(nil), wtTools...) }

func wtPark(d time.Duration, reason string) Step {
	return Step{Kind: StepWait, Reason: reason, ParkFor: d}
}

// wtHuntedPocket reports the pockets the hunters reach into: an account that stops moving in one is hit until it moves, so none is a place to park.
func wtHuntedPocket(a WtArea) bool {
	switch a {
	case WtAreaIsland, WtAreaCamp, WtAreaLowerCity, WtAreaCityGuard, WtAreaEastInside, WtAreaEnclave:
		return true
	}
	return false
}

// wtLeavePocket is the step that takes an account out of a hunted pocket: the enclave by its own walk (with the eater of the dragons' line), the others by the
// walk to the bank, which leaves the pocket by its way out first. false when the account is not in one, or when the enclave's way out has just failed (there
// is no better step then, and a park is what is left).
func wtLeavePocket(s Snapshot, why string) (Step, bool) {
	if !s.TileKnown {
		return Step{}, false
	}
	a := WtAreaOf(s.Tile)
	if !wtHuntedPocket(a) {
		return Step{}, false
	}
	if a == WtAreaEnclave {
		if s.Failed[CustomWtEnclaveOut] {
			return Step{}, false
		}
		return Step{Kind: StepCustom, Custom: CustomWtEnclaveOut, Reason: "out of the shamans' enclave before anything else: " + why}, true
	}
	return Step{Kind: StepWalk, Anchor: WtBank, Radius: 6, Reason: fmt.Sprintf("out of %s before anything else: %s", a, why)}, true
}

// wtParkSafe parks the quest, or, when the account stands in a hunted pocket, walks it out first and lets the same decision park it outside.
func wtParkSafe(s Snapshot, d time.Duration, reason string) Step {
	if step, ok := wtLeavePocket(s, reason); ok {
		return step
	}
	return wtPark(d, reason)
}

func wtHeld(s Snapshot, ids ...int) int {
	n := 0
	for _, id := range ids {
		n += s.HeldID(id)
	}
	return n
}

// wtLit is how many lit light sources the pack holds.
func wtLit(s Snapshot) int { return wtHeld(s, WtObjLitCandle, WtObjLitBlackCandle, WtObjLitTorch) }

// wtGate parks the quest for a level the account lacks (an unread level is not judged). what names the step that wants it.
func wtGate(s Snapshot, skill string, level int, what string) (Step, bool) {
	if have, read := s.Levels[skill]; read && have < level {
		return wtParkSafe(s, WtStatsPark, fmt.Sprintf("the Watch Tower is waiting at %s, which wants %s %d (the account has %d; no rung trains it)", what, skill, level, have)), true
	}
	return Step{}, false
}

// ---------------------------------------------------------------------------------------------------------------------
// Kits
// ---------------------------------------------------------------------------------------------------------------------

func wtFoodLine() KitLine {
	return KitLine{ID: WtObjShark, Name: "Shark", Qty: WtFoodQty, Optional: true}
}

func wtCoinsLine() KitLine {
	return KitLine{ID: WtObjCoins, Name: "Coins", Qty: WtPurse, Min: WtPurseMin, Stackable: true}
}

func wtRopeLine() KitLine {
	return KitLine{ID: WtObjRope, Name: "Rope", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: AemadShop, Price: 20, Note: "Aemad's Adventurers' Supplies keeps 20, restocking in a hundred ticks"}}}
}

func wtVialLine() KitLine {
	return KitLine{ID: WtObjVialWater, Name: "Vial of water", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: AemadShop, Price: 5, Note: "Aemad keeps 500"}}}
}

func wtPickaxeLine() KitLine {
	return KitLine{ID: WtObjPickaxe, Name: "Bronze pickaxe", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: AemadShop, Price: 5, Note: "Aemad keeps 2, restocking in a hundred ticks"}}}
}

func wtPestleLine() KitLine {
	return KitLine{ID: WtObjPestle, Name: "Pestle and mortar", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: PlagueJatixShop, Price: 4, Note: "Jatix's Herblore Store in Taverley keeps 3"}}}
}

func wtDeathRuneLine() KitLine {
	return KitLine{ID: WtObjDeathRune, Name: "Death rune", Qty: 1, Stackable: true, Sources: []Source{
		{Kind: SourceShop, Shop: BettyShop, Price: 40, Note: "Betty's Magic Emporium keeps 1000"}}}
}

func wtCandleLine() KitLine {
	return KitLine{ID: WtObjLitCandle, Name: "Lit candle", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: WtCandle, PerSpawn: 1, RespawnTicks: 100, Note: "the candle on the ground beside the Watchtower, which respawns lit"}}}
}

// wtScarceLine is an item the bank and the mule may not hold and nobody sells (the bones, the guam leaf): no sources, so a round with none is BLOCKED.
func wtScarceLine(id int, name string) KitLine { return KitLine{ID: id, Name: name, Qty: 1} }

// wtQuestLine is a quest item drawn from the bank (untradeable: never asked of the mule).
func wtQuestLine(id int, name string) KitLine {
	return KitLine{ID: id, Name: name, Qty: 1, Untradeable: true}
}

// wtMissing is the names of the non-optional lines the pack does not meet.
func wtMissing(s Snapshot, lines []KitLine) []string {
	var out []string
	for _, l := range lines {
		if !l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			out = append(out, l.Name)
		}
	}
	return out
}

// wtStock is the preparation's first half: the kit's lines, asked of the bank, the mule, the ground and the shops. It reports false when the pack holds
// them. A round that came back short (nothing in the bank or the mule for a line with no shop) parks the quest with the names. A round that SUCCEEDED
// and left a line short is a line the legs used up (the rope of each swing to Grew's island): the kit is asked again, and a round that gives nothing
// new is the watchdog's.
func wtStock(s Snapshot, kitName string, lines []KitLine, reason string) (Step, bool) {
	missing := wtMissing(s, lines)
	if len(missing) == 0 {
		return Step{}, false
	}
	key := "provision:" + kitName
	if s.Blocked[key] || s.Failed[key] {
		return wtParkSafe(s, WtSupplyPark, fmt.Sprintf("not going on with the Watch Tower: the bank, the mule and the shops give no %s (%s)", strings.Join(missing, ", "), reason)), true
	}
	return Step{Kind: StepProvision, KitName: kitName, Kit: lines, KeepIDs: WtKeepIDs(), Bank: WtBank, BankSet: true, Reason: reason}, true
}

// wtFood is the preparation's food: ten sharks, optional, asked once a run when the pack holds less than a hundred hitpoints of food.
func wtFood(s Snapshot) (Step, bool) {
	if FoodPoolHeal(s.InvIDs, WtFoodIDs) >= 100 || s.Ran["provision:"+KitWtFood] || s.Failed["provision:"+KitWtFood] {
		return Step{}, false
	}
	return Step{Kind: StepProvision, KitName: KitWtFood, Kit: []KitLine{wtFoodLine()}, KeepIDs: WtKeepIDs(), Bank: WtBank, BankSet: true,
		Reason: "food for the ogres' country"}, true
}

// wtJunk is how many kinds of item the pack holds that a bank visit would deposit: zero means a deposit frees nothing.
func wtJunk(s Snapshot) int {
	keep := map[int]bool{}
	for _, id := range WtKeepIDs() {
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

// wtLeg is one errand: a custom step, what it needs and what it is priced against.
type wtLeg struct {
	Custom string
	Qty    int
	Reason string
	// KitName and Kit are the lines the leg needs in the pack (the bank, the mule, the ground and the shops are asked once a run).
	KitName string
	Kit     []KitLine
	// Room is the free pack slots the leg wants before it begins.
	Room int
	// Food says the leg is through ogre country: food in the pack, and the prices below.
	Food bool
	// Odds are the models' prices; the first that refuses parks the quest.
	Odds []func(Snapshot) (bool, string)
	// Sites are the ogre sites the leg's trip passes through, priced with the maps' hunter counts (WtTripOdds). A leg with sites is also topped up before it
	// goes (the price counts on it).
	Sites []WtSite
	// Gorad prices the fight with Gorad (WtGoradOdds) on the leg, whose sites cover the rest of the trip.
	Gorad bool
	// Visit is a stay in the shamans' enclave, priced by WtEnclaveOdds (and parked on for WtEnclavePark: a nest of dragons does not get easier).
	Visit *WtVisit
	// TopUpPeak is the hardest hit the leg's hunters can land, for the top-up (FightStartHP); 0 takes the ogre's.
	TopUpPeak int
}

// wtOddsOf adapts a fight chain to the price form.
func wtOddsOf(f func(Snapshot) FightOdds) func(Snapshot) (bool, string) {
	return func(s Snapshot) (bool, string) {
		o := f(s)
		return o.OK, o.Why
	}
}

// The prices a leg is gated on. wtOgres is the pair of ogres the road is priced at in any case; the sites add the counts of the place.
var wtOgres = wtOddsOf(WtOgreOdds)

func wtChiefs(ticks int) func(Snapshot) (bool, string) {
	return func(s Snapshot) (bool, string) { return WtChieftainOdds(s, ticks) }
}

// wtFoodNames are the foods' display names, for a top-up bite.
var wtFoodNames = map[int]string{385: "Shark", 373: "Swordfish", 379: "Lobster", 365: "Bass", 361: "Tuna", 329: "Salmon", 333: "Trout", 2309: "Bread",
	1891: "Cake", 1893: "2/3 cake", 1895: "Slice of cake"}

// wtTopUp is a bite when the account is under the hitpoints a fight starts at (FightStartHP), which is what every price of a leg through hunters counts on:
// the largest bite that does not overshoot, else the smallest there is. peak is the hardest hit of the leg's hunters.
func wtTopUp(s Snapshot, peak int) (Step, bool) {
	if s.MaxHP <= 0 || s.HP <= 0 || s.HP >= FightStartHP(s.MaxHP, peak) {
		return Step{}, false
	}
	missing := s.MaxHP - s.HP
	best, bestHeal := 0, 0
	small, smallHeal := 0, 0
	for _, id := range WtFoodIDs {
		heal := FoodBiteHeal(id)
		if s.HeldID(id) == 0 || heal == 0 {
			continue
		}
		if heal <= missing && heal > bestHeal {
			best, bestHeal = id, heal
		}
		if small == 0 || heal < smallHeal {
			small, smallHeal = id, heal
		}
	}
	id := best
	if id == 0 {
		// No bite fits in what is missing: one that overshoots is worth its food only when the account is well down.
		if s.HP*10 >= s.MaxHP*6 {
			return Step{}, false
		}
		id = small
	}
	if id == 0 {
		return Step{}, false
	}
	name := wtFoodNames[id]
	if name == "" {
		name = fmt.Sprintf("food %d", id)
	}
	return Step{Kind: StepOpHeld, ItemID: id, Item: name, Op: "Eat", Reason: "top up before the hunters' country"}, true
}

// wtPrepare is the preparation of a leg: it returns the step that makes the account ready (a deposit, a provisioning round, a park, a bite), or false when
// it is.
func wtPrepare(s Snapshot, leg wtLeg) (Step, bool) {
	if leg.Custom != "" && s.Failed[leg.Custom] {
		return wtParkSafe(s, WtLegPark, fmt.Sprintf("the %s leg could not be done (%s)", leg.Custom, leg.Reason)), true
	}
	if leg.Room > 0 && s.FreeSlots < leg.Room && wtJunk(s) > 0 {
		return Step{Kind: StepDeposit, KeepIDs: WtKeepIDs(), Bank: WtBank, BankSet: true, Reason: "room in the pack for " + leg.Reason}, true
	}
	if len(leg.Kit) > 0 {
		if step, ok := wtStock(s, leg.KitName, leg.Kit, leg.Reason); ok {
			return step, true
		}
	}
	hunted := len(leg.Odds) > 0 || len(leg.Sites) > 0 || leg.Gorad || leg.Visit != nil
	if leg.Food {
		if step, ok := wtFood(s); ok {
			return step, true
		}
		if FoodPoolHeal(s.InvIDs, WtFoodIDs) == 0 && hunted {
			return wtParkSafe(s, WtSupplyPark, "not going on with the Watch Tower: the bank and the mule give no food for the ogres' country ("+leg.Reason+")"), true
		}
	}
	for _, price := range leg.Odds {
		if ok, why := price(s); !ok {
			return wtParkSafe(s, WtOddsPark, "not going on with the Watch Tower: "+why), true
		}
	}
	if len(leg.Sites) > 0 {
		if ok, why := WtTripOdds(s, leg.Sites...); !ok {
			return wtParkSafe(s, WtOddsPark, "not going on with the Watch Tower: "+why), true
		}
	}
	if leg.Gorad {
		if o := WtGoradOdds(s); !o.OK {
			return wtParkSafe(s, WtOddsPark, "not going on with the Watch Tower: "+o.Why), true
		}
	}
	if leg.Visit != nil {
		if ok, why := WtEnclaveOdds(s, *leg.Visit); !ok {
			return wtParkSafe(s, WtEnclavePark, "not going on with the Watch Tower: "+why), true
		}
	}
	if hunted {
		peak := leg.TopUpPeak
		if peak <= 0 {
			peak = FoeOgre.PeakHit()
		}
		if step, ok := wtTopUp(s, peak); ok {
			return step, true
		}
	}
	return Step{}, false
}

// wtGo is a custom step after the preparation.
func wtGo(s Snapshot, leg wtLeg) Step {
	if step, ok := wtPrepare(s, leg); ok {
		return step
	}
	return Step{Kind: StepCustom, Custom: leg.Custom, Qty: leg.Qty, Reason: leg.Reason}
}

// wtTalk is a talk step after the preparation of its leg.
func wtTalk(s Snapshot, stop NpcStop, leg wtLeg) Step {
	if step, ok := wtPrepare(s, leg); ok {
		return step
	}
	return Step{Kind: StepTalk, Stop: stop, Reason: leg.Reason}
}

// wtDraw is a draw from the bank of an item the pack lacks, or the park when the round found none.
func wtDraw(s Snapshot, kitName string, line KitLine, reason string) Step {
	if step, ok := wtStock(s, kitName, []KitLine{line}, reason); ok {
		return step
	}
	return Step{Kind: StepWait, Reason: "the " + line.Name + " was drawn from the bank"}
}

// ---------------------------------------------------------------------------------------------------------------------
// The decision
// ---------------------------------------------------------------------------------------------------------------------

// DecideWatchtower is the whole quest, read from the journal's state, the pack and where the account stands.
func DecideWatchtower(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return wtFinish(s)
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	var st WtState
	if s.Journal == StatusNotStarted {
		st = WtState{Stage: WtNotStarted, Left: -1}
	} else {
		if !s.StageKnown {
			return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
		}
		st = WtStateOf(s.Stage)
	}
	area := WtAreaMain
	if s.TileKnown {
		area = WtAreaOf(s.Tile)
	}
	switch st.Stage {
	case WtNotStarted:
		return wtStart(s, area)
	case WtStarted:
		return wtEvidence(s, area)
	case WtGivenFingernails, WtMadeRelic:
		if s.Owned(WtObjRelic) > 0 {
			return wtRelicGate(s)
		}
		return wtTribes(s, st, area)
	case WtGivenRelic:
		return wtCityEntry(s, st, area)
	case WtGivenRiddle:
		return wtRiddle(s, area)
	case WtSolvedRiddle:
		return wtCaves(s, st, area)
	case WtSkavidCrystal:
		return wtEnclaveEntry(s, area)
	case WtFedNightshade:
		return wtAskShamans(s, area)
	case WtLearnedPotion:
		return wtPotion(s, area)
	case WtMadePotion:
		return wtShamans(s, st, area)
	case WtFoundAll:
		return wtGo(s, wtLeg{Custom: CustomWtLever, Reason: "pull the lever to activate the shield"})
	case WtComplete, WtReadScroll:
		return Step{Kind: StepWait, Reason: "the quest tab has not turned green yet"}
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Watch Tower stage %d", st.Stage)}
}

// wtFinish is the quest done: the scroll read, the mirror tower left.
func wtFinish(s Snapshot) Step {
	if s.HeldID(WtObjSpellScroll) > 0 {
		return Step{Kind: StepOpHeld, ItemID: WtObjSpellScroll, Item: "Spell scroll", Op: "Read", Reason: "memorise the Watchtower Teleport scroll"}
	}
	if s.TileKnown && !s.Failed[CustomWtDown] {
		if a := WtAreaOf(s.Tile); a == WtAreaMirror || a == WtAreaTower {
			return Step{Kind: StepCustom, Custom: CustomWtDown, Reason: "down from the activated Watchtower"}
		}
	}
	// The quest is done; a descent that failed is left to the pocket's own exit on the next walk.
	return Step{Kind: StepDone}
}

// wtOnWizardFloor reports the account on the wizard's floor (the tower's second).
func wtOnWizardFloor(s Snapshot, area WtArea) bool {
	return area == WtAreaTower && s.Tile.Level == 2
}

// wtToWizard is the way to the wizard's side: the climb until the account is on his floor, then the talk the caller names. byWall says the quest is not
// started, so the ground ladder is shut and the wall is the way up. A climb that failed parks the quest.
func wtToWizard(s Snapshot, area WtArea, byWall bool, talk Step) Step {
	if wtOnWizardFloor(s, area) {
		return talk
	}
	if s.Failed[CustomWtClimb] {
		return wtParkSafe(s, WtLegPark, "the climb to the Watchtower wizard could not be done (the rocks or the ladders)")
	}
	q := 0
	if byWall {
		q = 1
	}
	return Step{Kind: StepCustom, Custom: CustomWtClimb, Qty: q, Reason: "up to the Watchtower wizard"}
}

// wtStart is stage 0: the wall (Agility 18), the wizard's floor, his three menus.
func wtStart(s Snapshot, area WtArea) Step {
	if !wtOnWizardFloor(s, area) {
		if step, ok := wtGate(s, "agility", WtAgilityWall, "the Climbing rocks on the Watchtower's wall, the only way up until the wizard has given his work"); ok {
			return step
		}
	}
	return wtToWizard(s, area, true, Step{Kind: StepTalk, Stop: WtWizardStop(WtStartPrefer...), Reason: "start the Watch Tower"})
}

// wtEvidence is stage 1: the fingernails from the bush, to the wizard.
func wtEvidence(s Snapshot, area WtArea) Step {
	if s.HeldID(WtObjFingernails) > 0 {
		return wtToWizard(s, area, false, Step{Kind: StepTalk, Stop: WtWizardStop(WtNailsPrefer...), Reason: "give the wizard the fingernails"})
	}
	return wtGo(s, wtLeg{Custom: CustomWtBush, Reason: "search the bush by the Watchtower for the skavid's fingernails", Room: 1})
}

// ---------------------------------------------------------------------------------------------------------------------
// Stage 2: the three tribes
// ---------------------------------------------------------------------------------------------------------------------

// wtParts are the relic's three pieces in the order they are given, each with the custom that asks its giver again and the giver's name.
var wtParts = []struct {
	Part  int
	Ask   string
	Giver string
}{
	{WtObjPart1, CustomWtAskOg, "Og"},
	{WtObjPart2, CustomWtAskGrew, "Grew"},
	{WtObjPart3, CustomWtAskToban, "Toban"},
}

// wtAskOrder is the order the givers are asked in (indexes of wtParts): Og on the road first, Toban's camp next, Grew's island (a rope, a swing) last.
var wtAskOrder = []int{0, 2, 1}

// wtOgreLeg is a leg through ogre country: food in the pack and the ogres' price (the pair of ogres the road is priced at). The sites that price the stands
// of a trip are the caller's.
func wtOgreLeg(custom string, qty int, reason string, kitName string, kit ...KitLine) wtLeg {
	return wtLeg{Custom: custom, Qty: qty, Reason: reason, KitName: kitName, Kit: kit, Food: true, Odds: []func(Snapshot) (bool, string){wtOgres}}
}

// wtSitesLeg is wtOgreLeg priced by the sites it stands in as well.
func wtSitesLeg(custom string, qty int, reason, kitName string, kit []KitLine, sites ...WtSite) wtLeg {
	leg := wtOgreLeg(custom, qty, reason, kitName, kit...)
	leg.Sites = sites
	return leg
}

// wtTribes is stage 2 (and 3 before the relic is told): Og, Grew, the camp, Grew again, Og again, then the parts to the wizard. The order is the map's
// (north to south and back), and every leg is its own flags' and pack's reading, so a death or a lost key just makes the leg come round again.
func wtTribes(s Snapshot, st WtState, area WtArea) Step {
	// The one thing nobody sells, asked before anything with a risk: Toban's bones.
	if !st.Has(WtTobanHelped) && s.HeldID(WtObjDragonBones) == 0 {
		if step, ok := wtStock(s, KitWtBones, []KitLine{wtScarceLine(WtObjDragonBones, "Dragon bones")},
			"Toban wants the bones of an adult dragon, which no shop sells"); ok {
			return step
		}
	}
	// A hurt account in a hunted pocket walks out before anything else is asked of it.
	if step, ok := wtHurtOut(s, area); ok {
		return step
	}
	// Og: his task (a key) the first time, and another key for an account that lost it before the gold came back.
	if !st.Has(WtOgSpoken) || (!st.Has(WtOgHelped) && s.HeldID(WtObjTobanKey) == 0 && s.HeldID(WtObjStolenGold) == 0) {
		return wtTalk(s, WtOgStop, wtSitesLeg("", 0, "Og's task (a key to Toban's chest)", "", nil, WtSiteOg))
	}
	if !st.Has(WtGrewSpoken) {
		return wtIslandTalk(s, area, WtGrewStop, "ask Grew for his task (one of Gorad's teeth)")
	}
	needChest := s.HeldID(WtObjTobanKey) > 0 && s.HeldID(WtObjStolenGold) == 0 && !st.Has(WtOgHelped)
	needToban := !st.Has(WtTobanHelped)
	needGorad := !st.Has(WtGrewHelped) && s.HeldID(WtObjTooth) == 0
	if needChest || needToban || needGorad {
		return wtCamp(s, area, needChest, needToban, needGorad)
	}
	if s.HeldID(WtObjTooth) > 0 && !st.Has(WtGrewHelped) {
		return wtIslandTalk(s, area, WtGrewStop, "give Grew the tooth for his relic part and the first crystal")
	}
	if s.HeldID(WtObjStolenGold) > 0 && !st.Has(WtOgHelped) {
		return wtTalk(s, WtOgStop, wtSitesLeg("", 0, "give Og his gold back", "", nil, WtSiteOg))
	}
	// The pieces the pack holds go to the wizard, each its own custom: a piece he already holds ("I already have that part") fails only that piece's.
	for _, p := range wtParts {
		if s.HeldID(p.Part) > 0 && !s.Failed[WtGivePartName(p.Part)] {
			return wtGo(s, wtLeg{Custom: WtGivePartName(p.Part), Qty: p.Part, Reason: "give the wizard a piece of the relic"})
		}
	}
	return wtLostPiece(s)
}

// wtLostPiece is every tribe helped and no piece in the pack that the wizard will take. Either the relic was made and lost (the wizard's menu at stage 3 offers
// another), or a piece was lost after its tribe was helped (a death in the camp, a dropped pack), or a piece the wizard refuses is kept. The journal cannot
// tell stage 2 from 3, so each remedy is tried once a run in the order of its cost: a piece in the bank is drawn; the wizard is asked for the relic; each
// giver is asked for his piece (Og's and Toban's menus offer it whatever the wizard holds, Grew's only when the wizard lacks it) and a piece handed over that
// the wizard refuses is kept and never given again. When all are asked the quest parks for a day: the account does not loop on the climb every half hour.
func wtLostPiece(s Snapshot) Step {
	for _, p := range wtParts {
		if s.HeldID(p.Part) == 0 && s.Owned(p.Part) > 0 {
			return wtDraw(s, KitWtParts, wtQuestLine(p.Part, "Relic piece"), "a piece of the relic from the bank")
		}
	}
	if !s.Ran[CustomWtRelicBack] && !s.Failed[CustomWtRelicBack] {
		return wtGo(s, wtLeg{Custom: CustomWtRelicBack, Reason: "ask the wizard for the relic he assembled, in case it was lost"})
	}
	for _, i := range wtAskOrder {
		p := wtParts[i]
		if s.HeldID(p.Part) > 0 || s.Ran[p.Ask] || s.Failed[p.Ask] {
			continue
		}
		reason := "ask " + p.Giver + " for the relic piece he gave and the account lost"
		switch p.Ask {
		case CustomWtAskOg:
			return wtGo(s, wtSitesLeg(p.Ask, 0, reason, "", nil, WtSiteOg))
		case CustomWtAskToban:
			return wtGo(s, wtSitesLeg(p.Ask, 0, reason, "", nil, wtCampTrip()...))
		default:
			if step, ok := wtGate(s, "agility", WtAgilitySwing, "the swing back off Grew's island"); ok {
				return step
			}
			return wtGo(s, wtSitesLeg(p.Ask, 0, reason, KitWtIsland, []KitLine{wtRopeLine()}, WtSiteBranch, WtSiteGrew))
		}
	}
	return wtParkSafe(s, WtRelicPark, "the Watch Tower is waiting: a piece of the ogre relic is lost, the wizard has no copy of the relic and the three tribes' replacements did not mend it")
}

// wtIslandTalk is a talk with Grew on his island: the swing over (a rope, used up), then the talk. The trip is priced before the swing, with the ogres of the
// bank and of the island; inside the island the account is past pricing, and the talk is next.
func wtIslandTalk(s Snapshot, area WtArea, stop NpcStop, reason string) Step {
	if area == WtAreaIsland {
		return wtTalk(s, stop, wtLeg{Reason: reason})
	}
	if step, ok := wtGate(s, "agility", WtAgilitySwing, "the swing back off Grew's island"); ok {
		return step
	}
	return wtGo(s, wtSitesLeg(CustomWtIslandIn, 0, "swing over to Grew's island", KitWtIsland, []KitLine{wtRopeLine()}, WtSiteBranch, WtSiteGrew))
}

// wtCampTrip is the sites of a visit to the camp: the road to the cave's mouth, the camp, and the cave mouth again on the way out.
func wtCampTrip() []WtSite { return []WtSite{WtSiteCaveMouth, WtSiteCamp, WtSiteCaveMouth} }

// wtCamp is the work in Toban's camp: the chest first, Gorad next and Toban last (the order keeps the relic piece Toban pays with out of the pack while the
// fight is on: a death at Gorad then loses the tooth and the gold, which the quest asks again, and not a piece the wizard has no copy of). The whole trip is
// priced before the cave: the road, the camp's four ogres through the chest and Toban's talk, and the fight with Gorad when it is needed. Inside the camp the
// account is topped up before the fight and walks out hurt (wtHurtOut).
func wtCamp(s Snapshot, area WtArea, chest, toban, gorad bool) Step {
	if area != WtAreaCamp {
		leg := wtSitesLeg(CustomWtCampIn, 0, "into Toban's camp by the cave", "", nil, wtCampTrip()...)
		leg.Gorad = gorad
		return wtGo(s, leg)
	}
	switch {
	case chest:
		return wtGo(s, wtLeg{Custom: CustomWtChest, Reason: "open Toban's chest with Og's key for his gold"})
	case gorad:
		if step, ok := wtTopUp(s, FoeGorad.MaxHit()+WtOgreVolley(WtSiteGorad.Ogres)); ok {
			return step
		}
		return wtGo(s, wtLeg{Custom: CustomWtGorad, Reason: "knock one of Gorad's teeth out", Room: 1})
	case toban:
		return wtTalk(s, WtTobanStop, wtLeg{Reason: "talk to Toban (his bones, once he has asked for them)"})
	}
	return wtGo(s, wtLeg{Custom: CustomWtCampIn, Reason: "into Toban's camp by the cave"})
}

// wtHurtOut walks an account out of a hunted pocket (the island, the camp) when it is hurt and has no food to mend it: under four of the ogres' hardest hits
// is the line. The engine's walk leaves the pocket by its own way first.
func wtHurtOut(s Snapshot, area WtArea) (Step, bool) {
	if (area != WtAreaIsland && area != WtAreaCamp) || s.MaxHP <= 0 || s.HP <= 0 {
		return Step{}, false
	}
	if s.HP >= 4*FoeOgre.PeakHit() || FoodPoolHeal(s.InvIDs, WtFoodIDs) > 0 {
		return Step{}, false
	}
	return wtLeavePocket(s, fmt.Sprintf("hurt (%d hitpoints) and with no food", s.HP))
}

// ---------------------------------------------------------------------------------------------------------------------
// Stages 3 and 4: the relic, the gate, the city
// ---------------------------------------------------------------------------------------------------------------------

// wtRelicGate is the assembled relic shown to a guard of the north-west gate. A relic in the bank is drawn first.
func wtRelicGate(s Snapshot) Step {
	if s.HeldID(WtObjRelic) == 0 {
		return wtDraw(s, KitWtRelic, wtQuestLine(WtObjRelic, "Ogre relic"), "the relic from the bank")
	}
	return wtGo(s, wtOgreLeg(CustomWtRelicGate, 0, "show the relic to a guard of the north-west gate", ""))
}

// wtCityLeg is a leg inside Gu'Tanoth: the ogres and the chieftains.
func wtCityLeg(custom string, reason string, kitName string, kit ...KitLine) wtLeg {
	return wtLeg{Custom: custom, Reason: reason, KitName: kitName, Kit: kit, Food: true,
		Odds: []func(Snapshot) (bool, string){wtOgres, wtChiefs(WtChieftainTicks)}}
}

// wtCityEntry is stage 4: the rock cake, the battlement, the chasm, the City guard's riddle. A leg that begins inside one of the city's pockets (the lower
// city, the City guard's side) is past pricing: it was priced at the leg that went in, and a park inside the pocket would leave the account where the ogres
// are.
func wtCityEntry(s Snapshot, st WtState, area WtArea) Step {
	if area == WtAreaCityGuard {
		return wtTalk(s, WtCityGuardRiddleStop, wtLeg{Reason: "the City guard's riddle"})
	}
	if !st.Has(WtMarketPaid) && area != WtAreaLowerCity {
		if s.HeldID(WtObjRockCake) == 0 {
			if step, ok := wtGate(s, "thieving", WtThieving, "the ogre trader's rock cake stall"); ok {
				return step
			}
			leg := wtCityLeg(CustomWtCake, "steal a rock cake from the ogre stall", "")
			leg.Room = 1
			return wtGo(s, leg)
		}
		return wtGo(s, wtCityLeg(CustomWtBattlement, "give the battlement guard the rock cake and climb the wall", ""))
	}
	return wtJump(s, area)
}

// wtJump is the chasm: Agility 25 and the toll, then the other side. From inside the lower city the account is past pricing.
func wtJump(s Snapshot, area WtArea) Step {
	if step, ok := wtGate(s, "agility", WtAgilityChasm, "the jump across the chasm in Gu'Tanoth"); ok {
		return step
	}
	leg := wtCityLeg(CustomWtJump, "pay the ogre guard and jump the chasm", KitWtCity, wtCoinsLine(), wtDeathRuneLine())
	if area == WtAreaLowerCity {
		leg.Food, leg.Odds = false, nil
	}
	return wtGo(s, leg)
}

// wtRiddle is stage 5: the death rune on the City guard.
func wtRiddle(s Snapshot, area WtArea) Step {
	if area != WtAreaCityGuard {
		return wtJump(s, area)
	}
	return wtGo(s, wtLeg{Custom: CustomWtRiddleGive, Reason: "answer the City guard's riddle with a death rune", KitName: KitWtCity, Kit: []KitLine{wtDeathRuneLine()}})
}

// ---------------------------------------------------------------------------------------------------------------------
// Stage 6: the caves
// ---------------------------------------------------------------------------------------------------------------------

// wtCaveKit is what a cave mouth wants: the map and a lit light (the candle on the ground by the tower unless the pack holds one alight), or the mouth
// drops the account in the dark.
func wtCaveKit(s Snapshot) []KitLine {
	kit := []KitLine{wtQuestLine(WtObjMap, "Skavid map")}
	if wtLit(s) == 0 {
		kit = append(kit, wtCandleLine())
	}
	return kit
}

// wtNextCave is the cave the account goes to next: the scared skavid's (5) until the words are learned, then the four talkers' (1 to 4), then the
// mad skavid's (6).
func wtNextCave(st WtState) int {
	if !st.Has(WtLearning) {
		return 5
	}
	for i := 0; i < 4; i++ {
		if !st.Has(WtCaveFlag[i]) {
			return i + 1
		}
	}
	return 6
}

// wtCaves is stage 6.
func wtCaves(s Snapshot, st WtState, area WtArea) Step {
	// A lost map: the bank's, or the City guard's other copy for the asking (across the chasm again).
	if s.HeldID(WtObjMap) == 0 {
		if s.Owned(WtObjMap) > 0 {
			return wtDraw(s, KitWtMap, wtQuestLine(WtObjMap, "Skavid map"), "the map from the bank")
		}
		if area == WtAreaCityGuard {
			return wtTalk(s, WtCityGuardMapStop, wtLeg{Reason: "ask the City guard for another map"})
		}
		return wtJump(s, area)
	}
	cave := wtNextCave(st)
	if cave == 6 {
		kit := append(wtCaveKit(s), KitLine{ID: WtObjGoldBar, Name: "Gold bar", Qty: 1})
		return wtGo(s, wtOgreLegWith(CustomWtMad, 0, "the mad skavid in cave 6, behind the east gate", KitWtBar, kit))
	}
	return wtGo(s, wtOgreLegWith(CustomWtCave, cave, fmt.Sprintf("the skavid in cave %d", cave), KitWtCaves, wtCaveKit(s)))
}

// wtOgreLegWith is wtOgreLeg with a kit slice.
func wtOgreLegWith(custom string, qty int, reason, kitName string, kit []KitLine) wtLeg {
	return wtOgreLeg(custom, qty, reason, kitName, kit...)
}

// ---------------------------------------------------------------------------------------------------------------------
// Stages 7 and 8: the nightshade, the guard, the wizard
// ---------------------------------------------------------------------------------------------------------------------

// wtEnclaveInLeg is the leg that feeds the enclave guard the nightshade and runs past him: the guard's stand is in the chieftains' country, and the account
// that is let in has the dragons' nest to walk out of (WtVisitExit at the least).
func wtEnclaveInLeg(visit WtVisit) wtLeg {
	return wtLeg{Custom: CustomWtEnclaveIn, Reason: "feed the enclave guard the nightshade and run past him", Food: true,
		Odds: []func(Snapshot) (bool, string){wtOgres, wtChiefs(WtChieftainTicks)}, Visit: &visit, TopUpPeak: WtDragonPeak(false)}
}

// wtEnclaveEntry is stage 7: a cave nightshade, the enclave guard. The enclave is priced FIRST (food, the guard's chieftains, the dragons' nest and the way
// out of it), before the nightshade is fetched or the guard's stand walked to: an account the nest would kill is parked here, at the first step it cannot
// do safely, with the numbers in the reason.
func wtEnclaveEntry(s Snapshot, area WtArea) Step {
	if area == WtAreaEnclave {
		return wtGo(s, wtLeg{Custom: CustomWtEnclaveOut, Reason: "out of the shamans' enclave by its cave"})
	}
	in := wtEnclaveInLeg(WtVisitExit)
	if step, ok := wtPrepare(s, in); ok {
		return step
	}
	if s.HeldID(WtObjNightshade) == 0 {
		return wtGo(s, wtOgreLegWith(CustomWtNightshade, 0, "take a cave nightshade from the floor of cave 2", KitWtCaves, wtCaveKit(s)))
	}
	return wtGo(s, in)
}

// wtAskShamans is stage 8: the wizard is told the shamans cannot be touched.
func wtAskShamans(s Snapshot, area WtArea) Step {
	return wtToWizard(s, area, false, Step{Kind: StepTalk, Stop: WtWizardStop("Hello."), Reason: "tell the wizard the shamans cannot be touched"})
}

// ---------------------------------------------------------------------------------------------------------------------
// Stage 9: the potion
// ---------------------------------------------------------------------------------------------------------------------

// wtPotionNeeds is what the mixes and the grinding still need, by what the pack already holds: the scarce lines (a guam leaf, bat bones: only a bank
// or the mule has them), the rest (a vial of water, a pestle: a shop has them) and whether the berries are to be fetched. The mixes go in the order
// the content takes them: a vial of water and a guam leaf, then jangerberries, then ground bat bones.
func wtPotionNeeds(s Snapshot) (scarce, rest []KitLine, berries bool) {
	made1 := wtHeld(s, WtObjGuamVial, WtObjGuamJangerVial) > 0
	made2 := s.HeldID(WtObjGuamJangerVial) > 0
	ground := s.HeldID(WtObjGroundBones) > 0
	if !made1 {
		scarce = append(scarce, wtScarceLine(WtObjGuamLeaf, "Guam leaf"))
		rest = append(rest, wtVialLine())
	}
	if !ground {
		scarce = append(scarce, wtScarceLine(WtObjBatBones, "Bat bones"))
		rest = append(rest, wtPestleLine())
	}
	berries = !made2 && s.HeldID(WtObjJangerberries) == 0
	return scarce, rest, berries
}

// wtPotionLegs is the brewing from wherever the pack stands: the scarce lines first (nothing is walked for the berries if the bones are not there), the
// berries from the island, the mixes, and the wizard. It is the same chain at stage 9 and at stage 10 after a potion was lost.
func wtPotionLegs(s Snapshot) Step {
	if step, ok := wtGate(s, "herblore", WtHerblore, "the ogre potion (the guam vial, the jangerberries and the ground bat bones are mixed at Herblore 14)"); ok {
		return step
	}
	if s.HeldID(WtObjOgrePotion) > 0 {
		return wtGo(s, wtLeg{Custom: CustomWtInfuse, Reason: "have the wizard infuse the ogre potion"})
	}
	scarce, rest, berries := wtPotionNeeds(s)
	if step, ok := wtStock(s, KitWtScarce, scarce, "a guam leaf and bat bones, which only a bank or the mule holds"); ok {
		return step
	}
	if berries {
		return wtBerryTrip(s)
	}
	return wtGo(s, wtLeg{Custom: CustomWtBrew, Reason: "mix the ogre potion: guam leaf in the vial, then the jangerberries, then the ground bat bones",
		KitName: KitWtPotion, Kit: rest})
}

// wtBerryTrip is a trip to Grew's island for the berries (WtJangerberryQty of them), the rope used up by the crossing. It is priced like Grew's talk: the bank
// of the branch and the island, the four ogres of each, with the walk round the four bushes.
func wtBerryTrip(s Snapshot) Step {
	if step, ok := wtGate(s, "agility", WtAgilitySwing, "the swing back off Grew's island"); ok {
		return step
	}
	return wtGo(s, wtSitesLeg(CustomWtBerries, WtJangerberryQty, "pick jangerberries on Grew's island", KitWtIsland, []KitLine{wtRopeLine()}, WtSiteBranch, WtSiteBerries))
}

func wtPotion(s Snapshot, area WtArea) Step { return wtPotionLegs(s) }

// ---------------------------------------------------------------------------------------------------------------------
// Stage 10: the shamans and the crystals
// ---------------------------------------------------------------------------------------------------------------------

// wtCrystals are the four, in the order the wizard is shown them.
var wtCrystals = []int{WtObjCrystal1, WtObjCrystal2, WtObjCrystal3, WtObjCrystal4}

// wtEnclaveLeg is a leg that works in the enclave (the nightshade is wanted only to get in). From outside, the whole visit (visit) is priced before the
// nightshade is fetched, as at stage 7; once the account is inside it is past pricing and the leg goes on.
func wtEnclaveLeg(s Snapshot, area WtArea, custom string, qty int, reason string, visit WtVisit) Step {
	leg := wtLeg{Custom: custom, Qty: qty, Reason: reason, Food: true}
	if custom == CustomWtRock {
		leg.KitName, leg.Kit = KitWtRock, []KitLine{wtPickaxeLine()}
	}
	if area != WtAreaEnclave {
		leg.Odds = []func(Snapshot) (bool, string){wtOgres, wtChiefs(WtChieftainTicks)}
		leg.Visit, leg.TopUpPeak = &visit, WtDragonPeak(false)
		if step, ok := wtPrepare(s, leg); ok {
			return step
		}
		if s.HeldID(WtObjNightshade) == 0 {
			return wtGo(s, wtOgreLegWith(CustomWtNightshade, 0, "take a cave nightshade for the enclave", KitWtCaves, wtCaveKit(s)))
		}
	}
	return wtGo(s, leg)
}

// wtShamans is stage 10.
func wtShamans(s Snapshot, st WtState, area WtArea) Step {
	left := st.Left
	if left < 0 {
		left = WtShamans
	}
	if left > 0 {
		if s.HeldID(WtObjMagicPotion) == 0 {
			if area == WtAreaEnclave {
				return wtGo(s, wtLeg{Custom: CustomWtEnclaveOut, Reason: "out of the enclave to brew another potion"})
			}
			return wtPotionLegs(s)
		}
		if step, ok := wtGate(s, "magic", WtMagic, "the magic ogre potion on the shamans"); ok {
			return step
		}
		// The pickaxe rides along so that the rock can be mined in the same visit.
		if s.HeldID(WtObjPickaxe) == 0 && area != WtAreaEnclave {
			if step, ok := wtStock(s, KitWtRock, []KitLine{wtPickaxeLine()}, "a pickaxe for the Rock of Dalgroth, carried in with the potion"); ok {
				return step
			}
		}
		return wtEnclaveLeg(s, area, CustomWtDissolve, left, fmt.Sprintf("dissolve the ogre shamans (%d left)", left), WtVisitWork)
	}
	// All six are gone: the third crystal came with the sixth, the fourth is the rock's. A level the rock wants is judged with the account outside: it is walked
	// out of the enclave first and parked there (wtParkSafe).
	if s.Owned(WtObjCrystal4) == 0 {
		if step, ok := wtGate(s, "mining", WtMining, "the Rock of Dalgroth"); ok {
			return step
		}
		return wtEnclaveLeg(s, area, CustomWtRock, 0, "mine the Rock of Dalgroth for the fourth crystal", WtVisitRock)
	}
	if s.Owned(WtObjCrystal3) == 0 {
		return wtEnclaveLeg(s, area, CustomWtRobe, 0, "search a shaman robe for the third crystal", WtVisitRobe)
	}
	if area == WtAreaEnclave {
		return wtGo(s, wtLeg{Custom: CustomWtEnclaveOut, Reason: "out of the enclave with the crystals"})
	}
	// Crystals in the bank are drawn, crystals lost are asked for again at their sources.
	for _, c := range wtCrystals {
		if s.HeldID(c) == 0 && s.Owned(c) > 0 {
			return wtDraw(s, KitWtDraw, wtQuestLine(c, "Crystal"), "a crystal from the bank")
		}
	}
	for _, c := range wtCrystals {
		if s.HeldID(c) == 0 {
			return wtLostCrystal(s, c, area)
		}
	}
	return wtGo(s, wtLeg{Custom: CustomWtCrystals, Reason: "take all four crystals to the wizard"})
}

// wtLostCrystal is the way back to a crystal the account does not hold and no bank does: Grew's and the mad skavid's are asked again.
func wtLostCrystal(s Snapshot, id int, area WtArea) Step {
	switch id {
	case WtObjCrystal1:
		return wtIslandTalk(s, area, WtGrewLostStop, "ask Grew for the crystal he gave and the account lost")
	case WtObjCrystal2:
		return wtGo(s, wtOgreLegWith(CustomWtMad, 1, "ask the mad skavid for the crystal he gave and the account lost", KitWtCaves, wtCaveKit(s)))
	}
	return Step{Kind: StepWait, Reason: "a crystal of the enclave is lost"}
}

// Watchtower is the engine module. It never fights back (NoRetaliate): the hunters of ogre country are walked on from and priced, and Gorad is fought by an
// explicit attack. One death anywhere parks it for a day: the pack is gone and the price was wrong for the account.
var Watchtower = Module{
	Record: WatchtowerRecord, Bank: WtBank, BankSet: true, Anchor: WtBank,
	ToolIDs:     wtTools,
	ReadStage:   ParseWatchtowerJournal,
	Sustain:     wtSustain,
	NoRetaliate: true,
	MaxDeaths:   1,
	Decide:      DecideWatchtower,
}
