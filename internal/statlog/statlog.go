// Package statlog records positive inventory deltas for later dashboard/statistics use.
// It deliberately depends only on gamestate and a caller-supplied item-name resolver;
// fleet/control-panel wiring can consume the resulting JSONL independently.
package statlog

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
)

type ItemName func(id int32) string

type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Account   string    `json:"account"`
	Inventory int       `json:"inventory"`
	Slot      int       `json:"slot"`
	ItemID    int32     `json:"item_id"`
	ItemName  string    `json:"item_name,omitempty"`
	Delta     int32     `json:"delta"`
}

type Logger struct {
	mu       sync.Mutex
	account  string
	resolve  ItemName
	previous map[int][]gamestate.SlotItem
	out      *bufio.Writer
	enc      *json.Encoder
}

func New(account string, out io.Writer, resolve ItemName) *Logger {
	w := bufio.NewWriter(out)
	return &Logger{account: account, resolve: resolve, previous: make(map[int][]gamestate.SlotItem), out: w, enc: json.NewEncoder(w)}
}

// Observe establishes a baseline on the first call, then records only positive
// quantity deltas for unchanged item IDs in each slot. Empty slots and removals
// never generate gain events.
func (l *Logger) Observe(inventories map[int][]gamestate.SlotItem, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	first := len(l.previous) == 0
	for invID, slots := range inventories {
		old := l.previous[invID]
		if !first {
			for _, cur := range slots {
				if cur.Slot < 0 || cur.Slot >= len(old) || old[cur.Slot].ID != cur.ID {
					continue
				}
				delta := cur.Item.Count - old[cur.Slot].Count
				if delta <= 0 || cur.ID == 0 {
					continue
				}
				e := Event{Timestamp: now.UTC(), Account: l.account, Inventory: invID, Slot: cur.Slot, ItemID: cur.ID, Delta: delta}
				if l.resolve != nil {
					e.ItemName = l.resolve(cur.ID)
				}
				if err := l.enc.Encode(e); err != nil {
					return err
				}
			}
		}
		l.previous[invID] = append([]gamestate.SlotItem(nil), slots...)
	}
	return l.out.Flush()
}

func (l *Logger) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.out.Flush()
}
