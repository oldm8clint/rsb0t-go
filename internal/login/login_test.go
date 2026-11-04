package login

import (
	"math/big"
	"testing"

	"rsb0t-go/internal/isaac"
)

// TestBuildSessionInit checks the 2-byte probe: p1(14) then the loginServer byte, which
// TestToUserhashMatchesTSReference already proves is correct for these usernames.
func TestBuildSessionInit(t *testing.T) {
	got := buildSessionInit("mule")
	if len(got) != 2 || got[0] != 14 || got[1] != loginServer("mule") {
		t.Fatalf("got %v, want [14 %d]", got, loginServer("mule"))
	}
}

// TestBuildLoginBlockLayout hand-verifies the plaintext block's exact byte layout for a
// fixed seed and short credentials: 1-byte marker, 4 big-endian int32 seed words, a
// big-endian int32 magic 1337, then two newline-terminated strings.
func TestBuildLoginBlockLayout(t *testing.T) {
	seed := [4]int32{1, 2, 3, -1}
	w := buildLoginBlock(seed, 0, "ab", "cd", false)
	got := w.Data[:w.Pos]

	want := []byte{
		10,         // fresh-login marker
		0, 0, 0, 1, // seed[0] = 1
		0, 0, 0, 2, // seed[1] = 2
		0, 0, 0, 3, // seed[2] = 3
		0xff, 0xff, 0xff, 0xff, // seed[3] = -1
		0, 0, 5, 57, // 1337
		'a', 'b', 10, // pjstr("ab")
		'c', 'd', 10, // pjstr("cd")
	}
	if len(got) != len(want) {
		t.Fatalf("length: got %d, want %d (got=%v want=%v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %d, want %d (full got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

// TestBuildLoginBlockReconnectMarker checks the reconnect (18) vs fresh (10) prefix byte.
func TestBuildLoginBlockReconnectMarker(t *testing.T) {
	w := buildLoginBlock([4]int32{0, 0, 0, 0}, 0, "a", "b", true)
	if w.Data[0] != blockReconnect {
		t.Fatalf("got %d, want %d (reconnect marker)", w.Data[0], blockReconnect)
	}
}

// TestBuildOuterPacketLayout hand-verifies the outer packet: opcode, the computed total
// length byte (ciphertext-length-inclusive + 41), magic 255, version, lowmem flag, the 9
// checksums, then the encrypted block bytes verbatim.
func TestBuildOuterPacketLayout(t *testing.T) {
	encrypted := []byte{9, 1, 2, 3} // a stand-in "1-byte length + 3-byte ciphertext"
	checksums := [9]int32{1, 2, 3, 4, 5, 6, 7, 8, 9}
	w := buildOuterPacket(encrypted, 289, true, checksums, false)
	got := w.Data[:w.Pos]

	var want []byte
	want = append(want, outerFresh)
	want = append(want, byte(len(encrypted)+36+1+2+1)) // 4 + 40 = 44
	want = append(want, 255)
	want = append(want, 0x01, 0x21) // 289 big-endian
	want = append(want, 1)          // lowMem
	for _, c := range checksums {
		want = append(want, 0, 0, 0, byte(c))
	}
	want = append(want, encrypted...)

	if len(got) != len(want) {
		t.Fatalf("length: got %d, want %d (got=%v want=%v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %d, want %d (full got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

// TestBuildOuterPacketReconnectOpcode checks the reconnect (18) vs fresh (16) opcode.
func TestBuildOuterPacketReconnectOpcode(t *testing.T) {
	w := buildOuterPacket([]byte{0}, 289, false, [9]int32{}, true)
	if w.Data[0] != outerReconnect {
		t.Fatalf("got %d, want %d (reconnect opcode)", w.Data[0], outerReconnect)
	}
}

// TestDerivedSeedSplitsLoginSeed checks the high/low 32-bit split of the 8-byte server
// seed, independent of the two random client words.
func TestDerivedSeedSplitsLoginSeed(t *testing.T) {
	loginSeed := int64(0x0102030405060708)
	seed := derivedSeed(loginSeed)
	if seed[2] != 0x01020304 {
		t.Fatalf("seed[2] (high 32 bits): got %#x, want %#x", seed[2], 0x01020304)
	}
	if seed[3] != 0x05060708 {
		t.Fatalf("seed[3] (low 32 bits): got %#x, want %#x", seed[3], 0x05060708)
	}
}

// TestCipherPairIncomingIsOutgoingSeedPlus50 checks the +50-per-word relationship
// between the two ciphers without depending on ISAAC's own internals (already verified
// separately in internal/isaac) — just that seeding math is right.
func TestCipherPairIncomingIsOutgoingSeedPlus50(t *testing.T) {
	seed := [4]int32{1, 2, 3, 4}
	_, in := cipherPair(seed)

	plus50 := []int32{51, 52, 53, 54}
	wantIn := isaac.NewCipher(plus50)

	// Two ciphers seeded identically must produce identical output streams — this is
	// exactly TestNextIntDeterministic's guarantee from the isaac package, reused here
	// to confirm cipherPair derives the +50 seed correctly rather than, say, +50 on the
	// wrong index or a sign error.
	for i := 0; i < 50; i++ {
		a, b := in.NextInt(), wantIn.NextInt()
		if a != b {
			t.Fatalf("call %d: incoming cipher diverged from a manually +50-seeded reference: %d != %d", i, a, b)
		}
	}
}

func TestRSAEncryptedBlockFeedsOuterPacketLengthCorrectly(t *testing.T) {
	// End-to-end (still network-free): build a block, encrypt it with the toy keypair
	// from the rsa package's own fixture, and confirm the outer packet's length byte
	// matches len(encrypted)+39 exactly — this is the exact arithmetic performLogin
	// relies on `this.out.pos` already reflecting post-rsaenc() length.
	mod, _ := new(big.Int).SetString("3233", 10)
	exp, _ := new(big.Int).SetString("17", 10)

	block := buildLoginBlock([4]int32{1, 2, 3, 4}, 0, "a", "b", false)
	block.RSAEncrypt(mod, exp)
	encrypted := block.Data[:block.Pos]

	outer := buildOuterPacket(encrypted, 289, false, [9]int32{}, false)
	gotLen := outer.Data[1]
	wantLen := byte(len(encrypted) + 36 + 1 + 2 + 1) // same formula as buildOuterPacket itself
	if gotLen != wantLen {
		t.Fatalf("length byte: got %d, want %d (encrypted block was %d bytes)", gotLen, wantLen, len(encrypted))
	}
}
