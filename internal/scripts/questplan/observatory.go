package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Observatory Quest (content id `itgronigen`, varp %itgronigen, 2 quest points), ported from the TS def (defs/observatory/*.ts)
// and checked against content 289: quests/quest_itgronigen/scripts (quest_itgronigen.rs2, observatory_professor.rs2,
// goblin_guard.rs2, itgronigen_journal.rs2), configs (the constants, the locs and the guard's npc config) and maps/m37_147.jm2.
//
// THE QUEST. The professor in the Observatory reception (south-west of Ardougne) wants a telescope mended: three planks, a bronze
// bar and a molten glass, one hand-over each and each only at its own stage (%itgronigen 1 to 4), then a lens mould that the goblins
// hid under a sack in the dungeon beneath the Observatory (stage 4). The professor takes the glass and gives it straight back, the
// account makes the lens by using the mould on the glass (stage 5), and the professor, who has gone ahead, is met through the
// dungeon again: the ladder up into the dome, the telescope (stage 6). The reward is Crafting 2,250 XP (stat_advance(crafting,
// 22500)), a sapphire, 2 quest points and one of twelve constellation gifts (875 XP of Strength, Defence, Hitpoints or Attack, a
// maple longbow, a black 2h sword, runes, food and the like). No level is asked for: the lens needs no Crafting level
// (craft_telescope_disc has none), though the quest's listing says 10.
//
// WHERE THE ITEMS COME FROM, in the owner's item rule (provision.go): the account's own bank, then the mule, then the world.
//   - Planks: no shop and no mule row; the Barbarian Outpost's four ground spawns (2552..2556,3573..3575, the same ones Horror from the
//     Deep walks to). The first bundled pass asks the bank and the mule for them anyway.
//   - The bronze bar: the mule holds thousands; failing that, a copper ore and a tin ore (the mule holds those too) are smelted on the
//     Ardougne furnace (an ore used on a furnace runs smelt_ore_single, one bar, no menu). Nothing is mined: an account whose bank and
//     the mule hold neither parks for ObsItemParkFor with the reason said.
//   - The molten glass: a bucket of sand and a soda ash used on a furnace (smelt_glass). The soda ash is a seaweed used on a range
//     (the mule holds 600,000 seaweed); the sand is a single ground spawn in the goblin huts 25 tiles east of the reception (the mule
//     holds a couple), so the quest's fleet slot keeps the crowd small.
//   - The lens mould is the sack's, the key chest's key the chest's: both untradeable, both refused by the content when the BANK
//     holds one (`~obj_gettotal` counts the pack, the bank and the worn slots), which is why the bank is read before the dungeon.
//
// THE DUNGEON AND WHAT LIVES IN IT, from maps/m37_147.jm2 and the npc configs (scripts/_unpack/225/all.npc and quest_itgronigen.npc):
// thirty-odd goblins (level 2 with no stats, level 5 with attack 3) and rats that have no hunt mode and attack nobody who leaves
// them be; the poisonous spiders (level 31) exist only in the six "shutdungeonchest" chests ("Search" spawns one and poisons the
// player), so no chest is ever touched by name: the key is the one chest loc 2197/2198 and every lookup is by id. The one real foe is
// the goblin guard (npc 489, level 42) who stands on the north side of the keep gate (m37_147 "0 23 50: 489", 2391,9458). He is
// passive until the gate is opened or he is talked to (open_keep_gate runs ~npc_retaliate(0) on a guard within 8 tiles of the account),
// and from then on he follows the account through the gate and into the keep, so he is killed first, at the gate, through the shared
// fight step, and only by an account the model says wins (ObsGuardReady): attack 32, defence 37, strength 37, 43 hitpoints, attack
// bonus 8 and strength bonus 5 make a hardest hit of 5 every 4 ticks.
//
// THE GATE. The two halves (locs 2199 and 2200, row 9457) are opened by oploc1 from the south side whatever the lock, and from the north
// only once %itkeepgatelock is set; using the keep key on it opens it from either side and sets the lock for good ("The gate unlocks.
// The key is useless now. You discard it."). The crossing is a p_teleport of one tile, so it is checked by tile. A locked gate answers
// "The gate is locked." and nothing else (no guard stir), so the sequence is: kill the guard, open the gate (key on it when the pack
// holds one), and only a locked answer sends the account to the key chest.
//
// THE LADDERS. The reception ladder down (loc 2187) is a p_telejump behind two lines of the assistant's warning at every stage
// before the end; the cavern's way up to the reception is loc 2188, and the ladder up to the dome loc 2189 (it arrives inside the
// walled dome, whose only exit is the dome's own ladder down, loc 2190).
//
// THE TELESCOPE. Stage 6 only: the telescope (loc 2210) answers when the dome's professor (npc 490) is within 7 tiles of the account.
// It opens with his "Well done" chat, puts the star chart on the main modal, and queues the constellation conversation, which
// cannot run while that chart is up: the chart is closed, and the conversation (two options, either one) is driven to the quest scroll.

// ObservatoryID is the engine's id and ObservatoryName the quest tab's spelling.
const (
	ObservatoryID   = "itgronigen"
	ObservatoryName = "Observatory Quest"
)

// The %itgronigen values (quest_itgronigen.constant and quest.constant).
const (
	ObsNotStarted    = 0
	ObsStarted       = 1
	ObsGivenPlanks   = 2
	ObsGivenBronze   = 3
	ObsGivenGlass    = 4
	ObsGivenMould    = 5
	ObsSentTelescope = 6
	ObsComplete      = 7
)

// ObsPlanksNeeded is the planks the professor takes (professor_planks: inv_total(woodplank) < 3 sends the account away).
const ObsPlanksNeeded = 3

// Ids from content/pack (checked against the production config by TestObservatoryIDsMatchTheProductionConfig in questmods).
const (
	NpcObsAssistant     = 487 // [observatory_assistant], who warns the account at the top of the ladder
	NpcObsProfessor     = 488 // [observatory_professor], the reception's
	NpcGoblinGuard      = 489 // [goblin_guard], level 42, at the keep gate
	NpcObsProfessorDome = 490 // [observatory_professor2], the dome's copy the telescope looks for

	ObjObsKeepKey = 601 // [keep_key], untradeable
	ObjLensMould  = 602 // [lens_mould], untradeable
	ObjLens       = 603 // [lens], untradeable
	ObjCopperOre  = 436
	ObjTinOre     = 438

	LocObsReceptionLadder = 2187 // [obs_dungeonladderdown], op1 Climb-down: the reception's way into the dungeon
	LocObsCavernExit      = 2188 // [loc_2188], op1 Climb-up: the cavern's way to the reception
	LocObsDomeLadderUp    = 2189 // [loc_2189], op1 Climb-up: the cavern's way into the dome
	LocObsDomeLadderDown  = 2190 // [domeladderdown], op1 Climb-down: the dome's way back to the cavern
	LocObsKeyChestShut    = 2197 // [loc_2197], the one chest with a key (Open)
	LocObsKeyChestOpen    = 2198 // [loc_2198], the same chest open (Search)
	LocObsGateRight       = 2199 // [keepgate_closed]
	LocObsGateLeft        = 2200 // [keepgate_closed_left]
	LocObsTelescope       = 2210 // [telescope], op1 Use
	LocObsSack            = 2212 // [loc_2212], op1 Search: the lens mould's
	LocObsFurnace         = 2781 // the Ardougne furnace (op Smelt)
	LocObsRange           = 2728 // [range], the stoves of East Ardougne
)

// Tiles from content maps and the loc index.
var (
	// ObsProfessorAnchor is the professor's spawn in the reception (m38_49 "0 54 34: 488").
	ObsProfessorAnchor = tile.Tile{X: 2438, Z: 3186}
	// ObsReceptionLadderStand is the tile north of the reception's ladder down (loc 2187 at 2444,3191).
	ObsReceptionLadderStand = tile.Tile{X: 2444, Z: 3192}
	// ObsCavernLanding is where the ladder down sets the account (0_37_147_52_51), one tile south of the cavern's way up (loc 2188 at 2420,9460).
	ObsCavernLanding = tile.Tile{X: 2420, Z: 9459}
	// ObsKeyChestStand is south of the key chest (loc 2197 at 2428,9419): forceapproach=north placed at angle 2 turns the legal side to the south.
	ObsKeyChestStand = tile.Tile{X: 2428, Z: 9418}
	// ObsGateNorth is the locked side of the gate, where the guard stands (his spawn is 2391,9458), and ObsGateSouth the keep's side.
	ObsGateNorth = tile.Tile{X: 2390, Z: 9458}
	ObsGateSouth = tile.Tile{X: 2390, Z: 9457}
	// ObsGuardSpawn is the guard's spawn.
	ObsGuardSpawn = tile.Tile{X: 2391, Z: 9458}
	// ObsSackStand is the only cardinal neighbour of the sack (loc 2212 at 2389,9454) that the keep leaves free.
	ObsSackStand = tile.Tile{X: 2390, Z: 9454}
	// ObsDomeLadderFoot is north of the cavern's ladder into the dome (loc 2189 at 2423,9439), ObsDomeArrive where it sets the account down
	// (0_38_49_8_29), beside the dome's ladder back down (loc 2190 at 2440,3164).
	ObsDomeLadderFoot = tile.Tile{X: 2423, Z: 9440}
	ObsDomeArrive     = tile.Tile{X: 2440, Z: 3165}
	// ObsTelescopeAt is the telescope (loc 2210 at 2440,3157, three tiles long) and ObsDomeProfessor the dome professor's spawn (m38_49).
	ObsTelescopeAt   = tile.Tile{X: 2440, Z: 3157}
	ObsDomeProfessor = tile.Tile{X: 2439, Z: 3160}
	// ObsSandSpawn is the lone bucket_sand spawn (maps/m38_49 "0 29 50: 1783" is 2461,3178), in the goblin huts east of the reception.
	ObsSandSpawn = tile.Tile{X: 2461, Z: 3178}
	// ObsRangeAt is the East Ardougne range nearest the furnace (loc 2728 at 2617,3317), where seaweed is burnt to soda ash.
	ObsRangeAt = tile.Tile{X: 2617, Z: 3317}
	// ObsFurnaceAt is the Ardougne furnace (loc 2781 at 2601,3310, three by three).
	ObsFurnaceAt = tile.Tile{X: 2601, Z: 3310}
	// ObsPlankSpawns are the four planks in the Barbarian Outpost's north-east corner (maps/m39_55.jm2 "0 56 54", "0 57 55", "0 58 54", "0 60 53").
	ObsPlankSpawns = []tile.Tile{{X: 2552, Z: 3574}, {X: 2553, Z: 3575}, {X: 2554, Z: 3574}, {X: 2556, Z: 3573}}
	// ObsPlankCentre is the tile between the first three (every one is within a tile of it): the ground-source runner picks up what lies within
	// a tile of a source, so standing here takes three planks one after another and the fourth tile is the source after it.
	ObsPlankCentre = tile.Tile{X: 2553, Z: 3574}
)

// InObsKeep reports a tile of the keep behind the gate: the 16 tiles the planner reaches on foot from the sack (a column at x 2390 and
// a small room east of it, x 2389..2394, z 9453..9457), none of which any tile north of the gate reaches on foot. (The TS def's
// rectangle, x 2386..2392 and z 9450..9457, held a third of the open cavern west of the gate as well.)
func InObsKeep(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2389 && t.X <= 2394 && t.Z >= 9453 && t.Z <= 9457
}

// InObsDome reports a tile inside the dome, the walled pocket on the surface whose only door is the ladder down.
func InObsDome(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2430 && t.X <= 2450 && t.Z >= 3150 && t.Z <= 3170
}

// InObsCavern reports a tile of the goblin cavern (the dungeon under the Observatory, keep included).
func InObsCavern(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2368 && t.X <= 2440 && t.Z >= 9400 && t.Z <= 9475
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomObsMould     = "obs-mould"
	CustomObsTelescope = "obs-telescope"
	CustomObsAsh       = "obs-ash"
	CustomObsLeave     = "obs-leave"

	KitObsBundle = "obs-bundle"
	KitObsPlanks = "obs-planks"
	KitObsSand   = "obs-sand"
	KitObsFood   = "obs-food"
)

// ObsItemParkFor is how long the quest skips an account whose bank and mule cannot give an item no skill of its own makes here (the
// planks, the seaweed or the ores): the mule's stock changes by the hour, not by the minute.
const ObsItemParkFor = 3 * time.Hour

// ObsGuardParkFor is how long the quest skips an account the model says loses to the goblin guard: its levels do not change in an hour.
const ObsGuardParkFor = 6 * time.Hour

// obsFoodIDs are the foods the quest eats (Module.Sustain; the eat loop's floor bite takes any food the pack carries).
var obsFoodIDs = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}

// obsSustain is the quest's survival policy: the foods it carries and the floor its eat line never goes under (the guard's
// hardest hit is 5, so FoeEatLine raises it to 10 for him).
var obsSustain = Sustain{FoodIDs: obsFoodIDs, EatAt: 14}

// obsTools are the quest's own items and its coins: what the floor bite never eats and every bank visit leaves in the pack.
var obsTools = []int{ObjPlank, ObjBronzeBar, ObjMoltenGlass, ObjLensMould, ObjLens, ObjObsKeepKey, ObjSeaweed, ObjSodaAsh,
	ObjBucketOfSand, ObjCopperOre, ObjTinOre, CoinsID}

// obsKeep is what every bank step of the quest leaves in the pack: the tools and the food. (The food is no tool: the module's tools are
// protected from the floor bite, which is the rule that eats any food the pack carries once an account falls to half its hitpoints on a
// walk, and the sharks are exactly the food it should eat.)
var obsKeep = append(append([]int(nil), obsTools...), obsFoodIDs...)

// ObsKeepIDs returns the keep list (a copy).
func ObsKeepIDs() []int { return append([]int(nil), obsKeep...) }

// ObsToolIDs returns the module's tools (a copy).
func ObsToolIDs() []int { return append([]int(nil), obsTools...) }

// ObsProfessorStop starts the quest and takes every hand-over. The first menu is "I am lost!!!" / "I'd like to have a look through that
// telescope." / "What's the ladder over there for?" / "It is of no concern of yours..." (a fifth line about Treasure Trails appears with a
// sextant clue), the second "Sounds interesting, what can I do for you?" / "Oh, sorry, I don't have time for that."; the hand-overs have no menu.
var ObsProfessorStop = NpcStop{Npc: "Observatory professor", ID: NpcObsProfessor, Anchor: ObsProfessorAnchor, Leash: 6,
	Prefer: []string{"I'd like to have a look through that telescope.", "Sounds interesting, what can I do for you?"}}

// FoeGoblinGuard is quest_itgronigen.npc [goblin_guard] (npc 489): attack 32, defence 37, strength 37, hitpoints 43, attackbonus 8,
// strengthbonus 5, stab/slash/crush defence 4/6/6, no attackrate (4), no magic stat, a hardest hit of 5.
var FoeGoblinGuard = MeleeFoe{Name: "Goblin guard", Attack: 32, Strength: 37, Defence: 37, Magic: 1, Hitpoints: 43, AttackTicks: 4,
	AttackBonus: 8, StrengthBonus: 5, DefenceBonus: 4}

// obsMarkers are the journal's markers, newest stage first: itgronigen_journal.rs2 keeps every earlier paragraph on the page, so the
// newest one decides (the completion page still lists "1 lens mould", the lens page still says "molten glass").
var obsMarkers = []struct {
	stage int
	text  string
}{
	{ObsComplete, "quest complete!"},
	{ObsSentTelescope, "gone ahead to the observatory"},
	{ObsGivenMould, "create the lens by using the molten glass"},
	{ObsGivenGlass, "1 lens mould"},
	{ObsGivenBronze, "1 molten glass"},
	{ObsGivenPlanks, "1 bronze bar"},
	{ObsStarted, "3 wooden planks"},
	{ObsNotStarted, "i can start this quest by talking to the"},
}

// ParseObservatoryJournal reads %itgronigen from the rendered journal. ok is false for an empty or unrecognised page, which is
// unknown, never stage 0.
func ParseObservatoryJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range obsMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	return 0, false
}

// ObsPastMould reports that the account is past the dungeon's crowded steps (the planks, the sand, the guard, the key chest, the
// sack): from the lens on, the quest only talks, and climbs into the dome (Module.ExclusiveDone).
func ObsPastMould(s Snapshot) bool {
	return s.Journal == StatusComplete || (s.StageKnown && s.Stage >= ObsGivenMould)
}

// ObsGuardVerdict is ObsGuardReady's answer.
type ObsGuardVerdict struct {
	OK  bool
	Why string
}

// ObsGuardReady judges whether the pack can take the goblin guard: the account's own swing (MeleeChain: the shared fight step melees)
// from the level a fight starts at (FightStartHP), eating by the foe's eat line, and it must win with a tenth of its bites left for the
// way out and never fall under his hardest hit. The reason names the account's levels and the food it holds, for the log.
func ObsGuardReady(s Snapshot) ObsGuardVerdict {
	bites := len(FoodBites(s.InvIDs, obsFoodIDs))
	if bites == 0 {
		return ObsGuardVerdict{Why: fmt.Sprintf("no food in the pack for the goblin guard (%d hitpoints, hits up to %d)", FoeGoblinGuard.Hitpoints, FoeGoblinGuard.MaxHit())}
	}
	odds := MeleeChain(s, []MeleeFoe{FoeGoblinGuard}, obsSustain)
	if odds.OK {
		return ObsGuardVerdict{OK: true}
	}
	return ObsGuardVerdict{Why: fmt.Sprintf("%s (the goblin guard: Attack %d, Strength %d, Defence %d, %d/%d hitpoints, %d bites of food)",
		odds.Why, s.Level("attack"), s.Level("strength"), s.Level("defence"), s.HP, s.MaxHP, bites)}
}

// obsBundle is the first pass of a run: everything the quest will want that the bank or the mule can give, in one visit and one ask.
// Every line is optional and carries no source of its own (a source on an optional line is worked), so a short bank never blocks it.
func obsBundle(s Snapshot) []KitLine {
	stage := s.Stage
	var kit []KitLine
	add := func(l KitLine) { l.Optional = true; kit = append(kit, l) }
	if stage <= ObsStarted && s.HeldID(ObjPlank) < ObsPlanksNeeded {
		add(KitLine{ID: ObjPlank, Name: "Plank", Qty: ObsPlanksNeeded})
	}
	if stage <= ObsGivenPlanks && s.HeldID(ObjBronzeBar) == 0 {
		add(KitLine{ID: ObjBronzeBar, Name: "Bronze bar", Qty: 1})
		add(KitLine{ID: ObjCopperOre, Name: "Copper ore", Qty: 1, OnlyIfShort: ObjBronzeBar})
		add(KitLine{ID: ObjTinOre, Name: "Tin ore", Qty: 1, OnlyIfShort: ObjBronzeBar})
	}
	if (stage <= ObsGivenBronze || (stage == ObsGivenMould && s.HeldID(ObjLens) == 0)) && s.HeldID(ObjMoltenGlass) == 0 {
		add(KitLine{ID: ObjMoltenGlass, Name: "Molten glass", Qty: 1})
		add(KitLine{ID: ObjBucketOfSand, Name: "Bucket of sand", Qty: 1, OnlyIfShort: ObjMoltenGlass})
		add(KitLine{ID: ObjSodaAsh, Name: "Soda ash", Qty: 1, OnlyIfShort: ObjMoltenGlass})
		add(KitLine{ID: ObjSeaweed, Name: "Seaweed", Qty: 1, OnlyIfShort: ObjMoltenGlass})
	}
	if stage <= ObsGivenMould && s.HeldID(ObjLensMould) == 0 {
		add(KitLine{ID: ObjLensMould, Name: "Lens mould", Qty: 1, Untradeable: true})
	}
	if stage == ObsGivenMould && s.HeldID(ObjLens) == 0 {
		add(KitLine{ID: ObjLens, Name: "Lens", Qty: 1, Untradeable: true})
	}
	if stage <= ObsGivenGlass && s.HeldID(ObjLensMould) == 0 {
		add(KitLine{ID: ObjObsKeepKey, Name: "Keep key", Qty: 1, Untradeable: true})
	}
	return kit
}

// obsPlankKit is the three planks of the first hand-over, from the outpost's four ground spawns after the bank and the mule.
func obsPlankKit() []KitLine {
	srcs := []Source{{Kind: SourceGround, Tile: ObsPlankCentre, PerSpawn: 1, RespawnTicks: 100, Note: "the Barbarian Outpost's three planks within a tile of its middle"},
		{Kind: SourceGround, Tile: ObsPlankSpawns[3], PerSpawn: 1, RespawnTicks: 100, Note: "the Barbarian Outpost's fourth plank"}}
	return []KitLine{{ID: ObjPlank, Name: "Plank", Qty: ObsPlanksNeeded, Sources: srcs}}
}

// obsSandKit is the bucket of sand for the glass, from the bank and the mule and then the one spawn in the goblin huts.
func obsSandKit() []KitLine {
	return []KitLine{{ID: ObjBucketOfSand, Name: "Bucket of sand", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: ObsSandSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the goblin huts east of the Observatory"}}}}
}

// obsFoodKit is the food the guard fight eats, from the bank and the mule only.
func obsFoodKit() []KitLine {
	return []KitLine{{ID: ObjShark, Name: "Shark", Qty: 8, Optional: true}}
}

// obsProvisionOnce runs a kit's provision step once a run (the bundle and the food): never again after it has run or failed.
func obsProvisionOnce(s Snapshot, name string, kit []KitLine, reason string) (Step, bool) {
	if len(kit) == 0 || s.Ran["provision:"+name] || s.Failed["provision:"+name] {
		return Step{}, false
	}
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: obsKeep, Bank: EastArdougneNorth, BankSet: true, Reason: reason}, true
}

// obsKitOrWait provisions a required kit, and waits (parking for ObsItemParkFor) once every source of it is dead.
func obsKitOrWait(s Snapshot, name string, kit []KitLine, reason, because string) Step {
	if s.Blocked["provision:"+name] {
		return Step{Kind: StepWait, ParkFor: ObsItemParkFor, Reason: because}
	}
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: obsKeep, Bank: EastArdougneNorth, BankSet: true, Reason: reason}
}

// obsTalk is a hand-over: the professor takes whatever his stage names.
func obsTalk(reason string) Step { return Step{Kind: StepTalk, Stop: ObsProfessorStop, Reason: reason} }

// obsFromBank is the step that gets an item the pack lacks out of the bank when the bank holds it, the bank read first when it has not
// been this run, and (nil, false) when the bank is read and holds none.
func obsFromBank(s Snapshot, id int, name string) (Step, bool) {
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: EastArdougneNorth, BankSet: true, Reason: "the bank may hold the " + strings.ToLower(name)}, true
	}
	if n, _ := s.BankedID(id); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: name, Qty: 1, ID: id}}, KeepIDs: obsKeep, Bank: EastArdougneNorth, BankSet: true,
			Reason: "the " + strings.ToLower(name) + " the bank holds"}, true
	}
	return Step{}, false
}

// obsBarStep is the step that makes the bar: a copper ore used on the Ardougne furnace with a tin ore in the pack.
func obsBarStep(s Snapshot) Step {
	if s.HeldID(ObjCopperOre) > 0 && s.HeldID(ObjTinOre) > 0 {
		return Step{Kind: StepUseOn, Item: "Copper ore", ItemID: ObjCopperOre, TargetKind: TargetLoc, Target: "Furnace", TargetID: LocObsFurnace,
			Anchor: ArdougneFurnace, Reason: "a bronze bar from a copper ore and a tin ore at the furnace"}
	}
	return Step{Kind: StepWait, ParkFor: ObsItemParkFor,
		Reason: "no bronze bar, and no copper ore and tin ore to smelt one from, in the bank or the mule (nothing is mined)"}
}

// obsGlassStep is the step that makes the molten glass, a stage at a time: the sand first (the one shared item), the soda ash from a
// seaweed on a range, and the furnace.
func obsGlassStep(s Snapshot) Step {
	hasSand, hasAsh, hasWeed := s.HeldID(ObjBucketOfSand) > 0, s.HeldID(ObjSodaAsh) > 0, s.HeldID(ObjSeaweed) > 0
	if !hasSand {
		return obsKitOrWait(s, KitObsSand, obsSandKit(), "a bucket of sand for the glass (the goblin huts' spawn when the bank and the mule hold none)",
			"no bucket of sand from the bank, the mule or the goblin huts' spawn")
	}
	if !hasAsh {
		if !hasWeed {
			return Step{Kind: StepWait, ParkFor: ObsItemParkFor, Reason: "no soda ash or seaweed to burn into it, in the bank or the mule"}
		}
		return Step{Kind: StepCustom, Custom: CustomObsAsh, Reason: "a seaweed burnt to soda ash on the range"}
	}
	return Step{Kind: StepUseOn, Item: "Bucket of sand", ItemID: ObjBucketOfSand, TargetKind: TargetLoc, Target: "Furnace", TargetID: LocObsFurnace,
		Anchor: ArdougneFurnace, Reason: "molten glass from the sand and the soda ash at the furnace"}
}

// obsMouldStep is the lens mould: from the bank when the bank holds it, else the dungeon trip behind its gates (the bank read, the
// key, the food, the odds against the guard).
func obsMouldStep(s Snapshot) Step {
	underground := s.TileKnown && IsUnderground(s.Tile)
	inKeep := s.TileKnown && InObsKeep(s.Tile)
	if !underground {
		if st, ok := obsFromBank(s, ObjLensMould, "Lens mould"); ok {
			return st
		}
		// The key, if the bank holds one the content would refuse a second of (the chest counts the bank).
		if n, _ := s.BankedID(ObjObsKeepKey); n > 0 && s.HeldID(ObjObsKeepKey) == 0 {
			return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Keep key", Qty: 1, ID: ObjObsKeepKey}}, KeepIDs: obsKeep, Bank: EastArdougneNorth,
				BankSet: true, Reason: "the keep key the bank holds (the chest refuses a second)"}
		}
		if st, ok := obsProvisionOnce(s, KitObsFood, obsFoodKit(), "food for the goblin guard"); ok {
			return st
		}
	}
	if !inKeep {
		if v := ObsGuardReady(s); !v.OK {
			if underground {
				return Step{Kind: StepCustom, Custom: CustomObsLeave, Reason: "the pack cannot take the goblin guard: " + v.Why}
			}
			return Step{Kind: StepWait, ParkFor: ObsGuardParkFor, Reason: "not going down to the goblin guard with this pack: " + v.Why}
		}
	}
	return Step{Kind: StepCustom, Custom: CustomObsMould, Reason: "the lens mould from the sack in the keep"}
}

// DecideObservatory is the whole quest, read from the journal's stage and the pack.
func DecideObservatory(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return obsTalk("start the Observatory Quest")
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	if s.Stage == ObsNotStarted || s.Stage >= ObsComplete {
		return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about the Observatory Quest"}
	}
	// One bundled pass of everything the bank or the mule can give, before the first walk of a run (stage 6 needs none of it).
	if s.Stage < ObsSentTelescope {
		if st, ok := obsProvisionOnce(s, KitObsBundle, obsBundle(s), "everything the bank or the mule can give for the telescope's parts"); ok {
			return st
		}
	}
	switch s.Stage {
	case ObsStarted:
		if s.HeldID(ObjPlank) >= ObsPlanksNeeded {
			return obsTalk("hand over the planks")
		}
		return obsKitOrWait(s, KitObsPlanks, obsPlankKit(), "three planks from the Barbarian Outpost's spawns",
			"no planks from the bank, the mule or the Barbarian Outpost's spawns")
	case ObsGivenPlanks:
		if s.HeldID(ObjBronzeBar) > 0 {
			return obsTalk("hand over the bronze bar")
		}
		return obsBarStep(s)
	case ObsGivenBronze:
		if s.HeldID(ObjMoltenGlass) > 0 {
			return obsTalk("hand over the molten glass")
		}
		return obsGlassStep(s)
	case ObsGivenGlass:
		if s.HeldID(ObjLensMould) > 0 {
			if s.FreeSlots < 1 {
				return Step{Kind: StepDeposit, KeepIDs: obsKeep, Bank: EastArdougneNorth, BankSet: true, Reason: "room for the glass the professor hands back"}
			}
			if s.TileKnown && IsUnderground(s.Tile) {
				return Step{Kind: StepCustom, Custom: CustomObsLeave, Reason: "up to the reception with the mould"}
			}
			return obsTalk("hand over the lens mould")
		}
		return obsMouldStep(s)
	case ObsGivenMould:
		if s.HeldID(ObjLens) > 0 {
			return obsTalk("hand over the lens")
		}
		if s.HeldID(ObjMoltenGlass) == 0 {
			if st, ok := obsFromBank(s, ObjMoltenGlass, "Molten glass"); ok {
				return st
			}
			return obsGlassStep(s)
		}
		if s.HeldID(ObjLensMould) == 0 {
			return obsMouldStep(s)
		}
		return Step{Kind: StepUseOn, Item: "Lens mould", ItemID: ObjLensMould, TargetKind: TargetItem, Target: "Molten glass", TargetID: ObjMoltenGlass,
			Reason: "the lens from the mould and the molten glass"}
	case ObsSentTelescope:
		if s.FreeSlots < 2 {
			return Step{Kind: StepDeposit, KeepIDs: obsKeep, Bank: EastArdougneNorth, BankSet: true, Reason: "room for the sapphire and the constellation's gift"}
		}
		return Step{Kind: StepCustom, Custom: CustomObsTelescope, Reason: "through the dungeon and the dome to the telescope"}
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Observatory Quest stage %d", s.Stage)}
}

// ObservatoryRecord is the quest's identity. The server gates nothing; the guard's odds are judged where the fight is.
var ObservatoryRecord = Record{ID: ObservatoryID, Name: ObservatoryName, QuestPoints: 2}

// Observatory is the engine module. The goblin guard is fought (a lost fight is a near-death), two deaths park it for a day and one at
// the mould trip does too (StepDeathCaps), and the slot is given up once the lens is made.
var Observatory = Module{
	Record: ObservatoryRecord, Bank: EastArdougneNorth, BankSet: true, Anchor: ObsProfessorAnchor,
	ToolIDs:   obsTools,
	ReadStage: ParseObservatoryJournal,
	Sustain:   obsSustain,
	MaxDeaths: 2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomObsMould: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	ExclusiveDone: ObsPastMould,
	Decide:        DecideObservatory,
}
