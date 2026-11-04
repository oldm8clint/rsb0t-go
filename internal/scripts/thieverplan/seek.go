package thieverplan

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Where a pickpocket rung's NPCs are when they are not in sight (live 11 Oct 2026 12:00-24:00: 21,864 "nothing to
// rob within N tiles of ... is X really here?" lines from ScarabSwarm, 7 account-hours standing still, the longest
// run 14 minutes).
//
// The rung's leash says how far from its anchor a target may be and still be ours; it does not put the target in
// sight. A client sees the NPCs within about fifteen tiles, the Knights of Ardougne wander fifteen from their
// spawns (content ardougne npc 23: wanderrange 15) and the Knight rung's leash is 29, so an account that came
// back from the bank stopped at the leash's edge, twenty-odd tiles from every Knight, and polled an empty NPC
// list every 1.2 seconds. The walk to the rung (PickpocketRung) only starts beyond the leash.
//
// The answer here is the spawn table: with none of the rung's NPCs in view, go and look where they spawn, the
// nearest first, remembering where the account has been so it tours them instead of standing at one.

// SeekArrive is how near a spawn the account stands to have looked there: inside it the NPC is in sight whatever
// it wanders to, so no new look at that spawn is worth a walk.
const SeekArrive = 3

// SeekMemory is how long a spawn the account walked to is not walked to again. A rung's NPCs respawn in a minute
// (respawnrate 100 ticks) and a Knight wanders across the market, so a spawn looked at and found empty is worth
// another look once the others have had theirs.
const SeekMemory = 90 * time.Second

// rungSpawns are where the NPCs of the Ardougne-side pickpocket rungs spawn on the ground floor, from the content
// maps (maps/m39_50 to m42_52 .jm2, NPC sections; ids: Knight of Ardougne 23 and 26, Hero 21, Paladin 20 and 365,
// Warrior woman 15, Guard 32), by Method label. SeekSpawns keeps the ones inside the rung's leash.
var rungSpawns = map[string][]tile.Tile{
	"Knight of Ardougne": {{X: 2582, Z: 3297}, {X: 2652, Z: 3318}, {X: 2653, Z: 3300}, {X: 2669, Z: 3298}, {X: 2671, Z: 3313}},
	"Hero":               {{X: 2630, Z: 3288}, {X: 2647, Z: 3306}, {X: 2667, Z: 3316}},
	"Paladin": {{X: 2571, Z: 3307}, {X: 2572, Z: 3303}, {X: 2575, Z: 3296}, {X: 2577, Z: 3308}, {X: 2581, Z: 3286},
		{X: 2581, Z: 3299}, {X: 2583, Z: 3292}, {X: 2653, Z: 3315}, {X: 2657, Z: 3307}},
	"Warrior woman": {{X: 2584, Z: 3305}, {X: 2585, Z: 3289}, {X: 2587, Z: 3291}, {X: 2629, Z: 3295}, {X: 2632, Z: 3299}},
	"Guard": {{X: 2651, Z: 3307}, {X: 2659, Z: 3309}, {X: 2660, Z: 3309}, {X: 2661, Z: 3309}, {X: 2663, Z: 3301},
		{X: 2664, Z: 3318}, {X: 2665, Z: 3300}},
}

// SeekSpawns are the known spawn tiles of m's NPCs that a target could be standing near and still be inside m's
// leash: on the anchor's level (the table lists the ground floor) and within the leash of the anchor. Nil for a
// rung with no table.
func SeekSpawns(m Method) []tile.Tile {
	if m.Anchor.Level != 0 {
		return nil
	}
	var out []tile.Tile
	for _, s := range rungSpawns[m.Label] {
		if tile.Chebyshev(s, m.Anchor) <= m.Leash {
			out = append(out, s)
		}
	}
	return out
}

// SeekTarget is the spawn an account that sees none of the rung's NPCs should walk to: the nearest of spawns that
// the account does not already stand within SeekArrive of and has not walked to inside SeekMemory (wentAt reports
// when it did, if it did; nil forgets nothing). ok is false when there is none.
func SeekTarget(spawns []tile.Tile, here tile.Tile, wentAt func(tile.Tile) (time.Time, bool), now time.Time) (tile.Tile, bool) {
	best, bestDist, found := tile.Tile{}, 0, false
	for _, s := range spawns {
		dist := tile.Chebyshev(here, s)
		if dist <= SeekArrive {
			continue
		}
		if wentAt != nil {
			if at, went := wentAt(s); went && now.Sub(at) < SeekMemory {
				continue
			}
		}
		if !found || dist < bestDist {
			best, bestDist, found = s, dist, true
		}
	}
	return best, found
}
