// Package toadcollectrunner is the I/O half of the ToadCollector: it reads the world, walks, takes the Swamp toad
// ground spawns, pulls the legs off and banks at an ordinary bank (VisitBank), driven by the pure decisions in
// internal/scripts/toadcollectplan. The mule hand-off itself is internal/scripts/donorrunner.Run, called by
// internal/bot/toadcollector.go.
//
// Every blocking wait goes through a caller-supplied Delayer so a stop request cancels it.
package toadcollectrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/toadcollectplan"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id (see flaxrunner.PlayerPack).
const PlayerPack = 3214

// removeLegs is the Swamp toad's first inventory option label (content gnome_cooking.obj iop1).
const removeLegs = "Remove-legs"

// Delayer sleeps for d, returning false early if the caller should stop.
type Delayer func(d time.Duration) bool

// Deps is everything the collector needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Walker plans walks over webwalk's door graph; nil falls back to plain pathfinding with door recovery.
	Walker *webwalk.Walker
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log, Walker: d.Walker,
	}
}

// Here is the account's tile.
func Here(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// WaitForGuardian pauses while a random event is present; false means stop.
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, pathfind.NavPoint{}, 0, d.Log)
	for randomevents.Busy(d.Session) {
		d.logf("[guardian] a random event is present; waiting")
		if !d.Delay(2 * time.Second) {
			return false
		}
	}
	return true
}

const walkTimeout = 120 * time.Second

// WalkNear walks to within radius of dest (door-aware recovery included, via freshstartrunner).
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	if d.Pack == nil {
		return false
	}
	if d.Walker != nil && freshstartrunner.WalkWithWalker(fresh(d), dest, radius, walkTimeout) {
		return true
	}
	return freshstartrunner.WalkNear(fresh(d), dest, radius, walkTimeout)
}

// ---- live reads ----

// PackSlotRows reads the pack one row per slot.
func PackSlotRows(d *Deps) []donorplan.ItemRow {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []donorplan.ItemRow
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: obj.Name, ID: int(it.ID), Count: c})
	}
	return out
}

// Held is the Toad's legs and whole Swamp toads in the pack.
func Held(d *Deps) (legs, toads int) { return toadcollectplan.Count(PackSlotRows(d)) }

// FreeSlots is the empty pack slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return toadcollectplan.PackSlots
	}
	free := toadcollectplan.PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// groundAt returns the Swamp toad lying on spot, if any.
func groundAt(d *Deps, spot toadcollectplan.Spot) (gamestate.GroundItem, bool) {
	for _, it := range d.Session.State().GroundItems() {
		if it.Level != spot.Tile.Level || it.X != spot.Tile.X || it.Z != spot.Tile.Z {
			continue
		}
		if obj, ok := d.Config.Obj(it.ID); ok && toadcollectplan.IsToad(obj.Name) {
			return it, true
		}
	}
	return gamestate.GroundItem{}, false
}

// Visible lists the spawns with a Swamp toad on the ground now.
func Visible(d *Deps) []toadcollectplan.Spot {
	var out []toadcollectplan.Spot
	for _, sp := range toadcollectplan.Spawns {
		if _, ok := groundAt(d, sp); ok {
			out = append(out, sp)
		}
	}
	return out
}

// Kit reports whether the pack holds anything that is neither a toad nor legs (a previous script's kit).
func Kit(d *Deps) bool {
	for _, r := range PackSlotRows(d) {
		if !toadcollectplan.IsToad(r.Name) && !toadcollectplan.IsLegs(r.Name) {
			return true
		}
	}
	return false
}

// ---- picking ----

// PickResult is one Collect attempt.
type PickResult struct {
	// Picked is true when the toad landed in the pack; Gone when it left the ground without landing.
	Picked, Gone bool
}

const (
	pickWait  = 6 * time.Second
	pickGrace = 1200 * time.Millisecond
)

// Collect walks to spot if needed, takes the Swamp toad lying there and reports whether it landed.
func Collect(d *Deps, spot toadcollectplan.Spot) PickResult {
	item, ok := groundAt(d, spot)
	if !ok {
		return PickResult{Gone: true}
	}
	if tile.Chebyshev(Here(d), spot.Tile) > toadcollectplan.PickRadius {
		if !WalkNear(d, spot.Tile, toadcollectplan.PickRadius) {
			return PickResult{}
		}
	}
	if item, ok = groundAt(d, spot); !ok {
		return PickResult{Gone: true}
	}
	_, before := Held(d)
	if err := actions.TakeGroundItem(d.Session, d.Config, item); err != nil {
		d.logf("toads: take swamp toad: %v", err)
		return PickResult{}
	}
	deadline := time.Now().Add(pickWait)
	var goneAt time.Time
	for time.Now().Before(deadline) {
		if !d.Delay(200 * time.Millisecond) {
			return PickResult{}
		}
		if _, now := Held(d); now > before {
			return PickResult{Picked: true}
		}
		if _, still := groundAt(d, spot); still {
			goneAt = time.Time{}
			continue
		}
		if goneAt.IsZero() {
			goneAt = time.Now()
		} else if time.Since(goneAt) >= pickGrace {
			return PickResult{Gone: true}
		}
	}
	return PickResult{}
}

// Skin pulls the legs off every whole Swamp toad in the pack ("Remove-legs", which converts at once) and returns how
// many it converted.
func Skin(d *Deps) int {
	eq := actions.NewEquipment(d.Session, d.Components)
	done := 0
	for guard := 0; guard < toadcollectplan.PackSlots; guard++ {
		var id int32 = -1
		for _, r := range PackSlotRows(d) {
			if toadcollectplan.IsToad(r.Name) {
				id = int32(r.ID)
				break
			}
		}
		if id < 0 {
			break
		}
		_, before := Held(d)
		if err := eq.InteractHeld(d.Config, id, removeLegs); err != nil {
			d.logf("toads: remove legs: %v", err)
			break
		}
		changed := false
		for i := 0; i < 10 && !changed; i++ {
			if !d.Delay(200 * time.Millisecond) {
				return done
			}
			_, now := Held(d)
			changed = now < before
		}
		if !changed {
			d.logf("toads: remove legs changed nothing")
			break
		}
		done++
	}
	return done
}

// ---- the bank ----

// BankVisit is what VisitBank left in the bank.
type BankVisit struct {
	BankLegs int
}

// VisitBank opens the ordinary bank at at (fleetrunner.OpenBank: the webwalk walk, then the booth), deposits everything
// in the pack that is not Toad's legs, and then either draws every Toad's legs the bank holds into the free slots (draw)
// or deposits the pack's own legs too. The bank is shared by every booth, so legs banked at one bank are drawn at
// another.
func VisitBank(d *Deps, at tile.Tile, draw bool) (BankVisit, error) {
	fd := &fleetrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: fleetrunner.Delayer(d.Delay), Walker: d.Walker, Log: d.Log,
	}
	bank, ok := fleetrunner.OpenBank(fd, at)
	if !ok {
		return BankVisit{}, fmt.Errorf("could not open the bank at %d,%d", at.X, at.Z)
	}
	defer func() { _ = bank.Close() }()
	w := actions.DelayWaiter(d.Delay)
	legs := func(id int32) bool {
		obj, ok := d.Config.Obj(int(id))
		return ok && toadcollectplan.IsLegs(obj.Name)
	}
	if err := bank.DepositAllMatching(func(it gamestate.SlotItem) bool { return !draw || !legs(it.ID) }, w); err != nil {
		return BankVisit{}, fmt.Errorf("deposit: %w", err)
	}
	if draw {
		if err := bank.SetNoteMode(false, w); err != nil {
			return BankVisit{}, fmt.Errorf("note mode off: %w", err)
		}
		for _, it := range bank.Items() {
			if it.Count <= 0 || !legs(it.ID) || FreeSlots(d) == 0 {
				continue
			}
			if err := bank.WithdrawX(it.ID, min(it.Count, int32(FreeSlots(d))), w); err != nil {
				d.logf("toads: drawing banked %d: %v", it.ID, err)
			}
		}
	}
	var out BankVisit
	for _, it := range bank.Items() {
		if it.Count > 0 && legs(it.ID) {
			out.BankLegs += int(it.Count)
		}
	}
	return out, nil
}
