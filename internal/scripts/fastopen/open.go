// Package fastopen opens held items five a game tick, the shared opener of every script that opens
// caskets and oysters (a user request: "casket opening oyster opening, any form of opening can be
// done at this tick speed we have found when banking gems"; "oysters get opened on handoff for
// progressive fisher, and opened on mule reciever if they happen to have closed oysters").
//
// THE CONTENT AND THE ENGINE (branch 289):
//
//   - Neither open has a delay: skill_fishing/scripts/fishing_spots/memberfish.rs2 [opheld1,
//     oystershell] removes the shell (inv_del: the lowest slot holding one) and adds an Oyster pearl
//     or an Empty oyster; [opheld1,casket] removes the clicked slot (inv_delslot) and adds a reward,
//     then shows it in an ~objbox, which pauses the script (interface_chat/scripts/chat.rs2
//     [proc,objbox]: p_pausebutton) only after the reward is in the pack.
//   - The box does not hold the next open up: an OPHELD on the backpack calls clearPendingAction
//     when the backpack is not the main modal (engine OpHeldHandler), and Player.closeModal drops
//     the paused script, so the next casket opens in the same read.
//   - OPHELD is a USER_EVENT, five handled a player a tick, the rest queued for the next tick
//     (engine NetworkPlayer.decodeIn), so five opens a tick is the most there is.
//   - Opened lowest slot first, every clicked slot still holds its item when the engine reaches it: a
//     reward or an opened oyster's product goes to the lowest empty slot (Inventory.add), which is
//     never above the slot just opened.
//
// Open sends the clicks five to a message (one websocket frame, which the engine buffers whole), a
// message a tick a little ahead of the server, then confirms ONCE that the pack no longer holds
// what was clicked and closes the last reward box. What it could not open is left for the caller's
// own (slower) path.
//
// Run is the same burst for a MIXED list of held-item clicks (BassOysterCaskets' drop cycle: opens,
// drops, key halves joined and pearls cut, which are all user events of the one five-a-tick budget),
// with the budget a caller may lower (Options.PerTick) when something else of its own, a tick
// manipulation batch, shares the tick.
package fastopen

import (
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
)

// PerTick is the opens a tick: the engine's five user events.
const PerTick = 5

// lookahead is how many messages go out beyond the tick the server is on.
const lookahead = 2

// tickPoll and tickFallback pace the messages by the server's ticks (PLAYER_INFO,
// gamestate.State.Tick); a tick that never shows is waited out after tickFallback.
const (
	tickPoll     = 10 * time.Millisecond
	tickFallback = 650 * time.Millisecond
)

// confirmSlack is the ticks the pack has, after the last message, to show every open.
const confirmSlack = 4

// Delayer sleeps for d and reports false when the caller should stop.
type Delayer func(d time.Duration) bool

// Result is what one Open did.
type Result struct {
	// Clicked is the items clicked, Left those the pack still holds in their slots at the end,
	// Messages the messages sent, Ticks the server ticks it spanned.
	Clicked, Left, Messages, Ticks int
	// Reason is "done" (every clicked item opened), "partial" (some are still held), "none"
	// (nothing to open) or "stopped".
	Reason string
}

// batch collects packets for one Send.
type batch struct {
	s      actions.Session
	builds []func(w *packet.Packet)
}

func (b *batch) Send(build func(w *packet.Packet)) error {
	b.builds = append(b.builds, build)
	return nil
}
func (b *batch) State() *gamestate.State     { return b.s.State() }
func (b *batch) Cheats() *anticheat.Counters { return b.s.Cheats() }

func (b *batch) flush() error {
	builds := b.builds
	b.builds = nil
	if len(builds) == 0 {
		return nil
	}
	return b.s.Send(func(w *packet.Packet) {
		for _, f := range builds {
			f(w)
		}
	})
}

// Step is one held-item click of a mixed burst (Run): an op on a pack slot, or one pack item used on
// another. The caller orders the steps; Run sends them in that order, which is the order the engine
// reads them in.
type Step struct {
	// ID and Slot name the item clicked: for a use, the TARGET (the item the other is used on).
	ID   int32
	Slot int
	// Op is the held op (1-5) sent on that item; ignored when Use is set.
	Op int
	// Use makes the step an OPHELDU: the item UseID in UseSlot used on the target.
	Use     bool
	UseID   int32
	UseSlot int
}

// send writes the step's packet to b.
func (st Step) send(b actions.Session, comID int) error {
	if st.Use {
		return actions.OpHeldU(b, st.ID, st.Slot, comID, st.UseID, st.UseSlot, comID)
	}
	return actions.OpHeld(b, st.Op, st.ID, st.Slot, comID)
}

// Pending is the steps whose named slot still holds the item they named, in the order given: what a
// burst has not yet done (or the engine refused). A caller that sent steps of several kinds reads what
// each kind did from it.
func Pending(st *gamestate.State, comID int, steps []Step) []Step {
	inv := st.Inventory(comID)
	if inv == nil {
		return append([]Step(nil), steps...)
	}
	var out []Step
	for _, it := range steps {
		if it.Slot < len(inv.Slots) && inv.Slots[it.Slot].ID == it.ID {
			out = append(out, it)
		}
	}
	return out
}

// held reports how many of steps still have their item in the slot they named (component comID):
// a step that opened, dropped or consumed its target has none, and it is how a burst is confirmed.
// Every step this package sends removes the item it names from that slot (an open, a drop, a cut
// pearl, the half a join uses up), whatever else the engine then puts into the slot.
func held(st *gamestate.State, comID int, steps []Step) int {
	inv := st.Inventory(comID)
	if inv == nil {
		return len(steps)
	}
	n := 0
	for _, it := range steps {
		if it.Slot < len(inv.Slots) && inv.Slots[it.Slot].ID == it.ID {
			n++
		}
	}
	return n
}

// nextTick waits for a server tick after from, at most tickFallback.
func nextTick(st *gamestate.State, delay Delayer, from int) bool {
	deadline := time.Now().Add(tickFallback)
	for st.Tick() == from && time.Now().Before(deadline) {
		if !delay(tickPoll) {
			return false
		}
	}
	return true
}

// Open opens every one of items (pack slots of component comID, the backpack the server knows as
// the inventory listener) with their first held op, "Open": five a message, a message a tick. It
// confirms once at the end and closes a reward box left open.
func Open(s actions.Session, delay Delayer, comID int, items []gamestate.SlotItem) Result {
	return Run(s, delay, comID, opSteps(items, openOption), Options{CloseBox: true})
}

// openOption is "Open", the first held op of a casket and an oyster (actions.Open).
const openOption = 1

// OpHeld is Open for any held op: every one of items gets held op option (1-5), five a message,
// a message a tick, confirmed once at the end by each clicked slot no longer holding its item. It
// closes nothing. BassOysterCaskets drops its junk catch with it (option 5, "Drop": content's
// [opheld5,_] drops the slot with no delay, so the five-a-tick limit is the engine's user-event
// cap, exactly as for opening).
func OpHeld(s actions.Session, delay Delayer, comID int, items []gamestate.SlotItem, option int) Result {
	return Run(s, delay, comID, opSteps(items, option), Options{})
}

// opSteps is items as held-op steps, lowest slot first: the order the engine's lowest-slot rules
// (an oyster's inv_del, a reward landing in the lowest empty slot) need clicks to arrive in.
func opSteps(items []gamestate.SlotItem, option int) []Step {
	items = append([]gamestate.SlotItem(nil), items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Slot < items[j].Slot })
	steps := make([]Step, len(items))
	for i, it := range items {
		steps[i] = Step{ID: it.ID, Slot: it.Slot, Op: option}
	}
	return steps
}

// Options are Run's knobs.
type Options struct {
	// PerTick is how many events a message carries, and so how many the burst spends of each tick's
	// five (PerTick when 0 or above it). A caller that shares the tick with other user events, a tick
	// manipulation batch, lowers it so the two together stay inside the engine's five.
	PerTick int
	// CloseBox closes the reward box the last casket opened, once the burst is confirmed: the reward
	// is already in the pack, and the next click on the backpack would close it itself.
	CloseBox bool
}

// Run sends steps in the order given, PerTick events to a message and a message a tick, confirms ONCE
// at the end that every named slot has lost its item, and reports what is left. The steps must not
// depend on each other's results (a slot named after the burst has run is a second burst's), and
// every step must name a slot that holds its item at the moment the burst is planned.
func Run(s actions.Session, delay Delayer, comID int, steps []Step, o Options) (r Result) {
	steps = append([]Step(nil), steps...)
	perMsg := o.PerTick
	if perMsg <= 0 || perMsg > PerTick {
		perMsg = PerTick
	}
	r.Clicked = len(steps)
	if len(steps) == 0 {
		r.Reason = "none"
		return r
	}
	st := s.State()
	tick0 := st.Tick()
	defer func() { r.Ticks = st.Tick() - tick0 }()
	messages := (len(steps) + perMsg - 1) / perMsg
	// Two messages go out ahead of the tick the server is on, which keeps five events queued for each
	// tick it reads; a lowered budget goes one message a tick instead, or two messages of the
	// budget could still be read in one tick.
	ahead := lookahead
	if perMsg < PerTick {
		ahead = 1
	}
	send := func(k int) error {
		b := &batch{s: s}
		for _, step := range steps[k*perMsg : min((k+1)*perMsg, len(steps))] {
			if err := step.send(b, comID); err != nil {
				return err
			}
		}
		return b.flush()
	}
	last := tick0
	for r.Messages < messages {
		for r.Messages < messages && r.Messages < st.Tick()-tick0+ahead {
			if err := send(r.Messages); err != nil {
				r.Left, r.Reason = held(st, comID, steps), "partial"
				return r
			}
			r.Messages++
		}
		if r.Messages == messages {
			break
		}
		if !nextTick(st, delay, last) {
			r.Left, r.Reason = held(st, comID, steps), "stopped"
			return r
		}
		last = st.Tick()
	}
	// Confirm once: every clicked item gone from its slot.
	for held(st, comID, steps) > 0 && st.Tick()-tick0 < messages+confirmSlack {
		if !nextTick(st, delay, st.Tick()) {
			r.Left, r.Reason = held(st, comID, steps), "stopped"
			return r
		}
	}
	r.Left = held(st, comID, steps)
	if _, _, chat := st.Modals(); o.CloseBox && chat != gamestate.NoModal {
		_ = actions.CloseModal(s) // the last casket's reward box: the reward is already in the pack
	}
	if r.Left > 0 {
		r.Reason = "partial"
	} else {
		r.Reason = "done"
	}
	return r
}
