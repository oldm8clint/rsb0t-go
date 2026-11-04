package sessionipc

import (
	"bytes"
	"errors"
	"net"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/isaac"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
)

// recordingSession is a real actions.Session — used on both sides of these tests, never
// a special-cased test double with different behaviour than a real *client.Session would
// have. Its Send builds a fresh *packet.Packet against its own real *isaac.Cipher (the
// same field client.Session itself feeds Send's writer from) and records the exact bytes
// produced, so a test can compare "called directly" output against "called through
// RemoteSession, over a real loopback connection, replayed host-side" output byte for
// byte — the strongest evidence available that the remote path is not just reachable but
// actually correct.
type recordingSession struct {
	cipher *isaac.Cipher
	state  *gamestate.State
	cheats *anticheat.Counters
	sent   [][]byte
}

func newRecordingSession(seed []int32) *recordingSession {
	return &recordingSession{cipher: isaac.NewCipher(seed), state: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (s *recordingSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	w.Random = s.cipher
	build(w)
	s.sent = append(s.sent, append([]byte(nil), w.Data[:w.Pos]...))
	return nil
}
func (s *recordingSession) State() *gamestate.State     { return s.state }
func (s *recordingSession) Cheats() *anticheat.Counters { return s.cheats }

// seed is shared by every test below: what matters is that the "direct" and "host" sides
// start from the IDENTICAL cipher seed, standing in for the same real session's cipher —
// the test proves the remote path reproduces what that one real session would have sent,
// not that any two arbitrary ciphers happen to agree.
var testSeed = []int32{11, 22, 33, 44}

// harness wires one host (Serve + Dispatcher, backed by a recordingSession standing in
// for the real *client.Session) and hands back a dial func so a test can open as many
// RemoteSession "brain generations" against it as it wants, proving the host survives
// however many times the caller reconnects — see TestRemoteSessionSurvivesBrainRestart.
type harness struct {
	host *recordingSession
	ln   net.Listener
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	host := newRecordingSession(testSeed)
	disp := NewDispatcher()
	RegisterActionHandlers(disp, func(account string) actions.Session { return host })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = Serve(ln, disp) }()
	t.Cleanup(func() { _ = ln.Close() })
	return &harness{host: host, ln: ln}
}

// dial opens one RemoteSession "brain generation" against h's still-running host.
func (h *harness) dial(t *testing.T, account string) (*RemoteSession, *Client) {
	t.Helper()
	client, err := Dial("tcp", h.ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return NewRemoteSession(account, client, NewStateMirror()), client
}

// TestRemoteSessionIfButtonMatchesDirectCall proves the whole point of this file: calling
// the real, completely unmodified actions.IfButton against a RemoteSession — over a real
// loopback connection, decoded, dispatched, and replayed host-side against a second real
// session — produces the exact same wire bytes as calling it directly against a session
// with an identically-seeded cipher. No script or actions.go change made this pass.
func TestRemoteSessionIfButtonMatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	if err := actions.IfButton(direct, 1337); err != nil {
		t.Fatalf("direct IfButton: %v", err)
	}
	if err := actions.IfButton(remote, 1337); err != nil {
		t.Fatalf("remote IfButton: %v", err)
	}

	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionWalkMatchesDirectCall is the same proof for actions.Walk — a
// multi-waypoint, variable-length packet, unlike IfButton's fixed 3 bytes — confirming
// decodeSend's length/size-byte handling round-trips a real route, not just the simplest
// possible case.
func TestRemoteSessionWalkMatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	waypoints := []pathfind.NavPoint{{X: 3200, Z: 3200}, {X: 3202, Z: 3201}, {X: 3205, Z: 3198}}

	if err := actions.Walk(direct, waypoints, true); err != nil {
		t.Fatalf("direct Walk: %v", err)
	}
	if err := actions.Walk(remote, waypoints, true); err != nil {
		t.Fatalf("remote Walk: %v", err)
	}
	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionOpLocMatchesDirectCall proves actions.OpLoc — HOTRELOAD.md's own
// "Concrete next step" growth of the allowlist, chosen because it's the one action
// BarbarianAgility's real obstacle-clearing loop actually calls (see
// internal/scripts/barbarianrunner) — round-trips byte for byte, the same proof already
// established for IfButton/Walk.
func TestRemoteSessionOpLocMatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	if err := actions.OpLoc(direct, 2, 3204, 3212, 14901); err != nil {
		t.Fatalf("direct OpLoc: %v", err)
	}
	if err := actions.OpLoc(remote, 2, 3204, 3212, 14901); err != nil {
		t.Fatalf("remote OpLoc: %v", err)
	}
	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionOpHeldMatchesDirectCall is the same proof for actions.OpHeld, which
// actions.Eat/actions.Bury/Equipment.Equip all delegate to unmodified — see equipment.go
// and bury.go's own doc comments.
func TestRemoteSessionOpHeldMatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	if err := actions.OpHeld(direct, 1, 379, 5, 3214); err != nil {
		t.Fatalf("direct OpHeld: %v", err)
	}
	if err := actions.OpHeld(remote, 1, 379, 5, 3214); err != nil {
		t.Fatalf("remote OpHeld: %v", err)
	}
	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionOpNpcMatchesDirectCall proves actions.OpNpc — one of this project's
// further allowlist growth: checked live against fisherrunner/essencerunner/
// fighterrunner/flinchrunner, every currently-registered script that talks to or attacks
// an NPC calls this — round-trips byte for byte, same as OpLoc/OpHeld before it.
func TestRemoteSessionOpNpcMatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	if err := actions.OpNpc(direct, 3, 42); err != nil {
		t.Fatalf("direct OpNpc: %v", err)
	}
	if err := actions.OpNpc(remote, 3, 42); err != nil {
		t.Fatalf("remote OpNpc: %v", err)
	}
	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionOpObjMatchesDirectCall proves actions.OpObj — the same shape as OpLoc,
// and what actions.TakeGroundItem/InteractGroundItem delegate to unmodified (fighterrunner
// calls TakeGroundItem, so this covers that call site with zero further changes there).
func TestRemoteSessionOpObjMatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	if err := actions.OpObj(direct, 3, 3210, 3220, 995); err != nil {
		t.Fatalf("direct OpObj: %v", err)
	}
	if err := actions.OpObj(remote, 3, 3210, 3220, 995); err != nil {
		t.Fatalf("remote OpObj: %v", err)
	}
	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionOpLocUMatchesDirectCall proves actions.OpLocU — a loc used with an
// inventory item (e.g. crafterrunner/firstday.go's bank-deposit-box interactions), a
// single fixed opcode rather than a 5-option family, and the widest packet in the
// allowlist so far (13 bytes, six P2 fields).
func TestRemoteSessionOpLocUMatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	if err := actions.OpLocU(direct, 3204, 3212, 14901, 379, 5, 3214); err != nil {
		t.Fatalf("direct OpLocU: %v", err)
	}
	if err := actions.OpLocU(remote, 3204, 3212, 14901, 379, 5, 3214); err != nil {
		t.Fatalf("remote OpLocU: %v", err)
	}
	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionInvButtonOption2MatchesDirectCall proves actions.InvButton for a
// non-1 option (a bank withdraw-5, say) round-trips correctly — the case decodeSend can
// safely decode because, unlike option 1, it never touches Cheats().
func TestRemoteSessionInvButtonOption2MatchesDirectCall(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	if err := actions.InvButton(direct, 2, 995, 3, 5382); err != nil {
		t.Fatalf("direct InvButton: %v", err)
	}
	if err := actions.InvButton(remote, 2, 995, 3, 5382); err != nil {
		t.Fatalf("remote InvButton: %v", err)
	}
	if len(direct.sent) != 1 || len(h.host.sent) != 1 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 1 each", len(direct.sent), len(h.host.sent))
	}
	if !bytes.Equal(direct.sent[0], h.host.sent[0]) {
		t.Fatalf("bytes differ:\n direct=% x\n remote=% x", direct.sent[0], h.host.sent[0])
	}
}

// TestRemoteSessionInvButtonOption1FailsClearly proves option 1 — the one InvButton case
// that touches Cheats().BeforeInvButton1 before its own P1Enc, making the generic
// raw[0]==opcode decode trick unsafe (see remote.go's invButtonOpcodeOption doc comment)
// — is refused rather than silently mis-decoded as some other action.
func TestRemoteSessionInvButtonOption1FailsClearly(t *testing.T) {
	h := newHarness(t)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	err := actions.InvButton(remote, 1, 995, 3, 5382)
	if err == nil {
		t.Fatal("InvButton option 1 over RemoteSession: want an error, got nil")
	}
	if !errors.Is(err, errNoRemoteHandler) {
		t.Fatalf("InvButton option 1 error = %v, want it to wrap errNoRemoteHandler", err)
	}
	if len(h.host.sent) != 0 {
		t.Fatalf("host.sent = %d, want 0 — option 1 must reach the host never, not with the wrong bytes", len(h.host.sent))
	}
}

// TestRemoteSessionUnsupportedActionFailsClearly proves an action outside decodeSend's
// curated allowlist (CloseModal — no Cheats() dependency, but genuinely just not
// implemented yet) fails loudly instead of silently sending nothing or the wrong bytes,
// the safety property this whole design depends on until Phase 3 fills the table out.
func TestRemoteSessionUnsupportedActionFailsClearly(t *testing.T) {
	h := newHarness(t)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	err := actions.CloseModal(remote)
	if err == nil {
		t.Fatal("CloseModal over RemoteSession: want an error, got nil")
	}
	if !errors.Is(err, errNoRemoteHandler) {
		t.Fatalf("CloseModal error = %v, want it to wrap errNoRemoteHandler", err)
	}
	// The host must never have been asked to do anything on this account's behalf — a
	// silently-dropped call would be far worse than a loud one.
	if len(h.host.sent) != 0 {
		t.Fatalf("host.sent = %d, want 0 — an unsupported action must reach the host never, not silently", len(h.host.sent))
	}
}

// TestRemoteSessionSurvivesBrainRestart is this file's actual hot-reload proof: two
// independent RemoteSession "brain generations" (separate *Client connections, separate
// StateMirrors — standing in for a brain process that was killed and restarted) both
// talk to the SAME still-running host, and the host's own cipher — real, stateful,
// sequential — keeps advancing correctly across that restart exactly as it would have
// for two consecutive calls on one long-lived connection. This is the literal mechanism
// HOTRELOAD.md's whole design depends on: the host process, and the live account it
// holds, never restarts merely because the brain did.
func TestRemoteSessionSurvivesBrainRestart(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)

	// Generation 1.
	remote1, client1 := h.dial(t, "acct1")
	if err := actions.IfButton(direct, 10); err != nil {
		t.Fatalf("direct call 1: %v", err)
	}
	if err := actions.IfButton(remote1, 10); err != nil {
		t.Fatalf("remote gen1 call: %v", err)
	}
	client1.Close() // stands in for the brain process exiting to pick up a code fix

	// Generation 2: a brand new connection/RemoteSession, same still-running host.
	remote2, client2 := h.dial(t, "acct1")
	defer client2.Close()
	if err := actions.IfButton(direct, 20); err != nil {
		t.Fatalf("direct call 2: %v", err)
	}
	if err := actions.IfButton(remote2, 20); err != nil {
		t.Fatalf("remote gen2 call: %v", err)
	}

	if len(direct.sent) != 2 || len(h.host.sent) != 2 {
		t.Fatalf("direct.sent=%d host.sent=%d, want 2 each", len(direct.sent), len(h.host.sent))
	}
	for i := range direct.sent {
		if !bytes.Equal(direct.sent[i], h.host.sent[i]) {
			t.Fatalf("call %d bytes differ (cipher desynced across the brain restart):\n direct=% x\n host  =% x", i, direct.sent[i], h.host.sent[i])
		}
	}
}

// TestRemoteSessionStateIsMirror proves State() is exactly the StateMirror it was built
// with, not a copy or a stub — a script reading ctx.Session.State() through RemoteSession
// sees live updates the moment the caller feeds them to the mirror, same as a real
// session's own gamestate.State would from its packet stream.
func TestRemoteSessionStateIsMirror(t *testing.T) {
	mirror := NewStateMirror()
	remote := NewRemoteSession("acct1", nil, mirror)
	if remote.State() != mirror.State() {
		t.Fatal("RemoteSession.State() did not return the mirror's own state")
	}
}

// TestRemoteSessionOpLocKeepsMatchingPastItsSidePacket: OpLoc option 3 carries OPLOGIC2 from
// its 124th call. Over a RemoteSession every call must still decode, and the host's replay
// must add the side-packet exactly where a direct call does.
func TestRemoteSessionOpLocKeepsMatchingPastItsSidePacket(t *testing.T) {
	h := newHarness(t)
	direct := newRecordingSession(testSeed)
	remote, client := h.dial(t, "acct1")
	defer client.Close()

	for i := 1; i <= 130; i++ {
		if err := actions.OpLoc(direct, 3, 3204, 3212, 14901); err != nil {
			t.Fatalf("direct OpLoc %d: %v", i, err)
		}
		if err := actions.OpLoc(remote, 3, 3204, 3212, 14901); err != nil {
			t.Fatalf("remote OpLoc %d: %v", i, err)
		}
		if !bytes.Equal(direct.sent[i-1], h.host.sent[i-1]) {
			t.Fatalf("call %d: bytes differ:\n direct=% x\n remote=% x", i, direct.sent[i-1], h.host.sent[i-1])
		}
	}
	if len(direct.sent[129]) == 7 {
		t.Fatal("control: call 130 should carry the OPLOGIC2 side-packet")
	}
}
