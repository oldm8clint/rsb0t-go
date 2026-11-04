// Package trace records bounded, offline ML experiment traces.
package trace

import (
	"encoding/json"
	"fmt"
	"io"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

const SchemaVersion uint16 = 1

type Decision struct {
	SchemaVersion uint16            `json:"schema_version"`
	ExperimentID  string            `json:"experiment_id"`
	Episode       int               `json:"episode"`
	Tick          uint64            `json:"tick"`
	Observation   observation.State `json:"observation"`
	Action        policy.Action     `json:"action"`
	Result        string            `json:"result"`
	Reward        float32           `json:"reward"`
	Terminal      bool              `json:"terminal,omitempty"`
}

type Summary struct {
	SchemaVersion uint16  `json:"schema_version"`
	ExperimentID  string  `json:"experiment_id"`
	Episode       int     `json:"episode"`
	Steps         int     `json:"steps"`
	TotalReward   float64 `json:"total_reward"`
	Terminal      bool    `json:"terminal"`
	Valid         bool    `json:"valid"`
}

type Writer struct{ enc *json.Encoder }

func NewWriter(w io.Writer) *Writer {
	enc := json.NewEncoder(w)
	// Trace files are machine-readable JSONL. Escaping HTML characters adds
	// work and bytes without providing value for this non-HTML transport.
	enc.SetEscapeHTML(false)
	return &Writer{enc: enc}
}

func (w *Writer) Decision(d Decision) error {
	if w == nil || w.enc == nil {
		return fmt.Errorf("trace writer is nil")
	}
	if d.SchemaVersion == 0 {
		d.SchemaVersion = SchemaVersion
	}
	if len(d.ExperimentID) > 128 {
		return fmt.Errorf("experiment_id exceeds 128 bytes")
	}
	if len(d.Result) > 128 {
		return fmt.Errorf("result exceeds 128 bytes")
	}
	return w.enc.Encode(d)
}

func (w *Writer) Summary(s Summary) error {
	if w == nil || w.enc == nil {
		return fmt.Errorf("trace writer is nil")
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = SchemaVersion
	}
	if len(s.ExperimentID) > 128 {
		return fmt.Errorf("experiment_id exceeds 128 bytes")
	}
	return w.enc.Encode(s)
}

type Reader struct{ dec *json.Decoder }

func NewReader(r io.Reader) *Reader { return &Reader{dec: json.NewDecoder(r)} }

func (r *Reader) Next(v any) error {
	if r == nil || r.dec == nil {
		return fmt.Errorf("trace reader is nil")
	}
	return r.dec.Decode(v)
}
