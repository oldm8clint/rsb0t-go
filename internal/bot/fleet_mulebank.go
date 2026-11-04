package bot

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/peerrunner"
)

// muleBankKinds is the mule's published bank (the peer board) as lower-case names, and its row
// count; known false when it has not published one recently.
func muleBankKinds(ctx *Context, mule string) (map[string]bool, int, bool) {
	if ctx == nil || ctx.Peers == nil {
		return nil, 0, false
	}
	want := strings.ToLower(strings.TrimSpace(mule))
	now := time.Now()
	for _, e := range ctx.Peers.Entries(now) {
		if strings.ToLower(strings.TrimSpace(e.Account)) != want {
			continue
		}
		if e.UpdatedAt.IsZero() || now.Sub(e.UpdatedAt) > peerplan.DefaultMaxAge || len(e.Bank) == 0 {
			return nil, 0, false
		}
		// The merged rows only: a bow or a grimy herb is also published once per obj under a qualified
		// name, and those extra rows are not rows of the mule's bank (counting them would make the bank
		// look fuller than it is, and hold back loot types it has room for).
		out := make(map[string]bool, len(e.Bank))
		for _, h := range peerrunner.PlainRows(e.Bank) {
			if h.Count > 0 {
				out[strings.ToLower(strings.TrimSpace(h.Item))] = true
			}
		}
		return out, len(out), true
	}
	return nil, 0, false
}
