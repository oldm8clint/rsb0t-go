package config

import (
	"strings"
	"testing"
	"time"
)

// These documents mirror FleetConfig in cmd/fleet/main.go with placeholder values only.

func TestLegacyFleetConfigLoads(t *testing.T) {
	text := `{
		"wsURL": "wss://w1.example.invalid/",
		"httpBase": "https://w1.example.invalid",
		"sharedPassword": "placeholderpw",
		"loginDelaySeconds": 3,
		"note": "kept by hand",
		"accounts": [
			{"username": "alice", "script": "", "settings": null},
			{"username": "bob", "script": "ProgressiveCrafter", "settings": {"mule": "mulecon"}, "comment": "x"},
			{"username": "carol"}
		]
	}`
	cfg := mustParse(t, text, nil)
	if !cfg.Legacy || len(cfg.Accounts) != 3 {
		t.Fatalf("legacy = %v, accounts = %d", cfg.Legacy, len(cfg.Accounts))
	}
	live := cfg.Global.Server.Active
	if live.URL != "wss://w1.example.invalid/" || live.HTTPBase != "https://w1.example.invalid/" {
		t.Errorf("active profile = %+v", live)
	}
	if cfg.Global.Login.LoginDelay != Duration(3*time.Second) {
		t.Errorf("login delay = %s", cfg.Global.Login.LoginDelay)
	}
	alice, bob := cfg.Accounts[0], cfg.Accounts[1]
	if alice.Script != "" || string(alice.Settings) != "{}" {
		t.Errorf("alice = %+v", alice)
	}
	if bob.Script != "ProgressiveCrafter" {
		t.Errorf("bob script = %q", bob.Script)
	}
	jsonEqual(t, "bob settings", bob.Settings, `{"mule":"mulecon"}`)
	for _, a := range cfg.Accounts {
		if a.Password.Reveal() != "placeholderpw" || !a.Enabled || a.LogLevel != "info" {
			t.Errorf("%s: enabled %v, log level %q, password set %v", a.Username, a.Enabled, a.LogLevel, a.Password.Reveal() != "")
		}
	}

	all := strings.Join(cfg.Warnings, "\n")
	for _, want := range []string{"plaintext", `unknown key "note" ignored`, `accounts[1]: unknown key "comment" ignored`} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings %q lack %q", all, want)
		}
	}
	if strings.Contains(all, "placeholderpw") {
		t.Errorf("a warning quotes the password: %s", all)
	}
}

func TestLegacyFleetConfigDefaultsTheServer(t *testing.T) {
	cfg := mustParse(t, `{"sharedPassword":"placeholderpw","accounts":[{"username":"alice"}]}`, nil)
	if a := cfg.Global.Server.Active; a.URL != "wss://w1.example.invalid/" || a.HTTPBase != "https://w1.example.invalid/" {
		t.Errorf("active profile = %+v", a)
	}
	if cfg.Global.Login.LoginDelay != 0 {
		t.Errorf("login delay = %s, want none", cfg.Global.Login.LoginDelay)
	}
}

func TestLegacyFleetConfigProblems(t *testing.T) {
	const acct = `"accounts":[{"username":"alice"}]`
	cases := []struct{ name, text, where, field, msg string }{
		{"missing password", `{"wsURL":"wss://w1.example.invalid/",` + acct + `}`, "file", "sharedPassword", "is required"},
		{"negative delay", `{"sharedPassword":"pw","loginDelaySeconds":-1,` + acct + `}`, "file", "loginDelaySeconds", "between 0 and 600"},
		{"delay as a string", `{"sharedPassword":"pw","loginDelaySeconds":"3",` + acct + `}`, "file", "loginDelaySeconds", "want a number, got string"},
		{"no accounts", `{"sharedPassword":"pw","accounts":[]}`, "file", "accounts", "at least one"},
		{"duplicate username", `{"sharedPassword":"pw","accounts":[{"username":"Bob_Smith"},{"username":"bob smith"}]}`, `accounts[1] "bob smith"`, "username", `same account as accounts[0] "Bob_Smith"`},
		{"bad username", `{"sharedPassword":"pw","accounts":[{"username":"al!ce"}]}`, `accounts[0] "al!ce"`, "username", "may hold only"},
		{"bad script name", `{"sharedPassword":"pw","accounts":[{"username":"alice","script":"9lives"}]}`, `accounts[0] "alice"`, "script", "want a letter"},
		{"settings not an object", `{"sharedPassword":"pw","accounts":[{"username":"alice","settings":[1]}]}`, `accounts[0] "alice"`, "settings", "want an object"},
		{"not a websocket url", `{"wsURL":"https://w1.example.invalid/","sharedPassword":"pw",` + acct + `}`, "global", "server.profiles.live.url", "want ws://"},
		{"overlong password", `{"sharedPassword":"` + strings.Repeat("x", 21) + `",` + acct + `}`, "file", "sharedPassword", "at most 20 characters, got 21"},
		{"version makes it the new format", `{"version":1,"sharedPassword":"pw",` + acct + `}`, "file", "sharedPassword", "passwords never go in the settings file"},
		{"no marker and no version", `{` + acct + `}`, "file", "version", "must be 1, got 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseIn(t, t.TempDir(), c.text, nil)
			wantProblem(t, err, c.where, c.field, c.msg)
		})
	}
}
