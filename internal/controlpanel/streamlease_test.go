package controlpanel

import (
	"net/http"
	"testing"
	"time"
)

// A view that is merely late (a terrain decode that outlasts the TTL on a busy machine, a
// background tab whose timers were throttled) resumes its own lease on its next request when
// nobody took the slot meanwhile. It used to get 410 and the whole view tore itself down.
func TestStreamLapsedLeaseResumesWhileNobodyElseTookIt(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := streamHarness(t, src, &at)

	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &open)
	var first streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &first)

	at = at.Add(DefaultStreamLeaseTTL + 10*time.Second)
	var late streamFrame
	res := h.get("/api/stream/frame?session=" + open.Session)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a lapsed but untaken lease = %d, want it resumed", res.StatusCode)
	}
	decode(t, res, &late)
	if !late.Lapsed {
		t.Error("the resumed frame did not say the lease had lapsed; a viewer holding input control must learn it was released")
	}
	if late.Seq <= first.Seq {
		t.Errorf("seq %d after %d: a resumed lease must keep numbering", late.Seq, first.Seq)
	}
	// Only the first request after the lapse says so.
	at = at.Add(time.Second)
	var next streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &next)
	if next.Lapsed {
		t.Error("a second request after the resume still reported a lapse")
	}

	// Once somebody else has taken the slot, the old session is gone for good.
	at = at.Add(DefaultStreamLeaseTTL + time.Second)
	var rival streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &rival)
	if rival.Session == open.Session {
		t.Fatal("a rival after the TTL got the old session back")
	}
	if res := h.get("/api/stream/frame?session=" + open.Session); res.StatusCode != http.StatusGone {
		t.Errorf("the replaced lease served a frame: %d", res.StatusCode)
	}
}

// The same page reopening (a reload, or a reconnect after its own connection dropped) takes its
// lease back at once with its owner token; a different page is still refused while the lease is
// fresh.
func TestStreamOwnerTokenLetsAPageRetakeItsOwnLease(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a"), ingameAccount("b")}}}
	h := streamHarness(t, src, &at)

	var mine streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a", "owner": "page-1"}), &mine)

	if res := h.postJSON("/api/stream/open", map[string]string{"account": "a", "owner": "page-2"}); res.StatusCode != http.StatusConflict {
		t.Errorf("a different page opened over a fresh lease: %d", res.StatusCode)
	}
	if res := h.postJSON("/api/stream/open", map[string]string{"account": "a"}); res.StatusCode != http.StatusConflict {
		t.Errorf("an anonymous page opened over a fresh lease: %d", res.StatusCode)
	}
	var again streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a", "owner": "page-1"}), &again)
	if again.Session == "" || again.Session == mine.Session {
		t.Fatalf("the owner did not retake its own lease: %+v", again)
	}
	if res := h.get("/api/stream/frame?session=" + mine.Session); res.StatusCode != http.StatusGone {
		t.Errorf("the superseded session of the same owner still served frames: %d", res.StatusCode)
	}
	if res := h.postJSON("/api/stream/open", map[string]string{"account": "a", "owner": string(make([]byte, 65))}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("an oversized owner token was accepted: %d", res.StatusCode)
	}
}
