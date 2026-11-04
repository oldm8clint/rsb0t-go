package fleet

import "testing"

// Every line here is a real Guardian.kick emission, taken verbatim from internal/randomevents
// /guardian.go and cross-checked against a live fleet log (2026-09-20, build fa474b3d). The
// point of the test is the distinction the dashboard got wrong: "did not clear" is not the
// same as "failed".
func TestParseRandomEventClassifiesTerminalOutcomes(t *testing.T) {
	for _, c := range []struct {
		name         string
		line         string
		wantEvent    string
		wantOutcome  string
		wantRecorded bool
		why          string
	}{
		{
			name:         "cleared",
			line:         "random event: Strange plant — cleared",
			wantEvent:    "Strange plant",
			wantOutcome:  "cleared",
			wantRecorded: true,
			why:          "the only genuine success",
		},
		{
			name:         "rejected is not ours, not a failure",
			line:         "random event: Mime — rejected for this account; checking other events next",
			wantEvent:    "Mime",
			wantOutcome:  "not_ours",
			wantRecorded: true,
			why:          "the ownership check declining another player's event is a correct decision",
		},
		{
			name:         "still present is a retry, not a failure",
			line:         "random event: Mime — still present after handling",
			wantEvent:    "Mime",
			wantOutcome:  "retry",
			wantRecorded: true,
			why:          "one live account logged this 13 times for a single mime that later cleared",
		},
		{
			name:         "handler error is a real failure",
			line:         "random event: Mysterious Old Man — handler error: no approach path to Mysterious Old Man",
			wantEvent:    "Mysterious Old Man",
			wantOutcome:  "error",
			wantRecorded: true,
			why:          "the handler could not run at all",
		},
		{
			name:         "giving up is a real failure, and its name is before the separator",
			line:         "random event: gave up on Mime after 3 attempts — ignoring it for 5m0s",
			wantEvent:    "Mime",
			wantOutcome:  "gave_up",
			wantRecorded: true,
			why:          "this line's shape differs from the rest and used to be dropped entirely",
		},
		{
			name:         "handling is parsed but the caller drops it",
			line:         "random event: Strange plant — handling",
			wantEvent:    "Strange plant",
			wantOutcome:  "handling",
			wantRecorded: true,
			why:          "fired, outcome not yet known; StartBot filters this one out",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			name, outcome, ok := parseRandomEvent(c.line)
			if ok != c.wantRecorded {
				t.Fatalf("ok = %v, want %v (%s)\nline: %s", ok, c.wantRecorded, c.why, c.line)
			}
			if name != c.wantEvent {
				t.Errorf("event name = %q, want %q\nline: %s", name, c.wantEvent, c.line)
			}
			if outcome != c.wantOutcome {
				t.Errorf("outcome = %q, want %q (%s)\nline: %s", outcome, c.wantOutcome, c.why, c.line)
			}
		})
	}
}

// Progress lines share the "random event: <name> — " prefix with terminal outcomes, so the
// parser must reject them by name rather than by a default case. These are all real lines from
// one live run; treated as outcomes they would drown the genuine ones (that run logged 252
// whirlpool lines and 200+ "walking to" lines against 93 cleared).
func TestParseRandomEventRejectsProgressAndUnrelatedLines(t *testing.T) {
	for _, line := range []string{
		"random event: Strange plant — walking to 2615,3397",
		"random event: whirlpool — whirlpool: stepped clear; not waiting for it to revert",
		"random event: Strange plant — fruit still growing; checking other events before the next try",
		"random event: Genie — no dialogue opened; checking other events before retrying",
		"random event: Mysterious Old Man — dialogue opened and navigated",
		"random event: Genie — checking other events",
		"random event: Strange plant",                   // no separator at all
		"[fisher] caught one — 20 raw fish in pack now", // an ordinary script line sharing the log
		"",
	} {
		if name, outcome, ok := parseRandomEvent(line); ok {
			t.Errorf("parseRandomEvent(%q) = (%q, %q, true); want ok=false — a progress line must never be recorded as an outcome", line, name, outcome)
		}
	}
}
