package gamestate

import "rsb0t-go/internal/packet"

// npcEntry is one tracked NPC's live state.
type npcEntry struct {
	combat        RenderCombat
	typeID        int
	localX        int
	localZ        int
	firstSeenTick int

	// From the extended-info block (see applyNpcInfoExtended).
	anim                int // sequence id, -1 for none
	animTick            int
	faceEntity          int // -1 for none; a player is slot + 32768
	speech              string
	speechLog           []string // copy-on-write: a snapshot's slice is never written again
	health, totalHealth int
	hitTick             int
	hitSeen             bool

	// mark is the NPC_INFO pass (State.npcGen) that last listed this NPC; an entry not marked by the current
	// pass is gone.
	mark uint32
}

// NPC is a snapshot of one visible NPC, in absolute world coordinates.
type NPC struct {
	Index int
	Type  int
	X, Z  int
	Level int

	// Anim is the last animation the server started on this NPC, -1 for none. The client clears
	// it when the sequence's frames run out, which a headless client cannot time, so it can
	// outlast the animation itself.
	Anim int
	// FaceEntity is who the NPC faces: -1 for nobody, an NPC index below 32768, or a player's
	// slot plus 32768.
	FaceEntity int
	// AnimTick is the scene tick Anim arrived on, so a renderer (and the KQ prayer flick, which
	// matches her magic animation to this account's hitsplats) can tell a restarted sequence
	// from the same one still playing.
	AnimTick int
	// VisibleTicks is the number of player-update ticks since this NPC entered view.
	VisibleTicks int
	// Speech is the latest server-authored overhead message. Random-event
	// greetings can identify their owner even before a face-entity update.
	Speech string
	// SpeechLog is every distinct overhead line this client has seen the NPC say since it
	// entered view, oldest first, the newest maxSpeechLog of them; Speech is its last entry.
	// A random-event NPC names its owner in its greeting and in each nag, and its owner is
	// fixed at spawn, but its later lines need not name anyone ("I lose more friends that
	// way.") or may be worded differently on production — so a line that replaces the
	// greeting must not erase what the greeting said.
	SpeechLog []string
	// Health and TotalHealth come from the last hitmark on this NPC; TotalHealth is 0 until one
	// has arrived.
	Health, TotalHealth int
	// InCombat ports the client's combatCycle window: a hitmark within the last
	// combatWindowTicks ticks.
	InCombat bool
	// Combat is the hitmark and spot-animation state a renderer draws. Only RenderSnapshot sets
	// it: NPCs(), which every script and the Guardian read several times a second on every
	// account, leaves it nil rather than copying about 200 bytes per NPC nobody draws.
	Combat *RenderCombat `json:"Combat,omitempty"`
}

// playerFaceBase is added to a player's slot in a face-entity value.
const playerFaceBase = 32768

// TargetsPlayer reports whether the NPC faces the player in slot (Npc.targetsMe when slot is
// State.Self()).
func (n NPC) TargetsPlayer(slot int) bool {
	return n.FaceEntity >= playerFaceBase && n.FaceEntity-playerFaceBase == slot
}

// TargetsAnotherPlayer ports Npc.targetsAnotherPlayer: it faces a player other than slot.
func (n NPC) TargetsAnotherPlayer(slot int) bool {
	return n.FaceEntity >= playerFaceBase && n.FaceEntity-playerFaceBase != slot
}

// NpcUpdate mask bits, ClientNpc.ts's enum, in the order getNpcPosExtended reads them.
const (
	npcUpdateHitmark2   = 0x1
	npcUpdateAnim       = 0x2
	npcUpdateFaceEntity = 0x4
	npcUpdateSay        = 0x8
	npcUpdateHitmark    = 0x10
	npcUpdateChangeType = 0x20
	npcUpdateSpotAnim   = 0x40
	npcUpdateFaceSquare = 0x80
)

// applyNpcInfo ports getNpcPos: getNpcPosOldVis, getNpcPosNewVis, then getNpcPosExtended. Unlike
// PLAYER_INFO, every visible NPC's position genuinely matters here (there's no "only decode our
// own entity" shortcut), so this maintains the same ordered active-index list the original client
// keeps, tick over tick, to correctly interpret each OldVis slot's index, and queues the same
// extended-update list: OldVis op 0 always queues, ops 1 and 2 and every NewVis entry queue when
// their extended-info bit is set.
func (s *State) applyNpcInfo(r *packet.Packet) {
	r.GBitStart()

	count := int(r.GBit(8))
	oldIDs := s.npcIDs
	if count > len(oldIDs) {
		// Matches the client's own "eek! too many npcs" guard turning into a hard
		// stop rather than reading garbage past what we actually have.
		return
	}

	// The list being built reuses the buffer from two passes ago, so a tick allocates nothing for it.
	newIDs := s.npcIDsSpare[:0]
	var updated []int
	for i := 0; i < count; i++ {
		index := oldIDs[i]
		if r.GBit(1) == 0 {
			newIDs = append(newIDs, index)
			continue
		}
		switch r.GBit(2) {
		case 0:
			newIDs = append(newIDs, index)
			updated = append(updated, index)
		case 1:
			dir := int(r.GBit(3))
			if e := s.npcs[index]; e != nil {
				e.localX += walkDX[dir]
				e.localZ += walkDZ[dir]
			}
			if r.GBit(1) == 1 {
				updated = append(updated, index)
			}
			newIDs = append(newIDs, index)
		case 2:
			dir := int(r.GBit(3))
			if e := s.npcs[index]; e != nil {
				e.localX += walkDX[dir]
				e.localZ += walkDZ[dir]
			}
			dir = int(r.GBit(3))
			if e := s.npcs[index]; e != nil {
				e.localX += walkDX[dir]
				e.localZ += walkDZ[dir]
			}
			if r.GBit(1) == 1 {
				updated = append(updated, index)
			}
			newIDs = append(newIDs, index)
		case 3:
			delete(s.npcs, index)
		}
	}

	for r.BitPos+21 < len(r.Data)*8 {
		index := int(r.GBit(14))
		if index == 16383 {
			break
		}
		typeID := int(r.GBit(11))
		dx := int(r.GBit(5))
		if dx > 15 {
			dx -= 32
		}
		dz := int(r.GBit(5))
		if dz > 15 {
			dz -= 32
		}
		_ = r.GBit(1) // jump flag — irrelevant to position tracking.

		// Like the client's npc[] array, an index keeps its entity (and so its last
		// hitmark and face target) if it is still tracked.
		e := s.npcs[index]
		if e == nil {
			e = &npcEntry{anim: -1, faceEntity: -1, firstSeenTick: s.scene.tick}
			s.npcs[index] = e
		}
		if e.typeID != typeID {
			e.speech, e.speechLog = "", nil // a different NPC now holds this index
		}
		e.typeID = typeID
		e.localX = s.localX + dx
		e.localZ = s.localZ + dz
		newIDs = append(newIDs, index)

		if r.GBit(1) == 1 {
			updated = append(updated, index)
		}
	}
	r.GBitEnd()
	s.applyNpcInfoExtended(r, updated)

	s.npcGen++
	gen := s.npcGen
	for _, id := range newIDs {
		if e := s.npcs[id]; e != nil {
			e.mark = gen
		}
	}
	for id, e := range s.npcs {
		if e.mark != gen {
			delete(s.npcs, id)
		}
	}
	s.npcIDsSpare = oldIDs
	s.npcIDs = newIDs
}

// applyNpcInfoExtended ports getNpcPosExtended: one mask byte per queued NPC, then each present
// field in mask-bit order. Only what the scripting API reads is kept (animation, face target,
// hitmark health and time, a type change); the rest is skipped by its exact length so later NPCs
// stay aligned. Two simplifications, both only affecting the kept values: a new animation always
// replaces the current one (the client compares sequence priorities, which need the seq config),
// and hitmark time is the server tick rather than the client's frame cycle. It stops quietly if
// the payload runs short.
func (s *State) applyNpcInfoExtended(r *packet.Packet, updated []int) {
	for _, index := range updated {
		e := s.npcs[index]
		if e == nil {
			// getNpcPosExtended skips a missing NPC before reading its mask; so does this.
			continue
		}
		if r.Available() < 1 {
			return
		}
		mask := r.G1()

		if mask&npcUpdateHitmark2 != 0 {
			if !s.readNpcHitmark(r, e) {
				return
			}
		}
		if mask&npcUpdateAnim != 0 {
			if r.Available() < 3 {
				return
			}
			seq := int(r.G2())
			r.G1() // delay
			if seq == 65535 {
				seq = -1
			}
			e.anim, e.animTick = seq, s.scene.tick
		}
		if mask&npcUpdateFaceEntity != 0 {
			if r.Available() < 2 {
				return
			}
			face := int(r.G2())
			if face == 65535 {
				face = -1
			}
			e.faceEntity = face
		}
		if mask&npcUpdateSay != 0 {
			e.speech = r.GJStr()
			e.speechLog = appendSpeech(e.speechLog, e.speech)
		}
		if mask&npcUpdateHitmark != 0 {
			if !s.readNpcHitmark(r, e) {
				return
			}
		}
		if mask&npcUpdateChangeType != 0 {
			if r.Available() < 2 {
				return
			}
			if t := int(r.G2()); t != e.typeID {
				// Only what the OLD type said goes: a line said in this same update block (SAY is
				// read before CHANGETYPE) is the NPC as it now is.
				e.typeID, e.speechLog = t, nil
				if mask&npcUpdateSay != 0 {
					e.speechLog = appendSpeech(nil, e.speech)
				} else {
					e.speech = ""
				}
			}
		}
		if mask&npcUpdateSpotAnim != 0 {
			if r.Available() < 6 {
				return
			}
			e.combat.readSpot(r, s.scene.tick)
		}
		if mask&npcUpdateFaceSquare != 0 {
			if r.Available() < 4 {
				return
			}
			r.Skip(4)
		}
	}
}

// maxSpeechLog bounds NPC.SpeechLog. A random-event NPC says at most six lines (a greeting,
// up to four nags, then its fail or despawn line), and an ordinary NPC's chatter only needs
// to not grow without bound.
const maxSpeechLog = 8

// appendSpeech returns log with line appended, unless line repeats the newest entry. It
// always builds a new slice, so a snapshot holding the old one never sees it change.
func appendSpeech(log []string, line string) []string {
	if n := len(log); n > 0 && log[n-1] == line {
		return log
	}
	if len(log) >= maxSpeechLog {
		log = log[len(log)-maxSpeechLog+1:]
	}
	out := make([]string, len(log), len(log)+1)
	copy(out, log)
	return append(out, line)
}

// readNpcHitmark reads damage(1) type(1) health(1) totalHealth(1); false when the payload is short.
func (s *State) readNpcHitmark(r *packet.Packet, e *npcEntry) bool {
	if r.Available() < 4 {
		return false
	}
	e.combat.read(r, s.scene.tick)
	e.health, e.totalHealth = e.combat.Health, e.combat.Total
	e.hitTick, e.hitSeen = s.scene.tick, true
	return true
}

// NPCs returns a snapshot of every currently-visible NPC in absolute world coordinates.
func (s *State) NPCs() []NPC {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]NPC, 0, len(s.npcs))
	for index, e := range s.npcs {
		out = append(out, NPC{
			Index:        index,
			Type:         e.typeID,
			X:            s.mapBuildBaseX + e.localX,
			Z:            s.mapBuildBaseZ + e.localZ,
			Level:        s.level,
			Anim:         e.anim,
			AnimTick:     e.animTick,
			FaceEntity:   e.faceEntity,
			VisibleTicks: max(0, s.scene.tick-e.firstSeenTick),
			Speech:       e.speech,
			SpeechLog:    e.speechLog,
			Health:       e.health,
			TotalHealth:  e.totalHealth,
			InCombat:     inCombatWindow(e.hitSeen, e.hitTick, s.scene.tick),
		})
	}
	return out
}

// max shadows the Go 1.21 builtin of the same name, which this repo cannot use: CLAUDE.md
// pins the build to Go 1.19.3, where it does not exist. internal/pathfind/astar.go carries
// the same helper for the same reason.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
