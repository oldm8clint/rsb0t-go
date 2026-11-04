// Package client ties transport, login, the ISAAC cipher pair, game state and the
// anti-cheat counters into one live Session: the thing an action layer sends through
// and a read loop feeds.
package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/isaac"
	"rsb0t-go/internal/login"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/transport"
)

// logoutReplyTimeout is how long Logout waits for the server's ServerProt.LOGOUT reply
// (see gamestate.State.IsLoggedOut) before giving up and closing the socket anyway. A
// local-engine round trip is one tick (600ms); this leaves generous headroom for a
// slower server without ever blocking a caller indefinitely if the server refuses the
// logout outright (combat lockout, duel arena — see
// content/scripts/login_logout/logout.rs2) and never replies at all.
const logoutReplyTimeout = 5 * time.Second

// logoutResendEvery spaces the logout clicks sent again while no reply has come. Content's try_logout
// (login_logout/logout.rs2) refuses a player busy in an action, and a socket closed without the reply leaves the
// player in the world: 3 Nov 2026 02:10, all 219 Fishing Trawler crew logged "no ServerProt.LOGOUT reply" after one
// click and stayed in the game. A click every two ticks lands once the action ends.
const logoutResendEvery = 1200 * time.Millisecond

// noTimeoutAfter mirrors Client.ts's own gate exactly: `now - noTimeoutTimer > 1_000`.
const noTimeoutAfter = 1 * time.Second

// keepaliveSlack is how far past noTimeoutAfter the keepalive goroutine wakes, so the gate
// (strictly more than noTimeoutAfter idle) has always passed when it looks.
const keepaliveSlack = 15 * time.Millisecond

// keepaliveRetry is how soon the goroutine looks again when the gate had passed but the
// NO_TIMEOUT could not be written (the connection going), the 200 ms it used to poll at.
const keepaliveRetry = 200 * time.Millisecond

// keepaliveWait is how long the keepalive goroutine sleeps when the socket has been idle for
// idle: until the gate can next pass, or keepaliveRetry when it already has.
func keepaliveWait(idle time.Duration) time.Duration {
	if idle > noTimeoutAfter {
		return keepaliveRetry
	}
	return noTimeoutAfter - idle + keepaliveSlack
}

// Config is everything Connect needs.
type Config struct {
	WSURL    string // e.g. ws://localhost/
	HTTPBase string // e.g. http://localhost/ — for the engine's own cache checksums
	Username string
	Password string
	Modulus  *big.Int
	Exponent *big.Int
	LowMem   bool
	// Log receives one line per notable event (nil = silent).
	Log func(string)

	// Checksums, when set, are the nine archive CRCs sent in the login block, so Connect does
	// not fetch HTTPBase+"crc0". With Modulus and Exponent also given, the game WebSocket is
	// the only connection a login makes. Nil fetches them, as before.
	Checksums *[9]int32
	// NoAntiCheat stops the ANTICHEAT_OPLOGIC side-packets (anticheat.Counters). The zero
	// value sends them, as the TS client does.
	NoAntiCheat bool

	// NetDialContext, when set, opens the game connection's TCP stream — for example a tunnel
	// through a proxy route (internal/proxydial); TLS for a wss URL still runs end to end
	// over it. Nil dials directly, exactly as before.
	NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// HTTPClient, when set, fetches /crc0 (so the fetch leaves through the same route as the
	// game connection). Nil uses the default client, as before.
	HTTPClient *http.Client
	// RouteKey separates the /crc0 cache per route when HTTPClient is set.
	RouteKey string
	// DialTimeout bounds the connect, any proxy handshake and the WebSocket upgrade. Zero
	// sets no bound, as before.
	DialTimeout time.Duration
	// DeviceID is the client id sent in the login block; zero sends login.DefaultDeviceID.
	DeviceID int32
	// Header adds request headers to the game WebSocket's upgrade; nil sends the library's
	// defaults. BrowserHeader builds a browser-like set.
	Header http.Header
}

// loginChecksums returns cfg.Checksums when set, otherwise the engine's own /crc0 (cached).
func loginChecksums(cfg Config) ([9]int32, error) {
	if cfg.Checksums != nil {
		return *cfg.Checksums, nil
	}
	if cfg.HTTPClient != nil {
		return fetchJagChecksumsCachedVia(cfg.HTTPBase, cfg.RouteKey, cfg.HTTPClient)
	}
	return fetchJagChecksumsCached(cfg.HTTPBase)
}

// Session is one logged-in connection.
type Session struct {
	cfg    Config
	stream *transport.Stream
	out    *isaac.Cipher
	in     *isaac.Cipher
	state  *gamestate.State
	cheats *anticheat.Counters

	// sendMu serialises outgoing packets: the outgoing ISAAC cipher is stateful, so two
	// goroutines interleaving P1Enc calls would desync it.
	sendMu sync.Mutex
	// lastSendAt tracks when bytes were last actually written to the socket, guarded
	// by sendMu — the keepalive goroutine (see startKeepalive) compares against this
	// exactly the way Client.ts's own noTimeoutTimer works.
	lastSendAt time.Time

	stopKeepalive chan struct{}
	closeOnce     sync.Once

	// reading is true while a ReadLoop is running on this session, so a second one is
	// refused instead of silently corrupting both. See ErrReaderAlreadyRunning.
	reading atomic.Bool

	StaffModLevel int
	MouseTracked  bool
	LoginResponse int
}

// FetchJagChecksums downloads the engine's own CrcBuffer (cache/CrcTable.ts: 9
// big-endian uint32 archive CRCs + a 10th combined hash the server ignores at login)
// and returns the first 9 verbatim — the server compares raw bytes against its own
// buffer before RSA decryption, so these must not be recomputed independently.
func FetchJagChecksums(httpBase string) ([9]int32, error) {
	return fetchJagChecksumsWith(nil, httpBase)
}

// fetchJagChecksumsWith is FetchJagChecksums through hc (nil: http.Get, as before).
func fetchJagChecksumsWith(hc *http.Client, httpBase string) ([9]int32, error) {
	var out [9]int32
	get := http.Get
	if hc != nil {
		get = hc.Get
	}
	url := httpBase + "crc0"
	resp, err := get(url)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	// Check the status before reading: an error page long enough to pass the length
	// check below would otherwise be decoded as nine archive CRCs and cached. See
	// HTTPStatusError's own doc comment.
	if resp.StatusCode != http.StatusOK {
		return out, &HTTPStatusError{URL: url, Status: resp.Status}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}
	if len(data) < 36 {
		return out, fmt.Errorf("expected >= 36 bytes from /crc0, got %d", len(data))
	}
	for i := 0; i < 9; i++ {
		out[i] = int32(binary.BigEndian.Uint32(data[i*4:]))
	}
	return out, nil
}

// LoginRejectedError is returned when the login server answers with a response code other
// than 2 (success) or 15 (LowMem success). A typed error rather than a plain formatted
// string so a caller can distinguish "the login server's own flood protection" (response
// 16 — confirmed live, and independently documented in cmd/memprobe's own -login-delay
// flag help text, to trigger after roughly 29 logins in a rolling minute) from every other
// rejection (bad password, banned account, etc.), which is not worth retrying at all.
type LoginRejectedError struct {
	Response int
	// RetrySeconds is how long the server said to wait, carried only by the world-hop
	// timer (HopTimerResponse, the one response with a payload byte). 0 for every other
	// rejection and for a hop timer whose second byte lost the race with the close.
	RetrySeconds int
}

func (e *LoginRejectedError) Error() string {
	if e.Response == HopTimerResponse && e.RetrySeconds > 0 {
		return fmt.Sprintf("client: login rejected with response %d (world-hop timer: %ds to wait)", e.Response, e.RetrySeconds)
	}
	if m := ResponseMeaning(e.Response); m != "" {
		return fmt.Sprintf("client: login rejected with response %d (%s)", e.Response, m)
	}
	return fmt.Sprintf("client: login rejected with response %d", e.Response)
}

// LoginAttemptsExceededResponse is the login server's own flood-protection response code —
// see LoginRejectedError's own doc comment.
const LoginAttemptsExceededResponse = 16

// The login responses multi-world operation turns on, each read from the engine's own
// source on branch 289 rather than inferred from behaviour. All three are ordinary,
// expected answers on a fleet spread across worlds, and none of them is a fault.
const (
	// WorldFullResponse is response 7: this world is at its player cap
	// (engine/src/engine/World.ts, `getTotalPlayers() > Environment.node.maxConnected`,
	// where maxConnected defaults to 1000 — the "world 1 has a limit of 1k players now"
	// the fleet owner reported on 2026-09-22). The right answer is ANOTHER WORLD, not a
	// wait: no amount of waiting makes a popular world smaller, and reading it as a rate
	// limit would penalise the whole fleet for one world being busy.
	//
	// Note the comparison is `>`, not `>=`, so the world admits maxConnected+1 players.
	WorldFullResponse = 7

	// AlreadyLoggedInResponse is response 5: this account is logged in somewhere. Two
	// distinct causes, both real when hopping — the LOGIN SERVER's cross-world
	// `account.logged_in !== 0` check (World.ts maps its reply 3 to this), and the world
	// node's own `logoutRequests.has(username)`, i.e. the previous session is still
	// finishing its logout on this very world. CLAUDE.md's rule is to stop the account
	// rather than retry into it.
	AlreadyLoggedInResponse = 5

	// HopTimerResponse is response 21: this account logged out of a DIFFERENT world too
	// recently. See internal/login.HopTimerResponse for the full transcript, including the
	// 45-second default and the fact that staff accounts are exempt — which is why a local
	// lab engine can never reproduce it.
	HopTimerResponse = login.HopTimerResponse
)

// Connect dials, logs in, and reads the two post-success bytes (staffModLevel,
// mouseTracked). It returns an error for any login response other than 2/15 so a
// caller never ends up with a half-open session. Call ReadLoop next.
func Connect(cfg Config) (*Session, error) {
	checksums, err := loginChecksums(cfg)
	if err != nil {
		return nil, &ConnectError{Stage: StageChecksums, Err: err}
	}
	stream, err := transport.DialWith(cfg.WSURL, transport.DialOptions{NetDialContext: cfg.NetDialContext, HandshakeTimeout: cfg.DialTimeout, Header: cfg.Header})
	if err != nil {
		return nil, &ConnectError{Stage: StageDial, Err: err}
	}
	res, err := login.PerformLogin(stream, login.Options{
		Username:     cfg.Username,
		Password:     cfg.Password,
		LowMem:       cfg.LowMem,
		JagChecksums: checksums,
		Modulus:      cfg.Modulus,
		Exponent:     cfg.Exponent,
		DeviceID:     cfg.DeviceID,
	})
	if err != nil {
		stream.Close()
		return nil, err
	}
	if res.Response != 2 && res.Response != 15 {
		stream.Close()
		return nil, &LoginRejectedError{Response: res.Response, RetrySeconds: res.RetrySeconds}
	}

	s := &Session{
		cfg:           cfg,
		stream:        stream,
		out:           res.OutCipher,
		in:            res.InCipher,
		state:         gamestate.New(),
		cheats:        &anticheat.Counters{},
		lastSendAt:    time.Now(),
		stopKeepalive: make(chan struct{}),
		LoginResponse: res.Response,
	}
	s.cheats.Reset()
	s.cheats.SetEnabled(!cfg.NoAntiCheat)

	staff, err := stream.ReadByte()
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("client: staffModLevel: %w", err)
	}
	mouse, err := stream.ReadByte()
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("client: mouseTracked: %w", err)
	}
	s.StaffModLevel = int(staff)
	s.MouseTracked = mouse == 1
	s.logf("logged in (response %d, staffModLevel %d, mouseTracked %v)", res.Response, s.StaffModLevel, s.MouseTracked)

	go s.runKeepalive()
	return s, nil
}

// runKeepalive ports Client.ts's own idle-keepalive exactly: whenever more than
// noTimeoutAfter has passed since the last real write to the socket, send a bare
// NO_TIMEOUT byte. Without this, the engine's own idle-disconnect
// (World.ts's TIMEOUT_NO_RESPONSE, ~100 ticks / 60s of receiving nothing) drops any
// session that isn't otherwise sending action packets constantly — confirmed live
// (three idle sessions against the local engine were all dropped at 61s before this
// fix existed). A real client sends this on every render frame it's otherwise idle,
// which is far more often than once a second. The goroutine sleeps until the gate can
// next pass (keepaliveWait) rather than polling: it polled every 200 ms, and with ~330
// sessions in one process those wake-ups were a steady share of the control panel's
// scheduler time (live 73ff4228, 5 Oct 2026). A send by anything else moves the gate, and
// the goroutine finds that when it wakes and sleeps again.
func (s *Session) runKeepalive() {
	timer := time.NewTimer(noTimeoutAfter + keepaliveSlack)
	defer timer.Stop()
	for {
		select {
		case <-s.stopKeepalive:
			return
		case <-timer.C:
			idle := s.idleFor()
			if idle > noTimeoutAfter {
				// Errors here just mean the connection is already going/gone —
				// ReadLoop's own error return is what a caller should act on.
				_ = s.Send(func(w *packet.Packet) {
					w.P1Enc(clientprot.NO_TIMEOUT)
				})
				idle = s.idleFor()
			}
			timer.Reset(keepaliveWait(idle))
		}
	}
}

// idleFor is how long ago bytes were last written to the socket.
func (s *Session) idleFor() time.Duration {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return time.Since(s.lastSendAt)
}

func (s *Session) logf(format string, args ...any) {
	if s.cfg.Log != nil {
		s.cfg.Log(fmt.Sprintf("[%s] ", s.cfg.Username) + fmt.Sprintf(format, args...))
	}
}

func (s *Session) State() *gamestate.State     { return s.state }
func (s *Session) Cheats() *anticheat.Counters { return s.cheats }
func (s *Session) Username() string            { return s.cfg.Username }

// Close stops the keepalive goroutine and closes the underlying stream. Safe to call
// more than once.
func (s *Session) Close() error {
	s.closeOnce.Do(func() { close(s.stopKeepalive) })
	return s.stream.Close()
}

// Logout ports the real client-side logout, replacing a bare Close() with what a real
// client actually does when the player clicks the logout tab's button: send the click
// (internal/actions.Logout, an IF_BUTTON on the real "Click here to logout" component),
// then wait for the server's ServerProt.LOGOUT reply before closing the socket — the
// same order Client.ts follows (its own private logout() only runs from that reply,
// see actions.Logout's doc comment for the full trace through Client.ts).
//
// Closing the raw socket without this (every live test tool's previous defer s.Close())
// leaves the account "logged in" server-side until its own idle timeout expires, so a
// fast reconnect gets login response 5 ("already logged in, try again in 60 secs")
// instead of a fresh response 2 — confirmed live, see cmd/logouttest.
//
// The caller's own ReadLoop goroutine must still be running when this is called — that
// is what observes the LOGOUT packet and sets gamestate.State.IsLoggedOut; Logout only
// polls that flag, it does not read the socket itself. If the server never replies
// (combat lockout, duel arena, a gnomeball pitch — content/scripts/login_logout/logout.rs2)
// this still closes the socket once logoutReplyTimeout elapses, matching a plain
// Close()'s behavior as a fallback rather than hanging forever.
func (s *Session) Logout() error {
	sendErr := actions.Logout(s)
	if sendErr != nil {
		s.logf("logout: send failed: %v", sendErr)
	} else {
		deadline := time.Now().Add(logoutReplyTimeout)
		resendAt := time.Now().Add(logoutResendEvery)
		for time.Now().Before(deadline) && !s.state.IsLoggedOut() {
			if time.Now().After(resendAt) {
				_ = actions.Logout(s) // the server may have refused the last click mid-action (logoutResendEvery)
				resendAt = time.Now().Add(logoutResendEvery)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !s.state.IsLoggedOut() {
			s.logf("logout: no ServerProt.LOGOUT reply within %s; closing anyway", logoutReplyTimeout)
		}
	}
	closeErr := s.Close()
	if sendErr != nil {
		return sendErr
	}
	return closeErr
}

// Send builds one outgoing packet with the session's ISAAC-keyed writer and writes it.
// The builder's first write should be P1Enc(opcode) (plus any anti-cheat side-packet
// the action layer emits before it), exactly like Client.ts's `this.out`. Every
// successful write resets the keepalive clock — see runKeepalive.
func (s *Session) Send(build func(w *packet.Packet)) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	w := packet.NewWriter(64)
	w.Random = s.out
	build(w)
	err := s.stream.Write(w.Data[:w.Pos])
	if err == nil {
		s.lastSendAt = time.Now()
	}
	return err
}

// Packet is one decoded incoming packet.
type Packet struct {
	Opcode  int
	Payload []byte
}

// ReadPacket reads, ISAAC-decodes and length-frames exactly one server packet. This is
// tcpInDispatch's framing half; the bookkeeping (a wrong VARBYTE/VARSHORT length would
// desync every following packet) is why it never guesses — an unknown-size opcode is a
// hard error, not a skip.
func (s *Session) ReadPacket() (Packet, error) {
	wire, err := s.stream.ReadByte()
	if err != nil {
		return Packet{}, err
	}
	opcode := int(byte(int32(wire) - s.in.NextInt()))

	size := serverprot.ServerProtSizes[opcode]
	var length int
	switch {
	case size >= 0:
		length = size
	case size == -1:
		b, err := s.stream.ReadByte()
		if err != nil {
			return Packet{}, err
		}
		length = int(b)
	case size == -2:
		var lb [2]byte
		if err := s.stream.ReadFull(lb[:]); err != nil {
			return Packet{}, err
		}
		length = int(lb[0])<<8 | int(lb[1])
	}

	payload := make([]byte, length)
	if err := s.stream.ReadFull(payload); err != nil {
		return Packet{}, err
	}
	return Packet{Opcode: opcode, Payload: payload}, nil
}

// ErrReaderAlreadyRunning is what a second, concurrent ReadLoop on one Session gets back.
//
// It exists because the failure it replaces is silent. ReadPacket reads a packet's header and
// its payload as separate reads of the shared stream, so two readers interleave inside one
// packet and each hands State.Handle a frame assembled partly out of the other's bytes. Nothing
// returns an error: the length taken from one reader's header is applied to the other's
// payload, State.Handle holds the gamestate lock while decoding a frame that was never a frame,
// and every other goroutine — the bot, the watchdog's own heartbeat, anything reading a tile —
// blocks behind that lock forever. What it looks like from outside is a tool that prints
// nothing and never returns. Two separate sessions lost hours to exactly this in one day
// (COLLAB.md, 2026-09-20, cmd/woodcuttest: connect started one reader and main added a second
// for chat), the second of them only finding it with a goroutine dump.
//
// The fix for a caller that hits this is never a second loop: pass what the extra reader wanted
// as ReadLoop's own onPacket callback, which sees every packet the one reader decodes.
var ErrReaderAlreadyRunning = errors.New("client: a ReadLoop is already running on this session — a second reader interleaves with the first inside ReadPacket (the header and the payload are separate reads) and hands the game state frames built from both readers' bytes, which deadlocks silently rather than failing; run exactly one ReadLoop per session and use its onPacket callback instead")

// ReadLoop reads packets until the stream closes or stop is closed, feeding each into
// the game state. onPacket (optional) sees every packet after the state has applied it.
//
// Exactly one ReadLoop may run on a Session at a time; a second concurrent call returns
// ErrReaderAlreadyRunning without reading a byte. The flag is released on every return, so
// calling ReadLoop again after a previous one has finished is unaffected — which is what
// internal/fleet's reconnect path does (startReadLoop runs one loop per Session, and a
// reconnect builds a new Session), and is the only pattern the live fleet uses.
func (s *Session) ReadLoop(stop <-chan struct{}, onPacket func(Packet)) error {
	if !s.reading.CompareAndSwap(false, true) {
		return ErrReaderAlreadyRunning
	}
	defer s.reading.Store(false)
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		p, err := s.ReadPacket()
		if err != nil {
			return err
		}
		s.handleSafely(p)
		if p.Opcode == serverprot.REBUILD_NORMAL {
			// The real client only sends this once its own scene-build sequence
			// finishes (Client.ts's checkScene(), gated on every mapsquare's
			// ground/location data having actually arrived via on-demand fetch —
			// see MAP_BUILD_COMPLETE's own opcode, verified present in our own
			// generated clientprot table but never sent anywhere in this codebase
			// until now). This client has no equivalent load step at all: the
			// full collision pack and loc index are already loaded once at
			// startup, not fetched per-zone, so there is nothing to wait for —
			// acknowledging immediately is the correct behavior for this
			// architecture, not a shortcut past a real wait. Found live
			// 2026-09-15 while chasing a long-standing, previously-unsolved
			// position-desync bug (see COLLAB.md): every account that ever
			// stalled mid-walk had crossed at least one REBUILD_NORMAL first,
			// and this acknowledgment was completely absent — the server may
			// reasonably expect it before trusting further position updates
			// from this connection are caught up with the new zone.
			_ = s.Send(func(w *packet.Packet) { w.P1Enc(clientprot.MAP_BUILD_COMPLETE) })
		}
		if onPacket != nil {
			s.onPacketSafely(onPacket, p)
		}
	}
}

// onPacketSafely is handleSafely for the caller's per-packet callback: live 3 Oct 14:47 the fleet's
// callback read a component id from an empty UPDATE_INV payload and the panic took down every
// account in the process.
func (s *Session) onPacketSafely(onPacket func(Packet), p Packet) {
	defer func() {
		if r := recover(); r != nil {
			if s.cfg.Log != nil {
				s.cfg.Log(fmt.Sprintf("recovered a panic in the packet callback for opcode %d (payload %d bytes): %v", p.Opcode, len(p.Payload), r))
			}
		}
	}()
	onPacket(p)
}

// handleSafely calls State.Handle for one packet, recovering a panic instead of letting it
// take down the whole process. Found live (2026-09-16, testing the ProgressiveThiever/
// ProgressiveWoodcutter ports against a local engine): two distinct "index out of range"
// panics, both from a packet decoder reading past the end of a payload it assumed was longer
// than the server actually sent — internal/gamestate.(*State).applyNpcInfo's GBit(8) call (an
// NPC_INFO payload apparently truncated or empty) and internal/gamestate.(*Inventory).
// applyPartial's readObjID (an INV_PARTIAL payload one byte short of a full slot record).
// Neither reproduced in a way this session could pin down a root cause for (a genuinely
// malformed/truncated server payload, a framing desync somewhere upstream in ReadPacket, or a
// real decoder bug for some rare update shape) — but a single edge-case packet crashing an
// entire fleet process outright, for every script equally, is a far worse failure mode than
// dropping that one packet and letting the next one resync the state, the same "fail safe, not
// fail catastrophically" judgment call this codebase already makes for other rare, not-fully-
// root-caused live edge cases (see e.g. randomevents/handle.go's own defensive recoveries).
// This does NOT touch any decoding logic — only what happens when it panics.
func (s *Session) handleSafely(p Packet) {
	defer func() {
		if r := recover(); r != nil {
			if s.cfg.Log != nil {
				s.cfg.Log(fmt.Sprintf("recovered a panic decoding opcode %d (payload %d bytes): %v", p.Opcode, len(p.Payload), r))
			}
		}
	}()
	s.state.Handle(p.Opcode, p.Payload)
}
