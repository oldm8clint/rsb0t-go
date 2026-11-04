package randomevents

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
)

// The last resort of a flee with nowhere to walk: a teleport.
//
// LIVE DEATHS, 16-17 Oct 2026, the Asgarnian ice dungeon: a cornered account (an event monster beside it, an
// aggressive herd round it, the only exit a ladder 97 steps away) ate until its pack was empty and then
// stood in melee until it died. Three of them held a Law rune stack and a Magic level that pays for Varrock,
// Lumbridge or Falador, and the code that casts one for a stranded account (strand_exec.go) only ran in
// catalogued strand sites. A cornered account that is about to die and can pay for a teleport casts it.
//
// This is deliberately NOT first: a leash walk (event_leash.go) or the ordinary flee comes before it, and food
// before both. It runs only when a flee has already found nowhere to go (fleeCornered, corneredStandDown) and
// the account is hurt enough that the next hits may kill it.

const (
	// lastResortBelow is the share of maximum hitpoints under which a cornered account with food still in
	// its pack casts a teleport rather than eat: the bite heals about a quarter of a small pool and the
	// herd and a Shade hit for more than that between two bites.
	lastResortBelow = 0.30

	// lastResortNoFoodBelow is the share under which one with NO food casts it: nothing is left to heal with,
	// so the cast does not wait for the pool to run low.
	lastResortNoFoodBelow = 0.50

	// lastResortGap is how long a cast is not repeated: a teleport takes a few ticks to land, and a second
	// cast in that time would only burn runes.
	lastResortGap = 8 * time.Second
)

// lastResortWanted is the decision, pure: hurt enough that a teleport is better than staying. Unknown
// hitpoints never want one.
func lastResortWanted(hp, max int, known, hasFood bool) bool {
	if !known || max <= 0 || hp <= 0 {
		return false
	}
	frac := float64(hp) / float64(max)
	if hasFood {
		return frac < lastResortBelow
	}
	return frac < lastResortNoFoodBelow
}

// lastResortCasts maps actions.Session -> time.Time of the last cast, recentCombat's per-session shape.
var lastResortCasts sync.Map

// lastResortExit casts the teleport this account can pay for (strandSpell: Magic level, runes and staff
// counted from its pack and worn items) when lastResortWanted says so, and reports whether it did. why starts
// the log line. False for a session with no Guardian, an account that is not hurt enough, one that cannot
// pay for a teleport, or one that cast a moment ago.
func lastResortExit(s actions.Session, why string, log func(string)) bool {
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g == nil || s.State() == nil {
		return false
	}
	st := s.State()
	hp, okHP := st.HP()
	max, okMax := st.MaxHP()
	_, hasFood := g.strandFood()
	if !lastResortWanted(hp, max, okHP && okMax, hasFood) {
		return false
	}
	return g.teleportOut(why, lastResortGap, log)
}

// teleportOut leaves by the spell the account can pay for, else by a rub of the jewellery it carries (rubteleport.go). The decision
// to leave is the caller's. A spell or a rub inside gap of the last one is not repeated, whichever the last was.
func (g *Guardian) teleportOut(why string, gap time.Duration, log func(string)) bool {
	if v, ok := lastResortCasts.Load(g.session); ok && time.Since(v.(time.Time)) < gap {
		return false
	}
	return g.castPayableTeleport(why, gap, log) || rubTeleport(g.session, why, gap, log)
}

// castPayableTeleport casts the teleport strandSpell finds the account able to pay for and records the cast, unless one went out
// within gap. False for no spell, a refused send, or a cast inside the gap. The decision to leave is the caller's.
func (g *Guardian) castPayableTeleport(why string, gap time.Duration, log func(string)) bool {
	s := g.session
	st := s.State()
	if v, ok := lastResortCasts.Load(s); ok && time.Since(v.(time.Time)) < gap {
		return false
	}
	spell := g.strandSpell()
	if spell == "" {
		return false
	}
	if err := actions.Teleport(s, spell); err != nil {
		if log != nil {
			log(fmt.Sprintf("%s: casting the %s teleport failed: %v", why, spell, err))
		}
		return false
	}
	lastResortCasts.Store(s, time.Now())
	if log != nil {
		hp, _ := st.HP()
		max, _ := st.MaxHP()
		x, z, level, _ := st.Tile()
		log(fmt.Sprintf("%s: %d/%d hitpoints at %d,%d,%d (%s) — casting the %s teleport",
			why, hp, max, x, z, level, attackerDiag(st), spell))
	}
	return true
}

// LastResortTeleport is lastResortExit for a script that has found its account being hit with nothing left to eat:
// it casts the teleport the account can pay for when it is hurt enough to need one, and reports whether it did.
func LastResortTeleport(s actions.Session, why string, log func(string)) bool {
	return lastResortExit(s, why, log)
}

// PayableTeleport is the name of the teleport the session's account can pay for right now (Magic level, runes and staff), "" when it
// cannot, has no Guardian, or has no name resolver to read its pack with.
func PayableTeleport(s actions.Session) string {
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g == nil || s == nil || s.State() == nil {
		return ""
	}
	return g.strandSpell()
}

// EscapeTeleport casts the teleport the account can pay for, for a caller that has already judged that the account must leave and
// by how much (scarabrunner.LagEscape: hit by hitters it cannot out-eat while its bites land late). It differs from LastResortTeleport
// only in that: no hitpoint share is asked of the account here, and the cast is not repeated within gap (the caller's, which should
// cover how late a cast can land: a second one sent while the first is on its way only burns runes). It reports whether it cast.
func EscapeTeleport(s actions.Session, why string, gap time.Duration, log func(string)) bool {
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g == nil || s == nil || s.State() == nil {
		return false
	}
	if gap < lastResortGap {
		gap = lastResortGap
	}
	return g.teleportOut(why, gap, log)
}
