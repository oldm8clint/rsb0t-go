package bot

// Genie Lamps go to Magic (a user request). A random-event Genie hands out a Lamp ("[loot] random
// event Genie: completed, reward 1 x Lamp"); rubbed, it opens content's xplamp modal and one skill
// gets its base level x 10 experience. The Guardian claims a fresh Genie Lamp the moment it is
// handed over, but it picked Fishing, and a Lamp the Guardian could not claim at the time (a hostile
// in view, the modal never opening) just stayed in the pack. ScarabSwarm, MasterCrafter and RuneRunner
// (which runs ScarabSwarm whole) choose Magic at OnStart and rub any Lamp still held from their own
// loop. The click sequence and the content facts are in internal/randomevents/lamp.go.
//
// A Lamp cannot be banked (content's unbankable param), so there is nothing to draw out of the
// account's own bank: a Lamp is only ever in the backpack.

import (
	"time"

	"rsb0t-go/internal/randomevents"
)

// lampRubEvery spaces the held-Lamp check: a few-slot scan, but no reason to look every pass.
const lampRubEvery = 30 * time.Second

// lampRubInput is what decides whether a held Lamp is rubbed on this pass.
type lampRubInput struct {
	Now, Next time.Time
	Held      bool // a Lamp is in the backpack
	InCombat  bool
	Dead      bool
	ModalOpen bool // a bank, shop, trade or dialogue is open: the click sequence would collide with it
}

// lampRubWanted is true when a held Lamp should be rubbed now: due, held, and the account is not
// fighting, dead or in the middle of an interface.
func lampRubWanted(in lampRubInput) bool {
	return in.Held && !in.Now.Before(in.Next) && !in.InCombat && !in.Dead && !in.ModalOpen
}

// lampRubber is a script's held-Lamp check, spaced by lampRubEvery.
type lampRubber struct{ next time.Time }

// chooseMagicLamps is the OnStart half: this session's Lamps advance Magic.
func chooseMagicLamps(ctx *Context) {
	randomevents.SetLampSkill(ctx.Session, randomevents.LampMagic)
}

// tick rubs a held Lamp when lampRubWanted says so and logs each as "lamp: rubbed for Magic (+N xp)".
func (l *lampRubber) tick(ctx *Context) {
	now := time.Now()
	if now.Before(l.next) {
		return
	}
	st := ctx.Session.State()
	main, _, _ := st.Modals()
	in := lampRubInput{
		Now: now, Next: l.next, Held: randomevents.HeldLamp(ctx.Session) != nil,
		InCombat: st.InCombat(), Dead: st.IsDead(), ModalOpen: main != 0,
	}
	l.next = now.Add(lampRubEvery)
	if !lampRubWanted(in) {
		return
	}
	randomevents.RubHeldLamps(ctx.Session, ctx.Pack, ctx.Log)
}
