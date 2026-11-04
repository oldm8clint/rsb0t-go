package controlpanel

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	errTooManyStreams = errors.New("controlpanel: too many open streams")
	errClosed         = errors.New("controlpanel: shutting down")
)

const kindEvents = "event"

type MemSample struct {
	T int64 `json:"t"`
	MemStats
}

type statusBody struct {
	Accounts  []AccountStatus `json:"accounts"`
	Truncated int             `json:"truncated"`
	Counts    map[State]int   `json:"counts"`
	Logins    LoginStatus     `json:"logins"`
}

func encodeStatus(accounts []AccountStatus, logins LoginStatus) []byte {
	body := statusBody{Counts: make(map[State]int, len(States))}
	for _, s := range States {
		body.Counts[s] = 0
	}
	shown := accounts
	if len(shown) > MaxAccounts {
		body.Truncated = len(shown) - MaxAccounts
		shown = shown[:MaxAccounts]
	}
	body.Accounts = make([]AccountStatus, len(shown))
	for i := range shown {
		body.Accounts[i] = clampAccount(shown[i])
	}
	for i := range accounts {
		body.Counts[State(cut(string(accounts[i].State), maxKindBytes))]++
	}
	body.Logins = clampLogins(logins)
	data, err := json.Marshal(body)
	if err != nil {
		return []byte(`{"accounts":[],"truncated":0,"counts":{},"logins":{"queue":[]}}`)
	}
	return data
}

type stream struct {
	queue   chan []byte
	wake    chan struct{}
	dropped uint64
}

func (s *stream) offer(frame []byte) {
	select {
	case s.queue <- frame:
	default:
		atomic.AddUint64(&s.dropped, 1)
	}
}
func (s *stream) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

type hub struct {
	src         Source
	opts        options
	mu          sync.Mutex
	closed      bool
	streams     map[*stream]struct{}
	events      *ring[Event]
	logs        *ring[Event]
	memory      *ring[MemSample]
	seq         uint64
	status      []byte
	statusFrame []byte
	statusGen   uint64
	memFrame    []byte
	memGen      uint64
	stopTick    chan struct{}
	samples     uint64
	received    uint64
	done        chan struct{}
	unsub       func()
	wg          sync.WaitGroup
	once        sync.Once
}

func newHub(src Source, cfg Config, opts options) *hub {
	return &hub{src: src, opts: opts, streams: make(map[*stream]struct{}), events: newRing[Event](cfg.EventHistory), logs: newRing[Event](cfg.LogHistory), memory: newRing[MemSample](cfg.MemoryHistory), done: make(chan struct{})}
}
func (h *hub) start() {
	in := make(chan Event, h.opts.sourceBuffer)
	h.unsub = h.src.Subscribe(in)
	h.wg.Add(1)
	go h.pump(in)
}
func (h *hub) close() {
	h.once.Do(func() {
		h.mu.Lock()
		h.closed = true
		if h.stopTick != nil {
			close(h.stopTick)
			h.stopTick = nil
		}
		h.mu.Unlock()
		close(h.done)
		if h.unsub != nil {
			h.unsub()
		}
	})
	h.wg.Wait()
}
func (h *hub) pump(in <-chan Event) {
	defer h.wg.Done()
	for {
		select {
		case <-h.done:
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			atomic.AddUint64(&h.received, 1)
			h.publish(ev)
		}
	}
}
func (h *hub) publish(ev Event) {
	ev = clampEvent(ev, h.opts.now().UnixMilli())

	h.mu.Lock()
	h.seq++
	ev.Seq = h.seq
	// KindChat rides the same higher-capacity ring as KindLog (see that const's own doc
	// comment) rather than the smaller "notable events only" ring state changes use.
	if ev.Kind == KindLog || ev.Kind == KindChat {
		h.logs.push(ev)
	} else {
		h.events.push(ev)
	}
	if len(h.streams) == 0 {
		h.mu.Unlock()
		return
	}
	streams := make([]*stream, 0, len(h.streams))
	for s := range h.streams {
		streams = append(streams, s)
	}
	h.mu.Unlock()
	// Encode only for current subscribers, after assigning the same sequence
	// kept in replay history. Keep encoding outside the hub mutex.
	frame := eventFrame(ev)
	for _, s := range streams {
		s.offer(frame)
	}
}
func (h *hub) keptAfter(after uint64, kind string) []Event {
	collect := func(r *ring[Event]) []Event {
		out := make([]Event, 0, r.size())
		r.each(func(ev Event) {
			if ev.Seq > after {
				out = append(out, ev)
			}
		})
		return out
	}
	var a, b []Event
	if kind != KindLog {
		a = collect(h.events)
	}
	if kind != kindEvents {
		b = collect(h.logs)
	}
	out := make([]Event, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if j == len(b) || (i < len(a) && a[i].Seq < b[j].Seq) {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	return out
}
func (h *hub) join(after uint64) (*stream, []Event, uint64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, 0, errClosed
	}
	if len(h.streams) >= h.opts.maxStreams {
		return nil, nil, 0, errTooManyStreams
	}
	if after > h.seq {
		after = 0
	}
	replay := h.keptAfter(after, "")
	s := &stream{queue: make(chan []byte, h.opts.queue), wake: make(chan struct{}, 1)}
	h.streams[s] = struct{}{}
	if h.statusGen > 0 {
		s.nudge()
	}
	if h.stopTick == nil {
		h.stopTick = make(chan struct{})
		h.wg.Add(1)
		go h.sampler(h.stopTick)
	}
	return s, replay, h.seq, nil
}
func (h *hub) leave(s *stream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.streams, s)
	if len(h.streams) == 0 && h.stopTick != nil {
		close(h.stopTick)
		h.stopTick = nil
	}
}
func (h *hub) streamCount() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.streams) }
func (h *hub) sampler(stop <-chan struct{}) {
	defer h.wg.Done()
	ticker := newTicker(h.opts.tick)
	defer ticker.Stop()
	var buf []AccountStatus
	for {
		buf = h.sample(buf)
		select {
		case <-stop:
			return
		case <-h.done:
			return
		case <-ticker.C:
		}
	}
}
func (h *hub) sample(buf []AccountStatus) []AccountStatus {
	accounts := h.src.Accounts(buf[:0])
	logins := h.src.Logins()
	mem := MemSample{T: h.opts.now().UnixMilli(), MemStats: h.src.MemStats()}
	body := encodeStatus(accounts, logins)
	memData, _ := json.Marshal(mem)
	memFrame := sseFrame(sseMemory, 0, memData)
	h.mu.Lock()
	h.samples++
	if !bytes.Equal(body, h.status) {
		h.status = body
		h.statusFrame = sseFrame(sseStatus, 0, body)
		h.statusGen++
	}
	h.memory.push(mem)
	h.memFrame = memFrame
	h.memGen++
	for s := range h.streams {
		s.nudge()
	}
	h.mu.Unlock()
	for i := range accounts {
		accounts[i] = AccountStatus{}
	}
	if cap(accounts) > 2*MaxAccounts {
		return nil
	}
	return accounts
}
func (h *hub) latest() (status []byte, statusGen uint64, memory []byte, memGen uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.statusFrame, h.statusGen, h.memFrame, h.memGen
}
func (h *hub) eventsAfter(after uint64, limit int, kind string) ([]Event, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if after > h.seq {
		after = 0
	}
	out := h.keptAfter(after, kind)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, h.seq
}
func (h *hub) memorySamples() []MemSample {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]MemSample, 0, h.memory.size())
	h.memory.each(func(m MemSample) { out = append(out, m) })
	return out
}
