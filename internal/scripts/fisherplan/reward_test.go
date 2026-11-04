package fisherplan

import "testing"

func TestPhaseRewardFish(t *testing.T) {
	if r := PhaseReward(PhaseFish, TraceProgress{FishCaught: 3}); r != 3 {
		t.Fatalf("got %v, want 3", r)
	}
	if r := PhaseReward(PhaseFish, TraceProgress{}); r != -0.1 {
		t.Fatalf("got %v, want -0.1", r)
	}
}

func TestPhaseRewardBank(t *testing.T) {
	if r := PhaseReward(PhaseBank, TraceProgress{BankTrip: true}); r != 0.5 {
		t.Fatalf("got %v, want 0.5", r)
	}
	if r := PhaseReward(PhaseBank, TraceProgress{}); r != -0.01 {
		t.Fatalf("got %v, want -0.01", r)
	}
}

func TestPhaseRewardBuy(t *testing.T) {
	if r := PhaseReward(PhaseBuy, TraceProgress{ToolBought: true}); r != 1 {
		t.Fatalf("got %v, want 1", r)
	}
	if r := PhaseReward(PhaseBuy, TraceProgress{}); r != -0.1 {
		t.Fatalf("got %v, want -0.1", r)
	}
}

func TestPhaseRewardSell(t *testing.T) {
	if r := PhaseReward(PhaseSell, TraceProgress{SoldGP: 200}); r != 2 {
		t.Fatalf("got %v, want 2", r)
	}
	if r := PhaseReward(PhaseSell, TraceProgress{}); r != -0.1 {
		t.Fatalf("got %v, want -0.1", r)
	}
}

func TestPhaseRewardSetupPhasesAreZero(t *testing.T) {
	for _, p := range []FishPhase{PhaseAccept, PhaseTutorial, PhaseBright, PhaseStore} {
		if r := PhaseReward(p, TraceProgress{}); r != 0 {
			t.Fatalf("phase %q: got %v, want 0", p, r)
		}
	}
}

func TestPhaseRewardMisc(t *testing.T) {
	cases := []struct {
		phase FishPhase
		want  float32
	}{
		{PhaseFund, -0.01},
		{PhaseTravel, -0.01},
		{PhaseWait, -0.05},
		{PhaseStuck, -1},
	}
	for _, c := range cases {
		if r := PhaseReward(c.phase, TraceProgress{}); r != c.want {
			t.Fatalf("phase %q: got %v, want %v", c.phase, r, c.want)
		}
	}
}
