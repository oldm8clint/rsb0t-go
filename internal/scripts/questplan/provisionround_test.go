package questplan

import "testing"

// simulateRound drives PlanProvision the way questrunner.Provision does, over a world where the
// mule publishes everything but gives nothing and every shop fails, and records every phase.
// It is the round-level check the single-plan tests cannot make: across a whole round there is
// exactly one mule ask, and an untradeable line is never asked of the mule nor bought.
func simulateRound(t *testing.T, kit []KitLine, mule map[int]int) (asks int, askedIDs, shopIDs map[int]bool, final ProvisionPhase) {
	t.Helper()
	askedIDs, shopIDs = map[int]bool{}, map[int]bool{}
	st := ProvisionState{
		Pack: map[int]int{CoinsID: 5000}, BankKnown: true, Bank: map[int]int{},
		Mule: mule, MuleKnown: true, FreeSlots: 28, Exhausted: map[string]bool{}, TravelFloat: 200,
	}
	for pass := 0; pass < 16; pass++ {
		p := PlanProvision(kit, st)
		final = p.Phase
		switch p.Phase {
		case PhaseSatisfied, PhaseBlocked:
			return
		case PhaseBank:
			// A bank visit that withdraws what it planned; coins drawn once.
			for _, w := range p.Withdraw {
				st.Bank[w.ID] -= w.Count
				st.Pack[w.ID] += w.Count
				if w.ID == CoinsID {
					st.CoinsDrawn = true
				}
			}
		case PhaseMule:
			asks++
			for _, c := range p.MuleAsk {
				askedIDs[c.ID] = true
			}
			st.MuleAsked = true // the mule gave nothing
		case PhaseSource:
			for _, task := range p.Tasks {
				if task.Source.Kind == SourceShop {
					shopIDs[task.Line.ID] = true
				}
				st.Exhausted[SourceKey(task.Line, task.Source)] = true // every source fails
			}
		}
	}
	t.Fatalf("the round never ended (last phase %s)", final)
	return
}

func TestARoundAsksTheMuleOnceEvenWhenItGivesNothing(t *testing.T) {
	kit := []KitLine{testRope, testAir, testShark}
	asks, asked, _, final := simulateRound(t, kit, map[int]int{954: 11, 556: 5000, 385: 900})
	if asks != 1 {
		t.Fatalf("%d mule asks in one round, want exactly 1", asks)
	}
	if !asked[954] || !asked[556] || !asked[385] {
		t.Fatalf("the one ask did not bundle every tradeable short line: %v", asked)
	}
	if final != PhaseBlocked {
		t.Fatalf("round ended %s with nothing arriving, want blocked", final)
	}
}

func TestAnUntradeableLineIsNeverAskedOrBoughtInARound(t *testing.T) {
	key := testKey
	key.Sources = []Source{testBetty} // even with a shop named, a quest item is never bought
	kit := []KitLine{key, testRope}
	_, asked, shops, final := simulateRound(t, kit, map[int]int{298: 5, 954: 11})
	if asked[298] {
		t.Fatal("the untradeable key was asked of the mule")
	}
	if shops[298] {
		t.Fatal("the untradeable key was bought")
	}
	if final != PhaseBlocked {
		t.Fatalf("round ended %s without the key, want blocked", final)
	}
}
