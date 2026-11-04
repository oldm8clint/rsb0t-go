package sessionipc

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := ActionCall{Account: "bob", Name: "Ping", Args: []byte(`{"n":1}`)}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	var out ActionCall
	if err := ReadFrame(&buf, &out); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if out.Account != in.Account || out.Name != in.Name || string(out.Args) != string(in.Args) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", out, in)
	}
}

// TestFrameMultipleInSequence proves frames are self-delimiting: several written back
// to back onto the same stream read back out in the same order, which is what lets
// Client/Server (conn.go) hold one long-lived connection open across many calls instead
// of one connection per call.
func TestFrameMultipleInSequence(t *testing.T) {
	var buf bytes.Buffer
	want := []string{"a", "bb", "ccc"}
	for _, w := range want {
		if err := WriteFrame(&buf, ActionResult{Err: w}); err != nil {
			t.Fatalf("WriteFrame(%q): %v", w, err)
		}
	}
	for _, w := range want {
		var out ActionResult
		if err := ReadFrame(&buf, &out); err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if out.Err != w {
			t.Fatalf("got %q, want %q", out.Err, w)
		}
	}
}

func TestReadFrameEOFOnCleanClose(t *testing.T) {
	var buf bytes.Buffer
	var out ActionResult
	if err := ReadFrame(&buf, &out); err == nil {
		t.Fatalf("expected an error reading from an empty stream")
	}
}
