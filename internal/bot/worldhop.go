package bot

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/worldhopplan"
	"rsb0t-go/internal/worlds"
)

// World hopping, from a script's point of view.
//
// This is the shared primitive users asked for: "all scripts must be able to world
// hop and determine what world the mule is on". It is on Context rather than in one script's
// package for the same reason RequestHandoff and RequestFinish are — every registered script
// gets it by construction, and there are 22 of them.
//
// THE ONE THING A SCRIPT AUTHOR MUST KNOW: a hop is a LOGOUT AND A LOGIN, so THIS SCRIPT
// STOPS. internal/fleet stops the running bot when the session goes and starts a brand-new
// one after the login — a fresh Context, a fresh Runner, nothing carried over but the
// settings map and whatever internal/settings persisted. So the shape of a hopping script is:
//
//	if w, ok := ctx.MuleWorld(muleName); ok && w != ctx.World() {
//	        if ctx.RequestWorldHop(w, "handing off to the mule") == nil {
//	                return // this run is over; the script restarts on the other world
//	        }
//	}
//
// and NOT "hop, then keep going" — there is nothing to keep going with.
//
// Everything here is nil-safe. A Context built by a cmd/ tool or a test has no fleet behind
// it, so World() reports worlds.Default, MuleWorld reports not-known, and RequestWorldHop
// returns ErrNoWorlds. A script written against this API therefore runs unchanged in a
// standalone harness, which is the property that lets it be tested at all.

// ErrNoWorlds is returned by RequestWorldHop when this Context has no fleet behind it (a
// standalone tool, a test) or the deployment has only one world. It is a refusal, not a
// fault: a script should carry on doing whatever it would have done without hopping.
var ErrNoWorlds = fmt.Errorf("bot: world hopping is not available for this account")

// World is the game world this account is logged in to. worlds.Default outside a fleet.
func (c *Context) World() int {
	if c.CurrentWorld <= 0 {
		return worlds.Default
	}
	return c.CurrentWorld
}

// HomeWorldNumber is the world this account's roster entry gave it — where a hop returns it
// to. Falls back to World() when nothing set it.
//
// Named HomeWorldNumber rather than HomeWorld because the field is already called that; the
// field is what internal/fleet fills in and this is the reader every script should use.
func (c *Context) HomeWorldNumber() int {
	if c.HomeWorld > 0 {
		return c.HomeWorld
	}
	return c.World()
}

// MuleWorld answers "what world is the mule on", which the owner named as a requirement in
// its own right. Two sources, in this order:
//
//  1. The fleet's own directory (Context.WorldOf). Exact for any account in THIS process,
//     which is every account the fleet runs, and it cannot be stale.
//  2. The bus (internal/mulebus): a mule publishes its world on its own heartbeat, and
//     because the bus is in-process and world-agnostic a donor on world 2 hears a mule on
//     world 1 perfectly well. This is the source that still works if the mule is ever run
//     from a second process.
//
// known is false when neither source could say. A caller must treat that as "do not hop"
// rather than as "world 1" — see worldhopplan.MuleWorld's own doc comment for why that
// distinction is the whole game.
func (c *Context) MuleWorld(mule string) (world int, known bool) {
	if mule == "" {
		return 0, false
	}
	if c.WorldOf != nil {
		if w := c.WorldOf(mule); w > 0 {
			return w, true
		}
	}
	if c.Bus == nil {
		return 0, false
	}
	now := time.Now()
	return worldhopplan.MuleWorld(mule, c.busPresence(now), now, worldhopplan.DefaultLimits.MuleWorldTTL)
}

// busPresence turns the bus's recent heartbeats into the plain values worldhopplan works
// with. Reading the bus here rather than importing its types into the plan package is what
// keeps that package free of every dependency but time.
//
// Every returned Presence is stamped `now` rather than with the message's own publish time,
// because mulebus.Bus.Since returns Messages without their timestamps. That is not a fudge:
// Since is asked only for the TTL window, so everything it returns is fresh by construction,
// and the TTL check in worldhopplan.MuleWorld is then a second, redundant guard rather than
// the only one. The ordering worldhopplan.MuleWorld relies on ("newest wins") still holds,
// because Since returns oldest first and equal timestamps leave the LAST match standing.
func (c *Context) busPresence(now time.Time) []worldhopplan.Presence {
	if c.Bus == nil {
		return nil
	}
	msgs := c.Bus.Since(now.Add(-worldhopplan.DefaultLimits.MuleWorldTTL))
	out := make([]worldhopplan.Presence, 0, len(msgs))
	for _, msg := range msgs {
		if msg.World <= 0 || msg.From == "" {
			continue
		}
		out = append(out, worldhopplan.Presence{Name: msg.From, World: msg.World, At: now})
	}
	return out
}

// RequestWorldHop asks the fleet to move this account to world. It returns as soon as the
// hop is accepted, and the CALLER SHOULD THEN STOP: the session is about to close and this
// script is about to be replaced by a fresh one on the other world.
//
// A nil error means the hop was accepted, not that it succeeded — the account may still end
// up back where it started, which is exactly what worldhopplan's give-up rules are for.
func (c *Context) RequestWorldHop(world int, reason string) error {
	if c.HopWorld == nil {
		return ErrNoWorlds
	}
	if world <= 0 {
		return fmt.Errorf("bot: world %d is not a world", world)
	}
	if world == c.World() {
		return nil
	}
	c.logf("world hop requested: world %d (%s)", world, reason)
	return c.HopWorld(world, reason)
}

// RequestWorldHopHome sends this account back to its roster world.
func (c *Context) RequestWorldHopHome(reason string) error {
	return c.RequestWorldHop(c.HomeWorldNumber(), reason)
}

// MakeHomeWorld asks the fleet to treat world as this account's home. ErrNoWorlds outside a
// fleet (or on a fleet build without the hook); the caller then keeps its own notion of home.
func (c *Context) MakeHomeWorld(world int) error {
	if c.SetHomeWorld == nil {
		return ErrNoWorlds
	}
	return c.SetHomeWorld(world)
}

// WorldHopInProgress reports whether the fleet is hopping this account right now. known is
// false when this Context cannot tell (no fleet, or a fleet build without the hook), which a
// caller must not read as "no hop".
func (c *Context) WorldHopInProgress() (hopping, known bool) {
	if c.HopInProgress == nil {
		return false, false
	}
	return c.HopInProgress(), true
}

// FleetLoginBusy reports whether the fleet's login path is busy (a restart ramp): false when
// this Context cannot tell.
func (c *Context) FleetLoginBusy() bool {
	return c.LoginBusy != nil && c.LoginBusy()
}
