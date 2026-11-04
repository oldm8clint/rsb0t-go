package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// scarabMuleHeardWithin is how recent a mule heartbeat must be to count as "the mule is there":
// ten heartbeats' worth, the same TTL worldhopplan uses for a mule's world.
const scarabMuleHeardWithin = 30 * time.Second

// scarabMulePresence is what the bus says about the mule right now.
type scarabMulePresence struct {
	// Heard is a heartbeat from the mule inside scarabMuleHeardWithin.
	Heard bool
	// SameWorld is false only when both worlds are known and differ.
	SameWorld bool
	// Post is where to walk to meet it: the post its heartbeat carries (Home), else the fixed post,
	// with its live tile only when that is within scarabMulePostNear of the post
	// (scarabMulePostFrom).
	Post tile.Tile
	// Why is the one-line reason a caller logs when it does not walk.
	Why string
}

// scarabMulePresenceOf reads the bus for the mule's latest heartbeat.
//
// A Context with no bus (a test, a standalone tool) answers Heard=true with the fixed post: the
// check exists to stop wasted walks on the live fleet, not to change what a harness that wires
// no bus does.
func scarabMulePresenceOf(ctx *Context, mule string) scarabMulePresence {
	p := scarabMulePresence{Heard: true, SameWorld: true, Post: scarabSwarmMulePost}
	if ctx == nil || ctx.Bus == nil || mule == "" {
		return p
	}
	want := strings.ToLower(strings.TrimSpace(mule))
	if len(want) > mulebus.NameMax {
		want = want[:mulebus.NameMax]
	}
	var last *mulebus.Message
	for _, m := range ctx.Bus.Since(time.Now().Add(-scarabMuleHeardWithin)) {
		m := m
		if m.Kind == mulebus.KindMuleHere && strings.EqualFold(m.From, want) {
			last = &m
		}
	}
	if last == nil {
		p.Heard = false
		p.Why = "no heartbeat from " + mule + " on the bus in the last " + scarabMuleHeardWithin.String() + " (logged out, or not running MuleReceiver)"
		return p
	}
	if last.World > 0 && ctx.World() > 0 && last.World != ctx.World() {
		p.SameWorld = false
		p.Why = mule + " is on another world"
		return p
	}
	p.Post = scarabMulePostFrom(*last)
	return p
}

// scarabMulePostNear is how close the mule's LIVE tile must be to its known post to be walked to
// instead of the post itself.
const scarabMulePostNear = 3

// scarabMulePostFrom is where to walk to meet the mule, from its heartbeat: the post it carries
// (Home) when it does, else the fixed post -- with the mule's live tile preferred only when it is
// within scarabMulePostNear of that post.
//
// LIVE BUG, 2026-09-23 (the first of the world-1 cohort): this used the heartbeat's live tile
// whenever its state read "home". A posted mule reports StateHome all the time, including while it
// cooks at the Catherby range (2817,3443), so every Scarab ask during a cook round walked into the
// range house and waited there, while the mule walked back to its post to serve and timed out with
// "you never opened the trade": 9 failed and 3 delivered in 15 minutes (run/fleet.log; the
// delivered ones had planned to 2812,3441).
func scarabMulePostFrom(hb mulebus.Message) tile.Tile {
	if hb.Home != nil && hb.Home.X > 0 {
		return tile.Tile{X: hb.Home.X, Z: hb.Home.Z}
	}
	if hb.State == mulebus.StateHome && hb.Tile.X > 0 {
		live := tile.Tile{X: hb.Tile.X, Z: hb.Tile.Z}
		if tile.Chebyshev(live, scarabSwarmMulePost) <= scarabMulePostNear {
			return live
		}
	}
	return scarabSwarmMulePost
}

// scarabKitBundle is scarabrunner.RequestKitBundle, and scarabKitPerItem RequestKitFromMule:
// seams for the tests of what the order asks for.
var (
	scarabKitBundle  = fleetrunner.RequestKitBundle
	scarabKitPerItem = fleetrunner.RequestKitFromMule
)

// scarabMuleQueueTooLong reports whether the mule's queue -- the depth its own heartbeat publishes
// (donorrunner.MuleQueue): donors announced and on their way, queued kits and, since NB2, the
// per-item hand-out orders, all served ahead of this ask -- at the mule's own cycle is longer than
// this ask can wait. Unknown (no recent heartbeat with a queue) is not "too long".
func scarabMuleQueueTooLong(ctx *Context, mule string, timeout time.Duration) (string, bool) {
	queue, cycle, ok := donorrunner.MuleQueue(ctx.Bus, mule, time.Now())
	if !ok {
		return "", false
	}
	if _, long := fleetplan.MuleQueueWait(queue, cycle, timeout); !long {
		return "", false
	}
	return fmt.Sprintf("%s has %d donor(s), kit(s) and order(s) ahead at about %s each: longer than this ask's %s",
		mule, queue, cycle.Round(time.Second), timeout), true
}

// scarabSwarmMulePost is where the MuleReceiver posts (mule.go: fisherplan.Catherby().Bank).
var scarabSwarmMulePost = tile.Tile{X: 2809, Z: 3441}

// Tunables that are not settings.
const (
	// scarabWorldMuleHeardWithin is how recent a mule heartbeat must be to count as heard: the
	// same TTL worldhopplan uses for a mule's world, ten of the mule's three-second heartbeats.
	scarabWorldMuleHeardWithin = 30 * time.Second
	// scarabWorldQuietFor is how long out of combat the account must be before it logs out.
	scarabWorldQuietFor = 15 * time.Second
	// scarabWorldPostReach is how near the post counts as at it (the sites walk to radius 3).
	scarabWorldPostReach = 3
	// scarabWorldAskTimeout is one order's budget, the sites' own 90 seconds.
	scarabWorldAskTimeout = 90 * time.Second
	// scarabWorldAskPerOrder is an order's bound when a kit line names none.
	scarabWorldAskPerOrder = 1000
	// scarabWorldHoldHeartbeat is how often a world step that keeps the stage from running
	// (a wait, a hold) repeats its line: the fleet stall watchdog acts on ten silent minutes,
	// and the held-state heartbeat on lane P's branch (024a2e88) uses the same five.
	scarabWorldHoldHeartbeat = 5 * time.Minute
	// scarabWorldRepublish is how often a kit order still waiting in the mule's line is published
	// again. The mule keeps a re-published kit's place (internal/bot/mule_kit.go takeKit), so this
	// only matters when the mule's script restarted and its in-memory line went with it: the kit
	// then goes back in line instead of being waited for in vain.
	scarabWorldRepublish = 2 * time.Minute
)

// scarabWorldMuleHeard reports a heartbeat from mule on the bus inside the TTL, whatever world
// it came from: the bus is in-process and world-agnostic, which is what lets a world-2 account
// hear a world-1 mule at all.
func scarabWorldMuleHeard(ctx *Context, mule string) bool {
	if ctx == nil || ctx.Bus == nil || mule == "" {
		return false
	}
	for _, m := range ctx.Bus.Since(time.Now().Add(-scarabWorldMuleHeardWithin)) {
		if m.Kind == mulebus.KindMuleHere && scarabWorldSameName(m.From, mule) {
			return true
		}
	}
	return false
}

func scarabWorldSameName(a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " ")) }
	na, nb := norm(a), norm(b)
	if len(nb) > mulebus.NameMax {
		nb = nb[:mulebus.NameMax]
	}
	if len(na) > mulebus.NameMax {
		na = na[:mulebus.NameMax]
	}
	return na == nb
}
