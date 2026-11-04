package flaxrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/protocol/clientprot"
)

// The hold loop under a level-up chat (docs/click_flood_audit_2026-10-15.md): the picker used to return "dialog" the moment a
// Continue-able chat showed and rely on its caller to answer it. A caller that calls it again at once (the shape essencerunner.Mine
// was found in, 15 Oct 2026) clicked the bush on every call while the client still showed the chat, thousands of clicks a second.

const (
	chatLevelUp = 4416
	chatButton  = 4440
	otherItem   = 995
)

type pickRig struct {
	sim    *actionsim.Session
	clock  *actionsim.Clock
	clicks *actionsim.Clicks
	d      *Deps
	bush   locindex.Match
	log    []string
}

func newPickRig(t *testing.T, chatClosesOnContinue, chatClosesOnClick bool) *pickRig {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(3000, 3000)
	r := &pickRig{sim: sim, clock: actionsim.NewClock(sim), bush: locindex.Match{Loc: locindex.Loc{ID: 2646, X: 3001, Z: 3000}, Name: "Flax", Op: 1}}
	sim.ModelChat(chatClosesOnContinue, chatClosesOnClick)
	r.clicks = sim.RecordClicks()
	r.d = &Deps{
		Session: sim, Config: &cachecfg.Config{}, Chat: chatlog.New(),
		Components: archive.Components{
			chatLevelUp: {ID: chatLevelUp, Type: archive.TypeLayer, Children: []int{chatButton}},
			chatButton:  {ID: chatButton, ButtonType: archive.ButtonContinue},
		},
		Delay: r.clock.Delay,
		Log:   func(s string) { r.log = append(r.log, s) },
	}
	return r
}

// fillPackIn makes the pack full ticks from now, which ends the hold with "full".
func (r *pickRig) fillPackIn(ticks int) {
	r.clock.In(ticks, func() { r.sim.Give(otherItem, 28) })
}

func (r *pickRig) maxClicksInATick() int {
	most, _ := r.clicks.MaxPerTick(actionsim.ClickOps...)
	return most
}

// pickBeforeTheFix is the hold loop as claude/batch2-1007 had it: a Continue-able chat ends it at once.
func pickBeforeTheFix(d *Deps, name string, m locindex.Match) PickResult {
	if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
		return PickResult{Reason: "no-flax"}
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	for {
		if dlg.CanContinue() {
			return PickResult{Reason: "dialog"}
		}
		if PackFull(d) {
			return PickResult{Reason: "full"}
		}
		if !d.Delay(pickTickInterval) {
			return PickResult{Reason: "stopped"}
		}
	}
}

// The failure, kept so the fix has something to be measured against: a chat is up, a caller with nothing to wait on calls the old
// hold again and again, and every call clicks the bush in the same tick.
func TestTheOldPickLoopFloodsTheBushWhileAChatStaysUp(t *testing.T) {
	r := newPickRig(t, false, false)
	r.sim.OpenChat(chatLevelUp)
	for i := 0; i < 200; i++ {
		pickBeforeTheFix(r.d, "Flax", r.bush)
	}
	got := r.maxClicksInATick()
	t.Logf("the old loop: 200 calls, %d click(s) in its busiest tick", got)
	if got < 100 {
		t.Errorf("the old loop sent only %d click(s) in its busiest tick; the failure being kept needs hundreds", got)
	}
}

// The fix: the same caller, the same chat, and the bush is clicked at most once a tick, at most once every other tick, and the
// chat is answered once.
func TestAChatThatStaysUpNeverMakesThePickerClickFasterThanTheGate(t *testing.T) {
	r := newPickRig(t, false, false)
	r.sim.OpenChat(chatLevelUp)
	for i := 0; i < 200; i++ {
		res := holdPick(r.d, "Flax", "Pick", r.bush)
		if res.Reason != "dialog" {
			t.Fatalf("call %d: reason %q; a chat that never goes is handed back as \"dialog\"", i, res.Reason)
		}
	}
	most := r.maxClicksInATick()
	ticks := int(r.clock.Now() / (600 * time.Millisecond))
	t.Logf("200 calls: %d click(s) on the bush in %d ticks, %d Continue(s), busiest tick %d click(s)",
		r.clicks.Count(clientprot.OPLOC1), ticks, r.clicks.Count(clientprot.RESUME_PAUSEBUTTON), most)
	if most > 1 {
		t.Errorf("%d clicks on the bush in one tick", most)
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n > ticks/actions.ClickGapTicks+1 {
		t.Errorf("%d clicks in %d ticks; the same bush may be clicked once every %d", n, ticks, actions.ClickGapTicks)
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Errorf("%d Continue(s); one per open chat", n)
	}
}

// A level-up that opens mid-pick is answered in place: Continue once, the chat closes, the bush is clicked once more, the pick goes
// on to fill the pack. It is not handed back to the Loop.
func TestALevelUpMidPickIsAnsweredInPlace(t *testing.T) {
	r := newPickRig(t, true, true)
	r.clock.In(3, func() { r.sim.OpenChat(chatLevelUp) })
	r.fillPackIn(14)
	res := holdPick(r.d, "Flax", "Pick", r.bush)
	if res.Reason != "full" {
		t.Fatalf("reason %q, want full (log %v)", res.Reason, r.log)
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Errorf("%d Continue(s), want 1", n)
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n != 2 {
		t.Errorf("%d clicks on the bush, want the first and one after the chat closed", n)
	}
	if most := r.maxClicksInATick(); most > 1 {
		t.Errorf("%d clicks in one tick", most)
	}
}

// A chat the click itself closes (it was up when the bush was clicked) needs no Continue and no second click.
func TestAChatUpWhenTheBushIsClickedIsLeftToTheClick(t *testing.T) {
	r := newPickRig(t, true, true)
	r.sim.OpenChat(chatLevelUp)
	r.fillPackIn(10)
	res := holdPick(r.d, "Flax", "Pick", r.bush)
	if res.Reason != "full" {
		t.Fatalf("reason %q, want full (log %v)", res.Reason, r.log)
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 0 {
		t.Errorf("%d Continue(s); the click closes that chat", n)
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n != 1 {
		t.Errorf("%d clicks on the bush, want 1", n)
	}
}

func TestAStopWhilePickingEndsTheHold(t *testing.T) {
	r := newPickRig(t, true, true)
	r.clock.Stop()
	res := holdPick(r.d, "Flax", "Pick", r.bush)
	if res.Reason != "stopped" {
		t.Errorf("reason %q, want stopped", res.Reason)
	}
}
