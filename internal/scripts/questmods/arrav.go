package questmods

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Shield of Arrav, the engine's side (questplan/arrav.go is the decision). Every custom step here is bounded: a deadline, a retry count or a
// death cap ends it, and a step that cannot do its job returns false so the quest parks instead of looping.

// ArravQuest is the quest ready for the engine. The pair link comes from Deps.Pair.
func ArravQuest() questrunner.Quest {
	m := questplan.ArravModule
	m.StepDeathCaps = map[string]questplan.StepDeathCap{
		questplan.CustomArravCross: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	}
	return questrunner.Quest{
		Module: m,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomArravDown:    arravDown,
			questplan.CustomArravSurface: arravSurface,
			questplan.CustomArravJonny:   arravJonny,
			questplan.CustomArravPhHalf:  arravPhoenixHalf,
			questplan.CustomArravBaHalf:  arravBlackArmHalf,
			questplan.CustomArravCross:   arravCrossbows,
			questplan.CustomArravKey:     arravKey,
			questplan.CustomArravMeet:    arravMeet,
		},
	}
}

const (
	// arravMeetBudget is how long one meeting waits for the partner before the quest parks and tries again later.
	arravMeetBudget = 20 * time.Minute
	// arravTradeBudget bounds one trade (requesting, offering, both accepts).
	arravTradeBudget = 90 * time.Second
)

// arravLoc operates one loc by id near a tile with an op, through the engine's own step (it walks to the anchor and waits for the op to land).
func arravLoc(e *questrunner.Engine, id int, name, op string, at tile.Tile) bool {
	return e.Execute(questplan.Step{Kind: questplan.StepInteractLoc, LocID: id, Loc: name, Op: op, Anchor: at})
}

// arravDown climbs the cellar ladder under the VTAM building.
func arravDown(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questplan.InPhoenixCellar(questrunner.CurrentTile(d)) {
		return true
	}
	if !arravLoc(e, questplan.LocArravCellarLdr, "Ladder", "Climb-down", questplan.ArravCellarLadder) {
		return false
	}
	return waitUntil(d, 6*time.Second, func() bool { return questplan.InPhoenixCellar(questrunner.CurrentTile(d)) })
}

// arravSurface climbs back to the street from the cellar, the weapon store's upper floor or the Black Arm upper floor, by whichever ladder or
// stair the account stands near.
func arravSurface(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	here := questrunner.CurrentTile(d)
	switch {
	case questplan.InPhoenixCellar(here):
		if !arravLoc(e, questplan.LocArravPhLadder, "Ladder", "Climb-up", tile.Tile{X: 3244, Z: 9783}) {
			return false
		}
		return waitUntil(d, 6*time.Second, func() bool { return !questplan.InPhoenixCellar(questrunner.CurrentTile(d)) })
	case here.Level == 1 && here.X >= 3230:
		if !arravLoc(e, questplan.LocArravStoreTop, "Ladder", "Climb-down", tile.Tile{X: 3252, Z: 3384, Level: 1}) {
			return false
		}
		return waitUntil(d, 6*time.Second, func() bool { return questrunner.CurrentTile(d).Level == 0 })
	case here.Level == 1:
		if !arravLoc(e, questplan.LocArravStairsTop, "Staircase", "Climb-down", tile.Tile{X: 3188, Z: 3390, Level: 1}) {
			return false
		}
		return waitUntil(d, 6*time.Second, func() bool { return questrunner.CurrentTile(d).Level == 0 })
	}
	return true
}

// arravJonny kills Jonny the Beard (level 2) in the Blue Moon Inn and takes the report he drops for this account.
func arravJonny(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for try := 0; try < 4; try++ {
		if questrunner.HeldID(d, questplan.ObjArravReport) > 0 {
			return true
		}
		if !questrunner.Travel(d, questplan.ArravJonnyAnchor, 4) {
			logf(d, "arrav: could not reach the Blue Moon Inn")
			return false
		}
		n, ok := questrunner.FindNpcByID(d, questplan.NpcArravJonny, "Jonny the beard", 12)
		if !ok {
			d.Delay(3 * time.Second) // he respawns
			continue
		}
		died := tile.Tile{X: n.X, Z: n.Z, Level: n.Level}
		questrunner.FightBack(d, n, questplan.Sustain{EatAt: 10})
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			if questrunner.TakeGround(d, questplan.ObjArravReport, died, 3) || questrunner.HeldID(d, questplan.ObjArravReport) > 0 {
				return true
			}
			d.Delay(400 * time.Millisecond)
		}
	}
	return questrunner.HeldID(d, questplan.ObjArravReport) > 0
}

// arravSearchDialogue continues the mesboxes an object search raises ("You search the chest." and the item box) until none is left.
func arravSearchDialogue(d *questrunner.Deps) {
	for i := 0; i < 5; i++ {
		dl := questrunner.NewDialogue(d)
		if !waitUntil(d, 2*time.Second, dl.Ready) {
			return
		}
		dl.Drive(nil)
		d.Delay(500 * time.Millisecond)
	}
	questrunner.ClearLeftoverModal(d)
}

// arravPhoenixHalf takes half a shield from the Phoenix chest: down to the cellar, through the member's door, open the chest, search it.
func arravPhoenixHalf(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjArravShield1) > 0 {
		return true
	}
	if !questplan.InPhoenixCellar(questrunner.CurrentTile(d)) && !arravDown(e, questplan.Step{}) {
		return false
	}
	// The hideout door teleports a member through; try once from where the ladder lands, and once more from the door's own room.
	if !arravLoc(e, questplan.LocArravPhDoor, "Door", "Open", questplan.ArravPhoenixDoor) {
		logf(d, "arrav: the Phoenix hideout door did not open")
		return false
	}
	arravSearchDialogue(d)
	for try := 0; try < 3 && questrunner.HeldID(d, questplan.ObjArravShield1) == 0; try++ {
		if !questrunner.Travel(d, questplan.ArravPhoenixChest, 2) {
			logf(d, "arrav: could not walk to the Phoenix chest")
			return false
		}
		if m, ok := questrunner.FindLoc(d, questplan.LocArravPhChest, "", "Open", questplan.ArravPhoenixChest, 4); ok {
			if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
				logf(d, "arrav: open the chest: %v", err)
			}
			d.Delay(1200 * time.Millisecond)
		}
		if m, ok := questrunner.FindLoc(d, questplan.LocArravPhChestOp, "", "Search", questplan.ArravPhoenixChest, 4); ok {
			if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
				logf(d, "arrav: search the chest: %v", err)
			}
			arravSearchDialogue(d)
		}
	}
	return questrunner.HeldID(d, questplan.ObjArravShield1) > 0
}

// arravBlackArmHalf takes half a shield from the Black Arm cupboard upstairs in the hideout.
func arravBlackArmHalf(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjArravShield2) > 0 {
		return true
	}
	if questrunner.CurrentTile(d).Level == 0 {
		if !arravLoc(e, questplan.LocArravBaDoor, "Door", "Open", questplan.ArravBaDoor) {
			logf(d, "arrav: the Black Arm door did not open")
			return false
		}
		d.Delay(1500 * time.Millisecond)
		if !arravLoc(e, questplan.LocArravStairs, "Staircase", "Climb-up", questplan.ArravBaStairs) {
			return false
		}
		if !waitUntil(d, 6*time.Second, func() bool { return questrunner.CurrentTile(d).Level == 1 }) {
			return false
		}
	}
	for try := 0; try < 3 && questrunner.HeldID(d, questplan.ObjArravShield2) == 0; try++ {
		if !questrunner.Travel(d, questplan.ArravBaCupboard, 3) {
			return false
		}
		if m, ok := questrunner.FindLoc(d, questplan.LocArravBaCupShut, "", "Open", questplan.ArravBaCupboard, 4); ok {
			if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
				logf(d, "arrav: open the cupboard: %v", err)
			}
			d.Delay(1200 * time.Millisecond)
		}
		if m, ok := questrunner.FindLoc(d, questplan.LocArravBaCupOpen, "", "Search", questplan.ArravBaCupboard, 4); ok {
			if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
				logf(d, "arrav: search the cupboard: %v", err)
			}
			arravSearchDialogue(d)
		}
	}
	return questrunner.HeldID(d, questplan.ObjArravShield2) > 0
}

// arravCrossbows takes two Phoenix crossbows from the weapon store: the Phoenix key on its door, the ladder up, the Weaponsmaster (level 23,
// hostile to a non-member) killed, then the crossbows. A death caps the step (ArravQuest's StepDeathCaps).
func arravCrossbows(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjArravCrossbow) >= 2 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjArravKey) == 0 {
		logf(d, "arrav: the weapon store's door needs the Phoenix key")
		return false
	}
	if questrunner.CurrentTile(d).Level == 0 {
		if !questrunner.Travel(d, questplan.ArravStoreDoor, 2) {
			return false
		}
		if m, ok := questrunner.FindLoc(d, questplan.LocArravPhDoor2, "", "Open", questplan.ArravStoreDoor, 3); ok {
			if err := questrunner.UseItemOnLoc(d, questplan.ObjArravKey, m); err != nil {
				logf(d, "arrav: key on the weapon store door: %v", err)
				return false
			}
			d.Delay(2 * time.Second)
		}
		if !arravLoc(e, questplan.LocArravStoreLdr, "Ladder", "Climb-up", questplan.ArravStoreLadder) {
			return false
		}
		if !waitUntil(d, 6*time.Second, func() bool { return questrunner.CurrentTile(d).Level == 1 }) {
			return false
		}
	}
	for try := 0; try < 4 && questrunner.HeldID(d, questplan.ObjArravCrossbow) < 2; try++ {
		if n, ok := questrunner.FindNpcByID(d, questplan.NpcArravWeaponmaster, "Weaponsmaster", 14); ok {
			questrunner.FightBack(d, n, questplan.Sustain{EatAt: 12})
			d.Delay(800 * time.Millisecond)
		}
		for _, at := range []tile.Tile{questplan.ArravCrossbowA, questplan.ArravCrossbowB} {
			if questrunner.HeldID(d, questplan.ObjArravCrossbow) >= 2 {
				break
			}
			if len(questrunner.GroundItemsNear(d, questplan.ObjArravCrossbow, at, 2)) == 0 {
				continue
			}
			questrunner.TakeGround(d, questplan.ObjArravCrossbow, at, 2)
			arravSearchDialogue(d) // the Weaponsmaster's "Stop! thief!" if he is still near
		}
	}
	return questrunner.HeldID(d, questplan.ObjArravCrossbow) >= 2
}

// arravKey meets the Phoenix account in the museum and takes its key.
func arravKey(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if d.Pair == nil {
		return false
	}
	if !questrunner.Travel(d, questplan.ArravMeetAnchor, 3) {
		return false
	}
	deadline := time.Now().Add(min(arravMeetBudget, max(e.RunLeft(), time.Minute)))
	for time.Now().Before(deadline) {
		if questrunner.HeldID(d, questplan.ObjArravKey) > 0 {
			return true
		}
		d.Pair.Tell(arravTell(d, true, true))
		st := d.Pair.State()
		if st.View.Present && st.View.Gang == questplan.GangPhoenix {
			if arravPairTrade(d, st.Partner, nil, map[int]int{questplan.ObjArravKey: 1}) {
				return true
			}
		}
		if !d.Delay(3 * time.Second) {
			return false
		}
	}
	d.Pair.Tell(arravTell(d, false, false))
	logf(d, "arrav: the Phoenix account did not bring its key in %s", arravMeetBudget)
	return false
}

// arravTell is this account's report for the partner, read from the pack.
func arravTell(d *questrunner.Deps, waiting, needsKey bool) questplan.ArravTell {
	n := func(id int) int { return questrunner.HeldID(d, id) }
	half := n(questplan.ObjArravShield1) > 0 || n(questplan.ObjArravShield2) > 0
	return questplan.ArravTell{
		HasHalf: half && !(n(questplan.ObjArravShield1) > 0 && n(questplan.ObjArravShield2) > 0),
		HasBoth: n(questplan.ObjArravShield1) > 0 && n(questplan.ObjArravShield2) > 0,
		HasCert: n(questplan.ObjArravCert) > 0, Certs: n(questplan.ObjArravCert),
		HasKey: n(questplan.ObjArravKey) > 0, NeedsKey: needsKey, Waiting: waiting,
	}
}

// arravMeet is the museum: ArravNext against the partner's reports until the account holds the one certificate that is its own.
func arravMeet(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if d.Pair == nil {
		return false
	}
	if !questrunner.Travel(d, questplan.ArravMeetAnchor, 3) {
		return false
	}
	start := time.Now()
	deadline := start.Add(min(arravMeetBudget, max(e.RunLeft(), time.Minute)))
	maxWait := int(arravMeetBudget / (3 * time.Second))
	for ticks := 0; time.Now().Before(deadline); ticks++ {
		st := d.Pair.State()
		tell := arravTell(d, true, false)
		d.Pair.Tell(tell)
		me := questplan.ArravMe{Gang: st.Gang, Stage: arravStageOf(st.Gang), HasHalf: tell.HasHalf, HasBoth: tell.HasBoth,
			HasCert: tell.HasCert, Certs: tell.Certs}
		// The Phoenix account carries its key to the museum for a Black Arm partner that waits for it.
		if t, _, ok := d.Pair.Heard(); ok && st.Gang == questplan.GangPhoenix && tell.HasKey && t.NeedsKey && st.View.Present {
			arravPairTrade(d, st.Partner, map[int]int{questplan.ObjArravKey: 1}, nil)
			continue
		}
		step, why := questplan.ArravNext(me, st.View, ticks, maxWait)
		switch step {
		case questplan.ArravDone, questplan.ArravClaimReward:
			return true
		case questplan.ArravStop:
			logf(d, "arrav: stop: %s", why)
			return false
		case questplan.ArravGiveHalf:
			arravPairTrade(d, st.Partner, map[int]int{arravHalfID(d): 1}, nil)
		case questplan.ArravTakeCert:
			if me.HasHalf && st.Gang == questplan.GangPhoenix {
				arravPairTrade(d, st.Partner, nil, map[int]int{questplan.ObjArravShield2: 1})
			} else {
				arravPairTrade(d, st.Partner, nil, map[int]int{questplan.ObjArravCert: 1})
			}
		case questplan.ArravGiveCert:
			arravPairTrade(d, st.Partner, map[int]int{questplan.ObjArravCert: 1}, nil)
		case questplan.ArravHandIn:
			ok := e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.NpcStop{Npc: "Curator", ID: questplan.NpcArravCurator,
				Anchor: questplan.ArravCuratorAnchor, Leash: 8, Prefer: []string{"I have retrieved the shield of Arrav", "Can I have two certificates?"}}})
			if !ok && questrunner.HeldID(d, questplan.ObjArravCert) == 0 {
				logf(d, "arrav: the Curator gave no certificates")
				return false
			}
		case questplan.ArravWaitPartner:
			// Wait in the museum; the partner's own run comes to the same tile.
		default:
			logf(d, "arrav: unexpected step %s (%s)", step, why)
			return false
		}
		if !d.Delay(3 * time.Second) {
			return false
		}
	}
	d.Pair.Tell(arravTell(d, false, false))
	logf(d, "arrav: the museum meeting timed out after %s", time.Since(start).Round(time.Second))
	return false
}

// arravHalfID is the half this account holds.
func arravHalfID(d *questrunner.Deps) int {
	if questrunner.HeldID(d, questplan.ObjArravShield2) > 0 {
		return questplan.ObjArravShield2
	}
	return questplan.ObjArravShield1
}

// arravStageOf is the stage the gang's join counts as: only "joined or beyond" matters to ArravNext, and the decision never reaches the
// meeting before that.
func arravStageOf(g questplan.Gang) int {
	if g == questplan.GangPhoenix {
		return questplan.PhoenixJoined
	}
	return questplan.BlackArmJoined
}

// arravPairTrade trades with the partner: both accounts request each other until the offer screen opens (a "Trade with" on each side), then the
// side that gives offers its lines, and each side accepts only when what the partner offers is exactly what it expects. Anything else is
// declined: the partner is one of ours, and a trade that does not match is a bug to stop on, not a gift. Bounded by arravTradeBudget.
func arravPairTrade(d *questrunner.Deps, partner string, give, want map[int]int) bool {
	tr := actions.NewTrade(d.Session)
	before := map[int]int{}
	for id := range give {
		before[id] = questrunner.HeldID(d, id)
	}
	for id := range want {
		before[id] = questrunner.HeldID(d, id)
	}
	done := func() bool {
		for id, n := range give {
			if questrunner.HeldID(d, id) > before[id]-n {
				return false
			}
		}
		for id, n := range want {
			if questrunner.HeldID(d, id) < before[id]+n {
				return false
			}
		}
		return true
	}
	deadline := time.Now().Add(arravTradeBudget)
	lastAsk, lastAccept := time.Time{}, time.Time{}
	for time.Now().Before(deadline) {
		if done() {
			questrunner.ClearLeftoverModal(d)
			return true
		}
		if !tr.Active() {
			if time.Since(lastAsk) > 4*time.Second {
				lastAsk = time.Now()
				_ = tr.RequestPlayer(partner)
			}
			d.Delay(600 * time.Millisecond)
			continue
		}
		if tr.OnOfferScreen() {
			if p := tr.Partner(); p != "" && !actions.SameUsername(p, partner) {
				logf(d, "arrav: a trade with %q, not the partner: declined", p)
				_ = tr.Decline(3 * time.Second)
				return false
			}
			if !arravOfferOK(tr.TheirOffer(), want) {
				logf(d, "arrav: the partner's offer is not what was agreed: declined")
				_ = tr.Decline(3 * time.Second)
				return false
			}
			var lines []actions.TradeLine
			for id, n := range give {
				if actions.CountByID(tr.MyOffer(), int32(id)) < int32(n) {
					lines = append(lines, actions.TradeLine{ID: int32(id), Qty: int32(n)})
				}
			}
			if len(lines) > 0 {
				tr.OfferMany(lines, nil)
			}
			mineOK, theirsOK := true, true
			for id, n := range give {
				mineOK = mineOK && actions.CountByID(tr.MyOffer(), int32(id)) >= int32(n)
			}
			for id, n := range want {
				theirsOK = theirsOK && actions.CountByID(tr.TheirOffer(), int32(id)) >= int32(n)
			}
			if mineOK && theirsOK && time.Since(lastAccept) > 2*time.Second {
				lastAccept = time.Now()
				_ = tr.Accept()
			}
		} else if tr.OnConfirmScreen() {
			mine, theirs := tr.ConfirmOffers()
			if !arravOfferOK(theirs, want) || !arravOfferOK(mine, give) {
				logf(d, "arrav: the confirm screen is not what was agreed: declined")
				_ = tr.Decline(3 * time.Second)
				return false
			}
			if time.Since(lastAccept) > 2*time.Second {
				lastAccept = time.Now()
				_ = tr.Accept()
			}
		}
		d.Delay(600 * time.Millisecond)
	}
	if tr.Active() {
		_ = tr.Decline(3 * time.Second)
	}
	return done()
}

// arravOfferOK is whether a side of a trade screen holds only objects in allowed (nothing at all is fine: not offered yet).
func arravOfferOK(items []gamestate.SlotItem, allowed map[int]int) bool {
	for _, it := range items {
		if _, ok := allowed[int(it.ID)]; !ok {
			return false
		}
	}
	return true
}
