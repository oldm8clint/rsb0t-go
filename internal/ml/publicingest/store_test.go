package publicingest

import "testing"

func testLesson(seed int64) Lesson {
	return Lesson{SchemaVersion: "v1", Source: "public-ts", ExperimentID: "e", StrategyID: "s", Seed: seed, Goal: "fish", Episodes: 10, Evidence: []Evidence{{Claim: "x", Confidence: .8, SampleSize: 10}}}
}

func TestStoreDeduplicatesAndBounds(t *testing.T) {
	s := NewStore(2)
	if !s.Add(testLesson(1)) {
		t.Fatal("first add rejected")
	}
	if s.Add(testLesson(1)) {
		t.Fatal("duplicate accepted")
	}
	if !s.Add(testLesson(2)) {
		t.Fatal("second add rejected")
	}
	if !s.Add(testLesson(3)) {
		t.Fatal("third add rejected")
	}
	got := s.List()
	if len(got) != 2 || got[0].Seed != 2 || got[1].Seed != 3 {
		t.Fatalf("unexpected store: %+v", got)
	}
}
