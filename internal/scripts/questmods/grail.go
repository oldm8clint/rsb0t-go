package questmods

import (
	"time"

	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// HolyGrail is The Holy Grail ready for the engine.
func HolyGrail() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.HolyGrail,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomGrailMerlin:       grailMerlin,
			questplan.CustomGrailEntrana:      grailEntrana,
			questplan.CustomGrailLeaveEntrana: grailLeaveEntrana,
			questplan.CustomGrailWhistles:     grailWhistles,
			questplan.CustomGrailRealm:        grailRealm,
			questplan.CustomGrailLeaveRealm:   grailLeaveRealm,
			questplan.CustomGrailPercival:     grailPercival,
			questplan.CustomGrailTake:         grailTake,
		},
	}
}

// grailMerlin opens Merlin's workshop door (which spawns a fresh Merlin inside) and talks to him.
func grailMerlin(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, tile.Tile{X: 2764, Z: 3504, Level: 1}, 1) {
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocMerlinWorkshop, "", "Open", questplan.MerlinWorkshopDoor, 3); ok {
		_ = questrunner.OperateLoc(d, m, "Open")
		d.Delay(1500 * time.Millisecond)
	}
	stop := questplan.NpcStop{Npc: "Merlin", ID: questplan.NpcMerlinWorkshop, Anchor: questplan.MerlinWorkshopNpc, Leash: 6,
		Prefer: []string{"Thank you for the advice."}}
	questrunner.WalkNear(d, questplan.MerlinWorkshopNpc, 1)
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// StripAtBank takes everything worn off and banks everything except keep: Entrana refuses any
// weapon or armour, and Glarial's tomb much the same. The deposit is bare (questrunner.DepositBare): the run staller's
// Ring of dueling or Games necklace, which the ordinary deposit keeps in the pack and draws back from the bank, is armour
// to the monks.
func StripAtBank(d *questrunner.Deps, bankTile tile.Tile, keep func(id int) bool) bool {
	if !questrunner.Travel(d, bankTile, 4) {
		return false
	}
	questrunner.PackVisible(d)
	questrunner.StripWorn(d)
	b, ok := questrunner.OpenBankAt(d, bankTile)
	if !ok {
		return false
	}
	clean := questrunner.DepositBare(d, b, keep)
	_ = b.Close()
	return clean && len(questrunner.WornCounts(d)) == 0
}

// grailEntrana strips at Falador, sails with the Port Sarim monks and talks to the High Priest,
// which summons the crone (stage 3 to 4).
func grailEntrana(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.OnEntrana(questrunner.CurrentTile(d)) {
		foods := questplan.HolyGrail.Sustain
		keep := func(id int) bool { return id == questplan.CoinsID || foods.IsFood(id) }
		if !StripAtBank(d, questplan.FaladorEastBank, keep) {
			return false
		}
		if !questrunner.Travel(d, questplan.PortSarimMonks, 4) {
			return false
		}
		monk := questplan.NpcStop{Npc: "Monk of Entrana", ID: questplan.NpcShipMonk, Anchor: questplan.PortSarimMonks, Leash: 8,
			Prefer: []string{"Yes, okay, I'm ready to go."}}
		if !questrunner.OpenDialogue(d, monk) {
			return false
		}
		questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.OnEntrana(questrunner.CurrentTile(d)) }, monk.Prefer, 40*time.Second)
		questrunner.ClearLeftoverModal(d)
		if !questplan.OnEntrana(questrunner.CurrentTile(d)) {
			logf(d, "the monks did not sail (something forbidden still carried?)")
			return false
		}
	}
	priest := questplan.NpcStop{Npc: "High Priest", ID: questplan.NpcHighPriest, Anchor: questplan.HighPriestAnchor, Leash: 8,
		Prefer: []string{"Ok, I will go searching"}}
	if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: priest}) {
		return false
	}
	// The crone's own dialogue follows the priest's; drive it through.
	dl := questrunner.NewDialogue(d)
	if waitUntil(d, 5*time.Second, dl.Ready) {
		dl.Drive(priest.Prefer)
	}
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// grailLeaveEntrana sails back to Port Sarim with an Entrana-side monk (monk_of_entrana.rs2:
// "Do you wish to leave holy Entrana?", then "Yes, I'm ready to go.").
func grailLeaveEntrana(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	off := func() bool { return !questplan.OnEntrana(questrunner.CurrentTile(d)) }
	if off() {
		return true
	}
	if !questrunner.Travel(d, questplan.EntranaDock, 4) {
		logf(d, "could not reach the Entrana dock")
		return false
	}
	monk := questplan.NpcStop{Npc: "Monk of Entrana", ID: questplan.NpcShipMonkEntrana, Anchor: questplan.EntranaDock, Leash: 8,
		Prefer: []string{"Yes, I'm ready to go."}}
	if !questrunner.OpenDialogue(d, monk) {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(off, monk.Prefer, 40*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return off()
}

// grailWhistles opens Draynor Manor's top-floor whistle door (the napkin held), which drops two
// whistles on the table, and takes them.
func grailWhistles(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.WhistleTake, 1) {
		return false
	}
	for i := 0; i < 3 && questrunner.HeldID(d, questplan.ObjMagicWhistle) < 2; i++ {
		if len(questrunner.GroundItemsNear(d, questplan.ObjMagicWhistle, questplan.WhistleTake, 3)) == 0 {
			m, ok := questrunner.FindLoc(d, questplan.LocWhistleDoor, "", "Open", questplan.WhistleDoorTile, 4)
			if !ok {
				return false
			}
			_ = questrunner.OperateLoc(d, m, "Open")
			d.Delay(1500 * time.Millisecond)
			questrunner.ClearLeftoverModal(d)
		}
		for questrunner.HeldID(d, questplan.ObjMagicWhistle) < 2 &&
			questrunner.TakeGround(d, questplan.ObjMagicWhistle, questplan.WhistleTake, 3) {
		}
	}
	return questrunner.HeldID(d, questplan.ObjMagicWhistle) > 0
}

// blowWhistle blows a whistle at the six heads (or anywhere in a realm, which returns) and waits
// for the teleport.
func blowWhistle(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if !questplan.InGrailRealm(here) {
		if !questrunner.Travel(d, questplan.SixHeads, 1) {
			return false
		}
	}
	was := questplan.InGrailRealm(questrunner.CurrentTile(d))
	// Announced to the account's Guardian: the whistle puts the account on the realm's arrival tile, or back on Karamja at the six
	// heads (opheld1,magic_whistle), and neither is in any fail-teleport table (live 14 Oct 2026: "2741,3235 -> 2806,4715 ...
	// UNEXPLAINED" and the way back).
	land, name := questplan.TitanArrival, "holy grail: the Magic whistle into the Fisher King's realm"
	if was {
		land, name = questplan.SixHeads, "holy grail: the Magic whistle out of the Fisher King's realm"
	}
	withdraw := randomevents.ExpectLandings(d.Session, name, 30*time.Second, randomevents.LandingNear(land.X, land.Z, 3))
	if err := questrunner.OpHeldByName(d, questplan.ObjMagicWhistle, "Blow"); err != nil {
		withdraw()
		logf(d, "blow the whistle: %v", err)
		return false
	}
	if !waitUntil(d, 10*time.Second, func() bool { return questplan.InGrailRealm(questrunner.CurrentTile(d)) != was }) {
		withdraw()
		return false
	}
	return true
}

// LeaveGrailRealm blows the Magic whistle when the account stands in the Fisher King's realm, which
// nothing walks out of (content: the whistle there sets the account down on Karamja at 2741,3235).
// Outside the realm, or with no whistle held, it does nothing. It reports whether the account left.
func LeaveGrailRealm(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if !questplan.InGrailRealm(here) {
		return false
	}
	if questrunner.HeldID(d, questplan.ObjMagicWhistle) == 0 {
		logf(d, "in the Fisher King's realm at %d,%d with no Magic whistle: no way out from here", here.X, here.Z)
		return false
	}
	logf(d, "in the Fisher King's realm at %d,%d outside the quest: blowing the whistle out", here.X, here.Z)
	return blowWhistle(d)
}

// excaliburWorn reports whether Excalibur is worn right now (the Titan passes only a caster who
// wears it when he falls).
func excaliburWorn(d *questrunner.Deps) bool {
	return questrunner.WornCounts(d)[questplan.ObjExcalibur] > 0
}

// grailLeaveRealm blows the account out of the arrival pocket when it stands there without Excalibur
// worn (questplan.CustomGrailLeaveRealm): the Titan is invincible to it and sets the quest back.
func grailLeaveRealm(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InGrailRealm(questrunner.CurrentTile(d)) {
		return true
	}
	logf(d, "in the realm at %v without Excalibur worn: blowing the whistle out before the Titan", questrunner.CurrentTile(d))
	ok := LeaveGrailRealm(d)
	e.InvalidateStage()
	return ok
}

// grailRealm is the blighted realm: the Titan (Excalibur worn), the fisherman's bell rung beside
// a maiden, the castle stairs and the Fisher King behind his diagonal door (stage to 8); and,
// once stage 8 is reached, the way out.
//
// The order is the realm's: the whistle sets the account down in the arrival pocket, whose only way
// out is the Titan's bridge, so the Titan comes FIRST, before anything beyond him. The old flow
// looked for the Titan within 12 tiles of where it stood (he is 15 from the arrival tile), found
// none, and went for the Fisherman across the river: five unreachable walks and a park, live
// 13 Oct 2026, while the account was dragged south of the bridge row where the server's walk to the
// Titan dead-ends (questplan/grailtitan.go).
func grailRealm(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InGrailRealm(questrunner.CurrentTile(d)) {
		// Never into the realm without Excalibur worn: he would heal to full and the quest would
		// fall back to stage 7.
		if !excaliburWorn(d) {
			logf(d, "not blowing the whistle into the realm: Excalibur is not worn")
			e.InvalidateStage()
			return false
		}
		// Nor on runes that cannot kill him: the fight would only spend them, and the park below frees
		// the quest's one-at-a-time slot for an account that can. (The pure decision refuses first,
		// DecideHolyGrail; this is for a pack that changed between the decision and the whistle.)
		if ok, why := questplan.TitanRunesEnough(questrunner.BaseLevels(d)["magic"], questrunner.PackCounts(d), 0, 0); !ok {
			logf(d, "not blowing the whistle into the realm: %s", why)
			e.RequestPark(questplan.TitanParkReasonPrefix+why, questplan.TitanParkKit)
			e.InvalidateStage()
			return false
		}
		if !blowWhistle(d) {
			return false
		}
	}
	if questrunner.ReadStatus(d, questplan.HolyGrailName) == questplan.StatusInProgress && e.StageIs(questplan.GrailFindPercy) {
		return blowWhistle(d)
	}
	// The Titan blocks the arrival pocket's only exit: while the account is in the pocket, fight him.
	if questplan.OnTitanSide(questrunner.CurrentTile(d)) {
		if !grailTitanFight(e) {
			e.InvalidateStage()
			return false
		}
		e.InvalidateStage()
	}
	if questrunner.CurrentTile(d).Level == 0 {
		// Behind the castle walls already (the bell was rung and the quest cut short before the staircase): the ring stand
		// cannot be reached from in here, and the bell is not rung twice.
		if !questplan.InGrailCastle(questrunner.CurrentTile(d)) {
			if questrunner.HeldID(d, questplan.ObjGrailBell) == 0 {
				fisher := questplan.NpcStop{Npc: "Fisherman", ID: questplan.NpcGrailFisherman, Anchor: questplan.GrailFisherman, Leash: 8,
					Prefer: []string{"Any idea how to get into the castle?"}}
				if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: fisher}) {
					return false
				}
				if !questrunner.TakeGround(d, questplan.ObjGrailBell, tile.Tile{X: 2762, Z: 4694}, 8) {
					questrunner.Travel(d, tile.Tile{X: 2762, Z: 4694}, 1)
					questrunner.TakeGround(d, questplan.ObjGrailBell, tile.Tile{X: 2762, Z: 4694}, 3)
				}
			}
			if !ringBell(d) {
				return false
			}
			e.InvalidateStage()
		}
		// The staircase: the walker opens the room door (2761,4684) the baked graph knows; the plain walk the old flow made had no
		// door edges and the account stood in the landing room.
		if !questrunner.Travel(d, questplan.GrailStairStand, 1) {
			logf(d, "the castle staircase stand %d,%d is not reachable from %v", questplan.GrailStairStand.X, questplan.GrailStairStand.Z, questrunner.CurrentTile(d))
			return false
		}
		if !climb(d, "Staircase", "Climb-up", tile.Tile{X: 2761, Z: 4680}) {
			return false
		}
		logf(d, "the castle staircase climbed: on the first floor at %v", questrunner.CurrentTile(d))
	}
	if questrunner.CurrentTile(d).Level == 1 {
		// The first floor's room door (2766,4684) stands between the staircase and the Fisher King's door stand: a walk with door
		// edges, not the plain one.
		if !questrunner.Travel(d, questplan.FisherKingDoorSt, 0) {
			logf(d, "the Fisher King's door stand %d,%d is not reachable from %v", questplan.FisherKingDoorSt.X, questplan.FisherKingDoorSt.Z, questrunner.CurrentTile(d))
		}
		if m, ok := questrunner.FindLoc(d, 0, "Door", "Open", questplan.FisherKingDoorSt, 3); ok {
			_ = questrunner.OperateLoc(d, m, "Open")
			d.Delay(1200 * time.Millisecond)
		}
		king := questplan.NpcStop{Npc: "The Fisher King", ID: questplan.NpcFisherKing, Anchor: questplan.FisherKingTile, Leash: 8,
			Prefer: []string{"You don't look too well."}}
		if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: king}) {
			return false
		}
		e.InvalidateStage()
		return true
	}
	return false
}

// grailBellBudget is how long the account waits at the ring stand for a maiden to wander into earshot.
const grailBellBudget = 3 * time.Minute

// grailBellScript is how long the bell's script (three boxes and a teleport) gets once the bell is rung.
const grailBellScript = 25 * time.Second

// grailBellStuckPasses is how many presses in a row may give up on the box that is up (an options menu with nothing to
// pick, which the dialogue driver refuses to guess at and leaves open, or a stop) before the account stops pressing and
// dismisses it.
const grailBellStuckPasses = 3

// grailBellStuckRings is how many rings may end on a box the account could not answer before it gives up: a menu that
// comes back after being dismissed is not the bell's.
const grailBellStuckRings = 2

// bellRig is what ringing the bell touches, so the loop can be run against a fake in a test.
type bellRig struct {
	now      func() time.Time
	delay    func(time.Duration) bool
	tile     func() tile.Tile
	hasBell  func() bool
	maiden   func() (tile.Tile, bool) // the nearest Grail Maiden within earshot's search radius
	ring     func() error
	boxReady func() bool // a chat box (the bell's, a maiden's) is up to be continued
	drive    func() bool // press continue through whatever is up; false when it gave up with the box still up (a menu it will not answer, a stop)
	clear    func()      // dismiss what is left
	logf     func(format string, args ...any)
}

// ringBell rings the Grail bell at the ring stand beside a maiden and clicks through the script that follows, which is
// the whole of the way into the castle (questplan/grailcastle.go): the bell only works with a Grail Maiden within four
// tiles, and its message boxes are p_pausebutton waits whose teleport runs only after the account has pressed continue on
// each. It reports whether the account stands in the castle.
func ringBell(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.GrailBellRing, 1) {
		return false
	}
	dl := questrunner.NewDialogue(d)
	return ringBellWith(bellRig{
		now:     time.Now,
		delay:   d.Delay,
		tile:    func() tile.Tile { return questrunner.CurrentTile(d) },
		hasBell: func() bool { return questrunner.HeldID(d, questplan.ObjGrailBell) > 0 },
		maiden: func() (tile.Tile, bool) {
			m, ok := questrunner.FindNpcByID(d, questplan.NpcGrailMaiden, "", questplan.GrailBellEarshot)
			return tile.Tile{X: m.X, Z: m.Z, Level: m.Level}, ok
		},
		ring:     func() error { return questrunner.OpHeldByName(d, questplan.ObjGrailBell, "Ring") },
		boxReady: dl.Ready,
		drive:    func() bool { return dl.Drive(nil) },
		clear:    func() { questrunner.ClearLeftoverModal(d) },
		logf:     func(format string, args ...any) { logf(d, format, args...) },
	})
}

func ringBellWith(r bellRig) bool {
	landed := func() bool { return questplan.GrailBellLanded(r.tile()) }
	waitLanded := func(timeout time.Duration) bool {
		end := r.now().Add(timeout)
		for r.now().Before(end) {
			if landed() {
				return true
			}
			if !r.delay(200 * time.Millisecond) {
				return false
			}
		}
		return landed()
	}
	deadline := r.now().Add(grailBellBudget)
	told := false
	stuckRings := 0
	for r.now().Before(deadline) {
		if landed() {
			return true
		}
		if !r.hasBell() {
			r.logf("the bell is gone from the pack before it was rung")
			return false
		}
		me := r.tile()
		m, ok := r.maiden()
		if !ok || !questplan.GrailMaidenInEarshot(me, m) {
			// The four maidens wander inside the castle: one drifts back within four tiles of the ring stand.
			if !told {
				r.logf("the bell: no Grail Maiden within %d tiles of %d,%d; waiting for one to walk by", questplan.GrailBellEarshot, me.X, me.Z)
				told = true
			}
			if !r.delay(1200 * time.Millisecond) {
				return false
			}
			continue
		}
		if err := r.ring(); err != nil {
			r.logf("ring the bell: %v", err)
			return false
		}
		r.logf("the bell rung with a maiden %d tiles away: pressing continue through its boxes", tile.Chebyshev(me, m))
		script := r.now().Add(grailBellScript)
		sawBox := false
		stuck := 0
		for r.now().Before(script) && !landed() {
			if r.boxReady() {
				sawBox = true
				// A press that gave up (an options menu the driver will not guess at, or a stop) leaves the box up and returns at
				// once: pressing again straight away would spin, so every pass that leaves a box up waits a beat, and a few that made
				// no headway end the attempt.
				if r.drive() {
					stuck = 0
				} else {
					stuck++
				}
				if stuck >= grailBellStuckPasses {
					break
				}
				if r.boxReady() && !r.delay(300*time.Millisecond) {
					return false
				}
				continue
			}
			if sawBox {
				// The script ended: the teleport is next tick, or "Nothing happens" and it never comes.
				waitLanded(3 * time.Second)
				break
			}
			if !r.delay(300 * time.Millisecond) {
				return false
			}
		}
		r.clear()
		if landed() {
			r.logf("the bell: inside the Grail castle at %v", r.tile())
			return true
		}
		if stuck >= grailBellStuckPasses {
			stuckRings++
			r.logf("the bell: a box at the ring stand could not be continued (at %v); dismissed it", r.tile())
			if stuckRings >= grailBellStuckRings {
				r.logf("the bell: stopping after %d rings that ended on a box it could not answer", stuckRings)
				return false
			}
		} else {
			r.logf("the bell rang and nothing came of it (a box was seen: %v; at %v); asking again", sawBox, r.tile())
		}
		if !r.delay(1200 * time.Millisecond) {
			return false
		}
	}
	r.logf("the bell: no Grail Maiden came into earshot of the ring stand in %v", grailBellBudget)
	return false
}

// grailPercival opens the Goblin Village sacks (Sir Percival appears) and sends him home with a
// whistle (stage 8 to 9).
func grailPercival(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questplan.InGrailRealm(questrunner.CurrentTile(d)) && !blowWhistle(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.PercySacks, 1) {
		return false
	}
	if _, here := questrunner.FindNpcByID(d, questplan.NpcPercival, "", 4); !here {
		m, ok := questrunner.FindLoc(d, questplan.LocPercySacks, "", "Open", questplan.PercySacks, 3)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
	}
	stop := questplan.NpcStop{Npc: "Sir Percival", ID: questplan.NpcPercival, Anchor: questplan.PercySacks, Leash: 5,
		Prefer: []string{"Your father wishes to speak to you.", "Come with me, I shall make you a king."}}
	dl := questrunner.NewDialogue(d)
	if !waitUntil(d, 5*time.Second, dl.Ready) && !questrunner.OpenDialogue(d, stop) {
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return true
}

// grailTake blows into the renewed realm, climbs to the round table, takes the Grail and blows
// out again.
func grailTake(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjHolyGrail) > 0 {
		return blowWhistle(d)
	}
	if !questplan.InGrailRealm(questrunner.CurrentTile(d)) && !blowWhistle(d) {
		return false
	}
	if !questrunner.Travel(d, tile.Tile{X: 2649, Z: 4683, Level: 2}, 1) {
		return false
	}
	if !questrunner.TakeGround(d, questplan.ObjHolyGrail, questplan.GrailTableTile, 2) {
		return false
	}
	return blowWhistle(d)
}

// The realm is a pocket for every quest walk too: nothing walks out of it.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Fisher King's realm", In: questplan.InGrailRealm, Leave: LeaveGrailRealm})
}
