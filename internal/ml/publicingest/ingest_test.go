package publicingest

import (
	"strings"
	"testing"
)

func TestIngestJSONLValidatesEachLesson(t *testing.T) {
	data := `{"schema_version":"v1","source":"public-ts","experiment_id":"exp-1","strategy_id":"s-1","seed":1,"goal":"fish","episodes":10,"metrics":{"success_rate":0.8,"mean_reward":5,"mean_steps":20},"evidence":[{"claim":"better","confidence":0.9,"sample_size":10}]}`
	count := 0
	if err := IngestJSONL(strings.NewReader(data+"\n"), func(Lesson) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d lessons", count)
	}
}

func TestIngestJSONLRejectsMalformedLesson(t *testing.T) {
	data := `{"schema_version":"v1","source":"public-ts"}`
	if err := IngestJSONL(strings.NewReader(data), func(Lesson) error { t.Fatal("should not store"); return nil }); err == nil {
		t.Fatal("expected validation error")
	}
}
