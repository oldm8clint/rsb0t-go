package questplan

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// Pirate's Treasure (content id `hunt`, %hunt 0 not started, 1 fetch rum, 2 received key, 3 read note, 4 complete; not
// transmitted), ported from the TS def (defs/piratestreasure/*.ts) and checked against content 289:
// quests/quest_hunt/scripts/{redbeard_frank,luthas,banana_crate,food_store,pirate_message,dig,hunt_journal}.rs2,
// areas/area_karamja/scripts/{zambo,banana_tree,customs_officer}.rs2 and areas/area_port_sarim/scripts/wydin.rs2.
//
// Redbeard Frank (Port Sarim) will tell where Hector's treasure is for a bottle of Karamjan rum, and the rum cannot be
// carried off Karamja: the customs officer who boards the Musa Point ship searches the pack, confiscates the rum and does
// not sail (customs_officer.rs2 customs_search), and a teleport breaks it (teleport.rs2 post_tele_checks). So the quest
// smuggles it in a crate:
//
//  1. Zambo, in the Karamja Spirits bar, sells the rum (stock 3, 100 ticks to restock, base cost 30).
//  2. Luthas, who runs the banana plantation, hires an account that asks ("Could you offer me employment...", bit 0 of
//     %hunt_store_employed). While employed, the plantation crate at 2943,3151 takes the rum (%crate_rum 0 to 1, refused
//     with "Why would I want to do that?" when unemployed) and up to ten bananas (%crate_bananas), one use-on each.
//  3. Luthas, shown a crate with ten bananas, pays 30 coins, clears bit 0 and ships it: %crate_rum 1 to 2, "in Wydin's
//     store". He then offers "another crate full" (which hires again); the quest takes "Thank you, I'll be on my way".
//  4. Wydin (Port Sarim, "Can I get a job here?", needs a white apron in the pack or worn, bit 1 of %hunt_store_employed)
//     lets an employee into the back room, where the grocery crate at 3009,3207 gives the rum back (%crate_rum 2 to 0).
//     The back room's door opens only for an account WEARING the apron while Wydin is within ten tiles (food_store.rs2).
//  5. Frank takes the rum and hands over Hector's key (%hunt 2). The key opens the chest upstairs in the Blue Moon Inn in
//     Varrock (3218,3396,1, used from the tile east of it: ChestStand) and the chest gives the pirate message; reading it (%hunt 3) names
//     Falador park, where a spade used on the tile 2999,3383 finds 450 coins, a gold ring and an emerald (%hunt 4), 2 quest
//     points and no experience. The dig is refused when a Falador gardener (npc 1217, level 3, 7 hitpoints) is within ten
//     tiles, and the refusal makes him attack; he respawns 50 ticks after he dies.
//
// What a script must plan for:
//   - The smuggle sub-state is three non-transmitted varps (%hunt_store_employed, %crate_rum, %crate_bananas), so the
//     journal is the only reading of it, and the journal says it in prose that several states share (a rum bought minutes
//     ago and a rum collected from Wydin's both read "I have the Karamja Rum" for an account that has smuggled before; the
//     store-job page reads the same while the rum is still in the plantation crate). ParseHuntJournal turns the page into a synthetic stage (Hunt*) the
//     module decides from, and what the page cannot say is settled by where the account stands (Karamja or not), the pack,
//     and the crate's own messages (ParseHuntCrate).
//   - The rum never crosses the sea. On Karamja an account that holds it can only stash it, and no step walks it off the
//     island. A rum that is lost (a death, a dump to the bank) is bought again.
//   - The quest is restartable from any state: nothing is remembered, every decision is the journal, the tile and the pack.

// PiratesTreasureName is the quest tab's spelling (component questlist:hunt).
const PiratesTreasureName = "Pirate's Treasure"

// PiratesTreasureID is the engine's id.
const PiratesTreasureID = "hunt"

// The %hunt values (quest_hunt.constant and general/configs/quest.constant), for the tools that read the varp, and the bits and
// states of the two smuggle varps the content tests pin the journal's oracle to.
const (
	HuntVarpNotStarted  = 0
	HuntVarpFetchRum    = 1
	HuntVarpReceivedKey = 2
	HuntVarpReadNote    = 3
	HuntVarpComplete    = 4

	// %hunt_store_employed: bit 0 is the plantation job (Luthas), bit 1 the job at Wydin's.
	HuntEmployedPlantation = 1
	HuntEmployedStore      = 2
	// %crate_rum: 0 nowhere, 1 in the plantation crate, 2 in Wydin's grocery crate. %crate_bananas counts to HuntCrateCapacity.
	HuntRumNowhere      = 0
	HuntRumInPlantation = 1
	HuntRumInStore      = 2
	HuntCrateCapacity   = 10
)

// The synthetic stages ParseHuntJournal returns. %hunt 1 is the whole smuggle and has ten faces; the others are %hunt 0, 2,
// 3 and 4 as 0, 20, 30 and 40.
const (
	HuntNotStarted = 0
	// HuntFetchRum is %hunt 1 on a page the journal gives no smuggle sentence for (%hunt_store_employed 2 with the rum in the
	// plantation crate, which no script reaches): decided like a rum still to buy.
	HuntFetchRum = 1
	// HuntNeedRum: unemployed at the plantation, the rum not shipped, no rum in the pack ("I need to go to Karamja and buy
	// some rum").
	HuntNeedRum = 10
	// HuntRumUnemployed: the same with a rum in the pack ("find a way to get the rum off Karamja").
	HuntRumUnemployed = 11
	// HuntEmployedNeedRum: employed at the plantation, the crate empty of rum, no rum in the pack ("Now all I need is some rum").
	HuntEmployedNeedRum = 12
	// HuntRumHeldEmployed: employed, a rum in the pack and none in the crate ("hide my rum in the next crate").
	HuntRumHeldEmployed = 13
	// HuntRumInCrate: the rum is in the plantation crate and the bananas are fewer than ten ("I should fill it with bananas").
	HuntRumInCrate = 14
	// HuntCrateFull: the rum and ten bananas are in the crate ("filled it with bananas. I should speak to Luthas").
	HuntCrateFull = 15
	// HuntShipped: Luthas has shipped the crate and the account has no job at Wydin's yet ("the crate has been shipped to
	// Wydin's store").
	HuntShipped = 16
	// HuntStoreJob: employed at Wydin's store and the rum is in one of the two crates ("I have taken a job at Wydin's
	// store"): in Wydin's grocery crate when it has shipped, still in the plantation's when this is a second smuggle.
	HuntStoreJob = 17
	// HuntRumInHand: employed at Wydin's store, the rum out of the crates and in the pack ("I should take it to Redbeard
	// Frank"); also what a rum bought on Karamja reads like for an account that has smuggled one before.
	HuntRumInHand = 18
	// HuntRumLost: the same with no rum in the pack ("I seem to have lost it. I will need to smuggle some more").
	HuntRumLost = 19
	// HuntReceivedKey is %hunt 2: Frank has the rum and the account has Hector's key, the chest or the message.
	HuntReceivedKey = 20
	// HuntReadNote is %hunt 3: the message has been read; the spade is next.
	HuntReadNote = 30
	// HuntComplete is %hunt 4.
	HuntComplete = 40
)

// Ids from content/pack (checked against the production config by TestHuntIdsMatchTheProductionConfig). The spade, 952, is
// ObjSpade of ernestchicken.go: one name for the one object in the package.
const (
	NpcRedbeardFrank   = 375  // [redbeard_frank], Port Sarim
	NpcLuthas          = 379  // [luthas], the Karamja banana plantation
	NpcZambo           = 568  // [zambo], the Karamja Wines, Spirits and Beers bar
	NpcFaladorGardener = 1217 // [falador_gardener], the park's gardener
	ObjKaramjaRum      = 431  // [karamja_rum], untradeable
	ObjChestKey        = 432  // [chest_key], untradeable
	ObjPirateMessage   = 433  // [piratemessage], untradeable
	ObjWhiteApron      = 1005 // [white_apron]
	ObjBanana          = 1963 // [banana]
	LocWydinDoor       = 2069 // [wydindoor], the back room's door
	LocGroceryCrate    = 2071 // [grocerycrate], Wydin's back room
	LocBananaCrate     = 2072 // [bananacrate], the plantation
	LocPirateChest     = 2079 // [piratechest], the Blue Moon Inn upstairs; 2080 is the open one for ten ticks
)

// BananaTreeLocs are the bearing banana trees, a full one (bananatreefull) to one with a single banana (bananatreeone). 2078
// is the same tree empty: it keeps the name and the Search op, so a name search would pick a bare tree for ever.
var BananaTreeLocs = []int{2073, 2074, 2075, 2076, 2077}

// Tiles from content maps (m45_49, m47_50, m46_52, m50_53) and the loc index.
var (
	// FrankAnchor is Redbeard Frank (m47_50, 3053,3250).
	FrankAnchor = tile.Tile{X: 3053, Z: 3250}
	// LuthasAnchor is Luthas (m45_49, 2939,3154).
	LuthasAnchor = tile.Tile{X: 2939, Z: 3154}
	// ZamboShop is the keeper of the Karamja bar (m45_49, 2925,3143); the shelf holds three rums and restocks one in 100 ticks.
	ZamboShop = ShopRef{Npc: "Zambo", NpcID: NpcZambo, Anchor: tile.Tile{X: 2925, Z: 3143}}
	// PlantationCrate is the crate Luthas ships (2943,3151, angle 1).
	PlantationCrate = tile.Tile{X: 2943, Z: 3151}
	// BananaGrove is the east end of the plantation's grove; 33 trees stand west and north of it.
	BananaGrove = tile.Tile{X: 2926, Z: 3160}
	// StoreCrate is Wydin's back-room grocery crate (3009,3207), behind the door at 3012,3204.
	StoreCrate = tile.Tile{X: 3009, Z: 3207}
	// ChestTile is Hector's chest on the Blue Moon Inn's upper floor (angle 1, forceapproach=north in the config); the one tile
	// the engine accepts an op from is east of it (locreach turns the config's blocked side with the placement's angle;
	// TestHuntLocQueriesFindTheirLocs and the real-pack test pin it).
	ChestTile  = tile.Tile{X: 3218, Z: 3396, Level: 1}
	ChestStand = tile.Tile{X: 3219, Z: 3396, Level: 1}
	// DigSite is the tile the spade works from (spade.rs2: distance 1 of 0_46_52_55_55), in Falador park.
	DigSite = tile.Tile{X: 2999, Z: 3383}
	// SpadeSpawn is the spade lying by Falador park (m46_52, 2981,3369).
	SpadeSpawn = tile.Tile{X: 2981, Z: 3369}
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomHuntRum     = "hunt-rum"
	CustomHuntShip    = "hunt-ship"
	CustomHuntCollect = "hunt-collect"
	CustomHuntChest   = "hunt-chest"
	CustomHuntDig     = "hunt-dig"

	KitHuntSupplies = "hunt-supplies"
	KitHuntSpade    = "hunt-spade"
)

// huntNoRumParkFor is how long the quest parks when the rum is in neither crate: nothing will move it, and a second look in
// five minutes finds the same two empty crates.
const huntNoRumParkFor = 3 * time.Hour

// The coins the smuggle wants in the pack before it crosses, and the room the crate wants.
const (
	// HuntCoinsQty is what the supplies pass asks the bank or the mule for: two ship fares, the rum, a margin, and the
	// FareReserve (questrunner.BuyFromShop) that a shop never spends.
	HuntCoinsQty = 400
	// HuntCoinsMin is the least the pack may hold when the account sets out: the Port Sarim fare (30), the rum (about 30) and
	// the 100 the shop keeps for the fares back, with a margin for the price to rise.
	HuntCoinsMin = 200
	// HuntRumPrice is the rum's base cost, the plan's price hint for the first purchase.
	HuntRumPrice = 30
	// HuntMinFreeSlots is the pack room the crossing wants before it starts (the rum, bananas picked in lots as room allows).
	HuntMinFreeSlots = 4
)

// huntTools are the quest's own items, which every bank visit of the quest leaves in the pack. The bananas are in the list
// because a quest walk's floor bite (questrunner.EatFloor) would otherwise eat the ones the crate is waiting for.
var huntTools = []int{CoinsID, ObjKaramjaRum, ObjChestKey, ObjPirateMessage, ObjWhiteApron, ObjSpade, ObjBanana}

// HuntKeepIDs returns the keep list (a copy).
func HuntKeepIDs() []int { return append([]int(nil), huntTools...) }

// huntFoodIDs are the foods the account may carry and eat if the gardener or a stray hits it, best first.
var huntFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1971, 1891, 1893, 1895}

// The stops. Frank's menus are "I'm in search of treasure." / "Arr!" / "Do you have anything for trade?" at the start, then
// "Ok thanks, I'll go and get it." / "So why didn't you ever get it?" after the rum, and "Arr!" / "Do you have anything for
// trade?" when he re-issues a lost key: "Arr!" loops back to the same menu, so the trade line is the one that ends the talk.
// Luthas's menus: the first, to an account that is not employed, is the employment line; the one after the payment is
// "Will you pay me for another crate full?" (which hires again, so it is never chosen) or "Thank you, I'll be on my way".
var (
	FrankStop = NpcStop{Npc: "Redbeard Frank", ID: NpcRedbeardFrank, Anchor: FrankAnchor, Leash: 6,
		Prefer: []string{"I'm in search of treasure.", "Ok thanks, I'll go and get it.", "Do you have anything for trade?"}}
	LuthasHireStop = NpcStop{Npc: "Luthas", ID: NpcLuthas, Anchor: LuthasAnchor, Leash: 6,
		Prefer: []string{"Could you offer me employment on your plantation?"}}
	LuthasShipStop = NpcStop{Npc: "Luthas", ID: NpcLuthas, Anchor: LuthasAnchor, Leash: 6,
		Prefer: []string{"Thank you, I'll be on my way"}}
	WydinJobStop = NpcStop{Npc: "Wydin", ID: NpcWydin, Anchor: WydinShop.Anchor, Leash: 6,
		Prefer: []string{"Can I get a job here?"}}
)

// OnKaramja reports whether t is on the island (the ship's deck at Musa Point included): where the rum must be stashed rather
// than carried.
func OnKaramja(t tile.Tile) bool { return freshstart.LandmassOf(t) == freshstart.LandKaramja }

// huntSmuggleNeedles are ParseHuntJournal's phrases for %hunt 1, newest first, written against NormaliseLines
// (hunt_journal.rs2; finished lines are struck through but kept, so an early phrase still matches late and only this order
// separates them). The lost-rum page leads because it is reachable with every shipping line already behind it.
var huntSmuggleNeedles = []struct {
	stage  int
	needle string
}{
	{HuntRumLost, "but i seem to have lost it"},
	{HuntRumInHand, "i should take it to redbeard frank"},
	{HuntStoreJob, "i have taken a job at wydin's store"},
	{HuntShipped, "the crate has been shipped to"},
	{HuntCrateFull, "in the crate and filled it with bananas"},
	{HuntRumInCrate, "i should fill it with"},
	{HuntRumHeldEmployed, "hide my rum in the next crate"},
	{HuntEmployedNeedRum, "now all i need is some rum"},
	{HuntRumUnemployed, "find a way to get the rum off"},
	{HuntNeedRum, "and buy some rum"},
}

// ParseHuntJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised journal, which is
// unknown, never stage 0. A %hunt 1 page with no smuggle sentence reads HuntFetchRum.
func ParseHuntJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete!"):
		return HuntComplete, true
	case strings.Contains(text, "the note reads"):
		return HuntReadNote, true
	case strings.Contains(text, "i have given the rum to"):
		return HuntReceivedKey, true
	case strings.Contains(text, "he has agreed to tell me"):
		for _, n := range huntSmuggleNeedles {
			if strings.Contains(text, n.needle) {
				return n.stage, true
			}
		}
		return HuntFetchRum, true
	case strings.Contains(text, "i can start this quest by speaking to"):
		return HuntNotStarted, true
	}
	return 0, false
}

// HuntCrate is what the plantation crate's Search says: whether the rum is in it and how many bananas.
type HuntCrate struct {
	Rum     bool
	Bananas int
}

var huntBananasRe = regexp.MustCompile(`the crate has (\d+) bananas? inside`)

// ParseHuntCrate reads banana_crate.rs2's [oploc1,bananacrate] messages from the game messages since the search (lower-cased
// by the caller or not). ok is false when none of the crate's lines is among them. The crate is the only reading of
// %crate_rum and %crate_bananas the client has.
func ParseHuntCrate(text string) (HuntCrate, bool) {
	text = strings.ToLower(text)
	rum := strings.Contains(text, "there is some rum in here") || strings.Contains(text, "there is also some rum stashed in here")
	switch {
	case strings.Contains(text, "the crate is completely empty"):
		return HuntCrate{}, true
	case strings.Contains(text, "the crate is full of bananas"):
		return HuntCrate{Rum: rum, Bananas: HuntCrateCapacity}, true
	}
	if m := huntBananasRe.FindStringSubmatch(text); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return HuntCrate{}, false
		}
		return HuntCrate{Rum: rum, Bananas: min(n, HuntCrateCapacity)}, true
	}
	if rum {
		return HuntCrate{Rum: true}, true
	}
	return HuntCrate{}, false
}

// HuntStash is the crate's answer to a rum used on it (banana_crate.rs2 banana_crate_pack_rum, and oplocu before it).
type HuntStash int

const (
	// HuntStashNone: none of the crate's lines is among the messages.
	HuntStashNone HuntStash = iota
	// HuntStashed: "You stash the rum in the crate."
	HuntStashed
	// HuntStashUnemployed: "Why would I want to do that?", the crate refusing an account that is not employed (bit 0 clear:
	// Luthas cleared it when he paid for the last crate).
	HuntStashUnemployed
	// HuntStashPlanted: "There's already some rum in here...".
	HuntStashPlanted
	// HuntStashInStore: "There is already some rum in Wydin's store, I should go and get that first."
	HuntStashInStore
	// HuntStashNoReason: "I see no reason to do that." (%hunt is not 1).
	HuntStashNoReason
)

// ParseHuntStash classifies the messages that followed a rum used on the plantation crate.
func ParseHuntStash(text string) HuntStash {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "you stash the rum in the crate"):
		return HuntStashed
	case strings.Contains(text, "why would i want to do that"):
		return HuntStashUnemployed
	case strings.Contains(text, "already some rum in wydin's store"):
		return HuntStashInStore
	case strings.Contains(text, "already some rum in here"):
		return HuntStashPlanted
	case strings.Contains(text, "i see no reason to do that"):
		return HuntStashNoReason
	}
	return HuntStashNone
}

// HuntApronOwned reports whether the pack or the worn slots hold the white apron.
func HuntApronOwned(s Snapshot) bool {
	return s.HeldID(ObjWhiteApron) > 0 || s.WornIDs[ObjWhiteApron] > 0
}

// huntBank is the bank a pack-making visit uses: the one nearest the account on the mainland, Draynor's from the island (the
// island's own nearest bank by distance is Ardougne's, across the other ship).
func huntBank(s Snapshot) tile.Tile {
	if !s.TileKnown || OnKaramja(s.Tile) {
		return DraynorBank
	}
	return NearestBank(s.Tile).Tile
}

// huntSuppliesKit is the coins the crossing needs and, while the job at Wydin's is still ahead, the apron. The coins are a
// flexible line (a ceiling and a floor), so an account holding the floor is topped up only from a bank that is open anyway.
// The apron is wanted worn or in the pack; Thessalia's shelf in Varrock is its one source (no spawn lies outside the back room it
// is for, the mule publishes none), after the bank.
func huntSuppliesKit(wantApron bool) []KitLine {
	kit := []KitLine{{ID: CoinsID, Name: "Coins", Qty: HuntCoinsQty, Min: HuntCoinsMin, Stackable: true}}
	if wantApron {
		kit = append(kit, KitLine{ID: ObjWhiteApron, Name: "White apron", Qty: 1, Worn: true, Sources: []Source{
			{Kind: SourceShop, Shop: ThessaliaShop, Price: 3, Note: "Thessalia's Fine Clothes keeps 3 at about 2 gp, restocking"}}})
	}
	return kit
}

// huntSpadeKit is the spade for the dig: the bank, the mule, then the one lying beside Falador park.
func huntSpadeKit() []KitLine {
	return []KitLine{{ID: ObjSpade, Name: "Spade", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: SpadeSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "Falador park"}}}}
}

// huntPrep is the pass before a crossing: coins for the ships and the rum, the apron if the job at Wydin's is ahead, and room
// in the pack. It reports false when the pack is ready.
func huntPrep(s Snapshot, wantApron bool) (Step, bool) {
	apronMissing := wantApron && !HuntApronOwned(s)
	if s.HeldID(CoinsID) >= HuntCoinsMin && !apronMissing {
		if s.FreeSlots < HuntMinFreeSlots {
			return Step{Kind: StepDeposit, KeepIDs: huntTools, Bank: huntBank(s), BankSet: true,
				Reason: "room in the pack for the rum and the bananas"}, true
		}
		return Step{}, false
	}
	if s.Blocked["provision:"+KitHuntSupplies] {
		why := "no " + strconv.Itoa(HuntCoinsMin) + " coins for the ships and the rum from the bank or the mule"
		if apronMissing {
			why += ", or no white apron from the bank or Thessalia's shelf"
		}
		return Step{Kind: StepWait, Reason: why}, true
	}
	return Step{Kind: StepProvision, KitName: KitHuntSupplies, Kit: huntSuppliesKit(wantApron), KeepIDs: huntTools,
		Bank: huntBank(s), BankSet: true, Reason: "coins for the ships and the rum, and the white apron for Wydin's back room"}, true
}

// huntShip is the plantation leg: the crate's rum and bananas and Luthas's shipping, walked and read by the custom step.
func huntShip(reason string) Step {
	return Step{Kind: StepCustom, Custom: CustomHuntShip, Reason: reason}
}

// DecidePiratesTreasure is the whole quest.
func DecidePiratesTreasure(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: FrankStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch {
	case s.Stage == HuntNotStarted:
		return Step{Kind: StepTalk, Stop: FrankStop}
	case s.Stage == HuntReceivedKey:
		return decideHuntTreasure(s)
	case s.Stage == HuntReadNote:
		return decideHuntDig(s)
	case s.Stage >= HuntFetchRum && s.Stage <= HuntRumLost:
		return decideHuntSmuggle(s)
	}
	return Step{Kind: StepWait, Reason: "unrecognised Pirate's Treasure stage"}
}

// decideHuntSmuggle is %hunt 1: the rum from Zambo's to the plantation crate, the crate to Wydin's back room, the rum to Frank.
func decideHuntSmuggle(s Snapshot) Step {
	island := s.TileKnown && OnKaramja(s.Tile)
	rum := s.HeldID(ObjKaramjaRum) > 0
	// A rum on the island never leaves it in the pack: the customs officer confiscates it and the ship does not sail. The only
	// steps an account holding one may take there are the ones that put it in the crate.
	if island && rum {
		if s.Stage == HuntRumUnemployed {
			return Step{Kind: StepTalk, Stop: LuthasHireStop}
		}
		if s.Failed[CustomHuntShip] {
			return Step{Kind: StepWait, Reason: "the plantation crate would not take the rum or be shipped (a rum is never carried off Karamja)"}
		}
		return huntShip("stash the rum, fill the crate with bananas and have Luthas ship it")
	}
	// The coins and the apron are drawn before the first crossing, from the mainland; once on the island the leg is finished there
	// (a second crossing for a pack the island has no bank to fill would only be a fare spent), and the pass that fetches what
	// is missing is the one after the crate has sailed.
	crossing := func() (Step, bool) {
		if island {
			return Step{}, false
		}
		return huntPrep(s, true)
	}
	switch s.Stage {
	case HuntFetchRum, HuntNeedRum, HuntEmployedNeedRum, HuntRumLost:
		if rum {
			// The journal is a step behind the pack (a rum bought a moment ago): the plantation is next.
			if step, ok := crossing(); ok {
				return step
			}
			return huntShip("stash the rum, fill the crate with bananas and have Luthas ship it")
		}
		if s.Failed[CustomHuntRum] {
			return Step{Kind: StepWait, Reason: "no Karamjan rum from Zambo's shelf (empty, or too few coins)"}
		}
		if step, ok := crossing(); ok {
			return step
		}
		return Step{Kind: StepCustom, Custom: CustomHuntRum, Reason: "buy a Karamjan rum from Zambo"}
	case HuntRumUnemployed:
		if step, ok := crossing(); ok {
			return step
		}
		return Step{Kind: StepTalk, Stop: LuthasHireStop}
	case HuntRumHeldEmployed, HuntRumInCrate, HuntCrateFull:
		if s.Failed[CustomHuntShip] {
			return Step{Kind: StepWait, Reason: "the plantation crate would not take the rum or be shipped"}
		}
		if step, ok := crossing(); ok {
			return step
		}
		return huntShip("fill the crate with bananas and have Luthas ship it")
	case HuntShipped:
		// The crate has sailed: Wydin hires an account with an apron.
		if step, ok := huntPrep(s, true); ok {
			return step
		}
		return Step{Kind: StepTalk, Stop: WydinJobStop}
	case HuntStoreJob:
		return decideHuntStoreJob(s, island)
	case HuntRumInHand:
		return Step{Kind: StepTalk, Stop: FrankStop}
	}
	return Step{Kind: StepWait, Reason: "unrecognised smuggle stage"}
}

// decideHuntStoreJob is the account employed at Wydin's with the rum in a crate: Wydin's grocery crate when it has shipped, the
// plantation's when it has not (a second smuggle after a lost rum). The back room is entered in the apron, and an empty one sends
// the account to the plantation, whose own empty crate ends the quest's tries (the rum is then in neither, and the quest waits).
func decideHuntStoreJob(s Snapshot, island bool) Step {
	// The plantation has been looked at in this run once its leg has run or failed. Until then an account on the island looks.
	shipSpent := s.Ran[CustomHuntShip] || s.Failed[CustomHuntShip]
	if island && !shipSpent {
		return huntShip("the rum may still be in the plantation crate")
	}
	if s.WornIDs[ObjWhiteApron] == 0 {
		if s.HeldID(ObjWhiteApron) > 0 {
			return Step{Kind: StepEquip, ItemID: ObjWhiteApron, Item: "White apron"}
		}
		if step, ok := huntPrep(s, true); ok {
			return step
		}
	}
	// An empty back room is remembered, but not past a plantation leg that has run since: the crate it shipped is in the back
	// room now, and the account on the island is on its way to look. A second empty look after that leg ends the tries.
	collectEmpty := s.Failed[CustomHuntCollect] && !(island && s.Ran[CustomHuntShip])
	if !collectEmpty {
		return Step{Kind: StepCustom, Custom: CustomHuntCollect, Reason: "search Wydin's back-room crate for the rum"}
	}
	if !shipSpent {
		return huntShip("Wydin's crate was empty: the rum may still be in the plantation crate")
	}
	return Step{Kind: StepWait, ParkFor: huntNoRumParkFor, Reason: "the rum is in neither Wydin's crate nor the plantation's"}
}

// decideHuntTreasure is %hunt 2: the key to the chest, the message out of it, then the reading.
func decideHuntTreasure(s Snapshot) Step {
	switch {
	case s.HeldID(ObjPirateMessage) > 0:
		return Step{Kind: StepOpHeld, ItemID: ObjPirateMessage, Item: "Pirate message", Op: "Read"}
	case s.HeldID(ObjChestKey) > 0:
		if s.Failed[CustomHuntChest] {
			return Step{Kind: StepWait, Reason: "Hector's chest in the Blue Moon Inn would not give up the message"}
		}
		return Step{Kind: StepCustom, Custom: CustomHuntChest, Reason: "open Hector's chest upstairs in the Blue Moon Inn with the key"}
	}
	// Frank re-issues the key to an account that turns up without one (redbeard_frank.rs2: the pack alone is looked at).
	return Step{Kind: StepTalk, Stop: FrankStop}
}

// decideHuntDig is %hunt 3: a spade, then Falador park.
func decideHuntDig(s Snapshot) Step {
	if s.HeldID(ObjSpade) == 0 {
		if s.Blocked["provision:"+KitHuntSpade] {
			return Step{Kind: StepWait, Reason: "no spade from the bank, the mule or Falador park"}
		}
		return Step{Kind: StepProvision, KitName: KitHuntSpade, Kit: huntSpadeKit(), KeepIDs: huntTools,
			Bank: huntBank(s), BankSet: true, Reason: "a spade for the dig"}
	}
	if s.Ran[CustomHuntDig] {
		return Step{Kind: StepWait, Reason: "the quest tab has not turned green yet"}
	}
	return Step{Kind: StepCustom, Custom: CustomHuntDig, Reason: "dig at the X in Falador park"}
}

// PiratesTreasureRecord is the quest's identity. The server gates nothing.
var PiratesTreasureRecord = Record{ID: PiratesTreasureID, Name: PiratesTreasureName, QuestPoints: 2}

// PiratesTreasure is the engine module. Two deaths park it for a day: the only fight is a level 3 gardener and the walks are the
// walker's, so an account that dies twice on them is not one to send back.
var PiratesTreasure = Module{
	Record: PiratesTreasureRecord, Bank: DraynorBank, BankSet: true, Anchor: FrankAnchor,
	ToolIDs:   huntTools,
	ReadStage: ParseHuntJournal,
	Sustain:   Sustain{FoodIDs: huntFoodIDs, EatAt: 8},
	MaxDeaths: 2,
	Decide:    DecidePiratesTreasure,
}
