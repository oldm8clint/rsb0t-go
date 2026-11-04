package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// impKit is Imp Catcher's shape: one bead, one skill source in another kingdom.
func impKit() []questplan.KitLine {
	return []questplan.KitLine{{ID: 1470, Name: "Red bead", Qty: 1, Sources: []questplan.Source{{
		Kind: questplan.SourceSkill, Skill: "attack", Method: "imp-farm", Tile: tile.Tile{X: 2630, Z: 3220},
	}}}}
}

// deadSourceProvisioner is a provisioner whose bank visit always works (and reads an empty bank) and whose one
// skill source "walks" and fails: calls counts the walks.
func deadSourceProvisioner(calls *int) *Provisioner {
	p := &Provisioner{Methods: map[string]SkillMethod{"imp-farm": func(d *Deps, task questplan.SourceTask) bool {
		*calls++
		d.travelFails.Add(1) // what a Travel that ended unreachable does
		return false
	}}}
	p.bankVisitFn = func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
		p.Bank.Note(map[int]int{})
		return true
	}
	return p
}

// A source whose walk did not arrive is walked to once, then skipped at once for SourceDeadFor: the next
// Provision round fails fast without another walk, and says the round was blocked by travel.
func TestAnUnreachableSourceIsNotWalkedToAgainForAWhile(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	walks := 0
	p := deadSourceProvisioner(&walks)

	ok, _ := p.Provision(e.D, "imp-beads", impKit(), nil, tile.Tile{X: 3092, Z: 3245})
	if ok || walks != 1 {
		t.Fatalf("round 1: ok=%v walks=%d (%v)", ok, walks, sink.lines)
	}
	if !p.LastBlocked || !p.LastTravelBlocked {
		t.Fatalf("round 1 should be blocked by travel: blocked=%v travel=%v", p.LastBlocked, p.LastTravelBlocked)
	}
	if !sink.has("the walk there did not arrive") {
		t.Errorf("the dead source is not said once: %v", sink.lines)
	}
	for i := 2; i <= 4; i++ {
		ok, _ = p.Provision(e.D, "imp-beads", impKit(), nil, tile.Tile{X: 3092, Z: 3245})
		if ok || walks != 1 || !p.LastTravelBlocked {
			t.Fatalf("round %d: ok=%v walks=%d travelBlocked=%v: the dead source was walked to again", i, ok, walks, p.LastTravelBlocked)
		}
	}
	n := 0
	for _, l := range sink.lines {
		if strings.Contains(l, "the walk there did not arrive") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the dead source was announced %d times, want once", n)
	}
}

// Once SourceDeadFor has passed the source is tried again.
func TestADeadSourceIsTriedAgainAfterItsTime(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	walks := 0
	p := deadSourceProvisioner(&walks)
	p.Provision(e.D, "imp-beads", impKit(), nil, tile.Tile{})
	for k := range p.deadSources {
		p.deadSources[k] = p.deadSources[k].Add(-2 * SourceDeadFor)
	}
	p.Provision(e.D, "imp-beads", impKit(), nil, tile.Tile{})
	if walks != 2 {
		t.Fatalf("walks %d, want 2 (the source was never retried)", walks)
	}
}

// A kit whose sources are all unreachable parks the quest after ProvisionTravelParkRounds rounds, not at the end
// of the slot (live 7 Oct 2026: Imp Catcher asked for the imp field every five seconds for twenty minutes).
func TestAQuestWhoseOnlySourceIsUnreachableParksAfterAFewRounds(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	walks := 0
	e.Prov = deadSourceProvisioner(&walks)
	decides := 0
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "imp", Name: "imp"},
		Decide: func(questplan.Snapshot) questplan.Step {
			decides++
			return questplan.Step{Kind: questplan.StepProvision, KitName: "imp-beads", Kit: impKit(), BankSet: true}
		},
	}}
	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeParked || !strings.Contains(res.Reason, "every source unreachable") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, sink.lines)
	}
	if decides != ProvisionTravelParkRounds || walks != 1 {
		t.Fatalf("decides %d (want %d), walks %d (want 1)", decides, ProvisionTravelParkRounds, walks)
	}
}
