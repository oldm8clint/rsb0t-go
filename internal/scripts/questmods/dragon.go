package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// DragonSlayer is Dragon Slayer ready for the engine.
func DragonSlayer() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.DragonSlayer,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomDragonOziach:    dragonOziach,
			questplan.CustomDragonMaze:      dragonMaze,
			questplan.CustomDragonLeaveMaze: func(e *questrunner.Engine, _ questplan.Step) bool { return leaveMaze(e.Deps()) },
			questplan.CustomDragonOracle:    dragonOracleChest,
			questplan.CustomDragonWormbrain: dragonWormbrain,
			questplan.CustomDragonCombine:   dragonCombine,
			questplan.CustomDragonAshore:    func(e *questrunner.Engine, _ questplan.Step) bool { return leaveShip(e.Deps()) },
			questplan.CustomDragonRepair:    dragonRepair,
			questplan.CustomDragonBoard:     func(e *questrunner.Engine, _ questplan.Step) bool { return belowDecks(e.Deps()) },
			questplan.CustomDragonPassage:   dragonPassage,
			questplan.CustomDragonElvarg:    dragonElvarg,
		},
	}
}

// ---------------------------------------------------------------------------
// Oziach
// ---------------------------------------------------------------------------

var oziachOpening = []string{
	"Can you sell me some Rune plate mail?", "The guildmaster of the Champions' Guild told me.",
	"So how am I meant to prove that?", "A dragon, that sounds like fun!", "And will I need anything to defeat this dragon?",
}

var oziachGoals = []string{
	"Where is the first piece of the map?", "Where is the second piece of the map?",
	"Where is the third piece of the map?", "Where can I get an antidragon shield?",
}

// dragonOziach drives Oziach's menus: the rune-plate chain first, then each of the four
// briefing questions once (his menus re-offer answered ones), then the farewell. The journal's
// "return to Oziach" line and the maze key say afterwards whether the briefing took.
func dragonOziach(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	stop := questplan.NpcStop{Npc: "Oziach", ID: questplan.NpcOziach, Anchor: questplan.OziachAnchor, Leash: 6, Prefer: []string{"Ok."}}
	if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
		return false
	}
	dl := questrunner.NewDialogue(d)
	asked := map[string]bool{}
	for i := 0; i < 40; i++ {
		if !dl.Ready() && !waitUntil(d, 2*time.Second, dl.Ready) {
			break
		}
		opts := dl.OptionTexts()
		if len(opts) == 0 {
			dl.Drive(nil)
			continue
		}
		pick := ""
		if p, ok := questplan.PickPreferred(opts, oziachOpening); ok {
			pick = p
		}
		for _, g := range oziachGoals {
			if pick != "" {
				break
			}
			if p, ok := questplan.PickPreferred(opts, []string{g}); ok && !asked[g] {
				pick, asked[g] = p, true
			}
		}
		if pick == "" {
			if p, ok := questplan.PickPreferred(opts, []string{"So where can I find this dragon?"}); ok && len(asked) < len(oziachGoals) {
				pick = p
			} else if p, ok := questplan.PickPreferred(opts, []string{"Ok I'll try and get everything together."}); ok {
				pick = p
			}
		}
		if pick == "" {
			logf(d, "nothing useful from Oziach in [%s]", strings.Join(opts, " | "))
			break
		}
		dl.DriveSequence([]string{pick}, 20*time.Second)
	}
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// ---------------------------------------------------------------------------
// Melzar's Maze
// ---------------------------------------------------------------------------

func mazeLegDone(d *questrunner.Deps, leg questplan.MazeLeg) bool {
	here := questrunner.CurrentTile(d)
	switch leg.Kind {
	case "kill":
		return held(d, leg.KeyID)
	case "door":
		if leg.KeyID == questplan.ObjMazeKey {
			return questplan.InMelzarsMaze(here) && here.X < leg.Door.X
		}
		return !held(d, leg.KeyID)
	case "climb":
		return here.Level == leg.Land.Level && abs(here.Z-leg.Land.Z) < 100
	case "chest":
		return held(d, questplan.ObjMapMelzar)
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// dragonMaze walks the maze leg by leg (a kill for each coloured key, its door, the ladders,
// the chest), resynced from the keys held and the tile.
func dragonMaze(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	sus := questplan.DragonSlayer.Sustain
	if !questplan.InMelzarsMaze(questrunner.CurrentTile(d)) && !questrunner.Travel(d, questplan.MazeLegs[0].Stand, 1) {
		return false
	}
	idx := questplan.MazeLegFromTile(questrunner.CurrentTile(d))
	for i, leg := range questplan.MazeLegs {
		if leg.Kind == "door" && leg.KeyID != questplan.ObjMazeKey && held(d, leg.KeyID) {
			idx = i
		}
	}
	deadline := time.Now().Add(20 * time.Minute)
	for idx < len(questplan.MazeLegs) && time.Now().Before(deadline) && !held(d, questplan.ObjMapMelzar) {
		leg := questplan.MazeLegs[idx]
		if mazeLegDone(d, leg) {
			idx++
			continue
		}
		ok := false
		switch leg.Kind {
		case "kill":
			ok = mazeKill(d, leg, sus)
		case "door":
			ok = mazeDoor(d, leg)
		case "climb":
			ok = mazeClimb(d, leg)
		case "chest":
			questrunner.WalkNear(d, leg.Stand, 1)
			ok = lootChest(d, questplan.LocMazeChestShut, questplan.ObjMapMelzar)
		}
		if !ok {
			logf(d, "maze: %s did not go", leg.What)
			return false
		}
		idx++
	}
	return held(d, questplan.ObjMapMelzar)
}

func mazeKill(d *questrunner.Deps, leg questplan.MazeLeg, sus questplan.Sustain) bool {
	take := func() bool {
		gs := questrunner.GroundItemsNear(d, leg.KeyID, questrunner.CurrentTile(d), 14)
		if len(gs) == 0 {
			return false
		}
		return questrunner.TakeGround(d, leg.KeyID, tile.Tile{X: gs[0].X, Z: gs[0].Z, Level: gs[0].Level}, 0)
	}
	if held(d, leg.KeyID) || take() {
		return true
	}
	if _, ok := questrunner.FindNpcByID(d, leg.NpcID, "", 14); !ok {
		questrunner.WalkNear(d, leg.At, 3)
	}
	if !waitUntil(d, 20*time.Second, func() bool { _, ok := questrunner.FindNpcByID(d, leg.NpcID, "", 14); return ok }) {
		logf(d, "no %s in the maze (respawning?)", leg.What)
		return false
	}
	step := questplan.Step{Kind: questplan.StepFight, TargetID: leg.NpcID, Target: leg.What}
	if leg.NpcID == 752 {
		fightBest(d, step, sus) // the lesser demon's magic defence is -10
	} else {
		questrunner.FightNpc(d, step, sus)
	}
	waitUntil(d, 4*time.Second, func() bool {
		return len(questrunner.GroundItemsNear(d, leg.KeyID, questrunner.CurrentTile(d), 14)) > 0 || held(d, leg.KeyID)
	})
	return held(d, leg.KeyID) || take()
}

func mazeDoor(d *questrunner.Deps, leg questplan.MazeLeg) bool {
	if !held(d, leg.KeyID) {
		return false
	}
	questrunner.WalkNear(d, leg.Stand, 0)
	m, ok := questrunner.FindLoc(d, 0, "Door", "", leg.Door, 0)
	if !ok {
		logf(d, "%s is not loaded at %v", leg.What, leg.Door)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, leg.KeyID, m); err != nil {
		logf(d, "key on %s: %v", leg.What, err)
		return false
	}
	return waitUntil(d, 10*time.Second, func() bool {
		here := questrunner.CurrentTile(d)
		return !held(d, leg.KeyID) || (here.X == leg.Land.X && here.Z == leg.Land.Z)
	})
}

func mazeClimb(d *questrunner.Deps, leg questplan.MazeLeg) bool {
	questrunner.WalkNear(d, leg.Stand, 0)
	m, ok := questrunner.FindLoc(d, 0, "Ladder", leg.Op, leg.LadderAt, 0)
	if !ok {
		logf(d, "%s is not loaded", leg.What)
		return false
	}
	if err := questrunner.OperateLoc(d, m, leg.Op); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool {
		here := questrunner.CurrentTile(d)
		return here.Level == leg.Land.Level && abs(here.Z-leg.Land.Z) < 100
	})
}

// lootChest opens a two-stage chest (Open swaps in a loc whose first op is Search) and searches it.
func lootChest(d *questrunner.Deps, shutID, want int) bool {
	here := questrunner.CurrentTile(d)
	if m, ok := questrunner.FindLoc(d, shutID, "", "Open", here, 5); ok {
		_ = questrunner.OperateLoc(d, m, "Open")
		waitUntil(d, 6*time.Second, func() bool { _, ok := questrunner.FindLoc(d, 0, "Chest", "Search", here, 5); return ok })
	}
	m, ok := questrunner.FindLoc(d, 0, "Chest", "Search", here, 5)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, want) }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// leaveMaze takes the one route out: the cellar's north-east ladder, the inside-only exit door,
// then the front door with the (unconsumed) maze key.
func leaveMaze(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if !questplan.InMelzarsMaze(here) {
		return true
	}
	if here.Z >= 9000 {
		questrunner.WalkNear(d, questplan.MazeCellarStand, 0)
		if !climb(d, "Ladder", "Climb-up", questplan.MazeCellarLadder) {
			return false
		}
		here = questrunner.CurrentTile(d)
	}
	if here.Z > 3251 && here.X > 2937 {
		questrunner.WalkNear(d, questplan.MazeExitDoor, 0)
		m, ok := questrunner.FindLoc(d, 0, "Door", "Open", questplan.MazeExitDoor, 0)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		waitUntil(d, 8*time.Second, func() bool { return questrunner.CurrentTile(d).X <= 2937 })
	}
	front := questplan.MazeLegs[0]
	questrunner.WalkNear(d, front.Land, 0)
	m, ok := questrunner.FindLoc(d, 0, "Door", "", front.Door, 0)
	if !ok || !held(d, questplan.ObjMazeKey) {
		logf(d, "cannot reopen the maze's front door from inside")
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjMazeKey, m); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return !questplan.InMelzarsMaze(questrunner.CurrentTile(d)) })
}

// ---------------------------------------------------------------------------
// The Oracle's chest, Wormbrain, the map
// ---------------------------------------------------------------------------

func pastMagicDoor(d *questrunner.Deps) bool {
	t := questrunner.CurrentTile(d)
	return t.Z >= 9800 && t.X >= questplan.MagicDoorTile.X && t.X <= 3065
}

func magicDoor(d *questrunner.Deps, in bool) bool {
	stand := questplan.MagicDoorWest
	if !in {
		stand = questplan.MagicDoorEast
	}
	if !questrunner.Travel(d, stand, 0) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocMagicDoor, "", "Open", questplan.MagicDoorTile, 1)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return pastMagicDoor(d) == in })
}

// dragonOracleChest opens the magic door with the four charms (freely after the first time),
// loots the chest for the third piece and leaves the sealed room again.
func dragonOracleChest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjMapOracle) {
		if pastMagicDoor(d) {
			return magicDoor(d, false)
		}
		return true
	}
	if !pastMagicDoor(d) && !magicDoor(d, true) {
		return false
	}
	questrunner.WalkNear(d, questplan.OracleChestStand, 1)
	if !lootChest(d, questplan.LocOracleChest, questplan.ObjMapOracle) {
		return false
	}
	return magicDoor(d, false)
}

// dragonWormbrain buys the piece from the caged goblin: the talk is taken through the bars from
// outside the cell on line of sight (no tile beside him is reachable). The 10,000 coins are the
// decide's kit.
func dragonWormbrain(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjMapWormbrain) {
		return true
	}
	if questrunner.HeldID(d, questplan.CoinsID) < questplan.WormbrainPrice {
		logf(d, "Wormbrain wants %d coins", questplan.WormbrainPrice)
		return false
	}
	if !questrunner.Travel(d, questplan.WormbrainStand, 1) {
		return false
	}
	npc, ok := questrunner.FindNpcByID(d, questplan.NpcWormbrain, "", 6)
	if !ok {
		return false
	}
	if err := entityquery.Interact(d.Session, npc, "Talk-to"); err != nil {
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 6*time.Second, dl.Ready)
	dl.DriveUntil(func() bool { return held(d, questplan.ObjMapWormbrain) }, []string{
		"I believe you've got a piece of map that I need.", "I suppose I could pay you for the map piece",
		"Alright then, 10,000 it is."}, 40*time.Second)
	questrunner.ClearLeftoverModal(d)
	return held(d, questplan.ObjMapWormbrain)
}

func dragonCombine(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if err := questrunner.UseItemOnItem(d, questplan.ObjMapMelzar, questplan.ObjMapWormbrain); err != nil {
		return false
	}
	ok := waitUntil(d, 8*time.Second, func() bool { return held(d, questplan.ObjCrandorMap) })
	questrunner.ClearLeftoverModal(d)
	return ok
}

// ---------------------------------------------------------------------------
// The Lady Lumbridge
// ---------------------------------------------------------------------------

func inHold(t tile.Tile) bool { return t.X >= 3040 && t.X <= 3055 && t.Z >= 9630 && t.Z <= 9650 }
func onDeck(t tile.Tile) bool {
	return t.Level >= 1 && t.X >= 3044 && t.X <= 3052 && t.Z >= 3204 && t.Z <= 3212
}

// belowDecks boards by the gangplank (a scripted teleport off the graph) and climbs into the hold;
// with the map handed to Ned, the hold ladder goes to the top deck where he waits.
func belowDecks(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if inHold(here) || here.Level >= 3 {
		return true
	}
	if !onDeck(here) {
		if !questrunner.Travel(d, questplan.GangplankStand, 0) {
			return false
		}
		m, ok := questrunner.FindLoc(d, 0, "Gangplank", "Cross", questplan.GangplankStand, 5)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Cross"); err != nil {
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool { return onDeck(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	m, ok := questrunner.FindLoc(d, 0, "Ladder", "Climb-down", questrunner.CurrentTile(d), 6)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool {
		t := questrunner.CurrentTile(d)
		return inHold(t) || t.Level >= 3
	})
}

func leaveShip(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if !questplan.OnLadyLumbridge(here) {
		return true
	}
	if inHold(here) {
		m, ok := questrunner.FindLoc(d, 0, "Ladder", "Climb-up", here, 6)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool { return onDeck(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	m, ok := questrunner.FindLoc(d, 0, "Gangplank", "Cross", questrunner.CurrentTile(d), 6)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Cross"); err != nil {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return !questplan.OnLadyLumbridge(questrunner.CurrentTile(d)) })
}

// dragonRepair patches the hull: one plank, four nails and the hammer per use, three uses.
func dragonRepair(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !belowDecks(d) {
		return false
	}
	for i := 0; i < 3 && held(d, questplan.ObjPlank); i++ {
		m, ok := questrunner.FindLoc(d, questplan.LocShipHole, "", "", questplan.ShipHoleTile, 6)
		if !ok {
			d.Delay(1200 * time.Millisecond)
			if m, ok = questrunner.FindLoc(d, questplan.LocShipHole, "", "", questplan.ShipHoleTile, 6); !ok {
				return false
			}
		}
		before := questrunner.HeldID(d, questplan.ObjPlank)
		if err := questrunner.UseItemOnLoc(d, questplan.ObjPlank, m); err != nil {
			logf(d, "plank on the hole: %v", err)
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjPlank) < before }) {
			return false
		}
		questrunner.ClearLeftoverModal(d)
	}
	e.InvalidateStage()
	return true
}

// ---------------------------------------------------------------------------
// Crandor
// ---------------------------------------------------------------------------

func inCrandorDungeon(t tile.Tile) bool {
	return t.X >= 2810 && t.X <= 2880 && t.Z >= 9600 && t.Z <= 9670
}

func openSecretWall(d *questrunner.Deps) bool {
	m, ok := questrunner.FindLoc(d, questplan.LocSecretDoor, "", "Open", questplan.SecretDoorTile, 2)
	if !ok {
		return false
	}
	return questrunner.OperateLoc(d, m, "Open") == nil
}

// intoCrandorDungeon: from the island surface by the rock opening; from anywhere else (the
// Lumbridge respawn) back through the Karamja secret wall, which opens only once it was opened
// from the Crandor side.
func intoCrandorDungeon(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if inCrandorDungeon(here) {
		return true
	}
	if questplan.OnCrandor(here) {
		if !questrunner.Travel(d, questplan.CrandorRock, 1) {
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocCrandorRock, "", "Climb-down", questplan.CrandorRock, 5)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
			return false
		}
		return waitUntil(d, 8*time.Second, func() bool { return questrunner.CurrentTile(d).Z >= 9000 })
	}
	if !questrunner.Travel(d, questplan.SecretWallKaramja, 0) || !openSecretWall(d) {
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return inCrandorDungeon(questrunner.CurrentTile(d)) })
}

// dragonPassage opens the Karamja secret wall from the Crandor side (so a death against Elvarg
// can come back in), lands on the Karamja row, and crosses straight back.
func dragonPassage(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !intoCrandorDungeon(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.SecretDoorTile, 0) || !openSecretWall(d) {
		return false
	}
	if !waitUntil(d, 8*time.Second, func() bool { return questrunner.CurrentTile(d).Z < questplan.SecretDoorTile.Z }) {
		return false
	}
	return intoCrandorDungeon(d)
}

// dragonElvarg reaches the lair (the gate opens inward only at stage 9) and fights Elvarg in
// melee with the shield worn (her magic defence is +30; the breath is 10 at most with the shield).
func dragonElvarg(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.WornCounts(d)[questplan.ObjAntiDragon] == 0 {
		logf(d, "no anti-dragon shield worn: not going near Elvarg")
		return false
	}
	if !questplan.InElvargLair(questrunner.CurrentTile(d)) {
		if !intoCrandorDungeon(d) || !questrunner.Travel(d, questplan.ElvargGateStand, 0) {
			return false
		}
		m, ok := questrunner.FindLoc(d, 0, "Gate", "Open", questplan.ElvargGateInside, 2)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool { return questplan.InElvargLair(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	healToFull(d, questplan.DragonSlayer.Sustain.FoodIDs)
	won := questrunner.FightNpc(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcElvarg, Target: "Elvarg"},
		questplan.DragonSlayer.Sustain)
	e.InvalidateStage()
	return won
}
