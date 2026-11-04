package controlpanel

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
)

// inputTestSource is a Source with everything opt-in input needs: an observable session, bot
// control and a session runner that records what it would send. Accounts are guarded because
// the control watchdog reads them from its own goroutine.
type inputTestSource struct {
	*renderSource
	amu     sync.Mutex
	acct    []AccountStatus
	started []string
	sends   int
}

func (s *inputTestSource) Accounts(dst []AccountStatus) []AccountStatus {
	s.amu.Lock()
	defer s.amu.Unlock()
	return append(dst, s.acct...)
}
func (s *inputTestSource) BotScripts() []string { return []string{"ProgressiveFisher", "MuleReceiver"} }
func (s *inputTestSource) StartBot(account, script string, settings map[string]interface{}) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	for i := range s.acct {
		if s.acct[i].Name == account {
			s.acct[i].Bot, s.acct[i].BotState, s.acct[i].Settings = script, "running", settings
			s.started = append(s.started, script)
			return nil
		}
	}
	return fmt.Errorf("no such account")
}
func (s *inputTestSource) StopBot(account string) error {
	s.amu.Lock()
	defer s.amu.Unlock()
	for i := range s.acct {
		if s.acct[i].Name == account {
			s.acct[i].BotState = "stopped"
			return nil
		}
	}
	return fmt.Errorf("no such account")
}
func (s *inputTestSource) WithStreamSession(account string, fn func(actions.Session) error) error {
	d := &discardSession{state: gamestate.New()}
	err := fn(d)
	s.amu.Lock()
	s.sends += d.sent
	s.amu.Unlock()
	return err
}
func (s *inputTestSource) botState() (string, string) {
	s.amu.Lock()
	defer s.amu.Unlock()
	return s.acct[0].Bot, s.acct[0].BotState
}

func newInputSource(bot string, main int) *inputTestSource {
	snap := gamestate.RenderSnapshot{X: 2840, Z: 3432, CentreX: 355, CentreZ: 429}
	snap.UI = gamestate.RenderUI{Version: 1, Main: main, Side: -1, ChatModal: -1}
	snap.NPCs = []gamestate.NPC{{Index: 12, Type: 316, X: 2842, Z: 3433, Anim: -1, FaceEntity: -1}}
	a := AccountStatus{Name: "fisher1", State: StateIngame, SinceUnixMs: 1000, Pos: &Position{X: 2840, Z: 3432}, Bot: bot, BotState: "running", Settings: map[string]interface{}{"k": 1.0}}
	return &inputTestSource{renderSource: &renderSource{staticSource: &staticSource{accounts: []AccountStatus{a}}, snapshot: snap}, acct: []AccountStatus{a}}
}

func inputHarness(t *testing.T, src Source, at *time.Time, enabled bool) *harness {
	t.Helper()
	opts := testOptions()
	opts.now = func() time.Time { return *at }
	return newHarness(t, Config{StreamInput: enabled}, opts, src)
}

func TestStreamInputIsOffWithoutTheFlag(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	h := inputHarness(t, newInputSource("ProgressiveFisher", -1), &at, false)
	var meta struct {
		StreamClient StreamClientCapabilities `json:"streamClient"`
	}
	decode(t, h.get("/api/meta"), &meta)
	if meta.StreamClient.SessionInput {
		t.Fatal("sessionInput advertised without -stream-input")
	}
	if res := h.postJSON("/api/stream/action", map[string]interface{}{"session": "x"}); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("action without the flag = %d, want 501", res.StatusCode)
	}
	if res := h.postJSON("/api/stream/input", map[string]interface{}{"session": "x", "enabled": true}); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("input without the flag = %d, want 501", res.StatusCode)
	}
}

func TestStreamInputTakesControlPausesAndResumesTheBot(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := newInputSource("ProgressiveFisher", -1)
	h := inputHarness(t, src, &at, true)
	var meta struct {
		StreamClient StreamClientCapabilities `json:"streamClient"`
	}
	decode(t, h.get("/api/meta"), &meta)
	if !meta.StreamClient.SessionInput {
		t.Fatal("sessionInput not advertised with -stream-input and a capable source")
	}
	if res := h.postJSON("/api/stream/input", map[string]interface{}{"session": "nope", "enabled": true}); res.StatusCode != http.StatusGone {
		t.Errorf("take control without a lease = %d, want 410", res.StatusCode)
	}
	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1", "transport": "scene-delta"}), &open)
	var took struct {
		Acknowledged bool   `json:"acknowledged"`
		PausedBot    string `json:"pausedBot"`
	}
	decode(t, h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true}), &took)
	if !took.Acknowledged || took.PausedBot != "ProgressiveFisher" {
		t.Fatalf("take control = %+v", took)
	}
	if _, st := src.botState(); st != "stopped" {
		t.Fatalf("bot state after take control = %q, want stopped", st)
	}

	// A frame to act on, then a walk: validated and sent through the session.
	at = at.Add(time.Second)
	var f streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &f)
	act := func(action map[string]interface{}, epoch, seq int64) *http.Response {
		return h.postJSON("/api/stream/action", map[string]interface{}{"session": open.Session, "epoch": epoch, "seq": seq, "action": action})
	}
	if res := act(map[string]interface{}{"kind": "walk", "x": 2845, "z": 3430}, f.Epoch, f.Seq); res.StatusCode != http.StatusOK {
		t.Fatalf("walk = %d", res.StatusCode)
	}
	if src.sends != 1 {
		t.Errorf("sends = %d, want the one walk", src.sends)
	}
	// Faster than one per 300 ms: refused.
	if res := act(map[string]interface{}{"kind": "walk", "x": 2845, "z": 3431}, f.Epoch, f.Seq); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second walk inside 300ms = %d, want 429", res.StatusCode)
	}
	at = at.Add(time.Second)
	for _, c := range []struct {
		name   string
		action map[string]interface{}
		epoch  int64
		seq    int64
		want   int
	}{
		{"out of range", map[string]interface{}{"kind": "walk", "x": 2900, "z": 3430}, f.Epoch, f.Seq, http.StatusBadRequest},
		{"stale epoch", map[string]interface{}{"kind": "walk", "x": 2841, "z": 3430}, f.Epoch + 1, f.Seq, http.StatusConflict},
		{"future observation", map[string]interface{}{"kind": "walk", "x": 2841, "z": 3430}, f.Epoch, f.Seq + 5, http.StatusConflict},
		{"npc not in view", map[string]interface{}{"kind": "npc", "index": 99, "op": 1}, f.Epoch, f.Seq, http.StatusConflict},
		{"npc op unverifiable without names", map[string]interface{}{"kind": "npc", "index": 12, "op": 1}, f.Epoch, f.Seq, http.StatusBadRequest},
		{"unknown kind", map[string]interface{}{"kind": "teleport"}, f.Epoch, f.Seq, http.StatusBadRequest},
	} {
		at = at.Add(time.Second)
		if res := act(c.action, c.epoch, c.seq); res.StatusCode != c.want {
			t.Errorf("%s = %d, want %d", c.name, res.StatusCode, c.want)
		}
	}
	if src.sends != 1 {
		t.Errorf("refused actions sent something: sends = %d", src.sends)
	}

	// Release resumes the remembered bot with its settings.
	decode(t, h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": false}), &took)
	if bot, st := src.botState(); bot != "ProgressiveFisher" || st != "running" {
		t.Fatalf("after release bot = %q %q, want ProgressiveFisher running", bot, st)
	}
	at = at.Add(time.Second)
	if res := act(map[string]interface{}{"kind": "walk", "x": 2841, "z": 3430}, f.Epoch, f.Seq); res.StatusCode != http.StatusConflict {
		t.Errorf("action after release = %d, want 409", res.StatusCode)
	}

	var audit struct {
		Enabled bool               `json:"enabled"`
		Entries []streamAuditEntry `json:"entries"`
	}
	decode(t, h.get("/api/stream/audit"), &audit)
	if !audit.Enabled || len(audit.Entries) < 10 {
		t.Fatalf("audit = %d entries (enabled %v), want every accepted and refused request", len(audit.Entries), audit.Enabled)
	}
}

func TestStreamInputResumesTheBotWhenTheViewGoesAway(t *testing.T) {
	for _, how := range []string{"close", "expiry"} {
		at := time.Unix(1_700_000_000, 0)
		src := newInputSource("ProgressiveFisher", -1)
		h := inputHarness(t, src, &at, true)
		var open streamSessionBody
		decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1"}), &open)
		h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true})
		if _, st := src.botState(); st != "stopped" {
			t.Fatalf("%s: bot not stopped by take control", how)
		}
		if how == "close" {
			h.postJSON("/api/stream/close", map[string]string{"session": open.Session})
		} else {
			at = at.Add(DefaultStreamLeaseTTL + time.Second)
			h.srv.checkStreamControl()
		}
		if _, st := src.botState(); st != "running" {
			t.Errorf("%s: bot state %q after the view went away, want running", how, st)
		}
	}
}

func TestStreamInputRefusesAnOpenInterfaceOrAMuleWithoutForce(t *testing.T) {
	for _, c := range []struct {
		bot  string
		main int
	}{{"ProgressiveFisher", 5292}, {"MuleReceiver", -1}} {
		at := time.Unix(1_700_000_000, 0)
		src := newInputSource(c.bot, c.main)
		h := inputHarness(t, src, &at, true)
		var open streamSessionBody
		decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1"}), &open)
		if res := h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true}); res.StatusCode != http.StatusConflict {
			t.Errorf("%s/main %d: take control = %d, want 409", c.bot, c.main, res.StatusCode)
		}
		if _, st := src.botState(); st != "running" {
			t.Errorf("%s: a refused take control stopped the bot", c.bot)
		}
		var took struct {
			Acknowledged bool `json:"acknowledged"`
		}
		decode(t, h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true, "force": true}), &took)
		if !took.Acknowledged {
			t.Errorf("%s: force did not take control", c.bot)
		}
		h.postJSON("/api/stream/close", map[string]string{"session": open.Session})
	}
}

// failingStart is an inputTestSource whose StartBot always fails.
type failingStart struct{ *inputTestSource }

func (f failingStart) StartBot(string, string, map[string]interface{}) error {
	return fmt.Errorf("script registry unavailable")
}

// TestLostTakeControlRaceReportsAFailedResume: a take-control that paused the bot, then lost
// the claim to another viewer, restarts the bot; if that fails the refusal and the audit log
// both say so rather than leaving the bot silently stopped.
func TestLostTakeControlRaceReportsAFailedResume(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := newInputSource("ProgressiveFisher", -1)
	h := inputHarness(t, src, &at, true)
	control := &streamControl{account: "fisher1", resumeScript: "ProgressiveFisher"}

	msg := h.srv.resumeAfterLostRace(failingStart{src}, "fisher1", control)
	if !strings.Contains(msg, "failed: script registry unavailable") {
		t.Fatalf("refusal = %q, want the failed resume named", msg)
	}
	h.srv.input.mu.Lock()
	last := h.srv.input.audit[len(h.srv.input.audit)-1]
	h.srv.input.mu.Unlock()
	if !strings.Contains(last.Result, "resuming ProgressiveFisher failed") {
		t.Fatalf("audit = %+v, want the failed resume recorded", last)
	}

	if msg := h.srv.resumeAfterLostRace(src, "fisher1", control); msg != "another viewer took control first" {
		t.Fatalf("successful resume: refusal = %q", msg)
	}
	if bot, st := src.botState(); bot != "ProgressiveFisher" || st != "running" {
		t.Fatalf("after a successful resume the bot is %s/%s", bot, st)
	}
}
