package bot

import "testing"

func TestMeleeFlincherTargetLabel(t *testing.T) {
	if got := targetLabel(""); got != "anything attackable" {
		t.Errorf("targetLabel(\"\") = %q, want %q", got, "anything attackable")
	}
	if got := targetLabel("Black demon"); got != "Black demon" {
		t.Errorf("targetLabel(named) = %q, want the name unchanged", got)
	}
}

func TestMeleeFlincherOnStartReadsSettings(t *testing.T) {
	b := &meleeFlincherBot{}
	ctx := NewContext(newAPISession(), "test", nil, nil, nil, nil, nil, nil, nil, map[string]interface{}{
		"target":   "Man",
		"safeTile": "3200,3200,0",
	}, nil)
	if err := b.OnStart(ctx); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if b.target != "Man" {
		t.Errorf("target = %q, want Man", b.target)
	}
	if b.safeTile.X != 3200 || b.safeTile.Z != 3200 || b.safeTile.Level != 0 {
		t.Errorf("safeTile = %+v, want 3200,3200,0", b.safeTile)
	}
	if b.watch == nil {
		t.Error("expected a fresh SwingWatch")
	}
	if b.settle.Ticks == 0 {
		t.Error("expected a non-zero fresh settle")
	}
}
