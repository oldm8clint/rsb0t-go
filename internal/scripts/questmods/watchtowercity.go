package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Gu'Tanoth: the north-west gate, the rock cake stall, the battlement, the chasm, the City guard's riddle, the six skavid caves and the east gate
// (questplan/watchtower.go has the geography; each region the walk graph cannot leave is a pocket, registered in watchtower.go).

// wtPastRelicGate reports the account inside the north-west gate (the guard throws it down the hill to the east on a refusal).
func wtPastRelicGate(d *questrunner.Deps) bool {
	t := questrunner.CurrentTile(d)
	return t.Level == 0 && t.X <= 2503 && t.Z >= 3058 && t.Z <= 3066
}

// wtRelicGateStep shows the relic to a guard of the north-west gate: the relic used on him is one conversation that ends with the account teleported through
// (the first contact by TALKING only records that he wants a sign and throws the account down the hill; the use-on does not need it).
func wtRelicGateStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtPastRelicGate(d) {
		return true
	}
	if !wtHeld(d, questplan.WtObjRelic) {
		logf(d, "watchtower: no relic for the gate")
		return false
	}
	for try := 0; try < 2; try++ {
		if !wtWalk(d, questplan.WtRelicGateStand, 2) {
			logf(d, "watchtower: the gate's stand %v was not reached from %v", questplan.WtRelicGateStand, questrunner.CurrentTile(d))
			return false
		}
		guard, ok := wtNpcIn(d, questplan.WtNpcGuardRelic, 8)
		if !ok {
			logf(d, "watchtower: no guard at the north-west gate near %v", questrunner.CurrentTile(d))
			return false
		}
		if err := questrunner.UseItemOnNpc(d, questplan.WtObjRelic, guard); err != nil {
			logf(d, "watchtower: the relic on the guard: %v", err)
			return false
		}
		wtAwait(d, nil, func() bool { return questrunner.NewDialogue(d).Ready() }, wtItemWait)
		wtDriveAll(d, nil, 4*time.Second, 40*time.Second)
		if wtAwait(d, nil, func() bool { return wtPastRelicGate(d) }, 10*time.Second) {
			d.Delay(1200 * time.Millisecond)
			return true
		}
		logf(d, "watchtower: the gate guard threw the account down the hill (at %v): showing the relic again", questrunner.CurrentTile(d))
	}
	return false
}

// wtTraderNear reports the ogre trader within 3 tiles of the counter: the steal is refused then, and he turns on the thief.
func wtTraderNear(d *questrunner.Deps) bool {
	trader, ok := wtNpcIn(d, questplan.WtNpcTrader, 14)
	if !ok {
		return false
	}
	return tile.Chebyshev(tile.Tile{X: trader.X, Z: trader.Z, Level: trader.Level}, questplan.WtStall) <= 3
}

// wtCakeStep steals a rock cake from the ogre stall (Thieving 15) while the trader is away from it. The counter swaps to its empty form for twelve ticks after
// each theft, whoever's.
func wtCakeStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjRockCake) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "watchtower: no free slot for the rock cake")
		return false
	}
	deadline := time.Now().Add(wtCakeBudget)
	for time.Now().Before(deadline) && !wtRunLow(e) {
		if !wtWalk(d, questplan.WtStallStand, 0) && questrunner.CurrentTile(d) != questplan.WtStallStand {
			logf(d, "watchtower: the stall's stand %v was not reached from %v", questplan.WtStallStand, questrunner.CurrentTile(d))
			return false
		}
		if wtTraderNear(d) {
			logf(d, "watchtower: the ogre trader is beside his counter: waiting for him to wander off")
			if !d.Delay(2400 * time.Millisecond) {
				return false
			}
			continue
		}
		m, ok := questrunner.FindLoc(d, questplan.LocWtCakeStall, "", "Steal-From", questplan.WtStall, 3)
		if !ok {
			// The empty counter: back in twelve ticks.
			if !d.Delay(3600 * time.Millisecond) {
				return false
			}
			continue
		}
		if err := questrunner.OperateLoc(d, m, "Steal-From"); err != nil {
			logf(d, "watchtower: the stall: %v", err)
			return false
		}
		if waitUntil(d, 6*time.Second, func() bool { return wtHeld(d, questplan.WtObjRockCake) }) {
			questrunner.ClearLeftoverModal(d)
			return true
		}
		questrunner.ClearLeftoverModal(d)
		if !d.Delay(3600 * time.Millisecond) {
			return false
		}
	}
	logf(d, "watchtower: no rock cake from the stall in %s (the ogre trader never left it?)", wtCakeBudget)
	return false
}

// wtClimbWall climbs the battlement (loc 2832) in either direction; the content lets an account over once the gift is paid.
func wtClimbWall(d *questrunner.Deps, done func() bool) bool {
	return wtOperate(d, questplan.LocWtBattlement, "Climb-over", tile.Tile{X: 2507, Z: 3011}, 4, nil, done, 12*time.Second)
}

// wtBattlementStep gives the battlement guard the rock cake and climbs the wall: a first talk gets the account asked for a gift ("But I am a friend to ogres..."),
// the second (or the cake used on him) hands it over, and the wall is climbed by the content itself or, if it is not, by the account.
func wtBattlementStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	lower := func() bool { return wtArea(d) == questplan.WtAreaLowerCity }
	if lower() {
		return true
	}
	if !wtHeld(d, questplan.WtObjRockCake) {
		logf(d, "watchtower: no rock cake for the battlement guard")
		return false
	}
	if !wtWalk(d, questplan.WtBattlementWest, 1) {
		logf(d, "watchtower: the battlement's stand %v was not reached from %v", questplan.WtBattlementWest, questrunner.CurrentTile(d))
		return false
	}
	stop := questplan.NpcStop{Npc: "Ogre guard", ID: questplan.WtNpcGuardWall, Anchor: questplan.WtWallGuardAnchor, Leash: 10,
		Prefer: []string{"But I am a friend to ogres..."}}
	// The talk, with the cake in the pack: the first asks for a gift, a later one takes the cake and the content climbs the wall.
	if questrunner.OpenDialogue(d, stop) {
		wtDriveAll(d, stop.Prefer, 4*time.Second, 40*time.Second)
	}
	if wtAwait(d, nil, lower, 10*time.Second) {
		d.Delay(1200 * time.Millisecond)
		return true
	}
	// Otherwise he has only asked: the cake is handed over.
	if guard, ok := wtNpcIn(d, questplan.WtNpcGuardWall, 10); ok && wtHeld(d, questplan.WtObjRockCake) {
		if err := questrunner.UseItemOnNpc(d, questplan.WtObjRockCake, guard); err != nil {
			logf(d, "watchtower: the cake on the guard: %v", err)
		} else {
			wtAwait(d, nil, func() bool { return questrunner.NewDialogue(d).Ready() }, wtItemWait)
			wtDriveAll(d, nil, 4*time.Second, 30*time.Second)
		}
	}
	if wtAwait(d, nil, lower, 10*time.Second) {
		d.Delay(1200 * time.Millisecond)
		return true
	}
	// The content climbs the wall for a gift handed over by use; if it did not, the wall is climbed by hand.
	if wtClimbWall(d, lower) {
		d.Delay(1200 * time.Millisecond)
		return true
	}
	return false
}

// wtLowerOut climbs the battlement back out of the lower city (the pocket's way out, open both ways once the gift is paid).
func wtLowerOut(d *questrunner.Deps) bool {
	if wtArea(d) != questplan.WtAreaLowerCity {
		return true
	}
	if !questrunner.Travel(d, questplan.WtBattlementEast, 0) && questrunner.CurrentTile(d) != questplan.WtBattlementEast {
		logf(d, "watchtower: the battlement's inside stand %v was not reached from %v", questplan.WtBattlementEast, questrunner.CurrentTile(d))
		return false
	}
	if !wtClimbWall(d, func() bool { return wtArea(d) != questplan.WtAreaLowerCity }) {
		logf(d, "watchtower: the battlement did not take the account out (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1200 * time.Millisecond)
	return true
}

// wtJumpStep crosses the chasm for 20 coins (Agility 25): over the wall first if the account is outside it, to the jump stand, the rock, the guard's price.
func wtJumpStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtArea(d) == questplan.WtAreaCityGuard {
		return true
	}
	if questrunner.HeldID(d, questplan.WtObjCoins) < questplan.WtChasmToll {
		logf(d, "watchtower: %d coins are wanted for the ogre guard at the chasm", questplan.WtChasmToll)
		return false
	}
	if wtArea(d) != questplan.WtAreaLowerCity {
		if !wtWalk(d, questplan.WtBattlementWest, 0) && questrunner.CurrentTile(d) != questplan.WtBattlementWest {
			logf(d, "watchtower: the battlement's stand %v was not reached from %v", questplan.WtBattlementWest, questrunner.CurrentTile(d))
			return false
		}
		if !wtClimbWall(d, func() bool { return wtArea(d) == questplan.WtAreaLowerCity }) {
			logf(d, "watchtower: the battlement did not take the account in (the gift is not paid?) (at %v)", questrunner.CurrentTile(d))
			return false
		}
		d.Delay(1200 * time.Millisecond)
	}
	if !questrunner.Travel(d, questplan.WtJumpStand, 0) && questrunner.CurrentTile(d) != questplan.WtJumpStand {
		logf(d, "watchtower: the jump's stand %v was not reached from %v", questplan.WtJumpStand, questrunner.CurrentTile(d))
		return false
	}
	pay := []string{"Okay, I'll pay it."}
	if !wtOperate(d, questplan.LocWtJumpIn, "Jump-From", tile.Tile{X: 2530, Z: 3026}, 3, pay, func() bool { return wtArea(d) == questplan.WtAreaCityGuard }, wtTeleWait+10*time.Second) {
		logf(d, "watchtower: the jump did not take the account across (at %v; Agility 25, 20 coins and the guard within 8 tiles are wanted)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1200 * time.Millisecond)
	return true
}

// wtJumpOut jumps back across the chasm (the pocket's way out; free).
func wtJumpOut(d *questrunner.Deps) bool {
	if wtArea(d) != questplan.WtAreaCityGuard {
		return true
	}
	if !questrunner.Travel(d, questplan.WtJumpBackStand, 0) && questrunner.CurrentTile(d) != questplan.WtJumpBackStand {
		logf(d, "watchtower: the jump back's stand %v was not reached from %v", questplan.WtJumpBackStand, questrunner.CurrentTile(d))
		return false
	}
	if !wtOperate(d, questplan.LocWtJumpOut, "Jump-From", tile.Tile{X: 2531, Z: 3029}, 3, nil, func() bool { return wtArea(d) != questplan.WtAreaCityGuard }, wtTeleWait) {
		logf(d, "watchtower: the jump back did not take the account out (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1200 * time.Millisecond)
	return true
}

// wtRiddleGiveStep answers the City guard's riddle: a death rune used on him is taken, and the Skavid map given. (The content takes it at any stage up to the
// riddle's, so the account that holds a rune needs no earlier talk.)
func wtRiddleGiveStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjMap) {
		return true
	}
	if !wtHeld(d, questplan.WtObjDeathRune) {
		logf(d, "watchtower: no death rune for the City guard")
		return false
	}
	if wtArea(d) != questplan.WtAreaCityGuard {
		logf(d, "watchtower: not on the City guard's side of the chasm (at %v)", questrunner.CurrentTile(d))
		return false
	}
	guard, ok := wtNpcIn(d, questplan.WtNpcCityGuard, 12)
	if !ok {
		logf(d, "watchtower: no City guard within 12 tiles of %v", questrunner.CurrentTile(d))
		return false
	}
	if err := questrunner.UseItemOnNpc(d, questplan.WtObjDeathRune, guard); err != nil {
		logf(d, "watchtower: the death rune on the City guard: %v", err)
		return false
	}
	wtAwait(d, nil, func() bool { return questrunner.NewDialogue(d).Ready() }, wtItemWait)
	wtDriveAll(d, nil, 4*time.Second, 40*time.Second)
	return wtAwait(d, nil, func() bool { return wtHeld(d, questplan.WtObjMap) }, wtItemWait)
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The caves
// ---------------------------------------------------------------------------------------------------------------------------------

// wtCaveStep goes into cave Qty (1 to 5), talks to the skavid there with the one word he understands, and comes out. Cave 5's scared skavid comes first: he is
// what teaches the account the five words.
func wtCaveStep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	idx := step.Qty
	if idx < 1 || idx > 5 {
		logf(d, "watchtower: cave %d is not one of the talkers' caves", idx)
		return false
	}
	if !wtEnterCave(d, idx) {
		return false
	}
	ok := wtCaveTalk(d, idx)
	left := wtCaveOut(d)
	return ok && left
}

// wtEnterCave takes cave idx's mouth with the map and the light in the pack (without them the mouth drops the account in the dark cave, which the check on the
// landing catches).
func wtEnterCave(d *questrunner.Deps, idx int) bool {
	if wtArea(d) == questplan.WtAreaCaves {
		if questplan.WtCaveAt(questrunner.CurrentTile(d)) == idx {
			return true
		}
		if !wtCaveOut(d) {
			return false
		}
	}
	stand := questplan.WtCaveStands[idx-1]
	if !wtWalk(d, stand, 0) && questrunner.CurrentTile(d) != stand {
		logf(d, "watchtower: cave %d's stand %v was not reached from %v", idx, stand, questrunner.CurrentTile(d))
		return false
	}
	if !wtOperate(d, questplan.LocWtCaveIn[idx-1], "Enter", stand, 6, nil, func() bool { return wtArea(d) == questplan.WtAreaCaves }, wtTeleWait) {
		logf(d, "watchtower: cave %d's mouth did not take the account in (at %v)", idx, questrunner.CurrentTile(d))
		return false
	}
	d.Delay(900 * time.Millisecond)
	if questplan.WtCaveAt(questrunner.CurrentTile(d)) != idx {
		logf(d, "watchtower: landed away from cave %d at %v: the map or a lit light is missing (the dark cave's rope is the way out)", idx, questrunner.CurrentTile(d))
		return false
	}
	return true
}

// wtCaveTalk talks to cave idx's skavid and reads the journal for the word that took.
func wtCaveTalk(d *questrunner.Deps, idx int) bool {
	var stop questplan.NpcStop
	var flag uint32
	if idx == 5 {
		stop = questplan.NpcStop{Npc: "Scared skavid", ID: questplan.WtNpcScared, Anchor: questplan.WtCaveTalker[4], Leash: 16,
			Prefer: []string{"Okay, okay, I'm not going to hurt you."}}
		flag = questplan.WtLearning
	} else {
		talker := [4]int{questplan.WtNpcTalker4, questplan.WtNpcTalker1, questplan.WtNpcTalker2, questplan.WtNpcTalker3}[idx-1]
		stop = questplan.NpcStop{Npc: "Skavid", ID: talker, Anchor: questplan.WtCaveTalker[idx-1], Leash: 16, Prefer: []string{questplan.WtCaveWords[idx-1]}}
		flag = questplan.WtCaveFlag[idx-1]
	}
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "watchtower: the skavid of cave %d was not reached (at %v)", idx, questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	wtDriveAll(d, stop.Prefer, wtChat, 60*time.Second)
	st, ok := wtReadState(d)
	if !ok || !st.Has(flag) {
		logf(d, "watchtower: the skavid of cave %d did not teach the account its word (journal read %v, flags %#x)", idx, ok, st.Flags)
		return false
	}
	return true
}

// wtDarkRopeReach is how far from where the dark cave drops the account its climbing rope is looked for: the rope is 35 tiles from the landing.
const wtDarkRopeReach = 48

// wtCaveOut leaves the cave the account stands in by its exit (or, in the dark cave, by the climbing rope that is the only way out); it is the pocket's way out.
func wtCaveOut(d *questrunner.Deps) bool {
	if wtArea(d) != questplan.WtAreaCaves {
		return true
	}
	here := questrunner.CurrentTile(d)
	done := func() bool { return wtArea(d) != questplan.WtAreaCaves }
	for _, id := range questplan.LocWtCaveOut {
		if _, ok := questrunner.FindLoc(d, id, "", "Leave", here, 24); ok {
			if wtOperate(d, id, "Leave", here, 24, nil, done, wtTeleWait) {
				d.Delay(900 * time.Millisecond)
				return true
			}
			return false
		}
	}
	if _, ok := questrunner.FindLoc(d, questplan.LocWtDarkRope, "", "Climb", here, wtDarkRopeReach); ok {
		if wtOperate(d, questplan.LocWtDarkRope, "Climb", here, wtDarkRopeReach, nil, done, wtTeleWait) {
			d.Delay(900 * time.Millisecond)
			return true
		}
	}
	logf(d, "watchtower: no exit of the cave in reach of %v", here)
	return false
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The east gate and the mad skavid
// ---------------------------------------------------------------------------------------------------------------------------------

// wtEastCross brings the account behind the east gate. The first time the guard wants a bar of gold and throws the account down the hill on the first approach, so
// the bar is offered again; once it is paid (the account that has learned the words has paid) the gate is a door that opens.
func wtEastCross(d *questrunner.Deps) bool {
	inside := func() bool { return wtArea(d) == questplan.WtAreaEastInside }
	if inside() {
		return true
	}
	for try := 0; try < 4; try++ {
		if !wtWalk(d, questplan.WtEastGateStand, 2) {
			logf(d, "watchtower: the east gate's stand %v was not reached from %v", questplan.WtEastGateStand, questrunner.CurrentTile(d))
			return false
		}
		if wtHeld(d, questplan.WtObjGoldBar) {
			guard, ok := wtNpcIn(d, questplan.WtNpcGuardEast, 8)
			if !ok {
				logf(d, "watchtower: no guard at the east gate near %v", questrunner.CurrentTile(d))
				return false
			}
			if err := questrunner.UseItemOnNpc(d, questplan.WtObjGoldBar, guard); err != nil {
				logf(d, "watchtower: the gold bar on the guard: %v", err)
				return false
			}
			wtAwait(d, nil, func() bool { return questrunner.NewDialogue(d).Ready() }, wtItemWait)
			wtDriveAll(d, nil, 4*time.Second, 40*time.Second)
			if wtAwait(d, nil, inside, 10*time.Second) {
				d.Delay(1200 * time.Millisecond)
				return true
			}
			logf(d, "watchtower: the east gate guard threw the account down the hill (at %v): offering the bar again", questrunner.CurrentTile(d))
			continue
		}
		// No bar: the gate was paid for, and opens.
		if wtOperate(d, questplan.LocWtGateEast, "Open", tile.Tile{X: 2550, Z: 3028}, 3, nil, inside, 10*time.Second) {
			d.Delay(900 * time.Millisecond)
			return true
		}
		if questrunner.WalkNear(d, questplan.WtEastGateInside, 0) && inside() {
			return true
		}
	}
	return false
}

// wtEastOut leaves the ground behind the east gate by opening the gate from inside (the pocket's way out; the gate is a door once the bar is paid).
func wtEastOut(d *questrunner.Deps) bool {
	if wtArea(d) != questplan.WtAreaEastInside {
		return true
	}
	if !questrunner.Travel(d, questplan.WtEastGateInside, 0) && questrunner.CurrentTile(d) != questplan.WtEastGateInside {
		logf(d, "watchtower: the east gate's inside stand %v was not reached from %v", questplan.WtEastGateInside, questrunner.CurrentTile(d))
		return false
	}
	out := func() bool { return wtArea(d) != questplan.WtAreaEastInside }
	if !wtOperate(d, questplan.LocWtGateEast, "Open", tile.Tile{X: 2550, Z: 3028}, 3, nil, out, 10*time.Second) {
		if !(questrunner.WalkNear(d, questplan.WtEastGateStand, 1) && out()) {
			logf(d, "watchtower: the east gate did not open from inside (at %v)", questrunner.CurrentTile(d))
			return false
		}
	}
	d.Delay(900 * time.Millisecond)
	return true
}

// wtMadPhrase is what the mad skavid says and the one answer that satisfies it (skavid.rs2 [opnpc1,mad_skavid]).
var wtMadPhrases = []struct{ say, answer string }{
	{"ar cur", "Gor."},
	{"bidith ig", "Cur."},
	{"cur tanath", "Bidith."},
	{"gor nod", "Tanath."},
}

// wtMadAnswers is the four answers in order, for an attempt that could not read what the skavid said.
var wtMadAnswers = []string{"Gor.", "Cur.", "Bidith.", "Tanath."}

// wtMadDrive drives one conversation with the mad skavid. He says one of four phrases (the page the account continues) and offers five answers; the right one
// takes the crystal, a wrong one ends the talk ("???"). lost is the account that has been given the crystal and lost it: he offers "But I've lost it!".
func wtMadDrive(d *questrunner.Deps, lost bool, attempt int) {
	chat := actions.NewChatDialog(d.Session, d.Components)
	end := time.Now().Add(60 * time.Second)
	lastActivity := time.Now()
	line := ""
	for time.Now().Before(end) {
		switch {
		case chat.CanContinue():
			text := strings.ToLower(strings.Join(chat.Texts(), " "))
			for _, p := range wtMadPhrases {
				if strings.Contains(text, p.say) {
					line = p.say
				}
			}
			if err := chat.Continue(); err != nil {
				logf(d, "watchtower: continuing the mad skavid's talk: %v", err)
			}
			lastActivity = time.Now()
			if !d.Delay(600 * time.Millisecond) {
				return
			}
		case len(chat.Options()) > 0:
			var texts []string
			for _, o := range chat.Options() {
				texts = append(texts, o.Text)
			}
			pick := ""
			switch {
			case lost:
				pick = "lost"
			case line != "":
				for _, p := range wtMadPhrases {
					if p.say == line {
						pick = p.answer
					}
				}
			default:
				pick = wtMadAnswers[attempt%len(wtMadAnswers)]
			}
			if pick == "" {
				pick = wtMadAnswers[attempt%len(wtMadAnswers)]
			}
			if err := chat.ChooseOption(pick); err != nil {
				logf(d, "watchtower: the mad skavid's menu [%s]: choosing %q: %v", strings.Join(texts, " | "), pick, err)
				questrunner.ClearLeftoverModal(d)
				return
			}
			lastActivity = time.Now()
			if !d.Delay(1200 * time.Millisecond) {
				return
			}
		case chat.IsOpen():
			if !d.Delay(300 * time.Millisecond) {
				return
			}
		default:
			if questrunner.ClearLeftoverModal(d) {
				lastActivity = time.Now()
				continue
			}
			if time.Since(lastActivity) >= 5*time.Second {
				return
			}
			if !d.Delay(300 * time.Millisecond) {
				return
			}
		}
	}
}

// wtMadStep takes the second crystal from the mad skavid in cave 6, which is behind the east gate. Qty 1 is the account that was given the crystal and lost it.
func wtMadStep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	lost := step.Qty == 1
	if wtHeld(d, questplan.WtObjCrystal2) {
		return true
	}
	if !wtEastCross(d) {
		return false
	}
	if !wtEnterCave(d, 6) {
		return false
	}
	stop := questplan.NpcStop{Npc: "Mad skavid", ID: questplan.WtNpcMad, Anchor: questplan.WtCaveTalker[5], Leash: 16, Prefer: []string{"Gor."}}
	got := false
	if questrunner.GotoNpc(d, stop, nil) {
		// He says one of four phrases at random, so a wrong guess costs only a retry.
		for attempt := 0; attempt < 6 && !wtRunLow(e); attempt++ {
			if questrunner.OpenDialogue(d, stop) {
				wtMadDrive(d, lost, attempt)
			}
			if wtHeld(d, questplan.WtObjCrystal2) {
				got = true
				break
			}
			d.Delay(1800 * time.Millisecond)
		}
	} else {
		logf(d, "watchtower: the mad skavid was not reached (at %v)", questrunner.CurrentTile(d))
	}
	out := wtCaveOut(d)
	back := wtEastOut(d)
	if !got {
		logf(d, "watchtower: the mad skavid did not hand over a crystal in six talks")
	}
	return got && out && back
}
