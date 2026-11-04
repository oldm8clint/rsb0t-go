package bot

import (
	"time"

	"rsb0t-go/internal/gamestate"
)

// Execution: the tick-driven half of the TS scripting API's Execution namespace
// (reference/server-api-docs.md). Context.Delay and Context.DelayUntil already cover
// delay/delayUntil; this adds delayTicks/delayUntilTicks on top of gamestate's server tick
// counter (one tick per observed PLAYER_INFO, the same thing upstream's BotHost.tickCount
// counts), plus the loop cadence TaskBot/TreeBot pace themselves with.
//
// Every wait here is cancelled by the Context's stop signal, like Delay.

// FrameInterval is how often tick-driven waits poll, standing in for the TS client's ~20 ms
// frame callback that settles its own scheduled waits.
const FrameInterval = 20 * time.Millisecond

// TickDuration is a server tick at normal engine speed. It is only used to pace a Context
// with no session to count ticks on; real waits count observed ticks, so they stay correct
// when a local engine runs with ::speed.
const TickDuration = 600 * time.Millisecond

// tickStallTimeout bounds how long DelayTicks waits without seeing any tick at all. A live
// session receives one every tick, so a gap this long means the connection is gone; without
// it a dead session would park a tick wait until the bot is stopped. It matches the
// transport's 30 s read timeout. A variable so tests can shorten it.
var tickStallTimeout = 30 * time.Second

func (c *Context) gameState() *gamestate.State {
	if c.Session == nil {
		return nil
	}
	return c.Session.State()
}

// Tick ports Game.tick: the number of server ticks this session has observed, or 0 without a
// session.
func (c *Context) Tick() int {
	if st := c.gameState(); st != nil {
		return st.Tick()
	}
	return 0
}

// SceneReady ports Game.sceneReady: logged in, the scene fully built, and a known tile. False
// without a session.
func (c *Context) SceneReady() bool {
	st := c.gameState()
	return st != nil && st.SceneReady()
}

// DelayTicks ports Execution.delayTicks: it waits until n more server ticks have been observed.
// It returns false if a stop is requested, if the Context has no session, or if no tick at all
// arrives for tickStallTimeout (a dead connection). n <= 0 only checks Stopped.
func (c *Context) DelayTicks(n int) bool {
	if n <= 0 {
		return !c.Stopped()
	}
	st := c.gameState()
	if st == nil {
		return false
	}
	last := st.Tick()
	target := last + n
	lastAdvance := time.Now()
	for {
		cur := st.Tick()
		if cur >= target {
			return true
		}
		if cur != last {
			last, lastAdvance = cur, time.Now()
		} else if time.Since(lastAdvance) >= tickStallTimeout {
			return false
		}
		if !c.Delay(FrameInterval) {
			return false
		}
	}
}

// DelayUntilTicks ports Execution.delayUntilTicks: it checks cond, then waits one tick, up to
// maxTicks times, and finally checks cond once more. It returns false straight away on a stop.
// Prefer it over DelayUntil when confirming something the server applies on a tick.
func (c *Context) DelayUntilTicks(cond func() bool, maxTicks int) bool {
	for i := 0; i < maxTicks; i++ {
		if cond() {
			return true
		}
		if !c.DelayTicks(1) {
			return !c.Stopped() && cond()
		}
	}
	return cond()
}

// Cadence is how long a TaskBot or TreeBot waits after each loop, porting Bot.ts's LoopCadence.
// The zero value waits one server tick.
type Cadence struct {
	// Every, when positive, is a wall-clock wait (LoopCadence {kind: 'time'}; FrameCadence
	// for {kind: 'frame'}). It overrides Ticks.
	Every time.Duration
	// Ticks is the number of server ticks to wait (LoopCadence {kind: 'server-tick'});
	// 0 means 1.
	Ticks int
}

// FrameCadence runs the next loop as soon as the next client frame would, for hot loops that
// already guard against dispatching twice.
var FrameCadence = Cadence{Every: FrameInterval}

// CadenceFromLoopDelay ports resolveLoopCadence: 0 is the next frame, 600 ms (the legacy "one
// tick") is one server tick, anything else is that wall-clock wait.
func CadenceFromLoopDelay(d time.Duration) Cadence {
	switch {
	case d <= 0:
		return FrameCadence
	case d == TickDuration:
		return Cadence{Ticks: 1}
	default:
		return Cadence{Every: d}
	}
}

// Pace waits out one cadence. Without a session to count ticks on, a tick cadence falls back
// to TickDuration per tick. It returns false when a stop is requested (or a tick wait gave up,
// see DelayTicks).
func (c *Context) Pace(cad Cadence) bool {
	if cad.Every > 0 {
		return c.Delay(cad.Every)
	}
	n := cad.Ticks
	if n <= 0 {
		n = 1
	}
	if c.gameState() == nil {
		return c.Delay(time.Duration(n) * TickDuration)
	}
	return c.DelayTicks(n)
}
