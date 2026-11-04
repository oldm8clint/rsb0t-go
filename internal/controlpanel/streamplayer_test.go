package controlpanel

import (
	"net/http"
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"testing"
	"time"
)

func TestStreamPlayerActionValidation(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := newInputSource("ProgressiveFisher", -1)
	src.snapshot.Players = []gamestate.RenderPlayer{{Index: 42, Name: "Zezima", X: 2841, Z: 3432}}
	src.snapshot.UI.PlayerOps = [5]string{"Follow", "Attack", "", "Trade with", ""}
	h := inputHarness(t, src, &at, true)
	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1"}), &open)
	res := h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true})
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()
	var frame streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &frame)
	for _, tc := range []struct{ index, op, want int }{{42, 4, 200}, {42, 1, 200}, {42, 3, 400}, {99, 4, 409}, {0, 4, 409}, {42, 6, 400}} {
		at = at.Add(time.Second)
		res = h.postJSON("/api/stream/action", map[string]interface{}{"session": open.Session, "epoch": frame.Epoch, "seq": frame.Seq, "action": streamAction{Kind: "player", Index: tc.index, Op: tc.op}})
		if res.StatusCode != tc.want {
			t.Fatalf("index %d op %d: %d != %d", tc.index, tc.op, res.StatusCode, tc.want)
		}
		res.Body.Close()
	}
	if src.sends != 2 {
		t.Fatalf("invalid player targets sent packets: %d", src.sends)
	}
}

func TestStreamPlayerApproachesBeforeInteracting(t *testing.T) {
	s := &Server{}
	snap := gamestate.RenderSnapshot{X: 3200, Z: 3200, Level: 0}
	ss := &discardSession{state: gamestate.New()}
	interaction := func(ss actions.Session) error { return actions.OpPlayer(ss, 4, 10) }
	if err := s.streamPlayerInteraction(&snap, &gamestate.RenderPlayer{X: 3205, Z: 3200}, interaction)(ss); err != nil {
		t.Fatal(err)
	}
	if ss.sent != 2 {
		t.Fatalf("expected route then player interaction: %d sends", ss.sent)
	}
	ss.sent = 0
	if err := s.streamPlayerInteraction(&snap, &gamestate.RenderPlayer{X: 3201, Z: 3200}, interaction)(ss); err != nil {
		t.Fatal(err)
	}
	if ss.sent != 1 {
		t.Fatal("adjacent target should require only interaction")
	}
}

func TestPlayerOptionsUIDeltaChangesAndClears(t *testing.T) {
	prev := gamestate.RenderUI{}
	next := gamestate.RenderUI{PlayerOps: [5]string{"", "", "", "Trade with", ""}, PlayerOpPriority: [5]bool{false, false, false, true, false}}
	d := diffStreamUI(&prev, &next)
	if d.empty() || d.PlayerOps == nil || d.PlayerOpPriority == nil {
		t.Fatal("player options missing from delta")
	}
	if d := diffStreamUI(&next, &next); !d.empty() {
		t.Fatal("unchanged options resent")
	}
	d = diffStreamUI(&next, &prev)
	if d.PlayerOps == nil || *d.PlayerOps != prev.PlayerOps {
		t.Fatal("cleared options omitted")
	}
}
