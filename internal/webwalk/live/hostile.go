package live

import (
	"fmt"
	"time"

	"rsb0t-go/internal/hostileevade"
	"rsb0t-go/internal/scripts/hostileavoid"
	"rsb0t-go/internal/webwalk"
)

// Client implements webwalk.HostileClient, so a walk that opts in breaks off from giant bats and
// White wolves before they engage instead of only fleeing once one has already hit.
var _ webwalk.HostileClient = (*Client)(nil)

// HostileNoticeMargin is added to a hostile's own 2-tile hunt range to decide how close is close
// enough to break off. 6 gives an 8-tile ring, the same one internal/scripts/freshstartrunner
// settled on for giant bats.
//
// It was built at 10 (a 12-tile ring) on the reasoning that this walk loop polls once per game
// tick, 600ms, against walkNearCore's 200ms, so the margin had to cover more ground between two
// checks. The live A/B rejected that, and the reasoning was wrong in a way worth writing down:
// a wider ring does NOT buy warning here, because the account's position moves 10 to 31 tiles
// between two consecutive polls of this loop (measured; mostly about 20, on a 3x-compressed
// engine with running on). No ring that a route can still be planned through gets ahead of that.
// What the wider ring did buy was more break-offs from wolves that were never going to reach the
// account, and on the one A/B seed that put the account in real danger the 12-tile ring died
// where the 8-tile ring arrived. Wolves stand in packs, so every extra break-off is another
// chance to be walked into a second wolf.
//
// See the wolfevade entry in COLLAB.md for the runs. Note the honest limit on that evidence:
// only one of four valid seeds was dangerous enough to separate the arms.
var HostileNoticeMargin = 6

// HostileBreakOffMargin is added to a hostile's chase limit to decide when the account is clear.
// Unlike the notice margin this is a property of the npc, not of the poll rate, so it matches
// freshstartrunner's.
var HostileBreakOffMargin = 3

// HostileEvadeEnabled turns the webwalk-side break-off off wholesale, for the live A/B harness.
// Ordinary callers control it per walk with webwalk.WalkOptions.AvoidHostiles instead.
var HostileEvadeEnabled = true

// hostilePoll re-samples position and threat this often during a break-off. Faster than the
// 600ms walk poll that triggered it: once the account is actively running from something, the
// distance between it and the threat is changing every tick.
const hostilePoll = 200 * time.Millisecond

// HostileConfig is the tuning this client breaks off with.
func HostileConfig() hostileavoid.Config {
	return hostileavoid.Config{
		Enabled:        HostileEvadeEnabled,
		NoticeMargin:   HostileNoticeMargin,
		BreakOffMargin: HostileBreakOffMargin,
	}
}

// BreakOffFromHostiles walks clear of a static-spawn hostile that is about to engage.
//
// Composes with the script's own survival rather than duplicating it: Deps.Delay here is the
// bot's guarded delay (internal/bot/fisher.go builds this Client with b.guardedDelay), which
// returns false the moment that script's eat or bank-flee rules are due, and the break-off
// returns immediately so the script can act. internal/scripts/fisherplan keeps owning those
// rules — see internal/hostileevade's package doc comment.
func (c *Client) BreakOffFromHostiles() (bool, string) {
	if c.d.Pack == nil {
		return false, ""
	}
	return hostileevade.BreakOff(hostileevade.Deps{
		Session: c.d.Session,
		Pack:    c.d.Pack,
		Delay:   c.Delay,
		Log:     c.d.Log,
		Cfg:     HostileConfig(),
		Poll:    hostilePoll,
		Blocked: c.noteBlockedByHostile,
	})
}

// noteBlockedByHostile records a hostile internal/hostileevade has given up on getting the
// account past, for the walker to route around. See webwalk.HostileBlocker.
func (c *Client) noteBlockedByHostile(name string, x, z, level int) {
	c.blockedMu.Lock()
	defer c.blockedMu.Unlock()
	p := webwalk.Point{X: x, Z: z, Level: level}
	for _, have := range c.blocked {
		if have == p {
			return
		}
	}
	c.blocked = append(c.blocked, p)
	if c.d.Log != nil {
		c.d.Log(fmt.Sprintf("hostile: giving up on getting past the %s at %d,%d — the next route will go around it",
			name, x, z))
	}
}

// BlockedByHostiles implements webwalk.HostileBlocker: it returns and clears what has been
// given up on since the last call. Draining is what stops the walker re-adding the same square
// on every poll of the walk.
func (c *Client) BlockedByHostiles() []webwalk.Point {
	c.blockedMu.Lock()
	defer c.blockedMu.Unlock()
	out := c.blocked
	c.blocked = nil
	return out
}
