package actions

import (
	"fmt"
	"time"

	"rsb0t-go/internal/gamestate"
)

// Shop.buy/buyById/sell returning how many actually changed hands, which can be less than
// asked for (stock runs out, coins run out, the pack fills, the shop stops buying). Ported from
// upstream's Shop.ts: each burst clicks up to userOpsPerTick 10/5/1 options (the engine reads five
// user events a tick and leaves the rest queued, in order, for the next), a pass is as many bursts as the
// quantity needs, one a tick (see transact), and the difference is counted on the pack once it has settled.
//
// Buy and Sell in shop.go stay as they are for their existing callers.

// userOpsPerTick is Shop.ts's USER_OPS_PER_TICK.
const userOpsPerTick = 5

// shopConfirmTimeout is how long a round waits for the pack to change, as upstream.
const shopConfirmTimeout = 3 * time.Second

// batchOptions ports shopOpBatch: up to userOpsPerTick options from steps (largest first)
// whose quantities add up to no more than remaining.
func batchOptions(steps []struct {
	qty    int32
	option int
}, remaining int32) []int {
	var batch []int
	left := remaining
	for len(batch) < userOpsPerTick && left > 0 {
		picked := false
		for _, step := range steps {
			if step.qty <= left {
				batch = append(batch, step.option)
				left -= step.qty
				picked = true
				break
			}
		}
		if !picked {
			break
		}
	}
	return batch
}

// BuyCount buys up to n of objID and returns how many landed in the pack. It returns an error
// only when the shop is not open, objID is not in stock at all, or a click fails to send.
func (sh *Shop) BuyCount(objID, n int32, w Waiter) (int32, error) {
	return sh.transact(objID, n, ShopStock, shopSteps, true, w)
}

// SellCount sells up to n of objID from the pack and returns how many left it.
func (sh *Shop) SellCount(objID, n int32, w Waiter) (int32, error) {
	return sh.transact(objID, n, ShopPack, shopSellSteps, false, w)
}

// shopSettleTicks is how many server ticks after the last burst every result has certainly arrived (read in the
// next tick, flushed with it, one spare).
const shopSettleTicks = 2

// shopQuiet is how many consecutive looks between bursts may find the pack unchanged before the pass stops sending: a
// burst's result shows a tick after it, so the look before the next burst often misses it, but two quiet looks in a row
// mean nothing is landing (the coins or the stock ran out, the pack is full, the shop will not buy).
const shopQuiet = 2

// qtyOfOption is the quantity a click of that option moves.
func qtyOfOption(steps []struct {
	qty    int32
	option int
}, option int) int32 {
	for _, s := range steps {
		if s.option == option {
			return s.qty
		}
	}
	return 1
}

// batchPlan is batchOptions with the quantity the batch adds up to.
func batchPlan(steps []struct {
	qty    int32
	option int
}, remaining int32) (batch []int, qty int32) {
	left := remaining
	for len(batch) < userOpsPerTick && left > 0 {
		picked := false
		for _, step := range steps {
			if step.qty <= left {
				batch = append(batch, step.option)
				left -= step.qty
				qty += step.qty
				picked = true
				break
			}
		}
		if !picked {
			break
		}
	}
	return batch, qty
}

// transact buys or sells up to n of objID and returns how many changed hands, measured on the pack.
//
// FIVE CLICKS EVERY TICK (twopertick-1012). A round used to be five clicks, a wait for the pack to change (a tick
// or more), one more tick to settle, and only then the next five: about two and a half ticks per fifty runes. A
// shop click has no delay and no dialog (content shop/scripts/shop.rs2 [label,buy_item] and the sell twin run to
// the end inside the handler), so the next five can go in the very next tick: this sends the planned quantity as
// bursts of five, one a game tick (the next burst waits for a tick to pass or for the last one's result to show),
// and measures the whole pass on the pack afterwards. A click that bought less than its quantity (the stock ran
// low, the coins ran short) is made good by the next pass, which plans the shortfall again as before; a pass in
// which nothing moved ends the call. Planned quantity is capped at what the stock (or the pack, selling) shows,
// and the burst count is bounded by what was asked, so a pipelined pass can only send clicks the old rounds
// would have sent, sooner.
func (sh *Shop) transact(objID, n int32, comID int, steps []struct {
	qty    int32
	option int
}, buying bool, w Waiter) (int32, error) {
	w = waiterOr(w)
	started := time.Now()
	sh.last = shopPass{buying: buying, asked: n}
	defer func() { sh.last.took = time.Since(started) }()
	if !sh.IsOpen() {
		return 0, ErrNotOnScreen
	}
	st := sh.s.State()
	var done int32
	for done < n && sh.IsOpen() {
		slot, count, found := slotOf(items(st, comID), objID)
		if !found {
			if done == 0 {
				return 0, ErrItemNotFound
			}
			break
		}
		if count == 0 {
			break
		}
		// What this pass plans: what is left to do, no more than the shelf holds when buying (the stock row carries
		// the whole count; a pack row of an unstackable item carries one, so selling is not capped).
		want := n - done
		if buying && count < want {
			want = count
		}
		before := sumCount(sh.Pack(), objID)
		// SELLING NAMES A DIFFERENT SLOT EACH CLICK. A shop sale removes the object from the LOWEST slots holding it, so
		// with an unstackable item in many slots the first Sell-10 empties ten of them and a second click naming the
		// first slot again is refused (the engine's inv.hasAt): click i names the slot the cumulative quantity before it
		// leaves at the front. A stack is one slot and every click names it.
		var sellSlots []int
		if !buying {
			for _, it := range items(st, comID) {
				if it.ID == objID {
					sellSlots = append(sellSlots, it.Slot)
				}
			}
			if before < want {
				want = before // nothing more than the pack holds
			}
		}
		var planned int32
		bursts, lastTick := 0, st.Tick()
		atSend, prevSeen, quiet := before, before, 0
		stopped, exhausted := false, false
		for planned < want && sh.IsOpen() {
			if bursts > 0 {
				// The previous burst has been read once a tick has passed or its result shows.
				deadline := time.Now().Add(fastChunkWait)
				for st.Tick() <= lastTick && sumCount(sh.Pack(), objID) == atSend && time.Now().Before(deadline) {
					if !w.Delay(fastPoll) {
						stopped = true
						break
					}
				}
				if stopped {
					break
				}
				if seen := sumCount(sh.Pack(), objID); seen == prevSeen {
					quiet++
				} else {
					quiet, prevSeen = 0, seen
				}
				if quiet >= shopQuiet {
					exhausted = true
					break // nothing has moved across two looks: nothing more will
				}
				if buying {
					if s2, c2, ok := slotOf(items(st, comID), objID); ok && c2 > 0 {
						slot = s2
					} else {
						break
					}
				}
			}
			batch, qty := batchPlan(steps, want-planned)
			if len(batch) == 0 {
				break
			}
			lastTick, atSend = st.Tick(), sumCount(sh.Pack(), objID)
			sentBefore := planned
			for _, option := range batch {
				clickSlot := slot
				if !buying && len(sellSlots) > 0 {
					idx := int(sentBefore)
					if idx >= len(sellSlots) {
						idx = len(sellSlots) - 1
					}
					if len(sellSlots) == 1 {
						idx = 0
					}
					clickSlot = sellSlots[idx]
					sentBefore += qtyOfOption(steps, option)
				}
				if err := InvButton(sh.s, option, objID, clickSlot, comID); err != nil {
					return done, err
				}
			}
			planned += qty
			bursts++
			sh.last.clicks += len(batch)
			sh.last.bursts++
		}
		if bursts == 0 {
			break
		}
		// Every result has arrived once the pack shows the planned total, or the ticks after the last burst
		// have passed; then the old single settle tick.
		expected := before + planned
		if !buying {
			expected = before - planned
		}
		w.DelayUntil(func() bool {
			return sumCount(sh.Pack(), objID) == expected || st.Tick() >= lastTick+shopSettleTicks
		}, shopConfirmTimeout)
		settled := w.DelayTicks(1)
		after := sumCount(sh.Pack(), objID)
		moved := after - before
		if !buying {
			moved = before - after
		}
		if moved <= 0 {
			break
		}
		done += moved
		sh.last.moved = done
		if !settled || stopped || exhausted {
			break // an exhausted pass planned the shortfall again would only repeat itself
		}
	}
	sh.last.moved = done
	return done, nil
}

// shopPass is what the last BuyCount or SellCount did.
type shopPass struct {
	buying       bool
	asked, moved int32
	clicks       int
	bursts       int
	took         time.Duration
}

// LastPass is the one-line account of the last BuyCount or SellCount for a script's log: how many clicks went out
// in how many ticks, and how many items moved.
func (sh *Shop) LastPass() string {
	verb := "sold"
	if sh.last.buying {
		verb = "bought"
	}
	return fmt.Sprintf("shop: %s %d of %d asked: %d click(s) in %d tick(s) of at most %d, %s", verb, sh.last.moved, sh.last.asked,
		sh.last.clicks, sh.last.bursts, userOpsPerTick, sh.last.took.Round(time.Millisecond))
}

func slotOf(items []gamestate.SlotItem, objID int32) (slot int, count int32, found bool) {
	for _, it := range items {
		if it.ID == objID {
			return it.Slot, it.Count, true
		}
	}
	return 0, 0, false
}
