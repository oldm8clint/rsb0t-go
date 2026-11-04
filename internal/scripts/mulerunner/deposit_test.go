package mulerunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

func slots(pairs ...int32) []gamestate.SlotItem {
	out := make([]gamestate.SlotItem, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		// Item is embedded, and a promoted-field literal needs go1.27 while go.mod sets the
		// language to go1.23 — so the inner struct is named explicitly.
		out = append(out, gamestate.SlotItem{Slot: i / 2, Item: gamestate.Item{ID: pairs[i], Count: pairs[i+1]}})
	}
	return out
}

// The coins case is the live one: a mule banked +1000 coins and still logged "deposit id=995:
// no confirmed inventory/bank change" and "banked 1 of 2" (COLLAB.md, 2026-09-20). Those counts
// feed the control panel and the run statistics, so a false negative there misreports work that
// actually happened.
func TestDepositLanded(t *testing.T) {
	const coins, logs, notedLogs = int32(995), int32(1511), int32(1512)

	for _, tc := range []struct {
		name           string
		inv, bankItems []gamestate.SlotItem
		id, bankID     int32
		held, before   int64
		want           bool
	}{
		{
			// The regression. The bank has the coins; the deposit-side panel has not caught
			// up. The old rule required both and called this a failed deposit.
			name:      "the bank gained them while the pack view still lags",
			inv:       slots(coins, 1000),
			bankItems: slots(logs, 40, coins, 1000),
			id:        coins, bankID: coins, held: 1000, before: 0,
			want: true,
		},
		{
			name:      "the bank gained them on top of what it already held",
			inv:       nil,
			bankItems: slots(coins, 3500),
			id:        coins, bankID: coins, held: 1000, before: 2500,
			want: true,
		},
		{
			// The weaker signal: the bank count has not arrived, but the stack has left the
			// pack and the bank view is readable, so the deposit is in flight and nothing
			// else could have taken it.
			name:      "the pack cleared while the bank count has not arrived",
			inv:       slots(logs, 12),
			bankItems: slots(logs, 40),
			id:        coins, bankID: coins, held: 1000, before: 0,
			want: true,
		},
		{
			// An empty bank view is a reading that has not landed, not evidence. Trusting a
			// cleared pack against it would confirm a deposit from two unread panels.
			name:      "nothing is readable yet",
			inv:       nil,
			bankItems: nil,
			id:        coins, bankID: coins, held: 1000, before: 0,
			want: false,
		},
		{
			name:      "nothing moved at all",
			inv:       slots(coins, 1000),
			bankItems: slots(logs, 40),
			id:        coins, bankID: coins, held: 1000, before: 0,
			want: false,
		},
		{
			name:      "only part of the stack reached the bank",
			inv:       slots(coins, 400),
			bankItems: slots(coins, 600),
			id:        coins, bankID: coins, held: 1000, before: 0,
			want: false,
		},
		{
			// A noted stack banks under its unnoted id, so the two ids differ and the pack is
			// checked against the noted one.
			name:      "a noted stack banks under its unnoted id",
			inv:       slots(notedLogs, 27),
			bankItems: slots(logs, 27),
			id:        notedLogs, bankID: logs, held: 27, before: 0,
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := depositLanded(tc.inv, tc.bankItems, tc.id, tc.bankID, tc.held, tc.before); got != tc.want {
				t.Fatalf("depositLanded = %v, want %v", got, tc.want)
			}
		})
	}
}
