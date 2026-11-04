package bot

// A carrier whose requests the game never answers (live 18 Oct 2026: eight of them, 17 requests a minute for 55 minutes, "he is busy or
// the queue is long") says what it saw, and its shift's end names it.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

func TestCarrierSilentNoScreenIsAWarningNotAFailureAndATradeForgivesIt(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	var answer runecraftrunner.OfferResult
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		if answer.Done {
			c.sim.Remove(runecraftplan.EssenceID, answer.Moved)
		}
		return answer
	}
	w := c.world()
	answer = runecraftrunner.OfferResult{NoScreen: true, Silent: true, Requests: 17, Took: time.Minute, Moves: 1,
		Reason: "no trade screen after 1m0s and 17 request(s) (the game said nothing of any of them; he stands at 2864,3020 and I stand at 2866,3020)"}
	for i := 0; i < 2*carrierStepFails; i++ {
		if res := w.trade(); !res.OK {
			t.Fatalf("attempt %d was a failure: %+v", i+1, res)
		}
	}
	if w.silentTimeouts != 2*carrierStepFails || w.tradeTimeouts != 2*carrierStepFails || w.tradeFails != 0 {
		t.Fatalf("silent %d timeouts %d fails %d", w.silentTimeouts, w.tradeTimeouts, w.tradeFails)
	}
	if !c.logged("WARNING: crafter deaf: no trade screen after 1m0s and 17 request(s) (the game said nothing of any of them; he stands at 2864,3020 and I stand at 2866,3020)") || !c.logged("1 move(s) tried") {
		t.Fatalf("log: %v", c.logs)
	}
	if c.logged("he is busy or the queue is long") {
		t.Fatalf("a silent crafter was called busy: %v", c.logs)
	}
	// A request that DID get a word and no screen is the ordinary wait, as before.
	answer = runecraftrunner.OfferResult{NoScreen: true, Requests: 17, Took: time.Minute, Reason: "no trade screen after 1m0s and 17 request(s)"}
	before := w.silentTimeouts
	if res := w.trade(); !res.OK || w.silentTimeouts != before {
		t.Fatalf("%+v silent %d", res, w.silentTimeouts)
	}
	if !c.logged("he is busy or the queue is long") {
		t.Fatalf("log: %v", c.logs)
	}
	// A trade clears the count.
	answer = runecraftrunner.OfferResult{Done: true, Moved: 25, Requests: 3, Took: 11 * time.Second}
	if res := w.trade(); !res.OK || w.silentTimeouts != 0 || w.trips != 1 {
		t.Fatalf("%+v silent %d trips %d", res, w.silentTimeouts, w.trips)
	}
}

func TestACarrierThatWaitedAtTheGateForSilenceEndsItsShiftSayingSo(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	c.sim.StandAt(exitX, exitZ)
	w := c.world()
	w.silentTimeouts = 9
	w.waitSince = time.Now().Add(-runecraftplan.GateWaitMax - time.Minute)
	snap := w.Read()
	if !snap.Over || !strings.Contains(w.over, "no trade") || !strings.Contains(w.over, "the game said nothing of the requests in 9 attempt(s)") {
		t.Fatalf("over %v %q", snap.Over, w.over)
	}
	// Without silence the reason is the plain one.
	w2 := c.world()
	w2.waitSince = time.Now().Add(-runecraftplan.GateWaitMax - time.Minute)
	w2.Read()
	if strings.Contains(w2.over, "the game said nothing") || !strings.Contains(w2.over, "at the gate with a load and no trade") {
		t.Fatalf("over %q", w2.over)
	}
}
