package randomevents

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// The plant's clock against the content that defines it: the seed's ai_timer is 45, run twice (the first fire ends the
// growing phase, the second turns it), and its leash is 3. Skipped without RSB0T_CONTENT_DIR.
func TestPlantClockMatchesContent(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		t.Skip("RSB0T_CONTENT_DIR not set")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "scripts", "macro events", "configs", "antimacro.npc"))
	if err != nil {
		t.Skipf("antimacro.npc not readable: %v", err)
	}
	cur, fields := "", map[string]map[string]int{}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = line[1 : len(line)-1]
			fields[cur] = map[string]int{}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && cur != "" {
			if n, err := strconv.Atoi(v); err == nil {
				fields[cur][k] = n
			}
		}
	}
	seed := fields["macro_triffidseed"]
	if seed["timer"]*2 != plantTurnTicks || seed["timer"] != plantGrowTicks {
		t.Errorf("content: timer %d (grows at %d, turns at %d); the clock says %d and %d", seed["timer"], seed["timer"], seed["timer"]*2, plantGrowTicks, plantTurnTicks)
	}
	if got, ok := eventLeashOf(angryPlantType); !ok || got != seed["maxrange"] {
		t.Errorf("content: maxrange %d; the leash table says %d", seed["maxrange"], got)
	}
	if fields["macro_triffidseed_angry"]["maxrange"] != seed["maxrange"] {
		t.Errorf("the angry plant's maxrange %d differs from the seed's %d", fields["macro_triffidseed_angry"]["maxrange"], seed["maxrange"])
	}
}

func TestPlantMustLeaveBeforeItWouldTurn(t *testing.T) {
	if plantTurnTicks-plantLeaveAtTicks < 20 {
		t.Fatalf("the deadline leaves only %d ticks before the plant turns; a six-step walk and a late click need twenty", plantTurnTicks-plantLeaveAtTicks)
	}
	for _, c := range []struct {
		age, lag int
		want     bool
	}{
		{0, 0, false}, {45, 0, false}, {plantLeaveAtTicks - 1, 0, false}, {plantLeaveAtTicks, 0, true}, {89, 0, true},
		// A recent lag episode moves the deadline forward by what it hid, never by more than the cap.
		{plantLeaveAtTicks - 10, 10, true}, {plantLeaveAtTicks - 11, 10, false},
		{plantLeaveAtTicks - plantLagCap, 500, true}, {plantLeaveAtTicks - plantLagCap - 1, 500, false},
		{-1, 0, false}, // not in view
	} {
		if got := plantMustLeave(c.age, c.lag); got != c.want {
			t.Errorf("plantMustLeave(age %d, lag %d) = %v, want %v", c.age, c.lag, got, c.want)
		}
	}
}

func TestPlantBusyWindowStartsBeforeItCanBePickedAndEndsAfterItHasTurned(t *testing.T) {
	if plantBusyWindow(plantBusyFromTicks-1) || !plantBusyWindow(plantBusyFromTicks) {
		t.Error("the window must open at plantBusyFromTicks")
	}
	if plantBusyFromTicks >= plantGrowTicks {
		t.Error("the window must open before the seed can first be picked")
	}
	if !plantBusyWindow(plantTurnTicks) || !plantBusyWindow(plantTurnTicks+10) {
		t.Error("the window must cover the tick the plant turns and the ticks after it, where the angry one is leaving")
	}
	if plantBusyWindow(plantClearTicks) {
		t.Error("the window must close at plantClearTicks")
	}
}

func TestPlantReplyWaitGrowsWithTheLagTheRouteHasShown(t *testing.T) {
	base, max := 2*time.Second, 8*time.Second
	if got := plantReplyWaitFor(base, 0, 0, max); got != base {
		t.Errorf("no lag: %s, want %s", got, base)
	}
	if got := plantReplyWaitFor(base, 5, 0, max); got != base+5*gamestate.ViewTick {
		t.Errorf("5 ticks of lag: %s", got)
	}
	if got := plantReplyWaitFor(base, 5, 7*time.Second, max); got != 7*time.Second {
		t.Errorf("a bite that took 7 s: %s, want 7s", got)
	}
	if got := plantReplyWaitFor(base, 500, 14*time.Second, max); got != max {
		t.Errorf("past the cap: %s, want %s", got, max)
	}
}

// A plant's age is the ticks it has been in view plus what a silent view is hiding: live 18 Oct the log read "seen 97t" for an angry
// plant 76 seconds after its first sighting, a server at 0.78 s a tick; a view that goes silent must not let the clock stand still.
func TestPlantAgeCountsTheTicksASilentViewHides(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{407: "Strange plant"}})
	s := overlapState()
	addNPC(s, 1, 407, 1, 0)
	advanceTicks(s, 50)
	s.OverrideViewLag(&gamestate.ViewLag{}) // ticks fed this fast read as a backlog; the real clock is not under test here
	if age, ok := plantAgeTicks(s, 1); !ok || age != 50 {
		t.Fatalf("age = %d, %v; want 50", age, ok)
	}
	s.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 12 * time.Second})
	if age, _ := plantAgeTicks(s, 1); age != 50+20 {
		t.Fatalf("a view silent for 12 s hides 20 ticks: age = %d, want 70", age)
	}
	s.OverrideViewLag(&gamestate.ViewLag{Lagging: true, CatchingUp: true, Recent: gamestate.RecentLag{Ticks: 14}})
	if age, _ := plantAgeTicks(s, 1); age != 50+14 {
		t.Fatalf("a view catching up on a 14-tick backlog: age = %d, want 64", age)
	}
	s.OverrideViewLag(nil)
	if _, ok := plantAgeTicks(s, 99); ok {
		t.Fatal("an NPC that is not in view has no age")
	}
}
