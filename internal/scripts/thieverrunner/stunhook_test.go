package thieverrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
)

type stunSession struct {
	state  *gamestate.State
	cheats *anticheat.Counters
}

func (s *stunSession) Send(func(w *packet.Packet)) error { return nil }
func (s *stunSession) State() *gamestate.State           { return s.state }
func (s *stunSession) Cheats() *anticheat.Counters       { return s.cheats }

// stunDeps is a Deps whose clicks answer with the stun line, and whose delays are recorded rather than slept.
func stunDeps(delays *[]time.Duration) (*Deps, *chatlog.Log) {
	chat := chatlog.New()
	d := &Deps{
		Session: &stunSession{state: gamestate.New(), cheats: &anticheat.Counters{}},
		Chat:    chat,
		Delay:   func(t time.Duration) bool { *delays = append(*delays, t); return true },
	}
	return d, chat
}

// A failed pickpocket's stun wait goes to the OnStun hook when one is set: the endgame casts High Level
// Alchemy inside it. Without a hook it is the plain Delay it always was.
func TestAttemptHandsTheStunWaitToTheHook(t *testing.T) {
	var delays []time.Duration
	d, chat := stunDeps(&delays)
	click := func() error { chat.Record("You've been stunned!"); return nil }

	if Attempt(d, 10, click) {
		t.Fatal("a stunned attempt is not a success")
	}
	want := 10*tickMs + 1200*time.Millisecond
	if len(delays) != 1 || delays[0] != want {
		t.Fatalf("no hook: delays %v, want one wait of %v", delays, want)
	}

	delays = nil
	var hooked []time.Duration
	d.OnStun = func(wait time.Duration) { hooked = append(hooked, wait) }
	if Attempt(d, 10, click) {
		t.Fatal("a stunned attempt is not a success")
	}
	if len(hooked) != 1 || hooked[0] != want {
		t.Fatalf("hook got %v, want one wait of %v", hooked, want)
	}
	for _, dl := range delays {
		if dl >= want {
			t.Fatalf("the stun was also slept in Attempt (%v): the hook owns it", dl)
		}
	}

	// The default stun is 8 ticks when the rung names none.
	hooked = nil
	Attempt(d, 0, click)
	if len(hooked) != 1 || hooked[0] != 8*tickMs+1200*time.Millisecond {
		t.Fatalf("default stun: %v", hooked)
	}
}
