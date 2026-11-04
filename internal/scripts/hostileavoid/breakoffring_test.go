package hostileavoid

import "testing"

// The notice ring and the break-off ring must never invert: a hostile close enough to break off
// from must never already count as broken off from. This is not hypothetical — it shipped and was
// caught live. A White wolf chases only 5 tiles, so its break-off ring (5+3=8) is naturally
// SMALLER than a notice ring widened for a slow poll (2+10=12), and every wolf 9 to 12 tiles away
// booked a break-off, walked nowhere, reported that it had moved, and made the caller replan on
// every poll until the whole per-session budget was gone in under three seconds.
func TestBreakOffRingIsNeverInsideTheNoticeRing(t *testing.T) {
	// Margins well past anything shipped, so this keeps holding if either is retuned.
	for noticeMargin := 0; noticeMargin <= 20; noticeMargin++ {
		for breakOffMargin := 0; breakOffMargin <= 20; breakOffMargin++ {
			cfg := Config{Enabled: true, NoticeMargin: noticeMargin, BreakOffMargin: breakOffMargin}
			for id, f := range Catalog {
				// One hostile sitting exactly on the edge of the notice ring, the worst case.
				dist := f.NoticeRange(noticeMargin)
				snap := Snapshot{X: 0, Z: 0, CombatLevel: 3, NPCs: []NPC{{Index: 1, Type: id, X: dist, Z: 0}}}
				act := Decide(cfg, snap)
				if !act.Evade {
					t.Fatalf("npc %d at its own notice range %d was not evaded (notice %d, breakoff %d)", id, dist, noticeMargin, breakOffMargin)
				}
				if act.BreakOff <= dist {
					t.Errorf("npc %d: break-off ring %d is not outside the notice ring %d (notice margin %d, breakoff margin %d) — the account would be told to break off and be called clear at the same moment",
						id, act.BreakOff, dist, noticeMargin, breakOffMargin)
				}
				// The decision must not immediately contradict itself.
				if Cleared(snap, act.Threat, act.BreakOff) {
					t.Errorf("npc %d: Decide said evade and Cleared said already clear, at %d tiles (notice margin %d, breakoff margin %d)",
						id, dist, noticeMargin, breakOffMargin)
				}
			}
		}
	}
}

// The clamp must not shrink a break-off ring that was already wide enough. The giant bat is the
// case that matters: it chases 10 tiles, so at the shipped margins its ring is 13 and the notice
// ring is 8, and nothing should touch it.
func TestTheClampLeavesAnAlreadyWideBreakOffRingAlone(t *testing.T) {
	cfg := DefaultConfig()
	bat := Catalog[78]
	snap := Snapshot{X: 0, Z: 0, CombatLevel: 3, NPCs: []NPC{{Index: 1, Type: 78, X: 4, Z: 0}}}
	act := Decide(cfg, snap)
	if want := bat.BreakOffRange(cfg.BreakOffMargin); act.BreakOff != want {
		t.Errorf("giant bat break-off = %d, want its own %d — the clamp should not have applied", act.BreakOff, want)
	}
	if act.BreakOff != 13 {
		t.Errorf("giant bat break-off = %d, want 13 (chase limit 10 + margin 3)", act.BreakOff)
	}
}

// At the margins the webwalk client actually ships, a wolf must get a break-off ring outside its
// notice ring rather than its own short 8-tile chase limit.
func TestWolfBreakOffRingAtTheWidePollMargin(t *testing.T) {
	cfg := Config{Enabled: true, NoticeMargin: 10, BreakOffMargin: 3}
	snap := Snapshot{X: 0, Z: 0, CombatLevel: 3, NPCs: []NPC{{Index: 1, Type: 97, X: 9, Z: 0}}}
	act := Decide(cfg, snap)
	if !act.Evade {
		t.Fatal("a White wolf 9 tiles away is inside a 12-tile notice ring and must be evaded")
	}
	if act.BreakOff != 13 {
		t.Errorf("White wolf break-off at notice margin 10 = %d, want 13 (notice ring 12 + 1)", act.BreakOff)
	}
}
