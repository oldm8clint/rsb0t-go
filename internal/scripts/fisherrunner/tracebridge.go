package fisherrunner

// This file is the real half of docs/ml_lab_real_observation_bridge.md: it turns
// ProgressiveFisher's actual live phase decisions (fisherplan.DecideFishPhase's own
// branches — fund, travel, bank, buy, sell, fish, stuck, ...) into trace.Decision JSONL
// records, mirroring the exact shape internal/ml/environment.FisherEnv's synthetic
// decisions already use so internal/ml/replay and internal/ml/evaluation can consume
// either source without caring which produced it. See internal/bot/fisher.go for the
// opt-in wiring (the traceDecisions/tracePath/traceExperimentId settings) and where this
// is actually called from inside Loop().
//
// Entirely additive and off by default: nothing here runs unless a script explicitly
// constructs a TraceRecorder, and every method on it is nil-receiver safe, so an account
// that never turns tracing on pays no cost beyond one nil check per Loop call.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/trace"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/tile"
)

// traceQueueSize bounds the asynchronous write queue. Per
// docs/ml_lab_real_observation_bridge.md's "Non-blocking runtime rule," recording must
// never block a live decision on disk I/O — Record enqueues and returns immediately,
// dropping (and counting) rather than blocking once this many decisions are backed up
// waiting to be written.
const traceQueueSize = 256

// TraceRecorder asynchronously appends live ProgressiveFisher decisions as trace.Decision
// JSONL records. A nil *TraceRecorder is always a safe no-op — every method here checks
// for it — so a script that never enables tracing (the common case) never allocates one
// and never touches this file's code path at all.
type TraceRecorder struct {
	ch     chan trace.Decision
	file   *os.File
	writer *trace.Writer
	done   chan struct{}

	dropped   uint64 // atomic
	writeErrs uint64 // atomic
	tick      uint64 // atomic

	experimentID string
}

// NewTraceRecorder opens path for append (creating parent directories as needed) and
// starts the background writer goroutine. Local-file-only by design: the doc's
// "Non-blocking runtime rule" singles out network I/O and remote upload as things that
// must never sit in this path, so this deliberately never does either.
func NewTraceRecorder(path, experimentID string) (*TraceRecorder, error) {
	if path == "" {
		return nil, fmt.Errorf("tracebridge: empty trace path")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("tracebridge: mkdir %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tracebridge: open %s: %w", path, err)
	}
	r := &TraceRecorder{
		ch:           make(chan trace.Decision, traceQueueSize),
		file:         f,
		writer:       trace.NewWriter(f),
		done:         make(chan struct{}),
		experimentID: experimentID,
	}
	go r.run()
	return r, nil
}

func (r *TraceRecorder) run() {
	defer close(r.done)
	for d := range r.ch {
		if err := r.writer.Decision(d); err != nil {
			atomic.AddUint64(&r.writeErrs, 1)
		}
	}
}

// Record enqueues one decision: the observation available when the phase was chosen,
// the phase itself (cast straight to policy.Action — see this file's own doc comment on
// why a real decision keeps its own name rather than being force-mapped onto
// FisherEnv's six synthetic actions), an outcome string, a reward
// (fisherplan.PhaseReward's own scale) and whether this is the episode's terminal
// decision. Safe on a nil receiver; never blocks — a full queue drops the record and
// counts it (see Dropped).
func (r *TraceRecorder) Record(episode int, obs observation.State, action policy.Action, result string, reward float32, terminal bool) {
	if r == nil {
		return
	}
	obs.Tick = atomic.AddUint64(&r.tick, 1) - 1
	d := trace.Decision{
		ExperimentID: r.experimentID,
		Episode:      episode,
		Tick:         obs.Tick,
		Observation:  obs,
		Action:       action,
		Result:       result,
		Reward:       reward,
		Terminal:     terminal,
	}
	select {
	case r.ch <- d:
	default:
		atomic.AddUint64(&r.dropped, 1)
	}
}

// Dropped is how many decisions were discarded because the queue was full. Safe on a nil
// receiver (always 0).
func (r *TraceRecorder) Dropped() uint64 {
	if r == nil {
		return 0
	}
	return atomic.LoadUint64(&r.dropped)
}

// WriteErrors is how many enqueued decisions failed to encode/write. Safe on a nil
// receiver (always 0).
func (r *TraceRecorder) WriteErrors() uint64 {
	if r == nil {
		return 0
	}
	return atomic.LoadUint64(&r.writeErrs)
}

// Close stops accepting new records, drains whatever is already queued, and closes the
// file. Safe on a nil receiver (a no-op returning nil).
func (r *TraceRecorder) Close() error {
	if r == nil {
		return nil
	}
	close(r.ch)
	<-r.done
	return r.file.Close()
}

func boolU16(v bool) uint16 {
	if v {
		return 0
	}
	return 1
}

func boolU8(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func clampU8(n int) uint8 {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}

func clampU16(n int) uint16 {
	if n < 0 {
		return 0
	}
	if n > 65535 {
		return 65535
	}
	return uint16(n)
}

// ObservationFromSnapshot turns a live FishSnapshot into the compact observation.State
// shape internal/ml already scores episodes by, so a live decision and a FisherEnv
// synthetic one are structurally identical to every downstream consumer — the
// "decision-boundary contract" docs/ml_lab_real_observation_bridge.md asks for. Two
// fields are deliberately kept as the same coarse 0/1 near-or-not flag FisherEnv itself
// uses (DistanceToFish/DistanceToBank), not a literal tile distance, so the two sources
// stay comparable rather than merely similarly shaped; XPDelta is left at its zero value
// here since it has no natural per-Loop-call analogue in a rule-based script (unlike a
// trained policy that would want a training signal there) — the reward passed to
// TraceRecorder.Record separately (fisherplan.PhaseReward) is this bridge's real signal.
func ObservationFromSnapshot(d *Deps, layout fisherplan.Layout, s fisherplan.FishSnapshot) observation.State {
	var x, y int
	nearBank, nearFish := false, false
	if s.Tile != nil {
		x, y = s.Tile.X, s.Tile.Z
		nearBank = tile.Chebyshev(*s.Tile, layout.Bank) <= layout.BankRadius
		nearFish = tile.Chebyshev(*s.Tile, layout.Camp) <= layout.SpotRange
	}
	used := 0
	if inv := d.Session.State().Inventory(PlayerPack); inv != nil {
		used = len(inv.NonEmpty())
	}
	hp, _ := d.Session.State().HP()
	return observation.State{
		SchemaVersion:  1,
		X:              x,
		Y:              y,
		InventoryUsed:  clampU8(used),
		InventorySize:  28,
		Health:         clampU16(hp),
		DistanceToFish: boolU16(nearFish),
		DistanceToBank: boolU16(nearBank),
		NearbyFish:     boolU8(nearFish),
		NearbyBank:     boolU8(nearBank),
	}
}
