package runecraftrunner

import (
	"slices"
	"testing"

	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// The ruins and the portal are jumps the account makes on purpose, and the Guardian is told before each (live 14 Oct 2026: ten
// "[guardian] displaced ... UNEXPLAINED" lines for MasterCrafter's Air and Body trips, 2984,3294 -> 2841,4830 and back).
func TestTheRuinsAndThePortalAreAnnouncedToTheGuardian(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsStand, 1)
	randomevents.ForgetTeleports(d.Session)
	t.Cleanup(func() { randomevents.ForgetTeleports(d.Session) })
	a := runecraftplan.Air

	if r := UseTalisman(d, a, airRuinsMatch()); !r.Arrived {
		t.Fatalf("%+v", r)
	}
	want := "runecraft: the Air talisman on the Air ruins"
	if got := randomevents.ExpectedJumps(d.Session); !slices.Contains(got, want) {
		t.Errorf("after the ruins the Guardian expects %v, want %q among them (the jump landed, nothing has used it up here)", got, want)
	}

	c := Craft(d, a, airAltarMatch())
	if !c.OK() {
		t.Fatalf("%+v", c)
	}
	if p := PortalOut(d, a, airPortalMatch(), c.Tick, nil); !p.Out {
		t.Fatalf("%+v", p)
	}
	want = "runecraft: the Air temple's portal"
	if got := randomevents.ExpectedJumps(d.Session); !slices.Contains(got, want) {
		t.Errorf("after the portal the Guardian expects %v, want %q among them", got, want)
	}
}

// A crossing that did not happen leaves nothing expected: the announcement is withdrawn.
func TestAFailedCrossingWithdrawsItsAnnouncement(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsBad, 1)
	randomevents.ForgetTeleports(d.Session)
	t.Cleanup(func() { randomevents.ForgetTeleports(d.Session) })
	if r := UseTalisman(d, runecraftplan.Air, airRuinsMatch()); r.Arrived {
		t.Fatalf("%+v", r)
	}
	if got := randomevents.ExpectedJumps(d.Session); len(got) != 0 {
		t.Errorf("a ruins use that failed left %v expected", got)
	}

	_, d = newEngine(t, runecraftplan.Air, nil, runecraftplan.Air.Enter, 1)
	t.Cleanup(func() { randomevents.ForgetTeleports(d.Session) })
	bad := airPortalMatch()
	bad.ID++
	if out := PortalOut(d, runecraftplan.Air, bad, 0, nil); out.Out {
		t.Fatalf("%+v", out)
	}
	if got := randomevents.ExpectedJumps(d.Session); len(got) != 0 {
		t.Errorf("a portal that never took left %v expected", got)
	}
}

// A refused use never announces anything (no talisman, nothing to land).
func TestNoTalismanAnnouncesNothing(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, essence(27), airRuinsStand, 1)
	t.Cleanup(func() { randomevents.ForgetTeleports(d.Session) })
	UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if got := randomevents.ExpectedJumps(d.Session); len(got) != 0 {
		t.Errorf("expected %v", got)
	}
}
