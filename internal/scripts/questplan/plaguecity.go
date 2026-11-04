package questplan

import (
	"fmt"
	"math"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Plague City (content id `elena`, %elenaquest, not transmitted), ported from the TS def (defs/plaguecity/*.ts) and checked against
// content 289: quests/quest_elena/scripts/{quest_elena,edmond,alrena,mud_patch,mud_pile,sewerpipe,cupboard,jethick,rehnisons,
// mourner,doors,plaguehouse,bravek,elena,elena_journal,elena_teleport_scroll}.rs2, areas/area_ardougne_west/scripts/{clerk,doors,
// manhole}.rs2, skill_cooking/.../cakes.rs2 and the maps of both Ardougnes. One quest point, 24,250 Mining experience (2,425 at 1x)
// and the Ardougne Teleport scroll, no requirement. The longest of the short quests: 17 stages, a hole under a garden, a pipe,
// a plague wall and a house that only opens for a warrant.
//
// The quest, stage by stage (%elenaquest):
//
//	 0 Edmond (East Ardougne) asks for help finding his daughter Elena, who went over the wall into West Ardougne.
//	 1 Alrena wants dwellberries for a gas mask: they grow in McGrubor's Wood, north of Ardougne, among ten guard dogs.
//	 2 She hands over the gas mask ("gasmask", worn to pass the sewer pipe) and keeps a spare in her cupboard; Edmond is asked again.
//	 3-6 The garden's soil is rock hard: four buckets of water used on a mud patch (the bucket comes back empty each time) soften it.
//	 7 A spade used on the patch digs the account down into the sewers under Ardougne (stage 8), Edmond falls in behind.
//	 8-9 The sewer pipe at the tunnel's west end is grilled shut: a rope used on it (stage 9), then Edmond talks and the grill comes off (10).
//	10 The pipe is open: with the gas mask WORN, "Search" squeezes the account through to West Ardougne's manhole. The manhole is a
//	   cover that reverts 500 ticks after it was opened; its closed form offers Open, its open form Climb-down (to the pipe's sewer end).
//	10->20 Jethick, shown the picture of Elena that lies on Edmond's floor, names the Rehnison family and lends a book to return.
//	20->21 The Rehnisons' door refuses everyone ("Go away") but an account that holds Jethick's book and Ted within seven tiles.
//	21->22 Ted or Martha, asked about Elena, say their daughter saw it; 22->23 Milli, upstairs, says the mossy windowless house.
//	23->24 The mourner by the plague house door, spoken to through the shut door, says clearance comes from Bravek or the Head Mourner.
//	24->25 The clerk calls Bravek in ("This is urgent though!"), only while Bravek stands within seven tiles of the clerk.
//	25->26 Bravek has a hangover: "Do you know what is in the cure?" hands over the recipe.
//	26->27 A hangover cure given to Bravek (the recipe, below) and "They won't listen to me!" gets the warrant.
//	27->28 With the warrant the plague house door lets the account in (only while a mourner is within fourteen tiles to be spoken to);
//	   a barrel on the ground floor holds a small key, the stairs lead to a cellar and the key opens Elena's cell; Elena frees herself.
//	28->29 Edmond, told, pays the scroll and the experience.
//
// The hangover cure is the one thing that is made, not found: a chocolate bar ground with a pestle and mortar (chocolate dust), the
// dust mixed into a bucket of milk, a snape grass mixed into that (cakes.rs2). Every item of the recipe is untradeable once made and
// none is for sale together, so the quest is also a shopping trip: the pestle from Jatix in Taverley, the chocolate bar from Wydin in
// Port Sarim (one on the shelf, a hundred ticks to restock), the milk from the mule (it publishes eight) or a cow in the field east of
// Ardougne, and the snape grass from the three spawns on the hobgoblins' trail west of the Crafting Guild.
//
// What a script must plan for:
//
//   - Nine of the quest's messages are message boxes (mesbox/objbox), which sit on the main modal that Dialogue.Drive does not see:
//     every conversation here is driven by the module's own plagueConverse, which closes them as it goes (questmods/plague.go).
//   - The journal renders some stages alike. Stages 3 to 6 (the water) read one line, 20 and 21 one line, 24 and 25 one line: the parser
//     returns the lowest of each and the module settles the rest by the pack (the book) or by what the conversation says.
//   - West Ardougne is a pocket: its gates ("they will not open" until Biohazard) are shut, and the way out is the manhole to the
//     sewer's pipe end and the mud pile up to Edmond's garden. The sewer, the Rehnisons' upper floor and the plague house's cellar are
//     pockets of their own (Leave, below). Every walk the quest asks for starts on the side of the wall it is meant for.
//   - Several doors are gated by the script, not by a key: the Rehnisons' by the book and Ted (stage 20), Bravek's by the stage and
//     the clerk, the plague house's by the warrant and a mourner in earshot, Elena's cell by the key. check_axis decides which side
//     of the wall the script thinks the account is on, so each is operated from the loc's own tile (questrunner.LocStands) and the
//     result is read from where the account stands afterwards, never assumed.
//   - Two legs are near aggressive npcs and are priced, not walked into: McGrubor's Wood has ten level-44 guard dogs, and the three
//     snape grass spawns lie on the hobgoblins' trail (nine of level 28 that roam fifteen tiles from where they stand). Neither is
//     fought: the quest does not retaliate, the dogs are given a berth, and the hobgoblin leg is entered only by an account the
//     model says survives it (PlagueGrassOdds) after a food top-up; a death on either parks the quest for a day.
//   - The quest is restartable from any state: nothing is remembered, every decision is the journal, the tile and the pack.

// PlagueCityName is the quest tab's spelling (component 7372, `questlist:elena`).
const PlagueCityName = "Plague City"

// PlagueCityID is the engine's id, content's own.
const PlagueCityID = "elena"

// The %elenaquest values the journal tells apart (quest_elena.constant, ^elena_complete = 29 in general/configs/quest.constant). The
// water stages (3 to 6), the book (20 and 21) and the clearance (24 and 25) are one reading each, the lowest of the group.
const (
	PlagueNotStarted     = 0
	PlagueStarted        = 1  // Alrena wants dwellberries
	PlagueGasMask        = 2  // Alrena made the mask; Edmond is asked about the digging
	PlagueWater          = 3  // 3-6: the soil wants four buckets of water
	PlagueSoft           = 7  // the soil is soft: dig
	PlagueTunnel         = 8  // dug into the sewer: the rope on the grill
	PlagueRope           = 9  // the rope is tied: Edmond pulls
	PlaguePipeOpen       = 10 // the pipe is open: Jethick
	PlagueShown          = 20 // 20-21: Jethick told; the book to return, then the Rehnisons
	PlagueSpokenParents  = 22 // the Rehnisons told; Milli
	PlagueSpokenMilli    = 23 // Milli told; the plague house door
	PlagueNeedClearance  = 24 // 24-25: the clerk, then Bravek
	PlagueSpokenBravek   = 26 // the recipe; the cure to Bravek
	PlagueCured          = 27 // Bravek cured and the warrant given; the plague house
	PlagueFreed          = 28 // Elena freed; Edmond
	PlagueComplete       = 29
	PlagueCompleteScroll = 30 // the scroll read
)

// Ids from content/pack (checked against the production config by TestPlagueIdsMatchTheProductionConfig). The shared items already
// have their names in this package: the spade ObjSpade, the rope ObjRope, ObjBucket, ObjBucketOfWater and ObjBucketOfMilk.
const (
	PlagueNpcEdmond   = 714 // [edmond]: the garden's door and, after the dig, the sewer
	PlagueNpcAlrena   = 710
	PlagueNpcJethick  = 725
	PlagueNpcTed      = 721 // [ted_rehnison]
	PlagueNpcMartha   = 722
	PlagueNpcMilli    = 724
	PlagueNpcClerk    = 713
	PlagueNpcBravek   = 711
	PlagueNpcElena    = 715 // [elenap], the captive; [elena2] 335 is her at home
	PlagueNpcMourner  = 719 // [mournertwb], the four at the plague house; [mournertwa] 718 stands by Edmond's house
	PlagueNpcJatix    = 587
	PlagueNpcGuardDog = 99  // [guarddog], level 44, McGrubor's Wood
	PlagueNpcHobgob   = 122 // [hobgoblin_unarmed], level 28, the trail by the Crafting Guild
	PlagueNpcHobgobSp = 123 // [hobgoblin_armed], level 42

	PlagueObjDwellberries   = 2126
	PlagueObjGasMask        = 1506
	PlagueObjPicture        = 1510 // [elena_picture]
	PlagueObjBook           = 1509 // [turnip_book], "Book"
	PlagueObjNote           = 1508 // [scruffy_note]
	PlagueObjHangoverCure   = 1504
	PlagueObjWarrant        = 1503
	PlagueObjKey            = 1507 // [elenakey], "A small key"
	PlagueObjScroll         = 1505 // [ardougnescroll], "A magic scroll"
	PlagueObjPestle         = 233
	PlagueObjSnapeGrass     = 231
	PlagueObjChocolateBar   = 1973
	PlagueObjChocolateDust  = 1975
	PlagueObjChocolateMilk  = 1977 // [chocolaty_milk]
	PlagueObjShark          = 385
	PlagueLocMudPatch1      = 2531 // the garden's three patches are 2531, 2532 and 2531
	PlagueLocMudPatch2      = 2532
	PlagueLocMudPile        = 2533
	PlagueLocSewerPipe      = 2541
	PlagueLocManholeShut    = 2543 // "Open"
	PlagueLocManholeOpen    = 2544 // "Climb-down"
	PlagueLocCupboardShut   = 2524 // "Open"
	PlagueLocCupboardOpen   = 2525 // "Search"
	PlagueLocRehnisonDoor   = 2537
	PlagueLocRehnisonStairs = 2539 // "Walk-up", on the ground floor
	PlagueLocRehnisonTop    = 2540 // "Walk-down", on the upper floor
	PlagueLocHouseDoor      = 2535 // the plague house's door, a wall loc on the south edge of its tile
	PlagueLocBarrel         = 2530
	PlagueLocHouseStairs    = 2522 // "Walk-Down", on the ground floor
	PlagueLocCellarStairs   = 2523 // "Walk-Up", in the cellar
	PlagueLocElenaGate      = 2526 // the cell door
	PlagueLocBravekDoor     = 2528
	PlagueLocFountain       = 879
)

// The plague wall, its sewer and the places the quest works. Tiles are content's: spawns from the maps (m40_51-m41_52 for the two
// Ardougnes, m38_151/m39_151/m40_152 for the sewer), locs from the loc index, and the teleport targets the scripts name
// (quest_elena.constant).
var (
	PlagueEdmondAnchor = tile.Tile{X: 2568, Z: 3334}
	PlagueAlrenaAnchor = tile.Tile{X: 2573, Z: 3333}
	// PlagueGarden is south of the three patches (2566,3331-3333); the dig lands the account at PlagueSewerLanding.
	PlagueGarden        = tile.Tile{X: 2566, Z: 3330}
	PlagueSewerLanding  = tile.Tile{X: 2562, Z: 9737}
	PlagueMudPileStand  = tile.Tile{X: 2561, Z: 9737}
	PlagueSewerEdmond   = tile.Tile{X: 2535, Z: 9719}
	PlagueSewerPipeEnd  = tile.Tile{X: 2530, Z: 9703} // where the manhole drops the account and the pipe is searched from
	PlagueWestLanding   = tile.Tile{X: 2529, Z: 3304} // where the pipe lands the account; the manhole is at 2529,3303
	PlagueManhole       = tile.Tile{X: 2529, Z: 3303}
	PlagueJethickAnchor = tile.Tile{X: 2540, Z: 3305}
	// PlagueRehnisonDoor is the loc's own tile (the wall is on its north edge); the account enters from it.
	PlagueRehnisonDoor   = tile.Tile{X: 2531, Z: 3328}
	PlagueTedAnchor      = tile.Tile{X: 2531, Z: 3331}
	PlagueRehnisonStairs = tile.Tile{X: 2527, Z: 3332}
	PlagueRehnisonTop    = tile.Tile{X: 2527, Z: 3332, Level: 1}
	PlagueMilliAnchor    = tile.Tile{X: 2531, Z: 3330, Level: 1}
	PlagueClerkAnchor    = tile.Tile{X: 2528, Z: 3317}
	// PlagueBravekDoor is the loc's own tile (the wall is on its west edge); the clerk's side is the tile west of it.
	PlagueBravekDoor    = tile.Tile{X: 2530, Z: 3314}
	PlagueBravekAnchor  = tile.Tile{X: 2536, Z: 3314}
	PlagueHouseDoor     = tile.Tile{X: 2533, Z: 3272} // outside; z must equal the door's for check_axis to take the warrant branch
	PlagueHouseInside   = tile.Tile{X: 2533, Z: 3271}
	PlagueBarrel        = tile.Tile{X: 2534, Z: 3268}
	PlagueHouseStairs   = tile.Tile{X: 2536, Z: 3268}
	PlagueCellarLanding = tile.Tile{X: 2537, Z: 9670} // the stairs down land here and the cellar's stairs up are at 2536,9671
	PlagueCellarStairs  = tile.Tile{X: 2536, Z: 9671}
	PlagueElenaGate     = tile.Tile{X: 2539, Z: 9672}
	PlagueElenaAnchor   = tile.Tile{X: 2541, Z: 9672}
	// PlagueCupboard is Alrena's cupboard (2574,3334); it faces east and is searched from the tile east of it.
	PlagueCupboard = tile.Tile{X: 2574, Z: 3334}
	// PlagueFountain is the east market's fountain (2629,3311, 2x2) and PlagueCowField the cows' field east of the city.
	PlagueFountain = tile.Tile{X: 2629, Z: 3311}
	PlagueCowField = tile.Tile{X: 2664, Z: 3347}
	// The two ground spawns in Edmond's house (maps/m40_52.jm2): the spade and Elena's picture (the picture's respawn is 5 ticks).
	PlagueSpadeSpawn   = tile.Tile{X: 2574, Z: 3331}
	PlaguePictureSpawn = tile.Tile{X: 2576, Z: 3334}
	// PlagueGrassSpawns are the three snape grass spawns (30 ticks to come back), the first the one furthest from the armed hobgoblin.
	// PlagueRehnisonTop is the stairs' top, on the upper floor.
	PlagueGrassSpawns = []tile.Tile{{X: 2917, Z: 3274}, {X: 2906, Z: 3297}, {X: 2907, Z: 3295}}
	// PlagueBerrySpots are the dwellberry spawns in McGrubor's Wood the quest walks to. The wood has five (2633,3496, 2638,3479,
	// 2645,3498, 2664,3476 and this one), and the planner finds no way to any but this one that keeps four tiles from every dog's
	// spawn: the wood is entered by two gaps (2659,3478 and 2669,3488) that a dog stands in each. This one, 2667,3494 on the east side,
	// is six from the nearest dog and the route to it passes four tiles from one (TestPlagueBerryRouteKeepsClearOfTheDogs).
	PlagueBerrySpots = []tile.Tile{{X: 2667, Z: 3494}}
	// PlagueMarketDogs are the four guard dogs of East Ardougne's market (maps/m40_52.jm2, m41_52.jm2), reachable on foot: the walk from
	// the north bank to the fountain passes one at a tile (2623,3328 against a dog at 2624,3327), the walk from the garden to the
	// fountain and back keeps six tiles from every one.
	PlagueMarketDogs = []tile.Tile{{X: 2624, Z: 3319}, {X: 2624, Z: 3327}, {X: 2641, Z: 3315}, {X: 2646, Z: 3325}}
	// PlagueGuardDogs are the ten guard dog spawns of McGrubor's Wood (maps/m41_54.jm2, m42_54.jm2): wanderrange 3, huntrange 1.
	PlagueGuardDogs = []tile.Tile{{X: 2625, Z: 3502}, {X: 2628, Z: 3484}, {X: 2631, Z: 3472}, {X: 2636, Z: 3503}, {X: 2645, Z: 3488},
		{X: 2659, Z: 3478}, {X: 2665, Z: 3471}, {X: 2666, Z: 3506}, {X: 2669, Z: 3488}, {X: 2674, Z: 3504}}
)

// PlagueShops: Jatix (Taverley) sells the pestle and mortar (three on the shelf, a hundred ticks to restock), Wydin (Port Sarim) the
// chocolate bar (one, a hundred ticks), Aemad (East Ardougne's adventurers' store) the rope (twenty), and Port Khazard's general
// store or the bucket on the ground by Brother Cedric's cart the bucket.
var PlagueJatixShop = ShopRef{Npc: "Jatix", NpcID: PlagueNpcJatix, Anchor: tile.Tile{X: 2899, Z: 3427}}

// The boxes of the places the quest keeps an account in, found by flooding the collision pack (TestPlagueAreasAreTheCollisionPacksComponents).
// The sewer under the garden is one component of 696 tiles (x 2526-2571, z 9702-9738) with the pipe's end, Edmond and the mud pile in
// it; the cellar is two (the stairs' landing and the cell, x 2536-2542, z 9669-9673); the Rehnisons' upper floor is 20 tiles. The
// city is not a rectangle: the walled streets are x 2509-2556; west of them x 2461-2508 up to z 3324 and x 2462-2508 above it (the
// mainland outside the gate is x 2461, z 3325-3334); and a corridor to the west gate is x 2433-2460, z 3307-3324. The ground south of
// z 3279 west of x 2509 and the strip x 2433-2458 south of z 3305 are the mainland's (the east flood reaches them on foot, and they
// lie between the city's bounding box and its wall), so they are not the city's.
var (
	plagueSewerBox    = Rect{MinX: 2525, MaxX: 2572, MinZ: 9701, MaxZ: 9739}
	plagueCellarBox   = Rect{MinX: 2535, MaxX: 2543, MinZ: 9668, MaxZ: 9674}
	plagueUpstairsBox = Rect{MinX: 2526, MaxX: 2534, MinZ: 3328, MaxZ: 3334, Level: 1}
	plagueWestBoxes   = []Rect{
		{MinX: 2509, MaxX: 2556, MinZ: 3266, MaxZ: 3334},
		{MinX: 2461, MaxX: 2508, MinZ: 3279, MaxZ: 3324},
		{MinX: 2462, MaxX: 2508, MinZ: 3325, MaxZ: 3334},
		{MinX: 2433, MaxX: 2460, MinZ: 3307, MaxZ: 3324},
	}
)

// PlagueArea is which side of the wall an account stands on.
type PlagueArea int

const (
	// PlagueEast is everywhere that is none of the following: the mainland, where the walker goes anywhere.
	PlagueEast PlagueArea = iota
	// PlagueSewer is the sewer under East Ardougne, reached by the garden's hole and left by the mud pile.
	PlagueSewer
	// PlagueWest is West Ardougne's ground floor, the plague house included.
	PlagueWest
	// PlagueUpstairs is the Rehnisons' upper floor.
	PlagueUpstairs
	// PlagueCellar is the plague house's cellar.
	PlagueCellar
)

// String names the area for the log.
func (a PlagueArea) String() string {
	return [...]string{"the mainland", "the Ardougne sewer", "West Ardougne", "the Rehnisons' upper floor", "the plague house cellar"}[a]
}

// PlagueAreaOf classifies a tile.
func PlagueAreaOf(t tile.Tile) PlagueArea {
	switch {
	case plagueCellarBox.Contains(t):
		return PlagueCellar
	case plagueSewerBox.Contains(t):
		return PlagueSewer
	case plagueUpstairsBox.Contains(t):
		return PlagueUpstairs
	}
	for _, r := range plagueWestBoxes {
		if r.Contains(t) {
			return PlagueWest
		}
	}
	return PlagueEast
}

// InPlaguePocket reports a tile that is on the far side of the wall or under it: the sewer, the city, the Rehnisons' upper floor and
// the cellar. No walk leaves them but the quest's own way (the manhole and the mud pile), so they are one registered pocket.
func InPlaguePocket(t tile.Tile) bool { return PlagueAreaOf(t) != PlagueEast }

// InPlagueHouse reports a tile on the plague house's ground floor. The north wall steps back one tile east of the door, so a flat
// box would either claim the doorstep or lose the east strip.
func InPlagueHouse(t tile.Tile) bool {
	if t.Level != 0 || t.X < 2532 || t.X > 2541 {
		return false
	}
	north := 3271
	if t.X >= 2535 {
		north = 3272
	}
	return t.Z >= 3268 && t.Z <= north
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomPlagueStart    = "plague-start"     // Edmond: the quest begins
	CustomPlagueAlrena   = "plague-alrena"    // the dwellberries to Alrena
	CustomPlagueEdmond   = "plague-edmond"    // Edmond asked about the digging
	CustomPlagueBerries  = "plague-berries"   // dwellberries from McGrubor's Wood
	CustomPlagueCupboard = "plague-cupboard"  // the spare mask from Alrena's cupboard
	CustomPlagueWater    = "plague-water"     // buckets of water to the soil
	CustomPlagueDig      = "plague-dig"       // the spade on the patch
	CustomPlagueRope     = "plague-rope"      // the rope on the grill
	CustomPlagueGrill    = "plague-grill"     // Edmond pulls the grill off
	CustomPlagueCross    = "plague-cross"     // the mask on, through the pipe
	CustomPlagueHome     = "plague-home"      // out of the pocket, to Edmond's garden
	CustomPlagueJethick  = "plague-jethick"   // the picture shown
	CustomPlagueBook     = "plague-book"      // the book returned through the Rehnisons' door
	CustomPlagueParents  = "plague-parents"   // Ted or Martha asked
	CustomPlagueMilli    = "plague-milli"     // Milli, upstairs
	CustomPlagueDown     = "plague-down"      // down the Rehnisons' stairs
	CustomPlagueMourner  = "plague-mourner"   // the mourner through the plague house door
	CustomPlagueAudience = "plague-audience"  // the clerk, Bravek's door, Bravek's recipe
	CustomPlagueCure     = "plague-cure"      // the cure to Bravek, and the warrant
	CustomPlagueHouse    = "plague-house"     // in, the key, the cellar, Elena
	CustomPlagueFinish   = "plague-finish"    // Edmond told
	CustomPlagueMilk     = "plague-milk"      // a bucket of milk from a cow
	CustomPlagueGrass    = "plague-grass"     // snape grass on the hobgoblins' trail
	CustomPlagueBrew     = "plague-brew"      // the cure mixed
	CustomPlagueScroll   = "plague-scroll"    // the scroll read
	KitPlagueBerries     = "plague-berries"   // dwellberries from the bank or the mule
	KitPlagueBasics      = "plague-basics"    // spade, rope, buckets and the picture
	KitPlagueMask        = "plague-mask"      // the gas mask from the bank
	KitPlagueBook        = "plague-book"      // Jethick's book from the bank
	KitPlagueCureBank    = "plague-cure-bank" // whatever of the cure the bank or the mule holds
	KitPlagueCureShops   = "plague-cure-shop" // the pestle and the chocolate bar
	KitPlagueFood        = "plague-food"      // sharks for the dogs and the hobgoblins
)

// plagueFoods are the foods the account may carry and eat, best first.
var plagueFoods = []int{PlagueObjShark, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// PlagueFoodIDs returns the food list (a copy).
func PlagueFoodIDs() []int { return append([]int(nil), plagueFoods...) }

// plagueTools are the quest's own items, which every bank visit of the quest leaves in the pack and the eat loop never eats (the
// chocolate bar has an Eat op).
var plagueTools = []int{CoinsID, ObjSpade, ObjRope, ObjBucket, ObjBucketOfWater, ObjBucketOfMilk, PlagueObjDwellberries, PlagueObjGasMask,
	PlagueObjPicture, PlagueObjBook, PlagueObjNote, PlagueObjHangoverCure, PlagueObjWarrant, PlagueObjKey, PlagueObjScroll, PlagueObjPestle,
	PlagueObjSnapeGrass, PlagueObjChocolateBar, PlagueObjChocolateDust, PlagueObjChocolateMilk}

// PlagueToolIDs returns the tool list (a copy).
func PlagueToolIDs() []int { return append([]int(nil), plagueTools...) }

// plagueKeep is what a bank visit of this quest leaves in the pack: the tools and the food.
func plagueKeep() []int { return append(append([]int(nil), plagueTools...), plagueFoods...) }

// PlagueKeepIDs returns the keep list (a copy).
func PlagueKeepIDs() []int { return plagueKeep() }

// ---------------------------------------------------------------------------------------------------------------
// The journal.
// ---------------------------------------------------------------------------------------------------------------

// plagueJournalNeedles are ParsePlagueJournal's phrases, newest stage first, written against NormaliseLines (elena_journal.rs2:
// finished lines are struck through but kept, so a later stage holds the earlier ones' words and only this order separates
// them). Each needle is on one journal line or across two joined by a space.
var plagueJournalNeedles = []struct {
	stage  int
	needle string
}{
	{PlagueComplete, "quest complete"},
	{PlagueFreed, "i've freed elena from the plague house"},
	{PlagueCured, "i've given bravek the hangover cure"},
	{PlagueSpokenBravek, "bravek might give me clearance if i make his hangover"},
	{PlagueNeedClearance, "i need clearance from either the head mourner"},
	{PlagueSpokenMilli, "i need to get access to the plague house"},
	{PlagueSpokenParents, "milli may have seen elena"},
	// The unstruck line is cut by colour tags, so it reads "jethick , he thinks" and the struck one "jethick, he thinks": the needle stops before the comma.
	{PlagueShown, "he thinks elena was staying with"},
	{PlaguePipeOpen, "i've managed to clear the way into west ardougne"},
	{PlagueRope, "i've tied a rope to the sewer grill"},
	{PlagueTunnel, "i've dug a tunnel into the sewers"},
	{PlagueSoft, "i've softened the ground in edmond's garden"},
	{PlagueWater, "i need to use water to soften the ground"},
	{PlagueGasMask, "i need to talk to edmond about getting into west ardougne"},
	{PlagueStarted, "i need to get some dwellberries"},
	{PlagueNotStarted, "i can start this quest by speaking to"},
}

// ParsePlagueJournal reads the stage from the rendered journal, the lowest stage of the groups the journal does not tell apart (3
// for 3-6, 20 for 20-21, 24 for 24-25). ok is false for an empty or unrecognised journal, which is unknown, never stage 0.
func ParsePlagueJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range plagueJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------------------------------------------
// The foes: the hobgoblins' trail and McGrubor's guard dogs.
// ---------------------------------------------------------------------------------------------------------------

// FoePlagueHobgoblin is [hobgoblin_unarmed] (scripts/_unpack/225/all.npc): level 28, attack 22, defence 24, strength 24, 29
// hitpoints, no attackrate (4) and no bonus params (a maximum hit of 3), huntmode cowardly (outside the Wilderness it hunts only a
// player of up to twice its level, 56), huntrange 1, wanderrange 15, maxrange 17. Nine stand along the trail by the Crafting Guild.
var FoePlagueHobgoblin = MeleeFoe{Name: "Hobgoblin", Attack: 22, Strength: 24, Defence: 24, Magic: 1, Hitpoints: 29, AttackTicks: 4}

// FoePlagueGuardDog is [guarddog]: level 44, attack 35, defence 37, strength 36, 49 hitpoints, no bonus params (a maximum hit of
// 5), huntmode cowardly (it hunts up to combat 88), huntrange 1, wanderrange 3.
var FoePlagueGuardDog = MeleeFoe{Name: "Guard dog", Attack: 35, Strength: 36, Defence: 37, Magic: 1, Hitpoints: 49, AttackTicks: 4}

// The hunting facts the module prices the two legs by.
const (
	// PlagueHobgoblinHunts is the highest combat level the unarmed hobgoblins go for (2 x 28).
	PlagueHobgoblinHunts = 56
	// PlagueGrassReach is how many hobgoblins are expected to reach the account during the grass leg, and PlagueGrassTicks how many ticks
	// they hit for: the trail is walked in to the spawn (three to ten tiles of it), the grass taken in a tick and the way out run at two
	// tiles a tick, with a hobgoblin at the heels for at most a few attacks.
	PlagueGrassReach = 2
	PlagueGrassTicks = 20
	// PlagueGrassPark is how long the quest is left alone when the model refuses the leg even with food.
	PlagueGrassPark = 6 * time.Hour
	// PlagueDogReach is how far from a guard dog's spawn it can reach an account: wanderrange 3 and huntrange 1, and a tile for the
	// 1x1 body and the step it takes; the route and the berry spot keep PlagueDogBerth tiles from every spawn.
	PlagueDogReach = 4
	PlagueDogBerth = 6
)

// PlagueDogZones are the rectangles a walk keeps out of round guard dog spawns: berth tiles each way.
func PlagueDogZones(spawns []tile.Tile, berth int) []Rect {
	out := make([]Rect, 0, len(spawns))
	for _, g := range spawns {
		out = append(out, Rect{MinX: g.X - berth, MaxX: g.X + berth, MinZ: g.Z - berth, MaxZ: g.Z + berth, Level: g.Level})
	}
	return out
}

// PlagueCombat is the 2004 combat level from a snapshot's base levels, 0 when they are not known.
func PlagueCombat(s Snapshot) int {
	if len(s.Levels) == 0 {
		return 0
	}
	l := func(k string) float64 { return float64(max(s.Level(k), 1)) }
	base := 0.25 * (l("defence") + float64(max(s.Level("hitpoints"), 10)) + math.Floor(l("prayer")/2))
	melee := 0.325 * (l("attack") + l("strength"))
	ranged := 0.325 * math.Floor(l("ranged")*1.5)
	magic := 0.325 * math.Floor(l("magic")*1.5)
	return int(math.Floor(base + math.Max(melee, math.Max(ranged, magic))))
}

// PlagueGrassHunted reports whether the unarmed hobgoblins go for an account of this combat level (0, unknown, counts as hunted).
func PlagueGrassHunted(combat int) bool { return combat <= 0 || combat <= PlagueHobgoblinHunts }

// PlagueGrassOdds prices the leg onto the hobgoblins' trail for the account in the snapshot: an account they do not hunt (combat above
// 56) is not at risk; one they hunt takes the expected blows of PlagueGrassReach hobgoblins for PlagueGrassTicks ticks at this Defence
// (FoePerAttack, with the same quarter on top the quest fights use) from the hitpoints it goes in with, and must keep four hits of
// two hobgoblins or a quarter of its maximum. The food in the pack counts as the top-up before the leg only up to the level a fight
// starts at (FightStartHP): nothing is eaten on the trail itself.
func PlagueGrassOdds(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 || !PlagueGrassHunted(PlagueCombat(s)) {
		return true, ""
	}
	f := FoePlagueHobgoblin
	per := FoePerAttack(f, fighterOf(s)) * FightIncomingScale
	dmg := float64(PlagueGrassReach) * float64(PlagueGrassTicks) / float64(f.AttackTicks) * per
	hp := s.HP
	if hp <= 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	pool := FoodPoolHeal(s.InvIDs, plagueFoods)
	start := max(hp, min(FightStartHP(s.MaxHP, f.PeakHit()), hp+pool))
	left := float64(start) - dmg
	margin := float64(max(f.PeakHit()*PlagueGrassReach*2, s.MaxHP/4))
	if left < margin {
		return false, fmt.Sprintf("the hobgoblins' trail (level 28, hits up to %d, hunting up to combat %d) costs an account at combat %d and Defence %d about %.0f hitpoints; it goes in with %d of %d (%d in food to top up from), and the leg must leave %.0f",
			f.PeakHit(), PlagueHobgoblinHunts, PlagueCombat(s), s.Level("defence"), dmg, start, s.MaxHP, pool, margin)
	}
	return true, ""
}

// ---------------------------------------------------------------------------------------------------------------
// The stops.
// ---------------------------------------------------------------------------------------------------------------

// The conversations. Every menu of the content is spelt out in the content test (TestPlagueContentDialogue); the preferences are
// matched as substrings of the options, in order, and a menu with none of them stops the conversation rather than guessing.
var (
	// PlagueEdmondStart: "What's happened to her?" (the first menu) then "Can I help find her?" (the second).
	PlagueEdmondStart = NpcStop{Npc: "Edmond", ID: PlagueNpcEdmond, Anchor: PlagueEdmondAnchor, Leash: 6,
		Prefer: []string{"What's happened to her?", "Can I help find her?"}}
	// PlagueEdmondTalk is every later talk with Edmond: none of them has a menu. At stage 28 an account that comes back after the
	// quest sees "No problem."
	PlagueEdmondTalk = NpcStop{Npc: "Edmond", ID: PlagueNpcEdmond, Anchor: PlagueEdmondAnchor, Leash: 6, Prefer: []string{"No problem."}}
	// PlagueEdmondSewer is Edmond in the sewer, where the grill is pulled off.
	PlagueEdmondSewer = NpcStop{Npc: "Edmond", ID: PlagueNpcEdmond, Anchor: PlagueSewerEdmond, Leash: 6, Prefer: []string{"No problem."}}
	PlagueAlrenaStop  = NpcStop{Npc: "Alrena", ID: PlagueNpcAlrena, Anchor: PlagueAlrenaAnchor, Leash: 4, Prefer: []string{"No problem."}}
	// PlagueJethickStop: "I'm looking for a woman from East Ardougne." (the other is "So who's in charge here?").
	PlagueJethickStop = NpcStop{Npc: "Jethick", ID: PlagueNpcJethick, Anchor: PlagueJethickAnchor, Leash: 5,
		Prefer: []string{"I'm looking for a woman from East Ardougne."}}
	PlagueTedStop    = NpcStop{Npc: "Ted Rehnison", ID: PlagueNpcTed, Anchor: PlagueTedAnchor, Leash: 6, Prefer: []string{"No problem."}}
	PlagueMarthaStop = NpcStop{Npc: "Martha Rehnison", ID: PlagueNpcMartha, Anchor: PlagueTedAnchor, Leash: 6, Prefer: []string{"No problem."}}
	PlagueMilliStop  = NpcStop{Npc: "Milli Rehnison", ID: PlagueNpcMilli, Anchor: PlagueMilliAnchor, Leash: 5, Prefer: []string{"No problem."}}
	// PlagueMournerStop is the plague house door's conversation, driven through the door (the mourner is the npc that speaks):
	// "I fear not a mere plague." keeps it to one more page than "But I think a kidnap victim is in here.".
	PlagueMournerStop = NpcStop{Npc: "Mourner", ID: PlagueNpcMourner, Anchor: PlagueHouseDoor, Leash: 14,
		Prefer: []string{"I fear not a mere plague."}}
	// PlagueClerkStop: "I need permission to enter a plague house." then "This is urgent though!".
	PlagueClerkStop = NpcStop{Npc: "Clerk", ID: PlagueNpcClerk, Anchor: PlagueClerkAnchor, Leash: 3,
		Prefer: []string{"I need permission to enter a plague house.", "This is urgent though!"}}
	// PlagueBravekRecipe: "This is really important though!" then "Do you know what is in the cure?".
	PlagueBravekRecipe = NpcStop{Npc: "Bravek", ID: PlagueNpcBravek, Anchor: PlagueBravekAnchor, Leash: 6,
		Prefer: []string{"This is really important though!", "Do you know what is in the cure?"}}
	// PlagueBravekWarrant: the cure is given first ("Try this."), then "They won't listen to me!" asks for the warrant.
	PlagueBravekWarrant = NpcStop{Npc: "Bravek", ID: PlagueNpcBravek, Anchor: PlagueBravekAnchor, Leash: 6,
		Prefer: []string{"They won't listen to me!"}}
	PlagueElenaStop = NpcStop{Npc: "Elena", ID: PlagueNpcElena, Anchor: PlagueElenaAnchor, Leash: 4, Prefer: []string{"No problem."}}
)

// ---------------------------------------------------------------------------------------------------------------
// The kits.
// ---------------------------------------------------------------------------------------------------------------

// plagueBank is the bank a pass uses: the one nearest the account, East Ardougne's north bank before a position has arrived.
func plagueBank(s Snapshot) tile.Tile {
	if s.TileKnown && PlagueAreaOf(s.Tile) == PlagueEast {
		return NearestBank(s.Tile).Tile
	}
	return tile.Tile{X: 2616, Z: 3332}
}

// plagueBasics are the Ardougne errands the dig needs, by the stage the account is at (the stage it will do the work at): the
// spade lies in Edmond's house, the picture beside it, the rope is Aemad's, the bucket is the one by Brother Cedric's cart or Port
// Khazard's store. Every line is wanted only while the work it serves is ahead, and only the part the pack lacks.
func plagueBasics(s Snapshot, stage int) []KitLine {
	held := func(id int) int { return s.HeldID(id) }
	var kit []KitLine
	if stage >= PlagueWater && stage <= PlagueCured && held(ObjSpade) == 0 {
		kit = append(kit, KitLine{ID: ObjSpade, Name: "Spade", Qty: 1, Sources: []Source{
			{Kind: SourceGround, Tile: PlagueSpadeSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the floor of Edmond's house"}}})
	}
	if stage >= PlagueWater && stage <= PlagueTunnel && held(ObjRope) == 0 {
		kit = append(kit, KitLine{ID: ObjRope, Name: "Rope", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: AemadStore, Price: 24, Note: "Aemad's adventurers' store keeps twenty, a rope at 18 x 1.3"}}})
	}
	if stage == PlagueWater && held(ObjBucket)+held(ObjBucketOfWater) == 0 {
		// Four pours need four buckets, and the garden hands each back empty: one bucket is four trips to the fountain, four are one.
		kit = append(kit, KitLine{ID: ObjBucket, Name: "Bucket", Qty: 4, Min: 1, Sources: []Source{
			{Kind: SourceGround, Tile: CogBucketSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the bucket by Brother Cedric's cart"},
			{Kind: SourceShop, Shop: KhazardGeneralStore, Price: 2, Note: "Port Khazard's general store keeps 2 buckets"}}})
	}
	if (stage >= PlagueWater && stage <= PlaguePipeOpen || stage == PlagueShown && held(PlagueObjBook) == 0) && held(PlagueObjPicture) == 0 {
		kit = append(kit, KitLine{ID: PlagueObjPicture, Name: "Picture", Qty: 1, Untradeable: true, Sources: []Source{
			{Kind: SourceGround, Tile: PlaguePictureSpawn, PerSpawn: 1, RespawnTicks: 5, Note: "the floor of Edmond's house"}}})
	}
	return kit
}

// plagueCureParts says how far the cure is made: the cure itself, the chocolaty milk (the bucket with the dust mixed in), the
// chocolate dust, or none of them.
func plagueCureParts(s Snapshot) (cure, choc, dust bool) {
	return s.HeldID(PlagueObjHangoverCure) > 0, s.HeldID(PlagueObjChocolateMilk) > 0, s.HeldID(PlagueObjChocolateDust) > 0
}

// PlagueNextMix is the next mix of the recipe the pack allows, as the item to use and the item it is used on: the pestle on the
// chocolate bar (dust), the dust on the bucket of milk (chocolaty milk), the snape grass on the chocolaty milk (the cure). The latest
// mix possible goes first so that a pack that holds the chocolaty milk and the grass is never made to grind a spare bar. ok is false
// when no mix is possible yet.
func PlagueNextMix(held func(id int) bool) (use, on int, ok bool) {
	switch {
	case held(PlagueObjSnapeGrass) && held(PlagueObjChocolateMilk):
		return PlagueObjSnapeGrass, PlagueObjChocolateMilk, true
	case held(PlagueObjChocolateDust) && held(ObjBucketOfMilk):
		return PlagueObjChocolateDust, ObjBucketOfMilk, true
	case held(PlagueObjPestle) && held(PlagueObjChocolateBar) && !held(PlagueObjChocolateDust) && !held(PlagueObjChocolateMilk):
		return PlagueObjPestle, PlagueObjChocolateBar, true
	}
	return 0, 0, false
}

// plagueCureBankKit is the first pass of the cure: whatever of it the bank or the mule holds, every line optional and sourceless.
// The mule publishes buckets of milk (eight on 18 Oct 2026); the bank may hold anything an earlier run left.
func plagueCureBankKit() []KitLine {
	return []KitLine{
		{ID: PlagueObjHangoverCure, Name: "Hangover cure", Qty: 1, Optional: true, Untradeable: true},
		{ID: PlagueObjChocolateMilk, Name: "Chocolaty milk", Qty: 1, Optional: true, Untradeable: true},
		{ID: PlagueObjChocolateDust, Name: "Chocolate dust", Qty: 1, Optional: true},
		{ID: PlagueObjChocolateBar, Name: "Chocolate bar", Qty: 1, Optional: true},
		{ID: ObjBucketOfMilk, Name: "Bucket of milk", Qty: 1, Optional: true},
		{ID: PlagueObjSnapeGrass, Name: "Snape grass", Qty: 1, Optional: true},
		{ID: PlagueObjPestle, Name: "Pestle and mortar", Qty: 1, Optional: true},
	}
}

// plagueCureShopKit is the parts of the cure a shop or a spawn sells, for whichever the pack still needs: a pestle only while there is
// a bar to grind (no dust, no chocolaty milk yet), a bar only while there is no dust, and the empty bucket the milking wants
// once the water is done (the bucket the water came in is not a quest item the pack is sure to still hold).
func plagueCureShopKit(s Snapshot) []KitLine {
	_, choc, dust := plagueCureParts(s)
	var kit []KitLine
	if !dust && !choc && s.HeldID(PlagueObjPestle) == 0 {
		kit = append(kit, KitLine{ID: PlagueObjPestle, Name: "Pestle and mortar", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: PlagueJatixShop, Price: 4, Note: "Jatix's Herblore Store in Taverley keeps 3"}}})
	}
	if !dust && !choc && s.HeldID(PlagueObjChocolateBar) == 0 {
		kit = append(kit, KitLine{ID: PlagueObjChocolateBar, Name: "Chocolate bar", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: WydinShop, Price: 10, Note: "Wydin's food store in Port Sarim keeps 1, restocking in a hundred ticks"}}})
	}
	if !choc && s.HeldID(ObjBucketOfMilk) == 0 && s.HeldID(ObjBucket) == 0 && s.HeldID(ObjBucketOfWater) == 0 {
		kit = append(kit, KitLine{ID: ObjBucket, Name: "Bucket", Qty: 1, Sources: []Source{
			{Kind: SourceGround, Tile: CogBucketSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the bucket by Brother Cedric's cart"},
			{Kind: SourceShop, Shop: KhazardGeneralStore, Price: 2, Note: "Port Khazard's general store keeps 2 buckets"}}})
	}
	return kit
}

// plagueFoodKit is a few sharks from the mule (it publishes tens of thousands) for the legs near the dogs and the hobgoblins;
// optional, so an empty mule blocks nothing.
func plagueFoodKit() []KitLine {
	return []KitLine{{ID: PlagueObjShark, Name: "Shark", Qty: 3, Optional: true}}
}

// plagueProvision is a provision step for a kit with the module's keep list and bank.
func plagueProvision(s Snapshot, name string, kit []KitLine, reason string) Step {
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: plagueKeep(), Bank: plagueBank(s), BankSet: true, Reason: reason}
}

// plagueOnce is a pass the run makes once: not after it has run or failed.
func plagueOnce(s Snapshot, kit string) bool {
	return !s.Ran["provision:"+kit] && !s.Failed["provision:"+kit]
}

// plagueLeg is a custom step, or a wait once that step has failed in this run: a leg carries its own retries, so a failure is a verdict
// for the run (the wait ends in the engine's park, and the next run starts the leg afresh).
func plagueLeg(s Snapshot, custom, reason string) Step {
	if s.Failed[custom] {
		return Step{Kind: StepWait, Reason: "the " + custom + " leg failed this run: " + reason}
	}
	return Step{Kind: StepCustom, Custom: custom, Reason: reason}
}

// plagueTalk is a conversation, the module's own (a message box in the middle would end a plain talk step).
func plagueTalk(s Snapshot, custom string, stop NpcStop, reason string) Step {
	if s.Failed[custom] {
		return Step{Kind: StepWait, Reason: "the talk to " + stop.Npc + " failed this run: " + reason}
	}
	return Step{Kind: StepCustom, Custom: custom, Stop: stop, Reason: reason}
}

// plagueHome brings an account that stands in a pocket (or the city) back to Edmond's garden.
func plagueHome(s Snapshot, reason string) Step {
	if s.Failed[CustomPlagueHome] {
		return Step{Kind: StepWait, ParkFor: 30 * time.Minute, Reason: "no way out of " + PlagueAreaOf(s.Tile).String() + " this run (the manhole, the sewer and the mud pile): " + reason}
	}
	return Step{Kind: StepCustom, Custom: CustomPlagueHome, Reason: reason}
}

// plagueRoom is the pack slots a pass wants free: the parts are one each and the account carries food and its run staller's kit.
const plagueRoom = 4

// ---------------------------------------------------------------------------------------------------------------
// The decision.
// ---------------------------------------------------------------------------------------------------------------

// DecidePlagueCity is the whole quest, read from the quest tab, the journal stage, the pack and where the account stands.
func DecidePlagueCity(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return plagueFinished(s)
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not arrived yet"}
	}
	stage := PlagueNotStarted
	if s.Journal == StatusInProgress {
		if !s.StageKnown {
			return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
		}
		stage = s.Stage
	}
	switch PlagueAreaOf(s.Tile) {
	case PlagueEast:
		return plagueDecideEast(s, stage)
	case PlagueSewer:
		return plagueDecideSewer(s, stage)
	case PlagueWest:
		return plagueDecideWest(s, stage)
	case PlagueUpstairs:
		return plagueDecideUpstairs(s, stage)
	}
	return plagueDecideCellar(s, stage)
}

// plagueFinished is a green tab: the scroll Edmond paid is read once if the pack still holds it, then done. An account in a pocket
// (a relog) is not walked out first: the quest is over and the next quest's walks leave the pocket.
func plagueFinished(s Snapshot) Step {
	if s.HeldID(PlagueObjScroll) > 0 && PlagueAreaOf(s.Tile) == PlagueEast && !s.Ran[CustomPlagueScroll] && !s.Failed[CustomPlagueScroll] {
		return Step{Kind: StepCustom, Custom: CustomPlagueScroll, Reason: "read the Ardougne teleport scroll"}
	}
	return Step{Kind: StepDone}
}

// plagueDecideEast is an account on the mainland.
func plagueDecideEast(s Snapshot, stage int) Step {
	switch stage {
	case PlagueNotStarted:
		return plagueTalk(s, CustomPlagueStart, PlagueEdmondStart, "ask Edmond about his daughter")
	case PlagueStarted:
		return plagueDwellberries(s)
	case PlagueGasMask:
		return plagueTalk(s, CustomPlagueEdmond, PlagueEdmondTalk, "ask Edmond about the way into West Ardougne")
	case PlagueFreed:
		return plagueTalk(s, CustomPlagueFinish, PlagueEdmondTalk, "tell Edmond his daughter is safe")
	}
	if stage < PlagueWater || stage > PlagueCured {
		return Step{Kind: StepWait, Reason: fmt.Sprintf("Plague City stage %d is not one the module knows", stage)}
	}
	if s.FreeSlots < plagueRoom {
		return Step{Kind: StepDeposit, KeepIDs: plagueKeep(), Bank: plagueBank(s), BankSet: true, Reason: "room for the quest's items"}
	}
	// Jethick gives another book only while neither the pack nor the bank holds one (obj_gettotal), so a banked one comes out first.
	if stage == PlagueShown && s.HeldID(PlagueObjBook) == 0 && plagueOnce(s, KitPlagueBook) {
		return plagueProvision(s, KitPlagueBook, []KitLine{{ID: PlagueObjBook, Name: "Book", Qty: 1, Optional: true, Untradeable: true}},
			"Jethick's book, if the bank holds it")
	}
	// The errands of Ardougne: the spade and picture in Edmond's house, the rope, the bucket.
	if kit := plagueBasics(s, stage); len(kit) > 0 {
		if s.Failed["provision:"+KitPlagueBasics] {
			return Step{Kind: StepWait, Reason: "the spade, rope, bucket or picture could not be had from the bank, the mule, Edmond's floor or the shops"}
		}
		return plagueProvision(s, KitPlagueBasics, kit, "the spade, rope, bucket and picture the dig wants")
	}
	if stage == PlagueWater {
		return plagueLeg(s, CustomPlagueWater, "buckets of water from the market fountain onto the garden soil")
	}
	// The hangover cure is made before the first crossing, from the east: stage 26 would need a second.
	if stage <= PlagueSpokenBravek {
		if st, ok := plagueCure(s); ok {
			return st
		}
	}
	// The gas mask is worn at the pipe, so it is in hand before the dig.
	if stage >= PlaguePipeOpen && s.HeldID(PlagueObjGasMask) == 0 && s.WornIDs[PlagueObjGasMask] == 0 {
		if plagueOnce(s, KitPlagueMask) {
			return plagueProvision(s, KitPlagueMask, []KitLine{{ID: PlagueObjGasMask, Name: "Gas mask", Qty: 1, Optional: true, Untradeable: true}},
				"the gas mask Alrena made, if the bank holds it")
		}
		return plagueLeg(s, CustomPlagueCupboard, "the spare gas mask in Alrena's cupboard")
	}
	return plagueLeg(s, CustomPlagueDig, "dig the garden with the spade: down into the sewer")
}

// plagueDwellberries is stage 1: a dwellberry from the bank, the mule or McGrubor's Wood, then Alrena.
func plagueDwellberries(s Snapshot) Step {
	if s.HeldID(PlagueObjDwellberries) > 0 {
		return plagueTalk(s, CustomPlagueAlrena, PlagueAlrenaStop, "give Alrena the dwellberries")
	}
	if plagueOnce(s, KitPlagueBerries) {
		return plagueProvision(s, KitPlagueBerries, []KitLine{{ID: PlagueObjDwellberries, Name: "Dwellberries", Qty: 1, Optional: true}},
			"a dwellberry, if the bank or the mule holds one")
	}
	if s.FreeSlots < 2 {
		return Step{Kind: StepDeposit, KeepIDs: plagueKeep(), Bank: plagueBank(s), BankSet: true, Reason: "room for a dwellberry"}
	}
	if ok, why := plagueDogsOdds(s); !ok {
		if plagueOnce(s, KitPlagueFood) {
			return plagueProvision(s, KitPlagueFood, plagueFoodKit(), "food for McGrubor's Wood: "+why)
		}
		return Step{Kind: StepWait, ParkFor: PlagueGrassPark, Reason: "not entering McGrubor's Wood: " + why}
	}
	return plagueLeg(s, CustomPlagueBerries, "a dwellberry from McGrubor's Wood, keeping clear of the guard dogs")
}

// plagueDogsOdds is the guard dogs' bite should one find the account in spite of the berth: they hunt up to combat 88, hit for up to
// 5 every four ticks, and the account must be able to take two blows of one for every blow it is likely to meet, which at the berth
// the route keeps is none. The gate asks only that the account is not so weak that one stray dog is the end of it.
func plagueDogsOdds(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	f := FoePlagueGuardDog
	floor := 4 * f.PeakHit()
	hp := s.HP
	if hp <= 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	pool := FoodPoolHeal(s.InvIDs, plagueFoods)
	start := max(hp, min(FightStartHP(s.MaxHP, f.PeakHit()), hp+pool))
	if s.MaxHP < floor || start < floor {
		return false, fmt.Sprintf("a guard dog hits up to %d, and an account with %d of %d hitpoints (%d to top up from) cannot take four blows of one", f.PeakHit(), start, s.MaxHP, pool)
	}
	return true, ""
}

// plagueCure is the cure's assembly, in the east: no step (ok false) once the cure is in hand. The bank and the mule first, once; then
// the mixing as soon as the pack allows one; then the shops' parts (Jatix's pestle, Wydin's bar, the bucket); then the hobgoblins'
// grass and the cow's milk, each only if the recipe still lacks it.
func plagueCure(s Snapshot) (Step, bool) {
	cure, choc, dust := plagueCureParts(s)
	if cure {
		return Step{}, false
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if plagueOnce(s, KitPlagueCureBank) {
		return plagueProvision(s, KitPlagueCureBank, plagueCureBankKit(), "whatever of the hangover cure the bank or the mule holds"), true
	}
	if _, _, ok := PlagueNextMix(held); ok && !s.Failed[CustomPlagueBrew] {
		return Step{Kind: StepCustom, Custom: CustomPlagueBrew, Reason: "mix the next part of the hangover cure"}, true
	}
	if s.FreeSlots < plagueRoom {
		return Step{Kind: StepDeposit, KeepIDs: plagueKeep(), Bank: plagueBank(s), BankSet: true, Reason: "room for the hangover cure's parts"}, true
	}
	if kit := plagueCureShopKit(s); len(kit) > 0 {
		if s.Failed["provision:"+KitPlagueCureShops] {
			return Step{Kind: StepWait, Reason: "no pestle and mortar from Jatix, chocolate bar from Wydin or bucket (an empty shelf, or too few coins)"}, true
		}
		return plagueProvision(s, KitPlagueCureShops, kit, "a pestle and mortar from Taverley, a chocolate bar from Port Sarim and a bucket"), true
	}
	if !held(PlagueObjSnapeGrass) {
		// The three spawns lie on the hobgoblins' trail: entered only by an account the model says gets out of it.
		if ok, why := PlagueGrassOdds(s); !ok {
			if plagueOnce(s, KitPlagueFood) {
				return plagueProvision(s, KitPlagueFood, plagueFoodKit(), "food for the hobgoblins' trail: "+why), true
			}
			return Step{Kind: StepWait, ParkFor: PlagueGrassPark, Reason: "not entering the hobgoblins' trail: " + why}, true
		}
		return plagueLeg(s, CustomPlagueGrass, "a snape grass from the hobgoblins' trail west of the Crafting Guild"), true
	}
	if !choc && !held(ObjBucketOfMilk) {
		return plagueLeg(s, CustomPlagueMilk, "a bucket of milk from a cow in the field east of Ardougne"), true
	}
	_ = dust
	return Step{Kind: StepWait, Reason: "the hangover cure's parts are in hand and nothing can be mixed"}, true
}

// plagueDecideSewer is an account under Ardougne.
func plagueDecideSewer(s Snapshot, stage int) Step {
	switch {
	case stage == PlagueTunnel:
		if s.HeldID(ObjRope) == 0 {
			return plagueHome(s, "no rope for the grill: out to Aemad's for one")
		}
		return plagueLeg(s, CustomPlagueRope, "the rope on the sewer pipe's grill")
	case stage == PlagueRope:
		return plagueTalk(s, CustomPlagueGrill, PlagueEdmondSewer, "have Edmond pull the grill off with the rope")
	case stage >= PlaguePipeOpen && stage <= PlagueCured:
		if why := plagueWestMissing(s, stage); why != "" {
			return plagueHome(s, why)
		}
		return plagueLeg(s, CustomPlagueCross, "the gas mask on, then through the sewer pipe to West Ardougne")
	}
	return plagueHome(s, "the quest has nothing to do under Ardougne at this stage")
}

// plagueWestMissing is why an account about to cross to West Ardougne at this stage is not ready ("" when it is): the mask for the
// pipe, and what the next West leg uses.
func plagueWestMissing(s Snapshot, stage int) string {
	if s.HeldID(PlagueObjGasMask) == 0 && s.WornIDs[PlagueObjGasMask] == 0 {
		return "no gas mask for the pipe"
	}
	switch stage {
	case PlaguePipeOpen:
		if s.HeldID(PlagueObjPicture) == 0 {
			return "no picture of Elena to show Jethick"
		}
	case PlagueSpokenBravek:
		if s.HeldID(PlagueObjHangoverCure) == 0 {
			return "no hangover cure for Bravek"
		}
	}
	return ""
}

// plagueDecideWest is an account in the city.
func plagueDecideWest(s Snapshot, stage int) Step {
	switch stage {
	case PlaguePipeOpen:
		if s.HeldID(PlagueObjPicture) == 0 {
			return plagueHome(s, "no picture of Elena to show Jethick")
		}
		return plagueTalk(s, CustomPlagueJethick, PlagueJethickStop, "show Jethick the picture of Elena")
	case PlagueShown:
		return plagueDecideBook(s)
	case PlagueSpokenParents:
		return plagueLeg(s, CustomPlagueMilli, "up the Rehnisons' stairs and ask Milli what she saw")
	case PlagueSpokenMilli:
		return plagueLeg(s, CustomPlagueMourner, "ask the mourner at the plague house door about clearance")
	case PlagueNeedClearance:
		return plagueLeg(s, CustomPlagueAudience, "the clerk, then Bravek behind his door")
	case PlagueSpokenBravek:
		if s.HeldID(PlagueObjHangoverCure) == 0 {
			return plagueHome(s, "no hangover cure for Bravek")
		}
		return plagueLeg(s, CustomPlagueCure, "give Bravek the hangover cure and ask for the warrant")
	case PlagueCured:
		if s.HeldID(PlagueObjWarrant) == 0 {
			// Bravek gives another to an account whose pack holds none (the pack alone is looked at).
			return plagueLeg(s, CustomPlagueCure, "ask Bravek for another warrant")
		}
		return plagueLeg(s, CustomPlagueHouse, "into the plague house on the warrant, the key from the barrel, the cellar and Elena")
	}
	return plagueHome(s, "the quest has nothing to do in West Ardougne at this stage")
}

// plagueDecideBook is stages 20 and 21, which the journal renders alike. Holding Jethick's book is stage 20 for certain (the Rehnisons
// take it); without it the Rehnisons are asked outright (they talk from stage 21), and an account that finds them silent is at stage
// 20 without its book and must have Jethick give another, which he does for the picture.
func plagueDecideBook(s Snapshot) Step {
	if s.HeldID(PlagueObjBook) > 0 {
		return plagueLeg(s, CustomPlagueBook, "return Jethick's book through the Rehnisons' door")
	}
	// Once the book has gone through the door (this run) it is stage 21 for certain and the Rehnisons talk to anyone, so a try that
	// found them silent before the book was returned does not count: they are asked, and a second silence is the watchdog's.
	if s.Ran[CustomPlagueBook] {
		return Step{Kind: StepCustom, Custom: CustomPlagueParents, Reason: "ask Ted Rehnison about Elena"}
	}
	if !s.Failed[CustomPlagueParents] {
		return plagueLeg(s, CustomPlagueParents, "ask Ted Rehnison about Elena")
	}
	if s.HeldID(PlagueObjPicture) == 0 {
		return plagueHome(s, "the Rehnisons are silent and the book is lost: a picture of Elena to have Jethick give another")
	}
	return plagueTalk(s, CustomPlagueJethick, PlagueJethickStop, "show Jethick the picture again for another book")
}

// plagueDecideUpstairs is an account on the Rehnisons' upper floor: Milli at stage 22, the stairs down otherwise.
func plagueDecideUpstairs(s Snapshot, stage int) Step {
	if stage == PlagueSpokenParents {
		return plagueLeg(s, CustomPlagueMilli, "ask Milli what she saw")
	}
	return plagueLeg(s, CustomPlagueDown, "down the Rehnisons' stairs")
}

// plagueDecideCellar is an account in the plague house's cellar: Elena at stage 27, the stairs up otherwise.
func plagueDecideCellar(s Snapshot, stage int) Step {
	if stage == PlagueCured {
		return plagueLeg(s, CustomPlagueHouse, "Elena's cell")
	}
	return plagueHome(s, "the cellar has nothing more for the quest")
}

// ---------------------------------------------------------------------------------------------------------------
// The module.
// ---------------------------------------------------------------------------------------------------------------

// PlagueCityRecord is the quest's identity. The server gates nothing: no level, no quest, no item.
var PlagueCityRecord = Record{ID: PlagueCityID, Name: PlagueCityName, QuestPoints: 1}

// PlagueCity is the engine module. Two deaths park it for a day; a death on the dogs' or the hobgoblins' ground parks it at once
// for a day (StepDeathCaps). NoRetaliate keeps the account walking when something hits it: the dogs and the hobgoblins are never
// fought, and an answer to a blow would stop a walk at the attacker's feet.
var PlagueCity = Module{
	Record: PlagueCityRecord, Bank: tile.Tile{X: 2616, Z: 3332}, BankSet: true, Anchor: PlagueEdmondAnchor,
	ToolIDs:   plagueTools,
	ReadStage: ParsePlagueJournal,
	Sustain:   Sustain{FoodIDs: plagueFoods, EatAt: 12},
	MaxDeaths: 2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomPlagueGrass:   {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
		CustomPlagueBerries: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	NoRetaliate: true,
	Decide:      DecidePlagueCity,
}

// PlagueCustomNames are the custom steps the module can ask for, for the test that every one has an executor.
func PlagueCustomNames() []string {
	return []string{CustomPlagueStart, CustomPlagueAlrena, CustomPlagueEdmond, CustomPlagueBerries, CustomPlagueCupboard, CustomPlagueWater,
		CustomPlagueDig, CustomPlagueRope, CustomPlagueGrill, CustomPlagueCross, CustomPlagueHome, CustomPlagueJethick, CustomPlagueBook,
		CustomPlagueParents, CustomPlagueMilli, CustomPlagueDown, CustomPlagueMourner, CustomPlagueAudience, CustomPlagueCure,
		CustomPlagueHouse, CustomPlagueFinish, CustomPlagueMilk, CustomPlagueGrass, CustomPlagueBrew, CustomPlagueScroll}
}
