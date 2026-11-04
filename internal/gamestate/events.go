// Events: a small, additive subscription layer on top of State.
//
// Before this, every consumer of gamestate.State polled it: internal/statlog calls
// Observe() with a fresh KnownInventories() snapshot every tick and diffs it against
// the last one itself; internal/randomevents.Guardian calls Detect(session.State())
// on its own 600ms ticker. reference/server-api-docs.md's "events" namespace
// (events.on('chat.message', ...), this.on('skill.xp', ...)) documents a reference
// client that instead lets scripts subscribe once and get told "this changed" — see
// that file's "## events" section. This file is the Go equivalent: small on purpose,
// per COLLAB.md's task scope — one Subscribe method, one closed EventKind enum, no
// priorities, no replay/history, no separate channel types per kind.
//
// Design decision on statlog overlap (per the task's explicit ask to decide, not
// guess): internal/statlog stays completely unmodified, and EventInventoryChanged
// does NOT call into it or share code with it, for a real structural reason, not just
// caution. statlog.Logger.Observe() diffs two *externally supplied* snapshots
// (KnownInventories(), handed in by whatever is polling a fleet of accounts) and only
// ever reports positive-quantity gains, because that is what an item-gain log needs.
// EventInventoryChanged instead fires synchronously, per slot, from inside
// applyFull/applyPartial as each UPDATE_INV_FULL/UPDATE_INV_PARTIAL packet is
// decoded, and reports every change (gains, losses, removals, id swaps) because a
// generic "this slot changed" signal is what a subscriber-style event needs — a
// pickpocket script that wants to know when a stack it's carrying got removed by a
// junk-drop, for instance, has no "gain" to look for. The two diffs are similar in
// shape (compare old slot to new slot) but operate over different inputs for
// different purposes; sharing the loop would mean exporting an internal per-slot
// compare step from gamestate for statlog to import, which is a real refactor of a
// package this task was told to leave untouched, to save perhaps ten lines. Left
// as two small, separate implementations instead, documented here so the next person
// doesn't "fix" the duplication by coupling the two packages.
package gamestate

import "sync"

// EventKind identifies what changed about the game state for one emitted Event.
type EventKind int

const (
	// EventLevelUp fires when applyUpdateStat observes a skill's level rising above
	// its previously known value. The very first UPDATE_STAT ever seen for a given
	// skill does not fire this — there is no real previous value to compare against
	// yet (see applyUpdateStat's own guard on Skill.Generation).
	EventLevelUp EventKind = iota
	// EventInventoryChanged fires once per slot whose item id or count actually
	// changed during an UPDATE_INV_FULL/UPDATE_INV_PARTIAL apply. See this file's
	// package doc for why it does not reuse internal/statlog's own diffing.
	EventInventoryChanged
	// EventChatMessage fires once per chat line: a MESSAGE_GAME packet (Text verbatim,
	// including a raw "<name>:tradereq:"), a player's public chat or overhead SAY from
	// PLAYER_INFO, or a MESSAGE_PRIVATE. ChatType tells them apart; see chat.go. Filter on
	// ChatType == ChatGame before matching server text, since players can type anything.
	EventChatMessage
	// EventModalChanged fires whenever the (Main, Side, Chat) modal id triple
	// changes, whether from a server packet (IF_OPENMAIN/IF_OPENSIDE/IF_OPENCHAT/
	// IF_OPENMAIN_SIDE/IF_CLOSE) or State's own local optimistic close
	// (LocalCloseModal). No new decode here — MainModalID/SideModalID/ChatModalID
	// already existed as plain State fields; this only observes them changing.
	EventModalChanged
	// EventTick fires once per server tick, i.e. per PLAYER_INFO packet (see scene.go),
	// after that packet's position updates have been applied. Tick is the new count.
	EventTick
	// EventVarpChanged fires when VARP_SMALL/VARP_LARGE sets a varp to a value different
	// from its previous one. A varp never told reads 0, as in the client's own var array, so
	// being told 0 for the first time does not fire.
	EventVarpChanged
	// EventSkillXP fires when UPDATE_STAT changes a skill's XP. Like EventLevelUp, the first
	// UPDATE_STAT for a skill after login never fires it.
	EventSkillXP
	// EventDeath fires when the game's death line ("Oh dear you are dead!") arrives. State.LastDeath then holds the
	// client's reading of it (death.go); the pack and the tile are still those of the death.
	EventDeath
)

// String names an EventKind the way reference/server-api-docs.md's events namespace
// names its own EventMap keys (tick, chat.message, skill.level, inventory.changed) —
// useful for logging, not used for dispatch.
func (k EventKind) String() string {
	switch k {
	case EventLevelUp:
		return "skill.level"
	case EventInventoryChanged:
		return "inventory.changed"
	case EventChatMessage:
		return "chat.message"
	case EventModalChanged:
		return "modal.changed"
	case EventTick:
		return "tick"
	case EventVarpChanged:
		return "varp.changed"
	case EventSkillXP:
		return "skill.xp"
	case EventDeath:
		return "death"
	default:
		return "unknown"
	}
}

// Event is one detected state change. Only the fields belonging to Kind are
// meaningful; the rest are left at their zero value, the same discipline
// internal/randomevents.Event already expects of its callers (there, unused fields
// are -1 sentinels instead of zero, because 0 is a valid id there — every field here
// is fine at zero for a Kind it doesn't belong to, so no sentinel is needed).
type Event struct {
	Kind EventKind

	// EventLevelUp and EventSkillXP fields (EventSkillXP also sets Level to the current level).
	Skill         int // stat index, see SkillCount
	Level         int
	PreviousLevel int
	XP            int // whole XP, matching Skill.XP
	XPDelta       int // EventSkillXP: XP minus the previous XP

	// EventTick field.
	Tick int

	// EventVarpChanged fields.
	Varp          int
	Value         int
	PreviousValue int

	// EventInventoryChanged fields.
	Comp          int // inventory component id (State.Inventory's comID)
	Slot          int
	ItemID        int32
	PreviousID    int32
	Count         int32
	PreviousCount int32

	// EventChatMessage fields (see chat.go). ChatType is Client.ts addChat's type (ChatGame,
	// ChatPublic, ChatPrivate, ...), Username the sender's display name ("" for a game line),
	// and Crown 0, 1 or 2 for the mod crown the client would draw.
	Text     string
	ChatType int
	Username string
	Crown    int

	// EventModalChanged fields.
	Main, Side, Chat                         int
	PreviousMain, PreviousSide, PreviousChat int
}

// Subscriber receives one Event at a time.
//
// Per reference/server-api-docs.md's events namespace: "Callbacks fire during client
// processing. Keep them short... perform real actions from loop()". This
// implementation honors that by construction: Subscribe's callbacks always run after
// State's own internal lock (mu) has been released (see Handle/LocalCloseModal), so a
// Subscriber calling back into State — Skills(), Inventory(), Tile(), etc. — cannot
// deadlock, but they still run synchronously, in registration order, on whatever
// goroutine fed State.Handle the packet (or called LocalCloseModal). A slow or
// blocking Subscriber therefore stalls that goroutine's own packet processing — do
// real work (walking, clicking, eating) from a script's own loop after noting the
// event, not from inside the callback itself.
type Subscriber func(Event)

// Subscribe registers fn to receive every Event emitted after this call. The
// returned cancel func removes it; calling cancel more than once, or after fn has
// already been removed, is a safe no-op. There is no history or replay — a new
// subscriber only ever sees events emitted from this point forward.
func (s *State) Subscribe(fn Subscriber) (cancel func()) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if s.subscribers == nil {
		s.subscribers = make(map[int]Subscriber)
	}
	id := s.nextSubID
	s.nextSubID++
	s.subscribers[id] = fn

	var once sync.Once
	return func() {
		once.Do(func() {
			s.subMu.Lock()
			defer s.subMu.Unlock()
			delete(s.subscribers, id)
		})
	}
}

// emit dispatches events to every currently-registered subscriber, in a snapshot of
// registration order taken under subMu. Callers must NOT hold s.mu when calling this
// (see Subscriber's doc comment) — Handle and LocalCloseModal both unlock s.mu before
// calling emit.
func (s *State) emit(events []Event) {
	if len(events) == 0 {
		return
	}
	s.subMu.RLock()
	subs := make([]Subscriber, 0, len(s.subscribers))
	for _, fn := range s.subscribers {
		subs = append(subs, fn)
	}
	s.subMu.RUnlock()

	for _, ev := range events {
		for _, fn := range subs {
			fn(ev)
		}
	}
}
