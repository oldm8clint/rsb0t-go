package fisherplan

import "testing"

// TestPinnedTargetStaysBelowSharks: a pinned target is never raised by the shark climb, and
// otherwise behaves exactly like the plain one (2026-10-04, big-net gem fishing at 90+).
func TestPinnedTargetStaysBelowSharks(t *testing.T) {
	pinned := PinTarget("Big net")
	if PinTarget(pinned) != pinned {
		t.Fatal("pinning twice changed the target")
	}
	if base, ok := PinnedTarget(pinned); !ok || base != "Big net" {
		t.Fatalf("PinnedTarget = %q, %v", base, ok)
	}
	if _, ok := PinnedTarget("Big net"); ok {
		t.Fatal("a plain target reads as pinned")
	}
	if TargetIndex(pinned) != TargetIndex("Big net") {
		t.Fatal("a pinned target indexes differently from its plain one")
	}
	for _, level := range []int{1, 16, 68, SharkAutoLevel, 99} {
		if got, want := CurrentRungIndex(level, pinned), CurrentRungIndex(min(level, SharkAutoLevel-1), "Big net"); got != want {
			t.Errorf("level %d: pinned rung %d, want %d", level, got, want)
		}
	}
	if !AtTarget(99, pinned) {
		t.Fatal("a pinned Big net account at 99 is not at target")
	}
	if _, ok := NextRung(99, pinned); ok {
		t.Fatal("a pinned target has a next rung at 99")
	}
	if len(LadderView(99, pinned)) != TargetIndex("Big net")+1 {
		t.Fatal("the pinned ladder view is not the plain one")
	}
}
