package main

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/gorilla/websocket"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/transport"
)

// byteStream is the slice of *transport.Stream that the packet loop actually needs.
//
// Why it is an interface at all: this loop's job is to answer "did opcode N arrive?", and
// a NEGATIVE answer from it is worthless unless the same code has been shown to catch that
// opcode when it IS present. A live server cannot be asked to send one on demand, so the
// only honest proof is to drive this exact function with a synthetic burst in a test. See
// scan_test.go.
type byteStream interface {
	ReadByte() (byte, error)
	ReadFull(dst []byte) error
}

// scanOutcome is what the packet loop saw, in counted terms rather than in log lines.
//
// Quiet and SessionEnded are the fields that make an absence meaningful, and they are
// deliberately two separate signals with different failure modes. Quiet says the byte
// stream stopped (timeout, EOF, a closed socket). SessionEnded says the SERVER said so, by
// sending LOGOUT — a content-level fact that does not depend on how the websocket library
// happens to report a close. Either one means the burst is over, so "not seen" means "not
// sent". A scan that ends for any other reason has proved nothing about what came next.
type scanOutcome struct {
	Scanned      int
	Quiet        bool
	SessionEnded bool
	Err          error
	WatchSeen    bool
	WatchIndex   int
	WatchPayload []byte
}

// Conclusive reports whether an absence observed by this scan is evidence. Without it, a
// scan that stopped early and a scan that watched the session end read exactly alike.
func (r scanOutcome) Conclusive() bool { return r.Quiet || r.SessionEnded }

// scanPackets reads up to limit framed server packets, printing each, and records whether
// watchOp was among them. limit <= 0 reads until the stream ends. nextKey is the incoming
// ISAAC stream (result.InCipher.NextInt), taken as a function so a test can pass a
// zero-key stub and build its burst in plaintext.
func scanPackets(r byteStream, nextKey func() int32, limit, watchOp int, out io.Writer) scanOutcome {
	res := scanOutcome{WatchIndex: -1}

	for i := 0; limit <= 0 || i < limit; i++ {
		wireOp, err := r.ReadByte()
		if err != nil {
			res.Err, res.Quiet = err, endOfBurst(err)
			return res
		}
		opcode := int(byte(int32(wireOp) - nextKey()))

		size := serverprot.ServerProtSizes[opcode]
		var payloadLen int
		switch {
		case size >= 0:
			payloadLen = size
		case size == -1:
			b, err := r.ReadByte()
			if err != nil {
				res.Err, res.Quiet = err, endOfBurst(err)
				return res
			}
			payloadLen = int(b)
		case size == -2:
			var lenBytes [2]byte
			if err := r.ReadFull(lenBytes[:]); err != nil {
				res.Err, res.Quiet = err, endOfBurst(err)
				return res
			}
			payloadLen = int(lenBytes[0])<<8 | int(lenBytes[1])
		}

		payload := make([]byte, payloadLen)
		if err := r.ReadFull(payload); err != nil {
			res.Err, res.Quiet = err, endOfBurst(err)
			return res
		}
		res.Scanned++

		if opcode == serverprot.LOGOUT {
			res.SessionEnded = true
		}

		mark := ""
		if opcode == watchOp {
			mark = "   <== the watched opcode"
			if !res.WatchSeen {
				res.WatchSeen, res.WatchIndex, res.WatchPayload = true, i, payload
			}
		}
		fmt.Fprintf(out, "packet %d: opcode=%d (%s) len=%d%s\n", i, opcode, serverProtName(opcode), payloadLen, mark)
	}
	return res
}

// endOfBurst separates "no more bytes will ever arrive" from a decode failure. All of
// these mean the burst is over rather than that something went wrong with this tool: the
// read deadline passed with the socket still open (transport.ReadTimeout), the stream hit
// EOF, or the connection closed. A websocket close is included because it is how a real
// run actually ends — the engine logs an idle client out and closes, and without this the
// probe would call its own conclusive observation inconclusive.
func endOfBurst(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, transport.ErrReadTimeout) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var closeErr *websocket.CloseError
	return errors.As(err, &closeErr)
}

// lastLoginInfo is the decoded 10-byte LAST_LOGIN_INFO (opcode 253) payload, field for
// field as the engine's own writer emits it — LastLoginInfoEncoder.encode is
// p4(lastLoginIp) p2(daysSinceLogin) p1(daysSinceRecoveryChange) p2(unreadMessageCount)
// pbool(warnMembersInNonMembers), and Player.lastLoginInfo() supplies the values.
type lastLoginInfo struct {
	LastLoginIP             uint32
	DaysSinceLogin          int
	DaysSinceRecoveryChange int
	UnreadMessageCount      int
	WarnMembersInNonMembers bool
}

func decodeLastLoginInfo(payload []byte) (lastLoginInfo, error) {
	var info lastLoginInfo
	if len(payload) != 10 {
		return info, fmt.Errorf("LAST_LOGIN_INFO is 10 bytes, got %d", len(payload))
	}
	r := packet.NewReader(payload)
	info.LastLoginIP = uint32(r.G4())
	info.DaysSinceLogin = int(r.G2())
	info.DaysSinceRecoveryChange = int(r.G1())
	info.UnreadMessageCount = int(r.G2())
	info.WarnMembersInNonMembers = r.G1() == 1
	return info, nil
}

// IPString renders the p4 address the way the engine wrote it (its own constant is
// 2130706433 = 127.0.0.1).
func (l lastLoginInfo) IPString() string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(l.LastLoginIP>>24), byte(l.LastLoginIP>>16), byte(l.LastLoginIP>>8), byte(l.LastLoginIP))
}

// reportWatch states the outcome in terms the caller can act on, and refuses to let an
// absence read as proof unless the burst actually ended.
func reportWatch(out io.Writer, res scanOutcome, watchOp, limit int) {
	name := serverProtName(watchOp)

	if !res.WatchSeen {
		fmt.Fprintf(out, "\nNOT SEEN: opcode %d (%s) did not arrive in %d scanned packet(s).\n", watchOp, name, res.Scanned)
		if res.Conclusive() {
			fmt.Fprintf(out, "  Ended: %s — that was the whole session, so it was NOT SENT.\n", whyItEnded(res))
			if watchOp == serverprot.LAST_LOGIN_INFO {
				fmt.Fprintln(out, "  => map_live is FALSE on this world. content's tutorial skip (runescape_guide.rs2:11,")
				fmt.Fprintln(out, "     `if (map_live = false)`) EXISTS here, so a fresh account can self-serve the tutorial.")
			}
			return
		}
		if limit > 0 && res.Scanned >= limit {
			fmt.Fprintf(out, "  But the scan stopped at its own -packets limit of %d, NOT because the session ended.\n", limit)
			fmt.Fprintln(out, "  This proves nothing about what came next. Raise -packets (0 reads to the end) and re-run.")
			return
		}
		fmt.Fprintf(out, "  But the scan stopped on a read error (%v), NOT on the session ending.\n", res.Err)
		fmt.Fprintln(out, "  This proves nothing about what came next. Re-run it.")
		return
	}

	fmt.Fprintf(out, "\nSEEN: opcode %d (%s) arrived as packet %d, %d payload byte(s).\n", watchOp, name, res.WatchIndex, len(res.WatchPayload))
	if watchOp != serverprot.LAST_LOGIN_INFO {
		return
	}

	info, err := decodeLastLoginInfo(res.WatchPayload)
	if err != nil {
		fmt.Fprintf(out, "  could not decode it: %v\n", err)
		return
	}
	fmt.Fprintf(out, "  lastLoginIp=%s daysSinceLogin=%d daysSinceRecoveryChange=%d unreadMessages=%d warnMembersInNonMembers=%v\n",
		info.IPString(), info.DaysSinceLogin, info.DaysSinceRecoveryChange, info.UnreadMessageCount, info.WarnMembersInNonMembers)
	fmt.Fprintln(out, "  => map_live is TRUE on this world, because login.rs2:88 runs last_login_info only then.")
	fmt.Fprintln(out, "     The tutorial skip branch (`if (map_live = false)`) DOES NOT EXIST here, so no client")
	fmt.Fprintln(out, "     change can let a fresh account finish the tutorial by talking to the guide.")

	// Why the members reading is hedged: the flag is `!Environment.node.members && this.members`
	// (Player.ts:2277), so it is true only on a FREE world holding a members account. False is
	// ambiguous on its own, and UPDATE_PID's members bit is the other half of the pair.
	if info.WarnMembersInNonMembers {
		fmt.Fprintln(out, "  => members: this world is FREE-TO-PLAY (the flag is set only when the world is not")
		fmt.Fprintln(out, "     members but the account is).")
		return
	}
	fmt.Fprintln(out, "  => members: NOT settled by this flag alone — false means either a members world, or a")
	fmt.Fprintln(out, "     free world with a free account. Read UPDATE_PID's members bit (opcode 120, its third")
	fmt.Fprintln(out, "     byte) alongside it: account-members AND this flag false means a MEMBERS world.")
}

// whyItEnded names the signal that ended the session, so the operator can see WHICH of the
// two independent ones fired rather than taking "conclusive" on trust.
func whyItEnded(res scanOutcome) string {
	switch {
	case res.SessionEnded && res.Quiet:
		return fmt.Sprintf("the server sent LOGOUT and then the stream closed (%v)", res.Err)
	case res.SessionEnded:
		return "the server sent LOGOUT"
	default:
		return fmt.Sprintf("the stream ended (%v)", res.Err)
	}
}
