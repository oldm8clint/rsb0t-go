package fleet

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/peerplan"
)

// THE FLEET'S SHARED BANK DIRECTORY.
//
// internal/peerbank was built so one account can answer "what is in somebody else's bank" —
// which is what lets a donor take a fair share of the mule's stockpile instead of asking
// blind, and what lets a script take a spare tool from the nearest peer instead of walking to
// the mule. It has been complete and unreachable: peershare's own claim recorded "STILL TO
// WIRE (deliberately not done): no script calls this yet ... internal/fleet must build one
// peerbank.Directory per process and set Context.Peers", and nothing ever did.
//
// The consequence, live on 2026-09-22: internal/bot.PublishBank returned immediately because
// PeerDeps found a nil Peers, so the mule's OnBankRead hook published into nothing and every
// donor asked for items with no idea whether the mule held them. Woodcutters asked for four
// axe tiers the mule had none of, over and over, with an 83-tile walk attached.
//
// ONE directory per process, shared by every account, because that is what makes it a
// directory: an account publishes its own bank and reads everybody else's out of the same
// map. It is in-process only — the same scope internal/mulebus has, and for the same reason.

// peerFields is what this file adds to Manager, embedded so fleet.go takes one line.
type peerFields struct {
	peersOnce sync.Once
	peers     *peerbank.Directory
}

// peerDirectory is the fleet's one shared bank directory, created on first use.
func (m *Manager) peerDirectory() *peerbank.Directory {
	m.peersOnce.Do(func() {
		m.peers = peerbank.New()
		donorrunner.SetMuleStock(m.peerStock) // the mule's coin cap reads the directory (donorrunner/coincap.go)
	})
	return m.peers
}

// peerStock is how many of item an account's last published bank held, and whether that bank is fresh.
func (m *Manager) peerStock(account, item string) (int, bool) {
	now := time.Now()
	for _, e := range m.peerDirectory().Entries(now) {
		if !strings.EqualFold(strings.TrimSpace(e.Account), strings.TrimSpace(account)) {
			continue
		}
		if e.UpdatedAt.IsZero() || now.Sub(e.UpdatedAt) > peerplan.DefaultMaxAge {
			return 0, false
		}
		n := 0
		for _, h := range e.Bank {
			if strings.EqualFold(strings.TrimSpace(h.Item), item) {
				n += h.Count
			}
		}
		return n, true
	}
	return 0, false
}

// Peers exposes the directory for the control panel and for a harness.
func (m *Manager) Peers() *peerbank.Directory { return m.peerDirectory() }
