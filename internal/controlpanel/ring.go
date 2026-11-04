package controlpanel

import (
	"encoding/json"
	"unicode/utf8"
)

// ring is a fixed-size FIFO that overwrites its oldest entry. It is allocated once at its full size.
type ring[T any] struct {
	buf  []T
	head int
	n    int
}

func newRing[T any](size int) *ring[T] {
	if size < 1 {
		size = 1
	}
	return &ring[T]{buf: make([]T, size)}
}

func (r *ring[T]) push(v T) {
	if r.n < len(r.buf) {
		r.buf[(r.head+r.n)%len(r.buf)] = v
		r.n++
		return
	}
	r.buf[r.head] = v
	r.head = (r.head + 1) % len(r.buf)
}

func (r *ring[T]) size() int { return r.n }

// each visits entries oldest first.
func (r *ring[T]) each(fn func(T)) {
	for i := 0; i < r.n; i++ {
		fn(r.buf[(r.head+i)%len(r.buf)])
	}
}

// cut returns at most n bytes of s, ending on a rune boundary. The result may share s's memory, so
// use it only for values encoded straight away.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// cutOwned is cut for values the dashboard keeps.
// Why: a kept substring pins the whole string it came from, which would unbound the event ring.
func cutOwned(s string, n int) string {
	if len(s) <= n {
		return s
	}
	b := make([]byte, len(cut(s, n)))
	copy(b, s)
	return string(b)
}

// clampSettings bounds an AccountStatus.Settings map before it goes into a status body:
// nil straight through, and a map that marshals over maxAccountSettingsBytes dropped
// entirely (never truncated key-by-key — a partial settings map would be actively
// misleading for the "duplicate this account's settings" feature it exists to support).
func clampSettings(m map[string]interface{}) map[string]interface{} {
	if len(m) == 0 {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil || len(data) > maxAccountSettingsBytes {
		return nil
	}
	return m
}

func knownLevel(l Level) Level {
	switch l {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
		return l
	}
	return LevelInfo
}

// clampEvent bounds an event before it is kept.
func clampEvent(ev Event, nowMs int64) Event {
	ev.Seq = 0
	if ev.TimeUnixMs <= 0 {
		ev.TimeUnixMs = nowMs
	}
	ev.Account = cutOwned(ev.Account, maxNameBytes)
	ev.Kind = cutOwned(ev.Kind, maxKindBytes)
	ev.Text = cutOwned(ev.Text, maxTextBytes)
	ev.Level = knownLevel(ev.Level)
	return ev
}

// clampAccount bounds an account for one status body. Nothing it returns is kept.
func clampAccount(a AccountStatus) AccountStatus {
	a.Name = cut(a.Name, maxNameBytes)
	a.State = State(cut(string(a.State), maxKindBytes))
	a.Bot = cut(a.Bot, maxBotBytes)
	a.Activity = cut(a.Activity, maxActivityBytes)
	a.LastError = cut(a.LastError, maxErrorBytes)
	a.BotState = cut(a.BotState, maxBotStateBytes)
	a.BotError = cut(a.BotError, maxErrorBytes)
	a.AssignedScript = cut(a.AssignedScript, maxBotBytes)
	a.RosterScript = cut(a.RosterScript, maxBotBytes)
	a.Settings = clampSettings(a.Settings)
	counters := a.Counters
	if len(counters) > MaxCounters {
		counters = counters[:MaxCounters]
	}
	out := make([]Counter, len(counters))
	for i, c := range counters {
		out[i] = Counter{Name: cut(c.Name, maxCounterNameBytes), Value: c.Value}
	}
	a.Counters = out
	return a
}

func clampInventories(list []Inventory) []Inventory {
	if len(list) > MaxInventories {
		list = list[:MaxInventories]
	}
	out := make([]Inventory, len(list))
	for i, inv := range list {
		slots := inv.Slots
		if len(slots) > MaxInventorySlots {
			slots = slots[:MaxInventorySlots]
		}
		if slots == nil {
			slots = []Slot{}
		}
		out[i] = Inventory{ID: inv.ID, Slots: slots}
	}
	return out
}

// clampSlots bounds one /api/bank answer's item list, the same MaxInventorySlots cap
// clampInventories applies per-component (a bank can genuinely hold hundreds of stacks,
// same order of magnitude as the largest single inventory component).
func clampSlots(slots []Slot) []Slot {
	if len(slots) > MaxInventorySlots {
		slots = slots[:MaxInventorySlots]
	}
	if slots == nil {
		slots = []Slot{}
	}
	return slots
}

func clampLogins(l LoginStatus) LoginStatus {
	queue := l.Queue
	if len(queue) > MaxAccounts {
		queue = queue[:MaxAccounts]
	}
	out := make([]string, len(queue))
	for i, name := range queue {
		out[i] = cut(name, maxNameBytes)
	}
	l.Queue = out
	for i := range l.Routes {
		l.Routes[i].LastError = cut(l.Routes[i].LastError, maxErrorBytes)
	}
	return l
}
