package minemanipplan

import "rsb0t-go/internal/scripts/chopmanipplan"

// Engine is a tick-accurate model of one player mining the rune essence rock, written from the content's own scripts and the
// engine's order inside a tick, so the claims in plan.go can be checked and the controller driven against something that is
// not itself:
//
//	World.processClientsIn   the player's packets, at most five user events a tick, the rest wait in the socket buffer
//	World.processPlayers     weak queue (fletch_logs), engine queue (the level-up chat), then the interaction
//	                         (Player.processInteraction runs the op script only while canAccess: no chat or main modal open)
//
// The scripts are mining.rs2's mining_firstswing and mining_continue with get_ore_essence, cut_logs.rs2's process_fletch_logs
// and the make menu's quirks the lab found (an op click or CLOSE_MODAL clears the weak queue; the batch cancels its own
// fletch). Everything the model leaves out (the random-event flag, level and pickaxe checks, the rock being real) is the same
// with and without a batch.
type Engine struct {
	// Pick is the pickaxe that pickaxe_checker.rs2 finds. Kit says the pack holds the knife and a log (Others counts every
	// pack slot but the essence, so Free = PackSlots - Others - Essence). LevelEvery makes every Nth essence a level-up.
	Pick       Pickaxe
	Kit        bool
	Others     int
	LevelEvery int
	// Unbounded gives the pack unlimited room, for studying a steady state without the bank trips that bound a real load.
	Unbounded bool

	// Clock is map_clock and Delay the varp %action_delay.
	Clock, Delay int
	// Essence is the pack's rune essence.
	Essence int
	// Rolls are the ticks essence arrived in. Leaks counts carrier logs the fletch queue consumed (a batch split by the
	// five-events limit), Boxes the "too full" message boxes opened, Chats the level-up chats opened.
	Rolls               []int
	Leaks, Boxes, Chats int
	// Events is the user events read in each tick, by tick.
	Events map[int]int

	target   int // p_oploc: 0 none, 1 or 3 (the op the next tick runs)
	modal    Modal
	fletchAt int // tick the queued fletch_logs fires in, 0 none
	levelDue bool
	inbox    []Event
	anim     int // the tick's animation so far, and its priority
	animPri  int
}

// Event is one user packet.
type Event int

// The packets the model reads.
const (
	// ClickRock is OPLOC1 on the essence rock.
	ClickRock Event = iota + 1
	// KnifeOnLog is OPHELDU, the knife used on the carrier log: it opens the make menu.
	KnifeOnLog
	// Make1 is IF_BUTTON on the make menu's first Make 1 button.
	Make1
	// CloseModal is CLOSE_MODAL.
	CloseModal
	// Continue is RESUME_PAUSEBUTTON.
	Continue
)

func (e Event) String() string {
	switch e {
	case ClickRock:
		return "click"
	case KnifeOnLog:
		return "knife-on-log"
	case Make1:
		return "make-1"
	case CloseModal:
		return "close-modal"
	case Continue:
		return "continue"
	}
	return "?"
}

// Batch is the packets of one batch, in the order they are written: [knife on log, Make 1, CLOSE_MODAL, rock click].
var Batch = []Event{KnifeOnLog, Make1, CloseModal, ClickRock}

// Modal is the interface open in the chat or main viewport.
type Modal int

// The modals.
const (
	ModalNone Modal = iota
	// ModalMake is the make menu a knife on a log opens (skill_multi3), ModalChat the level-up chat, ModalBox the
	// "inventory is too full" message box. Each is a script paused on its button.
	ModalMake
	ModalChat
	ModalBox
)

// Seen is what the client can read of one tick.
type Seen struct {
	N int
	// Anim is the animation the server started on the player this tick (chopmanipplan.NoAnim for none).
	Anim    int
	Modal   Modal
	Essence int
	// Read is the user events the server read this tick.
	Read int
}

// Animation priorities (SeqType.priority): the pickaxes' is 0 (225 all.seq: priority=0), the fletching animation's the
// default for a seq with walkmerge labels. Player.playAnimation only replaces the tick's animation with one of equal or
// higher priority, which is why the fletching animation wins the stale tick (the lab saw seq 1248 at every batch tick).
const (
	pickPriority   = 0
	fletchPriority = 2
)

// NewEngine returns an engine at tick 0 for the pickaxe. The pickaxe is carried in the pack.
func NewEngine(p Pickaxe, kit bool) *Engine {
	e := &Engine{Pick: p, Kit: kit, Events: map[int]int{}, Delay: -1000}
	e.Others = 1 // the pickaxe
	if kit {
		e.Others += KitSlots
	}
	return e
}

// Free is inv_freespace(inv).
func (e *Engine) Free() int {
	if e.Unbounded {
		return 1 << 30
	}
	return PackSlots - e.Others - e.Essence
}

// Empty clears the pack of essence, as a bank trip does (the varp keeps its old value, so it is stale on return).
func (e *Engine) Empty(ticksAway int) {
	e.Essence = 0
	e.target = 0
	e.modal = ModalNone
	e.Clock += ticksAway
}

// play is Player.playAnimation: a new animation replaces the tick's only when it has the same or a higher priority.
func (e *Engine) play(seq, pri int) {
	if e.anim == chopmanipplan.NoAnim || pri >= e.animPri {
		e.anim, e.animPri = seq, pri
	}
}

// Step runs one game tick in which the client's packets send arrive at the server (they are appended to what is waiting,
// and at most five are read).
func (e *Engine) Step(send []Event) Seen {
	e.Clock++
	e.inbox = append(e.inbox, send...)
	n := len(e.inbox)
	if n > EngineEventsPerTick {
		n = EngineEventsPerTick
	}
	read := append([]Event(nil), e.inbox[:n]...)
	e.inbox = append([]Event(nil), e.inbox[n:]...)
	e.Events[e.Clock] = n

	e.anim, e.animPri = chopmanipplan.NoAnim, -1
	for _, ev := range read {
		switch ev {
		case ClickRock:
			// OpLocHandler: clearPendingAction (interaction, modal, weak queue), then the op becomes the interaction.
			e.clearPending()
			e.target = 1
		case KnifeOnLog:
			e.clearPending()
			if e.Kit {
				e.modal = ModalMake
			}
		case Make1:
			if e.modal == ModalMake {
				// process_fletch_logs (cut_logs.rs2:81-86): animation, re-arm a stale varp to +2, queue the fletch.
				e.play(chopmanipplan.FletchAnim, fletchPriority)
				if e.Delay < e.Clock {
					e.Delay = e.Clock + 2
				}
				e.fletchAt = e.Delay
				e.modal = ModalNone
			}
		case CloseModal:
			e.fletchAt = 0
			e.modal = ModalNone
		case Continue:
			if e.modal == ModalChat || e.modal == ModalBox {
				e.modal = ModalNone
			}
		}
	}

	// processQueues: the weak queue.
	if e.fletchAt != 0 && e.Clock >= e.fletchAt && e.modal == ModalNone {
		e.fletchAt = 0
		if e.Kit {
			e.Kit = false // the log is cut into arrow shafts: the carrier is gone, the shafts keep its slot
			e.Leaks++
		}
	}
	// processEngineQueue: the level-up chat the last tick's experience queued.
	if e.levelDue && e.modal == ModalNone {
		e.levelDue = false
		e.modal = ModalChat
		e.Chats++
	}
	// processInteraction.
	if e.target != 0 && e.modal == ModalNone {
		op := e.target
		e.target = 0
		if op == 1 {
			e.firstSwing()
		} else {
			e.continueSwing()
		}
	}
	return Seen{N: e.Clock, Anim: e.anim, Modal: e.modal, Essence: e.Essence, Read: n}
}

func (e *Engine) clearPending() {
	e.target = 0
	e.modal = ModalNone
	e.fletchAt = 0
}

func (e *Engine) tooFull() bool {
	if e.Free() >= 1 {
		return false
	}
	e.modal = ModalBox
	e.Boxes++
	return true
}

// firstSwing is [label,mining_firstswing] (mining.rs2:41-75) for the essence rock.
func (e *Engine) firstSwing() {
	if e.tooFull() {
		return
	}
	if e.Delay < e.Clock {
		e.Delay = e.Clock + e.Pick.Rate
		e.target = 1
		return
	}
	e.play(e.Pick.Anim, pickPriority)
	if e.Delay == e.Clock {
		e.getOre()
		return
	}
	e.target = 3
}

// continueSwing is [label,mining_continue] (mining.rs2:77-113) for the essence rock.
func (e *Engine) continueSwing() {
	if e.tooFull() {
		return
	}
	switch {
	case e.Delay < e.Clock:
		e.Delay = e.Clock + e.Pick.Rate
	case e.Delay == e.Clock:
		e.play(e.Pick.Anim, pickPriority)
		e.getOre()
		return
	}
	e.target = 3
}

// getOre is [label,get_ore_essence] (mining.rs2:197-207): the essence, the experience, and the loop goes on.
func (e *Engine) getOre() {
	e.Essence++
	e.Rolls = append(e.Rolls, e.Clock)
	if e.LevelEvery > 0 && e.Essence%e.LevelEvery == 0 {
		e.levelDue = true
	}
	e.target = 3
}
