package questplan

import (
	"fmt"
	"math"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Demon Slayer (content id `demon`, %demonstart, not transmitted: the quest tab's colour and the journal are what the client
// sees), ported from the TS def (defs/demonslayer.ts) and checked against content 289: quests/quest_demon (demon_slayer.rs2,
// delrith.rs2, demon_journal.rs2, demon.npc, demon.obj, demon.constant), areas/area_varrock/scripts/{gypsy,sir_prysin,captain_rovin}.rs2,
// areas/area_draynor/scripts/traiborn.rs2, npc/scripts/dark_wizard.rs2, skill_combat/scripts/npc/npc_combat.rs2 and
// general_use/scripts/manholes.rs2, the maps (m50_52, m50_53, m50_54, m50_154, m48_49) and the hunt table (_unpack/225/all.hunt).
//
// The story, with the stage each step moves %demonstart to (general/configs/quest.constant: ^demon_complete = 30, three points):
//
//	0   Gypsy Aris (the tent in Varrock's square) wants a coin ("Ok, here you go."); her fortune names Delrith: 1.
//	1   Sir Prysin (the palace) keeps Silverlight in a box with three locks. "Gypsy Aris said I should come and talk to you." ...
//	    "So give me the keys!" sets 2 at once, whatever the account says after it.
//	2   Three keys, each an untradeable item named "Key":
//	      Wizard Traiborn's (2399), in the Wizards' Tower's first floor: "I need to get a key given to you by Sir Prysin." ...
//	      "I'll get the bones for you." moves to 3; from then each bone he is handed, one every tick, is one more stage, and the
//	      25th (stage 28) brings his ritual (three message boxes and a verse) and the key. Stage 3 to 28 read the same in the
//	      journal. A visit with fewer than 25 bones hands in what it has, so the count is not recoverable from the journal: the
//	      module carries 25 at one visit and over-gathers rather than counting.
//	      Captain Rovin's (2400), at the top of the guards' quarters in the palace's north-west wing (level 2): "Yes I know, but
//	      this is important." ... "There's a demon who wants to invade this city.", no check, key in two ticks.
//	      The drain key (2401): a bucket of water used on the drain in the palace kitchen (loc 2843, whose own op "Search" is
//	      a run of message boxes the module never sends; the kitchen sink at 3224,3494 fills the bucket) adds the key as a PUBLIC ground item at 3225,9897 in the Varrock sewer for 300
//	      ticks (^lootdrop_duration 200 * 3 / 2), and does nothing at all when the account holds the key already, in the pack or
//	      the bank. The manhole at 3237,3458 (closed, "Open"; open, "Climb down") lands at 3237,9859, forty-six steps from the key.
//	      Sir Prysin takes all three ("So how are you doing with getting the keys?") and hands over Silverlight (2402) after
//	      two p_delay(2): stage 29. A lost Silverlight is replaced for nothing at stage 29 ("No, I'm afraid I've lost Silverlight.").
//	29  Delrith (npc 879, level 27, 7 hitpoints, attack 32, strength 25, defence 27, demonbane_vulnerable) stands at the stone circle
//	    south of Varrock (3229,3369) and cannot be attacked without Silverlight WORN ("Maybe I'd better wield silverlight first.").
//	    At 0 hitpoints [ai_queue3,delrith] makes him Weakened Delrith (npc 880) for 100 ticks and queues the incantation: a chat
//	    menu of four lines, of which only "Carlem Aber Camerinthum Purchai Gabindo" is right (the TS def's "Aber Camerinthum Purchai
//	    Gabindo" is a substring of it); a wrong one heals him and he is fought again. The right one is a message box
//	    ("As you chant, Delrith is sucked towards the vortex...") and the quest-complete scroll one tick later: 30.
//
// What the content does that a script must plan for:
//   - The drain key is a clock. 300 ticks after the pour it is gone, and anyone in the sewer may take it, so the pour, the manhole and
//     the sewer are one step (questmods demonDrain), with the bucket of water ready and a free slot, and a missing key means pouring
//     again, not waiting.
//   - The keys can be in the bank (the Stage 5 trainer banks what it does not keep between runs, quest items included). A key is
//     never asked of Traiborn twice: with stage 28 and the key in the bank his script takes the account back to stage 3, and the
//     next 25 bones are 25 more, so the keys kit (a bank read per run, every line optional) comes first.
//   - Traiborn's ritual and Delrith's chant are message boxes on the MAIN modal, which the chat driver cannot see: the conversation
//     is driven by DriveUntil, which closes one per pass, and the key is waited for, not assumed.
//   - The dark wizards at the circle (young, level 7, water strike up to 4; bearded, level 20, earth strike up to 6) hunt with
//     huntmode ranged, whose check_nottoostrong skips an account of combat above twice their level outside the Wilderness
//     (Npc.ts HuntCheckNotTooStrong): combat 15 to 40 meets the two bearded ones, 14 and under all eleven, above 40 none. The
//     circle is SINGLE-way combat (the map square 50_52 has no row in maps/multiway.csv and Delrith has no npc_forcemulti), and
//     player_in_combat_check (skill_combat/scripts/player/player_combat.rs2) refuses an attack with "I'm already under attack!"
//     while a monster other than the target has fought the account in the last eight ticks. Every cast ends in
//     npc_set_attack_vars (npc_combat_magic.rs2) and a hunting wizard casts again every four ticks, so an account the wizards
//     hunt cannot start the fight with Delrith at all for as long as one of them is casting, however many hitpoints it has; and
//     both bearded wizards stand within their huntrange (5) of every tile beside him (m50_52 rows 172 at 3225,3374 and
//     3230,3365). A hunted account is therefore not priced but parked: the quest is gated on combat DemonMinCombat
//     (DemonHuntFit) and waits below it. Once Delrith has hit the account the wizards stop (npc_check_notcombat), and they hold
//     their spells from an account that is busy (a dialogue open) at stage 29, but the first blow is the one a hunted account
//     cannot throw.
//   - The sewer's monsters near the route are cowardly hunters (huntrange 1): zombies of level 24 and giant spiders of level 27 are
//     the highest among the spawns of the corridor from the ladder to the key, so an account of combat 55 and over meets none of them.

// DemonSlayerName is the quest tab's spelling (component 7334, `questlist:demon`).
const DemonSlayerName = "Demon Slayer"

// DemonSlayerID is the engine's id.
const DemonSlayerID = "demon"

// The %demonstart stages (quest_demon/configs/demon.constant, general/configs/quest.constant). The journal cannot tell stage 2 from
// 3 to 28 (the page names the keys the pack holds, not the bones given): DemonKeyHunt stands for all of them.
const (
	DemonNotStarted  = 0
	DemonTalkedAris  = 1
	DemonKeyHunt     = 2
	DemonSilverlight = 29
	DemonComplete    = 30
)

// Ids from content/pack (checked against the production config by TestDemonSlayerIDsMatchContentAndTheProductionConfig).
const (
	NpcGypsyAris         = 882 // [aris], the tent in Varrock's square
	NpcSirPrysin         = 883 // [sir_prysin], the palace
	NpcCaptainRovin      = 884 // [captain_rovin], the palace's north-west tower, level 2
	NpcTraiborn          = 881 // [traiborn], the Wizards' Tower's first floor
	NpcDelrith           = 879 // [delrith], attackable only with Silverlight worn
	NpcDelrithWeakened   = 880 // [delrith_weakened], 100 ticks, the incantation's moment
	NpcYoungDarkWizard   = 174 // [young_dark_wizard], level 7
	NpcBeardedDarkWizard = 172 // [bearded_dark_wizard], level 20
	NpcSewerZombie       = 75  // [zombie_armed], level 24
	NpcSewerGiantSpider  = 60  // [giantspider2], level 27

	ObjKeyTraiborn = 2399 // [silverlight_key_1], "A key given to me by Wizard Traiborn."
	ObjKeyRovin    = 2400 // [silverlight_key_2], "A key given to me by Captain Rovin."
	ObjKeyDrain    = 2401 // [silverlight_key_3], "A key I found in a drain."
	ObjSilverlight = 2402 // [silverlight]

	LocQuestDrain    = 2843 // [questdrain], the palace kitchen's drain: oplocu with a bucket of water
	LocManholeClosed = 881  // [manholeclosed], op1 Open
	LocManholeOpen   = 882  // [manholeopen], op1 Climb down, back to closed after 400 ticks
)

// DemonKeyTicks is how long the pour's key lies on the sewer floor: obj_add's duration is ^lootdrop_duration (200) * 3 / 2 ticks.
const DemonKeyTicks = 300

// DemonBonesNeeded is the sets of bones Traiborn wants (^demon_got_traiborn_key 28 less ^demon_find_bones 3 is 25 stages of one bone).
const DemonBonesNeeded = 25

// DemonCoinFloat is the coins the quest wants in the pack: the Gypsy's one, a bucket's two and a margin. The bank and the mule are the
// only sources of coins.
const DemonCoinFloat = 30

// The incantation. The menu's four lines are quest_demon/scripts/delrith.rs2's p_choice4; only the last is right, and the three wrong
// ones share no run of words with it.
const DemonIncantation = "Carlem Aber Camerinthum Purchai Gabindo"

// Tiles from content maps (m50_52, m50_53, m50_54, m50_154, m48_49) and the loc index.
var (
	// GypsyAnchor is Aris's spawn (m50_53 "0 3 32: 882"), PrysinAnchor Sir Prysin's (m50_54 "0 4 17: 883"), RovinAnchor Captain Rovin's
	// (m50_54 "2 4 40: 884") and TraibornAnchor Traiborn's (m48_49 "1 40 26: 881").
	GypsyAnchor    = t3(3203, 3424, 0)
	PrysinAnchor   = t3(3204, 3473, 0)
	RovinAnchor    = t3(3204, 3496, 2)
	TraibornAnchor = t3(3112, 3162, 1)
	// DelrithSpawn is Delrith's (m50_52 "0 29 41: 879"), the middle of the stone circle.
	DelrithSpawn = t3(3229, 3369, 0)
	// DemonSinkStand is a tile beside the kitchen's sink (loc 874 at 3224,3494; its stands are 3223,3494, 3223,3495 and 3224,3496).
	DemonSinkStand = t3(3223, 3494, 0)
	// DemonDrain is the drain (3225,3495), which is its own only stand.
	DemonDrain = t3(3225, 3495, 0)
	// DemonManhole is the closed manhole (3237,3458) and DemonManholeStand the tile north of it the graph's edge begins on.
	DemonManhole      = t3(3237, 3458, 0)
	DemonManholeStand = t3(3237, 3459, 0)
	// DemonSewerLanding is where "Climb down" puts an account standing at DemonManholeStand (z + 6400), DemonSewerLadder the ladder
	// back up (loc 1755 at 3237,9858) and DemonSewerKey where the pour adds the key (m50_154: 0_50_154_25_41).
	DemonSewerLanding = t3(3237, 9859, 0)
	DemonSewerLadder  = t3(3237, 9858, 0)
	DemonSewerKey     = t3(3225, 9897, 0)
	// DemonCowField is the Lumbridge cow pasture the bones are farmed in (questmods.CowField).
	DemonCowField = t3(3259, 3270, 0)
	// DemonBucketSpawn is a bucket on the palace's first floor above the kitchen (m50_54 "1 21 41: 1925"), the one Priest in Peril
	// takes.
	DemonBucketSpawn = t3(3221, 3497, 1)
)

// InVarrockSewer reports a tile in the Varrock sewers' own floor (x 3130 to 3270, z 9856 to 9922: the moss giants' ground and the
// corridor to the drain key; the Wilderness dungeons begin at z 9923).
func InVarrockSewer(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 3130 && t.X <= 3270 && t.Z >= 9856 && t.Z <= 9922
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomDemonTraiborn = "demon-traiborn"
	CustomDemonWater    = "demon-water"
	CustomDemonDrain    = "demon-drain"
	CustomDemonSewer    = "demon-sewer"
	CustomDemonWield    = "demon-wield"
	CustomDemonDelrith  = "demon-delrith"

	KitDemonCoins      = "demon-coins"
	KitDemonKeys       = "demon-keys"
	KitDemonBones      = "demon-bones"
	KitDemonBucket     = "demon-bucket"
	KitDemonFoodSewer  = "demon-food-sewer"
	KitDemonFoodCircle = "demon-food-circle"

	// MethodDemonBones is the provisioner's skill source that kills the Lumbridge cows for their bones.
	MethodDemonBones = "demon-bones"
)

// How long the quest is skipped when a step is a dead end the account cannot get past by itself.
const (
	// DemonCoinRetry: not one coin for the Gypsy from the pack, the bank or the mule.
	DemonCoinRetry = 2 * time.Hour
	// DemonFitRetry: too few hitpoints, or too little hitpoints and food, for Delrith or the sewer, or a combat level the circle's dark
	// wizards hunt (DemonMinCombat). Hitpoints and combat levels come slowly.
	DemonFitRetry = 12 * time.Hour
	// DemonLowHPRetry: the account's hitpoints are low and it has no food; they come back by the hour.
	DemonLowHPRetry = 30 * time.Minute
	// DemonBonesRetry: no 25 bones from the pack, the bank, the mule or the cows.
	DemonBonesRetry = 3 * time.Hour
	// DemonTraibornRetry: the Wizards' Tower visit did not end with the key.
	DemonTraibornRetry = 3 * time.Hour
	// DemonBucketRetry: no bucket from the bank, the mule, the palace floor or Varrock's general store, or none could be filled.
	DemonBucketRetry = 2 * time.Hour
	// DemonDrainRetry: the pour and the sewer did not end with the key (the key had gone, or the way down was shut).
	DemonDrainRetry = 3 * time.Hour
	// DemonWieldRetry: Silverlight would not go on.
	DemonWieldRetry = time.Hour
	// DemonDelrithRetry: the visit to the stone circle did not end with the quest (no Delrith, or the account turned back).
	DemonDelrithRetry = 3 * time.Hour
	// DemonSewerRetry: the account could not climb out of the sewer. Any walk the next script makes takes it out.
	DemonSewerRetry = time.Hour
)

// DemonFoods are the foods the account eats on the way, best first (the fight foods' table, shark to bread).
var DemonFoods = []int{385, 373, 379, 365, 361, 329, 333, 1891, 1893, 1895, 1897, 1899, 1901, 2309}

// demonTools are the quest's own items and the coins: what every bank visit of the quest leaves in the pack. Bones are not among them
// (the bones kit keeps its own), or an account that gathered 25 and lost its place would carry them through every errand after; nor
// are the bucket and its water, which the bones trip's bare pack lets go of (25 bones, the coins and two keys are all of the 28 slots
// an account that arrives at the tower with the Rovin and drain keys has), and which only the steps that use them keep.
var demonTools = []int{CoinsID, ObjKeyTraiborn, ObjKeyRovin, ObjKeyDrain, ObjSilverlight}

// demonKeep is a bank visit's keep list: the tools, the bucket and its water, and the food.
func demonKeep() []int {
	return append(append(append([]int(nil), demonTools...), ObjBucket, ObjBucketOfWater), DemonFoods...)
}

// demonBonesKeep is the bones trip's keep list: the bones, the coins and the quest's keys.
func demonBonesKeep() []int {
	return []int{CoinsID, ObjBones, ObjKeyTraiborn, ObjKeyRovin, ObjKeyDrain, ObjSilverlight}
}

// DemonToolIDs returns the module's tools (a copy): what the floor bite leaves alone and every bank visit keeps.
func DemonToolIDs() []int { return append([]int(nil), demonTools...) }

// DemonKeepIDs returns the keep list (a copy).
func DemonKeepIDs() []int { return demonKeep() }

// DemonBonesKeepIDs returns the bones trip's keep list (a copy).
func DemonBonesKeepIDs() []int { return demonBonesKeep() }

// demonBank is the bank nearest the account (Draynor for an account in Lumbridge or Al Kharid: neither has a usable booth).
func demonBank(s Snapshot) tile.Tile { return cookSheepBank(s) }

// The stops. Preference order is priority order (PickPreferred): the first line listed that the menu offers is the one chosen, so a
// list is the whole conversation's path in the order its menus come.
var (
	// GypsyArisStop starts the quest. The first menu is "Ok, here you go." (a coin) or two refusals; after the fortune the menus are
	// the Aaargh question, Delrith, the way to meet him, and last the pair "What is the magical incantation?" / "Where can I find
	// Silverlight?" followed by the pair that ends it. Stage 1 is set on the way (demon_slayer_aris_destroy_delrith).
	GypsyArisStop = NpcStop{Npc: "Gypsy Aris", ID: NpcGypsyAris, Anchor: GypsyAnchor, Leash: 6,
		Prefer: []string{"Ok, here you go.", "Very interesting. What does that Aaargh bit mean?", "Who's Delrith?",
			"Okay, where is he? I'll kill him for you!", "Where can I find Silverlight?", "Okay, thanks. I'll do my best to stop the demon."}}
	// PrysinIntroStop asks for the keys: stage 1's menu has "Gypsy Aris said I should come and talk to you." that stage 0's lacks. The
	// stage becomes 2 at "So give me the keys!"; the two menus after it end in "Well I'd better go key hunting.".
	PrysinIntroStop = NpcStop{Npc: "Sir Prysin", ID: NpcSirPrysin, Anchor: PrysinAnchor, Leash: 6,
		Prefer: []string{"Gypsy Aris said I should come and talk to you.", "I need to find Silverlight.",
			"He's back and unfortunately I've got to deal with him.", "So give me the keys!", "Where does the wizard live?",
			"Well I'd better go key hunting."}}
	// PrysinKeysStop hands the three keys over. With all three held the conversation has no menu before the hand-in
	// (demon_slayer_prysin_got_them); the preference is a placeholder a talk step must name.
	PrysinKeysStop = NpcStop{Npc: "Sir Prysin", ID: NpcSirPrysin, Anchor: PrysinAnchor, Leash: 6,
		Prefer: []string{"I'm still looking."}}
	// PrysinLostSwordStop asks for Silverlight again at stage 29 (demon_slayer_need_to_kill; the menu appears only when the account
	// holds no Silverlight in the pack, worn or in the bank).
	PrysinLostSwordStop = NpcStop{Npc: "Sir Prysin", ID: NpcSirPrysin, Anchor: PrysinAnchor, Leash: 6,
		Prefer: []string{"No, I'm afraid I've lost Silverlight."}}
	// RovinStop is Captain Rovin: the first menu is "Yes I know, but this is important." (offered at stages 2 to 28 only) and the
	// second the demon.
	RovinStop = NpcStop{Npc: "Captain Rovin", ID: NpcCaptainRovin, Anchor: RovinAnchor, Leash: 6,
		Prefer: []string{"Yes I know, but this is important.", "There's a demon who wants to invade this city."}}
	// TraibornStop is Traiborn: at stage 2 "I need to get a key given to you by Sir Prysin." then "Well, have you got any keys
	// knocking around?" then "I'll get the bones for you."; from stage 3 on his talk has no menu until the key is out.
	TraibornStop = NpcStop{Npc: "Traiborn", ID: NpcTraiborn, Anchor: TraibornAnchor, Leash: 6,
		Prefer: []string{"I need to get a key given to you by Sir Prysin.", "Well, have you got any keys knocking around?", "I'll get the bones for you."}}
)

// demonJournalNeedles are ParseDemonSlayerJournal's phrases against NormaliseLines, newest stage first (a later page strikes the
// earlier lines through and keeps them), from demon_journal.rs2.
var demonJournalNeedles = []struct {
	stage   int
	needles []string
}{
	{DemonComplete, []string{"quest complete"}},
	{DemonSilverlight, []string{"now i should go to the stone circle south of the city"}},
	{DemonKeyHunt, []string{"sir prysin needs 3 keys before he can give me silverlight", "now i have all 3 keys"}},
	{DemonTalkedAris, []string{"i should ask sir prysin in varrock palace where it is"}},
	{DemonNotStarted, []string{"i can start this quest by speaking to the gypsy"}},
}

// ParseDemonSlayerJournal reads the stage from the rendered journal; ok is false for an empty or unrecognised journal, which is
// unknown, never stage 0. Stage 2 stands for 2 to 28.
func ParseDemonSlayerJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range demonJournalNeedles {
		for _, needle := range n.needles {
			if strings.Contains(text, needle) {
				return n.stage, true
			}
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------------------------------------
// The foes and what the visits are priced at.
// ---------------------------------------------------------------------------------------------------------

// FoeDelrith is quest_demon/configs/demon.npc [delrith]: attack 32, defence 27, strength 25, 7 hitpoints, no attackrate (4), no bonus
// params (a hit of up to 3), crush. He is not aggressive (no huntmode); he fights the account that attacks him.
var FoeDelrith = MeleeFoe{Name: "Delrith", Attack: 32, Strength: 25, Defence: 27, Magic: 1, Hitpoints: 7, AttackTicks: 4}

// The dark wizards of the circle (_unpack/225/all.npc): they cast every four ticks (npc/scripts/dark_wizard.rs2, ~npc_cast_spell(..., 4))
// and only strikes hurt (the weaken and the confuse they also cast take no hitpoints), as casters that always strike: ZapOneIn 1, the
// spell table's maximum (water strike 4, earth strike 6, magic_combat_spells.dbrow). They are never priced: an account they hunt is
// parked (DemonHuntFit), and one they skip is out of their reach. The definitions stay as the content-pinned facts the gate rests on
// (the levels the hunt ceilings are twice of).
var (
	// FoeYoungDarkWizard is [young_dark_wizard]: level 7, magic 6, 12 hitpoints, huntrange 3.
	FoeYoungDarkWizard = MeleeFoe{Name: "Dark wizard", Attack: 5, Strength: 2, Defence: 5, Magic: 6, Hitpoints: 12, AttackTicks: 4, ZapMax: 4, ZapOneIn: 1}
	// FoeBeardedDarkWizard is [bearded_dark_wizard]: level 20, magic 22, 24 hitpoints, huntrange 5.
	FoeBeardedDarkWizard = MeleeFoe{Name: "Dark wizard", Attack: 17, Strength: 17, Defence: 14, Magic: 22, Hitpoints: 24, AttackTicks: 4, ZapMax: 6, ZapOneIn: 1}
	// FoeSewerZombie is [zombie_armed] (level 24, attackrate 5, bonuses 5 and 7, huntrange 1) and FoeSewerGiantSpider [giantspider2]
	// (level 27, huntrange 1): the hardest hitters near the drain key's route.
	FoeSewerZombie = MeleeFoe{Name: "Zombie", Attack: 19, Strength: 21, Defence: 16, Magic: 1, Hitpoints: 30, AttackTicks: 5, AttackBonus: 5, StrengthBonus: 7}
	FoeSewerSpider = MeleeFoe{Name: "Giant spider", Attack: 20, Strength: 24, Defence: 21, Magic: 1, Hitpoints: 32, AttackTicks: 4}
)

// demonHunter is a kind of monster that hunts the account at the circle: Foe, and the combat level above which the hunt check skips
// the account (twice the monster's level).
type demonHunter struct {
	Foe     MeleeFoe
	Ceiling int
}

// demonCircleHunters are the circle's dark wizards: the nine young ones and the two bearded ones of the map. An account any of them
// hunts is parked (DemonHuntFit); the bearded ones stand within their huntrange of every tile beside Delrith.
var demonCircleHunters = []demonHunter{
	{FoeYoungDarkWizard, 14},
	{FoeBeardedDarkWizard, 40},
}

// demonCombatMargin is how far above a monster's ceiling an account's computed combat level must be before it is treated as skipped
// by the hunt: the account's own reading of its levels can be a point off the server's.
const demonCombatMargin = 1

// DemonMinCombat is the lowest combat level at which no dark wizard of the circle hunts the account: the bearded ones (level 20) skip
// an account above 40, and demonCombatMargin more is asked for. Below it the stone circle is never visited, and the quest is not
// begun (DemonHuntFit).
const DemonMinCombat = 2*20 + demonCombatMargin + 1

// DemonUnderAttackLimit is how many attacks on Delrith the server may refuse with "I'm already under attack!" before the visit leaves
// the circle: a wizard's spell landed in the last eight ticks, which the gate should have kept the account out of.
const DemonUnderAttackLimit = 2

// DemonUnderAttackLine is the server's refusal (player_combat.rs2, player_in_combat_check), as the game chat carries it.
const DemonUnderAttackLine = "I'm already under attack"

// DemonRefusedUnderAttack reports whether the game chat said since an attack went out holds the single-combat refusal.
func DemonRefusedUnderAttack(chat string) bool { return strings.Contains(chat, DemonUnderAttackLine) }

// DemonSewerAttacks is how many attacks of the sewer's hardest hitter the walk from the manhole to the key and back is priced at, for an
// account the monsters hunt: a cowardly hunter only attacks from one tile away and the account runs, so a handful of passes is the
// count a bad route gives.
const DemonSewerAttacks = 8

// DemonSewerCeiling is the combat level above which none of the sewer monsters near the route (the highest is the giant spider, 27)
// hunts the account.
const DemonSewerCeiling = 54

// DemonMinHitpoints is the least maximum Hitpoints the quest starts at (the fleet's own floor, for Delrith's blows and the sewer's
// monsters').
const DemonMinHitpoints = 20

// DemonMaxKillTicks is the longest the model may take to land Delrith's seven hitpoints with Silverlight (fourteen swings): an account
// that needs more cannot be expected to finish him before the wizards or he wears it down.
const DemonMaxKillTicks = 70

// demonCombat is the 2004 combat level from base levels by lower-case name, 0 when the defence or hitpoints level is unread.
func demonCombat(levels map[string]int) int {
	lv := func(k string) float64 { return float64(levels[k]) }
	if levels["defence"] <= 0 || levels["hitpoints"] <= 0 {
		return 0
	}
	base := 0.25 * (lv("defence") + lv("hitpoints") + float64(levels["prayer"]/2))
	melee := 0.325 * (lv("attack") + lv("strength"))
	ranged := 0.325 * float64(int(lv("ranged")*1.5))
	magic := 0.325 * float64(int(lv("magic")*1.5))
	best := math.Max(melee, math.Max(ranged, magic))
	return int(base + best)
}

// DemonCombat is the account's combat level from its snapshot's levels, 0 when they are unread.
func DemonCombat(s Snapshot) int { return demonCombat(s.Levels) }

// demonHunted is whether a monster with this ceiling hunts the account: an unread combat level counts as hunted.
func demonHunted(s Snapshot, ceiling int) bool {
	c := demonCombat(s.Levels)
	return c == 0 || c <= ceiling+demonCombatMargin
}

// demonSwing is the account's swing with Silverlight in hand: its levels, the sword's slash attack (14) and strength (12) bonuses and
// its five-tick rate (oc silverlight: slashattack 14, strengthbonus 12, attackrate 5). The 1.6 times maximum hit it gets against a
// demon (player_melee.rs2, demonbane_vulnerable) is left out: the model is a ceiling on the damage taken.
func demonSwing(s Snapshot) MeleeFighter {
	f := fighterOf(s)
	f.AttackBonus, f.StrengthBonus, f.AttackTicks = 14, 12, 5
	return f
}

// DemonHuntFit is the combat half of the gate before the stone circle: false when a dark wizard of the circle hunts the account. The
// circle is single-way combat, so a wizard whose spell landed in the last eight ticks makes the server refuse the attack on Delrith
// ("I'm already under attack!"), and the wizards that hunt an account cast again every four ticks for as long as it stands in their
// range: the visit cannot be priced in hitpoints and food, only declined. An unread combat level passes, as every other stat here does;
// the visit's own watch (questmods demonDelrith) leaves the circle when the refusals come.
func DemonHuntFit(s Snapshot) (bool, string) {
	c := demonCombat(s.Levels)
	if c == 0 {
		return true, ""
	}
	for _, h := range demonCircleHunters {
		if demonHunted(s, h.Ceiling) {
			return false, fmt.Sprintf("not doing Demon Slayer yet: the dark wizards round Delrith's stone circle hunt every account of combat %d and under, the circle is single-way combat and the server refuses the attack on Delrith (\"I'm already under attack!\") while a wizard's spell has landed in the last eight ticks (this account is combat %d, the gate is %d)",
				DemonMinCombat-1, c, DemonMinCombat)
		}
	}
	return true, ""
}

// demonFoodHeld is the hitpoints of food the pack holds.
func demonFoodHeld(s Snapshot) int { return FoodPoolHeal(s.InvIDs, DemonFoods) }

// DemonFitHitpoints judges the hitpoints half of the gate, which needs no food: an unread maximum passes, as every other gate here
// treats an unread stat.
func DemonFitHitpoints(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	if s.MaxHP < DemonMinHitpoints {
		return false, fmt.Sprintf("not doing Demon Slayer yet: Delrith (level 27) hits up to %d and the monsters of the Varrock sewer up to %d (this account has %d hitpoints, the gate is %d)",
			FoeDelrith.PeakHit(), max(FoeSewerZombie.PeakHit(), FoeSewerSpider.PeakHit()), s.MaxHP, DemonMinHitpoints)
	}
	return true, ""
}

// DemonFit is the gate before the stone circle: the hitpoints, a combat level the dark wizards leave alone (DemonHuntFit: a hunted
// account cannot start the fight), a swing that fells Delrith's seven hitpoints in time, and the hitpoints above two of his hits plus
// the food in the pack covering twice what his own blows are priced at over the time the model needs to kill him.
func DemonFit(s Snapshot) (bool, string) {
	if ok, why := DemonFitHitpoints(s); !ok {
		return false, why
	}
	if ok, why := DemonHuntFit(s); !ok {
		return false, why
	}
	if s.MaxHP <= 0 || s.Level("attack") <= 0 || s.Level("strength") <= 0 {
		return true, ""
	}
	x := MeleeExchange(demonSwing(s), FoeDelrith)
	if math.IsInf(x.KillTicks, 1) || x.KillTicks > DemonMaxKillTicks {
		return false, fmt.Sprintf("not going to Delrith with this swing: the model needs %s ticks to fell his seven hitpoints at Attack %d, Strength %d with Silverlight, and the limit is %d",
			killTicksText(x.KillTicks), s.Level("attack"), s.Level("strength"), DemonMaxKillTicks)
	}
	damage := x.DamageTaken * FightIncomingScale
	room := float64(s.MaxHP - 2*FoeDelrith.PeakHit())
	if have := room + float64(demonFoodHeld(s)); have < 2*damage {
		return false, fmt.Sprintf("not going to the stone circle with this pack: Delrith is priced at %d hitpoints (Defence %d) and %d hitpoints less two of his hardest hits plus %d of food is %d, short of twice that",
			int(damage+0.5), s.Level("defence"), s.MaxHP, demonFoodHeld(s), int(have))
	}
	return true, ""
}

func killTicksText(t float64) string {
	if math.IsInf(t, 1) {
		return "an unbounded number of"
	}
	return fmt.Sprintf("%d", int(t+0.5))
}

// DemonSewerDamage is the hitpoints the walk to the drain key and back is priced at, for an account the sewer's monsters hunt: the
// attacks above, each the worse of the zombie's and the giant spider's expected damage on this account's Defence.
func DemonSewerDamage(s Snapshot) float64 {
	if !demonHunted(s, DemonSewerCeiling) {
		return 0
	}
	p := fighterOf(s)
	per := math.Max(FoePerAttack(FoeSewerZombie, p), FoePerAttack(FoeSewerSpider, p))
	return float64(DemonSewerAttacks) * per * FightIncomingScale
}

// DemonSewerFit is the gate before the manhole: the hitpoints, and the hitpoints above two hits of the hardest sewer hitter plus the
// food in the pack covering twice what the walk is priced at.
func DemonSewerFit(s Snapshot) (bool, string) {
	if ok, why := DemonFitHitpoints(s); !ok {
		return false, why
	}
	if s.MaxHP <= 0 {
		return true, ""
	}
	damage := DemonSewerDamage(s)
	peak := max(FoeSewerZombie.PeakHit(), FoeSewerSpider.PeakHit())
	room := float64(s.MaxHP - 2*peak)
	if have := room + float64(demonFoodHeld(s)); have < 2*damage {
		return false, fmt.Sprintf("not going down the manhole with this pack: the sewer's zombies and giant spiders that hunt combat %d are priced at %d hitpoints (Defence %d) and %d hitpoints less two of their hits plus %d of food is %d, short of twice that",
			DemonCombat(s), int(damage+0.5), s.Level("defence"), s.MaxHP, demonFoodHeld(s), int(have))
	}
	return true, ""
}

// ---------------------------------------------------------------------------------------------------------
// The kits.
// ---------------------------------------------------------------------------------------------------------

// demonCoinsKit is the purse: the bank and the mule are the only sources of coins.
func demonCoinsKit() []KitLine {
	return []KitLine{{ID: CoinsID, Name: "Coins", Qty: DemonCoinFloat, Stackable: true}}
}

// demonKeysKit draws whatever the trainer banked of the quest's own items, once per run: the three keys and Silverlight, all quest items
// (never asked of the mule or bought), all optional, so a bank without them costs only the bank read.
func demonKeysKit() []KitLine {
	return []KitLine{
		{ID: ObjKeyTraiborn, Name: "Traiborn's key", Qty: 1, Untradeable: true, Optional: true},
		{ID: ObjKeyRovin, Name: "Rovin's key", Qty: 1, Untradeable: true, Optional: true},
		{ID: ObjKeyDrain, Name: "the drain key", Qty: 1, Untradeable: true, Optional: true},
		{ID: ObjSilverlight, Name: "Silverlight", Qty: 1, Untradeable: true, Optional: true, Worn: true},
	}
}

// demonBonesKit is the 25 bones: the bank, the mule, then the cows' (the owner's item rule; the skill source is questmods demonBones).
func demonBonesKit() []KitLine {
	return []KitLine{{ID: ObjBones, Name: "Bones", Qty: DemonBonesNeeded, Sources: []Source{{
		Kind: SourceSkill, Skill: "attack", Method: MethodDemonBones, Tile: DemonCowField,
		Note: "the Lumbridge cows' bones, one a kill"}}}}
}

// demonBucketKit is the bucket the drain's water is carried in: a bucket of water first (it needs no sink), else the empty bucket the
// bank or the mule holds, the one on the palace's first floor, then Varrock's general store.
func demonBucketKit() []KitLine {
	return []KitLine{
		{ID: ObjBucketOfWater, Name: "Bucket of water", Qty: 1, Optional: true},
		{ID: ObjBucket, Name: "Bucket", Qty: 1, OnlyIfShort: ObjBucketOfWater, Sources: []Source{
			{Kind: SourceGround, Tile: DemonBucketSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the bucket on the palace's first floor"},
			{Kind: SourceShop, Shop: VarrockGenStore, Price: 2, Note: "Varrock's general store keeps 100 buckets"},
		}},
	}
}

// demonFoodKit is a pass of food, whatever the bank or the mule has of it: optional, so a short bank never blocks the quest (the gate
// that needs food judges the pack afterwards).
func demonFoodKit() []KitLine {
	return []KitLine{
		{ID: ObjShark, Name: "Shark", Qty: 4, Optional: true},
		{ID: ObjBass, Name: "Bass", Qty: 4, Optional: true},
	}
}

// ---------------------------------------------------------------------------------------------------------
// Decide.
// ---------------------------------------------------------------------------------------------------------

// demonProvision is a provisioning step of the quest.
func demonProvision(s Snapshot, name string, kit []KitLine, keep []int, bare bool, reason string) Step {
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: keep, Bank: demonBank(s), BankSet: true, Bare: bare, Reason: reason}
}

// demonOnce is whether a kit has still to be tried this run.
func demonOnce(s Snapshot, name string) bool {
	return !s.Ran["provision:"+name] && !s.Failed["provision:"+name]
}

// DecideDemonSlayer is the whole quest, read from the quest tab, the journal stage and what the pack holds and the account wears.
func DecideDemonSlayer(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return decideDemonStart(s)
	}
	// An account in the sewer is out of it before anything else, whatever the journal says: the only reason to be there is the drain
	// key, and the monsters do not stop.
	if s.TileKnown && InVarrockSewer(s.Tile) {
		if s.Failed[CustomDemonSewer] {
			return Step{Kind: StepWait, ParkFor: DemonSewerRetry, Reason: "the account could not take the sewer's ladder up (the next walk it makes goes by the manhole)"}
		}
		return Step{Kind: StepCustom, Custom: CustomDemonSewer, Reason: "in the sewer: the drain key if it lies there, then up the ladder"}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	// A quest begun by an account the wizards hunt (by hand, or before its combat was read) is not carried on: the keys are an hour's
	// work for a fight it cannot start.
	if ok, why := DemonHuntFit(s); !ok {
		return Step{Kind: StepWait, ParkFor: DemonFitRetry, Reason: why}
	}
	switch s.Stage {
	case DemonNotStarted:
		return decideDemonStart(s)
	case DemonTalkedAris:
		return Step{Kind: StepTalk, Stop: PrysinIntroStop}
	case DemonKeyHunt:
		return decideDemonKeys(s)
	case DemonSilverlight:
		return decideDemonFight(s)
	}
	return Step{Kind: StepWait, Reason: "unrecognised Demon Slayer stage"}
}

// decideDemonStart is the Gypsy: a hitpoints gate and a combat gate (an account that cannot start the fight with Delrith does not spend
// an hour on keys), then a coin.
func decideDemonStart(s Snapshot) Step {
	if ok, why := DemonFitHitpoints(s); !ok {
		return Step{Kind: StepWait, ParkFor: DemonFitRetry, Reason: why}
	}
	if ok, why := DemonHuntFit(s); !ok {
		return Step{Kind: StepWait, ParkFor: DemonFitRetry, Reason: why}
	}
	if s.HeldID(CoinsID) < 1 {
		if s.Failed["provision:"+KitDemonCoins] {
			return Step{Kind: StepWait, ParkFor: DemonCoinRetry, Reason: "not one coin for Gypsy Aris from the pack, the bank or the mule"}
		}
		return demonProvision(s, KitDemonCoins, demonCoinsKit(), demonKeep(), false, "a coin for the Gypsy, and the bucket's two")
	}
	return Step{Kind: StepTalk, Stop: GypsyArisStop}
}

// decideDemonKeys is stage 2: draw what the bank holds of the quest, then the three keys in the order the pack allows (Traiborn's, whose
// 25 bones fill the pack, while it holds nothing else; then Rovin's, then the drain's), then Sir Prysin.
func decideDemonKeys(s Snapshot) Step {
	if demonOnce(s, KitDemonKeys) {
		return demonProvision(s, KitDemonKeys, demonKeysKit(), demonKeep(), false, "any key or sword the trainer banked, before a key is asked for twice")
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if held(ObjKeyTraiborn) && held(ObjKeyRovin) && held(ObjKeyDrain) {
		return Step{Kind: StepTalk, Stop: PrysinKeysStop, UntilItemID: ObjSilverlight}
	}
	room := func(next Step) Step {
		if s.FreeSlots < 2 {
			return Step{Kind: StepDeposit, KeepIDs: demonKeep(), Bank: demonBank(s), BankSet: true, Reason: "room for a key"}
		}
		return next
	}
	switch {
	case !held(ObjKeyTraiborn):
		return decideDemonTraiborn(s)
	case !held(ObjKeyRovin):
		return room(Step{Kind: StepTalk, Stop: RovinStop, UntilItemID: ObjKeyRovin})
	}
	return decideDemonDrain(s, room)
}

// decideDemonTraiborn is the Wizards' Tower: 25 bones in the pack (a bare-pack trip: they fill all but three slots), then Traiborn.
func decideDemonTraiborn(s Snapshot) Step {
	if s.Failed[CustomDemonTraiborn] {
		return Step{Kind: StepWait, ParkFor: DemonTraibornRetry, Reason: "the visit to Traiborn did not end with his key (no Traiborn in the tower, or the ritual would not run)"}
	}
	if s.HeldID(ObjBones) < DemonBonesNeeded {
		if s.Failed["provision:"+KitDemonBones] {
			return Step{Kind: StepWait, ParkFor: DemonBonesRetry, Reason: "no 25 bones from the pack, the bank, the mule or the Lumbridge cows"}
		}
		return demonProvision(s, KitDemonBones, demonBonesKit(), demonBonesKeep(), true, "25 sets of bones for Traiborn: the bank, the mule, then the cows")
	}
	return Step{Kind: StepCustom, Custom: CustomDemonTraiborn, Reason: "the bones to Traiborn in the Wizards' Tower, and his key"}
}

// decideDemonDrain is the drain key: a bucket of water from the pack, else the empty bucket filled at the kitchen's sink, else one
// provisioned; the pour, the manhole, the sewer and the key are one step, behind the sewer gate.
func decideDemonDrain(s Snapshot, room func(Step) Step) Step {
	if s.Failed[CustomDemonDrain] {
		return Step{Kind: StepWait, ParkFor: DemonDrainRetry, Reason: "the pour and the sewer did not end with the drain key (it had gone, or the way down would not open)"}
	}
	if demonOnce(s, KitDemonFoodSewer) {
		return demonProvision(s, KitDemonFoodSewer, demonFoodKit(), demonKeep(), false, "food for the sewer's zombies and spiders, if the bank or the mule has it")
	}
	if ok, why := DemonSewerFit(s); !ok {
		return Step{Kind: StepWait, ParkFor: DemonFitRetry, Reason: why}
	}
	if s.HeldID(ObjBucketOfWater) > 0 {
		return room(Step{Kind: StepCustom, Custom: CustomDemonDrain, Reason: "the bucket of water down the kitchen drain, then the manhole and the key in the sewer"})
	}
	if s.HeldID(ObjBucket) > 0 {
		if s.Failed[CustomDemonWater] {
			return Step{Kind: StepWait, ParkFor: DemonBucketRetry, Reason: "the bucket could not be filled at the palace kitchen's sink"}
		}
		return Step{Kind: StepCustom, Custom: CustomDemonWater, Reason: "fill the bucket at the kitchen's sink"}
	}
	if s.Failed["provision:"+KitDemonBucket] {
		return Step{Kind: StepWait, ParkFor: DemonBucketRetry, Reason: "no bucket from the bank, the mule, the palace's first floor or Varrock's general store"}
	}
	return room(demonProvision(s, KitDemonBucket, demonBucketKit(), demonKeep(), false, "a bucket of water (or the bucket to fill) for the palace drain"))
}

// decideDemonFight is stage 29: Silverlight in hand (drawn from the bank, else asked of Sir Prysin again), the gate, the sword worn, then Delrith.
func decideDemonFight(s Snapshot) Step {
	worn, held := s.WornIDs[ObjSilverlight] > 0, s.HeldID(ObjSilverlight) > 0
	if !worn && !held {
		if demonOnce(s, KitDemonKeys) {
			return demonProvision(s, KitDemonKeys, demonKeysKit(), demonKeep(), false, "Silverlight, if the trainer banked it")
		}
		return Step{Kind: StepTalk, Stop: PrysinLostSwordStop, UntilItemID: ObjSilverlight}
	}
	if demonOnce(s, KitDemonFoodCircle) {
		return demonProvision(s, KitDemonFoodCircle, demonFoodKit(), demonKeep(), false, "food for Delrith and the dark wizards, if the bank or the mule has it")
	}
	if ok, why := DemonFit(s); !ok {
		return Step{Kind: StepWait, ParkFor: DemonFitRetry, Reason: why}
	}
	if s.MaxHP > 0 && s.HP > 0 && s.HP*10 < s.MaxHP*7 && demonFoodHeld(s) == 0 {
		return Step{Kind: StepWait, ParkFor: DemonLowHPRetry, Reason: fmt.Sprintf("%d of %d hitpoints and no food: not walking to the stone circle like this", s.HP, s.MaxHP)}
	}
	if !worn {
		if s.Failed[CustomDemonWield] {
			return Step{Kind: StepWait, ParkFor: DemonWieldRetry, Reason: "Silverlight would not go on"}
		}
		return Step{Kind: StepCustom, Custom: CustomDemonWield, ItemID: ObjSilverlight, Reason: "wield Silverlight, the only weapon Delrith can be hurt with"}
	}
	if s.Failed[CustomDemonDelrith] {
		return Step{Kind: StepWait, ParkFor: DemonDelrithRetry, Reason: "the visit to the stone circle did not end with the quest (no Delrith to fight, or the account turned back)"}
	}
	return Step{Kind: StepCustom, Custom: CustomDemonDelrith, Reason: "Delrith at the stone circle with Silverlight worn, and the incantation when he is weakened"}
}

// DemonSlayerRecord is the quest's identity. The server gates nothing; the hitpoints are the fleet's own floor for the circle and the
// sewer (DemonMinHitpoints), and the combat level (DemonMinCombat) the decide function's own gate, which a Record cannot state.
var DemonSlayerRecord = Record{ID: DemonSlayerID, Name: DemonSlayerName, QuestPoints: 3,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "hitpoints", Level: DemonMinHitpoints}}}}

// demonSustain is the quest's survival policy: eat at 15 hitpoints (the fight loops raise it for the foe).
var demonSustain = Sustain{FoodIDs: DemonFoods, EatAt: 15}

// DemonSlayer is the engine module. The account never fights back (NoRetaliate): the only blows it throws are Delrith's and the
// cows' (each fight re-sends its own attack, so none needs auto-retaliate). Two deaths park it for a day, and one at the circle or in the
// sewer does too.
var DemonSlayer = Module{
	Record: DemonSlayerRecord, Bank: VarrockWestBank, BankSet: false, Anchor: PrysinAnchor,
	ToolIDs:     demonTools,
	ReadStage:   ParseDemonSlayerJournal,
	Sustain:     demonSustain,
	NoRetaliate: true,
	MaxDeaths:   2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomDemonDelrith: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
		CustomDemonDrain:   {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
		CustomDemonSewer:   {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	Decide: DecideDemonSlayer,
}
