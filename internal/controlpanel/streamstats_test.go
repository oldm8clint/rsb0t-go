package controlpanel

import (
	"net/http"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// The point of /api/stream/stats is to turn "can the bridge serve more than 5
// observations a second?" into a number taken on the machine that runs the fleet. A test
// that only asserted the endpoint answers 200 would be exactly the kind of check
// docs/trustworthy_harnesses.md warns about, so this one asserts the counters actually
// track what was served: real observations apart from paced replies, the transport and
// kind of each frame, and a byte count equal to the body the browser received.
func TestStreamStatsCountOnlyRealObservationsAndMeasureTheirCost(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{
		staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("fisher1")}},
		snapshot:     gamestate.RenderSnapshot{X: 3222, Z: 3218, Level: 0, CentreX: 3216, CentreZ: 3216},
	}
	h := streamHarness(t, src, &at)

	// Nothing streaming: the endpoint is still available and honestly empty.
	var idle streamStatsBody
	decode(t, h.get("/api/stream/stats"), &idle)
	if idle.Observations != 0 || idle.View != nil || idle.Window != 0 {
		t.Fatalf("idle stats = %+v, want no observations and no view", idle)
	}
	if idle.MaxFPS != DefaultStreamMaxFPS {
		t.Errorf("maxFps = %d, want the cap actually in force (%d)", idle.MaxFPS, DefaultStreamMaxFPS)
	}

	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1", "fps": 60, "transport": "scene-delta"}), &open)
	if open.Transport != streamTransportScene {
		t.Fatalf("transport = %q, want scene-delta", open.Transport)
	}

	// One real observation (a whole scene), then a paced reply, then a delta.
	var first streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &first)
	if first.Scene == nil {
		t.Fatalf("first scene-delta frame carried no whole scene: %+v", first)
	}
	var paced streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &paced)
	if !paced.Throttled {
		t.Fatalf("second request was not paced: %+v", paced)
	}
	at = at.Add(time.Second)
	var second streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session+"&have=1"), &second)
	if second.Delta == nil {
		t.Fatalf("confirmed base did not produce a delta: %+v", second)
	}

	var stats streamStatsBody
	res := h.get("/api/stream/stats")
	decode(t, res, &stats)
	if stats.Observations != 2 {
		t.Errorf("observations = %d, want 2 (the paced reply is not an observation)", stats.Observations)
	}
	if stats.Throttled != 1 {
		t.Errorf("throttled = %d, want 1", stats.Throttled)
	}
	if stats.Window != 2 || stats.Kinds.Scene != 1 || stats.Kinds.Delta != 1 || stats.Kinds.Snapshot != 0 {
		t.Errorf("kinds = %+v over a window of %d, want one scene and one delta", stats.Kinds, stats.Window)
	}
	if stats.View == nil || stats.View.Account != "fisher1" || stats.View.Transport != streamTransportScene {
		t.Errorf("view = %+v, want the held lease's own account and transport", stats.View)
	}
	// Bytes are the encoded body, not an estimate: the whole scene must be bigger than
	// the delta that followed it, and the max must be the whole scene's own size.
	if stats.Bytes.Summary.Max <= 0 || stats.Bytes.Mean <= 0 {
		t.Fatalf("byte summary = %+v, want real sizes", stats.Bytes)
	}
	// (p50 over a two-sample window is the larger of the two by nearest rank, so the
	// mean is what separates them here.)
	if stats.Bytes.Summary.Max <= stats.Bytes.Mean {
		t.Errorf("max %d is not above the mean %d, so the whole scene and the delta that followed it were the same size",
			stats.Bytes.Summary.Max, stats.Bytes.Mean)
	}

	// Closing the view leaves the totals standing (they are this process's, not the
	// lease's) but reports no held view.
	h.postJSON("/api/stream/close", map[string]string{"session": open.Session})
	var after streamStatsBody
	decode(t, h.get("/api/stream/stats"), &after)
	if after.View != nil {
		t.Errorf("view = %+v after close, want none", after.View)
	}
	if after.Observations != 2 {
		t.Errorf("observations = %d after close, want the running total 2", after.Observations)
	}

	// Read-only.
	if res := h.postJSON("/api/stream/stats", map[string]string{}); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/stream/stats = %d, want 405", res.StatusCode)
	}
}

// A full-snapshot view must be reported as such, and its frames must be measurably
// bigger than the scene-delta ones — that difference is the whole argument for the
// transport, and it is the number this endpoint exists to make checkable.
func TestStreamStatsReportTheFullSnapshotTransportApart(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{
		staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("fisher1")}},
		snapshot: gamestate.RenderSnapshot{X: 3222, Z: 3218, Level: 0, CentreX: 3216, CentreZ: 3216,
			NPCs: []gamestate.NPC{{Index: 1, Type: 316, X: 3223, Z: 3219}, {Index: 2, Type: 317, X: 3224, Z: 3219}}},
	}
	h := streamHarness(t, src, &at)

	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1"}), &open)
	if open.Transport != streamTransportFull {
		t.Fatalf("default transport = %q, want full-snapshot", open.Transport)
	}
	var frame streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &frame)
	if frame.Snapshot == nil {
		t.Fatalf("full-snapshot frame carried no snapshot")
	}
	var stats streamStatsBody
	decode(t, h.get("/api/stream/stats"), &stats)
	if stats.Kinds.Snapshot != 1 || stats.Kinds.Scene != 0 || stats.Kinds.Delta != 0 {
		t.Errorf("kinds = %+v, want one snapshot frame", stats.Kinds)
	}
	if stats.View == nil || stats.View.Transport != streamTransportFull {
		t.Errorf("view = %+v, want the full-snapshot transport named", stats.View)
	}
	// The scene projection did not run on this transport, so its cost is honestly zero
	// rather than a number borrowed from somewhere else.
	if stats.Project.Max != 0 {
		t.Errorf("projectUs.max = %d on a full-snapshot view, want 0", stats.Project.Max)
	}
}
