package bot

import (
	"errors"
	"fmt"
	"time"
)

// A skilling script's home world: the "homeWorld" setting, read the way ScarabSwarm reads it.
//
// WHY A SCRIPT HAS TO SAY IT, and not only the control panel. fleet.Manager.StartWorld logs an
// account in on its script's homeWorld at a PROCESS start, but an account switched to a new script
// at runtime stays on whatever world it is on, and its fleet home world is whatever the previous
// script made it: ScarabSwarm calls MakeHomeWorld(2) or (3) for most of the cohort. A fisher
// switched from ScarabSwarm with homeWorld 1 therefore kept home world 2, and every hand-off to
// the mule on world 1 was a hop there and a hop "home" to 2 afterwards (donorrunner.hopHome).
//
// So: at OnStart the script makes homeWorld the fleet's home for this account, and while the
// account is anywhere else the script asks to hop there once every homeWorldRetry. A hop is a
// logout and a login, so the script stops and is started again on the other world; the fleet
// bounds the hops themselves (worldhopplan.DefaultLimits: four in thirty minutes, a minimum gap),
// which is what stops a refused or failed hop from becoming a loop.
//
// homeWorld 0 or missing does nothing at all, so every account that does not set it behaves
// exactly as before.

// homeWorldRetry spaces this script's own hop requests. A hop the fleet accepted ends this run,
// so the wait only matters when a hop was refused or did not land.
const homeWorldRetry = 10 * time.Minute

// homeWorldCalmWait is the longest an ADOPTED home (adopt) defers its hop to a login path that is busy
// with a restart ramp, before hopping anyway.
const homeWorldCalmWait = 20 * time.Minute

// maxHomeWorld is the setting's upper bound, the one RuneRunner's schema states.
const maxHomeWorld = 255

// homeWorldSetting reads "homeWorld": 0 when missing, unreadable or out of 1-255.
func homeWorldSetting(settings map[string]interface{}) int {
	w := IntSetting(settings, "homeWorld", 0)
	if w < 1 || w > maxHomeWorld {
		return 0
	}
	return w
}

// homeWorld is one run's home-world state.
type homeWorld struct {
	home    int
	askedAt time.Time

	// The next three are the home a script CHOOSES for the account (adopt) rather than reads from its
	// settings: ProgressiveWCFletch's yew cohort is dealt over the fleet's worlds at run time
	// (wcfletch_world.go). fleetHome is what the fleet was last told the account's home is (0: the
	// settings' one stands). calm makes the hop wait for a quiet login path, so a restart ramp of
	// 400 accounts does not take 23 more logins on top of its own; busySince is when the wait began.
	fleetHome int
	calm      bool
	busySince time.Time
}

// start reads the setting and tells the fleet. A home the fleet refuses (a world this deployment
// does not list) is dropped with a log line rather than chased.
func (h *homeWorld) start(ctx *Context, who string) {
	*h = homeWorld{home: homeWorldSetting(ctx.Settings)}
	if h.home == 0 {
		return
	}
	if err := ctx.MakeHomeWorld(h.home); err != nil && !errors.Is(err, ErrNoWorlds) {
		ctx.logf("%s: the fleet refused home world %d (%v) — staying where the fleet puts this account", who, h.home, err)
		h.home = 0
		return
	}
	ctx.logf("%s: home world %d (this account is on world %d)", who, h.home, ctx.World())
}

// step asks to go home when the account is on another world. True means a hop was accepted or is
// already running: the caller returns from Loop and does nothing else, because the session is
// about to close.
func (h *homeWorld) step(ctx *Context, who string, now time.Time) bool {
	if h.home == 0 || ctx.World() == h.home {
		return false
	}
	if hopping, known := ctx.WorldHopInProgress(); known && hopping {
		return true
	}
	if !h.askedAt.IsZero() && now.Sub(h.askedAt) < homeWorldRetry {
		return false
	}
	if h.calm {
		if !ctx.FleetLoginBusy() {
			h.busySince = time.Time{}
		} else {
			if h.busySince.IsZero() {
				h.busySince = now
			}
			if now.Sub(h.busySince) < homeWorldCalmWait {
				return false
			}
		}
	}
	h.askedAt = now
	if err := ctx.RequestWorldHop(h.home, fmt.Sprintf("%s: going home to world %d", who, h.home)); err != nil {
		if !errors.Is(err, ErrNoWorlds) {
			ctx.logf("%s: on world %d, home is world %d, and the hop was refused (%v) — working here, asking again in %s",
				who, ctx.World(), h.home, err, homeWorldRetry)
		}
		return false
	}
	return true
}

// adopt makes stay the world the script keeps the account on from now and fleetHome the home the fleet
// holds for it (where a hand-off's hop home goes: donorrunner reads Context.HomeWorld, and the fleet's own
// bookkeeping the same value). They differ only during a hand-off trip, when the account stands on the
// mule's world and must not be sent away from it before the trade. Calling it again with the same values
// costs nothing and asks the fleet nothing.
//
// A home the fleet refuses is returned as the error and nothing changes, so the account stays where the
// settings put it. ErrNoWorlds (a Context with no fleet behind it) is an error too: there is nothing to
// hop with.
func (h *homeWorld) adopt(ctx *Context, stay, fleetHome int) error {
	if fleetHome > 0 && fleetHome != h.fleetHome {
		if err := ctx.MakeHomeWorld(fleetHome); err != nil {
			return err
		}
		ctx.HomeWorld = fleetHome
		h.fleetHome = fleetHome
	}
	if stay > 0 && stay != h.home {
		h.home = stay
		h.askedAt, h.busySince = time.Time{}, time.Time{}
	}
	h.calm = true
	return nil
}

// release gives back an adopted home: the settings' homeWorld (or none) is the home again, and the fleet is
// told so when it was told another.
func (h *homeWorld) release(ctx *Context) {
	if h.fleetHome == 0 && !h.calm {
		return
	}
	h.home = homeWorldSetting(ctx.Settings)
	h.askedAt, h.busySince, h.calm = time.Time{}, time.Time{}, false
	if h.fleetHome != 0 && h.home > 0 {
		_ = ctx.MakeHomeWorld(h.home)
		ctx.HomeWorld = h.home
	}
	h.fleetHome = 0
}
