package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// TreeGnomeVillage is Tree Gnome Village ready for the engine.
func TreeGnomeVillage() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.TreeGnomeVillage,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomTreeBallista: treeBallista,
			questplan.CustomTreeOrb:      treeOrb,
			questplan.CustomTreeLeave:    treeLeave,
			questplan.CustomTreeWarlord:  treeWarlord,
			questplan.CustomTreeCeremony: treeCeremony,
		},
	}
}

// treeBallista fires the ballista and answers its three identical menus in sequence.
func treeBallista(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.BallistaStand, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocBallista, "", "Fire", questplan.BallistaTile, 4)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Fire"); err != nil {
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 6*time.Second, dl.Ready)
	answered := dl.DriveSequence(questplan.BallistaAnswers, 45*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return answered == len(questplan.BallistaAnswers)
}

// treeOrb takes the orb out of the Khazard stronghold's chest and brings it out again. The tower has two Khazard
// commanders (22 hitpoints, hits of up to 5, questplan.FoeKhazardCommander), and the step fights both: climbing over
// the crumbled wall runs ~npc_retaliate on the one beside the landing, and opening or searching the chest does the same
// to the one upstairs (quest_tree.rs2), so an account that walks past them is hit in the back on every stand the
// step depends on. Live 16 Oct 2026 03:12-03:15 and 03:38-03:41 two accounts with an empty pack did exactly that,
// went up the ladder at 33/53 hitpoints and were beaten at the chest.
//
// The step is entered with food (Decide provisions it and judges the pack first, questplan.TreeOrbReady) and judges the
// pack again at the foot of the ladder with what the first fight left; a pack that cannot take the commander upstairs
// leaves by the way it came and parks the quest. Every fight is the shared fight step (food, the eat line from the
// foe's hardest hit, the race), so a lost one is recorded and walked away from, and the stronghold is a pocket: the
// walk away leaves by the stronghold's own exit (leaveStronghold).
func treeOrb(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjOrbOfProt) > 0 {
		// The orb is in hand (taken by an earlier pass whose walk out failed): all that is left is the way out.
		out := leaveStronghold(d)
		e.InvalidateStage()
		return out
	}
	if !questplan.InKhazardStronghold(questrunner.CurrentTile(d)) && !treeEnter(d) {
		return false
	}
	// The hall: the commander beside the landing, then the inner door (a walk-graph edge) to the ladder room.
	if questplan.InKhazardHall(questrunner.CurrentTile(d)) {
		if !clearCommanders(e, d) {
			return false
		}
		if !questrunner.Travel(d, questplan.KhazardLadderStand, 1) {
			return false
		}
	}
	if level(d) == 0 {
		if !questrunner.Travel(d, questplan.KhazardLadderStand, 1) {
			return false
		}
		// The second look, with what the landing fight left: the commander upstairs is the last between the account
		// and the chest, and the pack that cannot take him turns round while the way out is a ladder-room away.
		if v := treeReadiness(d); !v.OK {
			why := "not going up to the second Khazard commander with this pack: " + v.Why
			logf(d, "%s", why)
			e.RequestPark(why, questplan.TreeOrbParkFor)
			leaveStronghold(d)
			return false
		}
		questrunner.PrepareForFight(d, questplan.TreeGnomeVillage.Sustain.FoodIDs, questplan.FoeKhazardCommander.PeakHit())
		if !climb(d, "Ladder", "Climb-up", questplan.KhazardLadderTile) {
			return false
		}
	}
	if level(d) == 1 {
		if !clearCommanders(e, d) {
			return false
		}
		if !treeSearchChest(d) {
			return false
		}
	}
	out := leaveStronghold(d)
	e.InvalidateStage()
	return out && questrunner.HeldID(d, questplan.ObjOrbOfProt) > 0
}

// treeEnter walks to the crumbled wall's south stand and climbs over it (one way, once the ballista has fired: the wall
// answers "You can't get over the wall from this side." from the north and "The wall is damaged, but not enough to
// climb." before). It lands on the hall's landing tile, with the commander beside it already turned on the account.
func treeEnter(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.KhazardWallSouth, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocKhazardWall, "", "Climb-over", questplan.KhazardWallSouth, 3)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-over"); err != nil {
		return false
	}
	in := func() bool { return questplan.InKhazardStronghold(questrunner.CurrentTile(d)) }
	questrunner.NewDialogue(d).DriveUntil(in, nil, 15*time.Second)
	questrunner.ClearLeftoverModal(d)
	return in()
}

// treeReadiness is questplan.TreeOrbReady over the live pack, hitpoints and levels, for the step's own second look.
func treeReadiness(d *questrunner.Deps) questplan.TreeOrbVerdict {
	s := questrunner.BuildSnapshot(d, questplan.TreeGnomeVillageName, 0)
	s.HP, s.MaxHP = questrunner.Hitpoints(d)
	s.Levels = questrunner.BaseLevels(d)
	return questplan.TreeOrbReady(s)
}

// clearCommanders fights the Khazard commanders within six tiles of the account on its own floor, one after the other
// (TS clearCommander), through the shared fight step: topped up first, the eat line from his hardest hit, the
// race watched, a lost fight recorded and walked away from. It is false when a fight was not won.
func clearCommanders(e *questrunner.Engine, d *questrunner.Deps) bool {
	for i := 0; i < 3; i++ {
		npc, ok := nearestCommander(d, 6)
		if !ok {
			return true
		}
		hp, max := questrunner.Hitpoints(d)
		logf(d, "a Khazard commander %d tiles away, at %d/%d hitpoints with %d bites of food: fighting him", npc.Distance, hp, max,
			len(questplan.FoodBites(questrunner.PackCounts(d), questplan.TreeGnomeVillage.Sustain.FoodIDs)))
		if !e.Execute(questplan.Step{Kind: questplan.StepFight, Target: npc.Name, TargetID: questplan.NpcKhazardCommander}) {
			return false
		}
		if !d.Delay(600 * time.Millisecond) {
			return false
		}
	}
	return true
}

// nearestCommander is the nearest Khazard commander within radius tiles on the account's own floor: the one upstairs is
// not a target from the hall, nor the hall's from upstairs, whatever the scene lists.
func nearestCommander(d *questrunner.Deps, radius int) (entityquery.Entity, bool) {
	here := level(d)
	return entityquery.Npcs(d.Session.State(), d.Config).Within(radius).
		Where(func(e entityquery.Entity) bool { return e.ID == questplan.NpcKhazardCommander && e.Level == here }).Nearest()
}

// treeSearchChest stands at the chest (TS CHEST_STAND), opens it, searches it and drives the message boxes until the
// orb is in the pack.
func treeSearchChest(d *questrunner.Deps) bool {
	if questrunner.CurrentTile(d) != questplan.KhazardChestStand {
		questrunner.Travel(d, questplan.KhazardChestStand, 0)
	}
	openChest := func() (locindex.Match, bool) {
		return questrunner.FindLoc(d, questplan.LocKhazardChestO, "", "Search", questplan.KhazardChestTile, 4)
	}
	if _, ok := openChest(); !ok {
		if m, shut := questrunner.FindLoc(d, questplan.LocKhazardChestC, "", "Open", questplan.KhazardChestTile, 4); shut {
			if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
				return false
			}
			// A loc that transforms keeps its old id for a tick: the open chest is waited for.
			waitUntil(d, 6*time.Second, func() bool { _, ok := openChest(); return ok })
		}
	}
	m, ok := openChest()
	if !ok {
		logf(d, "the Khazard chest never opened")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return questrunner.HeldID(d, questplan.ObjOrbOfProt) > 0 }, nil, 6*time.Second)
	questrunner.ClearLeftoverModal(d)
	return questrunner.HeldID(d, questplan.ObjOrbOfProt) > 0
}

// treeLeave is the tree-leave step: the way out of the stronghold for an account whose pack cannot take the commanders
// (or that was left inside by a lost fight).
func treeLeave(e *questrunner.Engine, _ questplan.Step) bool {
	out := leaveStronghold(e.Deps())
	e.InvalidateStage()
	return out
}

// leaveStronghold walks an account standing in the Khazard stronghold out of it and reports whether it is out
// (TS leaveStronghold): down the ladder from the chest floor (the commander upstairs cannot follow), through the
// inner door from the hall (the walk graph has it), to the front door's inside stand and out of the front door, which
// opens from the inside only and sets the account down on 2502,3250. The walk graph has these doors too
// (internal/webwalk/khazard.go), so the walks of every other quest plan out; this is the exit a quest that is
// not walking anywhere asks for, and the pocket's own (a walk that starts inside leaves by it before it plans).
func leaveStronghold(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if !questplan.InKhazardStronghold(here) {
		return true
	}
	logf(d, "leaving the Khazard stronghold from %d,%d,%d", here.X, here.Z, here.Level)
	if questplan.InKhazardChestFloor(here) {
		if !questrunner.Travel(d, questplan.KhazardLadderTopStand, 1) {
			logf(d, "could not walk to the ladder down at %d,%d,%d", questplan.KhazardLadderTopStand.X, questplan.KhazardLadderTopStand.Z, questplan.KhazardLadderTopStand.Level)
			return false
		}
		if !climb(d, "Ladder", "Climb-down", tile.Tile{X: questplan.KhazardLadderTile.X, Z: questplan.KhazardLadderTile.Z, Level: 1}) {
			return false
		}
	}
	if questplan.InKhazardHall(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.KhazardLadderStand, 1) {
			logf(d, "could not cross the inner door to the ladder room")
			return false
		}
	}
	if questplan.InKhazardLadderRoom(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.KhazardFrontDoorStand, 0) {
			logf(d, "could not walk to the front door's inside at %d,%d", questplan.KhazardFrontDoorStand.X, questplan.KhazardFrontDoorStand.Z)
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocKhazardDoor, "", "Open", questplan.KhazardDoorTile, 6)
		if !ok {
			logf(d, "no stronghold door at %d,%d", questplan.KhazardDoorTile.X, questplan.KhazardDoorTile.Z)
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "opening the stronghold door: %v", err)
			return false
		}
		waitUntil(d, 8*time.Second, func() bool { return !questplan.InKhazardStronghold(questrunner.CurrentTile(d)) })
	}
	out := !questplan.InKhazardStronghold(questrunner.CurrentTile(d))
	if out {
		logf(d, "out of the Khazard stronghold at %v", questrunner.CurrentTile(d))
	}
	return out
}

// The stronghold is a pocket for every walk (questrunner.Travel), not only the orb step's: after a lost fight upstairs
// the ladder's next quest (live: Merlin's strip, then a death) started its first walk on the chest floor and every
// plan came back "unreachable". Now it leaves by the stronghold's own exit before it plans.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Khazard stronghold", In: questplan.InKhazardStronghold, Leave: leaveStronghold})
}

// treeCeremony gives Bolren the orbs and waits out his ceremony (he walks the account to the
// tree and chants; the quest completes on the queue that follows).
func treeCeremony(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjOrbsOfProt) == 0 {
		// Decide sends an account without them for the orbs (questplan.treeOrbsLostStep); a king asked for a ceremony with
		// nothing in hand only says "No, I'm afraid not." and the step used to wait sixty seconds on that.
		logf(d, "no orbs of protection in the pack: King Bolren cannot hold the ceremony")
		e.InvalidateStage()
		return false
	}
	if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.BolrenStop}) {
		return false
	}
	done := func() bool {
		return questrunner.ReadStatus(d, questplan.TreeGnomeVillageName) == questplan.StatusComplete
	}
	ok := questrunner.NewDialogue(d).DriveUntil(done, nil, 60*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// chopLogs is the provisioner's skill source for normal logs: an axe from the pack (or an iron
// axe bought from Aemad when none is owned), then the trees at the source's tile.
func chopLogs(d *questrunner.Deps, task questplan.SourceTask) bool {
	return chopTrees(d, task, questplan.ObjLogs, "Tree")
}

// chopOak is the provisioner's skill source for oak logs (the Fletching rung's, from level 20): chopLogs
// at the oaks.
func chopOak(d *questrunner.Deps, task questplan.SourceTask) bool {
	return chopTrees(d, task, objOakLogs, "Oak")
}

// chopTrees fells the trees named tree around the task's tile until the task's quantity of logID is
// held, the pack is full or the budget ends, buying an iron axe first when the pack holds none.
func chopTrees(d *questrunner.Deps, task questplan.SourceTask, logID int, tree string) bool {
	hasAxe := func() bool {
		pack, worn := questrunner.PackCounts(d), questrunner.WornCounts(d)
		for _, a := range questplan.Axes {
			if pack[a] > 0 || worn[a] > 0 {
				return true
			}
		}
		return false
	}
	if !hasAxe() {
		if questrunner.BuyFromShop(d, questplan.ShopRef{Npc: "Aemad", NpcID: 590, Anchor: tile.Tile{X: 2613, Z: 3294}},
			questplan.ObjIronAxe, 1) == 0 {
			return false
		}
	}
	if !questrunner.Travel(d, task.Source.Tile, 6) {
		return false
	}
	start := questrunner.HeldID(d, logID)
	deadline := time.Now().Add(5 * time.Minute)
	for questrunner.HeldID(d, logID)-start < task.Qty && time.Now().Before(deadline) && questrunner.FreeSlots(d) > 0 {
		m, ok := questrunner.FindLoc(d, 0, tree, "Chop down", questrunner.CurrentTile(d), 12)
		if !ok {
			break
		}
		before := questrunner.HeldID(d, logID)
		if err := questrunner.OperateLoc(d, m, "Chop down"); err != nil {
			break
		}
		waitUntil(d, 20*time.Second, func() bool {
			return questrunner.HeldID(d, logID) > before
		})
	}
	return questrunner.HeldID(d, logID) > start
}
