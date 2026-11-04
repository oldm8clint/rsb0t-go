package bot

import "rsb0t-go/internal/gamestate"

// On ports AbstractBot.on: fn receives the session's events of the given kinds (every kind
// when none are given) until cancel is called or the Runner driving this Context finishes,
// whichever comes first. With no session it subscribes to nothing.
//
// Callbacks run on the session's packet-reading goroutine, as upstream's run "during client
// processing": keep them to setting flags or recording values, and act on those from Loop.
func (c *Context) On(fn gamestate.Subscriber, kinds ...gamestate.EventKind) (cancel func()) {
	st := c.gameState()
	if st == nil {
		return func() {}
	}
	handler := fn
	if len(kinds) > 0 {
		want := make(map[gamestate.EventKind]bool, len(kinds))
		for _, k := range kinds {
			want[k] = true
		}
		handler = func(ev gamestate.Event) {
			if want[ev.Kind] {
				fn(ev)
			}
		}
	}
	cancel = st.Subscribe(handler)

	c.subMu.Lock()
	c.subs = append(c.subs, cancel)
	c.subMu.Unlock()
	return cancel
}

// disposeSubscriptions cancels every subscription On made. The Runner calls it once OnStop has
// returned; cancelling an already-cancelled subscription is a no-op.
func (c *Context) disposeSubscriptions() {
	c.subMu.Lock()
	subs := c.subs
	c.subs = nil
	c.subMu.Unlock()
	for _, cancel := range subs {
		cancel()
	}
}
