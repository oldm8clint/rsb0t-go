package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Prince Ali Rescue (content id `prince`, %princequest and %prince_keystatus, neither transmitted), ported from
// defs/princeali/*.ts and checked against content 289 (quests/quest_prince and areas/area_alkharid/scripts/{hassan,osman,
// border_gate}.rs2, areas/area_draynor/scripts/{leela,ned,aggie,joe_guard,lady_keli,prince_ali}.rs2, general_use/scripts/
// water_sources.rs2, skill_crafting/scripts/pottery, skill_firemaking/scripts/firemaking.rs2).
//
// The quest is a chain of eight hand-overs round one disguise, three quest points and 700 coins at the end:
//
//	0   Hassan, in Al-Kharid palace, "Can I help you? You must need some help here in the desert."      -> 10
//	10  Osman the spymaster, who lists the two jobs; "Okay, I better go find some things."              -> 20
//	20  Leela, outside Draynor, once the pack holds all four of: the key, a blonde wig, a pink skirt
//	    and skin paste (leela.rs2 promotes only on the sum)                                                -> 30
//	30  Joe, the door guard, three beers in the pack at once (joe_beer deletes 1, then 2)                  -> 40
//	40  a rope used on Lady Keli (opnpcu: she is deleted for a hundred ticks)                              -> 50
//	50  the key on the Prison Door from the north side, Keli gone, then the disguise and the key to
//	    Prince Ali in the cell (prince_rescue wants all four in the pack)                                   -> 100
//	100 Hassan again, for the 700 coins (the Al-Kharid toll gate is free from here on)                     -> 110
//
// What each piece of the disguise comes from, in the owner's item rule (provision.go: own bank, one bundled mule ask,
// ground spawns, own skills, shops), the mule's published bank (17 Oct 2026) being the first stop for most of it:
//
//   - The BLONDE WIG is three balls of wool to Ned in Draynor ("Ned, could you make other things from wool?", "How about some
//     sort of wig?", "I have that now. Please, make me a wig."), a plain wig that is untradeable, dyed with a yellow dye
//     (opheldu on the plain wig). Yellow dye is two onions and 5 coins to Aggie. The wool is the Sheep Shearer module's own
//     (the Lumbridge pen and the castle wheel, proven live).
//   - The PINK SKIRT is Thessalia's shelf in Varrock (the mule keeps fifteen).
//   - The SKIN PASTE is one pot of flour, one redberry, one bucket or jug of water and a pile of ashes to Aggie ("Could you think
//     of a way to make skin paste?" appears only while the quest is in progress). Ashes have no shop and no spawn: a fire
//     lit from logs (the two log spawns at 3089,3265, the mule has seven) burns out after 100 to 200 ticks and leaves them on
//     the ground for 100 more (firemaking.rs2 firemaking_success).
//   - The KEY is a print of Lady Keli's key in soft clay ("Heard of you?", "What is your latest plan then?", "Can you be sure
//     they will not try to get him out?", "Could I see the key please?", "Could I touch the key for a moment?"; only at stage
//     20 and only with soft clay in the pack), which Osman turns into a key with a bronze bar (keystatus 0 -> 1) and Leela hands
//     over (1 -> 2). Soft clay is clay and a jug, bucket or bowl of water (pottery.rs2). A lost key costs 15 coins from Osman
//     or from Leela, but Leela issues none while the BANK holds one (leela_help checks inv and bank), so a banked key is
//     drawn first, exactly as Juliet's message is.
//   - A ROPE is 15 coins from Ned ("Yes, I would like some rope.", "Okay, please sell me some rope."). Two before Keli is tied: she
//     comes back a hundred ticks later and the door will not open with her within ten tiles.
//   - The BEERS are the Rusty Anchor's, 2 coins each, one a talk (the mule keeps thirteen thousand).
//   - The mule waits in East Ardougne, so after the pantry's ask an account stands a few steps from Aemad's Adventurers' Store, which
//     shelves balls of wool, rope, a tinderbox and a bronze pickaxe: one pass over it (princeLegAemad) replaces the Lumbridge pen and
//     wheel, Ned's rope and the tinderbox and pickaxe walks, and only for an account that stands nearer his shop than Ned's.
//
// What a script must plan for:
//   - The Al-Kharid toll gate costs 10 coins each way until the quest is rescued, and the walker will not plan a crossing it
//     cannot pay for, so the purse comes first, ahead of every step that walks anywhere (the TS def's rule).
//   - Four level-26 Jail guards (npc 917, huntmode aggressive_melee: they hunt every combat level) stand round the compound at
//     3109,3237 3120,3238 3121,3249 and 3127,3248. Two of them reach Joe, the yard door and Keli (Keli, like the guards, has no
//     wanderrange in her config, so the engine's default of 5 lets both of the northern guards reach her stand), and the whole
//     of the print at Keli, the beers and the break-out is made under their blows. Their hits are small (a maximum of 3 every five
//     ticks), the dialogues are short and the cell, once entered, is closed to them, so the module prices each of the three
//     visits (PrinceVisitOdds, through princeCompound) against the account's Defence and the hitpoints it goes in with, and parks
//     instead of walking in when the model says it dies. Nothing can be eaten inside a chat (the engine's sustain waits for every
//     modal to close, and an inventory click while one is open aborts the script), so only the top-up before the visit counts.
//   - Dying drops the untradeable pieces (wig, paste, key), so the legs that make them run at every stage before the rescue.
//   - Keli and the prince are single npcs (npc_del removes them for every player for a hundred ticks), so the quest runs under a
//     small fleet-wide slot (questrunner/slots.go).
//   - The cell is closed to the guards but the door opens only from the north with the key; once rescued the account leaves it
//     by the same door from inside (oploc1 opens it from z <= 3243), which the walker does not know.

// PrinceAliName is the quest tab's spelling (component 7342, `questlist:prince`).
const PrinceAliName = "Prince Ali Rescue"

// PrinceAliID is the engine's id.
const PrinceAliID = "prince"

// Stages, from quest_prince.constant and general/configs/quest.constant (^prince_*), %princequest.
const (
	PrinceNotStarted   = 0
	PrinceStarted      = 10
	PrinceSpokeOsman   = 20
	PrinceHasEquipment = 30
	PrinceGuardDrunk   = 40
	PrinceTiedKeli     = 50
	PrinceSaved        = 100
	PrinceComplete     = 110
)

// Ids from content/pack (checked against the production config by TestPrinceAliIDsAreWhatContent289CallsThem).
const (
	NpcHassan          = 923
	NpcOsman           = 924
	NpcLeela           = 915
	NpcJoe             = 916
	NpcLadyKeli        = 919
	NpcPrinceAliPrison = 920
	NpcJailGuard       = 917
	NpcShantay         = 836
	ObjPrinceKey       = 2418 // [princeskey] "Bronze key"
	ObjBlondWig        = 2419 // [blondwig] "Wig"
	ObjPlainWig        = 2421 // [plainwig] "Wig"
	ObjKeyPrint        = 2423 // [keyprint]
	ObjSkinPaste       = 2424 // [skinpaste] "Paste"
	ObjPinkSkirt       = 1013
	ObjRope            = 954
	ObjSoftClay        = 1761
	ObjClay            = 434
	ObjJugOfWater      = 1937
	ObjBucketOfWater   = 1929
	ObjAshes           = 592
	ObjBronzeBar       = 2349
	LocPrisonDoor      = 2881 // [alidoor] "Prison Door", op1 Open, shape 0 (a straight wall), angle 1 at 3123,3243
	LocClayRockA       = 2108 // [clayrock1] "Rocks", Mine
	LocClayRockB       = 2109 // [clayrock2]
)

// What the quest costs and needs, from the scripts.
const (
	// PrinceBeers is the beers joe_beer takes in one conversation (1, then 2 more when two remain).
	PrinceBeers = 3
	// PrinceBalls is the balls of wool Ned takes for the wig.
	PrinceBalls = 3
	// PrinceOnions is the onions one yellow dye takes.
	PrinceOnions = 2
	// PrinceRopesBeforeTie and PrinceRopesAfterTie: one to tie Keli and one spare, because she respawns a hundred ticks later
	// and the door will not open while she is within ten tiles of the account; after the tie the spare is the only one needed.
	PrinceRopesBeforeTie = 2
	PrinceRopesAfterTie  = 1
	// PrinceRopeFee, PrinceDyeFee and PrinceKeyFee are the coins Ned, Aggie and Osman or Leela take.
	PrinceRopeFee = 15
	PrinceDyeFee  = 5
	PrinceKeyFee  = 15
	// PrinceCoinFloor is the purse below which a coin draw comes before any step that walks (the toll gate is ten coins each way,
	// the shops ~60, two ropes 30, the dye, a lost key 15); PrinceCoinFloat is what the draw asks for.
	PrinceCoinFloor = 120
	PrinceCoinFloat = 250
)

// Tiles from content maps (npc spawns m51_49 and m48_50; obj spawns m48_51 and m46_50) and the loc index.
var (
	// HassanAnchor is Hassan in Al-Kharid palace (3302,3163) and OsmanAnchor the spymaster (3286,3180).
	HassanAnchor = tile.Tile{X: 3302, Z: 3163}
	OsmanAnchor  = tile.Tile{X: 3286, Z: 3180}
	// LeelaAnchor is Leela outside Draynor (3113,3263); Ned (3100,3258) is NedAnchor, Dragon Slayer's.
	LeelaAnchor = tile.Tile{X: 3113, Z: 3263}
	// JoeAnchor is the door guard (3123,3245) and KeliAnchor Lady Keli (3128,3244), inside the compound's yard.
	JoeAnchor  = tile.Tile{X: 3123, Z: 3245}
	KeliAnchor = tile.Tile{X: 3128, Z: 3244}
	// PrinceAliAnchor is Prince Ali in his cell (3123,3242).
	PrinceAliAnchor = tile.Tile{X: 3123, Z: 3242}
	// PrisonDoorTile is the Prison Door (3123,3243): the cell's north row, with the wall on its north face. PrisonDoorStand is the
	// tile north of it, the only side oplocu accepts the key from (coordz(coord) > coordz(loc_coord)).
	PrisonDoorTile  = tile.Tile{X: 3123, Z: 3243}
	PrisonDoorStand = tile.Tile{X: 3123, Z: 3244}
	// PrinceCell is the cell's floor, x 3122-3124, z 3240-3243: closed to the guards once the door is shut behind the account.
	PrinceCell = Rect{MinX: 3122, MaxX: 3124, MinZ: 3240, MaxZ: 3243}
	// ShantayAnchor is Shantay at the desert pass (3304,3123): jugs and buckets of water and bronze bars on his shelf.
	ShantayAnchor = tile.Tile{X: 3304, Z: 3123}
	// ClayRocks is the clay near Rimmington (2986,3239).
	ClayRocks = tile.Tile{X: 2986, Z: 3239}
	// PrinceLogSpawns are the two log spawns by Draynor's market (m48_51), and PrinceFireSite where the fire is lit.
	PrinceLogSpawns = []tile.Tile{{X: 3089, Z: 3265}, {X: 3089, Z: 3266}}
	PrinceFireSite  = tile.Tile{X: 3089, Z: 3265}
	// PrincePickaxeSpawn is the bronze pickaxe that lies on the ground west of Port Sarim (m46_50).
	PrincePickaxeSpawn = tile.Tile{X: 2963, Z: 3216}
	// ShantayShop is Shantay (op3 Trade).
	ShantayShop = ShopRef{Npc: "Shantay", NpcID: NpcShantay, Anchor: ShantayAnchor}
)

// InPrinceCell reports whether t is on the cell's floor.
func InPrinceCell(t tile.Tile) bool { return PrinceCell.Contains(t) }

// FoeJailGuard is the compound's guard: quests/../area_draynor/configs/draynor.npc [jailguard], level 26, attack 19, defence 21,
// strength 23, 32 hitpoints, attackrate 5, attackbonus 9, strengthbonus 5 (a maximum hit of 3), huntmode aggressive_melee.
var FoeJailGuard = MeleeFoe{Name: "Jail guard", Attack: 19, Strength: 23, Defence: 21, Magic: 1, Hitpoints: 32,
	AttackTicks: 5, AttackBonus: 9, StrengthBonus: 5}

// The compound's exposure, for PrinceVisitOdds.
const (
	// PrinceGuardsInRange is how many guards reach Joe, the yard and the door at once: those at 3121,3249 and 3127,3248.
	PrinceGuardsInRange = 2
	// PrinceBeerVisitTicks, PrinceKeliPrintVisitTicks and PrinceBreakoutVisitTicks are the ticks one visit spends in their reach,
	// the walks in and out included, from the dialogues' length (joe_beer is twelve pages and one menu, Keli's print eighteen pages
	// and five menus, prince_rescue ten) with a margin. The print is the longest chat of the three, and the first visit of every run.
	PrinceBeerVisitTicks      = 60
	PrinceKeliPrintVisitTicks = 100
	PrinceBreakoutVisitTicks  = 100
	// PrinceOddsPark is how long a quest whose visit the model refuses is parked: long enough for hitpoints to come back and a
	// food kit to reach the bank.
	PrinceOddsPark = 3 * time.Hour
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomPrinceKey      = "prince-key"
	CustomPrincePrint    = "prince-print"
	CustomPrinceAshes    = "prince-ashes"
	CustomPrinceClay     = "prince-clay"
	CustomPrinceRope     = "prince-rope"
	CustomPrinceBeerBuy  = "prince-beer-buy"
	CustomPrinceBeer     = "prince-beer"
	CustomPrinceBreakout = "prince-breakout"
	CustomPrinceLeave    = "prince-leave"

	KitPrinceCoins   = "prince-coins"
	KitPrincePantry  = "prince-pantry"
	KitPrinceAemad   = "prince-aemad"
	KitPrinceWater   = "prince-water"
	KitPrinceBar     = "prince-bar"
	KitPrinceFlour   = "prince-flour"
	KitPrinceBerries = "prince-berries"
	KitPrinceSkirt   = "prince-skirt"
	KitPrinceShears  = "prince-shears"
	KitPrincePickaxe = "prince-pickaxe"
	KitPrinceFire    = "prince-fire"
	KitPrinceFood    = "prince-food"
)

// princeFoodIDs are the foods the account may carry into the compound, best first.
var princeFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// princePickaxeIDs are the pickaxes mining clay accepts (pickaxe_checker.rs2), held or worn.
var princePickaxeIDs = []int{1265, 1267, 1269, 1271, 1273, 1275}

// princeTools are the quest's own items, which no bank visit of the quest may leave in the bank: every piece of the
// disguise and the key, and what makes them. The trainer banks them between two ladder passes all the same, which the pantry
// draws back out.
var princeTools = append([]int{CoinsID, ObjPrinceKey, ObjBlondWig, ObjPlainWig, ObjKeyPrint, ObjSkinPaste, ObjPinkSkirt, ObjRope,
	ObjBeer, ObjSoftClay, ObjClay, ObjJugOfWater, ObjBucketOfWater, ObjAshes, ObjBronzeBar, ObjPotOfFlour, ObjRedberries,
	ObjYellowDye, ObjOnion, ObjBallOfWool, ObjWool, ObjShears, ObjTinderbox, ObjLogs}, princePickaxeIDs...)

// princeKeep is a bank visit's keep list: the tools and the food.
func princeKeep() []int { return append(append([]int(nil), princeTools...), princeFoodIDs...) }

var (
	// HassanStartStop starts the quest: the first menu is "Can I help you? You must need some help here in the desert." / "It's
	// just too hot here. How can you stand it?" / "Do you mind if I just kill your warriors?".
	HassanStartStop = NpcStop{Npc: "Hassan", ID: NpcHassan, Anchor: HassanAnchor, Leash: 6,
		Prefer: []string{"Can I help you? You must need some help here in the desert."}}
	// HassanRewardStop pays the 700 coins at stage 100: no menu (the preference is the placeholder talk steps require).
	HassanRewardStop = NpcStop{Npc: "Hassan", ID: NpcHassan, Anchor: HassanAnchor, Leash: 6,
		Prefer: []string{"Can I help you? You must need some help here in the desert."}}
	// OsmanBriefStop is the brief: the first menu is "What is the first thing I must do?" / "What is the second thing you need?", the
	// second (after the second thing) "What is the first thing I must do?" / "What exactly is the second thing you need?" / "Okay, I
	// better go find some things." The exit goes first because osman_second_thing offers the first branch again and taking it
	// loops; the "second thing" line is the way to the exit.
	OsmanBriefStop = NpcStop{Npc: "Osman", ID: NpcOsman, Anchor: OsmanAnchor, Leash: 6,
		Prefer: []string{"Okay, I better go find some things.", "What is the second thing you need?"}}
	// OsmanForgeStop turns the print and a bronze bar into the key; its menu ends the conversation with "Thank you. I will try to
	// find the other items."
	OsmanForgeStop = NpcStop{Npc: "Osman", ID: NpcOsman, Anchor: OsmanAnchor, Leash: 6,
		Prefer: []string{"Thank you. I will try to find the other items."}}
	// LeelaStop hands the key over and, with the whole disguise in the pack at stage 20, promotes the quest. At stage 30 she asks
	// for the plan for the guard (the third line); with the pack short she lists the jobs (the last line ends it).
	LeelaStop = NpcStop{Npc: "Leela", ID: NpcLeela, Anchor: LeelaAnchor, Leash: 6,
		Prefer: []string{"I hoped to get him drunk.", "I will go and get the rest of the escape equipment."}}
	// NedWigStop asks for the wig ("Ned, could you make other things from wool?" appears only while the quest is in progress).
	NedWigStop = NpcStop{Npc: "Ned", ID: NpcNed, Anchor: NedAnchor, Leash: 6,
		Prefer: []string{"Ned, could you make other things from wool?", "How about some sort of wig?", "I have that now. Please, make me a wig."}}
	// NedRopeStop buys a rope; "Okay, please sell me some rope." must outrank his offer to spin four balls of wool.
	NedRopeStop = NpcStop{Npc: "Ned", ID: NpcNed, Anchor: NedAnchor, Leash: 6,
		Prefer: []string{"Yes, I would like some rope.", "Okay, please sell me some rope."}}
	// AggiePasteStop mixes the paste once the pack holds all four ingredients.
	AggiePasteStop = NpcStop{Npc: "Aggie", ID: NpcAggie, Anchor: AggieAnchor, Leash: 6,
		Prefer: []string{"Could you think of a way to make skin paste?", "Yes please. Mix me some skin paste."}}
	// KeliPrintStop walks Keli's menus to the key and takes the print (the last line is offered only with soft clay in the pack).
	KeliPrintStop = NpcStop{Npc: "Lady Keli", ID: NpcLadyKeli, Anchor: KeliAnchor, Leash: 8,
		Prefer: []string{"Heard of you? You are famous in RuneScape!", "What is your latest plan then?",
			"Can you be sure they will not try to get him out?", "Could I see the key please?", "Could I touch the key for a moment?"}}
	// JoeBeerStop gives the three beers.
	JoeBeerStop = NpcStop{Npc: "Joe", ID: NpcJoe, Anchor: JoeAnchor, Leash: 6,
		Prefer: []string{"I have some beer here, fancy one?"}}
	// PrinceAliStop is the prince in his cell: no menu (the preference is the placeholder the driver requires).
	PrinceAliStop = NpcStop{Npc: "Prince Ali", ID: NpcPrinceAliPrison, Anchor: PrinceAliAnchor, Leash: 6,
		Prefer: []string{"Take this disguise, and this key."}}
	// BartenderBeerStop buys a beer from the Rusty Anchor's bartender for 2 coins.
	BartenderBeerStop = NpcStop{Npc: "Bartender", ID: NpcRustyAnchorBartender, Anchor: RustyAnchorBartender, Leash: 8,
		Prefer: []string{"Could I buy a beer please?"}}
)

// princeJournalNeedles are ParsePrinceAliJournal's phrases, newest first, written against NormaliseLines
// (prince_journal.rs2; finished lines are struck through but kept, so the later stages contain the earlier ones' phrases and
// only the order makes the answer right). None spans a colour tag next to punctuation.
var princeJournalNeedles = []struct {
	stage  int
	needle string
}{
	{PrinceComplete, "quest complete"},
	{PrinceSaved, "i then used a wig, a skirt and some skin paste"},
	{PrinceTiedKeli, "i used my rope to tie up lady"},
	{PrinceGuardDrunk, "i also had to prevent the guard from seeing what i was up"},
	{PrinceHasEquipment, "i need to deal with the guard"},
	{PrinceSpokeOsman, "for advice"},
	{PrinceStarted, "i should go and speak to osman"},
	{PrinceNotStarted, "i can start this quest by speaking to hassan"},
}

// ParsePrinceAliJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised journal, which is
// unknown, never stage 0.
func ParsePrinceAliJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range princeJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// princeBank is the bank a provisioning or deposit visit uses: the one nearest the account, except that Al-Kharid's is behind the
// toll gate for an account that is not inside it (an account in Lumbridge banks at Draynor, as the other fillers do).
func princeBank(s Snapshot) tile.Tile {
	if !s.TileKnown {
		return DraynorBank
	}
	b := NearestBank(s.Tile)
	if b.Name == "Al Kharid" && !princeInAlKharid(s.Tile) {
		return DraynorBank
	}
	return b.Tile
}

// princeInAlKharid reports whether a surface tile is inside Al-Kharid, east of the toll gate fence at x 3268.
func princeInAlKharid(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 3268 && t.X < 3340 && t.Z >= 3116 && t.Z < 3230
}

// princeHasPickaxe reports whether the pack or the worn items hold a pickaxe.
func princeHasPickaxe(s Snapshot) bool {
	for _, id := range princePickaxeIDs {
		if s.InvIDs[id] > 0 || s.WornIDs[id] > 0 {
			return true
		}
	}
	return false
}

// princeRopesWanted is the ropes the account carries at a stage: two until Keli is tied, one after.
func princeRopesWanted(stage int) int {
	switch {
	case stage >= PrinceSaved:
		return 0
	case stage >= PrinceTiedKeli:
		return PrinceRopesAfterTie
	}
	return PrinceRopesBeforeTie
}

// princeFacts is what the pack and the stage say is still to do, read once per decision.
type princeFacts struct {
	stage                            int
	haveKey, keyIssued               bool
	haveWig, haveSkirt, havePaste    bool
	needPrint, needSoft, needClay    bool
	waterWanted, waterHeld           int
	balls, wool, onions              int
	plain, yellow, flour, berries    int
	ashes, logs, tinderbox, softClay int
	clay, bar, print, rope, beers    int
}

func princeRead(s Snapshot) princeFacts {
	h := s.HeldID
	f := princeFacts{stage: s.Stage}
	f.haveKey = h(ObjPrinceKey) > 0
	f.keyIssued = f.stage >= PrinceHasEquipment
	f.haveWig = h(ObjBlondWig) > 0
	f.haveSkirt = h(ObjPinkSkirt) > 0
	f.havePaste = h(ObjSkinPaste) > 0
	f.balls, f.wool, f.onions = h(ObjBallOfWool), h(ObjWool), h(ObjOnion)
	f.plain, f.yellow = h(ObjPlainWig), h(ObjYellowDye)
	f.flour, f.berries = h(ObjPotOfFlour), h(ObjRedberries)
	f.ashes, f.logs, f.tinderbox = h(ObjAshes), h(ObjLogs), h(ObjTinderbox)
	f.softClay, f.clay, f.bar, f.print = h(ObjSoftClay), h(ObjClay), h(ObjBronzeBar), h(ObjKeyPrint)
	f.rope, f.beers = h(ObjRope), h(ObjBeer)
	// The print is still to be made while no key exists and no print is held; the soft clay and the clay it takes follow.
	f.needPrint = !f.haveKey && !f.keyIssued && f.print == 0
	f.needSoft = f.needPrint && f.softClay == 0
	f.needClay = f.needSoft && f.clay == 0
	f.waterWanted = b2i(f.needSoft) + b2i(!f.havePaste)
	f.waterHeld = h(ObjJugOfWater) + h(ObjBucketOfWater)
	return f
}

// princeWait is a wait step that names what is missing and why nothing can supply it.
func princeWait(format string, args ...any) Step {
	return Step{Kind: StepWait, Reason: fmt.Sprintf(format, args...)}
}

// princeProvision is a provisioning step of the quest: the tools and the food stay in the pack, the bank is the nearest one.
func princeProvision(s Snapshot, name string, kit []KitLine, reason string) Step {
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: princeKeep(), Bank: princeBank(s), BankSet: true, Reason: reason}
}

// princeKit runs a gap kit once and waits when it failed: the pantry has already tried the bank and the mule, the kit's own
// sources are all there is, and a quest does not walk on to a step it cannot do.
func princeKit(s Snapshot, name string, kit []KitLine, reason, missing string) Step {
	if s.Failed["provision:"+name] {
		return princeWait("%s: nothing gave it (the bank, the mule and the sources of kit %s all failed)", missing, name)
	}
	return princeProvision(s, name, kit, reason)
}

// ---------------------------------------------------------------------------------------------------------------
// Kits.
// ---------------------------------------------------------------------------------------------------------------

// princeCoinsKit is the purse: the bank and the mule are the only sources of coins.
func princeCoinsKit() []KitLine {
	return []KitLine{{ID: CoinsID, Name: "Coins", Qty: PrinceCoinFloat, Stackable: true}}
}

// princePantryKit is the kit of the first pass: every item of the quest the account may already own and does not hold,
// wherever the owner's rule finds it first (its bank, then the mule). Every line is optional and none has a source of its own, so
// the pass reads the bank, draws what is there, makes the one mule ask, and is satisfied: it never walks to a shop or a spawn.
// The quest items (the key, the print, the wigs, the paste) are untradeable, so the mule is never asked for them.
func princePantryKit(f princeFacts) []KitLine {
	var kit []KitLine
	add := func(id int, name string, qty int, untradeable bool) {
		if qty > 0 {
			kit = append(kit, KitLine{ID: id, Name: name, Qty: qty, Optional: true, Untradeable: untradeable})
		}
	}
	// The key and the print: a banked key blocks Leela's replacement, so it is drawn before anything is made again.
	if !f.haveKey {
		add(ObjPrinceKey, "Bronze key", 1, true)
	}
	if f.needPrint {
		add(ObjKeyPrint, "Key print", 1, true)
		if f.needSoft {
			add(ObjSoftClay, "Soft clay", 1, false)
		}
		if f.needClay {
			add(ObjClay, "Clay", 1, false)
		}
	}
	if !f.haveKey && !f.keyIssued && f.bar == 0 {
		add(ObjBronzeBar, "Bronze bar", 1, false)
	}
	if need := f.waterWanted - f.waterHeld; need > 0 {
		add(ObjBucketOfWater, "Bucket of water", need, false)
		add(ObjJugOfWater, "Jug of water", need, false)
	}
	if !f.havePaste {
		add(ObjSkinPaste, "Paste", 1, true)
		if f.flour == 0 {
			add(ObjPotOfFlour, "Pot of flour", 1, false)
		}
		if f.berries == 0 {
			add(ObjRedberries, "Redberries", 1, false)
		}
		if f.ashes == 0 {
			add(ObjAshes, "Ashes", 1, false)
			if f.logs == 0 {
				add(ObjLogs, "Logs", 1, false)
			}
			if f.tinderbox == 0 {
				add(ObjTinderbox, "Tinderbox", 1, false)
			}
		}
	}
	if !f.haveWig {
		add(ObjBlondWig, "Wig", 1, true)
		if f.plain == 0 {
			add(ObjPlainWig, "Wig", 1, true)
			add(ObjBallOfWool, "Ball of wool", PrinceBalls-f.balls, false)
		}
		if f.yellow == 0 {
			add(ObjYellowDye, "Yellow dye", 1, false)
			add(ObjOnion, "Onion", PrinceOnions-f.onions, false)
		}
	}
	if !f.haveSkirt {
		add(ObjPinkSkirt, "Pink skirt", 1, false)
	}
	add(ObjRope, "Rope", princeRopesWanted(f.stage)-f.rope, false)
	if f.stage < PrinceGuardDrunk {
		add(ObjBeer, "Beer", PrinceBeers-f.beers, false)
	}
	return kit
}

// princeWaterKit is the jugs of water Shantay's shelf holds ten of (shantayshop stock3), the bucket and the jug being
// equal to soft clay and to Aggie.
func princeWaterKit(jugs int) []KitLine {
	return []KitLine{{ID: ObjJugOfWater, Name: "Jug of water", Qty: jugs, Sources: []Source{
		{Kind: SourceShop, Shop: ShantayShop, Price: 2, Note: "Shantay keeps 10, restocking every 100 ticks"}}}}
}

// princeBarKit is a bronze bar from Shantay's shelf (stock10, ten of them).
func princeBarKit() []KitLine {
	return []KitLine{{ID: ObjBronzeBar, Name: "Bronze bar", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: ShantayShop, Price: 10, Note: "Shantay keeps 10"}}}}
}

// princeFlourKit is a pot of flour: Wydin's shelf is the only shop that stocks it on this side of the map (three, restocking
// one a minute).
func princeFlourKit() []KitLine {
	return []KitLine{{ID: ObjPotOfFlour, Name: "Pot of flour", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: WydinShop, Price: 15, Note: "Wydin's food store keeps 3, restocking one a minute"}}}}
}

// princeBerriesKit is one redberry: the ground spawns by the Varrock patch (the item rule's third rung) and Wydin's shelf, which
// holds one a minute. For an account that stands nearer Wydin than the patch (it has just bought the flour there) the shelf is
// the only source listed first.
func princeBerriesKit(s Snapshot) []KitLine {
	var src []Source
	if !ShopOnTheWay(s, WydinShop, RedberrySpawns[0]) {
		for _, t := range RedberrySpawns {
			src = append(src, Source{Kind: SourceGround, Tile: t, PerSpawn: 1, RespawnTicks: 100, Note: "the berry patch south-east of Varrock"})
		}
	}
	src = append(src, Source{Kind: SourceShop, Shop: WydinShop, Price: 3})
	return []KitLine{{ID: ObjRedberries, Name: "Redberries", Qty: 1, Sources: src}}
}

// princeSkirtKit is Thessalia's shelf in Varrock (five, restocking every 100 ticks).
func princeSkirtKit() []KitLine {
	return []KitLine{{ID: ObjPinkSkirt, Name: "Pink skirt", Qty: 1, Sources: []Source{
		{Kind: SourceShop, Shop: ThessaliaShop, Price: 20, Note: "Thessalia keeps 5"}}}}
}

// princeShearsKit is the shears for the Lumbridge pen: Fred's farm floor, else the general store.
func princeShearsKit() []KitLine { return sheepShearsKit() }

// princePickaxeKit is a pickaxe for the clay: the bronze one on the ground west of Port Sarim, else Bob's.
func princePickaxeKit() []KitLine {
	return []KitLine{{ID: ObjBronzePickaxe, Name: "Bronze pickaxe", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: PrincePickaxeSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "west of Port Sarim"},
		{Kind: SourceShop, Shop: BobsAxes, Price: 5},
	}}}
}

// princeFireKit is what the ashes burn from: a log (the two spawns by Draynor's market) and a tinderbox (the general store).
func princeFireKit(f princeFacts) []KitLine {
	var logs []Source
	for _, t := range PrinceLogSpawns {
		logs = append(logs, Source{Kind: SourceGround, Tile: t, PerSpawn: 1, RespawnTicks: 100, Note: "Draynor market"})
	}
	var kit []KitLine
	if f.logs == 0 {
		kit = append(kit, KitLine{ID: ObjLogs, Name: "Logs", Qty: 1, Sources: logs})
	}
	if f.tinderbox == 0 {
		kit = append(kit, KitLine{ID: ObjTinderbox, Name: "Tinderbox", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: LumbridgeStore, Price: 2}}})
	}
	return kit
}

// princeFoodKit is a little food for the compound: the bank and the mule only.
func princeFoodKit() []KitLine {
	return []KitLine{{ID: 385, Name: "Shark", Qty: 4, Optional: true}, {ID: 373, Name: "Swordfish", Qty: 4, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 4, Optional: true}, {ID: 365, Name: "Bass", Qty: 4, Optional: true},
		{ID: 361, Name: "Tuna", Qty: 4, Optional: true}}
}

// ---------------------------------------------------------------------------------------------------------------
// The compound's odds.
// ---------------------------------------------------------------------------------------------------------------

// PrinceVisitOdds prices one visit to the jail compound of ticks ticks: the two guards that reach Keli, the yard and the door hit at
// the expected rate of an account with this Defence (FoePerAttack, with the same quarter on top the quest fights use), and the
// visit must leave the account above four hits of both guards (and a quarter of its hitpoints).
//
// Only the hitpoints the account goes IN with count. Every visit is a chat or a chain of chats, and food cannot be eaten inside
// one: the engine's sustain waits for every modal to close, and the game closes a chat's script when an inventory item is clicked
// while it is open (OpHeldHandler's clearPendingAction, then closeModal on a script suspended on PAUSEBUTTON). So the account's
// food is credited only as the top-up the executor takes before the visit (PrepareForFight, to FightStartHP), never as hitpoints
// available during it; an account that is full and short of Defence is refused whatever it carries. Nothing is judged for an
// account whose hitpoints are unread.
func PrinceVisitOdds(s Snapshot, ticks int) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	f := FoeJailGuard
	per := FoePerAttack(f, fighterOf(s)) * FightIncomingScale
	dmg := float64(PrinceGuardsInRange) * float64(ticks) / float64(f.AttackTicks) * per
	hp := s.HP
	if hp <= 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	pool := FoodPoolHeal(s.InvIDs, princeFoodIDs)
	start := max(hp, min(FightStartHP(s.MaxHP, f.PeakHit()), hp+pool))
	left := float64(start) - dmg
	margin := float64(max(f.PeakHit()*PrinceGuardsInRange*2, s.MaxHP/4))
	if left < margin {
		return false, fmt.Sprintf("two jail guards (level 26, hits up to %d) cost an account at Defence %d about %.0f hitpoints in %d ticks and nothing can be eaten inside a chat; it goes in with %d of %d (%d in food to top up from), and the visit must leave %.0f",
			f.PeakHit(), s.Level("defence"), dmg, ticks, start, s.MaxHP, pool, margin)
	}
	return true, ""
}

// princeCompound is the entrance to a compound visit (the print at Keli, the beers, the break-out): a food kit when the model
// refuses it (food raises the hitpoints the account goes in with only when it is below the fight's starting level), the park when
// the food did not help, the visit's own custom step otherwise.
func princeCompound(s Snapshot, custom string, ticks int, qty int, reason string) Step {
	if ok, why := PrinceVisitOdds(s, ticks); !ok {
		if !s.Ran["provision:"+KitPrinceFood] && !s.Failed["provision:"+KitPrinceFood] {
			return princeProvision(s, KitPrinceFood, princeFoodKit(), "food for the jail guards: "+why)
		}
		return Step{Kind: StepWait, ParkFor: PrinceOddsPark, Reason: "not entering the Draynor jail compound: " + why}
	}
	if s.Failed[custom] {
		return princeWait("%s failed on its last try; it is not repeated at once", custom)
	}
	return Step{Kind: StepCustom, Custom: custom, Qty: qty, Reason: reason}
}

// ---------------------------------------------------------------------------------------------------------------
// The decision.
// ---------------------------------------------------------------------------------------------------------------

// princePurse is the coin draw that comes first: the toll gate is ten coins each way until the quest is rescued, the shops and
// Ned and Aggie want more, and the walker plans no crossing it cannot pay for.
func princePurse(s Snapshot) (Step, bool) {
	if s.HeldID(CoinsID) >= PrinceCoinFloor {
		return Step{}, false
	}
	if s.Failed["provision:"+KitPrinceCoins] {
		return princeWait("the toll gate, the shops, Ned and Aggie need %d coins (the pack holds %d) and the bank and the mule have no more", PrinceCoinFloor, s.HeldID(CoinsID)), true
	}
	return princeProvision(s, KitPrinceCoins, princeCoinsKit(), "the purse for the toll gate, the shops, Ned and Aggie"), true
}

// DecidePrinceAli is the whole quest.
func DecidePrinceAli(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return princeStart(s)
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	stage := s.Stage
	if stage >= PrinceComplete {
		return Step{Kind: StepDone}
	}
	if stage < PrinceSaved {
		if st, ok := princePurse(s); ok {
			return st
		}
	}
	switch {
	case stage <= PrinceNotStarted:
		return princeStart(s)
	case stage == PrinceStarted:
		return Step{Kind: StepTalk, Stop: OsmanBriefStop}
	case stage < PrinceSaved:
		return princeAssemble(s)
	}
	// Saved: the prince is out; the account may still stand in the cell, whose door only opens from inside. If the step that opens
	// it failed, the walker's graph knows the same door (a Prison Door hop from inside), so the walk to Hassan is its own way out.
	if s.TileKnown && InPrinceCell(s.Tile) && !s.Failed[CustomPrinceLeave] {
		return Step{Kind: StepCustom, Custom: CustomPrinceLeave, Reason: "out of the cell by the door the key opened"}
	}
	return Step{Kind: StepTalk, Stop: HassanRewardStop}
}

// princeStart is the first stop: the purse, then Hassan.
func princeStart(s Snapshot) Step {
	if st, ok := princePurse(s); ok {
		return st
	}
	return Step{Kind: StepTalk, Stop: HassanStartStop}
}

// princeLeg is one thing the account may still have to do before the quest's own stage work: a step, and whether there is one.
type princeLeg func(s Snapshot, f princeFacts) (Step, bool)

// princeLegs is the tour, top to bottom: Aemad's shelf if the pantry's mule ask has left the account in East Ardougne, the water and
// the paste's goods (Shantay, Wydin, the berry patch), the beers, the skirt (Varrock), the wool (the Lumbridge pen and wheel), the clay and the onions (Rimmington), the ashes (Draynor's
// market), then Aggie, Ned, the soft clay, Keli, the rope and the key. A leg that its item has already satisfied (the bank and
// the mule were drawn first, by the pantry) costs nothing, and a resumed run rejoins the tour wherever it left off.
var princeLegs = []princeLeg{
	princeLegAemad, princeLegWater, princeLegFlour, princeLegBerries, princeLegBeers, princeLegSkirt, princeLegWool, princeLegClay,
	princeLegOnions, princeLegAshes, princeLegYellowDye, princeLegWig, princeLegWigDye, princeLegPaste, princeLegSoftClay,
	princeLegPrint, princeLegRope, princeLegKey,
}

// princeAssemble is stages 20 to 50. The legs make and keep every piece of the disguise, the key, the rope and the beers (at
// every stage before the rescue: a death drops the untradeable pieces), then the stage's own talk or visit follows.
func princeAssemble(s Snapshot) Step {
	f := princeRead(s)
	if !s.Ran["provision:"+KitPrincePantry] && !s.Failed["provision:"+KitPrincePantry] {
		if kit := princePantryKit(f); len(kit) > 0 {
			return princeProvision(s, KitPrincePantry, kit, "whatever the bank or the mule holds of the key, the disguise and what makes them")
		}
	}
	if s.FreeSlots < 3 {
		return Step{Kind: StepDeposit, KeepIDs: princeKeep(), Bank: princeBank(s), BankSet: true, Reason: "room in the pack"}
	}
	for _, leg := range princeLegs {
		if st, ok := leg(s, f); ok {
			return st
		}
	}
	switch {
	case f.stage < PrinceHasEquipment:
		// Leela promotes only on the sum, so a quest with a piece short would talk to her for nothing.
		if !f.haveKey || !f.haveWig || !f.haveSkirt || !f.havePaste {
			return princeWait("every leg is satisfied but the disguise or the key is not in the pack (key %v, wig %v, skirt %v, paste %v)",
				f.haveKey, f.haveWig, f.haveSkirt, f.havePaste)
		}
		return Step{Kind: StepTalk, Stop: LeelaStop}
	case f.stage == PrinceHasEquipment:
		return princeCompound(s, CustomPrinceBeer, PrinceBeerVisitTicks, 0, "the three beers to Joe, the door guard")
	default:
		return princeCompound(s, CustomPrinceBreakout, PrinceBreakoutVisitTicks, b2i(f.stage >= PrinceTiedKeli),
			"tie Lady Keli, unlock the cell and free the prince")
	}
}

// princeAemadKit is what East Ardougne's Adventurers' Store (Aemad, content ardougne_east.inv [adventurershop]) shelves of the quest's
// goods: balls of wool (30), rope (20), a tinderbox and a bronze pickaxe (2 each), whichever the account still lacks. Every line is
// optional: a shelf that is empty only leaves the line to the leg that follows.
func princeAemadKit(s Snapshot, f princeFacts) []KitLine {
	var kit []KitLine
	add := func(id int, name string, qty, price int) {
		if qty > 0 {
			kit = append(kit, KitLine{ID: id, Name: name, Qty: qty, Optional: true, Sources: []Source{
				{Kind: SourceShop, Shop: AemadStore, Price: price, Note: "Aemad's Adventurers' Store, East Ardougne"}}})
		}
	}
	if !f.haveWig && f.plain == 0 {
		add(ObjBallOfWool, "Ball of wool", PrinceBalls-f.balls, 3)
	}
	add(ObjRope, "Rope", princeRopesWanted(f.stage)-f.rope, 25)
	if !f.havePaste && f.ashes == 0 && f.tinderbox == 0 {
		add(ObjTinderbox, "Tinderbox", 1, 3)
	}
	if f.needClay && !princeHasPickaxe(s) {
		add(ObjBronzePickaxe, "Bronze pickaxe", 1, 5)
	}
	return kit
}

// princeLegAemad is one pass over Aemad's shelf for an account that stands nearer his shop than Ned's (the mule waits in East
// Ardougne, so the pantry's ask ends there): the wool is three balls bought for 2 coins each instead of a trip to the Lumbridge pen
// and its wheel, and the rope, tinderbox and pickaxe are shop goods the tour would otherwise walk across the map for. It runs once;
// whatever the shelf lacked is left to the legs below.
func princeLegAemad(s Snapshot, f princeFacts) (Step, bool) {
	if !ShopOnTheWay(s, AemadStore, NedAnchor) || s.Ran["provision:"+KitPrinceAemad] || s.Failed["provision:"+KitPrinceAemad] {
		return Step{}, false
	}
	kit := princeAemadKit(s, f)
	if len(kit) == 0 {
		return Step{}, false
	}
	return princeProvision(s, KitPrinceAemad, kit, "balls of wool, rope, a tinderbox and a pickaxe from Aemad's shelf, East Ardougne"), true
}

func princeLegWater(s Snapshot, f princeFacts) (Step, bool) {
	if f.waterHeld >= f.waterWanted {
		return Step{}, false
	}
	need := f.waterWanted - s.HeldID(ObjBucketOfWater)
	return princeKit(s, KitPrinceWater, princeWaterKit(need), "jugs of water for the clay and the paste", "water"), true
}

func princeLegFlour(s Snapshot, f princeFacts) (Step, bool) {
	if f.havePaste || f.flour > 0 {
		return Step{}, false
	}
	return princeKit(s, KitPrinceFlour, princeFlourKit(), "a pot of flour for the paste", "flour"), true
}

func princeLegBerries(s Snapshot, f princeFacts) (Step, bool) {
	if f.havePaste || f.berries > 0 {
		return Step{}, false
	}
	return princeKit(s, KitPrinceBerries, princeBerriesKit(s), "a redberry for the paste", "redberries"), true
}

// princeLegBeers is the Rusty Anchor's bartender, who sells one beer a talk: the pantry has already drawn whatever the bank and
// the mule hold (the mule keeps thirteen thousand), so this is the last rung of the item rule.
func princeLegBeers(s Snapshot, f princeFacts) (Step, bool) {
	if f.stage >= PrinceGuardDrunk || f.beers >= PrinceBeers {
		return Step{}, false
	}
	if have := s.HeldID(CoinsID); have < 2*(PrinceBeers-f.beers) {
		return princeWait("beers for Joe need %d coins (the pack holds %d)", 2*(PrinceBeers-f.beers), have), true
	}
	if s.Failed[CustomPrinceBeerBuy] {
		return princeWait("the Rusty Anchor's bartender sold no beer"), true
	}
	return Step{Kind: StepCustom, Custom: CustomPrinceBeerBuy, Stop: BartenderBeerStop, ItemID: ObjBeer, Qty: PrinceBeers,
		Reason: "beers from the Rusty Anchor, 2 coins a talk"}, true
}

func princeLegSkirt(s Snapshot, f princeFacts) (Step, bool) {
	if f.haveSkirt {
		return Step{}, false
	}
	return princeKit(s, KitPrinceSkirt, princeSkirtKit(), "a pink skirt like Keli's", "a pink skirt"), true
}

// princeLegWool is the balls of wool for the wig: the shears for the pen, then the Sheep Shearer module's shear-and-spin.
func princeLegWool(s Snapshot, f princeFacts) (Step, bool) {
	if f.haveWig || f.plain > 0 || f.balls >= PrinceBalls {
		return Step{}, false
	}
	lack := PrinceBalls - f.balls
	if f.wool < lack && s.HeldID(ObjShears) == 0 {
		return princeKit(s, KitPrinceShears, princeShearsKit(), "shears for the Lumbridge pen", "shears"), true
	}
	if s.Failed[CustomSheepWool] {
		return princeWait("no balls of wool from the bank, the mule or the Lumbridge pen"), true
	}
	if s.FreeSlots < lack+1 {
		return Step{Kind: StepDeposit, KeepIDs: princeKeep(), Bank: princeBank(s), BankSet: true, Reason: "room for the wool"}, true
	}
	return Step{Kind: StepCustom, Custom: CustomSheepWool, Qty: lack, Reason: "shear the Lumbridge pen and spin the wool at the castle wheel"}, true
}

// princeLegClay is the clay for the print: a pickaxe, then the rocks.
func princeLegClay(s Snapshot, f princeFacts) (Step, bool) {
	if !f.needClay {
		return Step{}, false
	}
	if !princeHasPickaxe(s) {
		return princeKit(s, KitPrincePickaxe, princePickaxeKit(), "a pickaxe for the clay", "a pickaxe"), true
	}
	if s.Failed[CustomPrinceClay] {
		return princeWait("no clay from the Rimmington rocks"), true
	}
	return Step{Kind: StepCustom, Custom: CustomPrinceClay, Qty: 1, Reason: "mine a clay near Rimmington for the key's print"}, true
}

func princeLegOnions(s Snapshot, f princeFacts) (Step, bool) {
	if f.haveWig || f.yellow > 0 || f.onions >= PrinceOnions {
		return Step{}, false
	}
	return Step{Kind: StepPickLoc, LocID: LocOnionPlant, Loc: "Onion", Op: "Pick", Anchor: RimmingtonOnion,
		ExpectItemID: ObjOnion, ExpectItem: "Onion"}, true
}

// princeLegAshes is the ashes: logs and a tinderbox first, then the fire.
func princeLegAshes(s Snapshot, f princeFacts) (Step, bool) {
	if f.havePaste || f.ashes > 0 {
		return Step{}, false
	}
	if f.logs == 0 || f.tinderbox == 0 {
		return princeKit(s, KitPrinceFire, princeFireKit(f), "logs and a tinderbox to burn for the ashes", "logs and a tinderbox"), true
	}
	if s.Failed[CustomPrinceAshes] {
		return princeWait("the fire left no ashes"), true
	}
	return Step{Kind: StepCustom, Custom: CustomPrinceAshes, Reason: "burn the logs and take the ashes the fire leaves"}, true
}

func princeLegYellowDye(s Snapshot, f princeFacts) (Step, bool) {
	if f.haveWig || f.yellow > 0 || f.onions < PrinceOnions {
		return Step{}, false
	}
	if have := s.HeldID(CoinsID); have < PrinceDyeFee {
		return princeWait("Aggie's yellow dye needs %d coins (the pack holds %d)", PrinceDyeFee, have), true
	}
	return Step{Kind: StepTalk, Stop: AggieYellowStop, UntilItemID: ObjYellowDye}, true
}

func princeLegWig(s Snapshot, f princeFacts) (Step, bool) {
	if f.haveWig || f.plain > 0 || f.balls < PrinceBalls {
		return Step{}, false
	}
	return Step{Kind: StepTalk, Stop: NedWigStop, UntilItemID: ObjPlainWig}, true
}

func princeLegWigDye(s Snapshot, f princeFacts) (Step, bool) {
	if f.haveWig || f.plain == 0 || f.yellow == 0 {
		return Step{}, false
	}
	return Step{Kind: StepUseOn, ItemID: ObjYellowDye, Item: "Yellow dye", TargetKind: TargetItem, TargetID: ObjPlainWig, Target: "Wig"}, true
}

func princeLegPaste(s Snapshot, f princeFacts) (Step, bool) {
	if f.havePaste || f.flour == 0 || f.berries == 0 || f.ashes == 0 || f.waterHeld == 0 {
		return Step{}, false
	}
	return Step{Kind: StepTalk, Stop: AggiePasteStop, UntilItemID: ObjSkinPaste}, true
}

// princeWaterItem is the container of water the soft clay uses: the bucket first, the jug otherwise.
func princeWaterItem(s Snapshot) (int, string) {
	if s.HeldID(ObjBucketOfWater) > 0 {
		return ObjBucketOfWater, "Bucket of water"
	}
	return ObjJugOfWater, "Jug of water"
}

func princeLegSoftClay(s Snapshot, f princeFacts) (Step, bool) {
	if !f.needSoft || f.clay == 0 || f.waterHeld == 0 {
		return Step{}, false
	}
	id, name := princeWaterItem(s)
	return Step{Kind: StepUseOn, ItemID: id, Item: name, TargetKind: TargetItem, TargetID: ObjClay, Target: "Clay"}, true
}

// princeLegPrint is the first visit to the compound of every run: eighteen pages and five menus at Lady Keli, who stands four tiles
// from a level-26 guard that hunts every combat level. It is priced like the beers and the break-out (princeCompound: a food kit
// when the model refuses, a park when the food did not help) and the executor tops the account up first (CustomPrincePrint), so
// an account that cannot survive the guards never walks up to her.
func princeLegPrint(s Snapshot, f princeFacts) (Step, bool) {
	if !f.needPrint || f.softClay == 0 {
		return Step{}, false
	}
	return princeCompound(s, CustomPrincePrint, PrinceKeliPrintVisitTicks, 0, "Lady Keli's key imprint in soft clay"), true
}

// princeLegRope buys the rope from Ned, 15 coins a talk.
func princeLegRope(s Snapshot, f princeFacts) (Step, bool) {
	want := princeRopesWanted(f.stage)
	if f.rope >= want {
		return Step{}, false
	}
	if have := s.HeldID(CoinsID); have < PrinceRopeFee*(want-f.rope) {
		return princeWait("Ned's rope needs %d coins (the pack holds %d)", PrinceRopeFee*(want-f.rope), have), true
	}
	if s.Failed[CustomPrinceRope] {
		return princeWait("Ned sold no rope"), true
	}
	return Step{Kind: StepCustom, Custom: CustomPrinceRope, Stop: NedRopeStop, ItemID: ObjRope, Qty: want, Reason: "rope from Ned, 15 coins each"}, true
}

// princeLegKey is the key: Osman forges it from the print and a bronze bar, Leela hands it over; a lost key is Leela's for 15
// coins from stage 30 on (and while none is banked).
func princeLegKey(s Snapshot, f princeFacts) (Step, bool) {
	if f.haveKey {
		return Step{}, false
	}
	if s.Failed[CustomPrinceKey] {
		return princeWait("the key did not arrive from Osman and Leela"), true
	}
	if f.keyIssued {
		if have := s.HeldID(CoinsID); have < PrinceKeyFee {
			return princeWait("a new key costs %d coins (the pack holds %d)", PrinceKeyFee, have), true
		}
		return Step{Kind: StepCustom, Custom: CustomPrinceKey, Qty: 0, Reason: "Leela issues a new key for 15 coins"}, true
	}
	if f.print == 0 {
		return Step{}, false // the print legs come first
	}
	if f.bar == 0 {
		return princeKit(s, KitPrinceBar, princeBarKit(), "a bronze bar for Osman to forge the key from", "a bronze bar"), true
	}
	return Step{Kind: StepCustom, Custom: CustomPrinceKey, Qty: 1, Reason: "Osman forges the key from the print and the bar, Leela hands it over"}, true
}

// PrinceAliRecord is the quest's identity. The server gates nothing.
var PrinceAliRecord = Record{ID: PrinceAliID, Name: PrinceAliName, QuestPoints: 3}

// PrinceAli is the engine module. One death skips the quest for a day: the compound's guards are aggressive at every level and
// a death drops the disguise. NoRetaliate keeps the account moving and talking instead of trading blows with four guards; the
// engine's sustain eats between pages. The slot is released once the prince is out (ExclusiveDone).
var PrinceAli = Module{
	Record: PrinceAliRecord, Bank: DraynorBank, BankSet: true, Anchor: LeelaAnchor,
	ToolIDs:     princeTools,
	ReadStage:   ParsePrinceAliJournal,
	Sustain:     Sustain{FoodIDs: princeFoodIDs, EatAt: 15},
	MaxDeaths:   1,
	NoRetaliate: true,
	ExclusiveDone: func(s Snapshot) bool {
		return s.StageKnown && s.Stage >= PrinceSaved
	},
	Decide: DecidePrinceAli,
}
