package essencerunner

import (
	"errors"
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// ---- mining ----
//
// WHY A LEVEL-UP USED TO STRAND THE MINER FOR TEN MINUTES (found in the lab 15 Oct 2026, claude/mineexit-1015).
//
// The live symptom was a miner with a full pack that re-sent its walk to the portal every two seconds for 8-11 minutes without a
// step, a "Your inventory is too full" box (interface 356) found open before every walk and "back every time" after Continue or
// CLOSE_MODAL, until it freed itself. It was not a modal blocking Player.updateMovement and nothing in the engine reopened the
// box: the account's own input was queued behind thousands of its own clicks on the rock.
//
// The cycle, step by step (Engine-TS 289, NetworkPlayer.decodeIn, World.processClientsIn, OpLocHandler, Player.processInteraction;
// content skill_mining/scripts/mining.rs2 and levelup/scripts/levelup.rs2):
//
//  1. Essence gives XP, so a low-Mining miner levels up every few loads. Player.addXp queues the level-up on the ENGINE queue and
//     the next tick it runs and opens the level-up chat (4416, content's @levelup: if_openchat, p_pausebutton).
//  2. The old hold loop read an open continue-able chat as "interrupted" and returned "dialog" at once. RunStretch (and the supply
//     loop) call Mine again without a pause, so Mine clicked the rock (OpLocHandler closes the chat and re-sets the mining
//     interaction), looked at its own client's state, still saw the chat (the server's IF_CLOSE is a tick away) and returned
//     "dialog" again: about 8,000 clicks a second for the 0.2-0.6 s until IF_CLOSE arrived. Lab: 1,448 clicks in 173 ms.
//  3. The engine reads at most five USER events per player per tick (ClientGameProtCategory.USER_EVENT, NetworkPlayer.decodeIn)
//     and leaves the rest in the socket buffer, in order. 1,448 clicks are 290 ticks (174 s at 600 ms); the live episodes were
//     8-11 minutes, about 3,000 clicks. Every click re-sets the mining interaction, and once the pack is full each one runs
//     mining_continue's `inv_freespace(inv) < 1` branch and opens the box again.
//  4. Everything the bot sends afterwards (RESUME_PAUSEBUTTON, MOVE_GAMECLICK, CLOSE_MODAL) is read AFTER those clicks. The client
//     sees the box open, presses Continue, walks, sees it open again: each answer arrives minutes late, and between them the
//     queued clicks keep reopening it. The walk is not refused; it is behind the queue, and it takes its step the moment the queue
//     drains, which is why the account "freed itself" 8-11 minutes later, and why a Mining 50 miner (a level-up once in many
//     loads) walked out first time.
//
// Hence the rules (internal/actions/clickgate.go and holdchat.go, shared with every other hold loop since 15 Oct, see
// docs/click_flood_audit_2026-10-15.md): one click on the rock per actions.ClickGapTicks game ticks however Mine is called; a chat
// that was already up when the rock was clicked is the click's own to close (it is not an interruption); a level-up that opens
// while mining is answered with its Continue button, here, instead of being handed back to a caller that calls Mine again.

// MineResult is one Mine attempt's outcome.
type MineResult struct {
	// Gained is essence caught during this call.
	Gained int
	// Reason is why the mining session ended: "no-rock", "full", "stalled", "dialog" (a chat that Continue and the rock click
	// could not close), "left-mine", "stopped", or "pickaxe-head" (the lost-pickaxe event took the pickaxe and the head could
	// not be put back: RecoverPickaxeHead).
	Reason string
	// Clicks is how many times this call clicked the rock (a knife batch carries a click and counts as one), Dialogs how many chats
	// it answered with their Continue button, Batches how many knife-on-log batches it wrote (minemanip.go).
	Clicks, Dialogs, Batches int
}

// mineTickInterval is how often the hold loop below polls essence count/pack/dialog state
// while a rock is being mined — one game tick-ish, matching every sibling runner's own
// "poll fast, act on what changed" hold loops (fisherrunner.FishUntil's own inner loop).
const mineTickInterval = 600 * time.Millisecond

// pickaxeHeadCheckAfter is how long the rock may give nothing before the hold loop looks for the lost-pickaxe event's handle.
const pickaxeHeadCheckAfter = 3 * time.Second

// Mine finds the nearest essence rock, clicks it, and holds the click until the pack fills,
// no essence lands for essenceplan.MineStallTimeout, a chat it cannot close interrupts, or the account
// leaves the mine — ported from mine(), with the click and chat rules the comment above sets out: one click per actions.ClickGapTicks
// ticks, a level-up answered with Continue in place, never handed back for the caller to click again.
// rockLoc is the locreach model of a found essence rock, from its config's size and approach rules.
func rockLoc(d *Deps, m locindex.Match) (locreach.Loc, bool) {
	if d.Config == nil {
		return locreach.Loc{}, false
	}
	typ, ok := d.Config.Loc(m.ID)
	if !ok {
		return locreach.Loc{}, false
	}
	return locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length,
		Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}, true
}

// rockApproaches caps how many of the rock's stand tiles are walked to before the old walk.
const rockApproaches = 3

// approachRock puts the account on a tile the engine runs Mine from (internal/locreach: the rock is a
// multi-tile loc, so "within one tile of its origin" is not reach). Live 15 Oct 2026 an EssenceMiner
// set down by the teleport at 2924,4847, one tile from the rock's origin, clicked from there for 17
// minutes: every click went unanswered, each Mine call ended "stalled" after 20 s and no walk was
// ever made. A rock the model does not cover keeps the old walk to within one tile.
func approachRock(d *Deps, m locindex.Match, here tile.Tile) {
	rockTile := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
	l, ok := rockLoc(d, m)
	if !ok {
		if tile.Chebyshev(here, rockTile) > 1 {
			WalkNear(d, rockTile, 1)
		}
		return
	}
	if reached, modelled := locreach.Reached(here.X, here.Z, l); !modelled || reached {
		if !modelled && tile.Chebyshev(here, rockTile) > 1 {
			WalkNear(d, rockTile, 1)
		}
		return
	}
	fx, fz, fw, fh := l.Footprint()
	var stands []tile.Tile
	for _, t := range locreach.Tiles(l) {
		if t.X >= fx && t.X < fx+fw && t.Z >= fz && t.Z < fz+fh {
			continue
		}
		stands = append(stands, t)
	}
	sort.SliceStable(stands, func(i, j int) bool { return tile.Chebyshev(stands[i], here) < tile.Chebyshev(stands[j], here) })
	for i, s := range stands {
		if i >= rockApproaches {
			break
		}
		if WalkNear(d, s, 0) {
			d.logf("mine: walked from %d,%d onto %d,%d to mine the rock at %d,%d (the tile we were on is not one the engine mines from)",
				here.X, here.Z, s.X, s.Z, m.X, m.Z)
			return
		}
	}
	WalkNear(d, rockTile, 1)
}

func Mine(d *Deps) MineResult {
	if r := RecoverPickaxeHead(d); r.Tried {
		if !r.Fixed {
			d.logf("mine: the lost-pickaxe event took the pickaxe and it could not be put back (%s)", r.Reason)
			return MineResult{Reason: "pickaxe-head"}
		}
		d.logf("mine: the lost-pickaxe event: the head was picked up and put back on the handle")
	}
	here := currentTile(d)
	m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: 20, Name: essenceplan.EssenceRock, Op: essenceplan.MineOp}, locindex.ChangesFrom(d.Session.State().LocChanges()))
	if !ok {
		d.logf("no essence rock in sight — waiting")
		return MineResult{Reason: "no-rock"}
	}
	approachRock(d, m, here)

	var res MineResult
	gained := 0
	hold := actions.NewHoldChat(d.Session, Waiter(d), d.Components, actions.LocKey(m.X, m.Z, int(m.ID)), func() error {
		return actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID)
	})
	hold.Busy = func() bool { return randomevents.Busy(d.Session) }
	hold.Held = func(ticks int) {
		d.logf("mine: holding the next click on the rock for %d tick(s): the server reads five clicks a tick and the rest wait in front of everything sent after them", ticks)
	}
	hold.OnAnswer = func(chat int) {
		d.logf("mine: answered chat %d (a level-up or a message box) with its Continue after %d essence", chat, gained)
	}
	// With the knife-on-log manipulation on, the rock click is a batch whenever the controller wants one and a kit is held (the
	// batch carries the click); otherwise it is the plain click this function always sent.
	mp := startManip(d, m)
	// click clicks the rock once the gap allows and returns "" or why it did not.
	click := func() string {
		if mp != nil && mp.open() {
			hold.Clicked() // the batch carried the click
			res.Clicks = hold.Clicks
			return ""
		}
		if err := hold.Click(); err != nil {
			if errors.Is(err, actions.ErrClickStopped) {
				return "stopped"
			}
			d.logf("OpLoc(Mine) on the essence rock: %v", err)
			return "no-rock"
		}
		res.Clicks = hold.Clicks
		return ""
	}

	if why := click(); why != "" {
		if mp != nil {
			mp.watch.stop()
		}
		res.Reason = why
		return res
	}
	// What this rock yields, for the Loot panel's Mining card (see minecount.go).
	// Reporting only; the window spans exactly this call.
	mined := beginMine(d, int(m.ID))
	count := EssenceHeld(d)
	lastGain := time.Now()
	lastHandleCheck := time.Now()
	finish := func(reason string) MineResult {
		res.Gained, res.Reason, res.Dialogs = mined.Done(gained), reason, hold.Answered
		if mp != nil {
			res.Batches = mp.wrote
			mp.finish(gained)
		}
		if res.Clicks > 3 || res.Dialogs > 0 {
			d.logf("mine: %d essence this call, %d click(s) on the rock (%d knife batch(es)), %d chat(s) answered (%s)", res.Gained, res.Clicks, res.Batches, res.Dialogs, reason)
		}
		return res
	}
	for {
		if !InMine(d) {
			// No longer where it was mining: a teleport, a random event, a death. What the
			// pack holds after that did not come out of the rock.
			mined.Taint()
			return finish("left-mine")
		}
		now := EssenceHeld(d)
		if now > count {
			mined.Got(now - count)
			gained += now - count
			count = now
			lastGain = time.Now()
		}
		if PackFull(d) {
			return finish("full")
		}
		if time.Since(lastGain) > pickaxeHeadCheckAfter && time.Since(lastHandleCheck) > pickaxeHeadCheckAfter {
			// The lost-pickaxe event leaves a handle where the pickaxe was and the rock stops giving: the head lies on the ground
			// for two minutes, so the call ends and the next one picks it up rather than clicking through the stall wait.
			lastHandleCheck = time.Now()
			if _, inPack := packItemID(d, essenceplan.IsPickaxeHandle); inPack || pickaxeHandleWorn(d) {
				return finish("pickaxe-head")
			}
		}
		switch hold.Step() {
		case actions.HoldClick:
			if _, _, chat := d.Session.State().Modals(); mp != nil && chat == gamestate.NoModal {
				// The chat the hold answered is gone and the interaction it interrupted carries on by itself (the batch read before
				// the chat armed the varp). A click now would be a batch that finds the varp armed and moves nothing; wait for the roll,
				// and click only if none comes back (minemanip.go).
				mp.answered()
			} else if why := click(); why != "" {
				return finish(why)
			}
		case actions.HoldHandBack:
			return finish("dialog")
		}
		if mp != nil && mp.resumeDue() {
			if why := click(); why != "" {
				return finish(why)
			}
		}
		if time.Since(lastGain) > essenceplan.MineStallTimeout {
			return finish("stalled")
		}
		if mp != nil {
			// A batch run waits for the next PLAYER_INFO instead of sleeping, so a batch leaves within microseconds of the tick it
			// follows.
			if mp.pace() {
				return finish("stopped")
			}
			continue
		}
		if !d.Delay(mineTickInterval) {
			return finish("stopped")
		}
	}
}
