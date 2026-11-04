package bot

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/randomevents"
)

// masterDismissChat presses "Click here to continue" on a chat dialog left open while no random
// event is on (the Guardian owns those dialogs): in practice a level-up's congratulations, which
// stops the uses and keeps the bank from opening until something closes it.
//
// Live on a61e5d03 (5 Oct 2026, 17:29) the main account's Fletching went from 97 to 98 in the middle of a
// four-tick bow run: three runs in a row fell out of step, BowStringer's slow trips were refused
// three times, the bank would not open for the hand-over (failed, deliveries backed off 30 minutes)
// and the next round could not open it either, until a Genie's own dialog replaced the level-up
// three minutes later. Called before every fast run, so the dialog never outlives the load it
// interrupted. Reports whether it pressed.
func masterDismissChat(ctx *Context) bool {
	if ctx == nil || ctx.Session == nil || ctx.Components == nil || randomevents.Busy(ctx.Session) {
		return false
	}
	cd := actions.NewChatDialog(ctx.Session, ctx.Components)
	if !cd.IsOpen() || !cd.CanContinue() {
		return false
	}
	if err := cd.Continue(); err != nil {
		return false
	}
	ctx.logf("mastercrafter: closed a chat dialog (a level-up) that would stop the next load")
	ctx.DelayUntil(func() bool { return !cd.IsOpen() || !cd.CanContinue() }, 1500*time.Millisecond)
	return true
}
