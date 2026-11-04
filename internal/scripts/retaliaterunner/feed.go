package retaliaterunner

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/retaliateplan"
)

// feedSource is the part of *gamestate.State the tick feed reads.
type feedSource interface {
	Subscribe(gamestate.Subscriber) (cancel func())
	Tick() int
	SelfAnim() (seq, tick int)
	LastSelfHit() (tick int, ok bool)
}

// noAmmoText is the line the server prints when a ranged attack finds an empty quiver (content player_ranged.rs2
// ~player_ranged_check_ammo). One of these in the log means a retaliation reached the attack line: a clear or a click was late.
const noAmmoText = "no ammo left in your quiver"

// feed queues one observation per PLAYER_INFO: the tick, whether a hitmark landed on the local player in it, and whether the
// axe's chop animation started in it. It also counts the game lines the loop reports: logs gained and refused attacks.
type feed struct {
	mu     sync.Mutex // the queue is filled on the read loop and drained on the script's loop
	q      []retaliateplan.Obs
	wake   chan struct{}
	cancel func()

	logs    atomic.Int64
	refused atomic.Int64
}

func newFeed(src feedSource) *feed {
	f := &feed{wake: make(chan struct{}, 1)}
	f.cancel = src.Subscribe(func(e gamestate.Event) {
		switch e.Kind {
		case gamestate.EventChatMessage:
			if e.ChatType != gamestate.ChatGame {
				return
			}
			t := strings.ToLower(e.Text)
			switch {
			case strings.Contains(t, "you get some"):
				f.logs.Add(1)
			case strings.Contains(t, noAmmoText):
				f.refused.Add(1)
			}
		case gamestate.EventTick:
			roll := false
			if seq, at := src.SelfAnim(); at == e.Tick && chopmanipplan.IsChopAnim(seq) {
				roll = true
			}
			hit := false
			if ht, ok := src.LastSelfHit(); ok && ht == e.Tick {
				hit = true
			}
			f.mu.Lock()
			f.q = append(f.q, retaliateplan.Obs{N: e.Tick, Hit: hit, Roll: roll})
			if len(f.q) > 64 {
				f.q = f.q[len(f.q)-64:]
			}
			f.mu.Unlock()
			select {
			case f.wake <- struct{}{}:
			default:
			}
		}
	})
	return f
}

func (f *feed) stop() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
}

// pop takes the oldest unread observation.
func (f *feed) pop() (retaliateplan.Obs, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.q) == 0 {
		return retaliateplan.Obs{}, false
	}
	o := f.q[0]
	f.q = f.q[1:]
	return o, true
}

// next waits up to timeout for an observation. stopped is true when delay says the caller should stop.
func (f *feed) next(delay func(time.Duration) bool, timeout time.Duration) (obs retaliateplan.Obs, ok, stopped bool) {
	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		if o, have := f.pop(); have {
			return o, true, false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return retaliateplan.Obs{}, false, false
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(remaining)
		select {
		case <-f.wake:
		case <-timer.C:
		}
		if !delay(0) {
			return retaliateplan.Obs{}, false, true
		}
	}
}

var _ feedSource = (*gamestate.State)(nil)
