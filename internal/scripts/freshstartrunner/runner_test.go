package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// TestThiefNoteChatDetectsBothStunPhrases guards against the real bug found live tonight
// (see PickpocketStep's own doc comment on t.NoteChat): without NoteChat actually being fed
// the server's reply after a click, a real "You've been stunned!"/"you fail to pick" line was
// never recorded, so a stunned Thief looked identical to one just failing every roll forever —
// PickpocketStep kept re-clicking straight into the same stun window, and the server silently
// drops a pickpocket attempt made while already stunned (no chat, no xp), making the two
// situations indistinguishable from the caller's side. NoteChat/stunned are both unexported, so
// this lives in-package rather than as an external _test package.
func TestThiefNoteChatDetectsBothStunPhrases(t *testing.T) {
	cases := []string{
		"You attempt to pick the man's pocket. You fail to pick the man's pocket. You've been stunned!",
		"You have been stunned.", // NoteChat matches "been stunned" case-sensitively lower-cased
		"You fail to pick the pocket.",
	}
	for _, chat := range cases {
		th := NewThief()
		if th.stunned() {
			t.Fatalf("a fresh Thief should not start stunned")
		}
		th.NoteChat(chat)
		if !th.stunned() {
			t.Errorf("NoteChat(%q) should set the stun window", chat)
		}
	}
}

func TestThiefNoteChatIgnoresUnrelatedChat(t *testing.T) {
	th := NewThief()
	th.NoteChat("You steal some coins from the man's pocket.")
	if th.stunned() {
		t.Fatal("an unrelated chat line should not set the stun window")
	}
}

// TestSailProgressEscalatesOnlyOnRealStagnation guards the STRANDED escalation added after a
// real live incident (casket sealed in a building northeast of Yanille, SailLeg logging the
// identical "boat leg: ... -> destination" / "no path ... unreachable" pair forever with no
// operator-visible signal that anything was actually stuck versus merely retrying normally).
// note is unexported, so this lives in-package like the Thief tests above.
func TestSailProgressEscalatesOnlyOnRealStagnation(t *testing.T) {
	p := NewSailProgress()
	dest := tile.Tile{X: 2809, Z: 3441}
	here := tile.Tile{X: 2651, Z: 3165}

	// The very first call always establishes the baseline (nothing to compare against yet)
	// rather than counting as one failure, matching a fresh account's very first SailLeg
	// attempt never being reported STRANDED on its own. From the second call on, the same
	// failure from the same tile repeated escalates every sailStuckEvery-th call and never
	// before.
	if stuck := p.note(false, freshstart.LegDestination, dest, &here); stuck {
		t.Fatal("the very first call should never escalate")
	}
	for fails := 1; fails <= sailStuckEvery*2; fails++ {
		stuck := p.note(false, freshstart.LegDestination, dest, &here)
		want := fails%sailStuckEvery == 0
		if stuck != want {
			t.Errorf("fails=%d: note() = %v, want %v", fails, stuck, want)
		}
	}
}

func TestSailProgressResetsOnRealProgress(t *testing.T) {
	dest := tile.Tile{X: 2809, Z: 3441}
	here := tile.Tile{X: 2651, Z: 3165}
	moved := tile.Tile{X: 2652, Z: 3165}

	cases := []struct {
		name string
		note func(p *SailProgress) bool
	}{
		{"a success resets", func(p *SailProgress) bool {
			for i := 0; i < sailStuckEvery-1; i++ {
				p.note(false, freshstart.LegDestination, dest, &here)
			}
			p.note(true, freshstart.LegDestination, dest, &here)
			return p.note(false, freshstart.LegDestination, dest, &here)
		}},
		{"the account's tile actually moving resets", func(p *SailProgress) bool {
			for i := 0; i < sailStuckEvery-1; i++ {
				p.note(false, freshstart.LegDestination, dest, &here)
			}
			return p.note(false, freshstart.LegDestination, dest, &moved)
		}},
		{"a different leg resets", func(p *SailProgress) bool {
			for i := 0; i < sailStuckEvery-1; i++ {
				p.note(false, freshstart.LegDestination, dest, &here)
			}
			return p.note(false, freshstart.LegPier, dest, &here)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewSailProgress()
			if stuck := c.note(p); stuck {
				t.Error("should not have escalated — real progress happened along the way")
			}
		})
	}
}

// The run rule, pinned. It only ever turns running ON, and only with energy to spare -- the
// same shape fisherrunner has always had, now on the path 21 of 22 scripts actually walk.
func TestRunEnableRule(t *testing.T) {
	for _, c := range []struct {
		name   string
		on     bool
		energy int
		known  bool
		want   bool
	}{
		{"already running, nothing to do", true, 100, true, false},
		{"plenty of energy, not running", false, 100, true, true},
		{"exactly at the threshold", false, RunEnergyThreshold, true, true},
		{"one below the threshold", false, RunEnergyThreshold - 1, true, false},
		{"drained", false, 0, true, false},
		{"energy not observed yet: assume nothing", false, 100, false, false},
	} {
		got := wantRun(c.on, c.energy, c.known)
		if got != c.want {
			t.Errorf("%s: wantRun(%v,%d,%v) = %v, want %v", c.name, c.on, c.energy, c.known, got, c.want)
		}
	}
}
