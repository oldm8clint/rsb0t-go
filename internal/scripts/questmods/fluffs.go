package questmods

import (
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// GertrudesCat is Gertrude's Cat ready for the engine. The talk that starts it, the shops and the provisioning passes are the
// engine's generic steps; the executors below are the quest's own: the payment that needs both brothers standing close, the
// seasoning, the milk (Cook's Assistant's proven step), the offers to Fluffs on the lumber yard's walkway, the crate search
// and the way back out over the fence to Gertrude.
func GertrudesCat() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.GertrudesCat,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomFluffsPay:    fluffsPay,
			questplan.CustomFluffsSeason: fluffsSeason,
			questplan.CustomFluffsMilk:   cookMilk,
			questplan.CustomFluffsOffer:  fluffsOffer,
			questplan.CustomFluffsCrates: fluffsCrates,
			questplan.CustomFluffsReport: fluffsReport,
		},
	}
}

const (
	// fluffsPairWait is how long one attempt waits for the brothers to stand within questplan.FluffsBrotherGap of each other.
	fluffsPairWait = 90 * time.Second
	// fluffsFenceTries bounds the climbs over the fence in one crossing: another account's climb locks it for three ticks.
	fluffsFenceTries = 5
	// fluffsStepTries bounds the one-tile clicks that take the account off the fence tile; fluffsStepPolls is how many 200 ms looks
	// each is given to show the new tile. The climb's exact move leaves the account unable to take a click for a couple of ticks, so
	// a click sent straight after it can be dropped.
	fluffsStepTries = 6
	fluffsStepPolls = 10
	// fluffsOfferTries bounds the uses of one gift on Fluffs: the first use after a ladder climb can be dropped.
	fluffsOfferTries = 3
	// fluffsOfferWait is how long one use of a gift on Fluffs is given to take it.
	fluffsOfferWait = 12 * time.Second
	// fluffsCutscene is how long the kitten's hand-over runs (p_delay 1, 0, 1 and 4 ticks, plus the closing message box).
	fluffsCutscene = 7 * time.Second
	// fluffsSearchWait is how long one crate search is given: four ticks, the message box and the walk beside it.
	fluffsSearchWait = 20 * time.Second
	// fluffsCratesParkFor is how long the quest skips an account whose six crates all came up empty: the kitten is in exactly one
	// of them unless a copy sits in the bank or the stage is not what the journal said.
	fluffsCratesParkFor = time.Hour
)

// fluffsLandWait is how long a climb over the fence is given to show its landing tile (a var so a test can shorten it).
var fluffsLandWait = 8 * time.Second

func fluffsTileOf(e entityquery.Entity) tile.Tile { return tile.Tile{X: e.X, Z: e.Z, Level: e.Level} }

// fluffsStands are the tiles an interaction with whatever stands on at is accepted from (the collision pack's own rule), nearest
// to the account first, without at itself.
func fluffsStands(d *questrunner.Deps, at tile.Tile) []tile.Tile {
	if d.Pack == nil {
		return nil
	}
	from := questrunner.CurrentTile(d)
	var out []tile.Tile
	for _, p := range d.Pack.InteractTiles(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}) {
		if t := (tile.Tile{X: p.X, Z: p.Z, Level: p.Level}); t != at {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return tile.Chebyshev(from, out[i]) < tile.Chebyshev(from, out[j]) })
	return out
}

// fluffsApproach walks onto a tile an NPC on at can be spoken to or searched from, and reports whether it stands on one.
func fluffsApproach(d *questrunner.Deps, at tile.Tile) bool {
	if d.Pack == nil {
		return questrunner.WalkNear(d, at, 1)
	}
	here := questrunner.CurrentTile(d)
	if d.Pack.CanInteractFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}) {
		return true
	}
	for i, s := range fluffsStands(d, at) {
		if i >= 4 {
			break
		}
		if questrunner.WalkNear(d, s, 0) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The brothers
// ---------------------------------------------------------------------------

// fluffsAwaitBrothers stands beside the nearer of Shilop and Wilough until the two stand within questplan.FluffsBrotherGap of
// each other, and returns the stop of the nearer one. Opening the conversation then (the account already beside him) leaves the
// brothers the whole of the script's own distance limit to drift in: the script that takes the coins ends without a single
// option if the other brother is further than three tiles four chat lines in.
func fluffsAwaitBrothers(d *questrunner.Deps) (questplan.NpcStop, bool) {
	deadline := time.Now().Add(fluffsPairWait)
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return questplan.NpcStop{}, false
		}
		s, okS := questrunner.FindNpcByID(d, questplan.NpcShilop, "", 25)
		w, okW := questrunner.FindNpcByID(d, questplan.NpcWilough, "", 25)
		if okS && okW {
			near, stop := s, questplan.ShilopStop
			if w.Distance < s.Distance {
				near, stop = w, questplan.WilloughStop
			}
			beside := fluffsApproach(d, fluffsTileOf(near))
			if beside && questplan.BrothersPaired(fluffsTileOf(s), fluffsTileOf(w)) {
				return stop, true
			}
		}
		if !d.Delay(300 * time.Millisecond) {
			return questplan.NpcStop{}, false
		}
	}
	return questplan.NpcStop{}, false
}

// fluffsPay buys the lumber yard's place out of Shilop and Wilough (stage 1 to 2). It talks only while the brothers stand close
// together, drives the whole conversation (the stage is set at its very end, after the coins have gone and four more pages) and
// counts only the 100 coins leaving the pack as the payment: a conversation that ended without them is tried again.
func fluffsPay(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for attempt := 1; attempt <= questplan.FluffsPayAttempts; attempt++ {
		before := questrunner.HeldID(d, questplan.CoinsID)
		if before < questplan.FluffsBoyFee {
			logf(d, "fluffs: %d coins in the pack, the brothers want %d", before, questplan.FluffsBoyFee)
			return false
		}
		if !questrunner.TravelCautious(d, questplan.FluffsMarket, 3) {
			return false
		}
		stop, ok := fluffsAwaitBrothers(d)
		if !ok {
			logf(d, "fluffs: attempt %d of %d: Shilop and Wilough never stood within %d tile(s) of each other", attempt,
				questplan.FluffsPayAttempts, questplan.FluffsBrotherGap)
			continue
		}
		if !questrunner.OpenDialogue(d, stop) {
			continue
		}
		questrunner.NewDialogue(d).Drive(stop.Prefer)
		questrunner.ClearLeftoverModal(d)
		after := questrunner.HeldID(d, questplan.CoinsID)
		if after <= before-questplan.FluffsBoyFee {
			logf(d, "fluffs: paid %s %d coins for the lumber yard (attempt %d)", stop.Npc, before-after, attempt)
			return true
		}
		logf(d, "fluffs: attempt %d of %d: the conversation with %s ended without the payment (the brothers had drifted apart)",
			attempt, questplan.FluffsPayAttempts, stop.Npc)
		if !d.Delay(2 * time.Second) {
			return false
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The sardine
// ---------------------------------------------------------------------------

// fluffsSeason rubs the doogle leaves over the raw sardine. The script opens a message box first and converts the items only when
// it is dismissed, so the step presses its continue button (closing the modal instead would abort the script and leave the pack as
// it was).
func fluffsSeason(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	seasoned := func() bool { return questrunner.HeldID(d, questplan.FluffsSeasonedSardine) > 0 }
	for attempt := 0; attempt < 3 && !seasoned(); attempt++ {
		leaves, raw := questrunner.HeldID(d, questplan.FluffsDoogleLeaves), questrunner.HeldID(d, questplan.FluffsRawSardine)
		if leaves == 0 || raw == 0 {
			logf(d, "fluffs: %d doogle leaves and %d raw sardines in the pack, nothing to season", leaves, raw)
			return false
		}
		// A shop screen left half shut swallows the use with no refusal (the sardine was bought the tick before).
		questrunner.ClearLeftoverModal(d)
		if !d.Delay(600 * time.Millisecond) {
			return false
		}
		if err := questrunner.UseItemOnItem(d, questplan.FluffsDoogleLeaves, questplan.FluffsRawSardine); err != nil {
			logf(d, "fluffs: the doogle leaves on the raw sardine: %v", err)
			continue
		}
		questrunner.NewDialogue(d).DriveUntil(seasoned, nil, 8*time.Second)
	}
	if seasoned() {
		logf(d, "fluffs: a seasoned sardine")
	}
	return seasoned()
}

// ---------------------------------------------------------------------------
// The lumber yard
// ---------------------------------------------------------------------------

// fluffsFence climbs the broken fence in (inward) or out. The fence tile is the one gap in the yard's south wall and holds a
// loc, so the walker never plans through it; the climb is an exact move over the wall from the tile on the near side to the far
// one. The script decides the direction from the account's own z when it runs (south of the fence it climbs in, on the fence's row
// or north of it it climbs out), so an inward climb is made from the row south of the wall and an outward one from inside it.
//
// The climb in ends ON the fence's own tile, which is the fence loc's footprint: the collision pack has it blocked, so every pack
// walk from it fails or starts from the tile the pack snaps it to, outside the wall. The inward climb therefore does not return
// until fluffsStepOffFence has put the account on the yard's first free tile. While another account's climb holds the fence (three
// ticks) the script only says so, and the next try gets through; a refused outward climb leaves the account standing on the fence
// tile (the script teleports it there before it checks the lock), so every try starts by stepping off it.
func fluffsFence(d *questrunner.Deps, inward bool) bool {
	way := map[bool]string{true: "in", false: "out"}[inward]
	for attempt := 0; attempt < fluffsFenceTries; attempt++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if questrunner.CurrentTile(d) == questplan.YardFence {
			if !fluffsStepOffFence(d) {
				return false
			}
			if inward {
				return true
			}
		}
		// The climb has landed when the account stands on a new tile on the far side: a z threshold alone would be met by an account
		// that never moved.
		start := questrunner.CurrentTile(d)
		landed := func(t tile.Tile) bool {
			if t.Level != 0 || t == start {
				return false
			}
			if inward {
				return t.Z >= questplan.YardFence.Z
			}
			return t.Z <= questplan.YardOutside.Z
		}
		fence, ok := questrunner.FindLoc(d, questplan.LocYardFence, "", "Climb-over", questplan.YardFence, 5)
		if !ok {
			logf(d, "fluffs: no broken fence near %d,%d", questplan.YardFence.X, questplan.YardFence.Z)
			return false
		}
		if err := questrunner.OperateLoc(d, fence, "Climb-over"); err != nil {
			logf(d, "fluffs: climb the fence: %v", err)
			if !d.Delay(1500 * time.Millisecond) {
				return false
			}
			continue
		}
		if waitUntil(d, fluffsLandWait, func() bool { return landed(questrunner.CurrentTile(d)) }) {
			d.Delay(800 * time.Millisecond)
			if inward {
				return fluffsStepOffFence(d)
			}
			return true
		}
		if !d.Delay(1500 * time.Millisecond) {
			return false
		}
	}
	logf(d, "fluffs: the fence would not be climbed %s in %d tries", way, fluffsFenceTries)
	return false
}

// fluffsStepOffFence moves an account that stands on the fence's own tile one tile north, onto questplan.YardInside, and reports
// whether it stands off the fence tile (an account that is not on it already is done). It is one direct click on the next tile,
// never a pack walk: the pack has the fence tile blocked, so a pack path from it is unreachable or begins outside the wall. The
// click can be dropped while the climb's exact move is still running, so it is repeated, each time looking for the tile to change.
func fluffsStepOffFence(d *questrunner.Deps) bool {
	for try := 0; try < fluffsStepTries; try++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		here := questrunner.CurrentTile(d)
		if here != questplan.YardFence {
			return true
		}
		to := questplan.YardInside
		if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: here.X, Z: here.Z, Level: here.Level}, {X: to.X, Z: to.Z, Level: here.Level}}, false); err != nil {
			logf(d, "fluffs: step off the fence tile: %v", err)
		}
		for poll := 0; poll < fluffsStepPolls; poll++ {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			if questrunner.CurrentTile(d) != questplan.YardFence {
				return true
			}
		}
	}
	logf(d, "fluffs: could not step off the fence tile %d,%d in %d tries", questplan.YardFence.X, questplan.YardFence.Z, fluffsStepTries)
	return false
}

// fluffsLadder climbs the walkway's ladder (up) or down it, from a tile beside it.
func fluffsLadder(d *questrunner.Deps, up bool) bool {
	id, at, op, want := questplan.LocYardLadder, questplan.YardLadder, "Climb-up", 1
	if !up {
		id, at, op, want = questplan.LocYardTop, questplan.YardLadderTop, "Climb-down", 0
	}
	for attempt := 0; attempt < 3; attempt++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		ladder, ok := questrunner.FindLoc(d, id, "", op, at, 6)
		if !ok {
			logf(d, "fluffs: no ladder offering %q near %d,%d,%d", op, at.X, at.Z, at.Level)
			return false
		}
		if err := questrunner.OperateLoc(d, ladder, op); err != nil {
			logf(d, "fluffs: %s: %v", op, err)
			if !d.Delay(1500 * time.Millisecond) {
				return false
			}
			continue
		}
		if waitUntil(d, 10*time.Second, func() bool { return questrunner.CurrentTile(d).Level == want }) {
			// The climb leaves the account delayed for a tick: the first click after it is dropped.
			return d.Delay(1200 * time.Millisecond)
		}
	}
	return false
}

// fluffsInside reports whether the account stands inside the yard's wall on the ground floor: inside the yard's bounds and, where
// the collision pack is known, on ground the pack connects to the fence's far side (the yard's west side lies outside the wall).
// The fence's own tile counts as inside, because the climb in ends there, but the pack cannot plan from it: whoever finds the
// account on it steps it off first (fluffsStepOffFence).
func fluffsInside(d *questrunner.Deps) bool {
	t := questrunner.CurrentTile(d)
	if t == questplan.YardFence {
		return true
	}
	if !questplan.InYard(t) {
		return false
	}
	if d.Pack == nil {
		return true
	}
	return d.Pack.FindPath(pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level},
		pathfind.NavPoint{X: questplan.YardInside.X, Z: questplan.YardInside.Z, Level: 0}).OK
}

// fluffsEnterYard puts the account on the yard's ground floor, inside the wall: down from the walkway if it stands there, else
// round to the fence and over it.
func fluffsEnterYard(d *questrunner.Deps) bool {
	if questplan.OnWalkway(questrunner.CurrentTile(d)) {
		if !fluffsLadder(d, false) {
			return false
		}
	}
	if questrunner.CurrentTile(d) == questplan.YardFence {
		// A run resumed in the gap, or a step off it that was dropped: nothing plans from this tile.
		return fluffsStepOffFence(d)
	}
	if fluffsInside(d) {
		return true
	}
	if !questrunner.TravelCautious(d, questplan.YardOutside, 1) {
		return false
	}
	return fluffsFence(d, true)
}

// fluffsToWalkway puts the account on the raised walkway beside Fluffs.
func fluffsToWalkway(d *questrunner.Deps) bool {
	if questplan.OnWalkway(questrunner.CurrentTile(d)) {
		return true
	}
	if !fluffsEnterYard(d) {
		return false
	}
	// A tile beside the ladder: the climb lands on the same x,z a level up, and all of the ladder's neighbours have a walkway tile
	// there (checked on the real pack).
	if !questrunner.WalkNear(d, questplan.YardLadderStand, 1) {
		return false
	}
	return fluffsLadder(d, true)
}

// fluffsOffer uses one item on Fluffs: the bucket of milk (stage 2), the seasoned sardine (3) or the kitten (4). Only a use-on
// counts: Pick-up and Stroke make her claw for 3 damage and give nothing. The first use after the ladder can be dropped, so it is
// tried up to fluffsOfferTries times; the item leaving the pack is the proof.
func fluffsOffer(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	item := step.ItemID
	name := questrunner.ObjName(d, item)
	gone := func() bool { return questrunner.HeldID(d, item) == 0 }
	if gone() {
		return true
	}
	if !fluffsToWalkway(d) {
		return false
	}
	for attempt := 0; attempt < fluffsOfferTries && !gone(); attempt++ {
		cat, ok := questrunner.FindNpcByID(d, questplan.NpcFluffs, "", 20)
		if !ok {
			questrunner.WalkNear(d, questplan.CatStand, 2)
			d.Delay(3 * time.Second)
			continue
		}
		logf(d, "fluffs: offering Fluffs the %s", name)
		if err := questrunner.UseItemOnNpc(d, item, cat); err != nil {
			logf(d, "fluffs: the %s on Fluffs: %v", name, err)
			d.Delay(1500 * time.Millisecond)
			continue
		}
		questrunner.NewDialogue(d).DriveUntil(gone, nil, fluffsOfferWait)
	}
	if !gone() {
		logf(d, "fluffs: Fluffs did not take the %s", name)
		return false
	}
	fluffsSettle(d, item == questplan.FluffsKitten)
	return true
}

// fluffsSettle lets the chat boxes after a gift run out. The kitten's hand-over is a cutscene (it leaves the pack six ticks before
// Fluffs has walked home) that ends in a message box; the box is continued, not closed. It returns once the least wait is over and
// no box has been up for a moment.
func fluffsSettle(d *questrunner.Deps, cutscene bool) {
	dl := questrunner.NewDialogue(d)
	least := 2 * time.Second
	if cutscene {
		least = fluffsCutscene
	}
	start, lastChat := time.Now(), time.Time{}
	for time.Since(start) < least+10*time.Second {
		if dl.Ready() {
			dl.Drive(nil)
			lastChat = time.Now()
			continue
		}
		if time.Since(start) >= least && (lastChat.IsZero() || time.Since(lastChat) >= 1500*time.Millisecond) {
			return
		}
		if !d.Delay(300 * time.Millisecond) {
			return
		}
	}
}

// fluffsCrates searches the six crate NPCs, nearest first, for the kitten (stage 4). The crates are NPCs with the one op Search
// (kittens_mew): four ticks, then "You find a kitten!" in a message box and the kitten only once that is continued, or "You find
// nothing." The account walks beside each crate first (the pack plans the yard whole; the server's own walk to a far crate gives
// up short with no refusal). Every crate empty parks the quest for an hour with that said.
func fluffsCrates(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	kitten := func() bool { return questrunner.HeldID(d, questplan.FluffsKitten) > 0 }
	if kitten() {
		return true
	}
	if !fluffsEnterYard(d) {
		return false
	}
	left := append([]tile.Tile(nil), questplan.FluffsCrateTiles...)
	for len(left) > 0 && !kitten() {
		here := questrunner.CurrentTile(d)
		sort.SliceStable(left, func(i, j int) bool { return tile.Chebyshev(here, left[i]) < tile.Chebyshev(here, left[j]) })
		spot := left[0]
		left = left[1:]
		fluffsSearchCrate(d, spot, kitten)
	}
	if kitten() {
		logf(d, "fluffs: found Fluffs' kitten")
		return true
	}
	e.RequestPark("none of the six crates held the kitten (a copy in the bank blocks it, or the stage is not the sardine's)", fluffsCratesParkFor)
	return false
}

// fluffsSearchCrate searches the crate NPC standing at spot and waits for its answer.
func fluffsSearchCrate(d *questrunner.Deps, spot tile.Tile, kitten func() bool) {
	if !questrunner.WaitForGuardian(d) {
		return
	}
	crate, ok := entityquery.Npcs(d.Session.State(), d.Config).
		Where(func(n entityquery.Entity) bool {
			return n.ID == questplan.NpcKittenCrate && n.X == spot.X && n.Z == spot.Z
		}).Nearest()
	if !ok {
		logf(d, "fluffs: no crate standing at %d,%d", spot.X, spot.Z)
		return
	}
	if !fluffsApproach(d, spot) {
		logf(d, "fluffs: could not stand beside the crate at %d,%d", spot.X, spot.Z)
		return
	}
	mark := time.Now()
	if err := entityquery.Interact(d.Session, crate, "Search"); err != nil {
		logf(d, "fluffs: search the crate at %d,%d: %v", spot.X, spot.Z, err)
		return
	}
	logf(d, "fluffs: searching the crate at %d,%d", spot.X, spot.Z)
	nothing := func() bool { return cookSaw(d, mark, "you find nothing") }
	questrunner.NewDialogue(d).DriveUntil(func() bool { return kitten() || nothing() }, nil, fluffsSearchWait)
}

// ---------------------------------------------------------------------------
// Back to Gertrude
// ---------------------------------------------------------------------------

// fluffsReport takes the news back (stage 5): off the walkway, out over the fence, to Gertrude and through her last pages. The
// last pages end with a quest scroll some seconds after the final box, so the step waits for the tab to read complete and clears
// the scroll while it does.
func fluffsReport(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	dl := questrunner.NewDialogue(d)
	if dl.Ready() {
		// A resumed run can find the hand-over's closing box open: continued, never closed (a close aborts the script).
		dl.Drive(nil)
	}
	if questplan.OnWalkway(questrunner.CurrentTile(d)) {
		if !fluffsLadder(d, false) {
			return false
		}
	}
	if fluffsInside(d) && !fluffsFence(d, false) {
		return false
	}
	stop := questplan.GertrudeStop
	if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
		return false
	}
	dl.Drive(stop.Prefer)
	complete := func() bool {
		return questrunner.ReadStatus(d, questplan.GertrudesCatName) == questplan.StatusComplete
	}
	// The scroll comes a few ticks after the last box (two three-tick delays in the script): clear it as it opens.
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && !complete(); {
		if dl.Ready() {
			dl.Drive(nil)
		}
		questrunner.ClearLeftoverModal(d)
		if !d.Delay(600 * time.Millisecond) {
			break
		}
	}
	questrunner.ClearLeftoverModal(d)
	logf(d, "fluffs: Gertrude's last pages done (quest tab complete: %v)", complete())
	return complete()
}
