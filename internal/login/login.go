// Package login ports the client-side half of Client.ts's performLogin (the exact byte
// sequence, not the UI/retry state machine around it) plus the ISAAC cipher pair it
// hands off to the rest of the session.
package login

import (
	"fmt"
	"math/big"
	"math/rand"

	"rsb0t-go/internal/isaac"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/transport"
)

// Fresh vs reconnect selects the prefix byte inside the RSA-encrypted login block (10 vs
// 18) and the outer packet's opcode (16 vs 18) — see performLogin's `if (reconnect)`.
const (
	blockFresh     = 10
	blockReconnect = 18
	outerFresh     = 16
	outerReconnect = 18
)

// Options carries everything performLogin needs beyond the live seed exchange.
type Options struct {
	Username     string
	Password     string
	Reconnect    bool
	LowMem       bool
	JagChecksums [9]int32 // the 9 cache/jag archive CRC32s Client.ts sends as this.jagChecksum
	Modulus      *big.Int
	Exponent     *big.Int
	// DeviceID is the 4-byte client id the login block carries after the ISAAC seed
	// (World.ts reads it as `uid` and forwards it to the login server's session row).
	// Zero sends DefaultDeviceID, which is what the TS client and the live server's own web client
	// hard-code for every player.
	DeviceID int32
}

// DefaultDeviceID is the constant client id the TS client sends (Client.ts `this.out.p4(1337)`);
// The live server's production client.js sends the same value.
const DefaultDeviceID int32 = 1337

// Result is what a caller needs once the handshake finishes: the response code (2 =
// success, 15 = reconnect success — see Client.ts's response switch for the rest) and,
// on success, the two ISAAC ciphers the rest of the session's packets are keyed with.
type Result struct {
	Response  int
	OutCipher *isaac.Cipher // seeds every future outgoing packet's opcode obfuscation (Packet.Random)
	InCipher  *isaac.Cipher // decodes every future incoming packet's opcode byte
	// RetrySeconds is the ONE response that carries a payload: 21, the world-hop timer.
	// The engine sends two bytes for it -- `client.send([21, min(255, remaining / 1000)])`
	// in World.ts's onLoginMessage -- where the second is how many seconds the account must
	// wait before it may log in to a different world. Every other response is one byte and
	// leaves this 0.
	//
	// It matters because the wait is long: hopTime defaults to 45 seconds
	// (util/WorldConfig.ts), and the server counts it from the account's LOGOUT time on the
	// previous world, so a hop that logs straight back in is refused rather than delayed.
	// Reading the number the server actually stated beats guessing the default.
	RetrySeconds int
}

// HopTimerResponse is login response 21: "you logged out of another world too recently to
// log in to this one". Read from the engine's own source (engine/src/engine/World.ts's
// onLoginMessage, reply 10 -> `client.send([21, min(255, remaining / 1000)])`, and
// engine/src/server/login/LoginServer.ts's `account.logged_out !== nodeId` branch), on
// branch 289 and NOT a local lab modification -- verified against git show HEAD.
//
// Three things about it that bite anyone building world hopping:
//
//  1. It is the ONLY login response with a payload byte: the seconds remaining.
//  2. The default wait is 45 SECONDS (NODE_HOP_TIME, util/WorldConfig.ts hopTime: 45000),
//     counted from the LOGOUT time on the previous world. A hop that logs back in
//     immediately is refused, not delayed.
//  3. STAFF ARE EXEMPT (`account.staffmodlevel < 2` guards the whole branch). Every local
//     lab account is staffModLevel 2, so a local-engine hop test CANNOT see this response
//     and must not be read as evidence that hopping is timer-free.
const HopTimerResponse = 21

// buildSessionInit is the 2-byte "which login sub-server" probe: p1(14), p1(loginServer).
func buildSessionInit(username string) []byte {
	w := packet.NewWriter(2)
	w.P1(14)
	w.P1(loginServer(username))
	return w.Data[:w.Pos]
}

// buildLoginBlock writes the plaintext block that gets RSA-encrypted: a 1-byte
// fresh/reconnect marker, the 4-int32 ISAAC seed, the client device id, then the
// newline-terminated username and password. Kept separate from encryption so the exact
// plaintext layout is independently testable against a fixed seed.
func buildLoginBlock(seed [4]int32, deviceID int32, username, password string, reconnect bool) *packet.Packet {
	w := packet.NewWriter(64)
	if reconnect {
		w.P1(blockReconnect)
	} else {
		w.P1(blockFresh)
	}
	for _, s := range seed {
		w.P4(s)
	}
	if deviceID == 0 {
		deviceID = DefaultDeviceID
	}
	w.P4(deviceID)
	w.PJStr(username)
	w.PJStr(password)
	return w
}

// buildOuterPacket wraps an already-RSA-encrypted login block (encryptedBlock is
// everything in the login block Packet after RSAEncrypt — i.e. [1-byte length][ciphertext])
// in the real outer login packet: opcode, total length, magic byte, client version,
// low-memory flag, and the 9 cache checksums, exactly matching performLogin's field order.
func buildOuterPacket(encryptedBlock []byte, clientVersion uint16, lowMem bool, checksums [9]int32, reconnect bool) *packet.Packet {
	w := packet.NewWriter(128)
	if reconnect {
		w.P1(outerReconnect)
	} else {
		w.P1(outerFresh)
	}
	// 36 (9x4-byte checksums) + 1 (magic 255) + 2 (version) + 1 (lowmem byte)
	w.P1(byte(len(encryptedBlock) + 36 + 1 + 2 + 1))
	w.P1(255)
	w.P2(clientVersion)
	if lowMem {
		w.P1(1)
	} else {
		w.P1(0)
	}
	for _, c := range checksums {
		w.P4(c)
	}
	w.PData(encryptedBlock, 0, len(encryptedBlock))
	return w
}

// randomSeedHalf mirrors `Math.floor(Math.random() * 99999999)` — a value in [0, 99999999).
func randomSeedHalf() int32 {
	return int32(rand.Int63n(99999999))
}

// derivedSeed splits the server's 8-byte login seed into two int32 halves and prepends
// two client-random int32s, matching performLogin's seed construction exactly.
func derivedSeed(loginSeed int64) [4]int32 {
	return [4]int32{
		randomSeedHalf(),
		randomSeedHalf(),
		int32(loginSeed >> 32),
		int32(loginSeed),
	}
}

// cipherPair builds the outgoing/incoming ISAAC ciphers the way performLogin does:
// outgoing is seeded with the seed as-is, incoming with each word +50 — created only
// after the login packet's plaintext bytes are already built (order matters: doing this
// earlier would be harmless here since Go has no shared mutable Isaac state to race, but
// kept in this order to mirror the source exactly).
func cipherPair(seed [4]int32) (out, in *isaac.Cipher) {
	out = isaac.NewCipher(seed[:])
	inSeed := [4]int32{seed[0] + 50, seed[1] + 50, seed[2] + 50, seed[3] + 50}
	in = isaac.NewCipher(inSeed[:])
	return out, in
}

// PerformLogin drives the exact byte sequence performLogin uses over an already-dialed
// transport.Stream: session-init probe, seed exchange, RSA-encrypted credentials block,
// the wrapping login packet, and the final response byte. It does not implement
// performLogin's UI/retry loop (response 1 = retry after 2s, response 14 = server
// updating, etc.) — callers get the raw response code and decide what to do with it,
// same as Client.ts's caller does after this point in its switch statement.
func PerformLogin(stream *transport.Stream, opts Options) (*Result, error) {
	if err := stream.Write(buildSessionInit(opts.Username)); err != nil {
		return nil, fmt.Errorf("login: writing session-init: %w", err)
	}

	// performLogin discards 8 bytes here before reading the real response byte.
	var discard [8]byte
	if err := stream.ReadFull(discard[:]); err != nil {
		return nil, fmt.Errorf("login: reading 8-byte pad: %w", err)
	}

	respByte, err := stream.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("login: reading session-init response: %w", err)
	}
	response := int(respByte)

	if response != 0 {
		// Anything other than 0 here is a final code already (e.g. an immediate reject) —
		// there is no seed exchange or credentials block to send in that case.
		return &Result{Response: response}, nil
	}

	var seedBytes [8]byte
	if err := stream.ReadFull(seedBytes[:]); err != nil {
		return nil, fmt.Errorf("login: reading login seed: %w", err)
	}
	loginSeed := packet.NewReader(seedBytes[:]).G8()

	seed := derivedSeed(loginSeed)

	block := buildLoginBlock(seed, opts.DeviceID, opts.Username, opts.Password, opts.Reconnect)
	block.RSAEncrypt(opts.Modulus, opts.Exponent)
	encrypted := block.Data[:block.Pos]

	outer := buildOuterPacket(encrypted, uint16(clientprot.ClientVersion), opts.LowMem, opts.JagChecksums, opts.Reconnect)

	outCipher, inCipher := cipherPair(seed)

	if err := stream.Write(outer.Data[:outer.Pos]); err != nil {
		return nil, fmt.Errorf("login: writing login packet: %w", err)
	}

	finalByte, err := stream.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("login: reading final response: %w", err)
	}

	res := &Result{Response: int(finalByte), OutCipher: outCipher, InCipher: inCipher}
	if res.Response == HopTimerResponse {
		// The only two-byte response. Best-effort: the engine closes the socket straight
		// after writing both bytes, so a read that loses the race is normal and must not
		// turn a perfectly clear "wait N seconds" into a read error. A failure leaves
		// RetrySeconds 0, which every caller already has to treat as "the server did not
		// say" -- see Result.RetrySeconds.
		if b, err := stream.ReadByte(); err == nil {
			res.RetrySeconds = int(b)
		}
	}
	return res, nil
}
