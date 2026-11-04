package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// GrandTree is The Grand Tree ready for the engine.
func GrandTree() questrunner.Quest {
	roots := &rootCursor{}
	return questrunner.Quest{
		Module: questplan.GrandTree,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomGTStart:    gtStart,
			questplan.CustomGTCupboard: gtCupboard,
			questplan.CustomGTJailed:   gtJailed,
			questplan.CustomGTFly:      gtFly,
			questplan.CustomGTForeman:  gtForeman,
			questplan.CustomGTFemi:     gtFemi,
			questplan.CustomGTAnita:    gtAnita,
			questplan.CustomGTChest:    gtChest,
			questplan.CustomGTTwig:     gtTwig,
			questplan.CustomGTDescend:  func(e *questrunner.Engine, _ questplan.Step) bool { return descendGloughTree(e.Deps()) },
			questplan.CustomGTDemon:    gtDemon,
			questplan.CustomGTRoots:    roots.search,
			questplan.CustomGTRock:     gtRock,
		},
	}
}

// talkUntil walks to a stop (over the quest's hops), opens its talk and keeps answering whatever
// the scripted chain raises until expect lands: several Grand Tree legs close the dialogue,
// walk or teleport the account, and only then speak again.
func talkUntil(e *questrunner.Engine, stop questplan.NpcStop, expect func() bool, budget time.Duration) bool {
	d := e.Deps()
	if expect() {
		return true
	}
	if !questrunner.GotoNpc(d, stop, questplan.GrandTree.Hops) || !questrunner.OpenDialogue(d, stop) {
		return false
	}
	ok := questrunner.NewDialogue(d).DriveUntil(expect, stop.Prefer, budget)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// gtStart: the King walks the account to his trapdoor, drops it into the foundations and climbs
// it back out inside one conversation; the bark sample landing ends it.
func gtStart(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	stop := questplan.NarnodeStop
	if questplan.InGTCaves(questrunner.CurrentTile(d)) {
		stop.Anchor = questplan.NarnodeUnder
	}
	return talkUntil(e, stop, func() bool { return held(d, questplan.ObjGTBark) }, 2*time.Minute)
}

func gtCupboard(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjGTJournal) {
		return true
	}
	if !questrunner.Travel(d, questplan.GTCupStand, 1) {
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocGTCupShut, "", "Open", questplan.GTCupTile, 2); ok {
		_ = questrunner.OperateLoc(d, m, "Open")
		waitUntil(d, 6*time.Second, func() bool {
			_, open := questrunner.FindLoc(d, questplan.LocGTCupOpen, "", "Search", questplan.GTCupTile, 2)
			return open
		})
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGTCupOpen, "", "Search", questplan.GTCupTile, 2)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjGTJournal) }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// gtJailed: Glough calls the guards, the account is jailed at the top of the tree, and the King
// lets it out one tile east of the cell; from inside the cell, Charlie resumes the chain.
func gtJailed(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	jail := questplan.GTJail
	released := func() bool {
		t := questrunner.CurrentTile(d)
		return t.Level == 3 && t.Z == jail.Z && t.X > jail.X && !questrunner.NewDialogue(d).Ready()
	}
	stop := questplan.GloughStop
	if t := questrunner.CurrentTile(d); t.Level == 3 && t.X == jail.X && t.Z == jail.Z {
		stop = questplan.CharlieStop
	}
	if !questrunner.GotoNpc(d, stop, questplan.GrandTree.Hops) || !questrunner.OpenDialogue(d, stop) {
		return false
	}
	ok := questrunner.NewDialogue(d).DriveUntil(released, stop.Prefer, 2*time.Minute)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// gtFly takes the King's glider from the top of the tree; it crash-lands in the Karamja jungle.
func gtFly(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	pilot := questplan.NpcStop{Npc: "Gnome pilot", ID: questplan.NpcGTPilot, Anchor: questplan.GTGliderPad, Leash: 6,
		Prefer: []string{"Take me to Karamja please!"}}
	return talkUntil(e, pilot, func() bool {
		t := questrunner.CurrentTile(d)
		return t.Level == 0 && tile.Chebyshev(t, questplan.GTKaramjaCrash) <= 12
	}, time.Minute)
}

// gtForeman answers the foreman's three questions (a wrong answer makes him attack); he walks
// the account into his office first, so a foreman already in view is talked to where he stands.
func gtForeman(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjGTLumber) {
		return true
	}
	stop := questplan.NpcStop{Npc: "Foreman", ID: questplan.NpcForeman, Anchor: questplan.ForemanAnchor, Leash: 10,
		Prefer: []string{"Sadly his wife is no longer with us!", "He loves worm holes.", "Anita."}}
	if _, near := questrunner.FindNpcByID(d, questplan.NpcForeman, "", 8); !near && !questrunner.Travel(d, questplan.ForemanAnchor, 4) {
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	ok := questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjGTLumber) }, stop.Prefer, 3*time.Minute)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// gtFemi rides Femi's cart back into the stronghold (the gate refuses a spy at stage 90).
func gtFemi(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questplan.InStronghold(questrunner.CurrentTile(d)) {
		return true
	}
	femi := questplan.NpcStop{Npc: "Femi", ID: questplan.NpcFemi, Anchor: questplan.FemiAnchor, Leash: 6,
		Prefer: []string{"OK then, I'll pay.", "I'd better get going!"}}
	if !questrunner.Travel(d, questplan.FemiAnchor, 3) {
		return false
	}
	return talkUntil(e, femi, func() bool { return questplan.InStronghold(questrunner.CurrentTile(d)) }, time.Minute)
}

// gtAnita climbs Anita's staircase (no graph edge), takes Glough's key and comes down again.
func gtAnita(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	upstairs := func() bool {
		t := questrunner.CurrentTile(d)
		return t.Level == 1 && tile.Chebyshev(t, questplan.AnitaFloor) <= 6
	}
	if !held(d, questplan.ObjGTKey) {
		if !upstairs() {
			if !questrunner.Travel(d, questplan.AnitaStairs, 1) || !climb(d, "Staircase", "Climb-up", questplan.AnitaStairs) {
				return false
			}
		}
		anita := questplan.NpcStop{Npc: "Anita", ID: questplan.NpcAnita, Anchor: questplan.AnitaAnchor, Leash: 5, Prefer: []string{"Ok."}}
		if !questrunner.OpenDialogue(d, anita) {
			return false
		}
		questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjGTKey) }, nil, time.Minute)
		questrunner.ClearLeftoverModal(d)
	}
	if upstairs() {
		questrunner.WalkNear(d, questplan.AnitaFloor, 1)
		climb(d, "Staircase", "Climb-down", questplan.AnitaFloor)
	}
	return held(d, questplan.ObjGTKey)
}

// gtChest uses Anita's key on Glough's chest (forceapproach north) for the invasion plans.
func gtChest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.GTChestStand, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGTChest, "", "", questplan.GTChestStand, 3)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjGTKey, m); err != nil {
		logf(d, "key on Glough's chest: %v", err)
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjGTPlans) }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// climbToPillars climbs the Agility 25 tree from Glough's first floor to the pillar floor.
func climbToPillars(d *questrunner.Deps) bool {
	up := func() bool {
		t := questrunner.CurrentTile(d)
		return t.Level == 2 && tile.Chebyshev(t, questplan.GTPillarFloor) <= 6
	}
	if up() {
		return true
	}
	if !questrunner.Travel(d, questplan.GTClimbStand, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocGTClimb, "", "Climb-up", questplan.GTClimbStand, 4)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
		return false
	}
	return waitUntil(d, 10*time.Second, up)
}

// descendGloughTree climbs off the pillar floor (a pocket with no baked way down) to the ground.
func descendGloughTree(d *questrunner.Deps) bool {
	t := questrunner.CurrentTile(d)
	if t.Level >= 2 {
		questrunner.WalkNear(d, questplan.GTDownStand, 1)
		m, ok := questrunner.FindLoc(d, questplan.LocGTDown, "", "Climb-down", questplan.GTDownStand, 4)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool { return questrunner.CurrentTile(d).Level < 2 }) {
			return false
		}
	}
	return questrunner.Travel(d, questplan.GloughFoot, 2)
}

// gtTwig lays every held twig on its own pillar (TUZO; all four read "Twigs").
func gtTwig(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !climbToPillars(d) {
		return false
	}
	for _, p := range questplan.GTPillars {
		if !held(d, p.Twig) {
			continue
		}
		questrunner.WalkNear(d, p.Stand, 0)
		m, ok := questrunner.FindLoc(d, p.Pillar, "", "", p.Stand, 2)
		if !ok {
			return false
		}
		if err := questrunner.UseItemOnLoc(d, p.Twig, m); err != nil {
			logf(d, "twig %d on pillar %d: %v", p.Twig, p.Pillar, err)
			return false
		}
		twig := p.Twig
		waitUntil(d, 6*time.Second, func() bool { return !held(d, twig) })
	}
	e.InvalidateStage()
	return true
}

// gtDemon drops through Glough's trapdoor (ground decor that blocks its own tile, so the stand
// is north of it), waits out Glough's speech, and fights the Black Demon. The demon is deleted
// once the account is more than 17 tiles away, and only stage 130 replays the cutscene.
//
// At stage 130 it refuses while the Protect from Melee fight is not built (questplan.
// GTDemonFightBuilt): the decision never picks this step there (DecideGrandTree waits behind
// GTDemonReady), so this is the second lock on a door that killed emod four times and acct9 once
// on 5 Oct 2026. Past the demon (stage 140 on) the step only walks back down to the King.
func gtDemon(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.GTDemonFightBuilt && e.StageIs(questplan.GTTrapdoor) {
		logf(d, "not fighting the Black Demon: the account cannot hold Protect from Melee through it (questplan.GTDemonReady); the quest skips")
		if questplan.InGTCaves(questrunner.CurrentTile(d)) {
			leaveGTCaves(d)
		}
		return false
	}
	if !questplan.InGTCaves(questrunner.CurrentTile(d)) {
		if !climbToPillars(d) {
			return false
		}
		questrunner.WalkNear(d, questplan.GTTrapdoorStand, 0)
		m, ok := questrunner.FindLoc(d, questplan.LocGTTrapdoor, "", "Open", questplan.GTTrapdoorStand, 3)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		if !waitUntil(d, 12*time.Second, func() bool { return questplan.InGTCaves(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	e.InvalidateStage()
	if !e.StageIs(questplan.GTTrapdoor) {
		return true // past the demon: the caves were the point
	}
	demon := func() bool { _, ok := questrunner.FindNpcByID(d, questplan.NpcGTDemon, "", 20); return ok }
	questrunner.NewDialogue(d).DriveUntil(demon, nil, 90*time.Second)
	if !demon() {
		logf(d, "no Black Demon in the caves: out and back through the trapdoor")
		leaveGTCaves(d)
		return false
	}
	healToFull(d, questplan.GrandTree.Sustain.FoodIDs)
	won := fightBest(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcGTDemon, Target: "Black Demon"},
		questplan.GrandTree.Sustain)
	e.InvalidateStage()
	return won
}

func leaveGTCaves(d *questrunner.Deps) bool {
	if !questplan.InGTCaves(questrunner.CurrentTile(d)) {
		return true
	}
	return questrunner.HopLadder(d, questplan.GTHops[0])
}

// rootCursor walks the fifteen roots in a fixed order, so a resumed search takes the shortest
// remaining leg; fourteen answer "You search the root but don't find anything".
type rootCursor struct{ next int }

func (r *rootCursor) search(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	for tries := 0; tries < len(questplan.GTRoots) && !held(d, questplan.ObjGTDaconia); tries++ {
		root := questplan.GTRoots[r.next%len(questplan.GTRoots)]
		r.next++
		if !questrunner.Travel(d, root.Stand, 1) {
			continue
		}
		m, ok := questrunner.FindLoc(d, 0, "Root", "Search", root.SW, 1)
		if !ok {
			continue
		}
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			continue
		}
		questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjGTDaconia) }, nil, 6*time.Second)
		questrunner.ClearLeftoverModal(d)
	}
	return held(d, questplan.ObjGTDaconia)
}

// gtRock talks to the King in the caves: at 140 he confirms Glough, at 150 he takes the rock.
func gtRock(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	under := questplan.NarnodeStop
	under.Anchor = questplan.NarnodeUnder
	under.Leash = 8
	before := e.StageIs(questplan.GTDemon)
	ok := talkUntil(e, under, func() bool {
		if before {
			return false
		}
		return !held(d, questplan.ObjGTDaconia)
	}, time.Minute)
	e.InvalidateStage()
	return ok || before
}
