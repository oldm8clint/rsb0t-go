package mastercraftrunner

// The pearl round's I/O (the decisions and the content facts are internal/scripts/pearlplan's):
// a bank visit that draws the next load (QuickBank's, or a full one), the chisel on a load of
// oyster pearls (gemrunner.UseAll, five a tick, pipelined), and the tips on the Bolts
// (MakeBolts: five uses a tick on the same two stacks, ten bolts a use).

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// KeepChisel keeps the chisel in the pack through a deposit.
func KeepChisel(id int32) bool { return id == gemplan.ChiselID }

// PearlDraw is the next load of a tips phase: the pearl kind with the most tips a use that the
// bank holds (pearlplan.Kinds), nothing when it holds none.
func PearlDraw(bank []gamestate.SlotItem) []int32 {
	for _, k := range pearlplan.Kinds {
		if actions.CountByID(bank, k.ID) > 0 {
			return []int32{k.ID}
		}
	}
	return nil
}

// BoltDraw is the load of a bolts phase: every Pearl bolttips and every Bolts (both stack: two
// slots), nothing unless the bank holds both.
func BoltDraw(bank []gamestate.SlotItem) []int32 {
	if actions.CountByID(bank, pearlplan.TipsID) > 0 && actions.CountByID(bank, pearlplan.BoltID) > 0 {
		return []int32{pearlplan.TipsID, pearlplan.BoltID}
	}
	return nil
}

// PearlHeld reads the pearl round's items out of a bank reading.
func PearlHeld(bank []gamestate.SlotItem) pearlplan.Held {
	return pearlplan.Held{
		Pearl:  int(actions.CountByID(bank, pearlplan.PearlID)),
		Pearls: int(actions.CountByID(bank, pearlplan.PearlsID)),
		Tips:   int(actions.CountByID(bank, pearlplan.TipsID)),
		Bolts:  int(actions.CountByID(bank, pearlplan.BoltID)),
	}
}

// LoadVisit is a full bank visit of a load-by-load round.
type LoadVisit struct {
	Visit
	// Drew is the ids drawn and ChiselHeld a chisel in the pack afterwards.
	Drew       []int32
	ChiselHeld bool
}

// FullVisit opens the bank at post (OpenBank), teaches q, deposits every pack item keep does not
// keep, reads the bank, draws a chisel when wantChisel and none is carried, then Withdraw-All on
// each id draw picks, each confirmed in the pack. The fallback of a quick visit, and the first visit
// of a round.
func FullVisit(d *fleetrunner.Deps, post tile.Tile, q *QuickBank, keep func(id int32) bool, draw func(bank []gamestate.SlotItem) []int32, wantChisel bool) (v LoadVisit) {
	bank, ok := OpenBank(d, post)
	if !ok || bank == nil {
		v.Err = fmt.Sprintf("could not open the bank at %d,%d", post.X, post.Z)
		return v
	}
	v.Opened = true
	defer func() {
		_ = bank.Close()
		v.ChiselHeld = packCount(d, gemplan.ChiselID) > 0
	}()
	q.Learn(d, bank)
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if len(bank.Inventory()) > 0 {
		deposit := func(it gamestate.SlotItem) bool { return keep == nil || !keep(it.ID) }
		if err := bank.BatchDepositAllMatching(deposit, w); err != nil {
			v.Err = "deposit: " + err.Error()
			return v
		}
	}
	v.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	if err := bank.SetNoteMode(false, w); err != nil {
		v.Err = "note mode off: " + err.Error()
		return v
	}
	if wantChisel && packCount(d, gemplan.ChiselID) == 0 && v.Count(gemplan.ChiselID) > 0 {
		if err := bank.WithdrawOne(gemplan.ChiselID); err != nil {
			v.Err = "withdraw chisel: " + err.Error()
			return v
		}
		w.DelayUntil(func() bool { return packCount(d, gemplan.ChiselID) > 0 }, 3*time.Second)
	}
	if draw == nil {
		return v
	}
	for _, id := range draw(v.Banked) {
		before := packCount(d, id)
		if err := bank.WithdrawAll(id); err != nil {
			v.Err = fmt.Sprintf("withdraw %d: %v", id, err)
			return v
		}
		if !w.DelayUntil(func() bool { return packCount(d, id) > before }, 3*time.Second) {
			v.Err = fmt.Sprintf("withdraw %d did not show in the pack", id)
			return v
		}
		v.Drew = append(v.Drew, id)
	}
	return v
}

// fletchingXP is the account's Fletching XP in displayed points.
func fletchingXP(d *gemrunner.Deps) int {
	if sk, ok := d.Session.State().Skill(bowstringrunner.FletchingStat); ok {
		return sk.XP
	}
	return 0
}

// TipsResult is one load of pearls cut.
type TipsResult struct {
	gemrunner.UseResult
	Kind pearlplan.Kind
	// Cut is the pearls the pack lost, Tips the bolt tips it gained, XP the Fletching XP.
	Cut, Tips, XP int
}

// CutPearls uses the chisel on every held pearl of kind, five a tick, pipelined (gemrunner.UseAll),
// with afterLast (the next bank open) right behind the last burst.
func CutPearls(d *gemrunner.Deps, kind pearlplan.Kind, afterLast func()) TipsResult {
	r := TipsResult{Kind: kind}
	n0, t0, xp0 := gemrunner.CountByID(d, kind.ID), gemrunner.CountByID(d, pearlplan.TipsID), fletchingXP(d)
	r.UseResult = gemrunner.UseAll(d, gemrunner.UseOptions{
		Tool: gemplan.ChiselID, Target: kind.ID, AfterLast: afterLast, Refused: pearlplan.Refusal,
	})
	r.Cut = max(0, n0-gemrunner.CountByID(d, kind.ID))
	r.Tips = max(0, gemrunner.CountByID(d, pearlplan.TipsID)-t0)
	r.XP = fletchingXP(d) - xp0
	return r
}

// Describe is a one-line account of a load for a log.
func (r TipsResult) Describe() string {
	return fmt.Sprintf("%s: %d cut into %d Pearl bolttips in %d burst(s) over %d tick(s), +%d Fletching XP (%s)", r.Kind.Name, r.Cut, r.Tips, r.Bursts, r.Ticks, r.XP, r.Reason)
}

// boltConfirm is how long the pack has, after the last use of a run, to show the bolts.
const boltConfirm = 2500 * time.Millisecond

// BoltResult is one MakeBolts.
type BoltResult struct {
	// Made is the Pearl bolts the pack gained, Uses the uses sent, XP the Fletching XP gained,
	// Ticks the server ticks it spanned.
	Made, Uses, XP, Ticks int
	// Reason is "done" (the pack has no tips or no Bolts left), "refused", "members", "stalled"
	// (uses changed nothing), "held" (hold said stop) or "stopped".
	Reason       string
	RefusedLevel int
}

// MakeBolts puts the pack's Pearl bolttips on its Bolts: five uses a tick naming the same two
// stacks (actions.Equipment.UseOnHeldStack), each making up to ten bolts, as many uses as the two
// stacks allow (pearlplan.BoltUses) sent without waiting, then one confirm; again if uses were
// dropped. hold runs every tick before a burst (the caller's random-event wait and round budget):
// false ends the run with "held".
func MakeBolts(d *gemrunner.Deps, hold func() bool) (res BoltResult) {
	eq := actions.NewEquipment(d.Session, d.Components)
	made0, xp0, tick0 := gemrunner.CountByID(d, pearlplan.PearlBoltID), fletchingXP(d), d.Session.State().Tick()
	at := time.Now()
	defer func() {
		res.Made = max(0, gemrunner.CountByID(d, pearlplan.PearlBoltID)-made0)
		res.XP = fletchingXP(d) - xp0
		res.Ticks = d.Session.State().Tick() - tick0
	}()
	refused := func() bool {
		if d.Chat == nil {
			return false
		}
		if why, level := pearlplan.Refusal(d.Chat.Since(at)); why != "" {
			res.Reason, res.RefusedLevel = why, level
			return true
		}
		return false
	}
	for {
		tips, bolts := gemrunner.CountByID(d, pearlplan.TipsID), gemrunner.CountByID(d, pearlplan.BoltID)
		uses := pearlplan.BoltUses(tips, bolts)
		if uses == 0 {
			res.Reason = "done"
			return res
		}
		before := gemrunner.CountByID(d, pearlplan.PearlBoltID)
		for sent := 0; sent < uses; {
			if refused() {
				return res
			}
			if hold != nil && !hold() {
				res.Reason = "held"
				return res
			}
			tick := d.Session.State().Tick()
			n, err := eq.UseOnHeldStack(pearlplan.TipsID, pearlplan.BoltID, min(pearlplan.UsesPerTick, uses-sent))
			if n == 0 {
				if d.Log != nil {
					d.Log(fmt.Sprintf("mastercrafter: pearl bolts: tips on bolts: %v", err))
				}
				res.Reason = "stalled"
				return res
			}
			sent += n
			res.Uses += n
			if sent < uses && !gemrunner.NextTick(d, tick) {
				res.Reason = "stopped"
				return res
			}
		}
		// Confirm once: every use applied shows as up to ten more Pearl bolts.
		want := before + min(tips, bolts)
		deadline := time.Now().Add(boltConfirm)
		for gemrunner.CountByID(d, pearlplan.PearlBoltID) < want && time.Now().Before(deadline) {
			if !d.Delay(10 * time.Millisecond) {
				res.Reason = "stopped"
				return res
			}
		}
		if refused() {
			return res
		}
		if gemrunner.CountByID(d, pearlplan.PearlBoltID) <= before {
			res.Reason = "stalled"
			return res
		}
	}
}

// Describe is a one-line account of a MakeBolts for a log.
func (r BoltResult) Describe() string {
	return fmt.Sprintf("%d Pearl bolts made with %d use(s) over %d tick(s), +%d Fletching XP (%s)", r.Made, r.Uses, r.Ticks, r.XP, r.Reason)
}

// PearlProducts is what a pearl round hands the mule: the Pearl bolts.
func PearlProducts() []Product {
	return []Product{{ID: pearlplan.PearlBoltID, Name: pearlplan.PearlBolt}}
}

// PearlLeftovers is what goes back to the mule once a pearl round has no tips left: the Bolts it did
// not need (the mule's stock, whole again for whoever else reads it).
func PearlLeftovers() []Product {
	return []Product{{ID: pearlplan.BoltID, Name: pearlplan.Bolt}}
}
