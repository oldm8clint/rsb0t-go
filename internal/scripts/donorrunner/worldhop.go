package donorrunner

import (
	"fmt"
)

// World hopping for the mule hand-off.
//
// WHY IT IS HERE AND NOT IN ONE SCRIPT. The fleet owner's requirement is "all scripts must
// be able to world hop and determine what world the mule is on". This package is already the
// script-agnostic donor half that every skilling script drives — its own package doc says
// so — so a hop wired in here is inherited by every one of them at once, exactly the way the
// hand-off itself already is. Wiring it into one bot.go would have given it to one script and
// left twenty-one others to copy it.
//
// WHAT IT DOES. Before a hand-off, if the mule is on another world, ask the fleet to move
// this account there and STOP this run — the session is about to close. After a hand-off,
// if this account is not on its own home world, ask to go back and stop again. The account's
// script is restarted by the fleet on the far side of each hop; see
// internal/fleet/worldhop.go.
//
// WHAT IT DELIBERATELY DOES NOT DO: decide. Whether a hop is allowed at all, how many are
// too many, what to do when one fails, and what "the mule's world is unknown" means are all
// internal/scripts/worldhopplan's, reached through internal/bot.Context. This file is three
// nil checks and two calls.
//
// EVERY FIELD IS OPTIONAL. A Deps with no hop hooks — every cmd/ tool, every existing test,
// and any fleet on one world — takes exactly the path it took before this file existed. That
// is asserted by a test, because "additive" is a claim and not a fact.

// hopHooks is the world-hopping half of Deps, kept in its own struct so the addition costs
// runner.go one embedded line.
type hopHooks struct {
	// MyWorld is the world this donor is logged in to. 0 disables hopping entirely.
	MyWorld int
	// HomeWorld is the world this donor belongs on, so a hand-off on another world can be
	// followed by a hop back. 0 means "the same as MyWorld", i.e. never hop home.
	HomeWorld int
	// MuleWorldOf answers which world the named mule is on. known false means nobody could
	// say, which must NOT be read as "the same world as me" — see worldhopplan.MuleWorld.
	// Nil disables hopping.
	MuleWorldOf func(mule string) (world int, known bool)
	// RequestHop asks the fleet to move this account. A nil error means the hop was
	// ACCEPTED, not that it succeeded. Nil disables hopping.
	RequestHop func(world int, reason string) error
}

// hopEnabled reports whether this Deps was given everything a hop needs.
func (d *Deps) hopEnabled() bool {
	return d.MyWorld > 0 && d.MuleWorldOf != nil && d.RequestHop != nil
}

// hopToMule asks to move to the mule's world when it is not this one. hopping is true when a
// hop was accepted and Run must stop; reason says why, for the Result.
//
// The three ways this answers "no" are all deliberate and all mean "carry on exactly as
// before": hopping is not wired up, nobody knows where the mule is, or the mule is already
// here. Only the last of those is the common case on a one-world fleet, and it costs a map
// lookup.
func (d *Deps) hopToMule(mule string) (hopping bool, reason string) {
	if !d.hopEnabled() || mule == "" {
		return false, ""
	}
	world, known := d.MuleWorldOf(mule)
	if !known {
		// Not an error and not worth a log line every hand-off: on a single-world fleet
		// nothing publishes a world, and "unknown" is then the permanent, correct answer.
		return false, ""
	}
	if world == d.MyWorld {
		return false, ""
	}
	why := fmt.Sprintf("the mule %s is on world %d and this account is on world %d", mule, world, d.MyWorld)
	if err := d.RequestHop(world, why); err != nil {
		// A refused hop is not a failed hand-off. The likeliest refusal is a deployment
		// with only one world (internal/fleet.ErrOneWorld), where the donor should simply
		// get on with the trade it was already going to attempt.
		d.logf("world hop to the mule declined (%v) — handing off on world %d as usual", err, d.MyWorld)
		return false, ""
	}
	d.logf("hopping to world %d: %s", world, why)
	return true, fmt.Sprintf("hopping to world %d to reach %s", world, mule)
}

// hopHome asks to move back to this account's own world after a hand-off on another one.
// Same contract as hopToMule.
func (d *Deps) hopHome() (hopping bool, reason string) {
	if !d.hopEnabled() || d.HomeWorld <= 0 || d.HomeWorld == d.MyWorld {
		return false, ""
	}
	why := fmt.Sprintf("the hand-off is done; returning to world %d", d.HomeWorld)
	if err := d.RequestHop(d.HomeWorld, why); err != nil {
		d.logf("world hop home declined (%v) — staying on world %d", err, d.MyWorld)
		return false, ""
	}
	d.logf("%s", why)
	return true, fmt.Sprintf("hopping home to world %d", d.HomeWorld)
}

// SetWorlds wires the hop hooks. Called by internal/bot.WireDonorWorlds, which is the one
// place that knows how to get these four values out of a bot.Context; exported because this
// package cannot import that one.
//
// myWorld 0, a nil muleWorldOf or a nil requestHop each leave hopping disabled, which is the
// state every caller that has not been wired up is in.
func (d *Deps) SetWorlds(myWorld, homeWorld int, muleWorldOf func(string) (int, bool), requestHop func(int, string) error) {
	d.MyWorld, d.HomeWorld = myWorld, homeWorld
	d.MuleWorldOf, d.RequestHop = muleWorldOf, requestHop
}
