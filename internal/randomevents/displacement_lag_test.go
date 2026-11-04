package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// 17 Oct 12:23:09-17: a lagging view's backlog moved 24 accounts ten to twenty-eight tiles in one poll each, and a longer silence moves an
// account further than the 42 tiles a poll can legitimately carry it. That is the view being read, not a displacement, and it must
// neither be logged as one nor count toward the mass learner's strand site.
func TestAJumpReadFromALaggingViewIsNotADisplacement(t *testing.T) {
	r := newTeleportRig(t, 2504, 3104)
	r.s.state.OverrideViewLag(&gamestate.ViewLag{Lagging: true, CatchingUp: true, InWindow: 40})
	if line := r.jump(2640, 3104); line != "" {
		t.Fatalf("a 136-tile jump read from a catching-up view logged %q", line)
	}
	if line := r.jump(2700, 3180); line != "" {
		t.Fatalf("a second jump from a silent-then-settling view logged %q", line)
	}
	r.s.state.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 20 * time.Second})
	if line := r.jump(2504, 3104); line != "" {
		t.Fatalf("a jump read from a silent view logged %q", line)
	}
	// The same jump from a current view is read as it always was, and the readings above were recorded: it is measured from 2504,3104.
	r.s.state.OverrideViewLag(nil)
	if line := r.jump(2640, 3104); line == "" {
		t.Error("a 136-tile jump from a current view was not reported")
	}
}
