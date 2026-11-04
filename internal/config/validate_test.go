package config

import (
	"strings"
	"testing"
)

func TestValidationNamesSectionAndField(t *testing.T) {
	alice := `accounts[0] "alice"`
	acct := func(fields string) string { return doc("", "", `[{"username":"alice",`+fields+`}]`) }
	global := func(section string) string { return doc(section, "", "") }
	live := func(fields string) string {
		return global(`{"server":{"profiles":{"live":{"url":"wss://w1.example.invalid/","httpBase":"https://w1.example.invalid/",` + fields + `}}}}`)
	}
	const liveField = "server.profiles.live"
	cases := []struct {
		name, text, where, field, msg string
		env                           map[string]string
	}{
		{name: "syntax", text: `{"version":1,`, where: "file", msg: "invalid JSON at line 1"},
		{name: "trailing data", text: `{"version":1} {}`, where: "file", msg: "invalid JSON"},
		{name: "unknown top-level key", text: `{"version":1,"acounts":[]}`, where: "file", field: "acounts", msg: `unknown field "acounts"`},
		{name: "missing version", text: `{"accounts":[{"username":"alice"}]}`, where: "file", field: "version", msg: "must be 1, got 0"},
		{name: "accounts not an array", text: `{"version":1,"accounts":{}}`, where: "file", field: "accounts", msg: "want an array, got object"},
		{name: "no accounts", text: doc("", "", "[]"), where: "file", field: "accounts", msg: "at least one"},
		{name: "account not an object", text: doc("", "", "[5]"), where: "accounts[0]", msg: "want an object, got number"},
		{name: "global not an object", text: `{"version":1,"global":[],"accounts":[{"username":"alice"}]}`, where: "global", msg: "want an object, got array"},

		{name: "unknown nested key", text: global(`{"log":{"levle":"info"}}`), where: "global", field: "log.levle", msg: `unknown field "levle"`},
		{name: "key in the wrong case", text: global(`{"log":{"Level":"info"}}`), where: "global", field: "log.Level", msg: `did you mean "level"`},
		{name: "null value", text: global(`{"server":{"world":null}}`), where: "global", field: "server.world", msg: "null is not allowed"},
		{name: "wrong type", text: global(`{"server":{"world":"1"}}`), where: "global", field: "server.world", msg: "want a whole number, got string"},
		{name: "duration without a unit", text: global(`{"server":{"dialTimeout":15}}`), where: "global", field: "server.dialTimeout", msg: "duration string"},
		{name: "unreadable duration", text: global(`{"server":{"dialTimeout":"fifteen"}}`), where: "global", field: "server.dialTimeout", msg: `got "fifteen"`},

		{name: "world", text: global(`{"server":{"world":0}}`), where: "global", field: "server.world", msg: "between 1 and 255, got 0"},
		{name: "dial timeout", text: global(`{"server":{"dialTimeout":"0s"}}`), where: "global", field: "server.dialTimeout", msg: "between 1s and 2m"},
		{name: "read timeout", text: global(`{"server":{"readTimeout":"1h"}}`), where: "global", field: "server.readTimeout", msg: "between 5s and 10m"},
		{name: "unknown profile", text: global(`{"server":{"profile":"staging"}}`), where: "global", field: "server.profile", msg: `no profile named "staging"; have live, local`},
		{name: "profiles not an object", text: global(`{"server":{"profiles":[]}}`), where: "global", field: "server.profiles", msg: "want an object, got array"},
		{name: "unknown profile key", text: global(`{"server":{"profiles":{"local":{"rsaModulos":"1"}}}}`), where: "global", field: "server.profiles.local.rsaModulos", msg: `unknown field "rsaModulos"`},
		{name: "profile name", text: global(`{"server":{"profiles":{"my.server":{"url":"ws://localhost/"}}}}`), where: "global", field: "server.profiles.my.server", msg: "profile name"},
		{name: "profile url required", text: global(`{"server":{"profiles":{"dev":{"httpBase":"http://localhost/"}}}}`), where: "global", field: "server.profiles.dev.url", msg: "is required"},
		{name: "not a websocket url", text: global(`{"server":{"profiles":{"live":{"url":"https://w1.example.invalid/","httpBase":"https://w1.example.invalid/"}}}}`), where: "global", field: liveField + ".url", msg: "want ws://"},
		{name: "http base scheme", text: global(`{"server":{"profiles":{"live":{"url":"wss://w1.example.invalid/","httpBase":"ws://w1.example.invalid/"}}}}`), where: "global", field: liveField + ".httpBase", msg: "want http://"},
		{name: "local profile without a key", text: global(`{"server":{"profile":"local"}}`), where: "global", field: "server.profiles.local", msg: "needs its own login key"},
		{name: "local profile without an exponent", text: global(`{"server":{"profile":"local","profiles":{"local":{"url":"ws://localhost/","httpBase":"http://localhost/","rsaModulus":"` + testModulus + `"}}}}`), where: "global", field: "server.profiles.local.rsaExponent", msg: "is required"},
		{name: "http base required", text: global(`{"server":{"profile":"local","profiles":{"local":{"url":"ws://localhost/","rsaModulus":"` + testModulus + `","rsaExponent":"65537"}}}}`), where: "global", field: "server.profiles.local.httpBase", msg: "is required unless"},
		{name: "checksum count", text: live(`"crcTable":[1,2]`), where: "global", field: liveField + ".crcTable", msg: "want the 9 archive checksums"},
		{name: "checksum range", text: live(`"crcTable":[1,2,3,4,5,6,7,8,4294967296]`), where: "global", field: liveField + ".crcTable[8]", msg: "32-bit"},
		{name: "checksum type", text: live(`"crcTable":["a"]`), where: "global", field: liveField + ".crcTable", msg: "want an array of whole numbers"},
		{name: "short modulus", text: live(`"rsaModulus":"12345"`), where: "global", field: liveField + ".rsaModulus", msg: "250 decimal digits"},
		{name: "modulus and modulus file", text: live(`"rsaModulus":"` + testModulus + `","rsaModulusFile":"key.txt"`), where: "global", field: liveField + ".rsaModulusFile", msg: "not both"},
		{name: "missing modulus file", text: live(`"rsaModulusFile":"key.txt"`), where: "global", field: liveField + ".rsaModulusFile", msg: "key.txt"},
		{name: "even exponent", text: live(`"rsaExponent":"4"`), where: "global", field: liveField + ".rsaExponent", msg: "odd"},

		{name: "missing collision pack", text: global(`{"data":{"collision":"missing.lcnav"}}`), where: "global", field: "data.collision", msg: "missing.lcnav"},
		{name: "collision pack is a directory", text: global(`{"data":{"collision":"."}}`), where: "global", field: "data.collision", msg: "is a directory"},
		{name: "max sessions", text: global(`{"login":{"maxSessions":-1}}`), where: "global", field: "login.maxSessions", msg: "between 0 and 100000"},
		{name: "login delay", text: global(`{"login":{"loginDelay":"11m"}}`), where: "global", field: "login.loginDelay", msg: "between 0s and 10m"},
		{name: "unknown login key", text: global(`{"login":{"batchSize":4}}`), where: "global", field: "login.batchSize", msg: `unknown field "batchSize"`},
		{name: "missing credentials file", text: global(`{"login":{"credentialsFile":"nope.txt"}}`), where: "global", field: "login.credentialsFile", msg: "nope.txt"},

		{name: "memory limit word", text: global(`{"runtime":{"memoryLimit":"lots"}}`), where: "global", field: "runtime.memoryLimit", msg: `want "off" or a size`},
		{name: "memory limit decimal unit", text: global(`{"runtime":{"memoryLimit":"45MB"}}`), where: "global", field: "runtime.memoryLimit", msg: "binary units"},
		{name: "memory limit too small", text: global(`{"runtime":{"memoryLimit":"4MiB"}}`), where: "global", field: "runtime.memoryLimit", msg: "at least 8MiB"},
		{name: "gc percent", text: global(`{"runtime":{"gcPercent":0}}`), where: "global", field: "runtime.gcPercent", msg: "-1 (off) or between 10 and 1000"},
		{name: "gc off without a limit", text: global(`{"runtime":{"gcPercent":-1}}`), where: "global", field: "runtime.gcPercent", msg: "needs runtime.memoryLimit"},
		{name: "max procs", text: global(`{"runtime":{"maxProcs":-1}}`), where: "global", field: "runtime.maxProcs", msg: "between 0 and 1024"},

		{name: "log level", text: global(`{"log":{"level":"verbose"}}`), where: "global", field: "log.level", msg: `got "verbose"`},
		{name: "log output", text: global(`{"log":{"output":"both"}}`), where: "global", field: "log.output", msg: `got "both"`},
		{name: "per-account log without a dir", text: global(`{"log":{"output":"per-account","dir":""}}`), where: "global", field: "log.dir", msg: "is required"},
		{name: "memory report too often", text: global(`{"telemetry":{"memReportEvery":"1s"}}`), where: "global", field: "telemetry.memReportEvery", msg: "between 10s and 24h"},

		{name: "panel listen without a port", text: global(`{"controlPanel":{"listen":"8091"}}`), where: "global", field: "controlPanel.listen", msg: "want host:port"},
		{name: "panel port", text: global(`{"controlPanel":{"listen":"127.0.0.1:0"}}`), where: "global", field: "controlPanel.listen", msg: "between 1 and 65535"},
		{name: "panel public without a token", text: global(`{"controlPanel":{"listen":"0.0.0.0:8091"}}`), where: "global", field: "controlPanel.listen", msg: "reachable from other machines"},
		{name: "panel every interface without a token", text: global(`{"controlPanel":{"listen":":8091"}}`), where: "global", field: "controlPanel.listen", msg: "reachable from other machines"},
		{name: "panel token env and file", text: global(`{"controlPanel":{"tokenEnv":"PANEL","tokenFile":"t.txt"}}`), where: "global", field: "controlPanel.tokenFile", msg: "not both"},
		{name: "panel token env name", text: global(`{"controlPanel":{"tokenEnv":"PANEL-TOKEN"}}`), where: "global", field: "controlPanel.tokenEnv", msg: "environment variable name"},
		{name: "panel token unset", text: global(`{"controlPanel":{"enabled":true,"tokenEnv":"PANEL_TOKEN"}}`), where: "global", field: "controlPanel.tokenEnv", msg: "unset or empty"},
		{name: "panel token short", text: global(`{"controlPanel":{"enabled":true,"tokenEnv":"PANEL_TOKEN"}}`), where: "global", field: "controlPanel.tokenEnv", msg: "at least 16", env: withEnv(map[string]string{"PANEL_TOKEN": "short"})},
		{name: "panel token missing file", text: global(`{"controlPanel":{"enabled":true,"tokenFile":"token.txt"}}`), where: "global", field: "controlPanel.tokenFile", msg: "token.txt"},
		{name: "panel token in the settings file", text: global(`{"controlPanel":{"token":"0123456789abcdef"}}`), where: "global", field: "controlPanel.token", msg: `unknown field "token"`},
		{name: "panel event history", text: global(`{"controlPanel":{"eventHistory":8}}`), where: "global", field: "controlPanel.eventHistory", msg: "between 16 and 2048"},
		{name: "panel log history", text: global(`{"controlPanel":{"logHistory":20000}}`), where: "global", field: "controlPanel.logHistory", msg: "between 16 and 2048"},
		{name: "panel memory history", text: global(`{"controlPanel":{"memoryHistory":5}}`), where: "global", field: "controlPanel.memoryHistory", msg: "between 10 and 3600"},

		{name: "unknown defaults key", text: doc("", `{"passwordEnv":"PW_{USER}","state":{"trackers":{"npcz":true}}}`, ""), where: "defaults", field: "state.trackers.npcz", msg: `unknown field "npcz"`},
		{name: "username in defaults", text: doc("", `{"username":"alice","script":"idle"}`, ""), where: "defaults", field: "username", msg: "set per account"},
		{name: "out of range default", text: doc("", `{"passwordEnv":"PW_{USER}","state":{"viewRadius":0}}`, ""), where: "defaults", field: "state.viewRadius", msg: "between 1 and 15"},
		{name: "password env pattern", text: doc("", `{"passwordEnv":"PW-{USER}"}`, ""), where: "defaults", field: "passwordEnv", msg: "environment variable name"},

		{name: "unknown account key", text: acct(`"scirpt":"x"`), where: alice, field: "scirpt", msg: `unknown field "scirpt"`},
		{name: "inline password", text: acct(`"password":"hunter2"`), where: alice, field: "password", msg: "never go in the settings file"},
		{name: "missing username", text: doc("", "", `[{}]`), where: "accounts[0]", field: "username", msg: "is required"},
		{name: "username characters", text: doc("", "", `[{"username":"al!ce"}]`), where: `accounts[0] "al!ce"`, field: "username", msg: "may hold only"},
		{name: "username length", text: doc("", "", `[{"username":"abcdefghijklm"}]`), where: `accounts[0] "abcdefghijklm"`, field: "username", msg: "at most 12"},
		{name: "username padding", text: doc("", "", `[{"username":" alice"}]`), where: `accounts[0] " alice"`, field: "username", msg: "must not start or end"},
		{name: "duplicate username", text: doc("", "", `[{"username":"Bob_Smith"},{"username":"bob smith"}]`), where: `accounts[1] "bob smith"`, field: "username", msg: `same account as accounts[0] "Bob_Smith"`},
		{name: "script name pattern", text: acct(`"script":"9lives"`), where: alice, field: "script", msg: "want a letter"},
		{name: "settings not an object", text: acct(`"settings":[1]`), where: alice, field: "settings", msg: "want an object, got array"},
		{name: "negative max runtime", text: acct(`"maxRuntime":"-1s"`), where: alice, field: "maxRuntime", msg: "0s (no limit)"},
		{name: "start jitter", text: acct(`"startJitter":"2h"`), where: alice, field: "startJitter", msg: "between 0s and 1h"},
		{name: "start delay", text: acct(`"startDelay":"48h"`), where: alice, field: "startDelay", msg: "between 0s and 24h"},
		{name: "log level override", text: acct(`"logLevel":"loud"`), where: alice, field: "logLevel", msg: `got "loud"`},
		{name: "view radius", text: acct(`"state":{"viewRadius":16}`), where: alice, field: "state.viewRadius", msg: "between 1 and 15, got 16"},
		{name: "reconnect initial", text: acct(`"reconnect":{"initial":"0s"}`), where: alice, field: "reconnect.initial", msg: "between 1s and 1h"},
		{name: "reconnect max under initial", text: acct(`"reconnect":{"initial":"10m","max":"1m"}`), where: alice, field: "reconnect.max", msg: "at least reconnect.initial"},
		{name: "reconnect multiplier", text: acct(`"reconnect":{"multiplier":0.5}`), where: alice, field: "reconnect.multiplier", msg: "between 1 and 10"},
		{name: "reconnect jitter", text: acct(`"reconnect":{"jitter":2}`), where: alice, field: "reconnect.jitter", msg: "between 0 and 1"},
		{name: "reconnect max failures", text: acct(`"reconnect":{"maxFailures":-1}`), where: alice, field: "reconnect.maxFailures", msg: "between 0 and 10000"},

		{name: "password env unset", text: doc("", "", ""), where: alice, field: "password", msg: "PW_ALICE is unset", env: map[string]string{}},
		{name: "no password source", text: doc("", `{"script":"idle"}`, ""), where: alice, field: "password", msg: "set passwordEnv", env: map[string]string{}},
		{name: "password too long", text: doc("", "", ""), where: alice, field: "password", msg: "at most 20 characters, got 21", env: map[string]string{"PW_ALICE": strings.Repeat("x", 21)}},
		{name: "password not printable", text: doc("", "", ""), where: alice, field: "password", msg: "printable ASCII", env: map[string]string{"PW_ALICE": "pass\nword"}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			env := c.env
			if env == nil {
				env = testEnv
			}
			_, err := parseIn(t, t.TempDir(), c.text, env)
			wantProblem(t, err, c.where, c.field, c.msg)
		})
	}
}
