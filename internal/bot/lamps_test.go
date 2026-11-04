package bot

import (
	"testing"
	"time"
)

func TestLampRubWanted(t *testing.T) {
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	base := lampRubInput{Now: now, Next: now, Held: true}
	cases := []struct {
		name string
		edit func(*lampRubInput)
		want bool
	}{
		{"a held Lamp, due, idle", func(*lampRubInput) {}, true},
		{"no Lamp held", func(in *lampRubInput) { in.Held = false }, false},
		{"not due yet", func(in *lampRubInput) { in.Next = now.Add(time.Second) }, false},
		{"in combat", func(in *lampRubInput) { in.InCombat = true }, false},
		{"dead", func(in *lampRubInput) { in.Dead = true }, false},
		{"a bank or dialogue open", func(in *lampRubInput) { in.ModalOpen = true }, false},
	}
	for _, c := range cases {
		in := base
		c.edit(&in)
		if got := lampRubWanted(in); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
