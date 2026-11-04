package gemrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/gemplan"
)

// THE PIPELINE. One use of a tool on a held item a user event, five user events a game tick: the
// engine reads each player's queued client packets in order and stops for the tick after five
// user events that were handled (engine/src/engine/entity/NetworkPlayer.ts decodeIn, with
// ClientGameProtCategory.USER_EVENT's limit 5); the bytes behind them stay in the socket buffer
// (ClientSocket.in, 64 KB) for the next tick. A use whose script has no delay (a chisel on an uncut
// gem or an oyster pearl, bolt tips on bolts) is applied in that same read.
//
// So the fastest a pack is worked is one burst of five a tick, and nothing is gained by waiting for
// a burst to show in the pack before the next goes: the next burst only has to name slots the
// earlier ones did not. UseAll sends a burst, waits for the next server tick (a PLAYER_INFO, which
// the server sends every tick: gamestate.State.Tick), sends the next to the slots not yet named,
// and confirms ONCE at the end that the pack shows them all. Before, every burst waited to be seen
// in the pack (a tick for the engine, the trip back, and a 100 ms poll on top), which took 27 gems
// from six ticks to twice that.

// tickPoll is how often UseAll looks for the next server tick: well inside one 600 ms tick, so a
// burst goes out within a few milliseconds of the tick that makes room for it.
const tickPoll = 10 * time.Millisecond

// tickFallback is how long UseAll waits for a tick it does not see (a lag spike, no PLAYER_INFO)
// before it sends the next burst anyway. The engine queues what it cannot read this tick, so a
// burst sent early is late, never lost.
const tickFallback = 650 * time.Millisecond

// useConfirm is how long the pack has, after the LAST burst went out, to show every use applied:
// the backlog the engine still holds drains five a tick, then a tick back. A use the engine dropped
// (the pack changed under it) is simply left for the caller's next pass or bank visit.
const useConfirm = 2500 * time.Millisecond

// NextTick waits until the session has seen a server tick after from (gamestate.State.Tick), at most
// tickFallback, and reports false when the caller should stop.
func NextTick(d *Deps, from int) bool {
	deadline := time.Now().Add(tickFallback)
	for d.Session.State().Tick() == from && time.Now().Before(deadline) {
		if !d.Delay(tickPoll) {
			return false
		}
	}
	return true
}

// UseOptions shapes one UseAll.
type UseOptions struct {
	// Tool is used on every held Target (the chisel on uncut gems or oyster pearls).
	Tool, Target int32
	// Refused reads the chat since the run began and names a reason to stop at once ("refused",
	// "members"), with the level a refusal demanded; "" carries on.
	Refused func(said string) (reason string, level int)
	// AfterLast, when set, runs the moment the last burst has gone out, before the confirm: the
	// caller's next bank open, so it reaches the engine in the same tick as the last uses (after
	// them: a use clears a pending interaction, engine OpHeldUHandler clearPendingAction).
	AfterLast func()
}

// UseResult is what one UseAll did.
type UseResult struct {
	// Bursts and Sent count the bursts and the uses that went out; Left is how many Targets the
	// pack still showed when it ended.
	Bursts, Sent, Left int
	// Reason is "done" (the pack shows no Target), "no-tool", "refused", "members", "stalled" (the
	// uses changed nothing), "partial" (some uses were dropped: Left of them are still held) or
	// "stopped".
	Reason       string
	RefusedLevel int
	// Ticks is how many server ticks the run spanned, from the first burst to the confirm.
	Ticks int
}

// unnamed is how many pack slots hold target that no burst has named yet.
func unnamed(d *Deps, target int32, named map[int]bool) int {
	n := 0
	for _, it := range packSlots(d) {
		if it.ID == target && !named[it.Slot] {
			n++
		}
	}
	return n
}

// UseAll uses opts.Tool on every held opts.Target, five a server tick, pipelined (see above), and
// confirms once at the end. It makes ONE pass: a use the engine dropped leaves its Target held
// ("partial"), for the caller to bank and draw again rather than a second pass that would race the
// bank open AfterLast already sent.
func UseAll(d *Deps, o UseOptions) (res UseResult) {
	eq := actions.NewEquipment(d.Session, d.Components)
	start := CountByID(d, o.Target)
	if start == 0 {
		res.Reason = "done"
		return res
	}
	if CountByID(d, o.Tool) == 0 {
		res.Reason = "no-tool"
		return res
	}
	at := time.Now()
	tick0 := d.Session.State().Tick()
	refused := func() bool {
		if o.Refused == nil || d.Chat == nil {
			return false
		}
		if why, level := o.Refused(d.Chat.Since(at)); why != "" {
			res.Reason, res.RefusedLevel = why, level
			return true
		}
		return false
	}
	defer func() { res.Ticks = d.Session.State().Tick() - tick0 }()
	named := map[int]bool{}
	for {
		if refused() {
			res.Left = CountByID(d, o.Target)
			return res
		}
		tick := d.Session.State().Tick()
		slots, err := eq.UseOnHeldManyExcept(o.Tool, o.Target, gemplan.BurstSize, named)
		if len(slots) == 0 {
			if res.Sent == 0 {
				d.logf("useall: %d on %d: %v", o.Tool, o.Target, err)
				res.Reason, res.Left = "stalled", start
				return res
			}
			break
		}
		res.Bursts++
		res.Sent += len(slots)
		for _, s := range slots {
			named[s] = true
		}
		if unnamed(d, o.Target, named) == 0 {
			break // the last burst: AfterLast goes out right behind it, in the same tick
		}
		if !NextTick(d, tick) {
			res.Reason, res.Left = "stopped", CountByID(d, o.Target)
			return res
		}
	}
	if o.AfterLast != nil {
		o.AfterLast()
	}
	// Confirm once: every use applied shows as one Target fewer.
	want := max(0, start-res.Sent)
	deadline := time.Now().Add(useConfirm)
	for CountByID(d, o.Target) > want && time.Now().Before(deadline) {
		if refused() {
			res.Left = CountByID(d, o.Target)
			return res
		}
		if !d.Delay(tickPoll) {
			res.Reason, res.Left = "stopped", CountByID(d, o.Target)
			return res
		}
	}
	res.Left = CountByID(d, o.Target)
	if refused() {
		return res
	}
	switch {
	case res.Left == 0:
		res.Reason = "done"
	case res.Left >= start:
		res.Reason = "stalled"
	default:
		res.Reason = "partial"
	}
	return res
}
