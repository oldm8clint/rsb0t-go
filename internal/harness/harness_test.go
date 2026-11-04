package harness

import (
	"bytes"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestProductionURLCatchesTheRealGame(t *testing.T) {
	for _, u := range []string{"wss://w1.example.invalid", "https://example.invalid/", "ws://w2.example.invalid:443/", "wss://example-server.com/", "https://w1.example-server.com/", "localhost:8888", "%zz://x"} {
		if !ProductionURL(u) {
			t.Errorf("ProductionURL(%q) = false, want true", u)
		}
	}
	for _, u := range []string{"ws://localhost/", "http://127.0.0.1:8900/", "ws://localhost:80/", "ws://192.168.1.5:8888/", "ws://lab.test/", "http://engine.localhost/"} {
		if ProductionURL(u) {
			t.Errorf("ProductionURL(%q) = true, want false", u)
		}
	}
}

func TestCheatDecisionRefusesProductionEvenWhenAsked(t *testing.T) {
	allow, msg := CheatDecision("tool", true, []string{"ws://localhost/", "wss://w1.example.invalid"})
	if allow {
		t.Fatal("cheats allowed against production")
	}
	if !strings.Contains(msg, "example.invalid") {
		t.Errorf("message does not name the offending host: %q", msg)
	}
}

func TestCheatDecisionRefusesWithoutTheFlag(t *testing.T) {
	allow, msg := CheatDecision("tool", false, []string{"ws://localhost/"})
	if allow {
		t.Fatal("cheats allowed without -enable-cheats")
	}
	if !strings.Contains(msg, "-enable-cheats is required") {
		t.Errorf("message does not say what to do: %q", msg)
	}
}

func TestCheatDecisionAllowsALocalEngine(t *testing.T) {
	if allow, msg := CheatDecision("tool", true, []string{"ws://localhost/", "http://localhost/"}); !allow {
		t.Fatalf("local engine refused: %q", msg)
	}
}

func TestChebyshevCountsDiagonalsAsOneStep(t *testing.T) {
	a := tile.Tile{X: 3200, Z: 3200}
	if got := Chebyshev(a, tile.Tile{X: 3205, Z: 3203}); got != 5 {
		t.Errorf("Chebyshev = %d, want 5", got)
	}
	if got := Chebyshev(a, a); got != 0 {
		t.Errorf("Chebyshev to self = %d, want 0", got)
	}
}

func TestMovedAtLeastRejectsAnAccountThatNeverMoved(t *testing.T) {
	at := tile.Tile{X: 3200, Z: 3200}
	ok, detail := MovedAtLeast(at, at, 10)
	if ok {
		t.Fatal("a same-tile reading counted as travel")
	}
	if !strings.Contains(detail, "moved only 0 tiles") {
		t.Errorf("detail does not show the reading: %q", detail)
	}
}

func TestMovedAtLeastAcceptsARealWalkAndALevelChange(t *testing.T) {
	from := tile.Tile{X: 3200, Z: 3200}
	if ok, _ := MovedAtLeast(from, tile.Tile{X: 3240, Z: 3200}, 10); !ok {
		t.Error("a 40-tile walk was rejected")
	}
	if ok, detail := MovedAtLeast(from, tile.Tile{X: 3200, Z: 3200, Level: 1}, 10); !ok {
		t.Errorf("a level change was rejected: %q", detail)
	}
}

func TestCountChangedNeedsTheDeltaNotTheTotal(t *testing.T) {
	// The bug this guards: a harness asked a bank for items it already held and counted
	// the pre-existing stack as proof the withdrawal worked.
	if ok, _ := CountChanged("coins", 500, 500, 100); ok {
		t.Error("an unchanged count counted as a gain")
	}
	if ok, _ := CountChanged("coins", 500, 600, 100); !ok {
		t.Error("a real gain was rejected")
	}
	if ok, _ := CountChanged("coins", 500, 400, -100); !ok {
		t.Error("a real loss was rejected")
	}
	if ok, _ := CountChanged("coins", 500, 550, -100); ok {
		t.Error("a gain satisfied a check for a loss")
	}
}

func TestVerdictWithNoChecksIsAFailure(t *testing.T) {
	v := &Verdict{Tool: "t", Out: &bytes.Buffer{}}
	if code := v.ExitCode(); code != 1 {
		t.Errorf("a run that asserted nothing exited %d, want 1", code)
	}
	if !strings.Contains(v.Summary(), "without asserting anything") {
		t.Errorf("summary hides the problem: %q", v.Summary())
	}
}

func TestVerdictSetupAloneIsNotAPass(t *testing.T) {
	v := &Verdict{Tool: "t", Out: &bytes.Buffer{}}
	v.Require("teleported to the start", true, "at 3200,3200,0")
	if code := v.ExitCode(); code != 1 {
		t.Errorf("setup-only run exited %d, want 1 — setup is not evidence", code)
	}
}

func TestVerdictPassesOnlyWithAPassingCheck(t *testing.T) {
	v := &Verdict{Tool: "t", Out: &bytes.Buffer{}}
	v.Require("teleported to the start", true, "at 3200,3200,0")
	v.Check("walked to the bank", true, "moved 40 tiles")
	if code := v.ExitCode(); code != 0 {
		t.Errorf("ExitCode = %d, want 0", code)
	}
	if !strings.Contains(v.Summary(), "PASS") {
		t.Errorf("summary = %q", v.Summary())
	}
}

func TestVerdictFailsOnAFailedCheck(t *testing.T) {
	v := &Verdict{Tool: "t", Out: &bytes.Buffer{}}
	v.Check("walked to the bank", false, "moved only 0 tiles")
	if code := v.ExitCode(); code != 1 {
		t.Errorf("ExitCode = %d, want 1", code)
	}
}

func TestAFailedPreconditionVoidsTheRunRatherThanFailingIt(t *testing.T) {
	// The distinction that matters: the thing under test never got the chance to happen, so
	// neither "it works" nor "it is broken" is honest, and the exit code says which.
	v := &Verdict{Tool: "t", Out: &bytes.Buffer{}}
	v.Require("teleported to the start", false, "still at 3222,3218,0 after 8s")
	v.Check("walked to the bank", true, "moved 40 tiles")
	if code := v.ExitCode(); code != 2 {
		t.Errorf("ExitCode = %d, want 2 (void)", code)
	}
	s := v.Summary()
	if !strings.Contains(s, "VOID") || !strings.Contains(s, "proves nothing") {
		t.Errorf("summary does not void the run: %q", s)
	}
	if strings.Contains(s, "PASS") {
		t.Errorf("a voided run still claimed a pass: %q", s)
	}
}

func TestVerdictPrintsTheEvidenceForEveryCheck(t *testing.T) {
	var buf bytes.Buffer
	v := &Verdict{Tool: "t", Out: &buf}
	v.Check("walked to the bank", true, "moved 40 tiles, 3200,3200 -> 3240,3200")
	v.Note("energy", "run was on")
	out := buf.String()
	if !strings.Contains(out, "moved 40 tiles, 3200,3200 -> 3240,3200") {
		t.Errorf("evidence missing from the log: %q", out)
	}
	if !strings.Contains(out, "note  energy") {
		t.Errorf("note missing from the log: %q", out)
	}
}

func TestRequireNoErrorReportsTheError(t *testing.T) {
	var buf bytes.Buffer
	v := &Verdict{Tool: "t", Out: &buf}
	v.RequireNoError("gave the net", ErrCheatRefused)
	if v.ExitCode() != 2 {
		t.Errorf("ExitCode = %d, want 2", v.ExitCode())
	}
	if !strings.Contains(buf.String(), "cheat refused") {
		t.Errorf("error text missing: %q", buf.String())
	}
}
