package bot

import (
	"strings"
	"testing"
)

func TestBowStringerRegistered(t *testing.T) {
	b, ok := New("BowStringer")
	if !ok {
		t.Fatal("BowStringer is not registered")
	}
	if _, ok := b.(*bowStringerBot); !ok {
		t.Fatalf("BowStringer builds %T", b)
	}
}

// OnStart refuses bad settings before it looks at anything else.
func TestBowStringerRefusesUnknownSetting(t *testing.T) {
	ctx := &Context{Settings: map[string]interface{}{"muleAt": float64(5000)}}
	err := (&bowStringerBot{}).OnStart(ctx)
	if err == nil || !strings.Contains(err.Error(), "muleAt") {
		t.Fatalf("OnStart = %v, want an unknown-key refusal naming muleAt", err)
	}
}
