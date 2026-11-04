package questmods

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// FightArena is Fight Arena ready for the engine. Every beast is cast at from the study's stand
// (questplan.ArenaSafespot), and meleed with nothing castable (fightBeast). The stand is NOT a safespot, as the
// study thought: the real collision pack has open floor on both sides of it and production has the beasts on
// Jeremy's own tile beside it (questplan/fightarena.go says what was seen), so every fight here is a fight the
// beast takes to the account. The account therefore goes in only with a pack that wins the whole chain
// (questplan.ArenaOdds), tops up before every beast it can (the cell, the wait for Jeremy's word, the first
// blow) and eats at twice the beast's hardest hit, looking every 300 ms (questrunner.FightGuard). Every walk
// out of Khazard's building goes through a door1 first (arenaDoorOut), for this quest's steps and, through
// questrunner's pocket hook, for any other quest's walk too.
func FightArena() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.FightArena,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomArenaChest:  arenaChest,
			questplan.CustomArenaGuard:  arenaGuard,
			questplan.CustomArenaJeremy: arenaJeremy,
			questplan.CustomArenaFight:  arenaFight,
			questplan.CustomArenaFlee:   arenaFlee,
			questplan.CustomArenaLeave: func(e *questrunner.Engine, _ questplan.Step) bool {
				ok := arenaDoorOut(e.Deps())
				e.InvalidateStage()
				return ok
			},
		},
	}
}

// inArenaBuilding is questplan.InArenaBuilding.
func inArenaBuilding(t tile.Tile) bool { return questplan.InArenaBuilding(t) }

const npcArenaGuard1 = 253

// The three dialogues are linear (hengrad.rs2, khazard_guard.rs2 and jeremy_servil.rs2 have no
// p_choice), so each Prefer only names the player's own line: questplan.ValidateStep refuses a
// talk step with an empty prefer list, and the questproof EMPTY lab run arena-empty-1 parked on
// "refusing a malformed step: talk step for Khazard Guard with an empty prefer list".
var (
	hengradStop = questplan.NpcStop{Npc: "Hengrad", ID: questplan.NpcHengrad, Anchor: questplan.HengradAnchor, Leash: 2,
		Prefer: []string{"Ok."}}
	drunkStop = questplan.NpcStop{Npc: "Khazard Guard", ID: questplan.NpcDrunkGuard, Anchor: questplan.DrunkGuardAnchor, Leash: 6,
		Prefer: []string{"Do you still fancy a drink?"}}
	jeremyStop = questplan.NpcStop{Npc: "Jeremy Servil", ID: questplan.NpcJeremyArena,
		Anchor: tile.Tile{X: 2602, Z: 3154}, Leash: 3, Prefer: []string{"Come on Jeremy, we have to get out of here."}}
)

// arenaDoorIn opens door1 from outside. The door lets the account in only with a guard1 within
// five tiles of it and a Khazard piece worn (or, at stages 9-11, sends it straight back into the
// arena); anything else is "This door appears to be locked."
func arenaDoorIn(d *questrunner.Deps) bool {
	if inArenaBuilding(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.ArenaDoor1Out, 1) {
		return false
	}
	questrunner.WalkNear(d, questplan.ArenaDoor1Out, 0)
	if !waitUntil(d, 90*time.Second, func() bool {
		_, ok := questrunner.FindNpcByID(d, npcArenaGuard1, "", 5)
		return ok
	}) {
		logf(d, "no Khazard guard near the arena door to let the account in")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocArenaDoor1, "", "Open", questplan.ArenaDoor1In, 3)
	if !ok {
		logf(d, "arena door1 not found")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool {
		return inArenaBuilding(questrunner.CurrentTile(d))
	}, nil, 25*time.Second)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// arenaDoor1West is the building's west door1 (loc 81 at 2585,3141, a west wall), opened from
// its inside tile, the loc's own; the north one is questplan.ArenaDoor1In.
var arenaDoor1West = tile.Tile{X: 2585, Z: 3141}

// arenaDoorOut leaves the building through the nearer door1, which always opens from inside
// (quest_arena.rs2 [oploc1,fightarena_door1]: check_axis on the loc's own row or column, then
// open_and_close_door walks the account through). Neither door is in the walk graph, so no walk
// leaves the building: live 4 Oct 12:10-12:35, sike, emod and moderator stood in the drunk
// guard's room with every walk "unreachable (best 51)". The arena floor and the cells are not
// left this way (no walk reaches a door1 from them).
func arenaDoorOut(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if !inArenaBuilding(here) {
		return true
	}
	if questplan.InArenaTrap(here) {
		logf(d, "at %d,%d in the arena or a cell: no door1 is reachable from here", here.X, here.Z)
		return false
	}
	doors := []tile.Tile{questplan.ArenaDoor1In, arenaDoor1West}
	if tile.Chebyshev(here, arenaDoor1West) < tile.Chebyshev(here, questplan.ArenaDoor1In) {
		doors[0], doors[1] = doors[1], doors[0]
	}
	for _, door := range doors {
		if !questrunner.Travel(d, door, 1) {
			continue
		}
		m, ok := questrunner.FindLoc(d, questplan.LocArenaDoor1, "", "Open", door, 2)
		if !ok {
			logf(d, "no door1 (loc %d) at %d,%d", questplan.LocArenaDoor1, door.X, door.Z)
			continue
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "opening door1 at %d,%d: %v", door.X, door.Z, err)
			continue
		}
		if waitUntil(d, 10*time.Second, func() bool { return !inArenaBuilding(questrunner.CurrentTile(d)) }) {
			return true
		}
		questrunner.ClearLeftoverModal(d)
		logf(d, "still inside Khazard's building after door1 at %d,%d, at %v", door.X, door.Z, questrunner.CurrentTile(d))
	}
	return false
}

// LeaveArenaBuilding walks an account standing in Khazard's building (outside the arena and the
// cells) out through a door1, and reports whether it is out. Stage 5 calls it before every quest
// pass and trainer start, as LeaveWitchShed: the ladder's next quest cannot walk out of there.
func LeaveArenaBuilding(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if !questplan.InKhazardPocket(here) {
		return true
	}
	logf(d, "standing in Khazard's building at %d,%d: leaving it through door1 first", here.X, here.Z)
	return arenaDoorOut(d)
}

// The building is a pocket for every walk (questrunner.Travel), not only Fight Arena's.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "Khazard's building", In: questplan.InKhazardPocket, Leave: arenaDoorOut})
}

// arenaChest opens and searches the guards' chest north of the building (forceapproach north:
// the only stand is 2613,3190, which locreach works out).
func arenaChest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	here := questrunner.CurrentTile(d)
	if questplan.InArena(here) || questplan.InArenaCell(here) {
		return false
	}
	if !arenaDoorOut(d) || !questrunner.Travel(d, questplan.ArenaChestStand, 1) {
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocArenaChestShut, "", "Open", questplan.ArenaChestTile, 2); ok {
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		waitUntil(d, 6*time.Second, func() bool {
			_, open := questrunner.FindLoc(d, questplan.LocArenaChestOpen, "", "Search", questplan.ArenaChestTile, 2)
			return open
		})
	}
	m, ok := questrunner.FindLoc(d, questplan.LocArenaChestOpen, "", "Search", questplan.ArenaChestTile, 2)
	if !ok {
		logf(d, "the arena chest did not open")
		return false
	}
	have := func() bool {
		own := func(id int) bool { return questrunner.HeldID(d, id) > 0 || questrunner.WornCounts(d)[id] > 0 }
		return own(questplan.ObjKhazardHelmet) && own(questplan.ObjKhazardArmour)
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(have, nil, 8*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return have()
}

// arenaGuard gets into the building in disguise and talks to the drunk guard: at stage 2 that
// is the chat that sets stage 3, with the brew held it trades the brew for the cell keys, and
// below stage 6 it re-issues lost keys.
func arenaGuard(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !arenaDoorIn(d) {
		return false
	}
	ok := e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: drunkStop})
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// arenaJeremy uses the keys on Jeremy's cell gate. free_jeremy_servil opens it and runs the
// arena_enter cutscene, which lands the account on 2603,3155 inside the arena (stage 6).
func arenaJeremy(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !arenaDoorIn(d) {
		return false
	}
	if !questrunner.Travel(d, tile.Tile{X: 2617, Z: 3168}, 1) {
		return false
	}
	if !waitUntil(d, 60*time.Second, func() bool {
		_, ok := questrunner.FindNpcByID(d, questplan.NpcJeremyCell, "", 4)
		return ok
	}) {
		logf(d, "Jeremy is not in his cell (freed by someone else?): waiting for his respawn")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocJeremyDoor, "", "", questplan.JeremyCellDoor, 2)
	if !ok {
		logf(d, "Jeremy's cell gate not found")
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjKhazardKeys, m); err != nil {
		logf(d, "keys on Jeremy's gate: %v", err)
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.InArena(questrunner.CurrentTile(d)) }, nil, 60*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// arenaReenter gets a dead account back into the arena. At stages 9-11 the outside door1 sends
// it straight in (arena_enter). At stages 6 and 8 it goes through the building to door2's
// building side (open_arena_door) and then talks to Jeremy, who re-releases the ogre.
func arenaReenter(e *questrunner.Engine) bool {
	d := e.Deps()
	if e.StageIs(9) || e.StageIs(10) || e.StageIs(11) {
		if inArenaBuilding(questrunner.CurrentTile(d)) && !arenaDoorOut(d) {
			return false
		}
		arenaDoorIn(d)
		return questplan.InArena(questrunner.CurrentTile(d))
	}
	if !arenaDoorIn(d) {
		return false
	}
	if !questrunner.Travel(d, tile.Tile{X: 2607, Z: 3152}, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocArenaDoor2, "", "Open", questplan.ArenaDoor2Tile, 2)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.InArena(questrunner.CurrentTile(d)) }, nil, 25*time.Second)
	questrunner.ClearLeftoverModal(d)
	return questplan.InArena(questrunner.CurrentTile(d))
}

// beastFor is the beast the stage expects, 0 when the next move is a talk.
func beastFor(e *questrunner.Engine) (int, string) {
	switch {
	case e.StageIs(6):
		return questplan.NpcArenaOgre, "Khazard Ogre"
	case e.StageIs(9):
		return questplan.NpcArenaScorpion, "Khazard Scorpion"
	case e.StageIs(10):
		return questplan.NpcArenaBouncer, "Bouncer"
	}
	return 0, ""
}

// arenaFight runs one arena stage: the jail cutscene through Hengrad, re-entry after a death,
// then either a beast fought by fightBeast's plan (cast from the stand, or melee with nothing
// castable) or, when the beast is not out (a broken follow-up, or stage 8/11), a word with Jeremy
// beside the stand to set the chain going. The account is topped up before each of those: in the cell
// (where nothing can hit it, and the next beast is released the moment the escort lands it on the floor),
// and on the floor before Jeremy's word or the first blow.
func arenaFight(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	here := questrunner.CurrentTile(d)
	sus := questplan.FightArena.Sustain
	if questplan.InArenaCell(here) {
		// The cell is the one place in the arena nothing can hit the account. Live 16 Oct 2026 an account came
		// out of it at 19 of 53 with 22 pieces of cake, met the scorpion at 20 and Bouncer at under 15, and
		// died: eat to full here, the next beast is out the moment the escort lands it on the floor.
		_, maxHP := questrunner.Hitpoints(d)
		questrunner.TopUp(d, sus.FoodIDs, maxHP)
		ok := e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: hengradStop})
		questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.InArena(questrunner.CurrentTile(d)) }, nil, 60*time.Second)
		questrunner.ClearLeftoverModal(d)
		e.InvalidateStage()
		return ok && questplan.InArena(questrunner.CurrentTile(d))
	}
	if !questplan.InArena(here) {
		if !arenaReenter(e) {
			return false
		}
		e.InvalidateStage()
	}
	questrunner.WalkNear(d, questplan.ArenaSafespot, 0)
	id, name := beastFor(e)
	guard := questrunner.NewFightGuard(d, id, sus)
	// Eat up to the level a fight starts at before anything is released or reaches the account.
	if n := guard.Prepare(); n > 0 {
		logf(d, "topped up with %d bite(s) before %s", n, orWord(name, "Jeremy's word"))
	}
	if id == 0 || !waitUntil(d, 15*time.Second, func() bool {
		_, ok := looseBeast(d, id)
		return ok
	}) {
		if id != 0 {
			logf(d, "no %s loose on the arena floor (a penned one does not count): asking Jeremy to set the fight going", name)
		}
		ok := e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: jeremyStop})
		questrunner.ClearLeftoverModal(d)
		e.InvalidateStage()
		return ok
	}
	killed := fightBeast(d, guard, id, name)
	// A kill plays the General's follow-up (the jail escort, the next beast's release).
	arenaFollowUp(d, id)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return killed
}

// orWord is s, or word when s is empty.
func orWord(s, word string) string {
	if s == "" {
		return word
	}
	return s
}

// arenaNextFoe is the beast the chain sends after the one whose id is the key.
var arenaNextFoe = map[int]int{
	questplan.NpcArenaOgre:     questplan.NpcArenaScorpion,
	questplan.NpcArenaScorpion: questplan.NpcArenaBouncer,
	questplan.NpcArenaBouncer:  questplan.NpcGeneralKhazard,
}

// arenaFollowUp drives the General's talk after a kill of beast fromID (the dialogue boxes the next beast is
// released behind) until it has been quiet for a while, and in every quiet moment tops the account up for the
// NEXT beast (its eat line, its starting level). It used to drive for a flat twenty seconds with its hands on the
// Continue button: live 16 Oct 2026, by the shape of its log, the account killed the scorpion at about 15 of 53
// hitpoints, spent that time clicking through the General's words while Bouncer was released, and was at 4 when
// the box closed. A bite is never taken with a box up: closing the General's dialogue to eat would cut his
// speech.
func arenaFollowUp(d *questrunner.Deps, fromID int) {
	const quiet = 8 * time.Second
	g := questrunner.NewFightGuard(d, arenaNextFoe[fromID], questplan.FightArena.Sustain)
	dl := questrunner.NewDialogue(d)
	last := time.Now()
	dl.DriveUntil(func() bool {
		main, _, chat := d.Session.State().Modals()
		if dl.Ready() || main != gamestate.NoModal || chat != gamestate.NoModal {
			last = time.Now()
		} else {
			g.Prepare()
		}
		return time.Since(last) > quiet
	}, nil, 30*time.Second)
}

// fightBeast is the arena's combat plan for one beast: the best spell the pack has runes for,
// cast from the stand (the beasts defend magic at 1), and melee when nothing is castable or
// the runes run out mid-fight. Melee is the plan only because nothing else is left: on the arena
// floor before stage 12 no door lets the account out (door2 sends it back, door1 is not
// reachable), so it fights, or dies and respawns outside, which is the only other exit. Live 4
// Oct 12:30-12:33, acct9 stood at the stand logging "no castable combat spell left for
// Khazard Ogre" for three minutes and parked there. The plan's ArenaOdds keeps an account out
// of the arena without the runes and the food for what is left, so this is for one already inside.
func fightBeast(d *questrunner.Deps, g *questrunner.FightGuard, id int, name string) bool {
	if _, ok := questrunner.BestCastableSpell(d, 1); ok {
		return castFromSafespot(d, g, id, name)
	}
	logf(d, "no castable combat spell for %s (Magic %d): melee, as nothing else leads out of the arena", name, questrunner.BaseLevels(d)["magic"])
	return meleeBeast(d, g, id, name, time.Now())
}

// looseBeast finds the beast out on the arena floor (questplan.ArenaBeastLoose). Each beast has
// a map spawn in its pen east of the gates, visible from the whole arena, and every lookup by id
// alone found that one: acct9 (4 Oct 15:55-17:43) never talked to Jeremy, as the ogre was
// "out", and clicked Attack on it through the shut gate for two hours. After a kill the penned
// one is still there, so a lookup by id alone would fight on after the win too.
func looseBeast(d *questrunner.Deps, id int) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(22).Where(func(n entityquery.Entity) bool {
		return n.ID == id && questplan.ArenaBeastLoose(tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
	}).Nearest()
}

// arenaMeleeReclick is how often the melee loop clicks Attack again: eating, and the General's
// cutscenes, can leave the account standing.
const arenaMeleeReclick = 6 * time.Second

// arenaNoBlows is how long the melee loop goes on with no hitpoints changing on either side
// before it gives the step back, so the next pass looks again (and talks to Jeremy if the beast
// is not loose) rather than clicking Attack in silence until the eight minutes are up.
const arenaNoBlows = 90 * time.Second

// arenaRace folds one look at the fight into the guard's race and says so, at most every half minute, when
// the race is being lost: the arena has no way out before the General, so the account fights on, but the
// log shows what the next gate should have known.
func arenaRace(d *questrunner.Deps, g *questrunner.FightGuard, name string, npc entityquery.Entity, said *time.Time) {
	v := g.Poll(npc.Health, npc.TotalHealth)
	if v.Action == questplan.FightRetreat && time.Since(*said) > 30*time.Second {
		*said = time.Now()
		logf(d, "fight: losing to %s (%s): the arena has no way out before the General, so the account fights on", name, v.Why)
	}
}

// meleeBeast fights the beast hand to hand until it dies (true), the account dies, or eight
// minutes pass. The ogre hits at most 8 every six ticks and the scorpion 5 (npc_melee_maxhit);
// Bouncer's 13 is not a fight a Stage 5 account wins this way, and is said so in the log.
func meleeBeast(d *questrunner.Deps, g *questrunner.FightGuard, id int, name string, mark time.Time) bool {
	if id == questplan.NpcArenaBouncer {
		logf(d, "meleeing Bouncer (level 137, max hit 13) with no runes: a death here is likely, and leaves the arena")
	}
	deadline := time.Now().Add(8 * time.Minute)
	seen := false
	var clicked, said time.Time
	blow, lastBeast, lastOwn := time.Now(), -1, -1
	for time.Now().Before(deadline) {
		if questrunner.DiedSince(d, mark) {
			logf(d, "died meleeing %s", name)
			return false
		}
		npc, ok := looseBeast(d, id)
		if !ok {
			return seen
		}
		seen = true
		own, _ := questrunner.Hitpoints(d)
		if npc.Health != lastBeast || own != lastOwn {
			if lastBeast >= 0 {
				blow = time.Now()
			}
			lastBeast, lastOwn = npc.Health, own
		}
		if time.Since(blow) > arenaNoBlows {
			here := questrunner.CurrentTile(d)
			logf(d, "no blow traded with %s in %v (me at %d,%d, it at %d,%d): giving the step back", name, arenaNoBlows, here.X, here.Z, npc.X, npc.Z)
			return false
		}
		if g.Eat() {
			clicked = time.Time{} // eating drops the attack
		}
		arenaRace(d, g, name, npc, &said)
		if time.Since(clicked) >= arenaMeleeReclick {
			if err := entityquery.Interact(d.Session, npc, "Attack"); err != nil {
				logf(d, "attack %s: %v", name, err)
			}
			clicked = time.Now()
		}
		if !d.Delay(600 * time.Millisecond) {
			return false
		}
	}
	return false
}

// castFromSafespot casts at the beast from the stand until it dies. A beast further than
// nine tiles away (the ogre after its cutscene) is lured first: one cast from within range on
// x=2602, then a run straight back, so it follows down x=2602 to the stand. When the
// runes run out mid-fight the rest is melee (fightBeast). The stand holds nothing: the beast comes to
// the account (logged once), and the guard eats at twice its hardest hit every 300 ms of the cast's wait.
func castFromSafespot(d *questrunner.Deps, g *questrunner.FightGuard, id int, name string) bool {
	safe := questplan.ArenaSafespot
	mark := time.Now()
	deadline := mark.Add(8 * time.Minute)
	lured, seen, onUs := false, false, false
	var said time.Time
	for time.Now().Before(deadline) {
		if questrunner.DiedSince(d, mark) {
			logf(d, "died fighting %s", name)
			return false
		}
		npc, ok := looseBeast(d, id)
		if !ok {
			return seen
		}
		seen = true
		spell, ok := questrunner.BestCastableSpell(d, 1)
		if !ok {
			logf(d, "no castable combat spell left for %s: melee for the rest", name)
			return meleeBeast(d, g, id, name, mark)
		}
		at := tile.Tile{X: npc.X, Z: npc.Z}
		here := questrunner.CurrentTile(d)
		far := tile.Chebyshev(at, safe) > 9
		if far && !lured {
			lure := tile.Tile{X: safe.X, Z: at.Z - 8}
			if lure.Z < safe.Z {
				lure.Z = safe.Z
			}
			logf(d, "luring %s from %d,%d via %d,%d", name, at.X, at.Z, lure.X, lure.Z)
			questrunner.WalkNear(d, lure, 0)
			_ = actions.CastOnNpc(d.Session, spell.Name, npc.Index)
			d.Delay(1800 * time.Millisecond)
			lured = true
			questrunner.WalkNear(d, safe, 0)
			continue
		}
		if here != safe {
			questrunner.WalkNear(d, safe, 0)
		}
		if !onUs && tile.Chebyshev(at, questrunner.CurrentTile(d)) <= 1 {
			onUs = true
			logf(d, "%s is on the account at %d,%d (me at %d,%d): the stand is no safespot, so this is a melee exchange to the end", name, at.X, at.Z, here.X, here.Z)
		}
		g.Eat()
		arenaRace(d, g, name, npc, &said)
		if d.Chat != nil && actions.IsNoRunesLine(d.Chat.Since(mark)) {
			logf(d, "out of runes for %s: melee for the rest of %s", spell.Name, name)
			return meleeBeast(d, g, id, name, mark)
		}
		if err := actions.CastOnNpc(d.Session, spell.Name, npc.Index); err != nil {
			logf(d, "cast %s: %v", spell.Name, err)
			return false
		}
		if !g.WaitCast() {
			return false
		}
	}
	return false
}

// arenaFlee leaves the arena after Bouncer: door2 from the arena side (its default branch walks
// the account out past the inviswall that holds the General in), then door1 from inside.
func arenaFlee(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questplan.InArena(questrunner.CurrentTile(d)) {
		questrunner.WalkNear(d, tile.Tile{X: 2605, Z: 3152}, 0)
		m, ok := questrunner.FindLoc(d, questplan.LocArenaDoor2, "", "Open", questplan.ArenaDoor2Tile, 2)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool { return !questplan.InArena(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	ok := arenaDoorOut(d)
	e.InvalidateStage()
	return ok
}
