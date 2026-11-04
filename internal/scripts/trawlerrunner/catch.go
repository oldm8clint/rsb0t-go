package trawlerrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/trawlerplan"
)

// The catch's I/O: the anchor's claim at the net, the crew's pickup of the dumped fish, the bank visit that takes them away,
// and the loot each reports (internal/loot/trawler.go).

// CatchHeld is the raw trawler fish in the pack.
func CatchHeld(d *Deps) int {
	n := 0
	for _, it := range pack(d) {
		if trawlerplan.IsTrawlerFish(int(it.ID)) {
			n += int(it.Count)
		}
	}
	return n
}

// GroundFish is the raw trawler fish visible on the floor by the net, on the account's own level (the one it can take from).
func GroundFish(d *Deps) []gamestate.GroundItem {
	var out []gamestate.GroundItem
	level := Here(d).Level
	for _, g := range d.Session.State().GroundItems() {
		if g.Level == level && trawlerplan.IsTrawlerFish(g.ID) && trawlerplan.NearNet(tile.Tile{X: g.X, Z: g.Z, Level: g.Level}) {
			out = append(out, g)
		}
	}
	return out
}

// PickUpFish takes the nearest dumped fish on the account's level (walking the last tiles) and records what arrived on the
// Fishing Trawler row, with no event (the claim counted it). It reports whether something arrived.
func PickUpFish(d *Deps) bool {
	here := Here(d)
	var best gamestate.GroundItem
	bestD, found := 1<<30, false
	for _, g := range GroundFish(d) {
		gt := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		// The dearest first, the nearest of those (trawlerplan.FishRank): more fish lie by the net than the packs take.
		dist := tile.Chebyshev(here, gt)
		if !found || trawlerplan.FishRank(g.ID) < trawlerplan.FishRank(best.ID) || (trawlerplan.FishRank(g.ID) == trawlerplan.FishRank(best.ID) && dist < bestD) {
			best, bestD, found = g, dist, true
		}
	}
	if !found {
		return false
	}
	if bestD > 1 && !WalkTo(d, tile.Tile{X: best.X, Z: best.Z, Level: best.Level}, 1) {
		return false
	}
	before := fleetrunner.LootBefore(d)
	held := CatchHeld(d)
	if err := actions.TakeGroundItem(d.Session, d.Config, best); err != nil {
		logf(d, "trawler: taking a fish at %d,%d: %v", best.X, best.Z, err)
		return false
	}
	if !waitUntil(d, 4*time.Second, func() bool { return CatchHeld(d) > held }) {
		return false
	}
	fleetrunner.RecordTake(d, loot.FishingTrawler(), before, 0, 1500*time.Millisecond)
	return true
}

// BankCatch walks to the nearest bank and deposits the trawler fish, leaving the bank open for the caller (the alchemy kit's
// draw); nil when the bank did not open.
func BankCatch(d *Deps) (*actions.Bank, error) {
	at := trawlerplan.NearestBank(Here(d))
	bank, ok := fleetrunner.OpenBankDepositing(d, at, func(it gamestate.SlotItem) bool { return trawlerplan.IsTrawlerFish(int(it.ID)) })
	if !ok || bank == nil {
		return nil, fmt.Errorf("the bank at %d,%d did not open (at %s)", at.X, at.Z, fleetrunner.HereIs(d))
	}
	return bank, nil
}

// DropHeldFish drops the trawler fish the pack holds, dearest first (trawlerplan.HeldDrops), where the account stands, and
// reports how many went.
func DropHeldFish(d *Deps) int {
	var slots []gamestate.SlotItem
	for _, it := range trawlerplan.HeldDrops(packCatch(d)) {
		slots = append(slots, gamestate.SlotItem{Slot: it.Slot, Item: gamestate.Item{ID: int32(it.ID), Count: int32(it.Count)}})
	}
	if len(slots) == 0 {
		return 0
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	r := eq.DropMany(d.Config, slots, thieverrunner.Waiter(fleetrunner.Thiev(d)))
	if r.Err != nil || r.Left > 0 {
		logf(d, "trawler: dropping the held catch: %s", r.Log())
	}
	return r.Dropped()
}

// packCatch is the pack's stacks as catch items, priced.
func packCatch(d *Deps) []trawlerplan.CatchItem {
	var items []trawlerplan.CatchItem
	for _, it := range pack(d) {
		items = append(items, trawlerplan.CatchItem{Slot: it.Slot, ID: int(it.ID), Count: int(it.Count), Cost: fleetrunner.ObjCost(d, it.ID)})
	}
	return items
}

// ---- the claim ----

// Claim inspects the net, takes the best of the reward window into the pack and drops the rest at the account's feet, the junk
// first and the best fish last for the crew (trawlerplan.PlanClaim). It returns what moved into the pack and how many items
// the window held, and records the moved items on the Fishing Trawler row.
func Claim(d *Deps) (taken loot.Counts, rolls int, err error) {
	// The landed net (trawlernetland, 2x2 at 2666,3167) sits on a level-1 platform with the bridge flag, west of the lobby deck
	// behind a wall (map m41_49): it is reached from the pier at ground level, never over the gangplank (live 3 Nov 2026: from
	// the lobby deck, "could not reach the net"). Matched on any level.
	net := func() (entityquery.Entity, bool) {
		for _, c := range locsOf(d, trawlerplan.LocNetLand) {
			if c.X == trawlerplan.NetLand.X && c.Z == trawlerplan.NetLand.Z {
				return c, true
			}
		}
		return entityquery.Entity{}, false
	}
	LeaveDeck(d) // a claim that starts on the lobby deck
	// The last claim's fish still held (the bank skipped for the next watch): the dearest dropped in the next zone first, so
	// this claim has the room and they lie apart from its spill (trawlerplan.HeldDropStand).
	if len(trawlerplan.HeldDrops(packCatch(d))) > 0 {
		if WalkTo(d, trawlerplan.HeldDropStand, 0) {
			if n := DropHeldFish(d); n > 0 {
				logf(d, "trawler: dropped %d held fish at %d,%d for the crew before the claim", n, trawlerplan.HeldDropStand.X, trawlerplan.HeldDropStand.Z)
			}
		} else {
			logf(d, "trawler: could not reach %d,%d to drop the held fish: claiming with them", trawlerplan.HeldDropStand.X, trawlerplan.HeldDropStand.Z)
		}
	}
	// Walk unless already where the walk would end: from 2666,3165, two tiles off, the inspect never opened the window (live
	// 3 Nov 19:29 and 4 Nov 01:02, two won games of 2,000+ fish lost; the other 19 landings walked first and all claimed).
	stand := tile.Tile{X: trawlerplan.NetLand.X - 1, Z: trawlerplan.NetLand.Z, Level: 0}
	if tile.Chebyshev(Here(d), stand) > 1 && !WalkTo(d, stand, 1) {
		return nil, 0, fmt.Errorf("could not reach the net at %d,%d from %s", trawlerplan.NetLand.X, trawlerplan.NetLand.Z, fleetrunner.HereIs(d))
	}
	e, ok := net()
	if !ok {
		return nil, 0, fmt.Errorf("no net (loc %d) at %d,%d", trawlerplan.LocNetLand, trawlerplan.NetLand.X, trawlerplan.NetLand.Z)
	}
	before := loot.PackCounts(d.Session.State().Inventory(fleetrunner.PlayerPack))
	if err := entityquery.Interact(d.Session, e, "Inspect"); err != nil {
		return nil, 0, err
	}
	window := func() []gamestate.SlotItem {
		inv := d.Session.State().Inventory(trawlerplan.CatchInvComponent)
		if inv == nil {
			return nil
		}
		return inv.NonEmpty()
	}
	// The walk to the net and the roll loop: give it a while. The window must be OPEN: the last claim's items may still be
	// in the session's copy of the inv after it closed.
	open := func() bool {
		main, _, _ := d.Session.State().Modals()
		return main == trawlerplan.CatchInterface
	}
	// A dialog the game's end left open swallows the inspect (live 3 Nov 2026, game 18: a won game, 2,097 fish, and no window):
	// continue it and inspect again, twice.
	shown := waitUntil(d, 8*time.Second, func() bool { return open() && len(window()) > 0 })
	for try := 0; !shown && try < 2; try++ {
		dlg := actions.NewChatDialog(d.Session, d.Components)
		for i := 0; i < 4 && dlg.CanContinue(); i++ {
			_ = dlg.Continue()
			d.Delay(600 * time.Millisecond)
		}
		_ = actions.CloseModal(d.Session)
		if err := entityquery.Interact(d.Session, e, "Inspect"); err != nil {
			return nil, 0, err
		}
		shown = waitUntil(d, 8*time.Second, func() bool { return open() && len(window()) > 0 })
	}
	if !shown {
		return nil, 0, fmt.Errorf("the catch window never showed (no catch for this account, or Fishing under 15)")
	}
	d.Delay(600 * time.Millisecond) // the whole roll loop's inv update, read in one piece
	var items []trawlerplan.CatchItem
	for _, it := range window() {
		rolls += int(it.Count)
		items = append(items, trawlerplan.CatchItem{Slot: it.Slot, ID: int(it.ID), Count: int(it.Count), Cost: fleetrunner.ObjCost(d, it.ID)})
	}
	for _, mv := range trawlerplan.PlanClaim(items, fleetrunner.FreeSlots(d)) {
		for i := 0; i < mv.Times; i++ {
			if err := actions.InvButton(d.Session, mv.Option, int32(mv.ID), mv.Slot, trawlerplan.CatchInvComponent); err != nil {
				logf(d, "trawler: claim: moving obj %d from slot %d: %v", mv.ID, mv.Slot, err)
				break
			}
			if !d.Delay(150 * time.Millisecond) {
				break
			}
		}
		// A click that only drops needs no pause after it: the server takes five clicks a tick, in the order sent.
		if mv.Moves > 0 && !d.Delay(600*time.Millisecond) {
			break
		}
	}
	d.Delay(1200 * time.Millisecond)
	taken = loot.Gained(before, loot.PackCounts(d.Session.State().Inventory(fleetrunner.PlayerPack)), nil)
	if main, _, _ := d.Session.State().Modals(); main == trawlerplan.CatchInterface {
		if err := actions.CloseModal(d.Session); err != nil {
			logf(d, "trawler: claim: closing the catch window: %v", err)
		}
	}
	if d.Loot != nil {
		if drop, ok := loot.TrawlerClaim(d.Config, taken); ok {
			d.Loot(drop)
		}
	}
	return taken, rolls, nil
}

// LocGangplankOff is the docked boat's way back to the pier (trawler_gangplankoff, op "Cross").
const LocGangplankOff = 2179

// LeaveDeck crosses back to the pier from the docked boat's deck, so the countdown a claim's crossing started does not take
// the account to sea alone.
func LeaveDeck(d *Deps) {
	if Here(d).Level != trawlerplan.NetLand.Level && !trawlerplan.OnDeck(Here(d)) {
		return
	}
	// Below the deck (trawler_lobby_center_under, live 5 Nov 2026: 2671,3177,0): the Ship's ladder first, then the gangplank.
	if Here(d).Level == 0 {
		if l, ok := entityquery.Locs(d.Session.State(), d.Config, d.Locs, locRadius).Name("Ship's ladder").Nearest(); ok {
			if err := entityquery.Interact(d.Session, l, "Climb-up"); err != nil {
				logf(d, "trawler: below the deck: %v", err)
				return
			}
			if !waitUntil(d, 8*time.Second, func() bool { return Here(d).Level == trawlerplan.NetLand.Level }) {
				logf(d, "trawler: climbed the ladder at %d,%d but still below the deck at %s", l.X, l.Z, fleetrunner.HereIs(d))
				return
			}
		}
	}
	es := locsOf(d, LocGangplankOff)
	if len(es) == 0 {
		logf(d, "trawler: no gangplank off the deck in view at %s", fleetrunner.HereIs(d))
		return
	}
	e := es[0]
	// As Cross does for the way on (live 2 Nov 2026: clicks from beside the plank were answered "I can't reach that!"):
	// step onto the plank's own tile first, so the server's approach is none. 5 Nov 2026: 39 crew clicked it from
	// 2673,3172,1 and stayed on the deck, with no answer read.
	if plank := (tile.Tile{X: e.X, Z: e.Z, Level: Here(d).Level}); Here(d) != plank {
		fleetrunner.StepTo(d, plank)
	}
	at := time.Now()
	if err := entityquery.Interact(d.Session, e, "Cross"); err != nil {
		logf(d, "trawler: off the deck: %v", err)
		return
	}
	if waitUntil(d, 8*time.Second, func() bool { h := Here(d); return h.Level != trawlerplan.NetLand.Level && !trawlerplan.OnDeck(h) }) {
		logf(d, "trawler: off the deck by the gangplank at %d,%d: now at %s", e.X, e.Z, fleetrunner.HereIs(d))
		return
	}
	logf(d, "trawler: crossed the gangplank at %d,%d (loc %d) but still on the deck at %s (game said %q; movement held %v)",
		e.X, e.Z, e.ID, fleetrunner.HereIs(d), strings.TrimSpace(fleetrunner.ChatSince(d, at)), actions.MovementHeld(d.Session))
}

// reportLeak records n Swamp paste spent on leaks; reportMend n nets mended with this account's rope.
func reportLeak(d *Deps, n int) {
	if d.Loot == nil {
		return
	}
	if drop, ok := loot.TrawlerLeak(d.Config, n); ok {
		d.Loot(drop)
	}
}

func reportMend(d *Deps, n int) {
	if d.Loot == nil {
		return
	}
	if drop, ok := loot.TrawlerMended(d.Config, n); ok {
		d.Loot(drop)
	}
}

// ---- the alchemy kit ----

// DrawAlchKit draws the alchemy kit in an open bank -- the natures and noted alchables the walk alch takes (Travel.DrawAlchKit),
// and a fire staff when none is worn or carried -- closes the bank and wields the staff. It reports whether a fire staff is
// worn afterwards and the Nature runes carried.
func DrawAlchKit(d *Deps, bank *actions.Bank, t *fleetrunner.Travel) (staff bool, natures int) {
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if bank != nil {
		t.DrawAlchKit(d, bank, w)
		if d.Config != nil && !fleetrunner.FireStaffWorn(d) && !fleetrunner.FireStaffInPack(d) && fleetrunner.FreeSlots(d) > 0 {
			for _, it := range bank.Items() {
				if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && alchplan.IsFireStaff(o.Name) {
					if err := bank.WithdrawOne(it.ID); err != nil {
						logf(d, "trawler: withdrawing the %s: %v", o.Name, err)
					} else {
						waitUntil(d, 3*time.Second, func() bool { return fleetrunner.FireStaffInPack(d) })
					}
					break
				}
			}
		}
		_ = bank.Close()
		d.Delay(600 * time.Millisecond)
	}
	staff = fleetrunner.WieldFireStaffFromPack(d)
	return staff, fleetrunner.CountPackNamed(d, alchplan.NatureRune)
}

// DrawRope draws up to want Rope from an open bank, as many as the pack has room for beyond keepFree slots (a single Rope takes a
// slot), and reports how many arrived. Only the mender's kit asks (trawlerplan.KitRopeFor); a bank without Rope draws none.
func DrawRope(d *Deps, bank *actions.Bank, want, keepFree int) int {
	if bank == nil || want <= 0 {
		return 0
	}
	want = min(want, fleetrunner.FreeSlots(d)-keepFree)
	if want <= 0 {
		return 0
	}
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	for _, it := range bank.Items() {
		if int(it.ID) != trawlerplan.RopeID || it.Count <= 0 {
			continue
		}
		before := Held(d).Rope
		n := min(want, int(it.Count))
		if err := bank.WithdrawX(it.ID, int32(n), w); err != nil {
			logf(d, "trawler: drawing %d Rope from the bank: %v", n, err)
		}
		return Held(d).Rope - before
	}
	return 0
}
