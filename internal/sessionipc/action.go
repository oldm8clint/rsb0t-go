package sessionipc

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ActionCall is a request to run one named, host-side action for one account — the
// write-path counterpart to PacketFrame's read-path mirroring. A future brain process
// never builds or sends a raw game packet itself (see StateMirror's doc comment for why
// that has to stay host-side); instead it names an action already implemented in
// internal/actions (e.g. "OpPlayer4", "InvButton") and passes its arguments as opaque
// JSON, and the host process — the only place holding the real client.Session, ISAAC
// ciphers and anticheat.Counters — looks up and runs the real function.
type ActionCall struct {
	Account string          `json:"account"`
	Name    string          `json:"name"`
	Args    json.RawMessage `json:"args,omitempty"`
}

// ActionResult is Dispatcher's reply to one ActionCall: Err is empty on success, or the
// failing call's error message otherwise — a plain string, not a Go error, since it has
// to cross the wire.
type ActionResult struct {
	Err string `json:"err,omitempty"`
}

// OK reports whether the call succeeded.
func (r ActionResult) OK() bool { return r.Err == "" }

// Handler runs one named action against account with its JSON-encoded args.
type Handler func(account string, args json.RawMessage) error

// Dispatcher is a name -> Handler registry, safe for concurrent Register/Dispatch — the
// host side of the action-call boundary. A real integration would register one Handler
// per internal/actions function it wants reachable over IPC: a thin, mechanical shim
// that decodes Args, calls the real function against the real client.Session, and
// returns its error — not a reimplementation of any game logic here.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewDispatcher builds an empty Dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]Handler)}
}

// Register adds fn under name, replacing any previous registration for that name — a
// deliberately softer rule than internal/bot.Register's panic-on-duplicate, since a
// dispatcher may legitimately want to swap a handler out (tests replacing a stub, or a
// future hot-reload of the handler table itself).
func (d *Dispatcher) Register(name string, fn Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[name] = fn
}

// Dispatch runs the handler registered for call.Name, if any, and converts its result
// into an ActionResult a Server can send back over the wire.
func (d *Dispatcher) Dispatch(call ActionCall) ActionResult {
	d.mu.RLock()
	fn, ok := d.handlers[call.Name]
	d.mu.RUnlock()
	if !ok {
		return ActionResult{Err: fmt.Sprintf("sessionipc: no handler registered for action %q", call.Name)}
	}
	if err := fn(call.Account, call.Args); err != nil {
		return ActionResult{Err: err.Error()}
	}
	return ActionResult{}
}
