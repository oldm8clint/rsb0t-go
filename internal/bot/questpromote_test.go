package bot

import (
	"regexp"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/waterfallplan"
)

// cmd/questpromote asks git where each quest's module last changed from questplan.ModuleFiles. A quest
// the table does not name could not be judged ("unbounded"), and an entry for a quest nothing builds is a
// rename that left the table behind.
func TestEveryQuestFactoryHasModuleFiles(t *testing.T) {
	for id := range QuestFactories {
		if len(questplan.ModuleFiles[id]) == 0 {
			t.Errorf("quest %q has a factory but no questplan.ModuleFiles entry: cmd/questpromote cannot say where its module last changed", id)
		}
	}
	for id := range questplan.ModuleFiles {
		if QuestFactories[id] == nil {
			t.Errorf("questplan.ModuleFiles names %q, which no QuestFactories entry builds", id)
		}
	}
	if QuestFactories[waterfallplan.QuestID] == nil {
		t.Error("waterfall is not a registered quest")
	}
}

// A quest completion records the build in full (git cannot always resolve a short revision), while the
// lab logs keep the 8-digit form.
func TestBuildIdentityIsTheFullRevisionAndTheLogFormIsItsShortening(t *testing.T) {
	if id := BuildIdentity(); !regexp.MustCompile(`^(unknown|[0-9a-f]{40}(\+dirty)?)$`).MatchString(id) {
		t.Errorf("BuildIdentity %q is neither a full revision nor unknown", id)
	}
	full := "0123456789abcdef0123456789abcdef01234567"
	for in, want := range map[string]string{
		full:            "01234567",
		full + "+dirty": "01234567+dirty",
		"unknown":       "unknown",
		"abc":           "abc",
	} {
		if got := shortRevision(in); got != want {
			t.Errorf("shortRevision(%q) = %q, want %q", in, got, want)
		}
	}
}
