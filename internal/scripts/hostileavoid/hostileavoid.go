// Package hostileavoid decides when a permanently-spawned hostile NPC is about to engage the
// account and how far away counts as having broken off. It is the pure half: no session, no
// pathfinding, no I/O, so every rule here is unit-tested against the content's own numbers.
//
// Why this exists separately from internal/randomevents: that package's HandleEvade covers
// "event monsters", the antimacro NPCs that only ever spawn as one player's own random event
// (hostileEventNpcIDs — River troll, Swarm, Rock Golem, Zombie, Shade, Watchman, Tree spirit).
// The NPCs killing fresh accounts on the Ardougne->Catherby leg are not those: they are ordinary
// world mobs standing on their own static spawn tiles, which HandleEvade never looks at and
// Detect never raises an event for. The shape of the response is the same (notice early, break
// off, come back), so this mirrors handleEvade deliberately rather than inventing a new one.
//
// Every number in Catalog below was read out of the content and engine checkouts, not guessed:
//
//   - Giant bat is npc 78 and White wolf is npcs 96 (level 25) and 97 (level 38), confirmed
//     against the config archive this engine actually serves, not only against content/pack.
//   - huntrange comes straight from each npc block. Both are 2, which is the single most useful
//     fact here: these NPCs only notice a player within 2 tiles of themselves, so a berth of a
//     few tiles avoids the fight outright instead of having to win or outrun it.
//   - maxrange is how far from its OWN SPAWN TILE an NPC will chase before giving up
//     (engine/src/cache/config/NpcType.ts: maxrange defaults to wanderrange+2 when a block does
//     not set one, and neither of these blocks sets one). Giant bat wanders 8, so it chases to
//     10; White wolf wanders 3, so it chases to 5. Both are finite and short, which is why
//     breaking off works here — unlike the Drunken Dwarf, whose chase is effectively unlimited
//     and where walking farther is not a fix at all.
//   - MaxHit is the content's own npc melee max hit,
//     content/scripts/skill_combat/scripts/combat.rs2: combat_maxhit((strength+9)*(bonus+64))
//     = ((strength+9)*64 + 320)/640 with no strength bonus on either block. Giant bat strength
//     22 gives 3, White wolf 16 gives 3, White wolf 31 gives 4. These agree with the numbers
//     fisherplan/survival.go already derived independently for its eat margin.
//   - huntmode is "cowardly" on all three, which content/scripts/_unpack/225/all.hunt defines as
//     type=player with check_nottoostrong=outside_wilderness. engine/src/engine/entity/Npc.ts
//     applies that as "skip this player if combatLevel > vislevel*2 and they are outside the
//     wilderness" — see HuntsPlayer below. That is exactly why this only kills LOW-level
//     accounts: a fresh account is combat level 3 and is always hunted.
package hostileavoid

// Facts are one hostile NPC type's content-confirmed numbers. See the package doc comment for
// where each field was read from.
type Facts struct {
	Type int
	Name string
	// Level is the npc's vislevel, the level shown on its menu entry.
	Level int
	// HuntRange is how far from ITSELF the npc notices a player, straight off its npc block.
	HuntRange int
	// MaxRange is how far from its own SPAWN TILE it will chase before giving up.
	MaxRange int
	// MaxHit is the largest single melee hit it can land.
	MaxHit int
	// NoLevelCap marks an npc whose hunt mode ignores the account's combat level: huntmode aggressive_melee
	// (all.hunt) has check_nottoostrong=off, so it hunts a combat-126 account as readily as a combat-3 one. The
	// cowardly mode every other entry has skips a player stronger than twice its level (HuntsPlayer).
	NoLevelCap bool
}

// Catalog is every static-spawn hostile this package knows how to break off from. Keyed by npc
// type id. Deliberately small and explicit: an id that is not in here is left entirely alone,
// so adding a species is a considered act with content evidence behind it, never a wildcard
// that starts making a bot run away from cows.
var Catalog = map[int]Facts{
	78: {Type: 78, Name: "Giant bat", Level: 27, HuntRange: 2, MaxRange: 10, MaxHit: 3},
	96: {Type: 96, Name: "White wolf", Level: 25, HuntRange: 2, MaxRange: 5, MaxHit: 3},
	97: {Type: 97, Name: "White wolf", Level: 38, HuntRange: 2, MaxRange: 5, MaxHit: 4},
	// Draynor jail guard (content area_draynor/configs/draynor.npc [jailguard], spawns m48_50 at 3109,3237,
	// 3120,3238, 3121,3249 and 3127,3248): level 26, attack 19, strength 23 +5 (max hit 3), one attack every 5
	// ticks, no wanderrange (the engine's default 5) so maxrange 7, huntrange 5, huntmode aggressive_melee. A combat-63 account was killed
	// at 3107,3234 on 17 Oct 2026 (HP 55 to 0, no food) with nothing in the code reacting to it.
	917: {Type: 917, Name: "Jail guard", Level: 26, HuntRange: 5, MaxRange: 7, MaxHit: 3, NoLevelCap: true},
}

// Known reports the catalogued facts for an npc type.
func Known(npcType int) (Facts, bool) {
	f, ok := Catalog[npcType]
	return f, ok
}

// HuntsPlayer reports whether a cowardly-hunt npc would pick this player up at all.
// engine/src/engine/entity/Npc.ts skips a player when combatLevel > vislevel*2 outside the
// wilderness. combatLevel 0 means "not known yet", and is treated as hunted: the whole point of
// this package is protecting accounts too weak to fight, so an unknown level must not silently
// switch the protection off.
func (f Facts) HuntsPlayer(combatLevel int) bool {
	return f.NoLevelCap || combatLevel <= 0 || combatLevel <= f.Level*2
}

// Outclasses reports whether this npc is far too strong for an account of combatLevel to push
// past: its level is at least twice the account's (an unknown level counts as outclassed, the
// same fail-safe HuntsPlayer uses). Live 2026-09-20 on 3299c3c, three combat-3 fishers (acct84,
// herring, acct46) died to level-27 Giant bats on the Ardougne -> Catherby walk right after
// hostileevade logged "pushing on through".
func (f Facts) Outclasses(combatLevel int) bool {
	return combatLevel <= 0 || f.Level >= 2*combatLevel
}

// NoticeRange is how close this npc has to be before evading is worth it: its own hunt range
// plus a margin, so the account turns away before it is inside the range rather than after.
func (f Facts) NoticeRange(margin int) int {
	r := f.HuntRange + margin
	if r < 1 {
		r = 1
	}
	return r
}

// BreakOffRange is how far away counts as clear of this npc. Its own chase limit plus a margin.
//
// Measured from the npc's CURRENT position rather than its spawn tile, which the client cannot
// see. That is a safe approximation in the direction that matters: an npc chasing the account is
// somewhere between its spawn and here, so its spawn is at least as far away as it is, and a
// distance that clears MaxRange from the npc clears MaxRange from the spawn too.
func (f Facts) BreakOffRange(margin int) int {
	r := f.MaxRange + margin
	if r < 1 {
		r = 1
	}
	return r
}

// NPC is one visible npc, reduced to what a decision needs.
type NPC struct {
	Index int
	Type  int
	X, Z  int
	Level int
	// TargetsUs is whether this npc is facing the account specifically.
	TargetsUs bool
}

// Snapshot is the live state Decide reads, captured once by the caller.
type Snapshot struct {
	X, Z, Level int
	// CombatLevel is the account's own combat level, 0 when unknown (treated as hunted).
	CombatLevel int
	InCombat    bool
	NPCs        []NPC
}

// Config is the tuning. Both margins are small on purpose: this runs during ordinary travel, and
// a wide berth on a route that funnels through a walled compound gate would strand the account
// instead of protecting it — the exact failure a hard avoid-by-radius danger zone already hit
// here (cmd/dangerzonecheck: any radius of 2 or more makes the whole Catherby leg unreachable).
type Config struct {
	Enabled bool
	// NoticeMargin is added to a hostile's own hunt range to decide when to break off.
	NoticeMargin int
	// BreakOffMargin is added to its chase limit to decide when the account is clear.
	BreakOffMargin int
}

// DefaultConfig notices a hostile six tiles outside its hunt range and calls the account clear
// three tiles past its chase limit. For both catalogued species that means reacting at 8 tiles
// and clearing at 13 (bat) or 8 (wolf).
//
// Why the notice margin is 6 and not 2, which is what it was first built with: the margin has to
// cover the ground the account crosses between two checks, not just the hostile's own hunt range.
// A caller polls every 200ms, and a running account on this engine's 3x-compressed ticks covers
// two or three tiles in that time, so a 4-tile ring leaves barely one poll of warning. Measured
// live across five stranded runs walking east into the giant bat compound at 2752-2759,
// 3396-3409: with a 4-tile ring only one of the five broke off in time, and the logs of the other
// four show the bat first seen at 5, 9, 13 and 15 tiles and already attacking by the next poll.
// Widening the ring costs a break-off the account might not have strictly needed; not widening it
// costs the whole travel leg and a reset to Lumbridge. The per-session break-off budget in
// freshstartrunner is what stops the wider ring turning into a stalled route.
func DefaultConfig() Config {
	return Config{Enabled: true, NoticeMargin: 6, BreakOffMargin: 3}
}

// Action is what to do about static hostiles this pass.
type Action struct {
	// Evade is whether to break off now.
	Evade bool
	// Threat is the npc that triggered it, and Facts its catalogued numbers.
	Threat NPC
	Facts  Facts
	// BreakOff is the distance from Threat that counts as clear.
	BreakOff int
	// Reason is a short phrase for the log line.
	Reason string
}

// Chebyshev is the diagonal-counts-as-one distance the game itself uses.
func Chebyshev(ax, az, bx, bz int) int {
	dx := ax - bx
	if dx < 0 {
		dx = -dx
	}
	dz := az - bz
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// Decide picks the nearest catalogued hostile that is close enough to matter and says to break
// off from it. An npc already targeting the account counts however far away it is within the
// notice ring, since it has already made its decision; one that has not noticed the account is
// only worth avoiding while it is close enough to.
func Decide(cfg Config, snap Snapshot) Action {
	var a Action
	if !cfg.Enabled {
		return a
	}
	best := -1
	bestDist := 0
	var bestFacts Facts
	for i, npc := range snap.NPCs {
		if npc.Level != snap.Level {
			continue
		}
		f, ok := Known(npc.Type)
		if !ok {
			continue
		}
		if !npc.TargetsUs && !f.HuntsPlayer(snap.CombatLevel) {
			// Too strong to be hunted and not already engaged: walking past is safe.
			continue
		}
		dist := Chebyshev(npc.X, npc.Z, snap.X, snap.Z)
		if dist > f.NoticeRange(cfg.NoticeMargin) {
			continue
		}
		if best == -1 || dist < bestDist {
			best, bestDist, bestFacts = i, dist, f
		}
	}
	if best == -1 {
		return a
	}
	a.Evade = true
	a.Threat = snap.NPCs[best]
	a.Facts = bestFacts
	// The two rings must never invert. Decide fires when a hostile is inside the NOTICE ring,
	// and Cleared calls the account safe once it is outside the BREAK-OFF ring, so a break-off
	// ring smaller than the notice ring makes a hostile both "evade this" and "already clear"
	// at the same time. Live-found 2026-09-18 on the webwalk path, where the notice margin is
	// wider (a 600ms poll has to cover more ground than a 200ms one): a White wolf chases to
	// only 5 tiles, so its break-off ring of 8 sat inside a notice ring of 12, and every wolf
	// 9 to 12 tiles away booked a break-off, walked nowhere, and reported that it had moved.
	// The walker then replanned on every poll while the account stood still, and the whole
	// per-session budget went in under three seconds — six break-offs at the same timestamp,
	// every one of them "resuming the route" from the tile it started on.
	a.BreakOff = bestFacts.BreakOffRange(cfg.BreakOffMargin)
	if floor := bestFacts.NoticeRange(cfg.NoticeMargin) + 1; a.BreakOff < floor {
		a.BreakOff = floor
	}
	switch {
	case a.Threat.TargetsUs:
		a.Reason = "it has locked on"
	case snap.InCombat:
		a.Reason = "already under attack nearby"
	default:
		a.Reason = "inside its hunt range"
	}
	return a
}

// Point is one candidate tile.
type Point struct{ X, Z, Level int }

// compass is the eight directions candidates are offered in, the same ring shape
// randomevents' own flee candidate search uses.
var compass = [8][2]int{{0, 1}, {1, 1}, {1, 0}, {1, -1}, {0, -1}, {-1, -1}, {-1, 0}, {-1, 1}}

// EscapeCandidates offers tiles dist away from the account, ordered by how far each one puts it
// from the threat, farthest first. Pure: the caller filters these for reachability against the
// real collision pack, exactly as randomevents' walkableFlee does for its own candidates.
func EscapeCandidates(fromX, fromZ, level, threatX, threatZ, dist int) []Point {
	if dist < 1 {
		dist = 1
	}
	out := make([]Point, 0, len(compass))
	for _, d := range compass {
		out = append(out, Point{X: fromX + d[0]*dist, Z: fromZ + d[1]*dist, Level: level})
	}
	// Insertion sort by descending distance from the threat: tiny fixed-size slice, and a
	// stable order keeps the unit test meaningful.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a := Chebyshev(out[j].X, out[j].Z, threatX, threatZ)
			b := Chebyshev(out[j-1].X, out[j-1].Z, threatX, threatZ)
			if a <= b {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Cleared reports whether the account has broken off: the threat is gone from view, or it is
// farther away than its own chase limit plus the margin.
//
// Checks real npc positions rather than an in-combat flag, for the same reason
// randomevents.HostileEventNpcNearby does: a melee npc standing right next to the account leaves
// any "was I hit recently" signal reading false for seconds at a time between its attack ticks.
func Cleared(snap Snapshot, threat NPC, breakOff int) bool {
	for _, npc := range snap.NPCs {
		if npc.Index != threat.Index {
			continue
		}
		if npc.Level != snap.Level {
			return true
		}
		return Chebyshev(npc.X, npc.Z, snap.X, snap.Z) > breakOff
	}
	return true
}

// AnyNear reports whether any catalogued hostile sits within radius of a tile, whatever it is
// currently doing. Used to keep an escape from landing next to a second wolf, and to keep a
// resumed route from being planned straight back through the pack the account just left.
func AnyNear(snap Snapshot, x, z, level, radius int) bool {
	for _, npc := range snap.NPCs {
		if npc.Level != level {
			continue
		}
		if _, ok := Known(npc.Type); !ok {
			continue
		}
		if Chebyshev(npc.X, npc.Z, x, z) <= radius {
			return true
		}
	}
	return false
}
