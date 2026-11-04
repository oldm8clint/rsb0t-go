package mastercraftrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// The herb round's I/O (mastercraftplan/herbs.go has the content facts).

// HerbCounts is a bank reading's grimy herbs by obj id.
func HerbCounts(banked []gamestate.SlotItem) map[int32]int {
	out := map[int32]int{}
	for _, h := range herbplan.Herbs {
		if n := int(actions.CountByID(banked, h.UnidID)); n > 0 {
			out[h.UnidID] = n
		}
	}
	return out
}

// HerbDraw is the next load of a herb round: Withdraw-All of the first grimy herb of herbs (lowest
// level first) the bank holds.
func HerbDraw(herbs []herbplan.Herb) func(bank []gamestate.SlotItem) []int32 {
	return func(bank []gamestate.SlotItem) []int32 {
		for _, h := range herbs {
			if actions.CountByID(bank, h.UnidID) > 0 {
				return []int32{h.UnidID}
			}
		}
		return nil
	}
}

// HerbProducts are the clean herbs a herb round hands the mule.
//
// The Jungle Potion herbs are left out (mastercraftplan.HerbTradeable): they cannot be traded, so a
// hand-over that drew one could only fail. Live on 0741287b (5 Oct 2026) the main account's bank held 17 clean
// Rogue's purse, every deliver round drew them, nothing crossed, and the round backed off for an hour.
func HerbProducts() []Product {
	out := make([]Product, 0, len(herbplan.Herbs))
	for _, h := range herbplan.Herbs {
		if mastercraftplan.HerbTradeable(h) {
			out = append(out, Product{ID: h.CleanID, Name: h.Name})
		}
	}
	return out
}

// CleanResult is one CleanAll.
type CleanResult struct {
	Herb herbplan.Herb
	// Cleaned is the grimy herbs that left the pack, Sent the clicks sent, Bursts and Ticks the
	// bursts and the server ticks the run spanned.
	Cleaned, Sent, Bursts, Ticks int
	// Reason is "done", "refused", "members", "stalled", "partial" or "stopped".
	Reason string
}

// Describe is a one-line account of a CleanAll for a log.
func (r CleanResult) Describe() string {
	return fmt.Sprintf("%s: %d cleaned with %d click(s) in %d burst(s) over %d tick(s) (%s)", r.Herb.Name, r.Cleaned, r.Sent, r.Bursts, r.Ticks, r.Reason)
}

// cleanConfirm is how long the pack has, after the last burst, to show every herb cleaned.
const cleanConfirm = 2500 * time.Millisecond

// identifyOp is the herb's Identify option (iop1 on every grimy herb in content).
func identifyOp(d *fleetrunner.Deps, id int32) int {
	if d.Config != nil {
		if o, ok := d.Config.Obj(int(id)); ok {
			if n := actions.OptionIndex(actions.HeldOps(o.IOps), herbplan.IdentifyOp); n != 0 {
				return n
			}
		}
	}
	return 1
}

// CleanAll cleans every grimy h in the pack: five Identify clicks a server tick, each at its own
// slot (the clean herb takes the grimy one's slot: inv_setslot), every burst sent when the client
// has seen the tick before it (gemrunner.NextTick), with one confirm at the end. Cleaning has no
// delay in content, so this is the gem round's pace; nothing is streamed ahead of the server.
// afterLast, when set, runs right behind the last burst (the next bank open).
func CleanAll(d *fleetrunner.Deps, h herbplan.Herb, afterLast func()) (res CleanResult) {
	res.Herb = h
	gd := GemDeps(d)
	st := d.Session.State()
	start := packCount(d, h.UnidID)
	if start == 0 {
		res.Reason = "done"
		return res
	}
	op := identifyOp(d, h.UnidID)
	at, tick0 := time.Now(), st.Tick()
	defer func() {
		res.Cleaned = max(0, start-packCount(d, h.UnidID))
		res.Ticks = st.Tick() - tick0
	}()
	refused := func() bool {
		if d.Chat == nil {
			return false
		}
		said := d.Chat.Since(at)
		switch {
		case herbplan.IsMembersRefusal(said):
			res.Reason = "members"
		case herbplan.IsLevelRefusal(said):
			res.Reason = "refused"
		default:
			return false
		}
		return true
	}
	named := map[int]bool{}
	for {
		if refused() {
			return res
		}
		tick := st.Tick()
		sent := 0
		for _, it := range packItems(d) {
			if sent >= herbplan.BurstSize {
				break
			}
			if it.ID != h.UnidID || named[it.Slot] {
				continue
			}
			if err := actions.OpHeld(d.Session, op, it.ID, it.Slot, fleetrunner.PlayerPack); err != nil {
				logf(d, "mastercrafter: herbs: Identify slot %d: %v", it.Slot, err)
				break
			}
			named[it.Slot] = true
			sent++
		}
		if sent == 0 {
			if res.Sent == 0 {
				res.Reason = "stalled"
				return res
			}
			break
		}
		res.Sent += sent
		res.Bursts++
		left := 0
		for _, it := range packItems(d) {
			if it.ID == h.UnidID && !named[it.Slot] {
				left++
			}
		}
		if left == 0 {
			break
		}
		if !gemrunner.NextTick(gd, tick) {
			res.Reason = "stopped"
			return res
		}
	}
	if afterLast != nil {
		afterLast()
	}
	want := max(0, start-res.Sent)
	deadline := time.Now().Add(cleanConfirm)
	for packCount(d, h.UnidID) > want && time.Now().Before(deadline) {
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
	switch left := packCount(d, h.UnidID); {
	case left == 0:
		res.Reason = "done"
	case left >= start:
		res.Reason = "stalled"
	default:
		res.Reason = "partial"
	}
	return res
}
