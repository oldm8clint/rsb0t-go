package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/transport"
)

// fakeStream replays a prepared burst and then reports a chosen end condition, standing in
// for *transport.Stream. It exists so the scan can be shown to CATCH an opcode: a live
// server cannot be asked to send a LAST_LOGIN_INFO on demand, so without this the tool's
// "not seen" would be an unfalsifiable claim.
type fakeStream struct {
	data []byte
	pos  int
	end  error
}

func (f *fakeStream) ReadByte() (byte, error) {
	if f.pos >= len(f.data) {
		return 0, f.end
	}
	b := f.data[f.pos]
	f.pos++
	return b, nil
}

func (f *fakeStream) ReadFull(dst []byte) error {
	if f.pos+len(dst) > len(f.data) {
		f.pos = len(f.data)
		return f.end
	}
	copy(dst, f.data[f.pos:f.pos+len(dst)])
	f.pos += len(dst)
	return nil
}

func zeroKey() int32 { return 0 }

// appendPacket frames one packet the way the server does, reading the rule from the same
// table the scan reads, so the fixture cannot drift from the code under test.
func appendPacket(buf []byte, opcode int, payload []byte) []byte {
	buf = append(buf, byte(opcode))
	switch serverprot.ServerProtSizes[opcode] {
	case -1:
		buf = append(buf, byte(len(payload)))
	case -2:
		buf = append(buf, byte(len(payload)>>8), byte(len(payload)))
	}
	return append(buf, payload...)
}

// lastLoginInfoBytes is the engine's own writer, in Go: p4 p2 p1 p2 pbool.
func lastLoginInfoBytes(ip uint32, daysSinceLogin, daysSinceRecovery, unread int, warn bool) []byte {
	out := []byte{
		byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip),
		byte(daysSinceLogin >> 8), byte(daysSinceLogin),
		byte(daysSinceRecovery),
		byte(unread >> 8), byte(unread),
		0,
	}
	if warn {
		out[9] = 1
	}
	return out
}

// buildBurst returns a mixed burst with a LAST_LOGIN_INFO planted late, and the index it
// sits at. The fillers deliberately include VARBYTE (-1) and VARSHORT (-2) packets: a real
// login burst is full of them, and a scan that mis-framed one would desync and never reach
// the packet it is watching for.
func buildBurst(fillers int, tail []byte) ([]byte, int) {
	var buf []byte
	for i := 0; i < fillers; i++ {
		switch i % 3 {
		case 0:
			buf = appendPacket(buf, serverprot.UPDATE_PID, []byte{0, 7, 1})
		case 1:
			buf = appendPacket(buf, 21, bytes.Repeat([]byte{0xAB}, 5))
		default:
			buf = appendPacket(buf, 47, bytes.Repeat([]byte{0xCD}, 300))
		}
	}
	buf = appendPacket(buf, serverprot.LAST_LOGIN_INFO, tail)
	buf = appendPacket(buf, serverprot.UPDATE_PID, []byte{0, 8, 0})
	return buf, fillers
}

// The negative control this whole probe rests on: the scan CATCHES a 253 that arrives long
// after the old hardcoded 15-packet cap, so its absence against a real server means the
// packet was not sent rather than that the loop stopped looking.
func TestScanCatchesALastLoginInfoSentPastTheOldFifteenPacketCap(t *testing.T) {
	payload := lastLoginInfoBytes(2130706433, 5, 201, 3, true)
	burst, at := buildBurst(44, payload)
	if at <= 15 {
		t.Fatalf("the fixture must plant the packet past the old cap, planted at %d", at)
	}

	var out bytes.Buffer
	res := scanPackets(&fakeStream{data: burst, end: io.EOF}, zeroKey, 0, serverprot.LAST_LOGIN_INFO, &out)

	if !res.WatchSeen {
		t.Fatalf("scan missed the planted LAST_LOGIN_INFO: scanned %d, err %v", res.Scanned, res.Err)
	}
	if res.WatchIndex != at {
		t.Errorf("planted at packet %d, reported at %d", at, res.WatchIndex)
	}
	if !bytes.Equal(res.WatchPayload, payload) {
		t.Errorf("payload round-trip failed: got % x want % x", res.WatchPayload, payload)
	}
	if !strings.Contains(out.String(), "opcode=253 (LAST_LOGIN_INFO)") {
		t.Errorf("the operator would not have seen it in the output:\n%s", out.String())
	}

	// And the report reaches the conclusion the owner acts on.
	var verdict bytes.Buffer
	reportWatch(&verdict, res, serverprot.LAST_LOGIN_INFO, 0)
	if !strings.Contains(verdict.String(), "map_live is TRUE") {
		t.Errorf("a seen 253 must state map_live TRUE:\n%s", verdict.String())
	}
	if !strings.Contains(verdict.String(), "FREE-TO-PLAY") {
		t.Errorf("warn=true must be read as a free world:\n%s", verdict.String())
	}
}

// The same burst, scanned with the cap the tool used to have, misses it entirely. This is
// the bug, pinned: it is why one clean production login proved nothing before.
func TestTheOldFifteenPacketCapWouldHaveMissedIt(t *testing.T) {
	burst, _ := buildBurst(44, lastLoginInfoBytes(2130706433, 5, 201, 3, true))

	res := scanPackets(&fakeStream{data: burst, end: io.EOF}, zeroKey, 15, serverprot.LAST_LOGIN_INFO, io.Discard)

	if res.WatchSeen {
		t.Fatal("15 packets should not have reached it; the fixture is wrong")
	}
	if res.Scanned != 15 {
		t.Errorf("scanned %d, want 15", res.Scanned)
	}
	if res.Conclusive() {
		t.Error("stopping at its own limit must NOT count as conclusive, or an absence reads as proof")
	}
}

// Every way a real session actually ends must count as the end of the burst. The websocket
// close is not hypothetical: it is how the live lab run ended, and an earlier version of
// this tool called that conclusive observation inconclusive.
func TestEveryRealEndingCountsAsTheEndOfTheBurst(t *testing.T) {
	endings := []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		transport.ErrReadTimeout,
		net.ErrClosed,
		&websocket.CloseError{Code: websocket.CloseNoStatusReceived},
	}
	for _, end := range endings {
		var buf []byte
		for i := 0; i < 20; i++ {
			buf = appendPacket(buf, serverprot.UPDATE_PID, []byte{0, 7, 1})
		}

		res := scanPackets(&fakeStream{data: buf, end: end}, zeroKey, 200, serverprot.LAST_LOGIN_INFO, io.Discard)

		if res.WatchSeen {
			t.Fatalf("%v: nothing planted, yet reported seen", end)
		}
		if res.Scanned != 20 {
			t.Errorf("%v: scanned %d, want 20", end, res.Scanned)
		}
		if !res.Quiet || !res.Conclusive() {
			t.Errorf("%v: should be recognised as the end of the burst", end)
		}
	}

	// A decode-level failure is NOT an ending, and must not be dressed up as one.
	if endOfBurst(errors.New("some other failure")) {
		t.Error("an arbitrary error must not count as the burst ending")
	}
}

// LOGOUT is a second, independent end-of-session signal: the server said so in content
// terms, which does not depend on how the websocket library reports a close.
func TestALogoutMakesAnAbsenceConclusiveOnItsOwn(t *testing.T) {
	var buf []byte
	for i := 0; i < 5; i++ {
		buf = appendPacket(buf, serverprot.UPDATE_PID, []byte{0, 7, 1})
	}
	buf = appendPacket(buf, serverprot.LOGOUT, nil)

	res := scanPackets(&fakeStream{data: buf, end: errors.New("an error that is not an ending")}, zeroKey, 200, serverprot.LAST_LOGIN_INFO, io.Discard)

	if !res.SessionEnded {
		t.Fatal("a LOGOUT packet must be recorded as the session ending")
	}
	if res.Quiet {
		t.Error("the stream error here is not an ending; only the LOGOUT is")
	}
	if !res.Conclusive() {
		t.Error("a LOGOUT alone must make the absence conclusive")
	}

	var verdict bytes.Buffer
	reportWatch(&verdict, res, serverprot.LAST_LOGIN_INFO, 200)
	if !strings.Contains(verdict.String(), "NOT SENT") || !strings.Contains(verdict.String(), "LOGOUT") {
		t.Errorf("the report must name the signal it relied on:\n%s", verdict.String())
	}
}

// The three absences must not read alike: "not sent", "hit my limit", "read error".
func TestReportDistinguishesNotSentFromStoppedLooking(t *testing.T) {
	var ended, capped, broke bytes.Buffer
	reportWatch(&ended, scanOutcome{Scanned: 20, Quiet: true, Err: io.EOF, WatchIndex: -1}, serverprot.LAST_LOGIN_INFO, 200)
	reportWatch(&capped, scanOutcome{Scanned: 15, WatchIndex: -1}, serverprot.LAST_LOGIN_INFO, 15)
	reportWatch(&broke, scanOutcome{Scanned: 7, Err: errors.New("boom"), WatchIndex: -1}, serverprot.LAST_LOGIN_INFO, 200)

	if !strings.Contains(ended.String(), "NOT SENT") || !strings.Contains(ended.String(), "map_live is FALSE") {
		t.Errorf("an ended session must state the conclusion:\n%s", ended.String())
	}
	for name, buf := range map[string]*bytes.Buffer{"limit": &capped, "read error": &broke} {
		if strings.Contains(buf.String(), "map_live is FALSE") {
			t.Errorf("%s must NOT be reported as a map_live answer:\n%s", name, buf.String())
		}
		if !strings.Contains(buf.String(), "proves nothing") {
			t.Errorf("%s must say it proves nothing:\n%s", name, buf.String())
		}
	}
	if !strings.Contains(capped.String(), "limit of 15") {
		t.Errorf("the limit case must name the limit:\n%s", capped.String())
	}
	if !strings.Contains(broke.String(), "boom") {
		t.Errorf("the read-error case must name the error:\n%s", broke.String())
	}
}

// Field order and widths against the engine's own writer, LastLoginInfoEncoder.encode.
func TestDecodeLastLoginInfoMatchesTheEngineWriter(t *testing.T) {
	info, err := decodeLastLoginInfo(lastLoginInfoBytes(2130706433, 5, 201, 3, true))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := info.IPString(); got != "127.0.0.1" {
		t.Errorf("lastLoginIp %s, want 127.0.0.1 (the engine's own 2130706433)", got)
	}
	if info.DaysSinceLogin != 5 || info.DaysSinceRecoveryChange != 201 || info.UnreadMessageCount != 3 {
		t.Errorf("got days=%d recovery=%d unread=%d, want 5/201/3", info.DaysSinceLogin, info.DaysSinceRecoveryChange, info.UnreadMessageCount)
	}
	if !info.WarnMembersInNonMembers {
		t.Error("warnMembersInNonMembers should decode true")
	}

	// warn=false is the ambiguous case, and the report must not overclaim from it.
	plain, err := decodeLastLoginInfo(lastLoginInfoBytes(2130706433, 0, 201, 0, false))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if plain.WarnMembersInNonMembers {
		t.Error("warnMembersInNonMembers should decode false")
	}
	var verdict bytes.Buffer
	reportWatch(&verdict, scanOutcome{WatchSeen: true, WatchIndex: 300, WatchPayload: lastLoginInfoBytes(2130706433, 0, 201, 0, false)}, serverprot.LAST_LOGIN_INFO, 0)
	if strings.Contains(verdict.String(), "FREE-TO-PLAY") {
		t.Errorf("warn=false must NOT be read as a free world:\n%s", verdict.String())
	}
	if !strings.Contains(verdict.String(), "NOT settled by this flag alone") {
		t.Errorf("warn=false must be reported as ambiguous:\n%s", verdict.String())
	}

	if _, err := decodeLastLoginInfo(make([]byte, 9)); err == nil {
		t.Error("a short payload must be rejected rather than silently misread")
	}
}

// Asserted by RUNNING the table, not by reading it: this is why a 253 frames cleanly on the
// live fleet and causes no desync, and so why nothing noticed it.
func TestLastLoginInfoFramesAsTenFixedBytes(t *testing.T) {
	if got := serverprot.ServerProtSizes[serverprot.LAST_LOGIN_INFO]; got != 10 {
		t.Errorf("ServerProtSizes[253] = %d, want 10 to match ServerGameProt(253, 10)", got)
	}
	if serverProtName(serverprot.LAST_LOGIN_INFO) != "LAST_LOGIN_INFO" {
		t.Error("the reverse name lookup must name it, or the operator cannot spot it in the output")
	}
}
