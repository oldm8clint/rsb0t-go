package fleetplan

import (
	"testing"
	"time"
)

func enchantPoll(mod func(p *WalkEnchantPoll)) WalkEnchantPoll {
	p := WalkEnchantPoll{SinceClick: 2 * time.Second, TicksSinceLand: 99, GapTicks: WalkEnchantGapTicks, Ahead: 20, Running: true, Pieces: 10}
	if mod != nil {
		mod(&p)
	}
	return p
}

// The enchant's gates are the alchemy's, with the enchant's three-tick cast: nothing in the tick of a click, nothing
// inside the last cast's delay, nothing when the walk would click again before the delay ends, nothing in a fight,
// with an interface open or with nothing left to cast on.
func TestShouldWalkEnchantGates(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    WalkEnchantPoll
		cast bool
	}{
		{"all clear", enchantPoll(nil), true},
		{"a click this tick", enchantPoll(func(p *WalkEnchantPoll) { p.SinceClick = 0 }), false},
		{"a click one tick ago", enchantPoll(func(p *WalkEnchantPoll) { p.SinceClick = 600 * time.Millisecond }), false},
		{"the cast still on its way", enchantPoll(func(p *WalkEnchantPoll) { p.Pending = true }), false},
		{"one tick after the last landed", enchantPoll(func(p *WalkEnchantPoll) { p.TicksSinceLand = 1 }), false},
		{"two ticks after the last landed", enchantPoll(func(p *WalkEnchantPoll) { p.TicksSinceLand = 2 }), true},
		{"a widened gap holds the third tick", enchantPoll(func(p *WalkEnchantPoll) { p.TicksSinceLand, p.GapTicks = 2, 3 }), false},
		{"a gap below the spell's own is the spell's", enchantPoll(func(p *WalkEnchantPoll) { p.TicksSinceLand, p.GapTicks = 1, 0 }), false},
		{"nine tiles to a click target that is not the leg's end, running", enchantPoll(func(p *WalkEnchantPoll) { p.Ahead = 9 }), false},
		{"ten tiles, running", enchantPoll(func(p *WalkEnchantPoll) { p.Ahead = 10 }), true},
		{"six tiles, walking", enchantPoll(func(p *WalkEnchantPoll) { p.Ahead, p.Running = 6, false }), false},
		{"seven tiles, walking", enchantPoll(func(p *WalkEnchantPoll) { p.Ahead, p.Running = 7, false }), true},
		{"the leg's end: no click left, three tiles will do", enchantPoll(func(p *WalkEnchantPoll) { p.Ahead, p.LegEnd = 3, true }), true},
		{"the leg's end, two tiles: arrived", enchantPoll(func(p *WalkEnchantPoll) { p.Ahead, p.LegEnd = 2, true }), false},
		{"in combat", enchantPoll(func(p *WalkEnchantPoll) { p.InCombat = true }), false},
		{"an interface open", enchantPoll(func(p *WalkEnchantPoll) { p.Modal = true }), false},
		{"in the Wilderness", enchantPoll(func(p *WalkEnchantPoll) { p.Wilderness = 5 }), false},
		{"hitpoints low", enchantPoll(func(p *WalkEnchantPoll) { p.HP, p.MaxHP = 5, 50 }), false},
		{"nothing left to cast on", enchantPoll(func(p *WalkEnchantPoll) { p.Pieces = 0 }), false},
	} {
		if got := ShouldWalkEnchant(tc.p); got.Cast != tc.cast {
			t.Errorf("%s: cast %v (%s), want %v", tc.name, got.Cast, got.Why, tc.cast)
		}
	}
}

// The alchemy's distance rule is unchanged by the refactor that shares it: 4 plus 2 a tick over five ticks running.
func TestWalkAlchAheadStillFourPlusTheCast(t *testing.T) {
	if WalkAlchAhead(true) != 14 || WalkAlchAhead(false) != 9 {
		t.Fatalf("alch ahead = %d running, %d walking, want 14 and 9", WalkAlchAhead(true), WalkAlchAhead(false))
	}
	if WalkCastAhead(true, WalkEnchantCastTicks) != 10 || WalkCastAhead(false, WalkEnchantCastTicks) != 7 {
		t.Fatalf("enchant ahead = %d running, %d walking, want 10 and 7", WalkCastAhead(true, WalkEnchantCastTicks), WalkCastAhead(false, WalkEnchantCastTicks))
	}
}
