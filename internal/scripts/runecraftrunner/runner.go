// Package runecraftrunner is the I/O half of the levelling round (internal/scripts/runecraftplan decides):
// the ruins, the altar and the portal found by name where the table says they are, the talisman used on
// the ruins and the arrival WAITED FOR, the craft measured, the portal timed to the end of the craft's hold,
// the bank visit that loads a trip, and the mining stretch at the Wizards' Guild that fills the bank.
//
// Nothing here is done until it is OBSERVED (docs/trustworthy_harnesses.md): a teleport is done when the
// account's tile is inside the temple, a craft when the essence has left the pack AND the Runecrafting
// experience rose, a portal when the tile is outside again. Every step counts a failure instead of
// assuming; the caller bounds them.
package runecraftrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// Stat is the Runecraft index in UPDATE_STAT (gamestate.SkillNames[20]).
const Stat = 20

// PlayerPack is the backpack's component, the one OPLOCU names for the used item.
const PlayerPack = fleetrunner.PlayerPack

// WalkFunc walks to dest within radius tiles in at most timeout and reports whether it arrived
// (mastercraftrunner.WalkFunc: the round's own walk).
type WalkFunc = mastercraftrunner.WalkFunc

// poll is how often a wait looks at the session: well inside a tick. A variable so the test of a cast's frames can look often enough to
// be exact on its engine's short tick.
var poll = 20 * time.Millisecond

// Waits. A teleport is given arriveWait to be seen (the cast holds one tick and the jump lands on the
// next, so it is normally seen in two or three); a craft craftWait; a portal portalWait. The first click
// of each is sent again once after half the wait if nothing at all happened: a click read while the
// player is still delayed is dropped without a word.
//
// They are variables so a test can shorten them.
var (
	arriveWait = 6 * time.Second
	craftWait  = 6 * time.Second
	portalWait = 6 * time.Second
	xpWait     = 2 * time.Second
)

// jumpTTL is how long a crossing's landing is announced to the account's Guardian (randomevents.ExpectLandings): longer than any
// wait for it here.
const jumpTTL = 20 * time.Second

// expect announces a crossing that is about to land in one of landings, so the Guardian's displacement line reads "expected:"
// and not "UNEXPLAINED" (live 14 Oct 2026, the Air and Body altars: 2984,3294 -> 2841,4830, 2841,4829 -> 2983,3288 and the
// Body pair, ten lines in forty minutes). The result withdraws the announcement.
func expect(d *fleetrunner.Deps, name string, landings ...randomevents.Landing) func() {
	ttl := jumpTTL
	if Shaky(d) {
		// A view that lags is waited for longer than jumpTTL (lagwait.go), and a landing it reads late is still the crossing's.
		ttl += lagExpectExtra
	}
	return randomevents.ExpectLandings(d.Session, name, ttl, landings...)
}

// lagExpectExtra is how much longer a crossing's landing is announced when the view is shaky.
const lagExpectExtra = 40 * time.Second

func logf(d *fleetrunner.Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// waitUntil polls cond until it holds or timeout passes; false on a timeout or a stop.
func waitUntil(d *fleetrunner.Deps, cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		if !d.Delay(poll) {
			return false
		}
	}
	return true
}

// stopped reports whether the bot has been told to stop.
func stopped(d *fleetrunner.Deps) bool { return !d.Delay(0) }

// ---- reads ----

// Level is the Runecrafting level and experience as the server last sent them (0, 0 until it has).
func Level(d *fleetrunner.Deps) (level, xp int) {
	if sk, ok := d.Session.State().Skill(Stat); ok && sk.Generation > 0 {
		return sk.Level, sk.XP
	}
	return 0, 0
}

// Here is the account's tile (the zero tile when it is not known).
func Here(d *fleetrunner.Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

func packItems(d *fleetrunner.Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// PackCount is how many of object id the pack holds (a stack at its size).
func PackCount(d *fleetrunner.Deps, id int32) int {
	n := 0
	for _, it := range packItems(d) {
		if it.ID == id {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// Essence is the unnoted Rune essence in the pack: the only kind the altar binds.
func Essence(d *fleetrunner.Deps) int { return PackCount(d, runecraftplan.EssenceID) }

// PackFree is the pack's empty slots.
func PackFree(d *fleetrunner.Deps) int { return runecraftplan.PackSlots - len(packItems(d)) }

// Talismans is which altar keys' talismans the pack holds.
func Talismans(d *fleetrunner.Deps) map[string]bool {
	out := map[string]bool{}
	for _, a := range runecraftplan.Altars() {
		if PackCount(d, int32(a.TalismanID)) > 0 {
			out[a.Key] = true
		}
	}
	return out
}

// slotOf is the pack slot holding object id.
func slotOf(d *fleetrunner.Deps, id int32) (int, bool) {
	for _, it := range packItems(d) {
		if it.ID == id {
			return it.Slot, true
		}
	}
	return 0, false
}

// said is the chat since t, trimmed and on one line, for a log.
func said(d *fleetrunner.Deps, t time.Time) string {
	if d.Chat == nil {
		return ""
	}
	return strings.Join(strings.Fields(d.Chat.Since(t)), " ")
}

// ---- finding the locs ----

// find is the loc of the given name (and option, when op is set) nearest the table's tile for it, within
// runecraftplan.RuinsRadius, and only when its type is the table's id: the account never clicks a loc it
// did not expect. The loc is resolved by name through the loc index (CLAUDE.md: never a hardcoded id at a
// coordinate), the id only checked.
func find(d *fleetrunner.Deps, name, op string, id int, at tile.Tile) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	for _, m := range d.Locs.Find(d.Config, locindex.Query{X: at.X, Z: at.Z, Level: at.Level, Radius: runecraftplan.RuinsRadius, Name: name, Op: op}, live) {
		if m.ID == id {
			return m, true
		}
	}
	return locindex.Match{}, false
}

// FindRuins is the altar's ruins, FindAltar its altar and FindPortal the portal inside.
func FindRuins(d *fleetrunner.Deps, a runecraftplan.Altar) (locindex.Match, bool) {
	return find(d, runecraftplan.RuinsName, "", a.RuinsID, a.Ruins)
}

// FindAltar is the altar loc (option Craft-rune) of a, for an account in its temple.
func FindAltar(d *fleetrunner.Deps, a runecraftplan.Altar) (locindex.Match, bool) {
	return find(d, runecraftplan.AltarName, runecraftplan.AltarOp, a.AltarID, a.AltarAt)
}

// FindPortal is the exit portal (option Use) of a's temple.
func FindPortal(d *fleetrunner.Deps, a runecraftplan.Altar) (locindex.Match, bool) {
	return find(d, runecraftplan.PortalName, runecraftplan.PortalOp, a.PortalID, a.PortalAt)
}

// Reached reports whether the account stands where the engine accepts a use of, or an option on, the loc
// (internal/locreach: a 3x3 loc is reached from its sides, never "within 1 tile").
func Reached(d *fleetrunner.Deps, m locindex.Match) bool {
	return mastercraftrunner.FurnaceReached(d, m)
}

// Stands is the tiles the engine accepts a use on the loc from, nearest to from first, and that somebody can stand on: a tile
// locreach lists (it models the engine's reach rule and knows nothing of walls) that the collision pack says is blocked is left
// out. Live 16 Oct 2026 the Nature ruins' list led with (2867,3019), a blocked tile: the walker's arrival rule stops a walk to a
// blocked tile one tile short, the talisman was then clicked from a tile that is not a stand ("I can't reach that!") and
// four episodes of two 27-second failures cost 6 percent of the gate's time.
func Stands(d *fleetrunner.Deps, m locindex.Match, from tile.Tile) []tile.Tile {
	all := mastercraftrunner.FurnaceStands(d, m, from)
	if d.Pack == nil {
		return all
	}
	out := make([]tile.Tile, 0, len(all))
	for _, t := range all {
		if d.Pack.Walkable(t.X, t.Z, t.Level) {
			out = append(out, t)
		}
	}
	return out
}

// Approach walks the account onto a tile the engine accepts a use on the loc from, nearest first, and
// reports whether it got there.
func Approach(d *fleetrunner.Deps, m locindex.Match, walk WalkFunc, first time.Duration) bool {
	return mastercraftrunner.ApproachFurnace(d, m, walk, first)
}

// ---- the talisman on the ruins ----

// TalismanResult is one UseTalisman.
type TalismanResult struct {
	// Arrived is set when the account was seen inside the temple; At is where, Ticks how many server ticks
	// after the click.
	Arrived bool
	At      tile.Tile
	Ticks   int
	// Clicks is how many times the use was sent (the second only when nothing at all came of the first); ClickTick the server tick the
	// first was sent in (the gate's log sets it beside the tick its trade closed in, so the time between them is measured, not guessed).
	Clicks    int
	ClickTick int
	// Said is what the game said meanwhile; Reason why the use failed.
	Said, Reason string
	// Lag is set when the view was lagging, shaky or slow during the waits (lagwait.go): a failure with Lag says nothing about the
	// click, only that its result was not seen in time.
	Lag bool
}

// UseTalisman uses a's talisman on its ruins and waits for the account to be SEEN inside the temple (a
// teleport is not done until the tile is observed). The account must stand where the engine accepts a use
// on the ruins (Approach): from anywhere else the engine does nothing and says "I can't reach that!".
func UseTalisman(d *fleetrunner.Deps, a runecraftplan.Altar, ruins locindex.Match) (res TalismanResult) {
	return useTalisman(d, a, ruins, nil)
}

// UseTalismanSent is UseTalisman for a use that has ALREADY been written, in the same message as the trade's confirm accept (together.go):
// it does not send it again, it waits for the account to be seen inside the temple, and sends the use once more, as UseTalisman does,
// only if nothing at all came of the first in the wait. A nil sent is UseTalisman.
func UseTalismanSent(d *fleetrunner.Deps, a runecraftplan.Altar, ruins locindex.Match, sent *TalismanSent) (res TalismanResult) {
	return useTalisman(d, a, ruins, sent)
}

func useTalisman(d *fleetrunner.Deps, a runecraftplan.Altar, ruins locindex.Match, sent *TalismanSent) (res TalismanResult) {
	slot, haveSlot := slotOf(d, int32(a.TalismanID))
	if !haveSlot && sent == nil {
		res.Reason = "no " + a.Talisman + " in the pack"
		return res
	}
	st := d.Session.State()
	at, tick0 := time.Now(), st.Tick()
	inside := func() bool { return a.Inside(Here(d)) }
	var withdraw func()
	if sent != nil {
		// The use went out with the accept: the wait is for its result, timed from the write, and its announcement to the Guardian stands.
		at, tick0, withdraw = sent.At, sent.Tick, sent.Withdraw
		if withdraw == nil {
			withdraw = func() {}
		}
	} else {
		withdraw = expect(d, "runecraft: the "+a.Talisman+" on the "+a.Name+" ruins", randomevents.LandingNear(a.Enter.X, a.Enter.Z, 3))
	}
	defer func() {
		// A use whose arrival was not seen in a view that lags may still land: its announcement stays until it runs out, so the
		// Guardian reads the late landing as the crossing it is and not as "UNEXPLAINED".
		if !res.Arrived && !res.Lag {
			withdraw()
		}
	}()
	// "I can't reach that!" is the engine saying the account does not stand where a use on the ruins is accepted: it arrives within a
	// tick or two and nothing else will come of the click, so it ends the wait at once (it used to cost the whole of arriveWait twice,
	// six seconds) and a second click from the same tile is not sent.
	cantReach := func() bool { return strings.Contains(strings.ToLower(said(d, at)), "can't reach") }
	var note seenNote
	for res.Clicks < 2 {
		if res.Clicks == 0 && sent != nil {
			res.Clicks, res.ClickTick = 1, sent.Tick
		} else {
			if !haveSlot {
				res.Reason = "no " + a.Talisman + " in the pack to use again"
				return res
			}
			if err := actions.OpLocU(d.Session, ruins.X, ruins.Z, ruins.ID, int32(a.TalismanID), slot, PlayerPack); err != nil {
				res.Reason = "the use could not be sent: " + err.Error()
				return res
			}
			res.Clicks++
			if res.Clicks == 1 {
				res.ClickTick = st.Tick()
			}
		}
		ok, n := waitSeen(d, func() bool { return inside() || cantReach() }, arriveWait/2)
		note.add(n)
		res.Lag = note.any()
		if ok {
			if inside() {
				break
			}
			res.Said = said(d, at)
			res.Reason = fmt.Sprintf("the engine does not accept the %s on the %s ruins from %v (the game said: %s)", a.Talisman, a.Name, Here(d), res.Said)
			return res
		}
		if stopped(d) {
			res.Reason = "stopped"
			return res
		}
		if note.lagged {
			// The result of the first click has not been seen in a view that lags: it may have landed. A second use read inside the
			// teleport's own delay is dropped and one read after it is refused ("I can't reach that!"), so it only adds a message
			// to a stage that is waiting for one; the last wait below is for the view to catch up.
			break
		}
	}
	if !inside() {
		// A last short wait: the second click's arrival.
		_, n := waitSeen(d, inside, arriveWait/2)
		note.add(n)
		res.Lag = note.any()
	}
	res.Said = said(d, at)
	if !inside() {
		res.Reason = fmt.Sprintf("not seen inside the %s temple within %s", a.Name, arriveWait)
		if res.Said != "" {
			res.Reason += " (the game said: " + res.Said + ")"
		}
		if res.Lag {
			res.Reason += " (the view was lagging: the arrival may not have been read yet)"
		}
		return res
	}
	res.Arrived, res.At, res.Ticks = true, Here(d), st.Tick()-tick0
	return res
}

// ---- the craft ----

// CraftResult is one Craft.
type CraftResult struct {
	// Essence is what the pack held before, Left after; Runes the runes the pack gained; XPGain the points the
	// stat gained; Level0 and Level1 the levels around it.
	Essence, Left, Runes, XPGain int
	Level0, Level1               int
	// Tick is the server tick the craft was seen in (the portal is timed from it), Ticks how many ticks the
	// craft took to show after the first click.
	Tick, Ticks int
	// Clicks is how many times the altar was clicked.
	Clicks int
	// Verify is the measured result; OK only when the essence is gone and experience came.
	Verify runecraftplan.Verify
	// Said is what the game said, Reason why a craft failed.
	Said, Reason string
	// Lag is set when the view was lagging, shaky or slow around the click (lagwait.go): a failure with Lag says nothing about the
	// click, only that its result was not seen in time.
	Lag bool
	// Direct is set when the click went out in the same write as the walk to the altar's stand (CraftDirect). DirectFailed says that
	// attempt demonstrably did nothing -- the engine said "I can't reach that!", or nothing at all happened in a view that was keeping
	// up -- with the essence still in the pack, so the caller may walk to the stand and click the plain way; Reason says why.
	Direct, DirectFailed bool
	// Cast is the record of the cast that rode on the leg (CraftDirectCast); its Tried is false for a leg with none.
	Cast runecraftplan.LegCastRecord
}

// OK reports whether the craft happened.
func (r CraftResult) OK() bool { return r.Reason == "" && r.Verify.OK }

// Craft binds the essence in the pack at the altar (option Craft-rune) and VERIFIES it: the essence must
// have left the pack and the Runecrafting experience risen by what the table says (the live rate is
// measured here, not assumed). The click is sent again once if nothing at all came of it.
func Craft(d *fleetrunner.Deps, a runecraftplan.Altar, altar locindex.Match) (res CraftResult) {
	st := d.Session.State()
	// A pack read from a view that lags says nothing about the pack: a craft that landed in the silence would be bound again.
	var note seenNote
	note.lagged = AwaitView(d)
	res.Lag = note.any()
	res.Essence = Essence(d)
	res.Level0, _ = Level(d)
	_, xp0 := Level(d)
	runes0 := PackCount(d, int32(a.RuneID))
	if res.Essence == 0 {
		res.Reason = "no Rune essence in the pack to bind"
		return res
	}
	gone := func() bool { return Essence(d) == 0 }
	at, tick0 := time.Now(), st.Tick()
	for round := 0; round < 2 && !gone(); round++ {
		// The second click is sent only when the first one's silence is the server's: in a view that lags the first may have landed
		// late, and the altar answers an empty pack with a message box that stops every later loc click (lagwait.go).
		if round == 0 || !note.lagged {
			if err := actions.OpLoc(d.Session, altar.Op, altar.X, altar.Z, altar.ID); err != nil {
				res.Reason = "the click could not be sent: " + err.Error()
				return res
			}
			res.Clicks++
		}
		_, n := waitSeen(d, func() bool {
			if gone() {
				return true
			}
			s := strings.ToLower(said(d, at))
			return strings.Contains(s, "rune stones") || strings.Contains(s, "level of at least")
		}, craftWait/2)
		note.add(n)
		res.Lag = note.any()
		if !gone() {
			if s := said(d, at); s != "" {
				res.Said = s
				low := strings.ToLower(s)
				if strings.Contains(low, "rune stones") || strings.Contains(low, "level of at least") || strings.Contains(low, "members") {
					res.Reason = "the game refused the craft: " + s
					return res
				}
			}
		}
	}
	if !gone() {
		res.Left = Essence(d)
		res.Reason = fmt.Sprintf("%d essence still in the pack %s after the click", res.Left, craftWait)
		if res.Said = said(d, at); res.Said != "" {
			res.Reason += " (the game said: " + res.Said + ")"
		}
		if res.Lag {
			res.Reason += " (the view was lagging: the craft may not have been read yet)"
		}
		return res
	}
	return finishCraft(d, a, res, note, xp0, runes0, at, tick0)
}

// finishCraft is the end of a craft whose essence has left the pack: the tick it was seen in, the experience waited for, the result
// measured and verified, and the Loot panel told. Craft and CraftDirect share it.
func finishCraft(d *fleetrunner.Deps, a runecraftplan.Altar, res CraftResult, note seenNote, xp0, runes0 int, at time.Time, tick0 int) CraftResult {
	st := d.Session.State()
	res.Tick = st.Tick()
	res.Ticks = res.Tick - tick0
	// The experience comes in the same tick's stat update as the inventory change; give it a moment.
	_, n := waitSeen(d, func() bool { _, xp := Level(d); return xp > xp0 }, xpWait)
	note.add(n)
	res.Lag = note.any()
	res.Level1, _ = Level(d)
	_, xp1 := Level(d)
	res.XPGain = xp1 - xp0
	res.Runes = PackCount(d, int32(a.RuneID)) - runes0
	res.Left = Essence(d)
	res.Said = said(d, at)
	res.Verify = runecraftplan.VerifyCraft(a, res.Essence, res.Left, res.XPGain)
	if !res.Verify.OK {
		res.Reason = fmt.Sprintf("the essence left the pack but the Runecrafting experience did not rise (+%d, expected +%d)", res.XPGain, res.Verify.ExpectedXP)
	}
	if res.OK() {
		// The Loot panel's Runecraft card (internal/loot/runecraft.go), reported HERE because this is where a craft is known to
		// have happened: the essence gone and the experience risen, with the runes the pack gained and the level read afterwards
		// all measured in this function. Every caller (the levelling rounds, the Nature gate) is therefore counted once, and a
		// refused or unverified craft is never reported. Reporting only: a nil sink records nothing.
		loot.RuneCrafted(d.Loot, d.Config, a.Name, a.RuneID, a.Rune, res.Essence, res.Runes, res.XPGain, res.Level1)
	}
	return res
}

// ---- the portal ----

// PortalResult is one PortalOut.
type PortalResult struct {
	// Out is set when the account was seen outside the temple; At is where.
	Out bool
	At  tile.Tile
	// Clicks is how many times the portal was clicked (a click read in the craft's hold is dropped and sent
	// again), Waited the ticks the first click was held back for the hold.
	Clicks, Waited int
	Reason         string
	// Lag is set when the view was lagging, shaky or slow during the waits (lagwait.go): a failure with Lag says nothing about the
	// click, only that its result was not seen in time.
	Lag bool
	// Direct is set when a click went out in the same write as the walk to the portal's stand (PortalOutDirect); Clicks counts it.
	// DirectFailed says that attempt demonstrably did nothing and the plain approach and clicks ran after it (so Out may be true with it
	// set); DirectWhy is why.
	Direct, DirectFailed bool
	DirectWhy            string
	// Cast is the record of the cast that rode on the leg (PortalOutCast); its Tried is false for a leg with none.
	Cast runecraftplan.LegCastRecord
}

// PortalOut leaves a's temple by its portal. craftTick is the server tick a craft was seen in (0: none to
// wait for). The craft is p_delay(3) at tick T: delayedUntil = T + 1 + 3 = T+4. World.cycle READS the packets
// (processClientsIn) before processPlayers clears the flag (`currentTick >= delayedUntil`), so a packet read
// in any of the ticks T+1..T+4 is dropped without a word -- the tick delayedUntil itself included -- and T+5
// is the first free read. A click sent once the client has seen tick T+4's output (craftTick + HoldTicks) is
// read at T+5: no later, and no earlier (one sent after T+3's output is read at T+4 and lost, which a model
// that clears the flag before the read hides). A click that still moved nobody after a third of the wait is
// sent again. The portal itself has no delay.
//
// approach, when not nil, is run once the hold is over (a walk read inside the hold is discarded just as a click
// is): it closes a level-up's dialog and walks the account onto a tile the engine accepts the portal from, if it
// is not on one already.
func PortalOut(d *fleetrunner.Deps, a runecraftplan.Altar, portal locindex.Match, craftTick int, approach func() bool) PortalResult {
	return portalOut(d, a, portal, craftTick, approach, nil)
}

// portalOut is PortalOut, and PortalOutDirect and PortalOutCast when plan is not nil.
func portalOut(d *fleetrunner.Deps, a runecraftplan.Altar, portal locindex.Match, craftTick int, approach func() bool, plan func() (DirectOp, *LegCast, bool)) (res PortalResult) {
	st := d.Session.State()
	var note seenNote
	if craftTick > 0 {
		release := craftTick + runecraftplan.HoldTicks
		start := st.Tick()
		// The hold is four ticks, not 2.4 seconds: a server that ticks slowly holds the player for longer, and a click read inside the
		// hold is dropped without a word (18 Oct 2026: "waiting 1 tick(s) for the craft's hold, 3 click(s)", the walk to the portal
		// "stalled" twice). So the wait is for the TICK, with the wall-clock cap stretched for a view that lags or ticks slowly
		// (waitSeen) and still ending for a session that sees no ticks at all.
		_, n := waitSeen(d, func() bool { return st.Tick() >= release }, time.Duration(runecraftplan.HoldTicks+2)*tickPace)
		note.add(n)
		res.Lag = note.any()
		res.Waited = max(0, st.Tick()-start)
	}
	out := func() bool { return !a.Inside(Here(d)) }
	// A direct op (direct.go) goes first, at the first accepted read, and only the plain way follows a direct attempt that did nothing.
	directClicks := 0
	if plan != nil && !out() {
		if op, lc, ok := plan(); ok {
			var done bool
			done, directClicks = directPortal(d, a, portal, op, lc, out, &res, &note)
			if done {
				return res
			}
		}
	}
	if approach != nil {
		approach()
	}
	withdraw := expect(d, "runecraft: the "+a.Name+" temple's portal", randomevents.LandingNear(a.Exit.X, a.Exit.Z, 3))
	defer func() {
		if !res.Out && !res.Lag {
			withdraw()
		}
	}()
	for res.Clicks-directClicks < 3 && !out() {
		if err := actions.OpLoc(d.Session, portal.Op, portal.X, portal.Z, portal.ID); err != nil {
			res.Reason = "the click could not be sent: " + err.Error()
			return res
		}
		res.Clicks++
		_, n := waitSeen(d, out, portalWait/3)
		note.add(n)
		res.Lag = note.any()
		if note.lagged && !out() {
			// Not sent again for a late answer: the first may have landed, and a view that lags will say so.
			_, n = waitSeen(d, out, portalWait/3)
			note.add(n)
			res.Lag = note.any()
			break
		}
	}
	if !out() {
		res.Reason = fmt.Sprintf("still inside the %s temple %s after %d click(s) on the portal", a.Name, portalWait, res.Clicks)
		if res.Lag {
			res.Reason += " (the view was lagging: the exit may not have been read yet)"
		}
		return res
	}
	res.Out, res.At = true, Here(d)
	return res
}
