// Package antipoisoncollectrunner is the I/O half of the AntipoisonCollector: it reads the world, walks, takes the
// ground spawns and banks at an ordinary bank (VisitBank), driven by the pure decisions in
// internal/scripts/antipoisoncollectplan. The mule hand-off itself is internal/scripts/donorrunner.Run, called by
// internal/bot/antipoison.go.
//
// Every blocking wait goes through a caller-supplied Delayer so a stop request cancels it.
package antipoisoncollectrunner

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
	"rsb0t-go/internal/scripts/antipoisoncollectplan"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id (see flaxrunner.PlayerPack).
const PlayerPack = 3214

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

// Held is the antipoison potions and doses in the pack.
func Held(d *Deps) (pots, doses int) { return antipoisoncollectplan.Doses(PackSlotRows(d)) }

// FreeSlots is the empty pack slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return antipoisoncollectplan.PackSlots
	}
	free := antipoisoncollectplan.PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// groundAt returns the antipoison stack lying on spot, if any.
func groundAt(d *Deps, spot antipoisoncollectplan.Spot) (gamestate.GroundItem, bool) {
	for _, it := range d.Session.State().GroundItems() {
		if it.Level != spot.Tile.Level || it.X != spot.Tile.X || it.Z != spot.Tile.Z {
			continue
		}
		if obj, ok := d.Config.Obj(it.ID); ok && fleetplan.AntipoisonDoses(obj.Name) > 0 {
			return it, true
		}
	}
	return gamestate.GroundItem{}, false
}

// Visible lists the spawns with an antipoison on the ground now.
func Visible(d *Deps) []antipoisoncollectplan.Spot {
	var out []antipoisoncollectplan.Spot
	for _, sp := range antipoisoncollectplan.Spawns {
		if _, ok := groundAt(d, sp); ok {
			out = append(out, sp)
		}
	}
	return out
}

// ---- picking ----

// PickResult is one Collect attempt.
type PickResult struct {
	// Picked is true when the potion landed in the pack; Gone when it left the ground without landing
	// (somebody else took it, or it despawned).
	Picked, Gone bool
	Name         string
}

const (
	pickWait  = 6 * time.Second
	pickGrace = 1200 * time.Millisecond
)

// Collect walks to spot if needed, takes the antipoison lying there and reports whether it landed.
func Collect(d *Deps, spot antipoisoncollectplan.Spot) PickResult {
	item, ok := groundAt(d, spot)
	if !ok {
		return PickResult{Gone: true}
	}
	obj, _ := d.Config.Obj(item.ID)
	name := obj.Name
	if tile.Chebyshev(Here(d), spot.Tile) > antipoisoncollectplan.PickRadius {
		if !WalkNear(d, spot.Tile, antipoisoncollectplan.PickRadius) {
			return PickResult{Name: name}
		}
	}
	if item, ok = groundAt(d, spot); !ok {
		return PickResult{Gone: true, Name: name}
	}
	before, _ := Held(d)
	if err := actions.TakeGroundItem(d.Session, d.Config, item); err != nil {
		d.logf("antipoison: take %s: %v", name, err)
		return PickResult{Name: name}
	}
	deadline := time.Now().Add(pickWait)
	var goneAt time.Time
	for time.Now().Before(deadline) {
		if !d.Delay(200 * time.Millisecond) {
			return PickResult{Name: name}
		}
		if now, _ := Held(d); now > before {
			return PickResult{Picked: true, Name: name}
		}
		if _, still := groundAt(d, spot); still {
			goneAt = time.Time{}
			continue
		}
		// Off the ground: give the pack a moment to catch up before believing somebody else took it.
		if goneAt.IsZero() {
			goneAt = time.Now()
		} else if time.Since(goneAt) >= pickGrace {
			return PickResult{Gone: true, Name: name}
		}
	}
	return PickResult{Name: name}
}

// ---- decanting ----

// vialEmptyID is an empty Vial (content obj.pack 229), what a pour that empties one potion leaves behind.
const vialEmptyID = 229

// Decant pours the pack's partial antipoisons together (antipoisoncollectplan.DecantPair) until at most one partial is
// left, then drops the empty vials, so the pack holds Antipoison(4)s: 28 slots carry 112 doses instead of 28. It
// reports how many pours landed.
func Decant(d *Deps) int {
	eq := actions.NewEquipment(d.Session, d.Components)
	poured := 0
	for guard := 0; guard < antipoisoncollectplan.PackSlots; guard++ {
		rows := PackSlotRows(d)
		from, onto, ok := antipoisoncollectplan.DecantPair(rows)
		if !ok {
			break
		}
		before := fmt.Sprint(rows)
		if err := eq.UseOnHeld(int32(from.ID), int32(onto.ID)); err != nil {
			d.logf("antipoison: decanting %s into %s: %v", from.Name, onto.Name, err)
			break
		}
		changed := false
		for i := 0; i < 10 && !changed; i++ {
			if !d.Delay(300 * time.Millisecond) {
				return poured
			}
			changed = fmt.Sprint(PackSlotRows(d)) != before
		}
		if !changed {
			d.logf("antipoison: decanting %s into %s changed nothing", from.Name, onto.Name)
			break
		}
		poured++
	}
	var vials []gamestate.SlotItem
	if inv := d.Session.State().Inventory(PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if it.ID == vialEmptyID {
				vials = append(vials, it)
			}
		}
	}
	if len(vials) > 0 {
		eq.DropMany(d.Config, vials, actions.DelayWaiter(d.Delay))
	}
	return poured
}

// ---- the bank ----

// Kit reports whether the pack holds anything that is not an antipoison (a previous script's kit), which would fill
// the pack before the hand-off does.
func Kit(d *Deps) bool {
	for _, r := range PackSlotRows(d) {
		if !antipoisoncollectplan.IsAntipoison(r.Name) {
			return true
		}
	}
	return false
}

// BankVisit is what VisitBank left in the bank.
type BankVisit struct {
	BankPots, BankDoses int
}

// VisitBank opens the ordinary bank at at (fleetrunner.OpenBank: the webwalk walk, then the booth), deposits everything
// in the pack that is not an antipoison, and then either draws every antipoison the bank holds into the free slots
// (draw) or deposits the pack's own antipoison too. The bank is shared by every booth, so doses banked at one bank are
// drawn at another. It replaces the Castle Wars chest, whose building the walker could not leave on 1 Nov.
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
	potion := func(id int32) bool {
		obj, ok := d.Config.Obj(int(id))
		return ok && fleetplan.AntipoisonDoses(obj.Name) > 0
	}
	if err := bank.DepositAllMatching(func(it gamestate.SlotItem) bool { return !draw || !potion(it.ID) }, w); err != nil {
		return BankVisit{}, fmt.Errorf("deposit: %w", err)
	}
	if draw {
		if err := bank.SetNoteMode(false, w); err != nil {
			return BankVisit{}, fmt.Errorf("note mode off: %w", err)
		}
		for _, it := range bank.Items() {
			if it.Count <= 0 || !potion(it.ID) || FreeSlots(d) == 0 {
				continue
			}
			if err := bank.WithdrawX(it.ID, min(it.Count, int32(FreeSlots(d))), w); err != nil {
				d.logf("antipoison: drawing banked %d: %v", it.ID, err)
			}
		}
	}
	var out BankVisit
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && it.Count > 0 {
			if n := fleetplan.AntipoisonDoses(obj.Name); n > 0 {
				out.BankPots += int(it.Count)
				out.BankDoses += int(it.Count) * n
			}
		}
	}
	return out, nil
}
