package questplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Clock Tower (content id `cog`, %cogquest, not transmitted), ported from the TS def (defs/clocktower.ts) and checked against
// content 289: quests/quest_cog/{configs,scripts}, areas/area_ardougne_east/scripts/brother_kojo.rs2, areas/area_taverly/dungeon/
// scripts/jail_doors.rs2, doors/scripts/{open_and_close_doors,door_procs}.rs2, general_use/scripts/water_sources.rs2 and the
// maps (m40_50, m40_150). One quest point, 500 coins, no experience.
//
// Brother Kojo (2569,3249, south of East Ardougne) wants the clock's four cogs back on their spindles, one on each floor of the
// tower. The quest's state is %cogquest (bits 0-3 the progress, 0 not started, 1 tasked, 2-4 the cogs still to place, 5 all four
// placed, 8 complete; bit 4 the rat door) and %cog_bits (a perm varp: one bit per colour that is placed, one for the black
// cog being cooled). The journal draws each cog's line struck through once it is placed, which is the only place the colours
// are readable (ParseClockTowerJournal).
//
//	colour  cog lies (m40_150 OBJ)      spindle (loc, m40_50/m40_150)        the way there, and what is on it
//	blue    2574,9633 inside a sealed   blue 32 at 2569,3240 on level 1      the room is entered by pushing loc 1586 (Wall) at
//	        18-tile room                                                      2575,9631 from 2576,9631, a tile of the south-east
//	                                                                          corridor system that only the ladder at 2621,3262
//	                                                                          reaches; the room's own ladder (2572,9631) climbs to
//	                                                                          2572,3232. A dungeon rat (level 12) lives in it.
//	black   2613,9639 beside twelve     black 30 at 2570,9642 in the cellar  three doors from the cellar's foot. RED HOT:
//	        giant spiders (level 2)                                          Take refuses ("The cog is red hot from flames") until
//	                                                                          it is cooled, by ice gloves worn or a bucket of water
//	                                                                          used on it ([opobju,blackcog]); the cooled bit is
//	                                                                          perm, so the water is spent once.
//	white   2577,9655 in a 10-tile      white 31 at 2567,3241 on level 2     through the rat pen: the lever at 2591,9661 (loc 33;
//	        room west of the rat pen                                          loc 34 beside it does nothing) opens the pen gate
//	                                                                          (loc 37 at 2595,9657) for 500 ticks, rat poison on the
//	                                                                          food trough (loc 40) sets the rat-door bit, and the
//	                                                                          gate to the white room (loc 39 at 2579,9656, "Go-through")
//	                                                                          opens only then. The room's ladder (2575,9655) climbs
//	                                                                          to 2575,3256.
//	red     2583,9613 at the west end   red 29 at 2568,3243 on level 0       inside the OGRE cave: three ogres (level 53, 60
//	        of the ogres' cave                                                hitpoints, hit up to 7 every 6 ticks) stand within 3
//	                                                                          tiles of every route in, and the cog is a dead end.
//
// What the content does that a script must plan for:
//   - The cogs are too heavy to carry together ("inv_totalcat(inv, cog) > 0"), they cannot be taken before Kojo has started the
//     quest, and a cog whose spindle is already filled ("You have already placed a cog here.") stays in the pack: the module
//     drops it. The spindles are named alike on every floor (decoys 25-28 beside the working one), so every use is by loc id.
//   - Every cog, the rat poison (obj 24, 2564,9662) and the bucket's spawn are single ground spawns that come back 100 ticks
//     after a pickup: the quest holds a fleet-wide slot of four (questrunner/slots.go) and waits for a respawn.
//   - The pen's lever and gate are the world's (loc_add for 500 ticks), not the player's: another account's open gate is this
//     one's, and its closing leaves the account inside if the gate is shut again (Open from inside works; the module's
//     pocket for the pen does it). Pulling loc 35, the lever once pulled, shuts the gate again.
//   - The ogres are huntmode cowardly with huntrange 2 (all.hunt: they skip a player of more than twice their level, 106, and a
//     player or an ogre in combat within 8 ticks, so one ogre fights at a time) and a 6-tick attack. The red cog is fetched with
//     no retaliation, from a staging tile at the cave's mouth, after waiting for the ogres to stand off the cog, with food in
//     the pack, and its death cap is one a day.
//   - The black cog needs a bucket of water: from the bank, the mule, or an empty bucket (the ground spawn at 2616,3255, the
//     bank, the mule, Port Khazard's store) used on the well at 2612,3254.

// ClockTowerName is the quest tab's spelling (component 7353, `questlist:cog`).
const ClockTowerName = "Clock Tower"

// ClockTowerID is the engine's id.
const ClockTowerID = "cog"

// Journal stages. A started quest's stage is the bit mask of the colours placed (CogBitBlue ...); the two endpoints are named.
const (
	CogStageNotStarted = -1
	CogAllPlaced       = 100
	CogStageComplete   = 101
)

// The colours' bits in a started quest's stage.
const (
	CogBitBlue  = 1
	CogBitBlack = 2
	CogBitWhite = 4
	CogBitRed   = 8
)

// Ids from content/pack (checked against the production config by TestMonkCogIdsMatchTheProductionConfig). The bucket of water, 1929,
// is ObjBucketOfWater of princeali.go: one name for the one object in the package.
const (
	NpcBrotherKojo    = 223
	NpcOgre           = 114 // [ogre]: level 53, size 2, wanderrange 3, maxrange 5, huntrange 2, attackrate 6
	ObjCogWhite       = 20
	ObjCogBlack       = 21
	ObjCogBlue        = 22
	ObjCogRed         = 23
	ObjRatPoison      = 24
	LocSpindleRed     = 29 // [brokeclockpole_red]: the one that takes a cog on its floor (25-28 are the look-alikes)
	LocSpindleBlack   = 30
	LocSpindleWhite   = 31
	LocSpindleBlue    = 32
	LocCogLever       = 33 // [ctlevera], op Pull: opens the pen's gate
	LocCogLeverDummy  = 34 // [ctleverb]: does nothing
	LocCogLeverPulled = 35 // [ctlevera2], op Pull: shuts the gate again
	LocPenGate        = 37 // [ctratgatea], op Open
	LocRatGate        = 39 // [ctratgatec], op Go-through
	LocFoodTrough     = 40 // [ctfoodtrough], 2x1
	LocCogSecretWall  = 1586
	LocWell           = 884 // [well], category well: takes a bucket
)

// CogColour is one cog: its bit in the stage, the object, the working spindle's loc, where the cog is fitted from, and where it lies.
type CogColour struct {
	Name    string
	Bit     int
	Obj     int
	Spindle int
	// Stand is a tile beside the spindle, on its floor.
	Stand tile.Tile
	// Spawn is the cog's ground spawn.
	Spawn tile.Tile
}

// CogColours is the order the cogs are fetched in: blue (the east corridor system, nothing hostile), black (the cellar, a
// bucket), white (the pen, the lever, the poison), and red (the ogres) last, so that an account the ogres turn back has
// everything else done.
var CogColours = []CogColour{
	{Name: "blue", Bit: CogBitBlue, Obj: ObjCogBlue, Spindle: LocSpindleBlue,
		Stand: tile.Tile{X: 2570, Z: 3240, Level: 1}, Spawn: tile.Tile{X: 2574, Z: 9633}},
	{Name: "black", Bit: CogBitBlack, Obj: ObjCogBlack, Spindle: LocSpindleBlack,
		Stand: tile.Tile{X: 2571, Z: 9642}, Spawn: tile.Tile{X: 2613, Z: 9639}},
	{Name: "white", Bit: CogBitWhite, Obj: ObjCogWhite, Spindle: LocSpindleWhite,
		Stand: tile.Tile{X: 2567, Z: 3242, Level: 2}, Spawn: tile.Tile{X: 2577, Z: 9655}},
	{Name: "red", Bit: CogBitRed, Obj: ObjCogRed, Spindle: LocSpindleRed,
		Stand: tile.Tile{X: 2569, Z: 3243}, Spawn: tile.Tile{X: 2583, Z: 9613}},
}

// CogByName is the colour called name.
func CogByName(name string) (CogColour, bool) {
	for _, c := range CogColours {
		if c.Name == name {
			return c, true
		}
	}
	return CogColour{}, false
}

// CogByObj is the colour whose cog is object id.
func CogByObj(id int) (CogColour, bool) {
	for _, c := range CogColours {
		if c.Obj == id {
			return c, true
		}
	}
	return CogColour{}, false
}

// CogPlaced reports whether a started quest's stage says colour c is on its spindle.
func CogPlaced(stage int, c CogColour) bool {
	return stage >= 0 && stage < CogAllPlaced && stage&c.Bit != 0
}

// NextCog is the first colour (in CogColours' order) a started quest's stage says is not placed yet.
func NextCog(stage int) (CogColour, bool) {
	for _, c := range CogColours {
		if !CogPlaced(stage, c) {
			return c, true
		}
	}
	return CogColour{}, false
}

// HeldCog is the colour of the cog in the pack, if one is.
func HeldCog(s Snapshot) (CogColour, bool) {
	for _, c := range CogColours {
		if s.HeldID(c.Obj) > 0 {
			return c, true
		}
	}
	return CogColour{}, false
}

// Tiles from content maps and the loc index.
var (
	// KojoAnchor is Brother Kojo (m40_50, 2569,3249).
	KojoAnchor = tile.Tile{X: 2569, Z: 3249}
	// PoisonSpawn is the rat poison (m40_150 "0 4 62: 24").
	PoisonSpawn = tile.Tile{X: 2564, Z: 9662}
	// CogLeverTile is lever A, the one that works, a wall decoration in the single-row corridor north of the pen; the dummy lever
	// B is at 2593,9661.
	CogLeverTile = tile.Tile{X: 2591, Z: 9661}
	// PenGateTile is the pen gate's own tile (inside the pen) and PenGateOutside the tile east of it, outside.
	PenGateTile    = tile.Tile{X: 2595, Z: 9657}
	PenGateOutside = tile.Tile{X: 2596, Z: 9657}
	// TroughStand is a tile beside the food trough (2586,9654).
	TroughStand = tile.Tile{X: 2585, Z: 9654}
	// RatGateTile is the white room's gate: the tile east of it is the pen's, the one west the room's.
	RatGateTile = tile.Tile{X: 2579, Z: 9656}
	// CogSecretWallStand is the tile east of the secret wall (2575,9631): pushed from here it lands the player in the blue room.
	CogSecretWallStand = tile.Tile{X: 2576, Z: 9631}
	// WellStand is a tile beside the well (2612,3254, 2x2) and CogBucketSpawn the bucket on the ground by Cedric's cart.
	WellStand      = tile.Tile{X: 2611, Z: 3254}
	CogBucketSpawn = tile.Tile{X: 2616, Z: 3255}
	// RedCogStage is the tile at the ogre cave's mouth the account waits at: the room's north-east corner, 6 or more tiles from
	// every ogre's spawn.
	RedCogStage = tile.Tile{X: 2594, Z: 9615}
)

// InCogPen reports a tile in the rat pen, the gate tile included: the 116 tiles the planner reaches from the trough on foot, which
// are x 2579-2595 for z 9655-9660 and a step of the pen's south side below (z 9654 from x 2584, z 9653 from x 2592, z 9652 from
// x 2593). It is a pocket for every walk: once the gate has shut, the only way out is Open from inside. (The tiles at 2579-2580,
// 9652 beside it are the cellar's corridor.)
func InCogPen(t tile.Tile) bool {
	if t.Level != 0 || t.X < 2579 || t.X > 2595 {
		return false
	}
	switch {
	case t.Z >= 9655 && t.Z <= 9660:
		return true
	case t.Z == 9654:
		return t.X >= 2584
	case t.Z == 9653:
		return t.X >= 2592
	case t.Z == 9652:
		return t.X >= 2593
	}
	return false
}

// InCogWhiteRoom reports a tile in the white cog's room (x 2574-2578, z 9655-9658). Its ladder is in the walk graph.
func InCogWhiteRoom(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2574 && t.X <= 2578 && t.Z >= 9655 && t.Z <= 9658
}

// InCogBlueRoom reports a tile in the blue cog's room (x 2571-2575, z 9630-9633). Its ladder is in the walk graph.
func InCogBlueRoom(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2571 && t.X <= 2575 && t.Z >= 9630 && t.Z <= 9633
}

// OgreDistance is the Chebyshev distance from tile p to the footprint of an ogre (size 2) standing at ogre, its south-west
// tile, 0 when p is under it.
func OgreDistance(ogre, p tile.Tile) int {
	gap := func(v, lo, hi int) int {
		switch {
		case v < lo:
			return lo - v
		case v > hi:
			return v - hi
		}
		return 0
	}
	dx, dz := gap(p.X, ogre.X, ogre.X+1), gap(p.Z, ogre.Z, ogre.Z+1)
	if dz > dx {
		return dz
	}
	return dx
}

// OgreCalm reports that no ogre stands within near tiles of p: an ogre notices a player within its huntrange (2) of its
// footprint, and one more step is the margin for the tick it takes to look.
func OgreCalm(ogres []tile.Tile, p tile.Tile, near int) bool {
	for _, o := range ogres {
		if OgreDistance(o, p) <= near {
			return false
		}
	}
	return true
}

// OgreCalmNear is how far from the red cog every ogre must stand before the account goes in for it.
const OgreCalmNear = 3

// CogRedFoodHeal is the hitpoints of food the pack holds before the red cog's trip: two sharks, three times an ogre's hardest
// hit (7) over: the ogres hit one at a time (a player in combat is skipped by the others for 8 ticks), every 6 ticks, and the
// trip in and out is some 25 ticks.
const CogRedFoodHeal = 40

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomCogBlue  = "cog-blue"
	CustomCogBlack = "cog-black"
	CustomCogWhite = "cog-white"
	CustomCogRed   = "cog-red"
	CustomCogPlace = "cog-place"

	KitCogFood    = "cog-food"
	KitCogFoodRed = "cog-food-red"
	KitCogBucket  = "cog-bucket"
)

// cogFoodIDs are the foods the quest eats (Module.Sustain; the eat loop's floor bite takes any food the pack carries).
var cogFoodIDs = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}

// cogKeep is what every bank visit of the quest leaves in the pack: the cogs, the poison, the bucket and its water, coins and food.
var cogKeep = append([]int{ObjCogWhite, ObjCogBlack, ObjCogBlue, ObjCogRed, ObjRatPoison, ObjBucket, ObjBucketOfWater, CoinsID}, cogFoodIDs...)

// CogKeepIDs returns the keep list (a copy).
func CogKeepIDs() []int { return append([]int(nil), cogKeep...) }

// KojoStop starts the quest (cog_start_quest: "OK old monk, what can I do?") and takes the news that all four cogs are placed
// (no menu). The menu after "How much reward are we talking?" repeats, so the first preference answers it.
var KojoStop = NpcStop{Npc: "Brother Kojo", ID: NpcBrotherKojo, Anchor: KojoAnchor, Leash: 6,
	Prefer: []string{"OK old monk, what can I do?"}}

// cogJournalColours are the colours' words in the journal's per-cog lines.
var cogJournalColours = []struct {
	word string
	bit  int
}{{"blue", CogBitBlue}, {"black", CogBitBlack}, {"white", CogBitWhite}, {"red", CogBitRed}}

// ParseClockTowerJournal reads the stage from the rendered journal (cog_journal.rs2): CogStageNotStarted, CogAllPlaced,
// CogStageComplete, or for a started quest the mask of the colours whose line says "successfully placed". ok is false for an
// empty or unrecognised journal, which is unknown, never stage 0.
func ParseClockTowerJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return CogStageComplete, true
	// The reward page drops the four per-cog lines, so its own line is the only evidence they are all in.
	case strings.Contains(text, "i have placed all four cogs successfully"):
		return CogAllPlaced, true
	case strings.Contains(text, "agreed to help him repair the clock"):
		mask := 0
		for _, c := range cogJournalColours {
			if strings.Contains(text, "successfully placed the "+c.word+" cog") {
				mask |= c.bit
			}
		}
		return mask, true
	case strings.Contains(text, "i can start this quest by talking to brother kojo"):
		return CogStageNotStarted, true
	}
	return 0, false
}

// cogFoodKit is the food the quest's first pass asks for, optional: three sharks, from the bank or the mule.
func cogFoodKit() []KitLine {
	return []KitLine{{ID: ObjShark, Name: "Shark", Qty: 3, Optional: true}}
}

// cogBucketKit is the bucket of water the black cog is cooled with, or the empty bucket it is filled from: the bank and the mule
// first, then the bucket on the ground by Cedric's cart, then Port Khazard's store.
func cogBucketKit() []KitLine {
	return []KitLine{
		{ID: ObjBucketOfWater, Name: "Bucket of water", Qty: 1, Optional: true},
		{ID: ObjBucket, Name: "Bucket", Qty: 1, OnlyIfShort: ObjBucketOfWater, Sources: []Source{
			{Kind: SourceGround, Tile: CogBucketSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the bucket by Brother Cedric's cart"},
			{Kind: SourceShop, Shop: KhazardGeneralStore, Price: 2, Note: "Port Khazard's general store keeps 2 buckets"},
		}},
	}
}

// cogWaterStep makes the pack hold a bucket of water for the black cog: fill the bucket at the well, else provision one.
func cogWaterStep(s Snapshot) Step {
	if s.HeldID(ObjBucket) > 0 {
		return Step{Kind: StepUseOn, Item: "Bucket", ItemID: ObjBucket, TargetKind: TargetLoc, Target: "Well", TargetID: LocWell,
			Anchor: WellStand, Reason: "the bucket from the well, to cool the black cog"}
	}
	if s.Failed["provision:"+KitCogBucket] {
		return Step{Kind: StepWait, Reason: "no bucket of water or bucket from the bank, the mule, the ground by the cart or Port Khazard's store",
			ParkFor: 30 * time.Minute}
	}
	return Step{Kind: StepProvision, KitName: KitCogBucket, Kit: cogBucketKit(), KeepIDs: cogKeep, Bank: ArdougneSouthBank, BankSet: true,
		Reason: "a bucket of water (or the bucket to fill) for the red-hot black cog"}
}

// cogRedStep is the trip into the ogres' cave: the food first (two sharks' worth, asked for once more if the first pass fell
// short), the custom after.
func cogRedStep(s Snapshot) Step {
	if FoodPoolHeal(s.InvIDs, cogFoodIDs) < CogRedFoodHeal {
		if !s.Ran["provision:"+KitCogFoodRed] && !s.Failed["provision:"+KitCogFoodRed] {
			return Step{Kind: StepProvision, KitName: KitCogFoodRed, Kit: cogFoodKit(), KeepIDs: cogKeep, Bank: ArdougneSouthBank, BankSet: true,
				Reason: "two sharks' worth of food for the ogres' cave"}
		}
		return Step{Kind: StepWait, Reason: "no food for the ogres' cave: the red cog's trip wants two sharks' worth in the pack",
			ParkFor: 2 * time.Hour}
	}
	return Step{Kind: StepCustom, Custom: CustomCogRed, Reason: "the red cog from the ogres' cave"}
}

// DecideClockTower is the whole quest, read from the journal's colours and the pack.
func DecideClockTower(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: KojoStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	if s.Stage == CogStageNotStarted || s.Stage == CogStageComplete {
		return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about the Clock Tower"}
	}
	if s.Stage == CogAllPlaced {
		return Step{Kind: StepTalk, Stop: KojoStop}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	// A cog in the pack is fitted before another is fetched (they are too heavy to carry two); one whose spindle is full is a
	// spare the spindle will not take.
	if c, ok := HeldCog(s); ok {
		if CogPlaced(s.Stage, c) {
			return Step{Kind: StepOpHeld, Item: "Cog", ItemID: c.Obj, Op: "Drop", Reason: "the " + c.Name + " cog's spindle is full already"}
		}
		return Step{Kind: StepCustom, Custom: CustomCogPlace, ItemID: c.Obj, Reason: "fit the " + c.Name + " cog on its spindle"}
	}
	// One pass of food, whatever the cog (an optional kit: a short bank never blocks the quest).
	if !s.Ran["provision:"+KitCogFood] && !s.Failed["provision:"+KitCogFood] {
		return Step{Kind: StepProvision, KitName: KitCogFood, Kit: cogFoodKit(), KeepIDs: cogKeep, Bank: ArdougneSouthBank, BankSet: true,
			Reason: "food for the dungeon's hits, if the bank or the mule has it"}
	}
	next, ok := NextCog(s.Stage)
	if !ok {
		return Step{Kind: StepTalk, Stop: KojoStop}
	}
	// A cog, the poison and the bucket each take a free slot: a pack the trainer left full is banked first.
	room := func(step Step) Step {
		if s.FreeSlots < 2 {
			return Step{Kind: StepDeposit, KeepIDs: cogKeep, Bank: ArdougneSouthBank, BankSet: true, Reason: "room for a cog"}
		}
		return step
	}
	switch next.Name {
	case "blue":
		return room(Step{Kind: StepCustom, Custom: CustomCogBlue, Reason: "the blue cog from the sealed room"})
	case "black":
		if !held(ObjBucketOfWater) {
			return room(cogWaterStep(s))
		}
		return room(Step{Kind: StepCustom, Custom: CustomCogBlack, Reason: "the black cog, cooled with the bucket of water"})
	case "white":
		if !held(ObjRatPoison) {
			return room(Step{Kind: StepGrabGround, Item: "Rat poison", ItemID: ObjRatPoison, Anchor: PoisonSpawn, Radius: 90})
		}
		return room(Step{Kind: StepCustom, Custom: CustomCogWhite, Reason: "the white cog through the rat pen"})
	case "red":
		return room(cogRedStep(s))
	}
	return Step{Kind: StepWait, Reason: "unrecognised Clock Tower cog"}
}

// ClockTowerRecord is the quest's identity. The server gates nothing; Hitpoints 30 is the fleet's own floor for the ogres' cave
// (a hit of up to 7 every 6 ticks, one ogre at a time).
var ClockTowerRecord = Record{ID: ClockTowerID, Name: ClockTowerName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "hitpoints", Level: 30}}}}

// ClockTower is the engine module. It never fights (NoRetaliate). Two deaths park it for a day, and one at the ogres' cave does too.
var ClockTower = Module{
	Record: ClockTowerRecord, Bank: ArdougneSouthBank, BankSet: true, Anchor: KojoAnchor,
	ToolIDs:     cogKeep,
	ReadStage:   ParseClockTowerJournal,
	Sustain:     Sustain{FoodIDs: cogFoodIDs, EatAt: 15},
	NoRetaliate: true,
	MaxDeaths:   2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomCogRed: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	Decide: DecideClockTower,
}
