package questplan

import (
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Goblin Diplomacy (content id `gobdip`, %goblinquest, not transmitted), ported from defs/goblindiplomacy.ts and
// checked against content 289 (quests/quest_gobdip, areas/area_falador/scripts/goblin_village,
// areas/area_port_sarim/scripts/bartender.rs2, areas/area_draynor/scripts/aggie.rs2, skill_crafting/scripts/dye_cape).
//
// The quest is four visits to the two goblin generals, three of them hand-overs. The bartender of the Rusty Anchor
// starts it (stage 1); the generals, asked "Do you want me to pick an armour colour for you?", ask for ORANGE goblin mail (stage 2); the orange
// mail is handed over and they ask for BLUE (stage 3); the blue is handed over and they ask for the original BROWN,
// which is plain goblin mail (stage 4); the brown is handed over (stage 5) and the quest completes (stage 6: 5 quest
// points, 200 Crafting XP and a gold bar). Three goblin mail in all, two of them dyed:
//
//   - Orange dye is a red dye (3 redberries and 5 coins to Aggie in Draynor) mixed with a yellow dye (2 onions and 5
//     coins to Aggie); blue dye is 2 woad leaves and 5 coins to Aggie. Wyson the gardener in Falador's park sells two
//     woad leaves for 20 coins ("How about 20 coins?"). Redberries have no bush in content 289: they are three ground
//     spawns south-east of Varrock (the cadava berry patch, m51_52) and one a minute from Wydin's food store in Port
//     Sarim (stock 1, restock 100 ticks). Onions are picked in Rimmington (the Witch's Potion patch).
//   - A dye used on a goblin mail makes the coloured mail (goblin_mail.rs2 opheldu); the orange and blue mail are
//     untradeable, plain goblin mail is not. Plain mail is a goblin drop: 5/128 from the level 2-13 goblins and 10/128
//     from the Goblin Village's own goblins (goblin.rs2, the village table), 21 of them round the generals - so the
//     mail is farmed where it is handed in, and last, after the dyes (which are made 250 tiles south).
//
// The quest is decided from the journal stage (which handovers are done) and the pack (what is in hand), and every
// step is restartable: a stage 3 account holding the blue dye and two plain mail goes straight to dyeing it.
// Provisioning follows the owner's item rule everywhere: a first pass draws whatever the bank or the mule holds of
// ANY of the quest's items (a half-done run is banked by the trainer between ladder passes), then the redberries come
// from the ground patch or Wydin, the onions are picked, the woad bought from Wyson, the dyes made by Aggie, and the
// mail farmed.

// GoblinDiplomacyName is the quest tab's spelling (component 7338).
const GoblinDiplomacyName = "Goblin Diplomacy"

// GoblinDiplomacyID is the engine's id.
const GoblinDiplomacyID = "gobdip"

// Ids from content/pack.
const (
	NpcRustyAnchorBartender = 734
	NpcGeneralBentnoze      = 296
	NpcGeneralWartface      = 297
	NpcGoblinGreenArmour    = 298
	NpcGoblinRedArmour      = 299
	NpcAggie                = 922
	NpcWyson                = 36
	ObjGoblinMailOrange     = 286
	ObjGoblinMailBlue       = 287
	ObjGoblinMail           = 288
	ObjRedDye               = 1763
	ObjYellowDye            = 1765
	ObjBlueDye              = 1767
	ObjOrangeDye            = 1769
	ObjWoadLeaf             = 1793
	ObjRedberries           = 1951
)

// Stages, from quest_gobdip.constant (^gobdip_*), %goblinquest.
const (
	GobdipNotStarted      = 0
	GobdipStarted         = 1
	GobdipWillBringArmour = 2
	GobdipGaveOrange      = 3
	GobdipGaveBlue        = 4
	GobdipGaveBrown       = 5
	GobdipComplete        = 6
)

// What the quest costs, from the scripts.
const (
	// GobdipBerriesPerDye is the redberries one red dye takes (aggie_red_dye).
	GobdipBerriesPerDye = 3
	// GobdipOnionsPerDye is the onions one yellow dye takes (aggie_yellow_dye).
	GobdipOnionsPerDye = 2
	// GobdipWoadPerDye is the woad leaves one blue dye takes (aggie_blue_dye), and what Wyson gives for 20 coins.
	GobdipWoadPerDye = 2
	// GobdipDyeFee is the coins Aggie takes for each dye.
	GobdipDyeFee = 5
	// GobdipWoadFee is the coins Wyson takes for two woad leaves ("How about 20 coins?").
	GobdipWoadFee = 20
	// GobdipCoinFloat is the coins the pantry draws: both dyes' fees (15), the woad (20) and the shop berries (9),
	// with a margin.
	GobdipCoinFloat = 60
)

// Tiles from content maps (npc spawns: the generals and the village's goblins are in m46_54).
var (
	// RustyAnchorBartender is the Port Sarim pub's bartender (rustyanchor_bartender, 734: seven NPCs share "Bartender").
	RustyAnchorBartender = tile.Tile{X: 3045, Z: 3257}
	// GoblinGenerals is where Wartface (2957,3511) and Bentnoze (2958,3511) stand.
	GoblinGenerals = tile.Tile{X: 2957, Z: 3511}
	// AggieAnchor is Aggie's house in Draynor (the dyes).
	AggieAnchor = tile.Tile{X: 3086, Z: 3259}
	// WysonAnchor is Wyson the gardener's spawn in Falador's park. He wanders a little; the stop's leash covers it.
	WysonAnchor = tile.Tile{X: 3027, Z: 3379}
	// GoblinVillageField is the middle of the Goblin Village's 21 goblins (green and red armour, x 2951-2964, z
	// 3481-3514), south of the Wilderness line (z 3520) by 20 tiles. The farm never goes north of GoblinVillageNorth.
	GoblinVillageField = tile.Tile{X: 2956, Z: 3498}
	// GoblinVillageNorth is the northernmost tile the mail farm walks to: the village's last goblin spawn is at z
	// 3514 and the Wilderness begins at z 3520.
	GoblinVillageNorth = 3516
)

// GobdipFarmGoblinIDs are the village goblins: the green and the red armour (both "Goblin", level 5, 12 hitpoints,
// op "Attack", the village drop table at 10/128 goblin mail).
var GobdipFarmGoblinIDs = []int{NpcGoblinGreenArmour, NpcGoblinRedArmour}

// RedberrySpawns are the three redberry ground spawns beside the cadava berry patch south-east of Varrock
// (content/maps/m51_52, obj 1951), one each, back after about a minute.
var RedberrySpawns = []tile.Tile{{X: 3267, Z: 3371}, {X: 3271, Z: 3366}, {X: 3278, Z: 3374}}

// Custom step names (internal/scripts/questmods) and the skill method of the mail.
const (
	// MethodGoblinMail is the skill source of plain goblin mail: kill village goblins, take the mail they drop.
	MethodGoblinMail = "goblin-mail"
)

// Kit names.
const (
	KitGobdipPantry  = "gobdip-pantry"
	KitGobdipBerries = "gobdip-berries"
	KitGobdipMail    = "gobdip-mail"
)

var (
	// RustyAnchorBartenderStop starts the quest; the pub's menu offers it among two or three beer-and-rumour lines.
	RustyAnchorBartenderStop = NpcStop{Npc: "Bartender", ID: NpcRustyAnchorBartender, Anchor: RustyAnchorBartender, Leash: 8,
		Prefer: []string{"Not very busy in here today, is it?"}}
	// GoblinGeneralsStop is General Wartface. At stage 1 it picks "Do you want me to pick an armour colour for you?"
	// (the other two menu lines loop back to the same menu); at stages 2-5 there is no menu, only the hand-over.
	GoblinGeneralsStop = NpcStop{Npc: "General Wartface", ID: NpcGeneralWartface, Anchor: GoblinGenerals, Leash: 6,
		Prefer: []string{"Do you want me to pick an armour colour for you?"}}
	// WysonStop sells two woad leaves for 20 coins; the dialogue is driven until they are in the pack.
	WysonStop = NpcStop{Npc: "Wyson the gardener", ID: NpcWyson, Anchor: WysonAnchor, Leash: 12,
		Prefer: []string{"I'm looking for woad leaves.", "How about 20 coins?"}}
	// AggieRedStop, AggieYellowStop and AggieBlueStop walk Aggie's three menus to the dye ("Can you make dyes for me
	// please?", "What do you need to make red dye?", "Okay, make me some red dye please.").
	AggieRedStop = NpcStop{Npc: "Aggie", ID: NpcAggie, Anchor: AggieAnchor, Leash: 6,
		Prefer: []string{"Can you make dyes for me please?", "What do you need to make red dye?", "Okay, make me some red dye please."}}
	AggieYellowStop = NpcStop{Npc: "Aggie", ID: NpcAggie, Anchor: AggieAnchor, Leash: 6,
		Prefer: []string{"Can you make dyes for me please?", "What do you need to make yellow dye?", "Okay, make me some yellow dye please."}}
	AggieBlueStop = NpcStop{Npc: "Aggie", ID: NpcAggie, Anchor: AggieAnchor, Leash: 6,
		Prefer: []string{"Can you make dyes for me please?", "What do you need to make blue dye?", "Okay, make me some blue dye please."}}
)

// gobdipJournalNeedles are ParseGoblinDiplomacyJournal's phrases, newest first, written against NormaliseLines
// (gobdip_journal.rs2; finished lines are struck through but kept, so the later stages contain the earlier ones'
// phrases and only the order makes the answer right). None spans a colour tag next to punctuation.
var gobdipJournalNeedles = []struct {
	stage  int
	needle string
}{
	{GobdipComplete, "quest complete"},
	{GobdipGaveBrown, "i should see if they liked the brown goblin armour"},
	{GobdipGaveBlue, "they asked me to bring them some of their original brown goblin armour to try out instead"},
	{GobdipGaveOrange, "they asked me to bring them some blue goblin armour to try out instead"},
	{GobdipWillBringArmour, "i should bring the goblins some orange goblin armour"},
	{GobdipStarted, "i should investigate before it turns into a full blown war"},
	{GobdipNotStarted, "i can start this quest by talking to the bartender"},
}

// ParseGoblinDiplomacyJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised
// journal, which is unknown, never stage 0.
func ParseGoblinDiplomacyJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range gobdipJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// gobdipNeed is which hand-overs are still to come at a stage: the generals take the orange mail from stages 1-2, the
// blue from 1-3 and the brown (plain) from 1-4.
type gobdipNeed struct{ Orange, Blue, Brown bool }

func gobdipNeeds(stage int) gobdipNeed {
	return gobdipNeed{Orange: stage <= GobdipWillBringArmour, Blue: stage <= GobdipGaveOrange, Brown: stage <= GobdipGaveBlue}
}

// gobdipTools are the quest's own items, which no bank visit of the quest may leave in the bank: the plain and coloured
// mail, every dye, the ingredients and the coins. The trainer banks them between two ladder passes all the same, which
// the pantry draws back out.
var gobdipTools = []int{CoinsID, ObjGoblinMail, ObjGoblinMailOrange, ObjGoblinMailBlue, ObjRedDye, ObjYellowDye, ObjBlueDye,
	ObjOrangeDye, ObjWoadLeaf, ObjRedberries, ObjOnion}

// gobdipFoodIDs are the foods the account may carry and eat in the goblin fights, best first.
var gobdipFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1971, 1891, 1893, 1895}

// gobdipKeep is a bank visit's keep list: the tools and the food.
func gobdipKeep() []int { return append(append([]int(nil), gobdipTools...), gobdipFoodIDs...) }

// gobdipBank is the bank a provisioning visit uses: the one nearest where the account stands (the route runs from
// Port Sarim to Draynor to Falador and the village, and the bank read costs a walk each time), else Draynor's.
func gobdipBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return DraynorBank
}

// gobdipPantryKit is the kit of the first pass: every item of the quest the account may already own, wherever the
// owner's rule finds it first (its bank, then the mule). Every line is optional and none has a source of its own, so
// the pass reads the bank, draws what is there, makes the one mule ask, and is satisfied - it never walks to a shop or
// a goblin. The coloured mail is untradeable, so the mule is never asked for it.
func gobdipPantryKit(n gobdipNeed) []KitLine {
	var kit []KitLine
	add := func(id int, name string, qty int, untradeable bool) {
		// Woad leaves stack in content 289 (the config archive says so; the dyes, berries and onions do not).
		kit = append(kit, KitLine{ID: id, Name: name, Qty: qty, Optional: true, Untradeable: untradeable, Stackable: id == ObjWoadLeaf})
	}
	plain := 0
	if n.Orange {
		add(ObjGoblinMailOrange, "Orange goblin mail", 1, true)
		add(ObjOrangeDye, "Orange dye", 1, false)
		add(ObjRedDye, "Red dye", 1, false)
		add(ObjYellowDye, "Yellow dye", 1, false)
		add(ObjRedberries, "Redberries", GobdipBerriesPerDye, false)
		add(ObjOnion, "Onion", GobdipOnionsPerDye, false)
		plain++
	}
	if n.Blue {
		add(ObjGoblinMailBlue, "Blue goblin mail", 1, true)
		add(ObjBlueDye, "Blue dye", 1, false)
		add(ObjWoadLeaf, "Woad leaf", GobdipWoadPerDye, false)
		plain++
	}
	if n.Brown {
		plain++
	}
	if plain > 0 {
		add(ObjGoblinMail, "Goblin mail", plain, false)
	}
	kit = append(kit, KitLine{ID: CoinsID, Name: "Coins", Qty: GobdipCoinFloat, Stackable: true, Optional: true})
	return kit
}

// gobdipBerriesKit is the redberries of one red dye: the ground spawns first (the item rule's third rung) and Wydin's
// shelf as the last resort, which holds one berry a minute and so cannot supply three in a visit.
func gobdipBerriesKit() []KitLine {
	var src []Source
	for _, t := range RedberrySpawns {
		src = append(src, Source{Kind: SourceGround, Tile: t, PerSpawn: 1, RespawnTicks: 100, Note: "the berry patch south-east of Varrock"})
	}
	src = append(src, Source{Kind: SourceShop, Shop: WydinShop, Price: 3})
	return []KitLine{{ID: ObjRedberries, Name: "Redberries", Qty: GobdipBerriesPerDye, Sources: src}}
}

// gobdipMailKit is the plain mail the account is short of: the farm is the last rung, after the bank and the mule
// (a goblin killer's bank often holds some).
func gobdipMailKit(qty int) []KitLine {
	return []KitLine{{ID: ObjGoblinMail, Name: "Goblin mail", Qty: qty, Sources: []Source{{
		Kind: SourceSkill, Skill: "attack", Method: MethodGoblinMail, Tile: GoblinVillageField,
		Note: "10/128 from a Goblin Village goblin",
	}}}}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// gobdipCoinWait is the step of an account too poor for a fee: the pantry has already tried the bank and the mule
// for coins, so the quest waits (and parks) rather than walk to a seller with nothing to pay.
func gobdipCoinWait(what string, need, have int) Step {
	return Step{Kind: StepWait, Reason: what + " needs " + strconv.Itoa(need) + " coins (the pack holds " + strconv.Itoa(have) + ", and the bank and the mule gave no more)"}
}

// DecideGoblinDiplomacy is the whole quest. In order: the bartender; the pantry (once a run); then, for every hand-over
// still to come whose coloured mail is not in hand, the ingredients (redberries, onions, woad), Aggie's dyes and the
// mixing of the orange; then the plain mail (one for each dye and one for the brown); then the dyes on the mail; then
// the generals, four times over (each visit moves the quest one stage on).
func DecideGoblinDiplomacy(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: RustyAnchorBartenderStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	if s.Stage <= GobdipNotStarted {
		return Step{Kind: StepTalk, Stop: RustyAnchorBartenderStop}
	}
	if s.Stage >= GobdipGaveBrown {
		// The brown mail is handed over; the generals' next greeting finishes the quest.
		return Step{Kind: StepTalk, Stop: GoblinGeneralsStop}
	}
	need := gobdipNeeds(s.Stage)
	held := s.HeldID

	// A full pack banks first, before the pantry draws anything, and it banks EVERYTHING but the quest's own items: the food
	// and the run staller's kit go too (Step.Bare). The keep list used to name every food id and the deposit ran the ordinary
	// way, which keeps Deps.KeepInPack's kit as well: an account that arrived with the hold's eight food, the trainer's
	// leftovers and the staller's four kit items (17 food + 4 kit + 8 quest items = 0 free) banked nothing, the same step came
	// round every tick and the engine parked it after eight (live 17 Oct 2026: 22 "bank all but 23 kept item type(s)" parks,
	// each ten slot-minutes and a six hour retry). The goblins are level 5 and hit for 2 at most, so the fights need no food;
	// the kit comes back at the next ordinary bank visit (the same design as the Waterfall and Merlin strips).
	if s.FreeSlots < 3 {
		return Step{Kind: StepDeposit, KeepIDs: gobdipTools, Bare: true, Bank: gobdipBank(s), BankSet: true,
			Reason: "the pack is full: everything but the quest's items goes, food and kit included"}
	}
	if !s.Ran["provision:"+KitGobdipPantry] && !s.Failed["provision:"+KitGobdipPantry] {
		return Step{Kind: StepProvision, KitName: KitGobdipPantry, Kit: gobdipPantryKit(need), KeepIDs: gobdipKeep(),
			Bank: gobdipBank(s), BankSet: true, Reason: "whatever the bank or the mule holds of the dyes, the mail and their ingredients"}
	}

	pendOrange := need.Orange && held(ObjGoblinMailOrange) == 0
	pendBlue := need.Blue && held(ObjGoblinMailBlue) == 0
	needOrangeDye := pendOrange && held(ObjOrangeDye) == 0
	needBlueDye := pendBlue && held(ObjBlueDye) == 0
	needRed := needOrangeDye && held(ObjRedDye) == 0
	needYellow := needOrangeDye && held(ObjYellowDye) == 0

	// The ingredients first: one trip to Aggie then makes every dye.
	if needRed && held(ObjRedberries) < GobdipBerriesPerDye {
		if s.Failed["provision:"+KitGobdipBerries] {
			return Step{Kind: StepWait, Reason: "no redberries from the bank, the mule, the berry patch or Wydin"}
		}
		return Step{Kind: StepProvision, KitName: KitGobdipBerries, Kit: gobdipBerriesKit(), KeepIDs: gobdipKeep(),
			Bank: gobdipBank(s), BankSet: true, Reason: "three redberries for the red dye"}
	}
	if needYellow && held(ObjOnion) < GobdipOnionsPerDye {
		return Step{Kind: StepPickLoc, LocID: LocOnionPlant, Loc: "Onion", Op: "Pick", Anchor: RimmingtonOnion,
			ExpectItemID: ObjOnion, ExpectItem: "Onion"}
	}
	if needBlueDye && held(ObjWoadLeaf) < GobdipWoadPerDye {
		if have := held(CoinsID); have < GobdipWoadFee {
			return gobdipCoinWait("Wyson's woad leaves", GobdipWoadFee, have)
		}
		return Step{Kind: StepTalk, Stop: WysonStop, UntilItemID: ObjWoadLeaf}
	}

	// Aggie's dyes, one visit's worth.
	if needRed || needYellow || needBlueDye {
		if have := held(CoinsID); have < GobdipDyeFee {
			return gobdipCoinWait("Aggie's next dye", GobdipDyeFee, have)
		}
		switch {
		case needRed:
			return Step{Kind: StepTalk, Stop: AggieRedStop, UntilItemID: ObjRedDye}
		case needYellow:
			return Step{Kind: StepTalk, Stop: AggieYellowStop, UntilItemID: ObjYellowDye}
		default:
			return Step{Kind: StepTalk, Stop: AggieBlueStop, UntilItemID: ObjBlueDye}
		}
	}
	if needOrangeDye {
		// Both halves are in hand: the red and the yellow mix to the orange (dye_cape.rs2 craft_dyes).
		return Step{Kind: StepUseOn, ItemID: ObjRedDye, Item: "Red dye", TargetKind: TargetItem, TargetID: ObjYellowDye, Target: "Yellow dye"}
	}

	// The plain mail: one for each dye still to be used and one for the brown hand-over.
	plainNeeded := b2i(pendOrange) + b2i(pendBlue) + b2i(need.Brown)
	if held(ObjGoblinMail) < plainNeeded {
		if s.Failed["provision:"+KitGobdipMail] {
			return Step{Kind: StepWait, Reason: "no goblin mail from the bank, the mule or the Goblin Village's goblins"}
		}
		return Step{Kind: StepProvision, KitName: KitGobdipMail, Kit: gobdipMailKit(plainNeeded), KeepIDs: gobdipKeep(),
			Bank: gobdipBank(s), BankSet: true, Reason: strconv.Itoa(plainNeeded) + " goblin mail: the bank, the mule, then the village goblins"}
	}

	// The dyes on the mail (the plain mail is all in hand, so neither dye can eat the brown one).
	if pendOrange {
		return Step{Kind: StepUseOn, ItemID: ObjOrangeDye, Item: "Orange dye", TargetKind: TargetItem, TargetID: ObjGoblinMail, Target: "Goblin mail"}
	}
	if pendBlue {
		return Step{Kind: StepUseOn, ItemID: ObjBlueDye, Item: "Blue dye", TargetKind: TargetItem, TargetID: ObjGoblinMail, Target: "Goblin mail"}
	}
	return Step{Kind: StepTalk, Stop: GoblinGeneralsStop}
}

// GoblinDiplomacyRecord is the quest's identity. The server gates nothing.
var GoblinDiplomacyRecord = Record{ID: GoblinDiplomacyID, Name: GoblinDiplomacyName, QuestPoints: 5}

// GobdipPastCrowd reports the account past the quest's two crowded things, the ground the redberries spawn on (three spawns
// for a fleet of accounts) and the Goblin Village's single-combat goblins that drop the plain mail: it needs no berries and
// holds all the plain mail it will need, or the quest is at its last hand-over or done. What is left is the onions, the woad,
// Aggie's dyes, the dyes on the mail and the generals, which no other account is waiting for.
//
// It is Module.ExclusiveDone, so the fleet-wide slot (twelve holders) is given up here and not at the end of the run, and an
// account that arrives holding what it needs never queues for it again (questrunner.HintSlotDone). It reads the same facts
// DecideGoblinDiplomacy does: a reading it cannot make (an unread journal, an uncoloured tab) is not "past".
func GobdipPastCrowd(s Snapshot) bool {
	switch s.Journal {
	case StatusComplete:
		return true
	case StatusInProgress:
	default:
		return false
	}
	if !s.StageKnown || s.Stage <= GobdipNotStarted {
		return false
	}
	if s.Stage >= GobdipGaveBrown {
		return true
	}
	need := gobdipNeeds(s.Stage)
	held := s.HeldID
	pendOrange := need.Orange && held(ObjGoblinMailOrange) == 0
	pendBlue := need.Blue && held(ObjGoblinMailBlue) == 0
	needRed := pendOrange && held(ObjOrangeDye) == 0 && held(ObjRedDye) == 0
	if needRed && held(ObjRedberries) < GobdipBerriesPerDye {
		return false // the berry patch is still to do
	}
	return held(ObjGoblinMail) >= b2i(pendOrange)+b2i(pendBlue)+b2i(need.Brown)
}

// GoblinDiplomacy is the engine module. Two deaths park it for a day: the only fight is the village goblins (level 5, 12
// hitpoints, a max hit of 2) and an account that dies to them is not one to leave there.
var GoblinDiplomacy = Module{
	Record: GoblinDiplomacyRecord, Bank: DraynorBank, BankSet: true, Anchor: RustyAnchorBartender,
	ToolIDs:       gobdipTools,
	ReadStage:     ParseGoblinDiplomacyJournal,
	Sustain:       Sustain{FoodIDs: gobdipFoodIDs, EatAt: 8},
	MaxDeaths:     2,
	Decide:        DecideGoblinDiplomacy,
	ExclusiveDone: GobdipPastCrowd,
}
