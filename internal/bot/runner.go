package bot

import (
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// State is a Runner's coarse lifecycle state, for a dashboard to render.
type State string

const (
	StateIdle     State = "idle"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateError    State = "error"
)

// Status is a Runner's reportable state, safe to copy and safe to read concurrently with
// Runner.Start/Stop.
type Status struct {
	Script    string
	State     State
	Err       string
	StartedAt time.Time
	// StoppedAt is when the run loop first left Running (a stop honoured, the script ending itself,
	// an error); zero while it runs. OnStop may still be running then: Finished says when it is done.
	StoppedAt time.Time
}

// loopFloor is the shortest one Loop pass may take; a pass that returns sooner is padded to it (cancellably) before the next.
//
// This is the same floor ScarabSwarm's stage loops already put under their own dispatch (scarabplan.IdleStepFloor, 150 ms), for
// every other script. A Loop that re-derives its phase from live state and returns at once when a primitive did nothing, or
// hands back a chat for the next pass to answer, is called again at once by run below: CPU speed (freshstartrunner.SailLeg
// 398,648 times in 27 seconds; SkipTutorial's Talk-to ~25 times in a millisecond, until the server disconnected the account), and
// when each pass sends a click, thousands of packets the engine reads five at a time (essencerunner.Mine, 15 Oct 2026, ten
// minutes behind its own queue: docs/click_flood_audit_2026-10-15.md). It is well under a tick, so a pass that does real work
// is not slowed and a pass that does nothing costs about seven wakeups a second instead of a core. It bounds a spin; it is
// not the fix for one, which is the click gate (internal/actions/clickgate.go) and a wait in the loop that needs one.
const loopFloor = 150 * time.Millisecond

// Runner drives one Bot instance against one Context: OnStart once, then Loop repeatedly
// until it returns an error or a stop is requested between calls, then OnStop once. This is
// what fleet.Manager.StartBot/StopBot hold per account.
type Runner struct {
	script string
	b      Bot
	ctx    *Context
	// settings are the settings the script was started with. A script may swap ctx.Settings for
	// a while (RuneRunner runs ScarabSwarm with its swarmSettings while it waits), and a restart
	// must not pick up the swapped map.
	settings map[string]interface{}

	mu     sync.RWMutex
	status Status
	done   chan struct{}
	// stopCalled is Stop having been called, by anyone outside the script: an operator, the
	// watchdog, a world hop, the connection dropping. A script ending itself reaches the stop
	// channel through Context.requestStop and never through Stop, which is how the fleet tells a
	// bot somebody stopped from one that finished its own job (StoppedFromOutside).
	stopCalled atomic.Bool
	// trawler is the trawler crew this account runs while the anchor's block is on (runner_trawler.go).
	trawler runnerTrawler
}

// NewRunner builds a Runner for b against ctx, reporting as script in Status(). Call Start to
// actually run it.
func NewRunner(script string, b Bot, ctx *Context) *Runner {
	return &Runner{
		script:   script,
		b:        b,
		ctx:      ctx,
		settings: ctx.Settings,
		status:   Status{Script: script, State: StateIdle},
		done:     make(chan struct{}),
	}
}

// Start runs the bot in a new goroutine and returns immediately; poll Status() for progress.
// Calling Start more than once has no effect beyond the first.
func (r *Runner) Start() {
	r.mu.Lock()
	if r.status.State != StateIdle {
		r.mu.Unlock()
		return
	}
	r.status.State = StateRunning
	r.status.StartedAt = time.Now()
	r.mu.Unlock()
	go r.run()
}

func (r *Runner) run() {
	defer close(r.done)
	// A panicking Bot must never take the whole fleet process down with it — one
	// misconfigured or buggy script would otherwise kill every other account's bot too.
	defer func() {
		if p := recover(); p != nil {
			r.finish(StateError, fmt.Sprintf("panic: %v\n%s", p, debug.Stack()))
		}
	}()
	// Deferred before safeOnStop so it runs after it: OnStop may still read events.
	defer r.ctx.disposeSubscriptions()
	defer r.safeOnStop()

	// Every bone any script buries reaches the loot panel's Prayer card (lootprayer.go), without
	// any script having to report it. Before OnStart, so a burial in OnStart is seen too.
	r.ctx.watchBurials()
	// Every teleport any script casts reaches the Teleports tab, with the runes it cost (lootcast.go).
	r.ctx.watchCasts()
	// The experience each skill earns reaches the skill cards and the kills that earned it (lootxp.go).
	r.ctx.watchExperience()
	// Every food and drink any script eats or drinks reaches the Consumables tab (lootconsume.go).
	r.ctx.watchConsumption()
	// Every item any script buys from a shop reaches the Shop purchases tab, with the coins it cost
	// (lootshop.go).
	r.ctx.watchShopPurchases()
	// Every flax any script picks reaches the Picking card (lootpick.go).
	r.ctx.watchPicking()

	if err := r.b.OnStart(r.ctx); err != nil {
		r.finish(StateError, err.Error())
		return
	}
	for {
		if r.ctx.Stopped() {
			r.finish(StateStopped, "")
			return
		}
		passStarted := time.Now()
		if r.trawlerTick() {
			// the trawler crew's pass (runner_trawler.go)
		} else if err := r.b.Loop(r.ctx); err != nil {
			r.finish(StateError, err.Error())
			return
		}
		if r.ctx.Stopped() {
			r.finish(StateStopped, "")
			return
		}
		if spent := time.Since(passStarted); spent < loopFloor {
			r.ctx.Delay(loopFloor - spent) // false on a stop: the check at the top of the loop ends the run
		}
	}
}

// safeOnStop calls the Bot's OnStop and recovers a panic from it independently of run's own
// recover, so OnStop is guaranteed to run to completion (or fail safely) even if OnStart/Loop
// already panicked and is mid-unwind.
func (r *Runner) safeOnStop() {
	defer func() {
		if p := recover(); p != nil {
			r.finish(StateError, fmt.Sprintf("panic in OnStop: %v\n%s", p, debug.Stack()))
		}
	}()
	r.b.OnStop(r.ctx)
}

func (r *Runner) finish(state State, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.State = state
	r.status.Err = errMsg
	if r.status.StoppedAt.IsZero() {
		r.status.StoppedAt = time.Now()
	}
}

// Stop requests cancellation: any in-flight Context.Delay/DelayUntil call returns
// immediately, and the run loop exits (calling OnStop) as soon as the current Loop call
// returns, rather than starting another. It does not block until the bot has actually
// stopped — poll Status() or call Wait for that.
func (r *Runner) Stop() {
	r.stopCalled.Store(true)
	r.ctx.requestStop()
}

// StoppedFromOutside reports whether Stop was called on this Runner, by anything but the script
// itself. False for a run that ended on its own (a script that finished its job and stopped its
// own Context, or returned an error), which is how internal/fleet's watchdog tells a bot somebody
// stopped from one that is done and must be left alone.
func (r *Runner) StoppedFromOutside() bool {
	return r.stopCalled.Load()
}

// Wait blocks until the run loop has fully exited (OnStop has returned).
func (r *Runner) Wait() {
	<-r.done
}

// Done is closed once the run loop has fully exited (OnStop has returned), for a caller that
// wants to wait with a timeout or alongside other events instead of blocking in Wait.
func (r *Runner) Done() <-chan struct{} {
	return r.done
}

// Finished reports whether the run loop has fully exited (OnStop has returned), without blocking.
func (r *Runner) Finished() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// Status returns a snapshot of the runner's current state.
func (r *Runner) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := r.status
	if status.State == StateRunning && r.ctx.Stopped() {
		status.State = StateStopping
	}
	return status
}

// Settings returns the settings map this Runner's Context was built with (see NewContext) —
// never nil, and never mutated after construction (no Bot in this package writes back into
// Context.Settings), so it's safe for a caller (fleet.Manager.Snapshot, in practice) to hand
// straight to a dashboard/JSON encoder without copying.
func (r *Runner) Settings() map[string]interface{} {
	return r.settings
}

// RequestHandoff asks this Runner's Bot for a manual mule hand-off at its next check — see
// Context.RequestHandoff. Safe to call whether or not the running script ever looks at it.
func (r *Runner) RequestHandoff() {
	r.ctx.RequestHandoff()
}

// SetMuleAt live-overrides this Runner's Bot's own muleAt threshold — see
// Context.SetMuleAt. n <= 0 clears the override.
func (r *Runner) SetMuleAt(n int) {
	r.ctx.SetMuleAt(n)
}

// MuleAtOverride reports the live muleAt override set via SetMuleAt, if any.
func (r *Runner) MuleAtOverride() (int, bool) {
	return r.ctx.MuleAtOverride()
}

// FinishRequested reports whether this Runner's Bot declared itself finished for good — see
// Context.RequestFinish. internal/fleet uses it to tell a deliberate, final logout apart from
// a dropped connection, so it does not reconnect the account and restart the script.
func (r *Runner) FinishRequested() (string, bool) {
	return r.ctx.FinishRequested()
}
