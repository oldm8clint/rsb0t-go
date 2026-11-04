package trace

import (
	"encoding/json"
	"fmt"
	"io"
)

// Divergence identifies the first decision at which two traces differ.
type Divergence struct {
	Tick            uint64   `json:"tick"`
	ReferenceAction string   `json:"reference_action"`
	CandidateAction string   `json:"candidate_action"`
	Reference       Decision `json:"reference"`
	Candidate       Decision `json:"candidate"`
}

// Comparison is suitable for both CLI output and compact LLM feedback.
type Comparison struct {
	Equal           bool        `json:"equal"`
	FirstDivergence *Divergence `json:"first_divergence,omitempty"`
	RecordsCompared int         `json:"records_compared"`
	Error           string      `json:"error,omitempty"`
}

// Compare reads decision records from both streams without loading either
// complete trace into memory. Summary records are ignored by this comparator.
func Compare(reference, candidate io.Reader) Comparison {
	r := json.NewDecoder(reference)
	c := json.NewDecoder(candidate)
	comp := Comparison{Equal: true}
	for {
		rd, re := nextDecision(r)
		cd, ce := nextDecision(c)
		if re == io.EOF && ce == io.EOF {
			return comp
		}
		if re != nil && re != io.EOF {
			comp.Equal = false
			comp.Error = fmt.Sprintf("reference trace: %v", re)
			return comp
		}
		if ce != nil && ce != io.EOF {
			comp.Equal = false
			comp.Error = fmt.Sprintf("candidate trace: %v", ce)
			return comp
		}
		if re == io.EOF || ce == io.EOF {
			comp.Equal = false
			comp.Error = "trace lengths differ"
			return comp
		}
		comp.RecordsCompared++
		if rd != cd {
			comp.Equal = false
			comp.FirstDivergence = &Divergence{
				Tick:            rd.Tick,
				ReferenceAction: string(rd.Action),
				CandidateAction: string(cd.Action),
				Reference:       rd,
				Candidate:       cd,
			}
			return comp
		}
	}
}

// nextDecision decodes directly into the fixed-size Decision value. Summary
// records have no Action and are therefore skipped. This avoids the previous
// RawMessage allocation plus second JSON unmarshal on every trace record.
func nextDecision(dec *json.Decoder) (Decision, error) {
	for {
		var d Decision
		if err := dec.Decode(&d); err != nil {
			return Decision{}, err
		}
		if d.Action == "" {
			continue
		}
		return d, nil
	}
}
