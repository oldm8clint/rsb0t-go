package harness

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// outcome is what one recorded line of a run means for the verdict.
type outcome int

const (
	outcomeNote outcome = iota
	outcomePass
	outcomeFail
	outcomeVoid // a precondition that was never met: the run proves nothing either way
)

type step struct {
	kind   outcome
	label  string
	detail string
}

// Verdict collects what a live run actually observed and decides the exit code from it.
//
// The rules it enforces, each one a bug this project shipped:
//
//  1. **A run with no Check that ran is a failure, not a pass.** A harness whose assertions
//     were all skipped printed nothing but setup lines and exited 0, and was read as proof.
//  2. **A failed Require voids the run.** A precondition that never held (the teleport that
//     never landed, the item that was never given) means the thing under test never got the
//     chance to happen, so neither "it worked" nor "it is broken" is honest. Exit code 2 says
//     so out loud, and is deliberately different from a real failure's 1.
//  3. **Every Check carries its own evidence string**, printed whether it passed or failed, so
//     the summary line can be checked against what was seen rather than trusted.
type Verdict struct {
	Tool string
	Out  io.Writer // defaults to os.Stdout
	// Exit ends the process. Defaults to os.Exit; a test replaces it to observe Finish.
	Exit     func(code int)
	steps    []step
	cleanups []func()
}

// New starts a verdict for a tool.
func New(tool string) *Verdict {
	return &Verdict{Tool: tool}
}

func (v *Verdict) out() io.Writer {
	if v.Out != nil {
		return v.Out
	}
	return os.Stdout
}

// Require records a precondition: the state the thing under test needs before it can even be
// attempted. A false one voids the whole run rather than failing it. Returns ok, so a caller
// can stop early.
//
// Why the satisfied line brackets its detail instead of setting it off with a dash: the failed
// line reads "SETUP FAILED  <label> — <reason>", so printing a satisfied one in that same
// "<label> — <text>" shape made the detail scan as a failure reason. Two agents read a passing
// run's own "setup ok" lines as failures because of it. The evidence still prints — that is the
// point of Require's third argument — but a satisfied precondition no longer borrows the
// failure line's punctuation.
func (v *Verdict) Require(label string, ok bool, detail string) bool {
	if ok {
		v.steps = append(v.steps, step{outcomePass, "setup: " + label, detail})
		fmt.Fprintf(v.out(), "  setup ok   %s (%s)\n", label, detail)
		return true
	}
	v.steps = append(v.steps, step{outcomeVoid, label, detail})
	fmt.Fprintf(v.out(), "  SETUP FAILED  %s — %s\n", label, detail)
	return false
}

// RequireNoError is Require for a setup call that returns an error.
func (v *Verdict) RequireNoError(label string, err error) bool {
	if err != nil {
		return v.Require(label, false, err.Error())
	}
	return v.Require(label, true, "ok")
}

// Check records an assertion about the thing under test. detail must describe what was
// actually observed — the tile the account reached, the count before and after — not what was
// expected, so a reader can audit the verdict from the log alone.
func (v *Verdict) Check(label string, ok bool, detail string) bool {
	if ok {
		v.steps = append(v.steps, step{outcomePass, label, detail})
		fmt.Fprintf(v.out(), "  PASS  %s — %s\n", label, detail)
		return true
	}
	v.steps = append(v.steps, step{outcomeFail, label, detail})
	fmt.Fprintf(v.out(), "  FAIL  %s — %s\n", label, detail)
	return false
}

// Note records an observation that is worth printing but decides nothing.
func (v *Verdict) Note(label, detail string) {
	v.steps = append(v.steps, step{outcomeNote, label, detail})
	fmt.Fprintf(v.out(), "  note  %s — %s\n", label, detail)
}

// Voided reports whether any precondition failed.
func (v *Verdict) Voided() bool {
	for _, s := range v.steps {
		if s.kind == outcomeVoid {
			return true
		}
	}
	return false
}

// checks counts the assertions that actually ran.
func (v *Verdict) checks() (pass, fail int) {
	for _, s := range v.steps {
		switch s.kind {
		case outcomePass:
			if !strings.HasPrefix(s.label, "setup: ") {
				pass++
			}
		case outcomeFail:
			fail++
		}
	}
	return pass, fail
}

// ExitCode is 0 only for a run that asserted something and had every assertion hold: 2 when a
// precondition was never met (the run proves nothing), 1 for a real failure, and 1 for a run
// that never asserted anything at all.
func (v *Verdict) ExitCode() int {
	if v.Voided() {
		return 2
	}
	pass, fail := v.checks()
	if fail > 0 {
		return 1
	}
	if pass == 0 {
		return 1
	}
	return 0
}

// Summary is the one line a reader should be able to trust.
func (v *Verdict) Summary() string {
	pass, fail := v.checks()
	switch {
	case v.Voided():
		var missing []string
		for _, s := range v.steps {
			if s.kind == outcomeVoid {
				missing = append(missing, s.label)
			}
		}
		return fmt.Sprintf("%s: VOID — a precondition was never met (%s), so this run proves nothing either way. Read the full log.",
			v.Tool, strings.Join(missing, "; "))
	case fail > 0:
		return fmt.Sprintf("%s: FAIL — %d of %d checks failed", v.Tool, fail, pass+fail)
	case pass == 0:
		return fmt.Sprintf("%s: FAIL — the run finished without asserting anything, which is not a pass", v.Tool)
	default:
		return fmt.Sprintf("%s: PASS — %d checks, each against observed state", v.Tool, pass)
	}
}

// Report prints the summary and returns the exit code.
func (v *Verdict) Report() int {
	fmt.Fprintf(v.out(), "\n===== %s\n", v.Summary())
	return v.ExitCode()
}

// OnExit registers a teardown Finish must run before the process ends, most importantly a
// clean logout. Cleanups run in reverse registration order, like defer.
//
// Why this exists at all: Finish calls os.Exit, and os.Exit runs no deferred call, so a tool
// written the obvious way — `defer s.Logout()` near the top, `v.Finish()` at every exit — never
// logs out from any of them. That is not cosmetic. This engine persists a ::setvar only through
// its normal disconnect-save path, so a tool that dies without logging out loses the account
// setup it just paid for (cmd/equiptest and cmd/smeltertest both recorded the same finding
// independently), and the abrupt drop leaves the account inside the server's ~60s "already
// logged in" window, so the next run cannot start immediately.
func (v *Verdict) OnExit(fn func()) {
	if fn != nil {
		v.cleanups = append(v.cleanups, fn)
	}
}

// runCleanups runs every registered teardown in reverse order, once.
func (v *Verdict) runCleanups() {
	for i := len(v.cleanups) - 1; i >= 0; i-- {
		v.cleanups[i]()
	}
	v.cleanups = nil
}

// Finish runs the registered teardowns, prints the summary and exits with its code. A tool's
// last line, and the only one it needs at an early exit too.
func (v *Verdict) Finish() {
	v.runCleanups()
	code := v.Report()
	if v.Exit != nil {
		v.Exit(code)
		return
	}
	os.Exit(code)
}
