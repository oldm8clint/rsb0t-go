package crafterrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The I/O half of ProgressiveCrafter.ts's wool stage: dropWool, storeTrip, grabGroundShears, shear,
// backToPen and spinAt, driven against a live session. Each is one pass, as the TS steps are: it does
// a little and returns, and the bot's Loop re-derives the phase before calling again.

// ---- dropping ----

// Drop drops pack slots named one of names, up to crafterplan.DropGuard, five to a game tick: [opheld5,_] has no
// delay (content player/scripts/drop.rs2), so a pack of balls of wool is six ticks, not one slot and a wait for the
// pack to lose it a slot (about 0.7 s each, 19 s for 27 slots; twopertick-1012). Every slot is named from one
// reading of the pack, the burst is confirmed once, and a slot the server refused is dropped again one at a time.
// It returns how many were dropped.
func Drop(d *Deps, names []string) int {
	if d.Components == nil {
		d.logf("no interface archive configured — cannot drop")
		return 0
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	dropped := 0
	for pass := 0; pass < 3 && dropped < crafterplan.DropGuard; pass++ {
		slots := slotsNamed(d, names)
		if len(slots) > crafterplan.DropGuard-dropped {
			slots = slots[:crafterplan.DropGuard-dropped]
		}
		if len(slots) == 0 {
			break
		}
		r := eq.DropMany(d.Config, slots, Waiter(d))
		d.logf("crafter %s", r.Log())
		dropped += r.Dropped()
		if r.Dropped() == 0 || r.Err != nil {
			break
		}
	}
	return dropped
}

// DropJunk drops the first pack slot the store never sees (crafterplan.Junk), for a pack full of
// things the store refused.
func DropJunk(d *Deps) bool {
	pack := PackView(d)
	i := crafterplan.Junk(pack)
	if i < 0 {
		return false
	}
	return Drop(d, []string{pack[i].Name}) > 0
}

// ---- the general store ----

// StoreResult is what one store pass came to.
type StoreResult struct {
	// Opened is false when the pass only walked, or the store would not open.
	Opened       bool
	Sold, GP     int
	BoughtShears bool
}

// StoreTrip ports storeTrip: walk to the stand, open the store through either keeper, sell
// everything keep does not name (a name the store would not pay for goes into refused), and buy
// shears when wantShears.
func StoreTrip(d *Deps, stand tile.Tile, keep []string, refused map[string]bool, wantShears bool) StoreResult {
	var res StoreResult
	me, ok := currentTile(d)
	if !ok || me.Level != stand.Level || tile.Chebyshev(me, stand) > 3 {
		if !Walk(d, stand, 2, 3*time.Minute) {
			d.logf("could not reach the general store — retrying")
		}
		return res
	}
	shop := openShop(d, freshstart.ShopKeepers(), stand)
	if shop == nil {
		d.logf("could not open the general store (keepers: %s) — retrying", strings.Join(freshstart.ShopKeepers(), " / "))
		d.Delay(2 * time.Second)
		return res
	}
	res.Opened = true
	defer shop.Close()
	w := Waiter(d)
	for _, line := range freshstart.SellQueue(PackView(d), keep, refused) {
		id, found := itemIn(d, shop.Pack(), line.Name)
		if !found {
			continue
		}
		before := countOfName(d, freshstart.Coins)
		sold, err := shop.SellCount(id, int32(line.Count), w)
		d.Delay(600 * time.Millisecond)
		gained := countOfName(d, freshstart.Coins) - before
		if err != nil || freshstart.Refused(int(sold), gained) {
			refused[strings.ToLower(line.Name)] = true
			continue
		}
		res.Sold += int(sold)
		res.GP += gained
	}
	if wantShears && countOfName(d, freshstart.Shears) == 0 && countOfName(d, freshstart.Coins) >= 1 {
		if id, found := itemIn(d, shop.Stock(), freshstart.Shears); found {
			if got, err := shop.BuyCount(id, 1, w); err == nil && got > 0 {
				res.BoughtShears = true
			}
		}
	}
	return res
}

func itemIn(d *Deps, items []gamestate.SlotItem, name string) (int32, bool) {
	for _, it := range items {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), name) && it.Count > 0 {
			return it.ID, true
		}
	}
	return 0, false
}

// openShop finds the first of names within 10 tiles of near, walks beside it (the engine's
// clientRoutefinder does not walk to an NPC clicked from further off), and opens its shop.
func openShop(d *Deps, names []string, near tile.Tile) *actions.Shop {
	fd := Fresh(d)
	shop := actions.NewShop(d.Session)
	for _, name := range names {
		npc, ok := nearestNpc(d, name, near, 10)
		if !ok {
			continue
		}
		freshstartrunner.WalkNear(fd, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 1, 20*time.Second)
		npcType, _ := d.Config.Npc(npc.Type)
		op := cachecfg.OpIndex(npcType.Ops, "Trade")
		if op == 0 {
			op = 3
		}
		if err := actions.OpNpc(d.Session, op, npc.Index); err != nil {
			continue
		}
		if waitFor(d, name+"'s shop to open", 5*time.Second, shop.IsOpen) {
			return shop
		}
	}
	return nil
}

func nearestNpc(d *Deps, name string, near tile.Tile, radius int) (gamestate.NPC, bool) {
	var best gamestate.NPC
	bestDist, found := radius+1, false
	for _, n := range d.Session.State().NPCs() {
		t, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(t.Name, name) {
			continue
		}
		if dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}); dist <= radius && dist < bestDist {
			best, bestDist, found = n, dist, true
		}
	}
	return best, found
}

// GrabGroundShears ports grabGroundShears: with no coins for shears, walk to Fred's farm and take
// the free pair that respawns there. It returns true once shears are held.
func GrabGroundShears(d *Deps) bool {
	fd := Fresh(d)
	me, ok := currentTile(d)
	if !ok || me.Level != 0 || tile.Chebyshev(me, freshstart.ShearsSpawn) > 4 {
		d.logf("no coins for shears — walking to the free pair at the farm")
		Walk(d, freshstart.ShearsSpawn, 2, 3*time.Minute)
		return false
	}
	var item gamestate.GroundItem
	found, bestDist := false, 9
	for _, g := range d.Session.State().GroundItems() {
		if !strings.EqualFold(nameOf(d, int32(g.ID)), freshstart.Shears) || g.Level != me.Level {
			continue
		}
		if dist := tile.Chebyshev(me, tile.Tile{X: g.X, Z: g.Z}); dist < bestDist {
			item, bestDist, found = g, dist, true
		}
	}
	if !found {
		d.logf("no shears on the ground at the farm yet — waiting for the respawn")
		d.Delay(3 * time.Second)
		return false
	}
	if bestDist > 0 {
		freshstartrunner.WalkNear(fd, tile.Tile{X: item.X, Z: item.Z, Level: item.Level}, 0, 20*time.Second)
	}
	if err := actions.TakeGroundItem(d.Session, d.Config, item); err != nil {
		d.logf("take shears: %v", err)
		d.Delay(600 * time.Millisecond)
		return false
	}
	if waitFor(d, "the shears", 6*time.Second, func() bool { return countOfName(d, freshstart.Shears) > 0 }) {
		d.logf("picked up the farm shears")
		return true
	}
	return false
}

// ---- the pen ----

// GoToPen gets the account inside the Lumbridge sheep pen: down to the ground floor, to the tile
// outside the east gate, the gate opened (or found open), and one raw step across it — the collision
// pack bakes the gate shut, the same gap crossSeersDoor closes for the Seers wheelhouse.
func GoToPen(d *Deps) bool {
	l := crafterplan.Lumbridge()
	if d.Walker != nil {
		if me, ok := currentTile(d); ok && l.InPen(me) {
			return true
		}
		// webwalk's graph has the pen's gate (1551 at 3213,3261), so it plans straight in.
		if !Walk(d, tile.Tile{X: 3203, Z: 3266}, 3, 5*time.Minute) {
			return false
		}
		me, _ := currentTile(d)
		return l.InPen(me)
	}
	fd := Fresh(d)
	for i := 0; i < 6; i++ {
		me, ok := currentTile(d)
		if !ok {
			return false
		}
		if l.InPen(me) {
			return true
		}
		if me.Level != 0 {
			if !ClimbToGround(d) {
				return false
			}
			continue
		}
		if tile.Chebyshev(me, l.PenOutside) > 0 {
			if !freshstartrunner.WalkNear(fd, l.PenOutside, 0, 3*time.Minute) {
				d.logf("could not reach the pen gate at %d,%d", l.PenOutside.X, l.PenOutside.Z)
				return false
			}
			continue
		}
		live := locindex.ChangesFrom(d.Session.State().LocChanges())
		if gate, closed := d.Locs.Nearest(d.Config, locindex.Query{X: l.PenGate.X, Z: l.PenGate.Z, Level: 0, Radius: 1, Name: "Gate", Op: "Open"}, live); closed {
			d.logf("opening the pen gate at %d,%d", gate.X, gate.Z)
			if err := actions.OpLoc(d.Session, gate.Op, gate.X, gate.Z, gate.ID); err != nil {
				d.logf("OpLoc (pen gate): %v", err)
				return false
			}
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
		}
		if !rawStep(d, l.PenInside) {
			d.logf("could not step through the pen gate")
			d.Delay(2 * time.Second)
			return false
		}
	}
	me, _ := currentTile(d)
	return l.InPen(me)
}

// LeavePen gets the account out of the sheep pen through the east gate: to the tile inside it, the
// gate opened (or found open), and one raw step across. It is a no-op outside the pen. Found live: a
// full pack of wool asked for the castle wheel from inside the pen, the collision pack's shut gate
// made every route unreachable, and the generic door recovery kept trying Fred's farmhouse door.
func LeavePen(d *Deps) bool {
	l := crafterplan.Lumbridge()
	if d.Walker != nil {
		if me, ok := currentTile(d); ok && !l.InPen(me) {
			return true
		}
		return Walk(d, tile.Tile{X: l.PenOutside.X + 2, Z: l.PenOutside.Z}, 1, 2*time.Minute)
	}
	for i := 0; i < 4; i++ {
		me, ok := currentTile(d)
		if !ok {
			return false
		}
		if !l.InPen(me) {
			return true
		}
		if me != l.PenInside {
			if !freshstartrunner.WalkNear(Fresh(d), l.PenInside, 0, time.Minute) {
				d.logf("could not reach the pen gate from inside")
				return false
			}
			continue
		}
		live := locindex.ChangesFrom(d.Session.State().LocChanges())
		if gate, closed := d.Locs.Nearest(d.Config, locindex.Query{X: l.PenGate.X, Z: l.PenGate.Z, Level: 0, Radius: 1, Name: "Gate", Op: "Open"}, live); closed {
			d.logf("opening the pen gate at %d,%d to leave", gate.X, gate.Z)
			if err := actions.OpLoc(d.Session, gate.Op, gate.X, gate.Z, gate.ID); err != nil {
				d.logf("OpLoc (pen gate): %v", err)
				return false
			}
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
		}
		if !rawStep(d, l.PenOutside) {
			d.logf("could not step out through the pen gate")
			d.Delay(2 * time.Second)
			return false
		}
	}
	me, _ := currentTile(d)
	return !l.InPen(me)
}

// ---- shearing ----

// ShearOutcome is what one shear came to.
type ShearOutcome string

// The shear outcomes.
const (
	ShearWool     ShearOutcome = "wool"
	ShearNoWool   ShearOutcome = "no wool"
	ShearNoSheep  ShearOutcome = "no sheep"
	ShearNoShears ShearOutcome = "no shears"
)

func visibleSheep(d *Deps) []crafterplan.Sheep {
	var out []crafterplan.Sheep
	for _, n := range d.Session.State().NPCs() {
		t, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(t.Name, crafterplan.SheepName) {
			continue
		}
		out = append(out, crafterplan.Sheep{Index: n.Index, Type: int(n.Type), Tile: tile.Tile{X: n.X, Z: n.Z, Level: n.Level}})
	}
	return out
}

// Shear ports shear: use the shears on the nearest woolly sheep in range, and wait for the wool. A
// sheep that gave none (it got away, or was out of reach) is skipped for crafterplan.SheepSkip, as is
// one that did.
func Shear(d *Deps, skip map[int]time.Time) ShearOutcome {
	shears, ok := firstSlotNamed(d, []string{freshstart.Shears})
	if !ok {
		return ShearNoShears
	}
	me, ok := currentTile(d)
	if !ok {
		return ShearNoSheep
	}
	now := time.Now()
	sheep := visibleSheep(d)
	i := crafterplan.ChooseSheep(sheep, me, func(index int) bool {
		until, ok := skip[index]
		return ok && now.Before(until)
	})
	if i < 0 {
		return ShearNoSheep
	}
	target := sheep[i]
	if tile.Chebyshev(me, target.Tile) > 1 {
		freshstartrunner.WalkNear(Fresh(d), target.Tile, 1, 15*time.Second)
	}
	before := countOfName(d, freshstart.Wool)
	eq := actions.NewEquipment(d.Session, d.Components)
	if err := eq.UseOnNpc(shears.ID, target.Index); err != nil {
		d.logf("shears on sheep %d: %v", target.Index, err)
		skip[target.Index] = time.Now().Add(crafterplan.SheepSkip)
		d.Delay(600 * time.Millisecond)
		return ShearNoWool
	}
	got := d2wait(d, crafterplan.ShearWait, func() bool { return countOfName(d, freshstart.Wool) > before })
	skip[target.Index] = time.Now().Add(crafterplan.SheepSkip)
	if got {
		return ShearWool
	}
	return ShearNoWool
}

// d2wait is waitFor without the timeout log line: a sheep that gets away is ordinary.
func d2wait(d *Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return cond()
}

// ---- the castle staircase ----

// climbStaircase works the Lumbridge castle staircase one floor in a direction. Each floor's loc
// offers its own ops (1738 Climb-up, 1739 Climb-up and Climb-down, 1740 Climb-down), so the op
// fixes the direction; freshstartrunner's generic level change takes the first op it finds and can
// go up when it meant down.
func climbStaircase(d *Deps, up bool) bool {
	l := crafterplan.Lumbridge()
	me, ok := currentTile(d)
	if !ok {
		return false
	}
	op := crafterplan.ClimbDown
	if up {
		op = crafterplan.ClimbUp
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	m, found := d.Locs.Nearest(d.Config, locindex.Query{X: l.Staircase.X, Z: l.Staircase.Z, Level: me.Level, Radius: 3, Name: "Staircase", Op: op}, live)
	if !found {
		d.logf("no castle staircase offering %q on level %d", op, me.Level)
		d.Delay(2 * time.Second)
		return false
	}
	if tile.Chebyshev(me, tile.Tile{X: m.X, Z: m.Z, Level: me.Level}) > 2 {
		if !freshstartrunner.WalkNear(Fresh(d), tile.Tile{X: m.X, Z: m.Z, Level: me.Level}, 2, time.Minute) {
			return false
		}
	}
	d.logf("OPLOC%d on %q %d at %d,%d (%s)", m.Op, m.Name, m.ID, m.X, m.Z, op)
	if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
		d.logf("OpLoc (staircase): %v", err)
		return false
	}
	return waitFor(d, "the staircase to change floor", 8*time.Second, func() bool {
		t, ok := currentTile(d)
		return ok && t.Level != me.Level
	})
}

// ClimbToGround takes the account down the castle staircase to the ground floor.
func ClimbToGround(d *Deps) bool {
	l := crafterplan.Lumbridge()
	for i := 0; i < 6; i++ {
		me, ok := currentTile(d)
		if !ok {
			return false
		}
		switch crafterplan.RouteDownstairs(me, l.Staircase) {
		case crafterplan.MoveHere:
			return true
		case crafterplan.MoveWalk:
			if !freshstartrunner.WalkNear(Fresh(d), tile.Tile{X: l.Staircase.X, Z: l.Staircase.Z, Level: me.Level}, 2, time.Minute) {
				return false
			}
		case crafterplan.MoveClimbDown:
			if !climbStaircase(d, false) {
				return false
			}
		}
	}
	return false
}

// Walk walks to within radius of dest. With a Walker it plans over webwalk's door, gate and stair
// graph, as the TS walkResilient does; found live that the castle's wheel sits behind the Large
// door, an inner door, the staircase and a level-1 door, which a same-level walker with one-door
// recovery cannot chain. Without one it is freshstartrunner.WalkNear.
func Walk(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	if d.Walker == nil {
		return freshstartrunner.WalkNear(Fresh(d), dest, radius, timeout)
	}
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{Radius: radius, Timeout: timeout})
	if res.Arrived() {
		return true
	}
	d.logf("walk to %d,%d,%d did not arrive (%v: %s), stopped at %v", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, res.End)
	d.Delay(2 * time.Second)
	return false
}

// GoToLumbridgeWheel gets the account beside the castle's spinning wheel: up the staircase to the
// wheel's floor and along to stand (the TS wheel tile, the wheel's one open side).
func GoToLumbridgeWheel(d *Deps, stand tile.Tile) bool {
	if d.Walker != nil {
		// Exactly on the stand: found live that radius 1 stopped at 3208,3214,1, diagonal to the
		// stand and two tiles from the wheel, and the Spin click from there never reached it.
		me, ok := currentTile(d)
		if ok && me == stand {
			return true
		}
		return Walk(d, stand, 0, 5*time.Minute)
	}
	l := crafterplan.Lumbridge()
	fd := Fresh(d)
	for i := 0; i < 6; i++ {
		me, ok := currentTile(d)
		if !ok {
			return false
		}
		switch crafterplan.RouteUpstairs(me, l.Staircase, stand.Level) {
		case crafterplan.MoveClimbDown:
			if !climbStaircase(d, false) {
				return false
			}
		case crafterplan.MoveWalk:
			if !freshstartrunner.WalkNear(fd, tile.Tile{X: l.Staircase.X, Z: l.Staircase.Z, Level: me.Level}, 2, 3*time.Minute) {
				return false
			}
		case crafterplan.MoveClimbUp:
			if !climbStaircase(d, true) {
				return false
			}
		case crafterplan.MoveHere:
			if tile.Chebyshev(me, stand) <= 1 {
				return true
			}
			return freshstartrunner.WalkNear(fd, stand, 1, time.Minute)
		}
	}
	return false
}

// ---- spinning wool ----

// SpinWool ports spinAt for wool: at the castle wheel, Spin opens the make menu and Make-X spins the
// pack's wool; the batch is watched until the wool stops going down for crafterplan.BatchIdleTicks
// or a dialogue interrupts it. When the menu never opens, the wool is used on the wheel instead
// (spinning.rs2's oplocu spins one). It returns how many wool were spun.
func SpinWool(d *Deps, stand tile.Tile) int {
	start := countOfName(d, freshstart.Wool)
	if start == 0 {
		return 0
	}
	if !GoToLumbridgeWheel(d, stand) {
		d.logf("could not reach the spinning wheel — check the wheel tile setting")
		d.Delay(3 * time.Second)
		return 0
	}
	me, _ := currentTile(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	wheel, ok := d.Locs.Nearest(d.Config, locindex.Query{X: me.X, Z: me.Z, Level: me.Level, Radius: crafterplan.WheelRadius, Name: crafterplan.Wheel, Op: crafterplan.SpinOp}, live)
	if !ok {
		d.logf("no spinning wheel within %d tiles of %d,%d,%d", crafterplan.WheelRadius, me.X, me.Z, me.Level)
		d.Delay(3 * time.Second)
		return 0
	}
	// Each wool is used on the wheel: spinning.rs2's [oplocu,_spinning_wheel] spins one per use with
	// no menu, the same path crafterrunner.SpinFlaxUntil proved live on the Seers wheel. The TS opens
	// Spin and makes a batch with Make-X; found live that re-clicking between batches left the pack
	// unspun (27 wool, no progress, no log line), so the menu is not used.
	dlg := actions.NewChatDialog(d.Session, d.Components)
	deadline := time.Now().Add(3 * time.Minute)
	misses := 0
	for time.Now().Before(deadline) && misses < 3 {
		if dlg.CanContinue() {
			break // a level-up; the Loop's dialogue guard answers it and spinning resumes
		}
		wool, found := firstSlotNamed(d, []string{freshstart.Wool})
		if !found {
			break
		}
		before := countOfName(d, freshstart.Wool)
		if err := actions.OpLocU(d.Session, wheel.X, wheel.Z, wheel.ID, wool.ID, wool.Slot, PlayerPack); err != nil {
			d.logf("OpLocU (wool on wheel): %v", err)
			d.Delay(time.Second)
			return start - countOfName(d, freshstart.Wool)
		}
		if d2wait(d, spinAnswerWait, func() bool { return countOfName(d, freshstart.Wool) < before || dlg.CanContinue() }) &&
			countOfName(d, freshstart.Wool) < before {
			misses = 0
			// The server ignores the next use until the spin's p_delay is over (spinRedelay).
			if !d.Delay(spinRedelay) {
				break
			}
			continue
		}
		misses++
	}
	spun := start - countOfName(d, freshstart.Wool)
	if spun == 0 {
		d.logf("the wheel at %d,%d,%d spun no wool (3 uses with no ball)", wheel.X, wheel.Z, wheel.Level)
	}
	return spun
}
