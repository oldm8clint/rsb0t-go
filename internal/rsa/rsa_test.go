package rsa

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type vector struct {
	Mod   string `json:"mod"`
	Exp   string `json:"exp"`
	Plain []byte `json:"plain"`
	Enc   []byte `json:"enc"`
}

type fixture struct {
	Toy   vector `json:"toy"`
	Local vector `json:"local"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "rsa_vectors.json")
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

func mustBigInt(t *testing.T, s string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("could not parse %q as a base-10 bigint", s)
	}
	return n
}

func checkVector(t *testing.T, name string, v vector) {
	t.Helper()
	mod := mustBigInt(t, v.Mod)
	exp := mustBigInt(t, v.Exp)
	got := Encrypt(v.Plain, mod, exp)
	if len(got) != len(v.Enc) {
		t.Fatalf("%s: length mismatch: got %d bytes, want %d", name, len(got), len(v.Enc))
	}
	for i := range got {
		if got[i] != v.Enc[i] {
			t.Fatalf("%s: byte %d: got %d, want %d (full got=%v want=%v)", name, i, got[i], v.Enc[i], got, v.Enc)
		}
	}
}

// TestEncryptMatchesTSReference checks the Go port against known-answer vectors
// captured from the real TypeScript JsUtil.ts/Packet.rsaenc math: a textbook toy
// keypair (self round-trip verified in the generator script) and the actual local test
// engine's real, genuinely-large-exponent key — the case the docs warn a stock engine
// produces, and exactly why this doesn't use crypto/rsa.
func TestEncryptMatchesTSReference(t *testing.T) {
	f := loadFixture(t)
	checkVector(t, "toy", f.Toy)
	checkVector(t, "local", f.Local)
}

// TestEncryptRoundTripsWithPrivateKey is an independent correctness check that doesn't
// depend on the TS reference at all: encrypt with the toy keypair's public (mod, e) and
// decrypt with its private d, recovering the original plaintext.
func TestEncryptRoundTripsWithPrivateKey(t *testing.T) {
	mod := big.NewInt(3233)
	pubExp := big.NewInt(17)
	privExp := big.NewInt(2753)
	plain := []byte{65}

	enc := Encrypt(plain, mod, pubExp)
	// "Decrypt" is the same modPow operation with the private exponent — Encrypt's
	// generality (raw modPow, no padding) makes it reusable for this direction too.
	dec := Encrypt(enc, mod, privExp)

	if len(dec) != 1 || dec[0] != 65 {
		t.Fatalf("round trip failed: got %v, want [65]", dec)
	}
}

// TestEncryptPadsHighBitByte exercises the conditional leading-0x00-byte rule directly,
// independent of any external fixture, using small hand-computable numbers.
func TestEncryptPadsHighBitByte(t *testing.T) {
	// mod=257 (prime), exp=1 (identity map: ciphertext = plaintext mod 257).
	// plaintext 200 (0xC8, high bit set) should come back as [0x00, 0xC8].
	mod := big.NewInt(257)
	exp := big.NewInt(1)
	got := Encrypt([]byte{200}, mod, exp)
	want := []byte{0x00, 0xc8}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}

	// plaintext 100 (0x64, high bit clear) should come back as a single byte, no padding.
	got2 := Encrypt([]byte{100}, mod, exp)
	if len(got2) != 1 || got2[0] != 100 {
		t.Fatalf("got %v, want [100] (no padding)", got2)
	}
}
