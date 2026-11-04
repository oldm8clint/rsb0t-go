package controlpanelapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"rsb0t-go/internal/worlds"
)

// THE LEGACY ROUND TRIP. The live fleet runs the LEGACY fleet.config.json shape — no
// "version" key, read by this command itself rather than by internal/config's version-1
// loader — so anything multi-world has to survive that shape or it does not exist as far as
// production is concerned. This file writes the real thing to disk, reads it back through
// loadRoster, and resolves it exactly the way main() does.

// legacyRoster is a roster of the shape the live fleet uses: no "version" key, a shared
// password, per-account entries, and now a per-account "world".
const legacyRoster = `{
  "wsURL": "wss://w1.example.test/",
  "httpBase": "https://w1.example.test/",
  "sharedPassword": "not-a-real-password",
  "loginDelaySeconds": 3,
  "accounts": [
    { "username": "chopper_one", "script": "ProgressiveWoodcutter", "settings": { "mule": "mulecon" } },
    { "username": "chopper_two", "script": "ProgressiveWoodcutter", "world": 2, "settings": { "mule": "mulecon" } },
    { "username": "mulecon",     "script": "MuleReceiver",          "world": 1 }
  ]
}`

func writeRoster(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fleet.config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLegacyRosterCarriesAPerAccountWorld(t *testing.T) {
	r, err := loadRoster(writeRoster(t, legacyRoster))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Accounts) != 3 {
		t.Fatalf("read %d accounts, want 3", len(r.Accounts))
	}
	if r.Accounts[0].World != 0 {
		t.Errorf("an account with no world key must read as 0 (= the fleet's own), got %d", r.Accounts[0].World)
	}
	if r.Accounts[1].World != 2 {
		t.Errorf("chopper_two world = %d, want 2", r.Accounts[1].World)
	}
	if r.Accounts[2].World != 1 {
		t.Errorf("mulecon world = %d, want 1", r.Accounts[2].World)
	}

	// And resolving it gives the URLs a login would actually dial. This is the whole
	// mechanism: nothing else about the login changes per world.
	entries, err := worlds.ParseEntries(r.Worlds)
	if err != nil {
		t.Fatal(err)
	}
	wm, err := worlds.New(r.WSURL, r.HTTPBase, entries)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"chopper_one": "wss://w1.example.test/", // no world named -> the fleet's own
		"chopper_two": "wss://w2.example.test/",
		"mulecon":     "wss://w1.example.test/",
	}
	for _, a := range r.Accounts {
		e, err := wm.Endpoints(a.World)
		if err != nil {
			t.Fatalf("%s: %v", a.Username, err)
		}
		if e.WSURL != want[a.Username] {
			t.Errorf("%s dials %q, want %q", a.Username, e.WSURL, want[a.Username])
		}
	}
}

// A roster written before worlds existed must resolve every account to exactly the endpoints
// it always used — otherwise this change is not additive, whatever its comments claim.
func TestARosterThatNeverMentionsWorldsIsUnchanged(t *testing.T) {
	const old = `{
  "wsURL": "ws://localhost/",
  "httpBase": "http://localhost/",
  "sharedPassword": "not-a-real-password",
  "accounts": [ { "username": "throwaway1", "script": "", "settings": {} } ]
}`
	r, err := loadRoster(writeRoster(t, old))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Worlds) != 0 {
		t.Fatalf("a roster with no worlds object read %d entries", len(r.Worlds))
	}
	wm, err := worlds.New(r.WSURL, r.HTTPBase, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := wm.Endpoints(r.Accounts[0].World)
	if err != nil {
		t.Fatal(err)
	}
	if e.WSURL != "ws://localhost/" || e.HTTPBase != "http://localhost/" {
		t.Fatalf("resolved to %+v, want the roster's own endpoints byte for byte", e)
	}
	if wm.Distinct() {
		t.Fatal("a localhost roster must not claim to have distinct worlds")
	}
}

// The optional "worlds" object, for a deployment whose hosts cannot be derived.
func TestRosterWorldsObjectOverridesDerivation(t *testing.T) {
	const body = `{
  "wsURL": "wss://game.example.test/",
  "httpBase": "https://game.example.test/",
  "sharedPassword": "not-a-real-password",
  "worlds": {
    "2": { "wsURL": "wss://game2.example.test/", "httpBase": "https://game2.example.test/" }
  },
  "accounts": [ { "username": "a", "world": 2 } ]
}`
	r, err := loadRoster(writeRoster(t, body))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := worlds.ParseEntries(r.Worlds)
	if err != nil {
		t.Fatal(err)
	}
	wm, err := worlds.New(r.WSURL, r.HTTPBase, entries)
	if err != nil {
		t.Fatal(err)
	}
	// game.example.test has no wN label, so derivation alone could never have produced a
	// second world — the explicit entry is the only thing that makes this deployment
	// multi-world, and Distinct must say so.
	if !wm.Distinct() {
		t.Fatal("an explicit worlds entry makes a deployment multi-world; Distinct said otherwise")
	}
	e, err := wm.Endpoints(r.Accounts[0].World)
	if err != nil {
		t.Fatal(err)
	}
	if e.WSURL != "wss://game2.example.test/" {
		t.Fatalf("dials %q, want the explicit entry", e.WSURL)
	}
}

// A worlds object with a key that is not a world number must fail loudly: an operator who
// wrote it believes those accounts moved, and silently resolving them to the fleet default
// would put them on the wrong world with nothing reporting it.
func TestABadWorldsKeyIsAnError(t *testing.T) {
	var m map[string]worlds.Entry
	if err := json.Unmarshal([]byte(`{"two":{"wsURL":"wss://x/"}}`), &m); err != nil {
		t.Fatal(err)
	}
	if _, err := worlds.ParseEntries(m); err == nil {
		t.Fatal(`"two" is not a world number and must be rejected`)
	}
}
