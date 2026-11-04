package woodcutrunner

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
	"rsb0t-go/internal/scripts/woodcutplan"
)

// The chop hold under a level-up chat (docs/click_flood_audit_2026-10-15.md). Chop used to return "dialog" the moment a Continue-able
// chat showed, so any caller that called it again at once clicked the tree on every call while the client still showed the chat.

const (
	holdChatLevelUp = 4416
	holdChatButton  = 4440
	holdFiller      = 995
)

type chopRig struct {
	sim    *actionsim.Session
	clock  *actionsim.Clock
	clicks *actionsim.Clicks
	d      *Deps
	tree   locindex.Match
	tier   woodcutplan.Tier
	log    []string
}

func newChopRig(chatClosesOnContinue, chatClosesOnClick bool) *chopRig {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(3000, 3000)
	r := &chopRig{
		sim: sim, clock: actionsim.NewClock(sim),
		tree: locindex.Match{Loc: locindex.Loc{ID: 1276, X: 3001, Z: 3000}, Name: "Tree", Op: 1},
		tier: woodcutplan.Tier{Label: "Normal", Loc: "Tree"},
	}
	sim.ModelChat(chatClosesOnContinue, chatClosesOnClick)
	r.clicks = sim.RecordClicks()
	r.d = &Deps{
		Session: sim, Config: &cachecfg.Config{}, Chat: chatlog.New(),
		Components: archive.Components{
			holdChatLevelUp: {ID: holdChatLevelUp, Type: archive.TypeLayer, Children: []int{holdChatButton}},
			holdChatButton:  {ID: holdChatButton, ButtonType: archive.ButtonContinue},
		},
		Delay: r.clock.Delay,
		Log:   func(s string) { r.log = append(r.log, s) },
	}
	return r
}

func (r *chopRig) hold(pick woodcutplan.TreePick) (ChopResult, bool) {
	return holdChop(r.d, r.tier, r.tree, pick, func() bool { return true })
}

func (r *chopRig) fillPackIn(ticks int) {
	r.clock.In(ticks, func() { r.sim.Give(holdFiller, 28) })
}

func (r *chopRig) maxClicksInATick() int {
	most, _ := r.clicks.MaxPerTick(actionsim.ClickOps...)
	return most
}

// chopBeforeTheFix is the hold as claude/batch2-1007 had it: a Continue-able chat ends it at once.
func chopBeforeTheFix(d *Deps, m locindex.Match) ChopResult {
	if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
		return ChopResult{Reason: "no-tree"}
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	for {
		if dlg.CanContinue() {
			return ChopResult{Reason: "dialog"}
		}
		if PackFull(d) {
			return ChopResult{Reason: "full"}
		}
		if !d.Delay(chopTickInterval) {
			return ChopResult{Reason: "stopped"}
		}
	}
}

func TestTheOldChopHoldFloodsTheTreeWhileAChatStaysUp(t *testing.T) {
	r := newChopRig(false, false)
	r.sim.OpenChat(holdChatLevelUp)
	for i := 0; i < 200; i++ {
		chopBeforeTheFix(r.d, r.tree)
	}
	got := r.maxClicksInATick()
	t.Logf("the old hold: 200 calls, %d click(s) in its busiest tick", got)
	if got < 100 {
		t.Errorf("the old hold sent only %d click(s) in its busiest tick; the failure being kept needs hundreds", got)
	}
}

func TestAChatThatStaysUpNeverMakesTheChopperClickFasterThanTheGate(t *testing.T) {
	r := newChopRig(false, false)
	r.sim.OpenChat(holdChatLevelUp)
	for i := 0; i < 200; i++ {
		res, _ := r.hold(woodcutplan.TreePick{})
		if res.Reason != "dialog" {
			t.Fatalf("call %d: reason %q; a chat that never goes is handed back as \"dialog\"", i, res.Reason)
		}
	}
	most := r.maxClicksInATick()
	ticks := int(r.clock.Now() / (600 * time.Millisecond))
	t.Logf("200 calls: %d click(s) on the tree in %d ticks, %d Continue(s), busiest tick %d click(s)",
		r.clicks.Count(clientprot.OPLOC1), ticks, r.clicks.Count(clientprot.RESUME_PAUSEBUTTON), most)
	if most > 1 {
		t.Errorf("%d clicks on the tree in one tick", most)
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n > ticks/actions.ClickGapTicks+1 {
		t.Errorf("%d clicks in %d ticks; the same tree may be clicked once every %d", n, ticks, actions.ClickGapTicks)
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Errorf("%d Continue(s); one per open chat", n)
	}
}

func TestALevelUpMidChopIsAnsweredInPlace(t *testing.T) {
	r := newChopRig(true, true)
	r.clock.In(3, func() { r.sim.OpenChat(holdChatLevelUp) })
	r.fillPackIn(14)
	res, _ := r.hold(woodcutplan.TreePick{})
	if res.Reason != "full" {
		t.Fatalf("reason %q, want full (log %v)", res.Reason, r.log)
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Errorf("%d Continue(s), want 1", n)
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n != 2 {
		t.Errorf("%d clicks on the tree, want the first and one after the chat closed", n)
	}
	if most := r.maxClicksInATick(); most > 1 {
		t.Errorf("%d clicks in one tick", most)
	}
}

func TestAChatUpWhenTheTreeIsClickedIsLeftToTheClick(t *testing.T) {
	r := newChopRig(true, true)
	r.sim.OpenChat(holdChatLevelUp)
	r.fillPackIn(10)
	res, _ := r.hold(woodcutplan.TreePick{})
	if res.Reason != "full" {
		t.Fatalf("reason %q, want full (log %v)", res.Reason, r.log)
	}
	if n := r.clicks.Count(clientprot.RESUME_PAUSEBUTTON); n != 0 {
		t.Errorf("%d Continue(s); the click closes that chat", n)
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n != 1 {
		t.Errorf("%d clicks on the tree, want 1", n)
	}
}

// A chat handed back keeps the account's claim on its tree (it comes straight back to it); one that is not wanted back does not.
func TestAHandedBackChatKeepsTheClaimOnTheTree(t *testing.T) {
	r := newChopRig(false, false)
	r.sim.OpenChat(holdChatLevelUp)
	res, keep := r.hold(woodcutplan.TreePick{Mine: true})
	if res.Reason != "dialog" || !keep {
		t.Errorf("reason %q, keep %v; want dialog and the claim kept", res.Reason, keep)
	}
	r2 := newChopRig(false, false)
	r2.sim.OpenChat(holdChatLevelUp)
	if res, keep := r2.hold(woodcutplan.TreePick{Mine: false}); res.Reason != "dialog" || keep {
		t.Errorf("reason %q, keep %v; a claim the account never held is not kept", res.Reason, keep)
	}
}

// Switching to another tree (after an Ent) is never held back by the gate, while the same tree is.
func TestTheNextTreeAfterAnEntIsClickedAtOnceButTheSameTreeWaits(t *testing.T) {
	r := newChopRig(true, true)
	r.sim.Give(holdFiller, 28) // a full pack: each hold clicks once and ends with "full" before it waits for anything
	if res, _ := r.hold(woodcutplan.TreePick{}); res.Reason != "full" {
		t.Fatalf("first hold: %q", res.Reason)
	}
	before := r.clock.Delays()
	other := r.tree
	other.X += 3
	if res, _ := holdChop(r.d, r.tier, other, woodcutplan.TreePick{}, func() bool { return true }); res.Reason != "full" {
		t.Fatalf("second hold: %q", res.Reason)
	}
	if got := r.clock.Delays() - before; got != 0 {
		t.Errorf("the click on another tree waited (%d delay(s)); the Ent rule is that the next click is not slowed", got)
	}
	before = r.clock.Delays()
	if res, _ := r.hold(woodcutplan.TreePick{}); res.Reason != "full" {
		t.Fatalf("third hold: %q", res.Reason)
	}
	if got := r.clock.Delays() - before; got == 0 {
		t.Error("a second click on the first tree, a tick later, did not wait for its gap")
	}
	if n := r.clicks.Count(clientprot.OPLOC1); n != 3 {
		t.Errorf("%d clicks, want 3", n)
	}
}

func TestAStopWhileChoppingEndsTheHold(t *testing.T) {
	r := newChopRig(true, true)
	r.clock.Stop()
	res, _ := r.hold(woodcutplan.TreePick{})
	if res.Reason != "stopped" {
		t.Errorf("reason %q, want stopped", res.Reason)
	}
}
