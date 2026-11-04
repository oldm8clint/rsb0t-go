// protocol_test verifies the generated clientprot/serverprot packages (produced by
// internal/protocol/gen from the local Phase 0 engine) against a ground-truth fixture
// parsed directly from rsb0t-main's ALREADY-COMMITTED ClientProt.ts/ServerProt.ts —
// the tables Phase 0 already proved this local engine matches byte-for-byte. This test
// exists to catch bugs in the *generator itself* (a bad regex, a missed alias, an
// off-by-one in the sizes rendering), independent of that Phase 0 drift check.
package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

type fixture struct {
	ClientCount     int            `json:"clientCount"`
	ServerCount     int            `json:"serverCount"`
	Client          map[string]int `json:"client"`
	Server          map[string]int `json:"server"`
	ServerProtSizes [256]int       `json:"serverProtSizes"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "opcode_vector.json")
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

func TestClientProtMatchesCommittedTS(t *testing.T) {
	f := loadFixture(t)
	if len(clientprot.ByName) != f.ClientCount {
		t.Fatalf("opcode count: got %d, want %d", len(clientprot.ByName), f.ClientCount)
	}
	for name, want := range f.Client {
		got, ok := clientprot.ByName[name]
		if !ok {
			t.Errorf("missing client opcode %q (want %d)", name, want)
			continue
		}
		if got != want {
			t.Errorf("client opcode %s: got %d, want %d", name, got, want)
		}
	}
	for name := range clientprot.ByName {
		if _, ok := f.Client[name]; !ok {
			t.Errorf("generated an extra client opcode not in the committed TS table: %s", name)
		}
	}
}

func TestServerProtMatchesCommittedTS(t *testing.T) {
	f := loadFixture(t)
	if len(serverprot.ByName) != f.ServerCount {
		t.Fatalf("opcode count: got %d, want %d", len(serverprot.ByName), f.ServerCount)
	}
	for name, want := range f.Server {
		got, ok := serverprot.ByName[name]
		if !ok {
			t.Errorf("missing server opcode %q (want %d)", name, want)
			continue
		}
		if got != want {
			t.Errorf("server opcode %s: got %d, want %d", name, got, want)
		}
	}
	for name := range serverprot.ByName {
		if _, ok := f.Server[name]; !ok {
			t.Errorf("generated an extra server opcode not in the committed TS table: %s", name)
		}
	}
}

func TestServerProtSizesMatchesCommittedTS(t *testing.T) {
	f := loadFixture(t)
	for i := 0; i < 256; i++ {
		if serverprot.ServerProtSizes[i] != f.ServerProtSizes[i] {
			t.Errorf("ServerProtSizes[%d]: got %d, want %d", i, serverprot.ServerProtSizes[i], f.ServerProtSizes[i])
		}
	}
}

// TestKeyOpcodesMatchExpectedValues spot-checks the specific opcodes Phase 1/2 actually
// need by name, as documented in the implementation plan — a belt-and-braces check that
// the ones this project cares about most are exactly right, independent of the
// exhaustive fixture comparison above.
func TestKeyOpcodesMatchExpectedValues(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"clientprot.ClientVersion", clientprot.ClientVersion, 289},
		{"clientprot.OPPLAYER4 (trade request, gates ANTICHEAT_OPLOGIC5)", clientprot.OPPLAYER4, 189},
		{"clientprot.ANTICHEAT_OPLOGIC5", clientprot.ANTICHEAT_OPLOGIC5, 46},
		{"serverprot.IF_OPENMAIN_SIDE (bank interface)", serverprot.IF_OPENMAIN_SIDE, 55},
		{"serverprot.UPDATE_INV_FULL", serverprot.UPDATE_INV_FULL, 107},
		{"serverprot.UPDATE_INV_PARTIAL", serverprot.UPDATE_INV_PARTIAL, 76},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, c.got, c.want)
		}
	}
}
