package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultsFillEveryKnob(t *testing.T) {
	cfg := mustParse(t, doc("", "", ""), testEnv)
	g := cfg.Global
	live := g.Server.Active
	local := g.Server.Profiles["local"]
	a := cfg.Accounts[0]
	checks := []struct {
		name      string
		got, want interface{}
	}{
		{"legacy", cfg.Legacy, false},
		{"warnings", len(cfg.Warnings), 0},
		{"server.profile", g.Server.Profile, "live"},
		{"live url", live.URL, "wss://w1.example.invalid/"},
		{"live httpBase", live.HTTPBase, "https://w1.example.invalid/"},
		{"live crc is fetched", live.CRC == nil, true},
		{"live modulus is fetched", live.RSAModulus, ""},
		{"live exponent", live.RSAExponent, "65537"},
		{"local url", local.URL, "ws://localhost/"},
		{"local httpBase", local.HTTPBase, "http://localhost/"},
		{"local exponent has no default", local.RSAExponent, ""},
		{"server.world", g.Server.World, 1},
		{"server.members", g.Server.Members, true},
		{"server.lowMemory", g.Server.LowMemory, false},
		{"server.dialTimeout", g.Server.DialTimeout, Duration(15 * time.Second)},
		{"server.readTimeout", g.Server.ReadTimeout, Duration(30 * time.Second)},
		{"data", g.Data, Data{}},
		{"login", g.Login, Login{}},
		{"runtime.memoryLimit is off", g.Runtime.MemoryLimitBytes, int64(math.MaxInt64)},
		{"runtime.gcPercent", g.Runtime.GCPercent, 100},
		{"runtime.maxProcs", g.Runtime.MaxProcs, 0},
		{"log.level", g.Log.Level, "info"},
		{"log.output", g.Log.Output, "shared"},
		{"log.file", g.Log.File, ""},
		{"telemetry.memReportEvery", g.Telemetry.MemReportEvery, Duration(0)},
		{"controlPanel.enabled", g.ControlPanel.Enabled, false},
		{"controlPanel.listen", g.ControlPanel.Listen, "127.0.0.1:7274"},
		{"controlPanel.token", g.ControlPanel.Token.Reveal(), ""},
		{"controlPanel.eventHistory", g.ControlPanel.EventHistory, 256},
		{"controlPanel.logHistory", g.ControlPanel.LogHistory, 256},
		{"controlPanel.memoryHistory", g.ControlPanel.MemoryHistory, 300},
		{"username", a.Username, "alice"},
		{"enabled", a.Enabled, true},
		{"logLevel", a.LogLevel, "info"},
		{"script", a.Script, "idle"},
		{"settings", string(a.Settings), "{}"},
		{"startDelay", a.StartDelay, Duration(0)},
		{"startJitter", a.StartJitter, Duration(0)},
		{"maxRuntime", a.MaxRuntime, Duration(0)},
		{"state.viewRadius", a.State.ViewRadius, 15},
		{"state.trackers all off", a.State.Trackers, Trackers{}},
		{"reconnect", a.Reconnect, Reconnect{Initial: Duration(6 * time.Second), Max: Duration(5 * time.Minute), Multiplier: 2, Jitter: 0.2, MaxFailures: 15}},
		{"password", a.Password.Reveal(), "alicepw"},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
	for name, p := range map[string]string{"log.dir": g.Log.Dir, "controlPanel.layoutFile": g.ControlPanel.LayoutFile} {
		if !filepath.IsAbs(p) {
			t.Errorf("%s = %q, want an absolute path", name, p)
		}
	}
}

func TestAccountsInheritDefaultsAndOverrideAnyOfThem(t *testing.T) {
	defaults := `{
		"passwordEnv": "PW_{USER}",
		"logLevel": "warn",
		"script": "ProgressiveCrafter",
		"startJitter": "9s",
		"settings": {"mule": "mulecon", "muleAt": 5000, "keep": {"items": ["Coins", "Shears"], "note": "never handed over"}},
		"state": {"trackers": {"npcs": true, "groundItems": true}},
		"reconnect": {"max": "10m"}
	}`
	accounts := `[
		{"username": "alice"},
		{"username": "bob", "logLevel": "debug", "state": {"viewRadius": 5, "trackers": {"groundItems": false}},
		 "settings": {"muleAt": 2000, "keep": {"items": ["Coins"]}}, "reconnect": {"initial": "2s"}},
		{"username": "carol", "script": "EssenceMiner", "settings": {"rock": "essence"}}
	]`
	cfg := mustParse(t, doc("", defaults, accounts), testEnv)
	alice, bob, carol := cfg.Accounts[0], cfg.Accounts[1], cfg.Accounts[2]

	if alice.LogLevel != "warn" || alice.Script != "ProgressiveCrafter" || alice.StartJitter != Duration(9*time.Second) {
		t.Errorf("alice did not inherit: %+v", alice)
	}
	if alice.State.Trackers != (Trackers{NPCs: true, GroundItems: true}) || alice.State.ViewRadius != 15 {
		t.Errorf("alice state = %+v", alice.State)
	}
	if alice.Reconnect.Max != Duration(10*time.Minute) || alice.Reconnect.Initial != Duration(6*time.Second) {
		t.Errorf("alice reconnect = %+v", alice.Reconnect)
	}
	jsonEqual(t, "alice settings", alice.Settings, `{"mule":"mulecon","muleAt":5000,"keep":{"items":["Coins","Shears"],"note":"never handed over"}}`)

	if bob.LogLevel != "debug" || bob.State.ViewRadius != 5 || bob.State.Trackers != (Trackers{NPCs: true}) {
		t.Errorf("bob overrides lost: %+v", bob)
	}
	if bob.Reconnect.Initial != Duration(2*time.Second) || bob.Reconnect.Max != Duration(10*time.Minute) {
		t.Errorf("bob reconnect = %+v", bob.Reconnect)
	}
	jsonEqual(t, "bob settings", bob.Settings, `{"mule":"mulecon","muleAt":2000,"keep":{"items":["Coins"],"note":"never handed over"}}`)

	if carol.Script != "EssenceMiner" || carol.StartJitter != Duration(9*time.Second) {
		t.Errorf("carol = %+v", carol)
	}
	jsonEqual(t, "carol settings", carol.Settings, `{"rock":"essence"}`)
}

func TestServerProfiles(t *testing.T) {
	const localExponent = "58778699976184461502525193738213253649000149147835990136706041084440742975821"
	cases := []struct {
		name, global, profile string
		want                  ServerProfile
	}{
		{"http base gains its slash", `{"server":{"profiles":{"live":{"url":"wss://example.test","httpBase":"https://example.test"}}}}`, "",
			ServerProfile{URL: "wss://example.test", HTTPBase: "https://example.test/", RSAExponent: "65537"}},
		{"override picks local", `{"server":{"profiles":{"local":{"url":"ws://127.0.0.1:43594/","httpBase":"http://127.0.0.1:43594/","rsaModulus":"` + testModulus + `","rsaExponent":"` + localExponent + `"}}}}`, "local",
			ServerProfile{URL: "ws://127.0.0.1:43594/", HTTPBase: "http://127.0.0.1:43594/", RSAExponent: localExponent}},
		{"named profile", `{"server":{"profile":"dev","profiles":{"dev":{"url":"ws://localhost:8888/","httpBase":"http://localhost:8888","rsaModulus":"` + testModulus + `","rsaExponent":"65537"}}}}`, "",
			ServerProfile{URL: "ws://localhost:8888/", HTTPBase: "http://localhost:8888/", RSAExponent: "65537"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Parse([]byte(doc(c.global, "", "")), Options{BaseDir: t.TempDir(), LookupEnv: envOf(testEnv), Profile: c.profile})
			if err != nil {
				t.Fatal(err)
			}
			a := cfg.Global.Server.Active
			if a.URL != c.want.URL || a.HTTPBase != c.want.HTTPBase || a.RSAExponent != c.want.RSAExponent {
				t.Errorf("active = %+v, want %+v", a, c.want)
			}
		})
	}
}

func TestOnlyTheActiveProfileTouchesFiles(t *testing.T) {
	dir := t.TempDir()
	text := doc(`{"server":{"profiles":{"local":{"url":"ws://localhost/","rsaModulusFile":"missing-key.txt"}}}}`, "", "")
	local := mustParseIn(t, dir, text, testEnv).Global.Server.Profiles["local"]
	if local.RSAModulusFile != filepath.Join(dir, "missing-key.txt") {
		t.Errorf("inactive local profile key path = %q", local.RSAModulusFile)
	}
}

func TestLoginDataWithoutAnHTTPBase(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeFile(t, dir, "loginkey.txt", "\xef\xbb\xbf"+testModulus+"\r\n")
	text := doc(`{"server":{"profile":"local","profiles":{"local":{"url":"ws://localhost/","rsaModulusFile":"loginkey.txt","rsaExponent":"65537","crcTable":[-1,0,1,2,3,4,5,6,4294967295]}}}}`, "", "")
	a := mustParseIn(t, dir, text, testEnv).Global.Server.Active
	if a.RSAModulus != testModulus || a.RSAModulusFile != keyPath || a.HTTPBase != "" {
		t.Errorf("modulus %q from %q, httpBase %q", a.RSAModulus, a.RSAModulusFile, a.HTTPBase)
	}
	want := []uint32{0xffffffff, 0, 1, 2, 3, 4, 5, 6, 0xffffffff}
	if !reflect.DeepEqual(a.CRC, want) {
		t.Errorf("CRC = %v, want %v", a.CRC, want)
	}
}

func TestPasswordsComeFromEnvironmentThenCredentialsFile(t *testing.T) {
	files := map[string]string{
		"creds.json":   `{"Alice": "filealice", "bob": "filebob", "Bob_Smith": "pw:with:colons"}`,
		"accounts.txt": "alice:filealice\r\n\n# the test accounts\nbob:filebob\n  Bob_Smith:pw:with:colons  \nnot a credential line\n",
	}
	for name, content := range files {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			creds := writeFile(t, dir, name, content)
			text := doc(`{"login":{"credentialsFile":"`+name+`"}}`, "", `[{"username":"alice"},{"username":"bob"},{"username":"bob smith"},{"username":"carol","passwordEnv":"CAROL_PW"}]`)
			cfg := mustParseIn(t, dir, text, map[string]string{"PW_BOB": "envbob", "CAROL_PW": "carolpw"})

			want := map[string]string{"alice": "filealice", "bob": "envbob", "bob smith": "pw:with:colons", "carol": "carolpw"}
			for _, a := range cfg.Accounts {
				if got := a.Password.Reveal(); got != want[a.Username] {
					t.Errorf("%s password = %q, want %q", a.Username, got, want[a.Username])
				}
			}
			if cfg.Global.Login.CredentialsFile != creds {
				t.Errorf("credentialsFile = %q, want %q", cfg.Global.Login.CredentialsFile, creds)
			}
		})
	}
}

func TestDisabledAccountNeedsNoPassword(t *testing.T) {
	cfg := mustParse(t, doc("", "", `[{"username":"alice"},{"username":"zed","enabled":false}]`), map[string]string{"PW_ALICE": "alicepw"})
	if n := len(cfg.Enabled()); n != 1 {
		t.Fatalf("Enabled() = %d accounts, want 1", n)
	}
	if cfg.Accounts[1].Password.Reveal() != "" {
		t.Errorf("disabled account got a password")
	}
}

func TestPasswordsNeverPrintOrReachErrors(t *testing.T) {
	a := mustParse(t, doc("", "", ""), testEnv).Accounts[0]
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprintf("%v", a), fmt.Sprintf("%+v", a), fmt.Sprintf("%#v", a), string(b), a.Password.String()} {
		if strings.Contains(s, "alicepw") {
			t.Errorf("password leaked: %s", s)
		}
	}

	secrets := []string{"hunter2pw", "averyveryverylongsecretpw", "s3cr3tjson", "s3cr3tline", "sharedlongpassword1234"}
	leaks := []struct {
		name, text, file, content string
		env                       map[string]string
	}{
		{name: "inline password", text: doc("", "", `[{"username":"alice","password":"hunter2pw"}]`), env: testEnv},
		{name: "overlong password", text: doc("", "", ""), env: map[string]string{"PW_ALICE": "averyveryverylongsecretpw"}},
		{name: "broken JSON credentials", file: "creds.json", content: `{"alice": "s3cr3tjson"`},
		{name: "bad username in credential lines", file: "accounts.txt", content: "no!:s3cr3tline\n"},
		{name: "overlong legacy shared password", text: `{"sharedPassword":"sharedlongpassword1234","accounts":[{"username":"alice"}]}`},
	}
	for _, l := range leaks {
		dir := t.TempDir()
		text := l.text
		if l.file != "" {
			writeFile(t, dir, l.file, l.content)
			text = doc(`{"login":{"credentialsFile":"`+l.file+`"}}`, "", "")
		}
		_, err := parseIn(t, dir, text, l.env)
		if err == nil {
			t.Fatalf("%s: want an error", l.name)
		}
		for _, secret := range secrets {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: error quotes the password: %v", l.name, err)
			}
		}
	}
}

func TestCredentialsFileProblems(t *testing.T) {
	cases := []struct{ name, file, content, msg string }{
		{"JSON not closed", "creds.json", `{"alice": `, "username:password lines or a JSON object"},
		{"JSON not strings", "creds.json", `{"alice": 5}`, "username:password lines or a JSON object"},
		{"JSON same account twice", "creds.json", `{"Bob_Smith": "a", "bob smith": "b"}`, `name the same account`},
		{"JSON bad username", "creds.json", `{"no!": "x"}`, `entry "no!"`},
		{"lines same account twice", "accounts.txt", "alice:a\nALICE:b\n", `entries "alice" and "ALICE" name the same account`},
		{"lines bad username", "accounts.txt", "much too long a name:x\n", `entry "much too long a name"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, c.file, c.content)
			_, err := parseIn(t, dir, doc(`{"login":{"credentialsFile":"`+c.file+`"}}`, "", ""), testEnv)
			wantProblem(t, err, "global", "login.credentialsFile", c.msg)
		})
	}
}

func TestControlPanelToken(t *testing.T) {
	const token = "0123456789abcdef"
	t.Run("from environment", func(t *testing.T) {
		cfg := mustParse(t, doc(`{"controlPanel":{"enabled":true,"listen":"0.0.0.0:8091","tokenEnv":"PANEL_TOKEN"}}`, "", ""), withEnv(map[string]string{"PANEL_TOKEN": token}))
		if cfg.Global.ControlPanel.Token.Reveal() != token {
			t.Errorf("token = %q", cfg.Global.ControlPanel.Token.Reveal())
		}
	})
	t.Run("from file", func(t *testing.T) {
		dir := t.TempDir()
		path := writeFile(t, dir, "token.txt", "\xef\xbb\xbf"+token+"\r\n")
		cfg := mustParseIn(t, dir, doc(`{"controlPanel":{"enabled":true,"tokenFile":"token.txt"}}`, "", ""), testEnv)
		if cp := cfg.Global.ControlPanel; cp.Token.Reveal() != token || cp.TokenFile != path {
			t.Errorf("token = %q from %q", cp.Token.Reveal(), cp.TokenFile)
		}
	})
	t.Run("loopback addresses need no token", func(t *testing.T) {
		for _, listen := range []string{"127.0.0.2:8091", "[::1]:8091", "localhost:8091"} {
			mustParse(t, doc(`{"controlPanel":{"enabled":true,"listen":"`+listen+`","layoutFile":""}}`, "", ""), testEnv)
		}
	})
	t.Run("disabled reads nothing", func(t *testing.T) {
		cfg := mustParse(t, doc(`{"controlPanel":{"tokenEnv":"PANEL_TOKEN"}}`, "", ""), testEnv)
		if cfg.Global.ControlPanel.Token.Reveal() != "" {
			t.Errorf("disabled control panel resolved a token")
		}
	})
}

func TestRelativePathsResolveAgainstTheSettingsFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "collision.lcnav.gz", "pack")
	cfg := mustParseIn(t, dir, doc(`{"data":{"collision":"collision.lcnav.gz"},"log":{"output":"per-account","dir":"out/logs"}}`, "", ""), testEnv)
	if cfg.Global.Data.Collision != path {
		t.Errorf("collision = %q, want %q", cfg.Global.Data.Collision, path)
	}
	if want := filepath.Join(dir, "out", "logs"); cfg.Global.Log.Dir != want {
		t.Errorf("log.dir = %q, want %q", cfg.Global.Log.Dir, want)
	}
}

func TestMemoryLimit(t *testing.T) {
	cases := map[string]int64{`"off"`: math.MaxInt64, `"40MiB"`: 40 << 20, `"2GiB"`: 2 << 30, `"8MiB"`: 8 << 20}
	for value, want := range cases {
		cfg := mustParse(t, doc(`{"runtime":{"memoryLimit":`+value+`}}`, "", ""), testEnv)
		if got := cfg.Global.Runtime.MemoryLimitBytes; got != want {
			t.Errorf("memoryLimit %s = %d, want %d", value, got, want)
		}
	}
}

func TestDecodeSettingsIsStrict(t *testing.T) {
	a := mustParse(t, doc("", "", `[{"username":"alice","settings":{"mule":"mulecon","muleAt":5000}}]`), testEnv).Accounts[0]
	var full struct {
		MuleName   string `json:"mule"`
		HandOverAt int    `json:"muleAt"`
	}
	if err := a.DecodeSettings(&full); err != nil || full.MuleName != "mulecon" || full.HandOverAt != 5000 {
		t.Fatalf("DecodeSettings = %+v, %v", full, err)
	}
	var narrow struct {
		MuleName string `json:"mule"`
	}
	err := a.DecodeSettings(&narrow)
	if err == nil || !strings.Contains(err.Error(), "idle") || !strings.Contains(err.Error(), `"muleAt"`) {
		t.Fatalf("DecodeSettings into a narrower struct = %v, want an unknown field error naming the script", err)
	}
}

func TestEveryProblemIsReported(t *testing.T) {
	_, err := parseIn(t, t.TempDir(), doc(`{"server":{"world":0},"runtime":{"maxProcs":-1}}`, "", `[{"username":"alice","state":{"viewRadius":99}}]`), testEnv)
	wantProblem(t, err, "global", "server.world", "between")
	wantProblem(t, err, "global", "runtime.maxProcs", "between")
	wantProblem(t, err, `accounts[0] "alice"`, "state.viewRadius", "between")
	if !strings.Contains(err.Error(), "3 problems") {
		t.Errorf("error = %v, want a count of 3", err)
	}
}

func TestByteOrderMarkIsAccepted(t *testing.T) {
	mustParse(t, "\xef\xbb\xbf"+doc("", "", ""), testEnv)
}

const examplePath = "../../settings.example.json"

func TestExampleFileLoads(t *testing.T) {
	t.Setenv("RSB0T_PASSWORD_EXAMPLE1", "examplepw1")
	t.Setenv("RSB0T_PASSWORD_EXAMPLE2", "examplepw2")
	cfg, err := Load(filepath.FromSlash(examplePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 3 || len(cfg.Enabled()) != 2 {
		t.Fatalf("accounts = %d, enabled = %d", len(cfg.Accounts), len(cfg.Enabled()))
	}
	crafter := cfg.Accounts[1]
	var settings struct {
		MuleName   string `json:"mule"`
		HandOverAt int    `json:"muleAt"`
	}
	if err := crafter.DecodeSettings(&settings); err != nil || settings.MuleName != "mulecon" || settings.HandOverAt != 2000 {
		t.Errorf("example2 settings = %+v, %v", settings, err)
	}
	if crafter.State.ViewRadius != 8 || crafter.State.Trackers != (Trackers{NPCs: true, Locs: true}) {
		t.Errorf("example2 state = %+v", crafter.State)
	}
	if off := cfg.Accounts[2]; off.Enabled || off.LogLevel != "debug" {
		t.Errorf("example3 = %+v", off)
	}

	_, err = LoadWith(filepath.FromSlash(examplePath), Options{Profile: "local", BaseDir: t.TempDir()})
	wantProblem(t, err, "global", "server.profiles.local", "needs its own login key")
}

// The example file doubles as the reference for every knob, so it must state the built-in defaults.
func TestExampleFileStatesTheBuiltInDefaults(t *testing.T) {
	base := filepath.Dir(filepath.FromSlash(examplePath))
	data, err := os.ReadFile(filepath.FromSlash(examplePath))
	if err != nil {
		t.Fatal(err)
	}
	var top struct {
		Global   json.RawMessage `json:"global"`
		Defaults json.RawMessage `json:"defaults"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"RSB0T_PASSWORD_A": "pw"}
	one := `[{"username":"a"}]`
	fromExample := mustParseIn(t, base, doc(string(top.Global), string(top.Defaults), one), env)
	builtIn := mustParseIn(t, base, doc(`{}`, `{"passwordEnv":"RSB0T_PASSWORD_{USER}","script":"ProgressiveCrafter","settings":{"mule":"mulecon","muleAt":5000}}`, one), env)
	if !reflect.DeepEqual(fromExample.Global, builtIn.Global) {
		t.Errorf("example global differs from the built-in defaults:\n example %+v\nbuilt-in %+v", fromExample.Global, builtIn.Global)
	}
	if !reflect.DeepEqual(fromExample.Accounts[0], builtIn.Accounts[0]) {
		t.Errorf("example defaults differ from the built-in defaults:\n example %+v\nbuilt-in %+v", fromExample.Accounts[0], builtIn.Accounts[0])
	}
}
