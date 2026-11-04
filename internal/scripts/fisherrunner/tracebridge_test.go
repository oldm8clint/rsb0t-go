package fisherrunner

import (
	"os"
	"path/filepath"
	"testing"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/ml/evaluation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/replay"
	"rsb0t-go/internal/ml/trace"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/scripts/fisherplan"
)

// fakeSession is the minimal actions.Session this package's own tests need — a real
// *gamestate.State (so State()-reading code, e.g. ObservationFromSnapshot's HP()/
// Inventory() calls, behaves exactly like production) with Send/Cheats stubbed out,
// mirroring internal/actions' own fakeSession pattern for the identical reason.
type fakeSession struct {
	state  *gamestate.State
	cheats *anticheat.Counters
}

func newFakeSession() *fakeSession {
	return &fakeSession{state: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (f *fakeSession) Send(build func(w *packet.Packet)) error { return nil }
func (f *fakeSession) State() *gamestate.State                 { return f.state }
func (f *fakeSession) Cheats() *anticheat.Counters             { return f.cheats }

func TestObservationFromSnapshotHandlesUnknownPosition(t *testing.T) {
	d := &Deps{Session: newFakeSession()}
	obs := ObservationFromSnapshot(d, fisherplan.Catherby(), fisherplan.FishSnapshot{})
	if obs.X != 0 || obs.Y != 0 {
		t.Fatalf("expected zero position with a nil Tile, got %d,%d", obs.X, obs.Y)
	}
	// Nothing places the account near the bank or the camp without a known tile — both
	// read as "far" (1), matching FisherEnv's own boolDistance(true) for "not there".
	if obs.DistanceToBank != 1 || obs.DistanceToFish != 1 {
		t.Fatalf("expected far/far with no known tile, got bank=%d fish=%d", obs.DistanceToBank, obs.DistanceToFish)
	}
}

func TestObservationFromSnapshotDetectsNearBank(t *testing.T) {
	d := &Deps{Session: newFakeSession()}
	layout := fisherplan.Catherby()
	at := layout.Bank
	s := fisherplan.FishSnapshot{Tile: &at}
	obs := ObservationFromSnapshot(d, layout, s)
	if obs.DistanceToBank != 0 || obs.NearbyBank != 1 {
		t.Fatalf("expected near the bank, got distance=%d nearby=%d", obs.DistanceToBank, obs.NearbyBank)
	}
}

// TestTraceRecorderEndToEnd is this bridge's own live-proof-in-miniature (per
// docs/ml_lab_real_observation_bridge.md's "ProgressiveFisher first demo" and this
// task's own verification requirement): it drives a TraceRecorder through a realistic
// phase sequence exactly the way internal/bot/fisher.go's Loop() will, closes it, then
// proves the resulting JSONL file is real, complete evidence — internal/ml/trace's own
// Assemble groups it into replay.Episode records, and internal/ml/evaluation's
// EvaluateValidated (the same function a real cmd/mlfishing-style tool would call)
// processes them with zero modification to either package.
func TestTraceRecorderEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fisher.jsonl")

	rec, err := NewTraceRecorder(path, "fisher-live-test")
	if err != nil {
		t.Fatalf("NewTraceRecorder: %v", err)
	}
	d := &Deps{Session: newFakeSession()}
	layout := fisherplan.Catherby()
	episode := 1

	// A small, realistic run: travel, bank, buy the next tool, fish twice, bank the
	// catch, then stop.
	seq := []struct {
		phase    fisherplan.FishPhase
		progress fisherplan.TraceProgress
		result   string
	}{
		{fisherplan.PhaseTravel, fisherplan.TraceProgress{}, "ok"},
		{fisherplan.PhaseBank, fisherplan.TraceProgress{}, "ok"},
		{fisherplan.PhaseBuy, fisherplan.TraceProgress{ToolBought: true}, "ok"},
		{fisherplan.PhaseFish, fisherplan.TraceProgress{FishCaught: 3}, "ok"},
		{fisherplan.PhaseBank, fisherplan.TraceProgress{BankTrip: true}, "ok"},
	}
	at := layout.Bank
	snap := fisherplan.FishSnapshot{Tile: &at}
	for _, step := range seq {
		obs := ObservationFromSnapshot(d, layout, snap)
		reward := fisherplan.PhaseReward(step.phase, step.progress)
		rec.Record(episode, obs, policy.Action(step.phase), step.result, reward, false)
	}
	// The terminal decision, exactly as internal/bot/fisher.go's OnStop will emit it.
	rec.Record(episode, ObservationFromSnapshot(d, layout, snap), policy.Action("STOP"), "operator_stop", 0, true)

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if dropped := rec.Dropped(); dropped != 0 {
		t.Fatalf("expected no dropped records, got %d", dropped)
	}
	if writeErrs := rec.WriteErrors(); writeErrs != 0 {
		t.Fatalf("expected no write errors, got %d", writeErrs)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace file: %v", err)
	}
	defer f.Close()

	episodes, err := trace.Assemble(trace.NewReader(f), trace.AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	if got := len(episodes[0].Steps); got != len(seq)+1 {
		t.Fatalf("expected %d steps (including the terminal one), got %d", len(seq)+1, got)
	}
	last := episodes[0].Steps[len(episodes[0].Steps)-1]
	if last.Reward != 0 {
		t.Fatalf("expected the terminal decision's reward to be 0, got %v", last.Reward)
	}

	ptrs := make([]*replay.Episode, len(episodes))
	for i := range episodes {
		ptrs[i] = &episodes[i]
	}
	summary, err := evaluation.EvaluateValidated(ptrs)
	if err != nil {
		t.Fatalf("EvaluateValidated: %v", err)
	}
	if summary.Episodes != 1 {
		t.Fatalf("summary.Episodes = %d, want 1", summary.Episodes)
	}
	wantReward := float64(fisherplan.PhaseReward(fisherplan.PhaseTravel, fisherplan.TraceProgress{}) +
		fisherplan.PhaseReward(fisherplan.PhaseBank, fisherplan.TraceProgress{}) +
		fisherplan.PhaseReward(fisherplan.PhaseBuy, fisherplan.TraceProgress{ToolBought: true}) +
		fisherplan.PhaseReward(fisherplan.PhaseFish, fisherplan.TraceProgress{FishCaught: 3}) +
		fisherplan.PhaseReward(fisherplan.PhaseBank, fisherplan.TraceProgress{BankTrip: true}))
	if summary.AverageReward < wantReward-1e-6 || summary.AverageReward > wantReward+1e-6 {
		t.Fatalf("summary.AverageReward = %v, want %v", summary.AverageReward, wantReward)
	}
}

func TestTraceRecorderNilIsANoOp(t *testing.T) {
	var rec *TraceRecorder
	rec.Record(1, ObservationFromSnapshot(&Deps{Session: newFakeSession()}, fisherplan.Catherby(), fisherplan.FishSnapshot{}), policy.Wait, "ok", 0, false)
	if rec.Dropped() != 0 || rec.WriteErrors() != 0 {
		t.Fatalf("expected a nil recorder to report zero counters")
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close on nil recorder: %v", err)
	}
}
