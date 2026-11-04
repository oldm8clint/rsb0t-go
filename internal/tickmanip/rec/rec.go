// Package rec records, tick by tick, what a client sees of one account: the tile, the animation the server started,
// the game messages, the experience and inventory changes, the run energy, and the packets the client itself wrote.
//
// It exists to make "which server tick did this happen on" an answered question rather than a guess. The engine
// writes a tick's messages before that tick's PLAYER_INFO and its inventory and stat updates after it, so a recorder
// that credited everything to "the tick count when it arrived" would put a log's message one tick before the log
// (tickmanip.ServerTick explains the rule, and cmd/tickmanipprobe's calibration arm measures it against the engine
// rather than trusting it).
//
// The recorder hangs off gamestate.State.Subscribe, so it runs on the read loop's goroutine; every method that
// the arm runner calls from another goroutine takes the mutex.
package rec

import (
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/tickmanip"
)

// Source is the part of gamestate.State a Recorder reads; *gamestate.State satisfies it, and a test supplies a fake
// so the attribution can be checked against a scripted packet sequence.
type Source interface {
	Tick() int
	Tile() (x, z, level int, ok bool)
	SelfAnim() (seq int, tick int)
	Energy() (energy int, ok bool)
	Modals() (main, side, chat int)
	MapFlagUnsetCount() int
}

// Recorder collects Rows for one state.
type Recorder struct {
	st Source

	mu   sync.Mutex
	rows map[int]*tickmanip.Row
	last int // the highest tick ordinal seen
	// unset is the UNSET_MAP_FLAG count at the previous PLAYER_INFO.
	unset int

	// waiters are closed when their tick arrives.
	waiters map[int][]chan struct{}

	// keep is how many ticks of history to retain (0 keeps everything; arms are short).
	keep int

	cancel func()
}

// New returns a Recorder over st and starts it. Stop removes it.
func New(st *gamestate.State) *Recorder {
	r := NewFrom(st)
	r.cancel = st.Subscribe(r.onEvent)
	return r
}

// NewFrom returns a Recorder over any Source without subscribing it: the caller feeds onEvent-equivalent events
// through Feed. Used by tests.
func NewFrom(src Source) *Recorder {
	r := &Recorder{st: src, rows: map[int]*tickmanip.Row{}, waiters: map[int][]chan struct{}{}}
	r.last = src.Tick()
	r.unset = src.MapFlagUnsetCount()
	return r
}

// Feed hands the recorder one event, as the state's subscription would.
func (r *Recorder) Feed(e gamestate.Event) { r.onEvent(e) }

// Stop detaches the recorder from the state.
func (r *Recorder) Stop() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// Last is the highest PLAYER_INFO ordinal the recorder has seen.
func (r *Recorder) Last() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func (r *Recorder) row(n int) *tickmanip.Row {
	if row, ok := r.rows[n]; ok {
		return row
	}
	row := tickmanip.NewRow(n)
	r.rows[n] = row
	return row
}

// Note files a client-side note under the row of tick n (a packet the client wrote for that tick). n is the tick the
// server will read it in: one past the last PLAYER_INFO the client had seen when it wrote.
func (r *Recorder) Note(n int, what string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.row(n)
	row.Sent = append(row.Sent, what)
}

// NextReadTick is the server tick a packet written right now will be read in.
func (r *Recorder) NextReadTick() int {
	return r.Last() + 1
}

// WaitFor blocks until the recorder has seen tick n (PLAYER_INFO ordinal), or the timeout passes. It reports whether
// the tick arrived. Everything the engine flushed after that PLAYER_INFO in the same cycle may still be in flight,
// which is why callers that decide on inventory or stat changes also Settle.
func (r *Recorder) WaitFor(n int, timeout time.Duration) bool {
	r.mu.Lock()
	if r.last >= n {
		r.mu.Unlock()
		return true
	}
	ch := make(chan struct{})
	r.waiters[n] = append(r.waiters[n], ch)
	r.mu.Unlock()
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Snapshot copies the rows of ticks [from, to] into a RowSet whose arm tick 0 is server tick base.
func (r *Recorder) Snapshot(base, from, to int) *tickmanip.RowSet {
	r.mu.Lock()
	defer r.mu.Unlock()
	set := &tickmanip.RowSet{Base: base, Rows: map[int]*tickmanip.Row{}}
	for n := from; n <= to; n++ {
		if row, ok := r.rows[n]; ok {
			set.Rows[n] = cloneRow(row)
		}
	}
	return set
}

func cloneRow(src *tickmanip.Row) *tickmanip.Row {
	dst := *src
	dst.Msgs = append([]string(nil), src.Msgs...)
	dst.Sent = append([]string(nil), src.Sent...)
	dst.Inv = append([]tickmanip.InvDelta(nil), src.Inv...)
	if src.XP != nil {
		dst.XP = make(map[int]int, len(src.XP))
		for k, v := range src.XP {
			dst.XP[k] = v
		}
	}
	return &dst
}

// RowAt returns a copy of tick n's row (an empty row when none was recorded).
func (r *Recorder) RowAt(n int) tickmanip.Row {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row, ok := r.rows[n]; ok {
		return *cloneRow(row)
	}
	return *tickmanip.NewRow(n)
}

func (r *Recorder) onEvent(e gamestate.Event) {
	switch e.Kind {
	case gamestate.EventTick:
		r.onTick(e.Tick)
	case gamestate.EventChatMessage:
		if e.ChatType != gamestate.ChatGame {
			return
		}
		seen := r.st.Tick()
		r.mu.Lock()
		row := r.row(tickmanip.ServerTick(tickmanip.Processing, seen))
		row.Msgs = append(row.Msgs, e.Text)
		r.mu.Unlock()
	case gamestate.EventSkillXP:
		seen := r.st.Tick()
		r.mu.Lock()
		row := r.row(tickmanip.ServerTick(tickmanip.Flush, seen))
		if row.XP == nil {
			row.XP = map[int]int{}
		}
		row.XP[e.Skill] += e.XPDelta
		r.mu.Unlock()
	case gamestate.EventInventoryChanged:
		seen := r.st.Tick()
		r.mu.Lock()
		row := r.row(tickmanip.ServerTick(tickmanip.Flush, seen))
		row.Inv = append(row.Inv, tickmanip.InvDelta{
			Comp: e.Comp, Slot: e.Slot,
			ID: e.ItemID, PrevID: e.PreviousID, Count: e.Count, PrevCount: e.PreviousCount,
		})
		r.mu.Unlock()
	}
}

// onTick runs when PLAYER_INFO n has been applied: the position and animation it carried are tick n's, and the run
// energy the client now holds is the one tick n-1's flush set (tick n's own UPDATE_RUNENERGY has not arrived yet).
func (r *Recorder) onTick(n int) {
	x, z, _, ok := r.st.Tile()
	seq, animTick := r.st.SelfAnim()
	energy, haveEnergy := r.st.Energy()
	main, _, chat := r.st.Modals()
	now := time.Now()

	r.mu.Lock()
	row := r.row(n)
	row.At = now
	row.X, row.Z, row.HaveTile = x, z, ok
	if prev0, have := r.rows[n-1]; have && prev0.HaveTile && ok {
		dx, dz := x-prev0.X, z-prev0.Z
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		if dz > dx {
			dx = dz
		}
		if dx > 2 {
			dx = 2
		}
		row.Steps = dx
	}
	u := r.st.MapFlagUnsetCount()
	row.MapUnset = u - r.unset
	r.unset = u
	if animTick == n {
		row.Anim = seq
	}
	// What the client holds now is what tick n-1's flush left: its run energy and its interfaces (encodeOut writes the
	// modal opens and closes last, after PLAYER_INFO, so they too belong to the tick before this PLAYER_INFO).
	prev := r.row(n - 1)
	prev.Modal = main != gamestate.NoModal || chat != gamestate.NoModal
	if haveEnergy {
		prev.Energy = energy
	}
	if n > r.last {
		r.last = n
	}
	waiters := r.waiters[n]
	delete(r.waiters, n)
	// A waiter for an earlier tick that was never delivered (the tick was skipped) is released too.
	for k, ws := range r.waiters {
		if k < n {
			waiters = append(waiters, ws...)
			delete(r.waiters, k)
		}
	}
	if r.keep > 0 {
		for old := range r.rows {
			if old < n-r.keep {
				delete(r.rows, old)
			}
		}
	}
	r.mu.Unlock()
	for _, ch := range waiters {
		close(ch)
	}
}
