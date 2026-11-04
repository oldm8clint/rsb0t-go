package questmods

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// SheepHerder is Sheep Herder ready for the engine. Halgrive's and Orbon's talks, the coins (the provisioner's kit), the suit and the prod
// worn are the engine's generic steps; the customs are the legs the content makes special: the prod from the barn inside the enclosure
// (a gate that is a teleport), a sheep herded to the fence by prodding and fed the poison inside it (the push loop of
// questplan.HerdLoop against the live session), and the remains burnt in the incinerator.
func SheepHerder() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.SheepHerder,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomHerdProd:  herdFetchProd,
			questplan.CustomHerdSheep: herdSheep,
			questplan.CustomHerdBurn:  herdBurn,
		},
	}
}

const (
	// herdCrossWait is how long the gate gets to teleport the account across: it is a p_teleport with a one tick delay.
	herdCrossWait = 8 * time.Second
	// herdProdWait is how long a Prod gets to move the sheep: the server walks the account to the stand, runs the script, and the sheep
	// takes its step a tick later.
	herdProdWait = 6 * time.Second
	// herdWalkWait is the longest one stand-to-stand walk is given.
	herdWalkWait = 10 * time.Second
	// herdLegBudget bounds one herding leg: the TS def measured 37 to 255 pushes for one sheep and 11 to 20 minutes for all four.
	herdLegBudget = 14 * time.Minute
	// herdProdRespawn is how long the barn is given to show a prod when none lies there: a prod comes back 100 ticks after a pickup.
	herdProdRespawn = 90 * time.Second
	// herdPoisonWait is how long the feed gets to kill the sheep and leave its remains (a message box to continue, two ticks and the drop).
	herdPoisonWait = 20 * time.Second
	// herdBurnWait is how long the incinerator gets to burn: three ticks after the remains go in.
	herdBurnWait = 10 * time.Second
	// herdTimeLeft is the run time below which a leg stops starting pushes.
	herdTimeLeft = 20 * time.Second
)

// packHerdGrid is questplan.HerdGrid over the collision pack, ground floor.
type packHerdGrid struct{ pk *pathfind.Pack }

func (g packHerdGrid) Walkable(x, z int) bool { return g.pk.Walkable(x, z, 0) }

// CanStep reads the pack's exit mask (bit 0 north, 1 east, 2 south, 3 west, in the order of questplan.HerdDirs).
func (g packHerdGrid) CanStep(x, z, dx, dz int) bool {
	for i, d := range questplan.HerdDirs {
		if d.DX == dx && d.DZ == dz {
			return g.pk.ExitMask(x, z, 0)&(1<<uint(i)) != 0
		}
	}
	return false
}

// herdLive is the live session as the push loop's world.
type herdLive struct {
	d    *questrunner.Deps
	e    *questrunner.Engine
	n    int
	stop time.Time
}

func (h *herdLive) npcs() []entityquery.Entity {
	id := questplan.NpcPlagueSheep(h.n)
	return entityquery.Npcs(h.d.Session.State(), h.d.Config).Where(func(e entityquery.Entity) bool { return e.ID == id }).Results()
}

func (h *herdLive) Copies() []questplan.HerdPos {
	var out []questplan.HerdPos
	for _, e := range h.npcs() {
		if e.Level == 0 {
			out = append(out, questplan.HerdPos{X: e.X, Z: e.Z})
		}
	}
	return out
}

// Find is the copy of the sheep with the shortest push route home, walking to the sheep's map spawn first when none is in view and
// waiting out the respawn when the field is empty (npc_del on a map spawn queues one).
func (h *herdLive) Find() (questplan.HerdPos, bool) {
	d, n := h.d, h.n
	grid := packHerdGrid{pk: d.Pack}
	if p, ok := questplan.HerdPick(grid, questplan.HerdGateZone, h.Copies()); ok {
		return p, true
	}
	spawn := questplan.HerdSheepSpawns[n-1]
	logf(d, "sheepherder: no movable sheep %d in view: walking to its field at %v", n, spawn)
	if !questrunner.Travel(d, spawn, 4) {
		logf(d, "sheepherder: the field of sheep %d at %v was not reached from %v", n, spawn, questrunner.CurrentTile(d))
		return questplan.HerdPos{}, false
	}
	d.Delay(1200 * time.Millisecond)
	if p, ok := questplan.HerdPick(grid, questplan.HerdGateZone, h.Copies()); ok {
		return p, true
	}
	logf(d, "sheepherder: sheep %d is not in its field: waiting out the respawn", n)
	d.Delay(5 * time.Second)
	if p, ok := questplan.HerdPick(grid, questplan.HerdGateZone, h.Copies()); ok {
		return p, true
	}
	logf(d, "sheepherder: no sheep %d at its field", n)
	return questplan.HerdPos{}, false
}

// Approach walks to within six tiles of the sheep by the long walker.
func (h *herdLive) Approach(p questplan.HerdPos) bool {
	if !questrunner.Travel(h.d, tile.Tile{X: p.X, Z: p.Z}, 6) {
		logf(h.d, "sheepherder: the sheep at (%d,%d) was not reached from %v", p.X, p.Z, questrunner.CurrentTile(h.d))
		return false
	}
	return true
}

func (h *herdLive) Me() (questplan.HerdPos, bool) {
	t, ok := questrunner.CurrentTileOK(h.d)
	return questplan.HerdPos{X: t.X, Z: t.Z}, ok && t.Level == 0
}

// WalkTo walks to exactly (x, z): a click on the route from here, waited for until the account stands there, stops moving, or the wait
// runs out. Within sight of the sheep the walk is the client's own; a stand far off goes through the long walker.
func (h *herdLive) WalkTo(x, z int) bool {
	d := h.d
	dest := tile.Tile{X: x, Z: z}
	at := func() bool { return questrunner.CurrentTile(d) == dest }
	if at() {
		return true
	}
	here := questrunner.CurrentTile(d)
	if here.Level != 0 || tile.Chebyshev(here, dest) > 14 || d.Pack == nil {
		return questrunner.Travel(d, dest, 0) && at()
	}
	res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z}, pathfind.NavPoint{X: x, Z: z})
	if !res.OK || len(res.Waypoints) < 2 {
		return false
	}
	questrunner.ClearLeftoverModal(d)
	if err := actions.Walk(d.Session, res.Waypoints, true); err != nil {
		logf(d, "sheepherder: walking to (%d,%d): %v", x, z, err)
		return false
	}
	deadline := time.Now().Add(herdWalkWait)
	last, moved := here, time.Now()
	for time.Now().Before(deadline) {
		if !d.Delay(150 * time.Millisecond) {
			return false
		}
		if at() {
			return true
		}
		if cur := questrunner.CurrentTile(d); cur != last {
			last, moved = cur, time.Now()
		} else if time.Since(moved) > 2500*time.Millisecond {
			break
		}
	}
	return at()
}

func (h *herdLive) Prod(at questplan.HerdPos) questplan.HerdProd {
	var target entityquery.Entity
	found := false
	for _, e := range h.npcs() {
		if e.X == at.X && e.Z == at.Z && e.Level == 0 {
			target, found = e, true
			break
		}
	}
	if !found {
		return questplan.HerdProd{}
	}
	if err := entityquery.Interact(h.d.Session, target, "Prod"); err != nil {
		logf(h.d, "sheepherder: no Prod on the sheep at (%d,%d): %v", at.X, at.Z, err)
		return questplan.HerdProd{}
	}
	out := questplan.HerdProd{Sent: true}
	moved := waitUntil(h.d, herdProdWait, func() bool {
		now, ok := questplan.HerdNearest(h.Copies(), at)
		return !ok || now != at
	})
	clearChat(h.d)
	if !moved {
		return out
	}
	out.Moved = true
	if after, ok := questplan.HerdNearest(h.Copies(), at); ok {
		out.After, out.Seen = after, true
	}
	return out
}

func (h *herdLive) Wait(ticks int) { h.d.Delay(time.Duration(ticks) * 600 * time.Millisecond) }

// runLow reports that the engine's run is nearly out of time.
func (h *herdLive) runLow() bool {
	if h.e == nil {
		return false
	}
	left := h.e.RunLeft()
	return left > 0 && left < herdTimeLeft
}

func (h *herdLive) Stop() bool { return time.Now().After(h.stop) || h.runLow() }

func (h *herdLive) Logf(format string, args ...any) { logf(h.d, "sheepherder: "+format, args...) }

// herdGate crosses the enclosure's gate, which is a teleport: the account stands on the stand, opens the gate and lands on the far side,
// and where it stands afterwards is the only oracle (want).
func herdGate(d *questrunner.Deps, stand tile.Tile, want func() bool) bool {
	if want() {
		return true
	}
	if !questrunner.Travel(d, stand, 0) && questrunner.CurrentTile(d) != stand {
		logf(d, "sheepherder: the gate's stand %v was not reached from %v", stand, questrunner.CurrentTile(d))
		return false
	}
	if want() {
		return true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocPlagueGateRight, "", "Open", stand, 3)
	if !ok {
		logf(d, "sheepherder: no enclosure gate (loc %d) near %v", questplan.LocPlagueGateRight, stand)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		logf(d, "sheepherder: the gate: %v", err)
		return false
	}
	crossed := waitUntil(d, herdCrossWait, want)
	clearChat(d)
	if !crossed {
		logf(d, "sheepherder: the gate did not take the account across (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(600 * time.Millisecond)
	return true
}

// clearChat continues and closes whatever the last op raised.
func clearChat(d *questrunner.Deps) {
	dl := questrunner.NewDialogue(d)
	for i := 0; i < 8 && dl.Ready(); i++ {
		dl.Drive(nil)
	}
	questrunner.ClearLeftoverModal(d)
}

func herdInsidePen(d *questrunner.Deps) bool { return questplan.InHerdPen(questrunner.CurrentTile(d)) }

// herdEnterPen opens the gate from the fence's west tile, wearing the suit (the clothing is checked only there).
func herdEnterPen(d *questrunner.Deps) bool {
	return herdGate(d, questplan.HerdGateOutside, func() bool { return herdInsidePen(d) })
}

// herdLeavePen opens the gate from inside, which checks nothing.
func herdLeavePen(d *questrunner.Deps) bool {
	return herdGate(d, questplan.HerdGateInside, func() bool { return !herdInsidePen(d) })
}

// herdFetchProd takes the prod off the barn floor inside the enclosure. It does not wield it: the decision's equip step does, behind the
// room the swap needs.
func herdFetchProd(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjCattleProd) > 0 {
		return true
	}
	if !herdEnterPen(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.HerdProdSpawn, 1) {
		logf(d, "sheepherder: the barn at %v was not reached from %v", questplan.HerdProdSpawn, questrunner.CurrentTile(d))
		return false
	}
	deadline := time.Now().Add(herdProdRespawn)
	for time.Now().Before(deadline) {
		if questrunner.TakeGround(d, questplan.ObjCattleProd, questplan.HerdProdSpawn, 8) || questrunner.HeldID(d, questplan.ObjCattleProd) > 0 {
			return true
		}
		if !d.Delay(3 * time.Second) {
			return false
		}
	}
	logf(d, "sheepherder: no prod on the barn floor at %v in %s", questplan.HerdProdSpawn, herdProdRespawn)
	return false
}

// herdSheepInPen is the nearest copy of sheep n standing inside the enclosure.
func herdSheepInPen(d *questrunner.Deps, n int) (entityquery.Entity, bool) {
	id := questplan.NpcPlagueSheep(n)
	return entityquery.Npcs(d.Session.State(), d.Config).Where(func(e entityquery.Entity) bool {
		return e.ID == id && questplan.InHerdPen(tile.Tile{X: e.X, Z: e.Z, Level: e.Level})
	}).Nearest()
}

// herdTakeBones picks sheep n's remains off the floor near the account, a few tries (the drop lands a tick or two after the kill).
func herdTakeBones(d *questrunner.Deps, n int) bool {
	id := questplan.ObjSheepBones(n)
	for attempt := 0; attempt < 4; attempt++ {
		if questrunner.HeldID(d, id) > 0 {
			return true
		}
		if len(questrunner.GroundItemsNear(d, id, questrunner.CurrentTile(d), 12)) == 0 {
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
			continue
		}
		questrunner.TakeGround(d, id, questrunner.CurrentTile(d), 12)
	}
	if questrunner.HeldID(d, id) > 0 {
		return true
	}
	logf(d, "sheepherder: no remains of sheep %d to pick up", n)
	return false
}

// herdPoison feeds the poison to sheep n inside the enclosure and takes its remains. The sheep there is a 150 tick npc_add, so the kill
// runs in the same leg as the herd.
func herdPoison(d *questrunner.Deps, n int) bool {
	if questrunner.HeldID(d, questplan.ObjSheepBones(n)) > 0 {
		return true
	}
	if !herdEnterPen(d) {
		return false
	}
	sheep, ok := herdSheepInPen(d, n)
	if !ok {
		// Gone from the enclosure (killed already, or its 150 ticks ran out): its remains may be on the floor.
		return herdTakeBones(d, n)
	}
	if questrunner.HeldID(d, questplan.ObjSheepFeed) == 0 {
		logf(d, "sheepherder: no poisoned feed in the pack")
		return false
	}
	logf(d, "sheepherder: feeding the poison to sheep %d", n)
	mark := time.Now()
	if err := questrunner.UseItemOnNpc(d, questplan.ObjSheepFeed, sheep); err != nil {
		logf(d, "sheepherder: the feed on sheep %d: %v", n, err)
		return false
	}
	// The script raises a message box and holds on its continue; the drop follows. Continue it, never close it.
	dl := questrunner.NewDialogue(d)
	dropped := waitUntil(d, herdPoisonWait, func() bool {
		if dl.Ready() {
			dl.Drive(nil)
		}
		return len(questrunner.GroundItemsNear(d, questplan.ObjSheepBones(n), questrunner.CurrentTile(d), 12)) > 0 ||
			questrunner.HeldID(d, questplan.ObjSheepBones(n)) > 0
	})
	clearChat(d)
	if !dropped {
		if cookSaw(d, mark, "already disposed", "haven't got") {
			logf(d, "sheepherder: sheep %d was refused the feed", n)
		}
		logf(d, "sheepherder: sheep %d left no remains within %s", n, herdPoisonWait)
	}
	return herdTakeBones(d, n)
}

// herdSheep is one sheep's whole leg: herd it to the fence, over it, feed it and leave holding the remains.
func herdSheep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	n := step.Qty
	if n < 1 || n > 4 {
		logf(d, "sheepherder: the herd step names sheep %d", n)
		return false
	}
	if questrunner.HeldID(d, questplan.ObjSheepBones(n)) > 0 {
		return true
	}
	if len(questrunner.GroundItemsNear(d, questplan.ObjSheepBones(n), questrunner.CurrentTile(d), 12)) > 0 {
		return herdTakeBones(d, n)
	}
	if _, in := herdSheepInPen(d, n); in {
		return herdPoison(d, n)
	}
	if herdInsidePen(d) && !herdLeavePen(d) {
		return false
	}
	if d.Pack == nil {
		logf(d, "sheepherder: no collision pack to plan the pushes on")
		return false
	}
	h := &herdLive{d: d, e: e, n: n, stop: time.Now().Add(herdLegBudget)}
	res := questplan.HerdLeg(h, packHerdGrid{pk: d.Pack}, questplan.HerdPen, questplan.HerdGateZone)
	if !res.Penned {
		logf(d, "sheepherder: sheep %d not penned after %d push(es): %s", n, res.Pushes, res.Why)
		if res.Why == "out of time" && h.runLow() {
			// The slot ran out in the middle of a herd, not the sheep's fault: the run ends as a timeout, without the half hour's park.
			e.YieldRun()
			return true
		}
		return false
	}
	return herdPoison(d, n)
}

// herdBurn puts sheep n's remains into the incinerator from its west side and waits for the line that says they burnt (the script deletes
// the remains, waits three ticks and only then sets the bits, so the pack emptying is not the end of it).
func herdBurn(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	n := step.Qty
	if n < 1 || n > 4 {
		logf(d, "sheepherder: the burn step names sheep %d", n)
		return false
	}
	id := questplan.ObjSheepBones(n)
	if questrunner.HeldID(d, id) == 0 {
		return true
	}
	if !herdEnterPen(d) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocPlagueFurnace, "", "", questplan.HerdFurnaceStand, 6)
	if !ok {
		logf(d, "sheepherder: no incinerator (loc %d) near %v", questplan.LocPlagueFurnace, questplan.HerdFurnaceStand)
		return false
	}
	mark := time.Now()
	logf(d, "sheepherder: burning the remains of sheep %d", n)
	if err := questrunner.UseItemOnLoc(d, id, m); err != nil {
		logf(d, "sheepherder: the remains on the incinerator: %v", err)
		return false
	}
	burnt := waitUntil(d, herdBurnWait, func() bool { return cookSaw(d, mark, "remains burn to dust") })
	clearChat(d)
	if !burnt {
		logf(d, "sheepherder: the remains of sheep %d did not burn (still held: %v)", n, questrunner.HeldID(d, id) > 0)
		return false
	}
	return true
}

// The enclosure is a pocket: its gate is a teleport the walk graph does not carry in either direction, so a walk from inside leaves by
// the gate first.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the plague sheep enclosure", In: questplan.InHerdPen, Leave: herdLeavePen})
}
