package thieverrunner

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// A thieving attempt under a level-up chat (docs/click_flood_audit_2026-10-15.md). The level-up the last steal earned opens the
// tick after the xp and blocks the next steal until it is closed. Attempt used to end the attempt as a miss at once, so a caller
// with nothing to wait on clicked the stall again in the same tick, and the miss counted towards the stand rotation.

const (
	attemptChat   = 4416
	attemptButton = 4440
	npcIndex      = 77
)

type attemptRig struct {
	sim    *actionsim.Session
	clock  *actionsim.Clock
	clicks *actionsim.Clicks
	d      *Deps
	xp     int
}

func newAttemptRig(chatClosesOnContinue, chatClosesOnClick bool) *attemptRig {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(3000, 3000)
	r := &attemptRig{sim: sim, clock: actionsim.NewClock(sim)}
	sim.ModelChat(chatClosesOnContinue, chatClosesOnClick)
	r.clicks = sim.RecordClicks()
	r.d = &Deps{
		Session: sim, Config: &cachecfg.Config{}, Chat: chatlog.New(),
		Components: archive.Components{
			attemptChat:   {ID: attemptChat, Type: archive.TypeLayer, Children: []int{attemptButton}},
			attemptButton: {ID: attemptButton, ButtonType: archive.ButtonContinue},
		},
		Delay: r.clock.Delay,
	}
	r.setXP(1000)
	return r
}

func (r *attemptRig) setXP(xp int) {
	r.xp = xp
	r.sim.State().Handle(serverprot.UPDATE_STAT, []byte{ThievingStat, byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), 10})
}

// stealsIn makes the steal land n ticks from now: the xp rises.
func (r *attemptRig) stealsIn(n int) { r.clock.In(n, func() { r.setXP(r.xp + 16) }) }

func (r *attemptRig) maxClicksInATick() int {
	most, _ := r.clicks.MaxPerTick(actionsim.ClickOps...)
	return most
}

// The level-up opens after the click and before the steal lands: Attempt presses its Continue, once, and the steal that follows is
// the attempt's success, not a miss.
func TestAnAttemptAnswersALevelUpInPlaceAndStillSucceeds(t *testing.T) {
	r := newAttemptRig(true, true)
	r.clock.In(1, func() { r.sim.OpenChat(attemptChat) })
	r.stealsIn(5)
	if !Attempt(r.d, 0, gatedNpc(r.d, 3, npcIndex)) {
		t.Fatal("the steal landed after the level-up was answered, but the attempt reported a miss")
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Errorf("%d Continue(s), want 1", n)
	}
	if n := r.clicks.Count(clientprot.OPNPC3); n != 1 {
		t.Errorf("%d click(s) on the NPC, want 1", n)
	}
}

// A chat that was already up when the click went out is the click's own to close: nothing is pressed.
func TestAnAttemptLeavesAChatUpAtTheClickToTheClick(t *testing.T) {
	r := newAttemptRig(true, true)
	r.sim.OpenChat(attemptChat)
	r.stealsIn(4)
	if !Attempt(r.d, 0, gatedNpc(r.d, 3, npcIndex)) {
		t.Fatal("a miss")
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 0 {
		t.Errorf("%d Continue(s); the click closes that chat", n)
	}
}

// A chat that nothing closes while the steal waits: one click, one Continue, and no second click from the attempt itself (the
// virtual clock runs far past the real 2.5 s wait, so the steal lands at +30 ticks inside it).
func TestAnAttemptUnderAChatThatStaysUpClicksOnceAndPressesOnce(t *testing.T) {
	r := newAttemptRig(false, false)
	r.clock.In(1, func() { r.sim.OpenChat(attemptChat) })
	r.stealsIn(30)
	if !Attempt(r.d, 0, gatedNpc(r.d, 3, npcIndex)) {
		t.Fatal("the steal landed, but the attempt reported a miss")
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Errorf("%d Continue(s), want 1 (the chat stayed up, and Continue latches once per open chat)", n)
	}
	if n := r.clicks.Count(clientprot.OPNPC3); n != 1 {
		t.Errorf("%d click(s) on the NPC, want 1", n)
	}
}

// The callers' clicks go through the gate: a loop of attempts on one NPC with nothing to wait on but the attempts themselves never
// clicks it more than once in two ticks, whatever the chat does.
func TestAttemptsOnOneNpcAreSpacedByTheGate(t *testing.T) {
	r := newAttemptRig(false, false)
	r.sim.OpenChat(attemptChat)
	for i := 0; i < 6; i++ {
		r.stealsIn(1)
		if !Attempt(r.d, 0, gatedNpc(r.d, 3, npcIndex)) {
			t.Fatalf("attempt %d missed", i)
		}
	}
	most := r.maxClicksInATick()
	ticks := int(r.clock.Now() / tickMs)
	t.Logf("6 attempts: %d click(s) in %d ticks, busiest tick %d", r.clicks.Count(clientprot.OPNPC3), ticks, most)
	if most > 1 {
		t.Errorf("%d clicks in one tick", most)
	}
	if n := r.clicks.Count(clientprot.OPNPC3); n != 6 {
		t.Errorf("%d clicks for 6 attempts", n)
	}
	if want := 5 * actions.ClickGapTicks; ticks < want {
		t.Errorf("6 clicks on one NPC took %d ticks; the gate wants at least %d", ticks, want)
	}
}

// Back-to-back steals of one stall: the second click waits out the gap from the first, however soon the caller comes round.
func TestTwoStealsOfOneStallAreAtLeastTwoTicksApart(t *testing.T) {
	r := newAttemptRig(true, true)
	r.stealsIn(1)
	if !Attempt(r.d, 0, gatedLoc(r.d, 1, 3002, 3002, 2561)) {
		t.Fatal("first steal missed")
	}
	firstSeen := r.sim.State().Tick()
	r.stealsIn(3)
	if !Attempt(r.d, 0, gatedLoc(r.d, 1, 3002, 3002, 2561)) {
		t.Fatal("second steal missed")
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n != 2 {
		t.Fatalf("%d clicks", n)
	}
	// the second click went out at least ClickGapTicks after the first; the second steal landed 3 ticks after that click
	if got := r.sim.State().Tick() - firstSeen; got < 3 {
		t.Errorf("the second steal finished %d tick(s) after the first was seen; its click cannot have been sent at once", got)
	}
	most, _ := r.clicks.MaxPerTick(clientprot.OPLOC1)
	if most > 1 {
		t.Errorf("%d stall clicks in one tick", most)
	}
}
