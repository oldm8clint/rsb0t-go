// Package tickmanip is the pure half of the tick-manipulation lab proof (cmd/tickmanipprobe): a
// tick-by-tick model of how the engine and content decide WHEN a skilling roll fires, the client
// action plans that try to pull it forward, and the arithmetic that turns what the lab observed
// into rates and run-energy costs.
//
// WHY A MODEL AT ALL. Four content scripts (woodcutting, fly fishing, net fishing, harpoon fishing)
// gate their roll on one per-player varp, %action_delay, compared with map_clock: a roll fires only
// on the tick where %action_delay = map_clock. Fletching writes the same varp. The model encodes
// that reading of content/scripts and engine/src/engine/entity/Player.ts so that
//
//   - the lab run can be compared with a deterministic prediction tick by tick, instead of with a
//     success count that is also noise (a fish roll can fail), and
//   - the unit tests below pin the reading down: if content changes, a test says which cadence moved.
//
// The model is a PREDICTION. Nothing here is evidence of what the engine does; the evidence is the
// lab run (docs/tick_manipulation_lab_2026-10-05.md), which feeds the same plans through the real
// engine and requires the observed roll ticks to equal the modelled ones.
//
// Every rule below cites the file it was read from. Engine = the lab checkout of Engine-TS 289
// (rsb0t-go-engine's `engine`, which carries local lab commits, so its line numbers are not the
// read-only reference's); content = Content 289 `scripts/`.
package tickmanip

import "fmt"

// Skill names a skilling script whose roll is gated by %action_delay.
type Skill int

const (
	// Woodcutting is [oploc1,_tree] / [oploc3,_tree] (content skill_woodcutting/scripts/woodcut.rs2).
	Woodcutting Skill = iota
	// FlyFishing is the freshfish spot's Lure (op1) and its hidden op4
	// (skill_fishing/scripts/fishing_spots/freshfish.rs2).
	FlyFishing
	// NetFishing is the saltfish spot's Net (op1) and its hidden op4 (saltfish.rs2).
	NetFishing
	// HarpoonFishing is the memberfish spot's Harpoon (op3) and its hidden op5 (memberfish.rs2).
	HarpoonFishing
)

// Skills lists every modelled skill in a stable order.
var Skills = []Skill{Woodcutting, FlyFishing, NetFishing, HarpoonFishing}

func (s Skill) String() string {
	switch s {
	case Woodcutting:
		return "woodcutting"
	case FlyFishing:
		return "fly-fishing"
	case NetFishing:
		return "net-fishing"
	case HarpoonFishing:
		return "harpoon-fishing"
	}
	return fmt.Sprintf("skill(%d)", int(s))
}

// Op is the interaction op the engine runs for the player's current target on the next
// Player.processInteraction. OpNone means there is no interaction pending.
type Op int

const (
	OpNone Op = iota
	OpLoc1
	OpLoc3
	OpNpc1
	OpNpc3
	OpNpc4
	OpNpc5
)

func (o Op) String() string {
	switch o {
	case OpNone:
		return "-"
	case OpLoc1:
		return "oploc1"
	case OpLoc3:
		return "oploc3"
	case OpNpc1:
		return "opnpc1"
	case OpNpc3:
		return "opnpc3"
	case OpNpc4:
		return "opnpc4"
	case OpNpc5:
		return "opnpc5"
	}
	return fmt.Sprintf("op(%d)", int(o))
}

// Step is one client packet in a tick's batch, in the order it is written.
type Step int

const (
	// KnifeUse is OPHELDU, a knife used on a log. OpHeldUHandler runs the opheldu script INSIDE the
	// handler, in the tick the packet is read, and the script opens the skill_multi3 dialogue
	// (content skill_fletching/scripts/cut_logs.rs2 [label,fletch_log] -> ~skill_multi3,
	// interface_chat/scripts/chat.rs2:801) and waits in p_pausebutton. The handler first runs
	// clearPendingAction, which drops the interaction and closes any modal (engine
	// network/game/client/handler/OpHeldUHandler.ts, Player.ts clearPendingAction).
	KnifeUse Step = iota
	// MakeShafts is IF_BUTTON on skill_multi3:make1_a (component 8889: the "Make 1" of the first
	// product, 15 arrow shafts). IfButtonHandler finds the component in player.resumeButtons and
	// resumes the suspended script in the same read (IfButtonHandler.ts), which reaches
	// [label,process_fletch_logs] (cut_logs.rs2): %action_delay = map_clock + 2 when it was below
	// map_clock, then weakqueue fletch_logs (%action_delay - map_clock) ticks out.
	MakeShafts
	// MoveOwn is MOVE_GAMECLICK on the player's own tile: an interrupt that only runs
	// clearPendingAction (MoveClickHandler.ts: a non-op click clears the pending action).
	MoveOwn
	// Horsey is OPHELD1 on a toy horsey: clearPendingAction in OpHeldHandler.ts, then
	// [opheld1,horsey_*] -> p_stopaction + anim (content areas/area_draynor/scripts/diango.rs2:36-48).
	// It sets no %action_delay and no p_delay.
	Horsey
	// Click is the skill's own first op: OPLOC1 on a tree, OPNPC1 on a fly or net spot, OPNPC3 on a
	// harpoon spot. OpLocHandler / OpNpcHandler run clearPendingAction and then setInteraction.
	Click
	// WalkClick is MOVE_GAMECLICK along the route the arm carries (the walking and energy arms). The skilling model
	// treats it as an interrupt, like MoveOwn: MoveClickHandler clears the pending action.
	WalkClick
	// CastHigh and CastLow are OPHELDT of High and Low Level Alchemy on the arm's alchable item (alchemy.rs2:
	// p_delay(3) and p_delay(1)); RubRing and RubNecklace are OPHELD4 on a ring of dueling and a games necklace
	// (p_delay(1), then a choice dialogue); Bury is OPHELD1 on a bone (p_stopaction, p_delay(0)). Each runs
	// clearPendingAction first (OpHeldHandler, OpHeldTHandler), so to the skilling model each is an interrupt; the
	// delays belong to internal/tickmanip/energy.go.
	CastHigh
	CastLow
	RubRing
	RubNecklace
	Bury
	// Wield is OPHELD2 on one of two daggers, whichever is in the pack: an op whose handler clears
	// player.moveClickRequest (OpHeldHandler.ts) and has no delay, to test what a cast needs in front of it.
	Wield
	// ClearMove is OPHELDU naming an item that is NOT at the slot it names (a packet a lagging client really sends):
	// OpHeldUHandler.ts sets player.moveClickRequest = false and runs clearPendingAction and returns, with no message, no
	// delay and no change to the backpack. It is the harmless way to do what a Wield does in front of a cast.
	ClearMove
	// CloseModal is CLOSE_MODAL: the engine closes the modal and with it clears the weak queue at the top of the player's next
	// queue step (Player.requestModalClose), so a Make 1 followed by it in the same tick never lets the fletch it queued run.
	CloseModal
	// CookUse is OPLOCU of a raw fish on a range (cooking.rs2 cook_item, p_delay(1)); see cook.go.
	CookUse
	// Ack is the lab's acknowledgement packet (`::getvar com_maxhit`, CLIENT_CHEAT): a user event that changes nothing,
	// which the model carries so the five-events-a-tick budget is counted the way the engine counts it.
	Ack
)

func (s Step) String() string {
	switch s {
	case KnifeUse:
		return "knife-on-log"
	case MakeShafts:
		return "make-1-shafts"
	case MoveOwn:
		return "move-own-tile"
	case Horsey:
		return "horsey"
	case Click:
		return "click"
	case WalkClick:
		return "walk-click"
	case CastHigh:
		return "cast-high-alch"
	case CastLow:
		return "cast-low-alch"
	case RubRing:
		return "rub-ring"
	case RubNecklace:
		return "rub-necklace"
	case Bury:
		return "bury"
	case Wield:
		return "wield"
	case ClearMove:
		return "clear-move"
	case CloseModal:
		return "close-modal"
	case CookUse:
		return "cook-use"
	case Ack:
		return "ack"
	}
	return fmt.Sprintf("step(%d)", int(s))
}

// Knife is the two-packet pair that sets %action_delay: the knife on a log, then Make 1 arrow shafts.
func Knife() []Step { return []Step{KnifeUse, MakeShafts} }

// UserEventLimit is how many successful USER_EVENT packets the engine reads for one player in one
// tick (engine network/game/client/ClientGameProtCategory.ts USER_EVENT limit 5, enforced by
// NetworkPlayer.decodeIn). Every Step above is a USER_EVENT (network/game/client/model/*.ts). A
// refused packet (its handler returned false) counts against the separate CLIENT_EVENT budget of 20
// instead, so it never eats into the five.
const UserEventLimit = 5

// maxWeak bounds the fletch queue (a safety net for runaway plans, never reached by a real one).
const maxWeak = 64

// Sim is one player's state for the %action_delay machine.
type Sim struct {
	Skill Skill
	// Clock is map_clock: the tick being (or about to be) processed. The first Tick call runs Clock.
	Clock int
	// Delay is %action_delay. A fresh login reads 0 (the varp is temp scope and has no default), so it
	// starts far below the clock.
	Delay int
	// Op is the op the engine will run for the current interaction target next processInteraction.
	Op Op

	// dialog is true while the skill_multi3 dialogue is open and its script waits in p_pausebutton.
	dialog bool
	// weak holds the due ticks of queued fletch_logs scripts (the weak queue).
	weak []int
	// carry is the packets not yet read: the per-tick limit leaves them in the socket for the next tick.
	carry []Step
}

// NewSim starts a fresh account at tick clock0, standing at its target with no interaction.
func NewSim(skill Skill, clock0 int) *Sim {
	return &Sim{Skill: skill, Clock: clock0}
}

// TickResult is what one engine cycle did to the player.
type TickResult struct {
	// Tick is the arm-relative tick (0 = the first tick the server processed).
	Tick int
	// Read is the steps the server read this tick, in order (after the per-tick limit).
	Read []Step
	// Deferred is how many steps the limit pushed to a later tick.
	Deferred int
	// Refused is how many of the read steps their handler refused (they cost no user-event budget).
	Refused int
	// Ran is the interaction op processInteraction executed this tick (OpNone if none ran).
	Ran Op
	// Roll is true when the skill's roll fired this tick (the stat_random / fish_roll was reached).
	Roll bool
	// AnimSkill is true when the executed script called anim() this tick (the swing, the cast, the
	// on-spot fishing animation, the harpoon). Whether the client sees it depends on the sequence
	// priorities, see AnimFletch.
	AnimSkill bool
	// AnimFletch is true when Make 1 resumed the fletching script, which calls anim(human_fletching)
	// (cut_logs.rs2:81). Two anim() calls in one tick leave one ANIM update on the wire, so a tick with
	// both can only be compared as "an animation was played".
	AnimFletch bool
	// AnimOther is true when a toy horsey played its animation.
	AnimOther bool
	// Fletched is true when a queued fletch_logs actually ran: a log was consumed and Fletching XP
	// was paid. A well-formed manipulation never does this (the next click cancels the queue).
	Fletched bool
	// DelayAfter is %action_delay at the end of the tick, relative to Clock (so 2 means "two ticks
	// from now"), which is the number every script compares.
	DelayAfter int
	// OpAfter is the op the interaction will run next tick.
	OpAfter Op
}

// AnyAnim reports whether any animation update is expected on the wire this tick.
func (r TickResult) AnyAnim() bool { return r.AnimSkill || r.AnimFletch || r.AnimOther }

func (s *Sim) clearPending() {
	s.Op = OpNone
	s.dialog = false
	s.weak = s.weak[:0]
}

// handle applies one packet the way its handler does, inside World.processClientsIn, and reports
// whether the handler accepted it (a refusal returns false, which the engine counts against the
// CLIENT_EVENT budget instead of the five-a-tick user budget: NetworkPlayer.ts read()).
func (s *Sim) handle(st Step, res *TickResult) bool {
	switch st {
	case KnifeUse:
		// OpHeldUHandler: clearPendingAction, then executeScript(opheldu) which opens the dialogue.
		s.clearPending()
		s.dialog = true
	case MakeShafts:
		if !s.dialog {
			// IfButtonHandler: the component is not visible (visible() is false), so it is refused.
			return false
		}
		s.dialog = false
		res.AnimFletch = true
		// [label,process_fletch_logs]: if %action_delay < map_clock then %action_delay = map_clock + 2;
		// weakqueue*(fletch_logs, %action_delay - map_clock).
		if s.Delay < s.Clock {
			s.Delay = s.Clock + 2
		}
		if len(s.weak) < maxWeak {
			s.weak = append(s.weak, s.Clock+(s.Delay-s.Clock))
		}
	case MoveOwn, WalkClick, CastHigh, CastLow, RubRing, RubNecklace, Bury, Wield, ClearMove, CookUse:
		s.clearPending()
	case CloseModal:
		// The interaction is not touched: only the modal and the weak queue.
		s.dialog = false
		s.weak = s.weak[:0]
	case Ack:
		// A read-only cheat: it uses one of the five user events and does nothing else.
	case Horsey:
		s.clearPending()
		res.AnimOther = true
	case Click:
		s.clearPending()
		s.Op = firstOp(s.Skill)
	}
	return true
}

// firstOp is the op a Click sets: the skill's op1 (op3 for the harpoon).
func firstOp(sk Skill) Op {
	switch sk {
	case Woodcutting:
		return OpLoc1
	case FlyFishing, NetFishing:
		return OpNpc1
	case HarpoonFishing:
		return OpNpc3
	}
	return OpNone
}

// Tick runs one engine cycle: the client input phase (processClientsIn, which reads at most
// UserEventLimit packets and runs each handler to completion), then processPlayers (the weak queue,
// then processInteraction). batch is what the client wrote since the previous cycle.
func (s *Sim) Tick(tick int, batch []Step) TickResult {
	res := TickResult{Tick: tick}
	// processClientsIn: whatever was left over from last tick comes first, then this tick's packets.
	// decodeIn reads while fewer than UserEventLimit accepted user events have been handled, so the
	// packet that would be the sixth waits in the socket buffer for the next tick.
	pending := append(append([]Step(nil), s.carry...), batch...)
	s.carry = nil
	used := 0
	for i, st := range pending {
		if used >= UserEventLimit {
			s.carry = append(s.carry, pending[i:]...)
			res.Deferred = len(pending) - i
			break
		}
		res.Read = append(res.Read, st)
		if s.handle(st, &res) {
			used++
		} else {
			res.Refused++
		}
	}

	// processPlayers -> processQueues -> processWeakQueue: a queued fletch_logs runs once its delay has
	// counted down AND the player can access (no modal open). Both hold here (a dialogue left open
	// blocks it, which is why the dialogue test is first).
	if !s.dialog {
		kept := s.weak[:0]
		for _, due := range s.weak {
			if s.Clock >= due {
				res.Fletched = true
				continue
			}
			kept = append(kept, due)
		}
		s.weak = kept
	}

	// processInteraction: tryInteract runs the op script when the player can access and is in range.
	if s.Op != OpNone && !s.dialog {
		res.Ran = s.Op
		s.Op, res.Roll, res.AnimSkill = s.run(s.Op)
	}
	res.DelayAfter = s.Delay - s.Clock
	res.OpAfter = s.Op
	s.Clock++
	return res
}

// run executes one op script against the current Delay and Clock and returns the op the interaction
// holds afterwards (p_opnpc / p_oploc, or OpNone when the script just ends), whether the roll fired,
// and whether the script called anim() on this execution.
//
// The comparisons are the scripts' own, quoted by line. A branch that matches nothing and calls no
// p_op* ends the interaction: that is how a click one tick early does nothing at a net or harpoon spot.
func (s *Sim) run(op Op) (next Op, roll, anim bool) {
	clk, d := s.Clock, s.Delay
	switch s.Skill {
	case Woodcutting:
		switch op {
		case OpLoc1:
			// woodcut.rs2:55-62: delay < clock => delay = clock + 3, p_oploc(1), no animation.
			if d < clk {
				s.Delay = clk + 3
				return OpLoc1, false, false
			}
			// woodcut.rs2:81-88: anim + "You swing your axe at the tree."; the roll only when the varp
			// equals the clock (:84-86), and get_logs ends in p_oploc(3) (:156) as the swing does (:88).
			return OpLoc3, d == clk, true
		case OpLoc3:
			// [label,cut_tree] woodcut.rs2:118-125.
			if d < clk {
				s.Delay = clk + 3
				return OpLoc3, false, false
			}
			return OpLoc3, d == clk, d == clk
		}
	case FlyFishing:
		switch op {
		case OpNpc1:
			// freshfish.rs2:37-41: delay < clock => delay = clock + 4, p_opnpc(1).
			if d < clk {
				s.Delay = clk + 4
				return OpNpc1, false, false
			}
			// :42-47: delay == clock + 3 => the cast animation, p_opnpc(1).
			if d == clk+3 {
				return OpNpc1, false, true
			}
			// :48-52: delay == clock + 2 only prints "You attempt to catch a fish."; EVERY other delay
			// falls through to p_opnpc(4). In particular delay == clock does NOT roll here, only op4
			// does (:79-85), so an op1 on the roll tick throws that roll away.
			return OpNpc4, false, false
		case OpNpc4:
			// freshfish.rs2:77-87 fish_trout_salmon: delay < clock => delay = clock + 4; delay == clock
			// => ~fish_roll then anim(human_fish_onspot); p_opnpc(4).
			if d < clk {
				s.Delay = clk + 4
				return OpNpc4, false, false
			}
			return OpNpc4, d == clk, d == clk
		}
	case NetFishing:
		switch op {
		case OpNpc1:
			// saltfish.rs2:37-41: delay < clock => delay = clock + 4, p_opnpc(1).
			if d < clk {
				s.Delay = clk + 4
				return OpNpc1, false, false
			}
			// :42-46: delay == clock + 3 => "You cast out your net..." + anim, p_opnpc(4).
			if d == clk+3 {
				return OpNpc4, false, true
			}
			// :47-54: delay == clock => ~fish_roll with NO animation ("doesnt play anim here") and NO
			// p_opnpc, so the interaction ends. Any other delay matches nothing and also ends it.
			if d == clk {
				return OpNone, true, false
			}
			return OpNone, false, false
		case OpNpc4:
			// saltfish.rs2:71-81 fish_shrimp: delay < clock => delay = clock + 5; delay == clock =>
			// anim(human_smallnet) + ~fish_roll; p_opnpc(4).
			if d < clk {
				s.Delay = clk + 5
				return OpNpc4, false, false
			}
			return OpNpc4, d == clk, d == clk
		}
	case HarpoonFishing:
		switch op {
		case OpNpc3:
			// memberfish.rs2:30-34: delay < clock => delay = clock + 5, p_opnpc(3).
			if d < clk {
				s.Delay = clk + 5
				return OpNpc3, false, false
			}
			// :35-39: delay == clock + 4 => "You start Harpooning fish." + anim, p_opnpc(5).
			if d == clk+4 {
				return OpNpc5, false, true
			}
			// :40-43: delay == clock => anim + ~fish_roll, no p_opnpc: the interaction ends. Any other
			// delay matches nothing and ends it.
			if d == clk {
				return OpNone, true, true
			}
			return OpNone, false, false
		case OpNpc5:
			// memberfish.rs2:72-78 fish_shark.
			if d < clk {
				s.Delay = clk + 5
				return OpNpc5, false, false
			}
			return OpNpc5, d == clk, d == clk
		}
	}
	// An op the skill does not use: nothing happens and the interaction ends.
	return OpNone, false, false
}

// Trace is the result of replaying a plan.
type Trace struct {
	Skill   Skill
	Results []TickResult
}

// RollTicks lists the arm-relative ticks on which the roll fired.
func (t Trace) RollTicks() []int {
	var out []int
	for _, r := range t.Results {
		if r.Roll {
			out = append(out, r.Tick)
		}
	}
	return out
}

// Rolls counts the rolls.
func (t Trace) Rolls() int { return len(t.RollTicks()) }

// Leaks counts the queued fletches that actually ran (a log consumed, Fletching XP paid).
func (t Trace) Leaks() int {
	n := 0
	for _, r := range t.Results {
		if r.Fletched {
			n++
		}
	}
	return n
}

// Intervals are the gaps between consecutive rolls.
func (t Trace) Intervals() []int {
	rolls := t.RollTicks()
	var out []int
	for i := 1; i < len(rolls); i++ {
		out = append(out, rolls[i]-rolls[i-1])
	}
	return out
}

// baseClock is an arbitrary map_clock for a replay; only differences matter.
const baseClock = 1000

// Replay runs plan against a fresh account for ticks cycles.
func Replay(skill Skill, plan Plan, ticks int) Trace {
	sim := NewSim(skill, baseClock)
	tr := Trace{Skill: skill}
	for t := 0; t < ticks; t++ {
		var batch []Step
		if plan != nil {
			batch = plan(t)
		}
		tr.Results = append(tr.Results, sim.Tick(t, batch))
	}
	return tr
}

// ReplayFrom is Replay for an account already mid-cycle: warm cycles of plan run first and are not
// reported, so the measured window starts wherever the natural cycle has got to.
func ReplayFrom(skill Skill, warm Plan, warmTicks int, plan Plan, ticks int) Trace {
	sim := NewSim(skill, baseClock)
	for t := 0; t < warmTicks; t++ {
		var batch []Step
		if warm != nil {
			batch = warm(t)
		}
		sim.Tick(t, batch)
	}
	tr := Trace{Skill: skill}
	for t := 0; t < ticks; t++ {
		var batch []Step
		if plan != nil {
			batch = plan(t)
		}
		tr.Results = append(tr.Results, sim.Tick(t, batch))
	}
	return tr
}
