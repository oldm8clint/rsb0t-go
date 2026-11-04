package gamestate

import (
	"fmt"
	"os"

	"rsb0t-go/internal/packet"
)

// This file ports the OTHER-PLAYERS branch of PLAYER_INFO — Client.ts's
// getPlayerPosOldVis/getPlayerPosNewVis/getPlayerPosExtended (the local player's own
// branch, getPlayerPosLocal, is applyPlayerInfoLocal in position.go and is unchanged by
// this file). Historically this client only ever decoded the local branch (see
// position.go's own doc comment) since nothing needed another player's position; a
// Players query is a real, useful gap per reference/server-api-docs.md (e.g. finding a
// known mule/donor account by name near you without relying on trade-UI text).
//
// PLAYER_INFO's OldVis/NewVis structure is closely analogous to NPC_INFO's — see
// npc.go's applyNpcInfo and its doc comment for the "maintain an ordered active-index
// list across ticks" technique this needs too, for the same reason: every visible
// player's position genuinely matters here, unlike the local-player shortcut.
//
// This file also walks the trailing per-entity "extended" data, because the player's display
// name lives there and nowhere else in PLAYER_INFO — NPCs have a type id to resolve a name from
// (via cachecfg), but a player's name is only ever sent in their own APPEARANCE update. See
// applyPlayerInfoExtended's doc comment for exactly how much of that block gets decoded and why
// the rest is skipped, not read. The local player's own block (index -1 there) is what
// self.go's SelfName, CombatLevel, SelfAnim, InCombat and AttackedByPlayer read.

// playerInfoDebug gates a raw diagnostic trace of PLAYER_INFO decoding behind an env var
// (RSB0T_PLAYERINFO_DEBUG=1) — zero-cost/silent for every normal run. Added 2026-09-14
// while chasing the "Player.Name comes back empty on re-sighting" bug (see COLLAB.md,
// worktrees agent-afacfcbaec77bf555 and agent-ada602387b8fc2de1, and
// playerAppearanceNames's own doc comment in state.go for the fix) — kept as a standing
// tool since nothing else in cmd/ dumps raw PLAYER_INFO bytes/bit-decode decisions, and
// this exact kind of live packet trace is what actually found the root cause.
var playerInfoDebug = os.Getenv("RSB0T_PLAYERINFO_DEBUG") != ""

func debugf(format string, args ...interface{}) {
	if playerInfoDebug {
		fmt.Fprintf(os.Stderr, "[playerinfo] "+format+"\n", args...)
	}
}

// playerUpdate* mirror ClientPlayer.ts's PlayerUpdate enum bit values exactly — the mask
// read at the start of each updated entity's extended block in getPlayerPosDecodeExtended.
const (
	playerUpdateAppearance = 0x1
	playerUpdateAnim       = 0x2
	playerUpdateFaceEntity = 0x4
	playerUpdateSay        = 0x8
	playerUpdateHitmark    = 0x10
	playerUpdateFaceSquare = 0x20
	playerUpdateChat       = 0x40
	playerUpdateBigUpdate  = 0x80
	playerUpdateSpotAnim   = 0x100
	playerUpdateExactMove  = 0x200
	playerUpdateHitmark2   = 0x400
)

// playerEntry is one tracked other-player's live state.
type playerEntry struct {
	combat     RenderCombat
	name       string
	localX     int
	localZ     int
	faceEntity int // -1 for none; a player is slot + 32768
	// combatLevel is from the player's appearance; 0 until one has been seen.
	combatLevel int
	hitTick     int
	hitSeen     bool
	// anim is the last sequence the server started on this player (-1 for none) and
	// animTick the scene tick it arrived on, kept for rendering (RenderSnapshot) only.
	anim     int
	animTick int
	// mark is the PLAYER_INFO pass (State.playerGen) that last listed this player.
	mark uint32
}

// Player is a snapshot of one visible other player, in absolute world coordinates —
// matches NPC's shape field-for-field (Index/X/Z/Level), swapping NPC's Type type-id for
// a player's Name, since that's the only per-entity identifier a player actually has.
type Player struct {
	Index int
	Name  string
	X, Z  int
	Level int
	// FaceEntity is who the player faces: -1 for nobody, an NPC index, or a player slot plus
	// 32768.
	FaceEntity int
	// InCombat is a hitmark on this player within the last combatWindowTicks ticks.
	InCombat bool
	// CombatLevel is from the player's appearance; 0 when none has been seen.
	CombatLevel int
}

// TargetsPlayer reports whether the player faces the player in slot (Player.targetsMe when slot
// is State.Self()).
func (p Player) TargetsPlayer(slot int) bool {
	return p.FaceEntity >= playerFaceBase && p.FaceEntity-playerFaceBase == slot
}

// localHasQueuedExtended is a read-only shadow-decode of PLAYER_INFO's local-player
// branch (position.go's applyPlayerInfoLocal, ported from getPlayerPosLocal) that learns
// ONLY whether it queues the local player's own extended-data update — see
// applyPlayerInfoOthers's doc comment for why that matters. It operates on a clone of r's
// cursor (same underlying Data, independent Pos/BitPos) so it never advances the real
// reader and never re-applies applyPlayerInfoLocal's position mutation a second time —
// that still happens exactly once, for real, in position.go, which this function does not
// call and does not need to: r.Pos is always 0 here (a fresh packet.NewReader per
// State.Handle dispatch), matching exactly what applyPlayerInfoLocal's own GBitStart call
// would compute.
func localHasQueuedExtended(r *packet.Packet) bool {
	shadow := &packet.Packet{Data: r.Data, Pos: r.Pos}
	shadow.GBitStart()
	if shadow.GBit(1) == 0 {
		return false
	}
	switch shadow.GBit(2) {
	case 0:
		return true
	case 1:
		shadow.GBit(3)
		return shadow.GBit(1) == 1
	case 2:
		shadow.GBit(3)
		shadow.GBit(3)
		return shadow.GBit(1) == 1
	case 3:
		shadow.GBit(2)
		shadow.GBit(7)
		shadow.GBit(7)
		shadow.GBit(1) // jump flag
		return shadow.GBit(1) == 1
	default:
		return false
	}
}

// applyPlayerInfoOthers ports getPlayerPosOldVis + getPlayerPosNewVis + the other-player
// slice of getPlayerPosExtended. It must be called on the SAME *packet.Packet, immediately
// after applyPlayerInfoLocal, without an intervening GBitStart — exactly mirroring
// Client.ts's getPlayerPos, which calls getPlayerPosLocal (updating the local player's own
// localX/localZ first) and only then getPlayerPosOldVis/NewVis, whose dx/dz offsets are
// relative to the ALREADY-UPDATED local player tile this same tick, not last tick's. This
// file never calls GBitStart itself for that reason — it continues the bit cursor
// applyPlayerInfoLocal left off.
//
// A second, easy-to-miss consequence of that shared packet: Client.ts's getPlayerPosLocal
// queues the LOCAL player's own extended-data update into the exact SAME entityUpdateIds
// list (under a LOCAL_PLAYER_INDEX sentinel) that getPlayerPosOldVis/NewVis append to, and
// getPlayerPosExtended walks that combined list strictly in order — local's own entry
// first, since getPlayerPosLocal runs first. position.go's applyPlayerInfoLocal (off
// limits to edit here) doesn't expose whether it queued one, so localHasQueuedExtended
// below performs a read-only shadow-decode of just that branch, purely to learn the one
// bit this file needs, without duplicating applyPlayerInfoLocal's real (mutating) work or
// touching position.go. Skipping this would silently misattribute the LOCAL player's own
// appearance bytes to whatever other player happens to be first in this file's own
// updated list — confirmed live against the local engine (see COLLAB.md): a second
// account's Players() view of the first showed the SECOND account's own name on the
// first account's entry, position still correct, only the name swapped.
func (s *State) applyPlayerInfoOthers(r *packet.Packet) {
	if playerInfoDebug {
		debugf("RAW payload (%d bytes): % x", len(r.Data), r.Data)
	}
	localQueuedExtended := localHasQueuedExtended(r)
	if playerInfoDebug {
		debugf("localQueuedExtended=%v startBitPos=%d", localQueuedExtended, r.BitPos)
	}

	// A real server's PLAYER_INFO always includes the OldVis count field, even when
	// it's 0 — but a hand-built payload covering only the local branch (the only part
	// of this packet anything needed before this file existed; internal/randomevents'
	// own test helpers build exactly this kind of minimal packet) has nothing further
	// to read here. Bail out the same defensive way this file already does elsewhere
	// on a too-short buffer, rather than letting the low-level GBit panic on an
	// out-of-range byte index.
	if r.BitPos+8 > len(r.Data)*8 {
		return
	}

	count := int(r.GBit(8))
	if playerInfoDebug {
		debugf("oldVis count=%d oldIDs=%v", count, s.playerIDs)
	}
	oldIDs := s.playerIDs
	if count > len(oldIDs) {
		// Matches the client's own "eek! too many players" guard turning into a hard
		// stop rather than reading garbage past what we actually have.
		return
	}

	// The list being built reuses the buffer from two passes ago, so a tick allocates nothing for it.
	newIDs := s.playerIDsSpare[:0]
	var updated []int
	for i := 0; i < count; i++ {
		index := oldIDs[i]
		if r.GBit(1) == 0 {
			newIDs = append(newIDs, index)
			continue
		}
		switch r.GBit(2) {
		case 0:
			// No extendedInfo bit for this op — getPlayerPosOldVis queues it
			// unconditionally (unlike op 1/2, which read one first), exactly as
			// applyNpcInfo's op 0 does.
			newIDs = append(newIDs, index)
			updated = append(updated, index)
		case 1:
			dir := int(r.GBit(3))
			if e := s.players[index]; e != nil {
				e.localX += walkDX[dir]
				e.localZ += walkDZ[dir]
			}
			newIDs = append(newIDs, index)
			if r.GBit(1) == 1 {
				updated = append(updated, index)
			}
		case 2:
			dir := int(r.GBit(3))
			if e := s.players[index]; e != nil {
				e.localX += walkDX[dir]
				e.localZ += walkDZ[dir]
			}
			dir = int(r.GBit(3))
			if e := s.players[index]; e != nil {
				e.localX += walkDX[dir]
				e.localZ += walkDZ[dir]
			}
			newIDs = append(newIDs, index)
			if r.GBit(1) == 1 {
				updated = append(updated, index)
			}
		case 3:
			delete(s.players, index)
		}
	}

	for r.BitPos+10 < len(r.Data)*8 {
		index := int(r.GBit(11))
		if index == 2047 {
			break
		}
		dx := int(r.GBit(5))
		if dx > 15 {
			dx -= 32
		}
		dz := int(r.GBit(5))
		if dz > 15 {
			dz -= 32
		}
		_ = r.GBit(1) // jump flag — irrelevant to position tracking.

		e := s.players[index]
		wasNew := e == nil
		if e == nil {
			e = &playerEntry{faceEntity: -1, anim: -1}
			// Mirrors Client.ts's getPlayerPosNewVis: immediately apply a cached
			// appearance (by index) for a brand-new entity, if we have one from an
			// earlier sighting — the server does not resend APPEARANCE on every
			// re-sighting of the same index, see playerAppearanceNames's own doc
			// comment in state.go.
			e.name = s.playerAppearanceNames[index]
			if data := s.playerAppearances[index]; data != nil {
				if _, level, ok := playerAppearance(data); ok {
					e.combatLevel = level
				}
			}
			s.players[index] = e
		}
		e.localX = s.localX + dx
		e.localZ = s.localZ + dz
		newIDs = append(newIDs, index)

		extInfo := r.GBit(1)
		if playerInfoDebug {
			debugf("newVis index=%d wasNew=%v dx=%d dz=%d extendedInfo=%d", index, wasNew, dx, dz, extInfo)
		}
		if extInfo == 1 {
			updated = append(updated, index)
		}
	}
	r.GBitEnd()
	if playerInfoDebug {
		debugf("updated=%v (before local sentinel)", updated)
	}

	if localQueuedExtended {
		// The local player's own extended block, if present, is FIRST in the shared
		// stream — see the doc comment above. -1 is a safe sentinel: real indices come
		// from GBit(11)/GBit(14) reads and are never negative, and s.players[-1] simply
		// doesn't exist, so applyPlayerInfoExtended walks its bytes (keeping every real
		// entity after it correctly aligned) and records the local fields into s.self.
		updated = append([]int{-1}, updated...)
	}
	s.applyPlayerInfoExtended(r, updated)

	s.playerGen++
	gen := s.playerGen
	for _, id := range newIDs {
		if e := s.players[id]; e != nil {
			e.mark = gen
		}
	}
	for id, e := range s.players {
		if e.mark != gen {
			delete(s.players, id)
		}
	}
	s.playerIDsSpare = oldIDs
	s.playerIDs = newIDs
}

// applyPlayerInfoExtended walks the byte-aligned extended-update block for every entity
// in updated, in order — ported from getPlayerPosDecodeExtended, as a partial decoder: the
// APPEARANCE name (and, for the local player, combat level), ANIM, FACEENTITY and the
// hitmarks' timing are kept; every other mask bit's fields are skipped by their exact byte
// length, never interpreted. Index -1 is the local player (see applyPlayerInfoOthers).
//
// This is the one place this file genuinely can't take applyNpcInfo's/
// applyPlayerInfoLocal's "abandon the cursor, the next packet starts fresh anyway"
// shortcut mid-block: once ANY entity needs its extended data walked to reach the name,
// every entity BEFORE it in this same list must also be walked correctly, or the mask
// byte for a later entity is read from the wrong offset. It remains safe to stop early
// (return) if the buffer runs out unexpectedly — VARSHORT framing means the next
// ReadPacket call still starts cleanly at the next opcode regardless of how far this
// function got, exactly as position.go's and npc.go's own doc comments already note.
//
// Field byte-lengths, in mask-check order (matches ClientPlayer.ts's PlayerUpdate enum
// order exactly):
//   - APPEARANCE (0x1): 1-byte length prefix + that many bytes (decoded for the name).
//   - ANIM (0x2): seqId (2) + delay (1) = 3 bytes.
//   - FACEENTITY (0x4): 2 bytes.
//   - SAY (0x8): newline-terminated string (GJStr already stops safely at end of data);
//     recorded as a chat line and, for the local player, SelfChat (chat.go).
//   - HITMARK (0x10): damage(1) + damageType(1) + health(1) + totalHealth(1) = 4 bytes.
//   - FACESQUARE (0x20): faceSquareX(2) + faceSquareZ(2) = 4 bytes.
//   - CHAT (0x40): colourEffect(2) + type(1) + a 1-byte length prefix + that many bytes
//     of WordPack-compressed text, unpacked into a public chat line (chat.go).
//   - BIG_UPDATE (0x80): not a field — folds a second mask byte in when set, exactly
//     like getPlayerPosDecodeExtended's own `mask.g1()` + conditional `+= g1()<<8`.
//   - SPOTANIM (0x100): spotanimId(2) + heightDelay(4) = 6 bytes.
//   - EXACTMOVE (0x200): 4 g1 fields + 2 g2 fields + 1 g1 field = 9 bytes.
//   - HITMARK2 (0x400): same shape as HITMARK, 4 bytes.
//
// Animations always replace the current one (the client compares sequence priorities from the
// seq config this client does not load), and hitmark and animation times are server ticks.
func (s *State) applyPlayerInfoExtended(r *packet.Packet, updated []int) {
	tick := s.playerInfoTick()
	for _, index := range updated {
		if r.Available() < 1 {
			debugf("extended: index=%d ABORT not enough bytes for mask (available=%d)", index, r.Available())
			return
		}
		mask := uint32(r.G1())
		if mask&playerUpdateBigUpdate != 0 {
			if r.Available() < 1 {
				debugf("extended: index=%d ABORT not enough bytes for big-update mask byte", index)
				return
			}
			mask += uint32(r.G1()) << 8
		}
		if playerInfoDebug {
			debugf("extended: index=%d mask=0x%x appearanceBit=%v", index, mask, mask&playerUpdateAppearance != 0)
		}

		if mask&playerUpdateAppearance != 0 {
			if r.Available() < 1 {
				debugf("extended: index=%d ABORT not enough bytes for appearance length", index)
				return
			}
			length := int(r.G1())
			if r.Available() < length {
				debugf("extended: index=%d ABORT appearance length=%d but only %d available", index, length, r.Available())
				return
			}
			data := make([]byte, length)
			r.GData(length, 0, data)
			name, combatLevel, hasLevel := playerAppearance(data)
			debugf("extended: index=%d appearance length=%d bytes=% x decodedName=%q", index, length, data, name)
			if index == -1 {
				s.self.appearance = data
				if name != "" {
					s.self.name = name
				}
				if hasLevel {
					s.self.combatLevel, s.self.haveLevel = combatLevel, true
				}
			}
			if e := s.players[index]; e != nil {
				e.name = name
				if hasLevel {
					e.combatLevel = combatLevel
				}
			}
			// Cache by index regardless of whether s.players[index] currently exists
			// (index -1 is the local-player sentinel and deliberately excluded, it is
			// never a real other-player index) — this is what lets a LATER re-sighting
			// of the same index, after this player has left and re-entered view, still
			// resolve a name even though the server won't send APPEARANCE again. See
			// state.go's playerAppearanceNames doc comment and the NewVis branch above.
			if index != -1 && name != "" {
				s.playerAppearanceNames[index] = name
				s.playerAppearances[index] = data
			}
		}
		if mask&playerUpdateAnim != 0 {
			if r.Available() < 3 {
				return
			}
			seq := int(r.G2())
			r.G1() // delay
			if seq == 65535 {
				seq = -1
			}
			if index == -1 {
				s.self.anim, s.self.animTick, s.self.haveAnim = seq, tick, true
			} else if e := s.players[index]; e != nil {
				e.anim, e.animTick = seq, tick
			}
		}
		if mask&playerUpdateFaceEntity != 0 {
			if r.Available() < 2 {
				return
			}
			face := int(r.G2())
			if face == 65535 {
				face = -1
			}
			if index == -1 {
				s.self.face, s.self.haveFace = face, true
			} else if e := s.players[index]; e != nil {
				e.faceEntity = face
			}
		}
		if mask&playerUpdateSay != 0 {
			s.noteSay(index, r.GJStr())
		}
		if mask&playerUpdateHitmark != 0 {
			if r.Available() < 4 {
				return
			}
			s.notePlayerHit(index, tick, s.readPlayerCombat(r, index, tick))
		}
		if mask&playerUpdateFaceSquare != 0 {
			if r.Available() < 4 {
				return
			}
			r.Skip(4)
		}
		if mask&playerUpdateChat != 0 {
			if r.Available() < 4 {
				return
			}
			r.Skip(2) // colour and effect
			rights := int(r.G1())
			length := int(r.G1())
			if r.Available() < length {
				return
			}
			packed := make([]byte, length)
			r.GData(length, 0, packed)
			s.notePublicChat(index, rights, packed)
		}
		if mask&playerUpdateSpotAnim != 0 {
			if r.Available() < 6 {
				return
			}
			if index == -1 {
				s.self.combat.readSpot(r, tick)
			} else if e := s.players[index]; e != nil {
				e.combat.readSpot(r, tick)
			} else {
				r.Skip(6)
			}
		}
		if mask&playerUpdateExactMove != 0 {
			if r.Available() < 9 {
				return
			}
			r.Skip(9)
		}
		if mask&playerUpdateHitmark2 != 0 {
			if r.Available() < 4 {
				return
			}
			s.notePlayerHit(index, tick, s.readPlayerCombat(r, index, tick))
		}
	}
}

// notePlayerHit stamps a hitmark on the local player (index -1) or another player. A poison
// hitmark (hitmark.constant ^hitmark_poison, damage type 2) on the local player is stamped on its
// own too (LastPoisonHit).
func (s *State) notePlayerHit(index, tick, damageType int) {
	if index == -1 {
		s.self.hitTick, s.self.hitSeen = tick, true
		if damageType == HitmarkPoison {
			s.self.poisonTick, s.self.poisonSeen = tick, true
		}
		return
	}
	if e := s.players[index]; e != nil {
		e.hitTick, e.hitSeen = tick, true
	}
}

// playerAppearanceName returns just the display name playerAppearance decodes.
func playerAppearanceName(data []byte) string {
	name, _, _ := playerAppearance(data)
	return name
}

// playerAppearance decodes just enough of a PLAYER_INFO extended-update's APPEARANCE
// sub-block (ClientPlayer.ts's setAppearance) to reach the display name and the combat level
// right after it. The name is not at a fixed offset — it comes after gender(1), headicons(1), a
// variable-length 12-part equipment-id loop (each part is 1 byte if empty, 2 if not, with an
// early-exit special case at part 0 for an NPC transmog id), 5 recolour bytes, and 7 two-byte
// animation ids — so all of that has to be walked in order first, exactly as setAppearance does,
// even though none of those fields (worn-item ids, colours, animations) are exposed by this
// decode. skillLevel, which follows combatLevel, is not read.
//
// The name is a base-37-packed int64 (JString.toRawUsername's encoding) —
// playerRawUsername/playerScreenName below port that decode and its capitalisation step.
// Any malformed/short blob decodes to "" (and hasLevel false) rather than panicking.
func playerAppearance(data []byte) (name string, combatLevel int, hasLevel bool) {
	r := packet.NewReader(data)
	if r.Available() < 2 {
		return "", 0, false
	}
	r.G1() // gender — not exposed.
	r.G1() // headicons — not exposed.

	for part := 0; part < 12; part++ {
		if r.Available() < 1 {
			return "", 0, false
		}
		msb := r.G1()
		if msb == 0 {
			continue
		}
		if r.Available() < 1 {
			return "", 0, false
		}
		lsb := r.G1()
		value := (int(msb) << 8) | int(lsb)
		if part == 0 && value == 65535 {
			// Transmogrified into an NPC's appearance — setAppearance reads the NPC
			// type id and stops the equipment loop early; nothing after part 0 is sent.
			if r.Available() < 2 {
				return "", 0, false
			}
			r.G2()
			break
		}
	}

	if r.Available() < 5 {
		return "", 0, false
	}
	for part := 0; part < 5; part++ {
		r.G1() // recolour — not exposed.
	}

	if r.Available() < 14 {
		return "", 0, false
	}
	for i := 0; i < 7; i++ {
		r.G2() // readyanim/turnanim/walkanim(x4)/runanim — not exposed.
	}

	if r.Available() < 8 {
		return "", 0, false
	}
	name = playerScreenName(playerRawUsername(r.G8()))
	if r.Available() < 1 {
		return name, 0, false
	}
	return name, int(r.G1()), true
}

// playerUsernameChars mirrors JString.ts's USERHASH_CHAR table exactly: index 0 is '_',
// 1-26 are 'a'-'z', 27-36 are '0'-'9'.
var playerUsernameChars = [...]byte{
	'_',
	'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
}

// playerRawUsername ports JString.toRawUsername: unpacks a base-37-encoded int64 back
// into the raw (lowercase, underscore-for-space) username string. value is always
// non-negative here since G8 reads it straight off the wire as the sender originally
// packed it via the mirror-image toUserhash, well within int64 range (the upper bound
// below, ~6.58e18, is less than math.MaxInt64).
func playerRawUsername(value int64) string {
	const maxValue = 6582952005840035281
	if value < 0 || value >= maxValue || value%37 == 0 {
		return "invalid_name"
	}

	var chars [12]byte
	n := 0
	for value != 0 {
		next := value / 37
		chars[11-n] = playerUsernameChars[value-next*37]
		value = next
		n++
	}
	return string(chars[12-n:])
}

// playerScreenName ports JString.toScreenName: replaces each '_' with a space
// (capitalising the letter that follows it) and capitalises the first character.
func playerScreenName(str string) string {
	if len(str) == 0 {
		return str
	}
	chars := []byte(str)
	for i := range chars {
		if chars[i] == '_' {
			chars[i] = ' '
			if i+1 < len(chars) && chars[i+1] >= 'a' && chars[i+1] <= 'z' {
				chars[i+1] = chars[i+1] - 'a' + 'A'
			}
		}
	}
	if chars[0] >= 'a' && chars[0] <= 'z' {
		chars[0] = chars[0] - 'a' + 'A'
	}
	return string(chars)
}

// Players returns a snapshot of every currently-visible other player in absolute world
// coordinates — matches NPCs()' exact shape and locking convention (see npc.go's NPCs()).
func (s *State) Players() []Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Player, 0, len(s.players))
	for index, e := range s.players {
		out = append(out, Player{
			Index:       index,
			Name:        e.name,
			X:           s.mapBuildBaseX + e.localX,
			Z:           s.mapBuildBaseZ + e.localZ,
			Level:       s.level,
			FaceEntity:  e.faceEntity,
			InCombat:    inCombatWindow(e.hitSeen, e.hitTick, s.scene.tick),
			CombatLevel: e.combatLevel,
		})
	}
	return out
}

// readPlayerCombat reads one hitmark block (damage, damage type, health, total health: four
// bytes, as the skip it replaced) into the render combat state of the player it belongs to, and
// returns the damage type for notePlayerHit.
func (s *State) readPlayerCombat(r *packet.Packet, index, tick int) (damageType int) {
	if index == -1 {
		return s.self.combat.read(r, tick)
	}
	if e := s.players[index]; e != nil {
		return e.combat.read(r, tick)
	}
	r.Skip(1) // damage
	damageType = int(r.G1())
	r.Skip(2) // health, totalHealth
	return damageType
}
