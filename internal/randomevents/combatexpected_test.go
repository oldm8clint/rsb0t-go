package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// Live-found 2026-09-18 (Scarab Swarm KQ reconnaissance): Guardian.CombatExpected was honoured
// by guardian.go's own kick() and by the three call sites that use EmergencyFleeCombatAware
// explicitly, and by nothing else — so an account whose script had correctly declared "my
// InCombat is expected" was still fled out of its own fight by the very next call to plain
// EmergencyFlee, which is what freshstartrunner.WalkNear and every skilling runner's
// WaitForGuardian loop actually use.
//
// The lookup has to satisfy three things at once: default false so every existing caller is
// unchanged, follow the flag once a Guardian sets it, and keep it per-account rather than
// process-wide.
func TestCombatExpectedForDefaultsFalseAndFollowsTheGuardian(t *testing.T) {
	unregistered := &safeRefSession{state: gamestate.New()}
	if combatExpectedFor(unregistered) {
		t.Fatal("a session with no registered Guardian reports CombatExpected — every caller that never sets the flag must be unchanged")
	}

	s := &safeRefSession{state: gamestate.New()}
	g := NewGuardian(s, nil)
	defer g.Stop()

	if combatExpectedFor(s) {
		t.Fatal("a Guardian that never set CombatExpected reports true")
	}
	g.CombatExpected = true
	if !combatExpectedFor(s) {
		t.Fatal("CombatExpected was set on the Guardian but the lookup still reports false")
	}
	g.CombatExpected = false
	if combatExpectedFor(s) {
		t.Fatal("clearing CombatExpected did not take — internal/bot/fisher.go sets this one way then the other every Loop, so it has to be readable both ways")
	}
}

// Per-account, not process-wide. internal/bot/fisher.go sets CombatExpected only while an
// account is in its fund phase AT its pickpocket spot, so on a fleet where one account is
// fighting and another is banking, one account's flag must not silently disarm the other's
// flee.
func TestCombatExpectedIsPerAccount(t *testing.T) {
	fighting := &safeRefSession{state: gamestate.New()}
	banking := &safeRefSession{state: gamestate.New()}
	gf := NewGuardian(fighting, nil)
	defer gf.Stop()
	gb := NewGuardian(banking, nil)
	defer gb.Stop()

	gf.CombatExpected = true
	if !combatExpectedFor(fighting) {
		t.Fatal("the fighting account does not report CombatExpected")
	}
	if combatExpectedFor(banking) {
		t.Fatal("one account's CombatExpected leaked to another — the flee would be disarmed fleet-wide")
	}
}

// A Guardian that has been stopped and deregistered must not keep an account's flee disarmed.
func TestCombatExpectedForFallsBackOnceTheGuardianIsGone(t *testing.T) {
	s := &safeRefSession{state: gamestate.New()}
	g := NewGuardian(s, nil)
	g.CombatExpected = true
	if !combatExpectedFor(s) {
		t.Fatal("CombatExpected not reported while the Guardian is registered")
	}
	g.Stop()
	if combatExpectedFor(s) {
		t.Fatal("a stopped Guardian still reports CombatExpected — an account whose script has shut down would never flee again")
	}
}

// The two exported entry points have to agree for an account that has declared the flag, and
// differ for one that has not: that difference is the whole reason plain EmergencyFlee needed
// changing rather than every caller being migrated to the CombatAware variant by hand.
func TestEmergencyFleeAndCombatAwareAgreeOnceTheFlagIsSet(t *testing.T) {
	s := &safeRefSession{state: gamestate.New()}
	g := NewGuardian(s, nil)
	defer g.Stop()

	// Not set: plain EmergencyFlee must resolve to the historical combatExpected=false.
	if combatExpectedFor(s) {
		t.Fatal("unset flag resolved true")
	}
	// Set: plain EmergencyFlee must now resolve to the same value the CombatAware variant
	// hardcodes, so the two cannot disagree for an account that declared it.
	g.CombatExpected = true
	if got, want := combatExpectedFor(s), true; got != want {
		t.Fatalf("plain EmergencyFlee resolves combatExpected=%v for an account that declared it; EmergencyFleeCombatAware hardcodes %v", got, want)
	}
}
