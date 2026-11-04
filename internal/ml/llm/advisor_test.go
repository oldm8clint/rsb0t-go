package llm

import (
	"rsb0t-go/internal/ml/evidence"
	"rsb0t-go/internal/ml/observation"
	"testing"
)

func TestMockAdvisorUsesObservation(t *testing.T) {
	r, err := (MockAdvisor{}).Recommend(evidence.Record{}, observation.State{SchemaVersion: 1, Health: 100, InventoryUsed: 2, InventorySize: 28, NearbyFish: 1})
	if err != nil || r.Action != "FISH" {
		t.Fatalf("unexpected recommendation: %+v err=%v", r, err)
	}
}

func TestDecodeRecommendationRejectsUnknownAction(t *testing.T) {
	if _, err := DecodeRecommendation([]byte(`{"Action":"EXECUTE","Confidence":1}`)); err == nil {
		t.Fatal("expected unsupported action error")
	}
}
