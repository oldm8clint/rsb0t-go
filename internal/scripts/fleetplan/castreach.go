package fleetplan

// Casting at a target the account cannot walk up to ("scarabducks", 2026-09-27): the Ducks on the
// Ardougne lake. Pure.
//
// HOW THE ENGINE JUDGES A SPELL'S REACH (Engine-TS 289 and its content, the authority):
//
//   - An autocast attack is the Attack op, and content's [apnpc2,_] (player_combat.rs2
//     player_combat_start_ap) takes ~player_attackrange, which is 10 whenever autocast is on, and
//     starts the fight once npc_range(coord) <= 10; further out it calls p_aprange and the player
//     keeps approaching.
//   - A hand cast is OPNPCT, whose [apnpct,magic:*] triggers run at the player's apRange, 10 by
//     default (PathingEntity.apRange, reset to 10 after every interaction).
//   - Either way the engine only runs an ap trigger when Player.inApproachDistance holds:
//     CoordGrid.distanceTo(player, npc) <= range (Chebyshev between the nearest tiles of the two,
//     so 10 for a one-tile duck) AND isApproached, which is rsmod's hasLineOfSight from the
//     player's tile to the npc's (LineValidator.rayCastLine with the SIGHT_BLOCKED_* flags).
//   - Until both hold the server paths the player TOWARDS the npc (PathingEntity.pathToTarget,
//     findPathToEntity), and for an npc on water that walk ends on the nearest land.
//
// So a Duck ten tiles out on open water is in reach from the bank; one behind a projectile-blocking
// wall is not, whatever the distance.

import "rsb0t-go/internal/scripts/tile"

// CastRange is how far (Chebyshev) a combat spell reaches in this era: player_attackrange's 10
// with autocast on, and the default apRange of 10 for a hand cast.
const CastRange = 10

// CastMargin is kept inside CastRange when choosing where to cast from: a Duck moves a tile a
// tick, and the cast is judged a tick or two after the choice.
const CastMargin = 1

// Sight is one tile's line-of-sight blockers, the engine's SIGHT_BLOCKED_* flags as far as a
// caller can know them: a projectile-blocking wall on one of its four faces, or a projectile
// blocker filling the tile.
type Sight uint8

const (
	SightNorth Sight = 1 << iota // a wall on the tile's north face
	SightEast
	SightSouth
	SightWest
	SightFull // the tile itself blocks projectiles (a LOC_PROJ_BLOCKER)
)

// LineOfSight is the engine's hasLineOfSight for two one-tile entities on one level
// (LineValidator.rayCastLine with los=true, ported step for step): a ray from the centre of src's
// tile to dst's, refused by a tile it enters through a blocked face or that blocks projectiles
// outright, except that the destination tile's own full blocker does not count. at reads a tile's
// blockers; a nil at sees through everything.
func LineOfSight(src, dst tile.Tile, at func(x, z int) Sight) bool {
	if src.Level != dst.Level {
		return false
	}
	if at == nil {
		return true
	}
	sx, sz, ex, ez := src.X, src.Z, dst.X, dst.Z
	if sx == ex && sz == ez {
		return true
	}
	const unit, half = 1 << 16, 1 << 15
	dxv, dzv := ex-sx, ez-sz
	adx, adz := Abs(dxv), Abs(dzv)
	east, north := dxv >= 0, dzv >= 0
	// The face a ray crosses entering a tile: travelling east it enters through the tile's west
	// face, and so on.
	xFace, zFace := SightEast, SightNorth
	if east {
		xFace = SightWest
	}
	if north {
		zFace = SightSouth
	}
	blocked := func(x, z int, face Sight, full bool) bool {
		s := at(x, z)
		return s&face != 0 || (full && s&SightFull != 0)
	}
	xFull, zFull := true, true
	if adx > adz {
		offX, offZ := 1, 0
		if !east {
			offX = -1
		}
		if !north {
			offZ = -1
		}
		scaledZ := sz*unit + half + offZ
		tangent := dzv * unit / adx
		for x := sx; x != ex; {
			x += offX
			z := scaledZ >> 16
			if x == ex && z == ez {
				xFull = false
			}
			if blocked(x, z, xFace, xFull) {
				return false
			}
			scaledZ += tangent
			nz := scaledZ >> 16
			if x == ex && nz == ez {
				zFull = false
			}
			if nz != z && blocked(x, nz, zFace, zFull) {
				return false
			}
		}
		return true
	}
	offX, offZ := 0, 1
	if !east {
		offX = -1
	}
	if !north {
		offZ = -1
	}
	scaledX := sx*unit + half + offX
	tangent := dxv * unit / adz
	for z := sz; z != ez; {
		z += offZ
		x := scaledX >> 16
		if x == ex && z == ez {
			zFull = false
		}
		if blocked(x, z, zFace, zFull) {
			return false
		}
		scaledX += tangent
		nx := scaledX >> 16
		if nx == ex && z == ez {
			xFull = false
		}
		if nx != x && blocked(nx, z, xFace, xFull) {
			return false
		}
	}
	return true
}

// CastableFrom reports whether a spell from `from` reaches a target at `at`: within CastRange less
// CastMargin and in line of sight.
func CastableFrom(from, at tile.Tile, sight func(x, z int) Sight) bool {
	return from.Level == at.Level && tile.Chebyshev(tile.Tile{X: from.X, Z: from.Z}, tile.Tile{X: at.X, Z: at.Z}) <= CastRange-CastMargin &&
		LineOfSight(from, at, sight)
}

// CastTile is where a magic-only pen's account should stand to cast at a target on `at`: where it
// stands now when that already reaches, else the nearest of the pen's CastFrom tiles that does and
// is on none of the pen's hazards. false when no tile of the pen reaches the target, which is a
// target this pen cannot train on.
func (p TrainPen) CastTile(here *tile.Tile, at tile.Tile, sight func(x, z int) Sight) (tile.Tile, bool) {
	if here != nil && p.SafeTarget(*here) && CastableFrom(*here, at, sight) {
		return *here, true
	}
	var best tile.Tile
	bestDist, found := 0, false
	for _, c := range p.CastFrom {
		if !p.SafeTarget(c) || !CastableFrom(c, at, sight) {
			continue
		}
		dist := 0
		if here != nil {
			dist = tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, c)
		}
		if found && dist >= bestDist {
			continue
		}
		best, bestDist, found = c, dist, true
	}
	return best, found
}
