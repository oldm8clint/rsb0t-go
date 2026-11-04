package trace

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

func writeDecisions(t *testing.T, ds ...Decision) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf)
	for _, d := range ds {
		if err := w.Decision(d); err != nil {
			t.Fatalf("Decision: %v", err)
		}
	}
	return &buf
}

func dec(episode int, tick uint64, action policy.Action, reward float32, terminal bool) Decision {
	return Decision{
		SchemaVersion: 1,
		ExperimentID:  "test",
		Episode:       episode,
		Tick:          tick,
		Observation:   observation.State{SchemaVersion: 1, Tick: tick},
		Action:        action,
		Result:        "ok",
		Reward:        reward,
		Terminal:      terminal,
	}
}

func TestAssembleHappyPathSingleEpisode(t *testing.T) {
	buf := writeDecisions(t,
		dec(1, 0, policy.WalkToFish, -0.01, false),
		dec(1, 1, policy.Fish, 1, false),
		dec(1, 2, policy.Bank, 0.5, true),
	)
	episodes, err := Assemble(NewReader(buf), AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	e := episodes[0]
	if e.EpisodeID != 1 || len(e.Steps) != 3 {
		t.Fatalf("unexpected episode: %+v", e)
	}
	want := float32(-0.01 + 1 + 0.5)
	if e.TotalReward < want-1e-6 || e.TotalReward > want+1e-6 {
		t.Fatalf("total reward = %v, want %v", e.TotalReward, want)
	}
}

func TestAssembleMultipleEpisodesInOrder(t *testing.T) {
	buf := writeDecisions(t,
		dec(5, 0, policy.Fish, 1, true),
		dec(2, 0, policy.Wait, -0.05, true),
	)
	episodes, err := Assemble(NewReader(buf), AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(episodes) != 2 || episodes[0].EpisodeID != 5 || episodes[1].EpisodeID != 2 {
		t.Fatalf("unexpected episode order: %+v", episodes)
	}
}

func TestAssembleAllowsTickGaps(t *testing.T) {
	// A live source may legitimately skip a tick; that alone is not an integrity
	// failure per docs/ml_lab_real_observation_bridge.md's "Ordering and integrity".
	buf := writeDecisions(t,
		dec(1, 0, policy.Wait, 0, false),
		dec(1, 5, policy.Wait, 0, false),
		dec(1, 6, policy.Wait, 0, true),
	)
	episodes, err := Assemble(NewReader(buf), AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(episodes) != 1 || len(episodes[0].Steps) != 3 {
		t.Fatalf("unexpected result: %+v", episodes)
	}
}

func TestAssembleRejectsDuplicateTick(t *testing.T) {
	buf := writeDecisions(t,
		dec(1, 0, policy.Wait, 0, false),
		dec(1, 0, policy.Wait, 0, false),
	)
	if _, err := Assemble(NewReader(buf), AssembleOptions{}); err == nil {
		t.Fatal("expected an error for a duplicate tick")
	} else if !strings.Contains(err.Error(), "duplicate tick") {
		t.Fatalf("expected a duplicate-tick error, got: %v", err)
	}
}

func TestAssembleRejectsOutOfOrderTick(t *testing.T) {
	buf := writeDecisions(t,
		dec(1, 5, policy.Wait, 0, false),
		dec(1, 3, policy.Wait, 0, false),
	)
	if _, err := Assemble(NewReader(buf), AssembleOptions{}); err == nil {
		t.Fatal("expected an error for an out-of-order tick")
	} else if !strings.Contains(err.Error(), "went backward") {
		t.Fatalf("expected an out-of-order error, got: %v", err)
	}
}

func TestAssembleRejectsDecisionAfterTerminal(t *testing.T) {
	buf := writeDecisions(t,
		dec(1, 0, policy.Wait, 0, true),
		dec(1, 1, policy.Wait, 0, false),
	)
	if _, err := Assemble(NewReader(buf), AssembleOptions{}); err == nil {
		t.Fatal("expected an error for a decision after terminal")
	} else if !strings.Contains(err.Error(), "already reached a terminal decision") {
		t.Fatalf("expected a terminal-consistency error, got: %v", err)
	}
}

func TestAssembleRejectsMixedSchemaVersion(t *testing.T) {
	d1 := dec(1, 0, policy.Wait, 0, false)
	d2 := dec(1, 1, policy.Wait, 0, false)
	d2.SchemaVersion = 2
	buf := writeDecisions(t, d1, d2)
	if _, err := Assemble(NewReader(buf), AssembleOptions{}); err == nil {
		t.Fatal("expected an error for a mixed schema version")
	} else if !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("expected a schema-version error, got: %v", err)
	}
}

func TestAssembleRejectsInvalidAction(t *testing.T) {
	d := dec(1, 0, policy.Action(""), 0, false)
	buf := writeDecisions(t, d)
	if _, err := Assemble(NewReader(buf), AssembleOptions{}); err == nil {
		t.Fatal("expected an error for an empty action")
	} else if !strings.Contains(err.Error(), "empty action") {
		t.Fatalf("expected an empty-action error, got: %v", err)
	}
}

func TestValidateDecisionRejectsNonFiniteReward(t *testing.T) {
	// Not reachable through Writer/Reader's own JSON round-trip (encoding/json refuses
	// to encode a NaN/Inf float in the first place, so a malformed on-disk trace file
	// can never actually carry one) — validateDecision is exercised directly here so
	// the guard still has real coverage as documented defense in depth.
	d := dec(1, 0, policy.Wait, float32(math.Inf(1)), false)
	if err := validateDecision(d, AssembleOptions{}.withDefaults()); err == nil {
		t.Fatal("expected an error for a non-finite reward")
	} else if !strings.Contains(err.Error(), "non-finite reward") {
		t.Fatalf("expected a non-finite-reward error, got: %v", err)
	}
}

func TestAssembleRejectsOversizedRecord(t *testing.T) {
	buf := writeDecisions(t, dec(1, 0, policy.Wait, 0, false))
	if _, err := Assemble(NewReader(buf), AssembleOptions{MaxRecordBytes: 8}); err == nil {
		t.Fatal("expected an error for an oversized record")
	} else if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected a size error, got: %v", err)
	}
}

func TestAssembleRejectsMalformedJSON(t *testing.T) {
	buf := bytes.NewBufferString("{not json\n")
	if _, err := Assemble(NewReader(buf), AssembleOptions{}); err == nil {
		t.Fatal("expected a decode error for malformed JSON")
	}
}

func TestAssembleNilReader(t *testing.T) {
	if _, err := Assemble(nil, AssembleOptions{}); err == nil {
		t.Fatal("expected an error for a nil reader")
	}
}
