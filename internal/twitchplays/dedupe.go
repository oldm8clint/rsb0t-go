package twitchplays

import "sync"

// Dedupe remembers message IDs so reconnects/replays cannot execute the same
// chat event twice. It is intentionally bounded by callers using Reset or a
// future expiring implementation when running long-lived streams.
type Dedupe struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewDedupe() *Dedupe { return &Dedupe{seen: make(map[string]struct{})} }

// First returns true exactly once for a non-empty message ID.
func (d *Dedupe) First(messageID string) bool {
	if messageID == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[messageID]; ok {
		return false
	}
	d.seen[messageID] = struct{}{}
	return true
}

func (d *Dedupe) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = make(map[string]struct{})
}
