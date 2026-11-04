package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Dragon Slayer (content id `dragon`, %dragonquest with side varps for the briefing, the Oracle,
// the shield, the hull and Ned, none transmitted), from the lane Q study
// (private evidence notes), dragon_journal.rs2 and the TS def
// (rsb0t-main/src/bot/api/ai/quests/defs/dragonslayer). The one server gate is the Champions'
// Guild door's 32 quest points. The journal renders the stage and six side facts, which this
// module packs as stage + 100*flags. Map pieces are judged by the pack (the journal renders them
// from the pack too). The finale is Elvarg (83): the anti-dragon shield caps her breath at 10.

// DragonSlayerName is the quest tab's spelling (component 7383).
const DragonSlayerName = "Dragon Slayer"

// DragonSlayerID is the engine's id (the ladder's rung id).
const DragonSlayerID = "dragon"

// Stages (quest_dragon.constant; 4-6 are unused).
const (
	DragonGuildmaster = 1
	DragonOziach      = 2
	DragonShip        = 3
	DragonRepaired    = 7
	DragonNedMap      = 8
	DragonSailed      = 9
	DragonComplete    = 10
)

// Journal side facts (packed into the stage as flags*100).
const (
	DragonNeedsBriefing = 1 << iota
	DragonAskedOracle
	DragonHasShield
	DragonShipRepaired
	DragonNedHired
	DragonSecretPassage
)

// DragonTracks unpacks the packed stage.
func DragonTracks(stage int) (st, flags int) { return stage % 100, stage / 100 }

// Ids from content/pack.
const (
	NpcGuildmaster   = 198
	NpcOziach        = 747
	NpcDuke          = 741
	NpcOracle        = 746
	NpcWormbrain     = 745
	NpcKlarense      = 744
	NpcNed           = 918
	NpcNedAboard     = 743
	NpcElvarg        = 742
	ObjMapMelzar     = 1535
	ObjMapWormbrain  = 1536
	ObjMapOracle     = 1537
	ObjCrandorMap    = 1538
	ObjMazeKey       = 1542
	ObjPlank         = 960
	ObjMindBomb      = 1907
	ObjUnfiredBowl   = 1791
	ObjLobsterPot    = 301
	ObjSilk          = 950
	ObjSteelBar      = 2353
	LocMazeChestShut = 2603
	LocMagicDoor     = 2586
	LocOracleChest   = 2587
	LocShipGangplank = 2593
	LocShipLadder    = 2590
	LocShipHole      = 2589
	LocCrandorRock   = 2609
	LocElvargGate    = 2607
	LocSecretDoor    = 2606
	WormbrainPrice   = 10000
	ShipPrice        = 2000
	MethodSmithNails = "smith-nails"
)

// MazeLeg is one leg of Melzar's Maze: a kill for a key, a door opened with it, a ladder, or the
// chest. Locs go by exact tile and NPCs by exact id (decoy rats and ladders share the names).
type MazeLeg struct {
	Kind       string // kill, door, climb, chest
	What       string
	NpcID      int
	At, Stand  tile.Tile
	Door, Land tile.Tile
	KeyID      int
	Op         string
	LadderAt   tile.Tile
}

// MazeLegs is the route (TS maze.ts, the shortest the collision pack admits, key order).
var MazeLegs = []MazeLeg{
	{Kind: "door", What: "the front door", Stand: tile.Tile{X: 2941, Z: 3248}, Door: tile.Tile{X: 2941, Z: 3248}, KeyID: ObjMazeKey, Land: tile.Tile{X: 2940, Z: 3248}},
	{Kind: "kill", What: "the giant rat", NpcID: 748, At: tile.Tile{X: 2930, Z: 3252}, KeyID: 1543},
	{Kind: "door", What: "the red door", Stand: tile.Tile{X: 2926, Z: 3253}, Door: tile.Tile{X: 2926, Z: 3253}, KeyID: 1543, Land: tile.Tile{X: 2925, Z: 3253}},
	{Kind: "climb", What: "the ladder to the first floor", Stand: tile.Tile{X: 2927, Z: 3256}, LadderAt: tile.Tile{X: 2928, Z: 3256}, Op: "Climb-up", Land: tile.Tile{X: 2928, Z: 3256, Level: 1}},
	{Kind: "kill", What: "the ghost", NpcID: 749, At: tile.Tile{X: 2929, Z: 3249, Level: 1}, KeyID: 1544},
	{Kind: "door", What: "the orange door", Stand: tile.Tile{X: 2930, Z: 3253, Level: 1}, Door: tile.Tile{X: 2931, Z: 3253, Level: 1}, KeyID: 1544, Land: tile.Tile{X: 2931, Z: 3253, Level: 1}},
	{Kind: "climb", What: "the ladder to the second floor", Stand: tile.Tile{X: 2933, Z: 3254, Level: 1}, LadderAt: tile.Tile{X: 2934, Z: 3254, Level: 1}, Op: "Climb-up", Land: tile.Tile{X: 2934, Z: 3254, Level: 2}},
	{Kind: "kill", What: "the skeleton", NpcID: 750, At: tile.Tile{X: 2923, Z: 3251, Level: 2}, KeyID: 1545},
	{Kind: "door", What: "the yellow door", Stand: tile.Tile{X: 2924, Z: 3250, Level: 2}, Door: tile.Tile{X: 2924, Z: 3249, Level: 2}, KeyID: 1545, Land: tile.Tile{X: 2924, Z: 3249, Level: 2}},
	{Kind: "climb", What: "the ladder back to the first floor", Stand: tile.Tile{X: 2939, Z: 3240, Level: 2}, LadderAt: tile.Tile{X: 2940, Z: 3240, Level: 2}, Op: "Climb-down", Land: tile.Tile{X: 2940, Z: 3240, Level: 1}},
	{Kind: "climb", What: "the ladder back to the ground floor", Stand: tile.Tile{X: 2938, Z: 3240, Level: 1}, LadderAt: tile.Tile{X: 2937, Z: 3240, Level: 1}, Op: "Climb-down", Land: tile.Tile{X: 2937, Z: 3240}},
	{Kind: "climb", What: "the ladder to the basement", Stand: tile.Tile{X: 2933, Z: 3240}, LadderAt: tile.Tile{X: 2932, Z: 3240}, Op: "Climb-down", Land: tile.Tile{X: 2933, Z: 9640}},
	{Kind: "kill", What: "the zombie", NpcID: 751, At: tile.Tile{X: 2932, Z: 9642}, KeyID: 1546},
	{Kind: "door", What: "the blue door", Stand: tile.Tile{X: 2931, Z: 9643}, Door: tile.Tile{X: 2931, Z: 9643}, KeyID: 1546, Land: tile.Tile{X: 2930, Z: 9643}},
	{Kind: "kill", What: "Melzar the mad", NpcID: 753, At: tile.Tile{X: 2929, Z: 9649}, KeyID: 1547},
	{Kind: "door", What: "the magenta door", Stand: tile.Tile{X: 2929, Z: 9651}, Door: tile.Tile{X: 2929, Z: 9652}, KeyID: 1547, Land: tile.Tile{X: 2929, Z: 9652}},
	{Kind: "kill", What: "the lesser demon", NpcID: 752, At: tile.Tile{X: 2936, Z: 9650}, KeyID: 1548},
	{Kind: "door", What: "the green door", Stand: tile.Tile{X: 2936, Z: 9655}, Door: tile.Tile{X: 2936, Z: 9655}, KeyID: 1548, Land: tile.Tile{X: 2936, Z: 9656}},
	{Kind: "chest", What: "the map chest", Stand: tile.Tile{X: 2935, Z: 9656}},
}

// InMelzarsMaze is anywhere inside the maze, on any of its four floors.
func InMelzarsMaze(t tile.Tile) bool {
	return (t.X >= 2920 && t.X <= 2940 && t.Z >= 3236 && t.Z <= 3262) || (t.X >= 2915 && t.X <= 2945 && t.Z >= 9630 && t.Z <= 9665)
}

// MazeLegFromTile is where an interrupted maze run picks up, from the tile alone.
func MazeLegFromTile(t tile.Tile) int {
	switch {
	case !InMelzarsMaze(t):
		return 0
	case t.Z >= 9600:
		return 12
	case t.Level == 2:
		return 7
	case t.Level == 1:
		return 4
	case t.Z <= 3242 && t.X >= 2929:
		return 11
	}
	return 1
}

// Tiles (TS areas.ts, from the engine's own map spawns).
var (
	GuildmasterAnchor = tile.Tile{X: 3191, Z: 3362}
	OziachAnchor      = tile.Tile{X: 3069, Z: 3517}
	DukeAnchor        = tile.Tile{X: 3212, Z: 3220, Level: 1}
	OracleAnchor      = tile.Tile{X: 3015, Z: 3504}
	WormbrainAnchor   = tile.Tile{X: 3014, Z: 3186}
	WormbrainStand    = tile.Tile{X: 3012, Z: 3186}
	KlarenseAnchor    = tile.Tile{X: 3047, Z: 3204}
	NedAnchor         = tile.Tile{X: 3100, Z: 3258}
	NedAboardAnchor   = tile.Tile{X: 3046, Z: 9640, Level: 3}
	MagicDoorWest     = tile.Tile{X: 3050, Z: 9840}
	MagicDoorTile     = tile.Tile{X: 3051, Z: 9840}
	MagicDoorEast     = tile.Tile{X: 3052, Z: 9840}
	OracleChestTile   = tile.Tile{X: 3057, Z: 9841}
	OracleChestStand  = tile.Tile{X: 3057, Z: 9842}
	GangplankStand    = tile.Tile{X: 3047, Z: 3204}
	ShipHoleTile      = tile.Tile{X: 3047, Z: 9639, Level: 1}
	CrandorRock       = tile.Tile{X: 2833, Z: 3255}
	SecretDoorTile    = tile.Tile{X: 2836, Z: 9600}
	SecretWallKaramja = tile.Tile{X: 2836, Z: 9599}
	ElvargGateStand   = tile.Tile{X: 2846, Z: 9637}
	ElvargGateInside  = tile.Tile{X: 2847, Z: 9637}
	MazeCellarLadder  = tile.Tile{X: 2939, Z: 9657}
	MazeCellarStand   = tile.Tile{X: 2938, Z: 9657}
	MazeExitDoor      = tile.Tile{X: 2938, Z: 3252}
	// VarrockAnvil is Varrock west's anvil beside the bank (loc 2783 at 3188,3424). It replaces
	// an earlier "Falador" tile at 3012,9812, which is underground in the Dwarven Mine.
	VarrockAnvil    = tile.Tile{X: 3188, Z: 3424}
	GerrantShop     = ShopRef{Npc: "Gerrant", Anchor: tile.Tile{X: 3013, Z: 3225}}
	FaladorGeneral  = ShopRef{Npc: "Shop keeper", Anchor: tile.Tile{X: 2955, Z: 3389}}
	DraynorBankTile = tile.Tile{X: 3093, Z: 3243}
	dragonFoods     = []int{385, 373, 379, 365, 361, 329, 333}
	dragonTools     = []int{CoinsID, ObjMazeKey, 1543, 1544, 1545, 1546, 1547, 1548, ObjMapMelzar, ObjMapWormbrain, ObjMapOracle,
		ObjCrandorMap, ObjNails, ObjPlank, ObjHammer, ObjAntiDragon, ObjMindBomb, ObjUnfiredBowl, ObjLobsterPot, ObjSilk,
		ObjSteelBar, 554, 556, 558, 562, 1381}
)

// OnLadyLumbridge is aboard the ship: the deck or a hold.
func OnLadyLumbridge(t tile.Tile) bool {
	deck := t.Level >= 1 && t.X >= 3044 && t.X <= 3052 && t.Z >= 3204 && t.Z <= 3212
	hold := t.X >= 3040 && t.X <= 3055 && t.Z >= 9630 && t.Z <= 9650
	return deck || hold
}

// OnCrandor is the island or its dungeon; InElvargLair is her lair.
func OnCrandor(t tile.Tile) bool {
	dungeon := t.X >= 2810 && t.X <= 2880 && t.Z >= 9600 && t.Z <= 9670
	surface := t.X >= 2810 && t.X <= 2880 && t.Z >= 3225 && t.Z <= 3300
	return dungeon || surface
}

// InElvargLair is her lair.
func InElvargLair(t tile.Tile) bool { return t.X >= 2847 && t.X <= 2863 && t.Z >= 9628 && t.Z <= 9646 }

var dragonFlagLines = []struct {
	needle string
	flag   int
}{
	{"i should return to oziach for more detailed instructions", DragonNeedsBriefing},
	{"the map's behind a door below", DragonAskedOracle},
	{"gave me an anti-dragonbreath", DragonHasShield},
	{"i have repaired my ship using wooden planks", DragonShipRepaired},
	{"has agreed to sail", DragonNedHired},
	{"i have found the secret passage", DragonSecretPassage},
}

// ParseDragonSlayerJournal reads dragon_journal.rs2 into the packed stage (TS journal.ts).
func ParseDragonSlayerJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	st := -1
	switch {
	case strings.Contains(text, "quest complete!"):
		st = DragonComplete
	case strings.Contains(text, "all i need to do is kill the"):
		st = DragonSailed
	case strings.Contains(text, "set sail for"):
		st = DragonNedMap
	case strings.Contains(text, "i still need to find a captain"):
		st = DragonShip
		if strings.Contains(text, "i have repaired my ship using wooden planks") {
			st = DragonRepaired
		}
	case strings.Contains(text, "i should see if there is a ship for sale"),
		strings.Contains(text, "i should return to oziach for more detailed instructions"):
		st = DragonOziach
	case strings.Contains(text, "i should speak to oziach"):
		st = DragonGuildmaster
	case strings.Contains(text, "i can start this quest"):
		st = 0
	}
	if st < 0 {
		return 0, false
	}
	flags := 0
	for _, f := range dragonFlagLines {
		if strings.Contains(text, f.needle) {
			flags |= f.flag
		}
	}
	return st + 100*flags, true
}

// Custom step names (internal/scripts/questmods).
const (
	CustomDragonOziach    = "dragon-oziach"
	CustomDragonMaze      = "dragon-maze"
	CustomDragonLeaveMaze = "dragon-leave-maze"
	CustomDragonOracle    = "dragon-oracle-chest"
	CustomDragonWormbrain = "dragon-wormbrain"
	CustomDragonCombine   = "dragon-combine"
	CustomDragonAshore    = "dragon-ashore"
	CustomDragonRepair    = "dragon-repair"
	CustomDragonBoard     = "dragon-board"
	CustomDragonPassage   = "dragon-passage"
	CustomDragonElvarg    = "dragon-elvarg"
	KitDragonOracle       = "dragon-oracle-kit"
	KitDragonCoins        = "dragon-coins"
	KitDragonShip         = "dragon-ship-kit"
	KitDragonElvarg       = "dragon-elvarg-kit"
)

var (
	GuildmasterStop = NpcStop{Npc: "Guild master", ID: NpcGuildmaster, Anchor: GuildmasterAnchor, Leash: 8,
		Prefer: []string{"Do you know where I could get a Rune Plate mail body?"}}
	DukeShieldStop = NpcStop{Npc: "Duke Horacio", ID: NpcDuke, Anchor: DukeAnchor, Leash: 6,
		Prefer: []string{"I seek a shield that will protect me from the dragon's breath."}}
	OracleStop = NpcStop{Npc: "Oracle", ID: NpcOracle, Anchor: OracleAnchor, Leash: 6,
		Prefer: []string{"I seek a piece of the map to the island of Crandor."}}
	KlarenseStop = NpcStop{Npc: "Klarense", ID: NpcKlarense, Anchor: KlarenseAnchor, Leash: 6,
		Prefer: []string{"I don't suppose I could buy it?", "Yep, sounds good."}}
	NedHireStop = NpcStop{Npc: "Ned", ID: NpcNed, Anchor: NedAnchor, Leash: 6, Prefer: []string{
		"You're a sailor? Could you take me to the island of Crandor?", "So are you going to take me to Crandor Island now then?"}}
	NedAboardStop = NpcStop{Npc: "Ned", ID: NpcNedAboard, Anchor: NedAboardAnchor, Leash: 8, Prefer: []string{"Yep lets go!"}}
)

func dragonOwns(s Snapshot, id int) bool {
	if s.HeldID(id) > 0 || s.WornIDs[id] > 0 {
		return true
	}
	n, _ := s.BankedID(id)
	return n > 0
}

// DecideDragonSlayer is the whole quest (TS index.ts decide, with the item rule for the kits).
func DecideDragonSlayer(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: GuildmasterStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	st, flags := DragonTracks(s.Stage)
	held := func(id int) bool { return s.HeldID(id) > 0 }
	keep := append(append([]int(nil), dragonTools...), dragonFoods...)
	here := s.TileKnown
	aboard := here && OnLadyLumbridge(s.Tile)
	withdraw := func(id int, name string, n int) Step {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: name, ID: id, Qty: n}}, KeepIDs: keep,
			Bank: NearestBank(s.Tile).Tile, BankSet: here}
	}
	coins := func(n int, why string) (Step, bool) {
		if s.HeldID(CoinsID) >= n || s.Failed["provision:"+KitDragonCoins] {
			return Step{}, false
		}
		return Step{Kind: StepProvision, KitName: KitDragonCoins, KeepIDs: keep, Bank: NearestBank(s.Tile).Tile, BankSet: here,
			Kit: []KitLine{{ID: CoinsID, Name: "Coins", Qty: n, Stackable: true}}, Reason: why}, true
	}
	gearUp := func() (Step, bool) {
		if s.WornIDs[ObjAntiDragon] == 0 {
			if held(ObjAntiDragon) {
				return Step{Kind: StepEquip, ItemID: ObjAntiDragon, Item: "Anti-dragon shield"}, true
			}
			if n, _ := s.BankedID(ObjAntiDragon); n > 0 {
				return withdraw(ObjAntiDragon, "Anti-dragon shield", 1), true
			}
			return Step{Kind: StepTalk, Stop: DukeShieldStop}, true
		}
		if !s.Ran["provision:"+KitDragonElvarg] && !s.Failed["provision:"+KitDragonElvarg] {
			return Step{Kind: StepProvision, KitName: KitDragonElvarg, KeepIDs: keep, Bank: NearestBank(s.Tile).Tile, BankSet: here,
				Kit: []KitLine{{ID: 385, Name: "Shark", Qty: 18, Optional: true}, {ID: 373, Name: "Swordfish", Qty: 18, Optional: true},
					{ID: 379, Name: "Lobster", Qty: 18, Optional: true}}, Reason: "food for Elvarg (no bank on Crandor)"}, true
		}
		return Step{}, false
	}
	switch st {
	case DragonGuildmaster:
		return Step{Kind: StepCustom, Custom: CustomDragonOziach, Reason: "Oziach, the rune plate chain and the briefing"}
	case DragonOziach:
		if aboard {
			return Step{Kind: StepCustom, Custom: CustomDragonAshore, Reason: "ashore first"}
		}
		if here && InMelzarsMaze(s.Tile) && dragonOwns(s, ObjMapMelzar) {
			return Step{Kind: StepCustom, Custom: CustomDragonLeaveMaze, Reason: "out of Melzar's Maze"}
		}
		if flags&DragonNeedsBriefing != 0 {
			return Step{Kind: StepCustom, Custom: CustomDragonOziach, Reason: "the rest of Oziach's briefing"}
		}
		if !held(ObjMazeKey) && !held(ObjCrandorMap) && !dragonOwns(s, ObjMapMelzar) {
			if n, _ := s.BankedID(ObjMazeKey); n > 0 {
				return withdraw(ObjMazeKey, "Maze key", 1)
			}
			if !s.BankKnown {
				return Step{Kind: StepScanBank, Bank: NearestBank(s.Tile).Tile, BankSet: here}
			}
			return Step{Kind: StepCustom, Custom: CustomDragonOziach, Reason: "Oziach for the maze key"}
		}
		if !dragonOwns(s, ObjCrandorMap) {
			for _, id := range []int{ObjMapMelzar, ObjMapOracle, ObjMapWormbrain} {
				if n, _ := s.BankedID(id); n > 0 && !held(id) {
					return withdraw(id, "Map part", 1)
				}
			}
			switch {
			case !held(ObjMapMelzar):
				return Step{Kind: StepCustom, Custom: CustomDragonMaze, Reason: "Melzar's Maze for the first map piece"}
			case !held(ObjMapOracle):
				charms := held(ObjMindBomb) && held(ObjUnfiredBowl) && held(ObjLobsterPot) && held(ObjSilk)
				pastDoor := here && s.Tile.Z >= 9800 && s.Tile.X >= MagicDoorTile.X && s.Tile.X <= 3065
				if !charms && !pastDoor && !s.Failed["provision:"+KitDragonOracle] {
					return Step{Kind: StepProvision, KitName: KitDragonOracle, KeepIDs: keep, Bank: NearestBank(s.Tile).Tile, BankSet: here,
						Kit: []KitLine{
							{ID: ObjMindBomb, Name: "Wizard's mind bomb", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: TostigShop, Price: 3}}},
							{ID: ObjUnfiredBowl, Name: "Unfired bowl", Qty: 1},
							{ID: ObjLobsterPot, Name: "Lobster pot", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: GerrantShop, Price: 20}}},
							{ID: ObjSilk, Name: "Silk", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: ThessaliaShop, Price: 30}}},
						}, Reason: "the four charms for the Oracle's magic door"}
				}
				if charms && flags&DragonAskedOracle == 0 && !pastDoor {
					return Step{Kind: StepTalk, Stop: OracleStop}
				}
				return Step{Kind: StepCustom, Custom: CustomDragonOracle, Reason: "the chest under Ice Mountain"}
			case !held(ObjMapWormbrain):
				if step, ok := coins(WormbrainPrice+200, "10,000 coins for Wormbrain's map piece"); ok {
					return step
				}
				return Step{Kind: StepCustom, Custom: CustomDragonWormbrain, Reason: "Wormbrain's map piece"}
			}
			return Step{Kind: StepCustom, Custom: CustomDragonCombine, Reason: "join the three map pieces"}
		}
		if flags&DragonHasShield == 0 && !dragonOwns(s, ObjAntiDragon) {
			return Step{Kind: StepTalk, Stop: DukeShieldStop}
		}
		if step, ok := coins(ShipPrice+200, "2,000 coins for the Lady Lumbridge"); ok {
			return step
		}
		return Step{Kind: StepTalk, Stop: KlarenseStop}
	case DragonShip:
		planks := s.HeldID(ObjPlank)
		if planks == 0 {
			planks = 3
		}
		if flags&DragonShipRepaired == 0 {
			if s.HeldID(ObjNails) < 4*planks || s.HeldID(ObjPlank) == 0 || !held(ObjHammer) {
				if aboard {
					return Step{Kind: StepCustom, Custom: CustomDragonAshore, Reason: "ashore for the repair kit"}
				}
				if !s.Failed["provision:"+KitDragonShip] {
					return Step{Kind: StepProvision, KitName: KitDragonShip, KeepIDs: keep, Bank: NearestBank(s.Tile).Tile, BankSet: here,
						Kit: append([]KitLine{
							{ID: ObjPlank, Name: "Plank", Qty: planks, Sources: []Source{
								{Kind: SourceGround, Tile: tile.Tile{X: 2552, Z: 3574}, RespawnTicks: 100},
								{Kind: SourceGround, Tile: tile.Tile{X: 2553, Z: 3575}, RespawnTicks: 100},
								{Kind: SourceGround, Tile: tile.Tile{X: 2554, Z: 3574}, RespawnTicks: 100},
								{Kind: SourceGround, Tile: tile.Tile{X: 2556, Z: 3573}, RespawnTicks: 100},
							}},
							{ID: ObjNails, Name: "Nails", Qty: 4 * planks, Stackable: true, Sources: []Source{
								{Kind: SourceSkill, Skill: "smithing", Level: 34, Method: MethodSmithNails, Tile: VarrockAnvil},
							}},
							{ID: ObjHammer, Name: "Hammer", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: FaladorGeneral, Price: 1}}},
						}, NailBars(4*planks, s.HeldID(ObjNails), s.HeldID(ObjSteelBar))...), Reason: "planks, nails and a hammer for the hull"}
				}
				return Step{Kind: StepWait, Reason: "the ship repair kit could not be filled (nails need Smithing 34 or the bank)"}
			}
			return Step{Kind: StepCustom, Custom: CustomDragonRepair, Reason: "patch the Lady Lumbridge"}
		}
		if aboard {
			return Step{Kind: StepCustom, Custom: CustomDragonAshore, Reason: "ashore to hire Ned"}
		}
		return Step{Kind: StepTalk, Stop: NedHireStop}
	case DragonRepaired:
		if aboard {
			return Step{Kind: StepCustom, Custom: CustomDragonAshore, Reason: "ashore to see Ned"}
		}
		return Step{Kind: StepTalk, Stop: NedHireStop}
	case DragonNedMap:
		if here && s.Tile.Level >= 3 {
			return Step{Kind: StepTalk, Stop: NedAboardStop}
		}
		if !aboard {
			if step, ok := gearUp(); ok {
				return step
			}
		}
		return Step{Kind: StepCustom, Custom: CustomDragonBoard, Reason: "board the Lady Lumbridge for Crandor"}
	case DragonSailed:
		if !(here && OnCrandor(s.Tile)) {
			if step, ok := gearUp(); ok {
				return step
			}
			if step, ok := coins(100, "the Karamja fares back to the secret wall"); ok {
				return step
			}
		}
		if flags&DragonSecretPassage == 0 && !s.Ran[CustomDragonPassage] {
			return Step{Kind: StepCustom, Custom: CustomDragonPassage, Reason: "open the Karamja secret wall from Crandor first"}
		}
		return Step{Kind: StepCustom, Custom: CustomDragonElvarg, Reason: "kill Elvarg"}
	}
	return Step{Kind: StepWait, Reason: "dragon slayer: unrecognised stage"}
}

// DragonSlayerRecord is the quest's identity: the Champions' Guild door's 32 quest points.
var DragonSlayerRecord = Record{ID: DragonSlayerID, Name: DragonSlayerName, QuestPoints: 2,
	Requirements: Requirements{MinQuestPoints: 32}}

// DragonSlayer is the engine module.
var DragonSlayer = Module{
	Record: DragonSlayerRecord, Bank: DraynorBankTile, BankSet: true, Anchor: GuildmasterAnchor,
	ToolIDs:   dragonTools,
	ReadStage: ParseDragonSlayerJournal,
	Sustain:   Sustain{FoodIDs: dragonFoods, EatAt: 25},
	MaxDeaths: 3,
	Decide:    DecideDragonSlayer,
}

// NailBars is the steel-bar line a kit that needs nails carries: nails have no shop, spawn or
// mule stock, so the smith-nails skill source works steel bars (two nails each) that must come
// from the bank or the kit's ONE bundled mule ask. It is empty when the bars held already cover
// the nails still short.
func NailBars(nails, heldNails, heldBars int) []KitLine {
	short := nails - heldNails
	if short <= 0 {
		return nil
	}
	bars := (short+1)/2 - heldBars
	if bars <= 0 {
		return nil
	}
	return []KitLine{{ID: ObjSteelBar, Name: "Steel bar", Qty: bars, Optional: true}}
}
