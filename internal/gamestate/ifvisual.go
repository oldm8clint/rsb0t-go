package gamestate

import "rsb0t-go/internal/packet"

// The interface packets that change how a component LOOKS rather than what it says: the head
// model in a dialogue box, the animation played on it, whether a component is hidden, an
// overlay interface, a scroll position and a moved component.
//
// Until 2026-09-21 this client decoded ten of the eighteen IF_* server opcodes and silently
// dropped the rest, which is why the stream client's dialogue boxes drew the speaker's name
// and text with an EMPTY head box: the engine really does send if_setnpchead + if_setanim for
// every page of every NPC dialogue (content/scripts/interface_chat/scripts/chat.rs2's
// chatnpc_page; 10,979 chatnpc and 7,279 chatplayer call sites in content 289), and nothing
// here was reading them.
//
// Every handler below is ported field for field from the reference client's own
// Client.ts handlers (src/client/shell/Client.ts, the checkout CLAUDE.md names as the
// protocol authority) — see each function's comment for the lines it mirrors. Like the
// reference, none of these is ever reset on its own: IfType.list is a global the client
// keeps until the server changes it again, so the maps here persist for the session too.
//
// What is deliberately NOT decoded: IF_SHOWICON (189) and TUT_OPEN/TUT_FLASH (12/181). The
// engine checkout contains no sender for IF_SHOWICON at all, and the tutorial packets only
// matter on Tutorial Island, which no fleet account is on.

// InterfaceHead is the head model IF_SETNPCHEAD or IF_SETPLAYERHEAD put on a component.
// Type mirrors the reference client's IfType.model1Type: 2 is an npc head (ID is the npc
// type), 3 is the local player's own head. The player's model id is the local player's
// appearance packed exactly as Client.ts packs it, which needs the decoded appearance block
// rather than this packet; a renderer already has that player and builds it itself, so Type 3
// carries no ID here and ID is left at 0.
type InterfaceHead struct {
	Type int `json:"type"`
	ID   int `json:"id"`
}

// InterfacePoint is the (x, y) IF_SETPOSITION moved a component to.
type InterfacePoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// applyIfSetNpcHead decodes IF_SETNPCHEAD (comId:u2, npcId:u2) — Client.ts sets
// IfType.list[comId].model1Type = 2 and model1Id = npcId.
func (s *State) applyIfSetNpcHead(r *packet.Packet) {
	comID := int(r.G2())
	npcID := int(r.G2())
	s.setHead(comID, InterfaceHead{Type: 2, ID: npcID})
}

// applyIfSetPlayerHead decodes IF_SETPLAYERHEAD (comId:u2) — Client.ts sets model1Type = 3
// and packs the local player's own appearance into model1Id.
func (s *State) applyIfSetPlayerHead(r *packet.Packet) {
	s.setHead(int(r.G2()), InterfaceHead{Type: 3})
}

func (s *State) setHead(comID int, h InterfaceHead) {
	if s.ifHeads == nil {
		s.ifHeads = make(map[int]InterfaceHead)
	}
	s.ifHeads[comID] = h
}

// applyIfSetAnim decodes IF_SETANIM (comId:u2, seqId:i2). Client.ts stores it as
// IfType.modelAnim and resets the component's animation counters when it is -1.
func (s *State) applyIfSetAnim(r *packet.Packet) {
	comID := int(r.G2())
	seqID := int(r.G2b())
	if s.ifAnims == nil {
		s.ifAnims = make(map[int]int)
	}
	s.ifAnims[comID] = seqID
}

// applyIfSetHide decodes IF_SETHIDE (comId:u2, hide:u1) — Client.ts sets IfType.hide.
func (s *State) applyIfSetHide(r *packet.Packet) {
	comID := int(r.G2())
	hide := r.G1() == 1
	if s.ifHidden == nil {
		s.ifHidden = make(map[int]bool)
	}
	s.ifHidden[comID] = hide
}

// applyIfOpenOverlay decodes IF_OPENOVERLAY (comId:i2) — the interface drawn OVER the world
// viewport (wilderness level, multiway combat, minigame overlays), which is a separate slot
// from the main modal and does not close it. -1 clears it.
func (s *State) applyIfOpenOverlay(r *packet.Packet) {
	s.OverlayID = int(r.G2b())
}

// applyIfSetScrollPos decodes IF_SETSCROLLPOS (comId:u2, pos:u2). The reference clamps it to
// the component's own scrollHeight - height, which needs the interface archive; the raw
// server value is kept here and the renderer clamps it the same way the client does.
func (s *State) applyIfSetScrollPos(r *packet.Packet) {
	comID := int(r.G2())
	pos := int(r.G2())
	if s.ifScroll == nil {
		s.ifScroll = make(map[int]int)
	}
	s.ifScroll[comID] = pos
}

// applyIfSetPosition decodes IF_SETPOSITION (comId:u2, x:i2, y:i2) — Client.ts sets
// IfType.x and IfType.y.
func (s *State) applyIfSetPosition(r *packet.Packet) {
	comID := int(r.G2())
	x := int(r.G2b())
	y := int(r.G2b())
	if s.ifPositions == nil {
		s.ifPositions = make(map[int]InterfacePoint)
	}
	s.ifPositions[comID] = InterfacePoint{X: x, Y: y}
}

// InterfaceHeadOf returns the head model last set on comID, if any.
func (s *State) InterfaceHeadOf(comID int) (InterfaceHead, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.ifHeads[comID]
	return h, ok
}

// InterfaceHidden reports whether IF_SETHIDE last hid comID. A component the server never
// mentioned keeps the interface archive's own default, so ok is false for it.
func (s *State) InterfaceHidden(comID int) (hidden bool, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hidden, ok = s.ifHidden[comID]
	return hidden, ok
}

// OverlayModalID returns the IF_OPENOVERLAY interface currently drawn over the viewport, or
// NoModal when there is none.
func (s *State) OverlayModalID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.OverlayID
}
