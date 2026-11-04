// Package zonetrigger knows which 8x8 zones and 64x64 map squares carry a content trigger script,
// so a caller can tell whether a step fires one.
//
// Engine-TS 289 (NetworkPlayer.updateMap, Player.triggerZone and triggerMapzone) checks at the end of
// every tick whether the player's zone or map square changed, and on a change queues the content
// script [zoneexit,...] for the zone left and [zone,...] for the zone entered, likewise
// [mapzoneexit,...] and [mapzone,...] for the map square (always named on level 0), each as an
// ENGINE queue entry. Most are the music player's ([mapzone,0_50_50] @music_playbyregion). An ENGINE
// entry cannot run while the player is delayed (Player.processEngineQueue needs canAccess), and while
// one waits, Player.updateMovement stops a player that has a move click pending and is busy. So a
// step that fires a trigger just before or during a p_delay halts the walk until the delay ends. A
// High Alchemy cast on the move is p_delay(3): on the lab engine on 6 Oct 2026 62 ticks of running
// with a cast every 5 ticks covered 90 tiles where the same route covered 122 without one
// (claude/tickmanip-1005).
//
// table.go is generated from a content checkout: go run ./internal/zonetrigger/gen -content <dir>.
package zonetrigger

import "rsb0t-go/internal/scripts/tile"

// Fires reports whether a step from a to b fires a zone or map-square trigger: b is in another zone
// or map square, and the one left has an exit script or the one entered an entry script. The two
// tiles need not be adjacent (a running tick covers two); only the zones they stand in matter.
func Fires(a, b tile.Tile) bool {
	if a.X>>6 != b.X>>6 || a.Z>>6 != b.Z>>6 {
		if mapzoneExit[squareKey(a)] || mapzoneEnter[squareKey(b)] {
			return true
		}
	}
	if a.Level != b.Level || a.X>>3 != b.X>>3 || a.Z>>3 != b.Z>>3 {
		if zoneExit[zoneKey(a)] || zoneEnter[zoneKey(b)] {
			return true
		}
	}
	return false
}

// FirstFiring is the distance along path (Chebyshev steps from its first tile) to the first step
// between consecutive tiles that fires a trigger, looking no further than within tiles; 0 when none
// does. path[0] is where the walker stands.
func FirstFiring(path []tile.Tile, within int) int {
	dist := 0
	for i := 1; i < len(path) && dist < within; i++ {
		dist += tile.Chebyshev(path[i-1], path[i])
		if Fires(path[i-1], path[i]) {
			return max(dist, 1)
		}
	}
	return 0
}

// squareKey and zoneKey are the table's keys: a map square by its square coordinates (level is not
// part of a map square's name), a zone by its level and zone coordinates.
func squareKey(t tile.Tile) uint32 { return uint32(t.X>>6)<<8 | uint32(t.Z>>6) }

func zoneKey(t tile.Tile) uint32 {
	return uint32(t.Level)<<24 | uint32(t.X>>3)<<12 | uint32(t.Z>>3)
}

func set(keys ...uint32) map[uint32]bool {
	m := make(map[uint32]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}
