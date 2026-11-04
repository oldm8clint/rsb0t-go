package packet

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func bigFromString(t *testing.T, s string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("could not parse %q as a base-10 bigint", s)
	}
	return n
}

type namedCase struct {
	Bytes []byte `json:"bytes"`
	Want  int32  `json:"want"`
}

type fixture struct {
	Written struct {
		Bytes []byte `json:"bytes"`
		Len   int    `json:"len"`
	} `json:"written"`
	GSmart           []namedCase `json:"gsmart"`
	GSmarts          []namedCase `json:"gsmarts"`
	CRC32Of123456789 uint32      `json:"crc32_of_123456789"`
	Bits             struct {
		Input  []byte   `json:"input"`
		Values []uint32 `json:"values"`
		EndPos int      `json:"endPos"`
	} `json:"bits"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "packet_vector.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return f
}

// TestWriteMatchesTSReference replays the exact same write sequence the reference
// script ran against the real Packet.ts and checks the resulting bytes match exactly —
// this covers P1/P2/IP2/P3/P4 (including a negative int32)/IP4/P8/PJStr in one pass.
func TestWriteMatchesTSReference(t *testing.T) {
	f := loadFixture(t)

	w := NewWriter(64)
	w.P1(0xab)
	w.P2(0x1234)
	w.IP2(0x1234)
	w.P3(0x123456)
	w.P4(-1)
	w.IP4(-1)
	w.P8(0x0102030405060708)
	w.PJStr("hi")

	got := w.Data[:w.Pos]
	if len(got) != f.Written.Len {
		t.Fatalf("length: got %d, want %d", len(got), f.Written.Len)
	}
	for i := range got {
		if got[i] != f.Written.Bytes[i] {
			t.Fatalf("byte %d: got %d, want %d (full got=%v want=%v)", i, got[i], f.Written.Bytes[i], got, f.Written.Bytes)
		}
	}
}

// TestReadRoundTripsWithWrite exercises the symmetric G/P pairs (G1/P1, G2/P2, G3/P3,
// G4/P4, G8/P8, GJStr/PJStr) by writing then reading back through this port alone —
// IP2/IP4 have no G-side counterpart in the original either, so they're checked only on
// the write side above, matching the source's own asymmetry.
func TestReadRoundTripsWithWrite(t *testing.T) {
	w := NewWriter(64)
	w.P1(0xab)
	w.P2(0x1234)
	w.P3(0x123456)
	w.P4(-1)
	w.P8(0x0102030405060708)
	w.PJStr("hello")

	r := NewReader(w.Data[:w.Pos])
	if v := r.G1(); v != 0xab {
		t.Fatalf("G1: got %d, want 0xab", v)
	}
	if v := r.G2(); v != 0x1234 {
		t.Fatalf("G2: got %#x, want 0x1234", v)
	}
	if v := r.G3(); v != 0x123456 {
		t.Fatalf("G3: got %#x, want 0x123456", v)
	}
	if v := r.G4(); v != -1 {
		t.Fatalf("G4: got %d, want -1", v)
	}
	if v := r.G8(); v != 0x0102030405060708 {
		t.Fatalf("G8: got %#x, want 0x0102030405060708", v)
	}
	if v := r.GJStr(); v != "hello" {
		t.Fatalf("GJStr: got %q, want %q", v, "hello")
	}
}

// TestPSize1Backpatch checks the write-placeholder-then-backfill pattern PSize1 exists for.
func TestPSize1Backpatch(t *testing.T) {
	w := NewWriter(16)
	w.P1(0) // placeholder
	w.P1(10)
	w.P1(20)
	w.P1(30)
	w.PSize1(3) // 3 payload bytes were written since the placeholder

	r := NewReader(w.Data[:w.Pos])
	if v := r.G1(); v != 3 {
		t.Fatalf("backpatched length: got %d, want 3", v)
	}
}

func TestGSmartMatchesTSReference(t *testing.T) {
	f := loadFixture(t)
	for _, c := range f.GSmart {
		r := NewReader(c.Bytes)
		if got := r.GSmart(); got != c.Want {
			t.Errorf("GSmart(%v): got %d, want %d", c.Bytes, got, c.Want)
		}
	}
}

func TestGSmartsMatchesTSReference(t *testing.T) {
	f := loadFixture(t)
	for _, c := range f.GSmarts {
		r := NewReader(c.Bytes)
		if got := r.GSmarts(); got != c.Want {
			t.Errorf("GSmarts(%v): got %d, want %d", c.Bytes, got, c.Want)
		}
	}
}

// TestCRC32MatchesStandardCheckValue cross-checks against the globally standard
// CRC-32/ISO-HDLC check value for "123456789" (0xCBF43926) — independent of the TS
// codebase entirely, on top of matching the TS reference vector.
func TestCRC32MatchesStandardCheckValue(t *testing.T) {
	f := loadFixture(t)
	input := []byte("123456789")
	got := GetCRC(input, 0, len(input))
	if got != f.CRC32Of123456789 {
		t.Fatalf("got %d (%#x), want %d (%#x, the standard CRC-32/ISO-HDLC check value)", got, got, f.CRC32Of123456789, f.CRC32Of123456789)
	}
	const standardCheckValue = 0xCBF43926
	if got != standardCheckValue {
		t.Fatalf("got %#x, want the standard check value %#x", got, uint32(standardCheckValue))
	}
}

// TestGBitMatchesTSReference replays a set of odd-width bit reads spanning byte
// boundaries (3+5+7+9 = 24 bits = 3 bytes) against a fixture captured from the real
// gBit implementation.
func TestGBitMatchesTSReference(t *testing.T) {
	f := loadFixture(t)
	r := NewReader(f.Bits.Input)
	r.GBitStart()
	widths := []int{3, 5, 7, 9}
	for i, w := range widths {
		got := r.GBit(w)
		if got != f.Bits.Values[i] {
			t.Errorf("GBit(%d) call %d: got %d, want %d", w, i, got, f.Bits.Values[i])
		}
	}
	r.GBitEnd()
	if r.Pos != f.Bits.EndPos {
		t.Errorf("GBitEnd: pos got %d, want %d", r.Pos, f.Bits.EndPos)
	}
}

// TestRSAEncryptWritesLengthPrefixedCiphertext is a light integration check that
// Packet.RSAEncrypt (packet layer) correctly delegates to internal/rsa and reframes the
// buffer as [1-byte length][ciphertext] — the detailed math is verified against TS
// vectors in internal/rsa's own tests.
func TestRSAEncryptWritesLengthPrefixedCiphertext(t *testing.T) {
	w := NewWriter(16)
	w.P1('A') // plaintext byte 65, mod=3233 exp=17 -> known ciphertext [10, 230] per rsa package's toy vector

	mod := bigFromString(t, "3233")
	exp := bigFromString(t, "17")
	w.RSAEncrypt(mod, exp)

	if w.Data[0] != 2 {
		t.Fatalf("length prefix: got %d, want 2", w.Data[0])
	}
	if w.Data[1] != 10 || w.Data[2] != 230 {
		t.Fatalf("ciphertext: got [%d %d], want [10 230]", w.Data[1], w.Data[2])
	}
	if w.Pos != 3 {
		t.Fatalf("pos after RSAEncrypt: got %d, want 3", w.Pos)
	}
}
