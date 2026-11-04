package gamestate

// What the client saw of a death, for the one log line every death gets (internal/deathreport,
// internal/fleet's death hook). The game says only "Oh dear you are dead!"; who or what did it is
// not in any packet, so this keeps the pieces a reader can infer from: the hitpoints in the seconds
// before, the NPCs facing the account, the players facing it, a poison hitmark, and the pack as it
// stood at the moment the line arrived. The server sends the line from the death script BEFORE
// player_death_lose_items drops the pack and before the respawn teleport, and the inventory and
// position updates ride later packets, so State still holds the pack and the tile of the death when
// the line is decoded (that is the whole reason the snapshot is taken here and not by a poller).
//
// Why it exists (deaths-1008, 8 Oct 2026): a script's own "died near" line used the tile of its last
// loop pass, which for any long action (a quest leg, a training step) is minutes and many tiles
// stale (five deaths "near 2580,3385" were the retreat tile the loop began on), and 754 lifetime
// deaths on the panel had no cause on file at all.

import "rsb0t-go/internal/scripts/levels"

// packComponentID is the backpack's inventory component (actions.PlayerPack; gamestate cannot import
// actions).
const packComponentID = 3214

// wornComponentID is the worn-items inventory component (actions.PlayerWorn).
const wornComponentID = 1688

// deathHPWindowTicks is how far back the hitpoints peak is read: the fight that killed the account.
const deathHPWindowTicks = 30

// deathNearbyRadius is how close an NPC that is not facing the account still counts as near it.
const deathNearbyRadius = 3

// deathNearbyMax caps the nearby list.
const deathNearbyMax = 6

// poisonWindowTicks is how recent a poison hitmark must be for the death to be called poisoned.
const poisonWindowTicks = 30

// DeathActor is one NPC at a death: its view index, type id and tile.
type DeathActor struct {
	Index, Type, X, Z int
}

// DeathContext is the client's reading of one death.
type DeathContext struct {
	// Tick is the server tick the death line arrived on.
	Tick int
	// HaveTile is false when the position was never read; X, Z and Level are then zero.
	HaveTile    bool
	X, Z, Level int
	// HPPeak is the highest hitpoints reading in the deathHPWindowTicks before the death (the health the fight
	// started from) and HPLast the last reading above zero (what was left before the killing blow); 0 when none was
	// seen. MaxHP is the hitpoints level.
	HPPeak, HPLast, MaxHP int
	// Attackers are the NPCs facing the account when it died; Nearby are other NPCs within deathNearbyRadius tiles.
	Attackers, Nearby []DeathActor
	// Players is how many other players were facing the account.
	Players int
	// Poisoned is a poison hitmark within poisonWindowTicks.
	Poisoned bool
	// Pack is the backpack as it stood when the line arrived (before the drop).
	Pack []SlotItem
	// Worn is the equipment as it stood when the line arrived: the death script drops it with the pack (deathloot).
	Worn []SlotItem
}

type hpSample struct{ tick, hp int }

// deathTracker is the State's death memory: a small ring of hitpoints readings and the last death.
type deathTracker struct {
	ring [8]hpSample
	n    int // readings ever noted; the ring holds the last len(ring)
	last DeathContext
	have bool
}

// noteHP records a hitpoints reading.
func (d *deathTracker) noteHP(tick, hp int) {
	d.ring[d.n%len(d.ring)] = hpSample{tick: tick, hp: hp}
	d.n++
}

// peakAndLast is the highest reading inside the window ending at tick and the last one above zero.
func (d *deathTracker) peakAndLast(tick int) (peak, last int) {
	count := d.n
	if count > len(d.ring) {
		count = len(d.ring)
	}
	for i := 0; i < count; i++ {
		smp := d.ring[(d.n-1-i)%len(d.ring)]
		if smp.hp <= 0 {
			continue
		}
		if last == 0 {
			last = smp.hp
		}
		if tick-smp.tick <= deathHPWindowTicks && smp.hp > peak {
			peak = smp.hp
		}
	}
	if peak == 0 {
		peak = last
	}
	return peak, last
}

// recordDeath snapshots the death into s.deaths. The caller holds s.mu.
func (s *State) recordDeath() {
	tick := s.scene.tick
	ctx := DeathContext{Tick: tick}
	if s.haveTile {
		ctx.HaveTile, ctx.X, ctx.Z, ctx.Level = true, s.mapBuildBaseX+s.localX, s.mapBuildBaseZ+s.localZ, s.level
	}
	ctx.HPPeak, ctx.HPLast = s.deaths.peakAndLast(tick)
	ctx.MaxHP = s.hpMaxSeen
	if lv := levels.LevelFromXP(s.skills[HitpointsStat].XP); lv > ctx.MaxHP {
		ctx.MaxHP = lv
	}
	me := s.SelfSlot
	for index, e := range s.npcs {
		x, z := s.mapBuildBaseX+e.localX, s.mapBuildBaseZ+e.localZ
		actor := DeathActor{Index: index, Type: e.typeID, X: x, Z: z}
		switch {
		case me >= 0 && e.faceEntity == playerFaceBase+me:
			ctx.Attackers = append(ctx.Attackers, actor)
		case ctx.HaveTile && len(ctx.Nearby) < deathNearbyMax && chebyshev(x, z, ctx.X, ctx.Z) <= deathNearbyRadius:
			ctx.Nearby = append(ctx.Nearby, actor)
		}
	}
	for _, p := range s.players {
		if me >= 0 && p.faceEntity == playerFaceBase+me {
			ctx.Players++
		}
	}
	ctx.Poisoned = s.self.poisonSeen && tick-s.self.poisonTick <= poisonWindowTicks
	if inv, ok := s.inventories[packComponentID]; ok {
		ctx.Pack = inv.NonEmpty()
	}
	if inv, ok := s.inventories[wornComponentID]; ok {
		ctx.Worn = inv.NonEmpty()
	}
	s.deaths.last, s.deaths.have = ctx, true
}

// LastDeath is the client's reading of the most recent death line, false before any.
func (s *State) LastDeath() (DeathContext, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deaths.last, s.deaths.have
}

func chebyshev(ax, az, bx, bz int) int {
	dx, dz := ax-bx, az-bz
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}
