package evaluation

import (
	"testing"

	"rsb0t-go/internal/ml/replay"
)

func TestValidateEpisodesRequiresEvidence(t *testing.T) {
	if err := ValidateEpisodes(nil); err == nil {
		t.Fatal("expected empty evaluation to be rejected")
	}
	if err := ValidateEpisodes([]*replay.Episode{nil}); err == nil {
		t.Fatal("expected nil episode to be rejected")
	}
}

func TestValidateEpisodesAcceptsEpisode(t *testing.T) {
	episode := &replay.Episode{}
	if err := ValidateEpisodes([]*replay.Episode{episode}); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
