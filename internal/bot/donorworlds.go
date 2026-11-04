package bot

import (
	"time"

	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

// WireDonorWorlds gives a donor hand-off the ability to reach a mule on another game world.
//
// One call, at the site where a script builds its donorrunner.Deps:
//
//	rd := &donorrunner.Deps{ ...as before... }
//	WireDonorWorlds(ctx, rd)
//
// After it, donorrunner.Run hops to the mule's world before the hand-off and back to this
// account's own world afterwards, both bounded by internal/scripts/worldhopplan. Without it,
// Run behaves exactly as it did before worlds existed — so a script that has not been wired
// up is not broken, only single-world.
//
// It is a free function in this package rather than a method on Deps because donorrunner
// cannot import this one (this package imports donorrunner), and rather than four more
// fields on every Deps literal because there are seventeen of them.
//
// Safe on any Context: one built by a cmd/ tool or a test has no fleet behind it, so
// ctx.HopWorld is nil, hopEnabled() is false, and this call costs nothing.
func WireDonorWorlds(ctx *Context, d *donorrunner.Deps) {
	if ctx == nil || d == nil {
		return
	}
	d.SetWorlds(
		ctx.World(),
		ctx.HomeWorldNumber(),
		ctx.MuleWorld,
		func(world int, reason string) error { return ctx.RequestWorldHop(world, reason) },
	)
	if d.WalkFallback == nil {
		d.WalkFallback = donorWalkFallback(ctx)
	}
}

// donorWalkFallback is donorrunner.Deps.WalkFallback for ctx: a webwalk Walker with the crossings
// installed, built on first use, or nil when ctx has no pack, loc index or config.
func donorWalkFallback(ctx *Context) func(dest tile.Tile, radius int) bool {
	if ctx.Session == nil || ctx.Pack == nil || ctx.Locs == nil || ctx.Config == nil {
		return nil
	}
	var w *webwalk.Walker
	return func(dest tile.Tile, radius int) bool {
		if w == nil {
			nw, err := live.NewWalker(live.Deps{
				Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
				Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log, Run: true,
			})
			if err != nil {
				ctx.logf("donor: no door-aware walker: %v", err)
				return false
			}
			if ctx.Components != nil {
				hops.Install(nw, hops.New(hops.Deps{Client: nw.Client, Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Log: ctx.Log}))
			}
			w = nw
		}
		res := w.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{Radius: radius, Timeout: 90 * time.Second})
		return res.Outcome == webwalk.OutcomeArrived
	}
}
