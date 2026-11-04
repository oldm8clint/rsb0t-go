package actionsim

import "time"

// tickLength is the engine's tick.
const tickLength = 600 * time.Millisecond

// Clock is virtual time for a hold-loop test: a Delayer whose every Delay advances time and, with it, the session by the server
// ticks the time covers (AdvanceTick), so a loop that sleeps six times 100 ms sees one tick, and a test can schedule what the
// server does at a tick. Nothing in the loop under test sleeps for real; loops that watch time.Now still need to end by their own
// conditions.
type Clock struct {
	s       *Session
	now     time.Duration
	fed     int
	events  map[int][]func()
	delays  int
	stopped bool
}

// NewClock is a clock for s.
func NewClock(s *Session) *Clock { return &Clock{s: s, events: map[int][]func(){}} }

// Delay is a Deps.Delay: time passes, the server ticks, and it reports false once Stop was called.
func (c *Clock) Delay(d time.Duration) bool {
	c.delays++
	if c.stopped {
		return false
	}
	c.Advance(d)
	return true
}

// Advance lets d of virtual time pass without counting as a Delay.
func (c *Clock) Advance(d time.Duration) {
	c.now += d
	for int(c.now/tickLength) > c.fed {
		c.fed++
		c.s.AdvanceTick()
		tick := c.s.State().Tick()
		for _, f := range c.events[tick] {
			f()
		}
		delete(c.events, tick)
	}
}

// At runs f once the session's tick counter reaches tick (right after that tick's PLAYER_INFO): the server's side of a scenario.
func (c *Clock) At(tick int, f func()) { c.events[tick] = append(c.events[tick], f) }

// In runs f ticks from now.
func (c *Clock) In(ticks int, f func()) { c.At(c.s.State().Tick()+ticks, f) }

// Stop makes every later Delay report false, as a stopped bot's does.
func (c *Clock) Stop() { c.stopped = true }

// Delays is how many Delay calls were made.
func (c *Clock) Delays() int { return c.delays }

// Now is the virtual time that has passed.
func (c *Clock) Now() time.Duration { return c.now }
