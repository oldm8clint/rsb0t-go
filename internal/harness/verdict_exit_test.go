package harness

import (
	"bytes"
	"testing"
)

// Finish must run teardown before it ends the process. It calls os.Exit, which runs no deferred
// call, so a tool written the obvious way — `defer s.Logout()` at the top, `v.Finish()` at every
// exit — never logged out from any of them.
func TestFinishRunsCleanupsBeforeExiting(t *testing.T) {
	var out bytes.Buffer
	var order []string
	code := -1

	v := New("tool")
	v.Out = &out
	v.Exit = func(c int) { code = c }
	v.OnExit(func() { order = append(order, "first") })
	v.OnExit(func() { order = append(order, "second") })
	v.Check("something was observed", true, "observed it")
	v.Finish()

	// Reverse order, like defer: the logout registered last runs first.
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("cleanups ran %v, want [second first]", order)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !bytes.Contains(out.Bytes(), []byte("PASS")) {
		t.Fatalf("summary missing from %q", out.String())
	}
}

// An early exit — the voided run a failed precondition produces — is exactly the case that used
// to skip the logout, so it must run teardown too.
func TestFinishRunsCleanupsOnAVoidedRun(t *testing.T) {
	var out bytes.Buffer
	loggedOut := false
	code := -1

	v := New("tool")
	v.Out = &out
	v.Exit = func(c int) { code = c }
	v.OnExit(func() { loggedOut = true })
	if v.Require("the account reached the start tile", false, "still at 0,0") {
		t.Fatal("Require returned true for a failed precondition")
	}
	v.Finish()

	if !loggedOut {
		t.Fatal("the voided run exited without logging out")
	}
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (VOID)", code)
	}
}

// Finish is a tool's last line, but nothing stops a tool calling it twice on the way out of
// nested helpers. Teardown must not run twice: these cleanups close channels and sockets.
func TestCleanupsRunOnlyOnce(t *testing.T) {
	var out bytes.Buffer
	runs := 0

	v := New("tool")
	v.Out = &out
	v.Exit = func(int) {}
	v.OnExit(func() { runs++ })
	v.Check("observed", true, "yes")
	v.Finish()
	v.Finish()

	if runs != 1 {
		t.Fatalf("cleanup ran %d times, want 1", runs)
	}
}
