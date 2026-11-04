package randomevents

import (
	"time"

	"rsb0t-go/internal/actions"
)

// driftYieldWindow is how long one failed drift-recovery lock attempt keeps box/lamp handling
// stepped aside. EmergencyFlee is polled every 500ms by every script's WaitForGuardian loop,
// so a window a little over two polls is always refreshed while recovery keeps wanting in.
const driftYieldWindow = 1500 * time.Millisecond

// ownEvadeEventPresent reports whether the next event this Guardian would dispatch is the
// account's own event monster (KindEvade), which HandleEvade handles by that NPC rather than
// as an unnamed fight.
func (g *Guardian) ownEvadeEventPresent() bool {
	ev := g.nextEvent()
	return ev != nil && ev.Kind == KindEvade
}

// pursued reports whether a recorded attacker is still closing in on this account.
func (g *Guardian) pursued() bool {
	_, ok := pursuingAttacker(g.session)
	return ok
}

// requestDriftYield records that session's drift recovery wanted the action lock and could
// not have it.
//
// Live 2026-09-19 (spare102, herring, acct98 after deaths on c36f809): "[guardian] drifted
// far from home ... but the action lock is held elsewhere — will retry" repeated every five
// seconds for many minutes. The account had respawned at Lumbridge holding a stack of Strange
// boxes, and kick() re-took the lock for the next box 600ms after each solve released it, so
// the 500ms drift poll almost never found it free. A box keeps; an account 400 tiles from its
// script's home does not, so box and lamp handling now steps aside while recovery is waiting,
// and a box solve in progress yields at its next wait (waitEventReply).
func requestDriftYield(session actions.Session) {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil {
		return
	}
	g.mu.Lock()
	g.driftWantedUntil = time.Now().Add(driftYieldWindow)
	g.mu.Unlock()
}

// driftRecoveryWanted reports whether drift recovery asked for the lock recently.
func (g *Guardian) driftRecoveryWanted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Now().Before(g.driftWantedUntil)
}

// driftRecoveryWaiting is driftRecoveryWanted for session's registered Guardian, false when
// there is none.
func driftRecoveryWaiting(session actions.Session) bool {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	return g != nil && g.driftRecoveryWanted()
}
