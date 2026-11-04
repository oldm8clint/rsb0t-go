package mulereceiverplan

import (
	"sort"
	"time"
)

// QueuePosition is key's 1-based place in a mule's hand-off queue, where announced maps each
// waiting donor to when it announced itself: every donor that announced earlier is ahead of it,
// ties broken by name so two mules (or two calls) always agree. A key not in the queue is placed
// at the back, behind everybody.
//
// This is the order mulerunner serves visible requesters in (its Deps.Priority), which is what
// makes a quote built on it a turn time. Before 2026-09-23 the mule served whichever trade-request
// line it read first, so a queue position predicted nothing: live 263cceda, the donors served from
// one crush had announce ranks 6, 2, 15, 30, 23, 21, 38 and 48 while ranks 1, 3, 4, 5, 7, 8, 9 and
// 10 all gave up.
func QueuePosition(announced map[string]time.Time, key string) int {
	at, ok := announced[key]
	if !ok {
		return len(announced) + 1
	}
	pos := 1
	for other, t := range announced {
		if other == key {
			continue
		}
		if t.Before(at) || (t.Equal(at) && other < key) {
			pos++
		}
	}
	return pos
}

// QueueOrder is the announced donors in the order they will be served.
func QueueOrder(announced map[string]time.Time) []string {
	out := make([]string, 0, len(announced))
	for k := range announced {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := announced[out[i]], announced[out[j]]
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return out[i] < out[j]
	})
	return out
}

// QueueETA is how long the donor at position should expect to wait for its trade to finish: one
// hand-off cycle for itself and each one ahead of it. Position below 1 is treated as 1.
func QueueETA(position int, cycle time.Duration) time.Duration {
	if position < 1 {
		position = 1
	}
	return time.Duration(position) * cycle
}
