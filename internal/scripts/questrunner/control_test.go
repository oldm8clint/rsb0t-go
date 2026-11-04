package questrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
)

// The deeper control lets a quest go as far as it can without acquiring anything: bank reads,
// withdrawals and satisfied kits pass; the mule and every source are refused. Off, it refuses
// nothing.
func TestTheDeepControlRefusesOnlyTheMuleAndTheSources(t *testing.T) {
	on := &Provisioner{SourcesDisabled: true}
	for ph, want := range map[questplan.ProvisionPhase]bool{
		questplan.PhaseBank: false, questplan.PhaseSatisfied: false, questplan.PhaseBlocked: false,
		questplan.PhaseMule: true, questplan.PhaseSource: true,
	} {
		if _, refused := on.controlRefuses(ph); refused != want {
			t.Errorf("deep control, phase %s: refused %v, want %v", ph, refused, want)
		}
	}
	off := &Provisioner{}
	for _, ph := range []questplan.ProvisionPhase{questplan.PhaseMule, questplan.PhaseSource, questplan.PhaseBank} {
		if _, refused := off.controlRefuses(ph); refused {
			t.Errorf("no control, phase %s refused", ph)
		}
	}
}
