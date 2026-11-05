package loot

import "testing"

func TestIsNotable(t *testing.T) {
	cases := []struct {
		name        string
		id          int
		unit, count int64
		want        bool
	}{
		{"dragon chainbody from an npc", 3140, 50000, 1, true},
		{"rune scimitar", 1333, 20000, 1, true},
		{"a bone", 526, 150, 1, false},
		{"a big stack of cheap things", 1511, 100, 1999, false},
		{"a stack worth 200k", 385, 400, 500, true},
		{"coins never", 995, 1, 5_000_000, false},
		{"unpriced", 1234, 0, 10, false},
		{"nothing", 3140, 50000, 0, false},
	}
	for _, c := range cases {
		if got := IsNotable(c.id, c.unit, c.count); got != c.want {
			t.Errorf("%s: IsNotable = %v", c.name, got)
		}
	}
}
