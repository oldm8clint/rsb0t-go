package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// teleportRig is a Guardian over a bare session whose log is captured; tile moves are applied with setTile.
type teleportRig struct {
	s    *safeRefSession
	g    *Guardian
	logs []string
}

func newTeleportRig(t *testing.T, x, z int) *teleportRig {
	t.Helper()
	r := &teleportRig{s: &safeRefSession{state: gamestate.New()}}
	r.g = NewGuardian(r.s, nil)
	r.g.Log = func(s string) { r.logs = append(r.logs, s) }
	t.Cleanup(func() { r.g.Stop(); ForgetTeleports(r.s) })
	setTileExact(r.s.state, x, z, 0)
	r.g.watchDisplacement() // the first reading only records
	return r
}

func (r *teleportRig) jump(x, z int) string {
	before := len(r.logs)
	setTileExact(r.s.state, x, z, 0)
	r.g.watchDisplacement()
	if len(r.logs) == before {
		return ""
	}
	return r.logs[len(r.logs)-1]
}

// THE LINE A TRAVEL CAST PRINTED, 13 Oct 2026: Camelot's landing (mapsquare 43,54) is in the fail-teleport enum, so a cast
// from the ogre pens logged the loud "an ignored event's own timer is the known mechanism" line. Announced, it is an expected
// fixture; unannounced, or after its time is up, it reads as it always did.
func TestExpectedTeleportReadsAsFixtureNotFailTeleport(t *testing.T) {
	// Not announced: the old reading.
	r := newTeleportRig(t, 2504, 3104)
	if line := r.jump(2760, 3480); !strings.Contains(line, "fail-teleport") {
		t.Fatalf("an unannounced Camelot landing reads %q, want the fail-teleport reading (the fixture of this test)", line)
	}
	// Announced.
	r = newTeleportRig(t, 2504, 3104)
	ExpectTeleport(r.s, 2757, 3478, 15*time.Second)
	line := r.jump(2760, 3480)
	if !strings.Contains(line, "expected: "+ExpectedTeleportName) || strings.Contains(line, "fail-teleport") {
		t.Errorf("an announced Camelot landing reads %q", line)
	}
	// One cast is one jump: the expectation is used up.
	setTileExact(r.s.state, 2504, 3104, 0)
	r.g.watchDisplacement()
	if line := r.jump(2760, 3480); !strings.Contains(line, "fail-teleport") {
		t.Errorf("the second landing on the same tile reads %q, want the fail-teleport reading again", line)
	}
	// After its time.
	r = newTeleportRig(t, 2504, 3104)
	ExpectTeleport(r.s, 2757, 3478, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if line := r.jump(2760, 3480); !strings.Contains(line, "fail-teleport") {
		t.Errorf("an expired expectation still explained the landing: %q", line)
	}
	// A jump that lands somewhere other than announced is not excused.
	r = newTeleportRig(t, 2504, 3104)
	ExpectTeleport(r.s, 2757, 3478, 15*time.Second)
	if line := r.jump(3216, 3424); strings.Contains(line, "expected: "+ExpectedTeleportName) {
		t.Errorf("a landing 450 tiles from the announced one was excused: %q", line)
	}
	// Another account's expectation is not this account's.
	r = newTeleportRig(t, 2504, 3104)
	other := &safeRefSession{state: gamestate.New()}
	ExpectTeleport(other, 2757, 3478, 15*time.Second)
	defer ForgetTeleports(other)
	if line := r.jump(2760, 3480); strings.Contains(line, "expected: "+ExpectedTeleportName) {
		t.Errorf("another session's expectation excused this one: %q", line)
	}
}

// Ardougne's landing (mapsquare 41,51) is in no table: unannounced it reads UNEXPLAINED, and twelve of them inside 90 seconds
// teach the mass learner a twelve-hour strand site at the East Ardougne market. Announced, they never count.
func TestExpectedTeleportNeverReachesTheMassLearner(t *testing.T) {
	t.Cleanup(func() { mass = newMassTracker() })
	mass = newMassTracker()
	run := func(announce bool) (learned bool, unexplained int) {
		for i := 0; i < massThreshold+1; i++ {
			r := newTeleportRig(t, 2504, 3104)
			if announce {
				ExpectTeleport(r.s, 2661, 3301, 15*time.Second)
			}
			line := r.jump(2664, 3304)
			if strings.Contains(line, "UNEXPLAINED") || strings.Contains(line, "STRAND SITE") {
				unexplained++
			}
			for _, l := range r.logs {
				if strings.Contains(l, "MASS DISPLACEMENT") {
					learned = true
				}
			}
		}
		return learned, unexplained
	}
	if learned, n := run(true); learned || n != 0 {
		t.Fatalf("13 announced Ardougne casts: learned a site %v, %d unexplained", learned, n)
	}
	if _, ok := learnedSiteAt(2664, 3304, 0, time.Now()); ok {
		t.Fatal("a strand site was learned from announced casts")
	}
	// The contrast: the same burst unannounced does teach it, so the test above is testing something.
	if learned, _ := run(false); !learned {
		t.Error("13 unannounced Ardougne landings did not reach the mass learner: the test no longer tests the bypass")
	}
}
