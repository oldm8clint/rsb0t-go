package bot

import (
	"testing"
	"time"
)

// THE HAND-OFF OF AN ACCOUNT NO REGISTRY HOLDS (latent on 8c77500e, live from 13 October 2026 when the whole
// cohort moved to the magic target with homeWorld 1, 2 and 3). A magic account lives on the world its homeWorld
// setting names. A hand-off is a trade, the mule is on world 104, so donorrunner.Run hops the account there and
// the fleet restarts the script; the script that starts there must stay and resume the hand-off. Before the trip
// was kept for every account, only a dealt yew account had one: a magic account on world 2 or 3 hopped home
// before the trade, drew its notes again, and looped until the fleet's hop limit.
func TestAMagicAccountOnItsSettingsWorldHandsOffThroughTheMulesWorld(t *testing.T) {
	for _, c := range []struct {
		name           string
		yewSpreadWorld bool // the live config: the yew deal is on, and the account is on the magic rung and so not dealt
	}{{"the yew deal is on (the live config)", true}, {"the yew deal is off", false}} {
		t.Run(c.name, func(t *testing.T) {
			tag := yewTestTag()
			build := func(world int) *worldRig {
				r := newWorldRig(t, "MagTrip_"+tag, world, "105,106", 105, "mule")
				r.b.cfg.Wood.Target = "Magic"
				r.b.cfg.SpreadYewWorlds = c.yewSpreadWorld
				return r
			}
			r := build(105)
			r.start()
			if r.b.yewW.assigned != 0 || r.b.handoffPending {
				t.Fatalf("a magic account holds no yew world and has no hand-off pending: %+v", r.b.yewW)
			}
			// The hand-off falls due on 105; Run hops it to the mule on 104 and returns "hopping".
			r.b.tripBegin(r.ctx)
			r.b.tripEnd(r.ctx, true, false, false)

			// Control: with no trip the restarted script would hop straight back home.
			ctl := newWorldRig(t, "MagCtl_"+tag, 104, "105,106", 105, "mule")
			ctl.b.cfg.Wood.Target = "Magic"
			ctl.start()
			if !ctl.b.goHome(ctl.ctx, time.Now()) || ctl.lastHop() != 105 {
				t.Fatalf("control: an account away from its home with no trip should hop home: hops %v", ctl.f.hops)
			}

			// The script on the mule's world.
			r = build(104)
			r.start()
			if !r.b.handoffPending || !r.said("resuming the hand-off to") {
				t.Fatalf("the restarted script did not resume the hand-off: pending %v, %v", r.b.handoffPending, *r.lines)
			}
			for i := 0; i < 3; i++ {
				if !r.b.yewWorldStep(r.ctx, r.d, time.Now()) || r.b.goHome(r.ctx, time.Now()) || len(r.f.hops) != 0 {
					t.Fatalf("pass %d: hopped home before the trade: %v", i, r.f.hops)
				}
			}
			if !r.said("on world 104 for a hand-off to the mule (home world 105) - staying until it is done") {
				t.Errorf("the hold should be said: %v", *r.lines)
			}
			// Traded: donorrunner hops home itself; were it refused, the trip is over and the next pass goes home.
			r.b.tripEnd(r.ctx, false, true, false)
			if !r.b.goHome(r.ctx, time.Now()) || r.lastHop() != 105 {
				t.Errorf("after the trade the account should go home to 105: hops %v", r.f.hops)
			}
		})
	}
}

// A trip that never ends (the mule gone, the script killed) lapses after TripTTL, and the account goes home.
func TestATripThatNeverEndsLapses(t *testing.T) {
	tag := yewTestTag()
	r := newWorldRig(t, "MagLapse_"+tag, 104, "105,106", 105, "mule")
	r.b.cfg.Wood.Target = "Magic"
	r.start()
	r.b.tripBegin(r.ctx)
	if r.b.goHome(r.ctx, time.Now()) || len(r.f.hops) != 0 {
		t.Fatalf("hopped during a trip: %v", r.f.hops)
	}
	if !r.b.goHome(r.ctx, time.Now().Add(26*time.Minute)) || r.lastHop() != 105 {
		t.Errorf("a trip past its TTL should let the account go home: hops %v", r.f.hops)
	}
}
