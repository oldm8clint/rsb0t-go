package mastercraftrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/gemrunner"
)

// StackOptions shapes one MakeStack: Use is used on With, two stacks in the pack, each use making
// up to PerUse of Product; Refusal reads the run's chat for content's refusals ("" carries on).
type StackOptions struct {
	Use, With, Product int32
	PerUse             int
	Refusal            func(said string) (reason string, level int)
	// Hold runs every tick before a burst: false ends the run with "held".
	Hold func() bool
}

// StackResult is one MakeStack.
type StackResult struct {
	// Made is the Product the pack gained, Uses the uses sent, XP the Fletching XP gained, Ticks
	// the server ticks it spanned.
	Made, Uses, XP, Ticks int
	// Reason is "done" (one of the two stacks is gone), "refused", "members", "stalled", "held" or
	// "stopped".
	Reason       string
	RefusedLevel int
}

// Describe is a one-line account of a MakeStack for a log.
func (r StackResult) Describe() string {
	return fmt.Sprintf("%d made with %d use(s) over %d tick(s), +%d Fletching XP (%s)", r.Made, r.Uses, r.Ticks, r.XP, r.Reason)
}

// stackConfirm is how long the pack has, after the last use of a pass, to show the product.
const stackConfirm = 2500 * time.Millisecond

// MakeStack is MakeBolts for any two stacks (the arrow round's shafts on feathers, then steel tips on
// headless arrows): five uses a server tick naming the same two stacks (actions.Equipment.
// UseOnHeldStack), each tick's burst sent when the client has seen the tick before it
// (gemrunner.NextTick), then one confirm that the product rose by what the stacks allowed; again if
// uses were dropped. Every burst waits for a server tick: nothing is streamed ahead of the server.
func MakeStack(d *gemrunner.Deps, o StackOptions) (res StackResult) {
	eq := actions.NewEquipment(d.Session, d.Components)
	per := max(o.PerUse, 1)
	made0, xp0, tick0 := gemrunner.CountByID(d, o.Product), stackFletchingXP(d), d.Session.State().Tick()
	at := time.Now()
	defer func() {
		res.Made = max(0, gemrunner.CountByID(d, o.Product)-made0)
		res.XP = stackFletchingXP(d) - xp0
		res.Ticks = d.Session.State().Tick() - tick0
	}()
	refused := func() bool {
		if d.Chat == nil || o.Refusal == nil {
			return false
		}
		if why, level := o.Refusal(d.Chat.Since(at)); why != "" {
			res.Reason, res.RefusedLevel = why, level
			return true
		}
		return false
	}
	for {
		a, b := gemrunner.CountByID(d, o.Use), gemrunner.CountByID(d, o.With)
		n := min(a, b)
		if n <= 0 {
			res.Reason = "done"
			return res
		}
		uses := (n + per - 1) / per
		before := gemrunner.CountByID(d, o.Product)
		for sent := 0; sent < uses; {
			if refused() {
				return res
			}
			if o.Hold != nil && !o.Hold() {
				res.Reason = "held"
				return res
			}
			tick := d.Session.State().Tick()
			k, err := eq.UseOnHeldStack(o.Use, o.With, min(5, uses-sent))
			if k == 0 {
				if d.Log != nil {
					d.Log(fmt.Sprintf("mastercrafter: %d on %d: %v", o.Use, o.With, err))
				}
				res.Reason = "stalled"
				return res
			}
			sent += k
			res.Uses += k
			if sent < uses && !gemrunner.NextTick(d, tick) {
				res.Reason = "stopped"
				return res
			}
		}
		want := before + n
		deadline := time.Now().Add(stackConfirm)
		for gemrunner.CountByID(d, o.Product) < want && time.Now().Before(deadline) {
			if refused() {
				return res
			}
			if !d.Delay(10 * time.Millisecond) {
				res.Reason = "stopped"
				return res
			}
		}
		if refused() {
			return res
		}
		if gemrunner.CountByID(d, o.Product) <= before {
			res.Reason = "stalled"
			return res
		}
	}
}

// stackFletchingXP is the account's Fletching XP in displayed points.
func stackFletchingXP(d *gemrunner.Deps) int {
	if sk, ok := d.Session.State().Skill(bowstringrunner.FletchingStat); ok {
		return sk.XP
	}
	return 0
}
