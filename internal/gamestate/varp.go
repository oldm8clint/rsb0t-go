package gamestate

import "rsb0t-go/internal/packet"

// Varps and IF_SETCOLOUR
//
// Client.ts decodes three opcodes for player variables: VARP_SMALL (varpId:u16,
// value:i8), VARP_LARGE (varpId:u16, value:i32) and VARP_SYNC (no payload — tells the
// client to copy its own server-shadow array over its locally-scripted var array,
// only meaningful for interface CS2 scripts that can mutate a var without a server
// round-trip, e.g. a checkbox toggle). This headless client never runs interface
// scripts, so its own var[] can never diverge from what the server told it — VARP_SYNC
// is a deliberate no-op here, not an unhandled opcode.
//
// Not every varp reaches the client at all: content marks each varp `transmit=yes` or
// leaves it untransmitted (the default). The engine's Player.setVar only calls
// writeVarp (which picks VARP_SMALL for -128..127, VARP_LARGE otherwise) when
// varp.transmit is true (engine/src/engine/entity/Player.ts). Most quest-progress
// varps (cookquest, zanaris, waterfall_quest, elemental_workshop_bits, ...) are
// `scope=perm` with no `transmit=yes` line at all (checked against
// content/scripts/quests/*/configs/*.varp) — an untransmitted varp never appears on
// this wire and Varp() below reads back its zero-value default, indistinguishable from
// "never told". That is real server-side silence, not a decode gap: see
// internal/quests's doc comment for how quest stage is actually read instead.
//
// IF_SETCOLOUR (comId:u16, colour:u16 packed 5-5-5 RGB) is decoded here too because
// it is quest status's actual real transmitted signal: general/scripts/quests.rs2's
// send_quest_progress_colour proc sets the quest tab's per-quest text component to
// red/yellow/green (not-started/in-progress/complete) via this exact opcode, and never
// touches a varp the client could read instead.

// Colour is a decoded IF_SETCOLOUR value, kept in its native 5-bit-per-channel wire
// form (0-31 per channel) rather than rescaled to 0-255, since callers only need to
// classify it (see internal/quests.Classify), not render it.
type Colour struct {
	R, G, B int
}

// applyVarpSmall decodes VARP_SMALL: varpId:u16, value:i8 (sign-extended).
func (s *State) applyVarpSmall(r *packet.Packet) []Event {
	id := int(r.G2())
	value := int(r.G1b())
	return s.setVarp(id, value)
}

// applyVarpLarge decodes VARP_LARGE: varpId:u16, value:i32.
func (s *State) applyVarpLarge(r *packet.Packet) []Event {
	id := int(r.G2())
	value := int(r.G4())
	return s.setVarp(id, value)
}

// setVarp stores value and returns an EventVarpChanged when it differs from the previous
// value (0 for a varp never told, matching the client's zero-filled var array).
func (s *State) setVarp(id, value int) []Event {
	if s.varps == nil {
		s.varps = make(map[int]int)
	}
	previous := s.varps[id]
	s.varps[id] = value
	if previous == value {
		return nil
	}
	return []Event{{Kind: EventVarpChanged, Varp: id, Value: value, PreviousValue: previous}}
}

// applyIfSetColour decodes IF_SETCOLOUR: comId:u16, colour:u16 (5 bits red, 5 bits
// green, 5 bits blue — see Client.ts's own IF_SETCOLOUR handler, ported verbatim).
func (s *State) applyIfSetColour(r *packet.Packet) {
	comID := int(r.G2())
	packed := int(r.G2())
	c := Colour{R: (packed >> 10) & 0x1f, G: (packed >> 5) & 0x1f, B: packed & 0x1f}
	if s.colours == nil {
		s.colours = make(map[int]Colour)
	}
	s.colours[comID] = c
}

// Varp returns the last value this session was told for player variable id, or 0 if
// the server never sent one (either because nothing changed it yet, or because
// content never marks it transmit=yes — see this file's doc comment; the two cases
// are indistinguishable from the wire alone, matching the real client's own
// limitation, not an added one).
func (s *State) Varp(id int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.varps[id]
}

// VarpKnown is Varp plus whether this session has actually received a VARP_SMALL/
// VARP_LARGE for id at all, for callers that need to distinguish "told 0" from
// "never told" (Varp alone cannot, since both read back as the zero value).
func (s *State) VarpKnown(id int) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.varps[id]
	return v, ok
}

// Colour returns the last IF_SETCOLOUR value received for component id.
func (s *State) Colour(comID int) (Colour, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.colours[comID]
	return c, ok
}
