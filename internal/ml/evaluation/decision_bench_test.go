package evaluation

import (
	"testing"

	"rsb0t-go/internal/ml/replay"
)

func BenchmarkDecideEvidence(b *testing.B) {
	episodes := make([]EpisodeEvidence, 128)
	for i := range episodes {
		episodes[i] = EpisodeEvidence{Episode: &replay.Episode{}, Status: StatusCompleted}
	}
	cfg := EvidenceGateConfig{MaxFailureRate: 0.5}
	report := Report{PassesGate: true}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if DecideEvidence(episodes, cfg, report) != DecisionPass {
			b.Fatal("expected PASS")
		}
	}
}
