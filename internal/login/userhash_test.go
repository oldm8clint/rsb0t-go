package login

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

type userhashCase struct {
	Name        string `json:"name"`
	Hash        string `json:"hash"`
	LoginServer int    `json:"loginServer"`
}

func loadUserhashFixture(t *testing.T) []userhashCase {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "userhash_vector.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var cases []userhashCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return cases
}

// TestToUserhashMatchesTSReference checks the Go port against vectors captured from the
// real JString.toUserhash, including edge cases (empty string, 16 chars truncated to
// 12, surrounding whitespace, digits).
func TestToUserhashMatchesTSReference(t *testing.T) {
	for _, c := range loadUserhashFixture(t) {
		want, err := strconv.ParseUint(c.Hash, 10, 64)
		if err != nil {
			t.Fatalf("fixture hash %q for %q doesn't parse as uint64: %v", c.Hash, c.Name, err)
		}
		if got := toUserhash(c.Name); got != want {
			t.Errorf("toUserhash(%q): got %d, want %d", c.Name, got, want)
		}
		if got := loginServer(c.Name); int(got) != c.LoginServer {
			t.Errorf("loginServer(%q): got %d, want %d", c.Name, got, c.LoginServer)
		}
	}
}
