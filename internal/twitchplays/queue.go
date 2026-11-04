package twitchplays

import "time"

// Queue is a bounded FIFO command queue. The game runtime should consume only
// validated commands; producers may be YouTube Live, Twitch, a local console,
// or tests.
type Queue struct {
	items []Command
	max   int
}

func NewQueue(max int) *Queue {
	if max < 1 {
		max = 1
	}
	return &Queue{max: max}
}

func (q *Queue) Push(c Command) bool {
	if !Validate(c) {
		return false
	}
	if c.Received.IsZero() {
		c.Received = time.Now()
	}
	if len(q.items) >= q.max {
		return false
	}
	q.items = append(q.items, c)
	return true
}

func (q *Queue) Pop() (Command, bool) {
	if len(q.items) == 0 {
		return Command{}, false
	}
	c := q.items[0]
	q.items = q.items[1:]
	return c, true
}

func (q *Queue) Len() int { return len(q.items) }
