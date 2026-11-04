package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
)

// lagescape-1018: the engine's eat loop asks Deps.Escape before it eats. An account that the hook says has left (a teleport out of what
// its bites cannot outrun on a lagging server) is not fed in the same breath, and one the hook leaves is eaten as ever.
func TestEatLoopAsksTheEscapeBeforeItEats(t *testing.T) {
	cfg := stallConfig(t)
	bread := stallObj(t, cfg, "Bread")
	heal := map[int32]int{bread: 4}
	for _, left := range []bool{true, false} {
		r := newFloorRig(t, 9, 34, []actionsim.Stack{{ID: bread, Count: 15}}, heal)
		asked := make(chan struct{}, 64)
		r.d.Escape = func() bool {
			asked <- struct{}{}
			return left
		}
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() { eatLoop(r.d, questplan.Sustain{}, stop); close(done) }()
		end := r.d.beginWalk() // a leg is under way, so the floor bite is due at 9 of 34
		time.Sleep(2500 * time.Millisecond)
		end()
		close(stop)
		<-done
		if len(asked) == 0 {
			t.Fatalf("left=%v: the eat loop never asked", left)
		}
		if left && r.packCount(bread) != 15 {
			t.Errorf("the hook said the account left, and the loop still ate (%d bread left)", r.packCount(bread))
		}
		if !left && r.packCount(bread) == 15 {
			t.Error("the hook said the account stayed, and the loop did not eat")
		}
	}
}
