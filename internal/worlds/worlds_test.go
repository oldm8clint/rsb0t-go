package worlds

import "testing"

func TestHostWorld(t *testing.T) {
	cases := []struct {
		host string
		want int
		ok   bool
	}{
		{"w1.example.invalid", 1, true},
		{"w2.example.invalid", 2, true},
		{"w12.example.invalid", 12, true},
		{"localhost", 0, false},
		{"example.invalid", 0, false},
		{"west.example.invalid", 0, false}, // "w" followed by letters is not a world label
		{"w0.example.invalid", 0, false},   // there is no world 0
		{"w999.example.invalid", 0, false}, // out of the 1..255 range
	}
	for _, c := range cases {
		got, ok := HostWorld(c.host)
		if got != c.want || ok != c.ok {
			t.Errorf("HostWorld(%q) = %d,%v want %d,%v", c.host, got, ok, c.want, c.ok)
		}
	}
}

func TestDeriveRewritesOnlyTheWorldLabel(t *testing.T) {
	m, err := New("wss://w1.example.invalid/", "https://w1.example.invalid/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.FleetWorld() != 1 {
		t.Fatalf("fleet world = %d, want 1", m.FleetWorld())
	}
	if !m.Distinct() {
		t.Fatal("a wN host must be able to produce distinct worlds")
	}
	e, err := m.Endpoints(2)
	if err != nil {
		t.Fatal(err)
	}
	if e.WSURL != "wss://w2.example.invalid/" {
		t.Errorf("ws = %q", e.WSURL)
	}
	if e.HTTPBase != "https://w2.example.invalid/" {
		t.Errorf("http = %q", e.HTTPBase)
	}
	if e.World != 2 {
		t.Errorf("world = %d", e.World)
	}
}

// The fleet's own endpoints must come back completely untouched for an account that names
// no world, so adding this package cannot change a single-world fleet's bytes.
func TestWorldZeroIsTheFleetsOwnEndpointsUnchanged(t *testing.T) {
	m, err := New("wss://w1.example.invalid/ws?x=1", "https://w1.example.invalid/base/", nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := m.Endpoints(0)
	if err != nil {
		t.Fatal(err)
	}
	if e.WSURL != "wss://w1.example.invalid/ws?x=1" || e.HTTPBase != "https://w1.example.invalid/base/" {
		t.Fatalf("world 0 changed the endpoints: %+v", e)
	}
	if e.World != 1 {
		t.Errorf("world 0 should report the fleet's own world 1, got %d", e.World)
	}
}

func TestDerivePreservesSchemePortAndPath(t *testing.T) {
	m, err := New("ws://w1.example.test:8080/game?v=289", "http://w1.example.test:8080/", nil)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := m.Endpoints(3)
	if e.WSURL != "ws://w3.example.test:8080/game?v=289" {
		t.Errorf("ws = %q", e.WSURL)
	}
	if e.HTTPBase != "http://w3.example.test:8080/" {
		t.Errorf("http = %q", e.HTTPBase)
	}
}

// A local engine cannot be split into worlds, and this is the assertion that stops a
// single-world logout/login cycle being reported as a two-world hop.
func TestLocalEngineIsNotDistinctAndEveryWorldIsTheSameURL(t *testing.T) {
	m, err := New("ws://localhost/", "http://localhost/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Distinct() {
		t.Fatal("ws://localhost/ has no wN label, so no world can be distinct from another")
	}
	if m.FleetWorld() != Default {
		t.Errorf("fleet world = %d, want %d", m.FleetWorld(), Default)
	}
	a, _ := m.Endpoints(1)
	b, _ := m.Endpoints(2)
	if a.WSURL != b.WSURL || a.WSURL != "ws://localhost/" {
		t.Fatalf("worlds 1 and 2 must both be the fleet URL: %q vs %q", a.WSURL, b.WSURL)
	}
}

func TestExplicitEntryBeatsDerivation(t *testing.T) {
	m, err := New("wss://w1.example.invalid/", "https://w1.example.invalid/", map[int]Endpoints{
		2: {WSURL: "wss://other.example.test/", HTTPBase: "https://other.example.test/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Explicit(2) || m.Explicit(3) {
		t.Fatal("Explicit should report only the configured world")
	}
	e, _ := m.Endpoints(2)
	if e.WSURL != "wss://other.example.test/" {
		t.Errorf("explicit ws ignored: %q", e.WSURL)
	}
	// world 3 is still derived
	e3, _ := m.Endpoints(3)
	if e3.WSURL != "wss://w3.example.invalid/" {
		t.Errorf("derivation broken by an explicit entry: %q", e3.WSURL)
	}
}

// An explicit entry that gives only one half fills the other from derivation, so a roster
// can override just the WebSocket without restating the HTTP base.
func TestExplicitHalfEntryFillsTheOtherHalf(t *testing.T) {
	m, err := New("wss://w1.example.invalid/", "https://w1.example.invalid/", map[int]Endpoints{
		2: {WSURL: "wss://w2.mirror.test/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := m.Endpoints(2)
	if e.WSURL != "wss://w2.mirror.test/" || e.HTTPBase != "https://w2.example.invalid/" {
		t.Fatalf("half entry not filled: %+v", e)
	}
}

// An explicit entry that names a world nobody can log in to must be an error, not a silent
// fallback to the fleet default — an operator who wrote it believes the split happened.
func TestNewRejectsBadExplicitEntries(t *testing.T) {
	if _, err := New("wss://w1.example.invalid/", "", map[int]Endpoints{0: {WSURL: "ws://x/"}}); err == nil {
		t.Error("world 0 should be rejected")
	}
	if _, err := New("wss://w1.example.invalid/", "", map[int]Endpoints{999: {WSURL: "ws://x/"}}); err == nil {
		t.Error("world 999 should be rejected")
	}
	if _, err := New("wss://w1.example.invalid/", "", map[int]Endpoints{2: {}}); err == nil {
		t.Error("an entry naming neither endpoint should be rejected")
	}
	if _, err := New("  ", "", nil); err == nil {
		t.Error("an empty fleet wsURL should be rejected")
	}
}

func TestNormalise(t *testing.T) {
	m, _ := New("wss://w2.example.invalid/", "https://w2.example.invalid/", nil)
	// The fleet itself is configured for world 2 here, so "no world named" means 2.
	if n, err := m.Normalise(0); err != nil || n != 2 {
		t.Errorf("Normalise(0) = %d,%v want 2,nil", n, err)
	}
	if n, err := m.Normalise(1); err != nil || n != 1 {
		t.Errorf("Normalise(1) = %d,%v want 1,nil", n, err)
	}
	if _, err := m.Normalise(Max + 1); err == nil {
		t.Error("an out-of-range world should be an error, not a clamp")
	}
}

func TestEndpointsRejectsOutOfRange(t *testing.T) {
	m, _ := New("wss://w1.example.invalid/", "", nil)
	if _, err := m.Endpoints(Max + 1); err == nil {
		t.Error("want an error for a world above the range")
	}
}
