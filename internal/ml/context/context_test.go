package context

import (
	"rsb0t-go/internal/ml/observation"
	"strings"
	"testing"
)

func TestRequestIsBoundedAndStructured(t *testing.T) {
	r := New("improve fishing", observation.State{SchemaVersion: 1, Tick: 42, NearbyFish: 1})
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "allowed_actions") || !strings.Contains(s, "observation") {
		t.Fatal("missing structured fields")
	}
	if strings.Contains(s, "password") || strings.Contains(s, "raw_packet") {
		t.Fatal("unsafe context field")
	}
}

func TestRequestRequiresTask(t *testing.T) {
	r := New("", observation.State{})
	if _, err := r.MarshalJSON(); err == nil {
		t.Fatal("expected missing task error")
	}
}
