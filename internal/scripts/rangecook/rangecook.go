// Package rangecook is the range half of cooking raw food on a range, shared by the mule's idle
// cooking (internal/scripts/mulerunner/cook.go, CookRound) and ScarabSwarm's self-cooked food
// (the ScarabSwarm runner package's selfcook.go). It was mulerunner's own code, moved here unchanged
// in behaviour on 3 October 2026 so a second caller could use it: crossing the one real door
// between Catherby's bank and its range, finding a range by NAME through the loc index, standing
// where the engine accepts a use on it (internal/locreach), and using raw food on it until none is
// left. The live evidence behind each piece is in mulerunner/cook.go's doc comment and in the
// doc comments below; none of it is repeated here.
//
// What it does not do: open or use a bank, decide what to cook, or yield to trades. Those stay
// with each caller; Deps.Stop is the one hook a caller's yield needs.
package rangecook

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// RangeLocName is the loc name every usable range shares in content 289 (cooking_sources.loc:
// "[range]" and loc_2729..2731, all name=Range, category cooking_oven). Lumbridge castle's
// cook-o-matic is named "Cooking range" and answers only after Cook's Assistant
// (quest_cook.rs2's [oplocu,cooksquestrange]), so a by-name query for "Range" never finds it.
const RangeLocName = "Range"

// PlayerPack is the fixed backpack component every runner reads (mulerunner.PlayerPack's doc
// comment has the live reason the dynamic Equipment view is not used during a cook round).
const PlayerPack = 3214

// Deps is what the range half needs. Delay and WalkNear are called at use time, so a caller may
// swap its own delay underneath (mulerunner.CookRound's yieldable delay does).
type Deps struct {
	Session actions.Session
	Locs    *locindex.Index
	Config  *cachecfg.Config
	// Delay sleeps and reports false when the caller wants to stop.
	Delay func(time.Duration) bool
	// WalkNear walks door-aware toward dest (freshstartrunner.WalkNear).
	WalkNear func(dest tile.Tile, radius int, timeout time.Duration) bool
	// Log receives one formatted line per notable event; nil drops them.
	Log func(string)
	// Loot receives each CookOut as one Cooking batch (the raw food used, the cooked and burnt food
	// that came of it); nil records nothing. Set from bot.Context.Loot.
	Loot func(loot.Drop)
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// waitFor polls cond every 100 ms through d.Delay until it holds or timeout passes; false at once
// (no timeout line) when the delay reports a stop. mulerunner's own waitFor, unchanged.
func waitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	d.logf("timed out waiting for %s", what)
	return false
}

// Here is the account's tile, nil before the first position update.
func Here(s actions.Session) *tile.Tile {
	x, z, level, ok := s.State().Tile()
	if !ok {
		return nil
	}
	return &tile.Tile{X: x, Z: z, Level: level}
}

// ---- the one real closed door between Catherby's bank and its range ----
//
// Door loc 1530 at 2816,3438,0 accepts "Open" only from its OWN tile (from the south neighbour it
// answers "I can't reach that!"), so freshstartrunner's generic door recovery, which tries a
// fixture's four neighbours, can never open it. Found live 2026-09-20 with cmd/doorprobe and
// cmd/cooktest; mulerunner/cook.go's crossCatherbyDoor doc comment has the full transcript.
var (
	CatherbyDoorTile   = tile.Tile{X: 2816, Z: 3438, Level: 0}
	CatherbyDoorInside = tile.Tile{X: 2816, Z: 3439, Level: 0}
)

const catherbyDoorName = "Door"

// WhyStopped is the reason CrossCatherbyDoor gives when the caller's Delay refused: the bot is stopping (a stop
// request, a dropped connection), so every wait and every walk answers false at once. It is NOT a door that did not
// take the account across, and a caller must not retry it, count it as an abandoned load or log it as one.
//
// Live 14 Oct 2026: of 16 "the Catherby range's door did not take me across (could not reach 2816,3438,0 / 2816,3439,0
// beside the Catherby range's own door)" lines, 15 carried the second the fleet was stopped (04:45:07, eight accounts)
// and the other the second a connection dropped (04:17:13); the 79 loads in between crossed it without one failure.
const WhyStopped = "stopped"

// stopped reports whether the caller wants to stop: a zero-length Delay is the stop test every caller's delay
// already answers (waitFor and invFeed.waitFor say the same).
func (d *Deps) stopped() bool {
	return d.Delay != nil && !d.Delay(0)
}

// CrossCatherbyDoor gets the account across the door's one blocked edge in whichever direction its
// position implies: an ordinary WalkNear onto the near tile, open the door when a query still
// finds its "Open" option, then one raw single-waypoint step across (the static collision pack
// bakes every door shut, so FindPath would refuse the step). It returns why it failed rather than
// logging it, because only the caller knows whether a failure was a yield. A failure while the caller's
// Delay refuses (a stop, a yield, a dropped connection) is WhyStopped, not a door: on 20 Sep and again on 14
// Oct 2026 every "could not reach 2816,3438,0 / 2816,3439,0 beside the Catherby range's own door" line landed
// in the second of a yield or a stop, and the door was fine each time.
func CrossCatherbyDoor(d *Deps) (bool, string) {
	near, far := CatherbyDoorTile, CatherbyDoorInside
	if here := Here(d.Session); here != nil {
		if *here == far {
			return true, "" // already on the far side
		}
		if tile.Chebyshev(*here, CatherbyDoorInside) < tile.Chebyshev(*here, CatherbyDoorTile) {
			near, far = CatherbyDoorInside, CatherbyDoorTile
		}
	}
	if !d.WalkNear(near, 0, 30*time.Second) {
		if d.stopped() {
			return false, WhyStopped
		}
		return false, fmt.Sprintf("could not reach %d,%d,%d beside the Catherby range's own door", near.X, near.Z, near.Level)
	}
	q := locindex.Query{X: CatherbyDoorTile.X, Z: CatherbyDoorTile.Z, Level: CatherbyDoorTile.Level, Radius: 1, Name: catherbyDoorName, Op: "Open"}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	if m, ok := d.Locs.Nearest(d.Config, q, live); ok {
		d.logf("cooking: opening the Catherby range's own door")
		if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
			if d.stopped() {
				return false, WhyStopped
			}
			return false, fmt.Sprintf("OpLoc(door): %v", err)
		}
		waitFor(d, "the door to open", 3*time.Second, func() bool {
			live := locindex.ChangesFrom(d.Session.State().LocChanges())
			_, stillClosed := d.Locs.Nearest(d.Config, q, live)
			return !stillClosed
		})
	}
	if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: far.X, Z: far.Z, Level: far.Level}}, false); err != nil {
		if d.stopped() {
			return false, WhyStopped
		}
		return false, fmt.Sprintf("step across the door: %v", err)
	}
	if !waitFor(d, "stepping across the Catherby range's own door", 5*time.Second, func() bool {
		here := Here(d.Session)
		return here != nil && *here == far
	}) {
		if d.stopped() {
			return false, WhyStopped
		}
		return false, fmt.Sprintf("did not arrive on %d,%d,%d across the door", far.X, far.Z, far.Level)
	}
	return true, ""
}

// OnCatherbyRangeSide reports whether t is inside the small room the Catherby range stands in, on
// the far side of CatherbyDoorTile from the bank. cmd/doordiag on the collision pack: a 13-tile
// component, x 2815..2818, z 3439..3443 on level 0, whose only way to the bank is the door's edge.
// The door's own tile (z 3438) is on the bank side.
func OnCatherbyRangeSide(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2815 && t.X <= 2818 && t.Z >= 3439 && t.Z <= 3443
}

// FindRange is the nearest loc named RangeLocName within radius of the account's tile.
func FindRange(d *Deps, radius int) (locindex.Match, bool) {
	here := Here(d.Session)
	if here == nil || d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	return d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: radius, Name: RangeLocName}, live)
}

// ReachLoc is the locreach model of a found loc, and false when its type has no config.
func ReachLoc(cfg *cachecfg.Config, m locindex.Match) (locreach.Loc, bool) {
	if cfg == nil {
		return locreach.Loc{}, false
	}
	typ, ok := cfg.Loc(m.ID)
	if !ok {
		return locreach.Loc{}, false
	}
	return locreach.Loc{
		X: m.X, Z: m.Z, Level: m.Level,
		Width: typ.Width, Length: typ.Length,
		Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach,
	}, true
}

// ApproachRange walks the account onto a tile the ENGINE accepts an OPLOCU against rng from, and
// reports whether it got there. The engine runs a loc's [oplocu,...] trigger only from a tile its
// own reachedLoc accepts and, with clientRoutefinder on, never walks the account into position;
// from any other tile the use produces only "I can't reach that!". A range is a 1x2 loc with
// forceapproach 13, so only its footprint and the tiles on its open side work (internal/locreach
// has the live transcript).
func ApproachRange(d *Deps, rng locindex.Match) bool {
	l, ok := ReachLoc(d.Config, rng)
	if !ok {
		d.logf("cooking: no config for range loc %d — using whatever tile the walk reached", rng.ID)
		return true
	}
	here := Here(d.Session)
	if here != nil {
		if reached, modelled := locreach.Reached(here.X, here.Z, l); modelled && reached {
			return true
		}
	}
	for _, to := range locreach.Tiles(l) {
		if here != nil && to == *here {
			return true
		}
		if d.WalkNear(to, 0, 20*time.Second) {
			return true
		}
	}
	d.logf("cooking: could not stand on any tile the engine accepts a use on range %d at %d,%d from", rng.ID, rng.X, rng.Z)
	return false
}

// PackItems is the account's backpack (PlayerPack), nil-safe.
func PackItems(s actions.Session) []gamestate.SlotItem {
	inv := s.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// PackCount is how many items named name the backpack holds (case-insensitive).
func PackCount(s actions.Session, cfg *cachecfg.Config, name string) int {
	n := 0
	for _, it := range PackItems(s) {
		obj, ok := cfg.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// packSlotFor is the first backpack slot holding an UN-NOTED name: a note cannot go on a range.
func packSlotFor(s actions.Session, cfg *cachecfg.Config, name string) (int32, int, bool) {
	for _, it := range PackItems(s) {
		if obj, ok := cfg.Obj(int(it.ID)); ok && !obj.Noted() && strings.EqualFold(obj.Name, name) {
			return it.ID, it.Slot, true
		}
	}
	return 0, 0, false
}

// invFeed wakes a waiter whenever the account's backpack changes. The engine writes a tick's inventory changes after that
// tick's PLAYER_INFO, so a loop woken by one acts inside the tick the change arrived in, which a 100 ms poll followed by a
// fixed pause never did.
type invFeed struct {
	wake   chan struct{}
	cancel func()
}

func newInvFeed(s actions.Session) *invFeed {
	f := &invFeed{wake: make(chan struct{}, 1)}
	if st := s.State(); st != nil {
		f.cancel = st.Subscribe(func(e gamestate.Event) {
			if e.Kind != gamestate.EventInventoryChanged || e.Comp != PlayerPack {
				return
			}
			select {
			case f.wake <- struct{}{}:
			default:
			}
		})
	}
	return f
}

func (f *invFeed) stop() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
}

// waitFor holds until cond is true, for up to timeout, looking again at every backpack change and, in case one was
// missed, every 100 ms. It returns false on a timeout (said in the log) and false at once, unsaid, when d.Delay(0)
// reports that the caller wants to stop: a zero-length Delay is the stop test every caller's delay already answers.
func (f *invFeed) waitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	poll := time.NewTimer(100 * time.Millisecond)
	defer poll.Stop()
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			d.logf("timed out waiting for %s", what)
			return false
		}
		select {
		case <-f.wake:
		case <-poll.C:
			poll.Reset(100 * time.Millisecond)
		}
		if !d.Delay(0) {
			return false
		}
	}
}

// CookOut uses the pack's raw item on the range at rng repeatedly -- one OPLOCU, wait for the raw
// count to drop, the next OPLOCU at once -- until none is left (cooked or burnt alike: content's
// cook_item deletes one raw and adds the cooked or the burnt item in its place), a use does not
// clear, guard uses have been made, or stop reports true (nil: never). Returns how many were
// consumed. mulerunner's cookOutTier, with its yield check passed in as stop.
//
// THE NEXT USE IS WRITTEN THE MOMENT THE BACKPACK SHOWS THE RAW ITEM GONE, with no pause. Measured on the lab
// engine at real speed (docs/tick_manipulation_lab_2026-10-06.md, 4.6; cmd/wcmanipprobe -mode cook): content's
// cook_item does p_delay(1) before it deletes the raw item, so a use read at tick T consumes at T+2, uses read at
// T+1 and T+2 are refused, and the next one is accepted at T+3, one cook per 3 ticks at best. The engine sends the
// inventory change after that tick's PLAYER_INFO, so a use written on the change is read at T+3. This loop used to
// wait 600 ms after seeing the change (one more tick) and polled for it every 100 ms, which made it 4.01 ticks a
// cook (24.9 per 100 ticks) against 3.06 (32.7) for the event-driven write. A use that arrives late is only read a
// tick later, so the worst case is the old cadence; nothing is ever written before the drop, so none is refused.
func CookOut(d *Deps, rng locindex.Match, raw string, guard int, stop func() bool) int {
	if d.Loot == nil {
		return cookOut(d, rng, raw, guard, stop)
	}
	// Onto the Loot tab's Cooking card: one event a raw food used, the cooked and burnt food that
	// arrived as its products, measured as the pack's own change round the whole run (owner, 7 Oct
	// 2026: every produced item counts under the activity that produced it).
	before := packCounts(d.Session)
	rawID, _, haveRaw := packSlotFor(d.Session, d.Config, raw)
	consumed := cookOut(d, rng, raw, guard, stop)
	if haveRaw && consumed > 0 {
		loot.MadeFrom(d.Loot, d.Config, loot.Cooking(d.Config, int(rawID), raw), consumed, before, packCounts(d.Session),
			func(id int) bool { return id == int(rawID) })
	}
	return consumed
}

// packCounts is the backpack as loot.Counts.
func packCounts(s actions.Session) loot.Counts {
	out := loot.Counts{}
	for _, it := range PackItems(s) {
		n := int64(it.Count)
		if n < 1 {
			n = 1
		}
		out[int(it.ID)] += n
	}
	return out
}

func cookOut(d *Deps, rng locindex.Match, raw string, guard int, stop func() bool) int {
	feed := newInvFeed(d.Session)
	defer feed.stop()
	consumed := 0
	for i := 0; i < guard; i++ {
		if stop != nil && stop() {
			break
		}
		rawID, slot, ok := packSlotFor(d.Session, d.Config, raw)
		if !ok {
			break // none left in the pack
		}
		before := PackCount(d.Session, d.Config, raw)
		if err := actions.OpLocU(d.Session, rng.X, rng.Z, rng.ID, rawID, slot, PlayerPack); err != nil {
			d.logf("cooking: OpLocU(%s on range): %v", raw, err)
			if !d.Delay(time.Second) {
				break
			}
			continue
		}
		if !feed.waitFor(d, fmt.Sprintf("%s to finish cooking", raw), 5*time.Second, func() bool {
			return PackCount(d.Session, d.Config, raw) < before
		}) {
			if !d.Delay(0) {
				break
			}
			continue
		}
		consumed++
	}
	return consumed
}
