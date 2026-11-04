package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ScorpionCatcher is Scorpion Catcher ready for the engine.
func ScorpionCatcher() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.ScorpionCatcher,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomScorpB:     scorpB,
			questplan.CustomScorpC:     scorpC,
			questplan.CustomScorpA:     scorpA,
			questplan.CustomScorpLeave: func(e *questrunner.Engine, _ questplan.Step) bool { return leaveDeep(e.Deps()) },
		},
	}
}

func heldCage(d *questrunner.Deps) int {
	for _, id := range []int{questplan.CageEmpty, questplan.CageA, questplan.CageAB, questplan.CageAC,
		questplan.CageB, questplan.CageBC, questplan.CageC, questplan.CageFull} {
		if held(d, id) {
			return id
		}
	}
	return 0
}

// catchScorpion uses the cage on a scorpion (never Pick-up, which stings for 3 and catches
// nothing) and waits for the cage to change.
func catchScorpion(d *questrunner.Deps, npcID int, near tile.Tile) bool {
	before := heldCage(d)
	if before == 0 {
		return false
	}
	npc, ok := questrunner.FindNpcByID(d, npcID, "", 12)
	if !ok {
		if !questrunner.Travel(d, near, 3) {
			return false
		}
		if npc, ok = questrunner.FindNpcByID(d, npcID, "", 12); !ok {
			logf(d, "no Kharid scorpion %d in view (it respawns 100 ticks after a catch)", npcID)
			return false
		}
	}
	if err := questrunner.UseItemOnNpc(d, before, npc); err != nil {
		logf(d, "cage on the scorpion: %v", err)
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return heldCage(d) != before }, nil, 12*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// scorpB catches scorpion B in Ivor's hut (the outpost gate lets a Barcrawl finisher through).
func scorpB(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.OutpostHut, 2) {
		logf(d, "Ivor's hut is unreachable (is the Barcrawl done?)")
		return false
	}
	return catchScorpion(d, questplan.NpcScorpionB, questplan.OutpostHut)
}

// scorpC joins the monastery's order (base Prayer 31) and catches scorpion C upstairs.
func scorpC(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.CurrentTile(d).Level != 1 {
		abbot := questplan.NpcStop{Npc: "Abbot Langley", ID: questplan.NpcAbbotLangley, Anchor: questplan.AbbotAnchor, Leash: 8,
			Prefer: []string{"How do I get further into the monastery?", "Well can I join your order?"}}
		e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: abbot})
		questrunner.ClearLeftoverModal(d)
		if !questrunner.Travel(d, questplan.MonasteryRoom, 3) {
			logf(d, "the monastery ladder refused (base Prayer 31 and the order are needed)")
			return false
		}
	}
	return catchScorpion(d, questplan.NpcScorpionC, questplan.MonasteryRoom)
}

// dustyKey kills the Jailer for the jail key (it lies 100 ticks), frees Velrak, takes his
// dusty key and leaves the cell again (a dead end).
func dustyKey(e *questrunner.Engine) bool {
	d := e.Deps()
	if held(d, questplan.ObjDustyKey) {
		return true
	}
	if !questplan.InVelrakCell(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.JailDoorStand, 1) {
			return false
		}
		if !held(d, questplan.ObjJailKey) {
			if _, ok := questrunner.FindNpcByID(d, questplan.NpcJailer, "", 14); !ok {
				logf(d, "no Jailer in the prison corridor")
				return false
			}
			// The Jailer's magic defence is 0: casts all but always land.
			fightBest(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcJailer, Target: "Jailer"},
				questplan.ScorpionCatcher.Sustain)
			if !waitUntil(d, 6*time.Second, func() bool {
				return len(questrunner.GroundItemsNear(d, questplan.ObjJailKey, questrunner.CurrentTile(d), 12)) > 0
			}) {
				logf(d, "the Jailer dropped no jail key")
				return false
			}
			g := questrunner.GroundItemsNear(d, questplan.ObjJailKey, questrunner.CurrentTile(d), 12)[0]
			if !questrunner.TakeGround(d, questplan.ObjJailKey, tile.Tile{X: g.X, Z: g.Z}, 0) {
				return false
			}
		}
		m, ok := questrunner.FindLoc(d, questplan.LocJailDoor, "", "", questplan.JailCell, 3)
		if !ok {
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.ObjJailKey, m); err != nil {
			logf(d, "jail key on the cell door: %v", err)
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool { return questplan.InVelrakCell(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	velrak := questplan.NpcStop{Npc: "Velrak the explorer", ID: questplan.NpcVelrak, Anchor: questplan.VelrakAnchor, Leash: 6,
		Prefer: []string{"So... do you know anywhere good to explore?", "Yes please!"}}
	e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: velrak})
	questrunner.ClearLeftoverModal(d)
	if !waitUntil(d, 6*time.Second, func() bool { return held(d, questplan.ObjDustyKey) }) {
		logf(d, "Velrak handed over no dusty key")
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocJailDoor, "", "Open", questplan.JailCell, 3); ok {
		_ = questrunner.OperateLoc(d, m, "Open")
		waitUntil(d, 8*time.Second, func() bool { return !questplan.InVelrakCell(questrunner.CurrentTile(d)) })
	}
	return !questplan.InVelrakCell(questrunner.CurrentTile(d))
}

func inSecretRoom(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2874 && t.X <= 2880 && t.Z >= 9793 && t.Z <= 9798
}

// crossWall searches the old wall by the coffins, in either direction (it teleports).
func crossWall(d *questrunner.Deps, inward bool) bool {
	if inSecretRoom(questrunner.CurrentTile(d)) == inward {
		return true
	}
	stand := questplan.SecretWallStand
	if !inward {
		stand = questplan.SecretRoomTile
	}
	questrunner.WalkNear(d, stand, 0)
	m, ok := questrunner.FindLoc(d, questplan.LocSecretWall, "", "Search", questplan.SecretWallStand, 3)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return inSecretRoom(questrunner.CurrentTile(d)) == inward }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// scorpA fetches the dusty key, opens the deep gate with it, runs the blue-dragon corridor with
// the shield worn, crosses the secret wall and catches scorpion A.
func scorpA(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InDeepDungeon(questrunner.CurrentTile(d)) {
		if !dustyKey(e) {
			return false
		}
		if !questrunner.Travel(d, questplan.DeepGateEast, 0) {
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocDeepGate, "", "", questplan.DeepGateEast, 2)
		if !ok {
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.ObjDustyKey, m); err != nil {
			logf(d, "dusty key on the deep gate: %v", err)
			return false
		}
		if !questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.InDeepDungeon(questrunner.CurrentTile(d)) }, nil, 10*time.Second) {
			return false
		}
	}
	healToFull(d, questplan.ScorpionCatcher.Sustain.FoodIDs)
	if !inSecretRoom(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.SecretWallStand, 0) || !crossWall(d, true) {
			return false
		}
	}
	return catchScorpion(d, questplan.NpcScorpionA, questplan.ScorpionATile)
}

// leaveDeep crosses back out: the wall, the corridor and the deep gate (a plain Open from inside).
func leaveDeep(d *questrunner.Deps) bool {
	if inSecretRoom(questrunner.CurrentTile(d)) && !crossWall(d, false) {
		return false
	}
	if !questplan.InDeepDungeon(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.DeepGateWest, 0) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocDeepGate, "", "Open", questplan.DeepGateEast, 2)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		return false
	}
	return waitUntil(d, 10*time.Second, func() bool { return !questplan.InDeepDungeon(questrunner.CurrentTile(d)) })
}
