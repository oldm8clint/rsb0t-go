package controlpanel

import (
	"encoding/json"
	"runtime"
	"testing"
)

func TestRuntimeMemStatsIncludesHeapObjects(t *testing.T) {
	var runtimeStats runtime.MemStats
	runtime.ReadMemStats(&runtimeStats)

	got := RuntimeMemStats()
	if got.HeapObjects == 0 && runtimeStats.HeapObjects != 0 {
		t.Fatalf("HeapObjects was not propagated: runtime=%d got=%d", runtimeStats.HeapObjects, got.HeapObjects)
	}
}

func TestMemStatsJSONIncludesHeapObjects(t *testing.T) {
	payload, err := json.Marshal(MemStats{HeapObjects: 123})
	if err != nil {
		t.Fatalf("marshal MemStats: %v", err)
	}

	const want = `"heapObjects":123`
	if !containsJSONField(string(payload), want) {
		t.Fatalf("JSON %s does not contain %s", payload, want)
	}
}

func containsJSONField(payload, field string) bool {
	for i := 0; i+len(field) <= len(payload); i++ {
		if payload[i:i+len(field)] == field {
			return true
		}
	}
	return false
}
