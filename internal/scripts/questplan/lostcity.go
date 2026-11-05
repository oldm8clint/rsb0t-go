package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Lost City (content id `zanaris`, %zanaris, perm and not transmitted, 3 quest points, Crafting 31 and Woodcutting 36), ported from the TS
// def (defs/lostcity.ts) and checked against content 289: quests/quest_zanaris/{configs,scripts} (quest_zanaris.rs2, shamus.rs2,
// tree_spirit.rs2, zanaris_journal.rs2), areas/area_lumbridge/scripts/warrior_zanaris.rs2, areas/area_entrana/scripts/{entrana_dungeon,
// cave_monk}.rs2, areas/area_port_sarim/scripts/monk_of_entrana.rs2, areas/area_lostcity/scripts/ladder_fairy.rs2, general_use/scripts/
// gangplank.rs2, ladders+stairs/scripts/ladders.rs2, drop tables/scripts/zombie.rs2 and the maps (m44_52, m44_152, m49_50, m50_149).
//
// The story, with the stage each step moves %zanaris to:
//
//	0   the warrior of the camp in Lumbridge swamp: "What are you camped here for?", "Who's Zanaris?", "If it's hidden how are you planning
//	    to find it?", "Looks like you don't know either." (a talk through four menus): 1. Nothing is asked of the account (no level, no item).
//	1   the leprechaun's tree (loc 2409, 3138,3212, op Chop) wants an AXE (the pack or worn: woodcutting_axe_checker, no level in 2004)
//	    and sets Shamus (npc 654) down at 3139,3211 for 100 ticks, who walks ten tiles east; the tree does nothing while a Shamus is
//	    within 15 tiles of his spawn ("Hey! Yer big elephant!"), but ANY player may talk to a Shamus that is there. His dialogue
//	    ends "How does it fit in a shed then?" and a message box: 2.
//	2   the Dramen tree (loc 1292, 2860,9734, op "Chop down") is in the dungeon under Entrana. A chop at stage 2 sets the TREE SPIRIT
//	    (npc 655, level 101: attack 90, strength 95, defence 80, 85 hitpoints, a crush hit up to 10) at 2860,9737 and
//	    it attacks the account (~npc_retaliate); no branch comes. Its death, credited to the player who dealt most (npc_findhero),
//	    raises a message box: 3.
//	3   a chop gives a Dramen branch (a free pack slot; no level beyond Woodcutting 36 for the tree) and moves to 4.
//	4   the knife used on the branch (Crafting 31, "You carve the branch into a staff."): 5. Any knife, anywhere.
//	5   the Dramen staff WORN, and the shed's door (loc 2406, 3202,3169, op Open) opened from OUTSIDE (from 3201,3169, a tile whose x is
//	    not the door's: check_axis) teleports the account to Zanaris (3220,9592) and, at stage 5, completes the quest in a queue: 6.
//
// What the content does that a script must plan for:
//   - ENTRANA REFUSES WEAPONS AND ARMOUR, AND AXES. The Port Sarim monk (npc 657) searches the pack and the worn items before he sails
//     (monk_of_entrana.rs2 has_entrana_restricted_items): hands, helmets, bodies, legs, shields, capes, god capes, every melee weapon
//     class, crossbows, AXES, PICKAXES, javelins, two-handed swords, spears, spiked weapons, thrown weapons, scythes, bows, claws, polearms,
//     STAVES (the Dramen staff and every elemental staff) and cannon parts. Boots, gloves of no class, amulets, rings, ammunition, runes,
//     food, coins and a knife are not on the list. So the account banks every restricted item and goes bare; the axe the Dramen tree wants
//     is taken from an Entrana zombie in the dungeon (zombie_entranan drops a bronze axe 50 in 128 and an iron axe 1 in 128).
//   - THE WAY DOWN IS ONE WAY, AND THE WAY OUT IS A TELEPORT. The ladder on Entrana (loc 2408) only works with the cave monk (npc 656)
//     standing at 2822,3374: his chat ("Well that is a risk I will have to take.") climbs the account down to 2822,9774 and takes 95% of
//     its Prayer points. No walk leaves the dungeon: the ladder at 2884,9797 climbs to 2884,3397, which is sea, and is not reachable from
//     the arrival, and the Magic door (loc 2407, 2874,9750) sets the account down at 3250,3772, in the WILDERNESS at level 32, a bare walk
//     of some 250 Wilderness tiles that the fleet's rule (never the Wilderness) forbids. The other way out is a TELEPORT cast from where
//     the account stands: teleport.rs2 refuses one only above level 20 of the Wilderness (wilderness_level reads wilderness_zones, and
//     the Entrana dungeon, map square x 44, is in neither) and in the Tutorial Island, trawler, mime, maze, duel arena and Castle Wars
//     zones, and nothing on Entrana forbids a spell. So the trip kit carries the runes of Varrock Teleport (Magic 25: a fire, three air
//     and a law rune, all of them on the mule; no earth rune is published there, which Lumbridge Teleport, Magic 31, would want and is
//     cast instead when the pack happens to hold them) and the account leaves by a cast from the tree's stand, which also spares it the
//     corridor's second pass. Runes are no weapon or armour and are not on the monks' list. The Magic door and the Wilderness walk are
//     the last resort of an account that cannot cast (a stranded one, by the dungeon pocket's Leave), logged as such.
//   - THE DUNGEON IS A GAUNTLET THE TS DEF NEVER PRICED. Two greater demons (npc 83, level 92, hits up to 9, wander 6 and hunt 3, at
//     2861,9747 and 2862,9751) stand IN the one corridor that leads south to the Dramen tree and east to the door, so the way in runs
//     past both (the way out is a teleport cast at the tree, not a second pass); four black demons (npc 84, level 172, hits up to 16)
//     stand north-east of the zombies and their ground is kept out of by every walk (LcBlackDemonAvoid: a route exists round them on
//     the real pack); the zombies (npc 76, level 25, hits up to 3) are the axe's source and are fought on the west side of their
//     spawns. The account never stops to fight a demon (Module.NoRetaliate) and runs the corridor at full hitpoints, with the pass
//     priced (LcCorridorFit).
//   - THE TREE SPIRIT IS THE ONE REAL FIGHT: level 101, 85 hitpoints, a hit up to 10 every four ticks and defence 80, fought without a weapon
//     or armour. Meleed at the fleet's usual levels (Attack 53, Strength 41) it takes the account's hitpoints several times over; CAST, it has
//     NO magic defence (its config names no magic stat: an npc's magic is 1 and the account's cast rolls 640 against its own Magic level), so
//     a Fire Blast (Magic 59, up to 16) lands nine times in ten and the 85 hitpoints are about fourteen casts. Runes are not weapons, so the
//     account carries them down (Death, Fire and Air for Fire Blast, the best spell its Magic level casts: LcKitSpell) and the fight is
//     priced on the pack the account sails with (LcSpiritOdds) and refused, for six hours, when the model loses.
//   - THE SPIRIT IS ONE ACCOUNT'S AT A TIME. It is one npc with one health bar, single combat gives it to one attacker, and the stage is
//     credited to the one who dealt the most damage: the quest holds a one-at-a-time slot until the account is past the spirit
//     (Module.ExclusiveDone, LcPastSpirit). Shamus is shared the same way: the slot also keeps two accounts from chopping the leprechaun's
//     tree together.
//   - The shed's teleport, the ladder's drop and the exit cast are the account's own and unlisted: each is announced to the Guardian by
//     the module (the displacement is explained, not a random event). Zanaris itself is NOT a pocket: the walk graph has its entrance
//     ladder (the generic "Ladder" 1755 at 3222,9588 climbs to 3201,3169, outside the shed), so the account leaves by an ordinary walk.
//   - Message boxes to continue: Shamus's disappearance ("The leprechaun magically disappears."), the spirit's death ("With the Tree
//     Spirit defeated you can now chop the tree.") and the quest-complete scroll.

// LostCityName is the quest tab's spelling (component 7367, `questlist:zanaris`) and LostCityID the engine's id.
const (
	LostCityName = "Lost City"
	LostCityID   = "zanaris"
)

// The %zanaris stages (quest_zanaris.constant; 6 is ^zanaris_complete in general/configs/quest.constant).
const (
	LcNotStarted     = 0
	LcStarted        = 1
	LcSpokenShamus   = 2
	LcSpiritDefeated = 3
	LcTreeChopped    = 4
	LcStaffMade      = 5
	LcComplete       = 6
)

// Levels the content asks of the account (the staff is carved at Crafting 31, the Dramen tree chopped at Woodcutting 36).
const (
	LcCrafting    = 31
	LcWoodcutting = 36
)

// Ids from content/pack (checked against the production config by TestLostCityIDsMatchTheProductionConfig).
const (
	NpcLcWarrior      = 650 // [warrioradventurerpg]
	NpcLcShamus       = 654 // [zanarisleprechaun]
	NpcLcTreeSpirit   = 655 // [tree_spirit]
	NpcLcCaveMonk     = 656 // [cave_monk]
	NpcLcZombie       = 76  // [zombie_entranan]
	NpcLcGreaterDemon = 83  // [greater_demon]
	NpcLcBlackDemon   = 84  // [black_demon]

	LocLcLeprechaunTree = 2409 // [leprechauntree], "Tree", op Chop
	LocLcDramenTree     = 1292 // [dramentree], op "Chop down"
	LocLcShedDoor       = 2406 // [zanarisdoor], op Open
	LocLcMagicDoor      = 2407 // [zanarismagicdoor], "Magic door", op Open
	LocLcEntranaLadder  = 2408 // [entranaladdertop], "Ladder", op Climb-down

	ObjLcBranch = 771 // [dramen_branch], untradeable
	ObjLcStaff  = 772 // [dramen_staff], category weapon_staff, wearpos righthand
	ObjBlackAxe = 1361

	// ObjLcLawRune is [lawrune], the one rune of the exit teleports that no combat spell takes.
	ObjLcLawRune = 563
)

// Tiles from content maps, the loc index and the collision pack.
var (
	// LcWarriorAnchor is the warrior of the camp (m49_50, 3145,3202).
	LcWarriorAnchor = tile.Tile{X: 3145, Z: 3202}
	// LcShamusTreeAt is the leprechaun's tree (loc 2409), LcShamusSpawn the tile Shamus is set down on (0_49_50_3_11).
	LcShamusTreeAt = tile.Tile{X: 3138, Z: 3212}
	LcShamusSpawn  = tile.Tile{X: 3139, Z: 3211}
	// LcKnifeSpawn is the knife that lies on the ground in Lumbridge (m50_50, 3224,3202).
	LcKnifeSpawn = tile.Tile{X: 3224, Z: 3202}
	// LcCaveMonkAnchor is where the cave monk stands (m44_52, 2822,3374), LcEntranaLadderAt the ladder (loc 2408, 2820,3374).
	LcCaveMonkAnchor  = tile.Tile{X: 2822, Z: 3374}
	LcEntranaLadderAt = tile.Tile{X: 2820, Z: 3374}
	// LcDungeonArrival is where the ladder sets the account down (p_telejump 0_44_152_6_46).
	LcDungeonArrival = tile.Tile{X: 2822, Z: 9774}
	// LcZombieStand is a tile of the zombies' ground on its west side (zombies at 2842,9766, 2843,9761, 2846,9756, 2846,9760 and 2849,9763):
	// every black demon's hunting ground is north-east of it (LcBlackDemonAvoid).
	LcZombieStand = tile.Tile{X: 2844, Z: 9757}
	// LcTreeAt is the Dramen tree (loc 1292), LcTreeStand the tile south of it the account stands on, and LcSpiritSpawn where the spirit
	// is set down (0_44_152_44_9).
	LcTreeAt      = tile.Tile{X: 2860, Z: 9734}
	LcTreeStand   = tile.Tile{X: 2860, Z: 9733}
	LcSpiritSpawn = tile.Tile{X: 2860, Z: 9737}
	// LcDoorAt is the Magic door (loc 2407, a wall, angle 0), LcDoorStand the tile west of it it is opened from.
	LcDoorAt    = tile.Tile{X: 2874, Z: 9750}
	LcDoorStand = tile.Tile{X: 2873, Z: 9750}
	// LcWildernessLanding is where the Magic door sets the account down (p_telejump 0_50_58_50_60), at wilderness level 32: the last resort
	// of an account that cannot cast its way out.
	LcWildernessLanding = tile.Tile{X: 3250, Z: 3772}
	// LcShedDoorAt is the shed's door (loc 2406, a wall, angle 0) and LcShedOutside the tile west of it that Zanaris is entered from: the
	// teleport happens only when the account's x is not the door's (check_axis). LcZanarisArrival is where it sets the account down
	// (0_50_149_20_56).
	LcShedDoorAt     = tile.Tile{X: 3202, Z: 3169}
	LcShedOutside    = tile.Tile{X: 3201, Z: 3169}
	LcZanarisArrival = tile.Tile{X: 3220, Z: 9592}
)

// LcRegion is where an account stands relative to the quest's places.
type LcRegion int

const (
	// LcMainland is anywhere the walker reaches on the mainland (the Wilderness landing included) and anywhere not named below.
	LcMainland LcRegion = iota
	// LcIsland is Entrana's surface and the ship to it.
	LcIsland
	// LcDungeon is the dungeon under Entrana: 652 tiles the planner reaches from the ladder's foot (x 2820-2873, z 9730-9781) and
	// no walk leaves, with two tiles of margin.
	LcDungeon
	// LcZanaris is Zanaris, which the walker leaves by its entrance ladder.
	LcZanaris
)

// LcRegionOf is the region a tile is in.
func LcRegionOf(t tile.Tile) LcRegion {
	switch {
	case t.Level == 0 && t.X >= 2818 && t.X <= 2875 && t.Z >= 9728 && t.Z <= 9783:
		return LcDungeon
	case t.Level == 0 && t.X >= 3140 && t.X <= 3240 && t.Z >= 9485 && t.Z <= 9600:
		return LcZanaris
	case OnEntrana(t):
		return LcIsland
	}
	return LcMainland
}

// InLcDungeon reports a tile of the dungeon: no walk leaves it, so it is a registered pocket whose way out is the Magic door.
func InLcDungeon(t tile.Tile) bool { return LcRegionOf(t) == LcDungeon }

// InLcZanaris reports a tile of Zanaris.
func InLcZanaris(t tile.Tile) bool { return LcRegionOf(t) == LcZanaris }

// LcInWilderness reports a tile in the Wilderness of the surface (the walker's own rectangle).
func LcInWilderness(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2944 && t.X <= 3391 && t.Z >= 3523 && t.Z <= 3967
}

// ---------------------------------------------------------------------------------------------------------
// The dungeon's hostiles.
// ---------------------------------------------------------------------------------------------------------

// The black demons' and greater demons' spawns (maps/m44_152.jm2, "38 48: 84" and the rest) and the ground a hunter covers: its wander range
// (6 for both: [black_demon] and [greater_demon] wanderrange=6) and its hunt range (3; the hunt is taken from the npc's own tile, the south-west
// corner of its size-3 body) and a tile of slack. Both hunt every account whose combat is under twice their level (cowardly), which is all of
// them.
var (
	lcBlackDemons   = []tile.Tile{{X: 2854, Z: 9776}, {X: 2860, Z: 9781}, {X: 2863, Z: 9769}, {X: 2869, Z: 9776}}
	lcGreaterDemons = []tile.Tile{{X: 2861, Z: 9747}, {X: 2862, Z: 9751}}
)

// LcDemonPad is the tiles round a demon's spawn its hunting ground covers.
const LcDemonPad = 6 + 3 + 1

// LcBlackDemonSpawns lists the black demons' spawn tiles (a copy).
func LcBlackDemonSpawns() []tile.Tile { return append([]tile.Tile(nil), lcBlackDemons...) }

// LcGreaterDemonSpawns lists the greater demons' spawn tiles (a copy).
func LcGreaterDemonSpawns() []tile.Tile { return append([]tile.Tile(nil), lcGreaterDemons...) }

// LcBlackDemonAvoid is the rectangles every walk of the dungeon keeps out of, except the corridor's, which has no way round the greater
// demons: a square round each black demon as wide as the ground it wanders over and hunts across. A route from the ladder's foot to the
// zombies, to the tree and to the door exists round all four on the real pack (TestLostCityRoutesKeepOutOfTheBlackDemons).
func LcBlackDemonAvoid() []Rect {
	out := make([]Rect, 0, len(lcBlackDemons))
	for _, h := range lcBlackDemons {
		out = append(out, Rect{MinX: h.X - LcDemonPad, MaxX: h.X + LcDemonPad, MinZ: h.Z - LcDemonPad, MaxZ: h.Z + LcDemonPad})
	}
	return out
}

// InLcBlackDemonGround reports a tile inside a black demon's hunting ground.
func InLcBlackDemonGround(t tile.Tile) bool {
	for _, r := range LcBlackDemonAvoid() {
		if r.Contains(t) {
			return true
		}
	}
	return false
}

// The foes (npc configs: quests/quest_zanaris/configs/tree_spirit.npc, scripts/_unpack/225/all.npc). The tree spirit and the zombie are in the
// shared fight table (questFoes) so the fight loops raise their eat line by them.
var (
	// FoeTreeSpirit is [tree_spirit] (npc 655): attack 90, strength 95, defence 80, 85 hitpoints, a crush hit, no attackrate (4), no bonus
	// params and no magic stat (1): a hardest hit of 10.
	FoeTreeSpirit = MeleeFoe{Name: "Tree spirit", Attack: 90, Strength: 95, Defence: 80, Magic: 1, Hitpoints: 85, AttackTicks: 4}
	// FoeEntranaZombie is [zombie_entranan] (npc 76): attack 19, strength 21, defence 21, 30 hitpoints, attack bonus 5, strength bonus 7 (a hit
	// of up to 3), defence params 10/8/12.
	FoeEntranaZombie = MeleeFoe{Name: "Zombie", Attack: 19, Strength: 21, Defence: 21, Magic: 1, Hitpoints: 30, AttackTicks: 4,
		AttackBonus: 5, StrengthBonus: 7, DefenceBonus: 8}
	// FoeGreaterDemon is [greater_demon] (npc 83): attack 76, strength 78, defence 81, 87 hitpoints, a slash hit of up to 9.
	FoeGreaterDemon = MeleeFoe{Name: "Greater demon", Attack: 76, Strength: 78, Defence: 81, Magic: 1, Hitpoints: 87, AttackTicks: 4}
)

// ---------------------------------------------------------------------------------------------------------
// The journal.
// ---------------------------------------------------------------------------------------------------------

// lcJournal lists the journal's markers, newest page first: zanaris_journal.rs2 keeps every earlier paragraph on the page, so the newest one decides.
var lcJournal = []struct {
	stage  int
	needle string
}{
	{LcComplete, "quest complete!"},
	{LcStaffMade, "crafted a dramen staff"},
	{LcTreeChopped, "i should craft the branch from the tree into a staff"},
	{LcSpiritDefeated, "with the spirit defeated i can cut a branch"},
	{LcSpokenShamus, "i can find a dramen tree"},
	{LcStarted, "apparently there is a leprechaun"},
	{LcNotStarted, "i can start this quest"},
}

// ParseLostCityJournal reads the stage from zanaris_journal.rs2's rendered text. ok is false for an empty or unrecognised page, which is
// unknown, never stage 0.
func ParseLostCityJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, m := range lcJournal {
		if strings.Contains(text, m.needle) {
			return m.stage, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------------------------------------
// The stops.
// ---------------------------------------------------------------------------------------------------------

var (
	// LcWarriorStop starts the quest: "What are you camped here for?", "Who's Zanaris?", "If it's hidden how are you planning to find it?",
	// "Looks like you don't know either." (four menus, the last one's second option).
	LcWarriorStop = NpcStop{Npc: "Warrior", ID: NpcLcWarrior, Anchor: LcWarriorAnchor, Leash: 8,
		Prefer: []string{"What are you camped here for?", "Who's Zanaris?", "If it's hidden how are you planning to find it?", "Looks like you don't know either."}}
	// LcShamusStop is Shamus once the tree has set him down (he walks ten tiles east and wanders thirteen, for a hundred ticks): "How does it fit
	// in a shed then?". The leash is wide enough for one he has walked off from, and for one another account's chop has set down.
	LcShamusStop = NpcStop{Npc: "Shamus", ID: NpcLcShamus, Anchor: LcShamusSpawn, Leash: 30,
		Prefer: []string{"How does it fit in a shed then?"}}
	// LcPortMonkStop is the Port Sarim monk who searches the account and sails (monk_of_entrana.rs2 shipmonk).
	LcPortMonkStop = NpcStop{Npc: "Monk of Entrana", ID: NpcShipMonk, Anchor: PortSarimMonks, Leash: 8,
		Prefer: []string{"Yes, okay, I'm ready to go."}}
	// LcCaveMonkStop is the monk at the ladder's top: "Well that is a risk I will have to take." climbs the account down.
	LcCaveMonkStop = NpcStop{Npc: "Cave monk", ID: NpcLcCaveMonk, Anchor: LcCaveMonkAnchor, Leash: 8,
		Prefer: []string{"Well that is a risk I will have to take."}}
)

// ---------------------------------------------------------------------------------------------------------
// The pack: what is carried, what the monks forbid, and what the trip asks for.
// ---------------------------------------------------------------------------------------------------------

// LcAxes are the axes the content lets an account chop with, any of which the Dramen tree and the leprechaun's tree want in the pack or
// worn (woodcutting_axe_checker): the bronze to the rune axe and the black one the shared list lacks.
var LcAxes = append(append([]int(nil), Axes...), ObjBlackAxe)

// lcAxeHeld reports any axe in the pack or worn.
func lcAxeHeld(s Snapshot) bool {
	for _, a := range LcAxes {
		if s.InvIDs[a] > 0 || s.WornIDs[a] > 0 {
			return true
		}
	}
	return false
}

// lcStaffHeld reports the Dramen staff in the pack or worn.
func lcStaffHeld(s Snapshot) bool { return s.InvIDs[ObjLcStaff] > 0 || s.WornIDs[ObjLcStaff] > 0 }

// lcFoodIDs are the foods the quest carries and eats, best first.
var lcFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// lcSustain is the quest's survival policy: the eat line is twice the spirit's hardest hit (10), which also clears a greater demon's 8 and
// two.
var lcSustain = Sustain{FoodIDs: lcFoodIDs, EatAt: 20}

// LcRuneIDs are the runes a kit may carry: those of the combat spells (the standard book's strikes, bolts and blasts) and the law rune the
// exit teleports want.
var LcRuneIDs = []int{arenaFireRune, arenaWaterRune, arenaAirRune, arenaEarthRune, arenaMindRune, arenaDeathRune, arenaChaosRune, ObjLcLawRune}

// lcTools are the quest's own items: what the floor bite never eats and a bank visit of the quest leaves in the pack. Food is no tool (the
// top-up and the floor bite eat it).
var lcTools = func() []int {
	out := []int{ObjKnife, ObjLcBranch, ObjLcStaff, CoinsID}
	out = append(out, LcAxes...)
	return append(out, LcRuneIDs...)
}()

// lcKeep is what every bank step of the quest leaves in the pack: the tools and the food. The runes of the spirit's spell and the knife are
// among them; an axe is not carried over the sea (lcTripKeep leaves it out).
func lcKeep() []int { return append(append([]int(nil), lcTools...), lcFoodIDs...) }

// LcKeepIDs returns the keep list (a copy).
func LcKeepIDs() []int { return lcKeep() }

// LcToolIDs returns the module's tools (a copy).
func LcToolIDs() []int { return append([]int(nil), lcTools...) }

// LcEntranaKeep is what the account may carry on to the ship: everything the monks do not forbid that the trip wants (a knife, food, runes,
// the exit teleport's among them, coins). The strip step banks all else, bare (questrunner.DepositBare).
func LcEntranaKeep() []int {
	out := []int{ObjKnife, ObjLcBranch, CoinsID}
	out = append(out, LcRuneIDs...)
	return append(out, lcFoodIDs...)
}

// LcForbiddenOnEntrana lists the objects the quest itself handles that Entrana's monks refuse: every axe (weapon_axe) and the Dramen staff
// (weapon_staff). The knife, the branch, the runes, the food and the coins are not on the monks' list.
func LcForbiddenOnEntrana() []int { return append(append([]int(nil), LcAxes...), ObjLcStaff) }

// The kit.
const (
	// LcSharks is the food the trip with the Tree spirit asks of the bank and the mule, LcTripSharks the trip without it (the corridor's two
	// passes). The mule holds tens of thousands.
	LcSharks     = 10
	LcTripSharks = 6
	// LcSpiritCasts is the casts the kit carries for the spirit: its 85 hitpoints at a cast's expected damage (a maximum hit over 2.6) with
	// half again for the rolls, and at least LcMinCasts.
	LcMinCasts = 12
	LcMaxCasts = 60
)

// LcSpiritTicks is how long a fight of the quest lasts at the longest: three minutes of 0.6 second ticks (the shared fight loops' own
// budget is three minutes), a fight the model cannot see won inside it is not begun.
const LcSpiritTicks = 290

// lcSpell is a spell of the standard book.
type lcSpell = arenaSpell

// LcKitSpell is the spell the kit buys runes for: the highest-level combat spell the Magic level casts. ok is false for an account whose
// Magic is unread (0).
func LcKitSpell(magic int) (name string, level, maxHit int, runes map[int]int, ok bool) {
	var best *lcSpell
	for i := range arenaSpells {
		sp := &arenaSpells[i]
		if sp.Level <= magic {
			best = sp
		}
	}
	if best == nil {
		return "", 0, 0, nil, false
	}
	return best.Name, best.Level, best.MaxHit, best.Runes, true
}

// LcKitCasts is how many casts of a spell with this maximum hit the kit carries for the spirit.
func LcKitCasts(maxHit int) int {
	if maxHit <= 0 {
		return LcMinCasts
	}
	n := (FoeTreeSpirit.Hitpoints*26*3/2 + maxHit*10 - 1) / (maxHit * 10)
	return min(max(n, LcMinCasts), LcMaxCasts)
}

// ---------------------------------------------------------------------------------------------------------
// The way out: a teleport.
// ---------------------------------------------------------------------------------------------------------

// The Magic levels of the two teleports the account may leave the dungeon with (magic_spells.dbrow [magic_spell_teleport_varrock] and
// [magic_spell_teleport_lumbridge]); LcMinMagic is the lower, the Varrock Teleport the kit carries the runes of, and a level the quest asks.
const (
	LcVarrockLevel   = 25
	LcLumbridgeLevel = 31
	LcMinMagic       = LcVarrockLevel
)

// LcExit is a teleport of the standard spellbook the account leaves the dungeon by.
type LcExit struct {
	// Name is the spell as actions.Teleport takes it.
	Name string
	// Level is the Magic level it wants and Runes the runes one cast takes, by object id (a worn elemental staff pays for its own element, and
	// the account is bare on Entrana, so none is counted).
	Level int
	Runes map[int]int
	// Landing is the spell's tele_coord: the cast sets the account down within two tiles of it (map_findsquare, radius 2).
	Landing tile.Tile
}

// lcExits are the exits, nearest to the shed first: Lumbridge Teleport lands fifty tiles from it but wants an earth rune, which the mule does not
// publish; Varrock Teleport lands 255 tiles up the road (a walk of about a minute and a half) and wants a fire, three air and a law rune, of which
// the mule holds hundreds of thousands and hundreds.
var lcExits = []LcExit{
	{Name: "Lumbridge Teleport", Level: LcLumbridgeLevel, Runes: map[int]int{arenaEarthRune: 1, arenaAirRune: 3, ObjLcLawRune: 1}, Landing: tile.Tile{X: 3221, Z: 3218}},
	{Name: "Varrock Teleport", Level: LcVarrockLevel, Runes: map[int]int{arenaFireRune: 1, arenaAirRune: 3, ObjLcLawRune: 1}, Landing: tile.Tile{X: 3213, Z: 3424}},
}

// lcKitExitIndex is the exit whose runes the trip kit carries.
const lcKitExitIndex = 1

func (e LcExit) clone() LcExit {
	runes := make(map[int]int, len(e.Runes))
	for id, n := range e.Runes {
		runes[id] = n
	}
	e.Runes = runes
	return e
}

// LcExits lists the exits (a copy), nearest to the shed first.
func LcExits() []LcExit {
	out := make([]LcExit, 0, len(lcExits))
	for _, e := range lcExits {
		out = append(out, e.clone())
	}
	return out
}

// LcKitExit is the exit the trip kit carries the runes of: Varrock Teleport.
func LcKitExit() LcExit { return lcExits[lcKitExitIndex].clone() }

// refusal says what keeps the Magic level and the pack from casting e ("" when they can). A level that is not read (0) is not judged.
func (e LcExit) refusal(pack map[int]int, magic int) string {
	if magic > 0 && magic < e.Level {
		return fmt.Sprintf("Magic %d is under the %d the %s wants", magic, e.Level, e.Name)
	}
	var short []string
	for _, id := range LcRuneIDs {
		if per := e.Runes[id]; per > 0 && pack[id] < per {
			short = append(short, fmt.Sprintf("%d of %d %s", pack[id], per, strings.ToLower(lcRuneName(id))))
		}
	}
	if len(short) > 0 {
		return fmt.Sprintf("the pack holds %s for the %s", strings.Join(short, ", "), e.Name)
	}
	return ""
}

// LcExitFor is the nearest exit the Magic level casts from the pack's runes. A Magic level that is not read (0) is not judged: the cast itself
// tells.
func LcExitFor(pack map[int]int, magic int) (LcExit, bool) {
	for _, e := range lcExits {
		if e.refusal(pack, magic) == "" {
			return e.clone(), true
		}
	}
	return LcExit{}, false
}

// LcExitRefusal says why no exit can be cast, "" when one can: the kit exit's shortfall, the one a kit round fills.
func LcExitRefusal(pack map[int]int, magic int) string {
	if _, ok := LcExitFor(pack, magic); ok {
		return ""
	}
	return "no teleport out of the dungeon can be cast: " + lcExits[lcKitExitIndex].refusal(pack, magic)
}

// lcFightStock is the pack's runes less the kit exit's: the stock the spirit's casts are priced on (and, in the kit, the runes the fight's spell
// is bought for), so that the fight never spends the cast that takes the account out.
func lcFightStock(pack map[int]int) map[int]int {
	out := make(map[int]int, len(pack))
	for id, n := range pack {
		out[id] = n
	}
	for id, n := range lcExits[lcKitExitIndex].Runes {
		out[id] = max(out[id]-n, 0)
	}
	return out
}

// lcRuneLines is the runes of the kit for an account of this Magic level, summed per rune, stackable and required: the kit exit's, one cast of
// it (the account leaves the dungeon by a teleport and never by the Magic door), and for the trip that fights the spirit the kit spell's runes
// times LcKitCasts as well (an account without them goes down to meet the spirit with its swing alone, and the odds say what that is worth).
func lcRuneLines(magic int, fight bool) []KitLine {
	need := map[int]int{}
	for id, per := range lcExits[lcKitExitIndex].Runes {
		need[id] += per
	}
	if fight {
		if _, _, maxHit, runes, ok := LcKitSpell(magic); ok {
			casts := LcKitCasts(maxHit)
			for id, per := range runes {
				need[id] += per * casts
			}
		}
	}
	var out []KitLine
	for _, id := range LcRuneIDs {
		if n := need[id]; n > 0 {
			out = append(out, KitLine{ID: id, Name: lcRuneName(id), Qty: n, Stackable: true})
		}
	}
	return out
}

func lcRuneName(id int) string {
	switch id {
	case arenaFireRune:
		return "Fire rune"
	case arenaWaterRune:
		return "Water rune"
	case arenaAirRune:
		return "Air rune"
	case arenaEarthRune:
		return "Earth rune"
	case arenaMindRune:
		return "Mind rune"
	case arenaDeathRune:
		return "Death rune"
	case arenaChaosRune:
		return "Chaos rune"
	case ObjLcLawRune:
		return "Law rune"
	}
	return fmt.Sprintf("rune %d", id)
}

// LcTripKit is the kit of a trip to the dungeon: a knife (the bank, the mule, the ground spawn in Lumbridge), food, and the runes of the cast that
// takes the account out again (every trip needs them: the Magic door lands in the Wilderness), and for the trip that fights the spirit (stage 2)
// the runes of the kit spell as well, over and above the exit's.
func LcTripKit(s Snapshot, fight bool) []KitLine {
	kit := []KitLine{lcKnifeLine()}
	sharks := LcTripSharks
	if fight {
		sharks = LcSharks
	}
	kit = append(kit, KitLine{ID: ObjShark, Name: "Shark", Qty: sharks})
	return append(kit, lcRuneLines(s.Level("magic"), fight)...)
}

// lcKitShort lists the required lines of the kit the pack is short of.
func lcKitShort(s Snapshot, kit []KitLine) []KitLine {
	var out []KitLine
	for _, l := range kit {
		if !l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			out = append(out, l)
		}
	}
	return out
}

func lcNames(lines []KitLine) string {
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		names = append(names, strings.ToLower(l.Name))
	}
	return strings.Join(names, ", ")
}

// ---------------------------------------------------------------------------------------------------------
// The prices.
// ---------------------------------------------------------------------------------------------------------

// LcSpiritOdds prices the fight with the Tree spirit on a pack: the account starts topped up (FightStartHP) from the bites it holds, casts
// what its runes pay for above the exit teleport's (lcFightStock; never a worn staff: Entrana forbids one) and swings with its own Attack
// and Strength when they run out, and must win inside LcSpiritTicks with the margin JudgeFight asks (never under the spirit's hardest hit,
// a tenth of its bites left). pack is the pack priced (the real one, or the kit's, LcPotentialPack), maxHP the account's. Nothing is
// judged for an account whose hitpoints or levels are unread.
func LcSpiritOdds(s Snapshot, pack map[int]int) FightOdds {
	odds := FightOdds{OK: true}
	if s.MaxHP <= 0 || s.Level("magic") <= 0 || s.Level("attack") <= 0 || s.Level("strength") <= 0 {
		return odds
	}
	foe := FoeTreeSpirit
	bites := FoodBites(pack, lcFoodIDs)
	hp := s.HP
	if hp <= 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	// The way to the tree runs the corridor of the greater demons first: the account arrives at the tree with what that cost, and tops up
	// from its bites before the first blow.
	hp = max(hp-int(LcCorridorDamage(s)+0.5), 1)
	for hp < FightStartHP(s.MaxHP, foe.PeakHit()) && len(bites) > 0 {
		hp = min(hp+bites[0], s.MaxHP)
		bites = bites[1:]
	}
	fighter := fighterOf(s)
	phases := CastPlan(lcFightStock(pack), nil, max(fighter.Magic, 1), 400)
	if m, ok := MeleePhase(fighter, foe); ok {
		phases = append(phases, m)
	}
	res := SimulateFight(FightSimInput{Foe: foe, Fighter: fighter, HP: hp, MaxHP: s.MaxHP, Phases: phases, Bites: bites,
		EatLine: FoeEatLine(lcSustain.EatAt, foe.PeakHit(), s.MaxHP), MaxTicks: LcSpiritTicks})
	odds.Sims = []FightSimResult{res}
	if ok, why := JudgeFight(res, foe, len(bites)); !ok {
		odds.OK, odds.Why = false, why
	}
	return odds
}

// LcPotentialPack is the pack the kit would make: what the account holds, with every line of the fight kit raised to the quantity it asks for.
func LcPotentialPack(s Snapshot) map[int]int {
	pack := make(map[int]int, len(s.InvIDs)+8)
	for id, n := range s.InvIDs {
		pack[id] = n
	}
	for _, l := range LcTripKit(s, true) {
		pack[l.ID] = max(pack[l.ID], l.Qty)
	}
	return pack
}

// LcPotentialOdds is the fight priced as if the kit were in the pack: the question a quest not yet begun asks, so that an account that could
// not beat the spirit with everything the bank and the mule can give it never starts.
func LcPotentialOdds(s Snapshot) FightOdds {
	s.HP = s.MaxHP
	return LcSpiritOdds(s, LcPotentialPack(s))
}

// LcCorridorSwings is how many swings the two greater demons of the corridor are expected to land on the account for the pass (three each:
// the account runs at twice a demon's walking pace and a demon that notices it reaches it for a swing or three).
const LcCorridorSwings = 6

// LcCorridorDamage is the hitpoints the pass of the corridor is expected to cost the account: LcCorridorSwings swings at the rate a greater
// demon hits an account of this Defence, with the quarter on top the quest fights use.
func LcCorridorDamage(s Snapshot) float64 {
	return float64(LcCorridorSwings) * FoePerAttack(FoeGreaterDemon, fighterOf(s)) * FightIncomingScale
}

// LcCorridorFit judges the corridor's pass (the way in; the way out is a teleport): the account must be able to take it from its full
// hitpoints and still stand three of the demons' hardest hits (the walk and the fight are never started with less), and hold food that mends
// what it costs. The reason names the numbers. Nothing is judged for an account whose hitpoints are unread.
func LcCorridorFit(s Snapshot, pack map[int]int) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	dmg := LcCorridorDamage(s)
	margin := float64(3 * FoeGreaterDemon.PeakHit())
	if float64(s.MaxHP)-dmg < margin {
		return false, fmt.Sprintf("the corridor's two greater demons (level 92, hits up to %d) cost an account at Defence %d about %.0f of its %d hitpoints on the pass in, and it must keep %.0f",
			FoeGreaterDemon.PeakHit(), s.Level("defence"), dmg, s.MaxHP, margin)
	}
	if pool := FoodPoolHeal(pack, lcFoodIDs); float64(pool) < dmg {
		return false, fmt.Sprintf("the corridor's pass costs about %.0f hitpoints and the food held mends %d", dmg, pool)
	}
	return true, ""
}

// ---------------------------------------------------------------------------------------------------------
// Parks and the custom steps.
// ---------------------------------------------------------------------------------------------------------

const (
	// LcFightParkFor is how long the quest skips an account the model says loses to the Tree spirit or cannot take the corridor (its levels do
	// not change in an hour).
	LcFightParkFor = 6 * time.Hour
	// LcItemParkFor is how long it skips one whose bank, mule and ground cannot give it an item (food, runes, a knife, an axe).
	LcItemParkFor = 3 * time.Hour
	// LcLegPark is how long it skips an account whose errand failed (Shamus did not come, the monks did not sail, no axe came from the
	// zombies, a walk would not end).
	LcLegPark = 30 * time.Minute
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomLcShamus  = "lc-shamus"  // the leprechaun's tree chopped and Shamus talked to
	CustomLcSail    = "lc-sail"    // the strip at Draynor and the monks' ship to Entrana
	CustomLcDescend = "lc-descend" // the cave monk and the ladder down
	CustomLcAxe     = "lc-axe"     // an axe from an Entrana zombie
	CustomLcSpirit  = "lc-spirit"  // the corridor, the Dramen tree chopped, the Tree spirit fought
	CustomLcBranch  = "lc-branch"  // a branch from the Dramen tree
	CustomLcPortal  = "lc-portal"  // the way out of the dungeon: a teleport cast (the Magic door and the Wilderness only as the last resort)
	CustomLcZanaris = "lc-zanaris" // the staff worn and the shed's door opened

	KitLcAxe  = "lc-axe"
	KitLcTrip = "lc-trip"
)

// LcPastSpirit is Module.ExclusiveDone: the account is past the one-at-a-time Tree spirit (and Shamus, and the zombies' ground) once the
// journal says its spirit is defeated.
func LcPastSpirit(s Snapshot) bool { return s.StageKnown && s.Stage >= LcSpiritDefeated }

// ---------------------------------------------------------------------------------------------------------
// The decision.
// ---------------------------------------------------------------------------------------------------------

func lcCustom(name, reason string) Step { return Step{Kind: StepCustom, Custom: name, Reason: reason} }

// lcSkillShort says which of the quest's levels the account is under (the content's two, and the Magic of the teleport out; levels the snapshot
// has not read are not judged), "" for none.
func lcSkillShort(s Snapshot) string {
	var short []string
	if lvl := s.Level("crafting"); lvl > 0 && lvl < LcCrafting {
		short = append(short, fmt.Sprintf("Crafting %d/%d", lvl, LcCrafting))
	}
	if lvl := s.Level("woodcutting"); lvl > 0 && lvl < LcWoodcutting {
		short = append(short, fmt.Sprintf("Woodcutting %d/%d", lvl, LcWoodcutting))
	}
	if lvl := s.Level("magic"); lvl > 0 && lvl < LcMinMagic {
		short = append(short, fmt.Sprintf("Magic %d/%d", lvl, LcMinMagic))
	}
	return strings.Join(short, ", ")
}

// lcPotentialRefusal is the wait that refuses an account that could not beat the spirit even with the whole kit, "" when it could.
func lcPotentialRefusal(s Snapshot) (Step, bool) {
	if odds := LcPotentialOdds(s); !odds.OK {
		return Step{Kind: StepWait, ParkFor: LcFightParkFor,
			Reason: fmt.Sprintf("not beginning Lost City: with %d sharks and the runes of its best spell the account cannot beat the Tree spirit (level 101, 85 hitpoints, hits up to 10; Attack %d, Strength %d, Defence %d, Magic %d, %d hitpoints): %s",
				LcSharks, s.Level("attack"), s.Level("strength"), s.Level("defence"), s.Level("magic"), s.MaxHP, odds.Why)}, true
	}
	if ok, why := LcCorridorFit(s, LcPotentialPack(s)); !ok {
		return Step{Kind: StepWait, ParkFor: LcFightParkFor, Reason: "not beginning Lost City: " + why}, true
	}
	return Step{}, false
}

// lcBank is the bank the quest's provisioning and strip use: Draynor, the nearest to Port Sarim whose monks sail to Entrana (the TS def's own).
func lcBank() tile.Tile { return DraynorBank }

func lcDeposit(reason string) Step {
	return Step{Kind: StepDeposit, KeepIDs: lcKeep(), Bank: lcBank(), BankSet: true, Reason: reason}
}

// lcAxeLine is the kit line for the axe the leprechaun's tree wants: the best axe the bank holds that the Woodcutting level can use
// (AxeLine's rule: a bank not read yet is not an empty one, and the mule holds bronze, steel and rune axes), else a bronze axe from Bob's
// store in Lumbridge (no level in 2004: the content's woodcutting_axe_checker asks none), which the provisioner takes from the bank or the
// mule if either has one and buys for coins otherwise. A worn axe counts as held.
func lcAxeLine(s Snapshot) KitLine {
	if s.BankKnown {
		level := s.Level("woodcutting")
		for i := len(Axes) - 1; i >= 0; i-- {
			a := Axes[i]
			if s.BankIDs[a] > 0 && (level <= 0 || AxeLevel[a] <= level) {
				return KitLine{ID: a, Name: "Axe", Qty: 1, Worn: true}
			}
		}
	}
	return KitLine{ID: ObjBronzeAxe, Name: "Bronze axe", Qty: 1, Worn: true,
		Sources: []Source{{Kind: SourceShop, Shop: BobsAxes, Price: 20, Note: "Bob's Brilliant Axes in Lumbridge keeps ten bronze axes"}}}
}

// lcAxeStep is the provisioning round for the axe the leprechaun's tree wants.
func lcAxeStep(s Snapshot) Step {
	if s.Blocked["provision:"+KitLcAxe] {
		return Step{Kind: StepWait, ParkFor: LcItemParkFor, Reason: "no axe from the bank, the mule or Bob's store for the leprechaun's tree"}
	}
	if s.FreeSlots < 2 {
		return lcDeposit("room for an axe")
	}
	return Step{Kind: StepProvision, KitName: KitLcAxe, Kit: []KitLine{lcAxeLine(s)}, KeepIDs: lcKeep(), Bank: lcBank(), BankSet: true,
		Reason: "an axe to chop the leprechaun's tree"}
}

// lcKnifeLine is the kit line for the knife that carves the branch: the bank, the mule, or the knife on the ground in Lumbridge.
func lcKnifeLine() KitLine {
	return KitLine{ID: ObjKnife, Name: "Knife", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: LcKnifeSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "a knife on the ground in Lumbridge (m50_50)"}}}
}

// lcMainlandTrip is the decision for an account on the mainland that must go down to the dungeon (stage 2 to 5 with no branch or staff
// in hand): the kit, the price and the strip and sail. fight says the Tree spirit is still to be fought (stage 2).
func lcMainlandTrip(s Snapshot, fight bool) Step {
	kit := LcTripKit(s, fight)
	if short := lcKitShort(s, kit); len(short) > 0 {
		if s.Blocked["provision:"+KitLcTrip] {
			return Step{Kind: StepWait, ParkFor: LcItemParkFor, Reason: "no " + lcNames(short) + " from the bank, the mule or the ground: Lost City goes down to the dungeon with them"}
		}
		return Step{Kind: StepProvision, KitName: KitLcTrip, Kit: kit, KeepIDs: lcKeep(), Bank: lcBank(), BankSet: true,
			Reason: "the dungeon's kit (" + lcNames(short) + ")"}
	}
	if fight {
		if odds := LcSpiritOdds(s, s.InvIDs); !odds.OK {
			return Step{Kind: StepWait, ParkFor: LcFightParkFor, Reason: "not sailing for the Tree spirit with this pack: " + odds.Why}
		}
	}
	if ok, why := LcCorridorFit(s, s.InvIDs); !ok {
		return Step{Kind: StepWait, ParkFor: LcFightParkFor, Reason: "not sailing to the dungeon: " + why}
	}
	if s.Failed[CustomLcSail] {
		return Step{Kind: StepWait, ParkFor: LcLegPark, Reason: "the monks did not sail (something forbidden still carried, or the ship was not reached)"}
	}
	return lcCustom(CustomLcSail, "strip, bank and sail to Entrana")
}

// lcDungeonStep is the decision for an account in the dungeon, stage 2 to 5: with the staff, out (a teleport; the Magic door only when none can
// be cast); with a branch, carve it; otherwise the work of the stage: at stage 2 the price of the spirit, an axe and the fight; later an axe and
// a branch.
func lcDungeonStep(s Snapshot, stage int) Step {
	switch {
	case lcStaffHeld(s):
		return lcCustom(CustomLcPortal, "out of the dungeon with the staff")
	case s.InvIDs[ObjLcBranch] > 0:
		if s.InvIDs[ObjKnife] == 0 {
			return lcCustom(CustomLcPortal, "out of the dungeon: no knife to carve the branch with (the mainland's kit has one)")
		}
		return lcCarveStep()
	}
	if stage == LcSpokenShamus {
		// Priced again here: the food of the way down is part of what the pack holds, and an account that has met the corridor and the
		// zombies is judged on what is left.
		if odds := LcSpiritOdds(s, s.InvIDs); !odds.OK {
			return lcCustom(CustomLcPortal, "out of the dungeon: the pack cannot take the Tree spirit now: "+odds.Why)
		}
	}
	if !lcAxeHeld(s) {
		if s.Failed[CustomLcAxe] {
			return Step{Kind: StepWait, ParkFor: LcLegPark, Reason: "no axe from the Entrana zombies yet"}
		}
		return lcCustom(CustomLcAxe, "an axe from an Entrana zombie: the monks took the account's own")
	}
	if stage == LcSpokenShamus {
		if s.Failed[CustomLcSpirit] {
			return Step{Kind: StepWait, ParkFor: LcFightParkFor, Reason: "the Tree spirit was not beaten (the quest's fight failed)"}
		}
		return lcCustom(CustomLcSpirit, "the corridor, the Dramen tree and the Tree spirit")
	}
	if s.Failed[CustomLcBranch] {
		return Step{Kind: StepWait, ParkFor: LcLegPark, Reason: "no branch from the Dramen tree"}
	}
	return lcCustom(CustomLcBranch, "a branch from the Dramen tree")
}

// lcCarveStep is the knife used on the branch: Crafting 31, a staff, and stage 5. Any knife, anywhere.
func lcCarveStep() Step {
	return Step{Kind: StepUseOn, Item: "Knife", ItemID: ObjKnife, TargetKind: TargetItem, Target: "Dramen branch", TargetID: ObjLcBranch,
		Reason: "carve the branch into a Dramen staff"}
}

// lcMainlandAfterTrip is the decision for an account on the mainland at stage 3 to 5 (the exit's landing, the Wilderness landing and Zanaris
// included): the staff goes to the shed, a branch to the knife, an item the bank holds is drawn, and an account with nothing goes down
// again.
func lcMainlandAfterTrip(s Snapshot, stage int) Step {
	switch {
	case lcStaffHeld(s):
		if s.Failed[CustomLcZanaris] {
			return Step{Kind: StepWait, ParkFor: LcLegPark, Reason: "the shed's door did not take the account to Zanaris (the staff not worn, or the door not reached)"}
		}
		return lcCustom(CustomLcZanaris, "wear the staff and open the shed's door")
	case s.InvIDs[ObjLcBranch] > 0:
		if s.InvIDs[ObjKnife] == 0 {
			if s.Blocked["provision:"+KitLcTrip] {
				return Step{Kind: StepWait, ParkFor: LcItemParkFor, Reason: "no knife from the bank, the mule or the ground to carve the branch with"}
			}
			return Step{Kind: StepProvision, KitName: KitLcTrip, Kit: []KitLine{lcKnifeLine()}, KeepIDs: lcKeep(), Bank: lcBank(), BankSet: true,
				Reason: "a knife to carve the Dramen branch"}
		}
		return lcCarveStep()
	}
	// Nothing in hand. A staff or a branch only exists from stage 4 (the branch) and 5 (the staff); an earlier run's may lie in the bank, which
	// is read once (the stage loop banks what a run does not keep).
	if stage >= LcTreeChopped {
		if !s.BankKnown {
			return Step{Kind: StepScanBank, Bank: lcBank(), BankSet: true, Reason: "the staff or the branch the bank may hold"}
		}
		if stage >= LcStaffMade && s.BankIDs[ObjLcStaff] > 0 {
			return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Dramen staff", Qty: 1, ID: ObjLcStaff}}, Bank: lcBank(), BankSet: true,
				Reason: "the staff the bank holds"}
		}
		if s.BankIDs[ObjLcBranch] > 0 {
			return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Dramen branch", Qty: 1, ID: ObjLcBranch}}, Bank: lcBank(), BankSet: true,
				Reason: "the branch the bank holds"}
		}
	}
	return lcMainlandTrip(s, false)
}

// DecideLostCity is the whole quest, read from the journal's stage, the account's tile and the pack.
func DecideLostCity(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not been read"}
	}
	region := LcRegionOf(s.Tile)
	if why := lcSkillShort(s); why != "" {
		if region == LcDungeon {
			return lcCustom(CustomLcPortal, "out of the dungeon: "+why)
		}
		return Step{Kind: StepWait, ParkFor: LcFightParkFor,
			Reason: fmt.Sprintf("Lost City wants Crafting %d, Woodcutting %d and Magic %d (the Varrock Teleport out of the dungeon; the Wilderness is never walked): %s",
				LcCrafting, LcWoodcutting, LcMinMagic, why)}
	}
	stage := LcNotStarted
	if s.Journal != StatusNotStarted {
		if !s.StageKnown {
			return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
		}
		stage = s.Stage
	}
	if stage >= LcComplete {
		return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about Lost City"}
	}
	switch stage {
	case LcNotStarted:
		if st, refused := lcPotentialRefusal(s); refused {
			return st
		}
		return Step{Kind: StepTalk, Stop: LcWarriorStop}
	case LcStarted:
		if st, refused := lcPotentialRefusal(s); refused {
			return st
		}
		if !lcAxeHeld(s) {
			// The bank is read once first (as the TS def's own sourceMainlandAxe does): the line asks for the best axe it holds, and an axe the
			// bank holds is no purchase.
			if !s.BankKnown {
				return Step{Kind: StepScanBank, Bank: lcBank(), BankSet: true, Reason: "an axe the bank may hold for the leprechaun's tree"}
			}
			return lcAxeStep(s)
		}
		if s.Failed[CustomLcShamus] {
			return Step{Kind: StepWait, ParkFor: LcLegPark, Reason: "Shamus did not come out of the leprechaun's tree (another account's, or the tree would not be chopped) or would not talk"}
		}
		return lcCustom(CustomLcShamus, "chop the leprechaun's tree and talk to Shamus")
	}
	// Stages 2 to 5.
	switch region {
	case LcDungeon:
		return lcDungeonStep(s, stage)
	case LcZanaris:
		// The door's teleport is the quest's end: the closing queue turns the tab green a few ticks after the jump, and an account that
		// walked out and in again meanwhile would only repeat it.
		return Step{Kind: StepWait, Reason: "in Zanaris: waiting for the quest's completion"}
	case LcIsland:
		if s.Failed[CustomLcDescend] {
			return Step{Kind: StepWait, ParkFor: LcLegPark, Reason: "the cave monk's ladder did not take the account down"}
		}
		// The ladder is one way and the dungeon is left by a cast: never go down without the means (the Wilderness is never walked).
		if why := LcExitRefusal(s.InvIDs, s.Level("magic")); why != "" {
			return Step{Kind: StepWait, ParkFor: LcItemParkFor, Reason: "not going down the one-way ladder: " + why}
		}
		return lcCustom(CustomLcDescend, "the cave monk and the ladder down")
	}
	if stage == LcSpokenShamus {
		return lcMainlandTrip(s, true)
	}
	return lcMainlandAfterTrip(s, stage)
}

// LostCityRecord is the quest's identity: Crafting and Woodcutting are the content's own (the knife on the branch, the Dramen tree), Magic is
// the module's (the Varrock Teleport out of the dungeon: the Wilderness is never walked), and the Record states them so the ladder does not
// start an account under them.
var LostCityRecord = Record{ID: LostCityID, Name: LostCityName, QuestPoints: 3,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "crafting", Level: LcCrafting}, {Skill: "woodcutting", Level: LcWoodcutting}, {Skill: "magic", Level: LcMinMagic}}}}

// LostCity is the engine module. The account never stops to fight a demon (NoRetaliate); the Tree spirit is the one fight, begun only on
// the model's word, and a lost fight, a near-death or a death parks it for a day.
var LostCity = Module{
	Record: LostCityRecord, Bank: DraynorBank, BankSet: true, Anchor: LcShedOutside,
	ToolIDs:       lcTools,
	ReadStage:     ParseLostCityJournal,
	Sustain:       lcSustain,
	NoRetaliate:   true,
	MaxDeaths:     1,
	MaxNearDeaths: 1,
	StepDeathCaps: map[string]StepDeathCap{
		CustomLcSpirit: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	ExclusiveDone: LcPastSpirit,
	Decide:        DecideLostCity,
}
