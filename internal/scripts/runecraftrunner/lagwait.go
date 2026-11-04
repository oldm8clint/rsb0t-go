package runecraftrunner

import (
	"time"

	"rsb0t-go/internal/scripts/fleetrunner"
)

// WAITING FOR A CLICK'S RESULT IN A VIEW THAT LAGS (gatecraftlag-1018, 18 Oct 2026).
//
// The waits in this package are the confirmation of a click: the talisman used (the account seen inside the temple), the altar
// clicked (the essence gone from the pack), the portal taken (the account seen outside). Each used to be a wall-clock window of
// three to six seconds, which is a window of five to ten game ticks only while the server keeps its pace. From 18 Oct 01:30 it did
// not: the same night the gate logged "25 essence still in the pack 6s after the click" a dozen times, "not seen inside the Nature
// temple within 6s" with the game's own teleport line in the chat, and "still inside the Nature temple 6s after 3 click(s) on the
// portal". At 05:30 three crafts in a row went unconfirmed (24 seconds), the round ended on them, the crafter walked a quarter of
// the map to the mule's post and the gate stood closed for 40 minutes. Every one of those was the view being late, not the click
// failing; and a click sent AGAIN because its answer was late is not harmless: the altar answers an empty pack with a message box
// (runecraft.rs2: ~mesbox "You do not have any rune stones to bind."), and the engine refuses every later loc click while a box is
// open, which is how one late craft became a portal that would not take the account out.
//
// So a confirmation is waited for in the view's own time:
//   - the seconds the view is lagging (gamestate.ViewLag: silent, or catching up on a backlog, the settle after a silence
//     included) are not charged to the wait, up to lagWaitMax;
//   - a wait that has used its budget is not over until the server has delivered the ticks the budget is worth (a server that
//     ticks every two seconds is not "silent", but five seconds of it is two ticks of game time);
//   - a view that went silent in the last shakyFor is shaky: its waits are shakyFactor times as long;
//   - a click is not sent again for being late while a wait has SEEN the view lag: the first may have landed;
//   - a failure that came out of such a view says so (Lag on the result), because it says nothing about the click.
//
// A healthy view pays nothing for any of it: a wait ends the moment its condition holds.

var (
	// lagWaitMax is the most one wait is stretched past its budget for a view that lags.
	lagWaitMax = 45 * time.Second
	// shakyFor is how long after a silence the view still counts as shaky; shakyFactor how much longer a shaky view's waits are.
	shakyFor    = 60 * time.Second
	shakyFactor = 3
)

// tickPace is the server's tick (a variable so a test can shorten it).
var tickPace = 600 * time.Millisecond

// Shaky reports whether the account's view of the game cannot be trusted to answer "did my click land" in the usual wait: it is
// lagging now (silent, or reading a backlog), or it was in the last shakyFor.
func Shaky(d *fleetrunner.Deps) bool {
	if d == nil || d.Session == nil {
		return false
	}
	l := d.Session.State().ViewLag()
	if l.Lagging {
		return true
	}
	return l.Silence > 0 && !l.SilenceEnded.IsZero() && time.Since(l.SilenceEnded) < shakyFor
}

// AwaitView waits until the view is not lagging (at most lagWaitMax) and reports whether it had to. A pack read before it says
// nothing about the pack.
func AwaitView(d *fleetrunner.Deps) (waited bool) {
	st := d.Session.State()
	deadline := time.Now().Add(lagWaitMax)
	for st.ViewLag().Lagging {
		waited = true
		if !time.Now().Before(deadline) || !d.Delay(poll) {
			return waited
		}
	}
	return waited
}

// seenNote is what waits saw of the view. lagged: lag was OBSERVED during a wait (the view silent or catching up, or the ticks
// slower than the server's pace), so a click whose result has not shown may still land. shaky: the view had been silent in the last
// shakyFor when a wait began, which stretched the wait but says nothing about this click.
type seenNote struct{ lagged, shaky bool }

// any is whether a failure out of these waits is the view's to answer for.
func (n seenNote) any() bool { return n.lagged || n.shaky }

func (n *seenNote) add(o seenNote) {
	n.lagged = n.lagged || o.lagged
	n.shaky = n.shaky || o.shaky
}

// waitSeen is waitUntil for the result of a click: budget is the wait on a view that keeps the server's pace, and the wait stretches
// as the file comment says. It reports whether cond held and what the view did meanwhile: a false with a note that is any() is a
// click whose result has not been SEEN, not one that is known not to have landed.
func waitSeen(d *fleetrunner.Deps, cond func() bool, budget time.Duration) (ok bool, note seenNote) {
	st := d.Session.State()
	if Shaky(d) {
		budget *= time.Duration(shakyFactor)
		note.shaky = true
	}
	// The ticks the budget is worth at the server's pace, less one for the jitter of a healthy view.
	need := max(1, int(budget/tickPace)-1)
	began := time.Now()
	tick0 := st.Tick()
	var spent time.Duration
	last := began
	for {
		if cond() {
			return true, note
		}
		now := time.Now()
		l := st.ViewLag()
		if l.Lagging {
			note.lagged = true
		} else {
			spent += now.Sub(last)
		}
		last = now
		if now.Sub(began) >= budget+lagWaitMax {
			return false, note
		}
		if spent >= budget && !l.Lagging {
			// A view that has seen no tick at all is not ticking (a session that is not driven), and waiting longer would only wait.
			seen := st.Tick() - tick0
			if seen == 0 || seen >= need {
				return false, note
			}
			note.lagged = true
		}
		if !d.Delay(poll) {
			return false, note
		}
	}
}
