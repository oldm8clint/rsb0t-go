package bot

// The round schedule's I/O (mastercraftplan/schedule.go has the rules and the order): the fleet's banks read off the peer board for the
// combined Steel and Nature stocks, and the ledger notes a round's end makes for the clock rules.

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/peerplan"
)

// fleetBankTotals sums the named items over every bank the peer board holds a fresh row of (the mule's and every account that
// publishes one); known is false when the board is absent or holds none.
func fleetBankTotals(ctx *Context, items ...string) (totals map[string]int, known bool) {
	if ctx == nil || ctx.Peers == nil {
		return nil, false
	}
	want := map[string]string{}
	for _, it := range items {
		want[strings.ToLower(strings.TrimSpace(it))] = it
	}
	totals = map[string]int{}
	now := time.Now()
	for _, e := range ctx.Peers.Entries(now) {
		if e.UpdatedAt.IsZero() || now.Sub(e.UpdatedAt) > peerplan.DefaultMaxAge {
			continue
		}
		known = true
		for _, h := range e.Bank {
			if name, ok := want[strings.ToLower(strings.TrimSpace(h.Item))]; ok && h.Count > 0 {
				totals[name] += h.Count
			}
		}
	}
	return totals, known
}

// schedSnapshot reads the schedule's fleet-wide figures into the snapshot: the Steel bars and Nature runes of every bank together
// (a user request: "combined Steel bars", "combined bank total" of Natures).
func (b *masterCrafterBot) schedSnapshot(ctx *Context, snap *mastercraftplan.Snapshot) {
	totals, known := fleetBankTotals(ctx, "Steel bar", "Nature rune")
	if !known {
		return
	}
	snap.Sched = mastercraftplan.ScheduleInput{Steel: totals["Steel bar"], SteelKnown: true, Nature: totals["Nature rune"], NatureKnown: true}
	if b.ledger.Sched.SettleSteel(b.set, snap.Sched.Steel, true) {
		b.logf(ctx, "superheat: the combined Steel bars reached %d (craftSteelStop %d): the Steel phase is done", snap.Sched.Steel, b.set.Sched.SteelStop)
		b.saveLedger(ctx)
	}
}

// noteScheduleEnd records what a round's end means for the schedule: the rotation's turn, the herb clock, a superheat phase's pause.
// It runs for every round that was not stopped from outside.
func (b *masterCrafterBot) noteScheduleEnd(ctx *Context, out masterRoundOutcome, now time.Time) {
	l := &b.ledger
	if out.Transient {
		return // a round that could not run has not had its turn
	}
	if key := out.RotationKey; key != "" {
		l.Sched.NoteRun(key, now)
	}
	if out.Mode == mastercraftplan.ModeHerbs {
		l.Sched.NoteEnd(mastercraftplan.KeyHerbRound, now)
	}
	if out.Mode == mastercraftplan.ModeSuperheat && out.Phase != "" {
		// A Mithril or Adamantite batch is left alone for its pause setting once it ends, by its hour or by the ore (PauseHeat
		// ignores the Steel phase, which runs to its stock).
		l.Sched.PauseHeat(out.Phase, now, b.set)
		l.Sched.SteelActive = out.Phase == mastercraftplan.PhaseSteel && out.SteelOpen
	}
}
