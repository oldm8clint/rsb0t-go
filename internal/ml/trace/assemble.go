package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"math"

	"rsb0t-go/internal/ml/replay"
)

// This file is the "episode assembler" docs/ml_lab_real_observation_bridge.md's own
// architecture diagram places between per-decision trace.Decision JSONL and the
// replay.Episode shape internal/ml/replay and internal/ml/evaluation already consume.
// Nothing before this pass ever produced a real trace.Decision stream (the type existed,
// unused, since the ML lab was built) — this is the first reader for it, and per that
// doc's own "Ordering and integrity" section it fails closed: any impossible sequence
// (duplicate/out-of-order ticks, a decision after a terminal one, a schema mismatch, an
// invalid action, a non-finite reward, an oversized record) stops assembly with an error
// rather than silently dropping or repairing the bad record.

const (
	// DefaultMaxDecisions bounds how many Decision records one Assemble call will read,
	// so a runaway or corrupt trace file cannot force unbounded work — mirrors
	// replay.DefaultMaxEpisodes' own purpose for the episode-level reader.
	DefaultMaxDecisions = 1_000_000
	// DefaultMaxRecordBytes bounds one decoded Decision's re-encoded size. Generous
	// relative to the compact observation.State this pipeline actually uses (well under
	// 1KB), but still small enough to catch a corrupted or adversarial record long
	// before it could pressure memory.
	DefaultMaxRecordBytes = 64 << 10
	// DefaultMaxActionLen bounds Action, which — unlike the fixed six-value
	// internal/ml/policy.Action vocabulary the synthetic FisherEnv uses — carries real
	// fisherplan.FishPhase names live (see internal/scripts/fisherrunner/tracebridge.go),
	// a wider but still small, fixed vocabulary.
	DefaultMaxActionLen = 64
)

// AssembleOptions bounds one Assemble call. A zero value is replaced by the matching
// Default* constant.
type AssembleOptions struct {
	MaxDecisions   int
	MaxRecordBytes int
	MaxActionLen   int
}

func (o AssembleOptions) withDefaults() AssembleOptions {
	if o.MaxDecisions <= 0 {
		o.MaxDecisions = DefaultMaxDecisions
	}
	if o.MaxRecordBytes <= 0 {
		o.MaxRecordBytes = DefaultMaxRecordBytes
	}
	if o.MaxActionLen <= 0 {
		o.MaxActionLen = DefaultMaxActionLen
	}
	return o
}

// episodeState is one episode's in-progress assembly state.
type episodeState struct {
	ep            *replay.Episode
	schemaVersion uint16
	haveTick      bool
	lastTick      uint64
	seenTicks     map[uint64]bool
	terminal      bool
}

// Assemble reads every Decision record from r (as Writer.Decision produces, one JSON
// object per line) and groups them into replay.Episode records, keyed by
// Decision.Episode, in the order each episode was first seen. It stops and returns an
// error on the first record that violates the decision-boundary/ordering contract
// docs/ml_lab_real_observation_bridge.md specifies:
//
//   - schema version must be nonzero and consistent within one episode;
//   - episode ID must be non-negative;
//   - action must be non-empty and within MaxActionLen;
//   - reward must be finite (not NaN/+-Inf);
//   - tick must not repeat or go backward within one episode (gaps are fine — a live
//     source that legitimately skips a tick is not an integrity failure, only a
//     duplicate or a regression is);
//   - no decision may follow one already marked Terminal for the same episode;
//   - a decoded record's re-encoded size must not exceed MaxRecordBytes.
//
// A plain io.EOF from r ends the stream normally; any other read/decode error (including
// malformed JSON) is returned as-is, already itself a fail-closed result.
func Assemble(r *Reader, opts AssembleOptions) ([]replay.Episode, error) {
	if r == nil {
		return nil, fmt.Errorf("trace: nil reader")
	}
	opts = opts.withDefaults()

	episodes := map[int]*episodeState{}
	var order []int
	count := 0

	for {
		var d Decision
		if err := r.Next(&d); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("trace: decode decision %d: %w", count, err)
		}
		count++
		if count > opts.MaxDecisions {
			return nil, fmt.Errorf("trace: exceeds %d decisions", opts.MaxDecisions)
		}
		if err := validateDecision(d, opts); err != nil {
			return nil, fmt.Errorf("trace: decision %d (episode %d, tick %d): %w", count, d.Episode, d.Tick, err)
		}

		st, ok := episodes[d.Episode]
		if !ok {
			st = &episodeState{
				ep:            &replay.Episode{SchemaVersion: d.SchemaVersion, EpisodeID: uint64(d.Episode)},
				schemaVersion: d.SchemaVersion,
				seenTicks:     map[uint64]bool{},
			}
			episodes[d.Episode] = st
			order = append(order, d.Episode)
		}
		if st.terminal {
			return nil, fmt.Errorf("trace: decision %d: episode %d already reached a terminal decision", count, d.Episode)
		}
		if d.SchemaVersion != st.schemaVersion {
			return nil, fmt.Errorf("trace: decision %d: schema version %d does not match episode %d's earlier %d", count, d.SchemaVersion, d.Episode, st.schemaVersion)
		}
		if st.seenTicks[d.Tick] {
			return nil, fmt.Errorf("trace: decision %d: duplicate tick %d in episode %d", count, d.Tick, d.Episode)
		}
		if st.haveTick && d.Tick < st.lastTick {
			return nil, fmt.Errorf("trace: decision %d: tick %d went backward after %d in episode %d", count, d.Tick, st.lastTick, d.Episode)
		}
		st.seenTicks[d.Tick] = true
		st.lastTick = d.Tick
		st.haveTick = true

		st.ep.Add(replay.Step{
			Observation: d.Observation,
			Action:      d.Action,
			// Confidence has no analogue in a rule-based live decision (fisherplan's
			// phase machine, unlike a trained policy, never expresses uncertainty) —
			// 1 mirrors how internal/ml/policy.DeterministicFallback already reports
			// full confidence for its own deterministic picks, so a real and a
			// synthetic deterministic decision score identically here.
			Confidence: 1,
			Reward:     d.Reward,
		})
		if d.Terminal {
			st.terminal = true
		}
	}

	out := make([]replay.Episode, 0, len(order))
	for _, id := range order {
		out = append(out, *episodes[id].ep)
	}
	return out, nil
}

func validateDecision(d Decision, opts AssembleOptions) error {
	if d.SchemaVersion == 0 {
		return fmt.Errorf("missing schema version")
	}
	if d.Episode < 0 {
		return fmt.Errorf("negative episode id")
	}
	if len(d.Action) == 0 {
		return fmt.Errorf("empty action")
	}
	if len(d.Action) > opts.MaxActionLen {
		return fmt.Errorf("action %q exceeds %d bytes", d.Action, opts.MaxActionLen)
	}
	if r := float64(d.Reward); math.IsNaN(r) || math.IsInf(r, 0) {
		return fmt.Errorf("non-finite reward")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("re-encode decision: %w", err)
	}
	if len(raw) > opts.MaxRecordBytes {
		return fmt.Errorf("record size %d exceeds %d bytes", len(raw), opts.MaxRecordBytes)
	}
	return nil
}
