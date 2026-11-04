package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Biohazard (content id `biohazard`, %biohazard, perm and not transmitted), ported from the TS def (defs/biohazard/*.ts) and checked
// against content 289: quests/quest_biohazard/{configs,scripts}, areas/area_ardougne_east/scripts/{elena,king_lathas}.rs2,
// areas/area_ardougne_west/{configs,scripts}/{mourner,doors,omart,kilron,nurse_sarah}, areas/area_varrock/scripts/east_gate.rs2 and the
// maps of both Ardougnes, Rimmington and Varrock. Three quest points and 1,250 Thieving experience (stat_advance 12,500 tenths), the
// Plague City prerequisite (Elena is at home in East Ardougne once she is freed). 16 stages, two crossings of the plague wall, a
// headquarters that opens only for a doctor, and a chemist's errand through Rimmington and Varrock.
//
// The quest, stage by stage (%biohazard):
//
//	 0 Elena (East Ardougne) asks for her distillator, which the mourners confiscated and hold in West Ardougne.
//	 1 Jerico (beside the chapel) has two friends with a rope ladder at the south end of the wall; the watch tower must be distracted.
//	 2 Jerico's cupboard has a bag of bird feed (it gives one only while neither the pack nor the bank holds one); the bag used on the
//	   watch tower's wall moves the stage on (3).
//	 3 A pigeon cage lies on the ground behind Jerico's house (three spawns). Opened ("Open") inside the box 2559-2565 x 3299-3307 it
//	   sends the pigeons to the tower (4); anywhere else "the pigeons don't want to leave".
//	 4 Omart (south end of the wall, east side): "Ok, lets do it." climbs the account over the wall to 2556,3267 (5). He offers it at
//	   stages 4 to 7 only; Kilron, on the west side, brings the account back from stage 5 on ("Yes I do.", to 2559,3267), forever.
//	 5 Rotten apples lie on the ground in West Ardougne (2535,3333, one spawn) and go into the mourners' cauldron in the headquarters'
//	   yard (6). The yard is reached by the fence at 2541,3331 ("Climb-over"); the headquarters itself only through its door.
//	 6 The nurse's cupboard in the hut at 2517,3276 gives a doctors' gown (only from stage 6 on and only while the pack, the bank and
//	   the worn items hold none: a banked gown is a refusal). Worn, it opens the headquarters' door ("In you go doc.", stages 6 and 7
//	   only), the door upstairs and the sick mourners' talk. The sick mourner (two stand upstairs, level 13, 19 hitpoints) asks
//	   "What can you give me?": every answer ends with "You're no doctor!" and the mourner turns on the account (~npc_retaliate), and
//	   his death hands over the key (defeat_biohazard_mourner), to the account he is aggressive to. The key opens the double gate
//	   beside him; the crate behind it holds Elena's distillator (7). Leaving the headquarters needs nothing.
//	 7 Elena takes the distillator and, if the pack has room (an add is skipped unless inv_freespace > 1, so five free slots for the
//	   four), hands back the plague sample and three vials (ethenea, liquid honey, sulphuric broline) (10).
//	10 The Chemist (Rimmington) sells touch paper to a friend of Elena's who is not carrying the sample ("It's ok, I'm Elena's
//	   friend." then "I just need some touch paper for a guy called Guidor."; the first menu's other line, and the second's, make him
//	   confiscate the sample) (12). With no sample he is closing and says so.
//	12 The Varrock guards search for a plague carrier: the east gate (3264,3405-3406) takes every vial from an account that passes it
//	   in either direction while the stage is 10 to 13, so the vials go to the Chemist's three errand boys outside his house (Hops
//	   takes the broline, Chancy the honey, DeVinci the ethenea; any other pairing and the boy loses the vial). They wait at the
//	   Dancing Donkey inn behind the gate; Guidor's wife lets in only a priest (priest gown and priest robe worn, from Thessalia's
//	   shop at 5 coins each) to Guidor, who tests the sample with the three vials and the touch paper (14).
//	14 Elena is told what Guidor found (15).
//	15 King Lathas (upstairs in the castle) is confronted; the second of two answers ends the talk without the completion (16).
//
// What a script must plan for:
//
//   - Nearly every conversation holds a message box or a pause longer than a page turn (Omart's five beats before the crossing, the
//     cauldron's, the gate guard's search): they are driven by questmods' plagueDrive, which continues pages and boxes and answers
//     menus, to a goal (a stage, an item, a place) and never to silence.
//   - West Ardougne is a pocket (Plague City's): the account is put there by Omart and brought back by Kilron (or, if he refuses, by the
//     manhole and the sewer's mud pile, Plague City's own way home). The headquarters and its upper floor are a pocket inside it
//     (their doors are the scripts', no walk crosses them), registered on their own with a Leave that goes out by the door.
//   - The only fight is the sick mourner. The other mourners (level 24 and 12 downstairs, 18 at the doors) are never talked to or
//     attacked: they are passive until a talk without the gown sets them on the account (mournerstew3), and the account never
//     takes the gown off, never talks to them, and never answers one that attacks (NoRetaliate). The sick mourner is priced
//     (BioKeyOdds) before the account crosses and again before it climbs, on the hitpoints and food it holds, and a refusal parks
//     the quest; the talk itself is a message box the account cannot eat through, so the account goes in topped up.
//   - The vials are lost at the gate; the sample is lost to the Chemist's wrong option; the gown is lost to a banked copy. Elena
//     replaces vials and sample ("I'm afraid I've lost some of the stuff that you gave me...", stages 10 and 12) and the stash kits
//     draw banked copies out first.
//   - The quest is restartable from any state: nothing is remembered, every decision is the journal, the tile and the pack.

// BiohazardName is the quest tab's spelling (component 7352, `questlist:biohazard`).
const BiohazardName = "Biohazard"

// BiohazardID is the engine's id, content's own.
const BiohazardID = "biohazard"

// The %biohazard values (quest_biohazard.constant, ^biohazard_complete = 16 in general/configs/quest.constant). 8, 9, 11 and 13 are unused.
const (
	BioNotStarted       = 0
	BioStarted          = 1  // Jerico
	BioSpokenJerico     = 2  // the bird feed
	BioUsedBirdfeed     = 3  // the pigeons
	BioReleasedPigeons  = 4  // Omart's ladder
	BioClimbedLadder    = 5  // the rotten apples and the cauldron
	BioPoisonedStew     = 6  // the gown, the headquarters, the key and the crate
	BioFoundDistillator = 7  // back to Elena
	BioGivenDistillator = 10 // the Chemist
	BioSpokenChemist    = 12 // the errand boys, the priest's gown, Guidor
	BioFoundSecret      = 14 // Elena
	BioReportedElena    = 15 // King Lathas
	BioComplete         = 16
)

// Ids from content/pack (checked against the production config by TestBiohazardIdsMatchTheProductionConfig).
const (
	BioNpcElena       = 335 // [elena2], at home; [elenap] 715 is her in the plague house
	BioNpcJerico      = 366
	BioNpcOmart       = 350
	BioNpcKilron      = 349
	BioNpcChemist     = 367
	BioNpcHopsRim     = 340 // [drunk1], outside the Chemist's house; [drunk2] 341 is him at the Dancing Donkey
	BioNpcHopsVar     = 341
	BioNpcDeVinciRim  = 336 // [artist1]; [artist2] 337
	BioNpcDeVinciVar  = 337
	BioNpcChancyRim   = 338 // [gambler1]; [gambler2] 339
	BioNpcChancyVar   = 339
	BioNpcGuidor      = 343
	BioNpcGuidorsWife = 342
	BioNpcGuard       = 368 // [bioguard1], by the east gate
	BioNpcThessalia   = 548
	BioNpcLathas      = 364
	NpcSickMourner    = 370 // [mournerstew2], two upstairs in the headquarters
	BioNpcMournerDown = 369 // [mournerstew3], level 24, and 371 [mournerstew1], level 12: the dinner downstairs, never talked to
	BioNpcMournerLow  = 371
	BioNpcMournerArm  = 357 // [mourner_armed], level 18, at the headquarters' doors
	BioNpcZombie      = 73  // [zombie_unarmed], level 13, and 75 [zombie_armed], level 24, and 103 [ghost], level 19, in the west of the city
	BioNpcZombieArmed = 75
	BioNpcGhost       = 103

	BioObjEthenea      = 415 // "Ethanea" in this build
	BioObjLiquidHoney  = 416
	BioObjBroline      = 417
	BioObjSample       = 418
	BioObjTouchPaper   = 419
	BioObjDistillator  = 420
	BioObjBirdfeed     = 422
	BioObjKey          = 423
	BioObjPigeons      = 424 // the full cage, Open; the empty cage 425 shares its name
	BioObjEmptyCage    = 425
	BioObjPriestGown   = 426 // both halves render "Priest gown"
	BioObjPriestRobe   = 428
	BioObjDoctorGown   = 430
	BioObjRottenApples = 1984 // Eat: the module's tool list keeps the eat loop off it

	BioLocJericoCupboardShut = 2056
	BioLocJericoCupboardOpen = 2057
	BioLocWatchtower         = 2067
	BioLocCauldron           = 2043
	BioLocNurseCupboardShut  = 2062
	BioLocNurseCupboardOpen  = 2063
	BioLocHQDoor             = 2036 // the street door (2551,3320) and the back door (2551,3328)
	BioLocHQUpDoor           = 2034 // 2547,3325 upstairs
	BioLocHQGateRight        = 2060 // 2551,3325 upstairs
	BioLocHQGateLeft         = 2058 // 2551,3326 upstairs
	BioLocHQCrate            = 2064
	BioLocHQStairs           = 1738 // the spiral stairs up from the ground floor, at 2542,3324
	BioLocYardFence          = 2068
	BioLocVarrockGateA       = 2050 // 3264,3405
	BioLocVarrockGateB       = 2051 // 3264,3406
	BioLocGuidorDoor         = 2032
)

// The places of the quest. Tiles are content's: spawns from the maps (m39_51, m40_51, m40_52 and the others), locs from the loc index.
var (
	BioElenaAnchor  = tile.Tile{X: 2592, Z: 3336}
	BioJericoAnchor = tile.Tile{X: 2612, Z: 3324}
	// BioJericoCupboard is the cupboard (2611,3326, two long, angle 1, forceapproach east turned to the south): searched from 2611,3325.
	BioJericoCupboard = tile.Tile{X: 2611, Z: 3326}
	// BioPigeonSpawns are the three full cages behind Jerico's house; BioWatchtower is the tower's east wall (x 2563, z 3302-3306).
	BioPigeonSpawns = []tile.Tile{{X: 2618, Z: 3324}, {X: 2618, Z: 3323}, {X: 2618, Z: 3325}}
	BioWatchtower   = tile.Tile{X: 2563, Z: 3304}
	BioOmartAnchor  = tile.Tile{X: 2559, Z: 3266}
	BioKilronAnchor = tile.Tile{X: 2556, Z: 3266}
	// BioWestLanding and BioEastLanding are where the ladder puts the account (biohazard_climb_ladder's two targets).
	BioWestLanding = tile.Tile{X: 2556, Z: 3267}
	BioEastLanding = tile.Tile{X: 2559, Z: 3267}
	BioApples      = tile.Tile{X: 2535, Z: 3333}
	BioCauldron    = tile.Tile{X: 2543, Z: 3332} // the loc; the yard tile beside it is the stand
	BioCauldronAt  = tile.Tile{X: 2542, Z: 3332}
	// BioNurseCupboard is the cupboard in the hut (2517,3276); the hut's door is the ordinary door at 2519,3275.
	BioNurseCupboard = tile.Tile{X: 2517, Z: 3276}
	// BioHQDoorOutside is the loc's own tile (the street door's wall is on its north edge) and BioHQDoorInside the tile the crossing
	// lands on; BioHQStairs is the spiral stairs and BioHQUpLanding the first floor tile above them.
	BioHQDoorOutside = tile.Tile{X: 2551, Z: 3320}
	BioHQDoorInside  = tile.Tile{X: 2551, Z: 3321}
	BioHQStairs      = tile.Tile{X: 2542, Z: 3324}
	BioHQUpLanding   = tile.Tile{X: 2543, Z: 3326, Level: 1}
	// BioSickRoom is the room upstairs where the sick mourners stand (2551,3324 and 2551,3327, level 1), west of the double gate.
	BioSickRoom   = tile.Tile{X: 2550, Z: 3326, Level: 1}
	BioGate       = tile.Tile{X: 2551, Z: 3326, Level: 1}
	BioCrate      = tile.Tile{X: 2554, Z: 3327, Level: 1}
	BioCrateStand = tile.Tile{X: 2554, Z: 3326, Level: 1}
	BioLathas     = tile.Tile{X: 2578, Z: 3293, Level: 1}
	BioChemist    = tile.Tile{X: 2934, Z: 3210}
	BioHopsRim    = tile.Tile{X: 2931, Z: 3219}
	BioDeVinciRim = tile.Tile{X: 2933, Z: 3219}
	BioChancyRim  = tile.Tile{X: 2935, Z: 3219}
	BioThessalia  = tile.Tile{X: 3204, Z: 3417}
	// BioGateOutside and BioGateInside are the tiles either side of the northern leaf of Varrock's east gate (the loc at 3264,3406).
	BioGateOutside = tile.Tile{X: 3263, Z: 3406}
	BioGateInside  = tile.Tile{X: 3265, Z: 3406}
	BioDonkey      = tile.Tile{X: 3270, Z: 3390}
	BioHopsVar     = tile.Tile{X: 3268, Z: 3389}
	BioChancyVar   = tile.Tile{X: 3270, Z: 3391}
	BioDeVinciVar  = tile.Tile{X: 3274, Z: 3390}
	BioGuidorDoor  = tile.Tile{X: 3282, Z: 3382}
	BioGuidor      = tile.Tile{X: 3285, Z: 3381}
	// BioPigeonZone is the box inside which the cage opens (opheld1,pigeons: inzone 0_39_51_63_35 to 0_40_51_5_43).
	BioPigeonZone = Rect{MinX: 2559, MaxX: 2565, MinZ: 3299, MaxZ: 3307}
)

// BioThessaliaShop is the clothes shop (inv `clotheshop`: three priest robes and three priest gowns, back after a hundred ticks).
var BioThessaliaShop = ShopRef{Npc: "Thessalia", NpcID: BioNpcThessalia, Anchor: BioThessalia}

// BioHunter is one of the aggressive spawns of West Ardougne's west end: a zombie or a ghost that notices a player within HuntRange tiles
// of where it stands, wandering up to Wander tiles from its spawn, and hunts only an account up to twice its level (cowardly, outside
// the Wilderness). None is near a place the quest works; the real-pack test keeps every walk of the quest clear of their boxes.
type BioHunter struct {
	Name          string
	NPC           int
	Spawn         tile.Tile
	Wander, Level int
}

// BioHuntRange is the tiles from where it stands that a hunter notices a player (huntrange=1).
const BioHuntRange = 1

// Box is every tile the hunter can stand on, and Reach every tile it can notice a player on.
func (h BioHunter) Box() Rect {
	return Rect{MinX: h.Spawn.X - h.Wander, MaxX: h.Spawn.X + h.Wander, MinZ: h.Spawn.Z - h.Wander, MaxZ: h.Spawn.Z + h.Wander, Level: h.Spawn.Level}
}

// Reach is Box widened by BioHuntRange.
func (h BioHunter) Reach() Rect {
	b := h.Box()
	return Rect{MinX: b.MinX - BioHuntRange, MaxX: b.MaxX + BioHuntRange, MinZ: b.MinZ - BioHuntRange, MaxZ: b.MaxZ + BioHuntRange, Level: b.Level}
}

// BioWestHunters are the seven (maps/m39_51.jm2: [zombie_unarmed] level 13 and [zombie_armed] level 24, wanderrange 9; [ghost] level
// 19, wanderrange 8; all huntrange 1, huntmode cowardly), at the city's west edge, x 2497 to 2511.
var BioWestHunters = []BioHunter{
	{"Zombie", BioNpcZombie, tile.Tile{X: 2499, Z: 3286}, 9, 13}, {"Zombie", BioNpcZombie, tile.Tile{X: 2500, Z: 3283}, 9, 13},
	{"Zombie", BioNpcZombie, tile.Tile{X: 2506, Z: 3284}, 9, 13}, {"Armed zombie", BioNpcZombieArmed, tile.Tile{X: 2497, Z: 3292}, 9, 24},
	{"Armed zombie", BioNpcZombieArmed, tile.Tile{X: 2511, Z: 3287}, 9, 24}, {"Ghost", BioNpcGhost, tile.Tile{X: 2501, Z: 3289}, 8, 19},
	{"Ghost", BioNpcGhost, tile.Tile{X: 2501, Z: 3294}, 8, 19},
}

// BioHunterZones are the rectangles a walk in West Ardougne keeps out of: every hunter's Reach (the box it wanders in, widened by the
// tiles it notices a player from). The armed zombie at 2511,3287 reaches down to z 3277 and east to x 2521, which is where the nurse's
// hut lets out, so the walks from the hut go round it.
func BioHunterZones() []Rect {
	out := make([]Rect, 0, len(BioWestHunters))
	for _, h := range BioWestHunters {
		out = append(out, h.Reach())
	}
	return out
}

// ---------------------------------------------------------------------------------------------------------------
// Where the account is.
// ---------------------------------------------------------------------------------------------------------------

// BioArea is where the account stands, as far as the quest's choices go.
type BioArea int

const (
	// BioEast is everywhere the planner walks: East Ardougne, Rimmington, Varrock and the roads between. The account is on the side
	// of the wall (and of Varrock's gate) every walk of the quest is meant for.
	BioEast BioArea = iota
	// BioQuarter is Varrock's east quarter behind the guarded gate: the Dancing Donkey, Guidor's house and nothing else.
	BioQuarter
	// BioWest is West Ardougne's streets and the headquarters' yard.
	BioWest
	// BioHQ is the headquarters' ground floor and BioHQUp its first floor: rooms whose doors are the scripts'.
	BioHQ
	BioHQUp
	// BioPocket is the sewer under East Ardougne, the Rehnisons' upper floor and the plague house's cellar: Plague City's places,
	// which only Plague City's own way home leaves.
	BioPocket
)

// String names the area for the log.
func (a BioArea) String() string {
	return [...]string{"the mainland", "Varrock's east quarter", "West Ardougne", "the Mourners' headquarters", "the headquarters' first floor",
		"the Ardougne sewer or a plague house"}[a]
}

// bioHQRooms are the two rectangles the headquarters' rooms lie in, on each of its floors: the dining hall and the room above it
// (x 2547-2555, z 3321-3327) and the room with the stairs and the one above it (x 2542-2546, z 3324-3327). The street runs round them
// (x 2542-2546 at z 3321-3323 is outside). Floods of the collision pack give exactly these tiles
// (TestBiohazardAreasAreTheCollisionPacksComponents).
var bioHQRooms = []Rect{{MinX: 2547, MaxX: 2555, MinZ: 3321, MaxZ: 3327}, {MinX: 2542, MaxX: 2546, MinZ: 3324, MaxZ: 3327}}

// InBioHQ reports a tile in the headquarters on either of its floors.
func InBioHQ(t tile.Tile) bool {
	if t.Level > 1 {
		return false
	}
	for _, r := range bioHQRooms {
		if t.X >= r.MinX && t.X <= r.MaxX && t.Z >= r.MinZ && t.Z <= r.MaxZ {
			return true
		}
	}
	return false
}

// InBioQuarter reports a tile in Varrock's east quarter: x 3264 (the gate's own tile) to 3287, z 3376 to 3407, the flood of the
// collision pack from the Dancing Donkey. Guidor's room lies inside it.
func InBioQuarter(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 3264 && t.X <= 3287 && t.Z >= 3376 && t.Z <= 3407
}

// InBioGuidorsRoom reports a tile in Guidor's room (x 3283-3286, z 3380-3383), behind the door his wife guards: the flood of the collision
// pack from his tile.
func InBioGuidorsRoom(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 3283 && t.X <= 3286 && t.Z >= 3380 && t.Z <= 3383
}

// BioAreaOf classifies a tile.
func BioAreaOf(t tile.Tile) BioArea {
	switch {
	case InBioHQ(t) && t.Level == 1:
		return BioHQUp
	case InBioHQ(t):
		return BioHQ
	case InBioQuarter(t):
		return BioQuarter
	}
	switch PlagueAreaOf(t) {
	case PlagueWest:
		return BioWest
	case PlagueEast:
		return BioEast
	}
	return BioPocket
}

// ---------------------------------------------------------------------------------------------------------------
// Custom steps, kits and keeps.
// ---------------------------------------------------------------------------------------------------------------

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomBioStart     = "bio-start"    // Elena: the quest begins
	CustomBioJerico    = "bio-jerico"   // Jerico: how to cross the wall
	CustomBioBirdfeed  = "bio-birdfeed" // the bag from Jerico's cupboard
	CustomBioFeed      = "bio-feed"     // the bag on the watch tower
	CustomBioPigeons   = "bio-pigeons"  // a cage from behind Jerico's house
	CustomBioRelease   = "bio-release"  // the cage opened inside the box
	CustomBioCross     = "bio-cross"    // Omart's ladder, east to west
	CustomBioApples    = "bio-apples"   // rotten apples from the ground
	CustomBioStew      = "bio-stew"     // the apples in the cauldron
	CustomBioGown      = "bio-gown"     // the doctors' gown from the nurse's cupboard
	CustomBioHQ        = "bio-hq"       // the gown on, in by the headquarters' door
	CustomBioUp        = "bio-up"       // across the hall, up the stairs, through the upper door
	CustomBioKey       = "bio-key"      // the sick mourner: the talk, the fight, the key
	CustomBioCrate     = "bio-crate"    // the gate with the key and the crate
	CustomBioLeaveHQ   = "bio-leave-hq" // out by the door to the street
	CustomBioHome      = "bio-home"     // west to east: Kilron, or the manhole
	CustomBioHandBack  = "bio-handback" // the distillator to Elena
	CustomBioReplace   = "bio-replace"  // Elena replaces what was lost
	CustomBioChemist   = "bio-chemist"  // touch paper
	CustomBioErrands   = "bio-errands"  // the vials to the three errand boys
	CustomBioGateIn    = "bio-gate-in"  // Varrock's east gate, inward
	CustomBioCollect   = "bio-collect"  // the vials back at the Dancing Donkey
	CustomBioGuidor    = "bio-guidor"   // the priest, Guidor, the test
	CustomBioGateOut   = "bio-gate-out" // Varrock's east gate, outward
	CustomBioReport    = "bio-report"   // Elena told what Guidor found
	CustomBioLathas    = "bio-lathas"   // King Lathas
	KitBioStashEarly   = "bio-stash-1"  // the bird feed and the cage, if the bank holds them
	KitBioStashHQ      = "bio-stash-2"  // the gown, the key, the distillator, the apples
	KitBioStashVarrock = "bio-stash-3"  // the sample, the vials, the paper, the priest's gown
	KitBioFood         = "bio-food"     // sharks for the sick mourner
	KitBioCoins        = "bio-coins"    // the ship fares and the priest's gown
	KitBioSuit         = "bio-suit"     // the priest's gown and robe
)

// The quantities the decisions use.
const (
	// BioCoinsQty is what the coins pass draws and BioCoinsMin the least the trip is made on: Barnaby and the customs officer take 30
	// a crossing, twice each way over the whole quest, and the priest's suit is ten.
	BioCoinsQty = 300
	BioCoinsMin = 160
	// BioRoom is the slots a crossing wants free, BioHandbackSlots Elena's hand-back (she skips an add unless inv_freespace > 1, so
	// five free once the distillator has gone) and BioRoomVarrock the Chemist's errand (the paper, the priest's two halves, the coins).
	BioRoom          = 4
	BioHandbackSlots = 6
	BioRoomVarrock   = 5
	// BioFoodQty is the sharks asked for the fight.
	BioFoodQty = 4
	// BioKeyPark is how long a refused fight leaves the quest alone; BioSupplyPark how long a missing float or suit does.
	BioKeyPark    = 6 * time.Hour
	BioSupplyPark = 3 * time.Hour
)

// bioTools are the quest's own items, which every bank visit of the quest leaves in the pack and the eat loop never eats (the rotten
// apples have an Eat op).
var bioTools = []int{CoinsID, BioObjEthenea, BioObjLiquidHoney, BioObjBroline, BioObjSample, BioObjTouchPaper, BioObjDistillator,
	BioObjBirdfeed, BioObjKey, BioObjPigeons, BioObjEmptyCage, BioObjPriestGown, BioObjPriestRobe, BioObjDoctorGown, BioObjRottenApples}

// BioToolIDs returns the tool list (a copy).
func BioToolIDs() []int { return append([]int(nil), bioTools...) }

// BioFoodIDs returns the food list (a copy): Plague City's.
func BioFoodIDs() []int { return PlagueFoodIDs() }

// bioKeep is what a bank visit of this quest leaves in the pack: the tools and the food.
func bioKeep() []int { return append(append([]int(nil), bioTools...), plagueFoods...) }

// BioKeepIDs returns the keep list (a copy).
func BioKeepIDs() []int { return bioKeep() }

// bioVials are Guidor's three reagents.
var bioVials = []int{BioObjBroline, BioObjEthenea, BioObjLiquidHoney}

// BioVials returns the vials in errand order (a copy).
func BioVials() []int { return append([]int(nil), bioVials...) }

// ---------------------------------------------------------------------------------------------------------------
// The journal.
// ---------------------------------------------------------------------------------------------------------------

// bioJournalNeedles are ParseBiohazardJournal's phrases, newest stage first, written against NormaliseLines (biohazard_journal.rs2:
// finished lines are struck through but kept, so a later stage holds the earlier ones' words and only this order separates them).
var bioJournalNeedles = []struct {
	stage  int
	needle string
}{
	{BioComplete, "quest complete"},
	{BioReportedElena, "i need to confront the"},
	{BioFoundSecret, "findings were very interesting"},
	{BioSpokenChemist, "the varrock guards are out looking for someone"},
	{BioGivenDistillator, "elena's asked me to take the following items to"},
	{BioFoundDistillator, "i managed to find elena's distillator"},
	{BioPoisonedStew, "i have rather unkindly poisoned the mourners' stew"},
	{BioClimbedLadder, "i've crossed the wall into west ardougne"},
	{BioReleasedPigeons, "the watch tower is now surrounded by flapping pigeons"},
	{BioUsedBirdfeed, "i've chucked some birdfeed onto the watch tower"},
	{BioSpokenJerico, "will be able to get me over the wall"},
	{BioStarted, "about getting over the wall and into west ardougne"},
	{BioNotStarted, "i can start this quest by speaking to"},
}

// ParseBiohazardJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised journal, which is
// unknown, never stage 0.
func ParseBiohazardJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range bioJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------------------------------------------
// The one fight.
// ---------------------------------------------------------------------------------------------------------------

// FoeSickMourner is [mournerstew2] (scripts/areas/area_ardougne_west/configs/mourner.npc, npc 370): level 13, attack 10, defence 10,
// strength 10, 19 hitpoints, no attackrate (4), no bonus params (a maximum hit of 2), defence bonuses 3/2/4. Two stand upstairs in the
// Mourners' headquarters (2551,3324 and 2551,3327); one of them, talked to in a doctor's gown, turns on the account and drops the key
// when he dies (mourner.rs2 [opnpc1,mournerstew2], [ai_queue3,mournerstew2], [queue,defeat_biohazard_mourner]).
var FoeSickMourner = MeleeFoe{Name: "Sick mourner", Attack: 10, Strength: 10, Defence: 10, Magic: 1, Hitpoints: 19, AttackTicks: 4, DefenceBonus: 2}

// BioKeyOdds prices the fight with the sick mourner for the account in the snapshot: from the hitpoints it goes in with, topped up
// from the food it carries to the level a fight starts at, it must win, keep the hardest hit (2) three times over at the worst of the
// fight and, if it has to eat at all, keep a bite for the way out. An account that needs no food for a foe this weak is not made to
// carry any. A snapshot with no levels read passes: nothing is judged on an unread stat.
func BioKeyOdds(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 || s.Level("attack") <= 0 || s.Level("strength") <= 0 {
		return true, ""
	}
	f := FoeSickMourner
	fighter := fighterOf(s)
	phase, ok := MeleePhase(fighter, f)
	if !ok {
		return false, "the account has no way to hurt the sick mourner"
	}
	hp := s.HP
	if hp <= 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	bites := FoodBites(s.InvIDs, plagueFoods)
	total := len(bites)
	for hp < FightStartHP(s.MaxHP, f.PeakHit()) && len(bites) > 0 {
		hp = min(hp+bites[0], s.MaxHP)
		bites = bites[1:]
	}
	res := SimulateFight(FightSimInput{Foe: f, Fighter: fighter, HP: hp, MaxHP: s.MaxHP, Phases: []FightPhase{phase}, Bites: bites,
		EatLine: FoeEatLine(12, f.PeakHit(), s.MaxHP)})
	switch {
	case !res.Won:
		return false, fmt.Sprintf("the sick mourner (level 13, hits up to %d) beats an account at Attack %d, Strength %d, Defence %d with %d of %d hitpoints and %d bites: the model has it %s after %d ticks",
			f.PeakHit(), s.Level("attack"), s.Level("strength"), s.Level("defence"), hp, s.MaxHP, len(bites), res.How, res.Ticks)
	case res.MinHP < float64(3*f.PeakHit()):
		return false, fmt.Sprintf("the model has an account at %d of %d hitpoints at %.0f at the worst of the fight with the sick mourner, under three of his hardest hits (%d)",
			hp, s.MaxHP, res.MinHP, f.PeakHit())
	case res.BitesUsed > 0 && len(bites)-res.BitesUsed < fightSpareBites(total):
		return false, fmt.Sprintf("the model uses %d of the %d bites the account holds against the sick mourner and keeps none for the way out", res.BitesUsed, len(bites))
	}
	return true, ""
}

// ---------------------------------------------------------------------------------------------------------------
// The stops.
// ---------------------------------------------------------------------------------------------------------------

// The conversations. The preferences are matched as substrings of the options, in order; a menu with none of them stops the
// conversation rather than guessing (TestBiohazardContentDialogue spells every menu out).
var (
	// BioElenaStart: "I'll try to retrieve it for you." (the other is "Well, good luck.").
	BioElenaStart = NpcStop{Npc: "Elena", ID: BioNpcElena, Anchor: BioElenaAnchor, Leash: 5, Prefer: []string{"I'll try to retrieve it for you."}}
	// BioElenaTalk is every talk of hers with no menu: the distillator, the report and the stages in between.
	BioElenaTalk = NpcStop{Npc: "Elena", ID: BioNpcElena, Anchor: BioElenaAnchor, Leash: 5}
	// BioElenaReplace: "I'm afraid I've lost some of the stuff that you gave me..." (the others are the goodbye and "I've forgotten what I need to do.").
	BioElenaReplace = NpcStop{Npc: "Elena", ID: BioNpcElena, Anchor: BioElenaAnchor, Leash: 5, Prefer: []string{"I've lost some of the stuff"}}
	BioJericoStop   = NpcStop{Npc: "Jerico", ID: BioNpcJerico, Anchor: BioJericoAnchor, Leash: 5}
	// BioOmartStop: "Ok, lets do it." (the other is "I'll be back soon."). BioKilronStop: "Yes I do." (the other is "Not yet Kilron.").
	BioOmartStop  = NpcStop{Npc: "Omart", ID: BioNpcOmart, Anchor: BioOmartAnchor, Leash: 4, Prefer: []string{"Ok, lets do it."}}
	BioKilronStop = NpcStop{Npc: "Kilron", ID: BioNpcKilron, Anchor: BioKilronAnchor, Leash: 4, Prefer: []string{"Yes I do."}}
	// BioMournerStop is the sick mourner's talk: "There's nothing I can do, it's fatal." is the shortest of the three that all end in
	// "You're no doctor!" and the retaliation.
	BioMournerStop = NpcStop{Npc: "Mourner", ID: NpcSickMourner, Anchor: BioSickRoom, Leash: 6, Prefer: []string{"There's nothing I can do, it's fatal."}}
	// BioChemistStop: "It's ok, I'm Elena's friend." (the other first line hands him the sample), then "I just need some touch paper for
	// a guy called Guidor." (the other hands him the sample too). At stage 12 he gives another sheet with no menu at all.
	BioChemistStop = NpcStop{Npc: "Chemist", ID: BioNpcChemist, Anchor: BioChemist, Leash: 4,
		Prefer: []string{"It's ok, I'm Elena's friend.", "for a guy called Guidor"}}
	// BioGuidorStop: "I've come to ask your assistance in stopping a plague.", "I've been sent by your old pupil Elena." and
	// "So what does that mean exactly?" (the last menu comes after the stage has moved, and either line serves).
	BioGuidorStop = NpcStop{Npc: "Guidor", ID: BioNpcGuidor, Anchor: BioGuidor, Leash: 4,
		Prefer: []string{"I've come to ask your assistance in stopping a plague.", "I've been sent by your old pupil Elena.", "So what does that mean exactly?"}}
	// BioLathasStop: "I don't understand..." (the other, "Well I've wasted enough of my time here.", ends the talk without the completion).
	BioLathasStop = NpcStop{Npc: "King Lathas", ID: BioNpcLathas, Anchor: BioLathas, Leash: 5, Prefer: []string{"I don't understand"}}
)

// BioErrand is one of the Chemist's errand boys: the boy outside his house, the same man at the Dancing Donkey, the vial he can be
// trusted with and the menu line that gives it (hops.rs2, chancy.rs2, devinci.rs2: Hops drinks anything but the broline, Chancy sells
// anything but the honey, DeVinci paints with anything but the ethenea, and a wrong pairing loses the vial for good).
type BioErrand struct {
	Name     string
	Vial     int
	VialName string
	Give     string
	Rim, Var NpcStop
}

// BioErrands are the three boys in the Chemist's order.
var BioErrands = []BioErrand{
	{Name: "Hops", Vial: BioObjBroline, VialName: "sulphuric broline", Give: "vial of sulphuric broline",
		Rim: NpcStop{Npc: "Hops", ID: BioNpcHopsRim, Anchor: BioHopsRim, Leash: 5, Prefer: []string{"vial of sulphuric broline"}},
		Var: NpcStop{Npc: "Hops", ID: BioNpcHopsVar, Anchor: BioHopsVar, Leash: 6, Prefer: bioCollectPrefer}},
	{Name: "DeVinci", Vial: BioObjEthenea, VialName: "ethenea", Give: "vial of ethenea",
		Rim: NpcStop{Npc: "DeVinci", ID: BioNpcDeVinciRim, Anchor: BioDeVinciRim, Leash: 5, Prefer: []string{"vial of ethenea"}},
		Var: NpcStop{Npc: "DeVinci", ID: BioNpcDeVinciVar, Anchor: BioDeVinciVar, Leash: 6, Prefer: bioCollectPrefer}},
	{Name: "Chancy", Vial: BioObjLiquidHoney, VialName: "liquid honey", Give: "vial of liquid honey",
		Rim: NpcStop{Npc: "Chancy", ID: BioNpcChancyRim, Anchor: BioChancyRim, Leash: 5, Prefer: []string{"vial of liquid honey"}},
		Var: NpcStop{Npc: "Chancy", ID: BioNpcChancyVar, Anchor: BioChancyVar, Leash: 6, Prefer: bioCollectPrefer}},
}

// bioCollectPrefer answers the one menu a boy who spoilt his vial opens, both branches harmless.
var bioCollectPrefer = []string{"No! Nothing could be further from the truth!", "I'm getting a bad feeling about this."}

// ---------------------------------------------------------------------------------------------------------------
// The kits.
// ---------------------------------------------------------------------------------------------------------------

// bioBank is the bank a pass uses: the one nearest the account, East Ardougne's north bank before a position has arrived.
func bioBank(s Snapshot) tile.Tile {
	if s.TileKnown && BioAreaOf(s.Tile) == BioEast {
		return NearestBank(s.Tile).Tile
	}
	return tile.Tile{X: 2616, Z: 3332}
}

// bioProvision is a provision step for a kit with the module's keep list and the nearest bank.
func bioProvision(s Snapshot, name string, kit []KitLine, reason string) Step {
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: bioKeep(), Bank: bioBank(s), BankSet: true, Reason: reason}
}

// bioDeposit is a deposit step: everything that is not the quest's.
func bioDeposit(s Snapshot, reason string) Step {
	return Step{Kind: StepDeposit, KeepIDs: bioKeep(), Bank: bioBank(s), BankSet: true, Reason: reason}
}

// bioQuestItem is a kit line for a quest item a bank visit may have left in the bank: wanted only if the bank has it.
func bioQuestItem(id int, name string) KitLine {
	return KitLine{ID: id, Name: name, Qty: 1, Optional: true, Untradeable: true}
}

// bioStashEarly is the first stash: the bag and the cage, before the cupboard and the spawn are asked for another.
func bioStashEarly() []KitLine {
	return []KitLine{bioQuestItem(BioObjBirdfeed, "Bird feed"), bioQuestItem(BioObjPigeons, "Pigeon cage")}
}

// bioStashHQ is the stash before the crossing: the cupboard hands a gown over only while no copy is banked, the crate a distillator
// only while none is, and Elena can be handed only the pack's.
func bioStashHQ() []KitLine {
	return []KitLine{bioQuestItem(BioObjDoctorGown, "Doctors' gown"), bioQuestItem(BioObjKey, "Key"), bioQuestItem(BioObjDistillator, "Distillator"),
		{ID: BioObjRottenApples, Name: "Rotten apples", Qty: 1, Optional: true}}
}

// bioStashVarrock is the stash before the Chemist: whatever of Elena's hand-back, the paper and the priest's suit the bank holds.
func bioStashVarrock() []KitLine {
	return []KitLine{bioQuestItem(BioObjSample, "Plague sample"), bioQuestItem(BioObjBroline, "Sulphuric broline"), bioQuestItem(BioObjEthenea, "Ethanea"),
		bioQuestItem(BioObjLiquidHoney, "Liquid honey"), bioQuestItem(BioObjTouchPaper, "Touch paper"),
		{ID: BioObjPriestGown, Name: "Priest gown", Qty: 1, Optional: true}, {ID: BioObjPriestRobe, Name: "Priest gown", Qty: 1, Optional: true}}
}

// bioFoodKit is a few sharks from the bank or the mule (it publishes tens of thousands) for the sick mourner; optional, so an empty
// mule blocks nothing and the price decides.
func bioFoodKit() []KitLine {
	return []KitLine{{ID: PlagueObjShark, Name: "Shark", Qty: BioFoodQty, Optional: true}}
}

// bioCoinsKit is the ship fares: a flexible line (an account holding the floor is topped up from the bank, and takes as much as the
// pack has room for). Barnaby and the customs officer take 30 each way and the trip is made twice.
func bioCoinsKit() []KitLine {
	return []KitLine{{ID: CoinsID, Name: "Coins", Qty: BioCoinsQty, Min: BioCoinsMin, Stackable: true}}
}

// bioSuitKit is the priest's gown and robe, from the bank, the mule or Thessalia's shelf (five coins each at the base price).
func bioSuitKit(s Snapshot) []KitLine {
	var kit []KitLine
	for _, p := range []struct {
		id   int
		name string
	}{{BioObjPriestGown, "Priest gown"}, {BioObjPriestRobe, "Priest gown"}} {
		if s.HeldID(p.id)+s.WornIDs[p.id] == 0 {
			kit = append(kit, KitLine{ID: p.id, Name: p.name, Qty: 1, Sources: []Source{
				{Kind: SourceShop, Shop: BioThessaliaShop, Price: 8, Note: "Thessalia's clothes shop keeps three of each, a hundred ticks to restock"}}})
		}
	}
	return kit
}

// ---------------------------------------------------------------------------------------------------------------
// The decision.
// ---------------------------------------------------------------------------------------------------------------

// bioHas is whether the pack or the worn items hold an item.
func bioHas(s Snapshot, id int) bool { return s.HeldID(id)+s.WornIDs[id] > 0 }

// bioVialsHeld counts the three vials in the pack.
func bioVialsHeld(s Snapshot) int {
	n := 0
	for _, id := range bioVials {
		if s.HeldID(id) > 0 {
			n++
		}
	}
	return n
}

// bioSuitOwned reports both halves of the priest's suit held or worn.
func bioSuitOwned(s Snapshot) bool { return bioHas(s, BioObjPriestGown) && bioHas(s, BioObjPriestRobe) }

// DecideBiohazard is the whole quest, read from the quest tab, the journal stage, the pack and where the account stands.
func DecideBiohazard(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not arrived yet"}
	}
	stage := BioNotStarted
	if s.Journal == StatusInProgress {
		if !s.StageKnown {
			return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
		}
		stage = s.Stage
	}
	if stage >= BioComplete {
		return Step{Kind: StepDone}
	}
	switch BioAreaOf(s.Tile) {
	case BioEast:
		return bioDecideEast(s, stage)
	case BioQuarter:
		return bioDecideQuarter(s, stage)
	case BioWest:
		return bioDecideWest(s, stage)
	case BioHQ:
		return bioDecideHQ(s, stage)
	case BioHQUp:
		return bioDecideHQUp(s, stage)
	}
	return bioLegHome(s, "the quest has nothing to do under Ardougne or in a plague house")
}

// bioLegHome sends the account west to east by the quest's own way (Kilron, the manhole).
func bioLegHome(s Snapshot, reason string) Step {
	if s.Failed[CustomBioHome] {
		return Step{Kind: StepWait, ParkFor: 30 * time.Minute, Reason: "no way out of " + BioAreaOf(s.Tile).String() + " this run (Kilron, the manhole and the sewer's mud pile): " + reason}
	}
	return Step{Kind: StepCustom, Custom: CustomBioHome, Reason: reason}
}

// bioDecideEast is an account on the mainland (Rimmington and Varrock included, outside the quarter).
func bioDecideEast(s Snapshot, stage int) Step {
	switch {
	case stage == BioNotStarted:
		return plagueTalk(s, CustomBioStart, BioElenaStart, "ask Elena about her distillator")
	case stage == BioStarted:
		return plagueTalk(s, CustomBioJerico, BioJericoStop, "ask Jerico how to cross the wall")
	case stage == BioSpokenJerico:
		return bioBirdfeed(s)
	case stage == BioUsedBirdfeed:
		return bioPigeons(s)
	case stage >= BioReleasedPigeons && stage <= BioFoundDistillator:
		return bioToTheWall(s, stage)
	case stage == BioGivenDistillator || stage == BioSpokenChemist:
		return bioVarrock(s, stage)
	case stage == BioFoundSecret:
		return bioReport(s)
	case stage == BioReportedElena:
		return plagueTalk(s, CustomBioLathas, BioLathasStop, "confront King Lathas about the plague")
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("Biohazard stage %d is not one the module knows", stage)}
}

// bioStash is the one-off pass for the quest items the bank may hold, once per run.
func bioStash(s Snapshot, name string, kit []KitLine, reason string) (Step, bool) {
	if !plagueOnce(s, name) {
		return Step{}, false
	}
	return bioProvision(s, name, kit, reason), true
}

// bioBirdfeed is stage 2: the bag, then the tower.
func bioBirdfeed(s Snapshot) Step {
	if s.HeldID(BioObjBirdfeed) > 0 {
		return plagueLeg(s, CustomBioFeed, "throw the bird feed onto the watch tower")
	}
	if st, ok := bioStash(s, KitBioStashEarly, bioStashEarly(), "the bird feed Jerico's cupboard will not give again, if the bank holds it"); ok {
		return st
	}
	if s.FreeSlots < BioRoom {
		return bioDeposit(s, "room for the quest's items")
	}
	return plagueLeg(s, CustomBioBirdfeed, "take the bag of bird feed from Jerico's cupboard")
}

// bioPigeons is stage 3: the cage, then the box.
func bioPigeons(s Snapshot) Step {
	if s.HeldID(BioObjPigeons) > 0 {
		return plagueLeg(s, CustomBioRelease, "open the pigeon cage inside the box beside the watch tower")
	}
	if st, ok := bioStash(s, KitBioStashEarly, bioStashEarly(), "the pigeon cage, if the bank holds one"); ok {
		return st
	}
	if s.FreeSlots < BioRoom {
		return bioDeposit(s, "room for the quest's items")
	}
	return plagueLeg(s, CustomBioPigeons, "take a full pigeon cage from behind Jerico's house")
}

// bioKeyNeeded reports whether the sick mourner's key is still to be had: stage 6 without the key, or stage 7 without the
// distillator and without the key to the crate.
func bioKeyNeeded(s Snapshot, stage int) bool {
	if s.HeldID(BioObjKey) > 0 {
		return false
	}
	switch stage {
	case BioReleasedPigeons, BioClimbedLadder, BioPoisonedStew:
		return true
	case BioFoundDistillator:
		return s.HeldID(BioObjDistillator) == 0
	}
	return false
}

// bioToTheWall is the mainland at stages 4 to 7: the distillator to Elena if the pack holds it, otherwise across the wall.
func bioToTheWall(s Snapshot, stage int) Step {
	if stage >= BioClimbedLadder {
		if st, ok := bioStash(s, KitBioStashHQ, bioStashHQ(), "the gown, key, distillator and apples a bank visit may have left in the bank"); ok {
			return st
		}
	}
	if stage == BioFoundDistillator && s.HeldID(BioObjDistillator) > 0 {
		if s.FreeSlots < BioHandbackSlots {
			return bioDeposit(s, "room for Elena's hand-back (the sample and three vials)")
		}
		return plagueTalk(s, CustomBioHandBack, BioElenaTalk, "give Elena her distillator")
	}
	if s.FreeSlots < BioRoom {
		return bioDeposit(s, "room for the quest's items")
	}
	// The fight is priced before the account crosses: over the wall there is neither a bank nor a mule.
	if bioKeyNeeded(s, stage) {
		if ok, why := BioKeyOdds(s); !ok {
			if plagueOnce(s, KitBioFood) {
				return bioProvision(s, KitBioFood, bioFoodKit(), "food for the sick mourner: "+why)
			}
			return Step{Kind: StepWait, ParkFor: BioKeyPark, Reason: "not crossing to the Mourners' headquarters: " + why}
		}
	}
	return plagueLeg(s, CustomBioCross, "Omart's rope ladder over the wall to West Ardougne")
}

// bioCoins is the float for the ships, once per run, when the pack is short of it. ok is false when nothing is needed.
func bioCoins(s Snapshot) (Step, bool) {
	if s.HeldID(CoinsID) >= BioCoinsMin {
		return Step{}, false
	}
	if plagueOnce(s, KitBioCoins) {
		return bioProvision(s, KitBioCoins, bioCoinsKit(), "coins for the ships to Rimmington and back"), true
	}
	if s.Failed["provision:"+KitBioCoins] || s.Blocked["provision:"+KitBioCoins] {
		return Step{Kind: StepWait, ParkFor: BioSupplyPark, Reason: fmt.Sprintf("no %d coins for the ships from the bank or the mule (the pack holds %d)", BioCoinsMin, s.HeldID(CoinsID))}, true
	}
	return Step{}, false
}

// bioVarrock is stages 10 and 12 outside the quarter: the Chemist's errand through Rimmington and Varrock.
func bioVarrock(s Snapshot, stage int) Step {
	if s.FreeSlots < BioRoomVarrock {
		return bioDeposit(s, "room for the vials, the sample, the paper and the priest's suit")
	}
	if st, ok := bioStash(s, KitBioStashVarrock, bioStashVarrock(), "the sample, vials, touch paper and priest's gown a bank visit may have left in the bank"); ok {
		return st
	}
	// The sample is what the Chemist and Guidor both want, and what Elena gave with the vials.
	sampleMissing := s.HeldID(BioObjSample) == 0
	vialsMissing := bioVialsHeld(s) < len(bioVials)
	if stage == BioGivenDistillator && (sampleMissing || vialsMissing) {
		return plagueTalk(s, CustomBioReplace, BioElenaReplace, "ask Elena to replace the sample and vials that did not fit in the pack")
	}
	if st, ok := bioCoins(s); ok {
		return st
	}
	if stage == BioGivenDistillator {
		return plagueTalk(s, CustomBioChemist, BioChemistStop, "ask the Chemist for touch paper")
	}
	// Stage 12.
	if s.Failed[CustomBioCollect] && !s.Ran[CustomBioReplace] {
		return plagueTalk(s, CustomBioReplace, BioElenaReplace, "ask Elena for the vials a boy at the Dancing Donkey did not give back")
	}
	if s.HeldID(BioObjTouchPaper) == 0 {
		return plagueTalk(s, CustomBioChemist, BioChemistStop, "ask the Chemist for another sheet of touch paper")
	}
	if sampleMissing {
		return plagueTalk(s, CustomBioReplace, BioElenaReplace, "ask Elena to replace the sample")
	}
	if bioVialsHeld(s) > 0 && !s.Failed[CustomBioErrands] {
		return plagueLeg(s, CustomBioErrands, "give the three vials to the Chemist's errand boys")
	}
	if !bioSuitOwned(s) {
		if s.Failed["provision:"+KitBioSuit] || s.Blocked["provision:"+KitBioSuit] {
			return Step{Kind: StepWait, ParkFor: BioSupplyPark, Reason: "no priest's gown and robe from the bank, the mule or Thessalia's shelf (empty, or too few coins)"}
		}
		return bioProvision(s, KitBioSuit, bioSuitKit(s), "the priest's gown and robe Guidor's wife lets through")
	}
	return plagueLeg(s, CustomBioGateIn, "through Varrock's east gate, searched, carrying no vial")
}

// bioReport is stage 14 on the mainland: Elena is told what Guidor found.
func bioReport(s Snapshot) Step {
	if st, ok := bioCoins(s); ok {
		return st
	}
	return plagueTalk(s, CustomBioReport, BioElenaTalk, "tell Elena what Guidor found")
}

// bioDecideQuarter is an account behind Varrock's east gate: the vials back from the boys, then Guidor, then out.
func bioDecideQuarter(s Snapshot, stage int) Step {
	if stage != BioSpokenChemist {
		return bioLegGate(s, "nothing more for the quest in the quarter")
	}
	if s.HeldID(BioObjSample) == 0 || s.HeldID(BioObjTouchPaper) == 0 {
		return bioLegGate(s, "the sample or the touch paper is missing: out to Elena or the Chemist")
	}
	if bioVialsHeld(s) < len(bioVials) {
		// A boy who gave no vial back is Elena's to replace (she is on the mainland, and the gate takes every vial the account carries
		// out and in again, so the new ones go to the boys outside the Chemist's house first). Once she has, a second failure here is
		// the run's verdict (plagueLeg waits): the next run begins from a fresh slate and asks the boys again.
		if s.Failed[CustomBioCollect] && !s.Ran[CustomBioReplace] {
			return bioLegGate(s, "a boy at the Dancing Donkey gave no vial back: out to Elena for new ones")
		}
		return plagueLeg(s, CustomBioCollect, "collect the vials from the errand boys at the Dancing Donkey")
	}
	if !bioSuitOwned(s) {
		return bioLegGate(s, "no priest's suit for Guidor's wife: out to Thessalia")
	}
	return plagueLeg(s, CustomBioGuidor, "dressed as a priest, give Guidor the vials, the sample and the touch paper")
}

// bioLegGate sends the account out of the quarter.
func bioLegGate(s Snapshot, reason string) Step {
	if s.Failed[CustomBioGateOut] {
		return Step{Kind: StepWait, ParkFor: 30 * time.Minute, Reason: "the east gate did not let the account out: " + reason}
	}
	return Step{Kind: StepCustom, Custom: CustomBioGateOut, Reason: reason}
}

// bioDecideWest is an account in West Ardougne's streets or the headquarters' yard.
func bioDecideWest(s Snapshot, stage int) Step {
	switch {
	case stage < BioClimbedLadder || stage > BioFoundDistillator:
		return bioLegHome(s, "the quest has nothing to do in West Ardougne at this stage")
	case stage == BioClimbedLadder:
		if s.HeldID(BioObjRottenApples) == 0 {
			return plagueLeg(s, CustomBioApples, "rotten apples from the ground at 2535,3333")
		}
		return plagueLeg(s, CustomBioStew, "the rotten apples in the Mourners' cauldron")
	case stage == BioFoundDistillator && s.HeldID(BioObjDistillator) > 0:
		return bioLegHome(s, "Elena's distillator in the pack: back over the wall")
	}
	// Stage 6, or stage 7 without the distillator: the key, then the crate.
	// A failed cupboard is a verdict for the run (plagueLeg waits): the stash kit has already drawn any banked copy, which is the one
	// thing that makes the cupboard refuse, so another trip over the wall would only come back here.
	if !bioHas(s, BioObjDoctorGown) {
		return plagueLeg(s, CustomBioGown, "a doctors' gown from the nurse's cupboard")
	}
	if bioKeyNeeded(s, stage) {
		if ok, why := BioKeyOdds(s); !ok {
			if plagueOnce(s, KitBioFood) {
				return bioLegHome(s, "food for the sick mourner from the other side: "+why)
			}
			return Step{Kind: StepWait, ParkFor: BioKeyPark, Reason: "not entering the Mourners' headquarters: " + why}
		}
	}
	if s.FreeSlots < 1 {
		return bioLegHome(s, "no room in the pack for the key")
	}
	return plagueLeg(s, CustomBioHQ, "the doctors' gown on, in by the headquarters' door")
}

// bioDecideHQ is the headquarters' ground floor: up to the sick mourners, or out with the distillator.
func bioDecideHQ(s Snapshot, stage int) Step {
	if stage != BioPoisonedStew && stage != BioFoundDistillator {
		return bioLegLeaveHQ(s, "the quest has nothing to do in the headquarters at this stage")
	}
	if stage == BioFoundDistillator && s.HeldID(BioObjDistillator) > 0 {
		return bioLegLeaveHQ(s, "Elena's distillator in the pack: out of the headquarters")
	}
	if s.HeldID(BioObjKey) == 0 {
		if ok, why := BioKeyOdds(s); !ok {
			return bioLegLeaveHQ(s, "not facing the sick mourner: "+why)
		}
	}
	return plagueLeg(s, CustomBioUp, "across the hall and up the stairs to the sick mourners' room")
}

// bioDecideHQUp is the first floor: the sick mourner's key, then the gate and the crate, then out.
func bioDecideHQUp(s Snapshot, stage int) Step {
	if stage != BioPoisonedStew && stage != BioFoundDistillator {
		return bioLegLeaveHQ(s, "the quest has nothing to do in the headquarters at this stage")
	}
	if s.HeldID(BioObjDistillator) > 0 {
		return bioLegLeaveHQ(s, "Elena's distillator in the pack: out of the headquarters")
	}
	if s.HeldID(BioObjKey) == 0 {
		if ok, why := BioKeyOdds(s); !ok {
			return bioLegLeaveHQ(s, "not facing the sick mourner: "+why)
		}
		if s.FreeSlots < 1 {
			return bioLegLeaveHQ(s, "no room in the pack for the key")
		}
		return plagueLeg(s, CustomBioKey, "talk to the sick mourner in the gown, and win his key")
	}
	return plagueLeg(s, CustomBioCrate, "the key in the gate, and the distillator from the crate")
}

// bioLegLeaveHQ sends the account out of the headquarters by its door.
func bioLegLeaveHQ(s Snapshot, reason string) Step {
	if s.Failed[CustomBioLeaveHQ] {
		return Step{Kind: StepWait, ParkFor: 30 * time.Minute, Reason: "the headquarters' door did not let the account out: " + reason}
	}
	return Step{Kind: StepCustom, Custom: CustomBioLeaveHQ, Reason: reason}
}

// ---------------------------------------------------------------------------------------------------------------
// The module.
// ---------------------------------------------------------------------------------------------------------------

// BiohazardRecord is the quest's identity. The server gates nothing but the story: Plague City is the prerequisite the ladder keeps.
var BiohazardRecord = Record{ID: BiohazardID, Name: BiohazardName, QuestPoints: 3,
	Requirements: Requirements{Quests: []string{PlagueCityID}}}

// BioPastTheHeadquarters reports the account past the quest's shared steps (the apples' single spawn, the sick mourners, Thessalia's
// shelf is the only one left and an account buys one suit): Elena has the distillator, or the quest is done. The fleet-wide slot is
// held until then.
func BioPastTheHeadquarters(s Snapshot) bool {
	if s.Journal == StatusComplete {
		return true
	}
	return s.Journal == StatusInProgress && s.StageKnown && s.Stage >= BioGivenDistillator
}

// Biohazard is the engine module. One death parks it for a day on the headquarters' step and two anywhere (the one fight is priced;
// a death means the price was wrong for this account, or something in West Ardougne that is not in the price found it). NoRetaliate
// keeps the account from answering any mourner that is not the one the key leg fights by name.
var Biohazard = Module{
	Record: BiohazardRecord, Bank: tile.Tile{X: 2616, Z: 3332}, BankSet: true, Anchor: BioElenaAnchor,
	ToolIDs:   bioTools,
	ReadStage: ParseBiohazardJournal,
	Sustain:   Sustain{FoodIDs: plagueFoods, EatAt: 12},
	MaxDeaths: 2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomBioKey:  {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
		CustomBioGown: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	ExclusiveDone: BioPastTheHeadquarters,
	NoRetaliate:   true,
	Decide:        DecideBiohazard,
}

// BiohazardCustomNames are the custom steps the module can ask for, for the test that every one has an executor.
func BiohazardCustomNames() []string {
	return []string{CustomBioStart, CustomBioJerico, CustomBioBirdfeed, CustomBioFeed, CustomBioPigeons, CustomBioRelease, CustomBioCross,
		CustomBioApples, CustomBioStew, CustomBioGown, CustomBioHQ, CustomBioUp, CustomBioKey, CustomBioCrate, CustomBioLeaveHQ, CustomBioHome,
		CustomBioHandBack, CustomBioReplace, CustomBioChemist, CustomBioErrands, CustomBioGateIn, CustomBioCollect, CustomBioGuidor,
		CustomBioGateOut, CustomBioReport, CustomBioLathas}
}
