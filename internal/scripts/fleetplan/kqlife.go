package fleetplan

// KQHitpointsPerCast is the measured pool one cast costs: 99 hitpoints bought about five casts in
// the arena. It is a crude constant from a small sample and is used only for sizing, never for a
// claim about a particular life.
const KQHitpointsPerCast = 20

// KQOneShotSafeHitpoints is the hitpoints level at which none of her attacks can kill outright,
// because all three roll randominc(KQMaxHit).
const KQOneShotSafeHitpoints = KQMaxHit + 1

// OneShotSafe reports whether a single attack of hers can no longer kill this account.
func OneShotSafe(hitpoints int) bool { return hitpoints >= KQOneShotSafeHitpoints }

// CastsPerLife estimates the casts one life gets from its hitpoints and the healing it carries.
//
// Below the one-shot threshold the food is mostly NOT EATEN — the account dies to a hit it could
// not survive rather than bleeding down to its eat point — so only the hitpoints count. At or above
// it the healing counts too, because the account lives long enough to use it.
//
// REFUTED LIVE, 2026-09-21, and kept only as the record of it. This function predicted that an
// account past the one-shot threshold carrying 360 healing would land about twenty casts. Six
// accounts at hitpoints 32 with eighteen sharks each, in an arena where she was the only thing
// doing damage, were ALL DEAD INSIDE FIFTEEN SECONDS having sent TWO casts between them and
// eaten seven sharks. The pool-plus-food term is wrong: eating is rate-limited and she is not,
// so food does not convert into casts the way hitpoints do.
//
// What survives is the no-food term and the threshold itself, both of which the runs support:
// about one cast per twenty hitpoints of pool with nothing to eat (99 hitpoints bought five
// casts), and a single hit stops being lethal at 32. Callers should treat anything this returns
// with a foodHeal above zero as an upper bound nobody has reached.
// This is an ESTIMATE from one measured constant and a content threshold. It is the right shape to
// plan with and the wrong thing to quote as a measurement of any particular configuration.
func CastsPerLife(hitpoints, foodHeal int) float64 {
	if hitpoints <= 0 {
		return 0
	}
	pool := hitpoints
	if OneShotSafe(hitpoints) && foodHeal > 0 {
		pool += foodHeal
	}
	return float64(pool) / float64(KQHitpointsPerCast)
}
