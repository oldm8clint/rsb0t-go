package gamestate

import "rsb0t-go/internal/packet"

// TileUnknown is the sentinel level before the first REBUILD_NORMAL + PLAYER_INFO pair
// has been seen — mirrors having no usable Game.tile() yet.
const TileUnknown = -1

// walkDX/walkDZ mirror ClientEntity.ts's moveCode direction table exactly (a different,
// unrelated numbering from pathfind's own 8-direction exit-mask bits — this one is the
// wire encoding for PLAYER_INFO's walk/run step codes, and must not be reordered
// independently of it).
var walkDX = [8]int{-1, 0, 1, -1, 1, -1, 0, 1}
var walkDZ = [8]int{1, 1, 1, 0, 0, -1, -1, -1}

// DebugPosition, when set, receives one line per REBUILD_NORMAL and per PLAYER_INFO
// "case 3" full-position update — temporary diagnostic hook for the 2026-09-15
// long-walk position-desync investigation (see cmd/gatetest), nil (a no-op) otherwise.
var DebugPosition func(format string, args ...any)

func debugPosition(format string, args ...any) {
	if DebugPosition != nil {
		DebugPosition(format, args...)
	}
}

// applyRebuildNormal ports Client.ts's REBUILD_NORMAL handling: the packet's two zone
// coordinates (8-tile units, centred on the player) become the build area's absolute
// tile origin. Called with the mutex already held.
//
// Rebases localX/localZ (plus every tracked NPC's and other player's local coordinates)
// by the exact amount mapBuildBase just shifted, keeping mapBuildBase+local (the
// absolute tile) continuous across the rebuild. The reference client (rsb0t-main's own
// Client.ts) never does this — searched thoroughly, no adjustment to localPlayer.x/z (or
// any NPC/player) tied to a mapBuildBase change exists anywhere in it — because it
// doesn't need to: a real Jagex server is expected to always pair a rebuild with a hard
// (case 3, teleport-shaped) PLAYER_INFO update immediately after, which overwrites local
// coordinates outright rather than incrementing them, making any client-side rebasing
// moot. This local engine's own BuildArea.rebuildNormal() (see its own doc comment/
// source) does NOT provide that guarantee — it just writes RebuildNormal and lets the
// PLAYER_INFO encoder continue on its own ordinary per-tick schedule, which can and does
// emit a plain small-step (op 1/2) update immediately after a rebuild with no intervening
// hard resync. Found live 2026-09-15 (see COLLAB.md) via a real, definitive repro:
// engine-side ground-truth logging added directly to the local engine's own
// Player.ts/PathingEntity.ts (temporary, for this investigation) proved the account was
// walking correctly and continuously the entire time; our own client's tracked position
// diverged by exactly the mapBuildBase delta at the exact tick a rebuild fired without a
// following case-3 update — every subsequent walk click, computed from that wrong tracked
// position, was accepted by the server (within its own 104-tile stated-start tolerance —
// see MoveClickHandler.ts) but aimed at nowhere real, producing the "FindPath says clear,
// account never actually reaches it" symptom chased most of this session. This rebase is
// harmless when a case-3 DOES follow (case 3 overwrites local outright, ignoring whatever
// this set it to) and is the fix when it doesn't.
//
// Independently found and fixed the same session on the `aj/posdrift` line of work:
// every already-tracked NPC's and other player's localX/localZ was stored relative to
// the OLD mapBuildBase (new NPCs/players spawn at the current local coordinate, existing
// ones only ever get incremental walk deltas added — nothing about that path knows about
// a rebuild) — but NPCs()/Players() read them back out against the NEW mapBuildBase.
// Left unrebased, every NPC or player seen before a rebuild silently drifts by exactly
// this delta, compounding on every further zone boundary crossed. Confirmed live:
// accounts that had walked a long distance (crossing several REBUILD_NORMAL zone
// boundaries) reported ZERO NPCs within 20 tiles despite standing right next to real
// ones — cmd/npcscan's own fresh ::tele (a single hard resync, no accumulated drift)
// never reproduced it, which is what pointed at accumulated rebase error specifically.
func (s *State) applyRebuildNormal(r *packet.Packet) {
	zoneX := int(r.G2())
	zoneZ := int(r.G2())
	newBaseX := (zoneX - 6) * 8
	newBaseZ := (zoneZ - 6) * 8
	if s.haveTile {
		dx := s.mapBuildBaseX - newBaseX
		dz := s.mapBuildBaseZ - newBaseZ
		if dx != 0 || dz != 0 {
			s.localX += dx
			s.localZ += dz
			for _, e := range s.npcs {
				e.localX += dx
				e.localZ += dz
			}
			for _, e := range s.players {
				e.localX += dx
				e.localZ += dz
			}
		}
	}
	s.mapBuildBaseX = newBaseX
	s.mapBuildBaseZ = newBaseZ
	debugPosition("REBUILD_NORMAL zoneX=%d zoneZ=%d -> mapBuildBase=%d,%d (rebased local to %d,%d)", zoneX, zoneZ, s.mapBuildBaseX, s.mapBuildBaseZ, s.localX, s.localZ)
}

// applyPlayerInfoLocal ports getPlayerPosLocal — just the local player's own
// still/walk/run/teleport branch, the only part of PLAYER_INFO this client decodes.
// The rest of the packet (other players' updates, appearance data) is intentionally
// left unread: the caller already has the full payload length from VARSHORT framing,
// so abandoning the bit cursor here mid-packet is safe — the next ReadPacket call
// starts fresh at the next opcode byte regardless of how far this one got.
func (s *State) applyPlayerInfoLocal(r *packet.Packet) {
	r.GBitStart()
	info := r.GBit(1)
	if info == 0 {
		debugPosition("PLAYER_INFO info=0 (no local update this packet)")
		return
	}
	op := r.GBit(2)
	debugPosition("PLAYER_INFO info=1 op=%d before=%d,%d", op, s.localX, s.localZ)
	switch op {
	case 0:
		// "still" — appearance/extended update only, no position change.
	case 1:
		dir := int(r.GBit(3))
		s.localX += walkDX[dir]
		s.localZ += walkDZ[dir]
		_ = r.GBit(1) // extendedInfo flag — irrelevant to position, not read further.
		debugPosition("PLAYER_INFO op=1 dir=%d after=%d,%d", dir, s.localX, s.localZ)
	case 2:
		dir := int(r.GBit(3))
		s.localX += walkDX[dir]
		s.localZ += walkDZ[dir]
		dir2 := int(r.GBit(3))
		s.localX += walkDX[dir2]
		s.localZ += walkDZ[dir2]
		_ = r.GBit(1)
		debugPosition("PLAYER_INFO op=2 dir1=%d dir2=%d after=%d,%d", dir, dir2, s.localX, s.localZ)
	case 3:
		s.level = int(r.GBit(2))
		s.localX = int(r.GBit(7))
		s.localZ = int(r.GBit(7))
		_ = r.GBit(1) // jump flag — a smooth-vs-hard teleport distinction, irrelevant here.
		_ = r.GBit(1) // extendedInfo flag.
		debugPosition("PLAYER_INFO case3 local=%d,%d level=%d -> absolute=%d,%d", s.localX, s.localZ, s.level, s.mapBuildBaseX+s.localX, s.mapBuildBaseZ+s.localZ)
	}
	s.haveTile = true
}
