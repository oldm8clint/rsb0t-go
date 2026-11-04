// Package gamestate is the Go client's own model of what the server has told it — the
// part of Client.ts's state that ClientAdapter.ts reads out of a live rendering client
// (mainModalId, sideModalId, chatModalId, per-component inventories, component text,
// the local player's slot). A headless client has no live client to introspect, so it
// rebuilds this from the packet stream itself; State.Handle is the dispatcher.
//
// Scope: what Phase 2 (trade + bank, standing still) needs. Deliberately not here yet:
// PLAYER_INFO/NPC_INFO (the bit-packed entity update protocol — needed to find a trade
// partner's index by name; the two-account test learns indices from UPDATE_PID instead),
// zone/loc packets (needed to find a bank booth by scanning), and the interface archive
// (needed to resolve the bank's withdraw component the way ClientAdapter.bankComId does).
package gamestate

import (
	"sync"
	"sync/atomic"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// NoModal mirrors the client's -1 sentinel for "no interface open in this slot".
const NoModal = -1

type State struct {
	mu sync.RWMutex

	// soundTicks is the tick each SYNTH_SOUND id last played (sound.go); soundsHeard counts them.
	soundTicks  map[int]int
	soundsHeard int

	// closeSentTick is the observed tick (Tick) when this client last sent CLOSE_MODAL (LocalCloseModal), closeSent
	// whether one has been (modalclose.go).
	closeSentTick int
	closeSent     bool

	// subMu guards subscribers/nextSubID (see events.go) — kept separate from mu so
	// Subscribe/emit never need to take State's own data lock.
	subMu       sync.RWMutex
	subscribers map[int]Subscriber
	nextSubID   int

	MainModalID int
	SideModalID int
	ChatModalID int
	// OverlayID is IF_OPENOVERLAY's interface: drawn over the world viewport alongside
	// whatever main modal is open, not instead of it (see ifvisual.go).
	OverlayID int

	ResumedPauseButton bool
	DialogInputOpen    bool
	LoggedOut          bool
	Dead               bool

	SelfSlot       int
	MembersAccount bool

	mapBuildBaseX, mapBuildBaseZ int
	localX, localZ, level        int
	haveTile                     bool

	npcs   map[int]*npcEntry
	npcIDs []int
	// npcIDsSpare is the previous pass's list, kept to be built into next time; npcGen numbers the passes.
	npcIDsSpare []int
	npcGen      uint32
	players     map[int]*playerEntry
	playerIDs   []int
	// playerIDsSpare is the previous pass's list, kept to be built into next time; playerGen numbers the passes.
	playerIDsSpare []int
	playerGen      uint32
	// playerAppearanceNames caches the last decoded display name per player INDEX,
	// surviving that index leaving s.players entirely (a player going out of view, then
	// back in) — mirrors Client.ts's own playerAppearanceBuffer (getPlayerPosNewVis/
	// getPlayerPosDecodeExtended), a SEPARATE array from this.players that is never
	// cleared on visibility loss. See players.go's own doc comment on why this exists:
	// the server does not resend a player's APPEARANCE sub-block on every re-sighting of
	// the same index within one connection, only the first time (or when it actually
	// changes) — relying on the client to remember it itself.
	playerAppearanceNames map[int]string
	playerAppearances     map[int][]byte
	locs                  map[locKey]*LocInstance
	effects               []RenderEffect
	// renderTick is the scene tick of the last RenderSnapshot (watchRender), 0 never.
	renderTick               atomic.Int64
	groundItems              map[groundItemKey][]GroundItem
	zoneUpdateX, zoneUpdateZ int

	inventories map[int]*Inventory
	texts       map[int]string
	skills      Skills

	varps   map[int]int
	colours map[int]Colour

	sideIcons map[int]int // sidebar tab index -> the interface id currently loaded there, via IF_SETICON
	ifObjects map[int]int // component id -> object id from IF_SETOBJECT; see ifobject.go
	// The look-only interface packets, all in ifvisual.go: head models (IF_SETNPCHEAD /
	// IF_SETPLAYERHEAD), component animations (IF_SETANIM), hidden components
	// (IF_SETHIDE), scroll positions (IF_SETSCROLLPOS) and moved components
	// (IF_SETPOSITION). Lazily allocated, since most accounts never see most of them.
	ifHeads     map[int]InterfaceHead
	ifAnims     map[int]int
	ifHidden    map[int]bool
	ifScroll    map[int]int
	ifPositions map[int]InterfacePoint
	self        selfTracker  // the local player's extended-info state; see self.go
	hint        hintArrow    // HINT_ARROW; see hint.go
	chat        chatTracker  // public chat, private messages and the local SAY; see chat.go
	scene       sceneTracker // scene build state, server ticks, run energy and weight; see scene.go
	hpMaxSeen   int          // high-water mark of the Hitpoints stat's current level; see health.go's MaxHP doc comment for why this, not the XP curve, is authoritative

	// mapFlagUnset counts UNSET_MAP_FLAG packets — the server's own real "that click
	// didn't work" signal (npctest's live-verified "can't reach that" rejection, and
	// per the local engine's own MoveClickHandler.ts, ALSO what it sends whenever the
	// click's own stated start tile is more than 104 tiles from the player's real
	// server-side position: the whole click is silently dropped, no other response at
	// all). Never decoded anywhere in this client until 2026-09-15, while chasing the
	// long-standing position-desync bug (see COLLAB.md) — every walk click this client
	// ever sent could have been silently rejected by that exact 104-tile check with
	// zero visibility, which would look identical to every "FindPath says clear, the
	// account just never moves" symptom hunted all session. A plain counter, not an
	// event: callers that care snapshot it before sending a click and compare after.
	mapFlagUnset int

	// deaths is the hitpoints ring and the last death's context; see death.go.
	deaths deathTracker
}

func New() *State {
	return &State{
		MainModalID:           NoModal,
		SideModalID:           NoModal,
		ChatModalID:           NoModal,
		OverlayID:             NoModal,
		SelfSlot:              -1,
		inventories:           make(map[int]*Inventory),
		texts:                 make(map[int]string),
		npcs:                  make(map[int]*npcEntry),
		players:               make(map[int]*playerEntry),
		playerAppearanceNames: make(map[int]string),
		playerAppearances:     make(map[int][]byte),
		locs:                  make(map[locKey]*LocInstance),
		groundItems:           make(map[groundItemKey][]GroundItem),
		sideIcons:             make(map[int]int),
	}
}

func (s *State) Handle(opcode int, payload []byte) {
	s.emit(s.handleLocked(opcode, payload))
}

// handleLocked decodes one packet under the write lock and returns the events to emit. The
// unlock is deferred: live 3 Oct 15:51 a decoder panic (client.handleSafely recovers it) left
// the lock held for good, and every reader of that account's state, the control panel's
// status snapshot among them, blocked behind it.
func (s *State) handleLocked(opcode int, payload []byte) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := packet.NewReader(payload)

	// Captured before the switch so any opcode that touches a modal field (including
	// ones added later) is covered by one comparison after, rather than needing its
	// own case to know it changed something — see EventModalChanged's doc comment.
	prevMain, prevSide, prevChat := s.MainModalID, s.SideModalID, s.ChatModalID
	var events []Event

	switch opcode {
	case serverprot.IF_OPENMAIN:
		comID := int(r.G2())
		s.SideModalID = NoModal
		s.ChatModalID = NoModal
		s.DialogInputOpen = false
		s.MainModalID = comID
		s.ResumedPauseButton = false
	case serverprot.IF_OPENSIDE:
		comID := int(r.G2())
		s.ChatModalID = NoModal
		s.DialogInputOpen = false
		s.SideModalID = comID
		s.MainModalID = NoModal
		s.ResumedPauseButton = false
	case serverprot.IF_OPENCHAT:
		comID := int(r.G2())
		s.SideModalID = NoModal
		s.ChatModalID = comID
		s.MainModalID = NoModal
		s.ResumedPauseButton = false
	case serverprot.IF_OPENMAIN_SIDE:
		mainID := int(r.G2())
		sideID := int(r.G2())
		s.ChatModalID = NoModal
		s.DialogInputOpen = false
		s.MainModalID = mainID
		s.SideModalID = sideID
		s.ResumedPauseButton = false
	case serverprot.IF_CLOSE:
		s.SideModalID = NoModal
		s.ChatModalID = NoModal
		s.DialogInputOpen = false
		s.MainModalID = NoModal
		s.ResumedPauseButton = false
	case serverprot.P_COUNTDIALOG:
		s.DialogInputOpen = true
	case serverprot.UPDATE_PID:
		s.SelfSlot = int(r.G2())
		s.MembersAccount = r.G1() == 1
	case serverprot.IF_SETTEXT:
		comID := int(r.G2())
		s.texts[comID] = r.GJStr()
	case serverprot.UPDATE_INV_FULL:
		comID := int(r.G2())
		events = append(events, s.inventory(comID).applyFull(comID, r)...)
	case serverprot.UPDATE_INV_PARTIAL:
		comID := int(r.G2())
		events = append(events, s.inventory(comID).applyPartial(comID, r)...)
	case serverprot.UPDATE_STAT:
		events = append(events, s.applyUpdateStat(r)...)
	case serverprot.REBUILD_NORMAL:
		s.applyRebuildNormal(r)
	case serverprot.PLAYER_INFO:
		s.applyPlayerInfoLocal(r)
		s.applyPlayerInfoOthers(r)
	case serverprot.NPC_INFO:
		s.applyNpcInfo(r)
	case serverprot.UPDATE_ZONE_FULL_FOLLOWS:
		s.applyZoneFollows(r, true)
	case serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS:
		s.applyZoneFollows(r, false)
	case serverprot.UPDATE_ZONE_PARTIAL_ENCLOSED:
		s.applyZoneEnclosed(r)
	case serverprot.LOC_ADD_CHANGE:
		s.applyLocAddChange(r)
	case serverprot.LOC_DEL:
		s.applyLocDel(r)
	case serverprot.UNSET_MAP_FLAG:
		s.mapFlagUnset++
	case serverprot.SYNTH_SOUND:
		s.applySynthSound(r)
	case serverprot.MESSAGE_GAME:
		text := r.GJStr()
		events = append(events, gameChatEvent(text))
		if s.checkDeathLine(text) {
			events = append(events, Event{Kind: EventDeath})
		}
	case serverprot.MESSAGE_PRIVATE:
		events = append(events, s.applyMessagePrivate(r)...)
	case serverprot.MAP_PROJANIM:
		s.applyEffect(r, true)
	case serverprot.MAP_ANIM:
		s.applyEffect(r, false)
	case serverprot.OBJ_ADD:
		s.applyObjAdd(r)
	case serverprot.OBJ_DEL:
		s.applyObjDel(r)
	case serverprot.OBJ_COUNT:
		s.applyObjCount(r)
	case serverprot.OBJ_REVEAL:
		s.applyObjReveal(r)
	case serverprot.VARP_SMALL:
		events = append(events, s.applyVarpSmall(r)...)
	case serverprot.VARP_LARGE:
		events = append(events, s.applyVarpLarge(r)...)
	case serverprot.VARP_SYNC: // no payload; see varp.go's doc comment for why this is a deliberate no-op
	case serverprot.IF_SETCOLOUR:
		s.applyIfSetColour(r)
	case serverprot.IF_SETICON:
		s.applyIfSetIcon(r)
	case serverprot.IF_SETOBJECT:
		s.applyIfSetObject(r)
	case serverprot.IF_SETMODEL:
		s.applyIfSetModel(r)
	case serverprot.IF_SETNPCHEAD:
		s.applyIfSetNpcHead(r)
	case serverprot.IF_SETPLAYERHEAD:
		s.applyIfSetPlayerHead(r)
	case serverprot.IF_SETANIM:
		s.applyIfSetAnim(r)
	case serverprot.IF_SETHIDE:
		s.applyIfSetHide(r)
	case serverprot.IF_OPENOVERLAY:
		s.applyIfOpenOverlay(r)
	case serverprot.IF_SETSCROLLPOS:
		s.applyIfSetScrollPos(r)
	case serverprot.IF_SETPOSITION:
		s.applyIfSetPosition(r)
	case serverprot.HINT_ARROW:
		s.applyHintArrow(r)
	case serverprot.LOGOUT:
		s.LoggedOut = true
	}
	// Chat lines the PLAYER_INFO extended decode queued (chat.go).
	events = append(events, s.takeChatEvents()...)
	// Scene state, server ticks, run energy and weight (scene.go) read their own copy of
	// the payload, so the cases above stay as they are.
	if tradeModal(prevMain) && !tradeModal(s.MainModalID) {
		s.clearTradeInventories()
	}
	events = append(events, s.observeScene(opcode, payload)...)

	if s.MainModalID != prevMain || s.SideModalID != prevSide || s.ChatModalID != prevChat {
		events = append(events, Event{
			Kind: EventModalChanged,
			Main: s.MainModalID, Side: s.SideModalID, Chat: s.ChatModalID,
			PreviousMain: prevMain, PreviousSide: prevSide, PreviousChat: prevChat,
		})
	}

	s.rememberRenderChat(events)
	return events
}

// MapFlagUnsetCount returns how many UNSET_MAP_FLAG packets this session has ever seen —
// a plain monotonic counter, not an event queue. A caller that wants to know whether ITS
// OWN walk click was rejected snapshots this immediately before calling actions.Walk and
// compares after: if it changed, the server dropped the click (see this field's own doc
// comment on State for why, and on MoveClickHandler.ts's 104-tile stated-start check).
func (s *State) MapFlagUnsetCount() int { s.mu.RLock(); defer s.mu.RUnlock(); return s.mapFlagUnset }

func (s *State) Tile() (x, z, level int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.haveTile {
		return 0, 0, 0, false
	}
	return s.mapBuildBaseX + s.localX, s.mapBuildBaseZ + s.localZ, s.level, true
}

func (s *State) inventory(comID int) *Inventory {
	inv, ok := s.inventories[comID]
	if !ok {
		inv = &Inventory{}
		s.inventories[comID] = inv
	}
	return inv
}

func (s *State) Inventory(comID int) *Inventory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.inventories[comID]
	if !ok {
		return nil
	}
	cp := &Inventory{Generation: inv.Generation, FullGeneration: inv.FullGeneration}
	cp.Slots = append([]Item(nil), inv.Slots...)
	return cp
}

func (s *State) Text(comID int) string { s.mu.RLock(); defer s.mu.RUnlock(); return s.texts[comID] }

// applyIfSetIcon ports the IF_SETICON handler (Client.ts: `this.sideIcon[icon] = comId`,
// with the wire's 65535 standing in for -1/"nothing loaded there"): comId:u2, icon:u1.
// This is how the real client learns which interface occupies each sidebar tab slot —
// tab 3 is the backpack, tab 4 is Worn Equipment — rather than either ever being a fixed
// literal the way Trade's ids are.
func (s *State) applyIfSetIcon(r *packet.Packet) {
	comID := int(r.G2())
	icon := int(r.G1())
	if comID == 65535 {
		comID = -1
	}
	s.sideIcons[icon] = comID
}

// SideIcon returns the interface id currently loaded into sidebar tab `icon` (see
// applyIfSetIcon), or -1 if IF_SETICON hasn't reported one yet.
func (s *State) SideIcon(icon int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	comID, ok := s.sideIcons[icon]
	if !ok {
		return -1
	}
	return comID
}

func (s *State) KnownInventories() map[int][]SlotItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int][]SlotItem, len(s.inventories))
	for comID, inv := range s.inventories {
		if ne := inv.NonEmpty(); len(ne) > 0 {
			out[comID] = ne
		}
	}
	return out
}

func (s *State) Modals() (main, side, chat int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MainModalID, s.SideModalID, s.ChatModalID
}
func (s *State) Self() int             { s.mu.RLock(); defer s.mu.RUnlock(); return s.SelfSlot }
func (s *State) CountDialogOpen() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.DialogInputOpen }

// IsLoggedOut reports whether the server has sent ServerProt.LOGOUT (opcode 121) — its
// reply to a logout button click (see internal/actions.Logout), telling the client the
// session is over server-side and the socket can close. Never reset back to false:
// once true, this Session is done.
func (s *State) IsLoggedOut() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.LoggedOut }

// IsDead reports whether the game's own death chat line ("Oh dear, you are dead!",
// matched via internal/scripts/fighterfood.IsDeathLine) has arrived via MESSAGE_GAME.
// Unlike IsLoggedOut, death isn't terminal in this game (the player respawns at
// Lumbridge), so this can be reset — see ClearDead — once a caller has handled it.
func (s *State) IsDead() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.Dead }

// ClearDead resets IsDead back to false, e.g. once a script's death-recovery logic has
// finished re-equipping at Lumbridge and is ready to detect the next death.
func (s *State) ClearDead()              { s.mu.Lock(); s.Dead = false; s.mu.Unlock() }
func (s *State) MarkPauseButtonResumed() { s.mu.Lock(); s.ResumedPauseButton = true; s.mu.Unlock() }
func (s *State) PauseButtonResumed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ResumedPauseButton
}
func (s *State) ClearDialogInput() { s.mu.Lock(); s.DialogInputOpen = false; s.mu.Unlock() }

// LocalCloseModal applies the client's own optimistic "I closed this" without
// waiting for a server IF_CLOSE, and — like every modal-touching path in Handle —
// emits EventModalChanged if the (Main, Side, Chat) triple actually changed.
func (s *State) LocalCloseModal() {
	s.mu.Lock()
	prevMain, prevSide, prevChat := s.MainModalID, s.SideModalID, s.ChatModalID
	s.SideModalID = NoModal
	s.ChatModalID = NoModal
	s.MainModalID = NoModal
	s.ResumedPauseButton = false
	s.closeSentTick, s.closeSent = s.scene.tick, true
	if tradeModal(prevMain) {
		s.clearTradeInventories()
	}
	var events []Event
	if s.MainModalID != prevMain || s.SideModalID != prevSide || s.ChatModalID != prevChat {
		events = append(events, Event{
			Kind: EventModalChanged,
			Main: s.MainModalID, Side: s.SideModalID, Chat: s.ChatModalID,
			PreviousMain: prevMain, PreviousSide: prevSide, PreviousChat: prevChat,
		})
	}
	s.mu.Unlock()
	s.emit(events)
}
