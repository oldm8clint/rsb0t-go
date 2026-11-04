package freshstartrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/freshstart"
)

// The Stage 1 rest that never ended (live 5 Oct 2026, Howdy Folk, Hitpoints 10): "health at 40% -
// resting until it regenerates" at 14:53 and nothing for 1h14m, the watchdog reading chat modal 359
// ("Your wish has been granted! ... Click here to continue", the Genie's last page). Health
// regenerates on a normal player timer, which runs only while no main or chat modal is open, and
// a rest sends nothing that closes one.

const genieChatModal = 359

func restDeps(t *testing.T, hp int) (*Deps, *castSession, *[]string) {
	t.Helper()
	s := newCastSession()
	s.setStat(gamestate.HitpointsStat, 10) // seen at full health: the maximum
	s.setStat(gamestate.HitpointsStat, hp)
	var lines []string
	return &Deps{Session: s, Delay: func(time.Duration) bool { return true }, Log: func(l string) { lines = append(lines, l) }}, s, &lines
}

func openChat(s *castSession, id int) {
	w := packet.NewWriter(4)
	w.P2(uint16(id))
	s.state.Handle(serverprot.IF_OPENCHAT, w.Data[:w.Pos])
}

func logHas(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func TestARestClosesAModalLeftOpenSoHealthRegenerates(t *testing.T) {
	d, s, lines := restDeps(t, 4) // 40%: the rest starts
	openChat(s, genieChatModal)
	th := NewThief()
	if !th.restStep(d, freshstart.RestBelowHP, time.Now()) {
		t.Fatal("4 of 10 hitpoints did not start a rest")
	}
	if s.sent != 1 {
		t.Fatalf("%d packets sent, want the one CLOSE_MODAL", s.sent)
	}
	if main, _, chat := s.state.Modals(); main != gamestate.NoModal || chat != gamestate.NoModal {
		t.Fatalf("the modal is still open: main %d chat %d", main, chat)
	}
	if !logHas(*lines, "a modal was left open") || !logHas(*lines, "resting until it regenerates") {
		t.Fatalf("the log does not say what happened: %v", *lines)
	}
	// Nothing left to close: the next pass of the same rest sends nothing.
	if !th.restStep(d, freshstart.RestBelowHP, time.Now().Add(3*time.Second)) || s.sent != 1 {
		t.Fatalf("the second pass sent %d packets in all", s.sent)
	}
}

// A rest that has run its bound out is given up, with the hitpoints and the modals in the log, and
// the account works for the cooldown instead of resting again at once.
func TestARestIsGivenUpAfterTheBoundAndNotStartedAgainAtOnce(t *testing.T) {
	d, s, lines := restDeps(t, 4)
	th := NewThief()
	start := time.Now()
	if !th.restStep(d, freshstart.RestBelowHP, start) {
		t.Fatal("no rest")
	}
	// Eight minutes on, still 4 of 10 (the modal is gone but regeneration is not coming).
	at := start.Add(freshstart.RestMax)
	if th.restStep(d, freshstart.RestBelowHP, at) {
		t.Fatal("the rest was not given up after the bound")
	}
	if !logHas(*lines, "rest: still at 4/10 hitpoints after 8m0s") {
		t.Fatalf("the log does not read the hitpoints: %v", *lines)
	}
	// Still at 40% a pass later: it works (the cooldown), it does not rest again.
	if th.restStep(d, freshstart.RestBelowHP, at.Add(time.Minute)) {
		t.Fatal("it rested again inside the cooldown")
	}
	if th.restStep(d, freshstart.RestBelowHP, at.Add(freshstart.RestCooldown-time.Second)) {
		t.Fatal("it rested again just inside the cooldown")
	}
	// The cooldown over and still hurt: the rest starts afresh.
	if !th.restStep(d, freshstart.RestBelowHP, at.Add(freshstart.RestCooldown+time.Second)) {
		t.Fatal("no rest after the cooldown")
	}
	_ = s
}

// A rest that ends the ordinary way (health back over the resume mark) is not a give-up.
func TestARestEndsWhenHealthIsBack(t *testing.T) {
	d, s, lines := restDeps(t, 4)
	th := NewThief()
	now := time.Now()
	th.restStep(d, freshstart.RestBelowHP, now)
	s.setStat(gamestate.HitpointsStat, 9) // 90%
	if th.restStep(d, freshstart.RestBelowHP, now.Add(6*time.Minute)) {
		t.Fatal("still resting at 9 of 10")
	}
	if logHas(*lines, "rest: still at") {
		t.Fatalf("a rest that ended on health was logged as given up: %v", *lines)
	}
}

// Health that is fine never rests.
func TestAHealthyAccountDoesNotRest(t *testing.T) {
	d, _, _ := restDeps(t, 10)
	if NewThief().restStep(d, freshstart.RestBelowHP, time.Now()) {
		t.Fatal("rested at full health")
	}
}
