package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/protocol/clientprot"
)

// watchmanType is content's macro_watchman2, the Watchman that killed acct125 (live 2026-09-26).
const watchmanType = 432

// threatAttacking is true only for the event's own NPC, close, and attacking: facing the account,
// or the account taking hits.
func TestThreatAttackingIsTheEventNPCCloseAndOnTheAccount(t *testing.T) {
	s := combatSession()
	addNPCFacing(s.state, 77, watchmanType, 1, 0, playerFaceBase+testSelfSlot)
	ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 77, NpcType: watchmanType}
	if !threatAttacking(s.state, ev) {
		t.Fatal("a Watchman beside the account and facing it is not attacking")
	}
	if threatAttacking(s.state, &Event{Kind: KindEvade, NpcIndex: 78}) {
		t.Fatal("an NPC not in view is attacking")
	}
	far := combatSession()
	addNPCFacing(far.state, 77, watchmanType, 5, 0, playerFaceBase+testSelfSlot)
	if threatAttacking(far.state, ev) {
		t.Fatal("a Watchman five tiles away is attacking")
	}
	other := combatSession()
	addNPCFacing(other.state, 77, watchmanType, 1, 0, playerFaceBase+testSelfSlot+1)
	if threatAttacking(other.state, ev) {
		t.Fatal("a Watchman facing another player, with no hits on this account, is attacking it")
	}
	if threatAttacking(nil, ev) || threatAttacking(s.state, nil) {
		t.Fatal("nil state or event")
	}
}

// Cornered and attacked: the account attacks the threat, the event is deferred for the window so
// Busy() lets the script (and its eat step) run, and the in-combat flees stand down for the window.
func TestACorneredAccountFightsBackAndIsReleasedToItsScript(t *testing.T) {
	s := combatSession()
	g := NewGuardian(s, nil)
	t.Cleanup(g.unregister)
	addNPCFacing(s.state, 77, watchmanType, 1, 0, playerFaceBase+testSelfSlot)
	ev := Detect(s.state)
	if ev == nil || ev.Kind != KindEvade || ev.NpcIndex != 77 {
		t.Fatalf("the Watchman is not detected as the account's evade event: %+v", ev)
	}
	if !g.Busy() {
		t.Fatal("the guardian is not busy with a Watchman beside the account")
	}
	if combatExpectedFor(s) {
		t.Fatal("combat is expected before any fight-back")
	}
	var said []string
	if !fightBackCornered(s, ev, func(l string) { said = append(said, l) }) {
		t.Fatal("fightBackCornered did not act")
	}
	if len(s.sent) != 1 || int(s.sent[0]) != clientprot.OPNPC2 {
		t.Fatalf("sent %v, want one OPNPC2 (Attack) at the Watchman", s.sent)
	}
	if !g.cornered() {
		t.Fatal("no fight-back window recorded")
	}
	if g.Busy() {
		t.Fatal("the guardian still holds the account during the window: the script cannot eat")
	}
	if !combatExpectedFor(s) {
		t.Fatal("EmergencyFlee would still flee the fight during the window")
	}
	if len(said) != 1 {
		t.Fatalf("log lines %v", said)
	}
	// The window ends: the guardian owns the event again.
	g.mu.Lock()
	g.corneredUntil = time.Now().Add(-time.Second)
	for k := range g.retryUntil {
		g.retryUntil[k] = time.Now().Add(-time.Second)
	}
	g.mu.Unlock()
	if g.cornered() || combatExpectedFor(s) {
		t.Fatal("the window never ends")
	}
	if !g.Busy() {
		t.Fatal("after the window the guardian does not take the event back")
	}
}

// HandleEvade with nowhere to go: an attacking threat is fought (nil, one attack sent, no 3 s
// hold); a threat only in view keeps the old no-escape error.
func TestHandleEvadeWithNoEscapeFightsAnAttackingThreat(t *testing.T) {
	s := combatSession()
	g := NewGuardian(s, nil)
	t.Cleanup(g.unregister)
	addNPCFacing(s.state, 77, watchmanType, 1, 0, playerFaceBase+testSelfSlot)
	ev := Detect(s.state)
	start := time.Now()
	if err := HandleEvade(s, nil, ev, 1, nil); err != nil {
		t.Fatalf("cornered and attacked: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("the fight-back waited %s", time.Since(start))
	}
	if !g.cornered() || len(s.sent) == 0 || int(s.sent[len(s.sent)-1]) != clientprot.OPNPC2 {
		t.Fatalf("no fight-back: cornered=%v sent=%v", g.cornered(), s.sent)
	}

	idle := combatSession()
	gi := NewGuardian(idle, nil)
	t.Cleanup(gi.unregister)
	addNPCFacing(idle.state, 77, watchmanType, 2, 0, -1) // in view, facing nobody, no hits
	iev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 77, NpcType: watchmanType, ThreatX: 3202, ThreatZ: 3200}
	if err := HandleEvade(idle, nil, iev, 1, nil); err == nil {
		t.Fatal("a threat that is not attacking was fought instead of reporting no escape")
	}
	if gi.cornered() {
		t.Fatal("a non-attacking threat opened a fight-back window")
	}
}
