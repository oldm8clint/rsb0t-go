package webwalk

import "fmt"

// HostileClient is optional on a Client. When the Client implements it AND the walk sets
// WalkOptions.AvoidHostiles, every poll of a walk asks it whether a permanently-spawned hostile
// NPC is about to engage, and lets it walk the account clear before the first hit lands. A
// Client that does not implement it, or a walk that does not ask, behaves exactly as before.
//
// Why this is not folded into CombatClient. CombatClient is reactive: it watches for a hitmark
// and can run or flee a fight already in progress. That is too late for the NPCs this covers. A
// Giant bat or a White wolf has a hunt range of 2 tiles and a fresh account is combat level 3,
// so by the time a hitmark exists the account is in a fight it cannot win, and its chase is
// already under way. This fires before the fight starts, on the fact that the hostile is close,
// not on the fact that it has hit.
//
// The live gap this closes, found 2026-09-18: the same pre-emptive break-off already ran on
// internal/scripts/freshstartrunner's own walkNearCore poll, but once an account is around
// Catherby, fisherrunner.WalkNear routes through this Walker instead, which bypassed that hook
// entirely. So the White wolves north of the Catherby fishing camp had no pre-emptive cover at
// all — only Guardian's reactive post-hit flee.
//
// internal/webwalk/live implements this over internal/hostileevade; this package keeps no
// knowledge of npc catalogs or collision packs, exactly as it keeps none of combat or modals.
type HostileClient interface {
	// BreakOffFromHostiles walks the account clear of a static-spawn hostile that is about to
	// engage. It returns whether it actually moved the account — a true return makes the walk
	// plan again from the new tile — and a short phrase naming what it broke off from.
	BreakOffFromHostiles() (moved bool, why string)
}

// HostileBlocker is the optional other half of HostileClient: a client that can say it has
// GIVEN UP getting the account past a hostile, so the walk can route around it instead.
//
// Separate interface so every existing HostileClient keeps working untouched. A client that
// does not implement it behaves exactly as before.
type HostileBlocker interface {
	// BlockedByHostiles returns and CLEARS the hostiles the client has given up on since the
	// last call, as the tiles they were standing on. Draining rather than accumulating keeps
	// the walker from re-adding the same square every poll.
	BlockedByHostiles() []Point
}

// hostileBlocked folds anything the client has given up on into the walk's own flee squares,
// which activeZones already routes around.
//
// This is the third outcome internal/hostileevade could not reach on its own. Its budget
// branch can only push through or keep breaking off, and for a combat-3 account against a
// level-27 Giant bat neither ends -- measured live 2026-09-22, six accounts, 167 break-offs
// against 163 resumes, all at 0 xp, because every re-plan led back through the same bats.
//
// It reuses avoidAttackers' own rules deliberately rather than inventing new ones: the same
// FleeRadius, the same refusal to write off a square containing the destination, and the same
// maxFlees ceiling, so a walk cannot fence itself in by giving up on hostile after hostile.
func (w *Walker) hostileBlocked(dest Point) (bool, string) {
	hb, ok := w.Client.(HostileBlocker)
	if !ok || w.walk.flees >= maxFlees {
		return false, ""
	}
	blocked := hb.BlockedByHostiles()
	if len(blocked) == 0 {
		return false, ""
	}
	r := w.walk.combat.FleeRadius
	if r <= 0 {
		r = defaultFleeRadius
	}
	var added []Point
	for _, p := range blocked {
		if InZones(p, w.walk.flee) {
			continue
		}
		sq := Rect{MinX: p.X - r, MaxX: p.X + r, MinZ: p.Z - r, MaxZ: p.Z + r, Level: p.Level}
		if sq.Contains(dest) {
			// Routing around a hostile standing on the destination is not possible, and
			// pretending otherwise would make the walk unreachable instead of merely hard.
			continue
		}
		w.walk.flee = append(w.walk.flee, sq)
		added = append(added, p)
	}
	if len(added) == 0 {
		return false, ""
	}
	w.walk.flees++
	return true, fmt.Sprintf("gave up getting past %d hostile(s) at %v, planning around them (flee %d/%d)",
		len(added), added, w.walk.flees, maxFlees)
}

func (w *Walker) hostileClient() HostileClient {
	hc, _ := w.Client.(HostileClient)
	return hc
}

// hostileTick gives the client one chance to break off, once per walk poll. It is a no-op unless
// the walk opted in and the Client implements HostileClient.
func (w *Walker) hostileTick() (bool, string) {
	if !w.walk.avoidHostiles {
		return false, ""
	}
	hc := w.hostileClient()
	if hc == nil {
		return false, ""
	}
	return hc.BreakOffFromHostiles()
}
