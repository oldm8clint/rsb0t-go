package fleetrunner

import (
	"strings"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// HasAttackOp reports whether an NPC type offers Attack: the ones that can hunt a player.
func HasAttackOp(ops [5]string) bool {
	for _, op := range ops {
		if strings.EqualFold(op, "Attack") {
			return true
		}
	}
	return false
}

func NavOf(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }
