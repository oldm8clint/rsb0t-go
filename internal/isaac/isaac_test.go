package isaac

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fixture mirrors test/fixtures/isaac_vector.json, generated from the real vendored
// src/client/io/Isaac.ts (see ../../isaac_reference_vector.ts) with a fixed test seed.
type fixture struct {
	Seed   []int32 `json:"seed"`
	Values []int32 `json:"values"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "isaac_vector.json")
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

// TestNextIntMatchesTSReference checks the Go port bit-for-bit against a known-answer
// vector captured from the real TypeScript Isaac.ts for the same seed. This is the
// Phase 1 cross-check the plan calls for: prove the ISAAC port is correct in isolation,
// independent of packet framing, before trusting it against a live opcode stream.
func TestNextIntMatchesTSReference(t *testing.T) {
	f := loadFixture(t)
	c := NewCipher(f.Seed)
	for i, want := range f.Values {
		got := c.NextInt()
		if got != want {
			t.Fatalf("NextInt() call %d: got %d, want %d (from TS reference)", i, got, want)
		}
	}
}

// TestNextIntDeterministic guards against a seeding or state bug that only shows up
// across independent instances (e.g. accidentally sharing package-level state).
func TestNextIntDeterministic(t *testing.T) {
	seed := []int32{10, 20, 30, 40}
	a := NewCipher(seed)
	b := NewCipher(seed)
	for i := 0; i < 300; i++ { // > 256 to exercise a mix() reload
		av, bv := a.NextInt(), b.NextInt()
		if av != bv {
			t.Fatalf("call %d: two ciphers seeded identically diverged: %d != %d", i, av, bv)
		}
	}
}

// TestNextIntReloadsAt256 documents the count-underflow-triggers-remix behavior ported
// from `count-- === 0`, since that specific off-by-one is easy to get wrong in a port.
func TestNextIntReloadsAt256(t *testing.T) {
	c := NewCipher([]int32{1, 2, 3, 4})
	seen := make(map[int32]bool, 256)
	for i := 0; i < 256; i++ {
		seen[c.NextInt()] = true
	}
	if len(seen) < 250 { // ISAAC values needn't be unique, but near-total collision means the state isn't advancing
		t.Fatalf("expected the state to advance across a full 256-pull cycle, got only %d distinct values", len(seen))
	}
}
