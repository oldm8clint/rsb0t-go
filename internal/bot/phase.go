package bot

import (
	"sync"
	"time"
)

// scriptReport is what a running script tells the outside world about itself, beyond log lines:
// the phase it is in (for the control panel's "where the time goes" panel, internal/phaselog)
// and an optional structured status value (MuleReceiver publishes a *mulestatus.Tracker).
//
// Deliberately cheap: SetPhase is a mutex and a string compare, so a script can call it on every
// loop without thinking about it. The fleet samples it about once a second; it is never pushed.
type scriptReport struct {
	mu        sync.Mutex
	phase     string
	since     time.Time
	published any
}

// SetPhase reports the phase the script is in now ("fishing", "banking", ... — see
// internal/phaselog's names). Setting the same phase again keeps its original start time.
func (c *Context) SetPhase(phase string) {
	c.report.mu.Lock()
	if c.report.phase != phase {
		c.report.phase = phase
		c.report.since = time.Now()
	}
	c.report.mu.Unlock()
}

// PushPhase sets phase for the duration of one step and returns a func that restores whatever
// phase was reported before — for a sub-step such as a walk inside a bank trip. Safe to defer.
// The restore is skipped if the script moved on to another phase meanwhile.
func (c *Context) PushPhase(phase string) (restore func()) {
	c.report.mu.Lock()
	prev := c.report.phase
	if prev != phase {
		c.report.phase = phase
		c.report.since = time.Now()
	}
	c.report.mu.Unlock()
	return func() {
		c.report.mu.Lock()
		if c.report.phase == phase && prev != phase {
			c.report.phase = prev
			// The earlier phase resumes now; its segment restarts rather than claiming the
			// pushed step's time.
			c.report.since = time.Now()
		}
		c.report.mu.Unlock()
	}
}

// Phase is the script's current phase and when it began; "" if it never reported one.
func (c *Context) Phase() (string, time.Time) {
	c.report.mu.Lock()
	defer c.report.mu.Unlock()
	return c.report.phase, c.report.since
}

// Publish makes v the script's structured status, for a fleet or dashboard that knows its type.
// v must be safe to read from another goroutine (a *mulestatus.Tracker is).
func (c *Context) Publish(v any) {
	c.report.mu.Lock()
	c.report.published = v
	c.report.mu.Unlock()
}

// Published is whatever the script last passed to Publish, or nil.
func (c *Context) Published() any {
	c.report.mu.Lock()
	defer c.report.mu.Unlock()
	return c.report.published
}

// Phase is the running script's reported phase — see Context.Phase.
func (r *Runner) Phase() (string, time.Time) { return r.ctx.Phase() }

// Published is the running script's published status — see Context.Publish.
func (r *Runner) Published() any { return r.ctx.Published() }
