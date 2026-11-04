package freshstartrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/hostileevade"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/hostileavoid"
)

// Static-spawn hostile evasion for the travel legs this package drives.
//
// The problem this closes, in the account owner's own words: fresh accounts still die walking
// Ardougne to Catherby, which resets them to Lumbridge and costs the whole travel leg. Two
// killers were named — giant bats around 2755,3402 and White wolves near Catherby — and both are
// ordinary world mobs on permanent spawn tiles, which nothing in this codebase watched for.
// internal/randomevents only ever tracks "event monsters" (the antimacro NPCs that spawn as one
// player's own random event), and its Guardian only reacts once the account is ALREADY being hit.
//
// Why not a danger zone. A route-avoidance zone around the bats was tried first and rejected on
// evidence: cmd/dangerzonecheck against the real collision pack showed any radius of 2 or more
// makes the whole Catherby leg unreachable, because that area is a walled compound whose single
// gate the only walkable route funnels straight through. dangerZones above is deliberately empty
// for that reason. So this is reactive evasion instead: notice the hostile early, break off, then
// resume the route.
//
// Why breaking off actually works here, unlike for the Drunken Dwarf. An npc chases only to its
// own maxrange measured from its SPAWN tile, and neither of these species sets one, so the engine
// defaults it to wanderrange+2: 10 tiles for the giant bat, 5 for the White wolf. Both are short
// and finite. The Drunken Dwarf's is effectively unlimited, which is exactly why "flee farther"
// was not a fix there and is one here. All of those numbers, and the 2-tile hunt range that makes
// a small berth enough, live in internal/scripts/hostileavoid with the content files they came
// from cited.
//
// The escape loop itself lives in internal/hostileevade, shared with internal/webwalk/live. It
// was moved there when the same break-off had to run from webwalk's Walker as well: once an
// account is around Catherby, fisherrunner.WalkNear routes through that Walker and never reaches
// the walkNearCore poll below, which is why White wolves were never covered before.

// HostileEvadeEnabled turns static-spawn evasion off wholesale, on BOTH walk paths — this
// package's own walkNearCore poll and the webwalk walks walkWithWalker drives. On by default: a
// bot walking into a giant bat is a real, repeated, account-resetting death, and the response is
// bounded and self-limiting (see internal/hostileevade's budget).
var HostileEvadeEnabled = true

// hostileAvoidConfig is the tuning for THIS package's poll, which runs every 200ms. The notice
// margin has to cover the ground crossed between two checks, so the webwalk client uses a wider
// one for its slower poll — see internal/webwalk/live.HostileNoticeMargin.
func hostileAvoidConfig() hostileavoid.Config {
	cfg := hostileavoid.DefaultConfig()
	cfg.Enabled = HostileEvadeEnabled
	return cfg
}

// criticalHP reports whether the account is at or below the fisher's own flee threshold, reusing
// internal/scripts/fisherplan's survival rules rather than inventing a second set of numbers.
// Only used to say so in the log: the actual eating and bank-flee stay with the script that owns
// them, reached through d.Delay.
func criticalHP(d *Deps) (hp int, maxHP int, critical bool) {
	st := d.Session.State()
	hp, hpOK := st.HP()
	maxHP, maxOK := st.MaxHP()
	if !hpOK || !maxOK || maxHP <= 0 {
		return hp, maxHP, false
	}
	return hp, maxHP, hp <= fisherplan.DefaultSurvival().FleeAt(maxHP)
}

// EvadeStaticHostiles breaks off from a permanently-spawned hostile that is about to engage, and
// reports whether it did anything. Returning true means the account has moved and the caller
// should re-plan its route from wherever it now stands, which is exactly what walkNearCore's
// per-segment loop does with it.
func EvadeStaticHostiles(d *Deps) bool {
	if !HostileEvadeEnabled || d == nil || d.Session == nil || d.Pack == nil {
		return false
	}
	moved, _ := hostileevade.BreakOff(hostileevade.Deps{
		Session: d.Session,
		Pack:    d.Pack,
		Delay:   func(t time.Duration) bool { return d.Delay(t) },
		Log:     d.Log,
		Note: func() string {
			hp, maxHP, critical := criticalHP(d)
			if !critical {
				return ""
			}
			return fmt.Sprintf(", HP %d/%d is already critical", hp, maxHP)
		},
		Cfg: hostileAvoidConfig(),
	})
	return moved
}
