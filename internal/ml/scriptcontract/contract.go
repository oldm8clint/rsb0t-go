// Package scriptcontract defines the narrow boundary between an LLM-generated
// script candidate and the existing script runtime. It deliberately validates
// metadata and declared actions without executing generated source.
package scriptcontract

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/ml/policy"
)

type Candidate struct {
	SchemaVersion uint16          `json:"schema_version"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Source        string          `json:"source"`
	Actions       []policy.Action `json:"actions"`
	Tests         []string        `json:"tests"`
}

type Validation struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

func Validate(c Candidate) Validation {
	var errs []string
	if c.SchemaVersion != 1 {
		errs = append(errs, "unsupported schema_version")
	}
	if strings.TrimSpace(c.Name) == "" {
		errs = append(errs, "name is required")
	}
	if strings.TrimSpace(c.Source) == "" {
		errs = append(errs, "source is required")
	}
	if len(c.Source) > 256*1024 {
		errs = append(errs, "source exceeds 256 KiB")
	}
	if len(c.Tests) == 0 {
		errs = append(errs, "at least one test is required")
	}
	seen := map[policy.Action]bool{}
	for _, a := range c.Actions {
		if seen[a] {
			errs = append(errs, fmt.Sprintf("duplicate action %q", a))
			continue
		}
		seen[a] = true
		if !allowed(a) {
			errs = append(errs, fmt.Sprintf("action %q is outside the ML contract", a))
		}
	}
	return Validation{Valid: len(errs) == 0, Errors: errs}
}

func allowed(a policy.Action) bool {
	switch a {
	case policy.WalkToFish, policy.Fish, policy.WalkToBank, policy.Bank, policy.Wait, policy.Recover:
		return true
	default:
		return false
	}
}
