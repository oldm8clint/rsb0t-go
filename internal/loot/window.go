package loot

import (
	"time"

	"rsb0t-go/internal/cachecfg"
)

// A measurement window for a CONTINUOUS activity, and the rule that keeps what it measures
// honest. Extracted from the fishing recorder so chopping and mining reuse it rather than each
// growing a second mechanism of its own.
//
// # THE PROBLEM IT SOLVES
//
// Measuring a discrete action is easy: the crystal chest snapshots the pack across one OPLOCU
// with a two-second window, and nothing else can land inside it. A continuous activity has no
// such boundary. "Whatever the pack gained while the account was fishing" sounds equivalent and
// is not: every tool re-buy, bank withdrawal and random-event hand-out that happens to arrive
// while the account is fishing lands in it too. Live on 2026-09-20 that put a fishing net and a
// Mysterious Old Man's Strange box on the Loot panel's Fishing spot card.
//
// THE RULE, IN TWO PARTS
//
//  1. A skip set, for things the activity can never have produced: the account's own tools, and
//     an event's own item. Narrow on purpose — the events' REWARD tables overlap real loot (the
//     Old Man gives a Casket and so does the Big net spot, the maze gives feathers and so does a
//     chicken), so skipping every reward id would throw away genuine drops.
//
//  2. Taint, which is the part that generalises. No list of ids can cover every hand-out, reward
//     and recovered item, but refusing to REPORT a window that an interruption ran inside does.
//     A caller marks the window whenever it yields to something that can move the pack for
//     reasons of its own — a random event, a fight, a tool being recovered — and the window is
//     then re-baselined and thrown away rather than attributed to the activity.
//
// Losing a tainted window's real gains is the right trade: an interruption is rare, the next
// window picks up from the pack as it stands, and a missing handful of logs is a far smaller lie
// than a Strange box filed under Woodcutting.
//
// Nothing here predicts a drop table. An item is counted only when it really arrived.
type Window struct {
	src    Source
	cfg    *cachecfg.Config
	sink   func(Drop)
	pack   func() Counts
	skip   func(id int) bool
	now    func() time.Time
	before Counts
	events int
	taint  bool
	last   time.Time
}

// NewWindow opens a measurement window on src.
//
// sink receives the Drops; a nil sink returns a nil *Window and every method below is nil-safe,
// so a script run by a bare test context measures nothing and costs nothing. pack reads the
// account's pack as Counts. skip names ids the activity can never have produced, and may be nil.
func NewWindow(sink func(Drop), cfg *cachecfg.Config, pack func() Counts, src Source, skip func(id int) bool) *Window {
	if sink == nil || pack == nil {
		return nil
	}
	return newWindow(sink, cfg, pack, src, skip, time.Now)
}

func newWindow(sink func(Drop), cfg *cachecfg.Config, pack func() Counts, src Source, skip func(id int) bool, now func() time.Time) *Window {
	return &Window{src: src, cfg: cfg, sink: sink, pack: pack, skip: skip, now: now, before: pack(), last: now()}
}

// Source is the source this window measures, for a caller that wants to report it.
func (w *Window) Source() Source {
	if w == nil {
		return Source{}
	}
	return w.src
}

// Got records n more of whatever this activity counts as one event — a catch, a log, an ore.
func (w *Window) Got(n int) {
	if w != nil && n > 0 {
		w.events += n
	}
}

// Taint marks the window untrustworthy: whatever the pack does between here and the next flush
// did not necessarily come from this activity, so none of it is reported. See the rule above.
func (w *Window) Taint() {
	if w != nil {
		w.taint = true
	}
}

// Due reports whether the window has been open for at least every, so a long uninterrupted
// stretch reports while it runs rather than only when it ends.
func (w *Window) Due(every time.Duration) bool {
	return w != nil && w.now().Sub(w.last) >= every
}

// Flush reports what the window gained and reopens it from the pack as it stands now, so
// nothing is counted twice and nothing between flushes is lost. A tainted window is re-baselined
// and dropped.
func (w *Window) Flush() {
	if w == nil {
		return
	}
	after := w.pack()
	before := w.before
	w.before, w.last = after, w.now()
	w.report(Gained(before, after, nil))
}

// Done flushes and returns n unchanged, for a caller that ends its loop by returning.
func (w *Window) Done(n int) int {
	w.Flush()
	return n
}

// report applies the window's rules to one raw pack delta and hands what survives to the sink,
// then closes the window. Separate from Flush so the rules can be tested without a live session.
func (w *Window) report(gained Counts) {
	taint, events := w.taint, w.events
	w.taint, w.events = false, 0
	if taint {
		return
	}
	keep := Counts{}
	for id, n := range gained {
		if n > 0 && (w.skip == nil || !w.skip(id)) {
			keep[id] = n
		}
	}
	if len(keep) == 0 && events == 0 {
		return
	}
	w.sink(Drop{Source: w.src, Events: events, Items: Items(w.cfg, keep)})
}
