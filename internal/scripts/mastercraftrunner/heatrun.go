package mastercraftrunner

// THE SUPERHEAT RUN: the round's casts at the spell's own pace, the bank costing one tick a load
// (mastercraftplan/heatload.go has the engine facts, superheat.go THE PACE). Before it, every load
// was a full bank visit opened inside the last cast's delay (the engine dropped the booth op: a
// 20-second timeout), the casts read the next bar one round trip late (34 ticks for nine), and the
// round asked the mule again whenever the Nature runes sat in the pack instead of the bank.
//
//   - THE DOUBLE SEND. A cast read inside the last one's delay is dropped without a word
//     (OpHeldTHandler: "cannot interact while delayed"), and one read later than the first free
//     tick costs a tick. Which tick a packet is read in depends on the link: sent when the client
//     sees tick L+2 (L the tick the last bar showed), it is read in L+3 on a quick link and L+4 on
//     a slow one. So each cast goes twice: once when the client sees L+1 and again at L+2, unless
//     the first already showed. On a quick link the first is read in L+2 and dropped and the
//     second lands in L+3; on a slow one the first lands in L+3 and the second, read in L+4,
//     meets the new delay and is dropped. Either way the cast lands in L+3, three ticks a bar.
//   - THE BOOTH goes alone, in the first tick out of the delay (a cast in its tick would clear
//     it, and a booth op read inside the delay is dropped): sent at L+2 less the link's measured
//     lateness. The load and the first cast go one tick behind it in one message (five user
//     events), read in the tick after the bank opened.
//   - THE LINK's lateness is measured on every load: the tick the load's cast showed, against the
//     tick the client sent it in.
//
// Anything that does not land (a stray click, a bank that did not open, a slot that moved) is
// read back from the pack and the bank and sent again; three misses in a row end the run.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// heatXP is one cast's Magic XP as the client sees it (content's 530 tenths).
const heatXP = mastercraftplan.SuperheatXPTenths / 10

// heatLandTicks is how many ticks after a send the run waits for its cast to show before it
// counts a miss: a slow link's tick, a load's sixth event spilling into the next tick, a margin.
const heatLandTicks = 4

// heatMisses is the misses in a row that end a run.
const heatMisses = 3

// HeatOptions shapes one SuperheatRun.
type HeatOptions struct {
	// Pick is the bar the next load makes from the bank's and the pack's stock together; false
	// ends the run: nothing is left to make.
	Pick func(stock mastercraftplan.SuperheatStock) (mastercraftplan.Bar, bool)
	// Keep leaves more items in the pack through every deposit (the spell's runes always stay):
	// the weapon a fire staff replaced.
	Keep func(id int32) bool
	// Staff is a fire staff worn: no Fire runes are drawn or needed.
	Staff bool
	// Hold runs before every send; false ends the run (a stop, the round's budget, a random event).
	Hold func() bool
	// Quick is the booth the round's first bank visit learned (FullVisit); a run without it learns
	// the booth when it opens the bank.
	Quick *QuickBank
	// OnMade, when set, is told each landed cast as it lands: a run over a big ore stack lasts hours, and the loot
	// tracker should not wait for it.
	OnMade func(bar mastercraftplan.Bar, n int)
}

// HeatMade is the casts that landed on one bar.
type HeatMade struct {
	Bar   mastercraftplan.Bar
	Casts int
}

// HeatRun is what one SuperheatRun did.
type HeatRun struct {
	// Opened is the bank opening at the start.
	Opened bool
	// Loads is the loads drawn; Casts the casts that landed (from the Magic XP), by bar in Made;
	// Copies the cast packets sent (a cast goes twice when the last one's delay may still hold);
	// Booths the booth ops; Reopens the full bank opens after the first; Late the casts that
	// landed later than three ticks after the last; Lag the link's lateness in ticks as the last
	// load measured it; MagicXP and SmithingXP the XP; Ticks the server ticks the run spanned.
	Loads, Casts, Copies, Booths, Reopens, Late, Lag int
	MagicXP, SmithingXP, Ticks                       int
	Made                                             []HeatMade
	onMade                                           func(bar mastercraftplan.Bar, n int)
	// Reason is "done" (nothing left the bank and the pack can make), "level", "runes",
	// "smithing", "ore" (a refusal in the chat), "no-open" (the bank would not open), "stalled"
	// (casts or loads that did not land), "held" or "stopped"; Why says more.
	Reason, Why string
	Took        time.Duration
}

// Describe is a one-line account of a run for a log.
func (r HeatRun) Describe() string {
	var made []string
	for _, m := range r.Made {
		made = append(made, fmt.Sprintf("%d %s", m.Casts, m.Bar.Name))
	}
	per := "-"
	if r.Casts > 0 {
		per = fmt.Sprintf("%.2f", float64(r.Ticks)/float64(r.Casts))
	}
	s := fmt.Sprintf("superheat run: %d cast(s) (%s) in %d load(s) over %d tick(s) (%s a cast), %d copies sent, %d booth op(s), %d late, link lag %d, +%d Magic +%d Smithing XP in %s (%s",
		r.Casts, firstOr(strings.Join(made, ", "), "none"), r.Loads, r.Ticks, per, r.Copies, r.Booths, r.Late, r.Lag, r.MagicXP, r.SmithingXP, r.Took.Round(100*time.Millisecond), r.Reason)
	if r.Why != "" {
		s += ": " + r.Why
	}
	return s + ")"
}

func firstOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

func (r *HeatRun) made(bar mastercraftplan.Bar, n int) {
	if n <= 0 {
		return
	}
	r.Casts += n
	if r.onMade != nil {
		r.onMade(bar, n)
	}
	for i := range r.Made {
		if r.Made[i].Bar.BarID == bar.BarID {
			r.Made[i].Casts += n
			return
		}
	}
	r.Made = append(r.Made, HeatMade{Bar: bar, Casts: n})
}

// HeatPack is a pack as the load plan reads it, by slot.
func HeatPack(items []gamestate.SlotItem) (p [gemplan.PackSlots]mastercraftplan.HeatSlot) {
	for _, it := range items {
		if it.Slot >= 0 && it.Slot < len(p) && it.ID > 0 {
			p[it.Slot] = mastercraftplan.HeatSlot{ID: it.ID, Count: max(1, int(it.Count)), Slot: it.Slot}
		}
	}
	return p
}

// HeatRows is a bank as the load plan reads it.
func HeatRows(items []gamestate.SlotItem) []mastercraftplan.HeatSlot {
	out := make([]mastercraftplan.HeatSlot, 0, len(items))
	for _, it := range items {
		if it.ID > 0 && it.Count > 0 {
			out = append(out, mastercraftplan.HeatSlot{ID: it.ID, Count: int(it.Count), Slot: it.Slot})
		}
	}
	return out
}

// heatCastSlot is the slot of the pack's first ore of bar when the pack can make a cast of it now,
// else -1.
func heatCastSlot(pack []gamestate.SlotItem, bar mastercraftplan.Bar, staff bool) int {
	p := HeatPack(pack)
	if mastercraftplan.HeatCasts(bar, &p, staff) <= 0 {
		return -1
	}
	for i, s := range p {
		if s.ID == bar.OreID {
			return i
		}
	}
	return -1
}

// SuperheatRun superheats bars at post until Pick finds nothing left, a refusal, the hold or
// three misses in a row (see the file comment). It opens the bank the full way first, banks the
// pack but the runes and what Keep keeps, and leaves the bank closed.
func SuperheatRun(d *fleetrunner.Deps, post tile.Tile, o HeatOptions) (r HeatRun) {
	r.onMade = o.OnMade
	start := time.Now()
	st := d.Session.State()
	magic := func() int { _, xp := statOf(d, MagicStat); return xp }
	_, sxp0 := statOf(d, SmithingStat)
	mxp0, tick0 := magic(), st.Tick()
	defer func() {
		_, sxp := statOf(d, SmithingStat)
		r.MagicXP, r.SmithingXP, r.Ticks, r.Took = magic()-mxp0, sxp-sxp0, st.Tick()-tick0, time.Since(start)
	}()
	q := o.Quick
	if q == nil {
		q = &QuickBank{}
	}
	keep := func(id int32) bool { return KeepRunes(id) || (o.Keep != nil && o.Keep(id)) }
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))

	// waitTick waits until the client has seen tick c, bounded by wall time for a session that
	// stops seeing ticks; false on a stop.
	waitTick := func(c int) bool {
		deadline := time.Now().Add(time.Duration(max(1, c-st.Tick()))*650*time.Millisecond + 300*time.Millisecond)
		for st.Tick() < c && time.Now().Before(deadline) {
			if !d.Delay(quickPoll) {
				return false
			}
		}
		return true
	}
	// waitFor waits until cond holds or the client has seen tick c; it reports cond.
	waitFor := func(cond func() bool, c int) bool {
		deadline := time.Now().Add(time.Duration(max(1, c-st.Tick()))*650*time.Millisecond + 300*time.Millisecond)
		for !cond() {
			if st.Tick() >= c || !time.Now().Before(deadline) || !d.Delay(quickPoll) {
				return cond()
			}
		}
		return true
	}
	send := func(build func(s actions.Session) error) error {
		b := &batch{s: d.Session}
		if err := build(b); err != nil {
			return err
		}
		return b.flush()
	}

	// 1. The bank, the full way: the pack banked but the runes and what Keep keeps, notes off.
	bank, ok := OpenBank(d, post)
	if !ok || bank == nil {
		r.Reason, r.Why = "no-open", fmt.Sprintf("could not open the bank at %d,%d", post.X, post.Z)
		return r
	}
	r.Opened = true
	var hot bool   // a cast's delay may still hold
	var landed int // the tick the last cast showed
	defer func() {
		// Never hand the account back inside a cast's delay: the caller's next booth or bank op
		// would be dropped (the old round's 20-second "timed out waiting for bank to open").
		if hot {
			waitTick(landed + 2)
		}
		if bank.IsOpen() {
			_ = bank.Close()
		}
	}()
	if !q.Ready(d) {
		q.Learn(d, bank)
	}
	if len(bank.Inventory()) > 0 {
		if err := bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return !keep(it.ID) }, w); err != nil {
			r.Reason, r.Why = "stalled", "the first deposit: "+err.Error()
			return r
		}
	}
	if err := bank.SetNoteMode(false, w); err != nil {
		r.Reason, r.Why = "stalled", "note mode off: "+err.Error()
		return r
	}
	sideCom, _ := bank.InventorySnapshot()
	// rows is the bank as last read, kept up by every load's own operations: the cast closes the
	// bank in the tick the load is drawn, and the engine sends a closed bank's inventory to nobody,
	// so the client never sees what a load took. An opening's inventory replaces it only while it
	// is current (trust): read before this opening's load went out, or after a load whose cast
	// missed left the bank open (its inventory then follows every change). On a slow link the
	// opening's inventory arrives after the load went out, and is a load out of date.
	rows := append([]gamestate.SlotItem(nil), bank.Items()...)
	rowsGen := invGen(st, actions.BankWithdraw)
	trust := true
	refresh := func() {
		if !trust || !bank.IsOpen() {
			return
		}
		if g := invGen(st, actions.BankWithdraw); g > rowsGen {
			if it := bank.Items(); len(it) > 0 {
				rows, rowsGen = append(rows[:0], it...), g
			}
		}
	}

	lastM := magic()
	landed = st.Tick()
	misses := 0
	boothAt := -1 // the tick a booth op went out for the load not yet drawn
	// landing waits for the Magic XP to rise past lastM by tick c and books what landed on bar.
	landing := func(bar mastercraftplan.Bar, c int) bool {
		if !waitFor(func() bool { return magic() > lastM }, c) {
			return false
		}
		n := max(1, (magic()-lastM)/heatXP)
		r.made(bar, n)
		lastM, landed, hot, misses = magic(), st.Tick(), true, 0
		return true
	}
	miss := func(why string) bool {
		hot = false
		if misses++; misses >= heatMisses {
			r.Reason, r.Why = "stalled", why
			return false
		}
		return true
	}
	for {
		if o.Hold != nil && !o.Hold() {
			r.Reason = "held"
			return r
		}
		if d.Chat != nil {
			if why := mastercraftplan.SuperheatRefusal(d.Chat.Since(start)); why != "" {
				r.Reason = why
				return r
			}
		}
		pack := packItems(d)
		refresh()
		stock := SuperheatOwn(append(append([]gamestate.SlotItem(nil), rows...), pack...))
		bar, ok := o.Pick(stock)
		if !ok {
			r.Reason = "done"
			return r
		}

		// A cast on ore the pack already holds: the double send.
		if slot := heatCastSlot(pack, bar, o.Staff); slot >= 0 {
			cast := func(s actions.Session) error {
				return actions.CastOnItem(s, SuperheatSpell, bar.OreID, slot, fleetrunner.PlayerPack)
			}
			was, from := hot, landed
			if hot {
				if !waitTick(landed + 1) {
					r.Reason = "stopped"
					return r
				}
			}
			if err := send(cast); err != nil {
				r.Reason, r.Why = "stalled", "sending a cast: "+err.Error()
				return r
			}
			r.Copies++
			if hot {
				if !waitTick(landed + 2) {
					r.Reason = "stopped"
					return r
				}
				if magic() == lastM {
					if err := send(cast); err != nil {
						r.Reason, r.Why = "stalled", "sending a cast: "+err.Error()
						return r
					}
					r.Copies++
				}
			}
			if landing(bar, st.Tick()+heatLandTicks) {
				if was && landed > from+mastercraftplan.SuperheatTicks {
					r.Late++
				}
				continue
			}
			if !miss(fmt.Sprintf("a cast on %s in slot %d did not land", bar.Ore, slot)) {
				return r
			}
			continue
		}

		// A load: the booth (unless the bank is open), then the load and its first cast.
		if !bank.IsOpen() && boothAt < 0 {
			if !q.Ready(d) {
				// Not on the tile the booth was learned from: the full way, which learns again.
				if hot {
					waitTick(landed + mastercraftplan.SuperheatTicks)
				}
				hot = false
				_ = bank.Close()
				b2, ok := OpenBank(d, post)
				if !ok || b2 == nil {
					r.Reason, r.Why = "no-open", fmt.Sprintf("could not open the bank at %d,%d again", post.X, post.Z)
					return r
				}
				bank = b2
				r.Reopens++
				q.Learn(d, bank)
				sideCom, _ = bank.InventorySnapshot()
				continue
			}
			if hot && !waitTick(landed+2-r.Lag) {
				r.Reason = "stopped"
				return r
			}
			if err := actions.OpLoc(d.Session, q.booth.Op, q.booth.X, q.booth.Z, q.booth.ID); err != nil {
				r.Reason, r.Why = "stalled", "the booth op: "+err.Error()
				return r
			}
			r.Booths++
			boothAt = st.Tick()
			hot, trust = false, true
			// The load goes one tick behind the booth op, read in the tick after the bank opened.
			if !waitTick(boothAt + 1) {
				r.Reason = "stopped"
				return r
			}
			refresh()
		}
		if q.ready && q.sideCom >= 0 {
			sideCom = q.sideCom
		}
		l, why := mastercraftplan.PlanHeatLoad(bar, HeatPack(pack), HeatRows(rows), o.Keep, o.Staff)
		if l.Casts <= 0 || l.CastSlot < 0 {
			if bank.IsOpen() {
				r.Reason, r.Why = "done", why
				return r
			}
			// Planned from rows read before the booth op: wait for the bank and plan again.
			if !waitFor(bank.IsOpen, st.Tick()+heatLandTicks) && !miss("the bank did not open for the load: "+why) {
				return r
			}
			boothAt = -1
			continue
		}
		frame := func(s actions.Session) error {
			for _, op := range l.Ops {
				var err error
				switch op.Kind {
				case mastercraftplan.HeatDeposit:
					err = actions.InvButton(s, 4, op.ID, op.Slot, sideCom)
				case mastercraftplan.HeatWithdrawAll:
					err = actions.InvButton(s, 4, op.ID, op.Slot, actions.BankWithdraw)
				case mastercraftplan.HeatWithdrawX:
					if err = actions.InvButton(s, 5, op.ID, op.Slot, actions.BankWithdraw); err == nil {
						err = actions.ResumeCountDialog(s, int32(op.Count))
					}
				}
				if err != nil {
					return err
				}
			}
			return actions.CastOnItem(s, SuperheatSpell, bar.OreID, l.CastSlot, fleetrunner.PlayerPack)
		}
		if err := send(frame); err != nil {
			r.Reason, r.Why = "stalled", "sending a load: "+err.Error()
			return r
		}
		sentAt := st.Tick()
		boothAt, trust = -1, false
		r.Loads++
		spill := (l.Events - 1) / 5 // a load of more than five events reads its cast a tick later
		if landing(bar, sentAt+heatLandTicks+spill) {
			r.Lag = min(max(landed-sentAt-1-spill, 0), 2)
			rows = heatAfter(rows, l.Ops)
			continue
		}
		trust = bank.IsOpen()
		if !miss(fmt.Sprintf("a load of %s did not land (%d events)", bar.Ore, l.Events)) {
			return r
		}
	}
}

// heatAfter is the bank's rows once a load's operations went through (see rows in SuperheatRun).
// The engine's bank never shifts: a row withdrawn to nothing leaves its slot empty, and a kind the
// bank lacks goes to the lowest empty slot (Inventory.remove, Inventory.add).
func heatAfter(rows []gamestate.SlotItem, ops []mastercraftplan.HeatOp) []gamestate.SlotItem {
	out := append([]gamestate.SlotItem(nil), rows...)
	for _, op := range ops {
		at := -1
		for i := range out {
			if out[i].ID == op.ID {
				at = i
				break
			}
		}
		if op.Kind == mastercraftplan.HeatDeposit {
			if at >= 0 {
				out[at].Count += int32(op.Count)
				continue
			}
			used := map[int]bool{}
			for _, r := range out {
				used[r.Slot] = true
			}
			slot := 0
			for used[slot] {
				slot++
			}
			out = append(out, gamestate.SlotItem{Slot: slot, Item: gamestate.Item{ID: op.ID, Count: int32(op.Count)}})
			continue
		}
		if at < 0 {
			continue
		}
		if out[at].Count -= int32(op.Count); out[at].Count <= 0 {
			out = append(out[:at], out[at+1:]...)
		}
	}
	return out
}
