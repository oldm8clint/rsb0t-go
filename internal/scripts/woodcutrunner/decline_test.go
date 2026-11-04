package woodcutrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
)

// TestParseAskAgainIn pins the cooldown read out of a stall's decline.
//
// The live wording on 2026-09-21 was "Seers Market: Trade declined. Ask again in 15s." Until the
// fleet could hear that, a declined request looked like silence and the account blocked the full
// thirty-second window waiting for an offer that had already been refused.
func TestParseAskAgainIn(t *testing.T) {
	for _, c := range []struct {
		line string
		want time.Duration
	}{
		{"seers market: trade declined. ask again in 15s.", 15 * time.Second},
		{"seers market: trade declined. ask again in 5s", 5 * time.Second},
		// No time named: fall back rather than retry instantly and earn another decline.
		{"seers market: trade declined.", declineCooldown},
		{"seers market: trade declined. ask again in a bit", declineCooldown},
		{"seers market: trade declined. ask again in 0s", declineCooldown},
		// Absurd values are not honoured — a stall should not park an account for an hour.
		{"seers market: trade declined. ask again in 9999s", declineCooldown},
	} {
		if got := parseAskAgainIn(c.line); got != c.want {
			t.Errorf("parseAskAgainIn(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// TestSellerDeclinedFor pins which lines count as a decline at all.
func TestSellerDeclinedFor(t *testing.T) {
	for _, c := range []struct {
		name string
		line string
		want bool
	}{
		{"our stall declining", "Seers Market: Trade declined. Ask again in 15s.", true},
		{"another stall's decline is not ours", "Tradevault: Trade declined. Ask again in 15s.", false},
		{"an invitation is not a decline", "Seers Market: Got your rune axe. Trade me, acct223.", false},
		{"unrelated chatter", "Door Closer: The door demanded pink skirts.", false},
	} {
		log := chatlog.New()
		delivered := false
		d := &Deps{Chat: log, Delay: func(time.Duration) bool {
			if !delivered {
				log.Record(c.line)
				delivered = true
			}
			return true
		}}
		_, got := sellerDeclinedFor(d, "Seers Market", 50*time.Millisecond)
		if got != c.want {
			t.Errorf("%s: sellerDeclinedFor = %v, want %v", c.name, got, c.want)
		}
	}
}
