package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Shades of Mort'ton (content id `mortton`, %morttonquest, not transmitted, 3 quest points), ported from the TS def (defs/mortton/*.ts) and checked
// against content 289: quests/quest_mortton/scripts/{quest_mortton,razmire_keelgan,ulsquire_shauncy,serum_book,mortton_journal}.rs2 and its configs,
// minigames/game_mortton/scripts/{flamtaer_temple,mortton_shades,mortton_pyre}.rs2 and its configs (the shades, the temple's walls, the pyres), skill_herblore's
// serum brews and skill_fletching's pyre logs, and the maps m54_51 and m54_52. The reward is 2,000 Crafting and 2,000 Herblore experience and the quest
// points; the content asks Herblore 15 (the serum), Crafting 20 (the temple) and Firemaking 5 (the pyre).
//
// THE QUEST, stage by stage (%morttonquest, quest_mortton.constant; the journal tells every one of them apart):
//
//	0   Mort'ton lies at the south end of Mort Myre (the swamp's ghasts and its decay, the gate needs Nature Spirit started). Herbi Flax's diary is on the shelf
//	    of the experiment house (3481,3279) and read to its last page (5: the quest tab turns yellow).
//	5   The serum: the smashed table gives, once per account, two unidentified tarromin and a rogue's purse (Search, the table's op 5). A tarromin is identified
//	    (Herblore 11), put in a vial of water (a vial from the two floor spawns beside the shelf, filled at the sink, Herblore 12) and ashes added (Herblore 15):
//	    Serum 207, three doses (10).
//	10  A villager is "afflicted": his ops are gone and his talk is gibberish until a dose of serum is used on him (his type changes for 200 ticks and the visible
//	    bit is set for the same time); Razmire Keelgan, in the shop west of the church, is talked to with "What are all these shadow creatures?" and "Yes, I'll
//	    dispatch those dark and evil creatures." (15).
//	15  Five Loar shades are killed (a Loar Shadow turns into a Loar Shade when it hunts the account): each kill while the stage is 15 to 39 moves it on by 5, and
//	    each drops a set of Loar remains (shade_bones1) the account must pick up (40 at the fifth).
//	40  Razmire (cured again) takes two sets of remains when the pack holds five, and opens his two stores (45): the builders' (planks, limestone bricks, swamp paste)
//	    and the general (a hammer, olive oil). Ulsquire Shauncy, in the church, is shown a set (47) and asked "What can you tell me about that temple?" (50).
//	50  The Flamtaer temple north of the town (3504..3508, 3314..3318) is rebuilt: the hammer on 15 walls (11 straight, 4 corners) at Crafting 20, each action spending
//	    material from a pool (a set of one plank, one brick and five paste adds 800) and raising the build counter, which lets the walls climb their ten levels (3900 for
//	    the last), and the sanctity (55, then 60 when all fifteen are at level ten and the fire altar appears). The shades knock the walls down (20 off the counter
//	    for each blow) and hunt the account at the temple. The varps 343 (repaired), 344 (pool) and 345 (sanctity), all in percent, are transmitted.
//	60  The altar is lit with a tinderbox (Firemaking, a roll: 100/256 at level 1 on each try, sanctity 10 percent or more); olive oil used on the lit altar becomes
//	    sacred oil of the same doses and costs sanctity (65).
//	65  Sacred oil used on logs makes pyre logs (two doses for ordinary logs; 70).
//	70  Pyre logs used on an empty funeral pyre (Firemaking 5 for ordinary logs: 75). A set of remains used on the loaded pyre, and the pyre lit (the roll is
//	    stat_random(firemaking, 64, 512)): the stage is 80. The pyre reverts in 49 ticks at each of its stages, so the three are done back to back.
//	80  Ulsquire is talked to again (85, the quest's end: 2,000 Crafting and 2,000 Herblore experience).
//
// HARD PARTS. (1) THE WAY IN. The route from Varrock runs through Canifis, the Mort Myre gate and the swamp (the web graph's own: cost 672 from the east bank):
// 93 ghasts (level 25, invisible, rotting food), a swamp decay of 1 to 3 hitpoints every 200 ticks, and the gate refusing an account that has not started Nature
// Spirit; so the quest needs Nature Spirit, the pack is stocked and its ashes burnt BEFORE the crossing (nothing inside sells logs, vials or ashes, and the only
// bank is a long walk back), and the account is priced against two ghasts and given a floor of food. (2) THE SHADES. 25 Loar Shadows (level 40, 38 hitpoints, hits of
// up to 4) stand in the streets and hunt any account of combat 79 or less within three tiles; the ones within ten tiles of a fight join it, so a hunt is priced against
// two at once (FoeLoarShadePair) and so is the temple's building, where they come for the account as well as the walls. Nothing is fought but them. (3) THE SERUM:
// villagers are talked to only within the 200 ticks a dose gives, so every conversation opens by looking at the villager's own name (cured or afflicted) and spends a dose
// only when he has gone back; the doses a run needs are counted from the stage (MtDosesNeeded) and the two serums the table's two tarromin allow give six. (4) THE
// FIREMAKING. The pyre wants Firemaking 5 (388 experience), the fleet's accounts are at 1, and the quest's own fires (the ashes, the altar, the pyre logs) give 260 at
// most: logs enough to burn the rest are carried (MtFiremakingLogs) and burnt, at the pyre if the level is still short then. (5) THE WINDOWS: the pyre's three stages and
// the altar's armed fire last 49 and 99 ticks, so the cremation is one executor that does all three back to back. (6) THE TEMPLE'S POOL: a purchase buys a set of 800
// points, an action spends 7 to 9 at Crafting 20 (35 to 70 at higher levels), and the shell needs on the order of 1,400 points before the shades' damage: five sets are
// bought (as many as the pack has room for) and more when the temple says its pool is dry.

// MortonName is the quest tab's spelling (component 8969, `questlist:mortton`), the journal's own title spelling the name with an apostrophe, and MorttonID the engine's id.
const (
	MorttonName = "Shades of Mortton"
	MorttonID   = "mortton"
)

// The stages the journal tells apart (ParseMorttonJournal), named after the constants of quest_mortton.constant they stand for.
const (
	MtNotStarted       = 0
	MtReadDiary        = 5
	MtMadeSerum        = 10
	MtKillShades       = 15
	MtKilled1          = 20
	MtKilled2          = 25
	MtKilled3          = 30
	MtKilled4          = 35
	MtKilled5          = 40
	MtShadesToRazmire  = 45
	MtShadesToUlsquire = 47
	MtUlsquireTemple   = 50
	MtRebuildTemple    = 55
	MtCanLightAltar    = 60
	MtSacredOil        = 65
	MtPyreLogs         = 70
	MtLogsOnPyre       = 75
	MtLitPyre          = 80
	MtComplete         = 85
)

// The skills the quest asks for: Herblore 15 for the serum (skill_herblore's mort_serum3 structs) and Crafting 20 for the temple (flamtaer_temple.rs2: "You need a crafting
// skill level of at least 20 to repair the temple"). MtFiremakingPyre is the level an ordinary log's pyre wants (flamtaer_pyre.struct: pyre_level 5).
const (
	MtHerblore       = 15
	MtCrafting       = 20
	MtFiremakingPyre = 5
)

// Object ids from content/pack/obj.pack (checked against the production config by TestMorttonIDsMatchTheProductionConfig in questmods).
const (
	ObjMtDiary        = 3395 // [serum_book], "Diary"
	ObjMtUnidTarromin = 203  // [unidentified_tarromin]
	ObjMtTarromin     = 253  // [tarromin]
	ObjMtUnfTarromin  = 95   // [tarrominvial]: the unfinished potion
	ObjMtVialWater    = 227  // [vial_water]
	ObjMtVial         = 229  // [vial_empty]
	ObjMtSerum4       = 3408 // [mort_serum4] .. [mort_serum1]
	ObjMtSerum3       = 3410
	ObjMtSerum2       = 3412
	ObjMtSerum1       = 3414
	ObjMtPerm4        = 3416 // [mort_serum_perm4] .. [mort_serum_perm1]
	ObjMtPerm3        = 3417
	ObjMtPerm2        = 3418
	ObjMtPerm1        = 3419
	ObjMtRemains      = 3396 // [shade_bones1], "Loar remains"
	ObjMtBrick        = 3420 // [limestonebrick]
	ObjMtPlank        = 960  // [woodplank]
	ObjMtPaste        = 1941 // [swamppaste], stackable
	ObjMtOil4         = 3422 // [oliveoil4] .. [oliveoil1]
	ObjMtOil3         = 3424
	ObjMtOil2         = 3426
	ObjMtOil1         = 3428
	ObjMtSacred4      = 3430 // [sacred_oil4] .. [sacred_oil1]
	ObjMtSacred3      = 3432
	ObjMtSacred2      = 3434
	ObjMtSacred1      = 3436
	ObjMtPyreLogs     = 3438 // [logs_pyre]
	ObjMtRoguesPurse  = 1533 // [unidentified_rogues_purse]: the table's third herb, of no use here
	ObjMtRottenFood   = 2959 // what a ghast leaves where a lobster was
)

// Npc ids from content/pack/npc.pack.
const (
	NpcMtRazmire          = 1254 // [razmire_keelgan]: ops Talk, Trade-General-Store, Trade-Builders-Store
	NpcMtRazmireAfflicted = 1253 // [razmire_keelgan_afflicted]: Afflicted(Razmire), Talk only
	NpcMtUlsquire         = 1252 // [ulsquire_shauncy]
	NpcMtUlsquireAfflict  = 1251 // [ulsquire_shauncy_afflicted]
	NpcMtShadow           = 1240 // [shadeshadow_level1], "Loar Shadow"
	NpcMtShade            = 1241 // [shade_level1], "Loar Shade"
)

// Loc ids from content/pack/loc.pack and the baked index.
const (
	LocMtShelf        = 4062 // [shades_experimentshelf]: Search
	LocMtSink         = 4063 // [mortton_sink]: a use of an empty vial
	LocMtTable        = 4064 // [shades_experimenttable]: Search (op 5)
	LocMtWallBase     = 4068 // [templewall_base] .. [templewall_9]: Repair; 4078 [templewall_10]: Reinforce
	LocMtWallDone     = 4078
	LocMtCornerBase   = 4079 // [templewallcorner_base] .. [templewallcorner_9]: Repair; 4089 [templewallcorner_10]: Reinforce
	LocMtCornerDone   = 4089
	LocMtAltarLit     = 4090 // [templefire_altar], "Flaming Fire altar"
	LocMtAltarUnlit   = 4091 // [templefire_altar_nofire], "Fire altar": Light
	LocMtAltarBroken  = 4092 // [templefire_altar_nofire_broken]
	LocMtPyreEmpty    = 4093 // [temple_pyre]
	LocMtPyreLogs     = 4094 // [temple_pyre_logs]
	LocMtPyreBones    = 4100 // [temple_pyre_bones_logs]: Light
	LocMtTempleWallOp = "Repair"
)

// The transmitted varps of the temple, in percent (quest_mortton.varp: temple_repaired_p, temple_resources_p and temple_sanctity_p, transmit=yes).
const (
	VarpMtRepaired  = 343
	VarpMtResources = 344
	VarpMtSanctity  = 345
)

// Tiles from the content maps, the loc index and the collision pack.
var (
	// MtShelf is the experiment house's shelf (loc 4062), MtTable its smashed table (loc 4064) and MtVials the floor spawns of the two empty vials.
	MtShelf = tile.Tile{X: 3481, Z: 3279}
	MtTable = tile.Tile{X: 3482, Z: 3278}
	MtVials = tile.Tile{X: 3481, Z: 3278}
	// MtSink is the sink (loc 4063, turned: 2 by 1 at 3495,3288) and MtSinkNear a tile the walker can finish on, one of the two its stands are (the collision pack's).
	MtSink     = tile.Tile{X: 3495, Z: 3288}
	MtSinkNear = tile.Tile{X: 3495, Z: 3289}
	// MtRazmire is Razmire's spawn (m54_51 "0 33 32: 1253" is the afflicted form: 3489,3296), MtUlsquire Ulsquire's (3496,3289), MtTown the middle of the town.
	MtRazmire  = tile.Tile{X: 3489, Z: 3296}
	MtUlsquire = tile.Tile{X: 3496, Z: 3289}
	MtTown     = tile.Tile{X: 3490, Z: 3290}
	// MtTempleDoor is the one gap in the temple's shell (the south face), MtTempleInside a tile inside it and MtAltar the fire altar (loc 4090 to 4092).
	MtTempleDoor   = tile.Tile{X: 3506, Z: 3314}
	MtTempleInside = tile.Tile{X: 3506, Z: 3315}
	MtAltar        = tile.Tile{X: 3506, Z: 3316}
	// MtShadeField is the middle of the ground the shades spawn on (the 25 spawns of the maps lie within 27 tiles of it, MtShadeFieldRadius and the hunt's stray), a walkable tile
	// of the streets east of the church.
	MtShadeField = tile.Tile{X: 3494, Z: 3294}
	// MtPyre is a funeral pyre site (loc 4093 at 3507,3276, 1 by 3) near the town and the temple (two dozen of them lie within 40 tiles of the town's middle).
	MtPyre = tile.Tile{X: 3507, Z: 3276}
	// MtVarrockLogs is the two log spawns by Varrock's east bank (3243,3395 and 3243,3397).
	MtVarrockLogs = tile.Tile{X: 3243, Z: 3396}
)

// MtShadeFieldRadius is how far from MtShadeField the hunt looks for a shade (it strays five tiles beyond).
const MtShadeFieldRadius = 22

// MtTown's box is the map square the town and the temple are in (everything the quest does inside it happens at x 3456..3519, z 3264..3327), MtSwamp's the swamp
// round it that the way in crosses: Mort Myre (the swamp decay's own zone, x 3392..3519, z 3328..3455) and its strip down to the town (x 3392..3455, z 3256..3327).
var (
	mtTownBox  = Rect{MinX: 3456, MaxX: 3519, MinZ: 3264, MaxZ: 3327}
	mtSwampBox = []Rect{{MinX: 3392, MaxX: 3519, MinZ: 3328, MaxZ: 3455}, {MinX: 3392, MaxX: 3455, MinZ: 3256, MaxZ: 3327}}
)

// InMortton reports a tile of the town's map square.
func InMortton(t tile.Tile) bool { return mtTownBox.Contains(t) }

// InMorttonSwamp reports a tile of the swamp the way in crosses (not the town's own square): an account there is committed to the crossing, and no step turns it back
// for a shop or a bank.
func InMorttonSwamp(t tile.Tile) bool {
	for _, r := range mtSwampBox {
		if r.Contains(t) {
			return true
		}
	}
	return false
}

// MtInTempleZone is the rectangle the overlay and the meters follow (mortton_temple_zones: 3492..3518, 3308..3323): the three varps are stale outside it.
func MtInTempleZone(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 3492 && t.X <= 3518 && t.Z >= 3308 && t.Z <= 3323
}

// MtIsTempleWall reports a loc id of the temple's shell at any repair level.
func MtIsTempleWall(id int) bool {
	return (id >= LocMtWallBase && id <= LocMtWallDone) || (id >= LocMtCornerBase && id <= LocMtCornerDone)
}

// MtWallRepairable reports a wall piece below its tenth level (Repair); the tenth (4078 and 4089) offers Reinforce.
func MtWallRepairable(id int) bool {
	return (id >= LocMtWallBase && id < LocMtWallDone) || (id >= LocMtCornerBase && id < LocMtCornerDone)
}

// The counts. A quest's worth of the temple's material is MtBuildSets sets (one plank, one brick and MtPastePerSet paste each; five paste are what a set takes);
// MtRemainsWanted is what Razmire asks for in one pack, MtSpareRemains what is kept past Ulsquire's study (one for the pyre and one against a failed cremation).
const (
	MtBuildSets      = 5
	MtPastePerSet    = 6
	MtRemainsWanted  = 5
	MtSpareRemains   = 2
	MtCoins          = 5000
	MtCoinsMin       = 1500
	MtCoinsLow       = 300
	MtFoodAsk        = 8
	MtMinBitesCross  = 4
	MtMinHPCross     = 25
	MtDosesPerVial   = 3
	MtPyreSacredDose = 2
	MtMaxSerums      = 2
)

// The park times: MtSkillParkFor is how long the quest skips an account under one of its levels, MtFightParkFor one the model says loses to the shades or to the swamp's
// ghasts, MtItemParkFor one whose bank, mule and shops cannot give what the kit asks, MtStuckParkFor a leg that could not be done, MtPyreParkFor one whose pyre went out
// before it was lit with no oil left to make another log.
const (
	MtSkillParkFor = 6 * time.Hour
	MtFightParkFor = 6 * time.Hour
	MtItemParkFor  = 3 * time.Hour
	MtStuckParkFor = 3 * time.Hour
	MtPyreParkFor  = 3 * time.Hour
)

// FoeLoarShade is mortton_shades.npc [shade_level1] and [shadeshadow_level1] (npcs 1241 and 1240): level 40, Attack 45, Strength 30, Defence 26, 38 hitpoints, no attackrate
// (4), no bonus params (a hit of up to 4), crush, huntmode shades (any account of combat 79 or less within three tiles with the hunt's own conditions; the shade resets after
// 8 ticks without a hit).
var FoeLoarShade = MeleeFoe{Name: "Loar Shade", Attack: 45, Strength: 30, Defence: 26, Magic: 0, Hitpoints: 38, AttackTicks: 4}

// FoeLoarShadePair stands for the two shades that fight an account at once (a shade's hit wakes every shade within ten tiles: [ai_opplayer2,_shade]): the hitpoints of
// two and the blows of two, which charges the first half of the fight at twice the rate and the second half at the single rate's, so the fight is priced a quarter
// dearer than it goes.
var FoeLoarShadePair = MeleeFoe{Name: "two Loar Shades", Attack: 45, Strength: 30, Defence: 26, Magic: 0, Hitpoints: 76, AttackTicks: 2}

// mtFoodIDs are the foods the quest eats (Module.Sustain; the eat loop's floor bite takes any food the pack carries).
var mtFoodIDs = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}

// mtSustain is the quest's survival policy: the foods it carries and the floor its eat line never goes under (a shade's hardest hit is 4).
var mtSustain = Sustain{FoodIDs: mtFoodIDs, EatAt: 14}

// mtDoseTable are the serum 207 vials by object id and the doses each holds, and mtPermTable the sanctified ones'.
var (
	mtDoseTable = []struct{ ID, Doses int }{{ObjMtSerum4, 4}, {ObjMtSerum3, 3}, {ObjMtSerum2, 2}, {ObjMtSerum1, 1}}
	mtPermTable = []struct{ ID, Doses int }{{ObjMtPerm4, 4}, {ObjMtPerm3, 3}, {ObjMtPerm2, 2}, {ObjMtPerm1, 1}}
	mtOilTable  = []struct{ ID, Doses int }{{ObjMtOil4, 4}, {ObjMtOil3, 3}, {ObjMtOil2, 2}, {ObjMtOil1, 1}}
	mtSacredTab = []struct{ ID, Doses int }{{ObjMtSacred4, 4}, {ObjMtSacred3, 3}, {ObjMtSacred2, 2}, {ObjMtSacred1, 1}}
)

// MtSerumIDs, MtPermSerumIDs, MtOilIDs and MtSacredIDs return the object ids of the four tables, strongest first.
func MtSerumIDs() []int     { return mtIDs(mtDoseTable) }
func MtPermSerumIDs() []int { return mtIDs(mtPermTable) }
func MtOilIDs() []int       { return mtIDs(mtOilTable) }
func MtSacredIDs() []int    { return mtIDs(mtSacredTab) }

func mtIDs(t []struct{ ID, Doses int }) []int {
	out := make([]int, len(t))
	for i, r := range t {
		out[i] = r.ID
	}
	return out
}

func mtDoses(s Snapshot, t []struct{ ID, Doses int }) int {
	n := 0
	for _, r := range t {
		n += s.HeldID(r.ID) * r.Doses
	}
	return n
}

// MtDoses is the doses of serum 207 and 207(p) in the pack, MtSacredDoses the sacred oil's and MtOliveDoses the olive oil's.
func MtDoses(s Snapshot) int       { return mtDoses(s, mtDoseTable) + mtDoses(s, mtPermTable) }
func MtSacredDoses(s Snapshot) int { return mtDoses(s, mtSacredTab) }
func MtOliveDoses(s Snapshot) int  { return mtDoses(s, mtOilTable) }

// MtDosesNeeded is the doses of serum the rest of the quest spends from this stage on: a conversation with an afflicted villager is open only within 200 ticks of a dose, and each
// of Razmire's (the send-off, the remains), Ulsquire's (the study and the temple in one window) and Ulsquire's last is one window.
func MtDosesNeeded(stage int) int {
	doses := 1 // Ulsquire, once the pyre is lit
	if stage < MtKillShades {
		doses++ // Razmire, to be sent after the shades
	}
	if stage < MtShadesToRazmire {
		doses++ // Razmire, to take the remains
	}
	if stage < MtUlsquireTemple {
		doses++ // Ulsquire, who studies the remains and then names the temple
	}
	return doses
}

// MtSerumsShort is the vials of serum still to brew: the doses the stage still spends less those held, at three to a vial, never more than the table's two tarromin allow.
func MtSerumsShort(s Snapshot, stage int) int {
	short := (MtDosesNeeded(stage) - MtDoses(s) + MtDosesPerVial - 1) / MtDosesPerVial
	return max(0, min(short, MtMaxSerums))
}

// MtAshesShort is the ashes still to be burnt or found: one for each serum to brew that has no unfinished potion made for it yet.
func MtAshesShort(s Snapshot, stage int) int {
	return max(0, MtSerumsShort(s, stage)-s.HeldID(ObjAshes))
}

// MtFiremakingBurns is the logs to burn for experience before the pyre can be laid: the quest's own fires give 40 for each ashes burnt, 160 at the altar and 20 for the
// pyre log, and level 5 is 388 (levels 2, 3 and 4: 83, 174, 276); the worst case at a level is its first experience. Burns beyond the ashes' are asked for with a spare.
func MtFiremakingBurns(level int) int {
	floor := map[int]int{1: 0, 2: 83, 3: 174, 4: 276}
	have, ok := floor[level]
	if !ok {
		return 0 // level 5 and above: the pyre is laid; an unread level is not judged
	}
	need := 388 - have - 180 // the altar and the pyre log
	if need <= 0 {
		return 0
	}
	return (need + 39) / 40
}

// MtLogsWanted is the logs the pack is brought to before the crossing: one to be burnt for each ashes still short, the pyre's own log (until the pyre logs exist), and the
// experience's burns (MtFiremakingBurns, less the ashes' own, which are burns too).
func MtLogsWanted(s Snapshot, stage int) (lo, hi int) {
	ashes := MtSerumsShort(s, stage)
	pyre := 0
	if stage < MtPyreLogs && s.HeldID(ObjMtPyreLogs) == 0 {
		pyre = 1
	}
	burns := MtFiremakingBurns(s.Level("firemaking"))
	extra := 0
	if burns > ashes && s.Level("firemaking") > 0 {
		extra = burns - ashes + 1 // a spare log for a failed light
	}
	lo = ashes + pyre
	return lo, lo + extra
}

// ObjMtLogs is the ordinary logs' id the quest burns and makes pyre logs of.
const ObjMtLogs = ObjLogs

// MtSkillShort says which of the quest's two skills is under its level (levels the snapshot has not read are not judged), "" for none.
func MtSkillShort(s Snapshot) string {
	var short []string
	for _, c := range []struct {
		skill string
		level int
	}{{"herblore", MtHerblore}, {"crafting", MtCrafting}} {
		if lvl := s.Level(c.skill); lvl > 0 && lvl < c.level {
			short = append(short, fmt.Sprintf("%s %d/%d", skillLabel(c.skill), lvl, c.level))
		}
	}
	return strings.Join(short, ", ")
}

// MtFightVerdict is MtShadesReady's and MtCrossingReady's answer.
type MtFightVerdict struct {
	OK  bool
	Why string
}

// MtShadesReady judges whether the pack can take the shades: two at once (FoeLoarShadePair) by the account's own swing, from the level a fight starts at, eating by the foe's eat
// line; it must win with a tenth of its bites left and never fall under the shades' hardest hit.
func MtShadesReady(s Snapshot) MtFightVerdict {
	bites := len(FoodBites(s.InvIDs, mtFoodIDs))
	if bites == 0 {
		return MtFightVerdict{Why: fmt.Sprintf("no food in the pack for the Loar shades (%d hitpoints each, hits up to %d)", FoeLoarShade.Hitpoints, FoeLoarShade.MaxHit())}
	}
	odds := MeleeChain(s, []MeleeFoe{FoeLoarShadePair}, mtSustain)
	if odds.OK {
		return MtFightVerdict{OK: true}
	}
	return MtFightVerdict{Why: fmt.Sprintf("%s (two Loar shades at once: Attack %d, Strength %d, Defence %d, %d/%d hitpoints, %d bites of food)",
		odds.Why, s.Level("attack"), s.Level("strength"), s.Level("defence"), s.HP, s.MaxHP, bites)}
}

// MtStayMinBites is the bites of food under which a stay at the temple's walls or at its altar ends: the shades come back thirty-five ticks after they fall, and a stay that
// has eaten down to its last bite or two has nothing left to answer the next pair with.
const MtStayMinBites = 2

// MtStayVerdict is MtShadesReady asked in the middle of a stay, of the pack in hand now: the temple is priced once at the stage's entry, but the shades respawn and Razmire's
// timer pulls every shade within sixty tiles to the temple while it is being built, so a stay of a quarter of an hour is a run of fights and the single price at its start is no
// floor for the end of it. It says the stay must end (OK false, with the reason) when the pack holds fewer than MtStayMinBites bites, or when it could no longer take two shades at
// once from the hitpoints it has (topping up from the bites it holds first, as the fights do). The decision after the step then lets MtShadesReady say whether the account may go back.
func MtStayVerdict(s Snapshot) MtFightVerdict {
	bites := len(FoodBites(s.InvIDs, mtFoodIDs))
	if bites < MtStayMinBites {
		return MtFightVerdict{Why: fmt.Sprintf("%d bite(s) of food left for the Loar shades (the stay's floor is %d; %d/%d hitpoints)", bites, MtStayMinBites, s.HP, s.MaxHP)}
	}
	return MtShadesReady(s)
}

// MtCrossingReady judges the way in: the swamp's ghasts, priced as two of them one after the other, the hitpoints (a floor) and the bites of food the walk takes.
func MtCrossingReady(s Snapshot) MtFightVerdict {
	bites := len(FoodBites(s.InvIDs, mtFoodIDs))
	if s.MaxHP > 0 && s.MaxHP < MtMinHPCross {
		return MtFightVerdict{Why: fmt.Sprintf("%d hitpoints for the Mort Myre crossing (the floor is %d)", s.MaxHP, MtMinHPCross)}
	}
	if bites < MtMinBitesCross {
		return MtFightVerdict{Why: fmt.Sprintf("%d bites of food for the Mort Myre crossing (ghasts rot some; the floor is %d)", bites, MtMinBitesCross)}
	}
	odds := MeleeChain(s, []MeleeFoe{FoeGhast, FoeGhast}, mtSustain)
	if odds.OK {
		return MtFightVerdict{OK: true}
	}
	return MtFightVerdict{Why: fmt.Sprintf("%s (two ghasts of the swamp: Attack %d, Strength %d, Defence %d, %d/%d hitpoints, %d bites of food)",
		odds.Why, s.Level("attack"), s.Level("strength"), s.Level("defence"), s.HP, s.MaxHP, bites)}
}

// mtToolIDs are the quest's own items, what the floor bite never eats and every bank step of the quest leaves in the pack.
var mtToolIDs = func() []int {
	tools := []int{ObjMtDiary, ObjMtUnidTarromin, ObjMtTarromin, ObjMtUnfTarromin, ObjMtVialWater, ObjMtVial, ObjMtRemains, ObjMtBrick, ObjMtPlank, ObjMtPaste, ObjMtPyreLogs,
		ObjMtLogs, ObjTinderbox, ObjHammer, ObjAshes, CoinsID}
	for _, t := range [][]struct{ ID, Doses int }{mtDoseTable, mtPermTable, mtOilTable, mtSacredTab} {
		tools = append(tools, mtIDs(t)...)
	}
	return tools
}()

// mtKeep is what every bank step of the quest leaves in the pack: the tools and the foods.
var mtKeep = append(append([]int(nil), mtToolIDs...), mtFoodIDs...)

// MtKeepIDs returns the keep list (a copy); MtToolIDs the module's tools (a copy).
func MtKeepIDs() []int { return append([]int(nil), mtKeep...) }
func MtToolIDs() []int { return append([]int(nil), mtToolIDs...) }

// Custom step names (internal/scripts/questmods).
const (
	CustomMtBurn      = "mt-burn"        // logs burnt for ashes and Firemaking experience
	CustomMtTakeDiary = "mt-take-diary"  // the diary off the shelf
	CustomMtReadDiary = "mt-read-diary"  // the diary read to its last page
	CustomMtHerbs     = "mt-herbs"       // the smashed table searched
	CustomMtIdentify  = "mt-identify"    // a tarromin identified
	CustomMtVial      = "mt-vial"        // an empty vial off the floor
	CustomMtWater     = "mt-water"       // a vial filled at the sink
	CustomMtMixUnf    = "mt-mix-unf"     // tarromin into the vial of water
	CustomMtMixSerum  = "mt-mix-serum"   // ashes into the unfinished potion: serum 207
	CustomMtDropJunk  = "mt-drop-junk"   // the diary, the rogue's purse and rotten food dropped
	CustomMtSend      = "mt-send"        // Razmire asks for five shades
	CustomMtHunt      = "mt-hunt"        // one shade killed and its remains taken
	CustomMtHandOver  = "mt-hand-over"   // Razmire takes two remains and the builders' store is bought from
	CustomMtGeneral   = "mt-general"     // the general store: a hammer, olive oil
	CustomMtBuilders  = "mt-builders"    // the builders' store: planks, bricks, paste
	CustomMtShow      = "mt-show"        // Ulsquire shown the remains
	CustomMtTempleTlk = "mt-temple-talk" // Ulsquire asked about the temple
	CustomMtTemple    = "mt-temple"      // the temple rebuilt
	CustomMtAltar     = "mt-altar"       // the altar lit and the oil sanctified
	CustomMtPyreLogs  = "mt-pyre-logs"   // sacred oil on logs
	CustomMtCremate   = "mt-cremate"     // the pyre stacked, the remains laid, the pyre lit
	CustomMtFinish    = "mt-finish"      // Ulsquire told
)

// KitMorttonOut is the provisioning round before the crossing; KitMorttonIn the one a quest in the middle asks for.
const KitMorttonOut = "mt-kit"

// The menu lines the executors choose, each the start of a line of the scripts' menus (TestMorttonPicksAreMenuLines checks them against razmire_keelgan.rs2 and ulsquire_shauncy.rs2): Razmire's
// send-off (the question, then the promise), the stores (his menu after the remains, and the lines for the builders' and the general store that every later talk reaches) and
// Ulsquire's temple.
var (
	MtSendPicks     = []string{"What are all these shadow creatures", "Yes, I'll dispatch"}
	MtHandOverPicks = []string{"Can I see the building store", "Can you open a store for me"}
	MtBuildersPicks = []string{"I keep running out of building materials", "Can I see the building store", "Can you open a store for me"}
	MtGeneralPicks  = []string{"Can I see the general store", "Can you open a store for me"}
	MtTemplePicks   = []string{"What can you tell me about that temple", "Ok thanks"}
)

// The stops: Razmire's and Ulsquire's cured faces (the afflicted ones are looked for by the executors, which spend the serum).
var (
	MtRazmireStop  = NpcStop{Npc: "Razmire Keelgan", ID: NpcMtRazmire, Anchor: MtRazmire, Leash: 8, Prefer: []string{"Ok, thanks"}}
	MtUlsquireStop = NpcStop{Npc: "Ulsquire Shauncy", ID: NpcMtUlsquire, Anchor: MtUlsquire, Leash: 8, Prefer: []string{"Ok, thanks"}}
)

// mtMarkers are the journal's markers, newest stage first: mortton_journal.rs2 keeps every earlier paragraph on the page, so the newest one decides.
var mtMarkers = []struct {
	stage int
	text  string
}{
	{MtComplete, "quest complete!"},
	{MtLitPyre, "the shades spirit was released"},
	{MtLogsOnPyre, "i placed some pyre logs onto a funeral pyre"},
	{MtPyreLogs, "i used sacred oil on logs turning them into pyre logs"},
	{MtSacredOil, "i placed a flask of olive oil in the flame"},
	{MtCanLightAltar, "i helped rebuild the temple using limestone bricks"},
	{MtRebuildTemple, "i've started rebuilding the temple"},
	{MtUlsquireTemple, "he's quite interested in 'flaemtaer temple'"},
	{MtShadesToUlsquire, "maybe if i investigate around here a little more"},
	{MtShadesToRazmire, "i should show the shade remains to"},
	{MtKilled5, "i've killed all five shades"},
	{MtKilled4, "i've killed four shades"},
	{MtKilled3, "i've killed three shades"},
	{MtKilled2, "i've killed two shades"},
	{MtKilled1, "i've killed one shade"},
	{MtKillShades, "i have to kill five shades"},
	{MtMadeSerum, "i used the serum on a local npc"},
	{MtMadeSerum, "i made serum 207 from tarromin"},
	// From stage 10 on the page says "and found the town", so the pronoun is what separates the two.
	{MtReadDiary, "and i found the town of"},
	{MtNotStarted, "i can start this quest by exploring the town of"},
}

// ParseMorttonJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised page, which is unknown, never stage 0.
func ParseMorttonJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range mtMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	return 0, false
}

func mtCustom(name, reason string) Step { return Step{Kind: StepCustom, Custom: name, Reason: reason} }

func mtPark(d time.Duration, reason string) Step {
	return Step{Kind: StepWait, ParkFor: d, Reason: reason}
}

func mtNames(lines []KitLine) string {
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		names = append(names, strings.ToLower(l.Name))
	}
	return strings.Join(names, ", ")
}

// MtKitLines is the kit the account carries to Mort'ton (and back to Varrock for when it runs short before the shades are met): coins for the shops (to the stage where the
// shopping is over), a tinderbox for the ashes, the altar and the pyre, the logs, and food. The coins, the tinderbox and the logs' least are required; the rest is taken
// when a source has it.
func MtKitLines(s Snapshot, stage int) []KitLine {
	var kit []KitLine
	coinsMin, coinsQty := MtCoinsMin, MtCoins
	if stage >= MtCanLightAltar {
		coinsMin, coinsQty = MtCoinsLow, MtCoinsLow*2
	}
	if stage < MtLitPyre && s.HeldID(CoinsID) < coinsMin {
		kit = append(kit, KitLine{ID: CoinsID, Name: "Coins", Qty: coinsQty, Min: coinsMin, Stackable: true})
	}
	if s.HeldID(ObjTinderbox) == 0 && stage < MtLitPyre {
		kit = append(kit, KitLine{ID: ObjTinderbox, Name: "Tinderbox", Qty: 1, Sources: digShop(VarrockGenStore, 2, "Varrock's general store keeps 2")})
	}
	if lo, hi := MtLogsWanted(s, stage); hi > 0 && s.HeldID(ObjMtLogs) < lo {
		kit = append(kit, KitLine{ID: ObjMtLogs, Name: "Logs", Qty: hi, Min: lo, Sources: []Source{{Kind: SourceGround, Tile: MtVarrockLogs, PerSpawn: 1, RespawnTicks: 100,
			Note: "the two spawns by Varrock's east bank"}}})
	}
	if a := MtAshesShort(s, stage); a > 0 {
		kit = append(kit, KitLine{ID: ObjAshes, Name: "Ashes", Qty: a, Optional: true})
	}
	if len(FoodBites(s.InvIDs, mtFoodIDs)) < MtFoodAsk {
		kit = append(kit, KitLine{ID: ObjShark, Name: "Shark", Qty: MtFoodAsk, Optional: true})
	}
	return kit
}

func mtRequiredShort(s Snapshot, kit []KitLine) []KitLine {
	var short []KitLine
	for _, l := range kit {
		if !l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			short = append(short, l)
		}
	}
	return short
}

func mtOptionalShort(s Snapshot, kit []KitLine) bool {
	for _, l := range kit {
		if l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			return true
		}
	}
	return false
}

func mtProvision(kit []KitLine, reason string) Step {
	return Step{Kind: StepProvision, KitName: KitMorttonOut, Kit: kit, KeepIDs: mtKeep, Bank: VarrockEastBank, BankSet: true, Reason: reason}
}

// mtKitStep is the provisioning round the account wants before it crosses, if any: the required lines first (and a park once every source of one is dead), then the optional ones once a run.
func mtKitStep(s Snapshot, stage int) (Step, bool) {
	kit := MtKitLines(s, stage)
	if short := mtRequiredShort(s, kit); len(short) > 0 {
		if s.Blocked["provision:"+KitMorttonOut] {
			return mtPark(MtItemParkFor, "no "+mtNames(short)+" from the bank, the mule or the shops: Mort'ton cannot be done without"), true
		}
		return mtProvision(kit, "the crossing's kit ("+mtNames(short)+")"), true
	}
	if mtOptionalShort(s, kit) && !s.Ran["provision:"+KitMorttonOut] && !s.Failed["provision:"+KitMorttonOut] {
		return mtProvision(kit, "ashes and food, if the bank or the mule has them"), true
	}
	return Step{}, false
}

// MtBurnStep burns logs where the account stands: ashes of them (Radius) and Qty burns in all. A custom's own fields carry the two numbers.
func mtBurn(burns, ashes int, reason string) Step {
	return Step{Kind: StepCustom, Custom: CustomMtBurn, Qty: burns, Radius: ashes, Reason: reason}
}

// mtOutside is the decision before the crossing (and for an account that needs something only Varrock gives): the pack's fit for the swamp, the kit, the ashes burnt, then the walk.
func mtOutside(s Snapshot, stage int) Step {
	if v := MtCrossingReady(s); !v.OK {
		// Food first: the kit asks for it (optionally); the verdict is read on what the pack holds after.
		if st, ok := mtKitStep(s, stage); ok {
			return st
		}
		return mtPark(MtFightParkFor, "not crossing Mort Myre with this pack: "+v.Why)
	}
	if st, ok := mtKitStep(s, stage); ok {
		return st
	}
	if need := MtAshesShort(s, stage); need > 0 && s.HeldID(ObjTinderbox) > 0 {
		if s.Failed[CustomMtBurn] {
			return mtPark(MtStuckParkFor, "no logs could be burnt for the serum's ashes")
		}
		if s.HeldID(ObjMtLogs) >= need {
			return mtBurn(need, need, "burn logs for the serum's ashes")
		}
	}
	return Step{Kind: StepWalk, Anchor: MtTown, Radius: 4, Reason: "the way to Mort'ton"}
}

// DecideMortton is the whole quest, read from the journal's stage, the account's tile and the pack.
func DecideMortton(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not been read"}
	}
	if why := MtSkillShort(s); why != "" {
		return mtPark(MtSkillParkFor, "Shades of Mort'ton wants Herblore 15 and Crafting 20: "+why)
	}
	stage := MtNotStarted
	if s.Journal != StatusNotStarted {
		if !s.StageKnown {
			return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
		}
		stage = s.Stage
		if stage == MtNotStarted || stage >= MtComplete {
			return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about Shades of Mort'ton"}
		}
	}
	if InMorttonSwamp(s.Tile) {
		return Step{Kind: StepWalk, Anchor: MtTown, Radius: 4, Reason: "on through the swamp to Mort'ton"}
	}
	if !InMortton(s.Tile) {
		return mtOutside(s, stage)
	}
	return mtInside(s, stage)
}

// MtHasSerumWork says an unfinished serum is still owed: a tarromin or a potion in hand, or ashes and a vial to make one with.
func mtSerumLeg(s Snapshot, stage int) (Step, bool) {
	if stage < MtReadDiary || MtSerumsShort(s, stage) == 0 {
		return Step{}, false
	}
	fail := func(name, why string) (Step, bool) {
		if s.Failed[name] {
			return mtPark(MtStuckParkFor, why), true
		}
		return Step{}, false
	}
	switch {
	case s.HeldID(ObjMtUnfTarromin) > 0:
		if s.HeldID(ObjAshes) > 0 {
			if st, ok := fail(CustomMtMixSerum, "the ashes would not go into the unfinished potion"); ok {
				return st, true
			}
			return mtCustom(CustomMtMixSerum, "the ashes in the unfinished potion: serum 207"), true
		}
		if s.HeldID(ObjMtLogs) > 0 && s.HeldID(ObjTinderbox) > 0 {
			return mtBurn(1, 1, "burn a log for the ashes the serum wants"), true
		}
		return mtPark(MtItemParkFor, "no ashes for the serum 207 (and no log and tinderbox to burn some)"), true
	case s.HeldID(ObjMtTarromin) > 0:
		if s.HeldID(ObjMtVialWater) > 0 {
			if st, ok := fail(CustomMtMixUnf, "the tarromin would not go into the vial of water"); ok {
				return st, true
			}
			return mtCustom(CustomMtMixUnf, "the tarromin in a vial of water: an unfinished potion"), true
		}
		if s.HeldID(ObjMtVial) > 0 {
			if st, ok := fail(CustomMtWater, "no vial could be filled at the sink"); ok {
				return st, true
			}
			return mtCustom(CustomMtWater, "fill a vial at the sink"), true
		}
		if st, ok := fail(CustomMtVial, "no empty vial lay by the experiment house's shelf"); ok {
			return st, true
		}
		return mtCustom(CustomMtVial, "an empty vial off the floor of the experiment house"), true
	case s.HeldID(ObjMtUnidTarromin) > 0:
		if st, ok := fail(CustomMtIdentify, "the tarromin would not be identified"); ok {
			return st, true
		}
		return mtCustom(CustomMtIdentify, "identify a tarromin"), true
	}
	if s.Failed[CustomMtHerbs] {
		return mtPark(MtStuckParkFor, "the smashed table gave no tarromin (it gives its herbs once to an account)"), true
	}
	return mtCustom(CustomMtHerbs, "search the smashed table for herbs"), true
}

// mtSets is the sets of plank, brick and paste the free slots take: a plank and a brick each, and one slot for the paste.
func MtSetsThatFit(free int) int { return max(0, min(MtBuildSets, (free-1)/2)) }

// mtBuildReady reports the building material in hand: a plank, a brick and five paste.
func mtBuildReady(s Snapshot) bool {
	return s.HeldID(ObjMtPlank) > 0 && s.HeldID(ObjMtBrick) > 0 && s.HeldID(ObjMtPaste) >= 5
}

// mtInside is the decision in the town and the temple, by stage.
func mtInside(s Snapshot, stage int) Step {
	// Fetched from Varrock only before the shades are met: a missing tinderbox or ashes later is a park, not a walk back across the swamp.
	if stage <= MtMadeSerum {
		if MtAshesShort(s, stage) > 0 && s.HeldID(ObjAshes) == 0 && s.HeldID(ObjMtUnfTarromin) == 0 && s.HeldID(ObjMtLogs) == 0 {
			return mtOutside(s, stage)
		}
		if s.HeldID(ObjTinderbox) == 0 && stage <= MtMadeSerum {
			return mtOutside(s, stage)
		}
	}
	if stage == MtNotStarted {
		if s.HeldID(ObjMtDiary) > 0 {
			if s.Failed[CustomMtReadDiary] {
				return mtPark(MtStuckParkFor, "the diary would not be read to its last page")
			}
			return mtCustom(CustomMtReadDiary, "read Herbi Flax's diary to its last page")
		}
		if s.Failed[CustomMtTakeDiary] {
			return mtPark(MtStuckParkFor, "Herbi Flax's diary was not on the shelf")
		}
		if s.FreeSlots < 1 {
			if s.Failed[CustomMtDropJunk] {
				return mtPark(MtStuckParkFor, "no room in the pack for the diary and nothing the swamp spoiled to drop")
			}
			return mtCustom(CustomMtDropJunk, "room for the diary: drop what the swamp spoiled")
		}
		return mtCustom(CustomMtTakeDiary, "Herbi Flax's diary off the shelf")
	}
	if st, ok := mtSerumLeg(s, stage); ok {
		return st
	}
	// The diary, the rogue's purse and a rotten lobster are slots the temple load wants (a drop that did nothing is not asked for again).
	if stage >= MtMadeSerum && !s.Failed[CustomMtDropJunk] && (s.HeldID(ObjMtDiary) > 0 || s.HeldID(ObjMtRoguesPurse) > 0 || s.HeldID(ObjMtRottenFood) > 0) {
		return mtCustom(CustomMtDropJunk, "drop the diary and what the swamp spoiled")
	}
	// The serum leg above has brewed what the stage still spends (or parked), so a villager's talk is only ever asked for with a dose in the pack.
	needSerum := func(name, reason string) Step {
		if s.Failed[name] {
			return mtPark(MtStuckParkFor, reason+" did not go through")
		}
		return mtCustom(name, reason)
	}
	fight := func(reason string) (Step, bool) {
		if v := MtShadesReady(s); !v.OK {
			return mtPark(MtFightParkFor, "not going among the Loar shades with this pack ("+reason+"): "+v.Why), true
		}
		return Step{}, false
	}
	switch {
	case stage == MtReadDiary || stage == MtMadeSerum:
		return needSerum(CustomMtSend, "Razmire sends the account after five shades")
	case stage >= MtKillShades && stage <= MtKilled4, stage == MtKilled5 && s.HeldID(ObjMtRemains) < MtRemainsWanted:
		if st, ok := fight("the hunt"); ok {
			return st
		}
		if s.Failed[CustomMtHunt] {
			return mtPark(MtStuckParkFor, "no Loar shade could be killed for its remains")
		}
		return mtCustom(CustomMtHunt, "kill a Loar shade and take its remains")
	case stage == MtKilled5:
		return needSerum(CustomMtHandOver, "Razmire takes the remains and opens the builders' store")
	case stage == MtShadesToRazmire:
		if st, ok := mtShopLeg(s, stage); ok {
			return st
		}
		return needSerum(CustomMtShow, "Ulsquire is shown the remains")
	case stage == MtShadesToUlsquire:
		return needSerum(CustomMtTempleTlk, "Ulsquire is asked about the temple")
	case stage == MtUlsquireTemple || stage == MtRebuildTemple:
		if st, ok := mtShopLeg(s, stage); ok {
			return st
		}
		if st, ok := fight("the temple"); ok {
			return st
		}
		if s.Failed[CustomMtTemple] {
			return mtPark(MtStuckParkFor, "the Flamtaer temple could not be rebuilt")
		}
		return mtCustom(CustomMtTemple, "rebuild the Flamtaer temple")
	case stage == MtCanLightAltar:
		if st, ok := mtShopLeg(s, stage); ok {
			return st
		}
		if st, ok := fight("the temple"); ok {
			return st
		}
		if s.Failed[CustomMtAltar] {
			return mtPark(MtStuckParkFor, "the altar could not be lit and the oil sanctified")
		}
		return mtCustom(CustomMtAltar, "light the altar and sanctify the oil")
	case stage == MtSacredOil:
		if MtSacredDoses(s) < MtPyreSacredDose && s.HeldID(ObjMtPyreLogs) == 0 {
			// The oil was spent or lost: more olive oil goes back to the flame.
			if MtOliveDoses(s) >= MtPyreSacredDose {
				if s.Failed[CustomMtAltar] {
					return mtPark(MtStuckParkFor, "the altar could not sanctify more oil")
				}
				return mtCustom(CustomMtAltar, "sanctify more oil in the flame")
			}
			if st, ok := mtShopLeg(s, stage); ok {
				return st
			}
			return mtPark(MtItemParkFor, "no oil, olive or sacred, for the pyre logs")
		}
		if s.HeldID(ObjMtLogs) == 0 && s.HeldID(ObjMtPyreLogs) == 0 {
			return mtPark(MtItemParkFor, "no log to make a pyre log of")
		}
		if s.Failed[CustomMtPyreLogs] {
			return mtPark(MtStuckParkFor, "the sacred oil would not turn a log into a pyre log")
		}
		return mtCustom(CustomMtPyreLogs, "sacred oil on a log: a pyre log")
	case stage == MtPyreLogs || stage == MtLogsOnPyre:
		return mtCremation(s, stage)
	case stage == MtLitPyre:
		return needSerum(CustomMtFinish, "Ulsquire is told the shade is at rest")
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Shades of Mort'ton stage %d", stage)}
}

// mtShopLeg is the shopping at Razmire's counters, ok false when the pack is stocked for the stage: the hammer from the general store (the temple wants it from stage 50, so
// it is bought at the hand-over), the building material from the builders' (before the temple is begun) and the olive oil (stage 60 and 65, when the hand-over's purchase was
// lost). Razmire is cured for the purchase by a dose of serum.
func mtShopLeg(s Snapshot, stage int) (Step, bool) {
	if s.HeldID(ObjHammer) == 0 && stage >= MtShadesToRazmire && stage <= MtCanLightAltar {
		if s.Failed[CustomMtGeneral] {
			return mtPark(MtStuckParkFor, "no hammer from Razmire's general store"), true
		}
		return Step{Kind: StepCustom, Custom: CustomMtGeneral, ItemID: ObjHammer, Reason: "a hammer from Razmire's general store"}, true
	}
	// Before the first wall is touched the pack holds the material; afterwards the temple's pool does (an action takes a set from the pack whenever the pool has room), so a pack
	// with none says nothing, and the executor itself goes back to Razmire when the temple says the pool is dry.
	if stage >= MtShadesToRazmire && stage < MtRebuildTemple && !mtBuildReady(s) {
		sets := MtSetsThatFit(s.FreeSlots)
		if sets == 0 {
			return Step{Kind: StepWait, Reason: "no pack room for building material"}, true
		}
		if s.Failed[CustomMtBuilders] {
			return mtPark(MtStuckParkFor, "no building material from Razmire's builders' store"), true
		}
		return Step{Kind: StepCustom, Custom: CustomMtBuilders, Qty: sets, Reason: fmt.Sprintf("%d sets of planks, bricks and paste from Razmire's builders' store", sets)}, true
	}
	if stage >= MtCanLightAltar && stage <= MtSacredOil && MtOliveDoses(s) < MtPyreSacredDose && MtSacredDoses(s) < MtPyreSacredDose && s.HeldID(ObjMtPyreLogs) == 0 {
		if s.Failed[CustomMtGeneral] {
			return mtPark(MtStuckParkFor, "no olive oil from Razmire's general store"), true
		}
		return Step{Kind: StepCustom, Custom: CustomMtGeneral, ItemID: ObjMtOil3, Reason: "olive oil from Razmire's general store"}, true
	}
	return Step{}, false
}

// mtCremation is stages 70 and 75: the Firemaking the pyre wants, the three actions of the cremation, and the remake of a log when the pyre went out.
func mtCremation(s Snapshot, stage int) Step {
	level := s.Level("firemaking")
	if level > 0 && level < MtFiremakingPyre {
		if s.HeldID(ObjMtLogs) > 0 && s.HeldID(ObjTinderbox) > 0 {
			if s.Failed[CustomMtBurn] {
				return mtPark(MtStuckParkFor, "the logs could not be burnt for the Firemaking the pyre wants")
			}
			return mtBurn(s.HeldID(ObjMtLogs), 0, fmt.Sprintf("burn spare logs for Firemaking 5 (the account is at %d)", level))
		}
		return mtPark(MtSkillParkFor, fmt.Sprintf("the pyre wants Firemaking %d and the account is at %d with no logs left to burn", MtFiremakingPyre, level))
	}
	if s.HeldID(ObjMtRemains) == 0 {
		return mtPark(MtItemParkFor, "no shade remains left for the pyre")
	}
	if s.HeldID(ObjTinderbox) == 0 {
		return mtPark(MtItemParkFor, "no tinderbox to light the pyre")
	}
	if s.HeldID(ObjMtPyreLogs) == 0 {
		// The pyre logs went on a pyre that has since gone out (49 ticks): another is made when the oil allows.
		if MtSacredDoses(s) >= MtPyreSacredDose && s.HeldID(ObjMtLogs) > 0 {
			return mtCustom(CustomMtPyreLogs, "another pyre log: the first pyre went out unlit")
		}
		if stage == MtLogsOnPyre {
			return mtPark(MtPyreParkFor, "the pyre went out before it was lit and there is no sacred oil to make another pyre log")
		}
		return mtPark(MtItemParkFor, "no pyre logs for the pyre")
	}
	if s.Failed[CustomMtCremate] {
		return mtPark(MtStuckParkFor, "the shade's remains could not be cremated")
	}
	return mtCustom(CustomMtCremate, "stack the pyre logs, lay the remains and light the pyre")
}

// MorttonRecord is the quest's identity: the levels are the content's own and the Record states them, so the ladder never starts an account below them; Nature Spirit is
// the gate to the swamp.
var MorttonRecord = Record{ID: MorttonID, Name: MorttonName, QuestPoints: 3,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "herblore", Level: MtHerblore}, {Skill: "crafting", Level: MtCrafting}}, Quests: []string{NatureSpiritID}}}

// MorttonShades is the engine module. The shades are fought (at the hunt and while the temple is built) and a lost fight is a near-death; a death in either parks it a day, and two
// deaths anywhere do.
var MorttonShades = Module{
	Record: MorttonRecord, Bank: VarrockEastBank, BankSet: true, Anchor: MtTown,
	ToolIDs:   mtToolIDs,
	ReadStage: ParseMorttonJournal,
	Sustain:   mtSustain,
	MaxDeaths: 2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomMtHunt:   {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
		CustomMtTemple: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	Decide: DecideMortton,
}
