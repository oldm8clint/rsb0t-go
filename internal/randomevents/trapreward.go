package randomevents

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
)

// A TRAPPED EVENT'S REWARD DIALOGUE HOLDS THE ACCOUNT'S SIDEBAR HOSTAGE, so the Guardian
// finishes the event by pressing it.
//
// Live on 2026-09-23 (build 3dfb543a) Acct188 came back from a Mime and then wrote nothing,
// moved nowhere and gained nothing for twenty minutes, with botState "running" throughout; a
// bot restart did not help and only the next fleet restart did. Across the archived logs the
// same thing happened 17 times to 14 woodcutter-family accounts, ~22.6 account-hours in all.
// The chain, read from content and the engine and then reproduced in the lab
// (private evidence notes cmd/mimestall):
//
//   - The Mime's entry nulls sidebar tabs 0-6 (~macro_events_set_tabs; the Mime is the ONLY
//     event that calls it), and they come back only in [mapzoneexit,0_31_74] -> ~initalltabs.
//   - The solve path teleports the player home (~macro_return_teleport) and then, when the
//     reward roll unlocks an emote, calls ~unlock_mime_emote -> ~mesbox, which is if_openchat
//     plus p_pausebutton: a "Click here to continue" that waits for the player.
//   - The engine puts mapzoneexit on the ENGINE queue, which only runs while the player
//     canAccess() -- no MAIN or CHAT modal open. So ~initalltabs waits for the mesbox to close,
//     and the backpack and worn tabs stay null until something presses it.
//   - A script that refuses to decide while those panels are unreadable (woodcutterBot's
//     snapshot does, correctly: read blind, a wielding account looks axeless) and that only
//     continues dialogues AFTER that refusal therefore waits for the tabs, while the tabs wait
//     for the dialogue. One RESUME_PAUSEBUTTON restored them in 0.2s in the lab.
//
// Nothing in the Guardian ever looked at that dialogue: "cleared" means only that Detect no
// longer finds the event, which is true the moment the account leaves the mime's mapsquare.
// Worse, the return teleport usually lands BETWEEN two handling attempts, so no "cleared" line
// is written at all (the lab's first run showed exactly that) and the Guardian simply goes
// idle. So the exit is tracked on its own: every trapped handling notes it, and the first poll
// that finds the account outside the trap squares presses whatever continue is open, once per
// page, under the ordinary action lock.
//
// Scripts keep their own dialogue-continue branches; this is the event's owner finishing its
// own event, not a replacement for them.

// LabSkipTrappedReward, when set, leaves every trapped event's reward dialogue for something else
// to find. LAB HARNESS ONLY: cmd/stallwatchtest sets it to reproduce the pre-fix deadlock on
// purpose and prove the layers behind this one (the woodcutter family's blind gate, the fleet
// stall watchdog). Nothing in the fleet sets it. Package-level rather than a Guardian field
// because fleet.Manager builds each account's Guardian itself.
var LabSkipTrappedReward atomic.Bool

// trapRewardWait is how long the exit waits for a reward dialogue to appear, and for the next
// page after a press. The mesbox is opened by the same script execution that teleports the
// player, so it normally arrives in the same tick as the new position; four ticks is slack for
// a slow flush, and an item reward (no dialogue at all) costs exactly this much once.
const trapRewardWait = 2400 * time.Millisecond

// trapRewardMaxPages bounds the presses for one exit. The Mime's reward is one page.
const trapRewardMaxPages = 3

// trapExitExpiry drops a noted exit the account never made -- a Guardian that last handled the
// mime ten minutes ago has no business pressing a dialogue it finds now.
const trapExitExpiry = 10 * time.Minute

// trapExit is the Guardian's memory of a trapped event it has handled and whose exit it has not
// yet finished. Its own mutex, so it adds nothing to the Guardian's already-long g.mu sections.
type trapExit struct {
	mu   sync.Mutex
	name string
	at   time.Time
}

func (t *trapExit) note(name string, now time.Time) {
	t.mu.Lock()
	t.name, t.at = name, now
	t.mu.Unlock()
}

func (t *trapExit) clear() {
	t.mu.Lock()
	t.name, t.at = "", time.Time{}
	t.mu.Unlock()
}

// pending returns the noted event's name, or "" when there is none or it has expired.
func (t *trapExit) pending(now time.Time) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.name == "" {
		return ""
	}
	if now.Sub(t.at) > trapExitExpiry {
		t.name, t.at = "", time.Time{}
		return ""
	}
	return t.name
}

// trapExitDue reports whether a trapped event this Guardian handled has let the account go and
// its exit has not been finished yet: noted, not expired, and the account's tile known and
// outside both trap mapsquares. Pure over the state; safe from any goroutine.
func (g *Guardian) trapExitDue() (string, bool) {
	name := g.trapped.pending(time.Now())
	if name == "" {
		return "", false
	}
	x, z, _, ok := g.session.State().Tile()
	if !ok || InTrapEventSquare(x, z) {
		return "", false
	}
	return name, true
}

// finishTrapExitLocked presses the reward dialogue a trapped event left open. The caller holds
// actionMu (kick's own handling lock, or runTrapExit's TryLock).
func (g *Guardian) finishTrapExitLocked(name string) {
	g.trapped.clear()
	if LabSkipTrappedReward.Load() {
		g.log(fmt.Sprintf("random event: %s — LAB: leaving the reward dialogue alone (LabSkipTrappedReward)", name))
		return
	}
	components := currentInterfaceArchive()
	if components == nil {
		return
	}
	dlg := actions.NewChatDialog(g.session, components)
	st := g.session.State()
	deadline := time.Now().Add(trapRewardWait)
	for pressed := 0; pressed < trapRewardMaxPages; {
		if dlg.CanContinue() {
			text := strings.Join(dlg.Texts(), " ")
			if err := dlg.Continue(); err != nil {
				g.log(fmt.Sprintf("random event: %s — could not press continue on the reward dialogue %q: %v", name, text, err))
				return
			}
			pressed++
			// Report what the press did to the tabs it was for, so a failure to restore them is
			// visible in the same line rather than inferred from a silence afterwards.
			tabs := "still null"
			if g.waitOrStop(trapRewardWait, func() bool { return st.SideIcon(3) != -1 && st.SideIcon(4) != -1 }) {
				tabs = "back"
			}
			g.log(fmt.Sprintf("random event: %s — pressed continue on the reward dialogue %q (an open dialogue holds back the server's tab restore); backpack/worn tabs %s: %d/%d",
				name, text, tabs, st.SideIcon(3), st.SideIcon(4)))
			deadline = time.Now().Add(trapRewardWait)
			continue
		}
		if time.Now().After(deadline) || (pressed > 0 && !dlg.IsOpen()) {
			return
		}
		if !g.waitOrStop(200*time.Millisecond, nil) {
			return
		}
	}
}

// runTrapExit is the poll-loop entry for an exit kick() found due between two handling
// attempts. Non-blocking on the action lock, exactly like kick's own dispatch: if a script holds
// it, the next tick tries again.
func (g *Guardian) runTrapExit(name string) {
	if !g.actionMu.TryLock() {
		return
	}
	g.inFlight.Store(true)
	defer func() { g.inFlight.Store(false); g.actionMu.Unlock() }()
	// The mime's reward arrives with this dialogue, so it counts as event activity for any window
	// measuring the pack (eventmark.go).
	endEvent := g.BeginEvent()
	defer endEvent()
	g.finishTrapExitLocked(name)
}

// waitOrStop polls cond (nil = just wait) every 100ms for up to d, returning early (false) if
// the Guardian is stopped. With a cond it returns whether cond became true.
func (g *Guardian) waitOrStop(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if cond != nil && cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return cond == nil
		}
		select {
		case <-g.stop:
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}
